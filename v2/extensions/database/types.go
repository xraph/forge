// Package database provides unified database access with support for SQL (Postgres, MySQL, SQLite)
// and NoSQL (MongoDB) databases. It exposes native drivers for maximum flexibility while providing
// production-ready features like connection pooling, health checks, and observability.
package database

import (
	"context"
	"time"
)

// DatabaseType represents the type of database
type DatabaseType string

const (
	TypePostgres DatabaseType = "postgres"
	TypeMySQL    DatabaseType = "mysql"
	TypeSQLite   DatabaseType = "sqlite"
	TypeMongoDB  DatabaseType = "mongodb"
	TypeRedis    DatabaseType = "redis"
)

// Database represents a database connection
type Database interface {
	// Identity
	Name() string
	Type() DatabaseType

	// Lifecycle
	Open(ctx context.Context) error
	Close(ctx context.Context) error
	Ping(ctx context.Context) error

	// Health
	Health(ctx context.Context) HealthStatus
	Stats() DatabaseStats

	// Access to native driver/ORM
	Driver() interface{}
}

// DatabaseConfig is the configuration for a database connection
type DatabaseConfig struct {
	Name string       `yaml:"name" json:"name"`
	Type DatabaseType `yaml:"type" json:"type"`
	DSN  string       `yaml:"dsn" json:"dsn"`

	// Connection pool settings
	MaxOpenConns    int           `yaml:"max_open_conns" json:"max_open_conns" default:"25"`
	MaxIdleConns    int           `yaml:"max_idle_conns" json:"max_idle_conns" default:"5"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" json:"conn_max_lifetime" default:"5m"`
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time" json:"conn_max_idle_time" default:"5m"`

	// Retry settings
	MaxRetries int           `yaml:"max_retries" json:"max_retries" default:"3"`
	RetryDelay time.Duration `yaml:"retry_delay" json:"retry_delay" default:"1s"`

	// Health check
	HealthCheckInterval time.Duration `yaml:"health_check_interval" json:"health_check_interval" default:"30s"`

	// Additional config (database-specific)
	Config map[string]interface{} `yaml:"config" json:"config"`
}

// DatabaseStats provides connection pool statistics
type DatabaseStats struct {
	OpenConnections   int           `json:"open_connections"`
	InUse             int           `json:"in_use"`
	Idle              int           `json:"idle"`
	WaitCount         int64         `json:"wait_count"`
	WaitDuration      time.Duration `json:"wait_duration"`
	MaxIdleClosed     int64         `json:"max_idle_closed"`
	MaxLifetimeClosed int64         `json:"max_lifetime_closed"`
}

// HealthStatus provides database health status
type HealthStatus struct {
	Healthy   bool          `json:"healthy"`
	Message   string        `json:"message"`
	Latency   time.Duration `json:"latency"`
	CheckedAt time.Time     `json:"checked_at"`
}

// MigrationStatus provides migration status
type MigrationStatus struct {
	ID        int64     `json:"id"`
	Applied   bool      `json:"applied"`
	AppliedAt time.Time `json:"applied_at"`
}
