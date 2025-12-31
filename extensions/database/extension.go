package database

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
)

// Extension implements the database extension.
type Extension struct {
	*forge.BaseExtension

	config  Config
	manager *DatabaseManager
}

// NewExtension creates a new database extension with variadic options.
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
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

	finalConfig := DefaultConfig()
	if err := e.LoadConfig("database", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if err := e.LoadConfig("extensions.database", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
			if programmaticConfig.RequireConfig {
				return fmt.Errorf("database: failed to load required config: %w", err)
			}

			e.Logger().Warn("database: using default/programmatic config", forge.F("error", err.Error()))
		}
	}

	e.config = finalConfig

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("invalid database configuration: %w", err)
	}

	// Create database manager
	e.manager = NewDatabaseManager(e.Logger(), e.Metrics())

	// Register databases
	for _, dbConfig := range e.config.Databases {
		var (
			db  Database
			err error
		)

		switch dbConfig.Type {
		case TypePostgres, TypeMySQL, TypeSQLite:
			db, err = NewSQLDatabase(dbConfig, e.Logger(), e.Metrics())
		case TypeMongoDB:
			db, err = NewMongoDatabase(dbConfig, e.Logger(), e.Metrics())
		case TypeRedis:
			db, err = NewRedisDatabase(dbConfig, e.Logger(), e.Metrics())
		default:
			return fmt.Errorf("unsupported database type: %s", dbConfig.Type)
		}

		if err != nil {
			return fmt.Errorf("failed to create database %s: %w", dbConfig.Name, err)
		}

		if err := e.manager.Register(dbConfig.Name, db); err != nil {
			return err
		}
	}

	// Register database manager in DI
	if err := forge.RegisterSingleton(app.Container(), ManagerKey, func(c forge.Container) (*DatabaseManager, error) {
		return e.manager, nil
	}); err != nil {
		return fmt.Errorf("failed to register database manager: %w", err)
	}

	// Register default database (first one or specified)
	defaultName := e.config.Default
	if defaultName == "" && len(e.config.Databases) > 0 {
		defaultName = e.config.Databases[0].Name
	}

	if defaultName != "" {
		// Register default database
		// Resolve ManagerKey from container first to ensure DatabaseManager is started
		if err := forge.RegisterSingleton(app.Container(), DatabaseKey, func(c forge.Container) (Database, error) {
			// Resolve manager from container to trigger auto-start
			manager, err := forge.Resolve[*DatabaseManager](c, ManagerKey)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve database manager: %w", err)
			}
			return manager.Get(defaultName)
		}); err != nil {
			return fmt.Errorf("failed to register default database: %w", err)
		}

		// Get the default database config
		var defaultConfig *DatabaseConfig

		for i := range e.config.Databases {
			if e.config.Databases[i].Name == defaultName {
				defaultConfig = &e.config.Databases[i]

				break
			}
		}

		if defaultConfig == nil {
			return fmt.Errorf("default database %s not found in configuration", defaultName)
		}

		// If SQL, register Bun instance
		// Resolve ManagerKey from container first to ensure DatabaseManager is started
		// and connections are opened before returning *bun.DB
		if defaultConfig.Type == TypePostgres || defaultConfig.Type == TypeMySQL || defaultConfig.Type == TypeSQLite {
			if err := forge.RegisterSingleton(app.Container(), SQLKey, func(c forge.Container) (*bun.DB, error) {
				// Resolve manager from container to trigger auto-start
				manager, err := forge.Resolve[*DatabaseManager](c, ManagerKey)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve database manager: %w", err)
				}
				return manager.SQL(defaultName)
			}); err != nil {
				return fmt.Errorf("failed to register Bun DB: %w", err)
			}
		}

		// If MongoDB, register client
		// Resolve ManagerKey from container first to ensure DatabaseManager is started
		if defaultConfig.Type == TypeMongoDB {
			if err := forge.RegisterSingleton(app.Container(), MongoKey, func(c forge.Container) (*mongo.Client, error) {
				// Resolve manager from container to trigger auto-start
				manager, err := forge.Resolve[*DatabaseManager](c, ManagerKey)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve database manager: %w", err)
				}
				return manager.Mongo(defaultName)
			}); err != nil {
				return fmt.Errorf("failed to register MongoDB client: %w", err)
			}
		}

		// If Redis, register client
		// Resolve ManagerKey from container first to ensure DatabaseManager is started
		if defaultConfig.Type == TypeRedis {
			if err := forge.RegisterSingleton(app.Container(), RedisKey, func(c forge.Container) (redis.UniversalClient, error) {
				// Resolve manager from container to trigger auto-start
				manager, err := forge.Resolve[*DatabaseManager](c, ManagerKey)
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
		forge.F("databases", len(e.config.Databases)),
		forge.F("default", defaultName),
	)

	return nil
}

// Start starts the extension.
// Note: Database connections are opened by the DI container when it calls
// DatabaseManager.Start() during container.Start(). This ensures connections
// are available to other extensions that depend on the database.
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting database extension")

	// Database connections are already opened by the DI container calling
	// DatabaseManager.Start() before extension Start() methods are called.
	// This allows other extensions to resolve database connections during
	// their Register() phase using forge.ResolveReady().

	e.MarkStarted()
	e.Logger().Info("database extension started")

	return nil
}

// Stop stops the extension.
// Note: Database connections are closed by the DI container when it calls
// DatabaseManager.Stop() during container.Stop().
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping database extension")

	// Database connections are closed by the DI container calling
	// DatabaseManager.Stop() during container shutdown.

	e.MarkStopped()
	e.Logger().Info("database extension stopped")

	return nil
}

// Health checks the extension health.
func (e *Extension) Health(ctx context.Context) error {
	// Check all databases
	statuses := e.manager.HealthCheckAll(ctx)

	unhealthy := 0

	for name, status := range statuses {
		if !status.Healthy {
			unhealthy++

			e.Logger().Warn("database unhealthy", forge.F("name", name), forge.F("error", status.Message))
		}
	}

	if unhealthy > 0 {
		return fmt.Errorf("%d databases unhealthy", unhealthy)
	}

	return nil
}
