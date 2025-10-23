package consensus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Manager provides centralized consensus capabilities management
type Manager struct {
	config    ManagerConfig
	logger    common.Logger
	metrics   common.Metrics
	started   bool
	mu        sync.RWMutex
	shutdownC chan struct{}
	wg        sync.WaitGroup
}

// ManagerConfig contains configuration for the consensus manager
type ManagerConfig struct {
	EnableRaft        bool           `yaml:"enable_raft" default:"true"`
	EnableElection    bool           `yaml:"enable_election" default:"true"`
	EnableDiscovery   bool           `yaml:"enable_discovery" default:"true"`
	EnableStorage     bool           `yaml:"enable_storage" default:"true"`
	EnableTransport   bool           `yaml:"enable_transport" default:"true"`
	EnableMonitoring  bool           `yaml:"enable_monitoring" default:"true"`
	ClusterSize       int            `yaml:"cluster_size" default:"3"`
	ElectionTimeout   time.Duration  `yaml:"election_timeout" default:"5s"`
	HeartbeatInterval time.Duration  `yaml:"heartbeat_interval" default:"1s"`
	StoragePath       string         `yaml:"storage_path" default:"./consensus"`
	TransportPort     int            `yaml:"transport_port" default:"8080"`
	Logger            common.Logger  `yaml:"-"`
	Metrics           common.Metrics `yaml:"-"`
}

// NewManager creates a new consensus manager
func NewManager(config ManagerConfig) (*Manager, error) {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	return &Manager{
		config:    config,
		logger:    config.Logger,
		metrics:   config.Metrics,
		shutdownC: make(chan struct{}),
	}, nil
}

// Start starts the consensus manager
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("consensus manager already started")
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("consensus manager started",
			logger.Bool("raft_enabled", m.config.EnableRaft),
			logger.Bool("election_enabled", m.config.EnableElection),
			logger.Bool("discovery_enabled", m.config.EnableDiscovery),
			logger.Bool("storage_enabled", m.config.EnableStorage),
			logger.Bool("transport_enabled", m.config.EnableTransport),
			logger.Int("cluster_size", m.config.ClusterSize),
		)
	}

	return nil
}

// Stop stops the consensus manager
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	m.started = false
	close(m.shutdownC)

	// Wait for all goroutines to finish
	m.wg.Wait()

	if m.logger != nil {
		m.logger.Info("consensus manager stopped")
	}

	return nil
}

// HealthCheck performs health check on the consensus manager
func (m *Manager) HealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return fmt.Errorf("consensus manager not started")
	}

	// Perform basic health checks
	// In a real implementation, this would check:
	// - Raft cluster health
	// - Leader election status
	// - Service discovery status
	// - Storage connectivity
	// - Transport layer health

	return nil
}

// GetStats returns consensus manager statistics
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := map[string]interface{}{
		"started":            m.started,
		"raft_enabled":       m.config.EnableRaft,
		"election_enabled":   m.config.EnableElection,
		"discovery_enabled":  m.config.EnableDiscovery,
		"storage_enabled":    m.config.EnableStorage,
		"transport_enabled":  m.config.EnableTransport,
		"monitoring_enabled": m.config.EnableMonitoring,
		"cluster_size":       m.config.ClusterSize,
		"election_timeout":   m.config.ElectionTimeout.String(),
		"heartbeat_interval": m.config.HeartbeatInterval.String(),
		"storage_path":       m.config.StoragePath,
		"transport_port":     m.config.TransportPort,
	}

	return stats
}

// IsRaftEnabled returns whether Raft consensus is enabled
func (m *Manager) IsRaftEnabled() bool {
	return m.config.EnableRaft
}

// IsElectionEnabled returns whether leader election is enabled
func (m *Manager) IsElectionEnabled() bool {
	return m.config.EnableElection
}

// IsDiscoveryEnabled returns whether service discovery is enabled
func (m *Manager) IsDiscoveryEnabled() bool {
	return m.config.EnableDiscovery
}

// IsStorageEnabled returns whether persistent storage is enabled
func (m *Manager) IsStorageEnabled() bool {
	return m.config.EnableStorage
}

// IsTransportEnabled returns whether network transport is enabled
func (m *Manager) IsTransportEnabled() bool {
	return m.config.EnableTransport
}

// IsMonitoringEnabled returns whether monitoring is enabled
func (m *Manager) IsMonitoringEnabled() bool {
	return m.config.EnableMonitoring
}

// GetClusterSize returns the configured cluster size
func (m *Manager) GetClusterSize() int {
	return m.config.ClusterSize
}

// GetElectionTimeout returns the election timeout
func (m *Manager) GetElectionTimeout() time.Duration {
	return m.config.ElectionTimeout
}

// GetHeartbeatInterval returns the heartbeat interval
func (m *Manager) GetHeartbeatInterval() time.Duration {
	return m.config.HeartbeatInterval
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() ManagerConfig {
	return m.config
}

// UpdateConfig updates the configuration
func (m *Manager) UpdateConfig(config ManagerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("cannot update config while manager is running")
	}

	m.config = config
	return nil
}
