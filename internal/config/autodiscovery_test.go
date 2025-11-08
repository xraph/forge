package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xraph/forge/internal/logger"
)

// TestDiscoverAndLoadConfigs_SingleApp tests single-app config discovery.
func TestDiscoverAndLoadConfigs_SingleApp(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "forge-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create base config
	baseConfig := `
app:
  name: test-app
  port: 8080
database:
  host: prod.example.com
  name: prod_db
logging:
  level: info
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(baseConfig), 0644); err != nil {
		t.Fatalf("Failed to write base config: %v", err)
	}

	// Create local config
	localConfig := `
database:
  host: localhost
  name: dev_db
logging:
  level: debug
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.local.yaml"), []byte(localConfig), 0644); err != nil {
		t.Fatalf("Failed to write local config: %v", err)
	}

	// Test discovery
	cfg := DefaultAutoDiscoveryConfig()
	cfg.SearchPaths = []string{tmpDir}
	cfg.AppName = "test-app"
	cfg.Logger = logger.NewNoopLogger()

	manager, result, err := DiscoverAndLoadConfigs(cfg)
	if err != nil {
		t.Fatalf("Failed to discover configs: %v", err)
	}

	// Verify discovery results
	if result.BaseConfigPath == "" {
		t.Error("Base config path should be found")
	}

	if result.LocalConfigPath == "" {
		t.Error("Local config path should be found")
	}

	if result.WorkingDirectory != tmpDir {
		t.Errorf("Working directory should be %s, got %s", tmpDir, result.WorkingDirectory)
	}

	// Verify config values - local should override base
	if host := manager.GetString("database.host"); host != "localhost" {
		t.Errorf("Expected database.host to be 'localhost' (from local), got '%s'", host)
	}

	if name := manager.GetString("database.name"); name != "dev_db" {
		t.Errorf("Expected database.name to be 'dev_db' (from local), got '%s'", name)
	}

	if level := manager.GetString("logging.level"); level != "debug" {
		t.Errorf("Expected logging.level to be 'debug' (from local), got '%s'", level)
	}

	// Verify base-only values are still present
	if port := manager.GetInt("app.port"); port != 8080 {
		t.Errorf("Expected app.port to be 8080 (from base), got %d", port)
	}
}

// TestDiscoverAndLoadConfigs_Monorepo tests monorepo config discovery with app scoping.
func TestDiscoverAndLoadConfigs_Monorepo(t *testing.T) {
	// Create temp monorepo structure
	tmpDir, err := os.MkdirTemp("", "forge-monorepo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create apps directory to simulate monorepo
	appsDir := filepath.Join(tmpDir, "apps", "test-service")
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		t.Fatalf("Failed to create apps dir: %v", err)
	}

	// Create root config with app-scoped sections
	rootConfig := `
# Global settings
database:
  driver: postgres
  host: prod.example.com
  port: 5432

logging:
  level: info

# App-scoped settings
apps:
  test-service:
    app:
      name: test-service
      port: 8080
    database:
      name: test_service_db
      user: test_user
    service:
      workers: 10
      timeout: 30s
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(rootConfig), 0644); err != nil {
		t.Fatalf("Failed to write root config: %v", err)
	}

	// Create local overrides
	localConfig := `
database:
  host: localhost

apps:
  test-service:
    database:
      name: test_service_dev
    logging:
      level: debug
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.local.yaml"), []byte(localConfig), 0644); err != nil {
		t.Fatalf("Failed to write local config: %v", err)
	}

	// Test discovery from app directory
	cfg := DefaultAutoDiscoveryConfig()
	cfg.SearchPaths = []string{appsDir}
	cfg.AppName = "test-service"
	cfg.EnableAppScoping = true
	cfg.Logger = logger.NewNoopLogger()

	manager, result, err := DiscoverAndLoadConfigs(cfg)
	if err != nil {
		t.Fatalf("Failed to discover configs: %v", err)
	}

	// Verify discovery results
	if result.BaseConfigPath == "" {
		t.Error("Base config path should be found")
	}

	if !result.IsMonorepo {
		t.Error("Should detect monorepo layout")
	}

	// Verify app-scoped config is extracted and merged
	// 1. Global settings should be present
	if driver := manager.GetString("database.driver"); driver != "postgres" {
		t.Errorf("Expected global database.driver to be 'postgres', got '%s'", driver)
	}

	// 2. App-scoped settings should override globals
	if name := manager.GetString("database.name"); name != "test_service_dev" {
		t.Errorf("Expected database.name to be 'test_service_dev' (app-scoped + local), got '%s'", name)
	}

	if user := manager.GetString("database.user"); user != "test_user" {
		t.Errorf("Expected database.user to be 'test_user' (app-scoped), got '%s'", user)
	}

	// 3. Local overrides should take precedence
	if host := manager.GetString("database.host"); host != "localhost" {
		t.Errorf("Expected database.host to be 'localhost' (local override), got '%s'", host)
	}

	// 4. App-specific settings should be present
	if workers := manager.GetInt("service.workers"); workers != 10 {
		t.Errorf("Expected service.workers to be 10, got %d", workers)
	}

	// 5. Logging level should be from local app override
	if level := manager.GetString("logging.level"); level != "debug" {
		t.Errorf("Expected logging.level to be 'debug' (local app override), got '%s'", level)
	}
}

