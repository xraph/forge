// Package migrate provides migration management for the database extension
package migrate

import (
	"context"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// Migrations is the global migration collection
// All migrations should register themselves here using init()
var Migrations = migrate.NewMigrations()

// Models is the list of all models that should be auto-registered
// Add your models here for automatic table creation in development
var Models = []interface{}{}

// RegisterModel adds a model to the auto-registration list
func RegisterModel(model interface{}) {
	Models = append(Models, model)
}

// RegisterMigration is a helper to register a migration
func RegisterMigration(up, down func(ctx context.Context, db *bun.DB) error) {
	Migrations.MustRegister(up, down)
}

