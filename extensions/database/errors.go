package database

import "errors"

var (
	// Configuration errors
	ErrNoDatabasesConfigured = errors.New("no databases configured")
	ErrInvalidDatabaseName   = errors.New("invalid database name")
	ErrInvalidDatabaseType   = errors.New("invalid database type")
	ErrInvalidDSN            = errors.New("invalid DSN")
	ErrInvalidPoolConfig     = errors.New("invalid connection pool configuration")

	// Runtime errors
	ErrDatabaseNotFound      = errors.New("database not found")
	ErrDatabaseAlreadyExists = errors.New("database already exists")
	ErrDatabaseNotOpened     = errors.New("database not opened")
	ErrInvalidDatabaseTypeOp = errors.New("invalid database type for operation")
)
