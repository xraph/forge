package database

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// DefaultBatchSize is the default number of records to process in a single batch.
// This balances between performance and parameter limits in SQL databases.
const DefaultBatchSize = 1000

// BulkInsertOptions configures bulk insert behavior.
type BulkInsertOptions struct {
	// BatchSize is the number of records to insert per query. Defaults to 1000.
	BatchSize int

	// OnConflict specifies the conflict resolution strategy.
	// Example: "DO NOTHING", "DO UPDATE SET name = EXCLUDED.name"
	OnConflict string
}

// BulkInsert inserts multiple records in batches for optimal performance.
// This is significantly faster than inserting records one at a time.
//
// Performance: ~50-100x faster than individual inserts for 1000 records.
//
// Example:
//
//	users := []User{{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"}}
//	err := database.BulkInsert(ctx, db, users, 0) // Use default batch size
func BulkInsert[T any](ctx context.Context, db bun.IDB, records []T, batchSize int) error {
	if len(records) == 0 {
		return nil
	}

	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Process in batches
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[i:end]
		_, err := db.NewInsert().Model(&batch).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// BulkInsertWithOptions inserts multiple records with custom options.
//
// Example with conflict resolution:
//
//	opts := database.BulkInsertOptions{
//	    BatchSize:  500,
//	    OnConflict: "DO NOTHING",
//	}
//	err := database.BulkInsertWithOptions(ctx, db, users, opts)
func BulkInsertWithOptions[T any](ctx context.Context, db bun.IDB, records []T, opts BulkInsertOptions) error {
	if len(records) == 0 {
		return nil
	}

	if opts.BatchSize <= 0 {
		opts.BatchSize = DefaultBatchSize
	}

	// Process in batches
	for i := 0; i < len(records); i += opts.BatchSize {
		end := i + opts.BatchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[i:end]
		query := db.NewInsert().Model(&batch)

		if opts.OnConflict != "" {
			query = query.On(opts.OnConflict)
		}

		_, err := query.Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// BulkUpdate updates multiple records by their primary keys.
// Only the specified columns are updated.
//
// Example:
//
//	users := []User{{ID: 1, Name: "Alice"}, {ID: 2, Name: "Bob"}}
//	err := database.BulkUpdate(ctx, db, users, []string{"name"}, 0)
func BulkUpdate[T any](ctx context.Context, db bun.IDB, records []T, columns []string, batchSize int) error {
	if len(records) == 0 {
		return nil
	}

	if len(columns) == 0 {
		return fmt.Errorf("no columns specified for bulk update")
	}

	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Process in batches
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[i:end]
		_, err := db.NewUpdate().Model(&batch).Column(columns...).Bulk().Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// BulkUpsert performs bulk insert with conflict resolution (upsert).
// If a record with the same key exists, it's updated instead of inserted.
//
// Example for Postgres:
//
//	err := database.BulkUpsert(ctx, db, users, []string{"email"}, 0)
//	// ON CONFLICT (email) DO UPDATE SET ...
func BulkUpsert[T any](ctx context.Context, db bun.IDB, records []T, conflictColumns []string, batchSize int) error {
	if len(records) == 0 {
		return nil
	}

	if len(conflictColumns) == 0 {
		return fmt.Errorf("no conflict columns specified for upsert")
	}

	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// Build ON CONFLICT clause
	onConflict := fmt.Sprintf("(%s) DO UPDATE", conflictColumns[0])
	for i := 1; i < len(conflictColumns); i++ {
		onConflict = fmt.Sprintf("(%s, %s) DO UPDATE", onConflict[:len(onConflict)-10], conflictColumns[i])
	}

	// Process in batches
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[i:end]
		_, err := db.NewInsert().
			Model(&batch).
			On(onConflict).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to upsert batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// BulkDelete deletes multiple records by their IDs.
// More efficient than deleting one at a time.
//
// Example:
//
//	ids := []int64{1, 2, 3, 4, 5}
//	err := database.BulkDelete[User](ctx, db, ids)
func BulkDelete[T any](ctx context.Context, db bun.IDB, ids []any) error {
	if len(ids) == 0 {
		return nil
	}

	var model T
	_, err := db.NewDelete().
		Model(&model).
		Where("id IN (?)", bun.In(ids)).
		Exec(ctx)

	return err
}

// BulkSoftDelete soft deletes multiple records by their IDs.
// Only works with models that have a DeletedAt field with soft_delete tag.
//
// Example:
//
//	ids := []int64{1, 2, 3, 4, 5}
//	err := database.BulkSoftDelete[User](ctx, db, ids)
func BulkSoftDelete[T any](ctx context.Context, db bun.IDB, ids []any) error {
	if len(ids) == 0 {
		return nil
	}

	var model T
	_, err := db.NewDelete().
		Model(&model).
		Where("id IN (?)", bun.In(ids)).
		Exec(ctx)

	return err
}

// BulkOperationProgress provides progress updates during bulk operations.
type BulkOperationProgress struct {
	Total     int // Total number of records to process
	Processed int // Number of records processed so far
	Failed    int // Number of records that failed
}

// ProgressCallback is called periodically during bulk operations.
type ProgressCallback func(progress BulkOperationProgress)

// BulkInsertWithProgress inserts records in batches with progress updates.
// The callback is called after each batch completes.
//
// Example:
//
//	err := database.BulkInsertWithProgress(ctx, db, users, 100, func(p database.BulkOperationProgress) {
//	    fmt.Printf("Progress: %d/%d (%d failed)\n", p.Processed, p.Total, p.Failed)
//	})
func BulkInsertWithProgress[T any](ctx context.Context, db bun.IDB, records []T, batchSize int, callback ProgressCallback) error {
	if len(records) == 0 {
		return nil
	}

	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	progress := BulkOperationProgress{
		Total: len(records),
	}

	// Process in batches
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[i:end]
		_, err := db.NewInsert().Model(&batch).Exec(ctx)
		if err != nil {
			progress.Failed += len(batch)
		} else {
			progress.Processed += len(batch)
		}

		// Call progress callback
		if callback != nil {
			callback(progress)
		}

		// If there was an error, return it
		if err != nil {
			return fmt.Errorf("failed to insert batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// BulkUpdateWithProgress updates records in batches with progress updates.
func BulkUpdateWithProgress[T any](ctx context.Context, db bun.IDB, records []T, columns []string, batchSize int, callback ProgressCallback) error {
	if len(records) == 0 {
		return nil
	}

	if len(columns) == 0 {
		return fmt.Errorf("no columns specified for bulk update")
	}

	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	progress := BulkOperationProgress{
		Total: len(records),
	}

	// Process in batches
	for i := 0; i < len(records); i += batchSize {
		end := i + batchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[i:end]
		_, err := db.NewUpdate().Model(&batch).Column(columns...).Bulk().Exec(ctx)
		if err != nil {
			progress.Failed += len(batch)
		} else {
			progress.Processed += len(batch)
		}

		// Call progress callback
		if callback != nil {
			callback(progress)
		}

		// If there was an error, return it
		if err != nil {
			return fmt.Errorf("failed to update batch %d-%d: %w", i, end, err)
		}
	}

	return nil
}

// ChunkSlice splits a slice into chunks of the specified size.
// Useful for custom bulk operations.
//
// Example:
//
//	chunks := database.ChunkSlice(records, 100)
//	for _, chunk := range chunks {
//	    // Process chunk
//	}
func ChunkSlice[T any](slice []T, chunkSize int) [][]T {
	if chunkSize <= 0 {
		chunkSize = DefaultBatchSize
	}

	var chunks [][]T
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize
		if end > len(slice) {
			end = len(slice)
		}
		chunks = append(chunks, slice[i:end])
	}

	return chunks
}

