package forge

import (
	"context"

	"github.com/xraph/forge/v2/internal/shared"
)

// Extension represents an official Forge extension that can be registered with an App.
// Extensions have full access to the framework and are first-party, trusted components.
//
// Extensions follow a standard lifecycle:
//  1. Register(app) - Register services with DI container
//  2. Start(ctx) - Start the extension
//  3. Health(ctx) - Check extension health (called periodically)
//  4. Stop(ctx) - Stop the extension (called during graceful shutdown)
type Extension interface {
	// Name returns the unique name of the extension
	Name() string

	// Version returns the semantic version of the extension
	Version() string

	// Description returns a human-readable description
	Description() string

	// Register registers the extension's services with the DI container.
	// This is called before Start(), allowing the extension to:
	//  - Register services with the DI container
	//  - Access core services (logger, metrics, config)
	//  - Set up internal state
	Register(app App) error

	// Start starts the extension.
	// This is called after all extensions have been registered and the DI container has started.
	Start(ctx context.Context) error

	// Stop stops the extension gracefully.
	// Extensions are stopped in reverse dependency order.
	Stop(ctx context.Context) error

	// Health checks if the extension is healthy.
	// This is called periodically by the health check system.
	// Return nil if healthy, error otherwise.
	Health(ctx context.Context) error

	// Dependencies returns the names of extensions this extension depends on.
	// The app will ensure dependencies are started before this extension.
	Dependencies() []string
}

// ConfigurableExtension is an optional interface for extensions that support configuration.
type ConfigurableExtension interface {
	Extension
	// Configure configures the extension with the provided config object
	Configure(config any) error
}

// ObservableExtension is an optional interface for extensions that provide metrics.
type ObservableExtension interface {
	Extension
	// Metrics returns a map of metric names to values
	Metrics() map[string]any
}

// HotReloadableExtension is an optional interface for extensions that support hot reload.
type HotReloadableExtension interface {
	Extension
	// Reload reloads the extension's configuration or state without restarting
	Reload(ctx context.Context) error
}

// ExtensionInfo contains information about a registered extension
type ExtensionInfo = shared.ExtensionInfo
