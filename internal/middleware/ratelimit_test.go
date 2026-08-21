package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func makeRL(rate, capacity float64) *RateLimiter {
	return NewRateLimiter(RateLimitConfig{
		Rate:      rate,
		Capacity:  capacity,
		BucketTTL: time.Minute,
	})
}

// --- allow / deny ---

func TestRateLimit_AllowsUpToCapacity(t *testing.T) {
	rl := makeRL(1, 5)
	h := Chain(okHandler(), rl.RateLimit())

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimit_BlocksWhenExhausted(t *testing.T) {
	rl := makeRL(1, 3)
	h := Chain(okHandler(), rl.RateLimit())

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}

	// 4th request must be blocked.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestRateLimit_DifferentClientsIndependent(t *testing.T) {
	rl := makeRL(1, 1)
	h := Chain(okHandler(), rl.RateLimit())

	for _, ip := range []string{"1.1.1.1:100", "2.2.2.2:200", "3.3.3.3:300"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("ip %s: expected 200, got %d", ip, rec.Code)
		}
	}
}

func TestRateLimit_SameClientSharedBucket(t *testing.T) {
	rl := makeRL(1, 2)
	h := Chain(okHandler(), rl.RateLimit())

	// First two requests from same client — allowed.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "5.5.5.5:999"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// Third request — bucket empty, must be blocked.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "5.5.5.5:999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

// --- headers ---

func TestRateLimit_Headers_Present(t *testing.T) {
	rl := makeRL(10, 10)
	h := Chain(okHandler(), rl.RateLimit())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
	if rec.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("expected X-RateLimit-Remaining header")
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("expected X-RateLimit-Reset header")
	}
}

func TestRateLimit_Headers_LimitMatchesCapacity(t *testing.T) {
	rl := makeRL(10, 7)
	h := Chain(okHandler(), rl.RateLimit())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	limit := rec.Header().Get("X-RateLimit-Limit")
	if limit != "7" {
		t.Fatalf("expected X-RateLimit-Limit=7, got %q", limit)
	}
}

func TestRateLimit_Headers_RemainingDecreases(t *testing.T) {
	rl := makeRL(10, 5)
	h := Chain(okHandler(), rl.RateLimit())

	var prev int = 5
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "1.2.3.4:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		remaining, _ := strconv.Atoi(rec.Header().Get("X-RateLimit-Remaining"))
		if remaining >= prev {
			t.Fatalf("request %d: remaining should decrease, got %d (prev %d)", i+1, remaining, prev)
		}
		prev = remaining
	}
}

func TestRateLimit_429_HasRetryAfter(t *testing.T) {
	rl := makeRL(1, 1)
	h := Chain(okHandler(), rl.RateLimit())

	// Exhaust the bucket.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	httptest.NewRecorder()
	h.ServeHTTP(httptest.NewRecorder(), req)

	// Blocked request must have Retry-After.
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:1000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429")
	}
}

// --- key functions ---

func TestIPKeyFunc_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	key := ipKeyFunc(req)
	if key != "192.168.1.1" {
		t.Fatalf("expected 192.168.1.1, got %q", key)
	}
}

func TestIPKeyFunc_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	key := ipKeyFunc(req)
	if key != "203.0.113.5" {
		t.Fatalf("expected 203.0.113.5, got %q", key)
	}
}

func TestHeaderKeyFunc(t *testing.T) {
	fn := HeaderKeyFunc("X-API-Key")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "my-key")
	if fn(req) != "my-key" {
		t.Fatalf("expected my-key, got %q", fn(req))
	}
}

func TestHeaderKeyFunc_Missing(t *testing.T) {
	fn := HeaderKeyFunc("X-API-Key")
	req := httptest.NewRequest("GET", "/", nil)
	if fn(req) != "anonymous" {
		t.Fatalf("expected anonymous, got %q", fn(req))
	}
}

// --- bucket management ---

func TestRateLimit_BucketCreatedPerClient(t *testing.T) {
	rl := makeRL(10, 10)
	h := Chain(okHandler(), rl.RateLimit())

	ips := []string{"1.1.1.1:1", "2.2.2.2:2", "3.3.3.3:3"}
	for _, ip := range ips {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = ip
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	if rl.BucketCount() != 3 {
		t.Fatalf("expected 3 buckets, got %d", rl.BucketCount())
	}
}

func TestRateLimit_StaleEviction(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		Rate:      10,
		Capacity:  10,
		BucketTTL: time.Millisecond, // very short TTL
	})
	h := Chain(okHandler(), rl.RateLimit())

	// Create a bucket for client A.
	reqA := httptest.NewRequest("GET", "/", nil)
	reqA.RemoteAddr = "1.1.1.1:1"
	h.ServeHTTP(httptest.NewRecorder(), reqA)

	// Wait for TTL to expire.
	time.Sleep(5 * time.Millisecond)

	// New client triggers eviction of A's stale bucket.
	reqB := httptest.NewRequest("GET", "/", nil)
	reqB.RemoteAddr = "2.2.2.2:2"
	h.ServeHTTP(httptest.NewRecorder(), reqB)

	// Only B's bucket should remain.
	if rl.BucketCount() != 1 {
		t.Fatalf("expected 1 bucket after eviction, got %d", rl.BucketCount())
	}
}

// --- concurrency ---

func TestRateLimit_ConcurrentClients(t *testing.T) {
	rl := makeRL(100, 100)
	h := Chain(okHandler(), rl.RateLimit())

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				req := httptest.NewRequest("GET", "/", nil)
				req.RemoteAddr = "1.2.3." + strconv.Itoa(id) + ":1000"
				h.ServeHTTP(httptest.NewRecorder(), req)
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}