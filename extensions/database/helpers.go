package database

import (
	"fmt"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
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
// Returns error if not found or type assertion fails
func GetManager(c forge.Container) (*DatabaseManager, error) {
	return forge.Resolve[*DatabaseManager](c, ManagerKey)
}

// MustGetManager retrieves the DatabaseManager from the container
// Panics if not found or type assertion fails
func MustGetManager(c forge.Container) *DatabaseManager {
	return forge.Must[*DatabaseManager](c, ManagerKey)
}

// GetDatabase retrieves the default Database from the container
// Returns error if not found or type assertion fails
func GetDatabase(c forge.Container) (Database, error) {
	return forge.Resolve[Database](c, DatabaseKey)
}

// MustGetDatabase retrieves the default Database from the container
// Panics if not found or type assertion fails
func MustGetDatabase(c forge.Container) Database {
	return forge.Must[Database](c, DatabaseKey)
}

// GetSQL retrieves the default Bun SQL database from the container
// Returns error if not found, type assertion fails, or default is not a SQL database
func GetSQL(c forge.Container) (*bun.DB, error) {
	return forge.Resolve[*bun.DB](c, "db")
}

// MustGetSQL retrieves the default Bun SQL database from the container
// Panics if not found, type assertion fails, or default is not a SQL database
func MustGetSQL(c forge.Container) *bun.DB {
	return forge.Must[*bun.DB](c, "db")
}

// GetMongo retrieves the default MongoDB client from the container
// Returns error if not found, type assertion fails, or default is not MongoDB
func GetMongo(c forge.Container) (*mongo.Client, error) {
	return forge.Resolve[*mongo.Client](c, "mongo")
}

// MustGetMongo retrieves the default MongoDB client from the container
// Panics if not found, type assertion fails, or default is not MongoDB
func MustGetMongo(c forge.Container) *mongo.Client {
	return forge.Must[*mongo.Client](c, "mongo")
}

// =============================================================================
// App-based Helpers (Convenience wrappers)
// =============================================================================

// GetManagerFromApp retrieves the DatabaseManager from the app
// Returns error if not found or type assertion fails
func GetManagerFromApp(app forge.App) (*DatabaseManager, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetManager(app.Container())
}

// MustGetManagerFromApp retrieves the DatabaseManager from the app
// Panics if not found or type assertion fails
func MustGetManagerFromApp(app forge.App) *DatabaseManager {
	if app == nil {
		panic("app is nil")
	}
	return MustGetManager(app.Container())
}

// GetDatabaseFromApp retrieves the default Database from the app
// Returns error if not found or type assertion fails
func GetDatabaseFromApp(app forge.App) (Database, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetDatabase(app.Container())
}

// MustGetDatabaseFromApp retrieves the default Database from the app
// Panics if not found or type assertion fails
func MustGetDatabaseFromApp(app forge.App) Database {
	if app == nil {
		panic("app is nil")
	}
	return MustGetDatabase(app.Container())
}

// GetSQLFromApp retrieves the default Bun SQL database from the app
// Returns error if not found, type assertion fails, or default is not a SQL database
func GetSQLFromApp(app forge.App) (*bun.DB, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetSQL(app.Container())
}

// MustGetSQLFromApp retrieves the default Bun SQL database from the app
// Panics if not found, type assertion fails, or default is not a SQL database
func MustGetSQLFromApp(app forge.App) *bun.DB {
	if app == nil {
		panic("app is nil")
	}
	return MustGetSQL(app.Container())
}

// GetMongoFromApp retrieves the default MongoDB client from the app
// Returns error if not found, type assertion fails, or default is not MongoDB
func GetMongoFromApp(app forge.App) (*mongo.Client, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetMongo(app.Container())
}

// MustGetMongoFromApp retrieves the default MongoDB client from the app
// Panics if not found, type assertion fails, or default is not MongoDB
func MustGetMongoFromApp(app forge.App) *mongo.Client {
	if app == nil {
		panic("app is nil")
	}
	return MustGetMongo(app.Container())
}

// =============================================================================
// Named Database Helpers (Advanced usage)
// =============================================================================

// GetNamedDatabase retrieves a named database through the DatabaseManager
// This is useful when you have multiple databases configured
func GetNamedDatabase(c forge.Container, name string) (Database, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get database manager: %w", err)
	}
	return manager.Get(name)
}

// MustGetNamedDatabase retrieves a named database through the DatabaseManager
// Panics if manager not found or database not found
func MustGetNamedDatabase(c forge.Container, name string) Database {
	manager := MustGetManager(c)
	db, err := manager.Get(name)
	if err != nil {
		panic(fmt.Sprintf("failed to get database %s: %v", name, err))
	}
	return db
}

// GetNamedSQL retrieves a named SQL database as Bun DB
// Returns error if database not found or is not a SQL database
func GetNamedSQL(c forge.Container, name string) (*bun.DB, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get database manager: %w", err)
	}
	return manager.SQL(name)
}

// MustGetNamedSQL retrieves a named SQL database as Bun DB
// Panics if database not found or is not a SQL database
func MustGetNamedSQL(c forge.Container, name string) *bun.DB {
	manager := MustGetManager(c)
	db, err := manager.SQL(name)
	if err != nil {
		panic(fmt.Sprintf("failed to get SQL database %s: %v", name, err))
	}
	return db
}

// GetNamedMongo retrieves a named MongoDB database as mongo.Client
// Returns error if database not found or is not MongoDB
func GetNamedMongo(c forge.Container, name string) (*mongo.Client, error) {
	manager, err := GetManager(c)
	if err != nil {
		return nil, fmt.Errorf("failed to get database manager: %w", err)
	}
	return manager.Mongo(name)
}

// MustGetNamedMongo retrieves a named MongoDB database as mongo.Client
// Panics if database not found or is not MongoDB
func MustGetNamedMongo(c forge.Container, name string) *mongo.Client {
	manager := MustGetManager(c)
	client, err := manager.Mongo(name)
	if err != nil {
		panic(fmt.Sprintf("failed to get MongoDB database %s: %v", name, err))
	}
	return client
}

// =============================================================================
// App-based Named Database Helpers (Convenience)
// =============================================================================

// GetNamedDatabaseFromApp retrieves a named database from the app
func GetNamedDatabaseFromApp(app forge.App, name string) (Database, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetNamedDatabase(app.Container(), name)
}

// MustGetNamedDatabaseFromApp retrieves a named database from the app
// Panics if not found
func MustGetNamedDatabaseFromApp(app forge.App, name string) Database {
	if app == nil {
		panic("app is nil")
	}
	return MustGetNamedDatabase(app.Container(), name)
}

// GetNamedSQLFromApp retrieves a named SQL database from the app
func GetNamedSQLFromApp(app forge.App, name string) (*bun.DB, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetNamedSQL(app.Container(), name)
}

// MustGetNamedSQLFromApp retrieves a named SQL database from the app
// Panics if not found
func MustGetNamedSQLFromApp(app forge.App, name string) *bun.DB {
	if app == nil {
		panic("app is nil")
	}
	return MustGetNamedSQL(app.Container(), name)
}

// GetNamedMongoFromApp retrieves a named MongoDB database from the app
func GetNamedMongoFromApp(app forge.App, name string) (*mongo.Client, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	return GetNamedMongo(app.Container(), name)
}

// MustGetNamedMongoFromApp retrieves a named MongoDB database from the app
// Panics if not found
func MustGetNamedMongoFromApp(app forge.App, name string) *mongo.Client {
	if app == nil {
		panic("app is nil")
	}
	return MustGetNamedMongo(app.Container(), name)
}
