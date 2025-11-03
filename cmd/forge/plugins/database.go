package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"gopkg.in/yaml.v3"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/extensions/database"
)

// DatabasePlugin handles database operations
type DatabasePlugin struct {
	config *config.ForgeConfig
}

// NewDatabasePlugin creates a new database plugin
func NewDatabasePlugin(cfg *config.ForgeConfig) cli.Plugin {
	return &DatabasePlugin{config: cfg}
}

func (p *DatabasePlugin) Name() string           { return "database" }
func (p *DatabasePlugin) Version() string        { return "1.0.0" }
func (p *DatabasePlugin) Description() string    { return "Database management tools" }
func (p *DatabasePlugin) Dependencies() []string { return nil }
func (p *DatabasePlugin) Initialize() error      { return nil }

func (p *DatabasePlugin) Commands() []cli.Command {
	// Create main db command with subcommands
	dbCmd := cli.NewCommand(
		"db",
		"Database management commands",
		nil, // No handler, requires subcommand
		cli.WithAliases("database"),
	)

	// Add subcommands
	dbCmd.AddSubcommand(cli.NewCommand(
		"init",
		"Initialize migration tables",
		p.initMigrations,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
		cli.WithFlag(cli.NewBoolFlag("verbose", "v", "Verbose output", false)),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"migrate",
		"Run pending migrations",
		p.runMigrations,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
		cli.WithFlag(cli.NewBoolFlag("verbose", "v", "Verbose output", false)),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"rollback",
		"Rollback last migration group",
		p.rollbackMigrations,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
		cli.WithFlag(cli.NewBoolFlag("verbose", "v", "Verbose output", false)),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"status",
		"Show migration status",
		p.migrationStatus,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"reset",
		"Reset database (rollback all and rerun)",
		p.resetDatabase,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
		cli.WithFlag(cli.NewBoolFlag("force", "f", "Skip confirmation", false)),
		cli.WithFlag(cli.NewBoolFlag("verbose", "v", "Verbose output", false)),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"create-sql",
		"Create up and down SQL migration files",
		p.createSQLMigration,
		cli.WithFlag(cli.NewBoolFlag("tx", "", "Create transactional migrations (.tx.up.sql)", false)),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"create-go",
		"Create Go migration file",
		p.createGoMigration,
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"lock",
		"Lock migrations (prevent concurrent runs)",
		p.lockMigrations,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"unlock",
		"Unlock migrations",
		p.unlockMigrations,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"mark-applied",
		"Mark migrations as applied without running them",
		p.markApplied,
		cli.WithFlag(cli.NewStringFlag("database", "d", "Database name from config", "default")),
		cli.WithFlag(cli.NewStringFlag("dsn", "", "Override database DSN/connection string", "")),
		cli.WithFlag(cli.NewStringFlag("type", "t", "Override database type (postgres|mysql|sqlite|mongodb)", "")),
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-specific config", "")),
	))

	return []cli.Command{dbCmd}
}

func (p *DatabasePlugin) initMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	dbName := ctx.String("database")

	// Ensure migrations directory and migrations.go exist
	migrationPath, err := p.getMigrationPath()
	if err != nil {
		return err
	}

	// Create migrations.go if it doesn't exist
	migrationsGoPath := filepath.Join(migrationPath, "migrations.go")
	if _, err := os.Stat(migrationsGoPath); os.IsNotExist(err) {
		if err := p.createMigrationsGoFile(migrationsGoPath); err != nil {
			return fmt.Errorf("failed to create migrations.go: %w", err)
		}
		ctx.Println("")
		ctx.Success(fmt.Sprintf("Created: %s", migrationsGoPath))
	}

	// Check if there are Go migrations
	hasGo, err := p.hasGoMigrations()
	if err != nil {
		return fmt.Errorf("failed to check for Go migrations: %w", err)
	}

	// If Go migrations exist, use the enhanced runner
	if hasGo {
		return p.runWithGoMigrations(ctx, "init")
	}

	// Otherwise, use the standard SQL-only approach
	spinner := ctx.Spinner(fmt.Sprintf("Initializing migrations for %s...", dbName))

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	// Create migration manager
	manager := database.NewMigrationManager(db, migrations, &cliLoggerAdapter{ctx: ctx})

	// Initialize migration tables
	if err := manager.CreateTables(context.Background()); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Migration tables created!"))
	return nil
}

