package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Proxy forwards HTTP requests to an upstream service.
// It wraps httputil.ReverseProxy with gateway-specific configuration.
type Proxy struct {
	target   *url.URL
	delegate *httputil.ReverseProxy
}

// New creates a Proxy that forwards requests to target.
// target must be a valid absolute URL (e.g. "http://localhost:9001").
func New(target string) (*Proxy, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		target:   u,
		delegate: nil, // initialised in Day 2
	}, nil
}

// ServeHTTP forwards the request to the upstream.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Implementation in Day 2.
	http.Error(w, "proxy not implemented", http.StatusNotImplemented)
}
