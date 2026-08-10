package gateway

import "net/http"

// MiddlewareFunc wraps an http.Handler with additional behaviour.
// Middleware is applied in order: first registered = outermost wrapper.
type MiddlewareFunc func(next http.Handler) http.Handler

// Route describes a single gateway routing rule.
// A route matches on Method + Path and forwards to Upstream.
// Middleware is the per-route middleware chain (applied after global middleware).
type Route struct {
	// ID is a unique identifier for this route (used for removal via API).
	ID string

	// Method is the HTTP method this route matches (GET, POST, …).
	// Empty string matches all methods.
	Method string

	// Path is the URL path pattern this route matches.
	// Supports exact match and prefix match (trailing slash = prefix).
	Path string

	// Upstream is the base URL of the backend service.
	// Example: "http://localhost:9001"
	Upstream string

	// StripPrefix, when non-empty, is removed from the request path
	// before forwarding to the upstream.
	StripPrefix string

	// Middleware is the ordered list of per-route middleware functions.
	// Applied after global middleware, innermost to the proxy handler.
	Middleware []MiddlewareFunc
}
