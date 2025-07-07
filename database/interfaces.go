package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Database represents a generic database interface
type Database interface {
	// Connection management
	Connect(ctx context.Context) error
	Close() error
	Ping(ctx context.Context) error

	// Health and status
	IsConnected() bool
	Stats() map[string]interface{}
}

// SQLDatabase represents a SQL database interface
type SQLDatabase interface {
	Database

	// Query execution
	Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)

	// Prepared statements
	Prepare(ctx context.Context, query string) (*sql.Stmt, error)

	// Transactions
	Begin(ctx context.Context) (Transaction, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Transaction, error)
	WithTransaction(ctx context.Context, fn func(Transaction) error) error

	// Helper methods
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error

	// Schema operations
	CreateTable(ctx context.Context, tableName string, schema TableSchema) error
	DropTable(ctx context.Context, tableName string) error
	TableExists(ctx context.Context, tableName string) (bool, error)

	// Migration support
	GetMigrationVersion(ctx context.Context) (int, error)
	SetMigrationVersion(ctx context.Context, version int) error

	// Connection pool
	SetMaxOpenConns(n int)
	SetMaxIdleConns(n int)
	SetConnMaxLifetime(d time.Duration)
	SetConnMaxIdleTime(d time.Duration)
}

// NoSQLDatabase represents a NoSQL database interface
type NoSQLDatabase interface {
	Database

	// Collection operations
	Collection(name string) Collection
	CreateCollection(ctx context.Context, name string) error
	DropCollection(ctx context.Context, name string) error
	ListCollections(ctx context.Context) ([]string, error)

	// Database operations
	Drop(ctx context.Context) error
}

// Cache represents a cache interface
type Cache interface {
	Database

	// Basic operations
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)

	// Advanced operations
	GetMulti(ctx context.Context, keys []string) (map[string][]byte, error)
	SetMulti(ctx context.Context, items map[string][]byte, ttl time.Duration) error
	DeleteMulti(ctx context.Context, keys []string) error

	// TTL operations
	Expire(ctx context.Context, key string, ttl time.Duration) error
	TTL(ctx context.Context, key string) (time.Duration, error)

	// Increment/Decrement
	Increment(ctx context.Context, key string, delta int64) (int64, error)
	Decrement(ctx context.Context, key string, delta int64) (int64, error)

	// Pattern operations
	Keys(ctx context.Context, pattern string) ([]string, error)
	Clear(ctx context.Context) error

	// JSON operations (for caches that support it)
	GetJSON(ctx context.Context, key string, dest interface{}) error
	SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error
}

// Transaction represents a database transaction
type Transaction interface {
	// Query execution
	Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)

	// Prepared statements
	Prepare(ctx context.Context, query string) (*sql.Stmt, error)

	// Transaction control
	Commit() error
	Rollback() error

	// Helper methods
	Select(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	Get(ctx context.Context, dest interface{}, query string, args ...interface{}) error
}

// Collection represents a NoSQL collection/table
type Collection interface {
	// CRUD operations
	FindOne(ctx context.Context, filter interface{}) (interface{}, error)
	Find(ctx context.Context, filter interface{}) (Cursor, error)
	Insert(ctx context.Context, document interface{}) (interface{}, error)
	InsertMany(ctx context.Context, documents []interface{}) ([]interface{}, error)
	Update(ctx context.Context, filter interface{}, update interface{}) (int64, error)
	UpdateOne(ctx context.Context, filter interface{}, update interface{}) error
	Delete(ctx context.Context, filter interface{}) (int64, error)
	DeleteOne(ctx context.Context, filter interface{}) error

	// Aggregation
	Aggregate(ctx context.Context, pipeline interface{}) (Cursor, error)
	Count(ctx context.Context, filter interface{}) (int64, error)

	// Indexing
	CreateIndex(ctx context.Context, keys interface{}, opts *IndexOptions) error
	DropIndex(ctx context.Context, name string) error
	ListIndexes(ctx context.Context) ([]IndexInfo, error)

	// Bulk operations
	BulkWrite(ctx context.Context, operations []interface{}) error
}

