package benchmarks

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-api-gateway/internal/logger"
	"go-api-gateway/internal/middleware"
)

var sink int

func baseHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func BenchmarkChain_NoMiddleware(b *testing.B) {
	h := middleware.Chain(baseHandler())
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec.Code = http.StatusOK
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkChain_Recovery(b *testing.B) {
	h := middleware.Chain(baseHandler(), middleware.Recovery(nil))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkChain_Logger(b *testing.B) {
	log := logger.New(io.Discard)
	h := middleware.Chain(baseHandler(), middleware.Logger(log))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkChain_Transform(b *testing.B) {
	cfg := middleware.TransformConfig{
		AddRequestHeaders:     map[string]string{"X-Service": "gateway"},
		RemoveRequestHeaders:  []string{"X-Secret"},
		AddResponseHeaders:    map[string]string{"X-Powered-By": "go-api-gateway"},
		RemoveResponseHeaders: []string{"X-Internal"},
	}
	h := middleware.Chain(baseHandler(), middleware.Transform(cfg))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkChain_Full(b *testing.B) {
	log := logger.New(io.Discard)
	cfg := middleware.TransformConfig{
		AddRequestHeaders:  map[string]string{"X-Service": "gateway"},
		AddResponseHeaders: map[string]string{"X-Powered-By": "go-api-gateway"},
	}
	h := middleware.Chain(
		baseHandler(),
		middleware.Recovery(nil),
		middleware.Logger(log),
		middleware.Transform(cfg),
	)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.ServeHTTP(rec, req)
	}
}
