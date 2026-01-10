// Package database provides unified database access with support for SQL (Postgres, MySQL, SQLite)
// and NoSQL (MongoDB) databases. It exposes native drivers for maximum flexibility while providing
// production-ready features like connection pooling, health checks, and observability.
package database

import (
	"context"
	"time"

	"github.com/xraph/forge/extensions/database/migrate"
)

// DatabaseType represents the type of database.
type DatabaseType string

const (
	TypePostgres DatabaseType = "postgres"
	TypeMySQL    DatabaseType = "mysql"
	TypeSQLite   DatabaseType = "sqlite"
	TypeMongoDB  DatabaseType = "mongodb"
	TypeRedis    DatabaseType = "redis"
)

// ConnectionState represents the state of a database connection.
type ConnectionState int32

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateError
	StateReconnecting
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateError:
		return "error"
	case StateReconnecting:
		return "reconnecting"
	default:
		return "unknown"
	}
}

// Database represents a database connection.
type Database interface {
	// Identity
	Name() string
	Type() DatabaseType

	// Lifecycle
	Open(ctx context.Context) error
	Close(ctx context.Context) error
	Ping(ctx context.Context) error

	// State
	IsOpen() bool
	State() ConnectionState

	// Health
	Health(ctx context.Context) HealthStatus
	Stats() DatabaseStats

	// Access to native driver/ORM
	Driver() any
}

// DatabaseConfig is the configuration for a database connection.
type DatabaseConfig struct {
	Name string       `json:"name" mapstructure:"name" yaml:"name"`
	Type DatabaseType `json:"type" mapstructure:"type" yaml:"type"`
	DSN  string       `json:"dsn"  mapstructure:"dsn"  yaml:"dsn"`

	// Connection pool settings
	MaxOpenConns    int           `default:"25" json:"max_open_conns"     mapstructure:"max_open_conns"     yaml:"max_open_conns"`
	MaxIdleConns    int           `default:"5"  json:"max_idle_conns"     mapstructure:"max_idle_conns"     yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `default:"5m" json:"conn_max_lifetime"  mapstructure:"conn_max_lifetime"  yaml:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `default:"5m" json:"conn_max_idle_time" mapstructure:"conn_max_idle_time" yaml:"conn_max_idle_time"`

	// Retry settings
	MaxRetries int           `default:"3"  json:"max_retries" mapstructure:"max_retries" yaml:"max_retries"`
	RetryDelay time.Duration `default:"1s" json:"retry_delay" mapstructure:"retry_delay" yaml:"retry_delay"`

	// Timeout settings
	ConnectionTimeout time.Duration `default:"10s" json:"connection_timeout" mapstructure:"connection_timeout" yaml:"connection_timeout"`
	QueryTimeout      time.Duration `default:"30s" json:"query_timeout"      mapstructure:"query_timeout"      yaml:"query_timeout"`

	// Observability settings
	SlowQueryThreshold      time.Duration `default:"100ms" json:"slow_query_threshold"       mapstructure:"slow_query_threshold"       yaml:"slow_query_threshold"`
	DisableSlowQueryLogging bool          `default:"false" json:"disable_slow_query_logging" mapstructure:"disable_slow_query_logging" yaml:"disable_slow_query_logging"`
	AutoExplainThreshold    time.Duration `default:"0"     json:"auto_explain_threshold"     mapstructure:"auto_explain_threshold"     yaml:"auto_explain_threshold"` // 0 = disabled

	// Health check
	HealthCheckInterval time.Duration `default:"30s" json:"health_check_interval" mapstructure:"health_check_interval" yaml:"health_check_interval"`

	// Additional config (database-specific)
	Config map[string]any `json:"config" mapstructure:"config" yaml:"config"`
}

// DatabaseStats provides connection pool statistics.
type DatabaseStats struct {
	OpenConnections   int           `json:"open_connections"`
	InUse             int           `json:"in_use"`
	Idle              int           `json:"idle"`
	WaitCount         int64         `json:"wait_count"`
	WaitDuration      time.Duration `json:"wait_duration"`
	MaxIdleClosed     int64         `json:"max_idle_closed"`
	MaxLifetimeClosed int64         `json:"max_lifetime_closed"`
}

// HealthStatus provides database health status.
type HealthStatus struct {
	Healthy   bool          `json:"healthy"`
	Message   string        `json:"message"`
	Latency   time.Duration `json:"latency"`
	CheckedAt time.Time     `json:"checked_at"`
}

// MigrationStatus provides migration status.
type MigrationStatus struct {
	ID        int64     `json:"id"`
	Applied   bool      `json:"applied"`
	AppliedAt time.Time `json:"applied_at"`
}

// Re-export migrate package for convenience
// This allows users to import "github.com/xraph/forge/extensions/database"
// and use database.Migrations instead of importing the migrate subpackage.
var (
	// Migrations is the global migration collection
	// All migrations should register themselves here using init().
	Migrations = migrate.Migrations

	// RegisterMigration is a helper to register a migration.
	RegisterMigration = migrate.RegisterMigration

	// RegisterModel adds a model to the auto-registration list.
	RegisterModel = migrate.RegisterModel

	// Models is the list of all models that should be auto-registered.
	Models = &migrate.Models
)
