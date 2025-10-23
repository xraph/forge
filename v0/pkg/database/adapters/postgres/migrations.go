package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// PostgresMigrator handles PostgreSQL-specific migrations
type PostgresMigrator struct {
	connection *PostgresConnection
	logger     common.Logger
	tableName  string
}

// Migration represents a database migration record in PostgreSQL
type Migration struct {
	ID            uint      `gorm:"primaryKey;autoIncrement"`
	Version       string    `gorm:"uniqueIndex;not null;size:255"`
	Name          string    `gorm:"not null;size:255"`
	Description   string    `gorm:"size:500"`
	Checksum      string    `gorm:"not null;size:64"`
	AppliedAt     time.Time `gorm:"not null"`
	ExecutionTime int64     `gorm:"not null"` // Execution time in milliseconds
}

// TableName returns the table name for the Migration model
func (Migration) TableName() string {
	return "forge_migrations"
}

// NewPostgresMigrator creates a new PostgreSQL migrator
func NewPostgresMigrator(connection *PostgresConnection, logger common.Logger) *PostgresMigrator {
	return &PostgresMigrator{
		connection: connection,
		logger:     logger,
		tableName:  "forge_migrations",
	}
}

// RunMigrations runs all pending migrations
func (pm *PostgresMigrator) RunMigrations(ctx context.Context, migrationsPath string) error {
	// Ensure migration table exists
	if err := pm.createMigrationTable(ctx); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	// Load migrations from filesystem
	migrationManager := database.NewMigrationManager(migrationsPath, pm.logger)
	migrations, err := migrationManager.LoadMigrations(ctx, migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	if len(migrations) == 0 {
		pm.logger.Info("no migrations found")
		return nil
	}

	// Get applied migration versions
	appliedVersions, err := pm.getAppliedVersions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get applied versions: %w", err)
	}

	appliedMap := make(map[string]bool)
	for _, version := range appliedVersions {
		appliedMap[version] = true
	}

	// Filter pending migrations
	var pendingMigrations []*database.Migration
	for _, migration := range migrations {
		if !appliedMap[migration.Version] {
			pendingMigrations = append(pendingMigrations, migration)
		}
	}

	if len(pendingMigrations) == 0 {
		pm.logger.Info("no pending migrations")
		return nil
	}

	pm.logger.Info("running migrations",
		logger.Int("pending", len(pendingMigrations)),
		logger.Int("total", len(migrations)),
	)

	// Apply pending migrations
	for _, migration := range pendingMigrations {
		if err := pm.applyMigration(ctx, migration); err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", migration.ID, err)
		}
	}

	pm.logger.Info("migrations completed successfully",
		logger.Int("applied", len(pendingMigrations)),
	)

	return nil
}

// applyMigration applies a single migration
func (pm *PostgresMigrator) applyMigration(ctx context.Context, migration *database.Migration) error {
	pm.logger.Info("applying migration",
		logger.String("id", migration.ID),
		logger.String("name", migration.Name),
		logger.String("version", migration.Version),
	)

	start := time.Now()

	// Execute migration in a transaction
	err := pm.connection.Transaction(ctx, func(tx interface{}) error {
		gormTx := tx.(*gorm.DB)

		// Execute the migration SQL
		if err := pm.executeMigrationSQL(ctx, gormTx, migration.UpSQL); err != nil {
			return fmt.Errorf("failed to execute migration SQL: %w", err)
		}

		// Record the migration
		migrationRecord := &Migration{
			Version:       migration.Version,
			Name:          migration.Name,
			Description:   migration.Description,
			Checksum:      migration.Checksum,
			AppliedAt:     time.Now(),
			ExecutionTime: time.Since(start).Milliseconds(),
		}

		if err := gormTx.Create(migrationRecord).Error; err != nil {
			return fmt.Errorf("failed to record migration: %w", err)
		}

		return nil
	})

	if err != nil {
		pm.logger.Error("migration failed",
			logger.String("id", migration.ID),
			logger.Error(err),
		)
		return err
	}

	duration := time.Since(start)
	pm.logger.Info("migration applied successfully",
		logger.String("id", migration.ID),
		logger.Duration("duration", duration),
	)

	return nil
}

