package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/database"
)

// DatabasePlugin handles database operations.
type DatabasePlugin struct {
	config *config.ForgeConfig
}

// NewDatabasePlugin creates a new database plugin.
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
		return errors.New("not a forge project")
	}

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
		ctx.Success("✓ Created: " + migrationsGoPath)
	} else {
		ctx.Println("")
		ctx.Info("✓ Migration structure already exists: " + migrationPath)
	}

	ctx.Println("")
	ctx.Info("📚 Next steps:")
	ctx.Info("   1. Create migrations with: forge generate migration <name>")
	ctx.Info("   2. Run migrations with: forge db migrate")
	ctx.Println("")

	return nil
}

func (p *DatabasePlugin) runMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
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
		return errors.New("not a forge project")
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
		return errors.New("not a forge project")
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
			ctx.Println("  ⏸ " + name)
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
		return errors.New("not a forge project")
	}

	dbName := ctx.String("database")
	force := ctx.Bool("force")

	// Confirm reset
	if !force {
		ctx.Error(errors.New("⚠️  WARNING: This will rollback ALL migrations and re-run them"))
		ctx.Error(errors.New("⚠️  This is a DESTRUCTIVE operation"))
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
		return errors.New("not a forge project")
	}

	// Get migration name from args
	args := ctx.Args()
	if len(args) == 0 {
		return errors.New("migration name required. Usage: forge db create-sql <name>")
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
		ctx.Success("Created: " + mf.Path)
	}

	return nil
}

func (p *DatabasePlugin) createGoMigration(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	// Get migration name from args
	args := ctx.Args()
	if len(args) == 0 {
		return errors.New("migration name required. Usage: forge db create-go <name>")
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
	ctx.Success("Created: " + mf.Path)

	return nil
}

func (p *DatabasePlugin) lockMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
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
		return errors.New("not a forge project")
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
		return errors.New("not a forge project")
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
	var migrationPath string

	// First priority: Check if migrations_path is configured in .forge.yaml
	if p.config != nil && p.config.Database.MigrationsPath != "" {
		migrationPath = p.config.Database.MigrationsPath

		// Make relative paths absolute based on project root
		if !filepath.IsAbs(migrationPath) {
			migrationPath = filepath.Join(p.config.RootDir, migrationPath)
		}
	} else {
		// Fallback: Try multiple possible migration paths
		possiblePaths := []string{
			filepath.Join(p.config.RootDir, "migrations"),             // Standard location
			filepath.Join(p.config.RootDir, "database", "migrations"), // Alternative location
		}

		for _, path := range possiblePaths {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				migrationPath = path

				break
			}
		}
	}

	// If no migrations directory found, use default
	if migrationPath == "" {
		migrationPath = filepath.Join(p.config.RootDir, "migrations")
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(migrationPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create migrations directory: %w", err)
	}

	// Create migrations collection and discover SQL files
	migrations := migrate.NewMigrations()

	// Discover SQL migration files from the filesystem
	if err := migrations.Discover(os.DirFS(migrationPath)); err != nil {
		return nil, fmt.Errorf("failed to discover migrations in %s: %w", migrationPath, err)
	}

	// Check if any migrations were discovered
	sorted := migrations.Sorted()
	if len(sorted) == 0 {
		return nil, fmt.Errorf("no migration files found in %s\n\nTo create a migration, run:\n  forge db create-sql <migration_name>\n  forge db create-go <migration_name>", migrationPath)
	}

	return migrations, nil
}

func (p *DatabasePlugin) getMigrationPath() (string, error) {
	// First priority: Check if migrations_path is configured in .forge.yaml
	if p.config != nil && p.config.Database.MigrationsPath != "" {
		migrationPath := p.config.Database.MigrationsPath

		// Make relative paths absolute based on project root
		if !filepath.IsAbs(migrationPath) {
			migrationPath = filepath.Join(p.config.RootDir, migrationPath)
		}

		// Check if the configured path exists
		if info, err := os.Stat(migrationPath); err == nil && info.IsDir() {
			return migrationPath, nil
		}

		// If configured but doesn't exist, create it
		if err := os.MkdirAll(migrationPath, 0755); err != nil {
			return "", fmt.Errorf("failed to create configured migrations directory %s: %w", migrationPath, err)
		}

		return migrationPath, nil
	}

	// Fallback: Try multiple possible migration paths
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

import (
	"sync"

	"github.com/xraph/forge/extensions/database"
)

// Migrations is the application's migration collection
// It references the global Migrations from the database extension
var Migrations = database.Migrations

var (
	discoveryOnce sync.Once
	discoveryErr  error
)

// EnsureDiscovered ensures migrations are discovered from the filesystem.
// This is called automatically when migrations are loaded, but can be called
// explicitly if you need to check for discovery errors early.
func EnsureDiscovered() error {
	discoveryOnce.Do(func() {
		// DiscoverCaller may fail in environments where the filesystem isn't accessible
		// (e.g., Docker containers, CI/CD). This is safe to ignore if migrations are
		// registered programmatically or discovered explicitly via the CLI.
		discoveryErr = Migrations.DiscoverCaller()
	})
	return discoveryErr
}

func init() {
	// Lazy discovery - don't panic on error to allow app startup in containerized environments.
	// Migrations will be discovered when actually needed (e.g., via CLI commands).
	_ = EnsureDiscovered()
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
		return nil, errors.New("mongodb migrations not supported via CLI yet - use application context")
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbConfig.Type)
	}
}

