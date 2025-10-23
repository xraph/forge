package collectors

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"

	// "github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
	metrics "github.com/xraph/forge/pkg/metrics/core"
)

// =============================================================================
// DATABASE COLLECTOR
// =============================================================================

// DatabaseCollector collects database metrics from Phase 2 database components
type DatabaseCollector struct {
	name               string
	interval           time.Duration
	dbManager          common.DatabaseManager
	logger             logger.Logger
	metrics            map[string]interface{}
	enabled            bool
	mu                 sync.RWMutex
	lastCollectionTime time.Time
	connectionPools    map[string]*sql.DB
	queryStats         map[string]*QueryStats
	connectionStats    map[string]*DatabaseConnectionStats
}

// DatabaseCollectorConfig contains configuration for the database collector
type DatabaseCollectorConfig struct {
	Interval               time.Duration `yaml:"interval" json:"interval"`
	CollectConnectionStats bool          `yaml:"collect_connection_stats" json:"collect_connection_stats"`
	CollectQueryStats      bool          `yaml:"collect_query_stats" json:"collect_query_stats"`
	CollectSlowQueries     bool          `yaml:"collect_slow_queries" json:"collect_slow_queries"`
	SlowQueryThreshold     time.Duration `yaml:"slow_query_threshold" json:"slow_query_threshold"`
	MaxQueryHistory        int           `yaml:"max_query_history" json:"max_query_history"`
	TrackIndividualQueries bool          `yaml:"track_individual_queries" json:"track_individual_queries"`
	DatabaseNames          []string      `yaml:"database_names" json:"database_names"`
}

// QueryStats represents query performance statistics
type QueryStats struct {
	QueryCount      int64         `json:"query_count"`
	TotalDuration   time.Duration `json:"total_duration"`
	AverageDuration time.Duration `json:"average_duration"`
	MinDuration     time.Duration `json:"min_duration"`
	MaxDuration     time.Duration `json:"max_duration"`
	ErrorCount      int64         `json:"error_count"`
	LastExecuted    time.Time     `json:"last_executed"`
	SlowQueryCount  int64         `json:"slow_query_count"`
}

// DatabaseConnectionStats represents database connection statistics
type DatabaseConnectionStats struct {
	ActiveConnections int           `json:"active_connections"`
	IdleConnections   int           `json:"idle_connections"`
	TotalConnections  int           `json:"total_connections"`
	MaxConnections    int           `json:"max_connections"`
	WaitingQueries    int           `json:"waiting_queries"`
	ConnectionErrors  int64         `json:"connection_errors"`
	LastError         error         `json:"last_error,omitempty"`
	LastUpdated       time.Time     `json:"last_updated"`
	ConnectionTime    time.Duration `json:"connection_time"`
}

// SlowQuery represents a slow query record
type SlowQuery struct {
	Query        string        `json:"query"`
	Duration     time.Duration `json:"duration"`
	ExecutedAt   time.Time     `json:"executed_at"`
	DatabaseName string        `json:"database_name"`
	Error        error         `json:"error,omitempty"`
}

// DefaultDatabaseCollectorConfig returns default configuration
func DefaultDatabaseCollectorConfig() *DatabaseCollectorConfig {
	return &DatabaseCollectorConfig{
		Interval:               time.Second * 30,
		CollectConnectionStats: true,
		CollectQueryStats:      true,
		CollectSlowQueries:     true,
		SlowQueryThreshold:     time.Second * 1,
		MaxQueryHistory:        1000,
		TrackIndividualQueries: false,
		DatabaseNames:          []string{},
	}
}

// NewDatabaseCollector creates a new database collector
func NewDatabaseCollector(dbManager common.DatabaseManager, logger logger.Logger) metrics.CustomCollector {
	return NewDatabaseCollectorWithConfig(dbManager, DefaultDatabaseCollectorConfig(), logger)
}

