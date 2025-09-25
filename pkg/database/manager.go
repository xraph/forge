package database

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// DatabaseManager manages database connections and adapters
type DatabaseManager interface {
	common.Service // Integrates with Phase 1 service lifecycle

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

// Manager implements the DatabaseManager interface
type Manager struct {
	config            *DatabaseConfig
	connections       map[string]Connection
	factory           *ConnectionFactory
	migrator          MigrationManager
	logger            common.Logger
	metrics           common.Metrics
	configManager     common.ConfigManager
	started           bool
	startedAt         time.Time
	lastHealthCheck   time.Time
	healthCheckPassed bool
	mu                sync.RWMutex
}

// NewManager creates a new database manager
func NewManager(logger common.Logger, metrics common.Metrics, configManager common.ConfigManager) DatabaseManager {
	factory := NewConnectionFactory(logger, metrics)

	return &Manager{
		connections:   make(map[string]Connection),
		factory:       factory,
		logger:        logger,
		metrics:       metrics,
		configManager: configManager,
	}
}

// Name returns the service name
func (m *Manager) Name() string {
	return "database-manager"
}

// Dependencies returns service dependencies
func (m *Manager) Dependencies() []string {
	return []string{"logger", "metrics", "config-manager"}
}

// OnStart starts the database manager
func (m *Manager) OnStart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrServiceStartFailed(m.Name(), fmt.Errorf("database manager already started"))
	}

	m.logger.Info("starting database manager")

	// Load configuration
	if err := m.loadConfiguration(); err != nil {
		return common.ErrServiceStartFailed(m.Name(), fmt.Errorf("failed to load configuration: %w", err))
	}

	// Validate configuration
	if err := m.config.Validate(); err != nil {
		return common.ErrServiceStartFailed(m.Name(), fmt.Errorf("invalid configuration: %w", err))
	}

	// Create migration manager
	m.migrator = NewMigrationManager(m.config.MigrationsPath, m.logger)

	// OnStart all connections
	if err := m.startAllConnections(ctx); err != nil {
		return common.ErrServiceStartFailed(m.Name(), err)
	}

	// Run auto migrations if enabled
	if m.config.AutoMigrate {
		if err := m.runAutoMigrations(ctx); err != nil {
			m.logger.Error("auto migration failed",
				logger.Error(err),
			)
			// Don't fail startup for migration errors, just log them
		}
	}

	m.started = true
	m.startedAt = time.Now()

	m.logger.Info("database manager started",
		logger.Int("connections", len(m.connections)),
		logger.String("default_connection", m.config.Default),
		logger.Bool("auto_migrate", m.config.AutoMigrate),
	)

	if m.metrics != nil {
		m.metrics.Counter("forge.database.manager_started").Inc()
		m.metrics.Gauge("forge.database.total_connections").Set(float64(len(m.connections)))
	}

	return nil
}

// OnStop stops the database manager
func (m *Manager) OnStop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return common.ErrServiceStopFailed(m.Name(), fmt.Errorf("database manager not started"))
	}

	m.logger.Info("stopping database manager")

	// OnStop all connections
	var stopErrors []error
	for name, conn := range m.connections {
		if err := conn.OnStop(ctx); err != nil {
			m.logger.Error("failed to stop connection",
				logger.String("connection", name),
				logger.Error(err),
			)
			stopErrors = append(stopErrors, err)
		}
	}

	m.started = false

	if len(stopErrors) > 0 {
		return common.ErrServiceStopFailed(m.Name(), fmt.Errorf("failed to stop %d connections", len(stopErrors)))
	}

	m.logger.Info("database manager stopped")

	if m.metrics != nil {
		m.metrics.Counter("forge.database.manager_stopped").Inc()
		m.metrics.Gauge("forge.database.total_connections").Set(0)
	}

	return nil
}

// OnHealthCheck performs a health check on all connections
func (m *Manager) OnHealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return common.ErrHealthCheckFailed(m.Name(), fmt.Errorf("database manager not started"))
	}

	start := time.Now()
	var healthErrors []error

	for name, conn := range m.connections {
		if err := conn.OnHealthCheck(ctx); err != nil {
			m.logger.Error("connection health check failed",
				logger.String("connection", name),
				logger.Error(err),
			)
			healthErrors = append(healthErrors, err)
		}
	}

	// Update health check status
	m.lastHealthCheck = time.Now()
	m.healthCheckPassed = len(healthErrors) == 0

	duration := time.Since(start)
	if m.metrics != nil {
		m.metrics.Histogram("forge.database.health_check_duration").Observe(duration.Seconds())
		m.metrics.Counter("forge.database.health_checks_total").Inc()

		if len(healthErrors) > 0 {
			m.metrics.Counter("forge.database.health_checks_failed").Inc()
		}
	}

	if len(healthErrors) > 0 {
		return common.ErrHealthCheckFailed(m.Name(), fmt.Errorf("%d connections failed health check", len(healthErrors)))
	}

	return nil
}

