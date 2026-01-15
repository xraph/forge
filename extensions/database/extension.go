package database

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// Extension implements the database extension.
// The extension is now a lightweight facade that loads config and registers services.
// Service lifecycle is managed by Vessel, not by the extension.
type Extension struct {
	*forge.BaseExtension

	config Config
	// No longer storing manager - Vessel manages it
}

// NewExtension creates a new database extension with variadic options.
func NewExtension(opts ...ConfigOption) forge.Extension {
	// Start with empty config - defaults will be applied by LoadConfig only if YAML config not found
	// This prevents defaults from overriding YAML configuration
	config := Config{}
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("database", "2.0.0", "Multi-database support with SQL (Postgres, MySQL, SQLite), NoSQL (MongoDB), and caching (Redis)")

	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new database extension with a complete config.
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the application.
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config

	// Determine if we have actual programmatic databases (not just defaults)
	hasProgrammaticDatabases := len(programmaticConfig.Databases) > 0 &&
		(len(programmaticConfig.Databases) != 1 || programmaticConfig.Databases[0].Type != TypeSQLite)

	// Use direct ConfigManager binding since LoadConfig has issues with defaults
	cm := e.App().Config()
	finalConfig := Config{}

	// Try "extensions.database" first (namespaced pattern)
	configLoaded := false

	if cm.IsSet("extensions.database") {
		if err := cm.Bind("extensions.database", &finalConfig); err == nil {
			e.Logger().Info("database: loaded from config file",
				forge.F("key", "extensions.database"),
				forge.F("databases", len(finalConfig.Databases)),
			)

			configLoaded = true
		} else {
			e.Logger().Warn("database: failed to bind extensions.database", forge.F("error", err))
		}
	}

	// Try legacy "database" key if not loaded yet
	if !configLoaded && cm.IsSet("database") {
		if err := cm.Bind("database", &finalConfig); err == nil {
			e.Logger().Info("database: loaded from config file",
				forge.F("key", "database"),
				forge.F("databases", len(finalConfig.Databases)),
			)

			configLoaded = true
		} else {
			e.Logger().Warn("database: failed to bind database", forge.F("error", err))
		}
	}

	// Handle config not found
	if !configLoaded {
		if programmaticConfig.RequireConfig {
			return errors.New("database: configuration is required but not found in config files. " +
				"Ensure 'extensions.database' or 'database' key exists in your config.yaml")
		}

		// Use programmatic config if provided, otherwise defaults
		if hasProgrammaticDatabases {
			e.Logger().Info("database: using programmatic configuration")

			finalConfig = programmaticConfig
		} else {
			e.Logger().Info("database: using default configuration")

			finalConfig = DefaultConfig()
		}
	} else {
		// Config loaded from YAML - merge with programmatic databases if provided
		if hasProgrammaticDatabases {
			e.Logger().Info("database: merging programmatic databases")

			finalConfig.Databases = append(finalConfig.Databases, programmaticConfig.Databases...)
			if programmaticConfig.Default != "" {
				finalConfig.Default = programmaticConfig.Default
			}
		}
	}

	e.Logger().Info("database: configuration loaded",
		forge.F("databases", len(finalConfig.Databases)),
		forge.F("default", finalConfig.Default),
	)

	e.config = finalConfig

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("invalid database configuration: %w", err)
	}

	// Register DatabaseManager constructor with Vessel - config captured in closure
	// Vessel will manage the service lifecycle (Start/Stop)
	if err := e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*DatabaseManager, error) {
		manager := NewDatabaseManager(logger, metrics)

		// Register all configured databases
		for _, dbConfig := range finalConfig.Databases {
			var (
				db  Database
				err error
			)

			switch dbConfig.Type {
			case TypePostgres, TypeMySQL, TypeSQLite:
				db, err = NewSQLDatabase(dbConfig, logger, metrics)
			case TypeMongoDB:
				db, err = NewMongoDatabase(dbConfig, logger, metrics)
			case TypeRedis:
				db, err = NewRedisDatabase(dbConfig, logger, metrics)
			default:
				return nil, fmt.Errorf("unsupported database type: %s", dbConfig.Type)
			}

			if err != nil {
				return nil, fmt.Errorf("failed to create database %s: %w", dbConfig.Name, err)
			}

			if err := manager.Register(dbConfig.Name, db); err != nil {
				return nil, err
			}
		}

		return manager, nil
	}); err != nil {
		return fmt.Errorf("failed to register database manager constructor: %w", err)
	}

	// Also register by name for backward compatibility
	if err := forge.RegisterSingleton(app.Container(), ManagerKey, func(c forge.Container) (*DatabaseManager, error) {
		return forge.InjectType[*DatabaseManager](c)
	}); err != nil {
		return fmt.Errorf("failed to register database manager key: %w", err)
	}

	// Register helper services for default database
	defaultName := finalConfig.Default
	if defaultName == "" && len(finalConfig.Databases) > 0 {
		defaultName = finalConfig.Databases[0].Name
	}

	if defaultName != "" {
		// Register default database interface
		if err := forge.RegisterSingleton(app.Container(), DatabaseKey, func(c forge.Container) (Database, error) {
			manager, err := forge.InjectType[*DatabaseManager](c)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve database manager: %w", err)
			}
			return manager.Get(defaultName)
		}); err != nil {
			return fmt.Errorf("failed to register default database: %w", err)
		}

		// Get the default database config
		var defaultConfig *DatabaseConfig
		for i := range finalConfig.Databases {
			if finalConfig.Databases[i].Name == defaultName {
				defaultConfig = &finalConfig.Databases[i]
				break
			}
		}

		if defaultConfig == nil {
			return fmt.Errorf("default database %s not found in configuration", defaultName)
		}

		// Register type-specific accessors
		if defaultConfig.Type == TypePostgres || defaultConfig.Type == TypeMySQL || defaultConfig.Type == TypeSQLite {
			if err := forge.RegisterSingleton(app.Container(), SQLKey, func(c forge.Container) (*bun.DB, error) {
				manager, err := forge.InjectType[*DatabaseManager](c)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve database manager: %w", err)
				}
				return manager.SQL(defaultName)
			}); err != nil {
				return fmt.Errorf("failed to register Bun DB: %w", err)
			}
		}

		if defaultConfig.Type == TypeMongoDB {
			if err := forge.RegisterSingleton(app.Container(), MongoKey, func(c forge.Container) (*mongo.Client, error) {
				manager, err := forge.InjectType[*DatabaseManager](c)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve database manager: %w", err)
				}
				return manager.Mongo(defaultName)
			}); err != nil {
				return fmt.Errorf("failed to register MongoDB client: %w", err)
			}
		}

		if defaultConfig.Type == TypeRedis {
			if err := forge.RegisterSingleton(app.Container(), RedisKey, func(c forge.Container) (redis.UniversalClient, error) {
				manager, err := forge.InjectType[*DatabaseManager](c)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve database manager: %w", err)
				}
				return manager.Redis(defaultName)
			}); err != nil {
				return fmt.Errorf("failed to register Redis client: %w", err)
			}
		}
	}

	e.Logger().Info("database extension registered",
		forge.F("databases", len(finalConfig.Databases)),
		forge.F("default", defaultName),
	)

	return nil
}

// Start marks the extension as started.
// Database connections are opened by Vessel calling DatabaseManager.Start().
func (e *Extension) Start(ctx context.Context) error {
	e.MarkStarted()
	return nil
}

// Stop marks the extension as stopped.
// Database connections are closed by Vessel calling DatabaseManager.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.MarkStopped()
	return nil
}

// Health checks the extension health.
// This delegates to DatabaseManager health check managed by Vessel.
func (e *Extension) Health(ctx context.Context) error {
	// Health is now managed by Vessel through DatabaseManager.Health()
	return nil
}
