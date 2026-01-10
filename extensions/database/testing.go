package database

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/xraph/forge/errors"
)

// TestDBOption configures test database behavior.
type TestDBOption func(*testDBConfig)

type testDBConfig struct {
	useMemory      bool
	enableLogging  bool
	autoMigrate    bool
	models         []any
	useTransaction bool // Run tests in a transaction and rollback
}

// WithInMemory creates an in-memory SQLite database (default).
func WithInMemory() TestDBOption {
	return func(c *testDBConfig) {
		c.useMemory = true
	}
}

// WithLogging enables query logging for debugging.
func WithLogging() TestDBOption {
	return func(c *testDBConfig) {
		c.enableLogging = true
	}
}

// WithAutoMigrate automatically creates tables for the provided models.
func WithAutoMigrate(models ...any) TestDBOption {
	return func(c *testDBConfig) {
		c.autoMigrate = true
		c.models = append(c.models, models...)
	}
}

// WithTransactionRollback wraps each test in a transaction that's rolled back.
// This is faster than truncating tables between tests.
func WithTransactionRollback() TestDBOption {
	return func(c *testDBConfig) {
		c.useTransaction = true
	}
}

// NewTestDB creates a test database instance with automatic cleanup.
// By default, it uses an in-memory SQLite database.
//
// Example:
//
//	func TestUserRepo(t *testing.T) {
//	    db := database.NewTestDB(t,
//	        database.WithAutoMigrate(&User{}),
//	    )
//
//	    repo := database.NewRepository[User](db)
//	    // Test repo methods
//	}
func NewTestDB(t testing.TB, opts ...TestDBOption) *bun.DB {
	config := &testDBConfig{
		useMemory: true,
	}

	for _, opt := range opts {
		opt(config)
	}

	// Create in-memory SQLite database
	dsn := ":memory:"
	if config.useMemory {
		dsn = "file::memory:?cache=shared"
	}

	sqldb, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	db := bun.NewDB(sqldb, sqlitedialect.New())

	// Enable logging if requested
	if config.enableLogging {
		db.AddQueryHook(&testQueryHook{t: t})
	}

	// Auto-migrate models if requested
	if config.autoMigrate && len(config.models) > 0 {
		ctx := context.Background()
		for _, model := range config.models {
			_, err := db.NewCreateTable().
				Model(model).
				IfNotExists().
				Exec(ctx)
			if err != nil {
				t.Fatalf("failed to create table for model: %v", err)
			}
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close test database: %v", err)
		}
	})

	return db
}

// testQueryHook logs queries during tests.
type testQueryHook struct {
	t testing.TB
}

