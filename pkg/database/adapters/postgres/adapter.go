package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// PostgresAdapter implements database.DatabaseAdapter for PostgreSQL
type PostgresAdapter struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewPostgresAdapter creates a new PostgreSQL adapter
func NewPostgresAdapter(logger common.Logger, metrics common.Metrics) database.DatabaseAdapter {
	return &PostgresAdapter{
		logger:  logger,
		metrics: metrics,
	}
}

// Name returns the adapter name
func (pa *PostgresAdapter) Name() string {
	return "postgres"
}

// SupportedTypes returns the database types supported by this adapter
func (pa *PostgresAdapter) SupportedTypes() []string {
	return []string{"postgres", "postgresql"}
}

// Connect creates a new PostgreSQL connection
func (pa *PostgresAdapter) Connect(ctx context.Context, config *database.ConnectionConfig) (database.Connection, error) {
	if err := pa.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create GORM configuration
	gormConfig := &gorm.Config{
		Logger: pa.createGormLogger(),
	}

	// Create DSN
	dsn := pa.buildDSN(config)

	// Open database connection
	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Get underlying SQL DB
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying SQL DB: %w", err)
	}

	// Configure connection pool
	sqlDB.SetMaxOpenConns(config.Pool.MaxOpenConns)
	sqlDB.SetMaxIdleConns(config.Pool.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(config.Pool.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(config.Pool.ConnMaxIdleTime)

	// Create connection wrapper
	connectionName := fmt.Sprintf("postgres_%s", config.Database)
	conn := NewPostgresConnection(connectionName, config, db, pa.logger, pa.metrics)

	pa.logger.Info("PostgreSQL connection created",
		logger.String("connection", connectionName),
		logger.String("host", config.Host),
		logger.Int("port", config.Port),
		logger.String("database", config.Database),
		logger.String("username", config.Username),
	)

	return conn, nil
}

// ValidateConfig validates the PostgreSQL configuration
func (pa *PostgresAdapter) ValidateConfig(config *database.ConnectionConfig) error {
	if config == nil {
		return fmt.Errorf("configuration cannot be nil")
	}

	if config.Type != "postgres" && config.Type != "postgresql" {
		return fmt.Errorf("invalid database type: %s", config.Type)
	}

	if config.Host == "" {
		return fmt.Errorf("host is required")
	}

	if config.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}

	if config.Database == "" {
		return fmt.Errorf("database name is required")
	}

	if config.Username == "" {
		return fmt.Errorf("username is required")
	}

	// Validate SSL mode
	validSSLModes := []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
	if config.SSLMode != "" {
		found := false
		for _, mode := range validSSLModes {
			if config.SSLMode == mode {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("invalid SSL mode: %s", config.SSLMode)
		}
	}

	// Validate pool configuration
	if config.Pool.MaxOpenConns <= 0 {
		return fmt.Errorf("max open connections must be greater than 0")
	}

	if config.Pool.MaxIdleConns < 0 {
		return fmt.Errorf("max idle connections cannot be negative")
	}

	if config.Pool.MaxIdleConns > config.Pool.MaxOpenConns {
		return fmt.Errorf("max idle connections cannot be greater than max open connections")
	}

	return nil
}

// SupportsMigrations returns true as PostgreSQL supports migrations
func (pa *PostgresAdapter) SupportsMigrations() bool {
	return true
}

// Migrate runs database migrations
func (pa *PostgresAdapter) Migrate(ctx context.Context, conn database.Connection, migrationsPath string) error {
	postgresConn, ok := conn.(*PostgresConnection)
	if !ok {
		return fmt.Errorf("connection is not a PostgreSQL connection")
	}

	migrator := NewPostgresMigrator(postgresConn, pa.logger)
	return migrator.RunMigrations(ctx, migrationsPath)
}

// HealthCheck performs a health check on the connection
func (pa *PostgresAdapter) HealthCheck(ctx context.Context, conn database.Connection) error {
	postgresConn, ok := conn.(*PostgresConnection)
	if !ok {
		return fmt.Errorf("connection is not a PostgreSQL connection")
	}

	db := postgresConn.GetGormDB()
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying SQL DB: %w", err)
	}

	// Ping the database
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}

	// Check if we can execute a simple query
	var result int
	if err := db.WithContext(ctx).Raw("SELECT 1").Scan(&result).Error; err != nil {
		return fmt.Errorf("test query failed: %w", err)
	}

	if result != 1 {
		return fmt.Errorf("unexpected test query result: %d", result)
	}

	return nil
}

