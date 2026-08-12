package benchmarks

import (
	"net/http/httptest"
	"testing"

	"go-api-gateway/internal/router"
	"go-api-gateway/pkg/gateway"
)

func setupRouter(b *testing.B) router.Router {
	b.Helper()
	rt := router.New()

	routes := []gateway.Route{
		{ID: "users", Method: "GET", Path: "/api/v1/users", Upstream: "http://svc-a"},
		{ID: "orders", Method: "POST", Path: "/api/v1/orders", Upstream: "http://svc-b"},
		{ID: "products", Method: "GET", Path: "/api/v1/products", Upstream: "http://svc-c"},
		{ID: "health", Method: "GET", Path: "/health", Upstream: "http://svc-d"},
		{ID: "apiv1", Method: "", Path: "/api/v1/", Upstream: "http://svc-e"},
		{ID: "catchall", Method: "", Path: "/", Upstream: "http://svc-f"},
	}
	for _, r := range routes {
		if err := rt.Add(r); err != nil {
			b.Fatalf("setup: %v", err)
		}
	}
	return rt
}

func BenchmarkRouter_ExactMatch(b *testing.B) {
	rt := setupRouter(b)
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rt.Match(req)
	}
}

func BenchmarkRouter_PrefixMatch(b *testing.B) {
	rt := setupRouter(b)
	req := httptest.NewRequest("GET", "/api/v1/unknown/deep/path", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rt.Match(req)
	}
}

func BenchmarkRouter_NoMatch(b *testing.B) {
	rt := router.New()
	// No catch-all — forces full scan with no match
	_ = rt.Add(gateway.Route{ID: "r1", Method: "GET", Path: "/api/v1/users", Upstream: "http://svc"})
	req := httptest.NewRequest("GET", "/completely/different", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rt.Match(req)
	}
}

func BenchmarkRouter_ConcurrentMatch(b *testing.B) {
	rt := setupRouter(b)
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = rt.Match(req)
		}
	})
}
