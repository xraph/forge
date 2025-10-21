package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/schema"

	// Import SQL drivers
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/internal/logger"
)

// SQLDatabase wraps Bun ORM for SQL databases
type SQLDatabase struct {
	name   string
	dbType DatabaseType
	config DatabaseConfig

	// Native access
	sqlDB *sql.DB // Raw database/sql connection
	bun   *bun.DB // Bun ORM instance

	logger  forge.Logger
	metrics forge.Metrics
}

// NewSQLDatabase creates a new SQL database instance
func NewSQLDatabase(config DatabaseConfig, logger forge.Logger, metrics forge.Metrics) (*SQLDatabase, error) {
	return &SQLDatabase{
		name:    config.Name,
		dbType:  config.Type,
		config:  config,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Open establishes the database connection
func (d *SQLDatabase) Open(ctx context.Context) error {
	// Open raw SQL connection
	sqlDB, err := sql.Open(d.driverName(), d.config.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(d.config.MaxOpenConns)
	sqlDB.SetMaxIdleConns(d.config.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(d.config.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(d.config.ConnMaxIdleTime)

	// Verify connection
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	d.sqlDB = sqlDB

	// Wrap with Bun ORM
	d.bun = bun.NewDB(sqlDB, d.dialect())

	// Add query hook for observability
	d.bun.AddQueryHook(d.queryHook())

	d.logger.Info("database opened",
		logger.String("name", d.name),
		logger.String("type", string(d.dbType)),
		logger.Int("max_open", d.config.MaxOpenConns),
	)

	return nil
}

// Close closes the database connection
func (d *SQLDatabase) Close(ctx context.Context) error {
	if d.bun != nil {
		return d.bun.Close()
	}
	return nil
}

// Ping checks database connectivity
func (d *SQLDatabase) Ping(ctx context.Context) error {
	if d.sqlDB == nil {
		return fmt.Errorf("database not opened")
	}
	return d.sqlDB.PingContext(ctx)
}

// Name returns the database name
func (d *SQLDatabase) Name() string {
	return d.name
}

// Type returns the database type
func (d *SQLDatabase) Type() DatabaseType {
	return d.dbType
}

// Driver returns the raw *sql.DB for native driver access
func (d *SQLDatabase) Driver() interface{} {
	return d.sqlDB
}

// DB returns the raw *sql.DB
func (d *SQLDatabase) DB() *sql.DB {
	return d.sqlDB
}

// Bun returns the Bun ORM instance
func (d *SQLDatabase) Bun() *bun.DB {
	return d.bun
}

// Health returns the health status
func (d *SQLDatabase) Health(ctx context.Context) HealthStatus {
	start := time.Now()

	status := HealthStatus{
		CheckedAt: time.Now(),
	}

	if err := d.Ping(ctx); err != nil {
		status.Healthy = false
		status.Message = err.Error()
		return status
	}

	status.Healthy = true
	status.Message = "ok"
	status.Latency = time.Since(start)

	return status
}

// Stats returns connection pool statistics
func (d *SQLDatabase) Stats() DatabaseStats {
	if d.sqlDB == nil {
		return DatabaseStats{}
	}

	stats := d.sqlDB.Stats()

	return DatabaseStats{
		OpenConnections:   stats.OpenConnections,
		InUse:             stats.InUse,
		Idle:              stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration,
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
	}
}

// Transaction executes a function in a SQL transaction
func (d *SQLDatabase) Transaction(ctx context.Context, fn func(tx bun.Tx) error) error {
	return d.bun.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(tx)
	})
}

// TransactionWithOptions executes a function in a SQL transaction with options
func (d *SQLDatabase) TransactionWithOptions(ctx context.Context, opts *sql.TxOptions, fn func(tx bun.Tx) error) error {
	return d.bun.RunInTx(ctx, opts, func(ctx context.Context, tx bun.Tx) error {
		return fn(tx)
	})
}

// Helper: Get driver name
func (d *SQLDatabase) driverName() string {
	switch d.dbType {
	case TypePostgres:
		return "postgres"
	case TypeMySQL:
		return "mysql"
	case TypeSQLite:
		return "sqlite3"
	default:
		return string(d.dbType)
	}
}

// Helper: Get Bun dialect
func (d *SQLDatabase) dialect() schema.Dialect {
	switch d.dbType {
	case TypePostgres:
		return pgdialect.New()
	case TypeMySQL:
		return mysqldialect.New()
	case TypeSQLite:
		return sqlitedialect.New()
	default:
		return pgdialect.New()
	}
}

// Helper: Query hook for observability
func (d *SQLDatabase) queryHook() bun.QueryHook {
	return &QueryHook{
		logger:  d.logger,
		metrics: d.metrics,
		dbName:  d.name,
	}
}

// QueryHook provides observability for Bun queries
type QueryHook struct {
	logger  forge.Logger
	metrics forge.Metrics
	dbName  string
}

// BeforeQuery is called before query execution
func (h *QueryHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery is called after query execution
func (h *QueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	duration := time.Since(event.StartTime)

	// Log slow queries
	if duration > 100*time.Millisecond {
		h.logger.Warn("slow query detected",
			logger.String("db", h.dbName),
			logger.String("query", event.Query),
			logger.Duration("duration", duration),
		)
	}

	// Record metrics
	if h.metrics != nil {
		h.metrics.Histogram("db_query_duration",
			"db", h.dbName,
			"operation", event.Operation(),
		).Observe(duration.Seconds())

		if event.Err != nil && event.Err != sql.ErrNoRows {
			h.metrics.Counter("db_query_errors",
				"db", h.dbName,
				"operation", event.Operation(),
			).Inc()

			h.logger.Error("query error",
				logger.String("db", h.dbName),
				logger.String("query", event.Query),
				logger.Error(event.Err),
			)
		}
	}
}
