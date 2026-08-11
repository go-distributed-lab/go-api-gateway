package circuitbreaker

// State represents the circuit breaker state machine.
type State int32

const (
	// StateClosed is the normal operating state.
	// Requests flow through; failures are counted.
	StateClosed State = iota

	// StateOpen means the circuit is tripped.
	// Requests are rejected immediately without contacting the upstream.
	StateOpen

	// StateHalfOpen is the recovery probe state.
	// A limited number of requests are allowed through to test recovery.
	StateHalfOpen
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

// Breaker is the circuit breaker interface.
// All methods must be safe for concurrent use.
type Breaker interface {
	// Allow returns true if the request should be forwarded to the upstream.
	// Returns false when the circuit is open.
	Allow() bool

	// RecordSuccess records a successful upstream call.
	// In half-open state this closes the circuit.
	RecordSuccess()

	// RecordFailure records a failed upstream call.
	// Increments the failure counter; trips the circuit when threshold is reached.
	RecordFailure()

	// State returns the current circuit state.
	State() State
}
