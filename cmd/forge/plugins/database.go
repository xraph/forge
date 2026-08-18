package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/uptrace/bun/migrate"

	"github.com/xraph/forge"
	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
	"github.com/xraph/forge/errors"
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
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-scoped migration", "")),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"create-go",
		"Create Go migration file",
		p.createGoMigration,
		cli.WithFlag(cli.NewStringFlag("app", "a", "App name for app-scoped migration", "")),
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

	appName := ctx.String("app")

	// Ensure migrations directory and migrations.go exist
	migrationPath, err := p.getMigrationPathForApp(appName)
	if err != nil {
		return err
	}

	// Create migrations.go if it doesn't exist
	migrationsGoPath := filepath.Join(migrationPath, "migrations.go")
	if _, err := os.Stat(migrationsGoPath); os.IsNotExist(err) {
		if err := p.createMigrationsGoFile(migrationsGoPath, appName); err != nil {
			return fmt.Errorf("failed to create migrations.go: %w", err)
		}

		ctx.Println("")
		ctx.Success("✓ Created: " + migrationsGoPath)
	} else {
		ctx.Println("")
		ctx.Info("✓ Migration structure already exists: " + migrationPath)
	}

	// The scaffold above is filesystem-only and always succeeds without a
	// database. Table creation needs one, so it happens second, and its
	// failure must not read as though init did nothing: a developer running
	// this before starting Postgres should still walk away with the
	// migrations package on disk.
	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), ctx.String("database"), appName)
	if err != nil {
		ctx.Println("")
		ctx.Info("⚠️  Migration package scaffolded, but the migration tables could not be created: no DSN configured")

		return err
	}

	if err := p.runWithGoMigrations(ctx, "init", dsn); err != nil {
		ctx.Println("")
		ctx.Error(fmt.Errorf("⚠️  migration package scaffolded, but the migration tables were not created (dsn: %s): %w", dsn, err))

		return err
	}

	ctx.Println("")
	if appName != "" {
		ctx.Info(fmt.Sprintf("📚 Next steps for app '%s':", appName))
		ctx.Info(fmt.Sprintf("   1. Create migrations with: forge db create-sql <name> --app %s", appName))
		ctx.Info(fmt.Sprintf("   2. Run migrations with: forge db migrate --app %s", appName))
	} else {
		ctx.Info("📚 Next steps:")
		ctx.Info("   1. Create migrations with: forge generate migration <name>")
		ctx.Info("   2. Run migrations with: forge db migrate")
	}
	ctx.Println("")

	return nil
}

func (p *DatabasePlugin) runMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	appName := ctx.String("app")

	// Fail fast on a bad DSN before paying for the runner's build step.
	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), ctx.String("database"), appName)
	if err != nil {
		return err
	}

	return p.runWithGoMigrations(ctx, "migrate", dsn)
}

func (p *DatabasePlugin) rollbackMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	dbName := ctx.String("database")
	appName := ctx.String("app")

	label := dbName
	if appName != "" {
		label = fmt.Sprintf("%s (app: %s)", dbName, appName)
	}

	// Confirm rollback
	confirm, err := ctx.Confirm(fmt.Sprintf("Rollback last migration group on %s?", label))
	if err != nil || !confirm {
		ctx.Info("Rollback cancelled")

		return nil
	}

	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), dbName, appName)
	if err != nil {
		return err
	}

	return p.runWithGoMigrations(ctx, "rollback", dsn)
}

func (p *DatabasePlugin) migrationStatus(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	appName := ctx.String("app")

	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), ctx.String("database"), appName)
	if err != nil {
		return err
	}

	// The runner formats and prints its own status report, so there is
	// nothing left for the handler to compute or display.
	return p.runWithGoMigrations(ctx, "status", dsn)
}

