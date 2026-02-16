package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/cmd/forge/config"
)

// =============================================================================
// sanitizeAppName
// =============================================================================

func TestSanitizeAppName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "myapp",
			expected: "myapp",
		},
		{
			name:     "hyphens to underscores",
			input:    "api-gateway",
			expected: "api_gateway",
		},
		{
			name:     "dots to underscores",
			input:    "auth.service",
			expected: "auth_service",
		},
		{
			name:     "spaces to underscores",
			input:    "my app",
			expected: "my_app",
		},
		{
			name:     "uppercase to lowercase",
			input:    "MyApp",
			expected: "myapp",
		},
		{
			name:     "mixed special characters",
			input:    "My-App.V2",
			expected: "my_app_v2",
		},
		{
			name:     "strips invalid characters",
			input:    "app@name!#$",
			expected: "appname",
		},
		{
			name:     "collapses consecutive underscores",
			input:    "api--gateway",
			expected: "api_gateway",
		},
		{
			name:     "trims leading/trailing underscores",
			input:    "-api-gateway-",
			expected: "api_gateway",
		},
		{
			name:     "whitespace trimmed",
			input:    "  my-app  ",
			expected: "my_app",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "!@#$%",
			expected: "",
		},
		{
			name:     "numbers preserved",
			input:    "app123",
			expected: "app123",
		},
		{
			name:     "complex real-world name",
			input:    "user-auth-service-v2.1",
			expected: "user_auth_service_v2_1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeAppName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// migrationTableName / migrationLocksTableName
// =============================================================================

func TestMigrationTableName(t *testing.T) {
	tests := []struct {
		name     string
		appName  string
		expected string
	}{
		{
			name:     "empty app returns default",
			appName:  "",
			expected: "bun_migrations",
		},
		{
			name:     "simple app name",
			appName:  "myapp",
			expected: "bun_migrations_myapp",
		},
		{
			name:     "hyphenated app name",
			appName:  "api-gateway",
			expected: "bun_migrations_api_gateway",
		},
		{
			name:     "uppercase normalized",
			appName:  "AuthService",
			expected: "bun_migrations_authservice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, migrationTableName(tt.appName))
		})
	}
}

func TestMigrationLocksTableName(t *testing.T) {
	tests := []struct {
		name     string
		appName  string
		expected string
	}{
		{
			name:     "empty app returns default",
			appName:  "",
			expected: "bun_migration_locks",
		},
		{
			name:     "simple app name",
			appName:  "myapp",
			expected: "bun_migration_locks_myapp",
		},
		{
			name:     "hyphenated app name",
			appName:  "auth-service",
			expected: "bun_migration_locks_auth_service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, migrationLocksTableName(tt.appName))
		})
	}
}

// =============================================================================
// appFlagHint
// =============================================================================

func TestAppFlagHint(t *testing.T) {
	assert.Equal(t, "", appFlagHint(""))
	assert.Equal(t, " --app api-gateway", appFlagHint("api-gateway"))
	assert.Equal(t, " --app myapp", appFlagHint("myapp"))
}

// =============================================================================
// resolveAppDir
// =============================================================================

func TestResolveAppDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create apps directory structure
	appsDir := filepath.Join(tmpDir, "apps")
	require.NoError(t, os.MkdirAll(filepath.Join(appsDir, "api-gateway"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(appsDir, "auth-service"), 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	t.Run("existing app", func(t *testing.T) {
		dir, err := plugin.resolveAppDir("api-gateway")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(appsDir, "api-gateway"), dir)
	})

	t.Run("another existing app", func(t *testing.T) {
		dir, err := plugin.resolveAppDir("auth-service")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(appsDir, "auth-service"), dir)
	})

	t.Run("non-existing app", func(t *testing.T) {
		_, err := plugin.resolveAppDir("does-not-exist")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "app directory not found")
	})

	t.Run("nil config", func(t *testing.T) {
		p := &DatabasePlugin{config: nil}
		_, err := p.resolveAppDir("myapp")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a forge project")
	})
}

// =============================================================================
// getMigrationPathForApp
// =============================================================================

