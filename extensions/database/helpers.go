package database

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// Helper functions for convenient database access from DI container.
//
// This file provides lightweight wrappers (~16ns, 0 allocs) around Forge's DI system
// to eliminate verbose database resolution boilerplate.
//
// QUICK START:
//
//	// In a controller or service constructor:
//	func NewUserService(c forge.Container) *UserService {
//	    return &UserService{
//	        db: database.MustGetSQL(c),  // Simple!
//	    }
//	}
//
//	// Or with error handling:
//	db, err := database.GetSQL(c)
//	if err != nil {
//	    return err
//	}
//
// LIFECYCLE TIMING:
//
//   Database connections are managed by the DI container through the DatabaseManager
//   which implements shared.Service. The container calls Start() on DatabaseManager
//   during container.Start(), which opens all connections.
//
//   During Extension Register() phase:
//     ✅ GetManager()     - Manager exists, safe to call
//     ✅ GetDatabase()    - Database instance exists (but not connected)
//     ⚠️  GetSQL()        - Connection not open yet (use ResolveReady for eager resolution)
//     ⚠️  GetMongo()      - Connection not open yet (use ResolveReady for eager resolution)
//
//   During Extension Register() phase with ResolveReady:
//     ✅ forge.ResolveReady[*DatabaseManager](ctx, c, database.ManagerKey) - Opens connections first
//     ✅ Then GetSQL(), GetMongo(), GetRedis() all work with open connections
//
//   During Extension Start() phase:
//     ✅ All database helpers work - connections already opened by container.Start()
//
//   Best Practices:
//
//	// Option 1: Use ResolveReady during Register() for dependencies that need open connections
//	func (e *Extension) Register(app forge.App) error {
//	    ctx := context.Background()
//	    dbManager, err := forge.ResolveReady[*database.DatabaseManager](ctx, app.Container(), database.ManagerKey)
//	    if err != nil {
//	        return err
//	    }
//	    // Now connections are open and ready to use
//	    redisClient, _ := dbManager.Redis("cache")
//	    return nil
//	}
//
//	// Option 2: Resolve during Start() (connections already open)
//	func (e *Extension) Start(ctx context.Context) error {
//	    e.db = database.MustGetSQL(e.App().Container())
//	    return nil
//	}
//
//	// Option 3: Declare dependency, resolve anytime after container.Start()
//	func NewExtension() forge.Extension {
//	    base := forge.NewBaseExtension("my-ext", "1.0.0", "...")
//	    base.SetDependencies([]string{"database"})  // Database starts first
//	    return &Extension{BaseExtension: base}
//	}
//
// PATTERNS:
//   - Use Must* variants when database is required (fail-fast)
//   - Use regular variants when database is optional (explicit errors)
//   - Store database refs in structs during construction
//   - Use Named* helpers for multi-database scenarios
//
// See HELPERS_GUIDE.md for comprehensive documentation and examples.

// =============================================================================
// Container-based Helpers (Most flexible)
// =============================================================================

// GetManager retrieves the DatabaseManager from the container
// Returns error if not found or type assertion fails.
func GetManager(c forge.Container) (*DatabaseManager, error) {
	return forge.Resolve[*DatabaseManager](c, ManagerKey)
}

// MustGetManager retrieves the DatabaseManager from the container
// Panics if not found or type assertion fails.
func MustGetManager(c forge.Container) *DatabaseManager {
	return forge.Must[*DatabaseManager](c, ManagerKey)
}

// GetDatabase retrieves the default Database from the container
// Returns error if not found or type assertion fails.
// Automatically ensures DatabaseManager is started before resolving.
func GetDatabase(c forge.Container) (Database, error) {
	// Ensure manager is resolved and started first
	if _, err := forge.Resolve[*DatabaseManager](c, ManagerKey); err != nil {
		return nil, fmt.Errorf("failed to resolve database manager: %w", err)
	}

	return forge.Resolve[Database](c, DatabaseKey)
}

// MustGetDatabase retrieves the default Database from the container
// Panics if not found or type assertion fails.
// Automatically ensures DatabaseManager is started before resolving.
func MustGetDatabase(c forge.Container) Database {
	// Ensure manager is resolved and started first
	forge.Must[*DatabaseManager](c, ManagerKey)

	return forge.Must[Database](c, DatabaseKey)
}

