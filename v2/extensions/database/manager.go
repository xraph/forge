package database

import (
	"context"
	"fmt"
	"sync"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/internal/logger"
)

// DatabaseManager manages multiple database connections
type DatabaseManager struct {
	databases map[string]Database
	logger    forge.Logger
	metrics   forge.Metrics
	mu        sync.RWMutex
}

// NewDatabaseManager creates a new database manager
func NewDatabaseManager(logger forge.Logger, metrics forge.Metrics) *DatabaseManager {
	return &DatabaseManager{
		databases: make(map[string]Database),
		logger:    logger,
		metrics:   metrics,
	}
}

// Register adds a database to the manager
func (m *DatabaseManager) Register(name string, db Database) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.databases[name]; exists {
		return fmt.Errorf("database %s already registered", name)
	}

	m.databases[name] = db
	m.logger.Info("database registered", logger.String("name", name), logger.String("type", string(db.Type())))

	return nil
}

// Get retrieves a database by name
func (m *DatabaseManager) Get(name string) (Database, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	db, exists := m.databases[name]
	if !exists {
		return nil, fmt.Errorf("database %s not found", name)
	}

	return db, nil
}

// SQL retrieves an SQL database with Bun ORM by name
func (m *DatabaseManager) SQL(name string) (*bun.DB, error) {
	db, err := m.Get(name)
	if err != nil {
		return nil, err
	}

	sqlDB, ok := db.(*SQLDatabase)
	if !ok {
		return nil, fmt.Errorf("database %s is not a SQL database", name)
	}

	return sqlDB.Bun(), nil
}

// Mongo retrieves a MongoDB client by name
func (m *DatabaseManager) Mongo(name string) (*mongo.Client, error) {
	db, err := m.Get(name)
	if err != nil {
		return nil, err
	}

	mongoDB, ok := db.(*MongoDatabase)
	if !ok {
		return nil, fmt.Errorf("database %s is not a MongoDB database", name)
	}

	return mongoDB.Client(), nil
}

// MongoDatabase retrieves a MongoDB database wrapper by name
func (m *DatabaseManager) MongoDatabase(name string) (*MongoDatabase, error) {
	db, err := m.Get(name)
	if err != nil {
		return nil, err
	}

	mongoDB, ok := db.(*MongoDatabase)
	if !ok {
		return nil, fmt.Errorf("database %s is not a MongoDB database", name)
	}

	return mongoDB, nil
}

// OpenAll opens all registered databases
func (m *DatabaseManager) OpenAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error
	for name, db := range m.databases {
		if err := db.Open(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to open databases: %v", errs)
	}

	return nil
}

// CloseAll closes all registered databases
func (m *DatabaseManager) CloseAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error
	for name, db := range m.databases {
		if err := db.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to close databases: %v", errs)
	}

	return nil
}

// HealthCheckAll performs health checks on all databases
func (m *DatabaseManager) HealthCheckAll(ctx context.Context) map[string]HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make(map[string]HealthStatus)
	for name, db := range m.databases {
		statuses[name] = db.Health(ctx)
	}

	return statuses
}

// List returns the names of all registered databases
func (m *DatabaseManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.databases))
	for name := range m.databases {
		names = append(names, name)
	}

	return names
}
