package forge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLifecycleManager_RegisterHook(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	// Test successful registration
	hook := func(ctx context.Context, app App) error {
		return nil
	}

	opts := DefaultLifecycleHookOptions("test-hook")
	err := lm.RegisterHook(PhaseBeforeStart, hook, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Test duplicate registration
	err = lm.RegisterHook(PhaseBeforeStart, hook, opts)
	if err == nil {
		t.Fatal("expected error for duplicate hook name")
	}

	// Test nil hook
	err = lm.RegisterHook(PhaseBeforeStart, nil, opts)
	if err == nil {
		t.Fatal("expected error for nil hook")
	}

	// Test empty name
	err = lm.RegisterHook(PhaseBeforeStart, hook, LifecycleHookOptions{})
	if err == nil {
		t.Fatal("expected error for empty hook name")
	}
}

func TestLifecycleManager_RegisterHookFn(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	hook := func(ctx context.Context, app App) error {
		return nil
	}

	err := lm.RegisterHookFn(PhaseBeforeStart, "test-hook", hook)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	hooks := lm.GetHooks(PhaseBeforeStart)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Name != "test-hook" {
		t.Errorf("expected hook name 'test-hook', got %s", hooks[0].Name)
	}
}

func TestLifecycleManager_ExecuteHooks(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	// Track execution order
	var mu sync.Mutex
	executed := []string{}

	// Register hooks with different priorities
	hook1 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook1")
		return nil
	}
	hook2 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook2")
		return nil
	}
	hook3 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook3")
		return nil
	}

	// Register with different priorities (higher priority runs first)
	opts1 := DefaultLifecycleHookOptions("hook1")
	opts1.Priority = 10

	opts2 := DefaultLifecycleHookOptions("hook2")
	opts2.Priority = 50 // Should run first

	opts3 := DefaultLifecycleHookOptions("hook3")
	opts3.Priority = 30

	_ = lm.RegisterHook(PhaseBeforeStart, hook1, opts1)
	_ = lm.RegisterHook(PhaseBeforeStart, hook2, opts2)
	_ = lm.RegisterHook(PhaseBeforeStart, hook3, opts3)

	// Execute hooks
	err := lm.ExecuteHooks(context.Background(), PhaseBeforeStart, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify execution order (hook2, hook3, hook1)
	if len(executed) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executed))
	}
	if executed[0] != "hook2" {
		t.Errorf("expected hook2 first, got %s", executed[0])
	}
	if executed[1] != "hook3" {
		t.Errorf("expected hook3 second, got %s", executed[1])
	}
	if executed[2] != "hook1" {
		t.Errorf("expected hook1 third, got %s", executed[2])
	}
}

func TestLifecycleManager_ExecuteHooks_Error(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	// Track execution
	var mu sync.Mutex
	executed := []string{}

	hook1 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook1")
		return nil
	}
	hook2 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook2")
		return errors.New("hook2 failed")
	}
	hook3 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook3")
		return nil
	}

	// Register hooks (hook2 will fail)
	opts1 := DefaultLifecycleHookOptions("hook1")
	opts1.Priority = 30

	opts2 := DefaultLifecycleHookOptions("hook2")
	opts2.Priority = 20

	opts3 := DefaultLifecycleHookOptions("hook3")
	opts3.Priority = 10

	_ = lm.RegisterHook(PhaseBeforeStart, hook1, opts1)
	_ = lm.RegisterHook(PhaseBeforeStart, hook2, opts2)
	_ = lm.RegisterHook(PhaseBeforeStart, hook3, opts3)

	// Execute hooks - should fail on hook2
	err := lm.ExecuteHooks(context.Background(), PhaseBeforeStart, nil)
	if err == nil {
		t.Fatal("expected error from hook2")
	}

	// Verify hook3 was not executed (stopped on error)
	if len(executed) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(executed))
	}
	if executed[0] != "hook1" {
		t.Errorf("expected hook1 first, got %s", executed[0])
	}
	if executed[1] != "hook2" {
		t.Errorf("expected hook2 second, got %s", executed[1])
	}
}

