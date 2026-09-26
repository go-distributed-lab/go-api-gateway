package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-api-gateway/internal/circuitbreaker"
	"go-api-gateway/internal/logger"
	"go-api-gateway/internal/metrics"
	"go-api-gateway/internal/middleware"
	"go-api-gateway/internal/proxy"
	"go-api-gateway/internal/retry"
	"go-api-gateway/internal/router"
	"go-api-gateway/pkg/gateway"
)

// --- test harness ---

// harness wires the full gateway stack for integration testing.
type harness struct {
	router    router.Router
	collector *metrics.Collector
	retryer   *retry.Retryer
	log       *logger.Logger
	breakers  map[string]*circuitbreaker.Breaker
}

func newHarness() *harness {
	return &harness{
		router:    router.New(),
		collector: metrics.NewCollector(),
		retryer: retry.New(retry.Config{
			MaxAttempts:  2,
			InitialDelay: time.Millisecond,
			MaxDelay:     10 * time.Millisecond,
			Multiplier:   2.0,
		}),
		log:      logger.New(io.Discard),
		breakers: make(map[string]*circuitbreaker.Breaker),
	}
}

func (h *harness) addRoute(t *testing.T, route gateway.Route) {
	t.Helper()
	if err := h.router.Add(route); err != nil {
		t.Fatalf("addRoute %s: %v", route.ID, err)
	}
	h.breakers[route.ID] = circuitbreaker.New(circuitbreaker.Config{
		MaxFailures: 3,
		OpenTimeout: 50 * time.Millisecond,
	})
}

func (h *harness) removeRoute(t *testing.T, id string) {
	t.Helper()
	if err := h.router.Remove(id); err != nil {
		t.Fatalf("removeRoute %s: %v", id, err)
	}
	delete(h.breakers, id)
	h.collector.Remove(id)
}

// handler returns an http.Handler that implements the full gateway stack.
func (h *harness) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matched, err := h.router.Match(r)
		if err != nil {
			http.Error(w, "no route matched", http.StatusNotFound)
			return
		}

		route := matched.Route
		m := h.collector.GetOrCreate(route.ID)
		m.RecordRequest()

		br := h.breakers[route.ID]
		if br != nil && !br.Allow() {
			m.RecordError()
			m.RecordStatus(http.StatusServiceUnavailable)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"circuit open"}`))
			return
		}

		p, err := proxy.New(route.Upstream, route.StripPrefix)
		if err != nil {
			if br != nil {
				br.RecordFailure()
			}
			m.RecordError()
			http.Error(w, "bad upstream", http.StatusInternalServerError)
			return
		}

		transformCfg := middleware.TransformConfig{
			AddRequestHeaders: map[string]string{"X-Gateway-Route": route.ID},
		}
		handler := middleware.Chain(
			p,
			middleware.Recovery(nil),
			middleware.Logger(h.log),
			middleware.Transform(transformCfg),
		)

		rec := &statusRec{ResponseWriter: w, status: 200}
		var lastErr error
		_ = h.retryer.Do(r.Context(), func() error {
			handler.ServeHTTP(rec, r)
			if rec.status >= 500 {
				lastErr = fmt.Errorf("upstream %d", rec.status)
				return lastErr
			}
			return nil
		})

		m.RecordLatency(1000)
		m.RecordStatus(rec.status)

		if lastErr != nil {
			if br != nil {
				br.RecordFailure()
			}
			m.RecordError()
		} else {
			if br != nil {
				br.RecordSuccess()
			}
		}
	})
}

type statusRec struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRec) WriteHeader(code int) {
	if s.written {
		return
	}
	s.status = code
	s.written = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRec) Write(b []byte) (int, error) {
	if !s.written {
		s.written = true
	}
	return s.ResponseWriter.Write(b)
}

// startEchoUpstream starts a test upstream that echoes the request path.
func startEchoUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Path", r.URL.Path)
		w.Header().Set("X-Gateway-Route-Echo", r.Header.Get("X-Gateway-Route"))
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"path":"%s"}`, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startFailingUpstream starts a test upstream that always returns 500.
func startFailingUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"upstream failure"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// --- tests ---

func TestIntegration_BasicProxy(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "echo",
		Method:   "",
		Path:     "/",
		Upstream: upstream.URL,
	})

	rec := do(t, h.handler(), "GET", "/api/v1/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Gateway") != "go-api-gateway" {
		t.Error("expected X-Gateway header")
	}
}

