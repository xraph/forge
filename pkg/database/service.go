package database

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// DatabaseService wraps the DatabaseManager for dependency injection
type DatabaseService struct {
	manager DatabaseManager
}

// NewDatabaseService creates a new database service
func NewDatabaseService(logger common.Logger, metrics common.Metrics, configManager common.ConfigManager) common.Service {
	manager := NewManager(logger, metrics, configManager)
	return &DatabaseService{
		manager: manager,
	}
}

// Name returns the service name
func (ds *DatabaseService) Name() string {
	return "database-service"
}

// Dependencies returns service dependencies
func (ds *DatabaseService) Dependencies() []string {
	return []string{"logger", "metrics", "config-manager"}
}

// OnStart starts the database service
func (ds *DatabaseService) OnStart(ctx context.Context) error {
	return ds.manager.OnStart(ctx)
}

// OnStop stops the database service
func (ds *DatabaseService) OnStop(ctx context.Context) error {
	return ds.manager.OnStop(ctx)
}

// OnHealthCheck performs a health check
func (ds *DatabaseService) OnHealthCheck(ctx context.Context) error {
	return ds.manager.OnHealthCheck(ctx)
}

// Manager returns the underlying database manager
func (ds *DatabaseService) Manager() DatabaseManager {
	return ds.manager
}

// GetConnection returns a connection by name
func (ds *DatabaseService) GetConnection(name string) (Connection, error) {
	return ds.manager.GetConnection(name)
}

// GetDefaultConnection returns the default connection
func (ds *DatabaseService) GetDefaultConnection() (Connection, error) {
	return ds.manager.GetDefaultConnection()
}

// RegisterDatabaseServices registers database services with the DI container
func RegisterDatabaseServices(container common.Container) error {
	// Register the database service
	if err := container.Register(common.ServiceDefinition{
		Name:         "database-service",
		Type:         (*common.Service)(nil),
		Constructor:  NewDatabaseService,
		Singleton:    true,
		Dependencies: []string{"logger", "metrics", "config-manager"},
	}); err != nil {
		return err
	}

	// Register the database manager interface
	if err := container.Register(common.ServiceDefinition{
		Name: "database-manager",
		Type: (*DatabaseManager)(nil),
		Constructor: func(service common.Service) DatabaseManager {
			if dbService, ok := service.(*DatabaseService); ok {
				return dbService.Manager()
			}
			return nil
		},
		Singleton:    true,
		Dependencies: []string{"database-service"},
	}); err != nil {
		return err
	}

	return nil
}

// DatabaseConnectionProvider provides database connections for dependency injection
type DatabaseConnectionProvider struct {
	manager        DatabaseManager
	connectionName string
}

// NewDatabaseConnectionProvider creates a new connection provider
func NewDatabaseConnectionProvider(manager DatabaseManager, connectionName string) *DatabaseConnectionProvider {
	return &DatabaseConnectionProvider{
		manager:        manager,
		connectionName: connectionName,
	}
}

// GetConnection returns the database connection
func (dcp *DatabaseConnectionProvider) GetConnection() (Connection, error) {
	if dcp.connectionName == "" {
		return dcp.manager.GetDefaultConnection()
	}
	return dcp.manager.GetConnection(dcp.connectionName)
}

// RegisterConnectionProvider registers a connection provider with specific name
func RegisterConnectionProvider(container common.Container, connectionName string) error {
	serviceName := "database-connection"
	if connectionName != "" {
		serviceName = "database-connection-" + connectionName
	}

	return container.Register(common.ServiceDefinition{
		Name: serviceName,
		Type: (*Connection)(nil),
		Constructor: func(manager DatabaseManager) (Connection, error) {
			if connectionName == "" {
				return manager.GetDefaultConnection()
			}
			return manager.GetConnection(connectionName)
		},
		Singleton:    true,
		Dependencies: []string{"database-manager"},
	})
}

// DatabaseHealthService provides health checking for database connections
type DatabaseHealthService struct {
	manager DatabaseManager
}

// NewDatabaseHealthService creates a new database health service
func NewDatabaseHealthService(manager DatabaseManager) *DatabaseHealthService {
	return &DatabaseHealthService{
		manager: manager,
	}
}

// Check performs a health check on all database connections
func (dhs *DatabaseHealthService) Check(ctx context.Context) common.HealthResult {
	dhc := NewDatabaseHealthCheck(dhs.manager)
	result := dhc.Check(ctx)

	// Convert pointer to value for backward compatibility
	if result != nil {
		return *result
	}

	return common.HealthResult{
		Status:    common.HealthStatusUnhealthy,
		Message:   "Health check returned nil result",
		Timestamp: time.Now(),
		Duration:  0,
	}
}

// RegisterDatabaseHealthService registers database health service with the health checker
func RegisterDatabaseHealthService(container common.Container, healthChecker common.HealthChecker) error {
	if healthChecker == nil {
		return nil // No health checker available, skip registration
	}

	// Try to get the database manager from the container
	managerInterface, err := container.ResolveNamed("database-manager")
	if err != nil {
		// If we can't resolve the manager now, register a lazy health check
		lazyHealthCheck := &LazyDatabaseHealthCheck{
			container: container,
			name:      "database",
			timeout:   10 * time.Second,
			critical:  true,
		}
		return healthChecker.RegisterCheck("database", lazyHealthCheck)
	}

	// Cast to DatabaseManager
	manager, ok := managerInterface.(DatabaseManager)
	if !ok {
		// Register a health check that reports the type error
		errorHealthCheck := &ErrorDatabaseHealthCheck{
			name:    "database",
			message: "Invalid database manager type",
		}
		return healthChecker.RegisterCheck("database", errorHealthCheck)
	}

	// Create and register the actual health check
	healthCheck := NewDatabaseHealthCheck(manager)
	return healthChecker.RegisterCheck("database", healthCheck)
}

