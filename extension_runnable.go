package forge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/xraph/forge/internal/errors"
)

// RunnableExtension is an optional interface for extensions that need to run
// long-running processes (goroutines, external apps, workers) alongside the app.
//
// Extensions implementing this interface will have their Run() method called
// automatically during the PhaseAfterRun lifecycle phase, and their Shutdown()
// method called during PhaseBeforeStop.
//
// This provides a standardized way to manage external processes, background workers,
// and other long-running tasks without manually registering lifecycle hooks.
//
// Example usage:
//
//	type WorkerExtension struct {
//	    *forge.BaseExtension
//	    workerDone chan struct{}
//	}
//
//	func (e *WorkerExtension) Run(ctx context.Context) error {
//	    e.Logger().Info("starting background worker")
//	    go e.runWorker()
//	    return nil
//	}
//
//	func (e *WorkerExtension) Shutdown(ctx context.Context) error {
//	    e.Logger().Info("stopping background worker")
//	    close(e.workerDone)
//	    return nil
//	}
type RunnableExtension interface {
	Extension

	// Run starts the extension's long-running processes.
	// This is called during PhaseAfterRun, after the HTTP server starts.
	// Run should be non-blocking - start goroutines or external processes and return.
	Run(ctx context.Context) error

	// Shutdown gracefully stops the extension's long-running processes.
	// This is called during PhaseBeforeStop, before the app shuts down.
	// Implementations should respect the context deadline for graceful shutdown.
	Shutdown(ctx context.Context) error
}

// ExternalAppConfig configures an external application.
type ExternalAppConfig struct {
	// Name is a unique identifier for this external app
	Name string

	// Command is the executable to run
	Command string

	// Args are command-line arguments
	Args []string

	// Env are additional environment variables (key=value pairs)
	Env []string

	// Dir is the working directory (empty = inherit from parent)
	Dir string

	// RestartOnFailure enables automatic restart if the process crashes
	RestartOnFailure bool

	// RestartDelay is the delay before restarting (default: 5s)
	RestartDelay time.Duration

	// ShutdownTimeout is the maximum time to wait for graceful shutdown (default: 30s)
	ShutdownTimeout time.Duration

	// ForwardOutput forwards stdout/stderr to the app's stdout/stderr
	ForwardOutput bool

	// LogOutput logs stdout/stderr through the app's logger
	LogOutput bool
}

// DefaultExternalAppConfig returns default configuration.
func DefaultExternalAppConfig() ExternalAppConfig {
	return ExternalAppConfig{
		RestartOnFailure: false,
		RestartDelay:     5 * time.Second,
		ShutdownTimeout:  30 * time.Second,
		ForwardOutput:    true,
		LogOutput:        false,
	}
}

// ExternalAppExtension manages external applications as Forge extensions.
// It implements RunnableExtension and handles process lifecycle, monitoring,
// and graceful shutdown.
//
// Example:
//
//	func NewRedisExtension() forge.Extension {
//	    return forge.NewExternalAppExtension(forge.ExternalAppConfig{
//	        Name:    "redis-server",
//	        Command: "redis-server",
//	        Args:    []string{"--port", "6379"},
//	    })
//	}
type ExternalAppExtension struct {
	*BaseExtension

	config  ExternalAppConfig
	process *exec.Cmd
	running bool
	stopped chan struct{}
	waitCh  chan error // Channel to receive Wait() result (prevents double Wait() calls)
	mu      sync.RWMutex
}

// NewExternalAppExtension creates a new external app extension.
func NewExternalAppExtension(config ExternalAppConfig) *ExternalAppExtension {
	if config.Name == "" {
		config.Name = "external-app"
	}

	if config.RestartDelay == 0 {
		config.RestartDelay = 5 * time.Second
	}

	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 30 * time.Second
	}

	return &ExternalAppExtension{
		BaseExtension: NewBaseExtension(
			config.Name,
			"1.0.0",
			"External app: "+config.Command,
		),
		config:  config,
		stopped: make(chan struct{}),
	}
}

// Run starts the external application.
func (e *ExternalAppExtension) Run(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("external app %s already running", e.config.Name)
	}

	e.Logger().Info("starting external app",
		F("name", e.config.Name),
		F("command", e.config.Command),
		F("args", e.config.Args),
	)

	// Create wait channel for this run
	e.waitCh = make(chan error, 1)

	// Start the process
	if err := e.startProcess(ctx); err != nil {
		return err
	}

	// Monitor process in background
	go e.monitor(ctx)

	return nil
}

