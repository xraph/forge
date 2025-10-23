package common

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DatabaseManager manages database connections and adapters
type DatabaseManager interface {
	Service // Integrates with Phase 1 service lifecycle

	// RegisterAdapter registers a new database adapter to the DatabaseManager.
	// Returns an error if the adapter is invalid or a duplicate.
	RegisterAdapter(adapter DatabaseAdapter) error

	// GetConnection retrieves a database connection by its name.
	//
	// Returns a Connection interface for the specified name, or an error if the connection could not be found or accessed.
	GetConnection(name string) (Connection, error)

	// GetDefaultConnection retrieves the default database connection as defined in the configuration. Returns an error if unavailable.
	GetDefaultConnection() (Connection, error)

	// ListConnections returns a list of all active connection names managed by the DatabaseManager.
	ListConnections() []string

	// GetConnections returns a list of all active connection names managed by the DatabaseManager.
	GetConnections() map[string]Connection

	// CloseConnection closes the connection with the specified name and releases associated resources.
	CloseConnection(name string) error

	// StartAll starts all registered database connections and adapters. It initializes resources in the provided context.
	StartAll(ctx context.Context) error

	// StopAll stops all active database connections managed by the DatabaseManager to free resources and ensure proper cleanup.
	StopAll(ctx context.Context) error

	// HealthCheckAll performs a health check on all registered database connections and returns an error if any fails.
	HealthCheckAll(ctx context.Context) error

	// SetConfig updates the database configuration for the manager. Returns an error if the configuration is invalid.
	SetConfig(config *DatabaseConfig) error

	// GetConfig returns the current database configuration managed by the DatabaseManager.
	GetConfig() *DatabaseConfig

	// GetStats retrieves the current statistics of the database manager, including connection and adapter information.
	GetStats() ManagerStats

	// GetConnectionStats retrieves the connection statistics for the specified connection name.
	// Returns a ConnectionStats object and an error if the operation fails.
	GetConnectionStats(name string) (*ConnectionStats, error)

	// Migrate applies any pending migrations to the specified database connection. Returns an error on failure.
	Migrate(ctx context.Context, connectionName string) error

	// MigrateAll applies database migrations to all registered connections using the specified migration configurations.
	MigrateAll(ctx context.Context) error
}

// Connection defines the interface for database connections
type Connection interface {
	Service // Integrates with Phase 1 service lifecycle

	// Connection management
	Name() string
	Type() string
	DB() any                           // Returns underlying database connection
	Connect(ctx context.Context) error // Connect to database
	Close(ctx context.Context) error   // Close database connection
	Ping(ctx context.Context) error    // Ping database
	IsConnected() bool                 // Check if connected

	// Transaction management
	Transaction(ctx context.Context, fn func(tx interface{}) error) error

	// Configuration
	Config() *ConnectionConfig // Get connection configuration
	ConnectionString() string  // Get connection string (sanitized)

	// Health and metrics
	Stats() ConnectionStats                        // Get connection statistics
	SetHealthCheckInterval(interval time.Duration) // Set health check interval
}

// DatabaseAdapter defines the interface for database adapters
type DatabaseAdapter interface {
	// Adapter information
	Name() string
	SupportedTypes() []string

	// Connection management
	Connect(ctx context.Context, config *ConnectionConfig) (Connection, error)
	ValidateConfig(config *ConnectionConfig) error

	// Migration support
	SupportsMigrations() bool
	Migrate(ctx context.Context, conn Connection, migrationsPath string) error

	// Health checking
	HealthCheck(ctx context.Context, conn Connection) error
}

// ConnectionStats represents connection statistics
type ConnectionStats struct {
	Name             string        `json:"name"`
	Type             string        `json:"type"`
	Connected        bool          `json:"connected"`
	ConnectedAt      time.Time     `json:"connected_at"`
	LastPing         time.Time     `json:"last_ping"`
	PingDuration     time.Duration `json:"ping_duration"`
	TransactionCount int64         `json:"transaction_count"`
	QueryCount       int64         `json:"query_count"`
	ErrorCount       int64         `json:"error_count"`
	LastError        string        `json:"last_error,omitempty"`
	OpenConnections  int           `json:"open_connections"`
	IdleConnections  int           `json:"idle_connections"`
	MaxOpenConns     int           `json:"max_open_conns"`
	MaxIdleConns     int           `json:"max_idle_conns"`
	ConnMaxLifetime  time.Duration `json:"conn_max_lifetime"`
	ConnMaxIdleTime  time.Duration `json:"conn_max_idle_time"`
}

