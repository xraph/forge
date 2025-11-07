package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xraph/forge"
)

// ExternalApp represents an external application managed by Forge
type ExternalApp struct {
	name    string
	command string
	args    []string
	process *exec.Cmd
	logger  forge.Logger
	mu      sync.Mutex
	running bool
}

// NewExternalApp creates a new external app manager
func NewExternalApp(name, command string, args []string, logger forge.Logger) *ExternalApp {
	return &ExternalApp{
		name:    name,
		command: command,
		args:    args,
		logger:  logger,
	}
}

// Start starts the external application
func (e *ExternalApp) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("%s already running", e.name)
	}

	e.logger.Info("starting external app",
		forge.F("name", e.name),
		forge.F("command", e.command),
	)

	// Create command
	e.process = exec.CommandContext(ctx, e.command, e.args...)
	e.process.Stdout = os.Stdout
	e.process.Stderr = os.Stderr

	// Start process
	if err := e.process.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", e.name, err)
	}

	e.running = true

	e.logger.Info("external app started",
		forge.F("name", e.name),
		forge.F("pid", e.process.Process.Pid),
	)

	// Monitor process in background
	go e.monitor()

	return nil
}

// Stop stops the external application gracefully
func (e *ExternalApp) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.logger.Info("stopping external app",
		forge.F("name", e.name),
	)

	if e.process == nil || e.process.Process == nil {
		e.running = false
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := e.process.Process.Signal(syscall.SIGTERM); err != nil {
		e.logger.Warn("failed to send SIGTERM, forcing kill",
			forge.F("name", e.name),
			forge.F("error", err),
		)
		return e.process.Process.Kill()
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- e.process.Wait()
	}()

	select {
	case <-ctx.Done():
		e.logger.Warn("timeout waiting for external app, forcing kill",
			forge.F("name", e.name),
		)
		return e.process.Process.Kill()
	case err := <-done:
		if err != nil {
			e.logger.Warn("external app exited with error",
				forge.F("name", e.name),
				forge.F("error", err),
			)
		} else {
			e.logger.Info("external app stopped gracefully",
				forge.F("name", e.name),
			)
		}
		e.running = false
		return nil
	}
}

// monitor watches the process and restarts it if it crashes
func (e *ExternalApp) monitor() {
	err := e.process.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()

	if err != nil && e.running {
		e.logger.Error("external app crashed",
			forge.F("name", e.name),
			forge.F("error", err),
		)
	}

	e.running = false
}

// IsRunning returns whether the external app is running
func (e *ExternalApp) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// BackgroundWorker simulates a background worker
type BackgroundWorker struct {
	logger  forge.Logger
	done    chan struct{}
	running bool
	mu      sync.Mutex
}

// NewBackgroundWorker creates a new background worker
func NewBackgroundWorker(logger forge.Logger) *BackgroundWorker {
	return &BackgroundWorker{
		logger: logger,
		done:   make(chan struct{}),
	}
}

// Start starts the background worker
func (w *BackgroundWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	w.logger.Info("background worker started")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			w.logger.Info("background worker stopped")
			return
		case <-ticker.C:
			w.logger.Info("background worker tick")
		}
	}
}

// Stop stops the background worker
func (w *BackgroundWorker) Stop(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	w.logger.Info("stopping background worker")
	close(w.done)
	w.running = false

	return nil
}