// RegisterAdapter registers a database adapter
func (m *Manager) RegisterAdapter(adapter DatabaseAdapter) error {
	if err := m.factory.RegisterAdapter(adapter); err != nil {
		return fmt.Errorf("failed to register adapter: %w", err)
	}

	m.logger.Info("database adapter registered",
		logger.String("adapter", adapter.Name()),
		logger.String("types", fmt.Sprintf("%v", adapter.SupportedTypes())),
	)

	if m.metrics != nil {
		m.metrics.Counter("forge.database.adapters_registered").Inc()
		m.metrics.Gauge("forge.database.registered_adapters").Set(float64(len(m.factory.ListAdapters())))
	}

	return nil
}

// GetConnection returns a connection by name
func (m *Manager) GetConnection(name string) (Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connection, exists := m.connections[name]
	if !exists {
		return nil, fmt.Errorf("connection '%s' not found", name)
	}

	return connection, nil
}

// GetDefaultConnection returns the default connection
func (m *Manager) GetDefaultConnection() (Connection, error) {
	if m.config == nil {
		return nil, fmt.Errorf("database configuration not loaded")
	}

	return m.GetConnection(m.config.Default)
}

// ListConnections returns a list of connection names
func (m *Manager) ListConnections() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connections := make([]string, 0, len(m.connections))
	for name := range m.connections {
		connections = append(connections, name)
	}

	sort.Strings(connections)
	return connections
}

// GetConnections returns a list of connection names
func (m *Manager) GetConnections() map[string]Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.connections
}

// CloseConnection closes a specific connection
func (m *Manager) CloseConnection(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	connection, exists := m.connections[name]
	if !exists {
		return fmt.Errorf("connection '%s' not found", name)
	}

	if err := connection.OnStop(context.Background()); err != nil {
		return fmt.Errorf("failed to close connection '%s': %w", name, err)
	}

	delete(m.connections, name)

	m.logger.Info("connection closed",
		logger.String("connection", name),
	)

	if m.metrics != nil {
		m.metrics.Counter("forge.database.connections_closed").Inc()
		m.metrics.Gauge("forge.database.total_connections").Set(float64(len(m.connections)))
	}

	return nil
}

// StartAll starts all configured connections
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.startAllConnections(ctx)
}

// StopAll stops all connections
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var stopErrors []error
	for name, conn := range m.connections {
		if err := conn.OnStop(ctx); err != nil {
			m.logger.Error("failed to stop connection",
				logger.String("connection", name),
				logger.Error(err),
			)
			stopErrors = append(stopErrors, err)
		}
	}

	if len(stopErrors) > 0 {
		return fmt.Errorf("failed to stop %d connections", len(stopErrors))
	}

	return nil
}

// HealthCheckAll performs health checks on all connections
func (m *Manager) HealthCheckAll(ctx context.Context) error {
	return m.OnHealthCheck(ctx)
}

// SetConfig sets the database configuration
func (m *Manager) SetConfig(config *DatabaseConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.config = config

	m.logger.Info("database configuration set",
		logger.String("default_connection", config.Default),
		logger.Int("connections", len(config.Connections)),
		logger.Bool("auto_migrate", config.AutoMigrate),
	)

	return nil
}

// GetConfig returns the current database configuration
func (m *Manager) GetConfig() *DatabaseConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.config
}

// GetStats returns database manager statistics
func (m *Manager) GetStats() ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connectionStats := make(map[string]ConnectionStats)
	activeConnections := 0

	for name, conn := range m.connections {
		stats := conn.Stats()
		connectionStats[name] = stats
		if stats.Connected {
			activeConnections++
		}
	}

	var totalMigrations int
	var lastMigrationRun time.Time
	if m.migrator != nil {
		totalMigrations = m.migrator.GetTotalMigrations()
		lastMigrationRun = m.migrator.GetLastMigrationRun()
	}

	return ManagerStats{
		TotalConnections:   len(m.connections),
		ActiveConnections:  activeConnections,
		RegisteredAdapters: len(m.factory.ListAdapters()),
		StartedAt:          m.startedAt,
		LastHealthCheck:    m.lastHealthCheck,
		HealthCheckPassed:  m.healthCheckPassed,
		ConnectionStats:    connectionStats,
		AdapterInfo:        m.factory.GetAdapterInfo(),
		MigrationsEnabled:  m.config != nil && m.config.AutoMigrate,
		LastMigrationRun:   lastMigrationRun,
		TotalMigrations:    totalMigrations,
	}
}

// GetConnectionStats returns statistics for a specific connection
func (m *Manager) GetConnectionStats(name string) (*ConnectionStats, error) {
	connection, err := m.GetConnection(name)
	if err != nil {
		return nil, err
	}

	stats := connection.Stats()
	return &stats, nil
}

