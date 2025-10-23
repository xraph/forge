package pool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ConnectionPool provides connection pooling functionality
type ConnectionPool struct {
	config      PoolConfig
	connections chan Connection
	active      map[string]Connection
	mu          sync.RWMutex
	logger      common.Logger
	metrics     common.Metrics
	stats       PoolStats
	stopC       chan struct{}
	wg          sync.WaitGroup
}

// PoolConfig contains connection pool configuration
type PoolConfig struct {
	Name                string         `yaml:"name"`
	MaxConnections      int            `yaml:"max_connections" default:"100"`
	MinConnections      int            `yaml:"min_connections" default:"5"`
	MaxIdleTime         time.Duration  `yaml:"max_idle_time" default:"30m"`
	MaxLifetime         time.Duration  `yaml:"max_lifetime" default:"1h"`
	AcquireTimeout      time.Duration  `yaml:"acquire_timeout" default:"30s"`
	ReleaseTimeout      time.Duration  `yaml:"release_timeout" default:"5s"`
	HealthCheckInterval time.Duration  `yaml:"health_check_interval" default:"1m"`
	EnableMetrics       bool           `yaml:"enable_metrics" default:"true"`
	EnableHealthCheck   bool           `yaml:"enable_health_check" default:"true"`
	Logger              common.Logger  `yaml:"-"`
	Metrics             common.Metrics `yaml:"-"`
}

// Connection represents a pooled connection
type Connection interface {
	ID() string
	IsHealthy() bool
	Close() error
	Ping() error
	GetStats() ConnectionStats
}

// PoolStats represents pool statistics
type PoolStats struct {
	TotalConnections  int           `json:"total_connections"`
	ActiveConnections int           `json:"active_connections"`
	IdleConnections   int           `json:"idle_connections"`
	TotalAcquires     int64         `json:"total_acquires"`
	TotalReleases     int64         `json:"total_releases"`
	TotalErrors       int64         `json:"total_errors"`
	AverageWaitTime   time.Duration `json:"average_wait_time"`
	LastAcquire       time.Time     `json:"last_acquire"`
	LastRelease       time.Time     `json:"last_release"`
	LastError         time.Time     `json:"last_error"`
}

// ConnectionStats represents connection statistics
type ConnectionStats struct {
	ID          string        `json:"id"`
	CreatedAt   time.Time     `json:"created_at"`
	LastUsed    time.Time     `json:"last_used"`
	TotalUses   int64         `json:"total_uses"`
	TotalErrors int64         `json:"total_errors"`
	IsHealthy   bool          `json:"is_healthy"`
	Latency     time.Duration `json:"latency"`
}

