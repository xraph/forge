package auth

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for authentication and authorization.
// It provides a registry for managing auth providers and automatically
// integrates with the router for OpenAPI security scheme generation.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing registry - Vessel manages it
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
// This method now only loads configuration and registers service constructors.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	if !e.config.Enabled {
		e.Logger().Info("auth extension disabled")
		return nil
	}

	// Register Registry constructor with Vessel using vessel.WithAliases for backward compatibility
	if err := e.RegisterConstructor(func(container forge.Container, logger forge.Logger) (Registry, error) {
		return NewRegistry(container, logger), nil
	}, vessel.WithAliases(RegistryKey, RegistryKeyLegacy)); err != nil {
		return fmt.Errorf("failed to register auth registry: %w", err)
	}

	e.Logger().Info("auth extension registered")

	return nil
}

// Start marks the extension as started.
func (e *Extension) Start(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.MarkStarted()
	e.Logger().Info("auth extension started")

	return nil
}

// Stop marks the extension as stopped.
func (e *Extension) Stop(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	e.MarkStopped()
	e.Logger().Info("auth extension stopped")

	return nil
}

// Health checks the extension health.
func (e *Extension) Health(ctx context.Context) error {
	if !e.config.Enabled {
		return nil
	}

	// Registry health is managed by Vessel
	return nil
}
