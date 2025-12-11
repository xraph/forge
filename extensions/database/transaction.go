package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"

	"github.com/uptrace/bun"
)

// Transaction context keys
type ctxKey string

const (
	txKey    ctxKey = "database.tx"
	depthKey ctxKey = "database.tx.depth"
)

// MaxTransactionDepth is the maximum nesting level for transactions.
// This prevents infinite recursion and stack overflow.
const MaxTransactionDepth = 10

// TxFunc is a function that runs within a transaction.
// The context passed to the function contains the active transaction,
// which can be retrieved using GetDB.
type TxFunc func(ctx context.Context) error

// WithTransaction executes a function within a database transaction.
// If the function returns an error, the transaction is rolled back.
// If the function panics, the transaction is rolled back and the panic is converted to an error.
// If the function succeeds, the transaction is committed.
//
// For nested calls, savepoints are used to support partial rollbacks.
//
// Example:
//
//	err := database.WithTransaction(ctx, db, func(txCtx context.Context) error {
//	    // Use GetDB to get the transaction-aware database
//	    repo := database.NewRepository[User](database.GetDB(txCtx, db))
//	    return repo.Create(txCtx, &user)
//	})
func WithTransaction(ctx context.Context, db *bun.DB, fn TxFunc) error {
	// Check if we're already in a transaction
	if tx, ok := ctx.Value(txKey).(bun.Tx); ok {
		// We're in a nested transaction - use savepoint
		return withSavepoint(ctx, tx, fn)
	}

	// Start a new transaction
	return db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		// Store transaction and depth in context
		ctx = context.WithValue(ctx, txKey, tx)
		ctx = context.WithValue(ctx, depthKey, int32(1))

		// Wrap with panic recovery
		return executeTxFunc(ctx, fn)
	})
}

// WithTransactionOptions executes a function within a transaction with custom options.
// This allows control over isolation level and read-only mode.
//
// Example:
//
//	opts := &sql.TxOptions{
//	    Isolation: sql.LevelSerializable,
//	    ReadOnly:  false,
//	}
//	err := database.WithTransactionOptions(ctx, db, opts, func(txCtx context.Context) error {
//	    // Transaction code
//	    return nil
//	})
func WithTransactionOptions(ctx context.Context, db *bun.DB, opts *sql.TxOptions, fn TxFunc) error {
	// Check if we're already in a transaction
	if tx, ok := ctx.Value(txKey).(bun.Tx); ok {
		// Nested transaction - savepoints don't support options, just use savepoint
		return withSavepoint(ctx, tx, fn)
	}

	// Start a new transaction with options
	return db.RunInTx(ctx, opts, func(ctx context.Context, tx bun.Tx) error {
		ctx = context.WithValue(ctx, txKey, tx)
		ctx = context.WithValue(ctx, depthKey, int32(1))
		return executeTxFunc(ctx, fn)
	})
}

// WithNestedTransaction explicitly creates a nested transaction using savepoints.
// This must be called within an existing transaction context.
//
// Example:
//
//	database.WithTransaction(ctx, db, func(ctx1 context.Context) error {
//	    // Outer transaction
//	    repo.Create(ctx1, &user)
//
//	    // Inner transaction with savepoint
//	    err := database.WithNestedTransaction(ctx1, func(ctx2 context.Context) error {
//	        // This can be rolled back independently
//	        return repo.Create(ctx2, &profile)
//	    })
//
//	    return err
//	})
func WithNestedTransaction(ctx context.Context, fn TxFunc) error {
	tx, ok := ctx.Value(txKey).(bun.Tx)
	if !ok {
		return fmt.Errorf("WithNestedTransaction called outside of transaction context")
	}

	return withSavepoint(ctx, tx, fn)
}

// withSavepoint implements nested transaction using savepoints.
func withSavepoint(ctx context.Context, tx bun.Tx, fn TxFunc) error {
	// Get current depth
	depth := getTransactionDepth(ctx)
	if depth >= MaxTransactionDepth {
		return fmt.Errorf("maximum transaction nesting depth (%d) exceeded", MaxTransactionDepth)
	}

	// Generate savepoint name
	savepointName := fmt.Sprintf("sp_%d", depth)

	// Create savepoint
	_, err := tx.ExecContext(ctx, fmt.Sprintf("SAVEPOINT %s", savepointName))
	if err != nil {
		return fmt.Errorf("failed to create savepoint: %w", err)
	}

	// Increment depth in context
	ctx = context.WithValue(ctx, depthKey, depth+1)

	// Execute function with panic recovery
	err = executeTxFunc(ctx, fn)

	if err != nil {
		// Rollback to savepoint on error
		_, rbErr := tx.ExecContext(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepointName))
		if rbErr != nil {
			return fmt.Errorf("failed to rollback to savepoint: %w (original error: %v)", rbErr, err)
		}
		return err
	}

	// Release savepoint on success (optional, but good practice)
	_, err = tx.ExecContext(ctx, fmt.Sprintf("RELEASE SAVEPOINT %s", savepointName))
	if err != nil {
		return fmt.Errorf("failed to release savepoint: %w", err)
	}

	return nil
}