// PoolError represents a pool error
type PoolError struct {
	Type    string    `json:"type"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

func (e *PoolError) Error() string {
	return fmt.Sprintf("pool error [%s]: %s", e.Type, e.Message)
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config PoolConfig) *ConnectionPool {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	pool := &ConnectionPool{
		config:      config,
		connections: make(chan Connection, config.MaxConnections),
		active:      make(map[string]Connection),
		logger:      config.Logger,
		metrics:     config.Metrics,
		stats: PoolStats{
			LastAcquire: time.Now(),
			LastRelease: time.Now(),
		},
		stopC: make(chan struct{}),
	}

	// Initialize minimum connections
	if err := pool.initializeConnections(); err != nil {
		pool.logger.Error("failed to initialize connections", logger.String("error", err.Error()))
	}

	// Start health check goroutine
	if config.EnableHealthCheck {
		pool.wg.Add(1)
		go pool.startHealthCheck()
	}

	return pool
}

// initializeConnections initializes the minimum number of connections
func (cp *ConnectionPool) initializeConnections() error {
	for i := 0; i < cp.config.MinConnections; i++ {
		conn, err := cp.createConnection()
		if err != nil {
			return fmt.Errorf("failed to create connection %d: %w", i, err)
		}

		cp.connections <- conn
		cp.stats.TotalConnections++
		cp.stats.IdleConnections++
	}

	cp.logger.Info("connection pool initialized",
		logger.String("name", cp.config.Name),
		logger.Int("connections", cp.config.MinConnections))

	return nil
}

// createConnection creates a new connection
func (cp *ConnectionPool) createConnection() (Connection, error) {
	// Simple connection creation - in production, use proper connection factory
	conn := &MockConnection{
		id:        fmt.Sprintf("conn-%d", time.Now().UnixNano()),
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		healthy:   true,
	}

	return conn, nil
}

// Acquire acquires a connection from the pool
func (cp *ConnectionPool) Acquire(ctx context.Context) (Connection, error) {
	start := time.Now()
	cp.stats.TotalAcquires++
	cp.stats.LastAcquire = time.Now()

	// Try to get connection from pool
	select {
	case conn := <-cp.connections:
		cp.mu.Lock()
		cp.active[conn.ID()] = conn
		cp.stats.ActiveConnections++
		cp.stats.IdleConnections--
		cp.mu.Unlock()

		cp.recordAcquireMetrics(conn, time.Since(start), true)
		return conn, nil

	case <-ctx.Done():
		cp.recordAcquireMetrics(nil, time.Since(start), false)
		return nil, ctx.Err()

	case <-time.After(cp.config.AcquireTimeout):
		// Try to create new connection if under limit
		if cp.stats.TotalConnections < cp.config.MaxConnections {
			conn, err := cp.createConnection()
			if err != nil {
				cp.recordAcquireMetrics(nil, time.Since(start), false)
				return nil, fmt.Errorf("failed to create connection: %w", err)
			}

			cp.mu.Lock()
			cp.active[conn.ID()] = conn
			cp.stats.ActiveConnections++
			cp.stats.TotalConnections++
			cp.mu.Unlock()

			cp.recordAcquireMetrics(conn, time.Since(start), true)
			return conn, nil
		}

		cp.recordAcquireMetrics(nil, time.Since(start), false)
		return nil, &PoolError{
			Type:    "timeout",
			Message: "acquire timeout",
			Time:    time.Now(),
		}
	}
}

// Release releases a connection back to the pool
func (cp *ConnectionPool) Release(conn Connection) error {
	start := time.Now()
	cp.stats.TotalReleases++
	cp.stats.LastRelease = time.Now()

	cp.mu.Lock()
	delete(cp.active, conn.ID())
	cp.stats.ActiveConnections--
	cp.mu.Unlock()

	// Check if connection is still healthy
	if !conn.IsHealthy() {
		cp.stats.TotalErrors++
		cp.stats.LastError = time.Now()
		cp.recordReleaseMetrics(conn, time.Since(start), false)
		return &PoolError{
			Type:    "unhealthy",
			Message: "connection is unhealthy",
			Time:    time.Now(),
		}
	}

	// Try to return to pool
	select {
	case cp.connections <- conn:
		cp.stats.IdleConnections++
		cp.recordReleaseMetrics(conn, time.Since(start), true)
		return nil

	default:
		// Pool is full, close connection
		if err := conn.Close(); err != nil {
			cp.logger.Error("failed to close connection",
				logger.String("id", conn.ID()),
				logger.String("error", err.Error()))
		}

		cp.stats.TotalConnections--
		cp.recordReleaseMetrics(conn, time.Since(start), true)
		return nil
	}
}

// recordAcquireMetrics records acquire operation metrics
func (cp *ConnectionPool) recordAcquireMetrics(conn Connection, latency time.Duration, success bool) {
	cp.stats.AverageWaitTime = (cp.stats.AverageWaitTime + latency) / 2

	if cp.config.EnableMetrics && cp.metrics != nil {
		result := "success"
		if !success {
			result = "error"
		}

		cp.metrics.Counter("connection_pool_acquire_total", "name", cp.config.Name, "result", result).Inc()

		cp.metrics.Histogram("connection_pool_acquire_duration_seconds", "name", cp.config.Name, "result", result).Observe(latency.Seconds())
	}
}

// recordReleaseMetrics records release operation metrics
func (cp *ConnectionPool) recordReleaseMetrics(conn Connection, latency time.Duration, success bool) {
	if cp.config.EnableMetrics && cp.metrics != nil {
		result := "success"
		if !success {
			result = "error"
		}

		cp.metrics.Counter("connection_pool_release_total", "name", cp.config.Name, "result", result).Inc()

		cp.metrics.Histogram("connection_pool_release_duration_seconds", "name", cp.config.Name, "result", result).Observe(latency.Seconds())
	}
}

// startHealthCheck starts the health check goroutine
func (cp *ConnectionPool) startHealthCheck() {
	defer cp.wg.Done()

	ticker := time.NewTicker(cp.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cp.performHealthCheck()
		case <-cp.stopC:
			return
		}
	}
}

// performHealthCheck performs health checks on connections
func (cp *ConnectionPool) performHealthCheck() {
	cp.mu.RLock()
	connections := make([]Connection, 0, len(cp.active))
	for _, conn := range cp.active {
		connections = append(connections, conn)
	}
	cp.mu.RUnlock()

	// Check active connections
	for _, conn := range connections {
		if !conn.IsHealthy() {
			cp.logger.Warn("unhealthy connection detected",
				logger.String("id", conn.ID()))
		}
	}

	// Check idle connections
	idleConnections := make([]Connection, 0, cp.stats.IdleConnections)
	for i := 0; i < cp.stats.IdleConnections; i++ {
		select {
		case conn := <-cp.connections:
			idleConnections = append(idleConnections, conn)
		default:
			break
		}
	}

	// Check idle connections and remove expired ones
	for _, conn := range idleConnections {
		if cp.isConnectionExpired(conn) {
			if err := conn.Close(); err != nil {
				cp.logger.Error("failed to close expired connection",
					logger.String("id", conn.ID()),
					logger.String("error", err.Error()))
			}
			cp.stats.TotalConnections--
		} else {
			cp.connections <- conn
		}
	}

	cp.logger.Info("health check completed",
		logger.String("name", cp.config.Name),
		logger.Int("active", cp.stats.ActiveConnections),
		logger.Int("idle", cp.stats.IdleConnections))
}

// isConnectionExpired checks if a connection has expired
func (cp *ConnectionPool) isConnectionExpired(conn Connection) bool {
	// Check max lifetime
	if cp.config.MaxLifetime > 0 {
		if time.Since(conn.GetStats().CreatedAt) > cp.config.MaxLifetime {
			return true
		}
	}

	// Check max idle time
	if cp.config.MaxIdleTime > 0 {
		if time.Since(conn.GetStats().LastUsed) > cp.config.MaxIdleTime {
			return true
		}
	}

	return false
}

// GetStats returns pool statistics
func (cp *ConnectionPool) GetStats() PoolStats {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.stats
}

// GetConnectionStats returns connection statistics
func (cp *ConnectionPool) GetConnectionStats() map[string]ConnectionStats {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	stats := make(map[string]ConnectionStats)
	for _, conn := range cp.active {
		stats[conn.ID()] = conn.GetStats()
	}

	return stats
}

// Close closes the connection pool
func (cp *ConnectionPool) Close() error {
	close(cp.stopC)
	cp.wg.Wait()

	// Close all active connections
	cp.mu.Lock()
	for _, conn := range cp.active {
		if err := conn.Close(); err != nil {
			cp.logger.Error("failed to close connection",
				logger.String("id", conn.ID()),
				logger.String("error", err.Error()))
		}
	}
	cp.mu.Unlock()

	// Close all idle connections
	close(cp.connections)
	for conn := range cp.connections {
		if err := conn.Close(); err != nil {
			cp.logger.Error("failed to close idle connection",
				logger.String("id", conn.ID()),
				logger.String("error", err.Error()))
		}
	}

	cp.logger.Info("connection pool closed",
		logger.String("name", cp.config.Name))

	return nil
}

// MockConnection provides a mock connection for testing
type MockConnection struct {
	id        string
	createdAt time.Time
	lastUsed  time.Time
	healthy   bool
	uses      int64
	errors    int64
	latency   time.Duration
}

func (mc *MockConnection) ID() string {
	return mc.id
}

func (mc *MockConnection) IsHealthy() bool {
	return mc.healthy
}

func (mc *MockConnection) Close() error {
	return nil
}

func (mc *MockConnection) Ping() error {
	if !mc.healthy {
		return fmt.Errorf("connection is unhealthy")
	}
	return nil
}

func (mc *MockConnection) GetStats() ConnectionStats {
	return ConnectionStats{
		ID:          mc.id,
		CreatedAt:   mc.createdAt,
		LastUsed:    mc.lastUsed,
		TotalUses:   mc.uses,
		TotalErrors: mc.errors,
		IsHealthy:   mc.healthy,
		Latency:     mc.latency,
	}
}