// buildDSN builds the PostgreSQL DSN from configuration
func (pa *PostgresAdapter) buildDSN(config *database.ConnectionConfig) string {
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s",
		config.Host, config.Port, config.Database, config.Username)

	if config.Password != "" {
		dsn += fmt.Sprintf(" password=%s", config.Password)
	}

	if config.SSLMode != "" {
		dsn += fmt.Sprintf(" sslmode=%s", config.SSLMode)
	} else {
		dsn += " sslmode=disable"
	}

	if config.Timezone != "" {
		dsn += fmt.Sprintf(" TimeZone=%s", config.Timezone)
	}

	// Add any additional config parameters
	for key, value := range config.Config {
		if strVal, ok := value.(string); ok && strVal != "" {
			dsn += fmt.Sprintf(" %s=%s", key, strVal)
		}
	}

	return dsn
}

// createGormLogger creates a GORM logger that integrates with Forge logging
func (pa *PostgresAdapter) createGormLogger() gormlogger.Interface {
	return &gormLoggerAdapter{
		logger: pa.logger,
	}
}

// gormLoggerAdapter adapts Forge logger to GORM logger interface
type gormLoggerAdapter struct {
	logger common.Logger
}

func (gla *gormLoggerAdapter) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return gla
}

func (gla *gormLoggerAdapter) Info(ctx context.Context, msg string, data ...interface{}) {
	if gla.logger != nil {
		gla.logger.Info(fmt.Sprintf(msg, data...))
	}
}

func (gla *gormLoggerAdapter) Warn(ctx context.Context, msg string, data ...interface{}) {
	if gla.logger != nil {
		gla.logger.Warn(fmt.Sprintf(msg, data...))
	}
}

func (gla *gormLoggerAdapter) Error(ctx context.Context, msg string, data ...interface{}) {
	if gla.logger != nil {
		gla.logger.Error(fmt.Sprintf(msg, data...))
	}
}

func (gla *gormLoggerAdapter) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if gla.logger == nil {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	// Clean up SQL for logging
	sql = strings.ReplaceAll(sql, "\n", " ")
	sql = strings.ReplaceAll(sql, "\t", " ")
	for strings.Contains(sql, "  ") {
		sql = strings.ReplaceAll(sql, "  ", " ")
	}
	sql = strings.TrimSpace(sql)

	fields := []logger.Field{
		logger.Duration("duration", elapsed),
		logger.String("sql", sql),
		logger.Int64("rows", rows),
	}

	if err != nil {
		fields = append(fields, logger.Error(err))
		gla.logger.Error("database query failed", fields...)
	} else {
		gla.logger.Debug("database query executed", fields...)
	}
}

// PostgresConfig contains PostgreSQL-specific configuration
type PostgresConfig struct {
	*database.ConnectionConfig
	SearchPath         string `yaml:"search_path" json:"search_path"`
	ApplicationName    string `yaml:"application_name" json:"application_name"`
	StatementTimeout   string `yaml:"statement_timeout" json:"statement_timeout"`
	LockTimeout        string `yaml:"lock_timeout" json:"lock_timeout"`
	WorkMem            string `yaml:"work_mem" json:"work_mem"`
	MaintenanceWorkMem string `yaml:"maintenance_work_mem" json:"maintenance_work_mem"`
}

// NewPostgresConfig creates a new PostgreSQL configuration
func NewPostgresConfig() *PostgresConfig {
	return &PostgresConfig{
		ConnectionConfig: &database.ConnectionConfig{
			Type:     "postgres",
			Host:     "localhost",
			Port:     5432,
			Database: "forge",
			Username: "postgres",
			Password: "password",
			SSLMode:  "disable",
			Pool: database.PoolConfig{
				MaxOpenConns:    25,
				MaxIdleConns:    10,
				ConnMaxLifetime: time.Hour,
				ConnMaxIdleTime: 30 * time.Minute,
			},
			Retry: database.RetryConfig{
				MaxAttempts:   3,
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      5 * time.Second,
				BackoffFactor: 2.0,
			},
		},
		SearchPath:      "public",
		ApplicationName: "forge-app",
	}
}

// Apply applies PostgreSQL-specific configuration
func (pc *PostgresConfig) Apply() {
	if pc.ConnectionConfig.Config == nil {
		pc.ConnectionConfig.Config = make(map[string]interface{})
	}

	if pc.SearchPath != "" {
		pc.ConnectionConfig.Config["search_path"] = pc.SearchPath
	}

	if pc.ApplicationName != "" {
		pc.ConnectionConfig.Config["application_name"] = pc.ApplicationName
	}

	if pc.StatementTimeout != "" {
		pc.ConnectionConfig.Config["statement_timeout"] = pc.StatementTimeout
	}

	if pc.LockTimeout != "" {
		pc.ConnectionConfig.Config["lock_timeout"] = pc.LockTimeout
	}

	if pc.WorkMem != "" {
		pc.ConnectionConfig.Config["work_mem"] = pc.WorkMem
	}

	if pc.MaintenanceWorkMem != "" {
		pc.ConnectionConfig.Config["maintenance_work_mem"] = pc.MaintenanceWorkMem
	}
}
