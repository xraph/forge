package transport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ConnectionPool manages connection pooling for transports
type ConnectionPool struct {
	nodeID string
	logger forge.Logger

	// Connections
	connections map[string]*PooledConnection
	connMu      sync.RWMutex

	// Configuration
	config PoolConfig

	// Statistics
	stats PoolStatistics
}

// PooledConnection represents a pooled connection
type PooledConnection struct {
	PeerID     string
	Connection interface{}
	Created    time.Time
	LastUsed   time.Time
	UseCount   int64
	InUse      bool
	Healthy    bool
	mu         sync.RWMutex
}

// PoolConfig contains pool configuration
type PoolConfig struct {
	MaxConnections      int
	MaxIdleTime         time.Duration
	MaxConnectionAge    time.Duration
	HealthCheckInterval time.Duration
	ConnectTimeout      time.Duration
}

// PoolStatistics contains pool statistics
type PoolStatistics struct {
	TotalConnections   int64
	ActiveConnections  int64
	IdleConnections    int64
	CreatedConnections int64
	ClosedConnections  int64
	FailedConnections  int64
	TotalUses          int64
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(nodeID string, config PoolConfig, logger forge.Logger) *ConnectionPool {
	// Set defaults
	if config.MaxConnections == 0 {
		config.MaxConnections = 100
	}
	if config.MaxIdleTime == 0 {
		config.MaxIdleTime = 5 * time.Minute
	}
	if config.MaxConnectionAge == 0 {
		config.MaxConnectionAge = 30 * time.Minute
	}
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 30 * time.Second
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 5 * time.Second
	}

	return &ConnectionPool{
		nodeID:      nodeID,
		logger:      logger,
		connections: make(map[string]*PooledConnection),
		config:      config,
	}
}

// Get gets or creates a connection for a peer
func (cp *ConnectionPool) Get(ctx context.Context, peerID string, creator func() (interface{}, error)) (*PooledConnection, error) {
	cp.connMu.Lock()
	defer cp.connMu.Unlock()

	// Check if connection exists
	if conn, exists := cp.connections[peerID]; exists {
		conn.mu.Lock()
		defer conn.mu.Unlock()

		// Check if connection is still valid
		if !conn.InUse && conn.Healthy && !cp.isExpired(conn) {
			conn.InUse = true
			conn.LastUsed = time.Now()
			conn.UseCount++
			cp.stats.TotalUses++

			cp.logger.Debug("reusing pooled connection",
				forge.F("peer", peerID),
				forge.F("use_count", conn.UseCount),
			)

			return conn, nil
		}

		// Connection expired or unhealthy, close it
		if !conn.Healthy || cp.isExpired(conn) {
			delete(cp.connections, peerID)
			cp.stats.ClosedConnections++
		}
	}

	// Check pool capacity
	if len(cp.connections) >= cp.config.MaxConnections {
		// Try to remove an idle connection
		if !cp.removeIdleConnection() {
			return nil, internal.ErrPoolExhausted
		}
	}

	// Create new connection
	conn, err := cp.createConnection(ctx, peerID, creator)
	if err != nil {
		cp.stats.FailedConnections++
		return nil, err
	}

	cp.connections[peerID] = conn
	cp.stats.CreatedConnections++

	cp.logger.Debug("created new pooled connection",
		forge.F("peer", peerID),
		forge.F("pool_size", len(cp.connections)),
	)

	return conn, nil
}

// Release releases a connection back to the pool
func (cp *ConnectionPool) Release(peerID string) {
	cp.connMu.RLock()
	conn, exists := cp.connections[peerID]
	cp.connMu.RUnlock()

	if !exists {
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	conn.InUse = false
	conn.LastUsed = time.Now()

	cp.logger.Debug("released connection",
		forge.F("peer", peerID),
	)
}

// Close closes a connection
func (cp *ConnectionPool) Close(peerID string) error {
	cp.connMu.Lock()
	defer cp.connMu.Unlock()

	conn, exists := cp.connections[peerID]
	if !exists {
		return fmt.Errorf("connection not found: %s", peerID)
	}

	// Close the connection if it has a Close method
	if closer, ok := conn.Connection.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			cp.logger.Warn("failed to close connection",
				forge.F("peer", peerID),
				forge.F("error", err),
			)
		}
	}

	delete(cp.connections, peerID)
	cp.stats.ClosedConnections++

	cp.logger.Debug("closed connection",
		forge.F("peer", peerID),
	)

	return nil
}

// CloseAll closes all connections
func (cp *ConnectionPool) CloseAll() {
	cp.connMu.Lock()
	defer cp.connMu.Unlock()

	for peerID := range cp.connections {
		delete(cp.connections, peerID)
		cp.stats.ClosedConnections++
	}

	cp.logger.Info("closed all connections",
		forge.F("count", len(cp.connections)),
	)
}

