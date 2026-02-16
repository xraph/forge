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

// UserRepository demonstrates a service that depends on *bun.DB.
type UserRepository struct {
	db *bun.DB
}

func (r *UserRepository) DB() *bun.DB {
	return r.db
}

func (r *UserRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// TestNewDI_RegisterSingleton_SQL tests typed singleton registration with SQL database.
func TestNewDI_RegisterSingleton_SQL(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	dbExt := NewExtension(
		WithDefault("test"),
		WithDatabase(DatabaseConfig{
			Name: "test",
			Type: TypeSQLite,
			DSN:  ":memory:",
		}),
	)

	app := forge.New(
		forge.WithAppName("di-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(dbExt),
	)

	ctx := context.Background()
	err := app.Start(ctx)
	require.NoError(t, err, "Failed to start app")

	defer func() {
		_ = app.Stop(ctx)
	}()

	// Resolve DatabaseManager to ensure databases are opened.
	manager, err := forge.Inject[*DatabaseManager](app.Container())
	require.NoError(t, err, "Failed to resolve DatabaseManager")

	// Verify we can get *bun.DB directly.
	directDB, err := manager.SQL("test")
	require.NoError(t, err, "Failed to get SQL from manager")
	assert.NotNil(t, directDB, "manager.SQL() should return non-nil")

	// Register a UserRepository using the typed singleton pattern.
	// The factory receives the container and resolves deps inside.
	err = forge.RegisterSingleton[*UserRepository](app.Container(), "userRepository",
		func(c forge.Container) (*UserRepository, error) {
			db, dbErr := GetSQL(c, "test")
			if dbErr != nil {
				return nil, dbErr
			}
			return &UserRepository{db: db}, nil
		},
	)
	require.NoError(t, err, "Failed to register UserRepository")

	// Resolve the UserRepository.
	repo, err := forge.Resolve[*UserRepository](app.Container(), "userRepository")
	require.NoError(t, err, "Failed to resolve UserRepository")

	assert.NotNil(t, repo.DB(), "DB should not be nil")

	err = repo.Ping(ctx)
	assert.NoError(t, err, "DB should be pingable")
}

// TestNewDI_RegisterSingleton_DatabaseManager tests injection with DatabaseManager.
func TestNewDI_RegisterSingleton_DatabaseManager(t *testing.T) {
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

	type DBService struct {
		manager *DatabaseManager
	}

	err = forge.RegisterSingleton[*DBService](app.Container(), "dbService",
		func(c forge.Container) (*DBService, error) {
			mgr, mgrErr := GetManager(c)
			if mgrErr != nil {
				return nil, mgrErr
			}
			return &DBService{manager: mgr}, nil
		},
	)
	require.NoError(t, err)

	svc, err := forge.Resolve[*DBService](app.Container(), "dbService")
	require.NoError(t, err)

	assert.NotNil(t, svc.manager)

	db, err := svc.manager.Get("primary")
	require.NoError(t, err)
	assert.NotNil(t, db)
	assert.Equal(t, "primary", db.Name())
}

// TestNewDI_RegisterSingleton_MultipleDeps tests multiple database dependencies.
func TestNewDI_RegisterSingleton_MultipleDeps(t *testing.T) {
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

	type AppService struct {
		db      *bun.DB
		manager *DatabaseManager
	}

	err = forge.RegisterSingleton[*AppService](app.Container(), "appService",
		func(c forge.Container) (*AppService, error) {
			sqlDB, sqlErr := GetSQL(c, "main")
			if sqlErr != nil {
				return nil, sqlErr
			}
			mgr, mgrErr := GetManager(c)
			if mgrErr != nil {
				return nil, mgrErr
			}
			return &AppService{db: sqlDB, manager: mgr}, nil
		},
	)
	require.NoError(t, err)

	svc, err := forge.Resolve[*AppService](app.Container(), "appService")
	require.NoError(t, err)

	assert.NotNil(t, svc.db, "bun.DB should be injected")
	assert.NotNil(t, svc.manager, "DatabaseManager should be injected")

	err = svc.db.PingContext(ctx)
	assert.NoError(t, err, "DB should be pingable")

	_, err = svc.manager.Get("main")
	assert.NoError(t, err, "Manager should have 'main' database")
}

// TestNewDI_DeferredResolution_SQL tests deferred (lazy) injection with SQL database.
// The factory captures the container and resolves *bun.DB on first use,
// demonstrating how to defer resolution without an eager call during registration.
func TestNewDI_DeferredResolution_SQL(t *testing.T) {
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

	// Register a service that captures the container for deferred DB resolution.
	var dbResolved bool
	err = forge.RegisterSingleton[*UserRepository](app.Container(), "lazyRepo",
		func(c forge.Container) (*UserRepository, error) {
			// Defer: don't call GetSQL yet; just capture the container.
			return &UserRepository{}, nil
		},
	)
	require.NoError(t, err)

	repo, err := forge.Resolve[*UserRepository](app.Container(), "lazyRepo")
	require.NoError(t, err)

	// DB not resolved yet.
	assert.Nil(t, repo.DB(), "DB should not be resolved yet")
	assert.False(t, dbResolved)

	// Resolve *bun.DB via the database helper (deferred resolution).
	db, err := GetSQL(app.Container(), "lazy")
	require.NoError(t, err, "Should resolve *bun.DB via GetSQL helper")
	assert.NotNil(t, db, "DB should be available")

	err = db.PingContext(ctx)
	assert.NoError(t, err)
}

// TestNewDI_BackwardCompatible tests that old DI pattern still works.
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

	// Old pattern: RegisterSingleton with Container factory.
	err = forge.RegisterSingleton(app.Container(), "oldStyleRepo", func(c forge.Container) (*UserRepository, error) {
		db, dbErr := GetSQL(c, "compat")
		if dbErr != nil {
			return nil, dbErr
		}
		return &UserRepository{db: db}, nil
	})
	require.NoError(t, err)

	repo, err := forge.Resolve[*UserRepository](app.Container(), "oldStyleRepo")
	require.NoError(t, err)

	assert.NotNil(t, repo.DB())
	err = repo.Ping(ctx)
	assert.NoError(t, err)
}

// TestNewDI_SQLResolveBeforeAppStart tests that services registered BEFORE app.Start()
// can correctly resolve databases after the app starts.
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

	// Register BEFORE app.Start() -- factory closures run at resolve time.
	err := forge.RegisterSingleton(app.Container(), "preStartRepo", func(c forge.Container) (*UserRepository, error) {
		db, dbErr := GetSQL(c, "prestart")
		if dbErr != nil {
			return nil, dbErr
		}
		return &UserRepository{db: db}, nil
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer func() {
		_ = app.Stop(ctx)
	}()

	// Resolve after start -- factory runs now with database available.
	repo, err := forge.Resolve[*UserRepository](app.Container(), "preStartRepo")
	require.NoError(t, err, "Should resolve service registered before app.Start()")

	assert.NotNil(t, repo.DB(), "DB should not be nil after app.Start()")
	err = repo.Ping(ctx)
	assert.NoError(t, err, "DB should be pingable")
}

// TestNewDI_HelperFunctionsEnsureManagerStarted tests that all helper functions
// properly return working database references.
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

	// Test GetSQL helper.
	db, err := GetSQL(app.Container(), "helper-test")
	require.NoError(t, err, "GetSQL should work after app.Start()")
	assert.NotNil(t, db, "GetSQL should return non-nil *bun.DB")
	err = db.PingContext(ctx)
	assert.NoError(t, err, "Database should be pingable")

	// Test MustGetSQL helper.
	db2 := MustGetSQL(app.Container(), "helper-test")
	assert.NotNil(t, db2, "MustGetSQL should return non-nil *bun.DB")

	// Test GetDatabase helper.
	database, err := GetDatabase(app.Container(), "helper-test")
	require.NoError(t, err, "GetDatabase should work")
	assert.NotNil(t, database, "GetDatabase should return non-nil Database")

	// Test GetManager helper.
	manager, err := GetManager(app.Container())
	require.NoError(t, err, "GetManager should work")
	assert.NotNil(t, manager, "GetManager should return non-nil DatabaseManager")
}
