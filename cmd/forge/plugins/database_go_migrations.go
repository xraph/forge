package plugins

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/errors"
)

// hasGoMigrations checks if there are any .go migration files (excluding migrations.go) in the global path.
func (p *DatabasePlugin) hasGoMigrations() (bool, error) {
	return p.hasGoMigrationsForApp("")
}

// hasGoMigrationsForApp checks if there are any .go migration files (excluding migrations.go)
// in the migration directory for the given app (or global if appName is empty).
func (p *DatabasePlugin) hasGoMigrationsForApp(appName string) (bool, error) {
	migrationPath, err := p.getMigrationPathForApp(appName)
	if err != nil {
		return false, err
	}

	entries, err := os.ReadDir(migrationPath)
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Look for .go files that aren't migrations.go
		if strings.HasSuffix(name, ".go") && name != "migrations.go" {
			return true, nil
		}
	}

	return false, nil
}

// runWithGoMigrations builds and executes a temporary migration runner that includes Go migrations.
// dsn is the caller's already-resolved DSN (see resolveDSNFrom): this function does not
// re-derive it from ctx or config, so the value a handler validated is exactly the value
// the runner receives. A `--dsn` flag that passed preflight can no longer be silently
// ignored by a second, different resolution deeper in the call.
func (p *DatabasePlugin) runWithGoMigrations(ctx cli.CommandContext, command, dsn string) error {
	appName := ctx.String("app")

	// Catch the pre-grove migrations.go before spending a tidy and a build on
	// source that cannot compile. Without this the user's first sight of the
	// problem is a compiler diagnostic about a generated file they never wrote.
	if err := p.checkLegacyMigrationsScaffold(appName); err != nil {
		return err
	}

	// Detect extension migrations
	extensionImports, err := p.detectExtensionMigrations()
	if err != nil {
		return fmt.Errorf("failed to detect extensions: %w", err)
	}

	if len(extensionImports) > 0 {
		ctx.Info(fmt.Sprintf("🔍 Detected Go migrations (including %d extensions) - building migration runner...", len(extensionImports)))
	} else {
		ctx.Info("🔍 Detected Go migrations - building migration runner...")
	}

	// Show verbose info if requested
	if ctx.Bool("verbose") {
		migrationPath, _ := p.getMigrationPathForApp(appName)
		ctx.Info("📁 Migration directory: " + migrationPath)
		if appName != "" {
			ctx.Info("📦 App: " + appName)
		}

		// Check if migrations.go exists
		migrationsGoPath := filepath.Join(migrationPath, "migrations.go")
		if _, err := os.Stat(migrationsGoPath); os.IsNotExist(err) {
			ctx.Info("⚠️  migrations.go not found - run 'forge db init" + appFlagHint(appName) + "' to create it")
		}
	}

	binaryPath, err := p.buildMigrationRunner(dsn, appName)
	if err != nil {
		return err
	}

	// Execute the migration command
	migrationCmd := exec.CommandContext(context.Background(), binaryPath, command)
	migrationCmd.Dir = p.config.RootDir

	// grove's tracking tables are fixed names (grove_migrations, grove_migration_locks);
	// there is no per-app override in the verified API, so unlike the old bun runner this
	// no longer forwards FORGE_MIGRATION_TABLE/FORGE_MIGRATION_LOCKS_TABLE. App isolation
	// on a shared database now depends on the app's group name instead: the scaffolded
	// migrations.go names its group after the app, and FORGE_MIGRATION_GROUP tells the
	// runner which group to run.
	migrationCmd.Env = append(os.Environ(), "DATABASE_URL="+dsn)
	if appName != "" {
		migrationCmd.Env = append(migrationCmd.Env, "FORGE_MIGRATION_GROUP="+appName)
	}
	migrationCmd.Stdout = os.Stdout
	migrationCmd.Stderr = os.Stderr

	if err := migrationCmd.Run(); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// buildMigrationRunner generates, tidies and compiles a temporary migration runner for
// the given DSN and app, returning the path to the built binary. It does not execute the
// binary, so a test can build the runner and drive it directly without going through
// runWithGoMigrations' CommandContext plumbing.
//
// On success the temp directory holding the generated source is left in place: the
// returned binaryPath lives inside it, and the caller is expected to run the binary
// before the process exits. On a tidy or build failure, the temp directory is also left
// in place (and its path is folded into the returned error) so the generated source that
// caused the failure survives for inspection.
func (p *DatabasePlugin) buildMigrationRunner(dsn, appName string) (string, error) {
	// Resolve the driver before creating anything on disk, so a bad DSN fails
	// immediately instead of leaving a half-built temp directory. generateMigrationRunner
	// below resolves it again for the source it emits; drv is kept here too, because the
	// go.mod built below needs the driver module's identity to pin its version.
	drv, err := resolveGroveDriver(dsn)
	if err != nil {
		return "", fmt.Errorf("failed to resolve database driver: %w", err)
	}

	// Create temporary directory for migration runner
	tmpDir, err := os.MkdirTemp("", "forge-migrate-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// keepDir is set once the runner is ready, or when a build step fails.
	// In the failure case, the generated source that caused it survives for
	// inspection instead of vanishing with the temp directory. In the
	// success case, the binary itself lives inside tmpDir, so removing it
	// here would delete what the caller is about to run.
	keepDir := false
	defer func() {
		if !keepDir {
			os.RemoveAll(tmpDir)
		}
	}()

	// Generate migration runner
	runnerPath := filepath.Join(tmpDir, "main.go")
	if err := p.generateMigrationRunner(runnerPath, dsn, appName); err != nil {
		return "", fmt.Errorf("failed to generate migration runner: %w", err)
	}

	// Initialize go.mod in temp directory
	moduleName, err := p.getModuleName()
	if err != nil {
		return "", fmt.Errorf("failed to get module name: %w", err)
	}

	// Get Go version from project's go.mod
	goVersion, err := p.getGoVersion()
	if err != nil {
		goVersion = "1.21" // fallback to reasonable default
	}

	// Get replace directives from the project's go.mod for local modules
	replaceDirectives, err := p.getReplaceDirectives()
	if err != nil {
		return "", fmt.Errorf("failed to get replace directives: %w", err)
	}

	// Build replace section
	replacesSection := fmt.Sprintf("replace %s => %s\n", moduleName, p.config.RootDir)

	var replacesSectionSb103 strings.Builder
	for module, path := range replaceDirectives {
		replacesSectionSb103.WriteString(fmt.Sprintf("replace %s => %s\n", module, path))
	}

	replacesSection += replacesSectionSb103.String()

	// Pin grove and the driver module to the same version the user's own project already
	// requires, if it requires one. Their migrations package imports "grove/migrate" (see
	// createMigrationsGoFile), which is part of the grove module itself, so once "forge db
	// init" has run and "go mod tidy" has touched their project at least once, their go.mod
	// already names a real, resolved grove version.
	//
	// Without this, the driver module (and grove itself, which the generated source below
	// also imports directly) would be left unrequired below and "go mod tidy" would resolve
	// both to whatever the module proxy currently serves. That makes two runs of "forge db
	// migrate" against the same commit, on the same machine, capable of building against
	// different grove releases -- not an acceptable property for a tool whose whole job is
	// applying migrations to a live database. Pinning the driver to a literal "v0.0.0" is
	// not an option either: every grove driver's own go.mod requires "github.com/xraph/grove
	// v0.0.0" paired with a replace that only resolves inside grove's own repo checkout
	// (replace directives in a dependency's go.mod are never honored outside the main
	// module), so "v0.0.0" is unresolvable from here regardless of which version we mean.
	//
	// The driver modules are tagged in lockstep with grove core (confirmed against the
	// module cache: sqlitedriver, pgdriver, and the others carry the exact same version
	// numbers as grove itself at every release), so grove's version is also correct for the
	// driver.
	groveVersion, groveErr := p.getGroveVersion()

	groveRequireLines := ""
	if groveErr == nil {
		groveRequireLines = fmt.Sprintf("\tgithub.com/xraph/grove %s\n\t%s %s\n", groveVersion, drv.Module, groveVersion)
	}
	// groveErr != nil means the project's go.mod has no grove requirement yet (for example,
	// a freshly scaffolded project that has never run "go mod tidy" itself). There is no
	// version to pin to, so groveRequireLines stays empty and the require block below falls
	// back to leaving grove and the driver unrequired, exactly as before this fix: "go mod
	// tidy" resolves them unpinned, on this one run only.

	// Create go.mod with replace directives pointing to the actual project and local
	// dependencies. moduleName is locally replaced above, so "v0.0.0" is a harmless
	// placeholder for it: the replace redirects to a directory, and no network lookup of
	// that version string ever happens.
	goModContent := fmt.Sprintf(`module forge-migrate-runner

go %s

%s
require (
	%s v0.0.0
%s)
`, goVersion, replacesSection, moduleName, groveRequireLines)

	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		return "", fmt.Errorf("failed to create go.mod: %w", err)
	}

	// Run go mod tidy to generate go.sum and resolve all dependencies
	tidyCmd := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidyCmd.Dir = tmpDir

	tidyCmd.Env = os.Environ()
	if output, err := tidyCmd.CombinedOutput(); err != nil {
		// Leave tmpDir in place. Reporting a dependency error without the
		// generated source that caused it makes this unreportable as a bug.
		keepDir = true

		return "", fmt.Errorf("failed to tidy dependencies: %w\n\n%s\nGenerated source kept at: %s", err, output, tmpDir)
	}

	// Build the runner
	binaryPath := filepath.Join(tmpDir, "migrate")
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, ".")
	buildCmd.Dir = tmpDir

	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	if out, err := buildCmd.CombinedOutput(); err != nil {
		// Leave tmpDir in place. Reporting a compile error without the source
		// that caused it makes this unreportable as a bug.
		keepDir = true

		return "", fmt.Errorf("failed to build the migration runner: %w\n\n%s\nGenerated source kept at: %s", err, out, tmpDir)
	}

	// The binary now lives inside tmpDir; keep the directory so the caller can run it.
	keepDir = true

	return binaryPath, nil
}

