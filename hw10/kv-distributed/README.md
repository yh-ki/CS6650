# kv-distributed

Distributed in-memory Key-Value store implementing Leader-Follower and Leaderless replication.

## Structure

```
kv-distributed/
├── docker-compose.yml       # all four cluster profiles
├── leader-follower/         # leader + follower nodes, unit tests
│   ├── store.go             # thread-safe KV store with versioning
│   ├── leader.go            # W=1 / W=3 / W=5 and R=1 / R=3 / R=5
│   ├── follower.go          # replica node with artificial delays
│   ├── main.go              # env-var wiring (ROLE, PORT, W, R, PEERS)
│   ├── kv_test.go           # consistency + inconsistency window tests
│   ├── Dockerfile
│   └── go.mod
├── leaderless/              # symmetric leaderless cluster (W=N R=1)
│   ├── store.go
│   ├── leaderless.go        # write coordinator + replicate endpoint
│   ├── follower.go          # reused for delay constants
│   ├── main.go
│   ├── kv_test.go
│   ├── Dockerfile
│   └── go.mod
└── loadtest/                # load tester + graphing
    ├── loadtest.go          # workers, metrics, CSV output
    ├── helpers.go           # shared env/peer helpers
    ├── main.go
    ├── graph.py             # CDF, histogram, interval plots
    └── go.mod
```

## Quick start

```bash
# Leader-Follower W=5 R=1
docker compose --profile lf-w5-r1 up --build

# Leader-Follower W=1 R=5
docker compose --profile lf-w1-r5 up --build

# Leader-Follower W=3 R=3 (quorum)
docker compose --profile lf-w3-r3 up --build

# Leaderless W=5 R=1
docker compose --profile leaderless up --build

# Smoke test (cluster must be running)
curl -X POST http://localhost:8080/set \
  -H 'Content-Type: application/json' \
  -d '{"key":"hello","value":"world"}'
curl http://localhost:8080/get?key=hello
curl http://localhost:8081/local_read?key=hello
```

## Run tests (no Docker needed)

```bash
cd leader-follower
go test -race ./... -v -timeout 60s
```

## Run load tests

```bash
# Start a cluster first, then:
cd loadtest
LEADER_URL=http://localhost:8080 \
FOLLOWER_URLS=http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084 \
go run .

# Graph results
pip install matplotlib pandas numpy
python3 graph.py --dir . --out ./graphs
```

## Artificial delays

| Event | Delay | Purpose |
|---|---|---|
| Leader between follower sends | 200ms | Widens inconsistency window |
| Follower before writing on replicate | 100ms | Simulates storage commit latency |
| Follower before responding to leader read | 50ms | Simulates inter-node read latency |

## API

| Endpoint | Method | Description |
|---|---|---|
| `/set` | POST | `{"key":"...","value":"..."}` → 201 `{"version":N}` |
| `/get` | GET | `?key=foo` → 200 `{"value":"...","version":N}` or 404 |
| `/local_read` | GET | Test-only: raw local value, no replication |
| `/replicate` | POST | Internal: coordinator → peer replication |
| `/leader_read` | GET | Internal: leader fan-out read (50ms delay) |
