package database

import (
	"fmt"

	ierrors "github.com/xraph/forge/errors"
)

// Error codes for database operations.
const (
	CodeDatabaseError       = "DATABASE_ERROR"
	CodeDatabaseNotFound    = "DATABASE_NOT_FOUND"
	CodeDatabaseExists      = "DATABASE_ALREADY_EXISTS"
	CodeDatabaseNotOpened   = "DATABASE_NOT_OPENED"
	CodeDatabaseInvalidType = "DATABASE_INVALID_TYPE"
	CodeDatabaseConnection  = "DATABASE_CONNECTION_ERROR"
	CodeDatabaseQuery       = "DATABASE_QUERY_ERROR"
	CodeDatabaseTransaction = "DATABASE_TRANSACTION_ERROR"
	CodeDatabasePanic       = "DATABASE_PANIC_RECOVERED"
	CodeDatabaseConfig      = "DATABASE_CONFIG_ERROR"
)

// DatabaseError wraps database-specific errors with context.
type DatabaseError struct {
	DBName    string
	DBType    DatabaseType
	Operation string
	Code      string
	Err       error
}

func (e *DatabaseError) Error() string {
	if e.DBName != "" {
		return fmt.Sprintf("database %s (%s): %s: %v", e.DBName, e.DBType, e.Operation, e.Err)
	}

	return fmt.Sprintf("database (%s): %s: %v", e.DBType, e.Operation, e.Err)
}

func (e *DatabaseError) Unwrap() error {
	return e.Err
}

// NewDatabaseError creates a new database error with context.
func NewDatabaseError(dbName string, dbType DatabaseType, operation string, err error) *DatabaseError {
	return &DatabaseError{
		DBName:    dbName,
		DBType:    dbType,
		Operation: operation,
		Code:      CodeDatabaseError,
		Err:       err,
	}
}

// Error constructors for common database errors.
func ErrNoDatabasesConfigured() error {
	return ierrors.ErrConfigError("no databases configured", nil)
}

func ErrInvalidDatabaseName(name string) error {
	return ierrors.ErrValidationError("database.name", fmt.Errorf("invalid database name: %s", name))
}

func ErrInvalidDatabaseType(dbType string) error {
	return ierrors.ErrValidationError("database.type", fmt.Errorf("invalid database type: %s", dbType))
}

func ErrInvalidDSN(dsn string) error {
	return ierrors.ErrValidationError("database.dsn", ierrors.New("invalid or empty DSN"))
}

func ErrInvalidPoolConfig(reason string) error {
	return ierrors.ErrConfigError("invalid connection pool configuration: "+reason, nil)
}

func ErrDatabaseNotFound(name string) error {
	return &DatabaseError{
		DBName:    name,
		Operation: "get",
		Code:      CodeDatabaseNotFound,
		Err:       fmt.Errorf("database %s not found", name),
	}
}

func ErrDatabaseAlreadyExists(name string) error {
	return &DatabaseError{
		DBName:    name,
		Operation: "register",
		Code:      CodeDatabaseExists,
		Err:       fmt.Errorf("database %s already exists", name),
	}
}

func ErrDatabaseNotOpened(name string) error {
	return &DatabaseError{
		DBName:    name,
		Operation: "operation",
		Code:      CodeDatabaseNotOpened,
		Err:       fmt.Errorf("database %s not opened", name),
	}
}

func ErrInvalidDatabaseTypeOp(name string, expectedType, actualType DatabaseType) error {
	return &DatabaseError{
		DBName:    name,
		DBType:    actualType,
		Operation: "type_check",
		Code:      CodeDatabaseInvalidType,
		Err:       fmt.Errorf("expected %s database, got %s", expectedType, actualType),
	}
}

func ErrConnectionFailed(dbName string, dbType DatabaseType, cause error) error {
	return &DatabaseError{
		DBName:    dbName,
		DBType:    dbType,
		Operation: "connect",
		Code:      CodeDatabaseConnection,
		Err:       cause,
	}
}

func ErrQueryFailed(dbName string, dbType DatabaseType, cause error) error {
	return &DatabaseError{
		DBName:    dbName,
		DBType:    dbType,
		Operation: "query",
		Code:      CodeDatabaseQuery,
		Err:       cause,
	}
}

func ErrTransactionFailed(dbName string, dbType DatabaseType, cause error) error {
	return &DatabaseError{
		DBName:    dbName,
		DBType:    dbType,
		Operation: "transaction",
		Code:      CodeDatabaseTransaction,
		Err:       cause,
	}
}

func ErrPanicRecovered(dbName string, dbType DatabaseType, panicValue any) error {
	return &DatabaseError{
		DBName:    dbName,
		DBType:    dbType,
		Operation: "panic_recovery",
		Code:      CodeDatabasePanic,
		Err:       fmt.Errorf("panic recovered: %v", panicValue),
	}
}
