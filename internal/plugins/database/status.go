package database

import (
	"context"

	"github.com/xraph/forge/internal/services"
)

// GetMigrationStatus returns detailed migration status
func GetMigrationStatus(ctx context.Context, migrationService services.MigrationService) (*DatabaseStatus, error) {
	// Get all migrations
	all, err := migrationService.GetAllMigrations(ctx)
	if err != nil {
		return nil, err
	}

	// Get applied migrations
	applied, err := migrationService.GetAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	// Get pending migrations
	pending, err := migrationService.GetPendingMigrations(ctx)
	if err != nil {
		return nil, err
	}

	status := &DatabaseStatus{
		Migrations: make([]MigrationStatus, 0, len(all)),
		Applied:    len(applied),
		Pending:    len(pending),
	}

	// Build migration status list
	appliedMap := make(map[string]services.Migration)
	for _, m := range applied {
		appliedMap[m.Name] = m
	}

	for _, migration := range all {
		migrationStatus := MigrationStatus{
			Name:    migration.Name,
			Version: migration.Version,
			Applied: false,
		}

		if appliedMigration, exists := appliedMap[migration.Name]; exists {
			migrationStatus.Applied = true
			migrationStatus.AppliedAt = appliedMigration.AppliedAt
			migrationStatus.Checksum = appliedMigration.Checksum
		}

		status.Migrations = append(status.Migrations, migrationStatus)
	}

	// Set last migration
	if len(applied) > 0 {
		lastMigration := applied[len(applied)-1]
		status.LastMigration = &MigrationStatus{
			Name:      lastMigration.Name,
			Applied:   true,
			AppliedAt: lastMigration.AppliedAt,
			Version:   lastMigration.Version,
			Checksum:  lastMigration.Checksum,
		}
	}

	return status, nil
}