// executeTxFunc executes a transaction function with panic recovery.
func executeTxFunc(ctx context.Context, fn TxFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in transaction: %v", r)
		}
	}()

	return fn(ctx)
}

// GetDB returns the appropriate database connection from context.
// If a transaction is active in the context, it returns the transaction.
// Otherwise, it returns the provided default database.
//
// This allows code to work seamlessly both inside and outside transactions.
//
// Example:
//
//	func CreateUser(ctx context.Context, db *bun.DB, user *User) error {
//	    // Works both in and out of transaction
//	    repo := database.NewRepository[User](database.GetDB(ctx, db))
//	    return repo.Create(ctx, user)
//	}
func GetDB(ctx context.Context, defaultDB *bun.DB) bun.IDB {
	if tx, ok := ctx.Value(txKey).(bun.Tx); ok {
		return tx
	}
	return defaultDB
}

// MustGetDB returns the database connection from context.
// Panics if no default database is provided and no transaction is in context.
func MustGetDB(ctx context.Context, defaultDB *bun.DB) bun.IDB {
	db := GetDB(ctx, defaultDB)
	if db == nil {
		panic("no database connection available in context or provided as default")
	}
	return db
}

// IsInTransaction returns true if the context contains an active transaction.
//
// Example:
//
//	if database.IsInTransaction(ctx) {
//	    // We're in a transaction
//	}
func IsInTransaction(ctx context.Context) bool {
	_, ok := ctx.Value(txKey).(bun.Tx)
	return ok
}

// GetTransactionDepth returns the current nesting depth of transactions.
// Returns 0 if not in a transaction.
//
// Example:
//
//	depth := database.GetTransactionDepth(ctx)
//	fmt.Printf("Transaction nesting level: %d\n", depth)
func GetTransactionDepth(ctx context.Context) int32 {
	return getTransactionDepth(ctx)
}

// getTransactionDepth is the internal implementation.
func getTransactionDepth(ctx context.Context) int32 {
	if depth, ok := ctx.Value(depthKey).(int32); ok {
		return depth
	}
	return 0
}

// TransactionStats tracks transaction statistics.
type TransactionStats struct {
	Active     int32 // Number of active transactions
	Committed  int64 // Total committed transactions
	RolledBack int64 // Total rolled back transactions
	Panics     int64 // Total panics recovered
}

var globalTxStats TransactionStats

// GetTransactionStats returns global transaction statistics.
// Useful for monitoring and debugging.
func GetTransactionStats() TransactionStats {
	return TransactionStats{
		Active:     atomic.LoadInt32(&globalTxStats.Active),
		Committed:  atomic.LoadInt64(&globalTxStats.Committed),
		RolledBack: atomic.LoadInt64(&globalTxStats.RolledBack),
		Panics:     atomic.LoadInt64(&globalTxStats.Panics),
	}
}

// ResetTransactionStats resets global transaction statistics.
// Primarily useful for testing.
func ResetTransactionStats() {
	atomic.StoreInt32(&globalTxStats.Active, 0)
	atomic.StoreInt64(&globalTxStats.Committed, 0)
	atomic.StoreInt64(&globalTxStats.RolledBack, 0)
	atomic.StoreInt64(&globalTxStats.Panics, 0)
}

// Isolation level helpers for common patterns

// WithSerializableTransaction runs a function in a serializable transaction.
// This provides the highest isolation level.
func WithSerializableTransaction(ctx context.Context, db *bun.DB, fn TxFunc) error {
	return WithTransactionOptions(ctx, db, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
	}, fn)
}

// WithReadOnlyTransaction runs a function in a read-only transaction.
// This is useful for complex queries that need a consistent view.
func WithReadOnlyTransaction(ctx context.Context, db *bun.DB, fn TxFunc) error {
	return WithTransactionOptions(ctx, db, &sql.TxOptions{
		ReadOnly: true,
	}, fn)
}

// WithRepeatableReadTransaction runs a function in a repeatable read transaction.
// This prevents non-repeatable reads but allows phantom reads.
func WithRepeatableReadTransaction(ctx context.Context, db *bun.DB, fn TxFunc) error {
	return WithTransactionOptions(ctx, db, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
	}, fn)
}
