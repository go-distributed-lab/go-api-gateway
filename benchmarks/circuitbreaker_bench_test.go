package benchmarks

import (
	"testing"
	"time"

	"go-api-gateway/internal/circuitbreaker"
)

func BenchmarkBreaker_AllowClosed(b *testing.B) {
	br := circuitbreaker.New(circuitbreaker.Config{
		MaxFailures: 100,
		OpenTimeout: time.Second,
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.Allow()
	}
}

func BenchmarkBreaker_AllowOpen(b *testing.B) {
	br := circuitbreaker.New(circuitbreaker.Config{
		MaxFailures: 1,
		OpenTimeout: time.Hour, // stays open
	})
	br.RecordFailure()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.Allow()
	}
}

func BenchmarkBreaker_RecordSuccess(b *testing.B) {
	br := circuitbreaker.New(circuitbreaker.Config{
		MaxFailures: 1000000,
		OpenTimeout: time.Second,
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.RecordSuccess()
	}
}

func BenchmarkBreaker_RecordFailure(b *testing.B) {
	br := circuitbreaker.New(circuitbreaker.Config{
		MaxFailures: 1000000,
		OpenTimeout: time.Second,
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.RecordFailure()
	}
}

func BenchmarkBreaker_ConcurrentAllow(b *testing.B) {
	br := circuitbreaker.New(circuitbreaker.Config{
		MaxFailures: 1000000,
		OpenTimeout: time.Second,
	})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			br.Allow()
		}
	})
}