func (p *DatabasePlugin) resetDatabase(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	dbName := ctx.String("database")
	appName := ctx.String("app")
	force := ctx.Bool("force")

	label := dbName
	if appName != "" {
		label = fmt.Sprintf("%s (app: %s)", dbName, appName)
	}

	// Confirm reset
	if !force {
		ctx.Error(errors.New("⚠️  WARNING: This will rollback ALL migrations and re-run them"))
		ctx.Error(errors.New("⚠️  This is a DESTRUCTIVE operation"))
		ctx.Println("")

		confirm, err := ctx.Confirm(fmt.Sprintf("Reset database %s?", label))
		if err != nil || !confirm {
			ctx.Info("Reset cancelled")

			return nil
		}
	}

	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), dbName, appName)
	if err != nil {
		return err
	}

	// grove's orchestrator rolls back one migration per call; there is no
	// "roll everything back" primitive to ask for instead. Counting the
	// registered migration files on disk gives an upper bound on how many
	// rollbacks are needed, and calling rollback more times than that is a
	// harmless no-op on the runner side.
	count, err := p.countMigrationFilesForApp(appName)
	if err != nil {
		return fmt.Errorf("failed to count migrations: %w", err)
	}

	for range count {
		if err := p.runWithGoMigrations(ctx, "rollback", dsn); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
	}

	return p.runWithGoMigrations(ctx, "migrate", dsn)
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
	appName := ctx.String("app")

	// Get migrations directory path
	migrationPath, err := p.getMigrationPathForApp(appName)
	if err != nil {
		return err
	}

	label := name
	if appName != "" {
		label = fmt.Sprintf("%s (app: %s)", name, appName)
	}
	spinner := ctx.Spinner(fmt.Sprintf("Creating SQL migration '%s'...", label))

	up, down, goFile, err := writeSQLMigrationFiles(migrationPath, name, useTx)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	spinner.Stop(cli.Green("✓ Migration files created!"))
	ctx.Println("")
	ctx.Success("Created: " + up)
	ctx.Success("Created: " + down)
	ctx.Success("Created: " + goFile)

	return nil
}

// writeSQLMigrationFiles writes the up and down SQL pair plus the Go file that
// embeds and registers them. It returns all three paths.
//
// The Go file is what makes this a grove migration. Grove has no filesystem
// discovery, so a loose .sql pair would never run; the embed is the bridge
// between authoring SQL and grove's Go-value model.
func writeSQLMigrationFiles(dir, name string, tx bool) (upPath, downPath, goPath string, err error) {
	version := time.Now().Format("20060102150405")

	suffix := ""
	if tx {
		suffix = ".tx"
	}

	base := version + "_" + name
	upPath = filepath.Join(dir, base+suffix+".up.sql")
	downPath = filepath.Join(dir, base+suffix+".down.sql")
	goPath = filepath.Join(dir, base+".go")

	if err = os.WriteFile(upPath, []byte("-- Write your up migration here\n"), 0o644); err != nil {
		return "", "", "", fmt.Errorf("failed to write up migration: %w", err)
	}

	if err = os.WriteFile(downPath, []byte("-- Write your down migration here\n"), 0o644); err != nil {
		return "", "", "", fmt.Errorf("failed to write down migration: %w", err)
	}

	// Identifiers cannot start with a digit, so the version is suffixed rather
	// than prefixed onto the variable names.
	ident := "m" + version

	goSrc := fmt.Sprintf(`package migrations

import (
	"context"
	_ "embed"

	"github.com/xraph/grove/migrate"
)

//go:embed %s
var up%s string

//go:embed %s
var down%s string

func init() {
	App.MustRegister(&migrate.Migration{
		Name:    %q,
		Version: %q,
		Up: func(ctx context.Context, exec migrate.Executor) error {
			_, err := exec.Exec(ctx, up%s)
			return err
		},
		Down: func(ctx context.Context, exec migrate.Executor) error {
			_, err := exec.Exec(ctx, down%s)
			return err
		},
	})
}
`, filepath.Base(upPath), ident, filepath.Base(downPath), ident, name, version, ident, ident)

	if err = os.WriteFile(goPath, []byte(goSrc), 0o644); err != nil {
		return "", "", "", fmt.Errorf("failed to write migration registration: %w", err)
	}

	return upPath, downPath, goPath, nil
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
	appName := ctx.String("app")

	// Get migrations directory path
	migrationPath, err := p.getMigrationPathForApp(appName)
	if err != nil {
		return err
	}

	label := name
	if appName != "" {
		label = fmt.Sprintf("%s (app: %s)", name, appName)
	}
	spinner := ctx.Spinner(fmt.Sprintf("Creating Go migration '%s'...", label))

	path, err := writeGoMigrationFile(migrationPath, name)
	if err != nil {
		spinner.Stop(cli.Red("✗ Failed"))

		return err
	}

	spinner.Stop(cli.Green("✓ Migration file created!"))
	ctx.Println("")
	ctx.Success("Created: " + path)

	return nil
}

