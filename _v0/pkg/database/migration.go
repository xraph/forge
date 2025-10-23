package database

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Migration represents a database migration
type Migration struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	UpSQL       string    `json:"up_sql"`
	DownSQL     string    `json:"down_sql"`
	Checksum    string    `json:"checksum"`
	CreatedAt   time.Time `json:"created_at"`
	AppliedAt   time.Time `json:"applied_at"`
	Applied     bool      `json:"applied"`
}

// MigrationManager manages database migrations
type MigrationManager interface {
	// Migration loading
	LoadMigrations(ctx context.Context, migrationsPath string) ([]*Migration, error)
	GetMigration(id string) (*Migration, error)
	GetMigrations() []*Migration
	GetPendingMigrations() []*Migration
	GetAppliedMigrations() []*Migration

	// Migration execution
	ApplyMigration(ctx context.Context, conn Connection, migration *Migration) error
	RevertMigration(ctx context.Context, conn Connection, migration *Migration) error
	ApplyAll(ctx context.Context, conn Connection) error
	RevertTo(ctx context.Context, conn Connection, targetVersion string) error

	// Migration tracking
	CreateMigrationTable(ctx context.Context, conn Connection) error
	RecordMigration(ctx context.Context, conn Connection, migration *Migration) error
	RemoveMigrationRecord(ctx context.Context, conn Connection, migration *Migration) error
	GetAppliedVersions(ctx context.Context, conn Connection) ([]string, error)

	// Validation and information
	ValidateMigrations() error
	GetTotalMigrations() int
	GetLastMigrationRun() time.Time
}

// migrationManager implements MigrationManager
type migrationManager struct {
	migrationsPath   string
	migrations       []*Migration
	lastMigrationRun time.Time
	logger           common.Logger
}

// NewMigrationManager creates a new migration manager
func NewMigrationManager(migrationsPath string, logger common.Logger) *migrationManager {
	return &migrationManager{
		migrationsPath: migrationsPath,
		migrations:     make([]*Migration, 0),
		logger:         logger,
	}
}

// LoadMigrations loads migrations from the specified path
func (mm *migrationManager) LoadMigrations(ctx context.Context, migrationsPath string) ([]*Migration, error) {
	if migrationsPath != "" {
		mm.migrationsPath = migrationsPath
	}

	mm.logger.Info("loading migrations",
		logger.String("path", mm.migrationsPath),
	)

	// Check if migrations directory exists
	if _, err := os.Stat(mm.migrationsPath); os.IsNotExist(err) {
		mm.logger.Warn("migrations directory does not exist",
			logger.String("path", mm.migrationsPath),
		)
		return []*Migration{}, nil
	}

	var migrations []*Migration

	// Walk through the migrations directory
	err := filepath.WalkDir(mm.migrationsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-SQL files
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".sql") {
			return nil
		}

		migration, err := mm.parseMigrationFile(path)
		if err != nil {
			mm.logger.Error("failed to parse migration file",
				logger.String("file", path),
				logger.Error(err),
			)
			return err
		}

		if migration != nil {
			migrations = append(migrations, migration)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	mm.migrations = migrations

	mm.logger.Info("migrations loaded",
		logger.Int("count", len(migrations)),
	)

	return migrations, nil
}

// parseMigrationFile parses a migration file
func (mm *migrationManager) parseMigrationFile(filePath string) (*Migration, error) {
	fileName := filepath.Base(filePath)

	// Parse file name format: YYYYMMDDHHMMSS_name.sql or V001__name.sql
	var version, name string

	if strings.HasPrefix(fileName, "V") {
		// Flyway-style naming: V001__create_users_table.sql
		parts := strings.SplitN(fileName[:len(fileName)-4], "__", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration file name format: %s", fileName)
		}
		version = parts[0]
		name = strings.ReplaceAll(parts[1], "_", " ")
	} else {
		// Timestamp-style naming: 20240101120000_create_users_table.sql
		parts := strings.SplitN(fileName[:len(fileName)-4], "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid migration file name format: %s", fileName)
		}
		version = parts[0]
		name = strings.ReplaceAll(parts[1], "_", " ")
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration file: %w", err)
	}

	// Parse UP and DOWN sections
	upSQL, downSQL := mm.parseMigrationContent(string(content))

	// Generate checksum
	checksum := mm.generateChecksum(string(content))

	migration := &Migration{
		ID:          fmt.Sprintf("%s_%s", version, strings.ReplaceAll(name, " ", "_")),
		Name:        name,
		Version:     version,
		Description: mm.extractDescription(string(content)),
		UpSQL:       upSQL,
		DownSQL:     downSQL,
		Checksum:    checksum,
		CreatedAt:   mm.getFileCreationTime(filePath),
		Applied:     false,
	}

	return migration, nil
}

