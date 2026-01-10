package storage

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Extension implements the storage extension.
type Extension struct {
	*forge.BaseExtension

	config  Config
	manager *StorageManager
}

// NewExtension creates a new storage extension with variadic options.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("storage", "2.0.0", "Unified object storage (S3, GCS, Azure, Local)")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new storage extension with a complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the application.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("extensions.storage", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("storage: failed to load required config: %w", err)
		}

		e.Logger().Warn("storage: using default/programmatic config", forge.F("error", err.Error()))
	}

	e.config = finalConfig

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("invalid storage configuration: %w", err)
	}

	// Create storage manager
	e.manager = NewStorageManager(e.config, e.Logger(), e.Metrics())

	// Register storage manager in DI
	if err := forge.RegisterSingleton(app.Container(), ManagerKey, func(c forge.Container) (*StorageManager, error) {
		return e.manager, nil
	}); err != nil {
		return fmt.Errorf("failed to register storage manager: %w", err)
	}

	// Register default storage backend
	if e.config.Default != "" {
		// Resolve ManagerKey from container first to ensure StorageManager is started
		if err := forge.RegisterSingleton(app.Container(), StorageKey, func(c forge.Container) (Storage, error) {
			// Resolve manager from container to trigger auto-start
			manager, err := forge.Resolve[*StorageManager](c, ManagerKey)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve storage manager: %w", err)
			}
			backend := manager.Backend(e.config.Default)
			if backend == nil {
				return nil, fmt.Errorf("default backend %s not found", e.config.Default)
			}

			return backend, nil
		}); err != nil {
			return fmt.Errorf("failed to register default storage: %w", err)
		}
	}

	e.Logger().Info("storage extension registered",
		forge.F("backends", len(e.config.Backends)),
		forge.F("default", e.config.Default),
	)

	return nil
}

// Start starts the extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting storage extension")

	// Initialize storage manager
	if err := e.manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start storage manager: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("storage extension started")

	return nil
}

// Stop stops the extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping storage extension")

	// Stop storage manager
	if err := e.manager.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop storage manager: %w", err)
	}

	e.MarkStopped()
	e.Logger().Info("storage extension stopped")

	return nil
}

// Health checks the extension health.
func (e *Extension) Health(ctx context.Context) error {
	// Check all backends
	statuses := e.manager.HealthCheckAll(ctx)

	unhealthy := 0

	for name, status := range statuses {
		if !status.Healthy {
			unhealthy++

			e.Logger().Warn("storage backend unhealthy", forge.F("name", name), forge.F("error", status.Error))
		}
	}

	if unhealthy > 0 {
		return fmt.Errorf("%d storage backends unhealthy", unhealthy)
	}

	return nil
}
