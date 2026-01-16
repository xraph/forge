package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// MigrationManager manages database migrations.
type MigrationManager struct {
	db         *bun.DB
	migrations *migrate.Migrations
	logger     forge.Logger
}

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(db *bun.DB, migrations *migrate.Migrations, logger forge.Logger) *MigrationManager {
	return &MigrationManager{
		db:         db,
		migrations: migrations,
		logger:     logger,
	}
}

// CreateTables creates the migrations table.
func (m *MigrationManager) CreateTables(ctx context.Context) error {
	migrator := migrate.NewMigrator(m.db, m.migrations)

	return migrator.Init(ctx)
}

// Migrate runs all pending migrations.
func (m *MigrationManager) Migrate(ctx context.Context) error {
	// Check if any migrations are registered before proceeding
	if len(m.migrations.Sorted()) == 0 {
		return errors.New("no migrations registered")
	}

	migrator := migrate.NewMigrator(m.db, m.migrations)

	// Ensure migration tables exist before attempting to lock
	if err := migrator.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize migration tables: %w", err)
	}

	if err := migrator.Lock(ctx); err != nil {
		return fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	defer migrator.Unlock(ctx)

	group, err := migrator.Migrate(ctx)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	if group.IsZero() {
		m.logger.Debug("no pending migrations")

		return nil
	}

	m.logger.Info("migrated to group",
		forge.F("group_id", group.ID),
		forge.F("migrations", len(group.Migrations)),
	)

	return nil
}

// Rollback rolls back the last migration group.
func (m *MigrationManager) Rollback(ctx context.Context) error {
	migrator := migrate.NewMigrator(m.db, m.migrations)

	// Ensure migration tables exist before attempting to lock
	if err := migrator.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize migration tables: %w", err)
	}

	if err := migrator.Lock(ctx); err != nil {
		return fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	defer migrator.Unlock(ctx)

	group, err := migrator.Rollback(ctx)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	if group.IsZero() {
		m.logger.Debug("no migrations to rollback")

		return nil
	}

	m.logger.Info("rolled back group",
		forge.F("group_id", group.ID),
		forge.F("migrations", len(group.Migrations)),
	)

	return nil
}

// Status returns the current migration status.
func (m *MigrationManager) Status(ctx context.Context) (*MigrationStatusResult, error) {
	migrator := migrate.NewMigrator(m.db, m.migrations)

	// Ensure migration tables exist before querying status
	if err := migrator.Init(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize migration tables: %w", err)
	}

	// Get applied migrations
	appliedMigrations, err := migrator.MigrationsWithStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get migration status: %w", err)
	}

	status := &MigrationStatusResult{
		Applied: []AppliedMigration{},
		Pending: []string{},
	}

	appliedMap := make(map[string]bool)

	for _, mig := range appliedMigrations {
		appliedMap[mig.Name] = true
		status.Applied = append(status.Applied, AppliedMigration{
			Name:      mig.Name,
			GroupID:   mig.GroupID,
			AppliedAt: mig.MigratedAt,
		})
	}

	// Get all registered migrations
	ms := m.migrations.Sorted()
	for _, mig := range ms {
		if !appliedMap[mig.Name] {
			status.Pending = append(status.Pending, mig.Name)
		}
	}

	return status, nil
}

// MigrationStatusResult represents the current state of migrations.
type MigrationStatusResult struct {
	Applied []AppliedMigration
	Pending []string
}

// AppliedMigration represents an applied migration.
type AppliedMigration struct {
	Name      string
	GroupID   int64
	AppliedAt time.Time
}

// Reset drops all tables and re-runs all migrations.
func (m *MigrationManager) Reset(ctx context.Context) error {
	// This is a destructive operation - use with caution
	migrator := migrate.NewMigrator(m.db, m.migrations)

	// Ensure migration tables exist before attempting to lock
	if err := migrator.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize migration tables: %w", err)
	}

	if err := migrator.Lock(ctx); err != nil {
		return fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	defer migrator.Unlock(ctx)

	// Rollback all migrations
	for {
		group, err := migrator.Rollback(ctx)
		if err != nil {
			return fmt.Errorf("rollback failed during reset: %w", err)
		}

		if group.IsZero() {
			break
		}
	}

	// Run all migrations
	group, err := migrator.Migrate(ctx)
	if err != nil {
		return fmt.Errorf("migration failed during reset: %w", err)
	}

	m.logger.Debug("database reset complete",
		forge.F("group_id", group.ID),
		forge.F("migrations", len(group.Migrations)),
	)

	return nil
}

// AutoMigrate automatically creates/updates tables for registered models
// This is a development convenience - use migrations for production.
func (m *MigrationManager) AutoMigrate(ctx context.Context, models ...any) error {
	for _, model := range models {
		_, err := m.db.NewCreateTable().
			Model(model).
			IfNotExists().
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to auto-migrate model: %w", err)
		}
	}

	m.logger.Debug("auto-migration completed",
		forge.F("models", len(models)),
	)

	return nil
}

// CreateMigration creates the migration tables and initial structure.
func (m *MigrationManager) CreateMigration(ctx context.Context) error {
	migrator := migrate.NewMigrator(m.db, m.migrations)

	return migrator.Init(ctx)
}