func main() {
	// Create Forge app
	config := forge.DefaultAppConfig()
	config.Name = "lifecycle-demo"
	config.Version = "1.0.0"
	config.Environment = "development"
	config.HTTPAddress = ":8080"

	app := forge.NewApp(config)

	// Setup external apps and workers
	var (
		externalApp1 *ExternalApp
		externalApp2 *ExternalApp
		bgWorker     *BackgroundWorker
	)

	// =============================================================================
	// PHASE 1: Before Start - Validation & Pre-initialization
	// =============================================================================

	app.RegisterHookFn(forge.PhaseBeforeStart, "validate-environment", func(ctx context.Context, app forge.App) error {
		app.Logger().Info("🔍 validating environment")

		// Check required environment variables
		requiredEnvVars := []string{"HOME", "PATH"}
		for _, envVar := range requiredEnvVars {
			if os.Getenv(envVar) == "" {
				return fmt.Errorf("required environment variable %s not set", envVar)
			}
		}

		app.Logger().Info("✅ environment validation complete")
		return nil
	})

	// =============================================================================
	// PHASE 2: After Register - Configure services based on registered extensions
	// =============================================================================

	app.RegisterHookFn(forge.PhaseAfterRegister, "configure-services", func(ctx context.Context, app forge.App) error {
		app.Logger().Info("⚙️  configuring services")

		// Initialize external apps
		externalApp1 = NewExternalApp(
			"sleep-app",
			"sleep",
			[]string{"60"}, // Sleep for 60 seconds
			app.Logger(),
		)

		externalApp2 = NewExternalApp(
			"echo-app",
			"bash",
			[]string{"-c", "while true; do echo 'External app running...'; sleep 10; done"},
			app.Logger(),
		)

		bgWorker = NewBackgroundWorker(app.Logger())

		app.Logger().Info("✅ services configured")
		return nil
	})

	// =============================================================================
	// PHASE 3: After Start - Post-initialization setup
	// =============================================================================

	app.RegisterHookFn(forge.PhaseAfterStart, "post-init", func(ctx context.Context, app forge.App) error {
		app.Logger().Info("🚀 running post-initialization tasks")

		// Log app info
		app.Logger().Info("app information",
			forge.F("name", app.Name()),
			forge.F("version", app.Version()),
			forge.F("environment", app.Environment()),
		)

		app.Logger().Info("✅ post-initialization complete")
		return nil
	})

	// =============================================================================
	// PHASE 4: Before Run - Final setup before HTTP server starts
	// =============================================================================

	// Start external app 1 (with higher priority - runs first)
	opts1 := forge.DefaultLifecycleHookOptions("start-external-app-1")
	opts1.Priority = 100
	app.RegisterHook(forge.PhaseBeforeRun, func(ctx context.Context, app forge.App) error {
		app.Logger().Info("🔧 starting external app 1")
		return externalApp1.Start(ctx)
	}, opts1)

	// Start external app 2
	opts2 := forge.DefaultLifecycleHookOptions("start-external-app-2")
	opts2.Priority = 90
	app.RegisterHook(forge.PhaseBeforeRun, func(ctx context.Context, app forge.App) error {
		app.Logger().Info("🔧 starting external app 2")
		return externalApp2.Start(ctx)
	}, opts2)

	// Log ready message
	app.RegisterHookFn(forge.PhaseBeforeRun, "log-ready", func(ctx context.Context, app forge.App) error {
		app.Logger().Info("✅ all external apps started, ready to accept HTTP requests")
		return nil
	})

	// =============================================================================
	// PHASE 5: After Run - Background tasks after HTTP server starts
	// =============================================================================

	app.RegisterHookFn(forge.PhaseAfterRun, "start-background-worker", func(ctx context.Context, app forge.App) error {
		app.Logger().Info("🔄 starting background worker")
		go bgWorker.Start()
		return nil
	})

	// Log startup complete (continue even if this fails)
	notifyOpts := forge.DefaultLifecycleHookOptions("log-startup-complete")
	notifyOpts.ContinueOnError = true
	app.RegisterHook(forge.PhaseAfterRun, func(ctx context.Context, app forge.App) error {
		app.Logger().Info("🎉 STARTUP COMPLETE",
			forge.F("uptime", app.Uptime()),
		)
		return nil
	}, notifyOpts)

	// =============================================================================
	// PHASE 6: Before Stop - Pre-shutdown cleanup
	// =============================================================================

	app.RegisterHookFn(forge.PhaseBeforeStop, "log-shutdown-start", func(ctx context.Context, app forge.App) error {
		app.Logger().Info("🛑 shutdown initiated",
			forge.F("uptime", app.Uptime()),
		)
		return nil
	})

	// Stop background worker
	app.RegisterHookFn(forge.PhaseBeforeStop, "stop-background-worker", func(ctx context.Context, app forge.App) error {
		if bgWorker != nil {
			app.Logger().Info("🔄 stopping background worker")
			return bgWorker.Stop(ctx)
		}
		return nil
	})

	// Stop external apps (in reverse order)
	app.RegisterHookFn(forge.PhaseBeforeStop, "stop-external-app-2", func(ctx context.Context, app forge.App) error {
		if externalApp2 != nil && externalApp2.IsRunning() {
			app.Logger().Info("🔧 stopping external app 2")
			return externalApp2.Stop(ctx)
		}
		return nil
	})

	app.RegisterHookFn(forge.PhaseBeforeStop, "stop-external-app-1", func(ctx context.Context, app forge.App) error {
		if externalApp1 != nil && externalApp1.IsRunning() {
			app.Logger().Info("🔧 stopping external app 1")
			return externalApp1.Stop(ctx)
		}
		return nil
	})

	// =============================================================================
	// PHASE 7: After Stop - Final cleanup
	// =============================================================================

	app.RegisterHookFn(forge.PhaseAfterStop, "log-shutdown-complete", func(ctx context.Context, app forge.App) error {
		app.Logger().Info("✅ shutdown complete",
			forge.F("total_uptime", app.Uptime()),
		)
		return nil
	})

	// =============================================================================
	// Register a simple controller for testing
	// =============================================================================

	app.Router().GET("/", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]any{
			"name":    app.Name(),
			"version": app.Version(),
			"uptime":  app.Uptime().String(),
			"status": map[string]bool{
				"external_app_1_running": externalApp1 != nil && externalApp1.IsRunning(),
				"external_app_2_running": externalApp2 != nil && externalApp2.IsRunning(),
			},
		})
	})

	app.Router().GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]string{
			"status": "healthy",
		})
	})

	// =============================================================================
	// Run the application
	// =============================================================================

	fmt.Println("\n" + strings.Repeat("═", 80))
	fmt.Println("  Lifecycle Hooks Example")
	fmt.Println("  This demonstrates how to use lifecycle hooks to manage external apps")
	fmt.Println(strings.Repeat("═", 80) + "\n")

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}
}
