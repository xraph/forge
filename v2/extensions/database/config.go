package database

import "time"

// Config is the configuration for the database extension
type Config struct {
	// List of database configurations
	Databases []DatabaseConfig `yaml:"databases" json:"databases"`

	// Default database name (first one if not specified)
	Default string `yaml:"default" json:"default"`
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
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if len(c.Databases) == 0 {
		return ErrNoDatabasesConfigured
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
		return ErrInvalidDatabaseName
	}

	if config.Type == "" {
		return ErrInvalidDatabaseType
	}

	if config.DSN == "" {
		return ErrInvalidDSN
	}

	// Validate pool settings
	if config.MaxOpenConns < 0 {
		return ErrInvalidPoolConfig
	}

	if config.MaxIdleConns < 0 {
		return ErrInvalidPoolConfig
	}

	if config.MaxIdleConns > config.MaxOpenConns {
		return ErrInvalidPoolConfig
	}

	return nil
}
