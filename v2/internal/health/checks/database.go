package checks

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	health "github.com/xraph/forge/v2/internal/health/internal"
	"github.com/xraph/forge/v2/internal/shared"
)

// DatabaseHealthCheck performs health checks on database connections
type DatabaseHealthCheck struct {
	*health.BaseHealthCheck
	db              *sql.DB
	pingQuery       string
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
	mu              sync.RWMutex
}

// DatabaseHealthCheckConfig contains configuration for database health checks
type DatabaseHealthCheckConfig struct {
	Name            string
	DB              *sql.DB
	PingQuery       string
	Timeout         time.Duration
	Critical        bool
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	Tags            map[string]string
}

// NewDatabaseHealthCheck creates a new database health check
func NewDatabaseHealthCheck(config *DatabaseHealthCheckConfig) *DatabaseHealthCheck {
	if config == nil {
		config = &DatabaseHealthCheckConfig{}
	}

	if config.Name == "" {
		config.Name = "database"
	}

	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}

	if config.PingQuery == "" {
		config.PingQuery = "SELECT 1"
	}

	if config.Tags == nil {
		config.Tags = make(map[string]string)
	}

	baseConfig := &health.HealthCheckConfig{
		Name:     config.Name,
		Timeout:  config.Timeout,
		Critical: config.Critical,
		Tags:     config.Tags,
	}

	return &DatabaseHealthCheck{
		BaseHealthCheck: health.NewBaseHealthCheck(baseConfig),
		db:              config.DB,
		pingQuery:       config.PingQuery,
		maxOpenConns:    config.MaxOpenConns,
		maxIdleConns:    config.MaxIdleConns,
		connMaxLifetime: config.ConnMaxLifetime,
	}
}

// Check performs the database health check
func (dhc *DatabaseHealthCheck) Check(ctx context.Context) *health.HealthResult {
	start := time.Now()

	if dhc.db == nil {
		return health.NewHealthResult(dhc.Name(), health.HealthStatusUnhealthy, "database connection is nil").
			WithDuration(time.Since(start))
	}

	// Create result
	result := health.NewHealthResult(dhc.Name(), health.HealthStatusHealthy, "database is healthy").
		WithDuration(time.Since(start)).
		WithCritical(dhc.Critical()).
		WithTags(dhc.Tags())

	// Check database connection
	if err := dhc.checkConnection(ctx); err != nil {
		return result.
			WithError(err).
			WithDetail("connection_check", "failed")
	}

	// Check database statistics
	stats := dhc.checkStats()
	for k, v := range stats {
		result.WithDetail(k, v)
	}

	// Check connection pool health
	if poolHealth := dhc.checkConnectionPool(); poolHealth != nil {
		for k, v := range poolHealth {
			result.WithDetail(k, v)
		}
	}

	result.WithDuration(time.Since(start))
	return result
}

// checkConnection checks if the database connection is working
func (dhc *DatabaseHealthCheck) checkConnection(ctx context.Context) error {
	dhc.mu.RLock()
	defer dhc.mu.RUnlock()

	// First try to ping the database
	if err := dhc.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// Try to execute a simple query
	if dhc.pingQuery != "" {
		row := dhc.db.QueryRowContext(ctx, dhc.pingQuery)
		var result interface{}
		if err := row.Scan(&result); err != nil {
			return fmt.Errorf("database query failed: %w", err)
		}
	}

	return nil
}