// revertMigration reverts a single migration
func (pm *PostgresMigrator) revertMigration(ctx context.Context, migration *database.Migration) error {
	if migration.DownSQL == "" {
		return fmt.Errorf("migration %s has no down SQL", migration.ID)
	}

	pm.logger.Info("reverting migration",
		logger.String("id", migration.ID),
		logger.String("name", migration.Name),
		logger.String("version", migration.Version),
	)

	start := time.Now()

	// Execute reversion in a transaction
	err := pm.connection.Transaction(ctx, func(tx interface{}) error {
		gormTx := tx.(*gorm.DB)

		// Execute the down SQL
		if err := pm.executeMigrationSQL(ctx, gormTx, migration.DownSQL); err != nil {
			return fmt.Errorf("failed to execute down SQL: %w", err)
		}

		// Remove the migration record
		if err := gormTx.Where("version = ?", migration.Version).Delete(&Migration{}).Error; err != nil {
			return fmt.Errorf("failed to remove migration record: %w", err)
		}

		return nil
	})

	if err != nil {
		pm.logger.Error("migration reversion failed",
			logger.String("id", migration.ID),
			logger.Error(err),
		)
		return err
	}

	duration := time.Since(start)
	pm.logger.Info("migration reverted successfully",
		logger.String("id", migration.ID),
		logger.Duration("duration", duration),
	)

	return nil
}

// executeMigrationSQL executes migration SQL statements
func (pm *PostgresMigrator) executeMigrationSQL(ctx context.Context, db *gorm.DB, sql string) error {
	if sql == "" {
		return nil
	}

	// Split SQL into individual statements
	statements := pm.splitSQL(sql)

	for i, statement := range statements {
		statement = pm.cleanStatement(statement)
		if statement == "" {
			continue
		}

		pm.logger.Debug("executing migration statement",
			logger.Int("statement", i+1),
			logger.String("sql", statement),
		)

		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("failed to execute statement %d: %w\nSQL: %s", i+1, err, statement)
		}
	}

	return nil
}

// splitSQL splits SQL into individual statements
func (pm *PostgresMigrator) splitSQL(sql string) []string {
	// Simple SQL statement splitting - in production, use a proper SQL parser
	statements := []string{}
	current := ""
	inQuote := false
	quoteChar := ""

	for i, char := range sql {
		switch {
		case !inQuote && (char == '\'' || char == '"'):
			inQuote = true
			quoteChar = string(char)
			current += string(char)
		case inQuote && string(char) == quoteChar:
			// Check if it's an escaped quote
			if i > 0 && sql[i-1] == '\\' {
				current += string(char)
			} else {
				inQuote = false
				quoteChar = ""
				current += string(char)
			}
		case !inQuote && char == ';':
			statement := pm.cleanStatement(current)
			if statement != "" {
				statements = append(statements, statement)
			}
			current = ""
		default:
			current += string(char)
		}
	}

	// Add the last statement if it doesn't end with semicolon
	if statement := pm.cleanStatement(current); statement != "" {
		statements = append(statements, statement)
	}

	return statements
}