// loadDatabaseConfig loads database configuration using Forge's ConfigManager.
// This provides automatic environment variable expansion, file merging, and proper
// namespace support for both 'database' and 'extensions.database' keys.
func (p *DatabasePlugin) loadDatabaseConfig(dbName, appName string) (database.DatabaseConfig, error) {
	// CRITICAL: Load .env files BEFORE creating ConfigManager
	// expands environment variables when reading config files,
	// so .env vars must be in the environment at that point
	p.loadEnvFiles()

	// Create a temporary Forge app to access ConfigManager
	// This gives us all the benefits: file discovery, merging, env var expansion, etc.
	app := forge.NewApp(forge.AppConfig{
		Name:        "forge-cli",
		Version:     "1.0.0",
		Environment: os.Getenv("FORGE_ENV"),
		// Enable config auto-discovery to find config.yaml and config.local.yaml
		EnableConfigAutoDiscovery: true,
		ConfigSearchPaths:         []string{p.config.RootDir, filepath.Join(p.config.RootDir, "config")},
		Logger:                    forge.NewNoopLogger(),
	})

	cm := app.Config()

	// Try to load from extensions.database (new pattern) or database (legacy pattern)
	var (
		dbConfig   database.DatabaseConfig
		fullConfig database.Config
	)

	// First, try the namespaced key (preferred)

	if cm.IsSet("extensions.database") {
		if err := cm.Bind("extensions.database", &fullConfig); err != nil {
			return dbConfig, fmt.Errorf("failed to bind extensions.database config: %w", err)
		}
	} else if cm.IsSet("database") {
		// Fallback to legacy key
		if err := cm.Bind("database", &fullConfig); err != nil {
			return dbConfig, fmt.Errorf("failed to bind database config: %w", err)
		}
	} else {
		return dbConfig, fmt.Errorf("database configuration not found in config files. "+
			"Add 'extensions.database' or 'database' section to config.yaml or config.local.yaml\n\n"+
			"Example:\n"+
			"extensions:\n"+
			"  database:\n"+
			"    databases:\n"+
			"      - name: %s\n"+
			"        type: postgres\n"+
			"        dsn: postgres://user:pass@localhost:5432/dbname", dbName)
	}

	// Find the requested database
	for _, db := range fullConfig.Databases {
		if db.Name == dbName {
			// Set defaults if not specified
			if db.MaxOpenConns == 0 {
				db.MaxOpenConns = 25
			}

			if db.MaxIdleConns == 0 {
				db.MaxIdleConns = 25
			}

			if db.MaxRetries == 0 {
				db.MaxRetries = 3
			}

			return db, nil
		}
	}

	// Database not found - provide helpful error with available databases
	availableDbs := getDatabaseNames(fullConfig.Databases)
	if len(availableDbs) == 0 {
		return dbConfig, fmt.Errorf("no databases configured. Add a database to your config:\n\n"+
			"extensions:\n"+
			"  database:\n"+
			"    databases:\n"+
			"      - name: %s\n"+
			"        type: postgres\n"+
			"        dsn: ${DATABASE_DSN:-postgres://localhost:5432/dbname}", dbName)
	}

	return dbConfig, fmt.Errorf("database '%s' not found in config files.\n"+
		"Available databases: %v\n\n"+
		"Tip: Check config.yaml or config.local.yaml for 'extensions.database.databases'",
		dbName, availableDbs)
}

// getDatabaseNames extracts database names from a list of database configs.
func getDatabaseNames(databases []database.DatabaseConfig) []string {
	names := make([]string, len(databases))
	for i, db := range databases {
		names[i] = db.Name
	}

	return names
}

// loadEnvFiles loads environment variables from .env files.
// Loads in order of priority (later files override earlier ones):
//  1. .env                      (base configuration)
//  2. .env.local               (local overrides, gitignored)
//  3. .env.{environment}       (environment-specific)
//  4. .env.{environment}.local (environment-specific local overrides)
//
// This follows the standard dotenv convention used by many frameworks.
func (p *DatabasePlugin) loadEnvFiles() {
	if p.config == nil {
		return
	}

	// Determine environment (default to development)
	env := os.Getenv("FORGE_ENV")
	if env == "" {
		env = os.Getenv("GO_ENV")
	}

	if env == "" {
		env = "development"
	}

	// Files to load in priority order (earlier = lower priority)
	envFiles := []string{
		filepath.Join(p.config.RootDir, ".env"),
		filepath.Join(p.config.RootDir, ".env.local"),
	}

	// Add environment-specific files
	if env != "" {
		envFiles = append(envFiles,
			filepath.Join(p.config.RootDir, ".env."+env),
			filepath.Join(p.config.RootDir, fmt.Sprintf(".env.%s.local", env)),
		)
	}

	// Load each file that exists
	for _, envFile := range envFiles {
		if _, err := os.Stat(envFile); err == nil {
			// Load without overriding existing env vars (godotenv.Load would override)
			// We use Overload to ensure later files take precedence
			if err := godotenv.Overload(envFile); err != nil {
				// Silently continue - .env files are optional
				continue
			}
		}
	}
}

// cliLoggerAdapter adapts CLI context to database.Logger interface.
type cliLoggerAdapter struct {
	ctx cli.CommandContext
}

func (l *cliLoggerAdapter) Info(msg string, fields ...any) {
	l.ctx.Info(msg)
}

func (l *cliLoggerAdapter) Error(msg string, fields ...any) {
	l.ctx.Error(fmt.Errorf("%s", msg))
}

func (l *cliLoggerAdapter) Warn(msg string, fields ...any) {
	l.ctx.Info("⚠️  " + msg)
}