// DatabaseMetricsService provides metrics for database connections
type DatabaseMetricsService struct {
	manager DatabaseManager
	metrics common.Metrics
}

// NewDatabaseMetricsService creates a new database metrics service
func NewDatabaseMetricsService(manager DatabaseManager, metrics common.Metrics) *DatabaseMetricsService {
	return &DatabaseMetricsService{
		manager: manager,
		metrics: metrics,
	}
}

// CollectMetrics collects and reports database metrics
func (dms *DatabaseMetricsService) CollectMetrics() {
	stats := dms.manager.GetStats()

	// Report manager-level metrics
	if dms.metrics != nil {
		dms.metrics.Gauge("forge.database.total_connections").Set(float64(stats.TotalConnections))
		dms.metrics.Gauge("forge.database.active_connections").Set(float64(stats.ActiveConnections))
		dms.metrics.Gauge("forge.database.registered_adapters").Set(float64(stats.RegisteredAdapters))

		// Report connection-specific metrics
		for name, connStats := range stats.ConnectionStats {
			tags := []string{"connection", name, "type", connStats.Type}

			dms.metrics.Gauge("forge.database.connection_connected", tags...).Set(boolToFloat64(connStats.Connected))
			dms.metrics.Counter("forge.database.connection_transactions", tags...).Add(float64(connStats.TransactionCount))
			dms.metrics.Counter("forge.database.connection_queries", tags...).Add(float64(connStats.QueryCount))
			dms.metrics.Counter("forge.database.connection_errors", tags...).Add(float64(connStats.ErrorCount))

			if connStats.Connected {
				dms.metrics.Histogram("forge.database.connection_ping_duration", tags...).Observe(connStats.PingDuration.Seconds())
			}

			// Pool metrics
			dms.metrics.Gauge("forge.database.connection_open_connections", tags...).Set(float64(connStats.OpenConnections))
			dms.metrics.Gauge("forge.database.connection_idle_connections", tags...).Set(float64(connStats.IdleConnections))
			dms.metrics.Gauge("forge.database.connection_max_open_connections", tags...).Set(float64(connStats.MaxOpenConns))
			dms.metrics.Gauge("forge.database.connection_max_idle_connections", tags...).Set(float64(connStats.MaxIdleConns))
		}
	}
}

// StartMetricsCollection starts collecting metrics at regular intervals
func (dms *DatabaseMetricsService) StartMetricsCollection(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Collect initial metrics
	dms.CollectMetrics()

	for {
		select {
		case <-ticker.C:
			dms.CollectMetrics()
		case <-ctx.Done():
			return
		}
	}
}

// RegisterDatabaseMetricsService registers database metrics collection
func RegisterDatabaseMetricsService(container common.Container) error {
	return container.Register(common.ServiceDefinition{
		Name: "database-metrics-service",
		Type: (*DatabaseMetricsService)(nil),
		Constructor: func(manager DatabaseManager, metrics common.Metrics) *DatabaseMetricsService {
			return NewDatabaseMetricsService(manager, metrics)
		},
		Singleton:    true,
		Dependencies: []string{"database-manager", "metrics"},
	})
}

// Helper function to convert bool to float64 for metrics
func boolToFloat64(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}

// DatabaseServiceOptions provides options for configuring database service
type DatabaseServiceOptions struct {
	EnableMetrics   bool
	EnableHealth    bool
	MetricsInterval time.Duration
	ConnectionNames []string // Specific connections to register as services
}

// DefaultDatabaseServiceOptions returns default options
func DefaultDatabaseServiceOptions() *DatabaseServiceOptions {
	return &DatabaseServiceOptions{
		EnableMetrics:   true,
		EnableHealth:    true,
		MetricsInterval: 15 * time.Second,
		ConnectionNames: []string{}, // Empty means only default connection
	}
}

// RegisterDatabaseServicesWithOptions registers database services with custom options
func RegisterDatabaseServicesWithOptions(container common.Container, healthChecker common.HealthChecker, options *DatabaseServiceOptions) error {
	if options == nil {
		options = DefaultDatabaseServiceOptions()
	}

	// Register core database services
	if err := RegisterDatabaseServices(container); err != nil {
		return err
	}

	// Register connection providers for specific connections
	for _, connectionName := range options.ConnectionNames {
		if err := RegisterConnectionProvider(container, connectionName); err != nil {
			return err
		}
	}

	// Register default connection provider if no specific connections are configured
	if len(options.ConnectionNames) == 0 {
		if err := RegisterConnectionProvider(container, ""); err != nil {
			return err
		}
	}

	// Register health service
	if options.EnableHealth {
		if err := RegisterDatabaseHealthService(container, healthChecker); err != nil {
			return err
		}
	}

	// Register metrics service
	if options.EnableMetrics {
		if err := RegisterDatabaseMetricsService(container); err != nil {
			return err
		}
	}

	return nil
}