func (p *DatabasePlugin) runMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	// Check if there are Go migrations
	hasGo, err := p.hasGoMigrations()
	if err != nil {
		return fmt.Errorf("failed to check for Go migrations: %w", err)
	}

	// If Go migrations exist, use the enhanced runner
	if hasGo {
		return p.runWithGoMigrations(ctx, "migrate")
	}

	// Otherwise, use the standard SQL-only approach
	dbName := ctx.String("database")
	spinner := ctx.Spinner(fmt.Sprintf("Running migrations on %s...", dbName))

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	// Create migration manager
	manager := database.NewMigrationManager(db, migrations, &cliLoggerAdapter{ctx: ctx})

	// Run migrations
	if err := manager.Migrate(context.Background()); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Migrations completed!"))
	return nil
}

func (p *DatabasePlugin) rollbackMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	dbName := ctx.String("database")

	// Confirm rollback
	confirm, err := ctx.Confirm(fmt.Sprintf("Rollback last migration group on %s?", dbName))
	if err != nil || !confirm {
		ctx.Info("Rollback cancelled")
		return nil
	}

	// Check if there are Go migrations
	hasGo, err := p.hasGoMigrations()
	if err != nil {
		return fmt.Errorf("failed to check for Go migrations: %w", err)
	}

	// If Go migrations exist, use the enhanced runner
	if hasGo {
		return p.runWithGoMigrations(ctx, "rollback")
	}

	// Otherwise, use the standard SQL-only approach
	spinner := ctx.Spinner(fmt.Sprintf("Rolling back migrations on %s...", dbName))

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	// Create migration manager
	manager := database.NewMigrationManager(db, migrations, &cliLoggerAdapter{ctx: ctx})

	// Rollback migrations
	if err := manager.Rollback(context.Background()); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Rollback completed!"))
	return nil
}

func (p *DatabasePlugin) migrationStatus(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	// Check if there are Go migrations
	hasGo, err := p.hasGoMigrations()
	if err != nil {
		return fmt.Errorf("failed to check for Go migrations: %w", err)
	}

	// If Go migrations exist, use the enhanced runner
	if hasGo {
		return p.runWithGoMigrations(ctx, "status")
	}

	// Otherwise, use the standard SQL-only approach
	dbName := ctx.String("database")

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		return err
	}

	// Create migration manager
	manager := database.NewMigrationManager(db, migrations, &cliLoggerAdapter{ctx: ctx})

	// Get status
	status, err := manager.Status(context.Background())
	if err != nil {
		return err
	}

	// Display status
	ctx.Println("")
	ctx.Success(fmt.Sprintf("Migration Status for %s:", dbName))
	ctx.Println("")

	if len(status.Applied) > 0 {
		ctx.Info(fmt.Sprintf("Applied Migrations (%d):", len(status.Applied)))
		for _, mig := range status.Applied {
			ctx.Println(fmt.Sprintf("  ✓ %s (Group: %d, Applied: %s)",
				mig.Name,
				mig.GroupID,
				mig.AppliedAt.Format("2006-01-02 15:04:05"),
			))
		}
		ctx.Println("")
	}

	if len(status.Pending) > 0 {
		ctx.Info(fmt.Sprintf("Pending Migrations (%d):", len(status.Pending)))
		for _, name := range status.Pending {
			ctx.Println(fmt.Sprintf("  ⏸ %s", name))
		}
		ctx.Println("")
		ctx.Info("Run 'forge db migrate' to apply pending migrations")
	} else {
		ctx.Success("All migrations applied!")
	}

	return nil
}

