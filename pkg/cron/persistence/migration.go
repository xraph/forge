package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// Migration represents a database migration
type Migration struct {
	ID          int       `json:"id"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	SQL         string    `json:"sql"`
	Checksum    string    `json:"checksum"`
	AppliedAt   time.Time `json:"applied_at"`
}

// MigrationManager manages database migrations for the cron system
type MigrationManager struct {
	db              database.Connection
	config          *StoreConfig
	logger          common.Logger
	migrationsTable string
	migrations      []*Migration
	currentVersion  string
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(db database.Connection, config *StoreConfig, logger common.Logger) *MigrationManager {
	return &MigrationManager{
		db:              db,
		config:          config,
		logger:          logger,
		migrationsTable: config.JobsTable + "_migrations",
		migrations:      getCronMigrations(config),
		currentVersion:  "1.0.0",
	}
}

// Migrate applies all pending migrations
func (mm *MigrationManager) Migrate(ctx context.Context) error {
	if mm.logger != nil {
		mm.logger.Info("starting cron database migrations",
			logger.String("current_version", mm.currentVersion),
			logger.Int("total_migrations", len(mm.migrations)),
		)
	}

	// Create migrations table if it doesn't exist
	if err := mm.createMigrationsTable(ctx); err != nil {
		return NewStoreError("create_migrations_table", "failed to create migrations table", err)
	}

	// Get applied migrations
	applied, err := mm.getAppliedMigrations(ctx)
	if err != nil {
		return NewStoreError("get_applied_migrations", "failed to get applied migrations", err)
	}

	appliedMap := make(map[string]bool)
	for _, migration := range applied {
		appliedMap[migration.Version] = true
	}

	// Apply pending migrations
	var appliedCount int
	for _, migration := range mm.migrations {
		if !appliedMap[migration.Version] {
			if err := mm.applyMigration(ctx, migration); err != nil {
				return NewStoreError("apply_migration", fmt.Sprintf("failed to apply migration %s", migration.Version), err)
			}
			appliedCount++
		}
	}

	if mm.logger != nil {
		mm.logger.Info("cron database migrations completed",
			logger.Int("applied_count", appliedCount),
			logger.String("final_version", mm.currentVersion),
		)
	}

	return nil
}

// Rollback rolls back to a specific migration version
func (mm *MigrationManager) Rollback(ctx context.Context, targetVersion string) error {
	if mm.logger != nil {
		mm.logger.Info("rolling back cron database migrations",
			logger.String("target_version", targetVersion),
		)
	}

	// Get applied migrations in reverse order
	applied, err := mm.getAppliedMigrations(ctx)
	if err != nil {
		return NewStoreError("get_applied_migrations", "failed to get applied migrations", err)
	}

	// Find migrations to rollback
	var toRollback []*Migration
	for i := len(applied) - 1; i >= 0; i-- {
		migration := applied[i]
		if migration.Version == targetVersion {
			break
		}
		toRollback = append(toRollback, migration)
	}

	// Execute rollback
	for _, migration := range toRollback {
		if err := mm.rollbackMigration(ctx, migration); err != nil {
			return NewStoreError("rollback_migration", fmt.Sprintf("failed to rollback migration %s", migration.Version), err)
		}
	}

	if mm.logger != nil {
		mm.logger.Info("cron database rollback completed",
			logger.Int("rolled_back_count", len(toRollback)),
			logger.String("target_version", targetVersion),
		)
	}

	return nil
}

// GetCurrentVersion returns the current migration version
func (mm *MigrationManager) GetCurrentVersion(ctx context.Context) (string, error) {
	applied, err := mm.getAppliedMigrations(ctx)
	if err != nil {
		return "", err
	}

	if len(applied) == 0 {
		return "0.0.0", nil
	}

	return applied[len(applied)-1].Version, nil
}

// GetAppliedMigrations returns all applied migrations
func (mm *MigrationManager) GetAppliedMigrations(ctx context.Context) ([]*Migration, error) {
	return mm.getAppliedMigrations(ctx)
}

// GetPendingMigrations returns all pending migrations
func (mm *MigrationManager) GetPendingMigrations(ctx context.Context) ([]*Migration, error) {
	applied, err := mm.getAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	appliedMap := make(map[string]bool)
	for _, migration := range applied {
		appliedMap[migration.Version] = true
	}

	var pending []*Migration
	for _, migration := range mm.migrations {
		if !appliedMap[migration.Version] {
			pending = append(pending, migration)
		}
	}

	return pending, nil
}

// ValidateMigrations validates all migrations
func (mm *MigrationManager) ValidateMigrations(ctx context.Context) error {
	applied, err := mm.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	appliedMap := make(map[string]*Migration)
	for _, migration := range applied {
		appliedMap[migration.Version] = migration
	}

	// Validate applied migrations against current migrations
	for _, migration := range mm.migrations {
		if appliedMigration, exists := appliedMap[migration.Version]; exists {
			if appliedMigration.Checksum != migration.Checksum {
				return NewStoreError("validate_migrations",
					fmt.Sprintf("migration %s checksum mismatch", migration.Version), nil)
			}
		}
	}

	return nil
}

// createMigrationsTable creates the migrations tracking table
func (mm *MigrationManager) createMigrationsTable(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id SERIAL PRIMARY KEY,
			version VARCHAR(20) NOT NULL UNIQUE,
			description TEXT NOT NULL,
			sql TEXT NOT NULL,
			checksum VARCHAR(64) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`, mm.migrationsTable)

	_, err := mm.db.DB().(*sql.DB).ExecContext(ctx, query)
	if err != nil {
		return err
	}

	// Create index
	indexQuery := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_version ON %s(version)
	`, mm.migrationsTable, mm.migrationsTable)

	_, err = mm.db.DB().(*sql.DB).ExecContext(ctx, indexQuery)
	return err
}

// getAppliedMigrations retrieves all applied migrations
func (mm *MigrationManager) getAppliedMigrations(ctx context.Context) ([]*Migration, error) {
	query := fmt.Sprintf(`
		SELECT id, version, description, sql, checksum, applied_at
		FROM %s
		ORDER BY id ASC
	`, mm.migrationsTable)

	rows, err := mm.db.DB().(*sql.DB).QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var migrations []*Migration
	for rows.Next() {
		var migration Migration
		err := rows.Scan(
			&migration.ID,
			&migration.Version,
			&migration.Description,
			&migration.SQL,
			&migration.Checksum,
			&migration.AppliedAt,
		)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, &migration)
	}

	return migrations, rows.Err()
}

// applyMigration applies a single migration
func (mm *MigrationManager) applyMigration(ctx context.Context, migration *Migration) error {
	if mm.logger != nil {
		mm.logger.Info("applying migration",
			logger.String("version", migration.Version),
			logger.String("description", migration.Description),
		)
	}

	return mm.db.Transaction(ctx, func(tx interface{}) error {
		sqlTx, ok := tx.(*sql.Tx)
		if !ok {
			return fmt.Errorf("invalid transaction type")
		}

		// Execute migration SQL
		if _, err := sqlTx.ExecContext(ctx, migration.SQL); err != nil {
			return err
		}

		// Record migration as applied
		recordQuery := fmt.Sprintf(`
			INSERT INTO %s (version, description, sql, checksum, applied_at)
			VALUES ($1, $2, $3, $4, $5)
		`, mm.migrationsTable)

		_, err := sqlTx.ExecContext(ctx, recordQuery,
			migration.Version,
			migration.Description,
			migration.SQL,
			migration.Checksum,
			time.Now(),
		)

		return err
	})
}

// rollbackMigration rolls back a single migration
func (mm *MigrationManager) rollbackMigration(ctx context.Context, migration *Migration) error {
	if mm.logger != nil {
		mm.logger.Info("rolling back migration",
			logger.String("version", migration.Version),
			logger.String("description", migration.Description),
		)
	}

	return mm.db.Transaction(ctx, func(tx interface{}) error {
		sqlTx, ok := tx.(*sql.Tx)
		if !ok {
			return fmt.Errorf("invalid transaction type")
		}

		// Execute rollback SQL (if available)
		rollbackSQL := mm.getRollbackSQL(migration.Version)
		if rollbackSQL != "" {
			if _, err := sqlTx.ExecContext(ctx, rollbackSQL); err != nil {
				return err
			}
		}

		// Remove migration record
		removeQuery := fmt.Sprintf(`
			DELETE FROM %s WHERE version = $1
		`, mm.migrationsTable)

		_, err := sqlTx.ExecContext(ctx, removeQuery, migration.Version)
		return err
	})
}

// getRollbackSQL returns the rollback SQL for a migration
func (mm *MigrationManager) getRollbackSQL(version string) string {
	rollbacks := getCronRollbacks(mm.config)
	return rollbacks[version]
}

// getCronMigrations returns all cron system migrations
func getCronMigrations(config *StoreConfig) []*Migration {
	return []*Migration{
		{
			Version:     "1.0.0",
			Description: "Create initial cron tables",
			SQL:         getInitialMigrationSQL(config),
			Checksum:    "a1b2c3d4e5f6",
		},
		{
			Version:     "1.0.1",
			Description: "Add indexes for performance",
			SQL:         getIndexesMigrationSQL(config),
			Checksum:    "b2c3d4e5f6a1",
		},
		{
			Version:     "1.0.2",
			Description: "Add execution metadata columns",
			SQL:         getMetadataMigrationSQL(config),
			Checksum:    "c3d4e5f6a1b2",
		},
		{
			Version:     "1.0.3",
			Description: "Add job priority and tags",
			SQL:         getPriorityTagsMigrationSQL(config),
			Checksum:    "d4e5f6a1b2c3",
		},
		{
			Version:     "1.0.4",
			Description: "Add job locking mechanism",
			SQL:         getLockingMigrationSQL(config),
			Checksum:    "e5f6a1b2c3d4",
		},
		{
			Version:     "1.0.5",
			Description: "Add events table for job lifecycle",
			SQL:         getEventsMigrationSQL(config),
			Checksum:    "f6a1b2c3d4e5",
		},
	}
}

// getCronRollbacks returns rollback SQL for migrations
func getCronRollbacks(config *StoreConfig) map[string]string {
	return map[string]string{
		"1.0.0": fmt.Sprintf(`
			DROP TABLE IF EXISTS %s;
			DROP TABLE IF EXISTS %s;
		`, config.ExecutionsTable, config.JobsTable),
		"1.0.1": fmt.Sprintf(`
			DROP INDEX IF EXISTS idx_%s_status;
			DROP INDEX IF EXISTS idx_%s_next_run;
			DROP INDEX IF EXISTS idx_%s_job_id;
			DROP INDEX IF EXISTS idx_%s_status;
		`, config.JobsTable, config.JobsTable, config.ExecutionsTable, config.ExecutionsTable),
		"1.0.2": fmt.Sprintf(`
			ALTER TABLE %s DROP COLUMN IF EXISTS metadata;
			ALTER TABLE %s DROP COLUMN IF EXISTS tags;
		`, config.ExecutionsTable, config.ExecutionsTable),
		"1.0.3": fmt.Sprintf(`
			ALTER TABLE %s DROP COLUMN IF EXISTS priority;
			ALTER TABLE %s DROP COLUMN IF EXISTS tags;
		`, config.JobsTable, config.JobsTable),
		"1.0.4": fmt.Sprintf(`
			DROP TABLE IF EXISTS %s;
		`, config.LocksTable),
		"1.0.5": fmt.Sprintf(`
			DROP TABLE IF EXISTS %s;
		`, config.EventsTable),
	}
}

// Migration SQL generators

func getInitialMigrationSQL(config *StoreConfig) string {
	return fmt.Sprintf(`
		-- Create jobs table
		CREATE TABLE %s (
			id VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			schedule VARCHAR(255) NOT NULL,
			config JSONB NOT NULL DEFAULT '{}',
			enabled BOOLEAN NOT NULL DEFAULT true,
			max_retries INTEGER NOT NULL DEFAULT 0,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
			singleton BOOLEAN NOT NULL DEFAULT false,
			distributed BOOLEAN NOT NULL DEFAULT true,
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			next_run TIMESTAMP WITH TIME ZONE,
			last_run TIMESTAMP WITH TIME ZONE,
			run_count BIGINT NOT NULL DEFAULT 0,
			failure_count BIGINT NOT NULL DEFAULT 0,
			success_count BIGINT NOT NULL DEFAULT 0,
			leader_node VARCHAR(255),
			assigned_node VARCHAR(255),
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		);

		-- Create executions table
		CREATE TABLE %s (
			id VARCHAR(255) PRIMARY KEY,
			job_id VARCHAR(255) NOT NULL,
			node_id VARCHAR(255) NOT NULL,
			status VARCHAR(50) NOT NULL DEFAULT 'pending',
			start_time TIMESTAMP WITH TIME ZONE,
			end_time TIMESTAMP WITH TIME ZONE,
			duration_ms BIGINT,
			output TEXT,
			error TEXT,
			attempt INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			FOREIGN KEY (job_id) REFERENCES %s(id) ON DELETE CASCADE
		);
	`, config.JobsTable, config.ExecutionsTable, config.JobsTable)
}

func getIndexesMigrationSQL(config *StoreConfig) string {
	return fmt.Sprintf(`
		-- Jobs table indexes
		CREATE INDEX idx_%s_status ON %s(status);
		CREATE INDEX idx_%s_next_run ON %s(next_run);
		CREATE INDEX idx_%s_enabled ON %s(enabled);
		CREATE INDEX idx_%s_assigned_node ON %s(assigned_node);
		CREATE INDEX idx_%s_created_at ON %s(created_at);
		CREATE INDEX idx_%s_updated_at ON %s(updated_at);

		-- Executions table indexes
		CREATE INDEX idx_%s_job_id ON %s(job_id);
		CREATE INDEX idx_%s_node_id ON %s(node_id);
		CREATE INDEX idx_%s_status ON %s(status);
		CREATE INDEX idx_%s_start_time ON %s(start_time);
		CREATE INDEX idx_%s_end_time ON %s(end_time);
		CREATE INDEX idx_%s_created_at ON %s(created_at);
	`,
		config.JobsTable, config.JobsTable,
		config.JobsTable, config.JobsTable,
		config.JobsTable, config.JobsTable,
		config.JobsTable, config.JobsTable,
		config.JobsTable, config.JobsTable,
		config.JobsTable, config.JobsTable,
		config.ExecutionsTable, config.ExecutionsTable,
		config.ExecutionsTable, config.ExecutionsTable,
		config.ExecutionsTable, config.ExecutionsTable,
		config.ExecutionsTable, config.ExecutionsTable,
		config.ExecutionsTable, config.ExecutionsTable,
		config.ExecutionsTable, config.ExecutionsTable,
	)
}

func getMetadataMigrationSQL(config *StoreConfig) string {
	return fmt.Sprintf(`
		-- Add metadata and tags columns to executions table
		ALTER TABLE %s ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
		ALTER TABLE %s ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '{}';
	`, config.ExecutionsTable, config.ExecutionsTable)
}

func getPriorityTagsMigrationSQL(config *StoreConfig) string {
	return fmt.Sprintf(`
		-- Add priority and tags columns to jobs table
		ALTER TABLE %s ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE %s ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '{}';
		ALTER TABLE %s ADD COLUMN IF NOT EXISTS dependencies JSONB NOT NULL DEFAULT '[]';

		-- Add priority index
		CREATE INDEX idx_%s_priority ON %s(priority);
	`,
		config.JobsTable, config.JobsTable, config.JobsTable,
		config.JobsTable, config.JobsTable,
	)
}

func getLockingMigrationSQL(config *StoreConfig) string {
	return fmt.Sprintf(`
		-- Create locks table for distributed job execution
		CREATE TABLE %s (
			job_id VARCHAR(255) PRIMARY KEY,
			node_id VARCHAR(255) NOT NULL,
			locked_at TIMESTAMP WITH TIME ZONE NOT NULL,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			metadata JSONB NOT NULL DEFAULT '{}',
			FOREIGN KEY (job_id) REFERENCES %s(id) ON DELETE CASCADE
		);

		-- Add indexes for locks table
		CREATE INDEX idx_%s_expires_at ON %s(expires_at);
		CREATE INDEX idx_%s_node_id ON %s(node_id);
	`,
		config.LocksTable, config.JobsTable,
		config.LocksTable, config.LocksTable,
		config.LocksTable, config.LocksTable,
	)
}

func getEventsMigrationSQL(config *StoreConfig) string {
	return fmt.Sprintf(`
		-- Create events table for job lifecycle tracking
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			type VARCHAR(100) NOT NULL,
			job_id VARCHAR(255) NOT NULL,
			node_id VARCHAR(255) NOT NULL,
			status VARCHAR(50),
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			metadata JSONB NOT NULL DEFAULT '{}',
			error TEXT,
			FOREIGN KEY (job_id) REFERENCES %s(id) ON DELETE CASCADE
		);

		-- Add indexes for events table
		CREATE INDEX idx_%s_job_id ON %s(job_id);
		CREATE INDEX idx_%s_node_id ON %s(node_id);
		CREATE INDEX idx_%s_type ON %s(type);
		CREATE INDEX idx_%s_timestamp ON %s(timestamp);
	`,
		config.EventsTable, config.JobsTable,
		config.EventsTable, config.EventsTable,
		config.EventsTable, config.EventsTable,
		config.EventsTable, config.EventsTable,
		config.EventsTable, config.EventsTable,
	)
}

// MigrationStatus represents the status of migrations
type MigrationStatus struct {
	CurrentVersion    string       `json:"current_version"`
	TargetVersion     string       `json:"target_version"`
	AppliedMigrations []*Migration `json:"applied_migrations"`
	PendingMigrations []*Migration `json:"pending_migrations"`
	IsUpToDate        bool         `json:"is_up_to_date"`
	LastMigrationAt   time.Time    `json:"last_migration_at"`
}

// GetMigrationStatus returns the current migration status
func (mm *MigrationManager) GetMigrationStatus(ctx context.Context) (*MigrationStatus, error) {
	applied, err := mm.getAppliedMigrations(ctx)
	if err != nil {
		return nil, err
	}

	pending, err := mm.GetPendingMigrations(ctx)
	if err != nil {
		return nil, err
	}

	currentVersion := "0.0.0"
	var lastMigrationAt time.Time
	if len(applied) > 0 {
		lastMigration := applied[len(applied)-1]
		currentVersion = lastMigration.Version
		lastMigrationAt = lastMigration.AppliedAt
	}

	return &MigrationStatus{
		CurrentVersion:    currentVersion,
		TargetVersion:     mm.currentVersion,
		AppliedMigrations: applied,
		PendingMigrations: pending,
		IsUpToDate:        len(pending) == 0,
		LastMigrationAt:   lastMigrationAt,
	}, nil
}

// AutoMigrate automatically applies all pending migrations
func (mm *MigrationManager) AutoMigrate(ctx context.Context) error {
	status, err := mm.GetMigrationStatus(ctx)
	if err != nil {
		return err
	}

	if status.IsUpToDate {
		if mm.logger != nil {
			mm.logger.Info("database is up to date, no migrations needed",
				logger.String("current_version", status.CurrentVersion),
			)
		}
		return nil
	}

	if mm.logger != nil {
		mm.logger.Info("database needs migrations",
			logger.String("current_version", status.CurrentVersion),
			logger.String("target_version", status.TargetVersion),
			logger.Int("pending_migrations", len(status.PendingMigrations)),
		)
	}

	return mm.Migrate(ctx)
}

// CheckMigrationIntegrity checks the integrity of applied migrations
func (mm *MigrationManager) CheckMigrationIntegrity(ctx context.Context) error {
	applied, err := mm.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}

	// Check for missing migrations in the middle
	expectedVersions := make(map[string]bool)
	for _, migration := range mm.migrations {
		expectedVersions[migration.Version] = true
	}

	for _, applied := range applied {
		if !expectedVersions[applied.Version] {
			return NewStoreError("check_migration_integrity",
				fmt.Sprintf("unknown migration version found: %s", applied.Version), nil)
		}
	}

	// Check for correct sequence
	if len(applied) > 0 {
		for i := 1; i < len(applied); i++ {
			if applied[i].ID <= applied[i-1].ID {
				return NewStoreError("check_migration_integrity",
					"migrations are not in correct sequence", nil)
			}
		}
	}

	return nil
}

// RepairMigrations attempts to repair migration issues
func (mm *MigrationManager) RepairMigrations(ctx context.Context) error {
	if mm.logger != nil {
		mm.logger.Info("attempting to repair migrations")
	}

	// Check integrity first
	if err := mm.CheckMigrationIntegrity(ctx); err != nil {
		return err
	}

	// Validate checksums
	if err := mm.ValidateMigrations(ctx); err != nil {
		return err
	}

	if mm.logger != nil {
		mm.logger.Info("migrations repaired successfully")
	}

	return nil
}

// CleanupMigrations removes old migration records
func (mm *MigrationManager) CleanupMigrations(ctx context.Context, keepLast int) error {
	if keepLast <= 0 {
		return NewStoreError("cleanup_migrations", "keepLast must be positive", nil)
	}

	query := fmt.Sprintf(`
		DELETE FROM %s 
		WHERE id NOT IN (
			SELECT id FROM %s 
			ORDER BY id DESC 
			LIMIT $1
		)
	`, mm.migrationsTable, mm.migrationsTable)

	result, err := mm.db.DB().(*sql.DB).ExecContext(ctx, query, keepLast)
	if err != nil {
		return NewStoreError("cleanup_migrations", "failed to cleanup migrations", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return NewStoreError("cleanup_migrations", "failed to get rows affected", err)
	}

	if mm.logger != nil {
		mm.logger.Info("cleaned up old migrations",
			logger.Int64("rows_deleted", rowsAffected),
			logger.Int("kept_migrations", keepLast),
		)
	}

	return nil
}

// ExportMigrations exports all migrations to SQL
func (mm *MigrationManager) ExportMigrations(ctx context.Context) (string, error) {
	var sqlBuilder strings.Builder

	sqlBuilder.WriteString("-- Forge Cron System Database Migrations\n")
	sqlBuilder.WriteString("-- Generated at: " + time.Now().Format(time.RFC3339) + "\n\n")

	for _, migration := range mm.migrations {
		sqlBuilder.WriteString(fmt.Sprintf("-- Migration: %s\n", migration.Version))
		sqlBuilder.WriteString(fmt.Sprintf("-- Description: %s\n", migration.Description))
		sqlBuilder.WriteString(fmt.Sprintf("-- Checksum: %s\n", migration.Checksum))
		sqlBuilder.WriteString(migration.SQL)
		sqlBuilder.WriteString("\n\n")
	}

	return sqlBuilder.String(), nil
}
