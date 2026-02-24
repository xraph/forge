package cli

import (
	"context"
	"fmt"

	"github.com/xraph/forge"
)

// collectMigratableExtensions finds all registered extensions that implement
// forge.MigratableExtension.
func collectMigratableExtensions(app forge.App) []forge.MigratableExtension {
	var migratables []forge.MigratableExtension
	for _, ext := range app.Extensions() {
		if m, ok := ext.(forge.MigratableExtension); ok {
			migratables = append(migratables, m)
		}
	}
	return migratables
}

// buildServeCommand creates the serve/start/run command that starts the forge app.
func buildServeCommand(app forge.App, autoMigrate bool) Command {
	return NewCommand("serve", "Start the application server", func(ctx CommandContext) error {
		// If auto-migrate is enabled, register a lifecycle hook to run
		// migrations during PhaseBeforeRun (after extensions are started
		// but before the HTTP server begins accepting requests).
		if autoMigrate {
			if err := app.RegisterHook(forge.PhaseBeforeRun, func(hookCtx context.Context, a forge.App) error {
				migratables := collectMigratableExtensions(a)
				if len(migratables) == 0 {
					return nil
				}

				a.Logger().Info("running auto-migrations before serve")
				for _, m := range migratables {
					ext := m.(forge.Extension) //nolint:errcheck // MigratableExtension always embeds Extension
					result, err := m.Migrate(hookCtx)
					if err != nil {
						return fmt.Errorf("auto-migrate failed for %s: %w", ext.Name(), err)
					}
					if result.Applied > 0 {
						a.Logger().Info("migrations applied",
							forge.F("extension", ext.Name()),
							forge.F("applied", result.Applied),
						)
					}
				}
				return nil
			}, forge.LifecycleHookOptions{
				Name:     "cli-auto-migrate",
				Priority: 1000, // Run before other BeforeRun hooks
			}); err != nil {
				app.Logger().Warn("failed to register auto-migrate hook", forge.F("error", err))
			}
		}

		return app.Run()
	}, WithAliases("start", "run"))
}

// buildMigrateCommand creates the migrate parent command with up, down, and status subcommands.
func buildMigrateCommand(app forge.App) Command {
	// Parent migrate command — shows usage when invoked without subcommand.
	migrateCmd := NewCommand("migrate", "Database migration commands", func(ctx CommandContext) error {
		ctx.Println("Usage: " + app.Name() + " migrate <command>")
		ctx.Println("")
		ctx.Println("Commands:")
		ctx.Println("  up       Run all pending migrations")
		ctx.Println("  down     Rollback the last migration batch")
		ctx.Println("  status   Show migration status")
		return nil
	})

	// migrate up
	upCmd := buildMigrateUpCommand(app)
	if err := migrateCmd.AddSubcommand(upCmd); err != nil {
		app.Logger().Warn("failed to add migrate up subcommand", forge.F("error", err))
	}

	// migrate down
	downCmd := buildMigrateDownCommand(app)
	if err := migrateCmd.AddSubcommand(downCmd); err != nil {
		app.Logger().Warn("failed to add migrate down subcommand", forge.F("error", err))
	}

	// migrate status
	statusCmd := buildMigrateStatusCommand(app)
	if err := migrateCmd.AddSubcommand(statusCmd); err != nil {
		app.Logger().Warn("failed to add migrate status subcommand", forge.F("error", err))
	}

	return migrateCmd
}

// buildMigrateUpCommand creates the "migrate up" command.
func buildMigrateUpCommand(app forge.App) Command {
	return NewCommand("up", "Run all pending migrations", func(ctx CommandContext) error {
		// Bootstrap: Start the app to initialize extensions without HTTP server.
		if err := app.Start(ctx.Context()); err != nil {
			return fmt.Errorf("failed to start app for migrations: %w", err)
		}
		defer app.Stop(ctx.Context()) //nolint:errcheck // best-effort cleanup

		migratables := collectMigratableExtensions(app)
		if len(migratables) == 0 {
			ctx.Info("No extensions with migrations found")
			return nil
		}

		spinner := ctx.Spinner("Running migrations...")

		totalApplied := 0
		for _, m := range migratables {
			ext := m.(forge.Extension) //nolint:errcheck // MigratableExtension embeds Extension
			result, err := m.Migrate(ctx.Context())
			if err != nil {
				spinner.Stop("Migration failed")
				return fmt.Errorf("migration failed for %s: %w", ext.Name(), err)
			}
			totalApplied += result.Applied
			if result.Applied > 0 {
				for _, name := range result.Names {
					ctx.Success(fmt.Sprintf("  Applied: %s", name))
				}
			}
		}

		spinner.Stop("Done")
		if totalApplied > 0 {
			ctx.Success(fmt.Sprintf("Applied %d migration(s)", totalApplied))
		} else {
			ctx.Info("No pending migrations")
		}
		return nil
	})
}

