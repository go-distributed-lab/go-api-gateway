package middleware

import (
	"net/http"
	"strings"
)

// TransformConfig defines header and path rewrite rules applied per-route.
type TransformConfig struct {
	// AddRequestHeaders are injected into the upstream request.
	AddRequestHeaders map[string]string

	// RemoveRequestHeaders are stripped from the upstream request.
	RemoveRequestHeaders []string

	// AddResponseHeaders are injected into the client response.
	AddResponseHeaders map[string]string

	// RemoveResponseHeaders are stripped from the client response.
	RemoveResponseHeaders []string

	// RewritePath replaces the request path before forwarding.
	// Empty string means no rewrite.
	// Supports prefix replacement: "from|to" format.
	// Example: "/api/v1|/v1" rewrites /api/v1/users → /v1/users
	RewritePath string
}

// Transform returns a middleware that applies header and path transformations
// to requests and responses according to cfg.
func Transform(cfg TransformConfig) Func {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// --- Request transformation ---

			// Remove request headers.
			for _, h := range cfg.RemoveRequestHeaders {
				r.Header.Del(h)
			}

			// Add request headers.
			for k, v := range cfg.AddRequestHeaders {
				r.Header.Set(k, v)
			}

			// Rewrite path.
			if cfg.RewritePath != "" {
				r.URL.Path = rewritePath(r.URL.Path, cfg.RewritePath)
			}

			// --- Response transformation via wrapped writer ---
			tw := &transformResponseWriter{
				ResponseWriter:        w,
				addResponseHeaders:    cfg.AddResponseHeaders,
				removeResponseHeaders: cfg.RemoveResponseHeaders,
			}

			next.ServeHTTP(tw, r)
		})
	}
}

// transformResponseWriter intercepts WriteHeader to apply response header rules
// before the status code is sent to the client.
type transformResponseWriter struct {
	http.ResponseWriter
	addResponseHeaders    map[string]string
	removeResponseHeaders []string
	headersSent           bool
}

func (t *transformResponseWriter) WriteHeader(code int) {
	if t.headersSent {
		return
	}
	t.applyResponseHeaders()
	t.headersSent = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *transformResponseWriter) Write(b []byte) (int, error) {
	if !t.headersSent {
		t.applyResponseHeaders()
		t.headersSent = true
	}
	return t.ResponseWriter.Write(b)
}

func (t *transformResponseWriter) applyResponseHeaders() {
	for _, h := range t.removeResponseHeaders {
		t.ResponseWriter.Header().Del(h)
	}
	for k, v := range t.addResponseHeaders {
		t.ResponseWriter.Header().Set(k, v)
	}
}

// rewritePath applies a "from|to" prefix rewrite rule to path.
// If the rule is malformed or path doesn't match, path is returned unchanged.
func rewritePath(path, rule string) string {
	parts := strings.SplitN(rule, "|", 2)
	if len(parts) != 2 {
		return path
	}
	from, to := parts[0], parts[1]
	if strings.HasPrefix(path, from) {
		return to + path[len(from):]
	}
	return path
}