package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// =============================================================================
// Example 1: Background Worker Extension
// =============================================================================

// BackgroundWorkerExtension demonstrates a simple background worker
type BackgroundWorkerExtension struct {
	*forge.BaseExtension
	ticker   *time.Ticker
	done     chan struct{}
	counter  int
	mu       sync.Mutex
	interval time.Duration
}

// NewBackgroundWorkerExtension creates a new background worker extension
func NewBackgroundWorkerExtension(interval time.Duration) *BackgroundWorkerExtension {
	return &BackgroundWorkerExtension{
		BaseExtension: forge.NewBaseExtension(
			"background-worker",
			"1.0.0",
			"Background worker that runs periodic tasks",
		),
		done:     make(chan struct{}),
		interval: interval,
	}
}

// Run starts the background worker
func (e *BackgroundWorkerExtension) Run(ctx context.Context) error {
	e.Logger().Info("starting background worker",
		forge.F("interval", e.interval),
	)

	e.ticker = time.NewTicker(e.interval)

	// Start worker in goroutine
	go e.work()

	return nil
}

// work performs periodic background tasks
func (e *BackgroundWorkerExtension) work() {
	for {
		select {
		case <-e.done:
			e.ticker.Stop()
			e.Logger().Info("background worker stopped")
			return
		case <-e.ticker.C:
			e.mu.Lock()
			e.counter++
			count := e.counter
			e.mu.Unlock()

			e.Logger().Info("background worker tick",
				forge.F("count", count),
			)
		}
	}
}

// Shutdown gracefully stops the background worker
func (e *BackgroundWorkerExtension) Shutdown(ctx context.Context) error {
	e.Logger().Info("stopping background worker")
	close(e.done)
	return nil
}

// GetCounter returns the current counter value (for demonstration)
func (e *BackgroundWorkerExtension) GetCounter() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.counter
}

// =============================================================================
// Example 2: Multi-Process Manager Extension
// =============================================================================

// ProcessManagerExtension manages multiple external processes
type ProcessManagerExtension struct {
	*forge.BaseExtension
	processes []*forge.ExternalAppExtension
}

// NewProcessManagerExtension creates a new process manager
func NewProcessManagerExtension() *ProcessManagerExtension {
	return &ProcessManagerExtension{
		BaseExtension: forge.NewBaseExtension(
			"process-manager",
			"1.0.0",
			"Manages multiple external processes",
		),
		processes: []*forge.ExternalAppExtension{},
	}
}

// Register registers the extension and its managed processes
func (e *ProcessManagerExtension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	e.Logger().Info("configuring managed processes")

	// Configure external processes
	// Note: These are just examples - adjust for your environment

	// Example 1: Long-running sleep process
	sleepConfig := forge.DefaultExternalAppConfig()
	sleepConfig.Name = "sleep-process"
	sleepConfig.Command = "sleep"
	sleepConfig.Args = []string{"300"} // Sleep for 5 minutes
	sleepConfig.ForwardOutput = true

	sleepApp := forge.NewExternalAppExtension(sleepConfig)
	e.processes = append(e.processes, sleepApp)

	// Example 2: Echo loop process
	echoConfig := forge.DefaultExternalAppConfig()
	echoConfig.Name = "echo-loop"
	echoConfig.Command = "bash"
	echoConfig.Args = []string{"-c", "while true; do echo 'Hello from external app'; sleep 5; done"}
	echoConfig.ForwardOutput = true
	echoConfig.RestartOnFailure = true
	echoConfig.RestartDelay = 3 * time.Second

	echoApp := forge.NewExternalAppExtension(echoConfig)
	e.processes = append(e.processes, echoApp)

	return nil
}

// Run starts all managed processes
func (e *ProcessManagerExtension) Run(ctx context.Context) error {
	e.Logger().Info("starting managed processes",
		forge.F("count", len(e.processes)),
	)

	for _, proc := range e.processes {
		if err := proc.Run(ctx); err != nil {
			e.Logger().Error("failed to start process",
				forge.F("name", proc.Name()),
				forge.F("error", err),
			)
			// Continue starting other processes
		}
	}

	return nil
}

// Shutdown stops all managed processes
func (e *ProcessManagerExtension) Shutdown(ctx context.Context) error {
	e.Logger().Info("stopping managed processes",
		forge.F("count", len(e.processes)),
	)

	// Stop processes in reverse order
	for i := len(e.processes) - 1; i >= 0; i-- {
		proc := e.processes[i]
		if err := proc.Shutdown(ctx); err != nil {
			e.Logger().Error("failed to stop process",
				forge.F("name", proc.Name()),
				forge.F("error", err),
			)
			// Continue stopping other processes
		}
	}

	return nil
}

