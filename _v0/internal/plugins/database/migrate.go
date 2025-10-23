package database

import (
	"time"

	"github.com/xraph/forge/v0/internal/services"
)

// MigrationResult represents the result of a migration operation
type MigrationResult struct {
	Applied    int                        `json:"applied"`
	RolledBack int                        `json:"rolled_back"`
	Migrations []services.MigrationStatus `json:"migrations"`
	Duration   time.Duration              `json:"duration"`
	Errors     []string                   `json:"errors,omitempty"`
}

// MigrationStatus represents the status of a single migration
type MigrationStatus struct {
	Name      string     `json:"name"`
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
	Version   string     `json:"version"`
	Checksum  string     `json:"checksum"`
}

// DatabaseStatus represents the overall database status
type DatabaseStatus struct {
	Migrations      []MigrationStatus `json:"migrations"`
	Applied         int               `json:"applied"`
	Pending         int               `json:"pending"`
	LastMigration   *MigrationStatus  `json:"last_migration,omitempty"`
	DatabaseVersion string            `json:"database_version"`
	SchemaVersion   string            `json:"schema_version"`
}

// MigrationConfig contains configuration for migration operations
type MigrationConfig struct {
	Direction  string                                     `json:"direction"` // "up" or "down"
	Count      int                                        `json:"count"`
	DryRun     bool                                       `json:"dry_run"`
	Force      bool                                       `json:"force"`
	OnProgress func(migration string, current, total int) `json:"-"`
}