// writeGoMigrationFile writes a grove migration with empty up and down bodies
// for the author to fill in.
func writeGoMigrationFile(dir, name string) (string, error) {
	version := time.Now().Format("20060102150405")
	path := filepath.Join(dir, version+"_"+name+".go")

	src := fmt.Sprintf(`package migrations

import (
	"context"

	"github.com/xraph/grove/migrate"
)

func init() {
	App.MustRegister(&migrate.Migration{
		Name:    %q,
		Version: %q,
		Up: func(ctx context.Context, exec migrate.Executor) error {
			// Write your up migration here.
			return nil
		},
		Down: func(ctx context.Context, exec migrate.Executor) error {
			// Write your down migration here.
			return nil
		},
	})
}
`, name, version)

	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		return "", fmt.Errorf("failed to write migration: %w", err)
	}

	return path, nil
}

func (p *DatabasePlugin) lockMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	appName := ctx.String("app")

	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), ctx.String("database"), appName)
	if err != nil {
		return err
	}

	return p.runWithGoMigrations(ctx, "lock", dsn)
}

func (p *DatabasePlugin) unlockMigrations(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	appName := ctx.String("app")

	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), ctx.String("database"), appName)
	if err != nil {
		return err
	}

	return p.runWithGoMigrations(ctx, "unlock", dsn)
}

func (p *DatabasePlugin) markApplied(ctx cli.CommandContext) error {
	if p.config == nil {
		return errors.New("not a forge project")
	}

	appName := ctx.String("app")

	// Confirm action. The runner marks every pending migration in one go,
	// with no per-migration selection, so this warning covers the whole set.
	ctx.Println("")
	ctx.Info("⚠️  This will mark pending migrations as applied WITHOUT running them")
	ctx.Info("⚠️  Use this only if migrations were applied manually")
	ctx.Println("")

	confirm, err := ctx.Confirm("Mark pending migrations as applied?")
	if err != nil || !confirm {
		ctx.Info("Operation cancelled")

		return nil
	}

	dsn, err := p.resolveDSNFrom(ctx.String("dsn"), ctx.String("database"), appName)
	if err != nil {
		return err
	}

	return p.runWithGoMigrations(ctx, "mark-applied", dsn)
}

// Helper functions

func (p *DatabasePlugin) loadMigrations() (*migrate.Migrations, error) {
	return p.loadMigrationsForApp("")
}

// loadMigrationsForApp loads SQL migrations from the appropriate directory.
// If appName is provided, uses the app-scoped migration path.
// Note: Table namespacing is handled at the migrator level, not the migrations collection.
func (p *DatabasePlugin) loadMigrationsForApp(appName string) (*migrate.Migrations, error) {
	migrationPath, err := p.getMigrationPathForApp(appName)
	if err != nil {
		return nil, err
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
		hint := ""
		if appName != "" {
			hint = fmt.Sprintf(" (app: %s)", appName)
		}
		return nil, fmt.Errorf("no migration files found in %s%s\n\nTo create a migration, run:\n  forge db create-sql <migration_name>%s\n  forge db create-go <migration_name>%s",
			migrationPath, hint,
			appFlagHint(appName), appFlagHint(appName))
	}

	return migrations, nil
}

// countMigrationFilesForApp counts registered migration files (every .go file
// but migrations.go itself) in an app's migration directory. It only reads
// the filesystem, which is what lets resetDatabase bound its rollback loop
// without the CLI opening a database connection to ask grove directly.
func (p *DatabasePlugin) countMigrationFilesForApp(appName string) (int, error) {
	migrationPath, err := p.getMigrationPathForApp(appName)
	if err != nil {
		return 0, err
	}

	entries, err := os.ReadDir(migrationPath)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasSuffix(name, ".go") && name != "migrations.go" {
			count++
		}
	}

	return count, nil
}

// appFlagHint returns " --app <name>" if appName is non-empty, otherwise empty string.
func appFlagHint(appName string) string {
	if appName == "" {
		return ""
	}
	return " --app " + appName
}

func (p *DatabasePlugin) getMigrationPath() (string, error) {
	return p.getMigrationPathForApp("")
}