// Health checks if all managed processes are healthy
func (e *ProcessManagerExtension) Health(ctx context.Context) error {
	for _, proc := range e.processes {
		if err := proc.Health(ctx); err != nil {
			return fmt.Errorf("process %s is unhealthy: %w", proc.Name(), err)
		}
	}
	return nil
}

// =============================================================================
// Example 3: Metrics Collector Extension
// =============================================================================

// MetricsCollectorExtension collects and reports metrics periodically
type MetricsCollectorExtension struct {
	*forge.BaseExtension
	done     chan struct{}
	interval time.Duration
	app      forge.App
}

// NewMetricsCollectorExtension creates a new metrics collector
func NewMetricsCollectorExtension(interval time.Duration) *MetricsCollectorExtension {
	return &MetricsCollectorExtension{
		BaseExtension: forge.NewBaseExtension(
			"metrics-collector",
			"1.0.0",
			"Collects and reports system metrics",
		),
		done:     make(chan struct{}),
		interval: interval,
	}
}

// Register stores the app reference
func (e *MetricsCollectorExtension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}
	e.app = app
	return nil
}

// Run starts the metrics collector
func (e *MetricsCollectorExtension) Run(ctx context.Context) error {
	e.Logger().Info("starting metrics collector",
		forge.F("interval", e.interval),
	)

	go e.collect()

	return nil
}

// collect periodically collects and reports metrics
func (e *MetricsCollectorExtension) collect() {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-e.done:
			e.Logger().Info("metrics collector stopped")
			return
		case <-ticker.C:
			// Collect metrics
			e.Logger().Info("collecting metrics",
				forge.F("uptime", e.app.Uptime()),
				forge.F("extensions", len(e.app.Extensions())),
			)
		}
	}
}

// Shutdown stops the metrics collector
func (e *MetricsCollectorExtension) Shutdown(ctx context.Context) error {
	e.Logger().Info("stopping metrics collector")
	close(e.done)
	return nil
}

// =============================================================================
// Main Application
// =============================================================================

func main() {
	fmt.Println("\n" + strings.Repeat("═", 80))
	fmt.Println("  Runnable Extension Example")
	fmt.Println("  Demonstrates RunnableExtension interface for managing")
	fmt.Println("  external apps and background workers")
	fmt.Println(strings.Repeat("═", 80) + "\n")

	// Create Forge app
	config := forge.DefaultAppConfig()
	config.Name = "runnable-extension-demo"
	config.Version = "1.0.0"
	config.Environment = "development"
	config.HTTPAddress = ":8080"

	app := forge.NewApp(config)

	// Register runnable extensions
	// These extensions will automatically have their Run() and Shutdown() methods
	// called during the app lifecycle without manual hook registration

	// 1. Background worker
	workerExt := NewBackgroundWorkerExtension(3 * time.Second)
	config.Extensions = append(config.Extensions, workerExt)

	// 2. Process manager
	processManager := NewProcessManagerExtension()
	config.Extensions = append(config.Extensions, processManager)

	// 3. Metrics collector
	metricsCollector := NewMetricsCollectorExtension(10 * time.Second)
	config.Extensions = append(config.Extensions, metricsCollector)

	// Re-create app with extensions
	app = forge.NewApp(config)

	// Register some demo endpoints
	app.Router().GET("/", func(ctx forge.Context) error {
		return ctx.JSON(200, forge.Map{
			"name":    app.Name(),
			"version": app.Version(),
			"uptime":  app.Uptime().String(),
			"extensions": forge.Map{
				"count": len(app.Extensions()),
			},
		})
	})

	app.Router().GET("/worker/stats", func(ctx forge.Context) error {
		// Get worker extension
		ext, err := app.GetExtension("background-worker")
		if err != nil {
			return ctx.JSON(404, forge.Map{"error": "worker not found"})
		}

		worker := ext.(*BackgroundWorkerExtension)
		return ctx.JSON(200, forge.Map{
			"name":    worker.Name(),
			"counter": worker.GetCounter(),
		})
	})

	app.Router().GET("/health", func(ctx forge.Context) error {
		return ctx.JSON(200, forge.Map{
			"status": "healthy",
		})
	})

	// Run the application
	// All RunnableExtension implementations will automatically:
	// - Have their Run() method called during PhaseAfterRun
	// - Have their Shutdown() method called during PhaseBeforeStop
	// - Be managed by the app's lifecycle system
	//
	// No manual hook registration required!

	fmt.Println("✅ Application configured with runnable extensions")
	fmt.Println("   - Background worker (ticks every 3s)")
	fmt.Println("   - Process manager (manages external processes)")
	fmt.Println("   - Metrics collector (reports every 10s)")
	fmt.Println("\n📡 Server starting on http://localhost:8080")
	fmt.Println("   GET /            - App info")
	fmt.Println("   GET /worker/stats - Worker statistics")
	fmt.Println("   GET /health      - Health check")
	fmt.Println("\n⌨️  Press Ctrl+C to shutdown gracefully")

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}
}