// NewDatabaseCollectorWithConfig creates a new database collector with configuration
func NewDatabaseCollectorWithConfig(dbManager common.DatabaseManager, config *DatabaseCollectorConfig, logger logger.Logger) metrics.CustomCollector {
	return &DatabaseCollector{
		name:            "database",
		interval:        config.Interval,
		dbManager:       dbManager,
		logger:          logger,
		metrics:         make(map[string]interface{}),
		enabled:         true,
		connectionPools: make(map[string]*sql.DB),
		queryStats:      make(map[string]*QueryStats),
		connectionStats: make(map[string]*DatabaseConnectionStats),
	}
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name
func (dc *DatabaseCollector) Name() string {
	return dc.name
}

// Collect collects database metrics
func (dc *DatabaseCollector) Collect() map[string]interface{} {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if !dc.enabled {
		return dc.metrics
	}

	now := time.Now()

	// Only collect if enough time has passed
	if !dc.lastCollectionTime.IsZero() && now.Sub(dc.lastCollectionTime) < dc.interval {
		return dc.metrics
	}

	dc.lastCollectionTime = now

	// Clear previous metrics
	dc.metrics = make(map[string]interface{})

	// Collect database connection statistics
	if err := dc.collectConnectionStats(); err != nil && dc.logger != nil {
		dc.logger.Error("failed to collect database connection stats",
			logger.Error(err),
		)
	}

	// Collect query statistics
	if err := dc.collectQueryStats(); err != nil && dc.logger != nil {
		dc.logger.Error("failed to collect database query stats",
			logger.Error(err),
		)
	}

	// Collect database-specific metrics
	if err := dc.collectDatabaseSpecificMetrics(); err != nil && dc.logger != nil {
		dc.logger.Error("failed to collect database-specific metrics",
			logger.Error(err),
		)
	}

	return dc.metrics
}

// Reset resets the collector
func (dc *DatabaseCollector) Reset() {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	dc.metrics = make(map[string]interface{})
	dc.queryStats = make(map[string]*QueryStats)
	dc.connectionStats = make(map[string]*DatabaseConnectionStats)
	dc.lastCollectionTime = time.Time{}
}

// =============================================================================
// CONNECTION STATISTICS COLLECTION
// =============================================================================

// collectConnectionStats collects database connection statistics
func (dc *DatabaseCollector) collectConnectionStats() error {
	if dc.dbManager == nil {
		return fmt.Errorf("database manager not available")
	}

	// Get all database connections from the manager
	connections := dc.dbManager.GetConnections()

	for name, conn := range connections {
		if err := dc.collectConnectionStatsForDB(name, conn); err != nil {
			if dc.logger != nil {
				dc.logger.Error("failed to collect connection stats for database",
					logger.String("database", name),
					logger.Error(err),
				)
			}
			continue
		}
	}

	return nil
}

// collectConnectionStatsForDB collects connection statistics for a specific database
func (dc *DatabaseCollector) collectConnectionStatsForDB(dbName string, conn common.Connection) error {
	// Get SQL database handle if available
	sqlDB, err := dc.getSQLDB(conn)
	if err != nil {
		return fmt.Errorf("failed to get SQL database handle: %w", err)
	}

	// Get database statistics
	stats := sqlDB.Stats()

	connectionStats := &DatabaseConnectionStats{
		ActiveConnections: stats.OpenConnections,
		IdleConnections:   stats.Idle,
		TotalConnections:  stats.OpenConnections,
		MaxConnections:    stats.MaxOpenConnections,
		WaitingQueries:    int(stats.WaitCount),
		ConnectionErrors:  stats.MaxIdleClosed + stats.MaxLifetimeClosed,
		LastUpdated:       time.Now(),
		ConnectionTime:    stats.WaitDuration,
	}

	dc.connectionStats[dbName] = connectionStats

	// Add metrics to collection
	prefix := fmt.Sprintf("common.%s.connections", dbName)
	dc.metrics[prefix+".active"] = connectionStats.ActiveConnections
	dc.metrics[prefix+".idle"] = connectionStats.IdleConnections
	dc.metrics[prefix+".total"] = connectionStats.TotalConnections
	dc.metrics[prefix+".max"] = connectionStats.MaxConnections
	dc.metrics[prefix+".waiting"] = connectionStats.WaitingQueries
	dc.metrics[prefix+".errors"] = connectionStats.ConnectionErrors
	dc.metrics[prefix+".wait_duration"] = connectionStats.ConnectionTime.Seconds()

	// Connection pool utilization
	if connectionStats.MaxConnections > 0 {
		utilization := float64(connectionStats.ActiveConnections) / float64(connectionStats.MaxConnections) * 100
		dc.metrics[prefix+".utilization"] = utilization
	}

	return nil
}

// getSQLDB extracts SQL database handle from connection
func (dc *DatabaseCollector) getSQLDB(conn common.Connection) (*sql.DB, error) {
	// This is a simplified implementation
	// In a real implementation, we would need to handle different database types
	// and extract the underlying *sql.DB handle

	// For now, return a placeholder - this would be implemented based on
	// the actual common.Connection interface implementation
	return nil, fmt.Errorf("SQL database handle extraction not implemented")
}

// =============================================================================
// QUERY STATISTICS COLLECTION
// =============================================================================

// collectQueryStats collects database query statistics
func (dc *DatabaseCollector) collectQueryStats() error {
	// Calculate aggregate query statistics
	for dbName, stats := range dc.queryStats {
		prefix := fmt.Sprintf("common.%s.queries", dbName)

		dc.metrics[prefix+".count"] = stats.QueryCount
		dc.metrics[prefix+".total_duration"] = stats.TotalDuration.Seconds()
		dc.metrics[prefix+".average_duration"] = stats.AverageDuration.Seconds()
		dc.metrics[prefix+".min_duration"] = stats.MinDuration.Seconds()
		dc.metrics[prefix+".max_duration"] = stats.MaxDuration.Seconds()
		dc.metrics[prefix+".error_count"] = stats.ErrorCount
		dc.metrics[prefix+".slow_query_count"] = stats.SlowQueryCount

		// Calculate error rate
		if stats.QueryCount > 0 {
			errorRate := float64(stats.ErrorCount) / float64(stats.QueryCount) * 100
			dc.metrics[prefix+".error_rate"] = errorRate
		}

		// Calculate slow query rate
		if stats.QueryCount > 0 {
			slowQueryRate := float64(stats.SlowQueryCount) / float64(stats.QueryCount) * 100
			dc.metrics[prefix+".slow_query_rate"] = slowQueryRate
		}
	}

	return nil
}

// RecordQuery records a query execution for metrics collection
func (dc *DatabaseCollector) RecordQuery(dbName, query string, duration time.Duration, err error) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if !dc.enabled {
		return
	}

	// Get or create query stats for this database
	stats, exists := dc.queryStats[dbName]
	if !exists {
		stats = &QueryStats{
			MinDuration: time.Hour, // Initialize with high value
		}
		dc.queryStats[dbName] = stats
	}

	// Update statistics
	stats.QueryCount++
	stats.TotalDuration += duration
	stats.AverageDuration = stats.TotalDuration / time.Duration(stats.QueryCount)
	stats.LastExecuted = time.Now()

	// Update min/max duration
	if duration < stats.MinDuration {
		stats.MinDuration = duration
	}
	if duration > stats.MaxDuration {
		stats.MaxDuration = duration
	}

	// Record error if present
	if err != nil {
		stats.ErrorCount++
	}

	// Record slow query
	if duration > time.Second*1 { // Default slow query threshold
		stats.SlowQueryCount++

		if dc.logger != nil {
			dc.logger.Warn("slow query detected",
				logger.String("database", dbName),
				logger.String("query", query),
				logger.Duration("duration", duration),
				logger.Error(err),
			)
		}
	}
}

