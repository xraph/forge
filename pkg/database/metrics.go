package database

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// MetricsCollector collects and reports database metrics
type MetricsCollector interface {
	// Connection metrics
	RecordConnectionCreated(connectionName, dbType string)
	RecordConnectionDestroyed(connectionName, dbType string)
	RecordConnectionError(connectionName, dbType string, err error)
	RecordPing(connectionName, dbType string, duration time.Duration)

	// Query metrics
	RecordQuery(connectionName, dbType, operation string, duration time.Duration, success bool)
	RecordTransaction(connectionName, dbType string, duration time.Duration, success bool)
	RecordSlowQuery(connectionName, dbType, query string, duration time.Duration)

	// Pool metrics
	RecordPoolStats(connectionName, dbType string, stats PoolStats)

	// Migration metrics
	RecordMigration(connectionName, dbType string, migrationID string, duration time.Duration, success bool)

	// Collection control
	StartCollection(ctx context.Context, manager DatabaseManager, interval time.Duration)
	StopCollection()
	CollectMetrics(manager DatabaseManager)

	// Configuration
	SetSlowQueryThreshold(threshold time.Duration)
	EnableQueryMetrics(enabled bool)
	EnablePoolMetrics(enabled bool)
}

// PoolStats represents connection pool statistics
type PoolStats struct {
	OpenConnections int
	IdleConnections int
	MaxOpenConns    int
	MaxIdleConns    int
	WaitCount       int64
	WaitDuration    time.Duration
}

// DatabaseMetricsCollector implements MetricsCollector
type DatabaseMetricsCollector struct {
	metrics            common.Metrics
	logger             common.Logger
	slowQueryThreshold time.Duration
	enableQueryLogging bool
	enablePoolMetrics  bool
	ticker             *time.Ticker
	stopCh             chan struct{}
	running            bool
	mu                 sync.RWMutex

	// Aggregated metrics
	queryStats      map[string]*QueryStats
	connectionStats map[string]*ConnectionMetrics
	migrationStats  map[string]*MigrationStats
	lastCollection  time.Time
}