func TestLifecycleManager_ExecuteHooks_ContinueOnError(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	// Track execution
	var mu sync.Mutex
	executed := []string{}

	hook1 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook1")
		return nil
	}
	hook2 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook2")
		return errors.New("hook2 failed")
	}
	hook3 := func(ctx context.Context, app App) error {
		mu.Lock()
		defer mu.Unlock()
		executed = append(executed, "hook3")
		return nil
	}

	// Register hooks (hook2 will fail but continue)
	opts1 := DefaultLifecycleHookOptions("hook1")
	opts1.Priority = 30

	opts2 := DefaultLifecycleHookOptions("hook2")
	opts2.Priority = 20
	opts2.ContinueOnError = true // Continue execution even on error

	opts3 := DefaultLifecycleHookOptions("hook3")
	opts3.Priority = 10

	_ = lm.RegisterHook(PhaseBeforeStart, hook1, opts1)
	_ = lm.RegisterHook(PhaseBeforeStart, hook2, opts2)
	_ = lm.RegisterHook(PhaseBeforeStart, hook3, opts3)

	// Execute hooks - should continue despite hook2 error
	err := lm.ExecuteHooks(context.Background(), PhaseBeforeStart, nil)
	if err == nil {
		t.Fatal("expected error from hook2")
	}

	// Verify all hooks executed (continued despite error)
	if len(executed) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(executed))
	}
	if executed[0] != "hook1" {
		t.Errorf("expected hook1 first, got %s", executed[0])
	}
	if executed[1] != "hook2" {
		t.Errorf("expected hook2 second, got %s", executed[1])
	}
	if executed[2] != "hook3" {
		t.Errorf("expected hook3 third, got %s", executed[2])
	}
}

func TestLifecycleManager_RemoveHook(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	hook := func(ctx context.Context, app App) error {
		return nil
	}

	// Register hook
	opts := DefaultLifecycleHookOptions("test-hook")
	_ = lm.RegisterHook(PhaseBeforeStart, hook, opts)

	// Verify it's registered
	hooks := lm.GetHooks(PhaseBeforeStart)
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}

	// Remove hook
	err := lm.RemoveHook(PhaseBeforeStart, "test-hook")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify it's removed
	hooks = lm.GetHooks(PhaseBeforeStart)
	if len(hooks) != 0 {
		t.Fatalf("expected 0 hooks, got %d", len(hooks))
	}

	// Try to remove non-existent hook
	err = lm.RemoveHook(PhaseBeforeStart, "non-existent")
	if err == nil {
		t.Fatal("expected error for non-existent hook")
	}
}

func TestLifecycleManager_ClearHooks(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	hook := func(ctx context.Context, app App) error {
		return nil
	}

	// Register multiple hooks
	_ = lm.RegisterHookFn(PhaseBeforeStart, "hook1", hook)
	_ = lm.RegisterHookFn(PhaseBeforeStart, "hook2", hook)
	_ = lm.RegisterHookFn(PhaseBeforeStart, "hook3", hook)

	// Verify they're registered
	hooks := lm.GetHooks(PhaseBeforeStart)
	if len(hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(hooks))
	}

	// Clear all hooks
	lm.ClearHooks(PhaseBeforeStart)

	// Verify they're cleared
	hooks = lm.GetHooks(PhaseBeforeStart)
	if len(hooks) != 0 {
		t.Fatalf("expected 0 hooks, got %d", len(hooks))
	}
}

