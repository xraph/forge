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
	if p.config != nil {
		migrationPath = p.config.Database.GetMigrationsPath()

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
	if p.config != nil {
		migrationPath := p.config.Database.GetMigrationsPath()

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

// loadDatabaseConfig loads database configuration from multiple sources:
// 1. .forge.yaml (database.connections)
// 2. config.yaml (extensions.database or database)
// 3. Environment variable overrides
func (p *DatabasePlugin) loadDatabaseConfig(dbName, appName string) (database.DatabaseConfig, error) {
	// CRITICAL: Load .env files BEFORE processing config
	// expands environment variables when reading config files,
	// so .env vars must be in the environment at that point
	p.loadEnvFiles()

	var triedSources []string

	// STEP 1: Try loading from .forge.yaml database.connections
	if p.config != nil {
		if len(p.config.Database.Connections) > 0 {
			triedSources = append(triedSources, ".forge.yaml (database.connections)")
			dbConfig, err := p.loadFromForgeYaml(dbName)
			if err == nil {
				return dbConfig, nil
			}
			// If specific database not found in .forge.yaml, continue to config.yaml
		} else {
			// .forge.yaml exists but has no database.connections
			triedSources = append(triedSources, ".forge.yaml (no database.connections found)")
		}
	} else {
		triedSources = append(triedSources, ".forge.yaml (not found)")
	}

	// STEP 2: Try loading from config.yaml files
	// Manually discover and load config files
	var configFiles []string

	// Search for config files in root and config subdirectory
	searchDirs := []string{p.config.RootDir, filepath.Join(p.config.RootDir, "config")}
	configNames := []string{"config.yaml", "config.yml", "config.local.yaml", "config.local.yml"}

	for _, dir := range searchDirs {
		for _, name := range configNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				configFiles = append(configFiles, path)
				triedSources = append(triedSources, path)
			}
		}
	}

	// If no config.yaml files found, provide helpful error
	if len(configFiles) == 0 {
		return database.DatabaseConfig{}, p.buildConfigNotFoundError(dbName, triedSources)
	}

	// Create a simple Forge app that will load all the config files
	// This ensures proper merging and environment variable expansion
	app := forge.NewApp(forge.AppConfig{
		Name:                      "forge-cli",
		Version:                   "1.0.0",
		Environment:               os.Getenv("FORGE_ENV"),
		EnableConfigAutoDiscovery: true,
		ConfigSearchPaths:         searchDirs,
		ConfigBaseNames:           []string{"config.yaml", "config.yml"},
		ConfigLocalNames:          []string{"config.local.yaml", "config.local.yml"},
		EnableAppScopedConfig:     false,
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
		// Last attempt: try direct binding without IsSet check (confy sometimes has issues with IsSet for nested keys)
		err1 := cm.Bind("extensions.database", &fullConfig)
		if err1 == nil && len(fullConfig.Databases) > 0 {
			// Successfully bound even though IsSet returned false
		} else {
			// Try legacy key
			fullConfig = database.Config{} // Reset before trying again
			err2 := cm.Bind("database", &fullConfig)
			if err2 == nil && len(fullConfig.Databases) > 0 {
				// Successfully bound even though IsSet returned false
			} else {
				// Neither worked - return error
				return dbConfig, p.buildConfigNotFoundError(dbName, triedSources)
			}
		}
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
		return dbConfig, p.buildConfigNotFoundError(dbName, triedSources)
	}

	// Config exists but specific database not found
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("Database '%s' not found in config.yaml files.\n\n", dbName))
	msg.WriteString(fmt.Sprintf("Available databases in config: %v\n\n", availableDbs))

	// Show .forge.yaml connections if they exist
	if p.config != nil && len(p.config.Database.Connections) > 0 {
		forgeConnections := p.getForgeYamlConnectionNames()
		msg.WriteString(fmt.Sprintf("Available connections in .forge.yaml: %v\n\n", forgeConnections))
	}

	msg.WriteString("Tip: Either use one of the available names or add a new database configuration.")

	return dbConfig, errors.New(msg.String())
}

// loadFromForgeYaml loads database configuration from .forge.yaml
func (p *DatabasePlugin) loadFromForgeYaml(dbName string) (database.DatabaseConfig, error) {
	if p.config == nil || len(p.config.Database.Connections) == 0 {
		return database.DatabaseConfig{}, errors.New("no database connections in .forge.yaml")
	}

	// Map database name to connection
	// Support "default" as alias for first connection or "dev" connection
	var connConfig config.ConnectionConfig
	var found bool

	if dbName == "default" {
		// Try "dev" first, then "default", then first available
		if conn, ok := p.config.Database.Connections["dev"]; ok {
			connConfig = conn
			found = true
		} else if conn, ok := p.config.Database.Connections["default"]; ok {
			connConfig = conn
			found = true
		} else {
			// Use first connection
			for _, conn := range p.config.Database.Connections {
				connConfig = conn
				found = true
				break
			}
		}
	} else {
		// Look for exact match
		if conn, ok := p.config.Database.Connections[dbName]; ok {
			connConfig = conn
			found = true
		}
	}

	if !found {
		return database.DatabaseConfig{}, fmt.Errorf("connection '%s' not found in .forge.yaml", dbName)
	}

	// Expand environment variables in URL
	dsn := os.ExpandEnv(connConfig.URL)

	// Determine database type from driver or DSN
	dbType := p.inferDatabaseType(p.config.Database.Driver, dsn)

	// Set defaults if not specified
	maxOpenConns := connConfig.MaxConnections
	if maxOpenConns == 0 {
		maxOpenConns = 25
	}

	maxIdleConns := connConfig.MaxIdle
	if maxIdleConns == 0 {
		maxIdleConns = 25
	}

	return database.DatabaseConfig{
		Name:         dbName,
		Type:         dbType,
		DSN:          dsn,
		MaxOpenConns: maxOpenConns,
		MaxIdleConns: maxIdleConns,
		MaxRetries:   3, // Default
	}, nil
}