// getMigrationPathForApp resolves the migration directory.
// If appName is empty, returns the global migration path (existing behavior).
// If appName is provided, resolution order:
//  1. App's .forge.yaml database.migrations_path override (relative to app dir)
//  2. Centralized convention: <global_migrations_path>/<appName>/
func (p *DatabasePlugin) getMigrationPathForApp(appName string) (string, error) {
	if appName != "" {
		return p.getAppScopedMigrationPath(appName)
	}

	// Global migration path resolution (existing behavior)
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

// getAppScopedMigrationPath resolves the migration path for a specific app.
func (p *DatabasePlugin) getAppScopedMigrationPath(appName string) (string, error) {
	// Step 1: Try loading the app's .forge.yaml for a custom migrations_path override
	appDir, appDirErr := p.resolveAppDir(appName)
	if appDirErr == nil {
		appCfg, err := config.LoadAppConfig(appDir)
		if err == nil && appCfg.Database.GetMigrationsPath() != "" {
			overridePath := appCfg.Database.GetMigrationsPath()
			if !filepath.IsAbs(overridePath) {
				overridePath = filepath.Join(appDir, overridePath)
			}
			if err := os.MkdirAll(overridePath, 0755); err != nil {
				return "", fmt.Errorf("failed to create app migrations directory %s: %w", overridePath, err)
			}
			return overridePath, nil
		}
	}

	// Step 2: Centralized convention -- <global_migrations_path>/<appName>/
	globalPath, err := p.getMigrationPathForApp("") // get the global path
	if err != nil {
		return "", err
	}

	appMigrationPath := filepath.Join(globalPath, appName)
	if err := os.MkdirAll(appMigrationPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create app migrations directory %s: %w", appMigrationPath, err)
	}

	return appMigrationPath, nil
}

// createMigrationsGoFile scaffolds the migrations package for one app. The group
// name defaults to "app" when appName is empty (the single-app / global case);
// otherwise it is the app name, so migrations for different apps sharing one
// database stay isolated in grove's per-group tracking rather than colliding
// under the same group.
func (p *DatabasePlugin) createMigrationsGoFile(path, appName string) error {
	groupName := appName
	if groupName == "" {
		groupName = "app"
	}

	content := fmt.Sprintf(`package migrations

import "github.com/xraph/grove/migrate"

// Registry holds every migration group this application owns. The forge CLI
// builds a runner that reads it, so the name matters.
var Registry = migrate.NewMigrationRegistry()

// App is the group that generated migrations register into.
var App = migrate.NewGroup(%q)

func init() {
	Registry.Register(App)
}
`, groupName)

	return os.WriteFile(path, []byte(content), 0644)
}

// resolveDSNFrom resolves a database DSN without needing a cli.CommandContext,
// so it can be tested directly. Precedence: the --dsn flag, then .forge.yaml's
// database.connections.<name>.dsn, then the config.yaml family (config.yaml,
// config.yml, config.local.yaml, config.local.yml, searched in the project
// root and its config/ subdirectory), then the DATABASE_URL environment
// variable. It tolerates a nil p.config, simply skipping the .forge.yaml and
// config.yaml sources, since callers may want to probe resolution before
// confirming a forge project is even present.
//
// appName does not change which DSN is picked: per the grove migration, app
// isolation on a shared database happens at the migration group level
// (FORGE_MIGRATION_GROUP), not by routing an app to a different connection.
func (p *DatabasePlugin) resolveDSNFrom(flagDSN, dbName, appName string) (string, error) {
	if flagDSN != "" {
		return os.ExpandEnv(flagDSN), nil
	}

	p.loadEnvFiles()

	var triedSources []string

	if p.config != nil && len(p.config.Database.Connections) > 0 {
		triedSources = append(triedSources, ".forge.yaml (database.connections)")

		if dsn, err := p.loadFromForgeYaml(dbName); err == nil && dsn != "" {
			return dsn, nil
		}
	} else {
		triedSources = append(triedSources, ".forge.yaml (not found)")
	}

	if p.config != nil {
		dsn, sources := p.resolveConfigYamlDSN(dbName)
		triedSources = append(triedSources, sources...)

		if dsn != "" {
			return dsn, nil
		}
	}

	triedSources = append(triedSources, "DATABASE_URL environment variable")

	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn, nil
	}

	return "", p.buildConfigNotFoundError(dbName, triedSources)
}

