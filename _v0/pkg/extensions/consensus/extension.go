package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/consensus"
	"github.com/xraph/forge/v0/pkg/extensions"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ConsensusExtension provides distributed consensus capabilities as an extension
type ConsensusExtension struct {
	manager      *consensus.Manager
	config       ConsensusConfig
	logger       common.Logger
	metrics      common.Metrics
	status       extensions.ExtensionStatus
	startedAt    time.Time
	capabilities []string
}

// ConsensusConfig contains configuration for the consensus extension
type ConsensusConfig struct {
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

// NewConsensusExtension creates a new consensus extension
func NewConsensusExtension(config ConsensusConfig) *ConsensusExtension {
	capabilities := []string{"raft", "election", "discovery"}
	if config.EnableStorage {
		capabilities = append(capabilities, "storage")
	}
	if config.EnableTransport {
		capabilities = append(capabilities, "transport")
	}
	if config.EnableMonitoring {
		capabilities = append(capabilities, "monitoring")
	}

	return &ConsensusExtension{
		config:       config,
		logger:       config.Logger,
		metrics:      config.Metrics,
		status:       extensions.ExtensionStatusUnknown,
		capabilities: capabilities,
	}
}

// Extension interface implementation

func (ce *ConsensusExtension) Name() string {
	return "consensus"
}

func (ce *ConsensusExtension) Version() string {
	return "1.0.0"
}

func (ce *ConsensusExtension) Description() string {
	return "Distributed consensus capabilities including Raft, leader election, and cluster coordination"
}

func (ce *ConsensusExtension) Dependencies() []string {
	return []string{} // No dependencies for now
}

func (ce *ConsensusExtension) Initialize(ctx context.Context, config extensions.ExtensionConfig) error {
	ce.status = extensions.ExtensionStatusLoading

	// Initialize consensus manager
	consensusConfig := consensus.ManagerConfig{
		EnableRaft:        ce.config.EnableRaft,
		EnableElection:    ce.config.EnableElection,
		EnableDiscovery:   ce.config.EnableDiscovery,
		EnableStorage:     ce.config.EnableStorage,
		EnableTransport:   ce.config.EnableTransport,
		EnableMonitoring:  ce.config.EnableMonitoring,
		ClusterSize:       ce.config.ClusterSize,
		ElectionTimeout:   ce.config.ElectionTimeout,
		HeartbeatInterval: ce.config.HeartbeatInterval,
		StoragePath:       ce.config.StoragePath,
		TransportPort:     ce.config.TransportPort,
		Logger:            ce.logger,
		Metrics:           ce.metrics,
	}

	var err error
	ce.manager, err = consensus.NewManager(consensusConfig)
	if err != nil {
		ce.status = extensions.ExtensionStatusError
		return fmt.Errorf("failed to initialize consensus manager: %w", err)
	}

	ce.status = extensions.ExtensionStatusLoaded

	if ce.logger != nil {
		ce.logger.Info("Consensus extension initialized",
			logger.Strings("capabilities", ce.capabilities),
		)
	}

	return nil
}

func (ce *ConsensusExtension) Start(ctx context.Context) error {
	if ce.manager == nil {
		return fmt.Errorf("consensus extension not initialized")
	}

	ce.status = extensions.ExtensionStatusStarting

	if err := ce.manager.Start(ctx); err != nil {
		ce.status = extensions.ExtensionStatusError
		return fmt.Errorf("failed to start consensus manager: %w", err)
	}

	ce.status = extensions.ExtensionStatusRunning
	ce.startedAt = time.Now()

	if ce.logger != nil {
		ce.logger.Info("Consensus extension started")
	}

	return nil
}

func (ce *ConsensusExtension) Stop(ctx context.Context) error {
	if ce.manager == nil {
		return nil
	}

	ce.status = extensions.ExtensionStatusStopping

	if err := ce.manager.Stop(ctx); err != nil {
		ce.logger.Warn("failed to stop consensus manager", logger.Error(err))
	}

	ce.status = extensions.ExtensionStatusStopped

	if ce.logger != nil {
		ce.logger.Info("Consensus extension stopped")
	}

	return nil
}

func (ce *ConsensusExtension) HealthCheck(ctx context.Context) error {
	if ce.manager == nil {
		return fmt.Errorf("consensus extension not initialized")
	}

	if ce.status != extensions.ExtensionStatusRunning {
		return fmt.Errorf("consensus extension not running")
	}

	// Perform health check on consensus manager
	if err := ce.manager.HealthCheck(ctx); err != nil {
		return fmt.Errorf("consensus manager health check failed: %w", err)
	}

	return nil
}

func (ce *ConsensusExtension) GetCapabilities() []string {
	return ce.capabilities
}

func (ce *ConsensusExtension) IsCapabilitySupported(capability string) bool {
	for _, cap := range ce.capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

func (ce *ConsensusExtension) GetConfig() interface{} {
	return ce.config
}

func (ce *ConsensusExtension) UpdateConfig(config interface{}) error {
	if consensusConfig, ok := config.(ConsensusConfig); ok {
		ce.config = consensusConfig
		return nil
	}
	return fmt.Errorf("invalid config type for consensus extension")
}

// Consensus-specific methods

// GetManager returns the consensus manager instance
func (ce *ConsensusExtension) GetManager() *consensus.Manager {
	return ce.manager
}

// GetStats returns consensus extension statistics
func (ce *ConsensusExtension) GetStats() map[string]interface{} {
	if ce.manager == nil {
		return map[string]interface{}{
			"status": ce.status,
		}
	}

	stats := map[string]interface{}{
		"status":       ce.status,
		"started_at":   ce.startedAt,
		"capabilities": ce.capabilities,
	}

	// Add consensus manager stats if available
	if consensusStats := ce.manager.GetStats(); consensusStats != nil {
		stats["consensus_manager"] = consensusStats
	}

	return stats
}

// IsRaftEnabled returns whether Raft consensus is enabled
func (ce *ConsensusExtension) IsRaftEnabled() bool {
	return ce.config.EnableRaft
}

// IsElectionEnabled returns whether leader election is enabled
func (ce *ConsensusExtension) IsElectionEnabled() bool {
	return ce.config.EnableElection
}

// IsDiscoveryEnabled returns whether service discovery is enabled
func (ce *ConsensusExtension) IsDiscoveryEnabled() bool {
	return ce.config.EnableDiscovery
}

// IsStorageEnabled returns whether persistent storage is enabled
func (ce *ConsensusExtension) IsStorageEnabled() bool {
	return ce.config.EnableStorage
}

// IsTransportEnabled returns whether network transport is enabled
func (ce *ConsensusExtension) IsTransportEnabled() bool {
	return ce.config.EnableTransport
}

// IsMonitoringEnabled returns whether monitoring is enabled
func (ce *ConsensusExtension) IsMonitoringEnabled() bool {
	return ce.config.EnableMonitoring
}

// GetClusterSize returns the configured cluster size
func (ce *ConsensusExtension) GetClusterSize() int {
	return ce.config.ClusterSize
}

// GetElectionTimeout returns the election timeout
func (ce *ConsensusExtension) GetElectionTimeout() time.Duration {
	return ce.config.ElectionTimeout
}

// GetHeartbeatInterval returns the heartbeat interval
func (ce *ConsensusExtension) GetHeartbeatInterval() time.Duration {
	return ce.config.HeartbeatInterval
}
