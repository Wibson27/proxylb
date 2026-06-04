# proxylb

Reverse proxy and load balancer. Routes incoming HTTP traffic to a pool of upstream backends with three selectable algorithms, active health checking, and automatic failover.

Built with `net/http/httputil.ReverseProxy` (Rewrite API), `sync/atomic` for lock-free connection tracking, and `gopkg.in/yaml.v3` for configuration — no web framework, no external dependencies beyond YAML parsing.

## Features

- **Three load balancing algorithms** — round-robin, weighted round-robin (Nginx smooth), least connections
- **Active health checking** — per-backend goroutines with configurable interval, timeout, and failure threshold
- **Circuit breaker** — consecutive failures mark backends unhealthy, automatic recovery on success
- **Atomic connection tracking** — lock-free `sync/atomic` counters for in-flight requests
- **YAML backend config** — define backends with name, URL, weight, and custom health URL
- **Admin API** — `/_lb/health` and `/_lb/backends` for introspection
- **Request-ID middleware** — generates or propagates `X-Request-ID` across proxy and backend
- **Request logging** — method, path, status, duration, request ID per request
- **Streaming support** — `http.Flusher` passthrough for SSE and chunked responses
- **503 on all-down** — returns Service Unavailable when no healthy backends exist
- **Structured logging** with `log/slog` (JSON to stdout)
- **Graceful shutdown** on SIGTERM/SIGINT with connection draining
- **`FROM scratch` Docker image** — minimal attack surface

## Quick start

```bash
# Start 3 demo backends
go run ./cmd/demoserver -name app-1 -port 3001 &
go run ./cmd/demoserver -name app-2 -port 3002 &
go run ./cmd/demoserver -name app-3 -port 3003 &

# Start the proxy (HTTP on :8080)
go run .
```

## Configuration

### Server settings (environment variables)

| Variable | Default | Description |
|---|---|---|
| `PROXYLB_PORT` | `8080` | Proxy listener port |
| `PROXYLB_ALGORITHM` | `round-robin` | Algorithm: `round-robin`, `weighted`, `least-connections` |
| `PROXYLB_HEALTH_INTERVAL` | `10s` | Health check interval per backend |
| `PROXYLB_HEALTH_TIMEOUT` | `3s` | Per-check HTTP timeout |
| `PROXYLB_UNHEALTHY_THRESHOLD` | `3` | Consecutive failures before marking down |
| `PROXYLB_SHUTDOWN_TIMEOUT` | `15s` | Grace period for shutdown |

### Backends (YAML config file)

```yaml
backends:
  - name: app-1
    url: http://localhost:3001
    weight: 3                              # default: 1
    health_url: http://localhost:3001/ping  # default: url + "/health"

  - name: app-2
    url: http://localhost:3002
    weight: 1

  - name: app-3
    url: http://localhost:3003
    weight: 1
```

## HTTP API

### Proxy (all paths except `/_lb/`)

```bash
# Requests are forwarded to a healthy backend
curl http://localhost:8080/any/path
# {"message":"hello from app-1","server":"app-1","path":"/any/path","request_id":"a1b2c3d4e5f6a7b8"}
```

### Health

```bash
curl http://localhost:8080/_lb/health
# {"status":"ok"}
```

### Backend status

```bash
curl http://localhost:8080/_lb/backends
# [
#   {"name":"app-1","url":"http://localhost:3001","weight":3,"healthy":true,"active_connections":0,"consecutive_failures":0},
#   {"name":"app-2","url":"http://localhost:3002","weight":1,"healthy":false,"active_connections":0,"consecutive_failures":5},
#   {"name":"app-3","url":"http://localhost:3003","weight":1,"healthy":true,"active_connections":0,"consecutive_failures":0}
# ]
```

## Load balancing algorithms

### Round-robin

Cycles through backends in order. Each request advances an atomic counter. Even distribution regardless of backend performance.

```
Request 1 → app-1
Request 2 → app-2
Request 3 → app-3
Request 4 → app-1
...
```

### Weighted round-robin

Nginx's smooth weighted round-robin algorithm. Distributes requests proportionally to weights while interleaving — no bursts. With weights 3:1:1 over 5 requests:

```
A A B A C  (smooth — not A A A B C)
```

### Least connections

Picks the backend with the fewest in-flight requests. Adapts to uneven backend performance — slow backends accumulate connections and receive fewer new ones.

## Health checking

Each backend gets its own goroutine that periodically GETs its health URL. After `PROXYLB_UNHEALTHY_THRESHOLD` consecutive failures, the backend is marked unhealthy and excluded from the load balancing pool. A single successful check resets the failure counter and restores the backend to rotation.

```
healthy backend:   check ✓ → reset failures → stays in pool
failing backend:   check ✗ → increment failures → at threshold → mark unhealthy → excluded
recovered backend: check ✓ → reset failures → mark healthy → back in pool
all down:          proxy returns 503 Service Unavailable
```

## Docker

```bash
# Build the image
docker build -t proxylb .
```

## Tests

```bash
go test -v -race ./...
```

40 tests covering YAML config parsing with defaults/validation/errors, env var helpers, backend atomic operations (connections, health, failures) with concurrent safety, all three balancing algorithms (cycling, distribution, smoothness, tie-breaking, concurrent picks), pool filtering (healthy/unhealthy/all-down, copy isolation), health checker (stays healthy, threshold marking, recovery, below-threshold, multi-backend, graceful shutdown), middleware (request ID generation/preservation/uniqueness, status capture, flush passthrough), and proxy (forwarding, round-robin distribution, no-backends 503, connection tracking, bad gateway 502).

## Project structure

```
proxylb/
├── main.go                          HTTP server + wiring + admin API
├── config.yaml                      Backend definitions
├── cmd/
│   └── demoserver/
│       └── main.go                  Test backend for manual verification
├── internal/
│   ├── config/
│   │   ├── config.go                YAML + env vars
│   │   └── config_test.go
│   ├── backend/
│   │   ├── backend.go               Backend model (atomic counters)
│   │   └── backend_test.go
│   ├── balancer/
│   │   ├── balancer.go              Balancer interface
│   │   ├── roundrobin.go            Atomic counter round-robin
│   │   ├── weighted.go              Nginx smooth weighted round-robin
│   │   ├── leastconn.go             Least active connections
│   │   └── balancer_test.go
│   ├── pool/
│   │   ├── pool.go                  RWMutex-protected backend list
│   │   └── pool_test.go
│   ├── health/
│   │   ├── checker.go               Per-backend health check goroutines
│   │   └── checker_test.go
│   ├── proxy/
│   │   ├── proxy.go                 ReverseProxy with Rewrite API
│   │   └── proxy_test.go
│   └── middleware/
│       ├── middleware.go            RequestID + Logging
│       └── middleware_test.go
├── Dockerfile                       Multi-stage FROM scratch build
└── .github/
    └── workflows/
        └── ci.yaml                  Go vet + test + build
```
