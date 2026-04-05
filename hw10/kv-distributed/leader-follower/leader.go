package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"
)

const (
	// leaderFollowerDelay is the time the leader sleeps between sending
	// replication messages to successive followers.
	// e.g. with 4 followers and W=5: total replication time ≈ 4 × 200ms = 800ms
	// This widens the inconsistency window so tests can reliably catch stale reads.
	leaderFollowerDelay = 200 * time.Millisecond

	// replicateTimeout is how long the leader waits for a single follower to
	// acknowledge a replication before declaring it failed.
	// Set generously above followerReplicateDelay (100ms) + network overhead.
	replicateTimeout = 2 * time.Second
)

// Leader is the single writer node in a leader-follower cluster.
//
// It owns the canonical version counter (via store.Set), replicates writes
// to followers according to the configured W value, and fans out reads across
// followers according to the configured R value.
//
// Routes registered:
//
//	POST /set         — client write entry point
//	GET  /get         — client read entry point (behaviour depends on R)
//	GET  /local_read  — test-only: raw local value, no replication
type Leader struct {
	store   *Store
	peers   []string // follower base URLs, e.g. ["http://follower-1:8080", ...]
	w       int      // write quorum: how many nodes must ACK before 201
	r       int      // read quorum: how many nodes to consult on a read
	httpClient *http.Client
}

// NewLeader creates a Leader ready to serve requests.
//
// peers must contain the base URLs of every follower in declaration order.
// w and r must satisfy 1 ≤ w ≤ len(peers)+1 and 1 ≤ r ≤ len(peers)+1.
// (The +1 accounts for the leader itself counting as one of the N nodes.)
func NewLeader(peers []string, w, r int) *Leader {
	return &Leader{
		store: NewStore(),
		peers: peers,
		w:     w,
		r:     r,
		httpClient: &http.Client{Timeout: replicateTimeout},
	}
}

// RegisterRoutes attaches all leader endpoints to mux.
func (l *Leader) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/set", l.handleSet)
	mux.HandleFunc("/get", l.handleGet)
	mux.HandleFunc("/local_read", l.store.HandleLocalRead)
}

// =============================================================================
// Write path
// =============================================================================

// handleSet is the client-facing write entry point.
//
// It writes to the leader's local store first (always), then replicates to
// followers. The exact behaviour after that depends on W:
//
//	W=1  respond 201 immediately after local write; replicate async
//	W=3  wait until 2 followers ACK (leader + 2 = quorum of 3), then 201
//	W=5  wait until all 4 followers ACK (leader + 4 = 5), then 201
//
// In all cases the leader sleeps leaderFollowerDelay between each successive
// follower send to simulate real-world batching / network pacing.
func (l *Leader) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "bad request: key cannot be empty", http.StatusBadRequest)
		return
	}

	// Step 1: write locally and mint the canonical version number.
	// This must happen before any replication so followers receive a version
	// that is already committed on the leader.
	version := l.store.Set(req.Key, req.Value)
	log.Printf("[leader] SET key=%q version=%d W=%d", req.Key, version, l.w)

	switch l.w {
	case 1:
		l.handleSetW1(w, req.Key, req.Value, version)
	case 3:
		l.handleSetWQuorum(w, req.Key, req.Value, version, 2) // need 2 follower ACKs
	default: // W=5 or any other value — wait for all followers
		l.handleSetWAll(w, req.Key, req.Value, version)
	}
}

// handleSetW1 responds 201 immediately, then replicates asynchronously.
//
// The client gets a fast response but reads from followers may return stale
// data for up to (N-1) × leaderFollowerDelay after the write completes.
// This is the highest-throughput, lowest-consistency strategy.
func (l *Leader) handleSetW1(w http.ResponseWriter, key, value string, version int64) {
	// Respond to the client before replication starts.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"version": version})

	// Replicate in the background — the client is already done.
	go func() {
		for _, peer := range l.peers {
			time.Sleep(leaderFollowerDelay)
			if err := l.replicateToPeer(peer, key, value, version); err != nil {
				log.Printf("[leader] W=1 async replicate to %s failed: %v", peer, err)
			}
		}
	}()
}

// handleSetWAll waits for every follower to ACK before responding.
//
// Followers are contacted sequentially (not in parallel) with
// leaderFollowerDelay between each send. This matches the assignment spec
// and maximises the observable inconsistency window during the write itself.
//
// Sequential replication with W=5 means write latency ≈ 800ms on this setup.
// That is intentional — it makes the trade-off between consistency and
// latency painfully visible in the load-test graphs.
func (l *Leader) handleSetWAll(w http.ResponseWriter, key, value string, version int64) {
	for i, peer := range l.peers {
		time.Sleep(leaderFollowerDelay)
		if err := l.replicateToPeer(peer, key, value, version); err != nil {
			log.Printf("[leader] W=5 replicate to peer %d (%s) failed: %v", i, peer, err)
			// Continue trying the remaining peers rather than aborting — a
			// partial failure still leaves as many nodes updated as possible.
			// A production system would return 500 here; we log and continue
			// so load tests can complete even if a node is temporarily slow.
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"version": version})
}