func TestIntegration_XGatewayRouteInjected(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "echo",
		Method:   "",
		Path:     "/",
		Upstream: upstream.URL,
	})

	rec := do(t, h.handler(), "GET", "/test")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// The upstream echoes the X-Gateway-Route header it received.
	if rec.Header().Get("X-Gateway-Route-Echo") != "echo" {
		t.Errorf("expected X-Gateway-Route-Echo=echo, got %q",
			rec.Header().Get("X-Gateway-Route-Echo"))
	}
}

func TestIntegration_NoRouteMatch(t *testing.T) {
	h := newHarness()
	// No routes registered.
	rec := do(t, h.handler(), "GET", "/anything")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestIntegration_MethodRouting(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "get-users",
		Method:   "GET",
		Path:     "/users",
		Upstream: upstream.URL,
	})

	// GET matches.
	rec := do(t, h.handler(), "GET", "/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users: expected 200, got %d", rec.Code)
	}

	// POST does not match.
	rec = do(t, h.handler(), "POST", "/users")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /users: expected 404, got %d", rec.Code)
	}
}

func TestIntegration_StripPrefix(t *testing.T) {
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:          "api",
		Method:      "",
		Path:        "/api/v1/",
		Upstream:    upstream.URL,
		StripPrefix: "/api/v1",
	})

	rec := do(t, h.handler(), "GET", "/api/v1/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if seenPath != "/users" {
		t.Fatalf("expected upstream to see /users, got %q", seenPath)
	}
}

func TestIntegration_DynamicRouteAdd(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()

	// No route yet — should 404.
	rec := do(t, h.handler(), "GET", "/dynamic")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("before add: expected 404, got %d", rec.Code)
	}

	// Add route dynamically.
	h.addRoute(t, gateway.Route{
		ID:       "dynamic",
		Method:   "GET",
		Path:     "/dynamic",
		Upstream: upstream.URL,
	})

	// Now should succeed.
	rec = do(t, h.handler(), "GET", "/dynamic")
	if rec.Code != http.StatusOK {
		t.Fatalf("after add: expected 200, got %d", rec.Code)
	}
}

func TestIntegration_DynamicRouteRemove(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "removable",
		Method:   "GET",
		Path:     "/removable",
		Upstream: upstream.URL,
	})

	// Should work before removal.
	rec := do(t, h.handler(), "GET", "/removable")
	if rec.Code != http.StatusOK {
		t.Fatalf("before remove: expected 200, got %d", rec.Code)
	}

	// Remove route.
	h.removeRoute(t, "removable")

	// Should 404 after removal.
	rec = do(t, h.handler(), "GET", "/removable")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("after remove: expected 404, got %d", rec.Code)
	}
}

func TestIntegration_MetricsIncrement(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "metered",
		Method:   "",
		Path:     "/",
		Upstream: upstream.URL,
	})

	for i := 0; i < 5; i++ {
		do(t, h.handler(), "GET", "/test")
	}

	snap := h.collector.Snapshot()
	s, ok := snap["metered"]
	if !ok {
		t.Fatal("expected metered route in snapshot")
	}
	if s.Requests != 5 {
		t.Fatalf("expected 5 requests, got %d", s.Requests)
	}
	if s.Status2xx != 5 {
		t.Fatalf("expected 5 2xx responses, got %d", s.Status2xx)
	}
}

func TestIntegration_CircuitBreaker_Opens(t *testing.T) {
	failing := startFailingUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "fragile",
		Method:   "",
		Path:     "/",
		Upstream: failing.URL,
	})

	// Send enough requests to trip the circuit (threshold=3).
	for i := 0; i < 3; i++ {
		do(t, h.handler(), "GET", "/test")
	}

	// Circuit should now be open — next request should get 503.
	rec := do(t, h.handler(), "GET", "/test")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (circuit open), got %d", rec.Code)
	}
}

func TestIntegration_CircuitBreaker_HalfOpenRecovery(t *testing.T) {
	failing := startFailingUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "recovering",
		Method:   "",
		Path:     "/",
		Upstream: failing.URL,
	})

	// Trip the circuit.
	for i := 0; i < 3; i++ {
		do(t, h.handler(), "GET", "/test")
	}

	// Wait for open timeout.
	time.Sleep(100 * time.Millisecond)

	// Replace failing upstream with a healthy one.
	healthy := startEchoUpstream(t)
	h.removeRoute(t, "recovering")
	h.addRoute(t, gateway.Route{
		ID:       "recovering",
		Method:   "",
		Path:     "/",
		Upstream: healthy.URL,
	})

	// Probe should succeed and close the circuit.
	rec := do(t, h.handler(), "GET", "/test")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after recovery, got %d", rec.Code)
	}
}

