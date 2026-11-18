package resilience

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/go-utils/metrics"

	"github.com/xraph/forge/internal/logger"
)

func newTestBreaker() *CircuitBreaker {
	return NewCircuitBreaker(CircuitBreakerConfig{
		Name:                "t",
		MaxRequests:         10,
		MaxFailures:         5,
		FailureThreshold:    0.5,
		SuccessThreshold:    0.8,
		RecoveryTimeout:     50 * time.Millisecond,
		HalfOpenMaxRequests: 3,
		EnableMetrics:       true,
		Logger:              logger.NewNoopLogger(),
		Metrics:             metrics.NewMockMetrics(),
	})
}

// The mutex used to be held across fn, so every caller of a breaker ran one
// at a time.
func TestCircuitBreaker_CallsRunConcurrently(t *testing.T) {
	cb := newTestBreaker()

	var (
		wg      sync.WaitGroup
		running atomic.Int32
		peak    atomic.Int32
	)

	for range 50 {
		wg.Go(func() {
			_, _ = cb.Execute(context.Background(), func() (any, error) {
				n := running.Add(1)

				for {
					p := peak.Load()
					if n <= p || peak.CompareAndSwap(p, n) {
						break
					}
				}

				time.Sleep(20 * time.Millisecond)
				running.Add(-1)

				return struct{}{}, nil
			})
		})
	}

	wg.Wait()

	if peak.Load() < 10 {
		t.Fatalf("at most %d of 50 calls ran at once; the breaker serializes callers", peak.Load())
	}
}

// Half-open judged its probes against stats that were never reset, and
// needed MaxRequests results while admitting only HalfOpenMaxRequests, so a
// breaker that tripped once never closed again.
func TestCircuitBreaker_HalfOpenRecovers(t *testing.T) {
	cb := newTestBreaker()

	fail := func() (any, error) { return nil, errors.New("down") }
	ok := func() (any, error) { return "ok", nil }

	for range 10 {
		_, _ = cb.Execute(context.Background(), fail)
	}

	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state after 10 failures = %v, want open", cb.GetState())
	}

	time.Sleep(60 * time.Millisecond)

	for i := range 3 {
		if _, err := cb.Execute(context.Background(), ok); err != nil {
			t.Fatalf("probe %d rejected: %v", i, err)
		}
	}

	if cb.GetState() != CircuitStateClosed {
		t.Fatalf("state after 3 good probes = %v, want closed", cb.GetState())
	}

	// Cumulative stats keep the history.
	if s := cb.GetStats(); s.TotalRequests != 13 || s.FailedRequests != 10 {
		t.Errorf("stats = %+v, want 13 total and 10 failed", s)
	}
}

// A failed probe sends the breaker straight back to open.
func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := newTestBreaker()

	for range 10 {
		_, _ = cb.Execute(context.Background(), func() (any, error) { return nil, errors.New("down") })
	}

	time.Sleep(60 * time.Millisecond)

	_, _ = cb.Execute(context.Background(), func() (any, error) { return nil, errors.New("still down") })

	if cb.GetState() != CircuitStateOpen {
		t.Fatalf("state after a failed probe = %v, want open", cb.GetState())
	}

	if _, err := cb.Execute(context.Background(), func() (any, error) { return struct{}{}, nil }); err == nil {
		t.Fatal("a reopened breaker should reject until the recovery timeout passes again")
	}
}

// Half-open admits HalfOpenMaxRequests probes in flight, not more.
func TestCircuitBreaker_HalfOpenLimitsInFlightProbes(t *testing.T) {
	cb := newTestBreaker()

	for range 10 {
		_, _ = cb.Execute(context.Background(), func() (any, error) { return nil, errors.New("down") })
	}

	time.Sleep(60 * time.Millisecond)

	release := make(chan struct{})

	var (
		wg       sync.WaitGroup
		admitted atomic.Int32
		rejected atomic.Int32
	)

	for range 10 {
		wg.Go(func() {
			_, err := cb.Execute(context.Background(), func() (any, error) {
				admitted.Add(1)
				<-release

				return struct{}{}, nil
			})

			if _, ok := errors.AsType[*CircuitBreakerError](err); ok {
				rejected.Add(1)
			}
		})
	}

	deadline := time.Now().Add(2 * time.Second)
	for admitted.Load()+rejected.Load() < 10 && rejected.Load() < 7 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	close(release)
	wg.Wait()

	if admitted.Load() != 3 || rejected.Load() != 7 {
		t.Fatalf("admitted %d, rejected %d; want 3 probes admitted", admitted.Load(), rejected.Load())
	}
}

// A caller whose context has a deadline used to wait behind a slow call on
// the breaker's mutex for as long as that call took.
func TestCircuitBreaker_ContextIsHonoured(t *testing.T) {
	cb := newTestBreaker()

	started := make(chan struct{})
	release := make(chan struct{})

	go func() {
		_, _ = cb.Execute(context.Background(), func() (any, error) {
			close(started)
			<-release

			return struct{}{}, nil
		})
	}()

	<-started

	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	begin := time.Now()

	if _, err := cb.Execute(ctx, func() (any, error) { return struct{}{}, nil }); err != nil {
		t.Fatalf("an unrelated call failed: %v", err)
	}

	if elapsed := time.Since(begin); elapsed > 40*time.Millisecond {
		t.Fatalf("an independent call took %v behind a slow one", elapsed)
	}

	done, cancelDone := context.WithCancel(context.Background())
	cancelDone()

	ran := false
	mark := func() (any, error) {
		ran = true

		return struct{}{}, nil
	}

	if _, err := cb.Execute(done, mark); !errors.Is(err, context.Canceled) || ran {
		t.Fatalf("a done context should return its error without running fn: err=%v ran=%v", err, ran)
	}
}

// The state-change counter counts changes, not every call made after the
// first change, and instruments are looked up once rather than per call.
func TestCircuitBreaker_MetricsCountChangesAndAreCached(t *testing.T) {
	m := metrics.NewMockMetrics()

	stateChanges := metrics.NewMockCounter()
	m.CounterFunc = func(name string, _ ...metrics.MetricOption) metrics.Counter {
		if name == "circuit_breaker_state_changes_total" {
			return stateChanges
		}

		return metrics.NewMockCounter()
	}

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		Name: "m", MaxRequests: 2, FailureThreshold: 0.5, SuccessThreshold: 1,
		RecoveryTimeout: time.Hour, HalfOpenMaxRequests: 1, EnableMetrics: true,
		Logger: logger.NewNoopLogger(), Metrics: m,
	})

	lookups := m.CounterCalls

	for range 2 {
		_, _ = cb.Execute(context.Background(), func() (any, error) { return nil, errors.New("x") })
	}

	for range 5 {
		_, _ = cb.Execute(context.Background(), func() (any, error) { return struct{}{}, nil })
	}

	if got := cb.GetStats().StateChanges; got != 1 {
		t.Fatalf("StateChanges = %d, want 1", got)
	}

	if got := stateChanges.Value(); got != 1 {
		t.Errorf("circuit_breaker_state_changes_total = %v, want 1", got)
	}

	if m.CounterCalls != lookups {
		t.Errorf("Execute looked up %d counters in the registry, want 0", m.CounterCalls-lookups)
	}
}
