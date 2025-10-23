package storage

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// Extension implements the storage extension
type Extension struct {
	config  Config
	manager *StorageManager
	logger  forge.Logger
	metrics forge.Metrics
	app     forge.App
}

// NewExtension creates a new storage extension
func NewExtension(config Config) forge.Extension {
	return &Extension{
		config: config,
	}
}

// Name returns the extension name
func (e *Extension) Name() string {
	return "storage"
}

// Version returns the extension version
func (e *Extension) Version() string {
	return "2.0.0"
}

// Description returns the extension description
func (e *Extension) Description() string {
	return "Unified object storage (S3, GCS, Azure, Local)"
}

// Dependencies returns the list of extension dependencies
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// Register registers the extension with the application
func (e *Extension) Register(app forge.App) error {
	e.app = app

	// Get dependencies from DI
	e.logger = forge.Must[forge.Logger](app.Container(), "logger")
	e.metrics = forge.Must[forge.Metrics](app.Container(), "metrics")

	// Get config from ConfigManager (bind pattern)
	configMgr := forge.Must[forge.ConfigManager](app.Container(), "config")
	var tempConfig Config
	if err := configMgr.Bind("extensions.storage", &tempConfig); err == nil {
		e.config = tempConfig
	} else {
		// Use default config if not found
		e.logger.Info("using default storage config")
		e.config = DefaultConfig()
	}

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("invalid storage configuration: %w", err)
	}

	// Create storage manager
	e.manager = NewStorageManager(e.config, e.logger, e.metrics)

	// Register storage manager in DI
	if err := forge.RegisterSingleton(app.Container(), "storage", func(c forge.Container) (*StorageManager, error) {
		return e.manager, nil
	}); err != nil {
		return fmt.Errorf("failed to register storage manager: %w", err)
	}

	e.logger.Info("storage extension registered",
		forge.F("backends", len(e.config.Backends)),
		forge.F("default", e.config.Default),
	)

	return nil
}

// Start starts the extension
func (e *Extension) Start(ctx context.Context) error {
	e.logger.Info("starting storage extension")

	// Initialize storage manager
	if err := e.manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start storage manager: %w", err)
	}

	e.logger.Info("storage extension started")
	return nil
}

// Stop stops the extension
func (e *Extension) Stop(ctx context.Context) error {
	e.logger.Info("stopping storage extension")

	// Stop storage manager
	if err := e.manager.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop storage manager: %w", err)
	}

	e.logger.Info("storage extension stopped")
	return nil
}

// Health checks the extension health
func (e *Extension) Health(ctx context.Context) error {
	return e.manager.Health(ctx)
}
