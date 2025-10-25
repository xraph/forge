package database

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// Extension implements the database extension
type Extension struct {
	config  Config
	manager *DatabaseManager
	logger  forge.Logger
	metrics forge.Metrics
	app     forge.App
}

// NewExtension creates a new database extension
func NewExtension(config Config) forge.Extension {
	return &Extension{
		config: config,
	}
}

// Name returns the extension name
func (e *Extension) Name() string {
	return "database"
}

// Version returns the extension version
func (e *Extension) Version() string {
	return "2.0.0"
}

// Description returns the extension description
func (e *Extension) Description() string {
	return "Multi-database support with SQL (Postgres, MySQL, SQLite) and NoSQL (MongoDB)"
}

// Dependencies returns the list of extension dependencies
func (e *Extension) Dependencies() []string {
	return []string{} // No dependencies
}

// Register registers the extension with the application
func (e *Extension) Register(app forge.App) error {
	e.app = app

	// Get dependencies from DI
	e.logger = forge.Must[forge.Logger](app.Container(), "logger")
	e.metrics = forge.Must[forge.Metrics](app.Container(), "metrics")

	// Get config from ConfigManager (bind pattern)
	configMgr := forge.Must[forge.ConfigManager](app.Container(), "config")
	var tempConfig Config
	if err := configMgr.Bind("extensions.database", &tempConfig); err == nil {
		e.config = tempConfig
	} else {
		// Use default config if not found
		e.logger.Info("using default database config")
		e.config = DefaultConfig()
	}

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("invalid database configuration: %w", err)
	}

	// Create database manager
	e.manager = NewDatabaseManager(e.logger, e.metrics)

	// Register databases
	for _, dbConfig := range e.config.Databases {
		var db Database
		var err error

		switch dbConfig.Type {
		case TypePostgres, TypeMySQL, TypeSQLite:
			db, err = NewSQLDatabase(dbConfig, e.logger, e.metrics)
		case TypeMongoDB:
			db, err = NewMongoDatabase(dbConfig, e.logger, e.metrics)
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
	if err := forge.RegisterSingleton(app.Container(), "databaseManager", func(c forge.Container) (*DatabaseManager, error) {
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
		if err := forge.RegisterSingleton(app.Container(), "database", func(c forge.Container) (Database, error) {
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

	e.logger.Info("database extension registered",
		forge.F("databases", len(e.config.Databases)),
		forge.F("default", defaultName),
	)

	return nil
}

// Start starts the extension
func (e *Extension) Start(ctx context.Context) error {
	e.logger.Info("starting database extension")

	// Open all databases
	if err := e.manager.OpenAll(ctx); err != nil {
		return fmt.Errorf("failed to open databases: %w", err)
	}

	e.logger.Info("database extension started")
	return nil
}

// Stop stops the extension
func (e *Extension) Stop(ctx context.Context) error {
	e.logger.Info("stopping database extension")

	// Close all databases
	if err := e.manager.CloseAll(ctx); err != nil {
		return fmt.Errorf("failed to close databases: %w", err)
	}

	e.logger.Info("database extension stopped")
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
			e.logger.Warn("database unhealthy", logger.String("name", name), logger.String("error", status.Message))
		}
	}

	if unhealthy > 0 {
		return fmt.Errorf("%d databases unhealthy", unhealthy)
	}

	return nil
}
