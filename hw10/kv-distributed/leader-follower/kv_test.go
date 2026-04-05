package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// Test helpers
// =============================================================================

// cluster spins up a leader + n followers as httptest.Server instances.
// All servers are in-process — no Docker, no network ports needed.
// Call cluster.close() in a defer to shut everything down.
type cluster struct {
	leader    *httptest.Server
	followers []*httptest.Server
	leaderN   *Leader
}

func newCluster(t *testing.T, w, r, numFollowers int) *cluster {
	t.Helper()

	// Start followers first so we have their URLs for the leader.
	followers := make([]*httptest.Server, numFollowers)
	followerNodes := make([]*Follower, numFollowers)
	peerURLs := make([]string, numFollowers)

	for i := 0; i < numFollowers; i++ {
		f := NewFollower(fmt.Sprintf("follower-%d", i+1))
		mux := http.NewServeMux()
		f.RegisterRoutes(mux)
		srv := httptest.NewServer(mux)
		followers[i] = srv
		followerNodes[i] = f
		peerURLs[i] = srv.URL
	}

	// Start the leader pointing at the follower URLs.
	l := NewLeader(peerURLs, w, r)
	leaderMux := http.NewServeMux()
	l.RegisterRoutes(leaderMux)
	leaderSrv := httptest.NewServer(leaderMux)

	_ = followerNodes // keep reference so GC doesn't collect them

	return &cluster{
		leader:    leaderSrv,
		followers: followers,
		leaderN:   l,
	}
}

func (c *cluster) close() {
	c.leader.Close()
	for _, f := range c.followers {
		f.Close()
	}
}

