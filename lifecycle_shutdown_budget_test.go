package forge

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/xraph/go-utils/log"

	forgelogger "github.com/xraph/forge/internal/logger"
)

// stuckExtension's Stop ignores its context, as a driver Close or a consumer
// drain often does.
type stuckExtension struct {
	*BaseExtension

	stopCalled atomic.Bool
}

func (s *stuckExtension) Stop(context.Context) error {
	s.stopCalled.Store(true)

	select {}
}

// stopRecordingExtension records its Stop.
type stopRecordingExtension struct {
	*BaseExtension

	stopped atomic.Bool
}

func (r *stopRecordingExtension) Stop(context.Context) error {
	r.stopped.Store(true)

	return nil
}

func newRunApp(t *testing.T, exts ...Extension) *app {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("PORT", "")

	cfg := DefaultAppConfig()
	cfg.EnableConfigAutoDiscovery = false
	cfg.Environment = "test"
	cfg.Logger = NewNoopLogger()
	cfg.HTTPAddress = "127.0.0.1:0"
	cfg.ShutdownTimeout = 300 * time.Millisecond
	cfg.StopTimeout = 700 * time.Millisecond
	cfg.Extensions = exts

	return New(WithConfig(cfg)).(*app)
}

// runInBackground starts Run and returns once its signal handler is
// installed, which is before the after-run hooks are started.
func runInBackground(t *testing.T, a *app) <-chan error {
	t.Helper()

	running := make(chan struct{})

	if err := a.RegisterHookFn(PhaseAfterRun, "test-running", func(context.Context, App) error {
		close(running)

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	returned := make(chan error, 1)

	go func() { returned <- a.Run() }()

	select {
	case <-running:
	case err := <-returned:
		t.Fatalf("Run returned early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run never got going")
	}

	return returned
}

// An extension whose Stop never returns used to block Run forever. Shutdown
// is now bounded by ShutdownTimeout plus StopTimeout, and the extensions after
// the stuck one are still stopped.
func TestShutdown_StuckExtensionStopIsAbandoned(t *testing.T) {
	stuck := &stuckExtension{BaseExtension: NewBaseExtension("queue", "1.0.0", "q")}
	other := &stopRecordingExtension{BaseExtension: NewBaseExtension("cache", "1.0.0", "c")}
	a := newRunApp(t, other, stuck)

	returned := runInBackground(t, a)

	start := time.Now()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("Run still blocked 5s after SIGTERM with ShutdownTimeout=300ms and StopTimeout=700ms")
	}

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("shutdown took %v, want about ShutdownTimeout+StopTimeout", elapsed)
	}

	if !stuck.stopCalled.Load() {
		t.Error("the stuck extension's Stop was never called")
	}

	if !other.stopped.Load() {
		t.Error("an extension stopped after the stuck one was skipped")
	}
}

// A second signal during a slow shutdown exits instead of being swallowed.
func TestShutdown_SecondSignalExits(t *testing.T) {
	exited := make(chan int, 1)

	orig := forceExit
	forceExit = func(code int) { exited <- code }

	t.Cleanup(func() { forceExit = orig })

	stuck := &stuckExtension{BaseExtension: NewBaseExtension("queue", "1.0.0", "q")}
	a := newRunApp(t, stuck)
	a.config.StopTimeout = 5 * time.Second

	returned := runInBackground(t, a)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	// Wait until shutdown is inside the stuck Stop.
	deadline := time.Now().Add(5 * time.Second)
	for !stuck.stopCalled.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	select {
	case code := <-exited:
		if code == 0 {
			t.Errorf("forced exit code = 0, want non-zero")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second signal was swallowed")
	}

	// Run still finishes on its own once the stop budget runs out.
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("Run never returned")
	}
}

// A BeforeRun hook failure used to return from Run with every extension still
// running.
func TestRun_BeforeRunFailureStopsTheApp(t *testing.T) {
	ext := &stopRecordingExtension{BaseExtension: NewBaseExtension("cache", "1.0.0", "c")}
	a := newRunApp(t, ext)

	if err := a.RegisterHookFn(PhaseBeforeRun, "boom", func(context.Context, App) error {
		return errors.New("boom")
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.Run(); err == nil {
		t.Fatal("Run should fail when a BeforeRun hook fails")
	}

	if !ext.stopped.Load() {
		t.Error("the extension was not stopped")
	}

	if a.started {
		t.Error("the app still counts as started")
	}
}

// A listener that cannot bind used to return from Run with every extension
// still running.
func TestRun_ListenFailureStopsTheApp(t *testing.T) {
	busy, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	ext := &stopRecordingExtension{BaseExtension: NewBaseExtension("cache", "1.0.0", "c")}
	a := newRunApp(t, ext)
	a.config.HTTPAddress = busy.Addr().String()

	returned := make(chan error, 1)

	go func() { returned <- a.Run() }()

	select {
	case err := <-returned:
		if err == nil {
			t.Fatal("Run should fail when the address is in use")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after ListenAndServe failed")
	}

	if !ext.stopped.Load() {
		t.Error("the extension was not stopped")
	}
}

// A panicking after-run hook runs on a goroutine of its own, where a panic
// used to kill the process.
func TestRun_AfterRunHookPanicIsRecovered(t *testing.T) {
	logs := forgelogger.NewTestLogger().(*log.TestLogger)

	cfg := DefaultAppConfig()
	cfg.EnableConfigAutoDiscovery = false
	cfg.Logger = logs
	a := New(WithConfig(cfg)).(*app)

	if err := a.RegisterHookFn(PhaseAfterRun, "panics", func(context.Context, App) error {
		panic("hook bug")
	}); err != nil {
		t.Fatal(err)
	}

	a.runAfterRunHooks()

	if !logs.AssertHasLog("ERROR", "after run hook panicked") {
		t.Error("the panic was not logged")
	}
}

// Stop called with a context an earlier step already spent still gives the
// extensions a real budget, and a context with no deadline is still bounded.
func TestStop_ExpiredOrUnboundedContext(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		var sawLive atomic.Bool

		ext := &ctxCheckingExtension{BaseExtension: NewBaseExtension("db", "1.0.0", "d"), live: &sawLive}
		a := newRunApp(t, ext)

		if err := a.Start(context.Background()); err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_ = a.Stop(ctx)

		if !sawLive.Load() {
			t.Error("the extension got an already-cancelled context")
		}
	})

	t.Run("unbounded", func(t *testing.T) {
		stuck := &stuckExtension{BaseExtension: NewBaseExtension("queue", "1.0.0", "q")}
		a := newRunApp(t, stuck)

		if err := a.Start(context.Background()); err != nil {
			t.Fatal(err)
		}

		done := make(chan struct{})

		go func() {
			_ = a.Stop(context.Background())

			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Stop(context.Background()) hung on a stuck extension")
		}
	})
}

type ctxCheckingExtension struct {
	*BaseExtension

	live *atomic.Bool
}

func (c *ctxCheckingExtension) Stop(ctx context.Context) error {
	c.live.Store(ctx.Err() == nil)

	return nil
}