// =============================================================================
// DATABASE-SPECIFIC METRICS COLLECTION
// =============================================================================

// collectDatabaseSpecificMetrics collects database-specific metrics
func (dc *DatabaseCollector) collectDatabaseSpecificMetrics() error {
	if dc.dbManager == nil {
		return fmt.Errorf("database manager not available")
	}

	connections := dc.dbManager.GetConnections()

	for name, conn := range connections {
		if err := dc.collectDatabaseMetrics(name, conn); err != nil {
			if dc.logger != nil {
				dc.logger.Error("failed to collect database metrics",
					logger.String("database", name),
					logger.Error(err),
				)
			}
			continue
		}
	}

	return nil
}

// collectDatabaseMetrics collects metrics for a specific database
func (dc *DatabaseCollector) collectDatabaseMetrics(dbName string, conn common.Connection) error {
	// Get database type
	dbType := conn.Type()
	prefix := fmt.Sprintf("common.%s", dbName)

	// Common metrics
	dc.metrics[prefix+".type"] = dbType
	dc.metrics[prefix+".status"] = dc.getDatabaseStatus(conn)
	dc.metrics[prefix+".last_ping"] = dc.getLastPingTime(conn)

	// Database-specific metrics based on type
	switch dbType {
	case "postgres":
		return dc.collectPostgresMetrics(dbName, conn)
	case "mysql":
		return dc.collectMySQLMetrics(dbName, conn)
	case "mongodb":
		return dc.collectMongoDBMetrics(dbName, conn)
	case "redis":
		return dc.collectRedisMetrics(dbName, conn)
	default:
		// Generic metrics for unknown database types
		return dc.collectGenericMetrics(dbName, conn)
	}
}