// TestDiscoverAndLoadConfigs_NoConfigFiles tests behavior when no config files exist.
func TestDiscoverAndLoadConfigs_NoConfigFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-no-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultAutoDiscoveryConfig()
	cfg.SearchPaths = []string{tmpDir}
	cfg.RequireBase = false
	cfg.RequireLocal = false
	cfg.Logger = logger.NewNoopLogger()

	manager, result, err := DiscoverAndLoadConfigs(cfg)
	if err != nil {
		t.Fatalf("Should not error when config is optional: %v", err)
	}

	if result.BaseConfigPath != "" {
		t.Error("Base config path should be empty")
	}

	if result.LocalConfigPath != "" {
		t.Error("Local config path should be empty")
	}

	// Manager should still work with empty config
	if manager == nil {
		t.Fatal("Manager should not be nil")
	}

	if val := manager.GetString("nonexistent.key"); val != "" {
		t.Error("Non-existent key should return empty string")
	}
}

// TestDiscoverAndLoadConfigs_RequiredConfig tests behavior when config is required.
func TestDiscoverAndLoadConfigs_RequiredConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-required-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultAutoDiscoveryConfig()
	cfg.SearchPaths = []string{tmpDir}
	cfg.RequireBase = true
	cfg.Logger = logger.NewNoopLogger()

	_, _, err = DiscoverAndLoadConfigs(cfg)
	if err == nil {
		t.Error("Should error when required config is not found")
	}
}

// TestDiscoverAndLoadConfigs_HierarchicalSearch tests parent directory search.
func TestDiscoverAndLoadConfigs_HierarchicalSearch(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "forge-hierarchical-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested directory structure
	deepDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("Failed to create deep dir: %v", err)
	}

	// Place config in root
	config := `
app:
  name: test-app
  found: true
`
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Search from deep directory - should find config in parent
	cfg := DefaultAutoDiscoveryConfig()
	cfg.SearchPaths = []string{deepDir}
	cfg.Logger = logger.NewNoopLogger()

	manager, result, err := DiscoverAndLoadConfigs(cfg)
	if err != nil {
		t.Fatalf("Failed to discover config: %v", err)
	}

	if result.BaseConfigPath == "" {
		t.Error("Should find config in parent directory")
	}

	if !filepath.HasPrefix(result.BaseConfigPath, tmpDir) {
		t.Error("Config should be found in root temp dir")
	}

	// Verify config was loaded
	if found := manager.GetBool("app.found"); !found {
		t.Error("Config value should be loaded from parent directory")
	}
}

// TestAutoLoadConfigManager tests the convenience function.
func TestAutoLoadConfigManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-auto-load-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Save current directory and change to temp dir
	oldDir, _ := os.Getwd()
	defer os.Chdir(oldDir)

	os.Chdir(tmpDir)

	// Create config
	config := `
app:
  name: auto-test
database:
  host: example.com
`
	if err := os.WriteFile("config.yaml", []byte(config), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Test AutoLoadConfigManager
	manager, err := AutoLoadConfigManager("auto-test", logger.NewNoopLogger())
	if err != nil {
		t.Fatalf("AutoLoadConfigManager failed: %v", err)
	}

	if host := manager.GetString("database.host"); host != "example.com" {
		t.Errorf("Expected database.host to be 'example.com', got '%s'", host)
	}
}

// TestLoadConfigFromPaths tests explicit path loading.
func TestLoadConfigFromPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-explicit-paths-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	basePath := filepath.Join(tmpDir, "base.yaml")
	localPath := filepath.Join(tmpDir, "local.yaml")

	// Create configs
	baseConfig := `
app:
  name: explicit-test
database:
  host: prod.example.com
`
	localConfig := `
database:
  host: localhost
`

	if err := os.WriteFile(basePath, []byte(baseConfig), 0644); err != nil {
		t.Fatalf("Failed to write base config: %v", err)
	}

	if err := os.WriteFile(localPath, []byte(localConfig), 0644); err != nil {
		t.Fatalf("Failed to write local config: %v", err)
	}

	// Test explicit path loading
	manager, err := LoadConfigFromPaths(basePath, localPath, "", logger.NewNoopLogger())
	if err != nil {
		t.Fatalf("LoadConfigFromPaths failed: %v", err)
	}

	// Verify local overrides base
	if host := manager.GetString("database.host"); host != "localhost" {
		t.Errorf("Expected database.host to be 'localhost' (local override), got '%s'", host)
	}

	if name := manager.GetString("app.name"); name != "explicit-test" {
		t.Errorf("Expected app.name to be 'explicit-test' (from base), got '%s'", name)
	}
}