// generateMigrationRunner creates a temporary Go file that imports the user's migrations
// and drives them through grove's migrate orchestrator for the DSN's scheme.
func (p *DatabasePlugin) generateMigrationRunner(outputPath, dsn, appName string) error {
	// Resolve the driver first, so a bad DSN fails before anything is written to disk.
	drv, err := resolveGroveDriver(dsn)
	if err != nil {
		return err
	}

	// Determine the module name from go.mod
	moduleName, err := p.getModuleName()
	if err != nil {
		return err
	}

	// Determine migrations package path
	migrationPath, err := p.getMigrationPathForApp(appName)
	if err != nil {
		return err
	}

	// Calculate relative path from project root to migrations
	relPath, err := filepath.Rel(p.config.RootDir, migrationPath)
	if err != nil {
		return err
	}

	// Construct full import path
	migrationsImport := filepath.Join(moduleName, filepath.ToSlash(relPath))

	// Validate migration import path
	if migrationsImport == "" {
		return errors.New("migration import path is empty")
	}

	// Detect extension migrations (like authsome)
	extensionImports, err := p.detectExtensionMigrations()
	if err != nil {
		return err
	}

	// Build imports section with grove, the resolved driver, and project/extension migrations
	var importsBuilder strings.Builder
	importsBuilder.WriteString(fmt.Sprintf(`	"context"
	"fmt"
	"os"
	"strings"

	"github.com/xraph/grove"
	"github.com/xraph/grove/migrate"

	// The driver registers the DSN scheme, its migrate subpackage registers
	// the executor. Both are blank imports for their init() side effects.
	_ %q
	_ %q
`, drv.Module, drv.MigrateImport))

	// Add project migrations import
	if migrationsImport != "" {
		importsBuilder.WriteString(fmt.Sprintf("\n\t// Import project migrations\n\t\"%s\"\n", migrationsImport))
	}

	// Add extension migrations imports
	if len(extensionImports) > 0 {
		importsBuilder.WriteString("\n\t// Import extension migrations")

		for _, extImport := range extensionImports {
			importsBuilder.WriteString(fmt.Sprintf("\n\t_ \"%s\"", extImport))
		}
	}

	importsSection := importsBuilder.String()

	// grove's own drivers disagree on what shape of DSN they want. postgres, mysql,
	// mongodb, clickhouse and turso all hand the DSN straight to a client library that
	// natively understands "scheme://..." connection strings, so passing DATABASE_URL
	// through unchanged is correct for them. sqlite is the odd one out: its underlying
	// driver (modernc.org/sqlite) only recognizes a bare filesystem path, ":memory:", or
	// a "file:" URI -- a "sqlite://" prefix is not a URI scheme it understands, and gets
	// used literally as a (nonexistent) path component, so the open fails. Translate only
	// for sqlite; every other scheme keeps using DATABASE_URL as-is.
	dsnSection := "\topenDSN := dsn\n"
	if drv.Scheme == "sqlite" {
		dsnSection = `	// grove's sqlite driver wants a bare path, ":memory:", or a "file:" URI, not
	// the "sqlite://" scheme this CLI's DSN convention uses everywhere else.
	openDSN := strings.TrimPrefix(dsn, "sqlite://")
	openDSN = strings.TrimPrefix(openDSN, "sqlite:")
	if openDSN != ":memory:" && !strings.HasPrefix(openDSN, "file:") {
		openDSN = "file:" + openDSN
	}
`
	}

	// Generate the runner code
	code := fmt.Sprintf(`package main

import (
%s
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: migrate <command>\n")
		os.Exit(1)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL environment variable is required")
		os.Exit(1)
	}

%s
	ctx := context.Background()

	drv, err := grove.OpenDriver(ctx, %q, openDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open database: %%v\n", err)
		os.Exit(1)
	}

	db, err := grove.Open(drv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open grove: %%v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	exec, err := migrate.NewExecutorFor(db.Driver())
	if err != nil {
		fmt.Fprintf(os.Stderr, "no migration executor for this driver: %%v\n", err)
		os.Exit(1)
	}

	groups := migrations.Registry.Groups()

	// Grove tracks applied migrations per group (the "group" column), not per
	// table, so app isolation on a shared database is done by selecting a
	// group here rather than by overriding a table name. Do not reinstate
	// FORGE_MIGRATION_TABLE / FORGE_MIGRATION_LOCKS_TABLE: grove's migration
	// and lock table names are unexported constants with no override hook.
	if groupName := os.Getenv("FORGE_MIGRATION_GROUP"); groupName != "" {
		var selected []*migrate.Group
		for _, g := range groups {
			if g.Name() == groupName {
				selected = append(selected, g)
			}
		}
		groups = selected
	}

	orch := migrate.NewOrchestrator(exec, groups...)
	command := os.Args[1]

	// Check if any migration groups are registered
	if len(groups) == 0 {
		fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m No migrations found\n\n")
		fmt.Fprintln(os.Stderr, "This usually means:")
		fmt.Fprintln(os.Stderr, "  1. No migration files (.sql or .go) exist in your migrations directory")
		fmt.Fprintln(os.Stderr, "  2. Go migration files exist but aren't properly imported")
		fmt.Fprintln(os.Stderr, "  3. The migrations package doesn't have a migrations.go file")
		fmt.Fprintln(os.Stderr, "\nTo create a migration, run:")
		fmt.Fprintln(os.Stderr, "  forge db create-sql <migration_name>")
		fmt.Fprintln(os.Stderr, "  forge db create-go <migration_name>")
		fmt.Fprintln(os.Stderr, "\nOr initialize the migrations package:")
		fmt.Fprintln(os.Stderr, "  forge db init")
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}

	switch command {
	case "init":
		if err := exec.EnsureMigrationTable(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		if err := exec.EnsureLockTable(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		fmt.Println("\n\033[32m✓ Migration tables created\033[0m\n")

	case "migrate":
		result, err := orch.Migrate(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		if len(result.Applied) == 0 {
			fmt.Println("\n\033[33mℹ\033[0m  No pending migrations\n")
		} else {
			fmt.Printf("\n\033[32m✓ Applied %%d migration(s)\033[0m\n", len(result.Applied))
			for _, m := range result.Applied {
				fmt.Printf("\033[90m    • %%s\033[0m\n", m.Name)
			}
			fmt.Println()
		}

	case "rollback":
		result, err := orch.Rollback(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		if len(result.Rollback) == 0 {
			fmt.Println("\n\033[33mℹ\033[0m  No migrations to rollback\n")
		} else {
			fmt.Printf("\n\033[32m✓ Rolled back %%d migration(s)\033[0m\n", len(result.Rollback))
			for _, m := range result.Rollback {
				fmt.Printf("\033[90m    • %%s\033[0m\n", m.Name)
			}
			fmt.Println()
		}

	case "status":
		statuses, err := orch.Status(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}

		fmt.Println()
		fmt.Println("\033[1m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
		fmt.Println("\033[1m  Migration Status\033[0m")
		fmt.Println("\033[1m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")

		totalApplied, totalPending := 0, 0

		for _, gs := range statuses {
			fmt.Printf("\n\033[1mGroup: %%s\033[0m\n", gs.Name)

			if len(gs.Applied) > 0 {
				fmt.Printf("\n\033[32m✓ Applied\033[0m \033[90m(%%d)\033[0m\n", len(gs.Applied))
				for _, ms := range gs.Applied {
					fmt.Printf("  \033[32m✓\033[0m  \033[1m%%s\033[0m\n", ms.Migration.Name)
				}
			}

			if len(gs.Pending) > 0 {
				fmt.Printf("\n\033[33m⏸  Pending\033[0m \033[90m(%%d)\033[0m\n", len(gs.Pending))
				for _, ms := range gs.Pending {
					fmt.Printf("  \033[33m⏸\033[0m  \033[1m%%s\033[0m\n", ms.Migration.Name)
				}
			}

			totalApplied += len(gs.Applied)
			totalPending += len(gs.Pending)
		}

		fmt.Println()
		if totalPending > 0 {
			fmt.Println("\033[36m💡 Run 'forge db migrate' to apply pending migrations\033[0m")
		} else if totalApplied > 0 {
			fmt.Println("\033[32m✅ All migrations applied!\033[0m")
		} else {
			fmt.Println("\033[90mℹ️  No migrations found\033[0m")
		}

		fmt.Println("\n\033[1m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
		fmt.Println()

	case "unlock":
		if err := exec.ReleaseLock(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		fmt.Println("\n\033[32m✓ Migrations unlocked\033[0m\n")

	case "lock":
		if err := exec.AcquireLock(ctx, "forge-cli"); err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		fmt.Println("\n\033[32m✓ Migrations locked\033[0m\n")

	case "mark-applied":
		statuses, err := orch.Status(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}

		marked := 0
		for _, gs := range statuses {
			for _, ms := range gs.Pending {
				if err := exec.RecordApplied(ctx, ms.Migration); err != nil {
					fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
					os.Exit(1)
				}
				marked++
			}
		}

		if marked == 0 {
			fmt.Println("\n\033[33mℹ\033[0m  No pending migrations\n")
		} else {
			fmt.Printf("\n\033[32m✓ Marked %%d migration(s) as applied\033[0m\n\n", marked)
		}

	case "adopt":
		oldTable := os.Getenv("FORGE_LEGACY_MIGRATION_TABLE")
		if oldTable == "" {
			oldTable = "bun_migrations"
		}

		dryRun := os.Getenv("FORGE_ADOPT_DRY_RUN") == "1"

		// Grove tracks applied migrations per group, so the rows adopt writes
		// need to land in the same group the app's other migrations use.
		// FORGE_MIGRATION_GROUP is already set by the CLI handler for
		// app-scoped runs; the fallback matches the scaffold's default group.
		group := os.Getenv("FORGE_MIGRATION_GROUP")
		if group == "" {
			group = "app"
		}

		// Read the legacy table with a raw query. Grove knows nothing about
		// it, so there is nothing on the Executor contract that would.
		rows, err := exec.Query(ctx, "SELECT name FROM "+oldTable)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nothing to adopt: cannot read %%s: %%v\n", oldTable, err)
			os.Exit(1)
		}

		var legacy []string

		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				fmt.Fprintf(os.Stderr, "failed to read a row from %%s: %%v\n", oldTable, err)
				os.Exit(1)
			}

			legacy = append(legacy, name)
		}

		// Next() returning false means "no more rows" OR "iteration failed
		// partway through"; Err() is the only way to tell those apart. Without
		// this check, a mid-read failure leaves legacy as a silent partial
		// list that gets reported as if it were the whole table.
		if err := rows.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "nothing to adopt: failed reading %%s: %%v\n", oldTable, err)
			os.Exit(1)
		}

		_ = rows.Close()

		if len(legacy) == 0 {
			fmt.Fprintf(os.Stderr, "nothing to adopt: %%s is empty\n", oldTable)
			os.Exit(1)
		}

		// Table creation is a real write; --dry-run must not perform it.
		if !dryRun {
			if err := exec.EnsureMigrationTable(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "failed to create grove's migration table: %%v\n", err)
				os.Exit(1)
			}

			if err := exec.EnsureLockTable(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "failed to create grove's lock table: %%v\n", err)
				os.Exit(1)
			}
		}

		applied, err := exec.ListApplied(ctx)
		if err != nil {
			if !dryRun {
				fmt.Fprintf(os.Stderr, "failed to list applied migrations: %%v\n", err)
				os.Exit(1)
			}

			// adopt's primary scenario is a project that ran bun migrations
			// but has never run grove's init or migrate, so the legacy table
			// read above succeeded while grove's own tables do not exist yet.
			// EnsureMigrationTable was skipped above because this is a dry
			// run, so ListApplied failing here is expected, not exceptional:
			// treat it as "grove has no state yet" and proceed with an empty
			// known set, so every legacy row reports as would-be-adopted.
			//
			// This does mean a real connectivity failure during a dry run
			// reports the same way, as "no grove state" rather than an
			// outage. That is accepted on purpose: a dry run writes nothing
			// either way, and the operator sees the row count and can rerun
			// without --dry-run to get the real error if something is
			// actually wrong.
			fmt.Printf("grove's migration tables do not exist yet, so every row below would be adopted\n")

			applied = nil
		}

		// Grove's uniqueness constraint is on (version, group) together, and
		// ListApplied returns every group unscoped. Keying on version alone
		// would treat two different apps' migrations that happen to share a
		// version as the same row, so the second app's adopt would skip
		// recording its own copy and that migration would genuinely re-run
		// on the next "forge db migrate".
		type versionGroup struct {
			version string
			group   string
		}

		known := make(map[versionGroup]bool, len(applied))
		for _, a := range applied {
			known[versionGroup{version: a.Version, group: a.Group}] = true
		}

		var adopted, present, skipped int

		for _, raw := range legacy {
			version, name, ok := splitLegacyName(raw)
			if !ok {
				fmt.Printf("skipped %%q: no version could be read from it\n", raw)
				skipped++

				continue
			}

			if known[versionGroup{version: version, group: group}] {
				present++

				continue
			}

			if dryRun {
				fmt.Printf("would adopt %%s (%%s)\n", name, version)
				adopted++

				continue
			}

			if err := exec.RecordApplied(ctx, &migrate.Migration{
				Name:    name,
				Version: version,
				Group:   group,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "failed to record %%s: %%v\n", raw, err)
				os.Exit(1)
			}

			adopted++
		}

		fmt.Printf("\nadopted %%d, already present %%d, skipped %%d\n", adopted, present, skipped)

	default:
		fmt.Fprintf(os.Stderr, "\n\033[31m✗ Unknown command:\033[0m %%s\n\n", command)
		os.Exit(1)
	}
}

// splitLegacyName mirrors the CLI's splitBunMigrationName. The two must agree:
// if they drift, adopt records versions that sort differently from the ones
// mark-applied writes.
//
// A bare timestamp is the shape bun actually persists. Its migration table keeps
// only the digits group of a filename; the descriptive half lands in Comment,
// which is tagged as not-a-column and is therefore lost. The "<digits>_<rest>"
// form is honored too for hand-written tables that do store a full name.
//
// Grove keys an applied migration on group plus version, so the name is display
// text only. "adopted_<version>" makes it obvious in "forge db status" that the
// row was adopted rather than registered in code, and so has no Up or Down.
func splitLegacyName(raw string) (version, name string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	if prefix, rest, found := strings.Cut(raw, "_"); found {
		if prefix == "" || rest == "" || !allDigits(prefix) {
			return "", "", false
		}

		return prefix, rest, true
	}

	if !allDigits(raw) {
		return "", "", false
	}

	return raw, "adopted_" + raw, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
`, importsSection, dsnSection, drv.Scheme)

	return os.WriteFile(outputPath, []byte(code), 0644)
}

