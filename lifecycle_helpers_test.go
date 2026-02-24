package forge

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
)

func TestOnStarted(t *testing.T) {
	lm := NewLifecycleManager(logger.NewTestLogger())
	app := newMockAppWithLifecycle(lm)

	called := false
	err := OnStarted(app, "test-started", func(_ context.Context, _ App) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := lm.GetHooks(PhaseAfterStart)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Name != "test-started" {
		t.Errorf("expected hook name 'test-started', got '%s'", hooks[0].Name)
	}

	// Execute to verify function runs
	_ = lm.ExecuteHooks(context.Background(), PhaseAfterStart, app)
	if !called {
		t.Error("OnStarted hook was not called")
	}
}

func TestOnClose(t *testing.T) {
	lm := NewLifecycleManager(logger.NewTestLogger())
	app := newMockAppWithLifecycle(lm)

	called := false
	err := OnClose(app, "test-close", func(_ context.Context, _ App) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := lm.GetHooks(PhaseBeforeStop)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Name != "test-close" {
		t.Errorf("expected hook name 'test-close', got '%s'", hooks[0].Name)
	}

	_ = lm.ExecuteHooks(context.Background(), PhaseBeforeStop, app)
	if !called {
		t.Error("OnClose hook was not called")
	}
}

func TestOnBeforeRun(t *testing.T) {
	lm := NewLifecycleManager(logger.NewTestLogger())
	app := newMockAppWithLifecycle(lm)

	err := OnBeforeRun(app, "test-before-run", func(_ context.Context, _ App) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := lm.GetHooks(PhaseBeforeRun)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook at PhaseBeforeRun, got %d", len(hooks))
	}
	if hooks[0].Name != "test-before-run" {
		t.Errorf("expected 'test-before-run', got '%s'", hooks[0].Name)
	}
}

func TestOnAfterRun(t *testing.T) {
	lm := NewLifecycleManager(logger.NewTestLogger())
	app := newMockAppWithLifecycle(lm)

	err := OnAfterRun(app, "test-after-run", func(_ context.Context, _ App) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := lm.GetHooks(PhaseAfterRun)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook at PhaseAfterRun, got %d", len(hooks))
	}
}

func TestOnAfterRegister(t *testing.T) {
	lm := NewLifecycleManager(logger.NewTestLogger())
	app := newMockAppWithLifecycle(lm)

	err := OnAfterRegister(app, "test-after-register", func(_ context.Context, _ App) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hooks := lm.GetHooks(PhaseAfterRegister)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook at PhaseAfterRegister, got %d", len(hooks))
	}
}

func TestLifecycleHelpers_DuplicateNameError(t *testing.T) {
	lm := NewLifecycleManager(logger.NewTestLogger())
	app := newMockAppWithLifecycle(lm)

	hook := func(_ context.Context, _ App) error { return nil }

	err := OnStarted(app, "dup", hook)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	err = OnStarted(app, "dup", hook)
	if err == nil {
		t.Fatal("expected error for duplicate hook name")
	}
}

// mockAppForLifecycle is a minimal mock that delegates RegisterHookFn to a LifecycleManager.
type mockAppForLifecycle struct {
	lm LifecycleManager
}

func newMockAppWithLifecycle(lm LifecycleManager) *mockAppForLifecycle {
	return &mockAppForLifecycle{lm: lm}
}

func (m *mockAppForLifecycle) RegisterHookFn(phase LifecyclePhase, name string, hook LifecycleHook) error {
	return m.lm.RegisterHookFn(phase, name, hook)
}

// Implement the rest of App as no-ops to satisfy the interface.
func (m *mockAppForLifecycle) Container() Container               { return nil }
func (m *mockAppForLifecycle) Router() Router                     { return nil }
func (m *mockAppForLifecycle) Config() ConfigManager              { return nil }
func (m *mockAppForLifecycle) Logger() Logger                     { return NewNoopLogger() }
func (m *mockAppForLifecycle) Metrics() Metrics                   { return nil }
func (m *mockAppForLifecycle) HealthManager() HealthManager       { return nil }
func (m *mockAppForLifecycle) LifecycleManager() LifecycleManager { return m.lm }
func (m *mockAppForLifecycle) Start(_ context.Context) error      { return nil }
func (m *mockAppForLifecycle) Stop(_ context.Context) error       { return nil }
func (m *mockAppForLifecycle) Run() error                         { return nil }
func (m *mockAppForLifecycle) RegisterService(_ string, _ Factory, _ ...RegisterOption) error {
	return nil
}
func (m *mockAppForLifecycle) RegisterController(_ Controller) error { return nil }
func (m *mockAppForLifecycle) RegisterExtension(_ Extension) error   { return nil }
func (m *mockAppForLifecycle) RegisterHook(_ LifecyclePhase, _ LifecycleHook, _ LifecycleHookOptions) error {
	return nil
}
func (m *mockAppForLifecycle) Name() string                             { return "test-app" }
func (m *mockAppForLifecycle) Version() string                          { return "1.0.0" }
func (m *mockAppForLifecycle) Environment() string                      { return "test" }
func (m *mockAppForLifecycle) StartTime() time.Time                     { return time.Time{} }
func (m *mockAppForLifecycle) Uptime() time.Duration                    { return 0 }
func (m *mockAppForLifecycle) Extensions() []Extension                  { return nil }
func (m *mockAppForLifecycle) GetExtension(_ string) (Extension, error) { return nil, nil }
