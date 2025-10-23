package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Runner handles command execution with advanced features
type Runner struct {
	app     CLIApp
	config  RunnerConfig
	signals chan os.Signal
}

// RunnerConfig contains runner configuration
type RunnerConfig struct {
	EnableSignalHandling bool          `yaml:"enable_signal_handling" json:"enable_signal_handling"`
	GracefulTimeout      time.Duration `yaml:"graceful_timeout" json:"graceful_timeout"`
	EnableProfiling      bool          `yaml:"enable_profiling" json:"enable_profiling"`
	ProfilePort          int           `yaml:"profile_port" json:"profile_port"`
	EnableMetrics        bool          `yaml:"enable_metrics" json:"enable_metrics"`
	MetricsPort          int           `yaml:"metrics_port" json:"metrics_port"`
}

// NewRunner creates a new command runner
func NewRunner(app CLIApp, config RunnerConfig) *Runner {
	if config.GracefulTimeout == 0 {
		config.GracefulTimeout = 30 * time.Second
	}

	return &Runner{
		app:     app,
		config:  config,
		signals: make(chan os.Signal, 1),
	}
}

// Run executes the CLI application with enhanced features
func (r *Runner) Run() error {
	// Setup signal handling if enabled
	if r.config.EnableSignalHandling {
		r.setupSignalHandling()
	}

	// Setup profiling if enabled
	if r.config.EnableProfiling {
		if err := r.setupProfiling(); err != nil {
			return fmt.Errorf("failed to setup profiling: %w", err)
		}
	}

	// Setup metrics if enabled
	if r.config.EnableMetrics {
		if err := r.setupMetrics(); err != nil {
			return fmt.Errorf("failed to setup metrics: %w", err)
		}
	}

	// Record start time
	startTime := time.Now()

	// Execute the application
	err := r.app.Execute()

	// Record execution metrics
	if r.app.Metrics() != nil {
		duration := time.Since(startTime)
		r.app.Metrics().Timer("cli.execution.duration").Record(duration)

		if err != nil {
			r.app.Metrics().Counter("cli.execution.errors").Inc()
		} else {
			r.app.Metrics().Counter("cli.execution.success").Inc()
		}
	}

	return err
}

// RunWithContext executes with context for cancellation
func (r *Runner) RunWithContext(ctx context.Context) error {
	// Create a context with timeout if configured
	if r.config.GracefulTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.config.GracefulTimeout)
		defer cancel()
	}

	// Run in a goroutine to handle context cancellation
	done := make(chan error, 1)
	go func() {
		done <- r.Run()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// setupSignalHandling sets up graceful shutdown on signals
func (r *Runner) setupSignalHandling() {
	signal.Notify(r.signals, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-r.signals

		if r.app.Logger() != nil {
			r.app.Logger().Info("received signal, initiating graceful shutdown",
				logger.String("signal", sig.String()),
			)
		}

		// Perform graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), r.config.GracefulTimeout)
		defer cancel()

		if err := r.gracefulShutdown(ctx); err != nil {
			if r.app.Logger() != nil {
				r.app.Logger().Error("graceful shutdown failed", logger.Error(err))
			}
			os.Exit(1)
		}

		os.Exit(0)
	}()
}

// setupProfiling sets up profiling endpoints
func (r *Runner) setupProfiling() error {
	// This would typically set up pprof endpoints
	// For now, we'll just log that profiling is enabled
	if r.app.Logger() != nil {
		r.app.Logger().Info("profiling enabled",
			logger.Int("port", r.config.ProfilePort),
		)
	}
	return nil
}

// setupMetrics sets up metrics collection
func (r *Runner) setupMetrics() error {
	// This would typically set up metrics endpoints
	// For now, we'll just log that metrics are enabled
	if r.app.Logger() != nil {
		r.app.Logger().Info("metrics enabled",
			logger.Int("port", r.config.MetricsPort),
		)
	}
	return nil
}

// gracefulShutdown performs graceful shutdown
func (r *Runner) gracefulShutdown(ctx context.Context) error {
	// Stop the container if it's a service
	if service, ok := r.app.Container().(common.Service); ok {
		return service.Stop(ctx)
	}

	return nil
}

// ExecutionContext provides context for command execution
type ExecutionContext struct {
	context.Context
	App       CLIApp
	StartTime time.Time
	Command   string
	Args      []string
	Env       map[string]string
}

// NewExecutionContext creates a new execution context
func NewExecutionContext(ctx context.Context, app CLIApp, command string, args []string) *ExecutionContext {
	env := make(map[string]string)
	for _, envVar := range os.Environ() {
		// Parse environment variables
		// This is a simplified version
		env[envVar] = os.Getenv(envVar)
	}

	return &ExecutionContext{
		Context:   ctx,
		App:       app,
		StartTime: time.Now(),
		Command:   command,
		Args:      args,
		Env:       env,
	}
}

// Duration returns the execution duration
func (ec *ExecutionContext) Duration() time.Duration {
	return time.Since(ec.StartTime)
}

// CommandExecutor handles command execution with retries and timeouts
type CommandExecutor struct {
	config ExecutorConfig
	runner *Runner
}

// ExecutorConfig contains executor configuration
type ExecutorConfig struct {
	MaxRetries        int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay        time.Duration `yaml:"retry_delay" json:"retry_delay"`
	RetryMultiplier   float64       `yaml:"retry_multiplier" json:"retry_multiplier"`
	MaxRetryDelay     time.Duration `yaml:"max_retry_delay" json:"max_retry_delay"`
	CommandTimeout    time.Duration `yaml:"command_timeout" json:"command_timeout"`
	EnableRetryJitter bool          `yaml:"enable_retry_jitter" json:"enable_retry_jitter"`
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor(runner *Runner, config ExecutorConfig) *CommandExecutor {
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 1 * time.Second
	}
	if config.RetryMultiplier == 0 {
		config.RetryMultiplier = 2.0
	}
	if config.MaxRetryDelay == 0 {
		config.MaxRetryDelay = 30 * time.Second
	}

	return &CommandExecutor{
		config: config,
		runner: runner,
	}
}

// ExecuteWithRetry executes a command with retry logic
func (ce *CommandExecutor) ExecuteWithRetry(ctx context.Context, execFunc func() error) error {
	var lastErr error
	delay := ce.config.RetryDelay

	for attempt := 0; attempt <= ce.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Add jitter if enabled
			actualDelay := delay
			if ce.config.EnableRetryJitter {
				actualDelay = time.Duration(float64(delay) * (0.5 + 0.5*float64(time.Now().UnixNano()%2)))
			}

			select {
			case <-time.After(actualDelay):
			case <-ctx.Done():
				return ctx.Err()
			}

			// Update delay for next attempt
			delay = time.Duration(float64(delay) * ce.config.RetryMultiplier)
			if delay > ce.config.MaxRetryDelay {
				delay = ce.config.MaxRetryDelay
			}
		}

		// Execute with timeout if configured
		if ce.config.CommandTimeout > 0 {
			timeoutCtx, cancel := context.WithTimeout(ctx, ce.config.CommandTimeout)
			err := ce.executeWithTimeout(timeoutCtx, execFunc)
			cancel()

			if err == nil {
				return nil
			}
			lastErr = err
		} else {
			if err := execFunc(); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}

		// Check if we should retry
		if !ce.shouldRetry(lastErr) {
			break
		}
	}

	return fmt.Errorf("command failed after %d attempts: %w", ce.config.MaxRetries+1, lastErr)
}

