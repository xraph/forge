package health

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	healthinternal "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/shared"
)

func registerN(t testing.TB, m *ManagerImpl, n int) {
	t.Helper()

	for i := range n {
		name := fmt.Sprintf("ext-%d", i)
		if err := m.RegisterFn(name, func(ctx context.Context) *healthinternal.HealthResult {
			return healthinternal.NewHealthResult(name, healthinternal.HealthStatusHealthy, "ok")
		}); err != nil {
			t.Fatalf("RegisterFn(%s): %v", name, err)
		}
	}
}

// Every registered check must appear in the report, and under its own name.
// Results are collected by index now rather than through a shared map, so a
// mismatch between the index and the name would silently mislabel results.
func TestCheck_ReportsEveryCheckByName(t *testing.T) {
	m := New(nil, nil, nil, nil).(*ManagerImpl)
	registerN(t, m, 25)

	report := m.Check(context.Background())

	if len(report.Services) != 25 {
		t.Fatalf("got %d services, want 25", len(report.Services))
	}

	for i := range 25 {
		name := fmt.Sprintf("ext-%d", i)

		result, ok := report.Services[name]
		if !ok {
			t.Fatalf("missing result for %s", name)
		}

		if result.Name != name {
			t.Errorf("result under key %s carries name %s", name, result.Name)
		}
	}
}

// MaxConcurrentChecks is meant to bound how much work runs at once. It used to
// be acquired inside each goroutine, so every check got a goroutine
// immediately and the limit only governed how many of them were past the
// channel. That distinction matters when the check count grows with the number
// of installed extensions.
func TestCheck_RespectsMaxConcurrentChecks(t *testing.T) {
	cfg := DefaultHealthConfig()
	cfg.Performance.MaxConcurrentChecks = 4

	m := New(cfg, nil, nil, nil).(*ManagerImpl)

	var (
		inFlight atomic.Int64
		peak     atomic.Int64
	)

	for i := range 40 {
		name := fmt.Sprintf("ext-%d", i)
		if err := m.RegisterFn(name, func(ctx context.Context) *healthinternal.HealthResult {
			now := inFlight.Add(1)
			for {
				high := peak.Load()
				if now <= high || peak.CompareAndSwap(high, now) {
					break
				}
			}

			time.Sleep(time.Millisecond)
			inFlight.Add(-1)

			return healthinternal.NewHealthResult(name, healthinternal.HealthStatusHealthy, "ok")
		}); err != nil {
			t.Fatalf("RegisterFn: %v", err)
		}
	}

	m.Check(context.Background())

	if got := peak.Load(); got > 4 {
		t.Errorf("peak concurrent checks = %d, want at most 4", got)
	}
}

// Check reads a snapshot of the registered checks. Registering while a check
// runs must not race with that read.
func TestCheck_ConcurrentWithRegistration(t *testing.T) {
	m := New(nil, nil, nil, nil).(*ManagerImpl)
	registerN(t, m, 10)

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		for range 20 {
			m.Check(context.Background())
		}
	}()

	go func() {
		defer wg.Done()

		for i := range 20 {
			name := fmt.Sprintf("late-%d", i)
			_ = m.RegisterFn(name, func(ctx context.Context) *healthinternal.HealthResult {
				return healthinternal.NewHealthResult(name, healthinternal.HealthStatusHealthy, "ok")
			})
		}
	}()

	wg.Wait()
}

// Check runs every registered check. Its cost therefore scales with the number
// of extensions installed, and /_/health and /_/health/ready call it on every
// request. See handleHealth in app_impl.go.
func benchCheck(b *testing.B, n int) {
	m := New(nil, nil, nil, nil).(*ManagerImpl)
	registerN(b, m, n)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = m.Check(ctx)
	}
}

func BenchmarkHealthManager_Check_5(b *testing.B)  { benchCheck(b, 5) }
func BenchmarkHealthManager_Check_20(b *testing.B) { benchCheck(b, 20) }
func BenchmarkHealthManager_Check_50(b *testing.B) { benchCheck(b, 50) }

// A partial config is the normal case. forge.DefaultHealthConfig() sets only
// Enabled, and a caller filling one field in a literal leaves the rest zero.
// Those zeros used to reach time.NewTicker, which panics on a non-positive
// interval, in a goroutine where the panic killed the process at startup.
func TestNew_FillsPartialConfig(t *testing.T) {
	cfg := &HealthConfig{Enabled: true}

	m := New(cfg, nil, nil, nil).(*ManagerImpl)

	if m.config.Intervals.Check <= 0 {
		t.Errorf("Intervals.Check = %v, want a positive default", m.config.Intervals.Check)
	}

	if m.config.Intervals.Report <= 0 {
		t.Errorf("Intervals.Report = %v, want a positive default", m.config.Intervals.Report)
	}

	if m.config.Performance.MaxConcurrentChecks <= 0 {
		t.Errorf("MaxConcurrentChecks = %d, want a positive default", m.config.Performance.MaxConcurrentChecks)
	}

	if m.config.Performance.DefaultTimeout <= 0 {
		t.Errorf("DefaultTimeout = %v, want a positive default", m.config.Performance.DefaultTimeout)
	}
}