// buildMigrateDownCommand creates the "migrate down" command.
func buildMigrateDownCommand(app forge.App) Command {
	return NewCommand("down", "Rollback the last migration batch", func(ctx CommandContext) error {
		// Bootstrap app.
		if err := app.Start(ctx.Context()); err != nil {
			return fmt.Errorf("failed to start app for rollback: %w", err)
		}
		defer app.Stop(ctx.Context()) //nolint:errcheck // best-effort cleanup

		migratables := collectMigratableExtensions(app)
		if len(migratables) == 0 {
			ctx.Info("No extensions with migrations found")
			return nil
		}

		// Confirmation prompt unless --force is set.
		force := ctx.Bool("force")
		if !force {
			ok, err := ctx.Confirm("Are you sure you want to rollback?")
			if err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}
			if !ok {
				ctx.Info("Rollback cancelled")
				return nil
			}
		}

		totalRolledBack := 0
		for _, m := range migratables {
			ext := m.(forge.Extension) //nolint:errcheck // MigratableExtension embeds Extension
			result, err := m.Rollback(ctx.Context())
			if err != nil {
				return fmt.Errorf("rollback failed for %s: %w", ext.Name(), err)
			}
			totalRolledBack += result.RolledBack
			if result.RolledBack > 0 {
				ctx.Success(fmt.Sprintf("Rolled back %d migration(s) for %s", result.RolledBack, ext.Name()))
				for _, name := range result.Names {
					ctx.Println(fmt.Sprintf("  %s %s", Yellow("↩"), name))
				}
			}
		}

		if totalRolledBack == 0 {
			ctx.Info("Nothing to rollback")
		}
		return nil
	},
		WithFlag(NewBoolFlag("force", "f", "Skip confirmation prompt", false)),
	)
}

// buildMigrateStatusCommand creates the "migrate status" command.
func buildMigrateStatusCommand(app forge.App) Command {
	return NewCommand("status", "Show migration status", func(ctx CommandContext) error {
		// Bootstrap app.
		if err := app.Start(ctx.Context()); err != nil {
			return fmt.Errorf("failed to start app: %w", err)
		}
		defer app.Stop(ctx.Context()) //nolint:errcheck // best-effort cleanup

		migratables := collectMigratableExtensions(app)
		if len(migratables) == 0 {
			ctx.Info("No extensions with migrations found")
			return nil
		}

		for _, m := range migratables {
			ext := m.(forge.Extension) //nolint:errcheck // MigratableExtension embeds Extension
			groups, err := m.MigrationStatus(ctx.Context())
			if err != nil {
				ctx.Error(fmt.Errorf("failed to get status for %s: %w", ext.Name(), err))
				continue
			}

			if len(groups) == 0 {
				ctx.Info(fmt.Sprintf("No migrations registered for %s", ext.Name()))
				continue
			}

			ctx.Println("")
			ctx.Println(fmt.Sprintf("%s Migrations (%s %s):", Bold(ext.Name()), ext.Name(), ext.Version()))

			for _, g := range groups {
				ctx.Println(fmt.Sprintf("  Group: %s", Bold(g.Name)))

				table := ctx.Table()
				table.SetHeader([]string{"Version", "Name", "Status", "Applied At"})

				for _, mig := range g.Applied {
					table.AppendRow([]string{
						mig.Version,
						mig.Name,
						Green("applied"),
						mig.AppliedAt,
					})
				}
				for _, mig := range g.Pending {
					table.AppendRow([]string{
						mig.Version,
						mig.Name,
						Yellow("pending"),
						"",
					})
				}

				table.Render()
			}
		}
		return nil
	})
}

// registerExtensionCommands discovers and registers commands from extensions
// that implement forge.CLICommandProvider.
func registerExtensionCommands(c CLI, app forge.App) {
	for _, ext := range app.Extensions() {
		provider, ok := ext.(forge.CLICommandProvider)
		if !ok {
			continue
		}

		for _, cmdAny := range provider.CLICommands() {
			cmd, ok := cmdAny.(Command)
			if !ok {
				app.Logger().Warn("extension CLI command does not implement cli.Command",
					forge.F("extension", ext.Name()),
					forge.F("type", fmt.Sprintf("%T", cmdAny)),
				)
				continue
			}

			if err := c.AddCommand(cmd); err != nil {
				app.Logger().Warn("failed to register extension CLI command",
					forge.F("extension", ext.Name()),
					forge.F("command", cmd.Name()),
					forge.F("error", err),
				)
			}
		}
	}
}
