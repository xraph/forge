package database

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Connection defines the interface for database connections
type Connection interface {
	common.Service // Integrates with Phase 1 service lifecycle

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

// BaseConnection provides common functionality for database connections
type BaseConnection struct {
	name                string
	dbType              string
	config              *ConnectionConfig
	db                  any
	connected           bool
	connectedAt         time.Time
	lastPing            time.Time
	pingDuration        time.Duration
	transactionCount    int64
	queryCount          int64
	errorCount          int64
	lastError           error
	healthCheckInterval time.Duration
	healthCheckTicker   *time.Ticker
	healthCheckCtx      context.Context
	healthCheckCancel   context.CancelFunc
	logger              common.Logger
	metrics             common.Metrics
	mu                  sync.RWMutex
	shutdownCh          chan struct{}
	wg                  sync.WaitGroup
}

// NewBaseConnection creates a new base connection
func NewBaseConnection(name, dbType string, config *ConnectionConfig, logger common.Logger, metrics common.Metrics) *BaseConnection {
	return &BaseConnection{
		name:                name,
		dbType:              dbType,
		config:              config,
		connected:           false,
		healthCheckInterval: 30 * time.Second,
		logger:              logger,
		metrics:             metrics,
		shutdownCh:          make(chan struct{}),
	}
}

// Name returns the connection name
func (bc *BaseConnection) Name() string {
	return bc.name
}

// Type returns the database type
func (bc *BaseConnection) Type() string {
	return bc.dbType
}

// DB returns the underlying database connection
func (bc *BaseConnection) DB() any {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.db
}

// SetDB sets the underlying database connection (for use by adapters)
func (bc *BaseConnection) SetDB(db any) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.db = db
}

// Dependencies returns service dependencies
func (bc *BaseConnection) Dependencies() []string {
	return []string{"logger", "metrics"}
}

// Connect establishes the database connection (to be implemented by specific adapters)
func (bc *BaseConnection) Connect(ctx context.Context) error {
	return fmt.Errorf("Connect method must be implemented by specific adapter")
}

// Close closes the database connection (to be implemented by specific adapters)
func (bc *BaseConnection) Close(ctx context.Context) error {
	return fmt.Errorf("Close method must be implemented by specific adapter")
}

// Ping pings the database (to be implemented by specific adapters)
func (bc *BaseConnection) Ping(ctx context.Context) error {
	return fmt.Errorf("Ping method must be implemented by specific adapter")
}

// Transaction runs a function within a database transaction (to be implemented by specific adapters)
func (bc *BaseConnection) Transaction(ctx context.Context, fn func(tx interface{}) error) error {
	return fmt.Errorf("Transaction method must be implemented by specific adapter")
}

// IsConnected returns whether the connection is established
func (bc *BaseConnection) IsConnected() bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.connected
}

// SetConnected sets the connection status
func (bc *BaseConnection) SetConnected(connected bool) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.connected = connected
	if connected {
		bc.connectedAt = time.Now()
	}
}

// Config returns the connection configuration
func (bc *BaseConnection) Config() *ConnectionConfig {
	return bc.config
}

// ConnectionString returns a sanitized connection string (without password)
func (bc *BaseConnection) ConnectionString() string {
	// Create a copy of config without password for sanitized connection string
	sanitizedConfig := *bc.config
	sanitizedConfig.Password = "***"
	return sanitizedConfig.ConnectionString()
}

// Stats returns connection statistics
func (bc *BaseConnection) Stats() ConnectionStats {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	var lastErrorStr string
	if bc.lastError != nil {
		lastErrorStr = bc.lastError.Error()
	}

	return ConnectionStats{
		Name:             bc.name,
		Type:             bc.dbType,
		Connected:        bc.connected,
		ConnectedAt:      bc.connectedAt,
		LastPing:         bc.lastPing,
		PingDuration:     bc.pingDuration,
		TransactionCount: bc.transactionCount,
		QueryCount:       bc.queryCount,
		ErrorCount:       bc.errorCount,
		LastError:        lastErrorStr,
		MaxOpenConns:     bc.config.Pool.MaxOpenConns,
		MaxIdleConns:     bc.config.Pool.MaxIdleConns,
		ConnMaxLifetime:  bc.config.Pool.ConnMaxLifetime,
		ConnMaxIdleTime:  bc.config.Pool.ConnMaxIdleTime,
	}
}