// parseMigrationContent parses migration content to extract UP and DOWN sections
func (mm *migrationManager) parseMigrationContent(content string) (upSQL, downSQL string) {
	lines := strings.Split(content, "\n")
	var currentSection strings.Builder
	var inDownSection bool

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Check for section markers
		if strings.HasPrefix(trimmedLine, "-- +migrate Up") ||
			strings.HasPrefix(trimmedLine, "-- UP") ||
			strings.HasPrefix(trimmedLine, "-- +goose Up") {
			inDownSection = false
			continue
		} else if strings.HasPrefix(trimmedLine, "-- +migrate Down") ||
			strings.HasPrefix(trimmedLine, "-- DOWN") ||
			strings.HasPrefix(trimmedLine, "-- +goose Down") {
			upSQL = strings.TrimSpace(currentSection.String())
			currentSection.Reset()
			inDownSection = true
			continue
		}

		// Skip comment lines that start with --
		if strings.HasPrefix(trimmedLine, "--") {
			continue
		}

		currentSection.WriteString(line)
		currentSection.WriteString("\n")
	}

	if inDownSection {
		downSQL = strings.TrimSpace(currentSection.String())
	} else {
		upSQL = strings.TrimSpace(currentSection.String())
	}

	return upSQL, downSQL
}

// extractDescription extracts description from migration content
func (mm *migrationManager) extractDescription(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "-- Description:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmedLine, "-- Description:"))
		}
	}
	return ""
}

// generateChecksum generates a checksum for migration content
func (mm *migrationManager) generateChecksum(content string) string {
	// Simple checksum implementation (in production, use proper hash)
	hash := 0
	for _, char := range content {
		hash = 31*hash + int(char)
	}
	return fmt.Sprintf("%x", hash)
}

// getFileCreationTime gets the creation time of a file
func (mm *migrationManager) getFileCreationTime(filePath string) time.Time {
	if info, err := os.Stat(filePath); err == nil {
		return info.ModTime()
	}
	return time.Now()
}

// GetMigration returns a migration by ID
func (mm *migrationManager) GetMigration(id string) (*Migration, error) {
	for _, migration := range mm.migrations {
		if migration.ID == id {
			return migration, nil
		}
	}
	return nil, fmt.Errorf("migration '%s' not found", id)
}

// GetMigrations returns all migrations
func (mm *migrationManager) GetMigrations() []*Migration {
	return mm.migrations
}

// GetPendingMigrations returns migrations that haven't been applied
func (mm *migrationManager) GetPendingMigrations() []*Migration {
	var pending []*Migration
	for _, migration := range mm.migrations {
		if !migration.Applied {
			pending = append(pending, migration)
		}
	}
	return pending
}

// GetAppliedMigrations returns migrations that have been applied
func (mm *migrationManager) GetAppliedMigrations() []*Migration {
	var applied []*Migration
	for _, migration := range mm.migrations {
		if migration.Applied {
			applied = append(applied, migration)
		}
	}
	return applied
}

