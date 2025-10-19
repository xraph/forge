package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/xraph/forge/pkg/common"
	config2 "github.com/xraph/forge/pkg/config"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics"
)

// TestContainer represents a test database container
type TestContainer interface {
	// Container lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Terminate(ctx context.Context) error

	// Connection information
	ConnectionString() string
	Host() string
	Port() int
	Database() string
	Username() string
	Password() string

	// Container information
	ContainerID() string
	Image() string
	IsRunning() bool

	// Helper methods
	Exec(ctx context.Context, cmd []string) error
	Logs(ctx context.Context) (string, error)
}

// TestConnectionConfig provides test-specific connection configuration
type TestConnectionConfig struct {
	*ConnectionConfig
	Container TestContainer
	CleanupDB bool // Whether to clean database between tests
}

// TestDatabaseManager provides testing utilities for database operations
type TestDatabaseManager struct {
	manager    DatabaseManager
	containers map[string]TestContainer
	configs    map[string]*TestConnectionConfig
	logger     common.Logger
	cleanup    []func() error
}

// NewTestDatabaseManager creates a new test database manager
func NewTestDatabaseManager(l common.Logger) *TestDatabaseManager {
	if l == nil {
		l = logger.NewNoopLogger()
	}

	return &TestDatabaseManager{
		containers: make(map[string]TestContainer),
		configs:    make(map[string]*TestConnectionConfig),
		logger:     l,
		cleanup:    make([]func() error, 0),
	}
}

// SetupTestDatabase sets up a test database with the given configuration
func (tdm *TestDatabaseManager) SetupTestDatabase(t *testing.T, dbType string, config *TestConnectionConfig) (Connection, error) {
	t.Helper()

	if config == nil {
		// Create default test configuration
		var err error
		config, err = tdm.createDefaultTestConfig(dbType)
		if err != nil {
			return nil, fmt.Errorf("failed to create default test config: %w", err)
		}
	}

	// Start container if provided
	if config.Container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := config.Container.Start(ctx); err != nil {
			return nil, fmt.Errorf("failed to start test container: %w", err)
		}

		// Register cleanup
		tdm.registerCleanup(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return config.Container.Terminate(ctx)
		})

		// Update connection config with container details
		config.ConnectionConfig.Host = config.Container.Host()
		config.ConnectionConfig.Port = config.Container.Port()
		config.ConnectionConfig.Database = config.Container.Database()
		config.ConnectionConfig.Username = config.Container.Username()
		config.ConnectionConfig.Password = config.Container.Password()
	}

	metrics := metrics.NewMockMetricsCollector()

	// Create test manager if not exists
	if tdm.manager == nil {
		configManager := config2.NewTestConfigManager()
		tdm.manager = NewManager(tdm.logger, metrics, configManager)

		// Register appropriate adapter
		adapter, err := tdm.createTestAdapter(dbType)
		if err != nil {
			return nil, fmt.Errorf("failed to create test adapter: %w", err)
		}

		if err := tdm.manager.RegisterAdapter(adapter); err != nil {
			return nil, fmt.Errorf("failed to register adapter: %w", err)
		}
	}

	// Create connection factory
	factory := NewConnectionFactory(tdm.logger, metrics)

	// Register the same adapter with factory
	adapter, err := tdm.createTestAdapter(dbType)
	if err != nil {
		return nil, fmt.Errorf("failed to create test adapter: %w", err)
	}

	if err := factory.RegisterAdapter(adapter); err != nil {
		return nil, fmt.Errorf("failed to register adapter with factory: %w", err)
	}

	// Create connection
	connectionName := fmt.Sprintf("test_%s_%d", dbType, time.Now().UnixNano())
	connection, err := factory.CreateConnection(connectionName, config.ConnectionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	// Start connection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := connection.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start connection: %w", err)
	}

	// Register cleanup
	tdm.registerCleanup(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return connection.Stop(ctx)
	})

	// Store configuration
	tdm.configs[connectionName] = config

	return connection, nil
}

// CleanupDatabase cleans up the test database
func (tdm *TestDatabaseManager) CleanupDatabase(t *testing.T, conn Connection) error {
	t.Helper()

	if config, exists := tdm.configs[conn.Name()]; exists && config.CleanupDB {
		// Perform database cleanup based on type
		switch conn.Type() {
		case "postgres", "postgresql":
			return tdm.cleanupPostgres(conn)
		case "redis":
			return tdm.cleanupRedis(conn)
		case "mongodb", "mongo":
			return tdm.cleanupMongoDB(conn)
		}
	}

	return nil
}

