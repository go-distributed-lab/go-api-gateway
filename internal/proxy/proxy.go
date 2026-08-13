package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Proxy forwards HTTP requests to an upstream service.
// It wraps httputil.ReverseProxy with gateway-specific behaviour:
// path rewriting, error handling, and request ID injection.
type Proxy struct {
	target      *url.URL
	stripPrefix string
	delegate    *httputil.ReverseProxy
}

// New creates a Proxy that forwards requests to target.
// stripPrefix is removed from the request path before forwarding (may be empty).
func New(target, stripPrefix string) (*Proxy, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("proxy: invalid target %q: %w", target, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("proxy: target must be an absolute URL, got %q", target)
	}

	p := &Proxy{
		target:      u,
		stripPrefix: stripPrefix,
	}

	rp := &httputil.ReverseProxy{
		Director:       p.director,
		ErrorHandler:   p.errorHandler,
		ModifyResponse: p.modifyResponse,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	p.delegate = rp
	return p, nil
}

// ServeHTTP forwards the request to the upstream.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.delegate.ServeHTTP(w, r)
}

// director rewrites the outgoing request to point at the upstream.
func (p *Proxy) director(req *http.Request) {
	// Rewrite scheme and host to upstream target.
	req.URL.Scheme = p.target.Scheme
	req.URL.Host = p.target.Host

	// Merge target path prefix with request path.
	targetPath := p.target.Path
	requestPath := req.URL.Path

	// Strip the gateway-side prefix before forwarding.
	if p.stripPrefix != "" {
		requestPath = strings.TrimPrefix(requestPath, p.stripPrefix)
	}

	// Join target base path with remaining request path.
	if targetPath == "" {
		targetPath = "/"
	}
	req.URL.Path = singleJoiningSlash(targetPath, requestPath)

	// Preserve raw query string.
	if p.target.RawQuery == "" || req.URL.RawQuery == "" {
		req.URL.RawQuery = p.target.RawQuery + req.URL.RawQuery
	} else {
		req.URL.RawQuery = p.target.RawQuery + "&" + req.URL.RawQuery
	}

	// Set X-Forwarded-For.
	clientIP := req.RemoteAddr
	if prior, ok := req.Header["X-Forwarded-For"]; ok {
		clientIP = strings.Join(prior, ", ") + ", " + clientIP
	}
	req.Header.Set("X-Forwarded-For", clientIP)

	// Inject X-Request-ID if not already present.
	if req.Header.Get("X-Request-ID") == "" {
		req.Header.Set("X-Request-ID", newRequestID())
	}

	// Remove hop-by-hop headers that must not be forwarded.
	req.Header.Del("Te")
	req.Header.Del("Trailers")
}

// modifyResponse adds a gateway identification header to every response.
func (p *Proxy) modifyResponse(resp *http.Response) error {
	resp.Header.Set("X-Gateway", "go-api-gateway")
	return nil
}

// errorHandler returns a 502 Bad Gateway when the upstream is unreachable.
func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Gateway", "go-api-gateway")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = fmt.Fprintf(w, "bad gateway: upstream unreachable\n")
}

// singleJoiningSlash joins two URL path segments with exactly one slash.
func singleJoiningSlash(a, b string) string {
	aSlash := strings.HasSuffix(a, "/")
	bSlash := strings.HasPrefix(b, "/")
	switch {
	case aSlash && bSlash:
		return a + b[1:]
	case !aSlash && !bSlash:
		return a + "/" + b
	}
	return a + b
}

// newRequestID generates a lightweight request ID from the current time.
func newRequestID() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}