// checkStats returns database statistics
func (dhc *DatabaseHealthCheck) checkStats() map[string]interface{} {
	dhc.mu.RLock()
	defer dhc.mu.RUnlock()

	stats := dhc.db.Stats()

	return map[string]interface{}{
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration":        stats.WaitDuration.String(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}
}

// checkConnectionPool checks connection pool health
func (dhc *DatabaseHealthCheck) checkConnectionPool() map[string]interface{} {
	dhc.mu.RLock()
	defer dhc.mu.RUnlock()

	stats := dhc.db.Stats()
	poolHealth := make(map[string]interface{})

	// Check if we're approaching connection limits
	if dhc.maxOpenConns > 0 {
		utilizationPercent := float64(stats.OpenConnections) / float64(dhc.maxOpenConns) * 100
		poolHealth["connection_utilization_percent"] = utilizationPercent

		if utilizationPercent > 80 {
			poolHealth["connection_pool_warning"] = "high connection utilization"
		}
	}

	// Check wait statistics
	if stats.WaitCount > 0 {
		poolHealth["connection_waits"] = stats.WaitCount
		poolHealth["average_wait_time"] = stats.WaitDuration / time.Duration(stats.WaitCount)
	}

	// Check idle connection health
	if dhc.maxIdleConns > 0 {
		idleUtilization := float64(stats.Idle) / float64(dhc.maxIdleConns) * 100
		poolHealth["idle_utilization_percent"] = idleUtilization
	}

	return poolHealth
}

// PostgreSQLHealthCheck is a specialized health check for PostgreSQL
type PostgreSQLHealthCheck struct {
	*DatabaseHealthCheck
	checkReplication  bool
	enableTablespaces bool
	enableLocksCheck  bool
}

// NewPostgreSQLHealthCheck creates a new PostgreSQL health check
func NewPostgreSQLHealthCheck(config *DatabaseHealthCheckConfig) *PostgreSQLHealthCheck {
	if config.PingQuery == "" {
		config.PingQuery = "SELECT version()"
	}

	return &PostgreSQLHealthCheck{
		DatabaseHealthCheck: NewDatabaseHealthCheck(config),
		checkReplication:    true,
		enableTablespaces:   true,
		enableLocksCheck:    true,
	}
}

// Check performs PostgreSQL-specific health checks
func (pgc *PostgreSQLHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base database check
	result := pgc.DatabaseHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add PostgreSQL-specific checks
	if pgc.checkReplication {
		if replicationStatus := pgc.checkReplicationStatus(ctx); replicationStatus != nil {
			for k, v := range replicationStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if pgc.enableTablespaces {
		if tablespaceStatus := pgc.checkTablespaces(ctx); tablespaceStatus != nil {
			for k, v := range tablespaceStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if pgc.enableLocksCheck {
		if lockStatus := pgc.checkLocks(ctx); lockStatus != nil {
			for k, v := range lockStatus {
				result.WithDetail(k, v)
			}
		}
	}

	return result
}

// checkReplicationStatus checks PostgreSQL replication status
func (pgc *PostgreSQLHealthCheck) checkReplicationStatus(ctx context.Context) map[string]interface{} {
	query := `
		SELECT 
			client_addr,
			state,
			sent_lsn,
			write_lsn,
			flush_lsn,
			replay_lsn,
			write_lag,
			flush_lag,
			replay_lag
		FROM pg_stat_replication
	`

	rows, err := pgc.db.QueryContext(ctx, query)
	if err != nil {
		return map[string]interface{}{
			"replication_error": err.Error(),
		}
	}
	defer rows.Close()

	replicationInfo := make([]map[string]interface{}, 0)
	for rows.Next() {
		var clientAddr, state, sentLsn, writeLsn, flushLsn, replayLsn sql.NullString
		var writeLag, flushLag, replayLag sql.NullString

		if err := rows.Scan(&clientAddr, &state, &sentLsn, &writeLsn, &flushLsn, &replayLsn, &writeLag, &flushLag, &replayLag); err != nil {
			continue
		}

		replicationInfo = append(replicationInfo, map[string]interface{}{
			"client_addr": clientAddr.String,
			"state":       state.String,
			"sent_lsn":    sentLsn.String,
			"write_lsn":   writeLsn.String,
			"flush_lsn":   flushLsn.String,
			"replay_lsn":  replayLsn.String,
			"write_lag":   writeLag.String,
			"flush_lag":   flushLag.String,
			"replay_lag":  replayLag.String,
		})
	}

	return map[string]interface{}{
		"replication_status": replicationInfo,
		"replica_count":      len(replicationInfo),
	}
}

// enableTablespaces checks PostgreSQL tablespace usage
func (pgc *PostgreSQLHealthCheck) checkTablespaces(ctx context.Context) map[string]interface{} {
	query := `
		SELECT 
			spcname,
			pg_tablespace_location(oid) as location,
			pg_size_pretty(pg_tablespace_size(oid)) as size
		FROM pg_tablespace
	`

	rows, err := pgc.db.QueryContext(ctx, query)
	if err != nil {
		return map[string]interface{}{
			"tablespace_error": err.Error(),
		}
	}
	defer rows.Close()

	tablespaces := make([]map[string]interface{}, 0)
	for rows.Next() {
		var name, location, size string
		if err := rows.Scan(&name, &location, &size); err != nil {
			continue
		}

		tablespaces = append(tablespaces, map[string]interface{}{
			"name":     name,
			"location": location,
			"size":     size,
		})
	}

	return map[string]interface{}{
		"tablespaces": tablespaces,
	}
}

// enableLocksCheck checks for blocking locks
func (pgc *PostgreSQLHealthCheck) checkLocks(ctx context.Context) map[string]interface{} {
	query := `
		SELECT 
			COUNT(*) as lock_count,
			COUNT(CASE WHEN NOT granted THEN 1 END) as waiting_locks
		FROM pg_locks
	`

	var lockCount, waitingLocks int
	err := pgc.db.QueryRowContext(ctx, query).Scan(&lockCount, &waitingLocks)
	if err != nil {
		return map[string]interface{}{
			"locks_error": err.Error(),
		}
	}

	return map[string]interface{}{
		"total_locks":   lockCount,
		"waiting_locks": waitingLocks,
	}
}

// MySQLHealthCheck is a specialized health check for MySQL
type MySQLHealthCheck struct {
	*DatabaseHealthCheck
	checkReplication bool
	slowQueries      bool
}

// NewMySQLHealthCheck creates a new MySQL health check
func NewMySQLHealthCheck(config *DatabaseHealthCheckConfig) *MySQLHealthCheck {
	if config.PingQuery == "" {
		config.PingQuery = "SELECT 1"
	}

	return &MySQLHealthCheck{
		DatabaseHealthCheck: NewDatabaseHealthCheck(config),
		checkReplication:    true,
		slowQueries:         true,
	}
}

// Check performs MySQL-specific health checks
func (mc *MySQLHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// Perform base database check
	result := mc.DatabaseHealthCheck.Check(ctx)

	if result.IsUnhealthy() {
		return result
	}

	// Add MySQL-specific checks
	if mc.checkReplication {
		if replicationStatus := mc.checkReplicationStatus(ctx); replicationStatus != nil {
			for k, v := range replicationStatus {
				result.WithDetail(k, v)
			}
		}
	}

	if mc.slowQueries {
		if slowQueryStatus := mc.checkSlowQueries(ctx); slowQueryStatus != nil {
			for k, v := range slowQueryStatus {
				result.WithDetail(k, v)
			}
		}
	}

	return result
}

// checkReplicationStatus checks MySQL replication status
func (mc *MySQLHealthCheck) checkReplicationStatus(ctx context.Context) map[string]interface{} {
	query := "SHOW SLAVE STATUS"

	rows, err := mc.db.QueryContext(ctx, query)
	if err != nil {
		return map[string]interface{}{
			"replication_error": err.Error(),
		}
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return map[string]interface{}{
			"replication_columns_error": err.Error(),
		}
	}

	// Create slice to hold values
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	replicationInfo := make(map[string]interface{})
	if rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return map[string]interface{}{
				"replication_scan_error": err.Error(),
			}
		}

		for i, col := range columns {
			replicationInfo[col] = values[i]
		}
	}

	return map[string]interface{}{
		"replication_status": replicationInfo,
	}
}

// slowQueries checks for slow queries
func (mc *MySQLHealthCheck) checkSlowQueries(ctx context.Context) map[string]interface{} {
	query := "SHOW GLOBAL STATUS LIKE 'Slow_queries'"

	var variableName, value string
	err := mc.db.QueryRowContext(ctx, query).Scan(&variableName, &value)
	if err != nil {
		return map[string]interface{}{
			"slow_queries_error": err.Error(),
		}
	}

	return map[string]interface{}{
		"slow_queries": value,
	}
}

// MongoDBHealthCheck is a health check for MongoDB (placeholder)
type MongoDBHealthCheck struct {
	*health.BaseHealthCheck
	// MongoDB-specific fields would go here
}

// NewMongoDBHealthCheck creates a new MongoDB health check
func NewMongoDBHealthCheck(config *health.HealthCheckConfig) *MongoDBHealthCheck {
	return &MongoDBHealthCheck{
		BaseHealthCheck: health.NewBaseHealthCheck(config),
	}
}

// Check performs MongoDB health check
func (mhc *MongoDBHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// TODO: Implement MongoDB health check
	return health.NewHealthResult(mhc.Name(), health.HealthStatusHealthy, "MongoDB health check not implemented")
}

// RedisHealthCheck is a health check for Redis (placeholder)
type RedisHealthCheck struct {
	*health.BaseHealthCheck
	// Redis-specific fields would go here
}

// NewRedisHealthCheck creates a new Redis health check
func NewRedisHealthCheck(config *health.HealthCheckConfig) *RedisHealthCheck {
	return &RedisHealthCheck{
		BaseHealthCheck: health.NewBaseHealthCheck(config),
	}
}

// Check performs Redis health check
func (rhc *RedisHealthCheck) Check(ctx context.Context) *health.HealthResult {
	// TODO: Implement Redis health check
	return health.NewHealthResult(rhc.Name(), health.HealthStatusHealthy, "Redis health check not implemented")
}

// DatabaseHealthCheckFactory creates database health checks based on configuration
type DatabaseHealthCheckFactory struct {
	container shared.Container
}

// NewDatabaseHealthCheckFactory creates a new factory
func NewDatabaseHealthCheckFactory(container shared.Container) *DatabaseHealthCheckFactory {
	return &DatabaseHealthCheckFactory{
		container: container,
	}
}

// CreateDatabaseHealthCheck creates a database health check
func (factory *DatabaseHealthCheckFactory) CreateDatabaseHealthCheck(name string, dbType string, critical bool) (health.HealthCheck, error) {
	// This would typically resolve database connections from the container
	// For now, return a placeholder
	config := &health.HealthCheckConfig{
		Name:     name,
		Timeout:  5 * time.Second,
		Critical: critical,
	}

	switch dbType {
	case "postgres", "postgresql":
		return NewPostgreSQLHealthCheck(&DatabaseHealthCheckConfig{
			Name:     name,
			Timeout:  config.Timeout,
			Critical: critical,
		}), nil
	case "mysql":
		return NewMySQLHealthCheck(&DatabaseHealthCheckConfig{
			Name:     name,
			Timeout:  config.Timeout,
			Critical: critical,
		}), nil
	case "mongodb", "mongo":
		return NewMongoDBHealthCheck(config), nil
	case "redis":
		return NewRedisHealthCheck(config), nil
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// RegisterDatabaseHealthChecks registers database health checks with the health service
func RegisterDatabaseHealthChecks(healthService health.HealthService, container shared.Container) error {
	factory := NewDatabaseHealthCheckFactory(container)

	// This would typically discover database services from the container
	// and register appropriate health checks
	databases := []struct {
		name     string
		dbType   string
		critical bool
	}{
		{"postgres", "postgres", true},
		{"mysql", "mysql", true},
		{"redis", "redis", false},
		{"mongodb", "mongodb", false},
	}

	for _, db := range databases {
		check, err := factory.CreateDatabaseHealthCheck(db.name, db.dbType, db.critical)
		if err != nil {
			continue // Skip unsupported databases
		}

		if err := healthService.Register(check); err != nil {
			return fmt.Errorf("failed to register %s health check: %w", db.name, err)
		}
	}

	return nil
}
