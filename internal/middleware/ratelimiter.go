package middleware

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig configures the token bucket rate limiter.
type RateLimitConfig struct {
	// Rate is the number of tokens added per second.
	Rate float64

	// Capacity is the maximum number of tokens in the bucket.
	// Also the initial token count (bucket starts full).
	Capacity float64

	// KeyFunc extracts the rate limit key from the request.
	// Typically the client IP. Defaults to RemoteAddr if nil.
	KeyFunc func(r *http.Request) string

	// BucketTTL is how long a bucket lives without activity before cleanup.
	// Defaults to 5 minutes.
	BucketTTL time.Duration
}

// bucket holds the token state for a single client.
type bucket struct {
	mu       sync.Mutex
	tokens   float64
	lastSeen time.Time
}

// RateLimiter is the per-client token bucket rate limiter.
// Safe for concurrent use.
type RateLimiter struct {
	cfg     RateLimitConfig
	mu      sync.RWMutex
	buckets map[string]*bucket
}

// NewRateLimiter creates a RateLimiter from cfg.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = ipKeyFunc
	}
	if cfg.BucketTTL == 0 {
		cfg.BucketTTL = 5 * time.Minute
	}
	return &RateLimiter{
		cfg:     cfg,
		buckets: make(map[string]*bucket),
	}
}

// RateLimit returns a middleware that enforces the rate limiter's policy.
func (rl *RateLimiter) RateLimit() Func {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := rl.cfg.KeyFunc(r)
			b := rl.getOrCreate(key)

			allowed, remaining, resetAt := rl.take(b)

			// Always set informational headers.
			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(int64(rl.cfg.Capacity), 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(int64(math.Max(0, remaining)), 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt, 10))

			if !allowed {
				retryAfter := int64(math.Ceil(1.0 / rl.cfg.Rate))
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}` + "\n"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// take attempts to consume one token from b.
// Returns (allowed, remainingTokens, resetUnixTimestamp).
func (rl *RateLimiter) take(b *bucket) (bool, float64, int64) {
	now := time.Now()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Lazy refill: add tokens proportional to elapsed time.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens = math.Min(rl.cfg.Capacity, b.tokens+elapsed*rl.cfg.Rate)
	b.lastSeen = now

	if b.tokens < 1.0 {
		// Calculate when the bucket will have 1 token again.
		deficit := 1.0 - b.tokens
		secsUntilFull := deficit / rl.cfg.Rate
		resetAt := now.Add(time.Duration(secsUntilFull * float64(time.Second))).Unix()
		return false, b.tokens, resetAt
	}

	b.tokens -= 1.0
	// Reset time = time until bucket reaches full capacity from current level.
	secsUntilFull := (rl.cfg.Capacity - b.tokens) / rl.cfg.Rate
	resetAt := now.Add(time.Duration(secsUntilFull * float64(time.Second))).Unix()
	return true, b.tokens, resetAt
}

// getOrCreate returns the bucket for key, creating it if necessary.
// Uses double-checked locking: RLock first, Lock only on miss.
func (rl *RateLimiter) getOrCreate(key string) *bucket {
	rl.mu.RLock()
	b, ok := rl.buckets[key]
	rl.mu.RUnlock()

	if ok {
		return b
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Re-check under write lock — another goroutine may have inserted.
	if b, ok = rl.buckets[key]; ok {
		return b
	}

	b = &bucket{
		tokens:   rl.cfg.Capacity,
		lastSeen: time.Now(),
	}
	rl.buckets[key] = b

	// Lazy cleanup: evict stale buckets on every new bucket creation.
	rl.evictStale()

	return b
}

// evictStale removes buckets that have not been seen within BucketTTL.
// Must be called with rl.mu held for writing.
func (rl *RateLimiter) evictStale() {
	cutoff := time.Now().Add(-rl.cfg.BucketTTL)
	for key, b := range rl.buckets {
		b.mu.Lock()
		stale := b.lastSeen.Before(cutoff)
		b.mu.Unlock()
		if stale {
			delete(rl.buckets, key)
		}
	}
}

// BucketCount returns the number of active buckets.
// Useful for monitoring and tests.
func (rl *RateLimiter) BucketCount() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return len(rl.buckets)
}

// --- key functions ---

// ipKeyFunc extracts the client IP from RemoteAddr.
func ipKeyFunc(r *http.Request) string {
	// Check X-Forwarded-For first (set by our proxy layer).
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can be a comma-separated list; take the first entry.
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// Fall back to RemoteAddr — strip the port.
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// HeaderKeyFunc returns a KeyFunc that keys on a request header value.
// Useful for per-API-key rate limiting.
func HeaderKeyFunc(header string) func(r *http.Request) string {
	return func(r *http.Request) string {
		v := r.Header.Get(header)
		if v == "" {
			return "anonymous"
		}
		return v
	}
}