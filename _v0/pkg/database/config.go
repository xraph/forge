package database

import (
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// DatabaseConfig represents the overall database configuration
type DatabaseConfig = common.DatabaseConfig

// ConnectionConfig represents configuration for a database connection
type ConnectionConfig = common.ConnectionConfig

// PoolConfig represents connection pool configuration
type PoolConfig = common.PoolConfig

// RetryConfig represents retry configuration for database operations
type RetryConfig = common.RetryConfig

// HealthCheckConfig represents health check configuration
type HealthCheckConfig = common.HealthCheckConfig

// MetricsConfig represents metrics configuration
type MetricsConfig = common.MetricsConfig

// PostgresConfig represents PostgreSQL-specific configuration
type PostgresConfig = common.PostgresConfig

// RedisConfig represents Redis-specific configuration
type RedisConfig = common.RedisConfig

// MongoDBConfig represents MongoDB-specific configuration
type MongoDBConfig = common.MongoDBConfig

// DefaultDatabaseConfig returns a default database configuration
func DefaultDatabaseConfig() *DatabaseConfig {
	return &DatabaseConfig{
		Default:        "default",
		MigrationsPath: "./migrations",
		AutoMigrate:    false,
		Connections: map[string]ConnectionConfig{
			"default": {
				Type:     "postgres",
				Host:     "localhost",
				Port:     5432,
				Database: "forge_dev",
				Username: "postgres",
				Password: "password",
				SSLMode:  "disable",
				Pool: PoolConfig{
					MaxOpenConns:    25,
					MaxIdleConns:    10,
					ConnMaxLifetime: time.Hour,
					ConnMaxIdleTime: 30 * time.Minute,
				},
				Retry: RetryConfig{
					MaxAttempts:   3,
					InitialDelay:  100 * time.Millisecond,
					MaxDelay:      5 * time.Second,
					BackoffFactor: 2.0,
				},
			},
		},
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
			Timeout:  5 * time.Second,
		},
		Metrics: MetricsConfig{
			Enabled:            true,
			CollectionInterval: 15 * time.Second,
			SlowQueryThreshold: time.Second,
			EnableQueryMetrics: true,
			EnablePoolMetrics:  true,
		},
	}
}

// DefaultPostgresConfig returns a default PostgreSQL connection configuration
func DefaultPostgresConfig() ConnectionConfig {
	return ConnectionConfig{
		Type:     "postgres",
		Host:     "localhost",
		Port:     5432,
		Database: "forge_dev",
		Username: "postgres",
		Password: "",
		Pool: PoolConfig{
			MaxOpenConns:    25,
			MaxIdleConns:    10,
			ConnMaxLifetime: time.Hour,
			ConnMaxIdleTime: 10 * time.Minute,
		},
		Retry: RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  100 * time.Millisecond,
			MaxDelay:      5 * time.Second,
			BackoffFactor: 2.0,
		},
		Config: map[string]interface{}{
			"sslmode":         "disable",
			"connect_timeout": 10,
		},
	}
}

// DefaultMySQLConfig returns a default MySQL connection configuration
func DefaultMySQLConfig() ConnectionConfig {
	return ConnectionConfig{
		Type:     "mysql",
		Host:     "localhost",
		Port:     3306,
		Database: "forge_dev",
		Username: "root",
		Password: "",
		Pool: PoolConfig{
			MaxOpenConns:    25,
			MaxIdleConns:    10,
			ConnMaxLifetime: time.Hour,
			ConnMaxIdleTime: 10 * time.Minute,
		},
		Retry: RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  100 * time.Millisecond,
			MaxDelay:      5 * time.Second,
			BackoffFactor: 2.0,
		},
		Config: map[string]interface{}{
			"charset":   "utf8mb4",
			"parseTime": true,
			"loc":       "Local",
		},
	}
}

// DefaultRedisConfig returns a default Redis connection configuration
func DefaultRedisConfig() ConnectionConfig {
	return ConnectionConfig{
		Type:     "redis",
		Host:     "localhost",
		Port:     6379,
		Database: "0", // Redis database number as string
		Username: "",
		Password: "",
		Pool: PoolConfig{
			MaxOpenConns:    10,
			MaxIdleConns:    5,
			ConnMaxLifetime: time.Hour,
			ConnMaxIdleTime: 5 * time.Minute,
		},
		Retry: RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  8 * time.Millisecond,
			MaxDelay:      512 * time.Millisecond,
			BackoffFactor: 2.0,
		},
		Config: map[string]interface{}{
			"db":                0,
			"dial_timeout":      5 * time.Second,
			"read_timeout":      3 * time.Second,
			"write_timeout":     3 * time.Second,
			"pool_timeout":      4 * time.Second,
			"max_retries":       3,
			"min_retry_backoff": 8 * time.Millisecond,
			"max_retry_backoff": 512 * time.Millisecond,
		},
	}
}

// DefaultMongoConfig returns a default MongoDB connection configuration
func DefaultMongoConfig() ConnectionConfig {
	return ConnectionConfig{
		Type:     "mongodb",
		Host:     "localhost",
		Port:     27017,
		Database: "forge_dev",
		Username: "",
		Password: "",
		Pool: PoolConfig{
			MaxOpenConns:    100,
			MaxIdleConns:    10,
			ConnMaxLifetime: time.Hour,
			ConnMaxIdleTime: 10 * time.Minute,
		},
		Retry: RetryConfig{
			MaxAttempts:   3,
			InitialDelay:  100 * time.Millisecond,
			MaxDelay:      5 * time.Second,
			BackoffFactor: 2.0,
		},
		Config: map[string]interface{}{
			"auth_database":               "",
			"auth_mechanism":              "",
			"replica_set":                 "",
			"ssl":                         false,
			"ssl_cert_file":               "",
			"ssl_ca_file":                 "",
			"ssl_insecure":                false,
			"connect_timeout":             10 * time.Second,
			"server_selection_timeout":    5 * time.Second,
			"socket_timeout":              30 * time.Second,
			"max_conn_idle_time":          10 * time.Minute,
			"read_preference":             "primary",
			"write_concern":               "majority",
			"read_concern":                "local",
			"connect_timeout_ms":          10000,
			"server_selection_timeout_ms": 5000,
		},
	}
}