// AdapterInfo contains information about a database adapter
type AdapterInfo struct {
	Name           string   `json:"name"`
	SupportedTypes []string `json:"supported_types"`
	Migrations     bool     `json:"supports_migrations"`
}

// ManagerStats represents database manager statistics
type ManagerStats struct {
	TotalConnections   int                        `json:"total_connections"`
	ActiveConnections  int                        `json:"active_connections"`
	RegisteredAdapters int                        `json:"registered_adapters"`
	StartedAt          time.Time                  `json:"started_at"`
	LastHealthCheck    time.Time                  `json:"last_health_check"`
	HealthCheckPassed  bool                       `json:"health_check_passed"`
	ConnectionStats    map[string]ConnectionStats `json:"connection_stats"`
	AdapterInfo        []AdapterInfo              `json:"adapter_info"`
	MigrationsEnabled  bool                       `json:"migrations_enabled"`
	LastMigrationRun   time.Time                  `json:"last_migration_run"`
	TotalMigrations    int                        `json:"total_migrations"`
}

// DatabaseConfig represents the overall database configuration
type DatabaseConfig struct {
	// Default connection settings
	Default        string                      `yaml:"default" json:"default"`                 // Default connection name
	Connections    map[string]ConnectionConfig `yaml:"connections" json:"connections"`         // Named connections
	MigrationsPath string                      `yaml:"migrations_path" json:"migrations_path"` // Path to migration files
	AutoMigrate    bool                        `yaml:"auto_migrate" json:"auto_migrate"`       // Auto-run migrations on start
	HealthCheck    HealthCheckConfig           `yaml:"health_check" json:"health_check"`       // Health check configuration
	Metrics        MetricsConfig               `yaml:"metrics" json:"metrics"`                 // Metrics configuration
}

// ConnectionConfig represents configuration for a database connection
type ConnectionConfig struct {
	Type     string                 `yaml:"type" json:"type" validate:"required"` // Database type (postgres, redis, mongodb)
	Host     string                 `yaml:"host" json:"host" validate:"required"` // Database host
	Port     int                    `yaml:"port" json:"port" validate:"required"` // Database port
	Database string                 `yaml:"database" json:"database"`             // Database name
	Username string                 `yaml:"username" json:"username"`             // Username
	Password string                 `yaml:"password" json:"password"`             // Password
	SSLMode  string                 `yaml:"ssl_mode" json:"ssl_mode"`             // SSL mode
	Timezone string                 `yaml:"timezone" json:"timezone"`             // Timezone
	Config   map[string]interface{} `yaml:"config" json:"config"`                 // Type-specific configuration
	Pool     PoolConfig             `yaml:"pool" json:"pool"`                     // Connection pool configuration
	Retry    RetryConfig            `yaml:"retry" json:"retry"`                   // Retry configuration
}

// PoolConfig represents connection pool configuration
type PoolConfig struct {
	MaxOpenConns    int           `yaml:"max_open_conns" json:"max_open_conns" default:"25"`          // Maximum open connections
	MaxIdleConns    int           `yaml:"max_idle_conns" json:"max_idle_conns" default:"10"`          // Maximum idle connections
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" json:"conn_max_lifetime" default:"1h"`    // Maximum connection lifetime
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time" json:"conn_max_idle_time" default:"30m"` // Maximum connection idle time
}

// RetryConfig represents retry configuration for database operations
type RetryConfig struct {
	MaxAttempts   int           `yaml:"max_attempts" json:"max_attempts" default:"3"`       // Maximum retry attempts
	InitialDelay  time.Duration `yaml:"initial_delay" json:"initial_delay" default:"100ms"` // Initial delay between retries
	MaxDelay      time.Duration `yaml:"max_delay" json:"max_delay" default:"5s"`            // Maximum delay between retries
	BackoffFactor float64       `yaml:"backoff_factor" json:"backoff_factor" default:"2.0"` // Exponential backoff factor
}

// HealthCheckConfig represents health check configuration
type HealthCheckConfig struct {
	Enabled            bool          `yaml:"enabled" json:"enabled" default:"true"`  // Enable health checks
	Interval           time.Duration `yaml:"interval" json:"interval" default:"30s"` // Health check interval
	Timeout            time.Duration `yaml:"timeout" json:"timeout" default:"5s"`    // Health check timeout
	HealthyThreshold   int           `yaml:"healthy_threshold" json:"healthy_threshold"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold" json:"unhealthy_threshold"`
}

