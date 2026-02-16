package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/vessel"
)

// Extension implements the database extension.
// The extension is now a lightweight facade that loads config and registers services.
// Service lifecycle is managed by Vessel, not by the extension.
type Extension struct {
	*forge.BaseExtension

	config Config
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

	// Load and validate configuration
	if err := e.loadConfiguration(); err != nil {
		return err
	}

	// Register database manager
	if err := e.registerDatabaseManager(); err != nil {
		return err
	}

	// Register default database and type-specific accessors
	defaultName := e.determineDefaultDatabase()
	if defaultName != "" {
		if err := e.registerDefaultDatabase(defaultName); err != nil {
			return err
		}
	}

	e.Logger().Info("database extension registered",
		forge.F("databases", len(e.config.Databases)),
		forge.F("default", defaultName),
	)

	return nil
}

// loadConfiguration loads configuration from YAML files or programmatic sources.
func (e *Extension) loadConfiguration() error {
	programmaticConfig := e.config
	hasProgrammaticDatabases := e.hasProgrammaticDatabases(programmaticConfig)

	// Try loading from config file
	finalConfig, configLoaded := e.tryLoadFromConfigFile()

	// Handle config not found
	if !configLoaded {
		if programmaticConfig.RequireConfig {
			return errors.New("database: configuration is required but not found in config files. " +
				"Ensure 'extensions.database' or 'database' key exists in your config.yaml")
		}

		finalConfig = e.selectProgrammaticOrDefaultConfig(programmaticConfig, hasProgrammaticDatabases)
	} else {
		// Config loaded from YAML - merge with programmatic databases if provided
		finalConfig = e.mergeConfigurations(finalConfig, programmaticConfig, hasProgrammaticDatabases)
	}

	e.Logger().Debug("database: configuration loaded",
		forge.F("databases", len(finalConfig.Databases)),
		forge.F("default", finalConfig.Default),
	)

	e.config = finalConfig

	// Validate configuration
	if err := e.config.Validate(); err != nil {
		return fmt.Errorf("invalid database configuration: %w", err)
	}

	return nil
}

// hasProgrammaticDatabases determines if actual programmatic databases are provided.
// NewExtension starts from Config{}, so any entries in Databases were explicitly
// added by the caller via WithDatabase / WithDatabases options.
func (e *Extension) hasProgrammaticDatabases(config Config) bool {
	return len(config.Databases) > 0
}

// tryLoadFromConfigFile attempts to load configuration from YAML files.
func (e *Extension) tryLoadFromConfigFile() (Config, bool) {
	cm := e.App().Config()
	var finalConfig Config

	// Try "extensions.database" first (namespaced pattern)
	if cm.IsSet("extensions.database") {
		if err := cm.Bind("extensions.database", &finalConfig); err == nil {
			e.Logger().Debug("database: loaded from config file",
				forge.F("key", "extensions.database"),
				forge.F("databases", len(finalConfig.Databases)),
			)
			return finalConfig, true
		} else {
			e.Logger().Warn("database: failed to bind extensions.database", forge.F("error", err))
		}
	}

	// Try legacy "database" key if not loaded yet
	if cm.IsSet("database") {
		if err := cm.Bind("database", &finalConfig); err == nil {
			e.Logger().Debug("database: loaded from config file",
				forge.F("key", "database"),
				forge.F("databases", len(finalConfig.Databases)),
			)
			return finalConfig, true
		} else {
			e.Logger().Warn("database: failed to bind database", forge.F("error", err))
		}
	}

	return Config{}, false
}

// selectProgrammaticOrDefaultConfig selects between programmatic config and defaults.
func (e *Extension) selectProgrammaticOrDefaultConfig(programmaticConfig Config, hasProgrammaticDatabases bool) Config {
	if hasProgrammaticDatabases {
		e.Logger().Debug("database: using programmatic configuration")
		return programmaticConfig
	}

	e.Logger().Debug("database: using default configuration")
	return DefaultConfig()
}

// mergeConfigurations merges YAML config with programmatic databases.
func (e *Extension) mergeConfigurations(yamlConfig, programmaticConfig Config, hasProgrammaticDatabases bool) Config {
	if !hasProgrammaticDatabases {
		return yamlConfig
	}

	e.Logger().Debug("database: merging programmatic databases")

	// Build a set of existing database names from YAML config
	existingNames := make(map[string]bool)
	for _, db := range yamlConfig.Databases {
		existingNames[db.Name] = true
	}

	// Only append programmatic databases that don't already exist
	// YAML config takes precedence over programmatic config
	skipped := 0
	for _, db := range programmaticConfig.Databases {
		if existingNames[db.Name] {
			e.Logger().Warn("database: skipping duplicate programmatic database (YAML config takes precedence)",
				forge.F("name", db.Name))
			skipped++
			continue
		}
		yamlConfig.Databases = append(yamlConfig.Databases, db)
	}

	e.Logger().Debug("database: merged programmatic databases",
		forge.F("added", len(programmaticConfig.Databases)-skipped),
		forge.F("skipped", skipped))

	if programmaticConfig.Default != "" {
		yamlConfig.Default = programmaticConfig.Default
	}

	return yamlConfig
}