// Cursor represents a database cursor for iterating over results
type Cursor interface {
	// Iteration
	Next(ctx context.Context) bool
	Decode(dest interface{}) error
	All(ctx context.Context, dest interface{}) error
	Close(ctx context.Context) error

	// Current document
	Current() interface{}
	Err() error
}

// IndexOptions represents options for creating an index
type IndexOptions struct {
	Name        *string
	Unique      *bool
	Background  *bool
	Sparse      *bool
	ExpireAfter *time.Duration
}

// IndexInfo represents information about an index
type IndexInfo struct {
	Name   string
	Keys   map[string]interface{}
	Unique bool
}

// Configuration types

// Config represents database configuration
type Config struct {
	Name  string       `mapstructure:"name" yaml:"name"`
	Type  string       `mapstructure:"type" yaml:"type"` // sql, nosql, cache
	SQL   *SQLConfig   `mapstructure:"sql" yaml:"sql,omitempty"`
	NoSQL *NoSQLConfig `mapstructure:"nosql" yaml:"nosql,omitempty"`
	Cache *CacheConfig `mapstructure:"cache" yaml:"cache,omitempty"`
}

// SQLConfig represents SQL database configuration
type SQLConfig struct {
	Driver          string        `mapstructure:"driver" yaml:"driver"` // postgres, mysql, sqlite
	DSN             string        `mapstructure:"dsn" yaml:"dsn"`
	Host            string        `mapstructure:"host" yaml:"host"`
	Port            int           `mapstructure:"port" yaml:"port"`
	Database        string        `mapstructure:"database" yaml:"database"`
	Username        string        `mapstructure:"username" yaml:"username"`
	Password        string        `mapstructure:"password" yaml:"password"`
	SSLMode         string        `mapstructure:"ssl_mode" yaml:"ssl_mode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns" yaml:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime" yaml:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time" yaml:"conn_max_idle_time"`
	MigrationsPath  string        `mapstructure:"migrations_path" yaml:"migrations_path"`
	AutoMigrate     bool          `mapstructure:"auto_migrate" yaml:"auto_migrate"`
}

// NoSQLConfig represents NoSQL database configuration
type NoSQLConfig struct {
	Driver   string `mapstructure:"driver" yaml:"driver"` // mongodb, dynamodb
	URL      string `mapstructure:"url" yaml:"url"`
	Host     string `mapstructure:"host" yaml:"host"`
	Port     int    `mapstructure:"port" yaml:"port"`
	Database string `mapstructure:"database" yaml:"database"`
	Username string `mapstructure:"username" yaml:"username"`
	Password string `mapstructure:"password" yaml:"password"`
	AuthDB   string `mapstructure:"auth_db" yaml:"auth_db"`

	// MongoDB specific
	ReplicaSet     string        `mapstructure:"replica_set" yaml:"replica_set"`
	ConnectTimeout time.Duration `mapstructure:"connect_timeout" yaml:"connect_timeout"`
	SocketTimeout  time.Duration `mapstructure:"socket_timeout" yaml:"socket_timeout"`
	MaxPoolSize    int           `mapstructure:"max_pool_size" yaml:"max_pool_size"`
	MinPoolSize    int           `mapstructure:"min_pool_size" yaml:"min_pool_size"`
	MaxIdleTimeMS  time.Duration `mapstructure:"max_idle_time_ms" yaml:"max_idle_time_ms"`

	// DynamoDB specific
	Region    string `mapstructure:"region" yaml:"region"`
	AccessKey string `mapstructure:"access_key" yaml:"access_key"`
	SecretKey string `mapstructure:"secret_key" yaml:"secret_key"`
	Endpoint  string `mapstructure:"endpoint" yaml:"endpoint"` // for local development
}

