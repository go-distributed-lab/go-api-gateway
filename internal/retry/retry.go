package retry

import (
	"context"
	"errors"
	"math"
	"time"
)

// Config holds retry policy settings.
type Config struct {
	// MaxAttempts is the total number of attempts (1 = no retry).
	MaxAttempts int

	// InitialDelay is the wait time before the first retry.
	InitialDelay time.Duration

	// MaxDelay caps the backoff delay regardless of multiplier.
	MaxDelay time.Duration

	// Multiplier is the exponential backoff factor (e.g. 2.0 = double each time).
	Multiplier float64
}

// DefaultConfig returns a sensible retry policy.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Multiplier:   2.0,
	}
}

// ErrMaxAttempts is returned when all attempts are exhausted.
var ErrMaxAttempts = errors.New("retry: max attempts reached")

// Retryer executes operations with retry semantics.
type Retryer struct {
	cfg Config
}

// New creates a Retryer from cfg.
func New(cfg Config) *Retryer {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.Multiplier <= 0 {
		cfg.Multiplier = 2.0
	}
	return &Retryer{cfg: cfg}
}

// Do calls fn up to MaxAttempts times.
// Stops early on context cancellation or when fn returns nil.
// Returns the last error if all attempts fail.
func (r *Retryer) Do(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < r.cfg.MaxAttempts; attempt++ {
		// Check context before each attempt.
		if err := ctx.Err(); err != nil {
			return err
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// No delay after the last attempt.
		if attempt == r.cfg.MaxAttempts-1 {
			break
		}

		delay := r.delay(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return ErrMaxAttempts
}

// delay calculates the backoff duration for the given attempt index (0-based).
func (r *Retryer) delay(attempt int) time.Duration {
	delay := float64(r.cfg.InitialDelay) * math.Pow(r.cfg.Multiplier, float64(attempt))
	if delay > float64(r.cfg.MaxDelay) {
		delay = float64(r.cfg.MaxDelay)
	}
	return time.Duration(delay)
}