// MetricsConfig represents metrics configuration
type MetricsConfig struct {
	Enabled            bool          `yaml:"enabled" json:"enabled" default:"true"`                           // Enable metrics collection
	CollectionInterval time.Duration `yaml:"collection_interval" json:"collection_interval" default:"15s"`    // Metrics collection interval
	SlowQueryThreshold time.Duration `yaml:"slow_query_threshold" json:"slow_query_threshold" default:"1s"`   // Slow query threshold
	EnableQueryMetrics bool          `yaml:"enable_query_metrics" json:"enable_query_metrics" default:"true"` // Enable per-query metrics
	EnablePoolMetrics  bool          `yaml:"enable_pool_metrics" json:"enable_pool_metrics" default:"true"`   // Enable connection pool metrics

	// EnableQueryLogging   bool          `yaml:"enable_query_logging" json:"enable_query_logging"`
	// EnablePoolMetrics    bool          `yaml:"enable_pool_metrics" json:"enable_pool_metrics"`
}

// PostgresConfig represents PostgreSQL-specific configuration
type PostgresConfig struct {
	ConnectionConfig `yaml:",inline"`
	SearchPath       string `yaml:"search_path" json:"search_path"`             // PostgreSQL search path
	ApplicationName  string `yaml:"application_name" json:"application_name"`   // Application name for connection
	StatementTimeout string `yaml:"statement_timeout" json:"statement_timeout"` // Statement timeout
	LockTimeout      string `yaml:"lock_timeout" json:"lock_timeout"`           // Lock timeout
}

// RedisConfig represents Redis-specific configuration
type RedisConfig struct {
	ConnectionConfig `yaml:",inline"`
	DB               int           `yaml:"db" json:"db" default:"0"`                                   // Redis database number
	DialTimeout      time.Duration `yaml:"dial_timeout" json:"dial_timeout" default:"5s"`              // Dial timeout
	ReadTimeout      time.Duration `yaml:"read_timeout" json:"read_timeout" default:"3s"`              // Read timeout
	WriteTimeout     time.Duration `yaml:"write_timeout" json:"write_timeout" default:"3s"`            // Write timeout
	PoolTimeout      time.Duration `yaml:"pool_timeout" json:"pool_timeout" default:"4s"`              // Pool timeout
	IdleTimeout      time.Duration `yaml:"idle_timeout" json:"idle_timeout" default:"5m"`              // Idle connection timeout
	MaxRetries       int           `yaml:"max_retries" json:"max_retries" default:"3"`                 // Maximum retries
	MinRetryBackoff  time.Duration `yaml:"min_retry_backoff" json:"min_retry_backoff" default:"8ms"`   // Minimum retry backoff
	MaxRetryBackoff  time.Duration `yaml:"max_retry_backoff" json:"max_retry_backoff" default:"512ms"` // Maximum retry backoff
}

// MongoDBConfig represents MongoDB-specific configuration
type MongoDBConfig struct {
	ConnectionConfig       `yaml:",inline"`
	ReplicaSet             string        `yaml:"replica_set" json:"replica_set"`                                         // Replica set name
	AuthSource             string        `yaml:"auth_source" json:"auth_source"`                                         // Authentication source
	ConnectTimeout         time.Duration `yaml:"connect_timeout" json:"connect_timeout" default:"10s"`                   // Connection timeout
	ServerSelectionTimeout time.Duration `yaml:"server_selection_timeout" json:"server_selection_timeout" default:"30s"` // Server selection timeout
	SocketTimeout          time.Duration `yaml:"socket_timeout" json:"socket_timeout" default:"5s"`                      // Socket timeout
	HeartbeatInterval      time.Duration `yaml:"heartbeat_interval" json:"heartbeat_interval" default:"10s"`             // Heartbeat interval
	Compressors            []string      `yaml:"compressors" json:"compressors"`                                         // Compression algorithms
	ZlibLevel              int           `yaml:"zlib_level" json:"zlib_level" default:"6"`                               // Zlib compression level
}