// CacheConfig represents cache configuration
type CacheConfig struct {
	Driver   string `mapstructure:"driver" yaml:"driver"` // redis, memory, memcached
	URL      string `mapstructure:"url" yaml:"url"`
	Host     string `mapstructure:"host" yaml:"host"`
	Port     int    `mapstructure:"port" yaml:"port"`
	Database int    `mapstructure:"database" yaml:"database"`
	Username string `mapstructure:"username" yaml:"username"`
	Password string `mapstructure:"password" yaml:"password"`
	Prefix   string `mapstructure:"prefix" yaml:"prefix"`

	// Connection pool settings
	MaxRetries    int           `mapstructure:"max_retries" yaml:"max_retries"`
	PoolSize      int           `mapstructure:"pool_size" yaml:"pool_size"`
	MinIdleConns  int           `mapstructure:"min_idle_conns" yaml:"min_idle_conns"`
	MaxConnAge    time.Duration `mapstructure:"max_conn_age" yaml:"max_conn_age"`
	PoolTimeout   time.Duration `mapstructure:"pool_timeout" yaml:"pool_timeout"`
	IdleTimeout   time.Duration `mapstructure:"idle_timeout" yaml:"idle_timeout"`
	IdleCheckFreq time.Duration `mapstructure:"idle_check_freq" yaml:"idle_check_freq"`

	// Memory cache specific
	MaxSize         int64         `mapstructure:"max_size" yaml:"max_size"`                 // in bytes
	MaxItems        int64         `mapstructure:"max_items" yaml:"max_items"`               // max number of items
	DefaultTTL      time.Duration `mapstructure:"default_ttl" yaml:"default_ttl"`           // default TTL for items
	CleanupInterval time.Duration `mapstructure:"cleanup_interval" yaml:"cleanup_interval"` // cleanup frequency
}

// TableSchema represents a SQL table schema
type TableSchema struct {
	Columns     []ColumnDefinition `json:"columns"`
	PrimaryKey  []string           `json:"primary_key,omitempty"`
	ForeignKeys []ForeignKey       `json:"foreign_keys,omitempty"`
	Indexes     []Index            `json:"indexes,omitempty"`
	Constraints []Constraint       `json:"constraints,omitempty"`
}

// ColumnDefinition represents a table column
type ColumnDefinition struct {
	Name          string      `json:"name"`
	Type          string      `json:"type"`
	Size          int         `json:"size,omitempty"`
	Precision     int         `json:"precision,omitempty"`
	Scale         int         `json:"scale,omitempty"`
	NotNull       bool        `json:"not_null,omitempty"`
	AutoIncrement bool        `json:"auto_increment,omitempty"`
	DefaultValue  interface{} `json:"default_value,omitempty"`
	Comment       string      `json:"comment,omitempty"`
}

// ForeignKey represents a foreign key constraint
type ForeignKey struct {
	Name              string   `json:"name"`
	Columns           []string `json:"columns"`
	ReferencedTable   string   `json:"referenced_table"`
	ReferencedColumns []string `json:"referenced_columns"`
	OnDelete          string   `json:"on_delete,omitempty"` // CASCADE, SET NULL, RESTRICT, etc.
	OnUpdate          string   `json:"on_update,omitempty"`
}

// Index represents a database index
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique,omitempty"`
	Type    string   `json:"type,omitempty"` // BTREE, HASH, etc.
}

// Constraint represents a table constraint
type Constraint struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // CHECK, UNIQUE, etc.
	Expression string   `json:"expression,omitempty"`
	Columns    []string `json:"columns,omitempty"`
}

// Migration represents a database migration
type Migration interface {
	Version() int
	Name() string
	Up(db SQLDatabase) error
	Down(db SQLDatabase) error
}

// Migrator handles database migrations
type Migrator interface {
	// Migration management
	Add(migration Migration) error
	Remove(version int) error
	List() []Migration

	// Migration execution
	Up(ctx context.Context) error
	Down(ctx context.Context) error
	UpTo(ctx context.Context, version int) error
	DownTo(ctx context.Context, version int) error

	// Migration status
	Status(ctx context.Context) ([]MigrationStatus, error)
	CurrentVersion(ctx context.Context) (int, error)

	// Utility
	Reset(ctx context.Context) error
	Validate(ctx context.Context) error
}

