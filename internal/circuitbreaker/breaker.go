package circuitbreaker

import (
	"sync"
	"sync/atomic"
	"time"
)

// State represents the circuit breaker state machine.
type State int32

const (
	StateClosed   State = iota // normal — requests flow through
	StateOpen                  // tripped — requests rejected immediately
	StateHalfOpen              // recovery probe — one request allowed
)

// String returns a human-readable state name.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config holds circuit breaker settings.
type Config struct {
	// MaxFailures is the number of consecutive failures before the circuit opens.
	MaxFailures int

	// OpenTimeout is how long the circuit stays open before moving to half-open.
	OpenTimeout time.Duration

	// OnStateChange is called whenever the circuit transitions between states.
	// May be nil.
	OnStateChange func(from, to State)
}

// DefaultConfig returns a sensible circuit breaker configuration.
func DefaultConfig() Config {
	return Config{
		MaxFailures: 5,
		OpenTimeout: 10 * time.Second,
	}
}

// Breaker is a thread-safe circuit breaker.
type Breaker struct {
	cfg            Config
	state          atomic.Int32 // stores State as int32
	failures       atomic.Int32 // consecutive failure count
	openedAt       atomic.Int64 // Unix nano when circuit was opened
	halfOpenLock   sync.Mutex   // ensures only one probe in half-open
	halfOpenActive bool         // true when a probe is in flight
}

// New creates a new Breaker with the given config.
func New(cfg Config) *Breaker {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 5
	}
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = 10 * time.Second
	}
	b := &Breaker{cfg: cfg}
	b.state.Store(int32(StateClosed))
	return b
}

// Allow reports whether a request should be forwarded to the upstream.
// Returns false when the circuit is open (or half-open and a probe is already in flight).
func (b *Breaker) Allow() bool {
	state := State(b.state.Load())

	switch state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if the open timeout has elapsed — if so, move to half-open.
		openedAt := time.Unix(0, b.openedAt.Load())
		if time.Since(openedAt) >= b.cfg.OpenTimeout {
			b.transitionTo(StateOpen, StateHalfOpen)
			return b.allowProbe()
		}
		return false

	case StateHalfOpen:
		return b.allowProbe()

	default:
		return true
	}
}

// allowProbe allows exactly one probe request through in half-open state.
func (b *Breaker) allowProbe() bool {
	b.halfOpenLock.Lock()
	defer b.halfOpenLock.Unlock()
	if b.halfOpenActive {
		return false
	}
	b.halfOpenActive = true
	return true
}

// RecordSuccess records a successful upstream call.
// In half-open state, closes the circuit.
func (b *Breaker) RecordSuccess() {
	state := State(b.state.Load())
	switch state {
	case StateHalfOpen:
		b.halfOpenLock.Lock()
		b.halfOpenActive = false
		b.halfOpenLock.Unlock()
		b.failures.Store(0)
		b.transitionTo(StateHalfOpen, StateClosed)
	case StateClosed:
		b.failures.Store(0)
	}
}

// RecordFailure records a failed upstream call.
// Increments the failure counter and trips the circuit when the threshold is reached.
func (b *Breaker) RecordFailure() {
	state := State(b.state.Load())

	switch state {
	case StateHalfOpen:
		b.halfOpenLock.Lock()
		b.halfOpenActive = false
		b.halfOpenLock.Unlock()
		b.openedAt.Store(time.Now().UnixNano())
		b.transitionTo(StateHalfOpen, StateOpen)

	case StateClosed:
		failures := b.failures.Add(1)
		if int(failures) >= b.cfg.MaxFailures {
			b.openedAt.Store(time.Now().UnixNano())
			b.failures.Store(0)
			b.transitionTo(StateClosed, StateOpen)
		}
	}
}

// State returns the current circuit state.
func (b *Breaker) State() State {
	return State(b.state.Load())
}

// Failures returns the current consecutive failure count.
func (b *Breaker) Failures() int {
	return int(b.failures.Load())
}

// transitionTo moves from → to if the current state is still from.
// Uses CAS to ensure only one goroutine wins the transition.
func (b *Breaker) transitionTo(from, to State) {
	if b.state.CompareAndSwap(int32(from), int32(to)) {
		if b.cfg.OnStateChange != nil {
			b.cfg.OnStateChange(from, to)
		}
	}
}