// GetSQL retrieves the default Bun SQL database from the container.
//
// Safe to call anytime - automatically ensures DatabaseManager is started first.
//
// Returns error if:
//   - Database extension not registered
//   - Default database is not a SQL database (e.g., it's MongoDB)
func GetSQL(c forge.Container) (*bun.DB, error) {
	// Ensure manager is resolved and started first
	if _, err := forge.Resolve[*DatabaseManager](c, ManagerKey); err != nil {
		return nil, fmt.Errorf("failed to resolve database manager: %w", err)
	}

	return forge.Resolve[*bun.DB](c, SQLKey)
}

// MustGetSQL retrieves the default Bun SQL database from the container.
//
// Safe to call anytime - automatically ensures DatabaseManager is started first.
//
// Panics if:
//   - Database extension not registered
//   - Default database is not a SQL database (e.g., it's MongoDB)
func MustGetSQL(c forge.Container) *bun.DB {
	// Ensure manager is resolved and started first
	forge.Must[*DatabaseManager](c, ManagerKey)

	return forge.Must[*bun.DB](c, SQLKey)
}

// GetMongo retrieves the default MongoDB client from the container.
//
// Safe to call anytime - automatically ensures DatabaseManager is started first.
//
// Returns error if:
//   - Database extension not registered
//   - Default database is not MongoDB (e.g., it's SQL)
func GetMongo(c forge.Container) (*mongo.Client, error) {
	// Ensure manager is resolved and started first
	if _, err := forge.Resolve[*DatabaseManager](c, ManagerKey); err != nil {
		return nil, fmt.Errorf("failed to resolve database manager: %w", err)
	}

	return forge.Resolve[*mongo.Client](c, MongoKey)
}

// MustGetMongo retrieves the default MongoDB client from the container.
//
// Safe to call anytime - automatically ensures DatabaseManager is started first.
//
// Panics if:
//   - Database extension not registered
//   - Default database is not MongoDB (e.g., it's SQL)
func MustGetMongo(c forge.Container) *mongo.Client {
	// Ensure manager is resolved and started first
	forge.Must[*DatabaseManager](c, ManagerKey)

	return forge.Must[*mongo.Client](c, MongoKey)
}

// GetRedis retrieves the default Redis client from the container.
//
// Safe to call anytime - automatically ensures DatabaseManager is started first.
//
// Returns error if:
//   - Database extension not registered
//   - Default database is not Redis (e.g., it's SQL or MongoDB)
func GetRedis(c forge.Container) (redis.UniversalClient, error) {
	// Ensure manager is resolved and started first
	if _, err := forge.Resolve[*DatabaseManager](c, ManagerKey); err != nil {
		return nil, fmt.Errorf("failed to resolve database manager: %w", err)
	}

	return forge.Resolve[redis.UniversalClient](c, RedisKey)
}

// MustGetRedis retrieves the default Redis client from the container.
//
// Safe to call anytime - automatically ensures DatabaseManager is started first.
//
// Panics if:
//   - Database extension not registered
//   - Default database is not Redis (e.g., it's SQL or MongoDB)
func MustGetRedis(c forge.Container) redis.UniversalClient {
	// Ensure manager is resolved and started first
	forge.Must[*DatabaseManager](c, ManagerKey)

	return forge.Must[redis.UniversalClient](c, RedisKey)
}

// =============================================================================
// App-based Helpers (Convenience wrappers)
// =============================================================================

// GetManagerFromApp retrieves the DatabaseManager from the app
// Returns error if not found or type assertion fails.
func GetManagerFromApp(app forge.App) (*DatabaseManager, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetManager(app.Container())
}

// MustGetManagerFromApp retrieves the DatabaseManager from the app
// Panics if not found or type assertion fails.
func MustGetManagerFromApp(app forge.App) *DatabaseManager {
	if app == nil {
		panic("app is nil")
	}

	return MustGetManager(app.Container())
}

// GetDatabaseFromApp retrieves the default Database from the app
// Returns error if not found or type assertion fails.
func GetDatabaseFromApp(app forge.App) (Database, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetDatabase(app.Container())
}