// Cleanup cleans up all test resources
func (tdm *TestDatabaseManager) Cleanup() error {
	var errors []error

	// Run all cleanup functions in reverse order
	for i := len(tdm.cleanup) - 1; i >= 0; i-- {
		if err := tdm.cleanup[i](); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup failed with %d errors: %v", len(errors), errors)
	}

	return nil
}

// registerCleanup registers a cleanup function
func (tdm *TestDatabaseManager) registerCleanup(fn func() error) {
	tdm.cleanup = append(tdm.cleanup, fn)
}

// createDefaultTestConfig creates default test configuration for a database type
func (tdm *TestDatabaseManager) createDefaultTestConfig(dbType string) (*TestConnectionConfig, error) {
	switch dbType {
	case "postgres", "postgresql":
		return &TestConnectionConfig{
			ConnectionConfig: &ConnectionConfig{
				Type:     "postgres",
				Host:     "localhost",
				Port:     5432,
				Database: "test_db",
				Username: "test_user",
				Password: "test_password",
				SSLMode:  "disable",
				Pool: PoolConfig{
					MaxOpenConns:    5,
					MaxIdleConns:    2,
					ConnMaxLifetime: 5 * time.Minute,
					ConnMaxIdleTime: 2 * time.Minute,
				},
			},
			CleanupDB: true,
		}, nil

	case "redis":
		return &TestConnectionConfig{
			ConnectionConfig: &ConnectionConfig{
				Type:     "redis",
				Host:     "localhost",
				Port:     6379,
				Database: "test_db",
				Config: map[string]interface{}{
					"db": 15, // Use database 15 for testing
				},
				Pool: PoolConfig{
					MaxOpenConns:    5,
					MaxIdleConns:    2,
					ConnMaxLifetime: 5 * time.Minute,
					ConnMaxIdleTime: 2 * time.Minute,
				},
			},
			CleanupDB: true,
		}, nil

	case "mongodb", "mongo":
		return &TestConnectionConfig{
			ConnectionConfig: &ConnectionConfig{
				Type:     "mongodb",
				Host:     "localhost",
				Port:     27017,
				Database: "test_db",
				Username: "test_user",
				Password: "test_password",
				Pool: PoolConfig{
					MaxOpenConns:    5,
					MaxIdleConns:    2,
					ConnMaxLifetime: 5 * time.Minute,
					ConnMaxIdleTime: 2 * time.Minute,
				},
			},
			CleanupDB: true,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported database type for testing: %s", dbType)
	}
}

// createTestAdapter creates a test adapter for the given database type
func (tdm *TestDatabaseManager) createTestAdapter(dbType string) (DatabaseAdapter, error) {
	// This would create actual adapters - for now return a mock
	return &MockAdapter{dbType: dbType}, nil
}

// Database-specific cleanup methods
func (tdm *TestDatabaseManager) cleanupPostgres(conn Connection) error {
	// Implementation would clean PostgreSQL database
	// For now, just log
	tdm.logger.Info("cleaning up PostgreSQL test database",
		logger.String("connection", conn.Name()),
	)
	return nil
}

func (tdm *TestDatabaseManager) cleanupRedis(conn Connection) error {
	// Implementation would clean Redis database
	// For now, just log
	tdm.logger.Info("cleaning up Redis test database",
		logger.String("connection", conn.Name()),
	)
	return nil
}

func (tdm *TestDatabaseManager) cleanupMongoDB(conn Connection) error {
	// Implementation would clean MongoDB database
	// For now, just log
	tdm.logger.Info("cleaning up MongoDB test database",
		logger.String("connection", conn.Name()),
	)
	return nil
}

// Test helper functions

// WaitForConnection waits for a connection to be ready
func WaitForConnection(ctx context.Context, conn Connection, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for connection to be ready")
		case <-ticker.C:
			if err := conn.Ping(ctx); err == nil {
				return nil
			}
		}
	}
}

// AssertConnectionHealthy asserts that a connection is healthy
func AssertConnectionHealthy(t *testing.T, conn Connection) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.OnHealthCheck(ctx); err != nil {
		t.Fatalf("connection health check failed: %v", err)
	}

	if !conn.IsConnected() {
		t.Fatal("connection is not connected")
	}
}

// AssertCanExecuteQueries asserts that queries can be executed on the connection
func AssertCanExecuteQueries(t *testing.T, conn Connection) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This would be implemented by specific adapters
	// For now, just ping
	if err := conn.Ping(ctx); err != nil {
		t.Fatalf("failed to ping connection: %v", err)
	}
}

