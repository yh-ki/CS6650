package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
)

// Entry holds a value and a logical version number.
//
// Version starts at 1 on first write and increments on every subsequent write
// to the same key. It is used by multi-node read strategies (R=5, R=3) to
// decide which node holds the most recent data — the node with the highest
// version number wins.
//
// Version is never reset to 0 after a key is set; 0 means "key not found".
type Entry struct {
	Value   string `json:"value"`
	Version int64  `json:"version"`
}

// Store is a thread-safe in-memory key-value store.
//
// All exported methods are safe for concurrent use. The store uses a single
// RWMutex: multiple readers can hold the lock simultaneously, but a writer
// gets exclusive access. This is fine for our workload — reads will dominate
// in most load-test scenarios.
//
// We also keep a global monotonic version counter (lastVersion) so that
// version numbers are globally increasing across all keys, not just per-key.
// This makes it easier to compare two entries across a read-repair scenario:
// the entry with the higher Version is always the newer one, regardless of key.
type Store struct {
	mu          sync.RWMutex
	data        map[string]Entry
	lastVersion int64 // atomically incremented; never decremented
}

// NewStore creates an empty store ready for use.
func NewStore() *Store {
	return &Store{
		data: make(map[string]Entry),
	}
}

// Set stores value under key and returns the version number assigned to this write.
//
// The key must be non-empty (enforced by the HTTP handler, not here).
// The empty string is a valid value.
//
// Version numbers are assigned by atomically incrementing lastVersion, so
// concurrent writes to different keys still produce strictly increasing
// version numbers — important for read-repair correctness.
func (s *Store) Set(key, value string) int64 {
	version := atomic.AddInt64(&s.lastVersion, 1)

	s.mu.Lock()
	s.data[key] = Entry{Value: value, Version: version}
	s.mu.Unlock()

	return version
}

// SetWithVersion stores an entry with an explicit version number.
//
// This is used by followers and leaderless nodes when they receive a
// replication message from a coordinator. The coordinator already assigned
// the canonical version; we must store exactly that version, not a new one.
//
// SetWithVersion also updates lastVersion if the incoming version is higher
// than anything we have seen, so our own future writes continue to produce
// strictly increasing numbers even after receiving out-of-order replication.
func (s *Store) SetWithVersion(key, value string, version int64) {
	s.mu.Lock()
	s.data[key] = Entry{Value: value, Version: version}
	s.mu.Unlock()

	// Ratchet lastVersion upward so our next atomic.AddInt64 produces a
	// number higher than anything we have stored.
	for {
		current := atomic.LoadInt64(&s.lastVersion)
		if version <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&s.lastVersion, current, version) {
			break
		}
	}
}

// Get returns the Entry for key and a boolean indicating whether the key exists.
//
// A zero Entry (Version == 0) is never stored, so callers can treat
// Version == 0 as "not found" without checking the boolean separately,
// though checking the boolean is cleaner and is what the HTTP handlers do.
func (s *Store) Get(key string) (Entry, bool) {
	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()
	return entry, ok
}

// --- HTTP handlers ---------------------------------------------------------
//
// These are the two public API endpoints plus the test-only local_read.
// They are registered on whichever mux the node (leader / follower /
// leaderless) sets up. The node is responsible for the replication logic
// around these handlers; the store itself knows nothing about networking.

// HandleSet is the HTTP handler for POST /set.
//
// Expected JSON body: {"key": "...", "value": "..."}
//
// This handler only writes to the LOCAL store. In the leader-follower
// architecture the leader wraps this call with replication logic; in the
// leaderless architecture any node wraps it with coordinator logic.
// The handler itself is intentionally dumb.
//
// Returns:
//   201 Created  on success, with body {"version": N}
//   400 Bad Request if key is empty or body is malformed
func (s *Store) HandleSet(w http.ResponseWriter, r *http.Request) {
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

	version := s.Set(req.Key, req.Value)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"version": version})
}

// HandleSetWithVersion is used by replication messages from a coordinator.
//
// Expected JSON body: {"key": "...", "value": "...", "version": N}
//
// This endpoint is NOT part of the public client-facing API. It is called
// only by the leader (or write coordinator in leaderless) to push a write
// to a follower / peer with the version the coordinator already assigned.
//
// Returns 201 Created on success.
func (s *Store) HandleSetWithVersion(w http.ResponseWriter, r *http.Request) {
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

	s.SetWithVersion(req.Key, req.Value, req.Version)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int64{"version": req.Version})
}

// HandleGet is the HTTP handler for GET /get?key=...
//
// This handler only reads from the LOCAL store. In the leader-follower
// architecture the leader may fan this out to multiple nodes depending on R;
// this handler is what each node uses to satisfy its local portion of that
// fan-out (and is also what a client hits when reading from a follower directly).
//
// Returns:
//   200 OK   with body {"value": "...", "version": N}
//   400      if key query param is missing
//   404      if key is not present in this node's store
func (s *Store) HandleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "bad request: key query param required", http.StatusBadRequest)
		return
	}

	entry, ok := s.Get(key)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
}

// HandleLocalRead is a test-only endpoint: GET /local_read?key=...
//
// It behaves identically to HandleGet — it reads directly from this node's
// local store with no replication or fan-out — but is registered on a
// separate route so tests can distinguish "I explicitly want this node's raw
// local value" from "I want the globally consistent value."
//
// Why this matters: during a W=5 write the leader is sequentially replicating
// to followers one by one (with 200ms gaps). If a test hits /local_read on
// Follower 3 before it has received its replication message, it will see
// either the old value or a 404. That inconsistency window is what the unit
// tests are designed to expose.
//
// This endpoint should NOT be registered in production builds. It is safe
// to leave it here because all callers gate on it by route name.
func (s *Store) HandleLocalRead(w http.ResponseWriter, r *http.Request) {
	// Intentionally identical to HandleGet — the separation is semantic only.
	s.HandleGet(w, r)
}
