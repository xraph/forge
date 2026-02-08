package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements the storage extension.
// The extension is now a lightweight facade that loads config and registers services.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing manager - Vessel manages it
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

	// Register StorageManager constructor with Vessel
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*StorageManager, error) {
		manager := NewStorageManager(finalConfig, logger, metrics)

		// Initialize backends immediately (similar to how database extension works)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := manager.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize storage backends: %w", err)
		}

		return manager, nil
	}, vessel.WithAliases(ManagerKey), vessel.WithEager()); err != nil {
		return fmt.Errorf("failed to register storage manager constructor: %w", err)
	}

	e.Logger().Info("storage extension registered",
		forge.F("backends", len(finalConfig.Backends)),
		forge.F("default", finalConfig.Default),
	)

	return nil
}

// Start marks the extension as started.
// Storage manager is started by Vessel calling StorageManager.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// Storage manager is stopped by Vessel calling StorageManager.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Manager health is managed by Vessel through StorageManager.Health().
func (e *Extension) Health(ctx context.Context) error {
	return nil
}
