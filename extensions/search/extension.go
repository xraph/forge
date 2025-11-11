package search

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// Extension implements forge.Extension for search functionality.
type Extension struct {
	*forge.BaseExtension

	config Config
	search Search
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

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("search config validation failed: %w", err)
	}

	// Create search instance based on driver
	var (
		search Search
		err    error
	)

	switch e.config.Driver {
	case "inmemory":
		search = NewInMemorySearch(e.config, e.Logger(), e.Metrics())

	case "elasticsearch":
		search, err = NewElasticsearchSearch(e.config, e.Logger(), e.Metrics())
		if err != nil {
			return fmt.Errorf("failed to create elasticsearch search: %w", err)
		}

	case "meilisearch":
		search, err = NewMeilisearchSearch(e.config, e.Logger(), e.Metrics())
		if err != nil {
			return fmt.Errorf("failed to create meilisearch search: %w", err)
		}

	case "typesense":
		search, err = NewTypesenseSearch(e.config, e.Logger(), e.Metrics())
		if err != nil {
			return fmt.Errorf("failed to create typesense search: %w", err)
		}

	default:
		return fmt.Errorf("unknown search driver: %s", e.config.Driver)
	}

	e.search = search

	// Register search with DI container
	if err := forge.RegisterSingleton(app.Container(), "search", func(c forge.Container) (Search, error) {
		return e.search, nil
	}); err != nil {
		return fmt.Errorf("failed to register search service: %w", err)
	}

	// Also register the specific search implementation for type safety
	switch search := search.(type) {
	case *InMemorySearch:
		_ = forge.RegisterSingleton(app.Container(), "search:inmemory", func(c forge.Container) (*InMemorySearch, error) {
			return search, nil
		})
	case *ElasticsearchSearch:
		_ = forge.RegisterSingleton(app.Container(), "search:elasticsearch", func(c forge.Container) (*ElasticsearchSearch, error) {
			return search, nil
		})
	case *MeilisearchSearch:
		_ = forge.RegisterSingleton(app.Container(), "search:meilisearch", func(c forge.Container) (*MeilisearchSearch, error) {
			return search, nil
		})
	case *TypesenseSearch:
		_ = forge.RegisterSingleton(app.Container(), "search:typesense", func(c forge.Container) (*TypesenseSearch, error) {
			return search, nil
		})
	}

	e.Logger().Info("search extension registered",
		forge.F("driver", e.config.Driver),
		forge.F("url", e.config.URL),
	)

	return nil
}

// Start starts the search extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting search extension",
		forge.F("driver", e.config.Driver),
	)

	if err := e.search.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to search: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("search extension started")

	return nil
}

// Stop stops the search extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping search extension")

	if e.search != nil {
		if err := e.search.Disconnect(ctx); err != nil {
			e.Logger().Error("failed to disconnect search",
				forge.F("error", err),
			)
		}
	}

	e.MarkStopped()
	e.Logger().Info("search extension stopped")

	return nil
}

// Health checks if the search is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.search == nil {
		return errors.New("search not initialized")
	}

	if err := e.search.Ping(ctx); err != nil {
		return fmt.Errorf("search health check failed: %w", err)
	}

	return nil
}

// Search returns the search instance (for advanced usage).
func (e *Extension) Search() Search {
	return e.search
}