// GetTestConnectionString returns a connection string for testing
func GetTestConnectionString(dbType string) string {
	switch dbType {
	case "postgres", "postgresql":
		return getEnvOrDefault("TEST_POSTGRES_URL", "postgres://test_user:test_password@localhost:5432/test_db?sslmode=disable")
	case "redis":
		return getEnvOrDefault("TEST_REDIS_URL", "redis://localhost:6379/15")
	case "mongodb", "mongo":
		return getEnvOrDefault("TEST_MONGODB_URL", "mongodb://test_user:test_password@localhost:27017/test_db")
	default:
		return ""
	}
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(envVar, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}

// Mock implementations for testing

// MockAdapter is a mock database adapter for testing
type MockAdapter struct {
	dbType string
}

func (ma *MockAdapter) Name() string {
	return "mock-" + ma.dbType
}

func (ma *MockAdapter) SupportedTypes() []string {
	return []string{ma.dbType}
}

func (ma *MockAdapter) Connect(ctx context.Context, config *ConnectionConfig) (Connection, error) {
	return &MockConnection{
		BaseConnection: NewBaseConnection(
			"test-connection",
			ma.dbType,
			config,
			logger.NewNoopLogger(),
			metrics.NewMockMetricsCollector(),
		),
	}, nil
}

func (ma *MockAdapter) ValidateConfig(config *ConnectionConfig) error {
	if config.Type != ma.dbType {
		return fmt.Errorf("invalid type: expected %s, got %s", ma.dbType, config.Type)
	}
	return nil
}

func (ma *MockAdapter) SupportsMigrations() bool {
	return ma.dbType == "postgres" || ma.dbType == "mongodb"
}

func (ma *MockAdapter) Migrate(ctx context.Context, conn Connection, migrationsPath string) error {
	return nil // Mock implementation
}

func (ma *MockAdapter) HealthCheck(ctx context.Context, conn Connection) error {
	return nil // Mock implementation
}

// MockConnection is a mock database connection for testing
type MockConnection struct {
	*BaseConnection
	connected bool
}

func NewMockConnection(ctx context.Context, config *ConnectionConfig) *MockConnection {
	return &MockConnection{
		BaseConnection: NewBaseConnection(
			"test-connection",
			"postgres",
			config,
			logger.NewNoopLogger(),
			metrics.NewMockMetricsCollector(),
		),
	}
}

func (mc *MockConnection) Connect(ctx context.Context) error {
	mc.connected = true
	mc.SetConnected(true)
	mc.SetDB(&mockDB{})
	return nil
}

func (mc *MockConnection) Close(ctx context.Context) error {
	mc.connected = false
	mc.SetConnected(false)
	return nil
}

func (mc *MockConnection) Ping(ctx context.Context) error {
	if !mc.connected {
		return fmt.Errorf("connection not established")
	}
	return nil
}

func (mc *MockConnection) Transaction(ctx context.Context, fn func(tx interface{}) error) error {
	return fn(&mockTransaction{})
}

// mockDB is a mock database for testing
type mockDB struct{}

// mockTransaction is a mock transaction for testing
type mockTransaction struct{}

// TestCase represents a database test case
type TestCase struct {
	Name         string
	DatabaseType string
	Setup        func(t *testing.T, conn Connection) error
	Test         func(t *testing.T, conn Connection) error
	Cleanup      func(t *testing.T, conn Connection) error
}

// RunDatabaseTests runs a set of database test cases
func RunDatabaseTests(t *testing.T, testCases []TestCase) {
	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Setup test database manager
			tdm := NewTestDatabaseManager(nil)
			defer func() {
				if err := tdm.Cleanup(); err != nil {
					t.Errorf("cleanup failed: %v", err)
				}
			}()

			// Setup test database
			conn, err := tdm.SetupTestDatabase(t, tc.DatabaseType, nil)
			if err != nil {
				t.Fatalf("failed to setup test database: %v", err)
			}

			// Run setup if provided
			if tc.Setup != nil {
				if err := tc.Setup(t, conn); err != nil {
					t.Fatalf("test setup failed: %v", err)
				}
			}

			// Run the test
			if err := tc.Test(t, conn); err != nil {
				t.Errorf("test failed: %v", err)
			}

			// Run cleanup if provided
			if tc.Cleanup != nil {
				if err := tc.Cleanup(t, conn); err != nil {
					t.Errorf("test cleanup failed: %v", err)
				}
			}

			// Cleanup database
			if err := tdm.CleanupDatabase(t, conn); err != nil {
				t.Errorf("database cleanup failed: %v", err)
			}
		})
	}
}