// cleanStatement cleans and trims a SQL statement
func (pm *PostgresMigrator) cleanStatement(statement string) string {
	// Remove leading and trailing whitespace
	statement = strings.TrimSpace(statement)

	// Remove empty lines and comments
	lines := strings.Split(statement, "\n")
	var cleanLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and comment lines
		if line != "" && !strings.HasPrefix(line, "--") {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// createMigrationTable creates the migration tracking table
func (pm *PostgresMigrator) createMigrationTable(ctx context.Context) error {
	db := pm.connection.GetGormDB()
	if db == nil {
		return fmt.Errorf("database connection not available")
	}

	// Create migration table if it doesn't exist
	if err := db.WithContext(ctx).AutoMigrate(&Migration{}); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	pm.logger.Info("migration table ensured",
		logger.String("table", pm.tableName),
	)

	return nil
}

// getAppliedVersions returns a list of applied migration versions
func (pm *PostgresMigrator) getAppliedVersions(ctx context.Context) ([]string, error) {
	db := pm.connection.GetGormDB()
	if db == nil {
		return nil, fmt.Errorf("database connection not available")
	}

	var migrations []Migration
	if err := db.WithContext(ctx).Order("applied_at ASC").Find(&migrations).Error; err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}

	versions := make([]string, len(migrations))
	for i, migration := range migrations {
		versions[i] = migration.Version
	}

	return versions, nil
}

// GetMigrationHistory returns the migration history
func (pm *PostgresMigrator) GetMigrationHistory(ctx context.Context) ([]Migration, error) {
	db := pm.connection.GetGormDB()
	if db == nil {
		return nil, fmt.Errorf("database connection not available")
	}

	var migrations []Migration
	if err := db.WithContext(ctx).Order("applied_at DESC").Find(&migrations).Error; err != nil {
		return nil, fmt.Errorf("failed to query migration history: %w", err)
	}

	return migrations, nil
}

// GetLastMigration returns the last applied migration
func (pm *PostgresMigrator) GetLastMigration(ctx context.Context) (*Migration, error) {
	db := pm.connection.GetGormDB()
	if db == nil {
		return nil, fmt.Errorf("database connection not available")
	}

	var migration Migration
	if err := db.WithContext(ctx).Order("applied_at DESC").First(&migration).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // No migrations applied yet
		}
		return nil, fmt.Errorf("failed to query last migration: %w", err)
	}

	return &migration, nil
}

// ValidateMigrationChecksum validates that a migration's checksum matches the recorded one
func (pm *PostgresMigrator) ValidateMigrationChecksum(ctx context.Context, migration *database.Migration) error {
	db := pm.connection.GetGormDB()
	if db == nil {
		return fmt.Errorf("database connection not available")
	}

	var recorded Migration
	if err := db.WithContext(ctx).Where("version = ?", migration.Version).First(&recorded).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil // Migration not applied yet
		}
		return fmt.Errorf("failed to query migration record: %w", err)
	}

	if recorded.Checksum != migration.Checksum {
		return fmt.Errorf("checksum mismatch for migration %s: recorded=%s, current=%s",
			migration.Version, recorded.Checksum, migration.Checksum)
	}

	return nil
}

// IsMigrationApplied checks if a migration has been applied
func (pm *PostgresMigrator) IsMigrationApplied(ctx context.Context, version string) (bool, error) {
	db := pm.connection.GetGormDB()
	if db == nil {
		return false, fmt.Errorf("database connection not available")
	}

	var count int64
	if err := db.WithContext(ctx).Model(&Migration{}).Where("version = ?", version).Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}

	return count > 0, nil
}

// GetMigrationInfo returns information about applied migrations
func (pm *PostgresMigrator) GetMigrationInfo(ctx context.Context) (map[string]interface{}, error) {
	db := pm.connection.GetGormDB()
	if db == nil {
		return nil, fmt.Errorf("database connection not available")
	}

	var totalCount int64
	if err := db.WithContext(ctx).Model(&Migration{}).Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count migrations: %w", err)
	}

	lastMigration, err := pm.GetLastMigration(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get last migration: %w", err)
	}

	info := map[string]interface{}{
		"total_applied":   totalCount,
		"migration_table": pm.tableName,
	}

	if lastMigration != nil {
		info["last_migration"] = map[string]interface{}{
			"version":        lastMigration.Version,
			"name":           lastMigration.Name,
			"applied_at":     lastMigration.AppliedAt,
			"execution_time": lastMigration.ExecutionTime,
		}
	}

	return info, nil
}
