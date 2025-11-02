package database

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
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
		return ErrDatabaseAlreadyExists(name)
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
		return nil, ErrDatabaseNotFound(name)
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
		return nil, ErrInvalidDatabaseTypeOp(name, TypePostgres, db.Type())
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
		return nil, ErrInvalidDatabaseTypeOp(name, TypeMongoDB, db.Type())
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
		return nil, ErrInvalidDatabaseTypeOp(name, TypeMongoDB, db.Type())
	}

	return mongoDB, nil
}

// MultiError represents multiple database errors
type MultiError struct {
	Errors map[string]error
}

func (e *MultiError) Error() string {
	if len(e.Errors) == 0 {
		return "no errors"
	}

	var msgs []string
	for name, err := range e.Errors {
		msgs = append(msgs, fmt.Sprintf("%s: %v", name, err))
	}
	return fmt.Sprintf("multiple database errors: %s", strings.Join(msgs, "; "))
}

// HasErrors returns true if there are any errors
func (e *MultiError) HasErrors() bool {
	return len(e.Errors) > 0
}

// OpenAll opens all registered databases, collecting errors without stopping
func (m *DatabaseManager) OpenAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	multiErr := &MultiError{Errors: make(map[string]error)}

	for name, db := range m.databases {
		if err := db.Open(ctx); err != nil {
			multiErr.Errors[name] = err
			m.logger.Error("failed to open database",
				logger.String("name", name),
				logger.String("type", string(db.Type())),
				logger.Error(err),
			)
			if m.metrics != nil {
				m.metrics.Counter("db_open_failures",
					"db", name,
					"type", string(db.Type()),
				).Inc()
			}
		} else {
			m.logger.Info("database opened successfully",
				logger.String("name", name),
				logger.String("type", string(db.Type())),
			)
		}
	}

	if multiErr.HasErrors() {
		return multiErr
	}
	return nil
}

// CloseAll closes all registered databases, collecting errors without stopping
func (m *DatabaseManager) CloseAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	multiErr := &MultiError{Errors: make(map[string]error)}

	for name, db := range m.databases {
		if err := db.Close(ctx); err != nil {
			multiErr.Errors[name] = err
			m.logger.Error("failed to close database",
				logger.String("name", name),
				logger.String("type", string(db.Type())),
				logger.Error(err),
			)
		} else {
			m.logger.Info("database closed successfully",
				logger.String("name", name),
			)
		}
	}

	if multiErr.HasErrors() {
		return multiErr
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

func (m *DatabaseManager) Health(ctx context.Context) error {
	statuses := m.HealthCheckAll(ctx)

	for name, status := range statuses {
		if !status.Healthy {
			return fmt.Errorf("database %s is unhealthy: %s", name, status.Message)
		}
	}

	return nil
}

var _ shared.HealthChecker = (*DatabaseManager)(nil)