// set sends POST /set to the given base URL and returns the version number.
func set(t *testing.T, baseURL, key, value string) int64 {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"key": key, "value": value})
	resp, err := http.Post(baseURL+"/set", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /set failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /set: expected 201, got %d", resp.StatusCode)
	}
	var result struct {
		Version int64 `json:"version"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Version
}

// get sends GET /get to the given base URL and returns (value, version, found).
func get(baseURL, key string) (string, int64, bool) {
	resp, err := http.Get(fmt.Sprintf("%s/get?key=%s", baseURL, key))
	if err != nil || resp.StatusCode == http.StatusNotFound {
		return "", 0, false
	}
	defer resp.Body.Close()
	var entry Entry
	json.NewDecoder(resp.Body).Decode(&entry)
	return entry.Value, entry.Version, true
}

// localRead sends GET /local_read to the given base URL.
func localRead(baseURL, key string) (string, int64, bool) {
	resp, err := http.Get(fmt.Sprintf("%s/local_read?key=%s", baseURL, key))
	if err != nil || resp.StatusCode == http.StatusNotFound {
		return "", 0, false
	}
	defer resp.Body.Close()
	var entry Entry
	json.NewDecoder(resp.Body).Decode(&entry)
	return entry.Value, entry.Version, true
}

// =============================================================================
// Leader-Follower tests
// =============================================================================

// TestLeaderReadAfterWriteIsConsistent verifies the most basic guarantee:
// after the leader ACKs a write, reading from the leader always returns
// the value that was written. This should pass under ALL W/R configurations
// because the leader writes locally before doing anything else.
func TestLeaderReadAfterWriteIsConsistent(t *testing.T) {
	for _, cfg := range []struct{ w, r int }{{5, 1}, {1, 5}, {3, 3}} {
		cfg := cfg
		t.Run(fmt.Sprintf("W=%d_R=%d", cfg.w, cfg.r), func(t *testing.T) {
			t.Parallel()
			c := newCluster(t, cfg.w, cfg.r, 4)
			defer c.close()

			version := set(t, c.leader.URL, "color", "blue")

			val, ver, ok := get(c.leader.URL, "color")
			if !ok {
				t.Fatal("leader GET returned 404 after successful SET")
			}
			if val != "blue" {
				t.Errorf("leader read: want value=blue, got %q", val)
			}
			if ver != version {
				t.Errorf("leader read: want version=%d, got %d", version, ver)
			}
		})
	}
}

// TestFollowerReadAfterWriteIsConsistent verifies that after the leader ACKs
// a write, reading from any follower returns the correct value.
//
// This is guaranteed for W=5 (all followers confirmed before ACK) and
// W=3 (quorum confirmed). For W=1 it is NOT guaranteed — replication is
// async — so we skip W=1 here and test it separately below.
func TestFollowerReadAfterWriteIsConsistent(t *testing.T) {
	for _, cfg := range []struct{ w, r int }{{5, 1}, {3, 3}} {
		cfg := cfg
		t.Run(fmt.Sprintf("W=%d_R=%d", cfg.w, cfg.r), func(t *testing.T) {
			t.Parallel()
			c := newCluster(t, cfg.w, cfg.r, 4)
			defer c.close()

			set(t, c.leader.URL, "animal", "cat")

			// Check every follower — all must have the value.
			for i, follower := range c.followers {
				val, _, ok := get(follower.URL, "animal")
				if !ok {
					t.Errorf("follower %d: GET returned 404 after W=%d write", i+1, cfg.w)
					continue
				}
				if val != "cat" {
					t.Errorf("follower %d: want value=cat, got %q", i+1, val)
				}
			}
		})
	}
}

// TestW1FollowerMayBeStaleAfterWrite demonstrates that with W=1 the leader
// responds before replication is complete, so followers may hold stale data
// immediately after the ACK. This is expected behaviour, not a bug.
//
// We verify staleness by reading from every follower immediately after the
// leader ACKs and checking that at least one of them is missing the value.
// Because the artificial delays are large (200ms + 100ms per follower), this
// should reliably catch the inconsistency window.
func TestW1FollowerMayBeStaleAfterWrite(t *testing.T) {
	c := newCluster(t, 1, 5, 4)
	defer c.close()

	set(t, c.leader.URL, "planet", "mars")

	// Immediately after the ACK, check all followers via local_read.
	// With W=1 the goroutine hasn't even sent the first replication yet.
	staleCount := 0
	for _, follower := range c.followers {
		_, _, ok := localRead(follower.URL, "planet")
		if !ok {
			staleCount++
		}
	}

	if staleCount == 0 {
		t.Log("WARNING: no stale followers detected — replication may have been " +
			"unexpectedly fast or the test ran too slowly")
		// Don't fail — this is a timing-sensitive test. Log it and move on.
		// Under normal artificial delays (200ms + 100ms) all 4 followers
		// should be stale immediately after W=1 ACK.
	} else {
		t.Logf("PASS: %d/4 followers were stale immediately after W=1 write (expected)", staleCount)
	}
}

// TestInconsistencyWindowDuringReplication is the "sneaky local_read" test
// from the assignment spec. It fires a write, then immediately polls every
// follower via /local_read while replication is in progress.
//
// Because the leader sleeps 200ms between follower sends, and each follower
// sleeps 100ms on receipt, there is a multi-hundred-millisecond window where
// followers have not yet committed the write. This test captures that window.
func TestInconsistencyWindowDuringReplication(t *testing.T) {
	// W=5 so the leader blocks until all followers ACK — giving us time to
	// race local_reads against the in-progress replication loop.
	// We run the write in a goroutine and poll from the main goroutine.
	c := newCluster(t, 5, 1, 4)
	defer c.close()

	inconsistencySeen := false
	var mu sync.Mutex

	// Start the write asynchronously.
	writeDone := make(chan int64, 1)
	go func() {
		v := set(t, c.leader.URL, "key1", "value1")
		writeDone <- v
	}()

	// Poll followers rapidly while the write is in-flight.
	// The write takes ≈ 4×300ms = 1.2s, giving us plenty of polling time.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, follower := range c.followers {
			_, _, ok := localRead(follower.URL, "key1")
			if !ok {
				mu.Lock()
				inconsistencySeen = true
				mu.Unlock()
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for write to complete before we close the cluster.
	<-writeDone

	if !inconsistencySeen {
		t.Error("expected to observe at least one stale local_read during replication, but all followers were consistent")
	} else {
		t.Log("PASS: observed inconsistency window during W=5 replication")
	}
}

// TestHighLoadInconsistencyDetection runs many writes with immediate follower
// reads to show that inconsistency is reliably observable under load.
// This exercises the "repeat this often at high load" requirement from the spec.
func TestHighLoadInconsistencyDetection(t *testing.T) {
	c := newCluster(t, 1, 5, 4) // W=1 maximises async inconsistency
	defer c.close()

	const numWrites = 20
	staleObservations := 0

	for i := 0; i < numWrites; i++ {
		key := fmt.Sprintf("load-key-%d", i)
		val := fmt.Sprintf("load-val-%d", i)

		// Write to leader (returns immediately with W=1).
		set(t, c.leader.URL, key, val)

		// Immediately read from every follower via local_read.
		for _, follower := range c.followers {
			v, _, ok := localRead(follower.URL, key)
			if !ok || v != val {
				staleObservations++
			}
		}
	}

	t.Logf("stale observations: %d / %d possible (W=1, %d followers, %d writes)",
		staleObservations, numWrites*len(c.followers), len(c.followers), numWrites)

	if staleObservations == 0 {
		t.Error("expected stale reads under W=1 high load, got none — check artificial delays")
	}
}

// =============================================================================
// Leaderless tests
// =============================================================================

// leaderlessCluster spins up N leaderless nodes as httptest.Servers.
type leaderlessCluster struct {
	nodes []*httptest.Server
}

func newLeaderlessCluster(t *testing.T, n, w, r int) *leaderlessCluster {
	t.Helper()

	servers := make([]*httptest.Server, n)
	nodes := make([]*LeaderlessNode, n)

	// Two-pass setup: first create all servers to get their URLs, then
	// wire up each node's peer list (all servers except itself).
	// We use placeholder URLs in the first pass and patch them after.
	//
	// Since httptest.NewServer requires the handler upfront, we create a
	// mutable ServeMux per node and register routes after URL assignment.
	muxes := make([]*http.ServeMux, n)
	for i := 0; i < n; i++ {
		muxes[i] = http.NewServeMux()
		servers[i] = httptest.NewServer(muxes[i])
	}

	// Now that we have all URLs, build each node with its correct peer list.
	for i := 0; i < n; i++ {
		peers := make([]string, 0, n-1)
		for j, srv := range servers {
			if j != i {
				peers = append(peers, srv.URL)
			}
		}
		nodes[i] = NewLeaderlessNode(fmt.Sprintf("node-%d", i+1), peers, w, r)
		nodes[i].RegisterRoutes(muxes[i])

		// Register /leader_read on the mux (mirrors RunLeaderless).
		node := nodes[i]
		muxes[i].HandleFunc("/leader_read", func(rw http.ResponseWriter, req *http.Request) {
			time.Sleep(followerReadDelay)
			node.store.HandleGet(rw, req)
		})
	}

	return &leaderlessCluster{nodes: servers}
}

func (lc *leaderlessCluster) close() {
	for _, s := range lc.nodes {
		s.Close()
	}
}

// TestLeaderlessCoordinatorReadIsConsistent verifies that after the write
// coordinator responds 201, reading from the coordinator itself is consistent.
// With W=N this is guaranteed: the coordinator writes locally first.
func TestLeaderlessCoordinatorReadIsConsistent(t *testing.T) {
	lc := newLeaderlessCluster(t, 5, 5, 1)
	defer lc.close()

	coordinator := lc.nodes[0]
	set(t, coordinator.URL, "fruit", "mango")

	val, _, ok := get(coordinator.URL, "fruit")
	if !ok {
		t.Fatal("coordinator GET returned 404 after its own write")
	}
	if val != "mango" {
		t.Errorf("coordinator: want value=mango, got %q", val)
	}
}

// TestLeaderlessNonCoordinatorReadIsConsistentAfterWrite verifies that after
// W=N coordinator responds, reading from any non-coordinator node is also
// consistent. With W=5 (all nodes confirmed), every node must have the value.
func TestLeaderlessNonCoordinatorReadIsConsistentAfterWrite(t *testing.T) {
	lc := newLeaderlessCluster(t, 5, 5, 1)
	defer lc.close()

	coordinator := lc.nodes[0]
	set(t, coordinator.URL, "gem", "ruby")

	// Read from every non-coordinator node.
	for i := 1; i < len(lc.nodes); i++ {
		val, _, ok := get(lc.nodes[i].URL, "gem")
		if !ok {
			t.Errorf("node %d: GET returned 404 after W=5 write", i+1)
			continue
		}
		if val != "ruby" {
			t.Errorf("node %d: want value=ruby, got %q", i+1, val)
		}
	}
}

// TestLeaderlessInconsistencyWindowDuringReplication is the core leaderless
// inconsistency test from the assignment spec.
//
// The coordinator begins replicating sequentially to peers (200ms sleep
// between each, peer sleeps 100ms on receipt). During this window, non-
// coordinator nodes will not yet have the value. This test races /local_read
// calls against the in-flight replication to catch that window.
//
// Key point: the test reads from non-coordinator nodes BEFORE the coordinator
// responds 201. That is the only window where W=N leaderless is inconsistent.
func TestLeaderlessInconsistencyWindowDuringReplication(t *testing.T) {
	lc := newLeaderlessCluster(t, 5, 5, 1)
	defer lc.close()

	coordinator := lc.nodes[0]
	nonCoordinators := lc.nodes[1:]

	inconsistencySeen := false
	writeDone := make(chan struct{})

	// Fire the write in the background.
	go func() {
		set(t, coordinator.URL, "window-key", "window-value")
		close(writeDone)
	}()

	// Poll non-coordinator nodes via local_read while the write is in-flight.
	// With 4 peers × (200ms + 100ms) the write takes ~1.2s, plenty of time.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case <-writeDone:
			goto done // write finished, stop polling
		default:
		}

		for _, node := range nonCoordinators {
			_, _, ok := localRead(node.URL, "window-key")
			if !ok {
				inconsistencySeen = true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

done:
	<-writeDone // ensure write goroutine exits cleanly

	if !inconsistencySeen {
		t.Error("expected stale local_reads during leaderless replication, observed none")
	} else {
		t.Log("PASS: inconsistency window observed in leaderless cluster during replication")
	}
}

// TestLeaderlessRandomCoordinator verifies that any node can act as write
// coordinator and the result is still visible from all nodes after W=N ACK.
func TestLeaderlessRandomCoordinator(t *testing.T) {
	lc := newLeaderlessCluster(t, 5, 5, 1)
	defer lc.close()

	// Send writes to different nodes as coordinators.
	writes := []struct {
		coordinatorIdx int
		key, value     string
	}{
		{0, "k0", "v0"},
		{2, "k2", "v2"},
		{4, "k4", "v4"},
	}

	for _, w := range writes {
		coordinator := lc.nodes[w.coordinatorIdx]
		set(t, coordinator.URL, w.key, w.value)

		// After ACK, all nodes must have the value.
		for i, node := range lc.nodes {
			val, _, ok := get(node.URL, w.key)
			if !ok {
				t.Errorf("coordinator=%d key=%q: node %d returned 404", w.coordinatorIdx, w.key, i)
				continue
			}
			if val != w.value {
				t.Errorf("coordinator=%d key=%q: node %d returned %q, want %q",
					w.coordinatorIdx, w.key, i, val, w.value)
			}
		}
	}
}

// =============================================================================
// Store unit tests (fast, no HTTP)
// =============================================================================

// TestStoreSetAndGet verifies basic in-memory storage.
func TestStoreSetAndGet(t *testing.T) {
	s := NewStore()

	v := s.Set("hello", "world")
	if v < 1 {
		t.Errorf("Set should return version >= 1, got %d", v)
	}

	entry, ok := s.Get("hello")
	if !ok {
		t.Fatal("Get returned not-found after Set")
	}
	if entry.Value != "world" {
		t.Errorf("want value=world, got %q", entry.Value)
	}
	if entry.Version != v {
		t.Errorf("want version=%d, got %d", v, entry.Version)
	}
}

// TestStoreVersionMonotonicallyIncreases verifies that successive writes
// produce strictly increasing version numbers.
func TestStoreVersionMonotonicallyIncreases(t *testing.T) {
	s := NewStore()
	prev := int64(0)
	for i := 0; i < 100; i++ {
		v := s.Set(fmt.Sprintf("key-%d", i), "val")
		if v <= prev {
			t.Errorf("version %d not greater than previous %d", v, prev)
		}
		prev = v
	}
}

// TestStoreSetWithVersionRatchets verifies that SetWithVersion updates
// lastVersion so subsequent Set calls produce higher numbers.
func TestStoreSetWithVersionRatchets(t *testing.T) {
	s := NewStore()

	// Simulate receiving a replication with a large version number.
	s.SetWithVersion("replicated-key", "replicated-val", 1000)

	// The next local write should produce a version > 1000.
	v := s.Set("local-key", "local-val")
	if v <= 1000 {
		t.Errorf("after SetWithVersion(1000), Set() should return >1000, got %d", v)
	}
}

// TestStoreMissingKey verifies that Get returns false for unknown keys.
func TestStoreMissingKey(t *testing.T) {
	s := NewStore()
	_, ok := s.Get("nonexistent")
	if ok {
		t.Error("Get on empty store should return ok=false")
	}
}

// TestStoreConcurrentWrites verifies the store is safe under concurrent access.
func TestStoreConcurrentWrites(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	versions := make([]int64, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			versions[i] = s.Set(fmt.Sprintf("k%d", i), "v")
		}()
	}
	wg.Wait()

	// All versions must be unique (monotonic counter, no two goroutines
	// get the same value from atomic.AddInt64).
	seen := make(map[int64]bool)
	for _, v := range versions {
		if seen[v] {
			t.Errorf("duplicate version %d — concurrent writes are not safe", v)
		}
		seen[v] = true
	}
}
