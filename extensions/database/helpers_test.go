package database

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
)

func TestHelpers_Manager(t *testing.T) {
	// Create test app with database extension
	app := forge.NewApp(forge.AppConfig{
		Name:    "test-helpers",
		Version: "1.0.0",
	})

	ext := NewExtension(
		WithDatabases(DatabaseConfig{
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

	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	t.Run("GetManager", func(t *testing.T) {
		manager, err := GetManager(app.Container())
		if err != nil {
			t.Fatalf("GetManager failed: %v", err)
		}
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("MustGetManager", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetManager panicked: %v", r)
			}
		}()

		manager := MustGetManager(app.Container())
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("GetManagerFromApp", func(t *testing.T) {
		manager, err := GetManagerFromApp(app)
		if err != nil {
			t.Fatalf("GetManagerFromApp failed: %v", err)
		}
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("MustGetManagerFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetManagerFromApp panicked: %v", r)
			}
		}()

		manager := MustGetManagerFromApp(app)
		if manager == nil {
			t.Fatal("manager is nil")
		}
	})

	t.Run("GetManagerFromApp_NilApp", func(t *testing.T) {
		_, err := GetManagerFromApp(nil)
		if err == nil {
			t.Fatal("expected error for nil app")
		}
	})

	t.Run("MustGetManagerFromApp_NilApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil app")
			}
		}()

		MustGetManagerFromApp(nil)
	})
}

func TestHelpers_Database(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:    "test-helpers-db",
		Version: "1.0.0",
	})

	ext := NewExtension(
		WithDatabases(DatabaseConfig{
			Name:                "default",
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
		WithDefault("default"),
	)

	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	t.Run("GetDatabase", func(t *testing.T) {
		db, err := GetDatabase(app.Container())
		if err != nil {
			t.Fatalf("GetDatabase failed: %v", err)
		}
		if db == nil {
			t.Fatal("database is nil")
		}
		if db.Name() != "default" {
			t.Errorf("expected name 'default', got %s", db.Name())
		}
	})

	t.Run("MustGetDatabase", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetDatabase panicked: %v", r)
			}
		}()

		db := MustGetDatabase(app.Container())
		if db == nil {
			t.Fatal("database is nil")
		}
	})

	t.Run("GetDatabaseFromApp", func(t *testing.T) {
		db, err := GetDatabaseFromApp(app)
		if err != nil {
			t.Fatalf("GetDatabaseFromApp failed: %v", err)
		}
		if db == nil {
			t.Fatal("database is nil")
		}
	})

	t.Run("MustGetDatabaseFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetDatabaseFromApp panicked: %v", r)
			}
		}()

		db := MustGetDatabaseFromApp(app)
		if db == nil {
			t.Fatal("database is nil")
		}
	})
}

func TestHelpers_SQL(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:    "test-helpers-sql",
		Version: "1.0.0",
	})

	ext := NewExtension(
		WithDatabases(DatabaseConfig{
			Name:                "default",
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
		WithDefault("default"),
	)

	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	t.Run("GetSQL", func(t *testing.T) {
		db, err := GetSQL(app.Container())
		if err != nil {
			t.Fatalf("GetSQL failed: %v", err)
		}
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})

	t.Run("MustGetSQL", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetSQL panicked: %v", r)
			}
		}()

		db := MustGetSQL(app.Container())
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})

	t.Run("GetSQLFromApp", func(t *testing.T) {
		db, err := GetSQLFromApp(app)
		if err != nil {
			t.Fatalf("GetSQLFromApp failed: %v", err)
		}
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})

	t.Run("MustGetSQLFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetSQLFromApp panicked: %v", r)
			}
		}()

		db := MustGetSQLFromApp(app)
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})
}

