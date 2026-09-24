package benchmarks

import (
	"context"
	"testing"
	"time"

	"go-api-gateway/internal/retry"
)

func BenchmarkRetry_SuccessFirstAttempt(b *testing.B) {
	r := retry.New(retry.Config{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
	})
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Do(ctx, func() error { return nil })
	}
}

func BenchmarkRetry_AllFail(b *testing.B) {
	r := retry.New(retry.Config{
		MaxAttempts:  3,
		InitialDelay: time.Nanosecond, // minimal delay for benchmark
		MaxDelay:     time.Nanosecond,
		Multiplier:   1.0,
	})
	ctx := context.Background()
	someErr := retry.ErrMaxAttempts
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Do(ctx, func() error { return someErr })
	}
}