// executeWithTimeout executes with timeout
func (ce *CommandExecutor) executeWithTimeout(ctx context.Context, execFunc func() error) error {
	done := make(chan error, 1)

	go func() {
		done <- execFunc()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("command timeout: %w", ctx.Err())
	}
}

// shouldRetry determines if an error should trigger a retry
func (ce *CommandExecutor) shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific error types that shouldn't be retried
	switch err.(type) {
	case *common.ForgeError:
		forgeErr := err.(*common.ForgeError)
		switch forgeErr.Code {
		case "INVALID_CONFIG", "VALIDATION_ERROR", "SERVICE_NOT_FOUND":
			return false
		default:
			return true
		}
	default:
		// For unknown errors, assume they are retryable
		return true
	}
}

// PerformanceMonitor monitors command performance
type PerformanceMonitor struct {
	app     CLIApp
	enabled bool
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor(app CLIApp, enabled bool) *PerformanceMonitor {
	return &PerformanceMonitor{
		app:     app,
		enabled: enabled,
	}
}

// Monitor monitors command execution
func (pm *PerformanceMonitor) Monitor(command string, execFunc func() error) error {
	if !pm.enabled {
		return execFunc()
	}

	start := time.Now()
	err := execFunc()
	duration := time.Since(start)

	// Record metrics
	if pm.app.Metrics() != nil {
		pm.app.Metrics().Timer("cli.command.duration", "command", command).Record(duration)

		if err != nil {
			pm.app.Metrics().Counter("cli.command.errors", "command", command).Inc()
		} else {
			pm.app.Metrics().Counter("cli.command.success", "command", command).Inc()
		}
	}

	// Log performance
	if pm.app.Logger() != nil {
		logLevel := pm.app.Logger().Info
		if duration > 10*time.Second {
			logLevel = pm.app.Logger().Warn
		}

		logLevel("command executed",
			logger.String("command", command),
			logger.Duration("duration", duration),
			logger.Bool("success", err == nil),
		)
	}

	return err
}

// DefaultRunnerConfig returns a default runner configuration
func DefaultRunnerConfig() RunnerConfig {
	return RunnerConfig{
		EnableSignalHandling: true,
		GracefulTimeout:      30 * time.Second,
		EnableProfiling:      false,
		ProfilePort:          6060,
		EnableMetrics:        false,
		MetricsPort:          8080,
	}
}

// DefaultExecutorConfig returns a default executor configuration
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		MaxRetries:        3,
		RetryDelay:        1 * time.Second,
		RetryMultiplier:   2.0,
		MaxRetryDelay:     30 * time.Second,
		CommandTimeout:    0, // No timeout by default
		EnableRetryJitter: true,
	}
}
