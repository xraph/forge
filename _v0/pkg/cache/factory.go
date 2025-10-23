package cache

import (
	"fmt"
	"sync"

	"github.com/xraph/forge/v0/pkg/cache/backends/database"
	"github.com/xraph/forge/v0/pkg/cache/backends/hybrid"
	"github.com/xraph/forge/v0/pkg/cache/backends/memory"
	"github.com/xraph/forge/v0/pkg/cache/backends/redis"
	"github.com/xraph/forge/v0/pkg/cache/core"
	"github.com/xraph/forge/v0/pkg/common"
)

// DefaultCacheFactory implements the CacheFactory interface
type DefaultCacheFactory struct {
	factories map[CacheType]core.CacheConstructor
	mu        sync.RWMutex
}

// NewCacheFactory creates a new cache factory
func NewCacheFactory() CacheFactory {
	return &DefaultCacheFactory{
		factories: make(map[CacheType]core.CacheConstructor),
	}
}

// Create creates a cache backend instance
func (f *DefaultCacheFactory) Create(cacheType CacheType, name string, config interface{}, l common.Logger, metrics common.Metrics) (core.CacheBackend, error) {
	f.mu.RLock()
	factory, exists := f.factories[cacheType]
	f.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("cache type %s not registered", cacheType)
	}

	return factory(name, config, l, metrics)
}

// List returns all registered cache types
func (f *DefaultCacheFactory) List() []CacheType {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]CacheType, 0, len(f.factories))
	for cacheType := range f.factories {
		types = append(types, cacheType)
	}

	return types
}

// Register registers a cache factory function
func (f *DefaultCacheFactory) Register(cacheType CacheType, factory core.CacheConstructor) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.factories[cacheType] = factory
}

// Unregister removes a cache factory
func (f *DefaultCacheFactory) Unregister(cacheType CacheType) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.factories, cacheType)
}

// IsRegistered checks if a cache type is registered
func (f *DefaultCacheFactory) IsRegistered(cacheType CacheType) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	_, exists := f.factories[cacheType]
	return exists
}

// NewMemoryCache creates a new memory cache backend
func NewMemoryCache(name string, config interface{}, l common.Logger, metrics common.Metrics) (core.CacheBackend, error) {
	return memory.NewMemoryCache(name, config.(*core.MemoryConfig), l, metrics)
}

// NewRedisCache creates a new Redis cache backend
func NewRedisCache(name string, config interface{}, l common.Logger, metrics common.Metrics) (core.CacheBackend, error) {
	return redis.NewRedisCache(name, config.(*core.RedisConfig), l, metrics)
}

// NewHybridCache creates a new hybrid cache backend
func NewHybridCache(name string, config interface{}, l common.Logger, metrics common.Metrics) (core.CacheBackend, error) {
	return hybrid.NewHybridCache(name, config.(*core.HybridConfig), l, metrics)
}

// NewDatabaseCache creates a new database cache backend
func NewDatabaseCache(name string, config interface{}, l common.Logger, metrics common.Metrics) (core.CacheBackend, error) {
	return database.NewDatabaseCache(*(config.(*database.DatabaseCacheConfig)))
}

// RegisterDefaultFactories registers the default cache factories
func RegisterDefaultFactories(factory CacheFactory) {
	factory.Register(CacheTypeMemory, NewMemoryCache)
	factory.Register(CacheTypeRedis, NewRedisCache)
	factory.Register(CacheTypeHybrid, NewHybridCache)
	factory.Register(CacheTypeDatabase, NewDatabaseCache)
}

// GetDefaultFactory returns a factory with all default backends registered
func GetDefaultFactory() CacheFactory {
	factory := NewCacheFactory()
	RegisterDefaultFactories(factory)
	return factory
}