// Starting with a partial config must not panic the background loops.
func TestStart_WithPartialConfigDoesNotPanic(t *testing.T) {
	m := New(&HealthConfig{Enabled: true}, nil, nil, nil).(*ManagerImpl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give the loops a moment to reach their tickers.
	time.Sleep(20 * time.Millisecond)

	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// An explicitly configured interval survives normalization.
func TestNew_KeepsExplicitConfig(t *testing.T) {
	cfg := &HealthConfig{Enabled: true}
	cfg.Intervals.Check = 3 * time.Second
	cfg.Performance.MaxConcurrentChecks = 2

	m := New(cfg, nil, nil, nil).(*ManagerImpl)

	if m.config.Intervals.Check != 3*time.Second {
		t.Errorf("Intervals.Check = %v, want 3s", m.config.Intervals.Check)
	}

	if m.config.Performance.MaxConcurrentChecks != 2 {
		t.Errorf("MaxConcurrentChecks = %d, want 2", m.config.Performance.MaxConcurrentChecks)
	}
}

// fakeContainer reports a fixed service list. Resolve always fails, which is
// the path a name-registered service that cannot be resolved takes.
type fakeContainer struct {
	shared.Container

	services []string
}

func (f *fakeContainer) Services() []string { return f.services }

func (f *fakeContainer) Resolve(name string) (any, error) {
	return nil, fmt.Errorf("not resolvable: %s", name)
}

// Discovery registers one check per container service, not one per extension,
// and most of those services cannot answer a health question. It is opt-in, so
// a default app pays for the framework's own checks and nothing else.
func TestStart_ServiceDiscoveryIsOptIn(t *testing.T) {
	container := &fakeContainer{services: []string{"svc-a", "svc-b", "svc-c"}}

	t.Run("off by default", func(t *testing.T) {
		cfg := DefaultHealthConfig()

		m := New(cfg, nil, nil, container).(*ManagerImpl)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}

		defer func() { _ = m.Stop(context.Background()) }()

		for _, name := range container.services {
			if _, ok := m.ListChecks()[name]; ok {
				t.Errorf("registered a check for %s without opting in", name)
			}
		}

		// The framework's own checks still register.
		if _, ok := m.ListChecks()["memory"]; !ok {
			t.Error("built-in memory check missing")
		}
	})

	t.Run("opted in", func(t *testing.T) {
		cfg := DefaultHealthConfig()
		cfg.Features.AutoDiscovery = true

		m := New(cfg, nil, nil, container).(*ManagerImpl)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := m.Start(ctx); err != nil {
			t.Fatalf("Start: %v", err)
		}

		defer func() { _ = m.Stop(context.Background()) }()

		for _, name := range container.services {
			if _, ok := m.ListChecks()[name]; !ok {
				t.Errorf("missing a check for %s after opting in", name)
			}
		}

		if _, ok := m.ListChecks()["memory"]; !ok {
			t.Error("built-in memory check missing")
		}
	})
}

// A health check runs arbitrary extension code. A panic in one used to escape
// from a goroutine no caller could recover in, taking the process with it.
func TestCheck_SurvivesAPanickingCheck(t *testing.T) {
	m := New(nil, nil, nil, nil).(*ManagerImpl)

	if err := m.RegisterFn("exploding", func(ctx context.Context) *healthinternal.HealthResult {
		panic("extension went wrong")
	}); err != nil {
		t.Fatalf("RegisterFn: %v", err)
	}

	if err := m.RegisterFn("fine", func(ctx context.Context) *healthinternal.HealthResult {
		return healthinternal.NewHealthResult("fine", healthinternal.HealthStatusHealthy, "ok")
	}); err != nil {
		t.Fatalf("RegisterFn: %v", err)
	}

	report := m.Check(context.Background())

	exploded, ok := report.Services["exploding"]
	if !ok {
		t.Fatal("no result for the panicking check")
	}

	if exploded.Status != healthinternal.HealthStatusUnhealthy {
		t.Errorf("panicking check reported %v, want unhealthy", exploded.Status)
	}

	// The rest of the round still completes.
	if fine, ok := report.Services["fine"]; !ok {
		t.Error("no result for the healthy check")
	} else if fine.Status != healthinternal.HealthStatusHealthy {
		t.Errorf("healthy check reported %v", fine.Status)
	}
}
