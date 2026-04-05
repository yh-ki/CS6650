package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

const (
	// followerReplicateDelay is how long a follower sleeps after receiving a
	// replication message before writing to its local store and responding.
	// This simulates real-world storage write latency and widens the
	// inconsistency window so unit tests can reliably catch stale reads.
	followerReplicateDelay = 100 * time.Millisecond

	// followerReadDelay is how long a follower sleeps before responding to a
	// read request forwarded by the leader (R=5 or R=3 fan-out).
	// Direct client reads (e.g. a load-test client hitting a follower's /get)
	// do NOT sleep — the delay only applies to leader-coordinated reads so
	// that the leader's fan-out timing is realistic.
	followerReadDelay = 50 * time.Millisecond
)

// Follower is a read/write replica node. It never initiates replication; it
// only accepts instructions from the leader.
//
// Routes registered:
//
//	POST /replicate     — leader pushes a write here (with version)
//	GET  /get           — direct client read (no delay)
//	GET  /leader_read   — leader-coordinated read (50ms delay)
//	GET  /local_read    — test-only: raw local value, no delay
type Follower struct {
	store *Store
	id    string // e.g. "follower-1", used in log lines
}

// NewFollower creates a Follower backed by a fresh store.
func NewFollower(id string) *Follower {
	return &Follower{
		store: NewStore(),
		id:    id,
	}
}

// RegisterRoutes attaches all follower endpoints to mux.
// Call this once during startup before http.ListenAndServe.
func (f *Follower) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/replicate", f.handleReplicate)
	mux.HandleFunc("/get", f.handleGet)
	mux.HandleFunc("/leader_read", f.handleLeaderRead)
	mux.HandleFunc("/local_read", f.handleLocalRead)
}

// handleReplicate receives a write from the leader and stores it locally.
//
// The leader sends the canonical key, value, and version it already assigned.
// The follower sleeps followerReplicateDelay before writing, simulating the
// time a real storage system would take to commit to disk.
//
// Sleeping BEFORE writing (not after) is intentional: it means the
// inconsistency window starts the moment the leader fires the request and
// ends when this handler returns. Unit tests that hit /local_read during
// that window will see stale data.
//
// Request body:  {"key": "...", "value": "...", "version": N}
// Response:      201 Created  {"version": N}
//                400          malformed body or missing fields
func (f *Follower) handleReplicate(w http.ResponseWriter, r *http.Request) {
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

	// Simulate storage write latency — this is where the inconsistency window lives.
	log.Printf("[%s] replicating key=%q version=%d — sleeping %v",
		f.id, req.Key, req.Version, followerReplicateDelay)
	time.Sleep(followerReplicateDelay)

	f.store.SetWithVersion(req.Key, req.Value, req.Version)
	log.Printf("[%s] committed key=%q version=%d", f.id, req.Key, req.Version)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"version": req.Version})
}

// handleGet serves direct client reads from the follower's local store.
//
// No artificial delay — if a client (or load-test) sends GET /get directly
// to a follower, we respond as fast as we can. The data may be stale if
// replication hasn't reached this node yet; that is expected and is exactly
// what the load-test staleness counter measures.
func (f *Follower) handleGet(w http.ResponseWriter, r *http.Request) {
	f.store.HandleGet(w, r)
}

// handleLeaderRead serves reads that are part of a leader-coordinated fan-out
// (R=5 or R=3). The leader calls this endpoint on each follower in parallel,
// collects all responses, and returns the one with the highest version.
//
// The 50ms delay simulates inter-node network + storage read latency, making
// leader-coordinated reads visibly slower than local reads in the load test.
func (f *Follower) handleLeaderRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "bad request: key query param required", http.StatusBadRequest)
		return
	}

	time.Sleep(followerReadDelay)

	entry, ok := f.store.Get(key)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// handleLocalRead is the test-only backdoor — returns whatever is in the
// local store right now with no delay. Used by unit tests to probe a
// follower's state during an in-flight replication.
func (f *Follower) handleLocalRead(w http.ResponseWriter, r *http.Request) {
	f.store.HandleLocalRead(w, r)
}

// --- Startup helper --------------------------------------------------------

// RunFollower is called from main() when ROLE=follower.
// It reads PORT and NODE_ID from the environment, registers routes, and
// starts listening. It blocks until the server exits.
func RunFollower(port, nodeID string) error {
	f := NewFollower(fmt.Sprintf("follower-%s", nodeID))
	mux := http.NewServeMux()
	f.RegisterRoutes(mux)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("[follower-%s] listening on %s", nodeID, addr)
	return http.ListenAndServe(addr, mux)
}
