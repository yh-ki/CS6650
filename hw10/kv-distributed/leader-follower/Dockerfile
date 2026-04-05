# ---------- build stage ----------
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go module files first so Docker can cache the dependency layer
# independently of source changes.
COPY go.mod ./
RUN go mod download

# Copy all source files. All node types (leader, follower, leaderless) live
# in the same package and compile into a single binary. The ROLE env var
# decides which mode to run at startup.
COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o kvnode .

# ---------- runtime stage ----------
# Use a minimal image — no shell needed in production. Switch to
# debian:bookworm-slim if you need curl/wget for healthchecks during dev.
FROM alpine:3.19

WORKDIR /app
COPY --from=builder /app/kvnode .

# Document the env vars this image expects. These are not defaults —
# they must be supplied by docker-compose (or Kubernetes, etc.).
ENV ROLE=""
ENV PORT="8080"
ENV PEERS=""
ENV W=""
ENV R=""
ENV NODE_ID=""

EXPOSE 8080

ENTRYPOINT ["./kvnode"]