// registerDatabaseManager registers the DatabaseManager constructor with Vessel.
func (e *Extension) registerDatabaseManager() error {
	finalConfig := e.config

	return e.RegisterConstructor(func(logger forge.Logger, metrics forge.Metrics) (*DatabaseManager, error) {
		manager := NewDatabaseManager(logger, metrics)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Register and open all configured databases immediately
		for _, dbConfig := range finalConfig.Databases {
			db, err := e.createDatabase(dbConfig, logger, metrics)
			if err != nil {
				return nil, fmt.Errorf("failed to create database %s: %w", dbConfig.Name, err)
			}

			if err := manager.RegisterAndOpen(ctx, dbConfig.Name, db); err != nil {
				return nil, err
			}
		}

		// Set default database if configured
		defaultName := e.determineDefaultDatabase()
		if defaultName != "" {
			if err := manager.SetDefault(defaultName); err != nil {
				return nil, fmt.Errorf("failed to set default database: %w", err)
			}
		}

		return manager, nil
	}, vessel.WithAliases(ManagerKey), vessel.WithEager())
}

// createDatabase creates a database instance based on its type.
func (e *Extension) createDatabase(config DatabaseConfig, logger forge.Logger, metrics forge.Metrics) (Database, error) {
	switch config.Type {
	case TypePostgres, TypeMySQL, TypeSQLite:
		return NewSQLDatabase(config, logger, metrics)
	case TypeMongoDB:
		return NewMongoDatabase(config, logger, metrics)
	case TypeRedis:
		return NewRedisDatabase(config, logger, metrics)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", config.Type)
	}
}

// determineDefaultDatabase determines the default database name.
func (e *Extension) determineDefaultDatabase() string {
	if e.config.Default != "" {
		return e.config.Default
	}
	if len(e.config.Databases) > 0 {
		return e.config.Databases[0].Name
	}
	return ""
}

// registerDefaultDatabase registers the default database and type-specific accessors.
func (e *Extension) registerDefaultDatabase(defaultName string) error {
	// Register default database interface
	if err := vessel.Provide(e.App().Container(), func() (Database, error) {
		manager, err := vessel.Inject[*DatabaseManager](e.App().Container())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve database manager: %w", err)
		}

		db, err := manager.Get(defaultName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve database: %w", err)
		}

		return db, nil
	}, vessel.WithAliases(defaultName), vessel.WithEager()); err != nil {
		return fmt.Errorf("failed to register default database: %w", err)
	}

	// Get the default database config
	defaultConfig := e.findDatabaseConfig(defaultName)
	if defaultConfig == nil {
		return fmt.Errorf("default database %s not found in configuration", defaultName)
	}

	// Register type-specific accessors
	return e.registerTypeSpecificAccessors(defaultName, defaultConfig.Type)
}

// findDatabaseConfig finds a database configuration by name.
func (e *Extension) findDatabaseConfig(name string) *DatabaseConfig {
	for i := range e.config.Databases {
		if e.config.Databases[i].Name == name {
			return &e.config.Databases[i]
		}
	}
	return nil
}

// registerTypeSpecificAccessors registers type-specific database accessors.
func (e *Extension) registerTypeSpecificAccessors(defaultName string, dbType DatabaseType) error {
	switch dbType {
	case TypePostgres, TypeMySQL, TypeSQLite:
		return e.registerSQLAccessor(defaultName)
	case TypeMongoDB:
		return e.registerMongoAccessor(defaultName)
	case TypeRedis:
		return e.registerRedisAccessor(defaultName)
	}
	return nil
}

// registerSQLAccessor registers the Bun DB accessor.
func (e *Extension) registerSQLAccessor(defaultName string) error {
	return vessel.Provide(e.App().Container(), func() (*bun.DB, error) {
		manager, err := vessel.Inject[*DatabaseManager](e.App().Container())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve database manager: %w", err)
		}
		return manager.SQL(defaultName)
	}, vessel.WithName("sql-"+defaultName), vessel.WithEager())
}

// registerMongoAccessor registers the MongoDB client accessor.
func (e *Extension) registerMongoAccessor(defaultName string) error {
	return vessel.Provide(e.App().Container(), func() (*mongo.Client, error) {
		manager, err := vessel.Inject[*DatabaseManager](e.App().Container())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve database manager: %w", err)
		}
		return manager.Mongo(defaultName)
	}, vessel.WithName("mongo-"+defaultName), vessel.WithEager())
}

// registerRedisAccessor registers the Redis client accessor.
func (e *Extension) registerRedisAccessor(defaultName string) error {
	return vessel.Provide(e.App().Container(), func() (redis.UniversalClient, error) {
		manager, err := vessel.Inject[*DatabaseManager](e.App().Container())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve database manager: %w", err)
		}
		return manager.Redis(defaultName)
	}, vessel.WithName("redis-"+defaultName), vessel.WithEager())
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
