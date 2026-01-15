package middleware

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// =============================================================================
// DATABASE METRICS MIDDLEWARE
// =============================================================================

// DatabaseMetricsMiddleware provides automatic database metrics collection.
type DatabaseMetricsMiddleware struct {
	name      string
	collector shared.Metrics
	config    *DatabaseMetricsConfig
	logger    logger.Logger
	started   bool

	// Connection metrics
	connectionsOpen    shared.Gauge
	connectionsUsed    shared.Gauge
	connectionsIdle    shared.Gauge
	connectionsMax     shared.Gauge
	connectionErrors   shared.Counter
	connectionDuration shared.Timer

	// Query metrics
	queriesTotal       shared.Counter
	queryDuration      shared.Histogram
	queryErrors        shared.Counter
	slowQueries        shared.Counter
	queryRowsAffected  shared.Histogram
	queryRowsReturned  shared.Histogram
	queriesByTable     map[string]shared.Counter
	queriesByOperation map[string]shared.Counter

	// Transaction metrics
	transactionsTotal    shared.Counter
	transactionDuration  shared.Histogram
	transactionErrors    shared.Counter
	transactionRollbacks shared.Counter
	transactionCommits   shared.Counter

	// Migration metrics
	migrationsTotal   shared.Counter
	migrationDuration shared.Histogram
	migrationErrors   shared.Counter
}

// DatabaseMetricsConfig contains configuration for database metrics middleware.
type DatabaseMetricsConfig struct {
	Enabled                 bool          `json:"enabled"                   yaml:"enabled"`
	CollectConnectionStats  bool          `json:"collect_connection_stats"  yaml:"collect_connection_stats"`
	CollectQueryStats       bool          `json:"collect_query_stats"       yaml:"collect_query_stats"`
	CollectTransactionStats bool          `json:"collect_transaction_stats" yaml:"collect_transaction_stats"`
	CollectMigrationStats   bool          `json:"collect_migration_stats"   yaml:"collect_migration_stats"`
	CollectSlowQueries      bool          `json:"collect_slow_queries"      yaml:"collect_slow_queries"`
	SlowQueryThreshold      time.Duration `json:"slow_query_threshold"      yaml:"slow_query_threshold"`
	GroupByTable            bool          `json:"group_by_table"            yaml:"group_by_table"`
	GroupByOperation        bool          `json:"group_by_operation"        yaml:"group_by_operation"`
	MaxTableNames           int           `json:"max_table_names"           yaml:"max_table_names"`
	NormalizeQueries        bool          `json:"normalize_queries"         yaml:"normalize_queries"`
	LogSlowQueries          bool          `json:"log_slow_queries"          yaml:"log_slow_queries"`
	StatsInterval           time.Duration `json:"stats_interval"            yaml:"stats_interval"`
}

// DefaultDatabaseMetricsConfig returns default configuration.
func DefaultDatabaseMetricsConfig() *DatabaseMetricsConfig {
	return &DatabaseMetricsConfig{
		Enabled:                 true,
		CollectConnectionStats:  true,
		CollectQueryStats:       true,
		CollectTransactionStats: true,
		CollectMigrationStats:   true,
		CollectSlowQueries:      true,
		SlowQueryThreshold:      time.Second,
		GroupByTable:            true,
		GroupByOperation:        true,
		MaxTableNames:           100,
		NormalizeQueries:        true,
		LogSlowQueries:          true,
		StatsInterval:           time.Second * 30,
	}
}

// NewDatabaseMetricsMiddleware creates a new database metrics middleware.
func NewDatabaseMetricsMiddleware(collector shared.Metrics) *DatabaseMetricsMiddleware {
	return NewDatabaseMetricsMiddlewareWithConfig(collector, DefaultDatabaseMetricsConfig())
}

// NewDatabaseMetricsMiddlewareWithConfig creates a new database metrics middleware with configuration.
func NewDatabaseMetricsMiddlewareWithConfig(collector shared.Metrics, config *DatabaseMetricsConfig) *DatabaseMetricsMiddleware {
	return &DatabaseMetricsMiddleware{
		name:               "database-metrics",
		collector:          collector,
		config:             config,
		queriesByTable:     make(map[string]shared.Counter),
		queriesByOperation: make(map[string]shared.Counter),
	}
}

// =============================================================================
// SERVICE IMPLEMENTATION
// =============================================================================

// Name returns the middleware name.
func (m *DatabaseMetricsMiddleware) Name() string {
	return m.name
}

