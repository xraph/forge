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
// The app is resolved from ctx.App() at execution time.
func buildServeCommand(autoMigrate bool) Command {
	return NewCommand("serve", "Start the application server", func(ctx CommandContext) error {
		app := ctx.App()
		if app == nil {
			return NewError("app not available", ExitError)
		}

		// If auto-migrate is enabled, register a lifecycle hook to run
		// migrations during PhaseBeforeRun (after extensions are started
		// but before the HTTP server begins accepting requests).
		if autoMigrate {
			if err := app.RegisterHook(forge.PhaseBeforeRun, func(hookCtx context.Context, a forge.App) error {
				// Check if migrations are disabled via config or .forge.yaml
				if a.MigrationsDisabled() {
					a.Logger().Info("auto-migrations disabled via configuration")
					return nil
				}

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
func buildMigrateCommand() Command {
	// Parent migrate command — shows usage when invoked without subcommand.
	migrateCmd := NewCommand("migrate", "Database migration commands", func(ctx CommandContext) error {
		app := ctx.App()
		name := "app"
		if app != nil {
			name = app.Name()
		}
		ctx.Println("Usage: " + name + " migrate <command>")
		ctx.Println("")
		ctx.Println("Commands:")
		ctx.Println("  up       Run all pending migrations")
		ctx.Println("  down     Rollback the last migration batch")
		ctx.Println("  status   Show migration status")
		return nil
	})

	// migrate up
	upCmd := buildMigrateUpCommand()
	_ = migrateCmd.AddSubcommand(upCmd)

	// migrate down
	downCmd := buildMigrateDownCommand()
	_ = migrateCmd.AddSubcommand(downCmd)

	// migrate status
	statusCmd := buildMigrateStatusCommand()
	_ = migrateCmd.AddSubcommand(statusCmd)

	return migrateCmd
}

// buildMigrateUpCommand creates the "migrate up" command.
func buildMigrateUpCommand() Command {
	return NewCommand("up", "Run all pending migrations", func(ctx CommandContext) error {
		app := ctx.App()
		if app == nil {
			return NewError("app not available", ExitError)
		}

		// Bootstrap: Start the app to initialize extensions without HTTP server.
		// For central mode we do NOT suppress migrations — the PhaseAfterRegister
		// forward hook is what applies migrations (schema must be up before Start-all).
		if err := app.Start(ctx.Context()); err != nil {
			return fmt.Errorf("failed to start app for migrations: %w", err)
		}
		defer app.Stop(ctx.Context()) //nolint:errcheck // best-effort cleanup

		if app.CentralMigrationsEnabled() {
			cm, ok := app.CentralMigrator()
			if !ok {
				ctx.Info("migrations applied during startup; no CentralMigrator registered for status reporting")
				return nil
			}

			groups, err := cm.StatusAll(ctx.Context())
			if err != nil {
				return fmt.Errorf("failed to get migration status: %w", err)
			}

			totalApplied := 0
			totalPending := 0
			for _, g := range groups {
				totalApplied += len(g.Applied)
				totalPending += len(g.Pending)
			}
			if totalPending == 0 {
				ctx.Success(fmt.Sprintf("All migrations applied (%d total)", totalApplied))
			} else {
				ctx.Info(fmt.Sprintf("%d migration(s) applied, %d still pending", totalApplied, totalPending))
			}
			return nil
		}

		// Per-extension path (unchanged).
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
func buildMigrateDownCommand() Command {
	return NewCommand("down", "Rollback the last migration batch", func(ctx CommandContext) error {
		app := ctx.App()
		if app == nil {
			return NewError("app not available", ExitError)
		}

		if app.CentralMigrationsEnabled() {
			// Suppress the PhaseAfterRegister forward-migration hook so
			// bootstrapping does not apply pending migrations before rollback.
			app.SetMigrationsDisabled(true)
		}

		// Bootstrap app.
		if err := app.Start(ctx.Context()); err != nil {
			return fmt.Errorf("failed to start app for rollback: %w", err)
		}
		defer app.Stop(ctx.Context()) //nolint:errcheck // best-effort cleanup

		if app.CentralMigrationsEnabled() {
			cm, ok := app.CentralMigrator()
			if !ok {
				ctx.Info("CentralMigrations is enabled but no CentralMigrator was contributed to the container (is grove registered?)")
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

			result, err := cm.RollbackAll(ctx.Context())
			if err != nil {
				return fmt.Errorf("central rollback failed: %w", err)
			}
			if result.RolledBack == 0 {
				ctx.Info("Nothing to rollback")
			} else {
				ctx.Success(fmt.Sprintf("Rolled back %d migration(s)", result.RolledBack))
				for _, name := range result.Names {
					ctx.Println(fmt.Sprintf("  %s %s", Yellow("↩"), name))
				}
			}
			return nil
		}

		// Per-extension path (unchanged).
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

// renderMigrationGroups renders only the per-group section (group header +
// Version/Name/Status/Applied-At table) for each group in the slice.
// Callers are responsible for printing their own top-level section header
// before calling this helper.
func renderMigrationGroups(ctx CommandContext, groups []*forge.MigrationGroupInfo) {
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

// buildMigrateStatusCommand creates the "migrate status" command.
func buildMigrateStatusCommand() Command {
	return NewCommand("status", "Show migration status", func(ctx CommandContext) error {
		app := ctx.App()
		if app == nil {
			return NewError("app not available", ExitError)
		}

		if app.CentralMigrationsEnabled() {
			// Suppress the PhaseAfterRegister forward-migration hook so
			// bootstrapping does not apply pending migrations when showing status.
			app.SetMigrationsDisabled(true)
		}

		// Bootstrap app.
		if err := app.Start(ctx.Context()); err != nil {
			return fmt.Errorf("failed to start app: %w", err)
		}
		defer app.Stop(ctx.Context()) //nolint:errcheck // best-effort cleanup

		if app.CentralMigrationsEnabled() {
			cm, ok := app.CentralMigrator()
			if !ok {
				ctx.Info("CentralMigrations is enabled but no CentralMigrator was contributed to the container (is grove registered?)")
				return nil
			}

			groups, err := cm.StatusAll(ctx.Context())
			if err != nil {
				return fmt.Errorf("failed to get central migration status: %w", err)
			}

			if len(groups) == 0 {
				ctx.Info("No migrations registered")
				return nil
			}

			ctx.Println("")
			ctx.Println(fmt.Sprintf("%s:", Bold("Central migrations")))
			renderMigrationGroups(ctx, groups)
			return nil
		}

		// Per-extension path (unchanged).
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
			renderMigrationGroups(ctx, groups)
		}
		return nil
	})
}

// registerExtensionCommands discovers and registers commands from extensions
// that implement forge.CLICommandProvider. Requires a concrete app.
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