// collectPostgresMetrics collects PostgreSQL-specific metrics
func (dc *DatabaseCollector) collectPostgresMetrics(dbName string, conn common.Connection) error {
	prefix := fmt.Sprintf("common.%s.postgres", dbName)

	// Placeholder for PostgreSQL-specific metrics
	// In a real implementation, this would query PostgreSQL system tables
	dc.metrics[prefix+".version"] = "unknown"
	dc.metrics[prefix+".database_size"] = 0
	dc.metrics[prefix+".active_sessions"] = 0
	dc.metrics[prefix+".locked_queries"] = 0
	dc.metrics[prefix+".cache_hit_ratio"] = 0.0

	return nil
}

// collectMySQLMetrics collects MySQL-specific metrics
func (dc *DatabaseCollector) collectMySQLMetrics(dbName string, conn common.Connection) error {
	prefix := fmt.Sprintf("common.%s.mysql", dbName)

	// Placeholder for MySQL-specific metrics
	dc.metrics[prefix+".version"] = "unknown"
	dc.metrics[prefix+".database_size"] = 0
	dc.metrics[prefix+".threads_running"] = 0
	dc.metrics[prefix+".threads_connected"] = 0
	dc.metrics[prefix+".slow_queries"] = 0

	return nil
}

// collectMongoDBMetrics collects MongoDB-specific metrics
func (dc *DatabaseCollector) collectMongoDBMetrics(dbName string, conn common.Connection) error {
	prefix := fmt.Sprintf("common.%s.mongodb", dbName)

	// Placeholder for MongoDB-specific metrics
	dc.metrics[prefix+".version"] = "unknown"
	dc.metrics[prefix+".database_size"] = 0
	dc.metrics[prefix+".collection_count"] = 0
	dc.metrics[prefix+".document_count"] = 0
	dc.metrics[prefix+".index_count"] = 0

	return nil
}

// collectRedisMetrics collects Redis-specific metrics
func (dc *DatabaseCollector) collectRedisMetrics(dbName string, conn common.Connection) error {
	prefix := fmt.Sprintf("common.%s.redis", dbName)

	// Placeholder for Redis-specific metrics
	dc.metrics[prefix+".version"] = "unknown"
	dc.metrics[prefix+".memory_usage"] = 0
	dc.metrics[prefix+".key_count"] = 0
	dc.metrics[prefix+".expired_keys"] = 0
	dc.metrics[prefix+".evicted_keys"] = 0

	return nil
}

// collectGenericMetrics collects generic database metrics
func (dc *DatabaseCollector) collectGenericMetrics(dbName string, conn common.Connection) error {
	prefix := fmt.Sprintf("common.%s.generic", dbName)

	// Basic metrics available for all database types
	dc.metrics[prefix+".connected"] = dc.isConnected(conn)
	dc.metrics[prefix+".last_error"] = dc.getLastError(conn)

	return nil
}

// =============================================================================
// HELPER METHODS
// =============================================================================

