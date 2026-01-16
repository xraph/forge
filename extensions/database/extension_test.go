package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
	forgeTesting "github.com/xraph/forge/testing"
)

func TestExtension_Implementation(t *testing.T) {
	var _ forge.Extension = (*Extension)(nil)
}

func TestExtension_BasicInfo(t *testing.T) {
	ext := NewExtension()

	if ext.Name() != "database" {
		t.Errorf("expected name 'database', got %s", ext.Name())
	}

	if ext.Version() != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %s", ext.Version())
	}

	if ext.Description() == "" {
		t.Error("expected non-empty description")
	}

	deps := ext.Dependencies()
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(deps))
	}
}

func TestExtension_SQLiteLifecycle(t *testing.T) {
	// Create extension with SQLite using options
	ext := NewExtension(
		WithDatabase(DatabaseConfig{
			Name:                "test",
			Type:                TypeSQLite,
			DSN:                 ":memory:",
			MaxOpenConns:        10,
			MaxIdleConns:        5,
			ConnMaxLifetime:     5 * time.Minute,
			ConnMaxIdleTime:     5 * time.Minute,
			MaxRetries:          3,
			RetryDelay:          time.Second,
			HealthCheckInterval: 30 * time.Second,
		}),
	)

	// Create test app with the extension
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	// Start the app (this will call container.Start() which starts DatabaseManager)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	// Health check through extension
	if err := ext.Health(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}

	// Get database manager using constant
	dbManager := forge.Must[*DatabaseManager](app.Container(), ManagerKey)
	if dbManager == nil {
		t.Fatal("database manager not found")
	}

	// Get database
	db, err := dbManager.Get("test")
	if err != nil {
		t.Fatalf("failed to get database: %v", err)
	}

	if db.Name() != "test" {
		t.Errorf("expected database name 'test', got %s", db.Name())
	}

	if db.Type() != TypeSQLite {
		t.Errorf("expected database type SQLite, got %s", db.Type())
	}

	// Stop app (this will call container.Stop() which stops DatabaseManager)
	if err := app.Stop(ctx); err != nil {
		t.Errorf("failed to stop app: %v", err)
	}
}

func TestDatabaseManager_Register(t *testing.T) {
	log := forgeTesting.NewTestLogger()
	metrics := forgeTesting.NewMetrics()

	manager := NewDatabaseManager(log, metrics)

	// Create a test database
	config := DatabaseConfig{
		Name:         "test",
		Type:         TypeSQLite,
		DSN:          ":memory:",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}

	db, err := NewSQLDatabase(config, log, metrics)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	// Register database
	if err := manager.Register("test", db); err != nil {
		t.Fatalf("failed to register database: %v", err)
	}

	// Try to register again (should fail)
	if err := manager.Register("test", db); err == nil {
		t.Error("expected error when registering duplicate database")
	}

	// Get database
	retrieved, err := manager.Get("test")
	if err != nil {
		t.Fatalf("failed to get database: %v", err)
	}

	if retrieved.Name() != "test" {
		t.Errorf("expected name 'test', got %s", retrieved.Name())
	}

	// Get non-existent database
	_, err = manager.Get("nonexistent")
	if err == nil {
		t.Error("expected error when getting non-existent database")
	}
}

func TestDatabaseManager_List(t *testing.T) {
	log := forgeTesting.NewTestLogger()
	metrics := forgeTesting.NewMetrics()

	manager := NewDatabaseManager(log, metrics)

	// Initially empty
	if len(manager.List()) != 0 {
		t.Error("expected empty list initially")
	}

	// Add databases
	for i := 1; i <= 3; i++ {
		config := DatabaseConfig{
			Name:         fmt.Sprintf("db%d", i),
			Type:         TypeSQLite,
			DSN:          ":memory:",
			MaxOpenConns: 10,
		}

		db, _ := NewSQLDatabase(config, log, metrics)
		manager.Register(fmt.Sprintf("db%d", i), db)
	}

	// Check list
	list := manager.List()
	if len(list) != 3 {
		t.Errorf("expected 3 databases, got %d", len(list))
	}
}

