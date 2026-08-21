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
		Rewrite:        p.rewrite,
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

// rewrite rewrites the outgoing request to point at the upstream.
// Replaces the deprecated Director field.
func (p *Proxy) rewrite(req *httputil.ProxyRequest) {
	req.SetURL(p.target)

	// Strip the gateway-side prefix before forwarding.
	if p.stripPrefix != "" {
		req.Out.URL.Path = strings.TrimPrefix(req.Out.URL.Path, p.stripPrefix)
		if req.Out.URL.Path == "" {
			req.Out.URL.Path = "/"
		}
		req.Out.URL.RawPath = ""
	}

	// Propagate X-Forwarded-For automatically (SetURL handles this).
	// Inject X-Request-ID if not already present.
	if req.In.Header.Get("X-Request-ID") == "" {
		req.Out.Header.Set("X-Request-ID", newRequestID())
	} else {
		req.Out.Header.Set("X-Request-ID", req.In.Header.Get("X-Request-ID"))
	}

	// Remove hop-by-hop headers.
	req.Out.Header.Del("Te")
	req.Out.Header.Del("Trailers")
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
