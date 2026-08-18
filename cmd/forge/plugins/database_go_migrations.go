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
func (p *DatabasePlugin) runWithGoMigrations(ctx cli.CommandContext, command string) error {
	dbName := ctx.String("database")
	appName := ctx.String("app")

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

	// Get database config
	dbConfig, err := p.loadDatabaseConfig(dbName, appName)
	if err != nil {
		return fmt.Errorf("failed to load database config: %w", err)
	}

	// Override with flags if provided
	if customDSN := ctx.String("dsn"); customDSN != "" {
		dbConfig.DSN = os.ExpandEnv(customDSN)
	}

	// Resolve the driver before creating anything on disk, so a bad DSN
	// fails immediately instead of leaving a half-built temp directory.
	drv, err := resolveGroveDriver(dbConfig.DSN)
	if err != nil {
		return fmt.Errorf("failed to resolve database driver: %w", err)
	}

	// Create temporary directory for migration runner
	tmpDir, err := os.MkdirTemp("", "forge-migrate-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// keepDir is set when a build step fails, so the generated source that
	// caused the failure survives for inspection instead of vanishing with
	// the temp directory.
	keepDir := false
	defer func() {
		if !keepDir {
			os.RemoveAll(tmpDir)
		}
	}()

	// Generate migration runner
	runnerPath := filepath.Join(tmpDir, "main.go")
	if err := p.generateMigrationRunner(runnerPath, dbConfig.DSN, appName); err != nil {
		return fmt.Errorf("failed to generate migration runner: %w", err)
	}

	// Initialize go.mod in temp directory
	moduleName, err := p.getModuleName()
	if err != nil {
		return fmt.Errorf("failed to get module name: %w", err)
	}

	// Get Go version from project's go.mod
	goVersion, err := p.getGoVersion()
	if err != nil {
		goVersion = "1.21" // fallback to reasonable default
	}

	// Get replace directives from the project's go.mod for local modules
	replaceDirectives, err := p.getReplaceDirectives()
	if err != nil {
		return fmt.Errorf("failed to get replace directives: %w", err)
	}

	// Build replace section
	replacesSection := fmt.Sprintf("replace %s => %s\n", moduleName, p.config.RootDir)

	var replacesSectionSb103 strings.Builder
	for module, path := range replaceDirectives {
		replacesSectionSb103.WriteString(fmt.Sprintf("replace %s => %s\n", module, path))
	}

	replacesSection += replacesSectionSb103.String()

	// Create go.mod with replace directives pointing to the actual project and local dependencies.
	// The driver module is a real dependency, not a local replace, so its version is a
	// placeholder that "go mod tidy" resolves to whatever the module proxy actually serves.
	goModContent := fmt.Sprintf(`module forge-migrate-runner

go %s

%s
require (
	%s v0.0.0
	%s v0.0.0
)
`, goVersion, replacesSection, moduleName, drv.Module)

	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		return fmt.Errorf("failed to create go.mod: %w", err)
	}

	// Run go mod tidy to generate go.sum and resolve all dependencies
	tidyCmd := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidyCmd.Dir = tmpDir

	tidyCmd.Env = os.Environ()
	if output, err := tidyCmd.CombinedOutput(); err != nil {
		// Leave tmpDir in place. Reporting a dependency error without the
		// generated source that caused it makes this unreportable as a bug.
		keepDir = true

		return fmt.Errorf("failed to tidy dependencies: %w\n\n%s\nGenerated source kept at: %s", err, output, tmpDir)
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

		return fmt.Errorf("failed to build the migration runner: %w\n\n%s\nGenerated source kept at: %s", err, out, tmpDir)
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
	migrationCmd.Env = append(os.Environ(), "DATABASE_URL="+dbConfig.DSN)
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

	ctx := context.Background()

	drv, err := grove.OpenDriver(ctx, %q, dsn)
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

	default:
		fmt.Fprintf(os.Stderr, "\n\033[31m✗ Unknown command:\033[0m %%s\n\n", command)
		os.Exit(1)
	}
}
`, importsSection, drv.Scheme)

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
