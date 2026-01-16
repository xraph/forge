package cache

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// Extension implements forge.Extension for cache functionality.
// The extension is now a lightweight facade that loads config and registers services.
// Service lifecycle is managed by Vessel, not by the extension.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing cache instance - Vessel manages it
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
// This method now only loads configuration and registers service constructors.
// Service lifecycle (Start/Stop) is managed by Vessel.
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

	// Register service constructor with Vessel using vessel.WithAliases for backward compatibility
	// Vessel will manage the service lifecycle (Start/Stop)
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*CacheService, error) {
		return NewCacheService(finalConfig, logger, metrics)
	}, vessel.WithAliases(ServiceKey)); err != nil {
		return fmt.Errorf("failed to register cache service: %w", err)
	}

	e.Logger().Info("cache extension registered",
		forge.F("driver", finalConfig.Driver),
		forge.F("default_ttl", finalConfig.DefaultTTL),
	)

	return nil
}

// Start marks the extension as started.
// The actual cache service is started by Vessel calling CacheService.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// The actual cache service is stopped by Vessel calling CacheService.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks if the cache is healthy.
// This delegates to the CacheService health check managed by Vessel.
func (e *Extension) Health(ctx context.Context) error {
	// Health is now managed by Vessel through CacheService.Health()
	// Extension health check is optional and can aggregate service health if needed
	return nil
}
