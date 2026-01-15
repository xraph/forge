package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// CacheService wraps a Cache implementation and provides lifecycle management.
// It implements vessel's di.Service interface so Vessel can manage its lifecycle.
type CacheService struct {
	config  Config
	cache   Cache
	logger  forge.Logger
	metrics forge.Metrics
}

// NewCacheService creates a new cache service with the given configuration.
// This is the constructor that will be registered with the DI container.
func NewCacheService(config Config, logger forge.Logger, metrics forge.Metrics) (*CacheService, error) {
	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid cache config: %w", err)
	}

	// Create cache backend based on driver
	var cache Cache

	switch config.Driver {
	case "inmemory":
		cache = NewInMemoryCache(config, logger, metrics)

	case "redis":
		// TODO: Implement Redis backend
		return nil, errors.New("redis driver not yet implemented")

	case "memcached":
		// TODO: Implement Memcached backend
		return nil, errors.New("memcached driver not yet implemented")

	default:
		return nil, fmt.Errorf("unknown cache driver: %s", config.Driver)
	}

	return &CacheService{
		config:  config,
		cache:   cache,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Name returns the service name for Vessel's lifecycle management.
func (s *CacheService) Name() string {
	return "cache-service"
}

// Start starts the cache service by connecting to the backend.
// This is called automatically by Vessel during container.Start().
func (s *CacheService) Start(ctx context.Context) error {
	s.logger.Info("starting cache service",
		forge.F("driver", s.config.Driver),
	)

	if err := s.cache.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to cache: %w", err)
	}

	s.logger.Info("cache service started",
		forge.F("driver", s.config.Driver),
		forge.F("default_ttl", s.config.DefaultTTL),
	)

	return nil
}

// Stop stops the cache service by disconnecting from the backend.
// This is called automatically by Vessel during container.Stop().
func (s *CacheService) Stop(ctx context.Context) error {
	s.logger.Info("stopping cache service")

	if s.cache != nil {
		if err := s.cache.Disconnect(ctx); err != nil {
			s.logger.Error("failed to disconnect cache",
				forge.F("error", err),
			)
			// Don't return error, log and continue
		}
	}

	s.logger.Info("cache service stopped")
	return nil
}

// Health checks if the cache service is healthy.
// This is called by the health check system.
func (s *CacheService) Health(ctx context.Context) error {
	if s.cache == nil {
		return errors.New("cache not initialized")
	}

	if err := s.cache.Ping(ctx); err != nil {
		return fmt.Errorf("cache health check failed: %w", err)
	}

	return nil
}

// Cache returns the underlying cache implementation.
// This allows services to use the cache directly.
func (s *CacheService) Cache() Cache {
	return s.cache
}

// Ensure CacheService implements Cache interface for convenience
// This allows CacheService to be used directly as a Cache

func (s *CacheService) Connect(ctx context.Context) error {
	return s.cache.Connect(ctx)
}

func (s *CacheService) Disconnect(ctx context.Context) error {
	return s.cache.Disconnect(ctx)
}

func (s *CacheService) Ping(ctx context.Context) error {
	return s.cache.Ping(ctx)
}

func (s *CacheService) Get(ctx context.Context, key string) ([]byte, error) {
	return s.cache.Get(ctx, key)
}

func (s *CacheService) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.cache.Set(ctx, key, value, ttl)
}

func (s *CacheService) Delete(ctx context.Context, key string) error {
	return s.cache.Delete(ctx, key)
}

func (s *CacheService) Exists(ctx context.Context, key string) (bool, error) {
	return s.cache.Exists(ctx, key)
}

func (s *CacheService) Clear(ctx context.Context) error {
	return s.cache.Clear(ctx)
}

func (s *CacheService) Keys(ctx context.Context, pattern string) ([]string, error) {
	return s.cache.Keys(ctx, pattern)
}

func (s *CacheService) TTL(ctx context.Context, key string) (time.Duration, error) {
	return s.cache.TTL(ctx, key)
}

func (s *CacheService) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return s.cache.Expire(ctx, key, ttl)
}