// getDatabaseStatus returns the database connection status
func (dc *DatabaseCollector) getDatabaseStatus(conn common.Connection) string {
	if dc.isConnected(conn) {
		return "connected"
	}
	return "disconnected"
}

// isConnected checks if the database connection is active
func (dc *DatabaseCollector) isConnected(conn common.Connection) bool {
	// This would be implemented based on the actual common.Connection interface
	// For now, we'll assume all connections are active
	return true
}

// getLastPingTime returns the last ping time for the database
func (dc *DatabaseCollector) getLastPingTime(conn common.Connection) time.Time {
	// This would be implemented based on the actual common.Connection interface
	// For now, return current time
	return time.Now()
}

// getLastError returns the last error for the database connection
func (dc *DatabaseCollector) getLastError(conn common.Connection) string {
	// This would be implemented based on the actual common.Connection interface
	// For now, return empty string
	return ""
}

// =============================================================================
// CONFIGURATION METHODS
// =============================================================================

// Enable enables the collector
func (dc *DatabaseCollector) Enable() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.enabled = true
}

// Disable disables the collector
func (dc *DatabaseCollector) Disable() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.enabled = false
}

// IsEnabled returns whether the collector is enabled
func (dc *DatabaseCollector) IsEnabled() bool {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.enabled
}

// SetInterval sets the collection interval
func (dc *DatabaseCollector) SetInterval(interval time.Duration) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.interval = interval
}

// GetInterval returns the collection interval
func (dc *DatabaseCollector) GetInterval() time.Duration {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.interval
}

// GetStats returns collector statistics
func (dc *DatabaseCollector) GetStats() map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["enabled"] = dc.enabled
	stats["interval"] = dc.interval
	stats["last_collection"] = dc.lastCollectionTime
	stats["database_count"] = len(dc.connectionStats)
	stats["total_queries"] = dc.getTotalQueries()
	stats["total_errors"] = dc.getTotalErrors()

	return stats
}

// getTotalQueries returns the total number of queries across all databases
func (dc *DatabaseCollector) getTotalQueries() int64 {
	var total int64
	for _, stats := range dc.queryStats {
		total += stats.QueryCount
	}
	return total
}

// getTotalErrors returns the total number of errors across all databases
func (dc *DatabaseCollector) getTotalErrors() int64 {
	var total int64
	for _, stats := range dc.queryStats {
		total += stats.ErrorCount
	}
	return total
}

// =============================================================================
// METRICS INTERFACE INTEGRATION
// =============================================================================

// CreateMetricsWrapper creates a wrapper that integrates with the metrics system
func (dc *DatabaseCollector) CreateMetricsWrapper(metricsCollector metrics.MetricsCollector) *DatabaseMetricsWrapper {
	return &DatabaseMetricsWrapper{
		collector:        dc,
		metricsCollector: metricsCollector,
		queryDuration:    metricsCollector.Histogram("database_query_duration_seconds", "database", "query_type"),
		queryCount:       metricsCollector.Counter("database_query_total", "database", "status"),
		connectionCount:  metricsCollector.Gauge("database_connections", "database", "state"),
	}
}

// DatabaseMetricsWrapper wraps the database collector with metrics integration
type DatabaseMetricsWrapper struct {
	collector        *DatabaseCollector
	metricsCollector metrics.MetricsCollector
	queryDuration    metrics.Histogram
	queryCount       metrics.Counter
	connectionCount  metrics.Gauge
}

// RecordQueryMetrics records query metrics using the metrics system
func (dmw *DatabaseMetricsWrapper) RecordQueryMetrics(dbName, queryType string, duration time.Duration, err error) {
	// Record in the database collector
	dmw.collector.RecordQuery(dbName, queryType, duration, err)

	// Record in the metrics system
	dmw.queryDuration.Observe(duration.Seconds())

	status := "success"
	if err != nil {
		status = "error"
	}
	fmt.Println("status", status)
	dmw.queryCount.Add(1)
}

// UpdateConnectionMetrics updates connection metrics
func (dmw *DatabaseMetricsWrapper) UpdateConnectionMetrics(dbName string, active, idle, total int) {
	// Update in the metrics system
	dmw.connectionCount.Set(float64(active))
}
