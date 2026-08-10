package gateway

import (	
	"errors"
	"net/http"
)

// Sentinel errors returned by Gateway operations.
var (
	ErrRouteNotFound   = errors.New("gateway: route not found")
	ErrRouteDuplicate  = errors.New("gateway: route already exists")
	ErrInvalidRoute    = errors.New("gateway: invalid route")
	ErrInvalidUpstream = errors.New("gateway: invalid upstream URL")
)

// Config holds gateway-wide settings.
type Config struct {
	// Addr is the listening address for the gateway HTTP server.
	// Default: ":8080"
	Addr string

	// ReadTimeout is the maximum duration for reading the request.
	ReadTimeout int // seconds

	// WriteTimeout is the maximum duration before timing out writes.
	WriteTimeout int // seconds

	// IdleTimeout is the maximum idle time for keep-alive connections.
	IdleTimeout int // seconds

	// GlobalMiddleware is the ordered list of middleware applied to every route.
	// Applied before per-route middleware.
	GlobalMiddleware []MiddlewareFunc

	// LogOutput controls where structured logs are written.
	// Use io.Discard to silence all output.
	LogOutput interface{} // io.Writer — kept as interface{} to avoid import cycle
}

// Gateway is the top-level abstraction for the API gateway.
// It implements http.Handler and manages the full routing + middleware lifecycle.
type Gateway interface {
	http.Handler

	// AddRoute registers a new route. Returns ErrRouteDuplicate if a route
	// with the same ID already exists, ErrInvalidRoute if the route is malformed.
	AddRoute(route Route) error

	// RemoveRoute removes a route by ID. Returns ErrRouteNotFound if not found.
	RemoveRoute(id string) error

	// Routes returns a snapshot of all currently registered routes.
	Routes() []Route
}
