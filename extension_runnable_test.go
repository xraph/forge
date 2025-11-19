package forge

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/errors"
)

// =============================================================================
// Test RunnableExtension Interface
// =============================================================================

// MockRunnableExtension is a test implementation of RunnableExtension.
type MockRunnableExtension struct {
	*BaseExtension

	runCalled      atomic.Bool
	shutdownCalled atomic.Bool
	runError       error
	shutdownError  error
	runDelay       time.Duration
	shutdownDelay  time.Duration
}

func NewMockRunnableExtension(name string) *MockRunnableExtension {
	return &MockRunnableExtension{
		BaseExtension: NewBaseExtension(name, "1.0.0", "Mock runnable extension"),
	}
}

func (e *MockRunnableExtension) Run(ctx context.Context) error {
	if e.runDelay > 0 {
		time.Sleep(e.runDelay)
	}

	e.runCalled.Store(true)

	return e.runError
}

func (e *MockRunnableExtension) Shutdown(ctx context.Context) error {
	if e.shutdownDelay > 0 {
		time.Sleep(e.shutdownDelay)
	}

	e.shutdownCalled.Store(true)

	return e.shutdownError
}

func (e *MockRunnableExtension) WasRunCalled() bool {
	return e.runCalled.Load()
}

func (e *MockRunnableExtension) WasShutdownCalled() bool {
	return e.shutdownCalled.Load()
}

// =============================================================================
// Tests
// =============================================================================

func TestRunnableExtension_AutoRegistration(t *testing.T) {
	config := DefaultAppConfig()
	config.HTTPAddress = ":0" // Use random port
	app := NewApp(config)
	config.Logger = NewNoopLogger()
	config.Metrics = NewNoOpMetrics()

	mockExt := NewMockRunnableExtension("test-runnable")

	// Register the extension
	if err := app.RegisterExtension(mockExt); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Check that lifecycle hooks were registered
	afterRunHooks := app.LifecycleManager().GetHooks(PhaseAfterRun)
	beforeStopHooks := app.LifecycleManager().GetHooks(PhaseBeforeStop)

	// Should have auto-registered hooks
	foundRun := false

	for _, hook := range afterRunHooks {
		if hook.Name == "run-test-runnable" {
			foundRun = true

			break
		}
	}

	foundShutdown := false

	for _, hook := range beforeStopHooks {
		if hook.Name == "shutdown-test-runnable" {
			foundShutdown = true

			break
		}
	}

	if !foundRun {
		t.Error("expected Run hook to be registered in PhaseAfterRun")
	}

	if !foundShutdown {
		t.Error("expected Shutdown hook to be registered in PhaseBeforeStop")
	}
}

func TestRunnableExtension_LifecycleIntegration(t *testing.T) {
	// This test verifies that Run() and Shutdown() are called during app lifecycle
	mockExt := NewMockRunnableExtension("test-lifecycle")

	config := DefaultAppConfig()
	config.HTTPAddress = ":0"
	config.Extensions = []Extension{mockExt}
	config.Logger = NewNoopLogger()
	config.Metrics = NewNoOpMetrics()

	app := NewApp(config)

	// Start the app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	// Manually trigger PhaseAfterRun hooks
	if err := app.LifecycleManager().ExecuteHooks(ctx, PhaseAfterRun, app); err != nil {
		t.Fatalf("failed to execute after run hooks: %v", err)
	}

	// Verify Run was called
	if !mockExt.WasRunCalled() {
		t.Error("expected Run() to be called during PhaseAfterRun")
	}

	// Stop the app
	if err := app.Stop(ctx); err != nil {
		t.Fatalf("failed to stop app: %v", err)
	}

	// Verify Shutdown was called
	if !mockExt.WasShutdownCalled() {
		t.Error("expected Shutdown() to be called during PhaseBeforeStop")
	}
}

func TestRunnableExtension_ErrorHandling(t *testing.T) {
	mockExt := NewMockRunnableExtension("test-error")
	mockExt.runError = errors.New("run failed")

	config := DefaultAppConfig()
	config.HTTPAddress = ":0"
	config.Extensions = []Extension{mockExt}
	config.Logger = NewNoopLogger()
	config.Metrics = NewNoOpMetrics()

	app := NewApp(config)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	// Execute after run hooks (should fail)
	err := app.LifecycleManager().ExecuteHooks(ctx, PhaseAfterRun, app)
	if err == nil {
		t.Error("expected error from Run hook")
	}

	if !mockExt.WasRunCalled() {
		t.Error("expected Run() to be called even though it failed")
	}
}

// =============================================================================
// Test ExternalAppExtension
// =============================================================================