// ConnectionString generates a connection string for the given configuration
func (c *ConnectionConfig) ConnectionString() string {
	switch c.Type {
	case "postgres", "postgresql":
		return c.postgresConnectionString()
	case "redis":
		return c.redisConnectionString()
	case "mongodb", "mongo":
		return c.mongoConnectionString()
	default:
		return ""
	}
}

// postgresConnectionString generates a PostgreSQL connection string
func (c *ConnectionConfig) postgresConnectionString() string {
	connStr := ""
	if c.Host != "" {
		connStr += "host=" + c.Host + " "
	}
	if c.Port != 0 {
		connStr += fmt.Sprintf("port=%d ", c.Port)
	}
	if c.Database != "" {
		connStr += "dbname=" + c.Database + " "
	}
	if c.Username != "" {
		connStr += "user=" + c.Username + " "
	}
	if c.Password != "" {
		connStr += "password=" + c.Password + " "
	}
	if c.SSLMode != "" {
		connStr += "sslmode=" + c.SSLMode + " "
	} else {
		connStr += "sslmode=disable "
	}
	if c.Timezone != "" {
		connStr += "TimeZone=" + c.Timezone + " "
	}

	// Add any additional config parameters
	for key, value := range c.Config {
		if strVal, ok := value.(string); ok {
			connStr += fmt.Sprintf("%s=%s ", key, strVal)
		}
	}

	return strings.TrimSpace(connStr)
}

// redisConnectionString generates a Redis connection string
func (c *ConnectionConfig) redisConnectionString() string {
	if c.Password != "" {
		return fmt.Sprintf("redis://:%s@%s:%d", c.Password, c.Host, c.Port)
	}
	return fmt.Sprintf("redis://%s:%d", c.Host, c.Port)
}

// mongoConnectionString generates a MongoDB connection string
func (c *ConnectionConfig) mongoConnectionString() string {
	connStr := "mongodb://"

	if c.Username != "" && c.Password != "" {
		connStr += fmt.Sprintf("%s:%s@", c.Username, c.Password)
	}

	connStr += fmt.Sprintf("%s:%d", c.Host, c.Port)

	if c.Database != "" {
		connStr += "/" + c.Database
	}

	// Add query parameters
	params := make([]string, 0)

	if replicaSet, ok := c.Config["replica_set"].(string); ok && replicaSet != "" {
		params = append(params, "replicaSet="+replicaSet)
	}

	if authSource, ok := c.Config["auth_source"].(string); ok && authSource != "" {
		params = append(params, "authSource="+authSource)
	}

	if len(params) > 0 {
		connStr += "?" + strings.Join(params, "&")
	}

	return connStr
}

// Validate validates the database configuration
func (dc *DatabaseConfig) Validate() error {
	if dc.Default == "" {
		return fmt.Errorf("default connection name is required")
	}

	if len(dc.Connections) == 0 {
		return fmt.Errorf("at least one connection must be configured")
	}

	if _, exists := dc.Connections[dc.Default]; !exists {
		return fmt.Errorf("default connection '%s' not found in connections", dc.Default)
	}

	for name, conn := range dc.Connections {
		if err := conn.Validate(); err != nil {
			return fmt.Errorf("invalid configuration for connection '%s': %w", name, err)
		}
	}

	return nil
}

// Validate validates a connection configuration
func (c *ConnectionConfig) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("database type is required")
	}

	if c.Host == "" {
		return fmt.Errorf("host is required")
	}

	if c.Port <= 0 {
		return fmt.Errorf("port must be greater than 0")
	}

	// Type-specific validation
	switch c.Type {
	case "postgres", "postgresql":
		if c.Database == "" {
			return fmt.Errorf("database name is required for PostgreSQL")
		}
		if c.Username == "" {
			return fmt.Errorf("username is required for PostgreSQL")
		}
	case "mongodb", "mongo":
		if c.Database == "" {
			return fmt.Errorf("database name is required for MongoDB")
		}
	case "redis":
		// Redis validation - database number should be valid
		if db, ok := c.Config["db"].(int); ok && (db < 0 || db > 15) {
			return fmt.Errorf("redis database number must be between 0 and 15")
		}
	default:
		return fmt.Errorf("unsupported database type: %s", c.Type)
	}

	return nil
}

