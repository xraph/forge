package main

import (
	"fmt"
	"os"

	"github.com/xraph/forge/v2/cli"
)

// DatabasePlugin provides database-related commands
type DatabasePlugin struct {
	*cli.BasePlugin
}

func NewDatabasePlugin() cli.Plugin {
	plugin := &DatabasePlugin{
		BasePlugin: cli.NewBasePlugin(
			"database",
			"1.0.0",
			"Database management commands",
		),
	}

	// Add migrate command
	migrateCmd := cli.NewCommand(
		"db:migrate",
		"Run database migrations",
		func(ctx cli.CommandContext) error {
			ctx.Info("Running migrations...")

			spinner := ctx.Spinner("Migrating database...")
			// Simulate migration
			spinner.Stop(cli.Green("✓ Migrations completed!"))

			return nil
		},
	)

	// Add seed command
	seedCmd := cli.NewCommand(
		"db:seed",
		"Seed database with data",
		func(ctx cli.CommandContext) error {
			ctx.Info("Seeding database...")

			progress := ctx.ProgressBar(100)
			progress.Set(100)
			progress.Finish("Database seeded!")

			return nil
		},
	)

	// Add reset command
	resetCmd := cli.NewCommand(
		"db:reset",
		"Reset database",
		func(ctx cli.CommandContext) error {
			confirmed, err := ctx.Confirm("Are you sure you want to reset the database?")
			if err != nil {
				return err
			}

			if !confirmed {
				ctx.Info("Cancelled")
				return nil
			}

			ctx.Success("Database reset complete")
			return nil
		},
	)

	plugin.AddCommand(migrateCmd)
	plugin.AddCommand(seedCmd)
	plugin.AddCommand(resetCmd)

	return plugin
}

// CachePlugin provides cache-related commands
type CachePlugin struct {
	*cli.BasePlugin
}

func NewCachePlugin() cli.Plugin {
	plugin := &CachePlugin{
		BasePlugin: cli.NewBasePlugin(
			"cache",
			"1.0.0",
			"Cache management commands",
		),
	}

	// Add clear command
	clearCmd := cli.NewCommand(
		"cache:clear",
		"Clear cache",
		func(ctx cli.CommandContext) error {
			ctx.Info("Clearing cache...")
			ctx.Success("Cache cleared!")
			return nil
		},
	)

	// Add stats command
	statsCmd := cli.NewCommand(
		"cache:stats",
		"Show cache statistics",
		func(ctx cli.CommandContext) error {
			table := ctx.Table()
			table.SetHeader([]string{"Metric", "Value"})
			table.AppendRow([]string{"Total Keys", "1,234"})
			table.AppendRow([]string{"Hit Rate", "95.2%"})
			table.AppendRow([]string{"Memory Used", "256 MB"})
			table.Render()

			return nil
		},
	)

	plugin.AddCommand(clearCmd)
	plugin.AddCommand(statsCmd)

	return plugin
}

func main() {
	// Create CLI
	app := cli.New(cli.Config{
		Name:        "myapp",
		Version:     "1.0.0",
		Description: "Application with plugins",
	})

	// Register plugins
	if err := app.RegisterPlugin(NewDatabasePlugin()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register database plugin: %v\n", err)
		os.Exit(1)
	}

	if err := app.RegisterPlugin(NewCachePlugin()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register cache plugin: %v\n", err)
		os.Exit(1)
	}

	// Run the CLI
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(cli.GetExitCode(err))
	}
}