// Dependencies returns the middleware dependencies.
func (m *DatabaseMetricsMiddleware) Dependencies() []string {
	return []string{}
}

// OnStart is called when the middleware starts.
func (m *DatabaseMetricsMiddleware) Start(ctx context.Context) error {
	if m.started {
		return errors.ErrServiceAlreadyExists(m.name)
	}

	// Initialize metrics
	m.initializeMetrics()

	m.started = true

	if m.logger != nil {
		m.logger.Info("Database metrics middleware started",
			logger.String("name", m.name),
			logger.Bool("enabled", m.config.Enabled),
			logger.Bool("collect_connection_stats", m.config.CollectConnectionStats),
			logger.Bool("collect_query_stats", m.config.CollectQueryStats),
			logger.Bool("collect_transaction_stats", m.config.CollectTransactionStats),
			logger.Duration("slow_query_threshold", m.config.SlowQueryThreshold),
		)
	}

	return nil
}

// OnStop is called when the middleware stops.
func (m *DatabaseMetricsMiddleware) Stop(ctx context.Context) error {
	if !m.started {
		return errors.ErrServiceNotFound(m.name)
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("Database metrics middleware stopped", logger.String("name", m.name))
	}

	return nil
}

// OnHealthCheck is called to check middleware health.
func (m *DatabaseMetricsMiddleware) OnHealthCheck(ctx context.Context) error {
	if !m.started {
		return errors.ErrHealthCheckFailed(m.name, errors.New("middleware not started"))
	}

	return nil
}

// =============================================================================
// METRICS COLLECTION METHODS
// =============================================================================

// RecordConnection records connection metrics.
func (m *DatabaseMetricsMiddleware) RecordConnection(dbName string, stats sql.DBStats) {
	if !m.config.Enabled || !m.config.CollectConnectionStats {
		return
	}

	// Record connection pool stats
	m.connectionsOpen.Set(float64(stats.OpenConnections))
	m.connectionsUsed.Set(float64(stats.InUse))
	m.connectionsIdle.Set(float64(stats.Idle))
	m.connectionsMax.Set(float64(stats.MaxOpenConnections))

	if m.logger != nil {
		m.logger.Debug("Database connection stats recorded",
			logger.String("database", dbName),
			logger.Int("open_connections", stats.OpenConnections),
			logger.Int("in_use", stats.InUse),
			logger.Int("idle", stats.Idle),
			logger.Int("max_open", stats.MaxOpenConnections),
		)
	}
}

// RecordConnectionError records a connection error.
func (m *DatabaseMetricsMiddleware) RecordConnectionError(dbName string, err error) {
	if !m.config.Enabled || !m.config.CollectConnectionStats {
		return
	}

	m.connectionErrors.Inc()

	if m.logger != nil {
		m.logger.Error("Database connection error",
			logger.String("database", dbName),
			logger.Error(err),
		)
	}
}

// RecordQuery records query metrics.
func (m *DatabaseMetricsMiddleware) RecordQuery(query QueryInfo) {
	if !m.config.Enabled || !m.config.CollectQueryStats {
		return
	}

	// Record basic query metrics
	m.queriesTotal.Inc()
	m.queryDuration.Observe(query.Duration.Seconds())

	if query.RowsAffected >= 0 {
		m.queryRowsAffected.Observe(float64(query.RowsAffected))
	}

	if query.RowsReturned >= 0 {
		m.queryRowsReturned.Observe(float64(query.RowsReturned))
	}

	// Record slow query
	if m.config.CollectSlowQueries && query.Duration > m.config.SlowQueryThreshold {
		m.slowQueries.Inc()

		if m.config.LogSlowQueries && m.logger != nil {
			m.logger.Warn("Slow query detected",
				logger.String("database", query.Database),
				logger.String("table", query.Table),
				logger.String("operation", query.Operation),
				logger.Duration("duration", query.Duration),
				logger.String("query", query.Query),
			)
		}
	}

	// Record error if present
	if query.Error != nil {
		m.queryErrors.Inc()

		if m.logger != nil {
			m.logger.Error("Database query error",
				logger.String("database", query.Database),
				logger.String("table", query.Table),
				logger.String("operation", query.Operation),
				logger.Duration("duration", query.Duration),
				logger.String("query", query.Query),
				logger.Error(query.Error),
			)
		}
	}

	// Record grouped metrics
	if m.config.GroupByTable && query.Table != "" {
		m.recordTableMetric(query.Table)
	}

	if m.config.GroupByOperation && query.Operation != "" {
		m.recordOperationMetric(query.Operation)
	}
}

