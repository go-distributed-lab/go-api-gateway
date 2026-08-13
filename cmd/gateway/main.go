package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-api-gateway/internal/logger"
	"go-api-gateway/internal/proxy"
	"go-api-gateway/internal/router"
	"go-api-gateway/pkg/gateway"
)

func main() {
	log := logger.Default()
	addr := envOrDefault("GATEWAY_ADDR", ":8080")

	// Build router.
	rt := router.New()

	// Register default routes.
	// In a real deployment these come from config file or dynamic API.
	// For now we wire a single catch-all to the upstream echo server.
	upstreamAddr := envOrDefault("UPSTREAM_ADDR", "http://localhost:9001")

	if err := rt.Add(gateway.Route{
		ID:       "upstream-catchall",
		Method:   "",
		Path:     "/",
		Upstream: upstreamAddr,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: failed to add route: %v\n", err)
		os.Exit(1)
	}

	// Build HTTP mux.
	mux := http.NewServeMux()

	// Management endpoints.
	mux.HandleFunc("GET /gateway/health", handleHealth)
	mux.HandleFunc("GET /gateway/routes", handleRoutes(rt))

	// Proxy handler — matches everything not caught by management routes.
	mux.HandleFunc("/", makeProxyHandler(rt, log))

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server.
	go func() {
		log.Info("gateway starting", "addr", addr, "upstream", upstreamAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("gateway shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: shutdown error: %v\n", err)
	}
	log.Info("gateway stopped")
}

// makeProxyHandler returns an http.HandlerFunc that routes and proxies requests.
func makeProxyHandler(rt router.Router, log *logger.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		matched, err := rt.Match(r)
		if err != nil {
			log.Info("no route matched",
				"method", r.Method,
				"path", r.URL.Path,
			)
			http.Error(w, "no route matched", http.StatusNotFound)
			return
		}

		route := matched.Route
		p, err := proxy.New(route.Upstream, route.StripPrefix)
		if err != nil {
			log.Error("failed to create proxy",
				"route", route.ID,
				"upstream", route.Upstream,
				"err", err,
			)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		p.ServeHTTP(w, r)

		log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"route", route.ID,
			"upstream", route.Upstream,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

// handleHealth responds to liveness probes.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// routeResponse is the JSON-safe representation of a Route.
// MiddlewareFunc is a function type and cannot be marshalled — omitted here.
type routeResponse struct {
	ID          string `json:"id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Upstream    string `json:"upstream"`
	StripPrefix string `json:"strip_prefix,omitempty"`
}

// handleRoutes returns a JSON list of registered routes.
func handleRoutes(rt router.Router) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routes := rt.Routes()
		out := make([]routeResponse, len(routes))
		for i, route := range routes {
			out[i] = routeResponse{
				ID:          route.ID,
				Method:      route.Method,
				Path:        route.Path,
				Upstream:    route.Upstream,
				StripPrefix: route.StripPrefix,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(out)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
