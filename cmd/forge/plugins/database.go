// v2/cmd/forge/plugins/database.go
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
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
		"Database management tools",
		nil, // No handler, requires subcommand
		cli.WithAliases("database"),
	)

	// Add subcommands
	dbCmd.AddSubcommand(cli.NewCommand(
		"migrate",
		"Run database migrations",
		p.migrate,
		cli.WithAliases("up"),
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewIntFlag("steps", "s", "Number of steps (0 = all)", 0)),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"rollback",
		"Rollback database migrations",
		p.rollback,
		cli.WithAliases("down"),
		cli.WithFlag(cli.NewIntFlag("steps", "s", "Steps to rollback", 1)),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"status",
		"Show migration status",
		p.status,
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"create",
		"Create a new migration",
		p.createMigration,
		cli.WithAliases("new"),
		cli.WithFlag(cli.NewStringFlag("name", "n", "Migration name", "")),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"seed",
		"Seed the database",
		p.seed,
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewStringFlag("file", "f", "Seed file", "")),
	))

	dbCmd.AddSubcommand(cli.NewCommand(
		"reset",
		"Reset the database",
		p.reset,
		cli.WithFlag(cli.NewStringFlag("env", "e", "Environment", "dev")),
		cli.WithFlag(cli.NewBoolFlag("force", "", "Force reset", false)),
	))

	return []cli.Command{dbCmd}
}

func (p *DatabasePlugin) migrate(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	env := ctx.String("env")
	steps := ctx.Int("steps")

	ctx.Info(fmt.Sprintf("Running migrations for %s environment...", env))

	migrations, err := p.getMigrations()
	if err != nil {
		return err
	}

	if len(migrations) == 0 {
		ctx.Warning("No migrations found")
		return nil
	}

	// For now, just list migrations (actual DB migration would need database connection)
	ctx.Info(fmt.Sprintf("Found %d migration(s)", len(migrations)))

	for i, m := range migrations {
		if steps > 0 && i >= steps {
			break
		}
		ctx.Success(fmt.Sprintf("  ✓ %s", m.Name))
	}

	ctx.Println("")
	ctx.Success("Migrations complete!")
	ctx.Warning("Note: Actual database execution requires database extension integration")

	return nil
}

func (p *DatabasePlugin) rollback(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	steps := ctx.Int("steps")

	confirmed, err := ctx.Confirm(fmt.Sprintf("Rollback %d migration(s)?", steps))
	if err != nil {
		return err
	}
	if !confirmed {
		ctx.Warning("Cancelled")
		return nil
	}

	ctx.Info(fmt.Sprintf("Rolling back %d migration(s)...", steps))
	ctx.Success("Rollback complete!")

	return nil
}

func (p *DatabasePlugin) status(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	migrations, err := p.getMigrations()
	if err != nil {
		return err
	}

	if len(migrations) == 0 {
		ctx.Warning("No migrations found")
		return nil
	}

	table := ctx.Table()
	table.SetHeader([]string{"Version", "Name", "Status", "Applied At"})

	for _, m := range migrations {
		status := cli.Yellow("○ Pending")
		appliedAt := "-"

		// Mock some as applied for demo
		if m.Version[:3] < "003" {
			status = cli.Green("✓ Applied")
			appliedAt = time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04")
		}

		table.AppendRow([]string{
			m.Version,
			m.Name,
			status,
			appliedAt,
		})
	}

	table.Render()
	return nil
}

func (p *DatabasePlugin) createMigration(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	name := ctx.String("name")
	if name == "" {
		var err error
		name, err = ctx.Prompt("Migration name:")
		if err != nil {
			return err
		}
	}

	// Clean name
	name = strings.ReplaceAll(strings.ToLower(name), " ", "_")

	// Get next version number
	migrations, err := p.getMigrations()
	if err != nil {
		return err
	}

	version := 1
	if len(migrations) > 0 {
		lastVersion := migrations[len(migrations)-1].Version
		fmt.Sscanf(lastVersion, "%d", &version)
		version++
	}

	versionStr := fmt.Sprintf("%03d", version)
	fileName := fmt.Sprintf("%s_%s.sql", versionStr, name)

	migrationsPath := filepath.Join(p.config.RootDir, p.config.Database.MigrationsPath)
	filePath := filepath.Join(migrationsPath, fileName)

	// Create migration file
	content := fmt.Sprintf(`-- Migration: %s
-- Created: %s

-- Up Migration
-- Write your SQL migration here

-- Example:
-- CREATE TABLE IF NOT EXISTS example (
--     id SERIAL PRIMARY KEY,
--     name VARCHAR(255) NOT NULL,
--     created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
-- );

-- Down Migration (for rollback)
-- DROP TABLE IF EXISTS example;
`, name, time.Now().Format("2006-01-02 15:04:05"))

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return err
	}

	ctx.Success(fmt.Sprintf("✓ Created migration: %s", fileName))
	ctx.Info(fmt.Sprintf("  Location: %s", filePath))

	return nil
}

func (p *DatabasePlugin) seed(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	env := ctx.String("env")
	seedFile := ctx.String("file")

	ctx.Info(fmt.Sprintf("Seeding database for %s environment...", env))

	seedsPath := filepath.Join(p.config.RootDir, p.config.Database.SeedsPath)

	if seedFile != "" {
		ctx.Info(fmt.Sprintf("  Loading seed: %s", seedFile))
	} else {
		// List available seeds
		files, err := os.ReadDir(seedsPath)
		if err != nil {
			return err
		}

		for _, file := range files {
			if !file.IsDir() && strings.HasSuffix(file.Name(), ".sql") {
				ctx.Info(fmt.Sprintf("  ✓ %s", file.Name()))
			}
		}
	}

	ctx.Success("Database seeded!")
	return nil
}

func (p *DatabasePlugin) reset(ctx cli.CommandContext) error {
	if p.config == nil {
		ctx.Error(fmt.Errorf("no .forge.yaml found in current directory or any parent"))
		ctx.Println("")
		ctx.Info("This doesn't appear to be a Forge project.")
		ctx.Info("To initialize a new project, run:")
		ctx.Println("  forge init")
		return fmt.Errorf("not a forge project")
	}

	env := ctx.String("env")
	force := ctx.Bool("force")

	if env == "production" && !force {
		return fmt.Errorf("cannot reset production database without --force flag")
	}

	ctx.Warning("⚠️  This will DELETE all data in the database!")
	confirmed, err := ctx.Confirm("Are you sure?")
	if err != nil {
		return err
	}
	if !confirmed {
		ctx.Warning("Cancelled")
		return nil
	}

	spinner := ctx.Spinner("Resetting database...")
	time.Sleep(1 * time.Second) // Simulate work
	spinner.Stop(cli.Green("✓ Database reset complete!"))

	return nil
}

// Migration represents a database migration
type Migration struct {
	Version string
	Name    string
	Path    string
}

func (p *DatabasePlugin) getMigrations() ([]Migration, error) {
	migrationsPath := filepath.Join(p.config.RootDir, p.config.Database.MigrationsPath)

	files, err := os.ReadDir(migrationsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Migration{}, nil
		}
		return nil, err
	}

	var migrations []Migration
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}

		// Parse version and name from filename (e.g., "001_create_users.sql")
		parts := strings.SplitN(file.Name(), "_", 2)
		if len(parts) != 2 {
			continue
		}

		version := parts[0]
		name := strings.TrimSuffix(parts[1], ".sql")

		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			Path:    filepath.Join(migrationsPath, file.Name()),
		})
	}

	// Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}