func TestHelpers_NamedDatabases(t *testing.T) {
	app := forge.NewApp(forge.AppConfig{
		Name:    "test-helpers-named",
		Version: "1.0.0",
	})

	ext := NewExtension(
		WithDatabases(
			DatabaseConfig{
				Name:                "primary",
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
			DatabaseConfig{
				Name:                "secondary",
				Type:                TypeSQLite,
				DSN:                 ":memory:",
				MaxOpenConns:        5,
				MaxIdleConns:        2,
				ConnMaxLifetime:     5 * time.Minute,
				ConnMaxIdleTime:     5 * time.Minute,
				MaxRetries:          3,
				RetryDelay:          time.Second,
				HealthCheckInterval: 30 * time.Second,
			},
		),
		WithDefault("primary"),
	)

	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	t.Run("GetNamedDatabase", func(t *testing.T) {
		db, err := GetNamedDatabase(app.Container(), "secondary")
		if err != nil {
			t.Fatalf("GetNamedDatabase failed: %v", err)
		}
		if db == nil {
			t.Fatal("database is nil")
		}
		if db.Name() != "secondary" {
			t.Errorf("expected name 'secondary', got %s", db.Name())
		}
	})

	t.Run("MustGetNamedDatabase", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetNamedDatabase panicked: %v", r)
			}
		}()

		db := MustGetNamedDatabase(app.Container(), "primary")
		if db == nil {
			t.Fatal("database is nil")
		}
		if db.Name() != "primary" {
			t.Errorf("expected name 'primary', got %s", db.Name())
		}
	})

	t.Run("GetNamedSQL", func(t *testing.T) {
		db, err := GetNamedSQL(app.Container(), "secondary")
		if err != nil {
			t.Fatalf("GetNamedSQL failed: %v", err)
		}
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})

	t.Run("MustGetNamedSQL", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetNamedSQL panicked: %v", r)
			}
		}()

		db := MustGetNamedSQL(app.Container(), "primary")
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})

	t.Run("GetNamedDatabaseFromApp", func(t *testing.T) {
		db, err := GetNamedDatabaseFromApp(app, "secondary")
		if err != nil {
			t.Fatalf("GetNamedDatabaseFromApp failed: %v", err)
		}
		if db == nil {
			t.Fatal("database is nil")
		}
	})

	t.Run("MustGetNamedDatabaseFromApp", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetNamedDatabaseFromApp panicked: %v", r)
			}
		}()

		db := MustGetNamedDatabaseFromApp(app, "primary")
		if db == nil {
			t.Fatal("database is nil")
		}
	})

	t.Run("GetNamedDatabase_NotFound", func(t *testing.T) {
		_, err := GetNamedDatabase(app.Container(), "nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent database")
		}
	})

	t.Run("MustGetNamedDatabase_NotFound", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for non-existent database")
			}
		}()

		MustGetNamedDatabase(app.Container(), "nonexistent")
	})
}

func TestHelpers_NotRegistered(t *testing.T) {
	// Create app without database extension
	app := forge.NewApp(forge.AppConfig{
		Name:    "test-not-registered",
		Version: "1.0.0",
	})

	t.Run("GetManager_NotRegistered", func(t *testing.T) {
		_, err := GetManager(app.Container())
		if err == nil {
			t.Fatal("expected error when database extension not registered")
		}
	})

	t.Run("MustGetManager_NotRegistered", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic when database extension not registered")
			}
		}()

		MustGetManager(app.Container())
	})

	t.Run("GetDatabase_NotRegistered", func(t *testing.T) {
		_, err := GetDatabase(app.Container())
		if err == nil {
			t.Fatal("expected error when database extension not registered")
		}
	})

	t.Run("GetSQL_NotRegistered", func(t *testing.T) {
		_, err := GetSQL(app.Container())
		if err == nil {
			t.Fatal("expected error when database extension not registered")
		}
	})
}

// BenchmarkHelpers tests the performance of helper functions
func BenchmarkHelpers(b *testing.B) {
	app := forge.NewApp(forge.AppConfig{
		Name:    "bench-helpers",
		Version: "1.0.0",
	})

	ext := NewExtension(
		WithDatabases(DatabaseConfig{
			Name:                "default",
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

	if err := ext.Register(app); err != nil {
		b.Fatalf("failed to register extension: %v", err)
	}

	if err := ext.Start(context.Background()); err != nil {
		b.Fatalf("failed to start extension: %v", err)
	}

	b.Run("GetManager", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = GetManager(app.Container())
		}
	})

	b.Run("MustGetManager", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = MustGetManager(app.Container())
		}
	})

	b.Run("GetDatabase", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = GetDatabase(app.Container())
		}
	})

	b.Run("MustGetDatabase", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = MustGetDatabase(app.Container())
		}
	})
}