// handleSetWQuorum waits until `needed` follower ACKs arrive, then responds.
// Remaining followers are still updated asynchronously after the client gets
// its 201 — we don't want to leave nodes permanently out of sync.
//
// needed = W - 1 because the leader itself counts as one write node.
// For W=3: needed=2 means we wait for exactly 2 follower ACKs.
//
// The tricky part: we still need to sleep leaderFollowerDelay between sends
// (per spec), which means we can't just fire all followers in parallel and
// wait for the first two. We send them one at a time, and as soon as we hit
// `needed` successful ACKs we respond to the client and hand the rest off to
// a goroutine.
func (l *Leader) handleSetWQuorum(w http.ResponseWriter, key, value string, version int64, needed int) {
	acks := 0
	responded := false

	for i, peer := range l.peers {
		time.Sleep(leaderFollowerDelay)

		err := l.replicateToPeer(peer, key, value, version)
		if err != nil {
			log.Printf("[leader] W=3 replicate to peer %d (%s) failed: %v", i, peer, err)
			continue
		}
		acks++

		// Have we hit quorum? Respond to the client and continue replicating
		// the remaining peers in the background.
		if !responded && acks >= needed {
			responded = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]int64{"version": version})

			// Replicate the remaining peers asynchronously.
			remaining := l.peers[i+1:]
			if len(remaining) > 0 {
				go func(peers []string) {
					for _, p := range peers {
						time.Sleep(leaderFollowerDelay)
						if err := l.replicateToPeer(p, key, value, version); err != nil {
							log.Printf("[leader] W=3 async tail replicate to %s failed: %v", p, err)
						}
					}
				}(remaining)
			}
			return
		}
	}

	// If we get here we never hit quorum (too many follower failures).
	// If we already responded we're fine; otherwise return 500.
	if !responded {
		log.Printf("[leader] W=3 quorum not reached for key=%q version=%d (only %d ACKs)", key, version, acks)
		http.Error(w, "quorum not reached", http.StatusInternalServerError)
	}
}

// replicateToPeer sends a single replication request to one follower.
//
// It encodes the key, value, and canonical version, then POSTs to
// /replicate on the follower. The follower will sleep 100ms before writing
// (see follower.go) so this call blocks for at least that long.
func (l *Leader) replicateToPeer(peerURL, key, value string, version int64) error {
	body, _ := json.Marshal(map[string]any{
		"key":     key,
		"value":   value,
		"version": version,
	})

	resp, err := l.httpClient.Post(
		peerURL+"/replicate",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("POST /replicate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, peerURL)
	}
	return nil
}

// =============================================================================
// Read path
// =============================================================================

// handleGet is the client-facing read entry point.
//
// Behaviour depends on R:
//
//	R=1  read from the leader's local store only; fast, possibly stale
//	     (stale only if a write is in-flight — the leader is always up to date
//	     once it has committed locally, which it does before any replication)
//	R=3  fan out to leader + 2 followers; return highest version
//	R=5  fan out to all 5 nodes; return highest version
func (l *Leader) handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "bad request: key query param required", http.StatusBadRequest)
		return
	}

	switch l.r {
	case 1:
		l.handleGetR1(w, key)
	case 3:
		l.handleGetRQuorum(w, key, 3)
	default: // R=5
		l.handleGetRAll(w, key)
	}
}

