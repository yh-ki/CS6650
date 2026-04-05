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
	// coordinatorPeerDelay is how long the write coordinator sleeps between
	// successive replication sends to peers. Mirrors leaderFollowerDelay in
	// leader.go — same spec, same value, same reasoning.
	coordinatorPeerDelay = 200 * time.Millisecond

	// leaderlessPeerTimeout is how long a coordinator waits for one peer's
	// /replicate response before declaring it failed.
	leaderlessPeerTimeout = 2 * time.Second
)

// LeaderlessNode is a fully symmetric cluster member. Every node exposes
// identical routes. When a write arrives, that node becomes the Write
// Coordinator for that request and is responsible for propagating the write
// to all peers before responding to the client.
//
// Routes registered:
//
//	POST /set          — client write; this node becomes write coordinator
//	GET  /get          — client read; returns this node's local value (R=1)
//	POST /replicate    — peer-to-peer replication (coordinator → this node)
//	GET  /local_read   — test-only backdoor; raw local value, no delay
//
// W=5 R=1 is the only configuration tested in the assignment, but W and R
// are stored so the struct can be extended later without changing the API.
type LeaderlessNode struct {
	store      *Store
	peers      []string // base URLs of every OTHER node in the cluster
	id         string   // e.g. "node-1", used in log lines
	w          int      // write quorum (typically == len(peers)+1 == N)
	r          int      // read quorum (1 for this assignment)
	httpClient *http.Client
}

// NewLeaderlessNode creates a node ready to serve requests.
func NewLeaderlessNode(id string, peers []string, w, r int) *LeaderlessNode {
	return &LeaderlessNode{
		store: NewStore(),
		peers: peers,
		id:    id,
		w:     w,
		r:     r,
		httpClient: &http.Client{Timeout: leaderlessPeerTimeout},
	}
}

// RegisterRoutes attaches all leaderless endpoints to mux.
func (n *LeaderlessNode) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/set", n.handleSet)
	mux.HandleFunc("/get", n.handleGet)
	mux.HandleFunc("/replicate", n.handleReplicate)
	mux.HandleFunc("/local_read", n.store.HandleLocalRead)
}

// =============================================================================
// Write path — this node acts as Write Coordinator
// =============================================================================

// handleSet is the client-facing write entry point.
//
// The node that receives this request becomes the Write Coordinator. It must:
//   1. Write to its own local store and mint the canonical version number.
//   2. Replicate to all peers sequentially (with coordinatorPeerDelay between each).
//   3. Wait for all peer ACKs (W=N=5 means every node must confirm).
//   4. Only then return 201 to the client.
//
// This is intentionally strict: W=N means no node is left behind before the
// client gets its response. The trade-off is high write latency — the
// inconsistency window exists only DURING replication (between step 2 sends),
// not AFTER the coordinator responds. Unit tests must race inside that window.
func (n *LeaderlessNode) handleSet(w http.ResponseWriter, r *http.Request) {
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

	// Step 1: write locally, mint the canonical version.
	// This node's store is the source of truth for version assignment on this
	// particular write. Because every node uses a global monotonic counter
	// (store.lastVersion), the version is unique cluster-wide as long as
	// concurrent writes go to different coordinators for different keys.
	// If two coordinators write the SAME key concurrently, they may produce
	// different versions — last writer wins at each node, which is the
	// expected eventual-consistency behaviour for a leaderless system.
	version := n.store.Set(req.Key, req.Value)
	log.Printf("[%s] SET key=%q version=%d — coordinating W=%d peers",
		n.id, req.Key, version, n.w)

	// Step 2+3: replicate to all peers sequentially, collect ACKs.
	acks := 1 // coordinator itself counts as 1
	for i, peer := range n.peers {
		time.Sleep(coordinatorPeerDelay)
		if err := n.replicateToPeer(peer, req.Key, req.Value, version); err != nil {
			log.Printf("[%s] replicate to peer %d (%s) failed: %v", n.id, i+1, peer, err)
			// Continue — we still want to update as many nodes as possible.
			// A production system would track failures and potentially return 500.
		} else {
			acks++
		}
	}

	// Step 4: respond. We respond regardless of whether we hit exact quorum
	// (some peers may have timed out) so load tests can complete. In a real
	// system you'd return 500 if acks < w.
	log.Printf("[%s] write complete key=%q version=%d acks=%d/%d",
		n.id, req.Key, version, acks, n.w)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"version": version})
}