func (p *DatabasePlugin) resetDatabase(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	dbName := ctx.String("database")
	force := ctx.Bool("force")

	// Confirm reset
	if !force {
		ctx.Error(fmt.Errorf("⚠️  WARNING: This will rollback ALL migrations and re-run them"))
		ctx.Error(fmt.Errorf("⚠️  This is a DESTRUCTIVE operation"))
		ctx.Println("")

		confirm, err := ctx.Confirm(fmt.Sprintf("Reset database %s?", dbName))
		if err != nil || !confirm {
			ctx.Info("Reset cancelled")
			return nil
		}
	}

	spinner := ctx.Spinner(fmt.Sprintf("Resetting database %s...", dbName))

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	// Create migration manager
	manager := database.NewMigrationManager(db, migrations, &cliLoggerAdapter{ctx: ctx})

	// Reset database
	if err := manager.Reset(context.Background()); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Database reset completed!"))
	return nil
}

func (p *DatabasePlugin) createSQLMigration(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	// Get migration name from args
	args := ctx.Args()
	if len(args) == 0 {
		return fmt.Errorf("migration name required. Usage: forge db create-sql <name>")
	}

	name := strings.Join(args, "_")
	useTx := ctx.Bool("tx")

	// Get migrations directory path
	migrationPath, err := p.getMigrationPath()
	if err != nil {
		return err
	}

	// Create migrations with directory option
	migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(migrationPath))

	// Create migrator without database connection
	migrator := migrate.NewMigrator(nil, migrations)

	spinner := ctx.Spinner(fmt.Sprintf("Creating SQL migration '%s'...", name))

	var files []*migrate.MigrationFile
	if useTx {
		files, err = migrator.CreateTxSQLMigrations(context.Background(), name)
	} else {
		files, err = migrator.CreateSQLMigrations(context.Background(), name)
	}

	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Migration files created!"))
	ctx.Println("")
	for _, mf := range files {
		ctx.Success(fmt.Sprintf("Created: %s", mf.Path))
	}

	return nil
}

func (p *DatabasePlugin) createGoMigration(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	// Get migration name from args
	args := ctx.Args()
	if len(args) == 0 {
		return fmt.Errorf("migration name required. Usage: forge db create-go <name>")
	}

	name := strings.Join(args, "_")

	// Get migrations directory path
	migrationPath, err := p.getMigrationPath()
	if err != nil {
		return err
	}

	// Create migrations with directory option
	migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(migrationPath))

	// Create migrator without database connection
	migrator := migrate.NewMigrator(nil, migrations)

	spinner := ctx.Spinner(fmt.Sprintf("Creating Go migration '%s'...", name))

	mf, err := migrator.CreateGoMigration(context.Background(), name)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Migration file created!"))
	ctx.Println("")
	ctx.Success(fmt.Sprintf("Created: %s", mf.Path))

	return nil
}

func (p *DatabasePlugin) lockMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	spinner := ctx.Spinner("Locking migrations...")

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	migrator := migrate.NewMigrator(db, migrations)

	// Lock migrations
	if err := migrator.Lock(context.Background()); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Migrations locked!"))
	return nil
}

func (p *DatabasePlugin) unlockMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	spinner := ctx.Spinner("Unlocking migrations...")

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	migrator := migrate.NewMigrator(db, migrations)

	// Unlock migrations
	if err := migrator.Unlock(context.Background()); err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	spinner.Stop(cli.Green("✓ Migrations unlocked!"))
	return nil
}

func (p *DatabasePlugin) markApplied(ctx cli.CommandContext) error {
	if p.config == nil {
		return fmt.Errorf("not a forge project")
	}

	dbName := ctx.String("database")

	// Confirm action
	ctx.Println("")
	ctx.Info("⚠️  This will mark pending migrations as applied WITHOUT running them")
	ctx.Info("⚠️  Use this only if migrations were applied manually")
	ctx.Println("")

	confirm, err := ctx.Confirm("Mark pending migrations as applied?")
	if err != nil || !confirm {
		ctx.Info("Operation cancelled")
		return nil
	}

	spinner := ctx.Spinner(fmt.Sprintf("Marking migrations as applied on %s...", dbName))

	// Load migrations
	migrations, err := p.loadMigrations()
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Get database connection
	db, err := p.getDatabaseConnection(ctx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	migrator := migrate.NewMigrator(db, migrations)

	// Mark migrations as applied using WithNopMigration
	group, err := migrator.Migrate(context.Background(), migrate.WithNopMigration())
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))
		return err
	}

	if group.IsZero() {
		spinner.Stop(cli.Green("✓ No pending migrations"))
		ctx.Info("All migrations are already applied")
		return nil
	}

	spinner.Stop(cli.Green("✓ Migrations marked as applied!"))
	ctx.Success(fmt.Sprintf("Marked group %s as applied", group))
	return nil
}