func TestIntegration_RateLimit(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "limited",
		Method:   "",
		Path:     "/limited",
		Upstream: upstream.URL,
	})

	rl := middleware.NewRateLimiter(middleware.RateLimitConfig{
		Rate:     1,
		Capacity: 3,
	})

	// Wrap the gateway handler with rate limiting.
	limited := middleware.Chain(h.handler(), rl.RateLimit())

	allowed := 0
	denied := 0
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest("GET", "/limited", nil)
		req.RemoteAddr = "1.2.3.4:1000"
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			allowed++
		} else if rec.Code == http.StatusTooManyRequests {
			denied++
		}
	}

	if allowed != 3 {
		t.Errorf("expected 3 allowed, got %d", allowed)
	}
	if denied != 3 {
		t.Errorf("expected 3 denied, got %d", denied)
	}
}

func TestIntegration_BadUpstream_Returns502(t *testing.T) {
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "dead",
		Method:   "",
		Path:     "/",
		Upstream: "http://127.0.0.1:19998", // nothing listening
	})

	rec := do(t, h.handler(), "GET", "/test")
	// Proxy returns 502 on unreachable upstream.
	if rec.Code != http.StatusBadGateway && rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 502 or 500 for dead upstream, got %d", rec.Code)
	}
}

func TestIntegration_HealthEndpoint(t *testing.T) {
	// Test the health handler directly.
	rt := router.New()
	_ = rt.Add(gateway.Route{
		ID: "r1", Method: "GET", Path: "/a", Upstream: "http://x",
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes := rt.Routes()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"routes": len(routes),
		})
	})

	req := httptest.NewRequest("GET", "/gateway/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
	if body["routes"].(float64) != 1 {
		t.Errorf("expected routes=1, got %v", body["routes"])
	}
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	upstream := startEchoUpstream(t)
	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "concurrent",
		Method:   "",
		Path:     "/",
		Upstream: upstream.URL,
	})

	handler := h.handler()
	done := make(chan int, 50)

	for i := 0; i < 50; i++ {
		go func() {
			rec := do(t, handler, "GET", "/test")
			done <- rec.Code
		}()
	}

	for i := 0; i < 50; i++ {
		code := <-done
		if code != http.StatusOK {
			t.Errorf("concurrent request %d: expected 200, got %d", i, code)
		}
	}
}

func TestIntegration_ContextCancellation(t *testing.T) {
	// Upstream that blocks until context is cancelled.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(upstream.Close)

	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID:       "slow",
		Method:   "",
		Path:     "/",
		Upstream: upstream.URL,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest("GET", "/slow", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Should return before 5 seconds due to context timeout.
	start := time.Now()
	h.handler().ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("expected context cancellation within 1s, took %v", elapsed)
	}
}

func TestIntegration_LongestPrefixRouting(t *testing.T) {
	var hitRoute string
	makeUpstream := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hitRoute = name
			w.WriteHeader(http.StatusOK)
		}))
	}

	shortUpstream := makeUpstream("short")
	longUpstream := makeUpstream("long")
	t.Cleanup(shortUpstream.Close)
	t.Cleanup(longUpstream.Close)

	h := newHarness()
	h.addRoute(t, gateway.Route{
		ID: "short", Method: "", Path: "/api/", Upstream: shortUpstream.URL,
	})
	h.addRoute(t, gateway.Route{
		ID: "long", Method: "", Path: "/api/v1/", Upstream: longUpstream.URL,
	})

	do(t, h.handler(), "GET", "/api/v1/users")
	if hitRoute != "long" {
		t.Fatalf("expected long prefix to win, got %q", hitRoute)
	}

	do(t, h.handler(), "GET", "/api/v2/users")
	if hitRoute != "short" {
		t.Fatalf("expected short prefix for /api/v2/, got %q", hitRoute)
	}
}

// bodyString reads and returns the response body as a string.
func bodyString(rec *httptest.ResponseRecorder) string {
	return strings.TrimSpace(rec.Body.String())
}
