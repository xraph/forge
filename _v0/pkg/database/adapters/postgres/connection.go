package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/database"
	"github.com/xraph/forge/v0/pkg/logger"
)

// PostgresConnection implements database.Connection for PostgreSQL
type PostgresConnection struct {
	*database.BaseConnection
	gormDB *gorm.DB
	sqlDB  *sql.DB
}

// NewPostgresConnection creates a new PostgreSQL connection
func NewPostgresConnection(name string, config *database.ConnectionConfig, gormDB *gorm.DB, logger common.Logger, metrics common.Metrics) database.Connection {
	baseConn := database.NewBaseConnection(name, "postgres", config, logger, metrics)

	conn := &PostgresConnection{
		BaseConnection: baseConn,
		gormDB:         gormDB,
	}

	// Get underlying SQL DB
	if sqlDB, err := gormDB.DB(); err == nil {
		conn.sqlDB = sqlDB
	}

	// Set the GORM DB as the underlying database connection
	baseConn.SetDB(gormDB)

	return conn
}

// Connect establishes the PostgreSQL connection
func (pc *PostgresConnection) Connect(ctx context.Context) error {
	if pc.gormDB == nil {
		return fmt.Errorf("GORM database instance is nil")
	}

	// Get underlying SQL DB
	var err error
	pc.sqlDB, err = pc.gormDB.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying SQL DB: %w", err)
	}

	// Test connection with ping
	if err := pc.sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Test with a simple query
	var result int
	if err := pc.gormDB.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error; err != nil {
		return fmt.Errorf("failed to execute test query: %w", err)
	}

	if result != 1 {
		return fmt.Errorf("unexpected test query result: %d", result)
	}

	pc.SetConnected(true)

	return nil
}

// Close closes the PostgreSQL connection
func (pc *PostgresConnection) Close(ctx context.Context) error {
	if pc.sqlDB == nil {
		return nil
	}

	pc.SetConnected(false)

	if err := pc.sqlDB.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}

	return nil
}

// Ping pings the PostgreSQL database
func (pc *PostgresConnection) Ping(ctx context.Context) error {
	if pc.sqlDB == nil {
		return fmt.Errorf("database connection not established")
	}

	if err := pc.sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}

// Transaction executes a function within a database transaction
func (pc *PostgresConnection) Transaction(ctx context.Context, fn func(tx interface{}) error) error {
	if pc.gormDB == nil {
		return fmt.Errorf("database connection not established")
	}

	pc.IncrementTransactionCount()

	start := time.Now()
	err := pc.gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})

	duration := time.Since(start)

	// Record transaction metrics
	if pc.Metrics() != nil {
		tags := []string{"connection", pc.Name(), "type", pc.Type()}
		pc.Metrics().Histogram("forge.database.transaction_duration", tags...).Observe(duration.Seconds())

		if err != nil {
			pc.Metrics().Counter("forge.database.transaction_errors", tags...).Inc()
		} else {
			pc.Metrics().Counter("forge.database.transaction_success", tags...).Inc()
		}
	}

	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}

	return nil
}

// GetGormDB returns the underlying GORM database instance
func (pc *PostgresConnection) GetGormDB() *gorm.DB {
	return pc.gormDB
}

// GetSQLDB returns the underlying SQL database instance
func (pc *PostgresConnection) GetSQLDB() *sql.DB {
	return pc.sqlDB
}

// Stats returns connection statistics with PostgreSQL-specific data
func (pc *PostgresConnection) Stats() database.ConnectionStats {
	stats := pc.BaseConnection.Stats()

	// Add PostgreSQL-specific statistics
	if pc.sqlDB != nil {
		dbStats := pc.sqlDB.Stats()
		stats.OpenConnections = dbStats.OpenConnections
		stats.IdleConnections = dbStats.Idle
		// Note: MaxOpenConns and MaxIdleConns are already set from config in BaseConnection
	}

	return stats
}

// Execute executes a raw SQL query
func (pc *PostgresConnection) Execute(ctx context.Context, query string, args ...interface{}) error {
	if pc.gormDB == nil {
		return fmt.Errorf("database connection not established")
	}

	pc.IncrementQueryCount()

	start := time.Now()
	err := pc.gormDB.WithContext(ctx).Exec(query, args...).Error
	duration := time.Since(start)

	// Record query metrics
	if pc.Metrics() != nil {
		tags := []string{"connection", pc.Name(), "type", pc.Type(), "operation", "execute"}
		pc.Metrics().Histogram("forge.database.query_duration", tags...).Observe(duration.Seconds())

		if err != nil {
			pc.Metrics().Counter("forge.database.query_errors", tags...).Inc()
		} else {
			pc.Metrics().Counter("forge.database.query_success", tags...).Inc()
		}
	}

	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	return nil
}

