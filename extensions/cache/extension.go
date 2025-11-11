package cache

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// Extension implements forge.Extension for cache functionality.
type Extension struct {
	*forge.BaseExtension

	config Config
	cache  Cache
}

// NewExtension creates a new cache extension with functional options.
// Config is loaded from ConfigManager by default, with options providing overrides.
//
// Example:
//
//	// Load from ConfigManager (tries "extensions.cache", then "cache")
//	cache.NewExtension()
//
//	// Override specific fields
//	cache.NewExtension(
//	    cache.WithDriver("redis"),
//	    cache.WithURL("redis://localhost:6379"),
//	)
//
//	// Require config from ConfigManager
//	cache.NewExtension(cache.WithRequireConfig(true))
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("cache", "2.0.0", "Caching with Redis/Memcached/In-Memory")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new cache extension with a complete config.
// This is for backward compatibility or when config is fully known at initialization.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the cache extension with the app.
func (e *Extension) Register(app forge.App) error {
	// Call base registration (sets logger, metrics)
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	// Load config from ConfigManager with dual-key support
	// Tries "extensions.cache", then "cache", with programmatic config overrides
	programmaticConfig := e.config

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("cache", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("cache: failed to load required config: %w", err)
		}

		e.Logger().Warn("cache: using default/programmatic config",
			forge.F("error", err.Error()),
		)
	}

	e.config = finalConfig

	// Validate config
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("cache config validation failed: %w", err)
	}

	// Create cache instance based on driver
	var (
		cache Cache
		err   error
	)

	switch e.config.Driver {
	case "inmemory":
		cache = NewInMemoryCache(e.config, e.Logger(), e.Metrics())

	case "redis":
		// TODO: Implement Redis backend
		return errors.New("redis driver not yet implemented")

	case "memcached":
		// TODO: Implement Memcached backend
		return errors.New("memcached driver not yet implemented")

	default:
		return fmt.Errorf("unknown cache driver: %s", e.config.Driver)
	}

	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	e.cache = cache

	// Register cache with DI container
	if err := forge.RegisterSingleton(app.Container(), "cache", func(c forge.Container) (Cache, error) {
		return e.cache, nil
	}); err != nil {
		return fmt.Errorf("failed to register cache service: %w", err)
	}

	// Also register the specific cache implementation for type safety
	switch cache := cache.(type) {
	case *InMemoryCache:
		_ = forge.RegisterSingleton(app.Container(), "cache:inmemory", func(c forge.Container) (*InMemoryCache, error) {
			return cache, nil
		})
	}

	e.Logger().Info("cache extension registered",
		forge.F("driver", e.config.Driver),
		forge.F("default_ttl", e.config.DefaultTTL),
	)

	return nil
}

// Start starts the cache extension.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting cache extension",
		forge.F("driver", e.config.Driver),
	)

	if err := e.cache.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to cache: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("cache extension started")

	return nil
}

// Stop stops the cache extension.
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping cache extension")

	if e.cache != nil {
		if err := e.cache.Disconnect(ctx); err != nil {
			e.Logger().Error("failed to disconnect cache",
				forge.F("error", err),
			)
		}
	}

	e.MarkStopped()
	e.Logger().Info("cache extension stopped")

	return nil
}

// Health checks if the cache is healthy.
func (e *Extension) Health(ctx context.Context) error {
	if e.cache == nil {
		return errors.New("cache not initialized")
	}

	if err := e.cache.Ping(ctx); err != nil {
		return fmt.Errorf("cache health check failed: %w", err)
	}

	return nil
}

// Cache returns the cache instance (for advanced usage).
func (e *Extension) Cache() Cache {
	return e.cache
}