// TestExtractAppScopedConfig tests app scoping extraction.
func TestExtractAppScopedConfig(t *testing.T) {
	// Create manager with app-scoped config
	manager := NewManager(ManagerConfig{
		Logger: logger.NewNoopLogger(),
	})

	// Set config with apps section
	manager.Set("global.setting", "global-value")
	manager.Set("apps.myapp.app.name", "myapp")
	manager.Set("apps.myapp.database.host", "app-db.example.com")
	manager.Set("apps.myapp.custom.value", "app-specific")

	// Extract app-scoped config
	err := extractAppScopedConfig(manager, "myapp")
	if err != nil {
		t.Fatalf("Failed to extract app-scoped config: %v", err)
	}

	// Verify global settings are preserved
	if val := manager.GetString("global.setting"); val != "global-value" {
		t.Errorf("Expected global.setting to be preserved, got '%s'", val)
	}

	// Verify app-scoped settings are promoted to root
	if name := manager.GetString("app.name"); name != "myapp" {
		t.Errorf("Expected app.name to be 'myapp', got '%s'", name)
	}

	if host := manager.GetString("database.host"); host != "app-db.example.com" {
		t.Errorf("Expected database.host to be 'app-db.example.com', got '%s'", host)
	}

	if custom := manager.GetString("custom.value"); custom != "app-specific" {
		t.Errorf("Expected custom.value to be 'app-specific', got '%s'", custom)
	}

	// Verify apps section is removed
	if appsSection := manager.GetSection("apps"); appsSection != nil {
		t.Error("apps section should be removed after extraction")
	}
}

// TestConfigMergePrecedence tests the complete merge precedence order.
func TestConfigMergePrecedence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "forge-precedence-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create app directory
	appDir := filepath.Join(tmpDir, "apps", "testapp")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("Failed to create app dir: %v", err)
	}

	// Config with all levels
	rootConfig := `
# Global level (lowest precedence)
setting1: global
setting2: global
setting3: global
setting4: global

apps:
  testapp:
    # App-scoped level
    setting2: app-scoped
    setting3: app-scoped
    setting4: app-scoped
`
	localConfig := `
# Local override level
setting3: local-global
setting4: local-global

apps:
  testapp:
    # Local app-scoped level (highest precedence)
    setting4: local-app-scoped
`

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(rootConfig), 0644); err != nil {
		t.Fatalf("Failed to write root config: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "config.local.yaml"), []byte(localConfig), 0644); err != nil {
		t.Fatalf("Failed to write local config: %v", err)
	}

	// Load config
	cfg := DefaultAutoDiscoveryConfig()
	cfg.SearchPaths = []string{appDir}
	cfg.AppName = "testapp"
	cfg.EnableAppScoping = true
	cfg.Logger = logger.NewNoopLogger()

	manager, _, err := DiscoverAndLoadConfigs(cfg)
	if err != nil {
		t.Fatalf("Failed to load configs: %v", err)
	}

	// Verify precedence order:
	// setting1: only in global -> global
	// setting2: global + app-scoped -> app-scoped
	// setting3: global + app-scoped + local-global -> local-global
	// setting4: all levels -> local-app-scoped (highest)

	tests := []struct {
		key      string
		expected string
		desc     string
	}{
		{"setting1", "global", "global only"},
		{"setting2", "app-scoped", "app-scoped overrides global"},
		{"setting3", "local-global", "local overrides app-scoped"},
		{"setting4", "local-app-scoped", "local app-scoped overrides all"},
	}

	for _, tt := range tests {
		if val := manager.GetString(tt.key); val != tt.expected {
			t.Errorf("%s: expected '%s', got '%s'", tt.desc, tt.expected, val)
		}
	}
}

// Benchmark tests.
func BenchmarkDiscoverAndLoadConfigs(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "forge-bench-*")
	defer os.RemoveAll(tmpDir)

	config := `
app:
  name: bench-app
database:
  host: example.com
`
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(config), 0644)

	cfg := DefaultAutoDiscoveryConfig()
	cfg.SearchPaths = []string{tmpDir}
	cfg.Logger = logger.NewNoopLogger()

	for b.Loop() {
		_, _, _ = DiscoverAndLoadConfigs(cfg)
	}
}
