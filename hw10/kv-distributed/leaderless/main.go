package main

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// main for the leaderless package.
// Supported roles: leaderless, follower (peer replication receiver).
// For load testing, use the loadtest/ binary instead.
func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	role := requireEnv("ROLE")

	switch strings.ToLower(role) {

	case "leaderless":
		port := requireEnv("PORT")
		peers := parsePeers(requireEnv("PEERS"))
		nodeID := getEnvOrDefault("NODE_ID", "?")
		w := parseInt(requireEnv("W"), "W")
		r := parseInt(requireEnv("R"), "R")
		if err := RunLeaderless(port, nodeID, peers, w, r); err != nil {
			log.Fatalf("[main] leaderless exited: %v", err)
		}

	default:
		log.Fatalf("[main] unknown ROLE %q — leaderless package supports: leaderless", role)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("[main] required env var %s is not set", key)
	}
	return v
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

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
