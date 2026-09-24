package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errTransient = errors.New("transient error")

// --- basic behaviour ---

func TestRetryer_SuccessOnFirstAttempt(t *testing.T) {
	r := New(DefaultConfig())
	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRetryer_SuccessOnSecondAttempt(t *testing.T) {
	r := New(Config{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
	})
	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		if calls < 2 {
			return errTransient
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil after retry, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestRetryer_AllAttemptsExhausted(t *testing.T) {
	r := New(Config{
		MaxAttempts:  3,
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Multiplier:   2.0,
	})
	calls := 0
	err := r.Do(context.Background(), func() error {
		calls++
		return errTransient
	})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
	if !errors.Is(err, errTransient) {
		t.Fatalf("expected errTransient, got %v", err)
	}
}

func TestRetryer_NoRetryOnSuccess(t *testing.T) {
	r := New(Config{MaxAttempts: 5, InitialDelay: time.Millisecond, Multiplier: 2.0})
	calls := 0
	_ = r.Do(context.Background(), func() error {
		calls++
		return nil
	})
	if calls != 1 {
		t.Fatalf("expected 1 call on immediate success, got %d", calls)
	}
}

// --- context cancellation ---

func TestRetryer_ContextCancelledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	r := New(DefaultConfig())
	calls := 0
	err := r.Do(ctx, func() error {
		calls++
		return nil
	})
	if err == nil {
		t.Fatal("expected context error")
	}
	if calls != 0 {
		t.Fatalf("expected 0 calls with pre-cancelled context, got %d", calls)
	}
}

func TestRetryer_ContextCancelledDuringBackoff(t *testing.T) {
	r := New(Config{
		MaxAttempts:  10,
		InitialDelay: 500 * time.Millisecond, // long delay so cancel fires first
		MaxDelay:     time.Second,
		Multiplier:   2.0,
	})

	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- r.Do(ctx, func() error {
			calls++
			return errTransient
		})
	}()

	// Cancel after first attempt fires.
	time.Sleep(10 * time.Millisecond)
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call before cancel, got %d", calls)
	}
}

func TestRetryer_ContextTimeout(t *testing.T) {
	r := New(Config{
		MaxAttempts:  10,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     time.Second,
		Multiplier:   2.0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := r.Do(ctx, func() error {
		return errTransient
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

// --- delay calculation ---

func TestRetryer_DelayGrowsExponentially(t *testing.T) {
	r := New(Config{
		MaxAttempts:  5,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	})

	delays := []time.Duration{
		r.delay(0), // 100ms
		r.delay(1), // 200ms
		r.delay(2), // 400ms
		r.delay(3), // 800ms
	}

	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Errorf("delay[%d]=%v should be > delay[%d]=%v", i, delays[i], i-1, delays[i-1])
		}
	}
}

func TestRetryer_DelayCappedAtMaxDelay(t *testing.T) {
	r := New(Config{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     300 * time.Millisecond,
		Multiplier:   2.0,
	})

	for i := 0; i < 10; i++ {
		d := r.delay(i)
		if d > 300*time.Millisecond {
			t.Errorf("delay[%d]=%v exceeds MaxDelay=300ms", i, d)
		}
	}
}

// --- config defaults ---

func TestRetryer_ZeroMaxAttempts_DefaultsToOne(t *testing.T) {
	r := New(Config{MaxAttempts: 0, InitialDelay: time.Millisecond, Multiplier: 2.0})
	calls := 0
	_ = r.Do(context.Background(), func() error {
		calls++
		return errTransient
	})
	if calls != 1 {
		t.Fatalf("expected 1 call (zero MaxAttempts defaults to 1), got %d", calls)
	}
}
