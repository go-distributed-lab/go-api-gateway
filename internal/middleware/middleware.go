package middleware

import "net/http"

// Func is the middleware signature: wraps a handler and returns a new handler.
// Identical to gateway.MiddlewareFunc — redeclared here to keep internal
// packages free from a pkg/gateway import where not needed.
type Func func(next http.Handler) http.Handler

// Chain builds a single http.Handler by wrapping handler with each middleware
// in order. The first middleware in the slice is the outermost wrapper
// (executes first on the way in, last on the way out).
//
// Example:
//
//	Chain(handler, Recovery, Logger, Auth)
//	→ Recovery(Logger(Auth(handler)))
func Chain(handler http.Handler, middleware ...Func) http.Handler {
	// Apply in reverse so the first middleware ends up outermost.
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}
