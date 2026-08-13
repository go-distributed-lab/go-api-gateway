package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startUpstream starts a test HTTP server that echoes the request path.
func startUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

func TestNew_InvalidTarget(t *testing.T) {
	cases := []string{
		"",
		"not-a-url",
		"://missing-scheme",
	}
	for _, target := range cases {
		_, err := New(target, "")
		if err == nil {
			t.Errorf("expected error for target %q, got nil", target)
		}
	}
}

func TestNew_ValidTarget(t *testing.T) {
	_, err := New("http://localhost:9001", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestProxy_ForwardsRequest(t *testing.T) {
	upstream := startUpstream(t)
	defer upstream.Close()

	p, err := New(upstream.URL, "")
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestProxy_StripPrefix(t *testing.T) {
	upstream := startUpstream(t)
	defer upstream.Close()

	p, err := New(upstream.URL, "/api/v1")
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	upstreamPath := rec.Header().Get("X-Upstream-Path")
	if upstreamPath != "/users" {
		t.Fatalf("expected upstream to see /users, got %q", upstreamPath)
	}
}

func TestProxy_InjectsXRequestID(t *testing.T) {
	var capturedID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := New(upstream.URL, "")
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if capturedID == "" {
		t.Fatal("expected X-Request-ID to be injected, got empty")
	}
}

func TestProxy_PreservesExistingXRequestID(t *testing.T) {
	var capturedID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p, err := New(upstream.URL, "")
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "my-trace-id")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if capturedID != "my-trace-id" {
		t.Fatalf("expected X-Request-ID to be preserved, got %q", capturedID)
	}
}

func TestProxy_AddsXGatewayHeader(t *testing.T) {
	upstream := startUpstream(t)
	defer upstream.Close()

	p, err := New(upstream.URL, "")
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Header().Get("X-Gateway") != "go-api-gateway" {
		t.Fatal("expected X-Gateway header on response")
	}
}

func TestProxy_BadGatewayOnUnreachable(t *testing.T) {
	// Point at a port nothing is listening on.
	p, err := New("http://127.0.0.1:19999", "")
	if err != nil {
		t.Fatalf("new proxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bad gateway") {
		t.Fatalf("expected bad gateway body, got %q", rec.Body.String())
	}
}

func TestSingleJoiningSlash(t *testing.T) {
	cases := []struct {
		a, b, want string
	}{
		{"/", "/users", "/users"},
		{"/base", "/users", "/base/users"},
		{"/base/", "/users", "/base/users"},
		{"/base", "users", "/base/users"},
		{"/base/", "users", "/base/users"},
	}
	for _, c := range cases {
		got := singleJoiningSlash(c.a, c.b)
		if got != c.want {
			t.Errorf("singleJoiningSlash(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
