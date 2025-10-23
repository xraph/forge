package auth

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Extension implements forge.Extension for authentication and authorization.
// It provides a registry for managing auth providers and automatically
// integrates with the router for OpenAPI security scheme generation.
type Extension struct {
	*forge.BaseExtension
	config   Config
	registry Registry
	app      forge.App
}

// NewExtension creates a new auth extension with functional options.
//
// Example:
//
//	auth.NewExtension(
//	    auth.WithEnabled(true),
//	    auth.WithDefaultProvider("jwt"),
//	)
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("auth", "2.0.0", "Authentication & Authorization")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new auth extension with a complete config.
// This is for backward compatibility or when config is fully known at initialization.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the auth extension with the app.
// It creates and registers the auth provider registry as a singleton service.
func (e *Extension) Register(app forge.App) error {
	e.app = app

	if !e.config.Enabled {
		app.Logger().Info("auth extension disabled")
		return nil
	}

	// Get dependencies from app
	logger := app.Logger()
	container := app.Container()

	// Create registry
	e.registry = NewRegistry(container, logger)

	// Register registry as a singleton service
	if err := container.Register("auth:registry", func(c forge.Container) (interface{}, error) {
		return e.registry, nil
	}); err != nil {
		return fmt.Errorf("failed to register auth registry: %w", err)
	}

	// Register the registry under "auth.Registry" for easier access
	if err := container.Register("auth.Registry", func(c forge.Container) (interface{}, error) {
		return e.registry, nil
	}); err != nil {
		return fmt.Errorf("failed to register auth.Registry: %w", err)
	}

	logger.Info("auth extension registered")

	return nil
}

// Start starts the auth extension.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.app.Logger().Info("auth extension started")
	return nil
}

// Stop stops the auth extension gracefully.
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.app.Logger().Info("auth extension stopped")
	return nil
}

// Health checks the auth extension health.
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Check if registry is accessible
	if e.registry == nil {
		return fmt.Errorf("auth registry is nil")
	}

	return nil
}

// Registry returns the auth provider registry.
// This allows external code to register custom auth providers.
func (e *Extension) Registry() Registry {
	return e.registry
}