// Migrate runs migrations for a specific connection
func (m *Manager) Migrate(ctx context.Context, connectionName string) error {
	if m.migrator == nil {
		return fmt.Errorf("migration manager not initialized")
	}

	connection, err := m.GetConnection(connectionName)
	if err != nil {
		return fmt.Errorf("connection '%s' not found: %w", connectionName, err)
	}

	adapter, err := m.factory.GetAdapterForType(connection.Type())
	if err != nil {
		return fmt.Errorf("adapter not found for connection type '%s': %w", connection.Type(), err)
	}

	if !adapter.SupportsMigrations() {
		return fmt.Errorf("adapter '%s' does not support migrations", adapter.Name())
	}

	m.logger.Info("running migrations",
		logger.String("connection", connectionName),
		logger.String("type", connection.Type()),
	)

	start := time.Now()
	if err := adapter.Migrate(ctx, connection, m.config.MigrationsPath); err != nil {
		m.logger.Error("migration failed",
			logger.String("connection", connectionName),
			logger.Error(err),
		)
		return fmt.Errorf("migration failed for connection '%s': %w", connectionName, err)
	}

	duration := time.Since(start)
	m.logger.Info("migrations completed",
		logger.String("connection", connectionName),
		logger.Duration("duration", duration),
	)

	if m.metrics != nil {
		m.metrics.Counter("forge.database.migrations_completed").Inc()
		m.metrics.Histogram("forge.database.migration_duration").Observe(duration.Seconds())
	}

	return nil
}

// MigrateAll runs migrations for all connections that support it
func (m *Manager) MigrateAll(ctx context.Context) error {
	m.mu.RLock()
	connectionNames := make([]string, 0, len(m.connections))
	for name := range m.connections {
		connectionNames = append(connectionNames, name)
	}
	m.mu.RUnlock()

	var migrationErrors []error
	for _, name := range connectionNames {
		if err := m.Migrate(ctx, name); err != nil {
			m.logger.Error("migration failed for connection",
				logger.String("connection", name),
				logger.Error(err),
			)
			migrationErrors = append(migrationErrors, err)
		}
	}

	if len(migrationErrors) > 0 {
		return fmt.Errorf("migration failed for %d connections", len(migrationErrors))
	}

	return nil
}

// loadConfiguration loads database configuration from the config manager
func (m *Manager) loadConfiguration() error {
	if m.configManager == nil {
		// Use default configuration if no config manager is available
		m.config = DefaultDatabaseConfig()
		m.logger.Warn("no config manager available, using default database configuration")
		return nil
	}

	var config DatabaseConfig
	if err := m.configManager.Bind("database", &config); err != nil {
		// If binding fails, try to get the raw config
		if dbConfig := m.configManager.Get("database"); dbConfig != nil {
			// Try type assertion
			if typedConfig, ok := dbConfig.(*DatabaseConfig); ok {
				m.config = typedConfig
				return nil
			}
		}

		// Fall back to default configuration
		m.config = DefaultDatabaseConfig()
		m.logger.Warn("failed to load database configuration, using defaults",
			logger.Error(err),
		)
		return nil
	}

	m.config = &config
	return nil
}

// startAllConnections starts all configured connections
func (m *Manager) startAllConnections(ctx context.Context) error {
	if m.config == nil {
		return fmt.Errorf("database configuration not loaded")
	}

	var startErrors []error
	for name, connConfig := range m.config.Connections {
		connection, err := m.factory.CreateConnection(name, &connConfig)
		if err != nil {
			m.logger.Error("failed to create connection",
				logger.String("connection", name),
				logger.Error(err),
			)
			startErrors = append(startErrors, err)
			continue
		}

		if err := connection.OnStart(ctx); err != nil {
			m.logger.Error("failed to start connection",
				logger.String("connection", name),
				logger.Error(err),
			)
			startErrors = append(startErrors, err)
			continue
		}

		m.connections[name] = connection

		m.logger.Info("connection started",
			logger.String("connection", name),
			logger.String("type", connConfig.Type),
			logger.String("host", connConfig.Host),
			logger.Int("port", connConfig.Port),
		)
	}

	if len(startErrors) > 0 {
		return fmt.Errorf("failed to start %d connections", len(startErrors))
	}

	return nil
}

// runAutoMigrations runs auto migrations for all connections that support it
func (m *Manager) runAutoMigrations(ctx context.Context) error {
	m.logger.Info("running auto migrations")

	start := time.Now()
	if err := m.MigrateAll(ctx); err != nil {
		return fmt.Errorf("auto migration failed: %w", err)
	}

	duration := time.Since(start)
	m.logger.Info("auto migrations completed",
		logger.Duration("duration", duration),
	)

	return nil
}