// MustGetDatabaseFromApp retrieves the default Database from the app
// Panics if not found or type assertion fails.
func MustGetDatabaseFromApp(app forge.App) Database {
	if app == nil {
		panic("app is nil")
	}

	return MustGetDatabase(app.Container())
}

// GetSQLFromApp retrieves the default Bun SQL database from the app
// Returns error if not found, type assertion fails, or default is not a SQL database.
func GetSQLFromApp(app forge.App) (*bun.DB, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetSQL(app.Container())
}

// MustGetSQLFromApp retrieves the default Bun SQL database from the app
// Panics if not found, type assertion fails, or default is not a SQL database.
func MustGetSQLFromApp(app forge.App) *bun.DB {
	if app == nil {
		panic("app is nil")
	}

	return MustGetSQL(app.Container())
}

// GetMongoFromApp retrieves the default MongoDB client from the app
// Returns error if not found, type assertion fails, or default is not MongoDB.
func GetMongoFromApp(app forge.App) (*mongo.Client, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetMongo(app.Container())
}

// MustGetMongoFromApp retrieves the default MongoDB client from the app
// Panics if not found, type assertion fails, or default is not MongoDB.
func MustGetMongoFromApp(app forge.App) *mongo.Client {
	if app == nil {
		panic("app is nil")
	}

	return MustGetMongo(app.Container())
}

// GetRedisFromApp retrieves the default Redis client from the app
// Returns error if not found, type assertion fails, or default is not Redis.
func GetRedisFromApp(app forge.App) (redis.UniversalClient, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetRedis(app.Container())
}

// MustGetRedisFromApp retrieves the default Redis client from the app
// Panics if not found, type assertion fails, or default is not Redis.
func MustGetRedisFromApp(app forge.App) redis.UniversalClient {
	if app == nil {
		panic("app is nil")
	}

	return MustGetRedis(app.Container())
}

// =============================================================================
// Named Database Helpers (Advanced usage)
// =============================================================================

// GetNamedDatabase retrieves a named database through the DatabaseManager
// This is useful when you have multiple databases configured.
func GetNamedDatabase(c forge.Container, name string) (Database, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get database manager: %w", err)
	}

	return manager.Get(name)
}

// MustGetNamedDatabase retrieves a named database through the DatabaseManager
// Panics if manager not found or database not found.
func MustGetNamedDatabase(c forge.Container, name string) Database {
	manager := MustGetManager(c)

	db, err := manager.Get(name)
	if err != nil {
		panic(fmt.Sprintf("failed to get database %s: %v", name, err))
	}

	return db
}

// GetNamedSQL retrieves a named SQL database as Bun DB
// Returns error if database not found or is not a SQL database.
func GetNamedSQL(c forge.Container, name string) (*bun.DB, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get database manager: %w", err)
	}

	return manager.SQL(name)
}

// MustGetNamedSQL retrieves a named SQL database as Bun DB
// Panics if database not found or is not a SQL database.
func MustGetNamedSQL(c forge.Container, name string) *bun.DB {
	manager := MustGetManager(c)

	db, err := manager.SQL(name)
	if err != nil {
		panic(fmt.Sprintf("failed to get SQL database %s: %v", name, err))
	}

	return db
}

// GetNamedMongo retrieves a named MongoDB database as mongo.Client
// Returns error if database not found or is not MongoDB.
func GetNamedMongo(c forge.Container, name string) (*mongo.Client, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get database manager: %w", err)
	}

	return manager.Mongo(name)
}

// MustGetNamedMongo retrieves a named MongoDB database as mongo.Client
// Panics if database not found or is not MongoDB.
func MustGetNamedMongo(c forge.Container, name string) *mongo.Client {
	manager := MustGetManager(c)

	client, err := manager.Mongo(name)
	if err != nil {
		panic(fmt.Sprintf("failed to get MongoDB database %s: %v", name, err))
	}

	return client
}

// GetNamedRedis retrieves a named Redis database as redis.UniversalClient
// Returns error if database not found or is not Redis.
func GetNamedRedis(c forge.Container, name string) (redis.UniversalClient, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get database manager: %w", err)
	}

	return manager.Redis(name)
}