// detectExtensionMigrations scans go.mod for known extensions with migrations.
func (p *DatabasePlugin) detectExtensionMigrations() ([]string, error) {
	goModPath := filepath.Join(p.config.RootDir, "go.mod")

	content, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read go.mod: %w", err)
	}

	// Known extensions that have migrations
	knownExtensions := map[string]string{
		"github.com/xraph/authsome": "github.com/xraph/authsome/migrations",
		// Add more extensions here as needed
		// "github.com/yourorg/someext": "github.com/yourorg/someext/migrations",
	}

	var extensionImports []string

	contentStr := string(content)

	// Check if each known extension is in go.mod
	for extModule, migrationPkg := range knownExtensions {
		// Look for the extension in require statements
		if strings.Contains(contentStr, extModule) {
			extensionImports = append(extensionImports, migrationPkg)
		}
	}

	return extensionImports, nil
}

// getModuleName extracts the module name from go.mod.
func (p *DatabasePlugin) getModuleName() (string, error) {
	goModPath := filepath.Join(p.config.RootDir, "go.mod")

	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}

	return "", errors.New("module directive not found in go.mod")
}

// getGoVersion extracts the Go version from go.mod.
func (p *DatabasePlugin) getGoVersion() (string, error) {
	goModPath := filepath.Join(p.config.RootDir, "go.mod")

	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go")), nil
		}
	}

	return "", errors.New("go directive not found in go.mod")
}

