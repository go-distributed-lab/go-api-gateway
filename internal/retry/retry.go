package retry

import (
	"context"
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

// Retryer executes an operation with retry semantics.
// Implementations must respect context cancellation.
type Retryer interface {
	// Do calls fn up to MaxAttempts times.
	// Stops early on context cancellation or when fn returns nil.
	// Returns the last error if all attempts fail.
	Do(ctx context.Context, fn func() error) error
}
