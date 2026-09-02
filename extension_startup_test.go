package forge

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// startupExt records when it was registered and started so a test can check
// ordering and concurrency.
type startupExt struct {
	*BaseExtension

	deps    []string
	onStart func(name string)
	failOn  bool
	panicOn bool
}

func (e *startupExt) Dependencies() []string { return e.deps }

func (e *startupExt) Start(ctx context.Context) error {
	if e.panicOn {
		panic("boom")
	}

	if e.onStart != nil {
		e.onStart(e.Name())
	}

	if e.failOn {
		return fmt.Errorf("%s refused to start", e.Name())
	}

	return nil
}

func newStartupExt(name string, deps []string) *startupExt {
	return &startupExt{
		BaseExtension: NewBaseExtension(name, "1.0.0", "test extension"),
		deps:          deps,
	}
}

func newStartupApp(t *testing.T, parallel bool, exts ...Extension) App {
	t.Helper()

	opts := []AppOption{}
	if parallel {
		opts = append(opts, WithParallelExtensionStartup())
	}

	cfg := AppConfig{
		Name:        "startup-test",
		Version:     "1.0.0",
		Environment: "test",
		Logger:      logger.NewTestLogger(),
	}

	for _, o := range opts {
		o(&cfg)
	}

	app := NewApp(cfg)

	for _, ext := range exts {
		if err := app.RegisterExtension(ext); err != nil {
			t.Fatalf("RegisterExtension(%s): %v", ext.Name(), err)
		}
	}

	return app
}

// A dependency must be fully started before anything that declares it, whether
// or not levels run in parallel.
func TestExtensionStartup_RespectsDependencyOrder(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			var (
				mu    sync.Mutex
				order []string
			)

			record := func(name string) {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
			}

			// c depends on b, b depends on a; d is independent.
			a := newStartupExt("a", nil)
			b := newStartupExt("b", []string{"a"})
			c := newStartupExt("c", []string{"b"})
			d := newStartupExt("d", nil)

			for _, e := range []*startupExt{a, b, c, d} {
				e.onStart = record
			}

			app := newStartupApp(t, parallel, a, b, c, d)

			ctx := context.Background()
			if err := app.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}

			defer func() { _ = app.Stop(context.Background()) }()

			mu.Lock()
			defer mu.Unlock()

			if len(order) != 4 {
				t.Fatalf("started %d extensions, want 4: %v", len(order), order)
			}

			ia := slices.Index(order, "a")
			ib := slices.Index(order, "b")
			ic := slices.Index(order, "c")

			if !(ia < ib && ib < ic) {
				t.Errorf("dependency order violated: %v", order)
			}
		})
	}
}

// Independent extensions in the same level actually overlap when parallel
// startup is on. Serial startup never overlaps.
func TestExtensionStartup_IndependentExtensionsOverlap(t *testing.T) {
	const n = 4

	run := func(parallel bool) int64 {
		var (
			inFlight atomic.Int64
			peak     atomic.Int64
		)

		exts := make([]Extension, 0, n)

		for i := range n {
			e := newStartupExt(fmt.Sprintf("ext-%d", i), nil)
			e.onStart = func(string) {
				now := inFlight.Add(1)
				for {
					high := peak.Load()
					if now <= high || peak.CompareAndSwap(high, now) {
						break
					}
				}

				time.Sleep(20 * time.Millisecond)
				inFlight.Add(-1)
			}

			exts = append(exts, e)
		}

		app := newStartupApp(t, parallel, exts...)

		if err := app.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}

		defer func() { _ = app.Stop(context.Background()) }()

		return peak.Load()
	}

	if got := run(false); got != 1 {
		t.Errorf("serial startup ran %d extensions at once, want 1", got)
	}

	if got := run(true); got < 2 {
		t.Errorf("parallel startup peaked at %d concurrent extensions, want at least 2", got)
	}
}

// A failure anywhere in a level fails the whole startup, as it does serially.
func TestExtensionStartup_ParallelPropagatesFailure(t *testing.T) {
	a := newStartupExt("a", nil)
	b := newStartupExt("b", nil)
	b.failOn = true

	app := newStartupApp(t, true, a, b)

	err := app.Start(context.Background())
	if err == nil {
		_ = app.Stop(context.Background())
		t.Fatal("Start succeeded despite a failing extension")
	}

	if !strings.Contains(err.Error(), "b refused to start") {
		t.Errorf("error does not name the failing extension: %v", err)
	}
}

// A panic in a parallel Start would otherwise escape from a goroutine and kill
// the process instead of failing startup.
func TestExtensionStartup_ParallelRecoversPanic(t *testing.T) {
	a := newStartupExt("a", nil)
	b := newStartupExt("b", nil)
	b.panicOn = true

	app := newStartupApp(t, true, a, b)

	err := app.Start(context.Background())
	if err == nil {
		_ = app.Stop(context.Background())
		t.Fatal("Start succeeded despite a panicking extension")
	}

	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("error does not mention the panic: %v", err)
	}
}
