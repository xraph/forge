// forge/interfaces.go - Updated to include cache integration

package forge

import (
	"github.com/xraph/forge/pkg/cache"
	"github.com/xraph/forge/pkg/config"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/events"
	"github.com/xraph/forge/pkg/health"
	"github.com/xraph/forge/pkg/metrics"
	"github.com/xraph/forge/pkg/middleware"
	"github.com/xraph/forge/pkg/streaming"
)

// =============================================================================
// INTEGRATION HELPER FUNCTIONS
// =============================================================================

// Forge interface extends core.Application with additional phase methods
type Forge interface {
	// Application represents the primary interface for managing an application's lifecycle, configuration, and services.
	Application

	// DatabaseManager returns the instance of the database.DatabaseManager to manage database connections and adapters.
	DatabaseManager() database.DatabaseManager

	// EventBus returns an instance of the event bus for managing and dispatching events within the application.
	EventBus() events.EventBus

	// MiddlewareManager provides access to the middleware.Manager responsible for managing application middleware components.
	MiddlewareManager() *middleware.Manager

	// StreamingManager provides access to the application's streaming operations and management functionalities.
	StreamingManager() streaming.StreamingManager

	// CacheManager returns the cache manager responsible for managing multiple cache backends and operations.
	CacheManager() *cache.CacheManager

	// CacheService returns the cache service responsible for integrating cache management with the DI container.
	CacheService() *cache.CacheService

	// GetCache returns a cache instance by name, or the default cache if name is empty.
	GetCache(name string) (cache.Cache, error)

	// GetDefaultCache returns the default cache instance.
	GetDefaultCache() (cache.Cache, error)

	// MetricsCollector returns the metrics collector responsible for collecting application-specific metrics.
	MetricsCollector() metrics.MetricsCollector

	// HealthService provides access to the health functionality of the application, allowing health checks and status queries.
	HealthService() health.HealthService

	// EnableMetricsEndpoints enables HTTP endpoints for exposing application metrics and returns an error if the operation fails.
	EnableMetricsEndpoints() error

	// EnableHealthEndpoints enables HTTP endpoints for monitoring the health status of the application and its dependencies.
	EnableHealthEndpoints() error

	// GetStats retrieves runtime statistics related to the application, returning them as a map of string keys to values.
	GetStats() map[string]interface{}
}

type commonConfig struct {
	config        *config.ManagerConfig
	metrics       *metrics.ServiceConfig
	cache         *cache.CacheServiceConfig
	configSources []interface{} // Mix of config.ConfigSource and ConfigSourceCreator
}
