package database

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
)

// Extension implements the database extension
type Extension struct {
	*forge.BaseExtension
	config  Config
	manager *DatabaseManager
}

// NewExtension creates a new database extension with variadic options
func NewExtension(opts ...ConfigOption) forge.Extension {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	base := forge.NewBaseExtension("database", "2.0.0", "Multi-database support with SQL (Postgres, MySQL, SQLite) and NoSQL (MongoDB)")
	return &Extension{
		BaseExtension: base,
		config:        config,
	}
}

// NewExtensionWithConfig creates a new database extension with a complete config
func NewExtensionWithConfig(config Config) forge.Extension {
	return NewExtension(WithConfig(config))
}

// Register registers the extension with the application
func (e *Extension) Register(app forge.App) error {
	if err := e.BaseExtension.Register(app); err != nil {
		return err
	}

	programmaticConfig := e.config
	finalConfig := DefaultConfig()
	if err := e.LoadConfig("database", &finalConfig, programmaticConfig, DefaultConfig(), programmaticConfig.RequireConfig); err != nil {
		if programmaticConfig.RequireConfig {
			return fmt.Errorf("database: failed to load required config: %w", err)
		}
		e.Logger().Warn("database: using default/programmatic config", forge.F("error", err.Error()))
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
		var db Database
		var err error

		switch dbConfig.Type {
		case TypePostgres, TypeMySQL, TypeSQLite:
			db, err = NewSQLDatabase(dbConfig, e.Logger(), e.Metrics())
		case TypeMongoDB:
			db, err = NewMongoDatabase(dbConfig, e.Logger(), e.Metrics())
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
		if err := forge.RegisterSingleton(app.Container(), DatabaseKey, func(c forge.Container) (Database, error) {
			return e.manager.Get(defaultName)
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
		if defaultConfig.Type == TypePostgres || defaultConfig.Type == TypeMySQL || defaultConfig.Type == TypeSQLite {
			if err := forge.RegisterSingleton(app.Container(), "db", func(c forge.Container) (*bun.DB, error) {
				return e.manager.SQL(defaultName)
			}); err != nil {
				return fmt.Errorf("failed to register Bun DB: %w", err)
			}
		}

		// If MongoDB, register client
		if defaultConfig.Type == TypeMongoDB {
			if err := forge.RegisterSingleton(app.Container(), "mongo", func(c forge.Container) (*mongo.Client, error) {
				return e.manager.Mongo(defaultName)
			}); err != nil {
				return fmt.Errorf("failed to register MongoDB client: %w", err)
			}
		}
	}

	e.Logger().Info("database extension registered",
		forge.F("databases", len(e.config.Databases)),
		forge.F("default", defaultName),
	)

	return nil
}

// Start starts the extension
func (e *Extension) Start(ctx context.Context) error {
	e.Logger().Info("starting database extension")

	// Open all databases
	if err := e.manager.OpenAll(ctx); err != nil {
		return fmt.Errorf("failed to open databases: %w", err)
	}

	e.MarkStarted()
	e.Logger().Info("database extension started")
	return nil
}

// Stop stops the extension
func (e *Extension) Stop(ctx context.Context) error {
	e.Logger().Info("stopping database extension")

	// Close all databases
	if err := e.manager.CloseAll(ctx); err != nil {
		return fmt.Errorf("failed to close databases: %w", err)
	}

	e.MarkStopped()
	e.Logger().Info("database extension stopped")
	return nil
}

// Health checks the extension health
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
