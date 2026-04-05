package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// Configuration
// =============================================================================

// LoadTestConfig defines a single load-test run.
type LoadTestConfig struct {
	// LeaderURL is where all writes go (and reads, if ReadURLs is empty).
	LeaderURL string

	// ReadURLs is the pool of nodes that reads are distributed across.
	// For leader-follower: include leader + all follower URLs.
	// For leaderless: all node URLs.
	// If empty, all reads go to LeaderURL.
	ReadURLs []string

	// WriteRatio is the fraction of operations that are writes, e.g. 0.10 for 10%.
	WriteRatio float64

	// NumKeys is the size of the key space. Keeping this small (e.g. 20)
	// ensures reads and writes to the same key cluster closely in time,
	// which is what triggers stale-read detection.
	NumKeys int

	// Concurrency is the number of concurrent worker goroutines.
	Concurrency int

	// Duration is how long the load test runs.
	Duration time.Duration

	// Label is used in output filenames and log lines.
	Label string
}

// DefaultConfigs returns the four read/write ratio configurations the
// assignment requires, all pointing at a local leader-follower cluster.
func DefaultConfigs(leaderURL string, followerURLs []string) []LoadTestConfig {
	readURLs := append([]string{leaderURL}, followerURLs...)
	return []LoadTestConfig{
		{
			LeaderURL: leaderURL, ReadURLs: readURLs,
			WriteRatio: 0.01, NumKeys: 20, Concurrency: 20,
			Duration: 30 * time.Second, Label: "w01-r99",
		},
		{
			LeaderURL: leaderURL, ReadURLs: readURLs,
			WriteRatio: 0.10, NumKeys: 20, Concurrency: 20,
			Duration: 30 * time.Second, Label: "w10-r90",
		},
		{
			LeaderURL: leaderURL, ReadURLs: readURLs,
			WriteRatio: 0.50, NumKeys: 20, Concurrency: 20,
			Duration: 30 * time.Second, Label: "w50-r50",
		},
		{
			LeaderURL: leaderURL, ReadURLs: readURLs,
			WriteRatio: 0.90, NumKeys: 20, Concurrency: 20,
			Duration: 30 * time.Second, Label: "w90-r10",
		},
	}
}

// =============================================================================
// Key-local-in-time generator
// =============================================================================

// keyTracker tracks the most recently written version for each key so the
// load tester can detect stale reads. It is safe for concurrent use.
type keyTracker struct {
	mu       sync.RWMutex
	versions map[string]int64 // key → last written version
	values   map[string]string // key → last written value
}

func newKeyTracker(numKeys int) *keyTracker {
	return &keyTracker{
		versions: make(map[string]int64, numKeys),
		values:   make(map[string]string, numKeys),
	}
}

// recordWrite stores the version and value for a key after a successful write.
func (kt *keyTracker) recordWrite(key, value string, version int64) {
	kt.mu.Lock()
	kt.versions[key] = version
	kt.values[key] = value
	kt.mu.Unlock()
}

// isStale returns true if the returned value/version is older than what we
// last wrote for this key. Returns false if we have never written this key.
func (kt *keyTracker) isStale(key, value string, version int64) bool {
	kt.mu.RLock()
	lastVersion, ok := kt.versions[key]
	kt.mu.RUnlock()
	if !ok {
		return false // never written, can't be stale
	}
	return version < lastVersion
}

// pickKey returns a key name from the fixed key space.
// Using a small key space (NumKeys=20) means reads and writes naturally
// cluster around the same keys, producing frequent read-after-write scenarios.
func pickKey(numKeys int) string {
	return fmt.Sprintf("key-%03d", rand.Intn(numKeys))
}

// pickReadURL returns a random node URL from the read pool.
func pickReadURL(cfg LoadTestConfig) string {
	if len(cfg.ReadURLs) == 0 {
		return cfg.LeaderURL
	}
	return cfg.ReadURLs[rand.Intn(len(cfg.ReadURLs))]
}

// =============================================================================
// Metrics
// =============================================================================

// OpResult records the outcome of a single read or write operation.
type OpResult struct {
	Kind      string        // "read" or "write"
	Key       string
	Latency   time.Duration
	IsStale   bool  // true if read returned an older version than last write
	Version   int64 // version returned by the node
	Success   bool
	Timestamp time.Time // when the operation started — used for interval graphs
}

// Metrics aggregates results across all workers.
type Metrics struct {
	mu      sync.Mutex
	results []OpResult

	// Interval tracking: for each key, record the time of last write and
	// the time of the subsequent read (to build the read-write interval graph).
	lastWriteTime map[string]time.Time
	intervals     []time.Duration // read-write intervals for the same key
}

func newMetrics() *Metrics {
	return &Metrics{
		lastWriteTime: make(map[string]time.Time),
	}
}

