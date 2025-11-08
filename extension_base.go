package forge

import (
	"context"
	"sync"
)

// BaseExtension provides common functionality for implementing extensions.
// Extensions can embed BaseExtension to get standard implementations of common methods.
//
// Example usage:
//
//	type MyExtension struct {
//	    *forge.BaseExtension
//	    config MyConfig
//	    client *MyClient
//	}
//
//	func NewMyExtension(config MyConfig) forge.Extension {
//	    return &MyExtension{
//	        BaseExtension: forge.NewBaseExtension("my-ext", "1.0.0", "My extension"),
//	        config:        config,
//	    }
//	}
type BaseExtension struct {
	name         string
	version      string
	description  string
	dependencies []string
	logger       Logger
	metrics      Metrics
	app          App
	started      bool
	mu           sync.RWMutex
}

// NewBaseExtension creates a new base extension with the given identity.
func NewBaseExtension(name, version, description string) *BaseExtension {
	return &BaseExtension{
		name:         name,
		version:      version,
		description:  description,
		dependencies: []string{},
	}
}

// Name returns the extension name.
func (e *BaseExtension) Name() string {
	return e.name
}

// Version returns the extension version.
func (e *BaseExtension) Version() string {
	return e.version
}

// Description returns the extension description.
func (e *BaseExtension) Description() string {
	return e.description
}

// Dependencies returns the extension dependencies.
func (e *BaseExtension) Dependencies() []string {
	return e.dependencies
}

// SetDependencies sets the extension dependencies.
func (e *BaseExtension) SetDependencies(deps []string) {
	e.dependencies = deps
}

// SetLogger sets the logger for this extension.
func (e *BaseExtension) SetLogger(logger Logger) {
	e.logger = logger
}

// Logger returns the extension's logger.
func (e *BaseExtension) Logger() Logger {
	return e.logger
}

// SetMetrics sets the metrics for this extension.
func (e *BaseExtension) SetMetrics(metrics Metrics) {
	e.metrics = metrics
}

// Metrics returns the extension's metrics.
func (e *BaseExtension) Metrics() Metrics {
	return e.metrics
}

// IsStarted returns true if the extension has been started.
func (e *BaseExtension) IsStarted() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.started
}

// MarkStarted marks the extension as started.
func (e *BaseExtension) MarkStarted() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.started = true
}

// MarkStopped marks the extension as stopped.
func (e *BaseExtension) MarkStopped() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.started = false
}

// Register is a default implementation that does nothing.
// Extensions should override this to register their services.
func (e *BaseExtension) Register(app App) error {
	// Default: store app reference, logger and metrics from app
	e.app = app
	e.logger = app.Logger()
	e.metrics = app.Metrics()

	return nil
}

// Start is a default implementation that does nothing.
// Extensions should override this to start their services.
func (e *BaseExtension) Start(ctx context.Context) error {
	e.MarkStarted()

	return nil
}

// Stop is a default implementation that does nothing.
// Extensions should override this to stop their services.
func (e *BaseExtension) Stop(ctx context.Context) error {
	e.MarkStopped()

	return nil
}

// Health is a default implementation that always returns healthy.
// Extensions should override this to implement actual health checks.
func (e *BaseExtension) Health(ctx context.Context) error {
	return nil
}

// LoadConfig loads configuration for this extension from ConfigManager.
//
// It tries the following keys in order:
//  1. "extensions.{key}" - Namespaced pattern (preferred)
//  2. "{key}" - Top-level pattern (legacy/v1 compatibility)
//
// Parameters:
//   - key: The config key (e.g., "cache", "mcp")
//   - target: Pointer to config struct to populate
//   - programmaticConfig: Config provided programmatically (may be partially filled)
//   - defaults: Default config to use if nothing found
//   - requireConfig: If true, returns error when config not found; if false, uses defaults
//
// Example:
//
//	func (e *Extension) Register(app forge.App) error {
//	    if err := e.BaseExtension.Register(app); err != nil {
//	        return err
//	    }
//
//	    // Load config from ConfigManager
//	    finalConfig := DefaultConfig()
//	    if err := e.LoadConfig("cache", &finalConfig, e.config, DefaultConfig(), false); err != nil {
//	        return err
//	    }
//	    e.config = finalConfig
//
//	    // ... rest of registration
//	}
func (e *BaseExtension) LoadConfig(
	key string,
	target any,
	programmaticConfig any,
	defaults any,
	requireConfig bool,
) error {
	if e.app == nil {
		if requireConfig {
			return ErrExtensionNotRegistered
		}
		// No app available, use programmatic or defaults
		loader := NewExtensionConfigLoader(nil, e.logger)

		return loader.LoadConfig(key, target, programmaticConfig, defaults, false)
	}

	loader := NewExtensionConfigLoader(e.app, e.logger)

	return loader.LoadConfig(key, target, programmaticConfig, defaults, requireConfig)
}

// App returns the app instance this extension is registered with.
func (e *BaseExtension) App() App {
	return e.app
}
