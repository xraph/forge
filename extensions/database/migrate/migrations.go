// Package migrate provides migration management for the database extension
package migrate

import (
	"context"
	"sync"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// Migrations is the global migration collection
// All migrations should register themselves here using init().
var Migrations = migrate.NewMigrations(
	migrate.WithMigrationsDirectory("./migrations"),
)

// Models is the list of all models that should be auto-registered
// Add your models here for automatic table creation in development.
var Models = []any{}

var (
	discoveryOnce sync.Once
	discoveryErr  error
)

// ensureDiscovered ensures DiscoverCaller has been called exactly once.
// This is called lazily when migrations are actually used, not at package init time.
func ensureDiscovered() error {
	discoveryOnce.Do(func() {
		// DiscoverCaller may fail in environments where the filesystem isn't accessible
		// or the working directory isn't what's expected (e.g., Docker, CI).
		// We ignore the error here to allow the application to start.
		// If migrations are actually needed, they can be registered programmatically.
		discoveryErr = Migrations.DiscoverCaller()
	})

	return discoveryErr
}

// RegisterModel adds a model to the auto-registration list.
func RegisterModel(model any) {
	Models = append(Models, model)
}

// RegisterMigration is a helper to register a migration.
func RegisterMigration(up, down func(ctx context.Context, db *bun.DB) error) {
	Migrations.MustRegister(up, down)
}

// GetMigrations returns the migrations collection after ensuring discovery has been attempted.
// This should be called by code that actually uses migrations to ensure discovery happens.
func GetMigrations() (*migrate.Migrations, error) {
	if err := ensureDiscovered(); err != nil {
		// Log but don't fail - migrations may be registered programmatically
		// Return the migrations anyway as they may have been registered directly
		return Migrations, nil
	}

	return Migrations, nil
}