func (m *Metrics) record(r OpResult) {
	m.mu.Lock()
	m.results = append(m.results, r)
	if r.Kind == "write" && r.Success {
		m.lastWriteTime[r.Key] = r.Timestamp
	}
	if r.Kind == "read" && r.Success {
		if wt, ok := m.lastWriteTime[r.Key]; ok {
			m.intervals = append(m.intervals, r.Timestamp.Sub(wt))
			delete(m.lastWriteTime, r.Key) // consume — record next read after next write
		}
	}
	m.mu.Unlock()
}

// Summary prints a human-readable summary of the run to stdout.
func (m *Metrics) Summary(label string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reads := filter(m.results, func(r OpResult) bool { return r.Kind == "read" })
	writes := filter(m.results, func(r OpResult) bool { return r.Kind == "write" })

	fmt.Printf("\n======== %s ========\n", label)
	fmt.Printf("Total ops:    %d (%d reads, %d writes)\n",
		len(m.results), len(reads), len(writes))

	printLatencyStats("Read latency ", reads)
	printLatencyStats("Write latency", writes)

	stale := 0
	for _, r := range reads {
		if r.IsStale {
			stale++
		}
	}
	stalePct := 0.0
	if len(reads) > 0 {
		stalePct = float64(stale) / float64(len(reads)) * 100
	}
	fmt.Printf("Stale reads:  %d / %d (%.2f%%)\n", stale, len(reads), stalePct)

	if len(m.intervals) > 0 {
		sort.Slice(m.intervals, func(i, j int) bool { return m.intervals[i] < m.intervals[j] })
		fmt.Printf("Read-write interval (same key): p50=%v p95=%v p99=%v max=%v n=%d\n",
			percentile(m.intervals, 0.50),
			percentile(m.intervals, 0.95),
			percentile(m.intervals, 0.99),
			m.intervals[len(m.intervals)-1],
			len(m.intervals),
		)
	}
}

func printLatencyStats(label string, ops []OpResult) {
	if len(ops) == 0 {
		fmt.Printf("%s: no data\n", label)
		return
	}
	latencies := make([]time.Duration, 0, len(ops))
	for _, r := range ops {
		if r.Success {
			latencies = append(latencies, r.Latency)
		}
	}
	if len(latencies) == 0 {
		fmt.Printf("%s: all failed\n", label)
		return
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	fmt.Printf("%s: p50=%v p95=%v p99=%v max=%v n=%d\n",
		label,
		percentile(latencies, 0.50),
		percentile(latencies, 0.95),
		percentile(latencies, 0.99),
		latencies[len(latencies)-1],
		len(latencies),
	)
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func filter(rs []OpResult, f func(OpResult) bool) []OpResult {
	out := make([]OpResult, 0)
	for _, r := range rs {
		if f(r) {
			out = append(out, r)
		}
	}
	return out
}

// WriteCSV writes all results to a CSV file for graphing.
// Each row: kind, key, latency_ms, is_stale, version, success, timestamp_unix_ms
func (m *Metrics) WriteCSV(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"kind", "key", "latency_ms", "is_stale", "version", "success", "timestamp_ms"})
	for _, r := range m.results {
		w.Write([]string{
			r.Kind,
			r.Key,
			strconv.FormatFloat(float64(r.Latency.Microseconds())/1000.0, 'f', 3, 64),
			strconv.FormatBool(r.IsStale),
			strconv.FormatInt(r.Version, 10),
			strconv.FormatBool(r.Success),
			strconv.FormatInt(r.Timestamp.UnixMilli(), 10),
		})
	}
	w.Flush()
	return w.Error()
}

// WriteIntervalsCSV writes the read-write interval data for graphing.
func (m *Metrics) WriteIntervalsCSV(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{"interval_ms"})
	for _, d := range m.intervals {
		w.Write([]string{
			strconv.FormatFloat(float64(d.Microseconds())/1000.0, 'f', 3, 64),
		})
	}
	w.Flush()
	return w.Error()
}

// =============================================================================
// HTTP client operations
// =============================================================================

var sharedClient = &http.Client{Timeout: 10 * time.Second}

