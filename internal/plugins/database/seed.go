package database

import (
	"time"
)

// SeedConfig contains configuration for seeding operations
type SeedConfig struct {
	Seeder     string                                  `json:"seeder"`
	Force      bool                                    `json:"force"`
	DryRun     bool                                    `json:"dry_run"`
	OnProgress func(seeder string, current, total int) `json:"-"`
}

// SeedResult represents the result of a seeding operation
type SeedResult struct {
	SeededTables int           `json:"seeded_tables"`
	RecordsAdded int           `json:"records_added"`
	Duration     time.Duration `json:"duration"`
	Errors       []string      `json:"errors,omitempty"`
}

// Seeder represents a database seeder
type Seeder struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Order       int    `json:"order"`
	Enabled     bool   `json:"enabled"`
}