func TestGetMigrationPathForApp_Global(t *testing.T) {
	tmpDir := t.TempDir()

	// Create default migrations directory
	migrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
		},
	}

	// Empty app name should return global path
	path, err := plugin.getMigrationPathForApp("")
	require.NoError(t, err)
	assert.Equal(t, migrationsDir, path)
}

func TestGetMigrationPathForApp_CentralizedConvention(t *testing.T) {
	tmpDir := t.TempDir()

	// Create apps directory (app exists but has no .forge.yaml)
	appsDir := filepath.Join(tmpDir, "apps")
	require.NoError(t, os.MkdirAll(filepath.Join(appsDir, "api-gateway"), 0755))

	// Create the global migrations directory
	migrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	// With app name, should create centralized subfolder
	path, err := plugin.getMigrationPathForApp("api-gateway")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(migrationsDir, "api-gateway"), path)

	// Directory should have been created
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestGetMigrationPathForApp_AppConfigOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Create app directory with .forge.yaml that overrides migration path
	appDir := filepath.Join(tmpDir, "apps", "api-gateway")
	require.NoError(t, os.MkdirAll(appDir, 0755))

	appConfig := `app:
  name: api-gateway
  type: web
database:
  migrations_path: ./db/migrations
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, ".forge.yaml"), []byte(appConfig), 0644))

	// Also create the global migrations dir
	globalMigrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(globalMigrationsDir, 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	// Should use the app's override path
	path, err := plugin.getMigrationPathForApp("api-gateway")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(appDir, "db", "migrations"), path)

	// Directory should have been created
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestGetMigrationPathForApp_FallbackToConventionWhenNoOverride(t *testing.T) {
	tmpDir := t.TempDir()

	// Create app directory with .forge.yaml WITHOUT database override
	appDir := filepath.Join(tmpDir, "apps", "worker")
	require.NoError(t, os.MkdirAll(appDir, 0755))

	appConfig := `app:
  name: worker
  type: worker