// QueryStats tracks query statistics
type QueryStats struct {
	ConnectionName  string        `json:"connection_name"`
	DatabaseType    string        `json:"database_type"`
	Operation       string        `json:"operation"`
	TotalQueries    int64         `json:"total_queries"`
	SuccessCount    int64         `json:"success_count"`
	ErrorCount      int64         `json:"error_count"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
	MinDuration     time.Duration `json:"min_duration"`
	MaxDuration     time.Duration `json:"max_duration"`
	SlowQueries     int64         `json:"slow_queries"`
	LastQuery       time.Time     `json:"last_query"`
	mu              sync.RWMutex
}

// ConnectionMetrics tracks connection-level metrics
type ConnectionMetrics struct {
	ConnectionName    string        `json:"connection_name"`
	DatabaseType      string        `json:"database_type"`
	CreatedAt         time.Time     `json:"created_at"`
	TotalConnections  int64         `json:"total_connections"`
	ActiveConnections int64         `json:"active_connections"`
	ConnectionErrors  int64         `json:"connection_errors"`
	TotalPings        int64         `json:"total_pings"`
	PingErrors        int64         `json:"ping_errors"`
	AveragePingTime   time.Duration `json:"average_ping_time"`
	MinDuration       time.Duration `json:"min_duration"`
	LastPing          time.Time     `json:"last_ping"`
	LastError         string        `json:"last_error,omitempty"`
	mu                sync.RWMutex
}

// MigrationStats tracks migration statistics
type MigrationStats struct {
	ConnectionName       string        `json:"connection_name"`
	DatabaseType         string        `json:"database_type"`
	TotalMigrations      int64         `json:"total_migrations"`
	SuccessfulMigrations int64         `json:"successful_migrations"`
	FailedMigrations     int64         `json:"failed_migrations"`
	TotalMigrationTime   time.Duration `json:"total_migration_time"`
	AverageMigrationTime time.Duration `json:"average_migration_time"`
	LastMigration        string        `json:"last_migration"`
	LastMigrationTime    time.Time     `json:"last_migration_time"`
	mu                   sync.RWMutex
}

// NewDatabaseMetricsCollector creates a new metrics collector
func NewDatabaseMetricsCollector(metrics common.Metrics, logger common.Logger) MetricsCollector {
	return &DatabaseMetricsCollector{
		metrics:            metrics,
		logger:             logger,
		slowQueryThreshold: time.Second,
		enableQueryLogging: true,
		enablePoolMetrics:  true,
		stopCh:             make(chan struct{}),
		queryStats:         make(map[string]*QueryStats),
		connectionStats:    make(map[string]*ConnectionMetrics),
		migrationStats:     make(map[string]*MigrationStats),
	}
}

// SetSlowQueryThreshold sets the threshold for slow query detection
func (dmc *DatabaseMetricsCollector) SetSlowQueryThreshold(threshold time.Duration) {
	dmc.mu.Lock()
	defer dmc.mu.Unlock()
	dmc.slowQueryThreshold = threshold
}

// EnableQueryMetrics enables or disables query logging
func (dmc *DatabaseMetricsCollector) EnableQueryMetrics(enabled bool) {
	dmc.mu.Lock()
	defer dmc.mu.Unlock()
	dmc.enableQueryLogging = enabled
}

// EnablePoolMetrics enables or disables pool metrics collection
func (dmc *DatabaseMetricsCollector) EnablePoolMetrics(enabled bool) {
	dmc.mu.Lock()
	defer dmc.mu.Unlock()
	dmc.enablePoolMetrics = enabled
}

// RecordConnectionCreated records a new connection creation
func (dmc *DatabaseMetricsCollector) RecordConnectionCreated(connectionName, dbType string) {
	key := connectionName + ":" + dbType

	dmc.mu.Lock()
	stats, exists := dmc.connectionStats[key]
	if !exists {
		stats = &ConnectionMetrics{
			ConnectionName: connectionName,
			DatabaseType:   dbType,
			CreatedAt:      time.Now(),
			MinDuration:    time.Hour, // Initialize with high value
		}
		dmc.connectionStats[key] = stats
	}
	stats.mu.Lock()
	stats.TotalConnections++
	stats.ActiveConnections++
	stats.mu.Unlock()
	dmc.mu.Unlock()

	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType}
		dmc.metrics.Counter("forge.database.connections_created", tags...).Inc()
		dmc.metrics.Gauge("forge.database.active_connections", tags...).Set(float64(stats.ActiveConnections))
	}

	if dmc.enableQueryLogging && dmc.logger != nil {
		dmc.logger.Debug("database connection created",
			logger.String("connection", connectionName),
			logger.String("type", dbType),
		)
	}
}

// RecordConnectionDestroyed records a connection destruction
func (dmc *DatabaseMetricsCollector) RecordConnectionDestroyed(connectionName, dbType string) {
	key := connectionName + ":" + dbType

	dmc.mu.RLock()
	stats, exists := dmc.connectionStats[key]
	dmc.mu.RUnlock()

	if exists {
		stats.mu.Lock()
		stats.ActiveConnections--
		if stats.ActiveConnections < 0 {
			stats.ActiveConnections = 0
		}
		stats.mu.Unlock()
	}

	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType}
		dmc.metrics.Counter("forge.database.connections_destroyed", tags...).Inc()
		if exists {
			dmc.metrics.Gauge("forge.database.active_connections", tags...).Set(float64(stats.ActiveConnections))
		}
	}

	if dmc.enableQueryLogging && dmc.logger != nil {
		dmc.logger.Debug("database connection destroyed",
			logger.String("connection", connectionName),
			logger.String("type", dbType),
		)
	}
}

// RecordConnectionError records a connection error
func (dmc *DatabaseMetricsCollector) RecordConnectionError(connectionName, dbType string, err error) {
	key := connectionName + ":" + dbType

	dmc.mu.RLock()
	stats, exists := dmc.connectionStats[key]
	dmc.mu.RUnlock()

	if exists {
		stats.mu.Lock()
		stats.ConnectionErrors++
		stats.LastError = err.Error()
		stats.mu.Unlock()
	}

	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType}
		dmc.metrics.Counter("forge.database.connection_errors", tags...).Inc()
	}

	if dmc.logger != nil {
		dmc.logger.Error("database connection error",
			logger.String("connection", connectionName),
			logger.String("type", dbType),
			logger.Error(err),
		)
	}
}

// RecordPing records a ping operation
func (dmc *DatabaseMetricsCollector) RecordPing(connectionName, dbType string, duration time.Duration) {
	key := connectionName + ":" + dbType

	dmc.mu.RLock()
	stats, exists := dmc.connectionStats[key]
	dmc.mu.RUnlock()

	if exists {
		stats.mu.Lock()
		stats.TotalPings++
		stats.LastPing = time.Now()

		// Update average ping time
		if stats.TotalPings == 1 {
			stats.AveragePingTime = duration
		} else {
			stats.AveragePingTime = time.Duration(
				(int64(stats.AveragePingTime)*stats.TotalPings + int64(duration)) / (stats.TotalPings + 1),
			)
		}
		stats.mu.Unlock()
	}

	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType}
		dmc.metrics.Histogram("forge.database.ping_duration", tags...).Observe(duration.Seconds())
		dmc.metrics.Counter("forge.database.pings_total", tags...).Inc()
	}
}

// RecordQuery records a query execution
func (dmc *DatabaseMetricsCollector) RecordQuery(connectionName, dbType, operation string, duration time.Duration, success bool) {
	key := connectionName + ":" + dbType + ":" + operation

	dmc.mu.Lock()
	stats, exists := dmc.queryStats[key]
	if !exists {
		stats = &QueryStats{
			ConnectionName: connectionName,
			DatabaseType:   dbType,
			Operation:      operation,
			MinDuration:    time.Hour, // Initialize with high value
		}
		dmc.queryStats[key] = stats
	}
	dmc.mu.Unlock()

	stats.mu.Lock()
	stats.TotalQueries++
	stats.LastQuery = time.Now()
	stats.TotalDuration += duration

	// Update min/max duration
	if duration < stats.MinDuration {
		stats.MinDuration = duration
	}
	if duration > stats.MaxDuration {
		stats.MaxDuration = duration
	}

	// Update average duration
	stats.AverageDuration = stats.TotalDuration / time.Duration(stats.TotalQueries)

	if success {
		stats.SuccessCount++
	} else {
		stats.ErrorCount++
	}

	// Check for slow query
	if duration >= dmc.slowQueryThreshold {
		stats.SlowQueries++
	}
	stats.mu.Unlock()

	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType, "operation", operation}
		dmc.metrics.Histogram("forge.database.query_duration", tags...).Observe(duration.Seconds())
		dmc.metrics.Counter("forge.database.queries_total", tags...).Inc()

		if success {
			dmc.metrics.Counter("forge.database.queries_success", tags...).Inc()
		} else {
			dmc.metrics.Counter("forge.database.queries_error", tags...).Inc()
		}

		if duration >= dmc.slowQueryThreshold {
			dmc.metrics.Counter("forge.database.slow_queries", tags...).Inc()
		}
	}

	if dmc.enableQueryLogging && dmc.logger != nil {
		fields := []logger.Field{
			logger.String("connection", connectionName),
			logger.String("type", dbType),
			logger.String("operation", operation),
			logger.Duration("duration", duration),
			logger.Bool("success", success),
		}

		if duration >= dmc.slowQueryThreshold {
			fields = append(fields, logger.Bool("slow_query", true))
			dmc.logger.Warn("slow database query detected", fields...)
		} else {
			dmc.logger.Debug("database query executed", fields...)
		}
	}
}

// RecordSlowQuery records a slow query with additional details
func (dmc *DatabaseMetricsCollector) RecordSlowQuery(connectionName, dbType, query string, duration time.Duration) {
	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType}
		dmc.metrics.Counter("forge.database.slow_queries_detailed", tags...).Inc()
		dmc.metrics.Histogram("forge.database.slow_query_duration", tags...).Observe(duration.Seconds())
	}

	if dmc.logger != nil {
		dmc.logger.Warn("slow query detected",
			logger.String("connection", connectionName),
			logger.String("type", dbType),
			logger.String("query", query),
			logger.Duration("duration", duration),
		)
	}
}

// RecordTransaction records a transaction execution
func (dmc *DatabaseMetricsCollector) RecordTransaction(connectionName, dbType string, duration time.Duration, success bool) {
	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType}
		dmc.metrics.Histogram("forge.database.transaction_duration", tags...).Observe(duration.Seconds())
		dmc.metrics.Counter("forge.database.transactions_total", tags...).Inc()

		if success {
			dmc.metrics.Counter("forge.database.transactions_success", tags...).Inc()
		} else {
			dmc.metrics.Counter("forge.database.transactions_error", tags...).Inc()
		}
	}

	if dmc.enableQueryLogging && dmc.logger != nil {
		dmc.logger.Debug("database transaction executed",
			logger.String("connection", connectionName),
			logger.String("type", dbType),
			logger.Duration("duration", duration),
			logger.Bool("success", success),
		)
	}
}

// RecordPoolStats records connection pool statistics
func (dmc *DatabaseMetricsCollector) RecordPoolStats(connectionName, dbType string, stats PoolStats) {
	if !dmc.enablePoolMetrics || dmc.metrics == nil {
		return
	}

	tags := []string{"connection", connectionName, "type", dbType}

	dmc.metrics.Gauge("forge.database.pool_open_connections", tags...).Set(float64(stats.OpenConnections))
	dmc.metrics.Gauge("forge.database.pool_idle_connections", tags...).Set(float64(stats.IdleConnections))
	dmc.metrics.Gauge("forge.database.pool_max_open_connections", tags...).Set(float64(stats.MaxOpenConns))
	dmc.metrics.Gauge("forge.database.pool_max_idle_connections", tags...).Set(float64(stats.MaxIdleConns))
	dmc.metrics.Counter("forge.database.pool_wait_count", tags...).Add(float64(stats.WaitCount))
	dmc.metrics.Histogram("forge.database.pool_wait_duration", tags...).Observe(stats.WaitDuration.Seconds())
}

// RecordMigration records a migration execution
func (dmc *DatabaseMetricsCollector) RecordMigration(connectionName, dbType string, migrationID string, duration time.Duration, success bool) {
	key := connectionName + ":" + dbType

	dmc.mu.Lock()
	stats, exists := dmc.migrationStats[key]
	if !exists {
		stats = &MigrationStats{
			ConnectionName: connectionName,
			DatabaseType:   dbType,
		}
		dmc.migrationStats[key] = stats
	}
	dmc.mu.Unlock()

	stats.mu.Lock()
	stats.TotalMigrations++
	stats.LastMigration = migrationID
	stats.LastMigrationTime = time.Now()
	stats.TotalMigrationTime += duration
	stats.AverageMigrationTime = stats.TotalMigrationTime / time.Duration(stats.TotalMigrations)

	if success {
		stats.SuccessfulMigrations++
	} else {
		stats.FailedMigrations++
	}
	stats.mu.Unlock()

	if dmc.metrics != nil {
		tags := []string{"connection", connectionName, "type", dbType}
		dmc.metrics.Histogram("forge.database.migration_duration", tags...).Observe(duration.Seconds())
		dmc.metrics.Counter("forge.database.migrations_total", tags...).Inc()

		if success {
			dmc.metrics.Counter("forge.database.migrations_success", tags...).Inc()
		} else {
			dmc.metrics.Counter("forge.database.migrations_error", tags...).Inc()
		}
	}

	if dmc.logger != nil {
		dmc.logger.Info("database migration executed",
			logger.String("connection", connectionName),
			logger.String("type", dbType),
			logger.String("migration", migrationID),
			logger.Duration("duration", duration),
			logger.Bool("success", success),
		)
	}
}

// StartCollection starts periodic metrics collection
func (dmc *DatabaseMetricsCollector) StartCollection(ctx context.Context, manager DatabaseManager, interval time.Duration) {
	dmc.mu.Lock()
	defer dmc.mu.Unlock()

	if dmc.running {
		dmc.logger.Warn("metrics collection is already running")
		return
	}

	dmc.ticker = time.NewTicker(interval)
	dmc.running = true

	go func() {
		dmc.logger.Info("starting database metrics collection",
			logger.Duration("interval", interval),
		)

		// Collect initial metrics
		dmc.CollectMetrics(manager)

		for {
			select {
			case <-dmc.ticker.C:
				dmc.CollectMetrics(manager)

			case <-dmc.stopCh:
				dmc.logger.Info("stopping database metrics collection")
				return

			case <-ctx.Done():
				dmc.logger.Info("context cancelled, stopping database metrics collection")
				return
			}
		}
	}()
}

// StopCollection stops periodic metrics collection
func (dmc *DatabaseMetricsCollector) StopCollection() {
	dmc.mu.Lock()
	defer dmc.mu.Unlock()

	if !dmc.running {
		return
	}

	if dmc.ticker != nil {
		dmc.ticker.Stop()
		dmc.ticker = nil
	}

	close(dmc.stopCh)
	dmc.stopCh = make(chan struct{}) // Reset for next use
	dmc.running = false
}

// CollectMetrics collects current metrics from the database manager
func (dmc *DatabaseMetricsCollector) CollectMetrics(manager DatabaseManager) {
	start := time.Now()

	// Get manager stats
	managerStats := manager.GetStats()

	// Record manager-level metrics
	if dmc.metrics != nil {
		dmc.metrics.Gauge("forge.database.manager_total_connections").Set(float64(managerStats.TotalConnections))
		dmc.metrics.Gauge("forge.database.manager_active_connections").Set(float64(managerStats.ActiveConnections))
		dmc.metrics.Gauge("forge.database.manager_registered_adapters").Set(float64(managerStats.RegisteredAdapters))

		if managerStats.HealthCheckPassed {
			dmc.metrics.Gauge("forge.database.manager_healthy").Set(1)
		} else {
			dmc.metrics.Gauge("forge.database.manager_healthy").Set(0)
		}
	}

	// Collect metrics from individual connections
	connectionNames := manager.ListConnections()
	for _, connectionName := range connectionNames {
		conn, err := manager.GetConnection(connectionName)
		if err != nil {
			continue
		}

		connStats := conn.Stats()

		// Update pool stats if enabled
		if dmc.enablePoolMetrics {
			poolStats := PoolStats{
				OpenConnections: connStats.OpenConnections,
				IdleConnections: connStats.IdleConnections,
				MaxOpenConns:    connStats.MaxOpenConns,
				MaxIdleConns:    connStats.MaxIdleConns,
			}
			dmc.RecordPoolStats(connectionName, conn.Type(), poolStats)
		}

		// Record connection metrics
		if dmc.metrics != nil {
			tags := []string{"connection", connectionName, "type", conn.Type()}

			if connStats.Connected {
				dmc.metrics.Gauge("forge.database.connection_status", tags...).Set(1)
			} else {
				dmc.metrics.Gauge("forge.database.connection_status", tags...).Set(0)
			}

			dmc.metrics.Counter("forge.database.connection_total_queries", tags...).Add(float64(connStats.QueryCount))
			dmc.metrics.Counter("forge.database.connection_total_transactions", tags...).Add(float64(connStats.TransactionCount))
			dmc.metrics.Counter("forge.database.connection_total_errors", tags...).Add(float64(connStats.ErrorCount))

			if connStats.PingDuration > 0 {
				dmc.metrics.Histogram("forge.database.connection_ping_duration", tags...).Observe(connStats.PingDuration.Seconds())
			}
		}
	}

	dmc.lastCollection = time.Now()
	duration := time.Since(start)

	if dmc.metrics != nil {
		dmc.metrics.Histogram("forge.database.metrics_collection_duration").Observe(duration.Seconds())
		dmc.metrics.Counter("forge.database.metrics_collections_total").Inc()
	}

	if dmc.logger != nil {
		dmc.logger.Debug("database metrics collected",
			logger.Int("connections", len(connectionNames)),
			logger.Duration("duration", duration),
		)
	}
}

// GetQueryStats returns query statistics
func (dmc *DatabaseMetricsCollector) GetQueryStats() map[string]*QueryStats {
	dmc.mu.RLock()
	defer dmc.mu.RUnlock()

	stats := make(map[string]*QueryStats)
	for key, queryStats := range dmc.queryStats {
		// Return a copy to avoid race conditions
		queryStats.mu.RLock()
		statsCopy := *queryStats
		queryStats.mu.RUnlock()
		stats[key] = &statsCopy
	}

	return stats
}

// GetConnectionStats returns connection statistics
func (dmc *DatabaseMetricsCollector) GetConnectionStats() map[string]*ConnectionMetrics {
	dmc.mu.RLock()
	defer dmc.mu.RUnlock()

	stats := make(map[string]*ConnectionMetrics)
	for key, connStats := range dmc.connectionStats {
		// Return a copy to avoid race conditions
		connStats.mu.RLock()
		statsCopy := *connStats
		connStats.mu.RUnlock()
		stats[key] = &statsCopy
	}

	return stats
}

// GetMigrationStats returns migration statistics
func (dmc *DatabaseMetricsCollector) GetMigrationStats() map[string]*MigrationStats {
	dmc.mu.RLock()
	defer dmc.mu.RUnlock()

	stats := make(map[string]*MigrationStats)
	for key, migStats := range dmc.migrationStats {
		// Return a copy to avoid race conditions
		migStats.mu.RLock()
		statsCopy := *migStats
		migStats.mu.RUnlock()
		stats[key] = &statsCopy
	}

	return stats
}

// DefaultMetricsConfig returns default metrics configuration
func DefaultMetricsConfig() *MetricsConfig {
	return &MetricsConfig{
		Enabled:            true,
		CollectionInterval: 15 * time.Second,
		SlowQueryThreshold: time.Second,
		EnableQueryMetrics: true,
		EnablePoolMetrics:  true,
	}
}

// ConfigureMetricsCollector configures a metrics collector with the given configuration
func ConfigureMetricsCollector(collector MetricsCollector, config *MetricsConfig) {
	if config == nil {
		config = DefaultMetricsConfig()
	}

	collector.SetSlowQueryThreshold(config.SlowQueryThreshold)
	collector.EnableQueryMetrics(config.EnableQueryMetrics)
	collector.EnablePoolMetrics(config.EnablePoolMetrics)
}