// Helper functions

func (p *DatabasePlugin) loadMigrations() (*migrate.Migrations, error) {
	// Try multiple possible migration paths
	possiblePaths := []string{
		filepath.Join(p.config.RootDir, "migrations"),             // Standard location
		filepath.Join(p.config.RootDir, "database", "migrations"), // Alternative location
	}

	var migrationPath string
	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			migrationPath = path
			break
		}
	}

	// If no migrations directory exists, create one
	if migrationPath == "" {
		migrationPath = filepath.Join(p.config.RootDir, "migrations")
		if err := os.MkdirAll(migrationPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create migrations directory: %w", err)
		}
	}

	// Create migrations collection and discover SQL files
	migrations := migrate.NewMigrations()

	// Discover SQL migration files from the filesystem
	if err := migrations.Discover(os.DirFS(migrationPath)); err != nil {
		return nil, fmt.Errorf("failed to discover migrations in %s: %w", migrationPath, err)
	}

	return migrations, nil
}

func (p *DatabasePlugin) getMigrationPath() (string, error) {
	// Try multiple possible migration paths
	possiblePaths := []string{
		filepath.Join(p.config.RootDir, "migrations"),             // Standard location
		filepath.Join(p.config.RootDir, "database", "migrations"), // Alternative location
	}

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path, nil
		}
	}

	// Default to migrations/ if none exist
	migrationPath := filepath.Join(p.config.RootDir, "migrations")
	if err := os.MkdirAll(migrationPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create migrations directory: %w", err)
	}

	return migrationPath, nil
}

func (p *DatabasePlugin) createMigrationsGoFile(path string) error {
	content := `package migrations

import "github.com/xraph/forge/extensions/database"

// Migrations is the application's migration collection
// It references the global Migrations from the database extension
var Migrations = database.Migrations

func init() {
	// Discover Go migration files in this directory
	// This allows the migrations to be automatically registered
	if err := Migrations.DiscoverCaller(); err != nil {
		panic(err)
	}
}
`
	return os.WriteFile(path, []byte(content), 0644)
}

func (p *DatabasePlugin) getDatabaseConnection(ctx cli.CommandContext) (*bun.DB, error) {
	dbName := ctx.String("database")
	customDSN := ctx.String("dsn")
	customType := ctx.String("type")
	appName := ctx.String("app")

	// Load database config from forge config hierarchy
	dbConfig, err := p.loadDatabaseConfig(dbName, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load database config: %w", err)
	}

	// Override with command-line flags if provided
	if customDSN != "" {
		dbConfig.DSN = customDSN
	}
	if customType != "" {
		dbConfig.Type = database.DatabaseType(customType)
	}

	// Validate config
	if dbConfig.DSN == "" {
		return nil, fmt.Errorf("database DSN not configured for '%s'. Use --dsn flag or configure in config.yaml", dbName)
	}

	// Create database connection using the database extension
	switch dbConfig.Type {
	case database.TypePostgres, database.TypeMySQL, database.TypeSQLite:
		// Use noop logger for CLI - we don't need detailed database logs in CLI context
		logger := forge.NewNoopLogger()

		sqlDB, err := database.NewSQLDatabase(dbConfig, logger, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create database: %w", err)
		}
		if err := sqlDB.Open(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to connect to database: %w", err)
		}
		return sqlDB.Bun(), nil
	case database.TypeMongoDB:
		return nil, fmt.Errorf("mongodb migrations not supported via CLI yet - use application context")
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbConfig.Type)
	}
}

