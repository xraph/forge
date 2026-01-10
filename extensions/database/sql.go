package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/schema"

	// Import SQL drivers.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
)

// SQLDatabase wraps Bun ORM for SQL databases.
type SQLDatabase struct {
	name   string
	dbType DatabaseType
	config DatabaseConfig

	// Native access
	sqlDB *sql.DB // Raw database/sql connection
	bun   *bun.DB // Bun ORM instance

	// Connection state
	state atomic.Int32

	logger  forge.Logger
	metrics forge.Metrics
}

// NewSQLDatabase creates a new SQL database instance.
func NewSQLDatabase(config DatabaseConfig, logger forge.Logger, metrics forge.Metrics) (*SQLDatabase, error) {
	db := &SQLDatabase{
		name:    config.Name,
		dbType:  config.Type,
		config:  config,
		logger:  logger,
		metrics: metrics,
	}
	db.state.Store(int32(StateDisconnected))

	return db, nil
}

// Open establishes the database connection with retry logic.
func (d *SQLDatabase) Open(ctx context.Context) error {
	d.state.Store(int32(StateConnecting))

	var lastErr error

	for attempt := 0; attempt <= d.config.MaxRetries; attempt++ {
		if attempt > 0 {
			d.state.Store(int32(StateReconnecting))
			// Apply exponential backoff with jitter
			delay := min(d.config.RetryDelay*time.Duration(1<<uint(attempt-1)), 30*time.Second)

			d.logger.Info("retrying database connection",
				logger.String("name", d.name),
				logger.Int("attempt", attempt+1),
				logger.Int("max_attempts", d.config.MaxRetries+1),
				logger.Duration("delay", delay),
			)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				d.state.Store(int32(StateError))

				return ctx.Err()
			}
		}

		if err := d.openAttempt(ctx); err != nil {
			lastErr = err
			d.logger.Warn("database connection attempt failed",
				logger.String("name", d.name),
				logger.Int("attempt", attempt+1),
				logger.Error(err),
			)

			continue
		}

		d.state.Store(int32(StateConnected))
		d.logger.Info("database opened",
			logger.String("name", d.name),
			logger.String("type", string(d.dbType)),
			logger.String("dsn", MaskDSN(d.config.DSN, d.dbType)),
			logger.Int("max_open", d.config.MaxOpenConns),
			logger.Int("attempts", attempt+1),
		)

		return nil
	}

	d.state.Store(int32(StateError))

	return ErrConnectionFailed(d.name, d.dbType, fmt.Errorf("failed after %d attempts: %w", d.config.MaxRetries+1, lastErr))
}

// openAttempt performs a single connection attempt.
func (d *SQLDatabase) openAttempt(ctx context.Context) error {
	// Add timeout for connection
	connectCtx := ctx

	if d.config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc

		connectCtx, cancel = context.WithTimeout(ctx, d.config.ConnectionTimeout)
		defer cancel()
	}

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
	if err := sqlDB.PingContext(connectCtx); err != nil {
		sqlDB.Close()

		return fmt.Errorf("failed to ping database: %w", err)
	}

	d.sqlDB = sqlDB

	// Wrap with Bun ORM
	d.bun = bun.NewDB(sqlDB, d.dialect())

	// Add query hook for observability
	hook := d.queryHook()
	if d.config.AutoExplainThreshold > 0 {
		// Use enhanced observability hook with auto-explain
		hook = NewObservabilityQueryHook(
			d.logger,
			d.metrics,
			d.name,
			d.dbType,
			d.config.SlowQueryThreshold,
			d.config.DisableSlowQueryLogging,
		).WithAutoExplain(d.config.AutoExplainThreshold)
	}

	d.bun.AddQueryHook(hook)

	return nil
}

// Close closes the database connection.
func (d *SQLDatabase) Close(ctx context.Context) error {
	if d.bun != nil {
		err := d.bun.Close()
		if err != nil {
			d.state.Store(int32(StateError))

			return ErrConnectionFailed(d.name, d.dbType, fmt.Errorf("failed to close: %w", err))
		}

		d.state.Store(int32(StateDisconnected))
		d.logger.Info("database closed", logger.String("name", d.name))

		return nil
	}

	return nil
}