// MigrationStatus represents the status of a migration
type MigrationStatus struct {
	Version   int        `json:"version"`
	Name      string     `json:"name"`
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

// ConnectionPool manages database connections
type ConnectionPool interface {
	// Pool management
	Get(ctx context.Context) (interface{}, error)
	Put(conn interface{}) error
	Close() error

	// Pool statistics
	Stats() PoolStats

	// Pool configuration
	SetMaxOpenConns(n int)
	SetMaxIdleConns(n int)
	SetConnMaxLifetime(d time.Duration)
	SetConnMaxIdleTime(d time.Duration)
}

// PoolStats represents connection pool statistics
type PoolStats struct {
	OpenConnections int `json:"open_connections"`
	InUse           int `json:"in_use"`
	Idle            int `json:"idle"`
	WaitCount       int `json:"wait_count"`
	WaitDuration    int `json:"wait_duration"`
}

// Factory function declarations (implemented in specific database files)
var (
	// SQL database constructors
	NewPostgresDatabase  func(config SQLConfig) (SQLDatabase, error)
	NewMySQLDatabase     func(config SQLConfig) (SQLDatabase, error)
	NewSQLiteDatabase    func(config SQLConfig) (SQLDatabase, error)
	NewCockroachDatabase func(config SQLConfig) (SQLDatabase, error)

	// NoSQL database constructors
	NewMongoDatabase     func(config NoSQLConfig) (NoSQLDatabase, error)
	NewDynamoDatabase    func(config NoSQLConfig) (NoSQLDatabase, error)
	NewArangoDatabase    func(config NoSQLConfig) (NoSQLDatabase, error)
	NewCassandraDatabase func(config NoSQLConfig) (NoSQLDatabase, error)
	NewScyllaDatabase    func(config NoSQLConfig) (NoSQLDatabase, error)
	NewDgraphDatabase    func(config NoSQLConfig) (NoSQLDatabase, error)
	NewSurrealDatabase   func(config NoSQLConfig) (NoSQLDatabase, error)

	// Cache constructors
	NewRedisCache     func(config CacheConfig) (Cache, error)
	NewMemoryCache    func(config CacheConfig) (Cache, error)
	NewMemcachedCache func(config CacheConfig) (Cache, error)
	NewBadgerCache    func(config CacheConfig) (Cache, error)
	NewNATSKVCache    func(config CacheConfig) (Cache, error)

	// Time series database constructors
	NewOpenTSDBDatabase   func(config NoSQLConfig) (NoSQLDatabase, error)
	NewClickHouseDatabase func(config NoSQLConfig) (NoSQLDatabase, error)

	// Search database constructors
	NewElasticsearchDatabase func(config NoSQLConfig) (NoSQLDatabase, error)
	NewSolrDatabase          func(config NoSQLConfig) (NoSQLDatabase, error)
)

// Factory functions

// NewSQLDatabase creates a new SQL database instance
func NewSQLDatabase(config SQLConfig) (SQLDatabase, error) {
	switch config.Driver {
	case "postgres", "pgx":
		if NewPostgresDatabase == nil {
			return nil, fmt.Errorf("postgres driver not initialized")
		}
		return NewPostgresDatabase(config)
	case "mysql":
		if NewMySQLDatabase == nil {
			return nil, fmt.Errorf("mysql driver not initialized")
		}
		return NewMySQLDatabase(config)
	case "sqlite", "sqlite3":
		if NewSQLiteDatabase == nil {
			return nil, fmt.Errorf("sqlite driver not initialized")
		}
		return NewSQLiteDatabase(config)
	case "cockroach", "cockroachdb":
		if NewCockroachDatabase == nil {
			return nil, fmt.Errorf("cockroach driver not initialized")
		}
		return NewCockroachDatabase(config)
	default:
		return nil, fmt.Errorf("unsupported SQL driver: %s", config.Driver)
	}
}

// NewNoSQLDatabase creates a new NoSQL database instance
func NewNoSQLDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	switch config.Driver {
	case "mongodb", "mongo":
		if NewMongoDatabase == nil {
			return nil, fmt.Errorf("mongodb driver not initialized")
		}
		return NewMongoDatabase(config)
	case "dynamodb":
		if NewDynamoDatabase == nil {
			return nil, fmt.Errorf("dynamodb driver not initialized")
		}
		return NewDynamoDatabase(config)
	case "arangodb":
		if NewArangoDatabase == nil {
			return nil, fmt.Errorf("arangodb driver not initialized")
		}
		return NewArangoDatabase(config)
	case "cassandra":
		if NewCassandraDatabase == nil {
			return nil, fmt.Errorf("cassandra driver not initialized")
		}
		return NewCassandraDatabase(config)
	case "scylla", "scylladb":
		if NewScyllaDatabase == nil {
			return nil, fmt.Errorf("scylla driver not initialized")
		}
		return NewScyllaDatabase(config)
	case "dgraph":
		if NewDgraphDatabase == nil {
			return nil, fmt.Errorf("dgraph driver not initialized")
		}
		return NewDgraphDatabase(config)
	case "surrealdb":
		if NewSurrealDatabase == nil {
			return nil, fmt.Errorf("surrealdb driver not initialized")
		}
		return NewSurrealDatabase(config)
	default:
		return nil, fmt.Errorf("unsupported NoSQL driver: %s", config.Driver)
	}
}

