package database

import (
	"strings"
	"time"
)

// DI container keys for database extension services
const (
	// ManagerKey is the DI key for the database manager
	ManagerKey = "forge.database.manager"
	// DatabaseKey is the DI key for the default database
	DatabaseKey = "forge.database.database"
	// SQLKey is the DI key for the default SQL database (Bun DB)
	SQLKey = "forge.database.sql"
	// MongoKey is the DI key for the default MongoDB client
	MongoKey = "forge.database.mongo"
)

// Config is the configuration for the database extension
type Config struct {
	// List of database configurations
	Databases []DatabaseConfig `yaml:"databases" json:"databases" mapstructure:"databases"`

	// Default database name (first one if not specified)
	Default string `yaml:"default" json:"default" mapstructure:"default"`

	// Config loading flags
	RequireConfig bool `yaml:"-" json:"-"`
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Databases: []DatabaseConfig{
			{
				Name:                "default",
				Type:                TypeSQLite,
				DSN:                 "file::memory:?cache=shared",
				MaxOpenConns:        25,
				MaxIdleConns:        5,
				ConnMaxLifetime:     5 * time.Minute,
				ConnMaxIdleTime:     5 * time.Minute,
				MaxRetries:          3,
				RetryDelay:          time.Second,
				HealthCheckInterval: 30 * time.Second,
			},
		},
		RequireConfig: false,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if len(c.Databases) == 0 {
		return ErrNoDatabasesConfigured()
	}

	// Validate each database configuration
	for _, dbConfig := range c.Databases {
		if err := validateDatabaseConfig(dbConfig); err != nil {
			return err
		}
	}

	return nil
}

func validateDatabaseConfig(config DatabaseConfig) error {
	if config.Name == "" {
		return ErrInvalidDatabaseName("")
	}

	if config.Type == "" {
		return ErrInvalidDatabaseType("")
	}

	if config.DSN == "" {
		return ErrInvalidDSN("")
	}

	// Validate pool settings
	if config.MaxOpenConns < 0 {
		return ErrInvalidPoolConfig("max_open_conns cannot be negative")
	}

	if config.MaxIdleConns < 0 {
		return ErrInvalidPoolConfig("max_idle_conns cannot be negative")
	}

	if config.MaxIdleConns > config.MaxOpenConns {
		return ErrInvalidPoolConfig("max_idle_conns cannot exceed max_open_conns")
	}

	// Validate timeout settings
	if config.ConnectionTimeout < 0 {
		return ErrInvalidPoolConfig("connection_timeout cannot be negative")
	}

	if config.QueryTimeout < 0 {
		return ErrInvalidPoolConfig("query_timeout cannot be negative")
	}

	return nil
}

// ConfigOption is a functional option for Config
type ConfigOption func(*Config)

// WithDatabases sets the list of database configurations
func WithDatabases(databases ...DatabaseConfig) ConfigOption {
	return func(c *Config) {
		c.Databases = databases
	}
}

// WithDatabase adds a single database configuration
func WithDatabase(db DatabaseConfig) ConfigOption {
	return func(c *Config) {
		c.Databases = append(c.Databases, db)
	}
}

// WithDefault sets the default database name
func WithDefault(name string) ConfigOption {
	return func(c *Config) { c.Default = name }
}

// WithRequireConfig requires config from YAML
func WithRequireConfig(require bool) ConfigOption {
	return func(c *Config) { c.RequireConfig = require }
}

// WithConfig replaces the entire config
func WithConfig(config Config) ConfigOption {
	return func(c *Config) { *c = config }
}

// MaskDSN masks sensitive information in DSN for logging
func MaskDSN(dsn string, dbType DatabaseType) string {
	switch dbType {
	case TypePostgres:
		// postgres://user:password@host/db -> postgres://user:***@host/db
		if idx := strings.Index(dsn, "://"); idx != -1 {
			prefix := dsn[:idx+3]
			rest := dsn[idx+3:]
			if idx2 := strings.Index(rest, ":"); idx2 != -1 {
				if idx3 := strings.Index(rest[idx2:], "@"); idx3 != -1 {
					return prefix + rest[:idx2+1] + "***" + rest[idx2+idx3:]
				}
			}
		}
	case TypeMySQL:
		// user:password@tcp(host)/db -> user:***@tcp(host)/db
		if idx := strings.Index(dsn, ":"); idx != -1 {
			if idx2 := strings.Index(dsn[idx:], "@"); idx2 != -1 {
				return dsn[:idx+1] + "***" + dsn[idx+idx2:]
			}
		}
	case TypeMongoDB:
		// mongodb://user:password@host/db -> mongodb://user:***@host/db
		if idx := strings.Index(dsn, "://"); idx != -1 {
			prefix := dsn[:idx+3]
			rest := dsn[idx+3:]
			if idx2 := strings.Index(rest, ":"); idx2 != -1 {
				if idx3 := strings.Index(rest[idx2:], "@"); idx3 != -1 {
					return prefix + rest[:idx2+1] + "***" + rest[idx2+idx3:]
				}
			}
		}
	}
	// For SQLite and other types without credentials, return as-is
	return dsn
}