// getForgeYamlConnectionNames returns list of connection names from .forge.yaml
func (p *DatabasePlugin) getForgeYamlConnectionNames() []string {
	if p.config == nil {
		return nil
	}

	names := make([]string, 0, len(p.config.Database.Connections))
	for name := range p.config.Database.Connections {
		names = append(names, name)
	}
	return names
}

// inferDatabaseType determines database type from driver or DSN
func (p *DatabasePlugin) inferDatabaseType(driver, dsn string) database.DatabaseType {
	// First try explicit driver from .forge.yaml
	switch driver {
	case "postgres", "postgresql":
		return database.TypePostgres
	case "mysql":
		return database.TypeMySQL
	case "sqlite", "sqlite3":
		return database.TypeSQLite
	case "mongodb", "mongo":
		return database.TypeMongoDB
	}

	// Fallback to inferring from DSN prefix
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return database.TypePostgres
	}
	if strings.HasPrefix(dsn, "mysql://") {
		return database.TypeMySQL
	}
	if strings.HasPrefix(dsn, "mongodb://") || strings.HasPrefix(dsn, "mongodb+srv://") {
		return database.TypeMongoDB
	}
	if strings.HasSuffix(dsn, ".db") || strings.HasSuffix(dsn, ".sqlite") || strings.HasSuffix(dsn, ".sqlite3") {
		return database.TypeSQLite
	}

	// Default to postgres for backwards compatibility
	return database.TypePostgres
}

// getDatabaseNames extracts database names from a list of database configs.
func getDatabaseNames(databases []database.DatabaseConfig) []string {
	names := make([]string, len(databases))
	for i, db := range databases {
		names[i] = db.Name
	}

	return names
}

// buildConfigNotFoundError creates a helpful error message when database config is not found.
func (p *DatabasePlugin) buildConfigNotFoundError(dbName string, triedSources []string) error {
	var msg strings.Builder

	msg.WriteString("Database configuration not found for '")
	msg.WriteString(dbName)
	msg.WriteString("'\n\n")

	msg.WriteString("Checked sources:\n")
	for _, source := range triedSources {
		msg.WriteString("  • ")
		msg.WriteString(source)
		msg.WriteString("\n")
	}

	msg.WriteString("\nTo fix this, add database configuration to either:\n\n")

	// Option 1: .forge.yaml
	msg.WriteString("1. .forge.yaml (recommended for CLI usage):\n\n")
	msg.WriteString("database:\n")
	msg.WriteString("  driver: postgres\n")
	msg.WriteString("  connections:\n")
	msg.WriteString("    ")
	msg.WriteString(dbName)
	msg.WriteString(":\n")
	msg.WriteString("      url: postgres://user:pass@localhost:5432/dbname\n")
	msg.WriteString("      max_connections: 50\n")
	msg.WriteString("      max_idle: 10\n\n")

	// Option 2: config.yaml
	msg.WriteString("2. config.yaml or config.local.yaml:\n\n")
	msg.WriteString("extensions:\n")
	msg.WriteString("  database:\n")
	msg.WriteString("    databases:\n")
	msg.WriteString("      - name: ")
	msg.WriteString(dbName)
	msg.WriteString("\n")
	msg.WriteString("        type: postgres\n")
	msg.WriteString("        dsn: postgres://user:pass@localhost:5432/dbname\n")

	// Show available connections if they exist
	if p.config != nil && len(p.config.Database.Connections) > 0 {
		msg.WriteString("\n\nNote: Found connections in .forge.yaml: ")
		names := p.getForgeYamlConnectionNames()
		msg.WriteString(strings.Join(names, ", "))
		msg.WriteString("\nUse one of these names or add a new connection.")
	}

	return errors.New(msg.String())
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

// cliLoggerAdapter adapts CLI context to database.MigrationLogger interface.
type cliLoggerAdapter struct {
	ctx cli.CommandContext
}

func (l *cliLoggerAdapter) Debug(msg string, fields ...any) {
	// Only show debug messages in verbose mode
	if l.ctx.Bool("verbose") {
		l.ctx.Info("🔍 " + msg)
	}
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