// getGroveVersion reads the version of github.com/xraph/grove that the user's own
// project requires, matching either a single-line "require github.com/xraph/grove
// vX.Y.Z" or an entry inside a parenthesized "require ( ... )" block (the form "go mod
// tidy" itself produces, including as an "// indirect" entry, which is the common case:
// the project imports "github.com/xraph/grove/migrate" via its scaffolded migrations
// package, not the grove root package directly). Returns an error if go.mod cannot be
// read or names no such requirement, which callers treat as "nothing to pin to" rather
// than a hard failure.
func (p *DatabasePlugin) getGroveVersion() (string, error) {
	const groveModule = "github.com/xraph/grove"

	goModPath := filepath.Join(p.config.RootDir, "go.mod")

	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	inRequireBlock := false

	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "require (" {
			inRequireBlock = true
			continue
		}

		if inRequireBlock && trimmed == ")" {
			inRequireBlock = false
			continue
		}

		candidate := trimmed
		if !inRequireBlock {
			rest, ok := strings.CutPrefix(candidate, "require ")
			if !ok {
				continue
			}

			candidate = rest
		}

		// Fields splits on whitespace, so a trailing "// indirect" comment lands in its
		// own fields past the version and is simply ignored.
		fields := strings.Fields(candidate)
		if len(fields) >= 2 && fields[0] == groveModule {
			return fields[1], nil
		}
	}

	return "", fmt.Errorf("%s requirement not found in go.mod", groveModule)
}