// NewCache creates a new cache instance
func NewCache(config CacheConfig) (Cache, error) {
	switch config.Driver {
	case "redis":
		if NewRedisCache == nil {
			return nil, fmt.Errorf("redis driver not initialized")
		}
		return NewRedisCache(config)
	case "memory":
		if NewMemoryCache == nil {
			return nil, fmt.Errorf("memory cache not initialized")
		}
		return NewMemoryCache(config)
	case "memcached":
		if NewMemcachedCache == nil {
			return nil, fmt.Errorf("memcached driver not initialized")
		}
		return NewMemcachedCache(config)
	case "badger", "badgerdb":
		if NewBadgerCache == nil {
			return nil, fmt.Errorf("badger driver not initialized")
		}
		return NewBadgerCache(config)
	case "nats", "nats_kv":
		if NewNATSKVCache == nil {
			return nil, fmt.Errorf("nats kv driver not initialized")
		}
		return NewNATSKVCache(config)
	default:
		return nil, fmt.Errorf("unsupported cache driver: %s", config.Driver)
	}
}

// Additional database factory functions for specialized databases

// NewTimeSeriesDatabase creates a new time series database instance
func NewTimeSeriesDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	switch config.Driver {
	case "opentsdb":
		if NewOpenTSDBDatabase == nil {
			return nil, fmt.Errorf("opentsdb driver not initialized")
		}
		return NewOpenTSDBDatabase(config)
	case "clickhouse":
		if NewClickHouseDatabase == nil {
			return nil, fmt.Errorf("clickhouse driver not initialized")
		}
		return NewClickHouseDatabase(config)
	default:
		return nil, fmt.Errorf("unsupported time series driver: %s", config.Driver)
	}
}

// NewSearchDatabase creates a new search database instance
func NewSearchDatabase(config NoSQLConfig) (NoSQLDatabase, error) {
	switch config.Driver {
	case "elasticsearch":
		if NewElasticsearchDatabase == nil {
			return nil, fmt.Errorf("elasticsearch driver not initialized")
		}
		return NewElasticsearchDatabase(config)
	case "solr":
		if NewSolrDatabase == nil {
			return nil, fmt.Errorf("solr driver not initialized")
		}
		return NewSolrDatabase(config)
	default:
		return nil, fmt.Errorf("unsupported search driver: %s", config.Driver)
	}
}

// Error types
type DatabaseError struct {
	Op      string
	Driver  string
	Message string
	Err     error
}

func (e *DatabaseError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("database %s error in %s: %s: %v", e.Driver, e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("database %s error in %s: %s", e.Driver, e.Op, e.Message)
}

func (e *DatabaseError) Unwrap() error {
	return e.Err
}

// Database error types
var (
	ErrConnectionFailed     = &DatabaseError{Op: "connect", Message: "connection failed"}
	ErrConnectionLost       = &DatabaseError{Op: "ping", Message: "connection lost"}
	ErrTransactionAborted   = &DatabaseError{Op: "transaction", Message: "transaction aborted"}
	ErrMigrationFailed      = &DatabaseError{Op: "migrate", Message: "migration failed"}
	ErrInvalidConfiguration = &DatabaseError{Op: "config", Message: "invalid configuration"}
	ErrNotImplemented       = &DatabaseError{Op: "operation", Message: "not implemented"}
)