// RecordTransaction records transaction metrics.
func (m *DatabaseMetricsMiddleware) RecordTransaction(tx TransactionInfo) {
	if !m.config.Enabled || !m.config.CollectTransactionStats {
		return
	}

	m.transactionsTotal.Inc()
	m.transactionDuration.Observe(tx.Duration.Seconds())

	if tx.Error != nil {
		m.transactionErrors.Inc()

		if tx.Rollback {
			m.transactionRollbacks.Inc()
		}

		if m.logger != nil {
			m.logger.Error("Database transaction error",
				logger.String("database", tx.Database),
				logger.Duration("duration", tx.Duration),
				logger.Bool("rollback", tx.Rollback),
				logger.Error(tx.Error),
			)
		}
	} else {
		if tx.Rollback {
			m.transactionRollbacks.Inc()
		} else {
			m.transactionCommits.Inc()
		}
	}

	if m.logger != nil {
		m.logger.Debug("Database transaction completed",
			logger.String("database", tx.Database),
			logger.Duration("duration", tx.Duration),
			logger.Bool("rollback", tx.Rollback),
			logger.Bool("success", tx.Error == nil),
		)
	}
}

// RecordMigration records migration metrics.
func (m *DatabaseMetricsMiddleware) RecordMigration(migration MigrationInfo) {
	if !m.config.Enabled || !m.config.CollectMigrationStats {
		return
	}

	m.migrationsTotal.Inc()
	m.migrationDuration.Observe(migration.Duration.Seconds())

	if migration.Error != nil {
		m.migrationErrors.Inc()

		if m.logger != nil {
			m.logger.Error("Database migration error",
				logger.String("database", migration.Database),
				logger.String("migration", migration.Name),
				logger.String("direction", migration.Direction),
				logger.Duration("duration", migration.Duration),
				logger.Error(migration.Error),
			)
		}
	} else if m.logger != nil {
		m.logger.Info("Database migration completed",
			logger.String("database", migration.Database),
			logger.String("migration", migration.Name),
			logger.String("direction", migration.Direction),
			logger.Duration("duration", migration.Duration),
		)
	}
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// initializeMetrics initializes the metrics.
func (m *DatabaseMetricsMiddleware) initializeMetrics() {
	// Connection metrics
	if m.config.CollectConnectionStats {
		m.connectionsOpen = m.collector.Gauge("database_connections_open", metrics.WithLabel("database", m.name))
		m.connectionsUsed = m.collector.Gauge("database_connections_used", metrics.WithLabel("database", m.name))
		m.connectionsIdle = m.collector.Gauge("database_connections_idle", metrics.WithLabel("database", m.name))
		m.connectionsMax = m.collector.Gauge("database_connections_max", metrics.WithLabel("database", m.name))
		m.connectionErrors = m.collector.Counter("database_connection_errors_total", metrics.WithLabel("database", m.name))
		m.connectionDuration = m.collector.Timer("database_connection_duration", metrics.WithLabel("database", m.name))
	}

	// Query metrics
	if m.config.CollectQueryStats {
		m.queriesTotal = m.collector.Counter("database_queries_total", metrics.WithLabel("database", m.name), metrics.WithLabel("table", m.name), metrics.WithLabel("operation", m.name))
		m.queryDuration = m.collector.Histogram("database_query_duration_seconds", metrics.WithLabel("database", m.name), metrics.WithLabel("table", m.name), metrics.WithLabel("operation", m.name))
		m.queryErrors = m.collector.Counter("database_query_errors_total", metrics.WithLabel("database", m.name), metrics.WithLabel("table", m.name), metrics.WithLabel("operation", m.name))
		m.queryRowsAffected = m.collector.Histogram("database_query_rows_affected", metrics.WithLabel("database", m.name), metrics.WithLabel("table", m.name), metrics.WithLabel("operation", m.name))
		m.queryRowsReturned = m.collector.Histogram("database_query_rows_returned", metrics.WithLabel("database", m.name), metrics.WithLabel("table", m.name), metrics.WithLabel("operation", m.name))

		if m.config.CollectSlowQueries {
			m.slowQueries = m.collector.Counter("database_slow_queries_total", metrics.WithLabel("database", m.name), metrics.WithLabel("table", m.name), metrics.WithLabel("operation", m.name))
		}
	}

	// Transaction metrics
	if m.config.CollectTransactionStats {
		m.transactionsTotal = m.collector.Counter("database_transactions_total", metrics.WithLabel("database", m.name))
		m.transactionDuration = m.collector.Histogram("database_transaction_duration_seconds", metrics.WithLabel("database", m.name))
		m.transactionErrors = m.collector.Counter("database_transaction_errors_total", metrics.WithLabel("database", m.name))
		m.transactionRollbacks = m.collector.Counter("database_transaction_rollbacks_total", metrics.WithLabel("database", m.name))
		m.transactionCommits = m.collector.Counter("database_transaction_commits_total", metrics.WithLabel("database", m.name))
	}

	// Migration metrics
	if m.config.CollectMigrationStats {
		m.migrationsTotal = m.collector.Counter("database_migrations_total", metrics.WithLabel("database", m.name), metrics.WithLabel("direction", m.name))
		m.migrationDuration = m.collector.Histogram("database_migration_duration_seconds", metrics.WithLabel("database", m.name), metrics.WithLabel("direction", m.name))
		m.migrationErrors = m.collector.Counter("database_migration_errors_total", metrics.WithLabel("database", m.name), metrics.WithLabel("direction", m.name))
	}
}

// recordTableMetric records a table-specific metric.
func (m *DatabaseMetricsMiddleware) recordTableMetric(table string) {
	if len(m.queriesByTable) >= m.config.MaxTableNames {
		return // Prevent unlimited metric creation
	}

	counter, exists := m.queriesByTable[table]
	if !exists {
		counter = m.collector.Counter("database_queries_by_table", metrics.WithLabel("database", m.name), metrics.WithLabel("table", table))
		m.queriesByTable[table] = counter
	}

	counter.Inc()
}

// recordOperationMetric records an operation-specific metric.
func (m *DatabaseMetricsMiddleware) recordOperationMetric(operation string) {
	counter, exists := m.queriesByOperation[operation]
	if !exists {
		counter = m.collector.Counter("database_queries_by_operation", metrics.WithLabel("database", m.name), metrics.WithLabel("operation", operation))
		m.queriesByOperation[operation] = counter
	}

	counter.Inc()
}

// SetLogger sets the logger.
func (m *DatabaseMetricsMiddleware) SetLogger(logger logger.Logger) {
	m.logger = logger
}

// =============================================================================
// SUPPORTING TYPES
// =============================================================================

// QueryInfo contains information about a database query.
type QueryInfo struct {
	Database     string
	Table        string
	Operation    string
	Query        string
	Duration     time.Duration
	RowsAffected int64
	RowsReturned int64
	Error        error
}

// TransactionInfo contains information about a database transaction.
type TransactionInfo struct {
	Database string
	Duration time.Duration
	Rollback bool
	Error    error
}

// MigrationInfo contains information about a database migration.
type MigrationInfo struct {
	Database  string
	Name      string
	Direction string // "up" or "down"
	Duration  time.Duration
	Error     error
}

// =============================================================================
// DATABASE WRAPPER IMPLEMENTATIONS
// =============================================================================

// MetricsDB wraps a database connection with metrics collection.
type MetricsDB struct {
	*sql.DB

	middleware *DatabaseMetricsMiddleware
	dbName     string
}

// NewMetricsDB creates a new metrics-enabled database wrapper.
func NewMetricsDB(db *sql.DB, middleware *DatabaseMetricsMiddleware, dbName string) *MetricsDB {
	return &MetricsDB{
		DB:         db,
		middleware: middleware,
		dbName:     dbName,
	}
}

// Query executes a query with metrics collection.
func (db *MetricsDB) Query(query string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	rows, err := db.DB.QueryContext(context.Background(), query, args...)
	duration := time.Since(start)

	// Extract table and operation from query
	table, operation := parseQuery(query)

	// Record metrics
	queryInfo := QueryInfo{
		Database:     db.dbName,
		Table:        table,
		Operation:    operation,
		Query:        query,
		Duration:     duration,
		RowsAffected: -1, // Not available for Query
		RowsReturned: -1, // Would need to count rows
		Error:        err,
	}

	db.middleware.RecordQuery(queryInfo)

	return rows, err
}

// Exec executes a query with metrics collection.
func (db *MetricsDB) Exec(query string, args ...any) (sql.Result, error) {
	start := time.Now()
	result, err := db.DB.ExecContext(context.Background(), query, args...)
	duration := time.Since(start)

	// Extract table and operation from query
	table, operation := parseQuery(query)

	var rowsAffected int64 = -1

	if result != nil && err == nil {
		if affected, affectedErr := result.RowsAffected(); affectedErr == nil {
			rowsAffected = affected
		}
	}

	// Record metrics
	queryInfo := QueryInfo{
		Database:     db.dbName,
		Table:        table,
		Operation:    operation,
		Query:        query,
		Duration:     duration,
		RowsAffected: rowsAffected,
		RowsReturned: -1,
		Error:        err,
	}

	db.middleware.RecordQuery(queryInfo)

	return result, err
}

// Begin starts a transaction with metrics collection.
func (db *MetricsDB) Begin() (*MetricsTx, error) {
	start := time.Now()
	tx, err := db.DB.BeginTx(context.Background(), nil)
	duration := time.Since(start)

	if err != nil {
		db.middleware.RecordTransaction(TransactionInfo{
			Database: db.dbName,
			Duration: duration,
			Rollback: false,
			Error:    err,
		})

		return nil, err
	}

	return &MetricsTx{
		Tx:         tx,
		middleware: db.middleware,
		dbName:     db.dbName,
		startTime:  start,
	}, nil
}

// Stats returns database statistics and records metrics.
func (db *MetricsDB) Stats() sql.DBStats {
	stats := db.DB.Stats()
	db.middleware.RecordConnection(db.dbName, stats)

	return stats
}

// MetricsTx wraps a database transaction with metrics collection.
type MetricsTx struct {
	*sql.Tx

	middleware *DatabaseMetricsMiddleware
	dbName     string
	startTime  time.Time
}

// Commit commits the transaction with metrics collection.
func (tx *MetricsTx) Commit() error {
	err := tx.Tx.Commit()
	duration := time.Since(tx.startTime)

	tx.middleware.RecordTransaction(TransactionInfo{
		Database: tx.dbName,
		Duration: duration,
		Rollback: false,
		Error:    err,
	})

	return err
}

// Rollback rolls back the transaction with metrics collection.
func (tx *MetricsTx) Rollback() error {
	err := tx.Tx.Rollback()
	duration := time.Since(tx.startTime)

	tx.middleware.RecordTransaction(TransactionInfo{
		Database: tx.dbName,
		Duration: duration,
		Rollback: true,
		Error:    err,
	})

	return err
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// parseQuery extracts table and operation from a SQL query.
func parseQuery(query string) (table, operation string) {
	// Simple SQL parsing - in a real implementation, this would be more sophisticated
	query = strings.TrimSpace(strings.ToUpper(query))

	parts := strings.Fields(query)
	if len(parts) == 0 {
		return "", ""
	}

	operation = parts[0]

	// Extract table name based on operation
	switch operation {
	case "SELECT":
		// Look for FROM clause
		for i, part := range parts {
			if part == "FROM" && i+1 < len(parts) {
				table = parts[i+1]

				break
			}
		}
	case "INSERT":
		// Look for INTO clause
		for i, part := range parts {
			if part == "INTO" && i+1 < len(parts) {
				table = parts[i+1]

				break
			}
		}
	case "UPDATE":
		// Table name is usually the second part
		if len(parts) > 1 {
			table = parts[1]
		}
	case "DELETE":
		// Look for FROM clause
		for i, part := range parts {
			if part == "FROM" && i+1 < len(parts) {
				table = parts[i+1]

				break
			}
		}
	}

	// Clean up table name (remove schema prefix, quotes, etc.)
	if table != "" {
		table = strings.Trim(table, `"'`+"`")
		if dotIndex := strings.LastIndex(table, "."); dotIndex != -1 {
			table = table[dotIndex+1:]
		}
	}

	return table, operation
}

// CreateDatabaseMetricsMiddleware creates database metrics middleware.
func CreateDatabaseMetricsMiddleware(collector shared.Metrics) *DatabaseMetricsMiddleware {
	return NewDatabaseMetricsMiddleware(collector)
}

// CreateDatabaseMetricsMiddlewareWithConfig creates database metrics middleware with custom config.
func CreateDatabaseMetricsMiddlewareWithConfig(collector shared.Metrics, config *DatabaseMetricsConfig) *DatabaseMetricsMiddleware {
	return NewDatabaseMetricsMiddlewareWithConfig(collector, config)
}
