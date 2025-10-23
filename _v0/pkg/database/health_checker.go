package database

import (
	"context"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// DatabaseHealthCheck implements the core.HealthCheck interface for database health checking
type DatabaseHealthCheck struct {
	name         string
	manager      DatabaseManager
	timeout      time.Duration
	critical     bool
	dependencies []string
}

// NewDatabaseHealthCheck creates a new database health check
func NewDatabaseHealthCheck(manager DatabaseManager) *DatabaseHealthCheck {
	return &DatabaseHealthCheck{
		name:         "database",
		manager:      manager,
		timeout:      10 * time.Second,
		critical:     true, // Database is typically critical
		dependencies: []string{"database-manager"},
	}
}

// NewDatabaseHealthCheckWithName creates a new database health check with a custom name
func NewDatabaseHealthCheckWithName(name string, manager DatabaseManager) *DatabaseHealthCheck {
	return &DatabaseHealthCheck{
		name:         name,
		manager:      manager,
		timeout:      10 * time.Second,
		critical:     true,
		dependencies: []string{"database-manager"},
	}
}

// Name returns the health check name
func (dhc *DatabaseHealthCheck) Name() string {
	return dhc.name
}

// Check performs the health check and returns the result
func (dhc *DatabaseHealthCheck) Check(ctx context.Context) *common.HealthResult {
	start := time.Now()

	// Create a context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, dhc.timeout)
	defer cancel()

	result := common.NewHealthResult(dhc.name, common.HealthStatusHealthy, "Database connections are healthy").
		WithCritical(dhc.critical).
		WithTimestamp(start)

	// Perform health check on all connections
	err := dhc.manager.HealthCheckAll(checkCtx)
	duration := time.Since(start)
	result.WithDuration(duration)

	if err != nil {
		return result.
			WithStatus(common.HealthStatusUnhealthy).
			WithMessage("Database health check failed").
			WithError(err).
			WithDetail("error_details", err.Error())
	}

	// Get detailed statistics
	stats := dhc.manager.GetStats()
	details := map[string]interface{}{
		"total_connections":   stats.TotalConnections,
		"active_connections":  stats.ActiveConnections,
		"registered_adapters": stats.RegisteredAdapters,
		"health_check_passed": stats.HealthCheckPassed,
		"last_health_check":   stats.LastHealthCheck,
		"migrations_enabled":  stats.MigrationsEnabled,
		"total_migrations":    stats.TotalMigrations,
		"last_migration_run":  stats.LastMigrationRun,
	}

	// Add connection-specific details
	connectionDetails := make(map[string]interface{})
	for name, connStats := range stats.ConnectionStats {
		connectionDetails[name] = map[string]interface{}{
			"type":              connStats.Type,
			"connected":         connStats.Connected,
			"connected_at":      connStats.ConnectedAt,
			"last_ping":         connStats.LastPing,
			"ping_duration":     connStats.PingDuration,
			"transaction_count": connStats.TransactionCount,
			"query_count":       connStats.QueryCount,
			"error_count":       connStats.ErrorCount,
			"open_connections":  connStats.OpenConnections,
			"idle_connections":  connStats.IdleConnections,
			"max_open_conns":    connStats.MaxOpenConns,
			"max_idle_conns":    connStats.MaxIdleConns,
		}
	}
	details["connections"] = connectionDetails

	// Determine overall health status
	if stats.ActiveConnections == 0 && stats.TotalConnections > 0 {
		return result.
			WithStatus(common.HealthStatusUnhealthy).
			WithMessage("No active database connections").
			WithDetails(details)
	}

	if !stats.HealthCheckPassed {
		return result.
			WithStatus(common.HealthStatusDegraded).
			WithMessage("Some database connections are unhealthy").
			WithDetails(details)
	}

	// Check individual connection health
	unhealthyConnections := 0
	degradedConnections := 0

	for _, connStats := range stats.ConnectionStats {
		if !connStats.Connected {
			unhealthyConnections++
		} else if connStats.ErrorCount > 0 {
			degradedConnections++
		}
	}

	if unhealthyConnections > 0 {
		return result.
			WithStatus(common.HealthStatusUnhealthy).
			WithMessage("Some database connections are disconnected").
			WithDetails(details).
			WithDetail("unhealthy_connections", unhealthyConnections)
	}

	if degradedConnections > 0 {
		return result.
			WithStatus(common.HealthStatusDegraded).
			WithMessage("Some database connections have errors").
			WithDetails(details).
			WithDetail("degraded_connections", degradedConnections)
	}

	return result.WithDetails(details)
}

// Timeout returns the health check timeout
func (dhc *DatabaseHealthCheck) Timeout() time.Duration {
	return dhc.timeout
}

// Critical returns whether this health check is critical
func (dhc *DatabaseHealthCheck) Critical() bool {
	return dhc.critical
}

// Dependencies returns the health check dependencies
func (dhc *DatabaseHealthCheck) Dependencies() []string {
	return dhc.dependencies
}

// SetTimeout sets the health check timeout
func (dhc *DatabaseHealthCheck) SetTimeout(timeout time.Duration) *DatabaseHealthCheck {
	dhc.timeout = timeout
	return dhc
}

// SetCritical sets whether this health check is critical
func (dhc *DatabaseHealthCheck) SetCritical(critical bool) *DatabaseHealthCheck {
	dhc.critical = critical
	return dhc
}

// SetDependencies sets the health check dependencies
func (dhc *DatabaseHealthCheck) SetDependencies(dependencies []string) *DatabaseHealthCheck {
	dhc.dependencies = dependencies
	return dhc
}

// RegisterDatabaseHealthServiceWithName registers a named database health service
func RegisterDatabaseHealthServiceWithName(container common.Container, healthChecker common.HealthChecker, name string) error {
	if healthChecker == nil {
		return nil
	}

	managerInterface, err := container.ResolveNamed("database-manager")
	if err != nil {
		lazyHealthCheck := &LazyDatabaseHealthCheck{
			container: container,
			name:      name,
			timeout:   10 * time.Second,
			critical:  true,
		}
		return healthChecker.RegisterCheck(name, lazyHealthCheck)
	}

	manager, ok := managerInterface.(DatabaseManager)
	if !ok {
		errorHealthCheck := &ErrorDatabaseHealthCheck{
			name:    name,
			message: "Invalid database manager type",
		}
		return healthChecker.RegisterCheck(name, errorHealthCheck)
	}

	healthCheck := NewDatabaseHealthCheckWithName(name, manager)
	return healthChecker.RegisterCheck(name, healthCheck)
}

// LazyDatabaseHealthCheck implements lazy loading of the database manager
type LazyDatabaseHealthCheck struct {
	container common.Container
	name      string
	timeout   time.Duration
	critical  bool
}

func (ldhc *LazyDatabaseHealthCheck) Name() string {
	return ldhc.name
}

func (ldhc *LazyDatabaseHealthCheck) Check(ctx context.Context) *common.HealthResult {
	start := time.Now()
	result := common.NewHealthResult(ldhc.name, common.HealthStatusUnhealthy, "Database manager not available").
		WithCritical(ldhc.critical).
		WithTimestamp(start)

	// Try to resolve the database manager
	managerInterface, err := ldhc.container.ResolveNamed("database-manager")
	if err != nil {
		return result.
			WithError(err).
			WithDuration(time.Since(start)).
			WithDetail("resolution_error", err.Error())
	}

	manager, ok := managerInterface.(DatabaseManager)
	if !ok {
		return result.
			WithMessage("Invalid database manager type").
			WithDuration(time.Since(start)).
			WithDetail("type_error", "database-manager is not a DatabaseManager")
	}

	// Create a proper health check and delegate to it
	healthCheck := NewDatabaseHealthCheckWithName(ldhc.name, manager).
		SetTimeout(ldhc.timeout).
		SetCritical(ldhc.critical)

	return healthCheck.Check(ctx)
}

func (ldhc *LazyDatabaseHealthCheck) Timeout() time.Duration {
	return ldhc.timeout
}

func (ldhc *LazyDatabaseHealthCheck) Critical() bool {
	return ldhc.critical
}

func (ldhc *LazyDatabaseHealthCheck) Dependencies() []string {
	return []string{"database-manager"}
}

// ErrorDatabaseHealthCheck reports a configuration error
type ErrorDatabaseHealthCheck struct {
	name     string
	message  string
	critical bool
}

func (edhc *ErrorDatabaseHealthCheck) Name() string {
	return edhc.name
}

func (edhc *ErrorDatabaseHealthCheck) Check(ctx context.Context) *common.HealthResult {
	return common.NewHealthResult(edhc.name, common.HealthStatusUnhealthy, edhc.message).
		WithCritical(edhc.critical).
		WithTimestampNow().
		WithDetail("configuration_error", edhc.message)
}

func (edhc *ErrorDatabaseHealthCheck) Timeout() time.Duration {
	return 1 * time.Second // Quick timeout for error cases
}

func (edhc *ErrorDatabaseHealthCheck) Critical() bool {
	return edhc.critical
}

func (edhc *ErrorDatabaseHealthCheck) Dependencies() []string {
	return []string{}
}

// RegisterDatabaseHealthServicesWithOptions registers database health services with custom options
func RegisterDatabaseHealthServicesWithOptions(container common.Container, healthChecker common.HealthChecker, options *DatabaseServiceOptions) error {
	if options == nil {
		options = DefaultDatabaseServiceOptions()
	}

	if !options.EnableHealth || healthChecker == nil {
		return nil
	}

	// Register main database health check
	if err := RegisterDatabaseHealthService(container, healthChecker); err != nil {
		return err
	}

	// Register health checks for specific connections if requested
	for _, connectionName := range options.ConnectionNames {
		healthCheckName := "database-" + connectionName
		if err := RegisterConnectionHealthService(container, healthChecker, connectionName, healthCheckName); err != nil {
			return err
		}
	}

	return nil
}

// RegisterConnectionHealthService registers a health check for a specific database connection
func RegisterConnectionHealthService(container common.Container, healthChecker common.HealthChecker, connectionName, healthCheckName string) error {
	if healthChecker == nil {
		return nil
	}

	// Create a connection-specific health check
	connHealthCheck := &ConnectionHealthCheck{
		container:      container,
		connectionName: connectionName,
		name:           healthCheckName,
		timeout:        5 * time.Second,
		critical:       false, // Individual connections are typically not critical
	}

	return healthChecker.RegisterCheck(healthCheckName, connHealthCheck)
}

// ConnectionHealthCheck implements health checking for a specific database connection
type ConnectionHealthCheck struct {
	container      common.Container
	connectionName string
	name           string
	timeout        time.Duration
	critical       bool
}

func (chc *ConnectionHealthCheck) Name() string {
	return chc.name
}

func (chc *ConnectionHealthCheck) Check(ctx context.Context) *common.HealthResult {
	start := time.Now()
	result := common.NewHealthResult(chc.name, common.HealthStatusUnhealthy, "Connection not available").
		WithCritical(chc.critical).
		WithTimestamp(start).
		WithDetail("connection_name", chc.connectionName)

	// Get the database manager
	managerInterface, err := chc.container.ResolveNamed("database-manager")
	if err != nil {
		return result.
			WithError(err).
			WithDuration(time.Since(start)).
			WithDetail("manager_resolution_error", err.Error())
	}

	manager, ok := managerInterface.(DatabaseManager)
	if !ok {
		return result.
			WithMessage("Invalid database manager type").
			WithDuration(time.Since(start))
	}

	// Get the specific connection
	conn, err := manager.GetConnection(chc.connectionName)
	if err != nil {
		return result.
			WithError(err).
			WithDuration(time.Since(start)).
			WithDetail("connection_error", err.Error())
	}

	// Create a context with timeout
	checkCtx, cancel := context.WithTimeout(ctx, chc.timeout)
	defer cancel()

	// Perform health check on the connection
	if err := conn.OnHealthCheck(checkCtx); err != nil {
		return result.
			WithError(err).
			WithDuration(time.Since(start)).
			WithDetail("health_check_error", err.Error())
	}

	// Get connection statistics
	stats := conn.Stats()
	details := map[string]interface{}{
		"connection_name":   chc.connectionName,
		"type":              stats.Type,
		"connected":         stats.Connected,
		"connected_at":      stats.ConnectedAt,
		"last_ping":         stats.LastPing,
		"ping_duration":     stats.PingDuration,
		"transaction_count": stats.TransactionCount,
		"query_count":       stats.QueryCount,
		"error_count":       stats.ErrorCount,
		"open_connections":  stats.OpenConnections,
		"idle_connections":  stats.IdleConnections,
		"max_open_conns":    stats.MaxOpenConns,
		"max_idle_conns":    stats.MaxIdleConns,
	}

	status := common.HealthStatusHealthy
	message := "Connection is healthy"

	if !stats.Connected {
		status = common.HealthStatusUnhealthy
		message = "Connection is not established"
	} else if stats.ErrorCount > 0 {
		status = common.HealthStatusDegraded
		message = "Connection has recorded errors"
	}

	return result.
		WithStatus(status).
		WithMessage(message).
		WithDetails(details).
		WithDuration(time.Since(start))
}

func (chc *ConnectionHealthCheck) Timeout() time.Duration {
	return chc.timeout
}

func (chc *ConnectionHealthCheck) Critical() bool {
	return chc.critical
}

func (chc *ConnectionHealthCheck) Dependencies() []string {
	return []string{"database-manager"}
}