func TestDatabaseManager_Default(t *testing.T) {
	log := forgeTesting.NewTestLogger()
	metrics := forgeTesting.NewMetrics()

	manager := NewDatabaseManager(log, metrics)

	// Test getting default when none is set
	_, err := manager.Default()
	if err == nil {
		t.Error("expected error when getting default database with none set")
	}

	// Test DefaultName when none is set
	if name := manager.DefaultName(); name != "" {
		t.Errorf("expected empty default name, got %s", name)
	}

	// Create and register databases
	config1 := DatabaseConfig{
		Name:         "primary",
		Type:         TypeSQLite,
		DSN:          ":memory:",
		MaxOpenConns: 10,
	}
	db1, err := NewSQLDatabase(config1, log, metrics)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	manager.Register("primary", db1)

	config2 := DatabaseConfig{
		Name:         "secondary",
		Type:         TypeSQLite,
		DSN:          ":memory:",
		MaxOpenConns: 10,
	}
	db2, err := NewSQLDatabase(config2, log, metrics)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	manager.Register("secondary", db2)

	// Set default to non-existent database (should fail)
	if err := manager.SetDefault("nonexistent"); err == nil {
		t.Error("expected error when setting non-existent database as default")
	}

	// Set default to existing database
	if err := manager.SetDefault("primary"); err != nil {
		t.Fatalf("failed to set default database: %v", err)
	}

	// Test DefaultName
	if name := manager.DefaultName(); name != "primary" {
		t.Errorf("expected default name 'primary', got %s", name)
	}

	// Get default database
	defaultDB, err := manager.Default()
	if err != nil {
		t.Fatalf("failed to get default database: %v", err)
	}

	if defaultDB.Name() != "primary" {
		t.Errorf("expected default database name 'primary', got %s", defaultDB.Name())
	}

	// Change default
	if err := manager.SetDefault("secondary"); err != nil {
		t.Fatalf("failed to change default database: %v", err)
	}

	// Verify new default
	defaultDB, err = manager.Default()
	if err != nil {
		t.Fatalf("failed to get default database: %v", err)
	}

	if defaultDB.Name() != "secondary" {
		t.Errorf("expected default database name 'secondary', got %s", defaultDB.Name())
	}
}

func TestSQLDatabase_Health(t *testing.T) {
	log := forgeTesting.NewTestLogger()
	metrics := forgeTesting.NewMetrics()

	config := DatabaseConfig{
		Name:         "test",
		Type:         TypeSQLite,
		DSN:          ":memory:",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}

	db, err := NewSQLDatabase(config, log, metrics)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	ctx := context.Background()

	// Open database
	if err := db.Open(ctx); err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close(ctx)

	// Check health
	status := db.Health(ctx)
	if !status.Healthy {
		t.Errorf("expected healthy database, got: %s", status.Message)
	}

	if status.Latency == 0 {
		t.Error("expected non-zero latency")
	}

	if status.CheckedAt.IsZero() {
		t.Error("expected non-zero checked at time")
	}
}

func TestSQLDatabase_Stats(t *testing.T) {
	log := forgeTesting.NewTestLogger()
	metrics := forgeTesting.NewMetrics()

	config := DatabaseConfig{
		Name:         "test",
		Type:         TypeSQLite,
		DSN:          ":memory:",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}

	db, err := NewSQLDatabase(config, log, metrics)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	ctx := context.Background()

	// Open database
	if err := db.Open(ctx); err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close(ctx)

	// Get stats
	stats := db.Stats()

	// Stats should be non-negative
	if stats.OpenConnections < 0 {
		t.Error("open connections should be non-negative")
	}

	if stats.InUse < 0 {
		t.Error("in use connections should be non-negative")
	}

	if stats.Idle < 0 {
		t.Error("idle connections should be non-negative")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "empty config",
			config:  Config{},
			wantErr: true,
		},
		{
			name: "valid config",
			config: Config{
				Databases: []DatabaseConfig{
					{
						Name:         "test",
						Type:         TypeSQLite,
						DSN:          ":memory:",
						MaxOpenConns: 10,
						MaxIdleConns: 5,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing database name",
			config: Config{
				Databases: []DatabaseConfig{
					{
						Type:         TypeSQLite,
						DSN:          ":memory:",
						MaxOpenConns: 10,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing database type",
			config: Config{
				Databases: []DatabaseConfig{
					{
						Name:         "test",
						DSN:          ":memory:",
						MaxOpenConns: 10,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing DSN",
			config: Config{
				Databases: []DatabaseConfig{
					{
						Name:         "test",
						Type:         TypeSQLite,
						MaxOpenConns: 10,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid pool config - negative max open",
			config: Config{
				Databases: []DatabaseConfig{
					{
						Name:         "test",
						Type:         TypeSQLite,
						DSN:          ":memory:",
						MaxOpenConns: -1,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid pool config - idle > open",
			config: Config{
				Databases: []DatabaseConfig{
					{
						Name:         "test",
						Type:         TypeSQLite,
						DSN:          ":memory:",
						MaxOpenConns: 5,
						MaxIdleConns: 10,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if len(config.Databases) == 0 {
		t.Error("default config should have at least one database")
	}

	if config.Databases[0].Type != TypeSQLite {
		t.Error("default config should use SQLite")
	}

	// Should be valid
	if err := config.Validate(); err != nil {
		t.Errorf("default config should be valid: %v", err)
	}
}
