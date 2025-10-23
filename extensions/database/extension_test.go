package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestExtension_Implementation(t *testing.T) {
	var _ forge.Extension = (*Extension)(nil)
}

func TestExtension_BasicInfo(t *testing.T) {
	ext := NewExtension(DefaultConfig())

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
	// Create test app
	app := forge.NewApp(forge.AppConfig{
		Name:    "test-app",
		Version: "1.0.0",
	})

	// Create extension with SQLite
	config := Config{
		Databases: []DatabaseConfig{
			{
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
			},
		},
	}

	ext := NewExtension(config)

	// Register extension
	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Start extension
	ctx := context.Background()
	if err := ext.Start(ctx); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	// Health check
	if err := ext.Health(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}

	// Get database manager
	dbManager := forge.Must[*DatabaseManager](app.Container(), "databaseManager")
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

	// Stop extension
	if err := ext.Stop(ctx); err != nil {
		t.Errorf("failed to stop extension: %v", err)
	}
}

func TestDatabaseManager_Register(t *testing.T) {
	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

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
	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

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

func TestSQLDatabase_Health(t *testing.T) {
	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

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
	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

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
