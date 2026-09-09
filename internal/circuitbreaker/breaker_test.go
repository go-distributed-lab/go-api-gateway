package circuitbreaker

import (
	"sync"
	"testing"
	"time"
)

func makeBreaker(maxFailures int, openTimeout time.Duration) *Breaker {
	return New(Config{
		MaxFailures: maxFailures,
		OpenTimeout: openTimeout,
	})
}

// --- initial state ---

func TestBreaker_InitialStateClosed(t *testing.T) {
	b := makeBreaker(3, time.Second)
	if b.State() != StateClosed {
		t.Fatalf("expected closed, got %s", b.State())
	}
}

func TestBreaker_AllowWhenClosed(t *testing.T) {
	b := makeBreaker(3, time.Second)
	if !b.Allow() {
		t.Fatal("expected Allow=true when closed")
	}
}

// --- closed → open ---

func TestBreaker_OpensAfterMaxFailures(t *testing.T) {
	b := makeBreaker(3, time.Second)
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	if b.State() != StateOpen {
		t.Fatalf("expected open after 3 failures, got %s", b.State())
	}
}

func TestBreaker_DoesNotOpenBeforeThreshold(t *testing.T) {
	b := makeBreaker(3, time.Second)
	b.RecordFailure()
	b.RecordFailure()
	if b.State() != StateClosed {
		t.Fatalf("expected closed after 2 failures (threshold=3), got %s", b.State())
	}
}

func TestBreaker_DenyWhenOpen(t *testing.T) {
	b := makeBreaker(1, time.Hour) // long timeout so it stays open
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected open, got %s", b.State())
	}
	if b.Allow() {
		t.Fatal("expected Allow=false when open")
	}
}

func TestBreaker_SuccessResetFailureCount(t *testing.T) {
	b := makeBreaker(3, time.Second)
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()
	if b.Failures() != 0 {
		t.Fatalf("expected 0 failures after success, got %d", b.Failures())
	}
	if b.State() != StateClosed {
		t.Fatalf("expected closed, got %s", b.State())
	}
}

// --- open → half-open ---

func TestBreaker_MovesToHalfOpenAfterTimeout(t *testing.T) {
	b := makeBreaker(1, 10*time.Millisecond)
	b.RecordFailure()
	if b.State() != StateOpen {
		t.Fatalf("expected open, got %s", b.State())
	}

	time.Sleep(20 * time.Millisecond)

	// Allow() should trigger transition to half-open and return true.
	if !b.Allow() {
		t.Fatal("expected Allow=true after timeout (probe)")
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("expected half-open, got %s", b.State())
	}
}

func TestBreaker_HalfOpen_OnlyOneProbe(t *testing.T) {
	b := makeBreaker(1, 10*time.Millisecond)
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	// First Allow() — probe.
	first := b.Allow()
	// Second Allow() — must be blocked.
	second := b.Allow()

	if !first {
		t.Fatal("expected first Allow=true (probe)")
	}
	if second {
		t.Fatal("expected second Allow=false (probe already in flight)")
	}
}

// --- half-open → closed ---

func TestBreaker_HalfOpen_SuccessCloses(t *testing.T) {
	b := makeBreaker(1, 10*time.Millisecond)
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	b.Allow() // trigger half-open
	b.RecordSuccess()

	if b.State() != StateClosed {
		t.Fatalf("expected closed after probe success, got %s", b.State())
	}
}

// --- half-open → open ---

func TestBreaker_HalfOpen_FailureReopens(t *testing.T) {
	b := makeBreaker(1, 10*time.Millisecond)
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	b.Allow() // trigger half-open
	b.RecordFailure()

	if b.State() != StateOpen {
		t.Fatalf("expected open after probe failure, got %s", b.State())
	}
}

// --- state change callback ---

func TestBreaker_OnStateChange(t *testing.T) {
	var transitions []string
	var mu sync.Mutex

	b := New(Config{
		MaxFailures: 1,
		OpenTimeout: 10 * time.Millisecond,
		OnStateChange: func(from, to State) {
			mu.Lock()
			transitions = append(transitions, from.String()+"→"+to.String())
			mu.Unlock()
		},
	})

	b.RecordFailure() // closed → open
	time.Sleep(20 * time.Millisecond)
	b.Allow()         // open → half-open
	b.RecordSuccess() // half-open → closed

	mu.Lock()
	defer mu.Unlock()

	want := []string{"closed→open", "open→half-open", "half-open→closed"}
	if len(transitions) != len(want) {
		t.Fatalf("expected %v transitions, got %v", want, transitions)
	}
	for i, w := range want {
		if transitions[i] != w {
			t.Errorf("transition %d: want %q got %q", i, w, transitions[i])
		}
	}
}

// --- state string ---

func TestState_String(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}
	for _, c := range cases {
		if c.s.String() != c.want {
			t.Errorf("State(%d).String() = %q, want %q", c.s, c.s.String(), c.want)
		}
	}
}

// --- concurrency ---

func TestBreaker_ConcurrentRecordFailure(t *testing.T) {
	b := makeBreaker(100, time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.RecordFailure()
		}()
	}
	wg.Wait()
	// Circuit must be open — 100 failures at threshold 100.
	if b.State() != StateOpen {
		t.Fatalf("expected open after 100 concurrent failures, got %s", b.State())
	}
}

func TestBreaker_ConcurrentAllow(t *testing.T) {
	b := makeBreaker(1, 10*time.Millisecond)
	b.RecordFailure()
	time.Sleep(20 * time.Millisecond)

	// Many goroutines call Allow simultaneously — only one should get the probe.
	results := make([]bool, 50)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = b.Allow()
		}(i)
	}
	wg.Wait()

	probes := 0
	for _, r := range results {
		if r {
			probes++
		}
	}
	if probes != 1 {
		t.Fatalf("expected exactly 1 probe in half-open, got %d", probes)
	}
}