// SetHealthCheckInterval sets the health check interval
func (bc *BaseConnection) SetHealthCheckInterval(interval time.Duration) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.healthCheckInterval = interval
}

// OnStart starts the connection service
func (bc *BaseConnection) OnStart(ctx context.Context) error {
	bc.logger.Info("starting database connection",
		logger.String("name", bc.name),
		logger.String("type", bc.dbType),
		logger.String("host", bc.config.Host),
		logger.Int("port", bc.config.Port),
	)

	// Connect to database
	if err := bc.Connect(ctx); err != nil {
		bc.recordError(err)
		return common.ErrServiceStartFailed(bc.name, err)
	}

	// OnStart health check routine
	if err := bc.startHealthCheck(); err != nil {
		return common.ErrServiceStartFailed(bc.name, err)
	}

	bc.logger.Info("database connection started",
		logger.String("name", bc.name),
		logger.String("type", bc.dbType),
	)

	if bc.metrics != nil {
		bc.metrics.Counter("forge.database.connections_started").Inc()
		bc.metrics.Gauge("forge.database.active_connections", "name", bc.name, "type", bc.dbType).Set(1)
	}

	return nil
}

// OnStop stops the connection service
func (bc *BaseConnection) OnStop(ctx context.Context) error {
	bc.logger.Info("stopping database connection",
		logger.String("name", bc.name),
		logger.String("type", bc.dbType),
	)

	// OnStop health check
	bc.stopHealthCheck()

	// Close database connection
	if err := bc.Close(ctx); err != nil {
		bc.logger.Error("failed to close database connection",
			logger.String("name", bc.name),
			logger.Error(err),
		)
		return common.ErrServiceStopFailed(bc.name, err)
	}

	bc.logger.Info("database connection stopped",
		logger.String("name", bc.name),
		logger.String("type", bc.dbType),
	)

	if bc.metrics != nil {
		bc.metrics.Counter("forge.database.connections_stopped").Inc()
		bc.metrics.Gauge("forge.database.active_connections", "name", bc.name, "type", bc.dbType).Set(0)
	}

	return nil
}

// OnHealthCheck performs a health check
func (bc *BaseConnection) OnHealthCheck(ctx context.Context) error {
	start := time.Now()

	if err := bc.Ping(ctx); err != nil {
		bc.recordError(err)
		return common.ErrHealthCheckFailed(bc.name, err)
	}

	// Update ping statistics
	bc.mu.Lock()
	bc.lastPing = time.Now()
	bc.pingDuration = time.Since(start)
	bc.mu.Unlock()

	if bc.metrics != nil {
		bc.metrics.Histogram("forge.database.ping_duration", "name", bc.name, "type", bc.dbType).Observe(bc.pingDuration.Seconds())
	}

	return nil
}

// startHealthCheck starts the health check routine
func (bc *BaseConnection) startHealthCheck() error {
	bc.healthCheckCtx, bc.healthCheckCancel = context.WithCancel(context.Background())
	bc.healthCheckTicker = time.NewTicker(bc.healthCheckInterval)

	bc.wg.Add(1)
	go func() {
		defer bc.wg.Done()
		defer bc.healthCheckTicker.Stop()

		for {
			select {
			case <-bc.healthCheckTicker.C:
				if err := bc.OnHealthCheck(bc.healthCheckCtx); err != nil {
					bc.logger.Error("health check failed",
						logger.String("name", bc.name),
						logger.Error(err),
					)
				}

			case <-bc.healthCheckCtx.Done():
				return

			case <-bc.shutdownCh:
				return
			}
		}
	}()

	return nil
}

// stopHealthCheck stops the health check routine
func (bc *BaseConnection) stopHealthCheck() {
	close(bc.shutdownCh)
	if bc.healthCheckCancel != nil {
		bc.healthCheckCancel()
	}
	bc.wg.Wait()
}

// recordError records an error in connection statistics
func (bc *BaseConnection) recordError(err error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.errorCount++
	bc.lastError = err

	if bc.metrics != nil {
		bc.metrics.Counter("forge.database.connection_errors", "name", bc.name, "type", bc.dbType).Inc()
	}
}