// ApplyMigration applies a single migration
func (mm *migrationManager) ApplyMigration(ctx context.Context, conn Connection, migration *Migration) error {
	if migration.Applied {
		return fmt.Errorf("migration '%s' is already applied", migration.ID)
	}

	if migration.UpSQL == "" {
		return fmt.Errorf("migration '%s' has no UP SQL", migration.ID)
	}

	mm.logger.Info("applying migration",
		logger.String("id", migration.ID),
		logger.String("name", migration.Name),
		logger.String("version", migration.Version),
	)

	start := time.Now()

	// Execute migration within a transaction
	err := conn.Transaction(ctx, func(tx interface{}) error {
		// Execute the UP SQL
		if err := mm.executeMigrationSQL(ctx, tx, migration.UpSQL); err != nil {
			return fmt.Errorf("failed to execute UP SQL: %w", err)
		}

		// Record the migration
		migration.AppliedAt = time.Now()
		migration.Applied = true

		if err := mm.RecordMigration(ctx, conn, migration); err != nil {
			return fmt.Errorf("failed to record migration: %w", err)
		}

		return nil
	})

	if err != nil {
		migration.Applied = false
		return fmt.Errorf("failed to apply migration '%s': %w", migration.ID, err)
	}

	duration := time.Since(start)
	mm.lastMigrationRun = time.Now()

	mm.logger.Info("migration applied successfully",
		logger.String("id", migration.ID),
		logger.Duration("duration", duration),
	)

	return nil
}

