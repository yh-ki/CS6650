package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// main reads environment variables and starts the appropriate node type.
//
// Required env vars (all nodes):
//
//	ROLE   — "leader" | "follower" | "leaderless" | "loadtest"
//
// PORT is required for leader/follower/leaderless; not needed for loadtest.
//
// Additional vars for leader:
//
//	PORT   — TCP port to listen on, e.g. "8080"
//	PEERS  — comma-separated follower base URLs
//	W      — write quorum: 1, 3, or 5
//	R      — read quorum:  1, 3, or 5
//
// Additional vars for follower:
//
//	PORT    — TCP port to listen on
//	NODE_ID — short identifier used in log lines, e.g. "1"
//
// Additional vars for leaderless:
//
//	PORT    — TCP port to listen on
//	PEERS   — comma-separated peer base URLs (every other node)
//	NODE_ID — short identifier used in log lines
//	W       — write quorum (typically N = total node count)
//	R       — read quorum (typically 1)
//
// Additional vars for loadtest:
//
//	LEADER_URL    — base URL of the leader (or any node for leaderless)
//	FOLLOWER_URLS — comma-separated base URLs of followers / other nodes
func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	role := requireEnv("ROLE")

	switch strings.ToLower(role) {

	case "leader":
		port := requireEnv("PORT")
		peers := parsePeers(requireEnv("PEERS"))
		w := parseInt(requireEnv("W"), "W")
		r := parseInt(requireEnv("R"), "R")
		if err := RunLeader(port, peers, w, r); err != nil {
			log.Fatalf("[main] leader exited: %v", err)
		}

	case "follower":
		port := requireEnv("PORT")
		nodeID := getEnvOrDefault("NODE_ID", "?")
		if err := RunFollower(port, nodeID); err != nil {
			log.Fatalf("[main] follower exited: %v", err)
		}

	default:
		log.Fatalf("[main] unknown ROLE %q — must be leader or follower", role)
	}
}

// requireEnv reads an environment variable and fatals if it is not set.
func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[main] required environment variable %s is not set", key)
	}
	return v
}

// getEnvOrDefault reads an environment variable, returning def if unset.
func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parsePeers splits a comma-separated list of URLs into a slice.
// Empty strings are silently dropped so trailing commas are harmless.
func parsePeers(raw string) []string {
	var peers []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			peers = append(peers, p)
		}
	}
	if len(peers) == 0 {
		log.Fatal("[main] PEERS must contain at least one URL")
	}
	return peers
}

// parseInt parses an integer env var value, fataling with a clear message on failure.
func parseInt(raw, name string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		log.Fatalf("[main] env var %s=%q is not a valid integer: %v", name, raw, err)
	}
	if v < 1 {
		log.Fatalf("[main] env var %s must be >= 1, got %d", name, v)
	}
	return v
}

// printConfig logs the effective configuration at startup.
func printConfig(role, port string, extras ...string) {
	parts := append([]string{fmt.Sprintf("ROLE=%s PORT=%s", role, port)}, extras...)
	log.Printf("[main] starting node: %s", strings.Join(parts, " "))
}
