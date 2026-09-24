package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
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

func main() {
	log := logger.Default()
	addr := envOrDefault("GATEWAY_ADDR", ":8080")
	upstreamAddr := envOrDefault("UPSTREAM_ADDR", "http://localhost:9001")

	// Core components.
	rt := router.New()
	col := metrics.NewCollector()
	state := newRouteState()

	// Register default catch-all route.
	defaultRoute := gateway.Route{
		ID:       "upstream-catchall",
		Method:   "",
		Path:     "/",
		Upstream: upstreamAddr,
	}
	if err := addRoute(rt, state, defaultRoute); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: failed to add route: %v\n", err)
		os.Exit(1)
	}

	// Build HTTP mux.
	mux := http.NewServeMux()

	// Management API.
	mux.HandleFunc("GET /gateway/health", handleHealth(rt))
	mux.HandleFunc("GET /gateway/metrics", handleMetrics(col))
	mux.HandleFunc("GET /gateway/routes", handleListRoutes(rt))
	mux.HandleFunc("POST /gateway/routes", handleAddRoute(rt, state, log))
	mux.HandleFunc("DELETE /gateway/routes/{id}", handleRemoveRoute(rt, state, col, log))

	// Proxy handler.
	mux.HandleFunc("/", makeProxyHandler(rt, state, col, log))

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info("gateway starting", "addr", addr, "upstream", upstreamAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("gateway shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: shutdown: %v\n", err)
	}
	log.Info("gateway stopped")
}

// --- route state (circuit breaker per route) ---

// routeState holds per-route runtime state (circuit breaker).
type routeState struct {
	mu       sync.RWMutex
	breakers map[string]*circuitbreaker.Breaker
}

func newRouteState() *routeState {
	return &routeState{breakers: make(map[string]*circuitbreaker.Breaker)}
}

func (s *routeState) getBreaker(id string) (*circuitbreaker.Breaker, bool) {
	s.mu.RLock()
	b, ok := s.breakers[id]
	s.mu.RUnlock()
	return b, ok
}

func (s *routeState) addBreaker(id string) *circuitbreaker.Breaker {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := circuitbreaker.New(circuitbreaker.DefaultConfig())
	s.breakers[id] = b
	return b
}

func (s *routeState) removeBreaker(id string) {
	s.mu.Lock()
	delete(s.breakers, id)
	s.mu.Unlock()
}

// addRoute registers a route in the router and creates its circuit breaker.
func addRoute(rt router.Router, state *routeState, route gateway.Route) error {
	if err := rt.Add(route); err != nil {
		return err
	}
	state.addBreaker(route.ID)
	return nil
}

// --- proxy handler ---

func makeProxyHandler(
	rt router.Router,
	state *routeState,
	col *metrics.Collector,
	log *logger.Logger,
) http.HandlerFunc {
	retryer := retry.New(retry.Config{
		MaxAttempts:  3,
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		Multiplier:   2.0,
	})

	return func(w http.ResponseWriter, r *http.Request) {
		matched, err := rt.Match(r)
		if err != nil {
			http.Error(w, "no route matched", http.StatusNotFound)
			return
		}

		route := matched.Route
		m := col.GetOrCreate(route.ID)
		m.RecordRequest()

		// Get circuit breaker for this route.
		br, ok := state.getBreaker(route.ID)
		if !ok {
			br = state.addBreaker(route.ID)
		}

		// Check circuit breaker.
		if !br.Allow() {
			m.RecordError()
			m.RecordStatus(http.StatusServiceUnavailable)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"circuit open, upstream unavailable"}` + "\n"))
			return
		}

		// Build proxy.
		p, err := proxy.New(route.Upstream, route.StripPrefix)
		if err != nil {
			br.RecordFailure()
			m.RecordError()
			m.RecordStatus(http.StatusInternalServerError)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Build middleware chain.
		transformCfg := middleware.TransformConfig{
			AddRequestHeaders: map[string]string{
				"X-Gateway-Route": route.ID,
			},
		}
		handler := middleware.Chain(
			p,
			middleware.Recovery(func(err any, stack []byte) {
				log.Error("panic in proxy", "route", route.ID, "err", err)
			}),
			middleware.Logger(log),
			middleware.Transform(transformCfg),
		)

		// Wrap the handler in a recording writer to capture status.
		start := time.Now()
		rec := newStatusRecorder(w)

		// Retry loop.
		var proxyErr error
		retryErr := retryer.Do(r.Context(), func() error {
			if proxyErr != nil {
				// Reset recorder for retry.
				rec = newStatusRecorder(w)
			}
			handler.ServeHTTP(rec, r)
			// Treat 5xx as retryable upstream errors.
			if rec.status >= 500 {
				return fmt.Errorf("upstream returned %d", rec.status)
			}
			return nil
		})

		latency := time.Since(start).Nanoseconds()
		m.RecordLatency(latency)
		m.RecordStatus(rec.status)

		if retryErr != nil {
			br.RecordFailure()
			m.RecordError()
		} else {
			br.RecordSuccess()
		}
	}
}

// statusRecorder wraps http.ResponseWriter to capture the written status code.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.written {
		return
	}
	s.status = code
	s.written = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.written {
		s.written = true
	}
	return s.ResponseWriter.Write(b)
}

// --- management handlers ---

func handleHealth(rt router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routes := rt.Routes()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"routes": len(routes),
		})
	}
}

func handleMetrics(col *metrics.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := col.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(snap)
	}
}

func handleListRoutes(rt router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routes := rt.Routes()
		out := make([]routeResponse, len(routes))
		for i, route := range routes {
			out[i] = toRouteResponse(route)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func handleAddRoute(rt router.Router, state *routeState, log *logger.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req routeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}

		route := gateway.Route{
			ID:          req.ID,
			Method:      req.Method,
			Path:        req.Path,
			Upstream:    req.Upstream,
			StripPrefix: req.StripPrefix,
		}

		if err := addRoute(rt, state, route); err != nil {
			status := http.StatusInternalServerError
			if err == gateway.ErrRouteDuplicate {
				status = http.StatusConflict
			} else if err == gateway.ErrInvalidRoute {
				status = http.StatusBadRequest
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		log.Info("route added", "id", route.ID, "path", route.Path, "upstream", route.Upstream)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(toRouteResponse(route))
	}
}

func handleRemoveRoute(
	rt router.Router,
	state *routeState,
	col *metrics.Collector,
	log *logger.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, `{"error":"missing route id"}`, http.StatusBadRequest)
			return
		}

		if err := rt.Remove(id); err != nil {
			status := http.StatusInternalServerError
			if err == gateway.ErrRouteNotFound {
				status = http.StatusNotFound
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		state.removeBreaker(id)
		col.Remove(id)

		log.Info("route removed", "id", id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- JSON types ---

type routeRequest struct {
	ID          string `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Upstream    string `json:"upstream"`
	StripPrefix string `json:"strip_prefix"`
}

type routeResponse struct {
	ID          string `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Upstream    string `json:"upstream"`
	StripPrefix string `json:"strip_prefix,omitempty"`
}

func toRouteResponse(r gateway.Route) routeResponse {
	return routeResponse{
		ID:          r.ID,
		Method:      r.Method,
		Path:        r.Path,
		Upstream:    r.Upstream,
		StripPrefix: r.StripPrefix,
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