// getReplaceDirectives extracts replace directives from go.mod
// This handles local modules that aren't publicly available.
func (p *DatabasePlugin) getReplaceDirectives() (map[string]string, error) {
	goModPath := filepath.Join(p.config.RootDir, "go.mod")

	content, err := os.ReadFile(goModPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read go.mod: %w", err)
	}

	replaces := make(map[string]string)
	lines := strings.SplitSeq(string(content), "\n")

	for line := range lines {
		line = strings.TrimSpace(line)
		// Handle single-line replace directives: replace example.com/module => /path/to/module
		if after, ok := strings.CutPrefix(line, "replace "); ok {
			line = after

			parts := strings.Split(line, "=>")
			if len(parts) == 2 {
				module := strings.TrimSpace(parts[0])
				// Remove version if present (e.g., "module v1.2.3" -> "module")
				moduleParts := strings.Fields(module)
				if len(moduleParts) > 0 {
					module = moduleParts[0]
				}

				path := strings.TrimSpace(parts[1])
				// Remove version if present in replacement path
				pathParts := strings.Fields(path)
				if len(pathParts) > 0 {
					path = pathParts[0]
				}
				// Convert relative paths to absolute
				if !filepath.IsAbs(path) {
					path = filepath.Join(p.config.RootDir, path)
				}

				replaces[module] = path
			}
		}
	}

	return replaces, nil
}