// doWrite sends POST /set to the leader and returns the assigned version.
func doWrite(leaderURL, key, value string) (int64, time.Duration, error) {
	body, _ := json.Marshal(map[string]string{"key": key, "value": value})
	start := time.Now()
	resp, err := sharedClient.Post(leaderURL+"/set", "application/json", bytes.NewReader(body))
	latency := time.Since(start)
	if err != nil {
		return 0, latency, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return 0, latency, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var result struct {
		Version int64 `json:"version"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Version, latency, nil
}

// doRead sends GET /get to a node and returns the entry.
func doRead(nodeURL, key string) (string, int64, time.Duration, error) {
	start := time.Now()
	resp, err := sharedClient.Get(fmt.Sprintf("%s/get?key=%s", nodeURL, key))
	latency := time.Since(start)
	if err != nil {
		return "", 0, latency, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", 0, latency, nil // key not yet written — not an error
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, latency, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var entry struct {
		Value   string `json:"value"`
		Version int64  `json:"version"`
	}
	json.NewDecoder(resp.Body).Decode(&entry)
	return entry.Value, entry.Version, latency, nil
}

// =============================================================================
// Worker
// =============================================================================

// worker runs operations continuously until stop is closed.
// It decides write vs read based on WriteRatio and a random roll.
func worker(
	cfg LoadTestConfig,
	kt *keyTracker,
	metrics *Metrics,
	stop <-chan struct{},
	opCount *int64,
) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		key := pickKey(cfg.NumKeys)
		isWrite := rand.Float64() < cfg.WriteRatio

		if isWrite {
			value := fmt.Sprintf("v-%d", rand.Int63())
			start := time.Now()
			version, latency, err := doWrite(cfg.LeaderURL, key, value)
			result := OpResult{
				Kind:      "write",
				Key:       key,
				Latency:   latency,
				Version:   version,
				Success:   err == nil,
				Timestamp: start,
			}
			if err == nil {
				kt.recordWrite(key, value, version)
			}
			metrics.record(result)
			atomic.AddInt64(opCount, 1)

		} else {
			nodeURL := pickReadURL(cfg)
			start := time.Now()
			value, version, latency, err := doRead(nodeURL, key)
			result := OpResult{
				Kind:      "read",
				Key:       key,
				Latency:   latency,
				Version:   version,
				IsStale:   err == nil && version > 0 && kt.isStale(key, value, version),
				Success:   err == nil,
				Timestamp: start,
			}
			metrics.record(result)
			atomic.AddInt64(opCount, 1)
		}
	}
}

// =============================================================================
// Run a single load-test scenario
// =============================================================================

// RunLoadTest executes one load-test configuration and returns the populated
// Metrics. It also writes CSV files to the current directory for graphing.
func RunLoadTest(cfg LoadTestConfig) *Metrics {
	log.Printf("[loadtest] starting %s: writeRatio=%.0f%% keys=%d concurrency=%d duration=%v",
		cfg.Label, cfg.WriteRatio*100, cfg.NumKeys, cfg.Concurrency, cfg.Duration)

	kt := newKeyTracker(cfg.NumKeys)
	metrics := newMetrics()
	stop := make(chan struct{})
	var opCount int64
	var wg sync.WaitGroup

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(cfg, kt, metrics, stop, &opCount)
		}()
	}

	// Progress ticker.
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Printf("[loadtest] %s: %d ops completed", cfg.Label, atomic.LoadInt64(&opCount))
			case <-stop:
				return
			}
		}
	}()

	time.Sleep(cfg.Duration)
	close(stop)
	ticker.Stop()
	wg.Wait()

	log.Printf("[loadtest] %s done: %d total ops", cfg.Label, atomic.LoadInt64(&opCount))
	metrics.Summary(cfg.Label)

	// Write CSVs for graphing.
	csvPath := fmt.Sprintf("results-%s.csv", cfg.Label)
	if err := metrics.WriteCSV(csvPath); err != nil {
		log.Printf("[loadtest] failed to write %s: %v", csvPath, err)
	} else {
		log.Printf("[loadtest] wrote %s", csvPath)
	}

	intervalPath := fmt.Sprintf("intervals-%s.csv", cfg.Label)
	if err := metrics.WriteIntervalsCSV(intervalPath); err != nil {
		log.Printf("[loadtest] failed to write %s: %v", intervalPath, err)
	} else {
		log.Printf("[loadtest] wrote %s", intervalPath)
	}

	return metrics
}

// =============================================================================
// main — entry point for the load tester binary
// =============================================================================

// RunLoadTester is called from main() when ROLE=loadtest (or as a standalone binary).
// It reads target URLs from env vars and runs all four ratio scenarios in sequence.
func RunLoadTester() {
	leaderURL := getEnvOrDefault("LEADER_URL", "http://localhost:8080")
	followersRaw := getEnvOrDefault("FOLLOWER_URLS",
		"http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084")

	followerURLs := parsePeers(followersRaw)

	log.Printf("[loadtest] leader=%s followers=%v", leaderURL, followerURLs)

	configs := DefaultConfigs(leaderURL, followerURLs)
	for _, cfg := range configs {
		RunLoadTest(cfg)
		// Brief pause between scenarios so in-flight replication from the
		// previous run settles before the next one starts.
		time.Sleep(3 * time.Second)
	}

	log.Println("[loadtest] all scenarios complete — CSV files written to current directory")
}
