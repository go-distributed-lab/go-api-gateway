# go-api-gateway — Architecture

## Overview

`go-api-gateway` is a production-inspired API gateway written entirely in the
Go standard library.

It proxies HTTP traffic, enforces gateway policies such as authentication,
rate limiting, circuit breaking, and retries, and exposes a dynamic
configuration API.

The project has **zero external dependencies** and focuses on clean
architecture, concurrency, fault tolerance, and idiomatic Go design.

---

## Request Lifecycle

```text
Client Request
      │
      ▼
┌─────────────────────┐
│     HTTP Server     │
│      net/http       │
│       :8080         │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│       Router        │
│                     │
│ Path + Method Match │
└──────────┬──────────┘
           │
           ▼
┌───────────────────────────────────────┐
│           Middleware Chain             │
│                                       │
│ Recovery → Logger → Auth → RateLimit  │
│                    → Transform        │
└───────────────────┬───────────────────┘
                    │
                    ▼
┌─────────────────────┐
│   Circuit Breaker   │
│                     │
│    Allow / Reject   │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│        Retry        │
│                     │
│ Attempts + Backoff  │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────────┐
│     Reverse Proxy       │
│                         │
│ httputil.ReverseProxy   │
└──────────┬──────────────┘
           │
           ▼
    Upstream Service
```

### Request Processing

1. The HTTP server accepts the incoming request.
2. The router matches the request path and HTTP method to a configured route.
3. The middleware chain applies request policies.
4. The circuit breaker determines whether the upstream is currently healthy
   enough to receive the request.
5. The retry layer handles retryable upstream failures using backoff.
6. The reverse proxy forwards the request to the upstream service.
7. The upstream response is returned to the client.

---

## Package Layout

```text
go-api-gateway/
│
├── cmd/
│   ├── gateway/
│   │   └── main.go
│   │
│   └── upstream/
│       └── main.go
│
├── pkg/
│   └── gateway/
│       └── gateway.go
│
└── internal/
    ├── router/
    │   └── router.go
    │
    ├── middleware/
    │   └── middleware.go
    │
    ├── proxy/
    │   └── proxy.go
    │
    ├── circuitbreaker/
    │   └── breaker.go
    │
    ├── retry/
    │   └── retry.go
    │
    ├── metrics/
    │   └── metrics.go
    │
    └── logger/
        └── logger.go
```

---

## Package Responsibilities

| Package | Responsibility |
|---|---|
| `pkg/gateway` | Public types such as `Route`, `Config`, and `Gateway` |
| `internal/router` | Path and HTTP method matching |
| `internal/middleware` | Chainable middleware pipeline |
| `internal/proxy` | Reverse proxy wrapper around `httputil.ReverseProxy` |
| `internal/circuitbreaker` | Closed/Open/Half-Open circuit breaker state machine |
| `internal/retry` | Retry attempts and exponential backoff |
| `internal/metrics` | Per-route atomic counters |
| `internal/logger` | Structured `key=value` logging |
| `cmd/gateway` | Application entry point, component wiring, and graceful shutdown |
| `cmd/upstream` | Echo server used for local development and testing |

---

## Component Architecture

```text
                         ┌──────────────────┐
                         │      Client      │
                         └────────┬─────────┘
                                  │
                                  ▼
                         ┌──────────────────┐
                         │   HTTP Server    │
                         │    net/http      │
                         └────────┬─────────┘
                                  │
                                  ▼
                         ┌──────────────────┐
                         │      Router      │
                         └────────┬─────────┘
                                  │
                                  ▼
                    ┌──────────────────────────┐
                    │    Middleware Chain      │
                    │                          │
                    │ Recovery                  │
                    │ Logger                    │
                    │ Auth                      │
                    │ Rate Limit                │
                    │ Transform                 │
                    └────────────┬─────────────┘
                                 │
                                 ▼
                    ┌──────────────────────────┐
                    │    Circuit Breaker       │
                    └────────────┬─────────────┘
                                 │
                                 ▼
                    ┌──────────────────────────┐
                    │         Retry            │
                    └────────────┬─────────────┘
                                 │
                                 ▼
                    ┌──────────────────────────┐
                    │     Reverse Proxy        │
                    └────────────┬─────────────┘
                                 │
                                 ▼
                         ┌──────────────────┐
                         │ Upstream Service │
                         └──────────────────┘
```

