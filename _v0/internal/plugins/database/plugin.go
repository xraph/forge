package database

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/v0/internal/services"
	"github.com/xraph/forge/v0/pkg/cli"
)

// Plugin implements database management functionality
type Plugin struct {
	name        string
	description string
	version     string
	app         cli.CLIApp
}

// NewDatabasePlugin creates a new database plugin
func NewDatabasePlugin() cli.CLIPlugin {
	return &Plugin{
		name:        "database",
		description: "Database management commands",
		version:     "1.0.0",
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Description() string {
	return p.description
}

func (p *Plugin) Version() string {
	return p.version
}

func (p *Plugin) Commands() []*cli.Command {
	return []*cli.Command{
		p.migrateCommand(),
	}
}

func (p *Plugin) Middleware() []cli.CLIMiddleware {
	return []cli.CLIMiddleware{}
}

func (p *Plugin) Initialize(app cli.CLIApp) error {
	p.app = app
	return nil
}

func (p *Plugin) Cleanup() error {
	return nil
}

// Main migrate command
func (p *Plugin) migrateCommand() *cli.Command {
	cmd := cli.NewCommand("migrate", "Database migration commands")
	cmd.WithLong(`Manage database migrations and schema changes.

The migrate command provides tools to create, apply, and manage
database migrations across different database systems.`)

	// Add subcommands
	cmd.WithSubcommands(
		p.migrateUpCommand(),
		p.migrateDownCommand(),
		p.statusCommand(),
		p.seedCommand(),
		p.createMigrationCommand(),
	)

	return cmd
}

// Migration up command
func (p *Plugin) migrateUpCommand() *cli.Command {
	cmd := cli.NewCommand("up", "Apply pending migrations")

	countFlag := cli.IntFlag("count", "c", "Number of migrations to apply (0 = all)", false).WithDefault(0)
	dryRunFlag := cli.BoolFlag("dry-run", "d", "Show what would be applied without applying")
	forceFlag := cli.BoolFlag("force", "f", "Force apply migrations")
	yesFlag := cli.BoolFlag("yes", "y", "Skip confirmation prompt")

	cmd.WithFlags(countFlag, dryRunFlag, forceFlag, yesFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var migrationService services.MigrationService
		ctx.MustResolve(&migrationService)

		config := services.MigrationConfig{
			Direction: "up",
			Count:     ctx.GetInt("count"),
			DryRun:    ctx.GetBool("dry-run"),
			Force:     ctx.GetBool("force"),
		}

		// Get pending migrations
		pending, err := migrationService.GetPendingMigrations(context.Background())
		if err != nil {
			return err
		}

		if len(pending) == 0 {
			ctx.Info("No pending migrations")
			return nil
		}

		ctx.Info(fmt.Sprintf("Found %d pending migrations", len(pending)))

		// Show migrations to apply
		ctx.Info("Migrations to apply:")
		for i, migration := range pending {
			if config.Count > 0 && i >= config.Count {
				break
			}
			ctx.Info(fmt.Sprintf("  %d. %s", i+1, migration.Name))
		}

		// Confirm before applying
		if !ctx.GetBool("yes") && !ctx.Confirm("Apply migrations?") {
			ctx.Info("Migration cancelled")
			return nil
		}

		if config.DryRun {
			ctx.Info("Dry run completed - no migrations were actually applied")
			return nil
		}

		// Apply migrations with progress
		progress := ctx.Progress(len(pending))
		config.OnProgress = func(migration string, current, total int) {
			progress.Update(current, fmt.Sprintf("Applying %s", migration))
		}

		result, err := migrationService.ApplyMigrations(context.Background(), config)
		if err != nil {
			ctx.Error("Migration failed")
			return err
		}

		ctx.Success(fmt.Sprintf("Applied %d migrations successfully", result.Applied))
		return nil
	}

	return cmd
}

// Migration down command
func (p *Plugin) migrateDownCommand() *cli.Command {
	cmd := cli.NewCommand("down", "Rollback migrations")

	countFlag := cli.IntFlag("count", "c", "Number of migrations to rollback", false).WithDefault(1)
	dryRunFlag := cli.BoolFlag("dry-run", "d", "Show what would be rolled back without applying")
	forceFlag := cli.BoolFlag("force", "f", "Force rollback migrations")
	yesFlag := cli.BoolFlag("yes", "y", "Skip confirmation prompt")

	cmd.WithFlags(countFlag, dryRunFlag, forceFlag, yesFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var migrationService services.MigrationService
		ctx.MustResolve(&migrationService)

		config := services.MigrationConfig{
			Direction: "down",
			Count:     ctx.GetInt("count"),
			DryRun:    ctx.GetBool("dry-run"),
			Force:     ctx.GetBool("force"),
		}

		// Get applied migrations
		applied, err := migrationService.GetAppliedMigrations(context.Background())
		if err != nil {
			return err
		}

		if len(applied) == 0 {
			ctx.Info("No applied migrations to rollback")
			return nil
		}

		count := config.Count
		if count > len(applied) {
			count = len(applied)
		}

		ctx.Warning(fmt.Sprintf("Will rollback %d migrations", count))

		// Show migrations to rollback
		ctx.Info("Migrations to rollback:")
		for i := 0; i < count; i++ {
			ctx.Info(fmt.Sprintf("  %d. %s", i+1, applied[len(applied)-1-i].Name))
		}

		// Confirm before rolling back
		if !ctx.GetBool("yes") && !ctx.Confirm("Rollback migrations?") {
			ctx.Info("Rollback cancelled")
			return nil
		}

		if config.DryRun {
			ctx.Info("Dry run completed - no migrations were actually rolled back")
			return nil
		}

		// Rollback migrations with progress
		progress := ctx.Progress(count)
		config.OnProgress = func(migration string, current, total int) {
			progress.Update(current, fmt.Sprintf("Rolling back %s", migration))
		}

		result, err := migrationService.ApplyMigrations(context.Background(), config)
		if err != nil {
			ctx.Error("Rollback failed")
			return err
		}

		ctx.Success(fmt.Sprintf("Rolled back %d migrations successfully", result.RolledBack))
		return nil
	}

	return cmd
}

// Migration status command
func (p *Plugin) statusCommand() *cli.Command {
	cmd := cli.NewCommand("status", "Show migration status")

	verboseFlag := cli.BoolFlag("verbose", "v", "Show detailed migration information")

	cmd.WithFlags(verboseFlag).WithOutputFormats("table", "json", "yaml")

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var migrationService services.MigrationService
		ctx.MustResolve(&migrationService)

		status, err := migrationService.GetStatus(context.Background())
		if err != nil {
			return err
		}

		if ctx.GetBool("verbose") {
			return ctx.OutputData(status)
		}

		// Simple table output
		headers := []string{"Migration", "Status", "Applied At"}
		var rows [][]string

		for _, migration := range status.Migrations {
			appliedAt := "N/A"
			if migration.AppliedAt != nil {
				appliedAt = migration.AppliedAt.Format("2006-01-02 15:04:05")
			}

			statusStr := "Pending"
			if migration.Applied {
				statusStr = "Applied"
			}

			rows = append(rows, []string{
				migration.Name,
				statusStr,
				appliedAt,
			})
		}

		ctx.Table(headers, rows)

		// Summary
		ctx.Info(fmt.Sprintf("Total migrations: %d", len(status.Migrations)))
		ctx.Info(fmt.Sprintf("Applied: %d", status.Applied))
		ctx.Info(fmt.Sprintf("Pending: %d", status.Pending))

		return nil
	}

	return cmd
}

// Database seed command
func (p *Plugin) seedCommand() *cli.Command {
	cmd := cli.NewCommand("seed", "Seed the database with test data")

	seederFlag := cli.StringFlag("seeder", "s", "Specific seeder to run", false)
	forceFlag := cli.BoolFlag("force", "f", "Force seed even in production")
	dryRunFlag := cli.BoolFlag("dry-run", "d", "Show what would be seeded without applying")

	cmd.WithFlags(seederFlag, forceFlag, dryRunFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var migrationService services.MigrationService
		ctx.MustResolve(&migrationService)

		config := services.SeedConfig{
			Seeder: ctx.GetString("seeder"),
			Force:  ctx.GetBool("force"),
			DryRun: ctx.GetBool("dry-run"),
		}

		// Get available seeders
		seeders, err := migrationService.GetSeeders(context.Background())
		if err != nil {
			return err
		}

		if len(seeders) == 0 {
			ctx.Info("No seeders found")
			return nil
		}

		// Filter seeders if specific one requested
		if config.Seeder != "" {
			found := false
			for _, seeder := range seeders {
				if seeder.Name == config.Seeder {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("seeder '%s' not found", config.Seeder)
			}
		}

		ctx.Info(fmt.Sprintf("Found %d seeders", len(seeders)))

		// Show seeders to run
		ctx.Info("Seeders to run:")
		for i, seeder := range seeders {
			if config.Seeder != "" && seeder.Name != config.Seeder {
				continue
			}
			ctx.Info(fmt.Sprintf("  %d. %s - %s", i+1, seeder.Name, seeder.Description))
		}

		if !ctx.Confirm("Run seeders?") {
			ctx.Info("Seeding cancelled")
			return nil
		}

		if config.DryRun {
			ctx.Info("Dry run completed - no data was actually seeded")
			return nil
		}

		// Run seeders with progress
		progress := ctx.Progress(len(seeders))
		config.OnProgress = func(seeder string, current, total int) {
			progress.Update(current, fmt.Sprintf("Running %s", seeder))
		}

		result, err := migrationService.RunSeeders(context.Background(), config)
		if err != nil {
			ctx.Error("Seeding failed")
			return err
		}

		ctx.Success(fmt.Sprintf("Seeded %d tables successfully", result.SeededTables))
		return nil
	}

	return cmd
}

// Create migration command
func (p *Plugin) createMigrationCommand() *cli.Command {
	cmd := cli.NewCommand("create [name]", "Create a new migration")
	cmd.WithArgs(cobra.ExactArgs(1))

	typeFlag := cli.StringFlag("type", "t", "Migration type (sql, go)", false).WithDefault("sql")
	templateFlag := cli.StringFlag("template", "", "Migration template to use", false)
	packageFlag := cli.StringFlag("package", "p", "Package for Go migrations", false).WithDefault("migrations")
	dirFlag := cli.StringFlag("dir", "d", "Directory to create migration in", false).WithDefault("migrations")

	cmd.WithFlags(typeFlag, templateFlag, packageFlag, dirFlag)

	cmd.Run = func(ctx cli.CLIContext, args []string) error {
		var migrationService services.MigrationService
		ctx.MustResolve(&migrationService)

		config := services.CreateMigrationConfig{
			Name:      args[0],
			Type:      ctx.GetString("type"),
			Template:  ctx.GetString("template"),
			Package:   ctx.GetString("package"),
			Directory: ctx.GetString("dir"),
		}

		spinner := ctx.Spinner(fmt.Sprintf("Creating migration %s...", config.Name))
		defer spinner.Stop()

		result, err := migrationService.CreateMigration(context.Background(), config)
		if err != nil {
			ctx.Error("Failed to create migration")
			return err
		}

		ctx.Success(fmt.Sprintf("Created migration %s", result.Filename))
		ctx.Info(fmt.Sprintf("📄 File: %s", result.Path))

		// Show migration template
		if result.Template != "" {
			ctx.Info(fmt.Sprintf("Template: %s", result.Template))
		}

		// Show next steps
		ctx.Info("Next steps:")
		ctx.Info("  1. Edit the migration file to add your schema changes")
		if config.Type == "sql" {
			ctx.Info("  2. Add SQL statements in the UP and DOWN sections")
		} else {
			ctx.Info("  2. Implement the Up() and Down() methods")
		}
		ctx.Info("  3. Run 'forge migrate up' to apply the migration")

		return nil
	}

	return cmd
}

// cmd/forge/plugins/database/migrate.go
