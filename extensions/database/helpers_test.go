package database

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func TestHelpers_Manager(t *testing.T) {
	// Create extension
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

	// Create test app with database extension
	app := forge.New(
		forge.WithAppName("test-helpers"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

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

	app := forge.New(
		forge.WithAppName("test-helpers-db"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

	t.Run("GetDatabase", func(t *testing.T) {
		db, err := GetDatabase(app.Container(), "default")
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

		db := MustGetDatabase(app.Container(), "default")
		if db == nil {
			t.Fatal("database is nil")
		}
	})

	t.Run("GetDefault", func(t *testing.T) {
		db, err := GetDefault(app.Container())
		if err != nil {
			t.Fatalf("GetDefault failed: %v", err)
		}

		if db == nil {
			t.Fatal("database is nil")
		}

		if db.Name() != "default" {
			t.Errorf("expected name 'default', got %s", db.Name())
		}
	})

	t.Run("MustGetDefault", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustGetDefault panicked: %v", r)
			}
		}()

		db := MustGetDefault(app.Container())
		if db == nil {
			t.Fatal("database is nil")
		}

		if db.Name() != "default" {
			t.Errorf("expected name 'default', got %s", db.Name())
		}
	})

	t.Run("GetDatabaseFromApp", func(t *testing.T) {
		db, err := GetDatabaseFromApp(app, "default")
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

		db := MustGetDatabaseFromApp(app, "default")
		if db == nil {
			t.Fatal("database is nil")
		}
	})
}

func TestHelpers_SQL(t *testing.T) {
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

	app := forge.New(
		forge.WithAppName("test-helpers-sql"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

	t.Run("GetSQL", func(t *testing.T) {
		db, err := GetSQL(app.Container(), "default")
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

		db := MustGetSQL(app.Container(), "default")
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})

	t.Run("GetSQLFromApp", func(t *testing.T) {
		db, err := GetSQLFromApp(app, "default")
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

		db := MustGetSQLFromApp(app, "default")
		if db == nil {
			t.Fatal("SQL database is nil")
		}
	})
}

func TestHelpers_NamedDatabases(t *testing.T) {
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

	app := forge.New(
		forge.WithAppName("test-helpers-named"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

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
	app := forge.New(
		forge.WithAppName("test-not-registered"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

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
		_, err := GetDatabase(app.Container(), "default")
		if err == nil {
			t.Fatal("expected error when database extension not registered")
		}
	})

	t.Run("GetSQL_NotRegistered", func(t *testing.T) {
		_, err := GetSQL(app.Container(), "default")
		if err == nil {
			t.Fatal("expected error when database extension not registered")
		}
	})
}

// BenchmarkHelpers tests the performance of helper functions.
func BenchmarkHelpers(b *testing.B) {
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

	app := forge.New(
		forge.WithAppName("bench-helpers"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(ext),
	)

	if err := app.Start(context.Background()); err != nil {
		b.Fatalf("failed to start app: %v", err)
	}
	defer app.Stop(context.Background())

	b.Run("GetManager", func(b *testing.B) {
		for range b.N {
			_, _ = GetManager(app.Container())
		}
	})

	b.Run("MustGetManager", func(b *testing.B) {
		for range b.N {
			_ = MustGetManager(app.Container())
		}
	})

	b.Run("GetDatabase", func(b *testing.B) {
		for range b.N {
			_, _ = GetDatabase(app.Container(), "default")
		}
	})

	b.Run("MustGetDatabase", func(b *testing.B) {
		for range b.N {
			_ = MustGetDatabase(app.Container(), "default")
		}
	})
}
