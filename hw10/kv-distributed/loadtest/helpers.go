package main

import (
	"log"
	"strings"
	"os"
)

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
		log.Fatal("[loadtest] FOLLOWER_URLS must contain at least one URL")
	}
	return peers
}