// incrementTransactionCount increments the transaction counter
func (bc *BaseConnection) IncrementTransactionCount() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.transactionCount++

	if bc.metrics != nil {
		bc.metrics.Counter("forge.database.transactions", "name", bc.name, "type", bc.dbType).Inc()
	}
}

// IncrementQueryCount increments the query counter
func (bc *BaseConnection) IncrementQueryCount() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.queryCount++

	if bc.metrics != nil {
		bc.metrics.Counter("forge.database.queries", "name", bc.name, "type", bc.dbType).Inc()
	}
}

func (bc *BaseConnection) IncrementErrorCount() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.errorCount++
}

func (bc *BaseConnection) Metrics() common.Metrics {
	return bc.metrics
}

func (bc *BaseConnection) Logger() common.Logger {
	return bc.logger
}

// ConnectionFactory creates connections for different database types
type ConnectionFactory struct {
	adapters map[string]DatabaseAdapter
	logger   common.Logger
	metrics  common.Metrics
	mu       sync.RWMutex
}

// NewConnectionFactory creates a new connection factory
func NewConnectionFactory(logger common.Logger, metrics common.Metrics) *ConnectionFactory {
	return &ConnectionFactory{
		adapters: make(map[string]DatabaseAdapter),
		logger:   logger,
		metrics:  metrics,
	}
}

// RegisterAdapter registers a database adapter
func (cf *ConnectionFactory) RegisterAdapter(adapter DatabaseAdapter) error {
	cf.mu.Lock()
	defer cf.mu.Unlock()

	name := adapter.Name()
	if _, exists := cf.adapters[name]; exists {
		return fmt.Errorf("adapter '%s' already registered", name)
	}

	cf.adapters[name] = adapter

	cf.logger.Info("database adapter registered",
		logger.String("adapter", name),
		logger.String("types", fmt.Sprintf("%v", adapter.SupportedTypes())),
	)

	return nil
}

// GetAdapter returns a database adapter by name
func (cf *ConnectionFactory) GetAdapter(name string) (DatabaseAdapter, error) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	adapter, exists := cf.adapters[name]
	if !exists {
		return nil, fmt.Errorf("adapter '%s' not found", name)
	}

	return adapter, nil
}

// GetAdapterForType returns a database adapter for the given database type
func (cf *ConnectionFactory) GetAdapterForType(dbType string) (DatabaseAdapter, error) {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	for _, adapter := range cf.adapters {
		for _, supportedType := range adapter.SupportedTypes() {
			if supportedType == dbType {
				return adapter, nil
			}
		}
	}

	return nil, fmt.Errorf("no adapter found for database type '%s'", dbType)
}

// CreateConnection creates a new database connection
func (cf *ConnectionFactory) CreateConnection(name string, config *ConnectionConfig) (Connection, error) {
	adapter, err := cf.GetAdapterForType(config.Type)
	if err != nil {
		return nil, err
	}

	if err := adapter.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid configuration for connection '%s': %w", name, err)
	}

	connection, err := adapter.Connect(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection '%s': %w", name, err)
	}

	return connection, nil
}

// ListAdapters returns a list of registered adapters
func (cf *ConnectionFactory) ListAdapters() []string {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	adapters := make([]string, 0, len(cf.adapters))
	for name := range cf.adapters {
		adapters = append(adapters, name)
	}

	return adapters
}

// AdapterInfo contains information about a database adapter
type AdapterInfo struct {
	Name           string   `json:"name"`
	SupportedTypes []string `json:"supported_types"`
	Migrations     bool     `json:"supports_migrations"`
}

// GetAdapterInfo returns information about all registered adapters
func (cf *ConnectionFactory) GetAdapterInfo() []AdapterInfo {
	cf.mu.RLock()
	defer cf.mu.RUnlock()

	info := make([]AdapterInfo, 0, len(cf.adapters))
	for _, adapter := range cf.adapters {
		info = append(info, AdapterInfo{
			Name:           adapter.Name(),
			SupportedTypes: adapter.SupportedTypes(),
			Migrations:     adapter.SupportsMigrations(),
		})
	}

	return info
}