// configYamlDatabase is the slice of extensions/database.DatabaseConfig that
// resolveConfigYamlDSN actually needs. The runner only wants a connection
// string, not a full extension config, so there is no reason to import the
// extensions/database package just to bind these two fields.
type configYamlDatabase struct {
	Name string `mapstructure:"name" yaml:"name"`
	DSN  string `mapstructure:"dsn"  yaml:"dsn"`
}

type configYamlDatabaseSection struct {
	Databases []configYamlDatabase `mapstructure:"databases" yaml:"databases"`
}

// resolveConfigYamlDSN searches the config.yaml family for a database named
// dbName. This is the config.yaml/config.local.yaml lookup that
// loadDatabaseConfig used to perform on its own; it now lives here as one
// source among several that resolveDSNFrom tries in order.
//
// It returns the discovered DSN, or an empty string if none of the files
// exist or none define a matching database name, and the list of sources it
// consulted so the caller can report them alongside every other source it
// tried. A malformed config.yaml is treated the same as "no match" rather
// than surfaced as a hard error: precedence keeps moving on to
// DATABASE_URL, which mirrors how a missing .forge.yaml connection is
// already handled above.
func (p *DatabasePlugin) resolveConfigYamlDSN(dbName string) (dsn string, tried []string) {
	searchDirs := []string{p.config.RootDir, filepath.Join(p.config.RootDir, "config")}
	configNames := []string{"config.yaml", "config.yml", "config.local.yaml", "config.local.yml"}

	var configFiles []string
	for _, dir := range searchDirs {
		for _, name := range configNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				configFiles = append(configFiles, path)
				tried = append(tried, path)
			}
		}
	}

	if len(configFiles) == 0 {
		return "", append(tried, "config.yaml family (not found)")
	}

	// A Forge app is the existing way to get confy's merging and environment
	// variable expansion across config.yaml + config.local.yaml, so it is
	// reused here rather than reimplementing that merge.
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

	// Try the namespaced key (preferred), then the legacy top-level key.
	var section configYamlDatabaseSection
	if err := cm.Bind("extensions.database", &section); err != nil || len(section.Databases) == 0 {
		section = configYamlDatabaseSection{}
		_ = cm.Bind("database", &section)
	}

	for _, db := range section.Databases {
		if db.Name == dbName {
			return os.ExpandEnv(db.DSN), tried
		}
	}

	return "", tried
}

// loadFromForgeYaml resolves a DSN from .forge.yaml's database.connections.
func (p *DatabasePlugin) loadFromForgeYaml(dbName string) (string, error) {
	if p.config == nil || len(p.config.Database.Connections) == 0 {
		return "", errors.New("no database connections in .forge.yaml")
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
		return "", fmt.Errorf("connection '%s' not found in .forge.yaml", dbName)
	}

	// Expand environment variables in URL
	return os.ExpandEnv(connConfig.URL), nil
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

// sanitizeAppName converts an app name to a valid SQL identifier fragment.
// Lowercases, replaces hyphens/dots/spaces with underscores, and strips invalid characters.
var sanitizeAppNameRegex = regexp.MustCompile(`[^a-z0-9_]`)

func sanitizeAppName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, " ", "_")
	s = sanitizeAppNameRegex.ReplaceAllString(s, "")
	// Collapse consecutive underscores
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(s, "_")
}

// migrationTableName returns the bun migration table name, optionally namespaced for an app.
func migrationTableName(appName string) string {
	if appName == "" {
		return "bun_migrations"
	}
	return "bun_migrations_" + sanitizeAppName(appName)
}

// migrationLocksTableName returns the bun migration locks table name, optionally namespaced for an app.
func migrationLocksTableName(appName string) string {
	if appName == "" {
		return "bun_migration_locks"
	}
	return "bun_migration_locks_" + sanitizeAppName(appName)
}

// resolveAppDir returns the filesystem directory for a named app.
// It checks both single-module (apps/{name}) and multi-module layouts.
func (p *DatabasePlugin) resolveAppDir(appName string) (string, error) {
	if p.config == nil {
		return "", errors.New("not a forge project")
	}

	// Determine the apps base directory
	structure := p.config.Project.GetStructure()
	appsBase := filepath.Join(p.config.RootDir, structure.Apps)

	appDir := filepath.Join(appsBase, appName)
	if info, err := os.Stat(appDir); err == nil && info.IsDir() {
		return appDir, nil
	}

	return "", fmt.Errorf("app directory not found: %s (looked in %s)", appName, appsBase)
}