---

## Middleware Architecture

The middleware layer follows the standard Go HTTP middleware pattern.

```go
type Middleware func(http.Handler) http.Handler
```

Middleware is composed into a chain:

```text
Request
   │
   ▼
Recovery
   │
   ▼
Logger
   │
   ▼
Authentication
   │
   ▼
Rate Limiting
   │
   ▼
Transformation
   │
   ▼
Handler
```

Each middleware is responsible for one concern and can be independently
tested or replaced.

---

## Circuit Breaker

The circuit breaker protects upstream services from repeated failures.

It uses three states:

```text
                 ┌─────────────┐
                 │   CLOSED    │
                 └──────┬──────┘
                        │
                   failure threshold
                        │
                        ▼
                 ┌─────────────┐
                 │    OPEN     │
                 └──────┬──────┘
                        │
                    timeout
                        │
                        ▼
                 ┌─────────────┐
                 │  HALF-OPEN  │
                 └──────┬──────┘
                        │
                  ┌─────┴─────┐
                  │           │
               success      failure
                  │           │
                  ▼           ▼
               CLOSED        OPEN
```

### Closed

Requests are allowed normally.

Failures are tracked. When the configured failure threshold is reached, the
circuit transitions to `Open`.

### Open

Requests are rejected immediately without contacting the upstream service.

After the configured recovery timeout, the circuit transitions to `Half-Open`.

### Half-Open

A limited request is allowed through to determine whether the upstream has
recovered.

- Success → `Closed`
- Failure → `Open`

---

## Retry Architecture

The retry component is responsible for retrying retryable upstream failures.

```text
Request
   │
   ▼
Attempt 1
   │
   ├── Success ──────────────► Response
   │
   └── Failure
          │
          ▼
       Backoff
          │
          ▼
       Attempt 2
          │
          ├── Success ───────► Response
          │
          └── Failure
                 │
                 ▼
              Backoff
                 │
                 ▼
              Attempt N
```

The retry implementation supports:

- Maximum retry attempts
- Exponential backoff
- Configurable initial delay
- Retryable failure detection

Example backoff:

```text
Attempt 1
   │
   └── failure
          │
        100ms
          │
          ▼
Attempt 2
   │
   └── failure
          │
        200ms
          │
          ▼
Attempt 3
   │
   └── failure
          │
        400ms
          │
          ▼
Attempt 4
```

Retry logic is kept separate from the reverse proxy so retry behavior can be
changed independently.

---

## Reverse Proxy

The gateway uses Go's standard library:

```go
net/http/httputil
```

The proxy component wraps `httputil.ReverseProxy` and is responsible for
forwarding requests to configured upstream services.

```text
Gateway
   │
   ▼
Reverse Proxy
   │
   ├── Request
   │
   ▼
Upstream Service
   │
   └── Response
           │
           ▼
         Client
```

Keeping the proxy implementation isolated allows the gateway to apply
policies before forwarding traffic.

---

## Metrics

Metrics are maintained per route.

Hot-path counters use `sync/atomic` rather than mutex-protected counters.

Example counters:

```text
requests_total
requests_success
requests_failed
requests_rejected
```

Example:

```go
atomic.AddUint64(&counter, 1)
```

instead of:

```go
mutex.Lock()
counter++
mutex.Unlock()
```

The goal is to minimize lock contention on frequently accessed request
counters.

---

## Logger

The gateway provides a lightweight structured logger using `key=value`
formatting.

Example:

```text
method=GET path=/api/users status=200 duration=12ms route=users
```

The logger supports an enabled/disabled guard.

When logging is disabled and output is configured as `io.Discard`, the hot path
avoids unnecessary formatting and allocation work.

---

## Dynamic Configuration

The gateway exposes a configuration API for dynamically managing gateway
configuration.

Configuration can include:

- Routes
- Upstream targets
- Middleware policies
- Rate limits
- Retry configuration
- Circuit breaker configuration

The configuration layer is separated from request processing so configuration
management does not become tightly coupled to the request hot path.

---

## Graceful Shutdown

The gateway uses channel-based coordination and context cancellation for
graceful shutdown.

```text
SIGINT / SIGTERM
       │
       ▼
Stop accepting new requests
       │
       ▼
Allow in-flight requests to finish
       │
       ▼
Shutdown components
       │
       ▼
Process exits
```

The gateway should never intentionally drop in-flight requests during a
normal shutdown.

---

## Concurrency Model

The HTTP server processes requests concurrently using Go's HTTP server
concurrency model.

```text
                    Gateway
                       │
          ┌────────────┼────────────┐
          │            │            │
          ▼            ▼            ▼
      Request 1    Request 2    Request 3
      Goroutine    Goroutine    Goroutine
          │            │            │
          ▼            ▼            ▼
       Router        Router       Router
          │            │            │
          ▼            ▼            ▼
      Policies      Policies     Policies
```

Shared state is synchronized using the appropriate primitive:

- `sync.Mutex` for state requiring mutual exclusion
- `sync/atomic` for hot-path counters
- Channels for lifecycle coordination
- `context.Context` for cancellation and deadlines

---

## Interface-Driven Design

The architecture uses interfaces to decouple major components.

Conceptually:

```go
type Gateway interface {
    ServeHTTP(http.ResponseWriter, *http.Request)
}

type Router interface {
    Match(path, method string) (*Route, bool)
}

type Breaker interface {
    Allow() bool
    Success()
    Failure()
}

type Retryer interface {
    Do(func() error) error
}
```

This allows implementations to be replaced without modifying unrelated
components.

For example:

```text
Gateway
  │
  ├── Router
  ├── Breaker
  ├── Retryer
  └── Proxy
```

The design follows the principle:

> Depend on behavior, not concrete implementations.

---

## Design Decisions

### 1. Pure Standard Library

The project intentionally uses zero external dependencies.

Core packages include:

```text
net/http
net/http/httputil
sync
sync/atomic
context
time
log
```

Benefits:

- No framework lock-in
- Small dependency surface
- Easy deployment
- Direct understanding of Go HTTP internals
- Simple builds

---

### 2. Interface-Driven Components

Major components are abstracted through interfaces.

This allows implementations to be swapped independently.

For example, the retry implementation can change without requiring changes to
the router or reverse proxy.

---

### 3. Atomic Metrics

Hot-path metrics use `sync/atomic`.

This avoids unnecessary mutex contention when counters are updated for every
request.

---

### 4. Channel-Based Shutdown

Shutdown coordination uses channels and context cancellation.

This provides a clear lifecycle:

```text
Signal
  │
  ▼
Stop accepting
  │
  ▼
Drain
  │
  ▼
Shutdown
```

In-flight requests are allowed to complete during graceful shutdown.

---

### 5. Logger Enabled Guard

The logger uses an enabled flag to avoid unnecessary work when logging is
disabled.

When output is `io.Discard`, logging can effectively be disabled without
requiring changes to request-processing code.

---

## Design Goals

The project is intended to demonstrate production-inspired Go backend and
distributed-system concepts.

Primary goals:

- Idiomatic Go HTTP programming
- Reverse proxy implementation
- Composable middleware
- Fault tolerance
- Circuit breaker design
- Retry strategies
- Concurrent programming
- Atomic operations
- Interface-driven architecture
- Graceful shutdown
- Dynamic configuration
- Zero external dependencies

---

## Summary

The gateway follows a layered architecture:

```text
Client
  │
  ▼
HTTP Server
  │
  ▼
Router
  │
  ▼
Middleware
  │
  ▼
Circuit Breaker
  │
  ▼
Retry
  │
  ▼
Reverse Proxy
  │
  ▼
Upstream
```

Cross-cutting concerns such as metrics, logging, configuration, and graceful
shutdown remain separated from the core proxying path.

The result is a modular API gateway that demonstrates how production-inspired
gateway functionality can be built using only Go's standard library.