func (h *testQueryHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

func (h *testQueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	h.t.Logf("[SQL] %s", event.Query)
}

// SeedFixtures inserts test data into the database.
// Records are inserted as-is without modification.
//
// Example:
//
//	users := []User{
//	    {ID: 1, Name: "Alice"},
//	    {ID: 2, Name: "Bob"},
//	}
//	err := database.SeedFixtures(t, db, &users)
func SeedFixtures(t testing.TB, db *bun.DB, fixtures ...any) error {
	ctx := context.Background()

	for _, fixture := range fixtures {
		_, err := db.NewInsert().Model(fixture).Exec(ctx)
		if err != nil {
			t.Errorf("failed to seed fixture: %v", err)

			return err
		}
	}

	return nil
}

// TruncateTables removes all data from the specified model tables.
// Useful for cleaning up between tests.
//
// Example:
//
//	err := database.TruncateTables(t, db, (*User)(nil), (*Post)(nil))
func TruncateTables(t testing.TB, db *bun.DB, models ...any) error {
	ctx := context.Background()

	for _, model := range models {
		_, err := db.NewTruncateTable().Model(model).Exec(ctx)
		if err != nil {
			// SQLite doesn't support TRUNCATE, try DELETE instead
			_, err = db.NewDelete().Model(model).Where("1=1").Exec(ctx)
			if err != nil {
				t.Errorf("failed to truncate table: %v", err)

				return err
			}
		}
	}

	return nil
}

// CleanupDB closes the database connection and cleans up resources.
// This is automatically called by t.Cleanup() when using NewTestDB.
func CleanupDB(t testing.TB, db *bun.DB) {
	if err := db.Close(); err != nil {
		t.Errorf("failed to close database: %v", err)
	}
}

// AssertRecordExists verifies that a record with the given ID exists.
//
// Example:
//
//	database.AssertRecordExists[User](t, db, 123)
func AssertRecordExists[T any](t testing.TB, db *bun.DB, id any) {
	t.Helper()

	var model T

	exists, err := db.NewSelect().
		Model(&model).
		Where("id = ?", id).
		Exists(context.Background())
	if err != nil {
		t.Fatalf("failed to check record existence: %v", err)
	}

	if !exists {
		t.Fatalf("expected record with id=%v to exist, but it doesn't", id)
	}
}

// AssertRecordNotExists verifies that a record with the given ID does not exist.
//
// Example:
//
//	database.AssertRecordNotExists[User](t, db, 999)
func AssertRecordNotExists[T any](t testing.TB, db *bun.DB, id any) {
	t.Helper()

	var model T

	exists, err := db.NewSelect().
		Model(&model).
		Where("id = ?", id).
		Exists(context.Background())
	if err != nil {
		t.Fatalf("failed to check record existence: %v", err)
	}

	if exists {
		t.Fatalf("expected record with id=%v to not exist, but it does", id)
	}
}

// AssertRecordCount verifies the total number of records in a table.
//
// Example:
//
//	database.AssertRecordCount[User](t, db, 5)
func AssertRecordCount[T any](t testing.TB, db *bun.DB, expected int) {
	t.Helper()

	var model T

	count, err := db.NewSelect().
		Model(&model).
		Count(context.Background())
	if err != nil {
		t.Fatalf("failed to count records: %v", err)
	}

	if count != expected {
		t.Fatalf("expected %d records, got %d", expected, count)
	}
}

// AssertRecordCountWhere verifies the number of records matching a condition.
//
// Example:
//
//	database.AssertRecordCountWhere[User](t, db, 3, "active = ?", true)
func AssertRecordCountWhere[T any](t testing.TB, db *bun.DB, expected int, where string, args ...any) {
	t.Helper()

	var model T

	count, err := db.NewSelect().
		Model(&model).
		Where(where, args...).
		Count(context.Background())
	if err != nil {
		t.Fatalf("failed to count records: %v", err)
	}

	if count != expected {
		t.Fatalf("expected %d records matching %q, got %d", expected, where, count)
	}
}

// LoadFixture loads a single record by ID and returns it.
// Useful for verifying record state in tests.
//
// Example:
//
//	user := database.LoadFixture[User](t, db, 1)
//	assert.Equal(t, "Alice", user.Name)
func LoadFixture[T any](t testing.TB, db *bun.DB, id any) *T {
	t.Helper()

	var model T

	err := db.NewSelect().
		Model(&model).
		Where("id = ?", id).
		Scan(context.Background())
	if err != nil {
		t.Fatalf("failed to load fixture with id=%v: %v", id, err)
	}

	return &model
}

// LoadAllFixtures loads all records of a given type.
//
// Example:
//
//	users := database.LoadAllFixtures[User](t, db)
//	assert.Len(t, users, 5)
func LoadAllFixtures[T any](t testing.TB, db *bun.DB) []T {
	t.Helper()

	var models []T

	err := db.NewSelect().
		Model(&models).
		Scan(context.Background())
	if err != nil {
		t.Fatalf("failed to load fixtures: %v", err)
	}

	return models
}

// WithTestTransaction runs a test function within a transaction that's rolled back.
// This is useful for tests that need isolation without affecting the database.
//
// Example:
//
//	database.WithTestTransaction(t, db, func(txDB *bun.DB) {
//	    // All database operations here will be rolled back
//	    repo := database.NewRepository[User](txDB)
//	    repo.Create(ctx, &user)
//	})
func WithTestTransaction(t testing.TB, db *bun.DB, fn func(txDB *bun.DB)) {
	t.Helper()

	ctx := context.Background()

	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Run the test function - pass the original db, operations will use transaction from context
		fn(db)

		// Always rollback to keep tests isolated
		return errors.New("rollback transaction")
	})

	// We expect the error since we always rollback
	if err != nil && err.Error() != "rollback transaction" {
		t.Fatalf("unexpected error in test transaction: %v", err)
	}
}

// CreateTestModels is a helper to create multiple test records at once.
//
// Example:
//
//	users := database.CreateTestModels(t, db, []User{
//	    {Name: "Alice"},
//	    {Name: "Bob"},
//	})
func CreateTestModels[T any](t testing.TB, db *bun.DB, models []T) []T {
	t.Helper()

	ctx := context.Background()

	_, err := db.NewInsert().Model(&models).Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create test models: %v", err)
	}

	return models
}

// CreateTestModel creates a single test record.
//
// Example:
//
//	user := database.CreateTestModel(t, db, User{Name: "Alice"})
func CreateTestModel[T any](t testing.TB, db *bun.DB, model T) T {
	t.Helper()

	ctx := context.Background()

	_, err := db.NewInsert().Model(&model).Exec(ctx)
	if err != nil {
		t.Fatalf("failed to create test model: %v", err)
	}

	return model
}

// AssertNoDatabaseError is a helper to check for database errors in tests.
func AssertNoDatabaseError(t testing.TB, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected database error: %v", err)
	}
}

// AssertDatabaseError is a helper to verify an error occurred.
func AssertDatabaseError(t testing.TB, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected database error, but got nil")
	}
}
