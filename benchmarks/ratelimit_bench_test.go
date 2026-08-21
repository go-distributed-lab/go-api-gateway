package benchmarks

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"go-api-gateway/internal/middleware"
)

func makeBenchRL(rate, capacity float64) *middleware.RateLimiter {
	return middleware.NewRateLimiter(middleware.RateLimitConfig{
		Rate:      rate,
		Capacity:  capacity,
		BucketTTL: 5 * time.Minute,
	})
}

func BenchmarkRateLimit_Allow(b *testing.B) {
	rl := makeBenchRL(1e9, 1e9) // effectively unlimited — measures overhead only
	h := middleware.Chain(baseHandler(), rl.RateLimit())
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkRateLimit_Deny(b *testing.B) {
	rl := makeBenchRL(0.0001, 1) // almost never refills — measures deny path
	h := middleware.Chain(baseHandler(), rl.RateLimit())
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	// Exhaust the single token.
	h.ServeHTTP(httptest.NewRecorder(), req)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
}

func BenchmarkRateLimit_ManyClients(b *testing.B) {
	rl := makeBenchRL(1e9, 1e9)
	h := middleware.Chain(baseHandler(), rl.RateLimit())
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = "1.2.3." + strconv.Itoa(i%256) + ":1000"
			h.ServeHTTP(httptest.NewRecorder(), req)
			i++
		}
	})
}

func BenchmarkRateLimit_SingleClient_Parallel(b *testing.B) {
	rl := makeBenchRL(1e9, 1e9)
	h := middleware.Chain(baseHandler(), rl.RateLimit())
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
		}
	})
}