func TestExternalAppExtension_BasicLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external app test in short mode")
	}

	// Create a simple external app that sleeps for a bit
	config := DefaultExternalAppConfig()
	config.Name = "sleep-test"
	config.Command = "sleep"
	config.Args = []string{"2"}
	config.ForwardOutput = false

	ext := NewExternalAppExtension(config)
	ext.SetLogger(NewNoopLogger())
	ext.SetMetrics(NewNoOpMetrics())
	ctx := context.Background()

	// Run the app
	if err := ext.Run(ctx); err != nil {
		t.Fatalf("failed to run external app: %v", err)
	}

	// Verify it's running
	if !ext.IsRunning() {
		t.Error("expected external app to be running")
	}

	// Give it a moment to actually start
	time.Sleep(100 * time.Millisecond)

	// Health check should pass
	if err := ext.Health(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := ext.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("failed to shutdown external app: %v", err)
	}

	// Verify it's stopped
	if ext.IsRunning() {
		t.Error("expected external app to be stopped")
	}
}

func TestExternalAppExtension_GracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external app test in short mode")
	}

	// Create an app that handles SIGTERM gracefully
	config := DefaultExternalAppConfig()
	config.Name = "trap-test"
	config.Command = "bash"
	config.Args = []string{"-c", "trap 'exit 0' TERM; sleep 10 & wait"}
	config.ForwardOutput = false
	config.ShutdownTimeout = 2 * time.Second

	ext := NewExternalAppExtension(config)
	ext.SetLogger(NewNoopLogger())

	ctx := context.Background()

	// Run the app
	if err := ext.Run(ctx); err != nil {
		t.Fatalf("failed to run external app: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Shutdown should complete gracefully
	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()

	if err := ext.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("failed to shutdown external app: %v", err)
	}

	elapsed := time.Since(start)

	// Should have shut down quickly (< 1s) since it handles SIGTERM
	if elapsed > 1*time.Second {
		t.Logf("shutdown took %v (expected < 1s for graceful shutdown)", elapsed)
	}
}

func TestExternalAppExtension_HealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external app test in short mode")
	}

	config := DefaultExternalAppConfig()
	config.Name = "health-test"
	config.Command = "sleep"
	config.Args = []string{"2"}
	config.ForwardOutput = false

	ext := NewExternalAppExtension(config)
	ext.SetLogger(NewNoopLogger())

	ctx := context.Background()

	// Health check should fail before running
	if err := ext.Health(ctx); err == nil {
		t.Error("expected health check to fail before running")
	}

	// Run the app
	if err := ext.Run(ctx); err != nil {
		t.Fatalf("failed to run external app: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Health check should pass while running
	if err := ext.Health(ctx); err != nil {
		t.Errorf("health check failed while running: %v", err)
	}

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ext.Shutdown(shutdownCtx)

	// Health check should fail after stopping
	if err := ext.Health(ctx); err == nil {
		t.Error("expected health check to fail after stopping")
	}
}

func TestExternalAppExtension_InvalidCommand(t *testing.T) {
	config := DefaultExternalAppConfig()
	config.Name = "invalid-test"
	config.Command = "this-command-does-not-exist-12345"
	config.ForwardOutput = false

	ext := NewExternalAppExtension(config)
	ext.SetLogger(NewNoopLogger())

	ctx := context.Background()

	// Run should fail
	err := ext.Run(ctx)
	if err == nil {
		t.Error("expected error when running invalid command")
	}

	// Should not be running
	if ext.IsRunning() {
		t.Error("expected app to not be running after failed start")
	}
}

func TestExternalAppExtension_WithEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping external app test in short mode")
	}

	// Create a temp file to write output
	tmpFile, err := os.CreateTemp(t.TempDir(), "forge-test-")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Close()

	config := DefaultExternalAppConfig()
	config.Name = "env-test"
	config.Command = "bash"
	config.Args = []string{"-c", "echo $TEST_VAR > " + tmpFile.Name()}
	config.Env = []string{"TEST_VAR=hello-world"}
	config.ForwardOutput = false

	ext := NewExternalAppExtension(config)
	ext.SetLogger(NewNoopLogger())

	ctx := context.Background()

	// Run the app
	if err := ext.Run(ctx); err != nil {
		t.Fatalf("failed to run external app: %v", err)
	}

	// Wait for it to complete
	time.Sleep(500 * time.Millisecond)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ext.Shutdown(shutdownCtx)

	// Read the file
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}

	// Verify environment variable was set
	expected := "hello-world\n"
	if string(content) != expected {
		t.Errorf("expected %q, got %q", expected, string(content))
	}
}

// =============================================================================
// Benchmark
// =============================================================================

func BenchmarkRunnableExtension_Registration(b *testing.B) {
	config := DefaultAppConfig()
	config.HTTPAddress = ":0"

	for b.Loop() {
		app := NewApp(config)
		mockExt := NewMockRunnableExtension("test")
		app.RegisterExtension(mockExt)
	}
}

func BenchmarkRunnableExtension_Lifecycle(b *testing.B) {
	for b.Loop() {
		mockExt := NewMockRunnableExtension("test")
		ctx := context.Background()
		mockExt.Run(ctx)
		mockExt.Shutdown(ctx)
	}
}