// Query executes a query and returns rows
func (pc *PostgresConnection) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if pc.sqlDB == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	pc.IncrementQueryCount()

	start := time.Now()
	rows, err := pc.sqlDB.QueryContext(ctx, query, args...)
	duration := time.Since(start)

	// Record query metrics
	if pc.Metrics() != nil {
		tags := []string{"connection", pc.Name(), "type", pc.Type(), "operation", "query"}
		pc.Metrics().Histogram("forge.database.query_duration", tags...).Observe(duration.Seconds())

		if err != nil {
			pc.Metrics().Counter("forge.database.query_errors", tags...).Inc()
		} else {
			pc.Metrics().Counter("forge.database.query_success", tags...).Inc()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	return rows, nil
}

// QueryRow executes a query that returns a single row
func (pc *PostgresConnection) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if pc.sqlDB == nil {
		// Return a row with an error
		return &sql.Row{}
	}

	pc.IncrementQueryCount()

	start := time.Now()
	row := pc.sqlDB.QueryRowContext(ctx, query, args...)
	duration := time.Since(start)

	// Record query metrics
	if pc.Metrics() != nil {
		tags := []string{"connection", pc.Name(), "type", pc.Type(), "operation", "query_row"}
		pc.Metrics().Histogram("forge.database.query_duration", tags...).Observe(duration.Seconds())
		pc.Metrics().Counter("forge.database.query_success", tags...).Inc()
	}

	return row
}

// CreateTable creates a table using GORM's AutoMigrate
func (pc *PostgresConnection) CreateTable(ctx context.Context, model interface{}) error {
	if pc.gormDB == nil {
		return fmt.Errorf("database connection not established")
	}

	start := time.Now()
	err := pc.gormDB.WithContext(ctx).AutoMigrate(model)
	duration := time.Since(start)

	if pc.Logger() != nil {
		pc.Logger().Info("table creation attempted",
			logger.String("connection", pc.Name()),
			logger.Duration("duration", duration),
			logger.Bool("success", err == nil),
		)
	}

	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// DropTable drops a table
func (pc *PostgresConnection) DropTable(ctx context.Context, model interface{}) error {
	if pc.gormDB == nil {
		return fmt.Errorf("database connection not established")
	}

	start := time.Now()
	err := pc.gormDB.WithContext(ctx).Migrator().DropTable(model)
	duration := time.Since(start)

	if pc.Logger() != nil {
		pc.Logger().Info("table drop attempted",
			logger.String("connection", pc.Name()),
			logger.Duration("duration", duration),
			logger.Bool("success", err == nil),
		)
	}

	if err != nil {
		return fmt.Errorf("failed to drop table: %w", err)
	}

	return nil
}

// HasTable checks if a table exists
func (pc *PostgresConnection) HasTable(ctx context.Context, tableName string) (bool, error) {
	if pc.gormDB == nil {
		return false, fmt.Errorf("database connection not established")
	}

	exists := pc.gormDB.WithContext(ctx).Migrator().HasTable(tableName)
	return exists, nil
}

// GetTableNames returns a list of table names
func (pc *PostgresConnection) GetTableNames(ctx context.Context) ([]string, error) {
	if pc.sqlDB == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	query := `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = 'public' 
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	rows, err := pc.sqlDB.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table names: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, tableName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating table names: %w", err)
	}

	return tables, nil
}

// GetDatabaseVersion returns the PostgreSQL version
func (pc *PostgresConnection) GetDatabaseVersion(ctx context.Context) (string, error) {
	if pc.gormDB == nil {
		return "", fmt.Errorf("database connection not established")
	}

	var version string
	err := pc.gormDB.WithContext(ctx).Raw("SELECT version()").Scan(&version).Error
	if err != nil {
		return "", fmt.Errorf("failed to get database version: %w", err)
	}

	return version, nil
}

// GetDatabaseSize returns the database size in bytes
func (pc *PostgresConnection) GetDatabaseSize(ctx context.Context) (int64, error) {
	if pc.gormDB == nil {
		return 0, fmt.Errorf("database connection not established")
	}

	var size int64
	query := "SELECT pg_database_size(current_database())"
	err := pc.gormDB.WithContext(ctx).Raw(query).Scan(&size).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get database size: %w", err)
	}

	return size, nil
}

// Vacuum performs a VACUUM operation
func (pc *PostgresConnection) Vacuum(ctx context.Context, tableName string) error {
	if pc.sqlDB == nil {
		return fmt.Errorf("database connection not established")
	}

	query := "VACUUM"
	if tableName != "" {
		query += " " + tableName
	}

	start := time.Now()
	_, err := pc.sqlDB.ExecContext(ctx, query)
	duration := time.Since(start)

	if pc.Logger() != nil {
		pc.Logger().Info("vacuum operation completed",
			logger.String("connection", pc.Name()),
			logger.String("table", tableName),
			logger.Duration("duration", duration),
			logger.Bool("success", err == nil),
		)
	}

	if err != nil {
		return fmt.Errorf("vacuum failed: %w", err)
	}

	return nil
}

// Analyze performs an ANALYZE operation
func (pc *PostgresConnection) Analyze(ctx context.Context, tableName string) error {
	if pc.sqlDB == nil {
		return fmt.Errorf("database connection not established")
	}

	query := "ANALYZE"
	if tableName != "" {
		query += " " + tableName
	}

	start := time.Now()
	_, err := pc.sqlDB.ExecContext(ctx, query)
	duration := time.Since(start)

	if pc.Logger() != nil {
		pc.Logger().Info("analyze operation completed",
			logger.String("connection", pc.Name()),
			logger.String("table", tableName),
			logger.Duration("duration", duration),
			logger.Bool("success", err == nil),
		)
	}

	if err != nil {
		return fmt.Errorf("analyze failed: %w", err)
	}

	return nil
}

// BeginTransaction starts a new transaction
func (pc *PostgresConnection) BeginTransaction(ctx context.Context) (*gorm.DB, error) {
	if pc.gormDB == nil {
		return nil, fmt.Errorf("database connection not established")
	}

	tx := pc.gormDB.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}

	return tx, nil
}