// Ping checks database connectivity.
func (d *SQLDatabase) Ping(ctx context.Context) error {
	if d.sqlDB == nil {
		return ErrDatabaseNotOpened(d.name)
	}

	// Add timeout if configured
	pingCtx := ctx

	if d.config.ConnectionTimeout > 0 {
		var cancel context.CancelFunc

		pingCtx, cancel = context.WithTimeout(ctx, d.config.ConnectionTimeout)
		defer cancel()
	}

	return d.sqlDB.PingContext(pingCtx)
}

// IsOpen returns whether the database is connected.
func (d *SQLDatabase) IsOpen() bool {
	return d.State() == StateConnected
}

// State returns the current connection state.
func (d *SQLDatabase) State() ConnectionState {
	return ConnectionState(d.state.Load())
}

// Name returns the database name.
func (d *SQLDatabase) Name() string {
	return d.name
}

// Type returns the database type.
func (d *SQLDatabase) Type() DatabaseType {
	return d.dbType
}

// Driver returns the raw *sql.DB for native driver access.
func (d *SQLDatabase) Driver() any {
	return d.sqlDB
}

// DB returns the raw *sql.DB.
func (d *SQLDatabase) DB() *sql.DB {
	return d.sqlDB
}

// Bun returns the Bun ORM instance.
func (d *SQLDatabase) Bun() *bun.DB {
	return d.bun
}

// Health returns the health status.
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

// Stats returns connection pool statistics.
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

// Transaction executes a function in a SQL transaction with panic recovery.
func (d *SQLDatabase) Transaction(ctx context.Context, fn func(tx bun.Tx) error) (err error) {
	return d.bun.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = ErrPanicRecovered(d.name, d.dbType, r)
				d.logger.Error("panic recovered in transaction",
					logger.String("db", d.name),
					logger.Any("panic", r),
				)

				if d.metrics != nil {
					d.metrics.Counter("db_transaction_panics",
						"db", d.name,
					).Inc()
				}
			}
		}()

		return fn(tx)
	})
}

// TransactionWithOptions executes a function in a SQL transaction with options and panic recovery.
func (d *SQLDatabase) TransactionWithOptions(ctx context.Context, opts *sql.TxOptions, fn func(tx bun.Tx) error) (err error) {
	return d.bun.RunInTx(ctx, opts, func(ctx context.Context, tx bun.Tx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = ErrPanicRecovered(d.name, d.dbType, r)
				d.logger.Error("panic recovered in transaction",
					logger.String("db", d.name),
					logger.Any("panic", r),
				)

				if d.metrics != nil {
					d.metrics.Counter("db_transaction_panics",
						"db", d.name,
					).Inc()
				}
			}
		}()

		return fn(tx)
	})
}

// Helper: Get driver name.
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

// Helper: Get Bun dialect.
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

// Helper: Query hook for observability.
func (d *SQLDatabase) queryHook() bun.QueryHook {
	return &QueryHook{
		logger:                  d.logger,
		metrics:                 d.metrics,
		dbName:                  d.name,
		slowQueryThreshold:      d.config.SlowQueryThreshold,
		disableSlowQueryLogging: d.config.DisableSlowQueryLogging,
	}
}

// QueryHook provides observability for Bun queries.
type QueryHook struct {
	logger                  forge.Logger
	metrics                 forge.Metrics
	dbName                  string
	slowQueryThreshold      time.Duration
	disableSlowQueryLogging bool
}

// BeforeQuery is called before query execution.
func (h *QueryHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery is called after query execution.
func (h *QueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	duration := time.Since(event.StartTime)

	// Log slow queries using configurable threshold
	if !h.disableSlowQueryLogging && duration > h.slowQueryThreshold {
		h.logger.Warn("slow query detected",
			logger.String("db", h.dbName),
			logger.String("query", event.Query),
			logger.Duration("duration", duration),
			logger.Duration("threshold", h.slowQueryThreshold),
		)
	}

	// Record metrics
	if h.metrics != nil {
		h.metrics.Histogram("db_query_duration",
			"db", h.dbName,
			"operation", event.Operation(),
		).Observe(duration.Seconds())

		if event.Err != nil && !errors.Is(event.Err, sql.ErrNoRows) {
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