`
	require.NoError(t, os.WriteFile(filepath.Join(appDir, ".forge.yaml"), []byte(appConfig), 0644))

	// Create global migrations dir
	globalMigrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(globalMigrationsDir, 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	// No database.migrations_path in app config, should fall back to centralized
	path, err := plugin.getMigrationPathForApp("worker")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(globalMigrationsDir, "worker"), path)
}

func TestGetMigrationPathForApp_AppDirDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	// Don't create an apps directory at all -- only global migrations
	globalMigrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(globalMigrationsDir, 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	// App dir doesn't exist, should fall back to centralized convention
	path, err := plugin.getMigrationPathForApp("nonexistent-app")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(globalMigrationsDir, "nonexistent-app"), path)
}

// =============================================================================
// hasGoMigrationsForApp
// =============================================================================

func TestHasGoMigrationsForApp_NoGoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	migrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0755))

	// Create only SQL files
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "20240101120000_init.up.sql"), []byte("CREATE TABLE test;"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "20240101120000_init.down.sql"), []byte("DROP TABLE test;"), 0644))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
		},
	}

	hasGo, err := plugin.hasGoMigrationsForApp("")
	require.NoError(t, err)
	assert.False(t, hasGo)
}

func TestHasGoMigrationsForApp_WithGoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	migrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0755))

	// Create migrations.go (should be excluded) and an actual Go migration
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "migrations.go"), []byte("package migrations"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "20240101120000_init.go"), []byte("package migrations"), 0644))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
		},
	}

	hasGo, err := plugin.hasGoMigrationsForApp("")
	require.NoError(t, err)
	assert.True(t, hasGo)
}

func TestHasGoMigrationsForApp_OnlyMigrationsGo(t *testing.T) {
	tmpDir := t.TempDir()

	migrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0755))

	// Create only migrations.go (should be excluded)
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "migrations.go"), []byte("package migrations"), 0644))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
		},
	}

	hasGo, err := plugin.hasGoMigrationsForApp("")
	require.NoError(t, err)
	assert.False(t, hasGo)
}

func TestHasGoMigrationsForApp_AppScoped(t *testing.T) {
	tmpDir := t.TempDir()

	// Create global migrations dir and app-scoped subfolder
	globalDir := filepath.Join(tmpDir, "database", "migrations")
	appDir := filepath.Join(globalDir, "api-gateway")
	require.NoError(t, os.MkdirAll(appDir, 0755))

	// Put a Go migration in the app subfolder
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "20240101120000_create_routes.go"), []byte("package migrations"), 0644))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	// Global should not find Go migrations
	hasGo, err := plugin.hasGoMigrationsForApp("")
	require.NoError(t, err)
	assert.False(t, hasGo)

	// App-scoped should find Go migrations
	hasGo, err = plugin.hasGoMigrationsForApp("api-gateway")
	require.NoError(t, err)
	assert.True(t, hasGo)
}

// =============================================================================
// loadMigrationsForApp
// =============================================================================

func TestLoadMigrationsForApp_GlobalWithSQLFiles(t *testing.T) {
	tmpDir := t.TempDir()

	migrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0755))

	// Create valid SQL migration files
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "20240101120000_init.up.sql"), []byte("CREATE TABLE test (id INT);"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(migrationsDir, "20240101120000_init.down.sql"), []byte("DROP TABLE test;"), 0644))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
		},
	}

	migrations, err := plugin.loadMigrationsForApp("")
	require.NoError(t, err)
	assert.NotNil(t, migrations)
	assert.Len(t, migrations.Sorted(), 1)
}

func TestLoadMigrationsForApp_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	migrationsDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(migrationsDir, 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
		},
	}

	_, err := plugin.loadMigrationsForApp("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no migration files found")
}

func TestLoadMigrationsForApp_AppScopedWithSQLFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create global and app-scoped directories
	globalDir := filepath.Join(tmpDir, "database", "migrations")
	appDir := filepath.Join(globalDir, "api-gateway")
	require.NoError(t, os.MkdirAll(appDir, 0755))

	// Put migration in app-scoped dir
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "20240101120000_create_routes.up.sql"), []byte("CREATE TABLE routes (id INT);"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "20240101120000_create_routes.down.sql"), []byte("DROP TABLE routes;"), 0644))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	migrations, err := plugin.loadMigrationsForApp("api-gateway")
	require.NoError(t, err)
	assert.NotNil(t, migrations)
	assert.Len(t, migrations.Sorted(), 1)
}

func TestLoadMigrationsForApp_EmptyAppDir(t *testing.T) {
	tmpDir := t.TempDir()

	globalDir := filepath.Join(tmpDir, "database", "migrations")
	require.NoError(t, os.MkdirAll(globalDir, 0755))

	plugin := &DatabasePlugin{
		config: &config.ForgeConfig{
			RootDir: tmpDir,
			Database: config.DatabaseConfig{
				MigrationsPath: "./database/migrations",
			},
			Project: config.ProjectConfig{
				Layout: "single-module",
			},
		},
	}

	_, err := plugin.loadMigrationsForApp("api-gateway")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no migration files found")
	assert.Contains(t, err.Error(), "app: api-gateway")
	assert.Contains(t, err.Error(), "--app api-gateway")
}

// =============================================================================
// Table name consistency between functions
// =============================================================================

func TestTableNameConsistency(t *testing.T) {
	appNames := []string{"api-gateway", "auth-service", "worker", "MyApp.V2"}

	for _, app := range appNames {
		t.Run(app, func(t *testing.T) {
			migTable := migrationTableName(app)
			locksTable := migrationLocksTableName(app)

			// Both should contain the same sanitized suffix
			sanitized := sanitizeAppName(app)
			assert.Contains(t, migTable, sanitized)
			assert.Contains(t, locksTable, sanitized)

			// Migration table should start with "bun_migrations_"
			assert.Contains(t, migTable, "bun_migrations_")
			// Locks table should start with "bun_migration_locks_"
			assert.Contains(t, locksTable, "bun_migration_locks_")

			// Neither should contain uppercase
			assert.Equal(t, migTable, sanitizeForSQL(migTable))
			assert.Equal(t, locksTable, sanitizeForSQL(locksTable))
		})
	}
}

// sanitizeForSQL is a test helper that verifies a string is lowercase alphanumeric+underscore.
func sanitizeForSQL(s string) string {
	result := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			result = append(result, c)
		}
	}
	return string(result)
}
