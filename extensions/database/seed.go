package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/xraph/forge"
)

// Seeder defines the interface for database seeders.
// Seeders populate the database with initial or test data.
//
// Seeders should be idempotent - safe to run multiple times.
type Seeder interface {
	// Name returns a unique identifier for this seeder.
	Name() string

	// Seed executes the seeding logic.
	// Should return an error if seeding fails.
	Seed(ctx context.Context, db *bun.DB) error
}

// SeederRunner manages and executes database seeders.
type SeederRunner struct {
	db      *bun.DB
	seeders map[string]Seeder
	logger  forge.Logger
	tracked bool // Whether to track seeder execution in database
}

// SeederRecord tracks which seeders have been run.
type SeederRecord struct {
	bun.BaseModel `bun:"table:seeders"`

	ID        int64     `bun:"id,pk,autoincrement"`
	Name      string    `bun:"name,unique,notnull"`
	RunAt     time.Time `bun:"run_at,notnull"`
	Duration  int64     `bun:"duration"` // Duration in milliseconds
	Success   bool      `bun:"success"`
	ErrorMsg  string    `bun:"error_msg"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// NewSeederRunner creates a new seeder runner.
//
// Example:
//
//	runner := database.NewSeederRunner(db, logger)
//	runner.Register(&UserSeeder{})
//	runner.Register(&ProductSeeder{})
//	err := runner.Run(ctx)
func NewSeederRunner(db *bun.DB, logger forge.Logger) *SeederRunner {
	return &SeederRunner{
		db:      db,
		seeders: make(map[string]Seeder),
		logger:  logger,
		tracked: true,
	}
}

// WithTracking enables or disables tracking of seeder execution.
// When enabled, seeder runs are recorded in the database.
func (r *SeederRunner) WithTracking(enabled bool) *SeederRunner {
	r.tracked = enabled
	return r
}

// Register adds a seeder to the runner.
func (r *SeederRunner) Register(seeder Seeder) {
	if seeder == nil {
		return
	}

	name := seeder.Name()
	if name == "" {
		if r.logger != nil {
			r.logger.Warn("seeder has empty name, skipping registration")
		}
		return
	}

	r.seeders[name] = seeder

	if r.logger != nil {
		r.logger.Info("seeder registered", forge.F("name", name))
	}
}

// RegisterMany registers multiple seeders at once.
func (r *SeederRunner) RegisterMany(seeders ...Seeder) {
	for _, seeder := range seeders {
		r.Register(seeder)
	}
}

// ensureTrackingTable creates the seeders tracking table if tracking is enabled.
func (r *SeederRunner) ensureTrackingTable(ctx context.Context) error {
	if !r.tracked {
		return nil
	}

	_, err := r.db.NewCreateTable().
		Model((*SeederRecord)(nil)).
		IfNotExists().
		Exec(ctx)

	return err
}

// hasRun checks if a seeder has already been executed successfully.
func (r *SeederRunner) hasRun(ctx context.Context, name string) (bool, error) {
	if !r.tracked {
		return false, nil
	}

	exists, err := r.db.NewSelect().
		Model((*SeederRecord)(nil)).
		Where("name = ? AND success = ?", name, true).
		Exists(ctx)

	return exists, err
}

// recordRun records the execution of a seeder.
func (r *SeederRunner) recordRun(ctx context.Context, name string, duration time.Duration, success bool, err error) error {
	if !r.tracked {
		return nil
	}

	record := &SeederRecord{
		Name:     name,
		RunAt:    time.Now(),
		Duration: duration.Milliseconds(),
		Success:  success,
	}

	if err != nil {
		record.ErrorMsg = err.Error()
	}

	_, insertErr := r.db.NewInsert().
		Model(record).
		On("CONFLICT (name) DO UPDATE").
		Set("run_at = EXCLUDED.run_at").
		Set("duration = EXCLUDED.duration").
		Set("success = EXCLUDED.success").
		Set("error_msg = EXCLUDED.error_msg").
		Exec(ctx)

	return insertErr
}

// Run executes all registered seeders that haven't been run yet.
// If a seeder has already been run successfully, it's skipped.
//
// Example:
//
//	err := runner.Run(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
func (r *SeederRunner) Run(ctx context.Context) error {
	if r.logger != nil {
		r.logger.Info("starting database seeding", forge.F("seeders", len(r.seeders)))
	}

	// Ensure tracking table exists
	if err := r.ensureTrackingTable(ctx); err != nil {
		return fmt.Errorf("failed to create seeders tracking table: %w", err)
	}

	executed := 0
	skipped := 0
	failed := 0

	for name, seeder := range r.seeders {
		// Check if already run
		hasRun, err := r.hasRun(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to check seeder status for %s: %w", name, err)
		}

		if hasRun {
			if r.logger != nil {
				r.logger.Info("seeder already run, skipping", forge.F("name", name))
			}
			skipped++
			continue
		}

		// Execute seeder
		if r.logger != nil {
			r.logger.Info("running seeder", forge.F("name", name))
		}

		start := time.Now()
		err = seeder.Seed(ctx, r.db)
		duration := time.Since(start)

		// Record the run
		if recordErr := r.recordRun(ctx, name, duration, err == nil, err); recordErr != nil {
			if r.logger != nil {
				r.logger.Warn("failed to record seeder run", forge.F("name", name), forge.F("error", recordErr.Error()))
			}
		}

		if err != nil {
			if r.logger != nil {
				r.logger.Error("seeder failed",
					forge.F("name", name),
					forge.F("duration", duration.String()),
					forge.F("error", err.Error()))
			}
			failed++
			return fmt.Errorf("seeder %s failed: %w", name, err)
		}

		if r.logger != nil {
			r.logger.Info("seeder completed",
				forge.F("name", name),
				forge.F("duration", duration.String()))
		}
		executed++
	}

	if r.logger != nil {
		r.logger.Info("seeding completed",
			forge.F("executed", executed),
			forge.F("skipped", skipped),
			forge.F("failed", failed))
	}

	return nil
}

// RunSeeder executes a specific seeder by name.
// Unlike Run(), this always executes the seeder even if it has run before.
//
// Example:
//
//	err := runner.RunSeeder(ctx, "users")
func (r *SeederRunner) RunSeeder(ctx context.Context, name string) error {
	seeder, exists := r.seeders[name]
	if !exists {
		return fmt.Errorf("seeder %s not found", name)
	}

	if r.logger != nil {
		r.logger.Info("running seeder", forge.F("name", name))
	}

	start := time.Now()
	err := seeder.Seed(ctx, r.db)
	duration := time.Since(start)

	// Record the run
	if recordErr := r.recordRun(ctx, name, duration, err == nil, err); recordErr != nil {
		if r.logger != nil {
			r.logger.Warn("failed to record seeder run",
				forge.F("name", name),
				forge.F("error", recordErr.Error()))
		}
	}

	if err != nil {
		if r.logger != nil {
			r.logger.Error("seeder failed",
				forge.F("name", name),
				forge.F("duration", duration.String()),
				forge.F("error", err.Error()))
		}
		return fmt.Errorf("seeder %s failed: %w", name, err)
	}

	if r.logger != nil {
		r.logger.Info("seeder completed",
			forge.F("name", name),
			forge.F("duration", duration.String()))
	}

	return nil
}

// Reset clears the seeder tracking for a specific seeder.
// This allows the seeder to be run again.
//
// Example:
//
//	err := runner.Reset(ctx, "users")
func (r *SeederRunner) Reset(ctx context.Context, name string) error {
	if !r.tracked {
		return nil
	}

	_, err := r.db.NewDelete().
		Model((*SeederRecord)(nil)).
		Where("name = ?", name).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to reset seeder %s: %w", name, err)
	}

	if r.logger != nil {
		r.logger.Info("seeder reset", forge.F("name", name))
	}

	return nil
}

// ResetAll clears all seeder tracking.
// This allows all seeders to be run again.
//
// Example:
//
//	err := runner.ResetAll(ctx)
func (r *SeederRunner) ResetAll(ctx context.Context) error {
	if !r.tracked {
		return nil
	}

	_, err := r.db.NewTruncateTable().
		Model((*SeederRecord)(nil)).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to reset all seeders: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("all seeders reset")
	}

	return nil
}

// List returns the names of all registered seeders.
func (r *SeederRunner) List() []string {
	names := make([]string, 0, len(r.seeders))
	for name := range r.seeders {
		names = append(names, name)
	}
	return names
}

// GetSeeder returns a seeder by name.
func (r *SeederRunner) GetSeeder(name string) (Seeder, bool) {
	seeder, exists := r.seeders[name]
	return seeder, exists
}

// SeederFunc is a function type that implements the Seeder interface.
// This allows you to create seeders from functions.
type SeederFunc struct {
	name string
	fn   func(ctx context.Context, db *bun.DB) error
}

// NewSeederFunc creates a seeder from a function.
//
// Example:
//
//	seeder := database.NewSeederFunc("users", func(ctx context.Context, db *bun.DB) error {
//	    users := []User{{Name: "Alice"}, {Name: "Bob"}}
//	    return database.BulkInsert(ctx, db, users, 0)
//	})
func NewSeederFunc(name string, fn func(ctx context.Context, db *bun.DB) error) *SeederFunc {
	return &SeederFunc{
		name: name,
		fn:   fn,
	}
}

// Name implements Seeder interface.
func (s *SeederFunc) Name() string {
	return s.name
}

// Seed implements Seeder interface.
func (s *SeederFunc) Seed(ctx context.Context, db *bun.DB) error {
	return s.fn(ctx, db)
}

// IdempotentInsert inserts records only if they don't already exist.
// This is useful for making seeders idempotent.
//
// Example:
//
//	users := []User{{ID: 1, Name: "Alice"}, {ID: 2, Name: "Bob"}}
//	err := database.IdempotentInsert(ctx, db, &users, "id")
func IdempotentInsert[T any](ctx context.Context, db *bun.DB, records *[]T, conflictColumn string) error {
	if records == nil || len(*records) == 0 {
		return nil
	}

	// Use ON CONFLICT DO NOTHING for idempotency
	_, err := db.NewInsert().
		Model(records).
		On(fmt.Sprintf("CONFLICT (%s) DO NOTHING", conflictColumn)).
		Exec(ctx)

	return err
}

// IdempotentUpsert inserts or updates records based on conflict columns.
// This ensures seeders can be run multiple times safely.
//
// Example:
//
//	users := []User{{ID: 1, Name: "Alice"}, {ID: 2, Name: "Bob"}}
//	err := database.IdempotentUpsert(ctx, db, &users, "id")
func IdempotentUpsert[T any](ctx context.Context, db *bun.DB, records *[]T, conflictColumn string) error {
	if records == nil || len(*records) == 0 {
		return nil
	}

	// Use ON CONFLICT DO UPDATE for upsert behavior
	_, err := db.NewInsert().
		Model(records).
		On(fmt.Sprintf("CONFLICT (%s) DO UPDATE", conflictColumn)).
		Exec(ctx)

	return err
}