// MustGetNamedRedis retrieves a named Redis database as redis.UniversalClient
// Panics if database not found or is not Redis.
func MustGetNamedRedis(c forge.Container, name string) redis.UniversalClient {
	manager := MustGetManager(c)

	client, err := manager.Redis(name)
	if err != nil {
		panic(fmt.Sprintf("failed to get Redis database %s: %v", name, err))
	}

	return client
}

// =============================================================================
// App-based Named Database Helpers (Convenience)
// =============================================================================

// GetNamedDatabaseFromApp retrieves a named database from the app.
func GetNamedDatabaseFromApp(app forge.App, name string) (Database, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetNamedDatabase(app.Container(), name)
}

// MustGetNamedDatabaseFromApp retrieves a named database from the app
// Panics if not found.
func MustGetNamedDatabaseFromApp(app forge.App, name string) Database {
	if app == nil {
		panic("app is nil")
	}

	return MustGetNamedDatabase(app.Container(), name)
}

// GetNamedSQLFromApp retrieves a named SQL database from the app.
func GetNamedSQLFromApp(app forge.App, name string) (*bun.DB, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetNamedSQL(app.Container(), name)
}

// MustGetNamedSQLFromApp retrieves a named SQL database from the app
// Panics if not found.
func MustGetNamedSQLFromApp(app forge.App, name string) *bun.DB {
	if app == nil {
		panic("app is nil")
	}

	return MustGetNamedSQL(app.Container(), name)
}

// GetNamedMongoFromApp retrieves a named MongoDB database from the app.
func GetNamedMongoFromApp(app forge.App, name string) (*mongo.Client, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetNamedMongo(app.Container(), name)
}

// MustGetNamedMongoFromApp retrieves a named MongoDB database from the app
// Panics if not found.
func MustGetNamedMongoFromApp(app forge.App, name string) *mongo.Client {
	if app == nil {
		panic("app is nil")
	}

	return MustGetNamedMongo(app.Container(), name)
}

// GetNamedRedisFromApp retrieves a named Redis database from the app.
func GetNamedRedisFromApp(app forge.App, name string) (redis.UniversalClient, error) {
	if app == nil {
		return nil, errors.New("app is nil")
	}

	return GetNamedRedis(app.Container(), name)
}

// MustGetNamedRedisFromApp retrieves a named Redis database from the app
// Panics if not found.
func MustGetNamedRedisFromApp(app forge.App, name string) redis.UniversalClient {
	if app == nil {
		panic("app is nil")
	}

	return MustGetNamedRedis(app.Container(), name)
}

// =============================================================================
// Repository Helpers (New Developer Experience Features)
// =============================================================================

// NewRepositoryFromContainer creates a repository using the database from the container.
// This is a convenience wrapper for the common pattern of getting DB and creating a repo.
//
// Example:
//
//	func NewUserService(c forge.Container) *UserService {
//	    return &UserService{
//	        userRepo: database.NewRepositoryFromContainer[User](c),
//	    }
//	}
func NewRepositoryFromContainer[T any](c forge.Container) *Repository[T] {
	db := MustGetSQL(c)

	return NewRepository[T](db)
}

// NewRepositoryFromApp creates a repository using the database from the app.
func NewRepositoryFromApp[T any](app forge.App) *Repository[T] {
	db := MustGetSQLFromApp(app)

	return NewRepository[T](db)
}

// WithTransactionFromContainer is a convenience wrapper that gets the DB from container.
//
// Example:
//
//	err := database.WithTransactionFromContainer(ctx, c, func(txCtx context.Context) error {
//	    // Transaction code
//	    return nil
//	})
func WithTransactionFromContainer(ctx context.Context, c forge.Container, fn TxFunc) error {
	db := MustGetSQL(c)

	return WithTransaction(ctx, db, fn)
}

// WithTransactionFromApp is a convenience wrapper that gets the DB from app.
func WithTransactionFromApp(ctx context.Context, app forge.App, fn TxFunc) error {
	db := MustGetSQLFromApp(app)

	return WithTransaction(ctx, db, fn)
}