// replicateToPeer sends a replication message to one peer.
// Identical in structure to leader.go's replicateToPeer — extracted here
// rather than shared to keep the two implementations independent.
func (n *LeaderlessNode) replicateToPeer(peerURL, key, value string, version int64) error {
	body, _ := json.Marshal(map[string]any{
		"key":     key,
		"value":   value,
		"version": version,
	})

	resp, err := n.httpClient.Post(
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
// Replicate endpoint — receives writes from the coordinator
// =============================================================================

// handleReplicate receives a replication push from whichever node is
// currently acting as Write Coordinator for this write.
//
// It is identical to the follower's handleReplicate: sleep 100ms to simulate
// storage commit latency, then store the value with the coordinator's version.
//
// The 100ms sleep is what creates the inconsistency window that unit tests
// are designed to catch. During that sleep, /local_read on this node will
// return the old value (or 404 if this is the first write to this key).
func (n *LeaderlessNode) handleReplicate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Key     string `json:"key"`
		Value   string `json:"value"`
		Version int64  `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" || req.Version <= 0 {
		http.Error(w, "bad request: key and positive version required", http.StatusBadRequest)
		return
	}

	log.Printf("[%s] replicating key=%q version=%d — sleeping %v",
		n.id, req.Key, req.Version, followerReplicateDelay)
	time.Sleep(followerReplicateDelay) // reuse constant from follower.go

	n.store.SetWithVersion(req.Key, req.Value, req.Version)
	log.Printf("[%s] committed key=%q version=%d", n.id, req.Key, req.Version)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"version": req.Version})
}

// =============================================================================
// Read path — R=1, always local
// =============================================================================

// handleGet serves a client read from this node's local store only.
//
// With R=1 there is no fan-out: the node returns whatever it has locally.
// If this node has not yet received the replication for a recent write (i.e.
// a different node was the coordinator and replication is still in-flight),
// the response will be stale. This is the inconsistency the assignment asks
// you to expose and measure.
//
// For the leaderless W=N R=1 case, stale reads are only possible during the
// replication window (coordinator is mid-loop sending to peers). After the
// coordinator responds 201 to the client, all nodes should be up to date.
// The unit test must therefore send reads to non-coordinator nodes BEFORE
// the coordinator's 201 arrives — i.e., in a separate goroutine that races
// the write.
func (n *LeaderlessNode) handleGet(w http.ResponseWriter, r *http.Request) {
	n.store.HandleGet(w, r)
}

// =============================================================================
// Optional: multi-node read fan-out (not required for W=5 R=1 but useful
// if you extend to other quorum configurations)
// =============================================================================

// readQuorum fans out a GET to `quorum` nodes and returns the entry with the
// highest version. Always includes this node's local value as one of the reads.
// Not called by handleGet in the default W=5 R=1 config — wired in if R>1.
func (n *LeaderlessNode) readQuorum(key string, quorum int) (Entry, bool) {
	type result struct {
		entry Entry
		ok    bool
	}

	ch := make(chan result, len(n.peers)+1)

	// Local read — no network, no delay.
	go func() {
		entry, ok := n.store.Get(key)
		ch <- result{entry, ok}
	}()

	// Peer reads — each peer sleeps 50ms (via /leader_read endpoint).
	// Note: leaderless nodes share the /leader_read route with followers
	// so the same delay logic applies.
	for _, peer := range n.peers {
		peer := peer
		go func() {
			resp, err := n.httpClient.Get(
				fmt.Sprintf("%s/leader_read?key=%s", peer, key),
			)
			if err != nil || resp.StatusCode == http.StatusNotFound {
				ch <- result{}
				return
			}
			defer resp.Body.Close()
			var entry Entry
			if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
				ch <- result{}
				return
			}
			ch <- result{entry, true}
		}()
	}

	best := Entry{}
	found := false
	collected := 0

	for collected < quorum && collected < len(n.peers)+1 {
		res := <-ch
		collected++
		if res.ok && res.entry.Version > best.Version {
			best = res.entry
			found = true
		}
	}

	return best, found
}

// =============================================================================
// Startup helper
// =============================================================================

// RunLeaderless is called from main() when ROLE=leaderless.
func RunLeaderless(port, nodeID string, peers []string, w, r int) error {
	n := NewLeaderlessNode(fmt.Sprintf("node-%s", nodeID), peers, w, r)
	mux := http.NewServeMux()
	n.RegisterRoutes(mux)

	// Also register /leader_read so peers can fan-out reads to this node
	// if R>1 in a future configuration.
	mux.HandleFunc("/leader_read", func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		key := req.URL.Query().Get("key")
		if key == "" {
			http.Error(rw, "bad request: key query param required", http.StatusBadRequest)
			return
		}
		time.Sleep(followerReadDelay) // reuse constant from follower.go
		entry, ok := n.store.Get(key)
		if !ok {
			http.Error(rw, "not found", http.StatusNotFound)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(entry)
	})

	addr := fmt.Sprintf(":%s", port)
	log.Printf("[node-%s] leaderless listening on %s  peers=%v  W=%d R=%d",
		nodeID, addr, peers, w, r)
	return http.ListenAndServe(addr, mux)
}

// =============================================================================
// Shared fan-out utility (mirrors leader.go parallelFanOut)
// =============================================================================

// fanOutWrites sends a replication request to every peer in parallel and
// collects (peer, error) results. Used when you want to see which peers
// failed rather than just counting ACKs.
//
// Unlike the sequential loop in handleSet, this fires everything at once.
// It is provided as a utility for testing and for future W<N configurations
// where you only need a subset of peers to ACK before responding.
func (n *LeaderlessNode) fanOutWrites(key, value string, version int64) map[string]error {
	type result struct {
		peer string
		err  error
	}

	ch := make(chan result, len(n.peers))
	var wg sync.WaitGroup

	for _, peer := range n.peers {
		wg.Add(1)
		peer := peer
		go func() {
			defer wg.Done()
			err := n.replicateToPeer(peer, key, value, version)
			ch <- result{peer, err}
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	errs := make(map[string]error, len(n.peers))
	for res := range ch {
		errs[res.peer] = res.err
	}
	return errs
}

// bestEntry returns the Entry with the highest Version from a slice.
func bestEntry(entries []Entry) (Entry, bool) {
	if len(entries) == 0 {
		return Entry{}, false
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Version > entries[j].Version
	})
	return entries[0], true
}
