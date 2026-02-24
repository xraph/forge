package forge

import "context"

// MigrationInfo describes a single migration for display and status purposes.
// It is database-agnostic — extensions convert their internal migration types
// to this format for CLI display and programmatic inspection.
type MigrationInfo struct {
	// Name is a human-readable identifier (e.g., "create_users").
	Name string

	// Version is a timestamp-based version string (e.g., "20240115120000").
	Version string

	// Group identifies the module/extension that owns this migration.
	Group string

	// Comment is an optional description of what this migration does.
	Comment string

	// Applied indicates whether this migration has been applied.
	Applied bool

	// AppliedAt is an ISO 8601 timestamp of when the migration was applied.
	// Empty if not yet applied.
	AppliedAt string
}

// MigrationGroupInfo describes the status of all migrations in a group.
type MigrationGroupInfo struct {
	// Name is the group identifier (e.g., "core", "billing").
	Name string

	// Applied lists migrations that have already been applied.
	Applied []*MigrationInfo

	// Pending lists migrations that have not yet been applied.
	Pending []*MigrationInfo
}

// MigrationResult describes the outcome of a Migrate or Rollback operation.
type MigrationResult struct {
	// Applied is the count of newly applied migrations (for Migrate).
	Applied int

	// RolledBack is the count of rolled-back migrations (for Rollback).
	RolledBack int

	// Names lists the affected migration identifiers (group/name format).
	Names []string
}

// MigratableExtension is an optional interface for extensions that provide
// database migrations. Extensions implementing this interface will have their
// migrations auto-discovered by the CLI wrapper and can be run via lifecycle
// hooks or CLI commands.
//
// The interface is database-agnostic — implementations wrap their concrete
// migration systems (grove/migrate, goose, golang-migrate, etc.).
//
// Migrations are discovered from all registered extensions that implement
// this interface. The CLI wrapper provides `migrate up`, `migrate down`,
// and `migrate status` commands automatically.
//
// Example implementation:
//
//	func (e *MyDBExtension) Migrate(ctx context.Context) (*forge.MigrationResult, error) {
//	    result, err := e.orchestrator.Migrate(ctx)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return &forge.MigrationResult{Applied: len(result.Applied)}, nil
//	}
//
//	func (e *MyDBExtension) Rollback(ctx context.Context) (*forge.MigrationResult, error) {
//	    result, err := e.orchestrator.Rollback(ctx)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return &forge.MigrationResult{RolledBack: len(result.Rollback)}, nil
//	}
//
//	func (e *MyDBExtension) MigrationStatus(ctx context.Context) ([]*forge.MigrationGroupInfo, error) {
//	    // ... convert internal status to forge types ...
//	}
type MigratableExtension interface {
	Extension

	// Migrate runs all pending migrations forward.
	// Returns the result describing which migrations were applied.
	Migrate(ctx context.Context) (*MigrationResult, error)

	// Rollback rolls back the last batch of applied migrations.
	// Returns the result describing which migrations were rolled back.
	Rollback(ctx context.Context) (*MigrationResult, error)

	// MigrationStatus returns the current state of all migrations
	// grouped by their owning module/extension.
	MigrationStatus(ctx context.Context) ([]*MigrationGroupInfo, error)
}
