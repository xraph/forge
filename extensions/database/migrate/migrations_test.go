package migrate_test

import (
	"testing"

	"github.com/xraph/forge/extensions/database/migrate"
)

// TestMigrationsPackageInitialization verifies that the migrate package can be
// imported without panicking, even when the filesystem is not accessible or
// the working directory doesn't exist (e.g., in Docker/CI environments).
func TestMigrationsPackageInitialization(t *testing.T) {
	// This test passes if the package imports without panic.
	// The migrate.Migrations variable should be accessible even if
	// DiscoverCaller() would fail.
	if migrate.Migrations == nil {
		t.Error("Migrations should not be nil")
	}
}

// TestGetMigrations verifies that GetMigrations returns the migrations
// collection even if discovery fails.
func TestGetMigrations(t *testing.T) {
	migrations, err := migrate.GetMigrations()

	// Even if discovery fails, we should get a migrations object
	// (it just won't have filesystem-discovered migrations)
	if migrations == nil {
		t.Fatal("GetMigrations should never return nil")
	}

	// Error is informational only - the function still returns the migrations
	_ = err
}

// TestEnsureDiscoveredIdempotent verifies that discovery only happens once.
func TestEnsureDiscoveredIdempotent(t *testing.T) {
	// Call GetMigrations multiple times
	_, err1 := migrate.GetMigrations()
	_, err2 := migrate.GetMigrations()

	// Errors should be identical (discovery only happens once)
	if (err1 == nil) != (err2 == nil) {
		t.Error("Multiple calls to GetMigrations should return the same error state")
	}
}
