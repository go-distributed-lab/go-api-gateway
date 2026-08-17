package middleware

import "net/http"

// Func is the middleware signature.
// Wraps an http.Handler and returns a new http.Handler.
type Func func(next http.Handler) http.Handler

// Chain builds a single http.Handler by wrapping handler with each middleware
// in order. The first middleware in the slice is the outermost wrapper
// (executes first on the way in, last on the way out).
//
//	Chain(handler, Recovery, Logger, Transform)
//	→ Recovery(Logger(Transform(handler)))
func Chain(handler http.Handler, middleware ...Func) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

// responseRecorder wraps http.ResponseWriter to capture the status code
// and bytes written by downstream handlers.
// This is needed because http.ResponseWriter does not expose these after the fact.
type responseRecorder struct {
	http.ResponseWriter
	status  int
	bytes   int
	written bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *responseRecorder) WriteHeader(code int) {
	if r.written {
		return
	}
	r.status = code
	r.written = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Status returns the captured HTTP status code.
// Returns 200 if WriteHeader was never called.
func (r *responseRecorder) Status() int {
	return r.status
}

// BytesWritten returns the number of bytes written to the response body.
func (r *responseRecorder) BytesWritten() int {
	return r.bytes
}