// startProcess starts the external process.
func (e *ExternalAppExtension) startProcess(ctx context.Context) error {
	// Create command
	e.process = exec.CommandContext(ctx, e.config.Command, e.config.Args...)

	// Set environment
	if len(e.config.Env) > 0 {
		e.process.Env = append(os.Environ(), e.config.Env...)
	}

	// Set working directory
	if e.config.Dir != "" {
		e.process.Dir = e.config.Dir
	}

	// Configure output
	if e.config.ForwardOutput {
		e.process.Stdout = os.Stdout
		e.process.Stderr = os.Stderr
	} else if e.config.LogOutput {
		// TODO: Implement output logging through logger
		// For now, just forward to stdout
		e.process.Stdout = os.Stdout
		e.process.Stderr = os.Stderr
	}

	// Start process
	if err := e.process.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", e.config.Name, err)
	}

	e.running = true

	e.Logger().Info("external app started",
		F("name", e.config.Name),
		F("pid", e.process.Process.Pid),
	)

	return nil
}

// monitor watches the process and optionally restarts it.
func (e *ExternalAppExtension) monitor(ctx context.Context) {
	for {
		// Wait for process to exit (only this goroutine should call Wait())
		err := e.process.Wait()

		// Send result to waitCh so Shutdown() can receive it
		select {
		case e.waitCh <- err:
		default:
			// Channel full or closed, that's ok
		}

		e.mu.Lock()
		wasRunning := e.running
		e.running = false
		e.mu.Unlock()

		// Check if we were stopped intentionally
		select {
		case <-e.stopped:
			// Intentional shutdown
			return
		default:
			// Process crashed or exited unexpectedly
		}

		if !wasRunning {
			// Already stopped, don't restart
			return
		}

		if err != nil {
			e.Logger().Error("external app exited with error",
				F("name", e.config.Name),
				F("error", err),
			)
		} else {
			e.Logger().Warn("external app exited",
				F("name", e.config.Name),
			)
		}

		// Restart if configured
		if e.config.RestartOnFailure {
			e.Logger().Info("restarting external app",
				F("name", e.config.Name),
				F("delay", e.config.RestartDelay),
			)

			select {
			case <-e.stopped:
				return
			case <-time.After(e.config.RestartDelay):
				e.mu.Lock()

				if err := e.startProcess(ctx); err != nil {
					e.Logger().Error("failed to restart external app",
						F("name", e.config.Name),
						F("error", err),
					)
					e.mu.Unlock()

					return
				}

				e.mu.Unlock()
			}
		} else {
			return
		}
	}
}

// Shutdown gracefully stops the external application.
func (e *ExternalAppExtension) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.Logger().Info("stopping external app",
		F("name", e.config.Name),
	)

	// Signal that we're stopping intentionally
	close(e.stopped)

	if e.process == nil || e.process.Process == nil {
		e.running = false

		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := e.process.Process.Signal(syscall.SIGTERM); err != nil {
		e.Logger().Warn("failed to send SIGTERM, forcing kill",
			F("name", e.config.Name),
			F("error", err),
		)
		e.running = false

		return e.process.Process.Kill()
	}

	// Use configured timeout or context deadline
	timeout := e.config.ShutdownTimeout

	shutdownCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Wait for monitor goroutine to receive Wait() result
	// This prevents calling Wait() twice (race condition)
	select {
	case <-shutdownCtx.Done():
		e.Logger().Warn("timeout waiting for external app, forcing kill",
			F("name", e.config.Name),
			F("timeout", timeout),
		)

		if err := e.process.Process.Kill(); err != nil {
			e.Logger().Error("failed to kill external app",
				F("name", e.config.Name),
				F("error", err),
			)
		}

		e.running = false

		return errors.New("shutdown timeout exceeded")
	case err := <-e.waitCh:
		if err != nil {
			e.Logger().Warn("external app exited with error during shutdown",
				F("name", e.config.Name),
				F("error", err),
			)
		} else {
			e.Logger().Info("external app stopped gracefully",
				F("name", e.config.Name),
			)
		}

		e.running = false

		return nil
	}
}

// IsRunning returns whether the external app is running.
func (e *ExternalAppExtension) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.running
}

// Health checks if the external app is healthy.
func (e *ExternalAppExtension) Health(ctx context.Context) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.running {
		return fmt.Errorf("external app %s is not running", e.config.Name)
	}

	// Check if process is still alive
	if e.process == nil || e.process.Process == nil {
		return fmt.Errorf("external app %s process is nil", e.config.Name)
	}

	// On Unix systems, sending signal 0 checks if process exists
	if err := e.process.Process.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("external app %s process check failed: %w", e.config.Name, err)
	}

	return nil
}
