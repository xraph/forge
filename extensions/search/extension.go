package search

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for search functionality.
// The extension is now a lightweight facade that loads config and registers services.
// Service lifecycle is managed by Vessel, not by the extension.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing search instance - Vessel manages it
}

// NewExtension creates a new search extension with functional options.
// Config is loaded from ConfigManager by default, with options providing overrides.
//
// Example:
//
//	// Load from ConfigManager (tries "extensions.search", then "search")
//	search.NewExtension()
//
//	// Override specific fields
//	search.NewExtension(
//	    search.WithDriver("elasticsearch"),
//	    search.WithURL("http://localhost:9200"),
//	)
//
//	// Require config from ConfigManager
//	search.NewExtension(search.WithRequireConfig(true))
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("search", "2.0.0", "Full-text search with Elasticsearch/Meilisearch/Typesense")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new search extension with a complete config.
// This is for backward compatibility or when config is fully known at initialization.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the search extension with the app.
// This method now only loads configuration and registers service constructors.
// Service lifecycle (Start/Stop) is managed by Vessel.
func (e *Extension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config from ConfigManager with dual-key support
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("search", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("search: failed to load required config: %w", err)
		}

		e.Logger().Warn("search: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Register service constructor with Vessel using vessel.WithAliases for backward compatibility
	// Vessel will manage the service lifecycle (Start/Stop)
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*SearchService, error) {
		return NewSearchService(finalConfig, logger, metrics)
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register search service: %w", err)
	}

	e.Logger().Info("search extension registered",
		forge.F("driver", finalConfig.Driver),
		forge.F("url", finalConfig.URL),
	)

	return nil
}

// Start marks the extension as started.
// The actual search service is started by Vessel calling SearchService.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// The actual search service is stopped by Vessel calling SearchService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// Service health is managed by Vessel through SearchService.Health().
func (e *Extension) Health(ctx context.Context) error {
	// Health is now managed by Vessel through SearchService.Health()
	return nil
}