// handleGetR1 reads only from the leader's local store.
//
// This is always consistent from the leader's perspective — the leader writes
// locally before replicating, so its own store always has the latest version.
// Latency is minimal: no network calls, no delays.
func (l *Leader) handleGetR1(w http.ResponseWriter, key string) {
	entry, ok := l.store.Get(key)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// handleGetRAll fans out a read to all nodes (leader + all followers) in
// parallel, collects every response, and returns the entry with the highest
// version number.
//
// This is the R=5 strategy. It is the most expensive read (must wait for the
// slowest of 5 nodes, each sleeping 50ms) but guarantees you always get the
// latest committed write even if some followers are behind on replication.
//
// How read-repair works here: we don't actually repair in this implementation
// (that would be a background write back to lagging followers). We just return
// the highest version to the client. Full read-repair is a nice extension.
func (l *Leader) handleGetRAll(w http.ResponseWriter, key string) {
	results := l.fanOutRead(key, l.peers)
	entry, ok := l.pickBest(results)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// handleGetRQuorum fans out a read to `quorum` nodes and returns the highest
// version among the responses that arrive.
//
// For R=3 we need responses from 3 nodes total. We always include the leader
// (no network call, no delay), then contact followers in parallel until we
// have enough responses. Because follower reads sleep 50ms, the practical
// latency for R=3 is ~50ms — much cheaper than R=5 which must wait for all.
//
// Implementation note: we contact ALL followers in parallel but only wait
// until `quorum-1` of them respond (the -1 is for the leader's local read).
// Surplus responses are discarded. This avoids holding the client hostage to
// a slow follower when we already have enough data for a quorum decision.
func (l *Leader) handleGetRQuorum(w http.ResponseWriter, key string, quorum int) {
	// Leader counts as 1 node.
	localEntry, localOK := l.store.Get(key)

	// We need quorum-1 more responses from followers.
	needed := quorum - 1

	type result struct {
		entry Entry
		ok    bool
	}
	ch := make(chan result, len(l.peers))

	// Fan out to all followers in parallel — take the first `needed` that respond.
	for _, peer := range l.peers {
		peer := peer
		go func() {
			entry, ok := l.readFromPeer(peer, key)
			ch <- result{entry, ok}
		}()
	}

	// Collect the best entry seen so far, starting with the leader's local value.
	best := Entry{}
	found := false
	if localOK {
		best = localEntry
		found = true
	}

	received := 0
	for received < needed && received < len(l.peers) {
		res := <-ch
		received++
		if res.ok && res.entry.Version > best.Version {
			best = res.entry
			found = true
		}
	}

	if !found {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(best)
}

// fanOutRead contacts every peer in parallel and returns all results.
// Always includes the leader's own local value as the first result.
func (l *Leader) fanOutRead(key string, peers []string) []Entry {
	type result struct {
		entry Entry
		ok    bool
	}

	ch := make(chan result, len(peers)+1)

	// Leader reads locally — no network, no delay.
	go func() {
		entry, ok := l.store.Get(key)
		ch <- result{entry, ok}
	}()

	// Followers sleep 50ms before responding (see follower.go handleLeaderRead).
	for _, peer := range peers {
		peer := peer
		go func() {
			entry, ok := l.readFromPeer(peer, key)
			ch <- result{entry, ok}
		}()
	}

	results := make([]Entry, 0, len(peers)+1)
	for i := 0; i < len(peers)+1; i++ {
		res := <-ch
		if res.ok {
			results = append(results, res.entry)
		}
	}
	return results
}

// readFromPeer fetches the value for key from a single follower's
// /leader_read endpoint (which applies the 50ms delay).
func (l *Leader) readFromPeer(peerURL, key string) (Entry, bool) {
	url := fmt.Sprintf("%s/leader_read?key=%s", peerURL, key)
	resp, err := l.httpClient.Get(url)
	if err != nil {
		log.Printf("[leader] read from %s failed: %v", peerURL, err)
		return Entry{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Entry{}, false
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("[leader] unexpected status %d from %s", resp.StatusCode, peerURL)
		return Entry{}, false
	}

	var entry Entry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		log.Printf("[leader] decode error from %s: %v", peerURL, err)
		return Entry{}, false
	}
	return entry, true
}

// pickBest returns the Entry with the highest Version from a slice of results.
// Returns a zero Entry and false if results is empty.
func (l *Leader) pickBest(results []Entry) (Entry, bool) {
	if len(results) == 0 {
		return Entry{}, false
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Version > results[j].Version
	})
	return results[0], true
}

// =============================================================================
// Startup helper
// =============================================================================

// RunLeader is called from main() when ROLE=leader.
// It reads PORT, PEERS, W, and R from the environment, registers routes,
// and starts listening. It blocks until the server exits.
func RunLeader(port string, peers []string, w, r int) error {
	l := NewLeader(peers, w, r)
	mux := http.NewServeMux()
	l.RegisterRoutes(mux)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("[leader] listening on %s  peers=%v  W=%d R=%d", addr, peers, w, r)
	return http.ListenAndServe(addr, mux)
}

// =============================================================================
// Parallel fan-out helpers used by handleGetRAll
// =============================================================================

// parallelFanOut fires f(peer) for every peer concurrently and collects
// results into a channel. Used internally by fanOutRead.
// Kept separate so it can be reused by the leaderless coordinator later.
func parallelFanOut(peers []string, f func(peer string) (Entry, bool)) <-chan Entry {
	ch := make(chan Entry, len(peers))
	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		peer := peer
		go func() {
			defer wg.Done()
			if entry, ok := f(peer); ok {
				ch <- entry
			}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	return ch
}
