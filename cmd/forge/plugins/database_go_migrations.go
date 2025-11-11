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

// hasGoMigrations checks if there are any .go migration files (excluding migrations.go).
func (p *DatabasePlugin) hasGoMigrations() (bool, error) {
	migrationPath, err := p.getMigrationPath()
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

	// Get database config
	dbConfig, err := p.loadDatabaseConfig(dbName, ctx.String("app"))
	if err != nil {
		return fmt.Errorf("failed to load database config: %w", err)
	}

	// Override with flags if provided
	if customDSN := ctx.String("dsn"); customDSN != "" {
		dbConfig.DSN = customDSN
	}

	// Expand environment variables in DSN
	dbConfig.DSN = expandEnvVars(dbConfig.DSN)

	// Create temporary directory for migration runner
	tmpDir, err := os.MkdirTemp("", "forge-migrate-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate migration runner
	runnerPath := filepath.Join(tmpDir, "main.go")
	if err := p.generateMigrationRunner(runnerPath, dbConfig); err != nil {
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

	// Create go.mod with replace directives pointing to the actual project and local dependencies
	goModContent := fmt.Sprintf(`module forge-migrate-runner

go %s

%s
require (
	%s v0.0.0
)
`, goVersion, replacesSection, moduleName)

	goModPath := filepath.Join(tmpDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte(goModContent), 0644); err != nil {
		return fmt.Errorf("failed to create go.mod: %w", err)
	}

	// Run go mod tidy to generate go.sum and resolve all dependencies
	tidyCmd := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	tidyCmd.Dir = tmpDir

	tidyCmd.Env = os.Environ()
	if output, err := tidyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to tidy dependencies: %w\n%s", err, string(output))
	}

	// Build the runner
	binaryPath := filepath.Join(tmpDir, "migrate")
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, ".")
	buildCmd.Dir = tmpDir

	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	if output, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build migration runner: %w\n%s", err, string(output))
	}

	// Execute the migration command
	migrationCmd := exec.CommandContext(context.Background(), binaryPath, command)
	migrationCmd.Dir = p.config.RootDir

	migrationCmd.Env = append(os.Environ(), "DATABASE_URL="+dbConfig.DSN)
	migrationCmd.Stdout = os.Stdout
	migrationCmd.Stderr = os.Stderr

	if err := migrationCmd.Run(); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// generateMigrationRunner creates a temporary Go file that imports the user's migrations.
func (p *DatabasePlugin) generateMigrationRunner(outputPath string, dbConfig any) error {
	// Determine the module name from go.mod
	moduleName, err := p.getModuleName()
	if err != nil {
		return err
	}

	// Determine migrations package path
	migrationPath, err := p.getMigrationPath()
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

	// Build imports section with project and extension migrations
	var importsBuilder strings.Builder
	importsBuilder.WriteString(`	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
`)

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

	// Get database DSN from environment
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintf(os.Stderr, "DATABASE_URL environment variable is required\n")
		os.Exit(1)
	}

	// Connect to PostgreSQL database
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		fmt.Fprintf(os.Stderr, "Currently only PostgreSQL is supported. DSN must start with postgres:// or postgresql://\n")
		os.Exit(1)
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())

	// Create migrator with user's migrations
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	ctx := context.Background()
	command := os.Args[1]

	switch command {
	case "init":
		if err := migrator.Init(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		fmt.Println("\n\033[32m✓ Migration tables created\033[0m")
		fmt.Println("  • bun_migrations")
		fmt.Println("  • bun_migration_locks")
		fmt.Println()

	case "migrate":
		// Ensure migration tables exist (auto-init if needed)
		if err := migrator.Init(ctx); err != nil {
			// Ignore error if tables already exist
			if !strings.Contains(err.Error(), "already exists") {
				fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
				os.Exit(1)
			}
		}

		group, err := migrator.Migrate(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		if group.IsZero() {
			fmt.Println("\n\033[33mℹ\033[0m  No pending migrations\n")
		} else {
			fmt.Printf("\n\033[32m✓ Migrated to %%s\033[0m\n", group)
			if len(group.Migrations) > 0 {
				fmt.Println("\033[90m  Applied migrations:\033[0m")
				for _, m := range group.Migrations {
					fmt.Printf("\033[90m    • %%s\033[0m\n", m.Name)
				}
			}
			fmt.Println()
		}

	case "rollback":
		group, err := migrator.Rollback(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		if group.IsZero() {
			fmt.Println("\n\033[33mℹ\033[0m  No migrations to rollback\n")
		} else {
			fmt.Printf("\n\033[32m✓ Rolled back %%s\033[0m\n", group)
			if len(group.Migrations) > 0 {
				fmt.Println("\033[90m  Rolled back migrations:\033[0m")
				for _, m := range group.Migrations {
					fmt.Printf("\033[90m    • %%s\033[0m\n", m.Name)
				}
			}
			fmt.Println()
		}

	case "status":
		ms, err := migrator.MigrationsWithStatus(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}

		applied := ms.Applied()
		unapplied := ms.Unapplied()
		lastGroup := ms.LastGroup()

		fmt.Println()
		fmt.Println("\033[1m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
		fmt.Println("\033[1m  Migration Status\033[0m")
		fmt.Println("\033[1m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")

		if len(applied) > 0 {
			fmt.Printf("\n\033[32m✓ Applied Migrations\033[0m \033[90m(%%d)\033[0m\n", len(applied))
			if !lastGroup.IsZero() {
				fmt.Printf("\033[90m  Last Group: %%s\033[0m\n", lastGroup)
			}
			fmt.Println()
			for _, m := range applied {
				fmt.Printf("  \033[32m✓\033[0m  \033[1m%%s\033[0m\n", m.Name)
				fmt.Printf("      \033[90mGroup: %%d\033[0m\n", m.GroupID)
			}
		}

		if len(unapplied) > 0 {
			fmt.Printf("\n\033[33m⏸  Pending Migrations\033[0m \033[90m(%%d)\033[0m\n\n", len(unapplied))
			for _, m := range unapplied {
				fmt.Printf("  \033[33m⏸\033[0m  \033[1m%%s\033[0m\n", m.Name)
			}
			fmt.Println()
			fmt.Println("\033[36m💡 Run 'forge db migrate' to apply pending migrations\033[0m")
		} else if len(applied) > 0 {
			fmt.Println("\n\033[32m✅ All migrations applied!\033[0m")
		} else {
			fmt.Println("\n\033[90mℹ️  No migrations found\033[0m")
		}

		fmt.Println("\n\033[1m━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\033[0m")
		fmt.Println()

	case "unlock":
		if err := migrator.Unlock(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		fmt.Println("\n\033[32m✓ Migrations unlocked\033[0m\n")

	case "lock":
		if err := migrator.Lock(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "\n\033[31m✗ Error:\033[0m %%v\n\n", err)
			os.Exit(1)
		}
		fmt.Println("\n\033[32m✓ Migrations locked\033[0m\n")

	default:
		fmt.Fprintf(os.Stderr, "\n\033[31m✗ Unknown command:\033[0m %%s\n\n", command)
		os.Exit(1)
	}
}
`, importsSection)

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
