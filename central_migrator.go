package forge

import "context"

// CentralMigrator runs all extension migrations as one ordered set per database.
// The grove MigrationRegistry implements it; it is resolved from the DI container.
type CentralMigrator interface {
	RunAll(ctx context.Context) (*MigrationResult, error)
	RollbackAll(ctx context.Context) (*MigrationResult, error)
	StatusAll(ctx context.Context) ([]*MigrationGroupInfo, error)
}