// RevertMigration reverts a single migration
func (mm *migrationManager) RevertMigration(ctx context.Context, conn Connection, migration *Migration) error {
	if !migration.Applied {
		return fmt.Errorf("migration '%s' is not applied", migration.ID)
	}

	if migration.DownSQL == "" {
		return fmt.Errorf("migration '%s' has no DOWN SQL", migration.ID)
	}

	mm.logger.Info("reverting migration",
		logger.String("id", migration.ID),
		logger.String("name", migration.Name),
		logger.String("version", migration.Version),
	)

	start := time.Now()

	// Execute migration within a transaction
	err := conn.Transaction(ctx, func(tx interface{}) error {
		// Execute the DOWN SQL
		if err := mm.executeMigrationSQL(ctx, tx, migration.DownSQL); err != nil {
			return fmt.Errorf("failed to execute DOWN SQL: %w", err)
		}

		// Remove the migration record
		if err := mm.RemoveMigrationRecord(ctx, conn, migration); err != nil {
			return fmt.Errorf("failed to remove migration record: %w", err)
		}

		migration.Applied = false
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to revert migration '%s': %w", migration.ID, err)
	}

	duration := time.Since(start)

	mm.logger.Info("migration reverted successfully",
		logger.String("id", migration.ID),
		logger.Duration("duration", duration),
	)

	return nil
}

// ApplyAll applies all pending migrations
func (mm *migrationManager) ApplyAll(ctx context.Context, conn Connection) error {
	// Ensure migration table exists
	if err := mm.CreateMigrationTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	// Get applied versions to update migration status
	appliedVersions, err := mm.GetAppliedVersions(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to get applied versions: %w", err)
	}

	// Update applied status for existing migrations
	appliedMap := make(map[string]bool)
	for _, version := range appliedVersions {
		appliedMap[version] = true
	}

	for _, migration := range mm.migrations {
		migration.Applied = appliedMap[migration.Version]
	}

	pendingMigrations := mm.GetPendingMigrations()
	if len(pendingMigrations) == 0 {
		mm.logger.Info("no pending migrations to apply")
		return nil
	}

	mm.logger.Info("applying migrations",
		logger.Int("count", len(pendingMigrations)),
	)

	for _, migration := range pendingMigrations {
		if err := mm.ApplyMigration(ctx, conn, migration); err != nil {
			return err
		}
	}

	mm.logger.Info("all migrations applied successfully",
		logger.Int("count", len(pendingMigrations)),
	)

	return nil
}

// RevertTo reverts migrations to a specific version
func (mm *migrationManager) RevertTo(ctx context.Context, conn Connection, targetVersion string) error {
	appliedMigrations := mm.GetAppliedMigrations()

	// Sort in reverse order for reversion
	sort.Slice(appliedMigrations, func(i, j int) bool {
		return appliedMigrations[i].Version > appliedMigrations[j].Version
	})

	var migrationsToRevert []*Migration
	for _, migration := range appliedMigrations {
		if migration.Version > targetVersion {
			migrationsToRevert = append(migrationsToRevert, migration)
		}
	}

	if len(migrationsToRevert) == 0 {
		mm.logger.Info("no migrations to revert")
		return nil
	}

	mm.logger.Info("reverting migrations",
		logger.Int("count", len(migrationsToRevert)),
		logger.String("target_version", targetVersion),
	)

	for _, migration := range migrationsToRevert {
		if err := mm.RevertMigration(ctx, conn, migration); err != nil {
			return err
		}
	}

	mm.logger.Info("migrations reverted successfully",
		logger.Int("count", len(migrationsToRevert)),
	)

	return nil
}

// CreateMigrationTable creates the migration tracking table (adapter-specific implementation needed)
func (mm *migrationManager) CreateMigrationTable(ctx context.Context, conn Connection) error {
	// This method should be implemented by specific database adapters
	return fmt.Errorf("CreateMigrationTable must be implemented by database adapter")
}

// RecordMigration records a migration in the tracking table (adapter-specific implementation needed)
func (mm *migrationManager) RecordMigration(ctx context.Context, conn Connection, migration *Migration) error {
	// This method should be implemented by specific database adapters
	return fmt.Errorf("RecordMigration must be implemented by database adapter")
}

// RemoveMigrationRecord removes a migration record from the tracking table (adapter-specific implementation needed)
func (mm *migrationManager) RemoveMigrationRecord(ctx context.Context, conn Connection, migration *Migration) error {
	// This method should be implemented by specific database adapters
	return fmt.Errorf("RemoveMigrationRecord must be implemented by database adapter")
}

// GetAppliedVersions gets applied migration versions from the tracking table (adapter-specific implementation needed)
func (mm *migrationManager) GetAppliedVersions(ctx context.Context, conn Connection) ([]string, error) {
	// This method should be implemented by specific database adapters
	return nil, fmt.Errorf("GetAppliedVersions must be implemented by database adapter")
}

// executeMigrationSQL executes migration SQL (adapter-specific implementation needed)
func (mm *migrationManager) executeMigrationSQL(ctx context.Context, tx interface{}, sql string) error {
	// This method should be implemented by specific database adapters
	return fmt.Errorf("executeMigrationSQL must be implemented by database adapter")
}

// ValidateMigrations validates all loaded migrations
func (mm *migrationManager) ValidateMigrations() error {
	if len(mm.migrations) == 0 {
		return nil
	}

	// Check for duplicate versions
	versions := make(map[string]*Migration)
	for _, migration := range mm.migrations {
		if existing, exists := versions[migration.Version]; exists {
			return fmt.Errorf("duplicate migration version '%s': '%s' and '%s'",
				migration.Version, existing.ID, migration.ID)
		}
		versions[migration.Version] = migration
	}

	// Validate migration content
	for _, migration := range mm.migrations {
		if migration.UpSQL == "" {
			return fmt.Errorf("migration '%s' has no UP SQL", migration.ID)
		}
		// DOWN SQL is optional for some migrations
	}

	return nil
}

// GetTotalMigrations returns the total number of migrations
func (mm *migrationManager) GetTotalMigrations() int {
	return len(mm.migrations)
}

// GetLastMigrationRun returns the timestamp of the last migration run
func (mm *migrationManager) GetLastMigrationRun() time.Time {
	return mm.lastMigrationRun
}

// MigrationInfo provides information about migrations
type MigrationInfo struct {
	TotalMigrations   int          `json:"total_migrations"`
	AppliedMigrations int          `json:"applied_migrations"`
	PendingMigrations int          `json:"pending_migrations"`
	LastMigrationRun  time.Time    `json:"last_migration_run"`
	Migrations        []*Migration `json:"migrations"`
}

// GetMigrationInfo returns information about migrations
func (mm *migrationManager) GetMigrationInfo() *MigrationInfo {
	applied := mm.GetAppliedMigrations()
	pending := mm.GetPendingMigrations()

	return &MigrationInfo{
		TotalMigrations:   len(mm.migrations),
		AppliedMigrations: len(applied),
		PendingMigrations: len(pending),
		LastMigrationRun:  mm.lastMigrationRun,
		Migrations:        mm.migrations,
	}
}