// loadDatabaseConfig loads database configuration from the forge config hierarchy
func (p *DatabasePlugin) loadDatabaseConfig(dbName, appName string) (database.DatabaseConfig, error) {
	var dbConfig database.DatabaseConfig

	// Config file paths in priority order (lowest to highest)
	configPaths := []string{
		filepath.Join(p.config.RootDir, "config.yaml"),                 // Global config (root)
		filepath.Join(p.config.RootDir, "config", "config.yaml"),       // Global config (config/)
		filepath.Join(p.config.RootDir, "config.local.yaml"),           // Local global override (root)
		filepath.Join(p.config.RootDir, "config", "config.local.yaml"), // Local override (config/)
	}

	// Add app-specific configs if app name provided
	if appName != "" {
		configPaths = append(configPaths,
			filepath.Join(p.config.RootDir, "apps", appName, "config.yaml"),       // App config
			filepath.Join(p.config.RootDir, "apps", appName, "config.local.yaml"), // App local config
		)
	}

	// Load and merge configs
	found := false
	for _, path := range configPaths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		// Support both array and map formats
		var cfg struct {
			Database struct {
				Databases []database.DatabaseConfig          `yaml:"databases"` // Array format
				Map       map[string]database.DatabaseConfig `yaml:",inline"`   // Map format
			} `yaml:"database"`
			Apps map[string]struct {
				Database struct {
					Databases []database.DatabaseConfig          `yaml:"databases"`
					Map       map[string]database.DatabaseConfig `yaml:",inline"`
				} `yaml:"database"`
			} `yaml:"apps"`
		}

		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		// Look for database in global config (array format)
		for _, db := range cfg.Database.Databases {
			if db.Name == dbName {
				// Merge configs (later configs override earlier ones)
				if !found || db.DSN != "" {
					dbConfig.Name = db.Name
					if db.DSN != "" {
						dbConfig.DSN = db.DSN
					}
					if db.Type != "" {
						dbConfig.Type = db.Type
					}
					if db.MaxOpenConns > 0 {
						dbConfig.MaxOpenConns = db.MaxOpenConns
					}
					if db.MaxIdleConns > 0 {
						dbConfig.MaxIdleConns = db.MaxIdleConns
					}
					if db.MaxRetries > 0 {
						dbConfig.MaxRetries = db.MaxRetries
					}
					if db.ConnectionTimeout > 0 {
						dbConfig.ConnectionTimeout = db.ConnectionTimeout
					}
					if db.QueryTimeout > 0 {
						dbConfig.QueryTimeout = db.QueryTimeout
					}
					if db.SlowQueryThreshold > 0 {
						dbConfig.SlowQueryThreshold = db.SlowQueryThreshold
					}
					found = true
				}
			}
		}

		// Look for database in global config (map format)
		if db, ok := cfg.Database.Map[dbName]; ok {
			// Merge configs (later configs override earlier ones)
			if !found || db.DSN != "" {
				dbConfig.Name = dbName // Use the key as the name
				if db.DSN != "" {
					dbConfig.DSN = db.DSN
				}
				if db.Type != "" {
					dbConfig.Type = db.Type
				}
				if db.MaxOpenConns > 0 {
					dbConfig.MaxOpenConns = db.MaxOpenConns
				}
				if db.MaxIdleConns > 0 {
					dbConfig.MaxIdleConns = db.MaxIdleConns
				}
				if db.MaxRetries > 0 {
					dbConfig.MaxRetries = db.MaxRetries
				}
				if db.ConnectionTimeout > 0 {
					dbConfig.ConnectionTimeout = db.ConnectionTimeout
				}
				if db.QueryTimeout > 0 {
					dbConfig.QueryTimeout = db.QueryTimeout
				}
				if db.SlowQueryThreshold > 0 {
					dbConfig.SlowQueryThreshold = db.SlowQueryThreshold
				}
				found = true
			}
		}

		// Look for database in app-specific config
		if appName != "" {
			if appCfg, ok := cfg.Apps[appName]; ok {
				// Check array format
				for _, db := range appCfg.Database.Databases {
					if db.Name == dbName {
						// App config overrides global config
						if !found || db.DSN != "" {
							dbConfig.Name = db.Name
							if db.DSN != "" {
								dbConfig.DSN = db.DSN
							}
							if db.Type != "" {
								dbConfig.Type = db.Type
							}
							if db.MaxOpenConns > 0 {
								dbConfig.MaxOpenConns = db.MaxOpenConns
							}
							if db.MaxIdleConns > 0 {
								dbConfig.MaxIdleConns = db.MaxIdleConns
							}
							if db.MaxRetries > 0 {
								dbConfig.MaxRetries = db.MaxRetries
							}
							if db.ConnectionTimeout > 0 {
								dbConfig.ConnectionTimeout = db.ConnectionTimeout
							}
							if db.QueryTimeout > 0 {
								dbConfig.QueryTimeout = db.QueryTimeout
							}
							if db.SlowQueryThreshold > 0 {
								dbConfig.SlowQueryThreshold = db.SlowQueryThreshold
							}
							found = true
						}
					}
				}

				// Check map format
				if db, ok := appCfg.Database.Map[dbName]; ok {
					// App config overrides global config
					if !found || db.DSN != "" {
						dbConfig.Name = dbName
						if db.DSN != "" {
							dbConfig.DSN = db.DSN
						}
						if db.Type != "" {
							dbConfig.Type = db.Type
						}
						if db.MaxOpenConns > 0 {
							dbConfig.MaxOpenConns = db.MaxOpenConns
						}
						if db.MaxIdleConns > 0 {
							dbConfig.MaxIdleConns = db.MaxIdleConns
						}
						if db.MaxRetries > 0 {
							dbConfig.MaxRetries = db.MaxRetries
						}
						if db.ConnectionTimeout > 0 {
							dbConfig.ConnectionTimeout = db.ConnectionTimeout
						}
						if db.QueryTimeout > 0 {
							dbConfig.QueryTimeout = db.QueryTimeout
						}
						if db.SlowQueryThreshold > 0 {
							dbConfig.SlowQueryThreshold = db.SlowQueryThreshold
						}
						found = true
					}
				}
			}
		}
	}

	if !found {
		return dbConfig, fmt.Errorf("database '%s' not found in config files", dbName)
	}

	// Set defaults
	if dbConfig.MaxOpenConns == 0 {
		dbConfig.MaxOpenConns = 25
	}
	if dbConfig.MaxIdleConns == 0 {
		dbConfig.MaxIdleConns = 25
	}
	if dbConfig.MaxRetries == 0 {
		dbConfig.MaxRetries = 3
	}

	// Expand environment variables in DSN
	dbConfig.DSN = expandEnvVars(dbConfig.DSN)

	return dbConfig, nil
}

// expandEnvVars expands environment variables in format ${VAR} or ${VAR:default}
func expandEnvVars(s string) string {
	// Pattern matches ${VAR} or ${VAR:default}
	pattern := regexp.MustCompile(`\$\{([^}:]+)(?::([^}]*))?\}`)

	return pattern.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name and default value
		parts := pattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}

		varName := parts[1]
		defaultValue := ""
		if len(parts) > 2 {
			defaultValue = parts[2]
		}

		// Get environment variable
		if value := os.Getenv(varName); value != "" {
			return value
		}

		return defaultValue
	})
}

// cliLoggerAdapter adapts CLI context to database.Logger interface
type cliLoggerAdapter struct {
	ctx cli.CommandContext
}

func (l *cliLoggerAdapter) Info(msg string, fields ...interface{}) {
	l.ctx.Info(msg)
}

func (l *cliLoggerAdapter) Error(msg string, fields ...interface{}) {
	l.ctx.Error(fmt.Errorf("%s", msg))
}

func (l *cliLoggerAdapter) Warn(msg string, fields ...interface{}) {
	l.ctx.Info(fmt.Sprintf("⚠️  %s", msg))
}
