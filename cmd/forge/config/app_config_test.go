package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppDatabaseConfig_GetMigrationsPath(t *testing.T) {
	t.Run("returns configured path", func(t *testing.T) {
		cfg := AppDatabaseConfig{MigrationsPath: "./db/migrations"}
		assert.Equal(t, "./db/migrations", cfg.GetMigrationsPath())
	})

	t.Run("returns empty when not set", func(t *testing.T) {
		cfg := AppDatabaseConfig{}
		assert.Equal(t, "", cfg.GetMigrationsPath())
	})
}

func TestAppDatabaseConfig_GetSeedsPath(t *testing.T) {
	t.Run("returns configured path", func(t *testing.T) {
		cfg := AppDatabaseConfig{SeedsPath: "./db/seeds"}
		assert.Equal(t, "./db/seeds", cfg.GetSeedsPath())
	})

	t.Run("returns empty when not set", func(t *testing.T) {
		cfg := AppDatabaseConfig{}
		assert.Equal(t, "", cfg.GetSeedsPath())
	})
}

func TestLoadAppConfig_WithDatabaseSection(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `app:
  name: api-gateway
  type: web
  version: "1.0.0"
database:
  migrations_path: ./custom/migrations
  seeds_path: ./custom/seeds
  connection: production
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".forge.yaml"), []byte(configContent), 0644))

	cfg, err := LoadAppConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "api-gateway", cfg.App.Name)
	assert.Equal(t, "web", cfg.App.Type)
	assert.Equal(t, "./custom/migrations", cfg.Database.MigrationsPath)
	assert.Equal(t, "./custom/seeds", cfg.Database.SeedsPath)
	assert.Equal(t, "production", cfg.Database.Connection)
}

func TestLoadAppConfig_WithoutDatabaseSection(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `app:
  name: worker
  type: worker
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".forge.yaml"), []byte(configContent), 0644))

	cfg, err := LoadAppConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "worker", cfg.App.Name)
	// Database config should be zero-valued
	assert.Equal(t, "", cfg.Database.MigrationsPath)
	assert.Equal(t, "", cfg.Database.SeedsPath)
	assert.Equal(t, "", cfg.Database.Connection)
}

func TestLoadAppConfig_YmlExtension(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `app:
  name: my-service
  type: web
database:
  migrations_path: ./migrations
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".forge.yml"), []byte(configContent), 0644))

	cfg, err := LoadAppConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "my-service", cfg.App.Name)
	assert.Equal(t, "./migrations", cfg.Database.MigrationsPath)
}

func TestLoadAppConfig_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadAppConfig(tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no .forge.yaml or .forge.yml found")
}

func TestLoadAppConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".forge.yaml"), []byte("invalid: yaml: content: ["), 0644))

	_, err := LoadAppConfig(tmpDir)
	assert.Error(t, err)
}

func TestLoadAppConfig_InternalFieldsSet(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `app:
  name: test-app
`
	configPath := filepath.Join(tmpDir, ".forge.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cfg, err := LoadAppConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, tmpDir, cfg.AppDir)
	assert.Equal(t, configPath, cfg.ConfigPath)
}

func TestLoadAppConfig_DatabaseMigrationsPathOnly(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `app:
  name: api
database:
  migrations_path: ./db/migrate
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".forge.yaml"), []byte(configContent), 0644))

	cfg, err := LoadAppConfig(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "./db/migrate", cfg.Database.GetMigrationsPath())
	assert.Equal(t, "", cfg.Database.GetSeedsPath())
	assert.Equal(t, "", cfg.Database.Connection)
}