// createConnection creates a new connection
func (cp *ConnectionPool) createConnection(ctx context.Context, peerID string, creator func() (interface{}, error)) (*PooledConnection, error) {
	// Create with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, cp.config.ConnectTimeout)
	defer cancel()

	connChan := make(chan interface{}, 1)
	errChan := make(chan error, 1)

	go func() {
		conn, err := creator()
		if err != nil {
			errChan <- err
			return
		}
		connChan <- conn
	}()

	select {
	case conn := <-connChan:
		return &PooledConnection{
			PeerID:     peerID,
			Connection: conn,
			Created:    time.Now(),
			LastUsed:   time.Now(),
			UseCount:   1,
			InUse:      true,
			Healthy:    true,
		}, nil

	case err := <-errChan:
		return nil, err

	case <-timeoutCtx.Done():
		return nil, internal.ErrConnectionTimeout
	}
}

// isExpired checks if a connection is expired
func (cp *ConnectionPool) isExpired(conn *PooledConnection) bool {
	// Check age
	if time.Since(conn.Created) > cp.config.MaxConnectionAge {
		return true
	}

	// Check idle time
	if !conn.InUse && time.Since(conn.LastUsed) > cp.config.MaxIdleTime {
		return true
	}

	return false
}

// removeIdleConnection removes an idle connection
func (cp *ConnectionPool) removeIdleConnection() bool {
	for peerID, conn := range cp.connections {
		conn.mu.RLock()
		idle := !conn.InUse
		conn.mu.RUnlock()

		if idle {
			delete(cp.connections, peerID)
			cp.stats.ClosedConnections++
			return true
		}
	}
	return false
}

// Cleanup removes expired connections
func (cp *ConnectionPool) Cleanup() int {
	cp.connMu.Lock()
	defer cp.connMu.Unlock()

	removed := 0
	for peerID, conn := range cp.connections {
		conn.mu.RLock()
		expired := cp.isExpired(conn)
		inUse := conn.InUse
		conn.mu.RUnlock()

		if expired && !inUse {
			delete(cp.connections, peerID)
			cp.stats.ClosedConnections++
			removed++
		}
	}

	if removed > 0 {
		cp.logger.Debug("cleaned up expired connections",
			forge.F("removed", removed),
		)
	}

	return removed
}

// MonitorHealth monitors connection health
func (cp *ConnectionPool) MonitorHealth(ctx context.Context) {
	ticker := time.NewTicker(cp.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cp.Cleanup()
			cp.updateStatistics()
		}
	}
}

// updateStatistics updates pool statistics
func (cp *ConnectionPool) updateStatistics() {
	cp.connMu.RLock()
	defer cp.connMu.RUnlock()

	cp.stats.TotalConnections = int64(len(cp.connections))
	cp.stats.ActiveConnections = 0
	cp.stats.IdleConnections = 0

	for _, conn := range cp.connections {
		conn.mu.RLock()
		if conn.InUse {
			cp.stats.ActiveConnections++
		} else {
			cp.stats.IdleConnections++
		}
		conn.mu.RUnlock()
	}
}

// GetStatistics returns pool statistics
func (cp *ConnectionPool) GetStatistics() PoolStatistics {
	cp.updateStatistics()
	return cp.stats
}

// GetConnection returns a connection for a peer
func (cp *ConnectionPool) GetConnection(peerID string) (*PooledConnection, bool) {
	cp.connMu.RLock()
	defer cp.connMu.RUnlock()

	conn, exists := cp.connections[peerID]
	return conn, exists
}

// GetAllConnections returns all connections
func (cp *ConnectionPool) GetAllConnections() map[string]*PooledConnection {
	cp.connMu.RLock()
	defer cp.connMu.RUnlock()

	result := make(map[string]*PooledConnection)
	for peerID, conn := range cp.connections {
		result[peerID] = conn
	}

	return result
}

// MarkUnhealthy marks a connection as unhealthy
func (cp *ConnectionPool) MarkUnhealthy(peerID string) {
	cp.connMu.RLock()
	conn, exists := cp.connections[peerID]
	cp.connMu.RUnlock()

	if !exists {
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	conn.Healthy = false

	cp.logger.Warn("connection marked unhealthy",
		forge.F("peer", peerID),
	)
}

// MarkHealthy marks a connection as healthy
func (cp *ConnectionPool) MarkHealthy(peerID string) {
	cp.connMu.RLock()
	conn, exists := cp.connections[peerID]
	cp.connMu.RUnlock()

	if !exists {
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	conn.Healthy = true

	cp.logger.Debug("connection marked healthy",
		forge.F("peer", peerID),
	)
}

// Size returns the current pool size
func (cp *ConnectionPool) Size() int {
	cp.connMu.RLock()
	defer cp.connMu.RUnlock()
	return len(cp.connections)
}

// Available returns the number of available connection slots
func (cp *ConnectionPool) Available() int {
	cp.connMu.RLock()
	defer cp.connMu.RUnlock()
	return cp.config.MaxConnections - len(cp.connections)
}
