package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

// UserRepository demonstrates a service that depends on *bun.DB using new DI
type UserRepository struct {
	db *bun.DB
}

func (r *UserRepository) DB() *bun.DB {
	return r.db
}

func (r *UserRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// TestNewDI_RegisterSingletonWith_SQL tests the new typed injection pattern with SQL database
func TestNewDI_RegisterSingletonWith_SQL(t *testing.T) {
	// Skip if running short tests (requires SQLite)
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Add database extension with SQLite (in-memory for testing)
	// Set as default so SQLKey points to this database
	dbExt := NewExtension(
		WithDefault("test"), // Important: set this as the default database
		WithDatabase(DatabaseConfig{
			Name: "test",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	// Create a test app with database extension
	app := forge.New(
		forge.WithAppName("di-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	// Start the app FIRST (this initializes the database connections)
	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err, "Failed to start app")

	defer func() {
		_ = app.Stop(ctx)
	}()

	// First, resolve the DatabaseManager to ensure databases are opened
	manager, err := forge.Resolve[*DatabaseManager](app.Container(), ManagerKey)
	require.NoError(t, err, "Failed to resolve DatabaseManager")

	// Verify we can get *bun.DB directly
	directDB, err := manager.SQL("test")
	require.NoError(t, err, "Failed to get SQL from manager")
	assert.NotNil(t, directDB, "manager.SQL() should return non-nil")

	// Now register a UserRepository using the NEW typed injection pattern
	// We register AFTER app.Start() to ensure the database is available
	err = forge.RegisterSingletonWith[*UserRepository](app.Container(), "userRepository",
		forge.Inject[*bun.DB](SQLKey),
		func(db *bun.DB) (*UserRepository, error) {
			return &UserRepository{db: db}, nil
		},
	)
	require.NoError(t, err, "Failed to register UserRepository with new DI pattern")

	// Resolve the UserRepository
	repo, err := forge.Resolve[*UserRepository](app.Container(), "userRepository")
	require.NoError(t, err, "Failed to resolve UserRepository")

	// Verify the DB was injected correctly
	assert.NotNil(t, repo.DB(), "DB should not be nil")

	// Verify the DB is working
	err = repo.Ping(ctx)
	assert.NoError(t, err, "DB should be pingable")
}

// TestNewDI_RegisterSingletonWith_DatabaseManager tests injection with DatabaseManager
func TestNewDI_RegisterSingletonWith_DatabaseManager(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbExt := NewExtension(
		WithDefault("primary"),
		WithDatabase(DatabaseConfig{
			Name: "primary",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	app := forge.New(
		forge.WithAppName("di-manager-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err)

	defer func() {
		_ = app.Stop(ctx)
	}()

	// Service that depends on DatabaseManager
	type DBService struct {
		manager *DatabaseManager
	}

	err = forge.RegisterSingletonWith[*DBService](app.Container(), "dbService",
		forge.Inject[*DatabaseManager](ManagerKey),
		func(manager *DatabaseManager) (*DBService, error) {
			return &DBService{manager: manager}, nil
		},
	)
	require.NoError(t, err)

	// Resolve the service
	svc, err := forge.Resolve[*DBService](app.Container(), "dbService")
	require.NoError(t, err)

	// Verify manager was injected
	assert.NotNil(t, svc.manager)

	// Verify we can get the database
	db, err := svc.manager.Get("primary")
	require.NoError(t, err)
	assert.NotNil(t, db)
	assert.Equal(t, "primary", db.Name())
}

// TestNewDI_RegisterSingletonWith_MultipleDeps tests multiple database dependencies
func TestNewDI_RegisterSingletonWith_MultipleDeps(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbExt := NewExtension(
		WithDefault("main"),
		WithDatabase(DatabaseConfig{
			Name: "main",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	app := forge.New(
		forge.WithAppName("di-multi-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err)

	defer func() {
		_ = app.Stop(ctx)
	}()

	// Service with multiple dependencies
	type AppService struct {
		db      *bun.DB
		manager *DatabaseManager
	}

	err = forge.RegisterSingletonWith[*AppService](app.Container(), "appService",
		forge.Inject[*bun.DB](SQLKey),
		forge.Inject[*DatabaseManager](ManagerKey),
		func(db *bun.DB, manager *DatabaseManager) (*AppService, error) {
			return &AppService{db: db, manager: manager}, nil
		},
	)
	require.NoError(t, err)

	// Resolve and verify
	svc, err := forge.Resolve[*AppService](app.Container(), "appService")
	require.NoError(t, err)

	assert.NotNil(t, svc.db, "bun.DB should be injected")
	assert.NotNil(t, svc.manager, "DatabaseManager should be injected")

	// Verify both are working
	err = svc.db.PingContext(ctx)
	assert.NoError(t, err, "DB should be pingable")

	_, err = svc.manager.Get("main")
	assert.NoError(t, err, "Manager should have 'main' database")
}

// TestNewDI_LazyInject_SQL tests lazy injection with SQL database
func TestNewDI_LazyInject_SQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbExt := NewExtension(
		WithDefault("lazy"),
		WithDatabase(DatabaseConfig{
			Name: "lazy",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	app := forge.New(
		forge.WithAppName("di-lazy-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err)

	defer func() {
		_ = app.Stop(ctx)
	}()

	// Service with lazy DB injection
	// Note: LazyInject returns *LazyAny (non-generic) because Go can't create
	// generic types at runtime. Use LazyAny in factory params and type assert after Get().
	type LazyDBService struct {
		dbLazy *forge.LazyAny
	}

	err = forge.RegisterSingletonWith[*LazyDBService](app.Container(), "lazyService",
		forge.LazyInject[*bun.DB](SQLKey),
		func(dbLazy *forge.LazyAny) (*LazyDBService, error) {
			return &LazyDBService{dbLazy: dbLazy}, nil
		},
	)
	require.NoError(t, err)

	// Resolve the service
	svc, err := forge.Resolve[*LazyDBService](app.Container(), "lazyService")
	require.NoError(t, err)

	// DB should not be resolved yet
	assert.False(t, svc.dbLazy.IsResolved(), "DB should not be resolved yet (lazy)")

	// Now get the DB (returns any, need to type assert)
	dbAny := svc.dbLazy.MustGet()
	db, ok := dbAny.(*bun.DB)
	require.True(t, ok, "Should be able to type assert to *bun.DB")
	assert.NotNil(t, db, "DB should be available after Get()")

	// Now it should be resolved
	assert.True(t, svc.dbLazy.IsResolved(), "DB should be resolved after Get()")

	// Verify it works
	err = db.PingContext(ctx)
	assert.NoError(t, err)
}

// TestNewDI_BackwardCompatible tests that old DI pattern still works
func TestNewDI_BackwardCompatible(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbExt := NewExtension(
		WithDefault("compat"),
		WithDatabase(DatabaseConfig{
			Name: "compat",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	app := forge.New(
		forge.WithAppName("di-backward-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err)

	defer func() {
		_ = app.Stop(ctx)
	}()

	// OLD pattern - still works!
	err = forge.RegisterSingleton(app.Container(), "oldStyleRepo", func(c forge.Container) (*UserRepository, error) {
		db, err := forge.Resolve[*bun.DB](c, SQLKey)
		if err != nil {
			return nil, err
		}
		return &UserRepository{db: db}, nil
	})
	require.NoError(t, err)

	// Resolve using old pattern
	repo, err := forge.Resolve[*UserRepository](app.Container(), "oldStyleRepo")
	require.NoError(t, err)

	assert.NotNil(t, repo.DB())
	err = repo.Ping(ctx)
	assert.NoError(t, err)
}

// TestNewDI_SQLResolveBeforeAppStart tests that services registered BEFORE app.Start()
// can correctly resolve SQLKey - this is the key fix for the database DI issue.
func TestNewDI_SQLResolveBeforeAppStart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbExt := NewExtension(
		WithDefault("prestart"),
		WithDatabase(DatabaseConfig{
			Name: "prestart",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	app := forge.New(
		forge.WithAppName("di-prestart-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	// Register a service BEFORE app.Start() that depends on SQLKey
	// This is the pattern that was previously failing
	err := forge.RegisterSingleton(app.Container(), "preStartRepo", func(c forge.Container) (*UserRepository, error) {
		// Use GetSQL helper which should ensure manager is started
		db, err := GetSQL(c)
		if err != nil {
			return nil, err
		}
		return &UserRepository{db: db}, nil
	})
	require.NoError(t, err)

	// Start the app - this should start DatabaseManager
	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer func() {
		_ = app.Stop(ctx)
	}()

	// Now resolve the service that was registered before start
	// The fix ensures DatabaseManager.Start() is called when SQLKey is resolved
	repo, err := forge.Resolve[*UserRepository](app.Container(), "preStartRepo")
	require.NoError(t, err, "Should resolve service that was registered before app.Start()")

	// Verify DB is not nil and works
	assert.NotNil(t, repo.DB(), "DB should not be nil after app.Start()")
	err = repo.Ping(ctx)
	assert.NoError(t, err, "DB should be pingable")
}

// TestNewDI_HelperFunctionsEnsureManagerStarted tests that all helper functions
// properly ensure the DatabaseManager is started before returning
func TestNewDI_HelperFunctionsEnsureManagerStarted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbExt := NewExtension(
		WithDefault("helper-test"),
		WithDatabase(DatabaseConfig{
			Name: "helper-test",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	app := forge.New(
		forge.WithAppName("di-helper-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err)

	defer func() {
		_ = app.Stop(ctx)
	}()

	// Test GetSQL helper
	db, err := GetSQL(app.Container())
	require.NoError(t, err, "GetSQL should work after app.Start()")
	assert.NotNil(t, db, "GetSQL should return non-nil *bun.DB")
	err = db.PingContext(ctx)
	assert.NoError(t, err, "Database should be pingable")

	// Test MustGetSQL helper
	db2 := MustGetSQL(app.Container())
	assert.NotNil(t, db2, "MustGetSQL should return non-nil *bun.DB")

	// Test GetDatabase helper
	database, err := GetDatabase(app.Container())
	require.NoError(t, err, "GetDatabase should work")
	assert.NotNil(t, database, "GetDatabase should return non-nil Database")

	// Test GetManager helper
	manager, err := GetManager(app.Container())
	require.NoError(t, err, "GetManager should work")
	assert.NotNil(t, manager, "GetManager should return non-nil DatabaseManager")
}
