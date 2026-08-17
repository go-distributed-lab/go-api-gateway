package middleware

import (
	"net/http"
	"time"

	"go-api-gateway/internal/logger"
)

// Logger returns a middleware that logs each request using the structured logger.
// It captures: method, path, status code, response size, and duration.
func Logger(log *logger.Logger) Func {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := newResponseRecorder(w)

			next.ServeHTTP(rec, r)

			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.Status(),
				"bytes", rec.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"request_id", r.Header.Get("X-Request-ID"),
			)
		})
	}
}