// GetTypedConfig returns typed configuration for specific database types
func (c *ConnectionConfig) GetTypedConfig() interface{} {
	switch c.Type {
	case "postgres", "postgresql":
		config := &PostgresConfig{
			ConnectionConfig: *c,
		}
		if searchPath, ok := c.Config["search_path"].(string); ok {
			config.SearchPath = searchPath
		}
		if appName, ok := c.Config["application_name"].(string); ok {
			config.ApplicationName = appName
		}
		if stmtTimeout, ok := c.Config["statement_timeout"].(string); ok {
			config.StatementTimeout = stmtTimeout
		}
		if lockTimeout, ok := c.Config["lock_timeout"].(string); ok {
			config.LockTimeout = lockTimeout
		}
		return config

	case "redis":
		config := &RedisConfig{
			ConnectionConfig: *c,
		}
		if db, ok := c.Config["db"].(int); ok {
			config.DB = db
		}
		if dialTimeout, ok := c.Config["dial_timeout"].(time.Duration); ok {
			config.DialTimeout = dialTimeout
		}
		if readTimeout, ok := c.Config["read_timeout"].(time.Duration); ok {
			config.ReadTimeout = readTimeout
		}
		if writeTimeout, ok := c.Config["write_timeout"].(time.Duration); ok {
			config.WriteTimeout = writeTimeout
		}
		return config

	case "mongodb", "mongo":
		config := &MongoDBConfig{
			ConnectionConfig: *c,
		}
		if replicaSet, ok := c.Config["replica_set"].(string); ok {
			config.ReplicaSet = replicaSet
		}
		if authSource, ok := c.Config["auth_source"].(string); ok {
			config.AuthSource = authSource
		}
		if connectTimeout, ok := c.Config["connect_timeout"].(time.Duration); ok {
			config.ConnectTimeout = connectTimeout
		}
		return config

	default:
		return c
	}
}

// GetConnectionByType returns connections filtered by database type
func (dc *DatabaseConfig) GetConnectionByType(dbType string) map[string]ConnectionConfig {
	connections := make(map[string]ConnectionConfig)
	for name, conn := range dc.Connections {
		if conn.Type == dbType {
			connections[name] = conn
		}
	}
	return connections
}

// GetPostgresConnections returns all PostgreSQL connections
func (dc *DatabaseConfig) GetPostgresConnections() map[string]ConnectionConfig {
	postgres := dc.GetConnectionByType("postgres")
	postgresql := dc.GetConnectionByType("postgresql")

	// Merge both maps
	for name, conn := range postgresql {
		postgres[name] = conn
	}

	return postgres
}

// GetMySQLConnections returns all MySQL connections
func (dc *DatabaseConfig) GetMySQLConnections() map[string]ConnectionConfig {
	return dc.GetConnectionByType("mysql")
}

// GetRedisConnections returns all Redis connections
func (dc *DatabaseConfig) GetRedisConnections() map[string]ConnectionConfig {
	return dc.GetConnectionByType("redis")
}

// GetMongoConnections returns all MongoDB connections
func (dc *DatabaseConfig) GetMongoConnections() map[string]ConnectionConfig {
	mongo := dc.GetConnectionByType("mongodb")
	mongodb := dc.GetConnectionByType("mongo")

	// Merge both maps
	for name, conn := range mongodb {
		mongo[name] = conn
	}

	return mongo
}

// HasConnectionType checks if any connection of the given type exists
func (dc *DatabaseConfig) HasConnectionType(dbType string) bool {
	for _, conn := range dc.Connections {
		if conn.Type == dbType {
			return true
		}
	}
	return false
}

// AddConnection adds a new connection to the configuration
func (dc *DatabaseConfig) AddConnection(name string, connection ConnectionConfig) error {
	if err := connection.Validate(); err != nil {
		return fmt.Errorf("invalid connection configuration: %w", err)
	}

	if dc.Connections == nil {
		dc.Connections = make(map[string]ConnectionConfig)
	}

	dc.Connections[name] = connection
	return nil
}

// RemoveConnection removes a connection from the configuration
func (dc *DatabaseConfig) RemoveConnection(name string) error {
	if _, exists := dc.Connections[name]; !exists {
		return fmt.Errorf("connection '%s' not found", name)
	}

	if dc.Default == name {
		return fmt.Errorf("cannot remove default connection '%s'", name)
	}

	delete(dc.Connections, name)
	return nil
}

// SetDefaultConnection sets the default connection
func (dc *DatabaseConfig) SetDefaultConnection(name string) error {
	if _, exists := dc.Connections[name]; !exists {
		return fmt.Errorf("connection '%s' not found", name)
	}

	dc.Default = name
	return nil
}