func TestLifecycleManager_MultiplePhases(t *testing.T) {
	logger := NewNoopLogger()
	lm := NewLifecycleManager(logger)

	// Track execution per phase
	var mu sync.Mutex
	executed := make(map[LifecyclePhase][]string)

	createHook := func(phase LifecyclePhase, name string) LifecycleHook {
		return func(ctx context.Context, app App) error {
			mu.Lock()
			defer mu.Unlock()
			executed[phase] = append(executed[phase], name)
			return nil
		}
	}

	// Register hooks for different phases
	_ = lm.RegisterHookFn(PhaseBeforeStart, "before-start", createHook(PhaseBeforeStart, "before-start"))
	_ = lm.RegisterHookFn(PhaseAfterStart, "after-start", createHook(PhaseAfterStart, "after-start"))
	_ = lm.RegisterHookFn(PhaseBeforeRun, "before-run", createHook(PhaseBeforeRun, "before-run"))
	_ = lm.RegisterHookFn(PhaseAfterRun, "after-run", createHook(PhaseAfterRun, "after-run"))

	// Execute each phase
	_ = lm.ExecuteHooks(context.Background(), PhaseBeforeStart, nil)
	_ = lm.ExecuteHooks(context.Background(), PhaseAfterStart, nil)
	_ = lm.ExecuteHooks(context.Background(), PhaseBeforeRun, nil)
	_ = lm.ExecuteHooks(context.Background(), PhaseAfterRun, nil)

	// Verify each phase executed correctly
	if len(executed[PhaseBeforeStart]) != 1 || executed[PhaseBeforeStart][0] != "before-start" {
		t.Error("PhaseBeforeStart hook not executed correctly")
	}
	if len(executed[PhaseAfterStart]) != 1 || executed[PhaseAfterStart][0] != "after-start" {
		t.Error("PhaseAfterStart hook not executed correctly")
	}
	if len(executed[PhaseBeforeRun]) != 1 || executed[PhaseBeforeRun][0] != "before-run" {
		t.Error("PhaseBeforeRun hook not executed correctly")
	}
	if len(executed[PhaseAfterRun]) != 1 || executed[PhaseAfterRun][0] != "after-run" {
		t.Error("PhaseAfterRun hook not executed correctly")
	}
}

func TestApp_LifecycleHooks_Integration(t *testing.T) {
	// Track execution order
	var mu sync.Mutex
	executed := []string{}

	createHook := func(name string) LifecycleHook {
		return func(ctx context.Context, app App) error {
			mu.Lock()
			defer mu.Unlock()
			executed = append(executed, name)
			return nil
		}
	}

	// Create app
	config := DefaultAppConfig()
	config.Name = "lifecycle-test"
	config.HTTPAddress = ":9999" // Use different port to avoid conflicts

	app := NewApp(config)

	// Register hooks for all phases
	_ = app.RegisterHookFn(PhaseBeforeStart, "before-start", createHook("before-start"))
	_ = app.RegisterHookFn(PhaseAfterRegister, "after-register", createHook("after-register"))
	_ = app.RegisterHookFn(PhaseAfterStart, "after-start", createHook("after-start"))
	_ = app.RegisterHookFn(PhaseBeforeStop, "before-stop", createHook("before-stop"))
	_ = app.RegisterHookFn(PhaseAfterStop, "after-stop", createHook("after-stop"))

	// Start and stop app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	// Give it a moment to fully start
	time.Sleep(100 * time.Millisecond)

	if err := app.Stop(ctx); err != nil {
		t.Fatalf("failed to stop app: %v", err)
	}

	// Verify execution order
	expectedOrder := []string{
		"before-start",
		"after-register",
		"after-start",
		"before-stop",
		"after-stop",
	}

	if len(executed) != len(expectedOrder) {
		t.Fatalf("expected %d executions, got %d: %v", len(expectedOrder), len(executed), executed)
	}

	for i, expected := range expectedOrder {
		if executed[i] != expected {
			t.Errorf("position %d: expected %s, got %s", i, expected, executed[i])
		}
	}
}

func TestApp_RegisterHook_Methods(t *testing.T) {
	config := DefaultAppConfig()
	app := NewApp(config)

	hookCalled := false
	hook := func(ctx context.Context, app App) error {
		hookCalled = true
		return nil
	}

	// Test RegisterHookFn
	err := app.RegisterHookFn(PhaseBeforeStart, "test-hook", hook)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Test RegisterHook with custom options
	opts := DefaultLifecycleHookOptions("test-hook-2")
	opts.Priority = 100
	opts.ContinueOnError = true

	err = app.RegisterHook(PhaseBeforeStart, hook, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify hooks are registered
	hooks := app.LifecycleManager().GetHooks(PhaseBeforeStart)
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(hooks))
	}

	// Start app to trigger hooks
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	if !hookCalled {
		t.Error("hook was not called")
	}

	// Cleanup
	_ = app.Stop(ctx)
}

