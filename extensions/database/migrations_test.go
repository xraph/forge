package database

import (
	"testing"

	"github.com/uptrace/bun/migrate"
)

func TestNewMigrationManager(t *testing.T) {
	migrations := migrate.NewMigrations()
	logger := NewNoopMigrationLogger()

	mgr := NewMigrationManager(nil, migrations, logger)
	if mgr == nil {
		t.Fatal("expected non-nil migration manager")
	}
	if mgr.db != nil {
		t.Error("expected nil db")
	}
	if mgr.migrations != migrations {
		t.Error("migrations not set correctly")
	}
	if mgr.logger != logger {
		t.Error("logger not set correctly")
	}
	if len(mgr.migratorOpts) != 0 {
		t.Errorf("expected 0 migrator opts, got %d", len(mgr.migratorOpts))
	}
}

func TestNewMigrationManagerWithOpts(t *testing.T) {
	migrations := migrate.NewMigrations()
	logger := NewNoopMigrationLogger()

	opts := []migrate.MigratorOption{
		migrate.WithTableName("bun_migrations_myapp"),
		migrate.WithLocksTableName("bun_migration_locks_myapp"),
	}

	mgr := NewMigrationManagerWithOpts(nil, migrations, logger, opts...)
	if mgr == nil {
		t.Fatal("expected non-nil migration manager")
	}
	if len(mgr.migratorOpts) != 2 {
		t.Errorf("expected 2 migrator opts, got %d", len(mgr.migratorOpts))
	}
}

func TestNewMigrationManagerWithOpts_NoOpts(t *testing.T) {
	migrations := migrate.NewMigrations()
	logger := NewNoopMigrationLogger()

	mgr := NewMigrationManagerWithOpts(nil, migrations, logger)
	if mgr == nil {
		t.Fatal("expected non-nil migration manager")
	}
	if len(mgr.migratorOpts) != 0 {
		t.Errorf("expected 0 migrator opts, got %d", len(mgr.migratorOpts))
	}
}

func TestNewMigrationManager_BackwardCompatibility(t *testing.T) {
	migrations := migrate.NewMigrations()
	logger := NewNoopMigrationLogger()

	// Old constructor should still work and produce a manager with no extra opts
	mgr := NewMigrationManager(nil, migrations, logger)

	// Verify the manager can create a migrator without panicking
	// (we pass nil db, which is fine for construction -- it only fails on actual queries)
	migrator := mgr.newMigrator()
	if migrator == nil {
		t.Fatal("expected non-nil migrator")
	}
}

func TestNewMigrationManagerWithOpts_MigratorCreation(t *testing.T) {
	migrations := migrate.NewMigrations()
	logger := NewNoopMigrationLogger()

	mgr := NewMigrationManagerWithOpts(nil, migrations, logger,
		migrate.WithTableName("bun_migrations_api_gateway"),
		migrate.WithLocksTableName("bun_migration_locks_api_gateway"),
	)

	// Verify the manager can create a migrator without panicking
	migrator := mgr.newMigrator()
	if migrator == nil {
		t.Fatal("expected non-nil migrator from WithOpts manager")
	}
}

func TestMigrate_NoMigrations(t *testing.T) {
	migrations := migrate.NewMigrations()
	logger := NewNoopMigrationLogger()

	// Manager with no registered migrations
	mgr := NewMigrationManager(nil, migrations, logger)

	err := mgr.Migrate(t.Context())
	if err == nil {
		t.Fatal("expected error for empty migrations")
	}
	if err.Error() != "no migrations registered" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMigrate_NoMigrations_WithOpts(t *testing.T) {
	migrations := migrate.NewMigrations()
	logger := NewNoopMigrationLogger()

	mgr := NewMigrationManagerWithOpts(nil, migrations, logger,
		migrate.WithTableName("bun_migrations_myapp"),
	)

	err := mgr.Migrate(t.Context())
	if err == nil {
		t.Fatal("expected error for empty migrations")
	}
	if err.Error() != "no migrations registered" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNoopMigrationLogger(t *testing.T) {
	logger := NewNoopMigrationLogger()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	// Should not panic
	logger.Debug("test")
	logger.Info("test")
	logger.Warn("test")
	logger.Error("test")
	logger.Debug("test", "key", "value")
	logger.Info("test", "key", "value")
}
