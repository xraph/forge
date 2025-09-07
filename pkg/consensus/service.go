package consensus

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/consensus/transport"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// ConsensusService implements the common.Service interface for consensus management
type ConsensusService struct {
	manager  *ConsensusManager
	config   *ConsensusConfig
	database database.Connection
	logger   common.Logger
	metrics  common.Metrics
}

// NewConsensusService creates a new consensus service
func NewConsensusService(l common.Logger, metrics common.Metrics, config common.ConfigManager, db database.Connection) *ConsensusService {
	// Load consensus configuration
	consensusConfig := &ConsensusConfig{
		NodeID:            "node-1", // Default, should be overridden
		ClusterID:         "forge-cluster",
		ElectionTimeout:   5 * time.Second,
		HeartbeatInterval: 1 * time.Second,
		RequestTimeout:    3 * time.Second,
		LogCompaction: LogCompactionConfig{
			Enabled:       true,
			RetainEntries: 10000,
		},
		Snapshot: SnapshotConfig{
			Interval: 1 * time.Hour,
			MaxSize:  104857600, // 100MB
		},
		Transport: transport.TransportConfig{
			Type: "grpc",
			Port: 8090,
		},
		Storage: StorageConfig{
			Type: "file",
			File: storage.FileStorageConfig{
				Path: "./data/consensus",
			},
		},
		Discovery: DiscoveryConfig{
			Type:      "static",
			Endpoints: []string{"node-1:8090", "node-2:8090", "node-3:8090"},
			Nodes:     []string{"node-1", "node-2", "node-3"},
		},
	}

	// Load from config manager if available
	if config != nil {
		if err := config.Bind("consensus", consensusConfig); err != nil {
			if l != nil {
				l.Warn("failed to bind consensus config, using defaults", logger.Error(err))
			}
		}
	}

	// Create consensus manager
	manager := NewConsensusManager(consensusConfig, l, metrics, db)

	return &ConsensusService{
		manager:  manager,
		config:   consensusConfig,
		logger:   l,
		metrics:  metrics,
		database: db,
	}
}

// Name returns the service name
func (cs *ConsensusService) Name() string {
	return "consensus-service"
}

// Dependencies returns the service dependencies
func (cs *ConsensusService) Dependencies() []string {
	return []string{"config-manager", "logger", "metrics"}
}

// OnStart starts the consensus service
func (cs *ConsensusService) OnStart(ctx context.Context) error {
	if cs.logger != nil {
		cs.logger.Info("starting consensus service",
			logger.String("node_id", cs.config.NodeID),
			logger.String("cluster_id", cs.config.ClusterID),
		)
	}

	// Start the consensus manager
	if err := cs.manager.OnStart(ctx); err != nil {
		return common.ErrServiceStartFailed("consensus-service", err)
	}

	// Create default cluster if configured
	if cs.config.ClusterID != "" {
		defaultClusterConfig := ClusterConfig{
			ID:           cs.config.ClusterID,
			Nodes:        cs.config.Discovery.Nodes,
			StateMachine: "memory",
			Config:       StateMachineConfig{Type: "memory"},
		}

		if _, err := cs.manager.CreateCluster(ctx, defaultClusterConfig); err != nil {
			// Log error but don't fail startup - cluster might already exist
			if cs.logger != nil {
				cs.logger.Warn("failed to create default cluster",
					logger.String("cluster_id", cs.config.ClusterID),
					logger.Error(err),
				)
			}
		}
	}

	if cs.logger != nil {
		cs.logger.Info("consensus service started successfully")
	}

	return nil
}

// OnStop stops the consensus service
func (cs *ConsensusService) OnStop(ctx context.Context) error {
	if cs.logger != nil {
		cs.logger.Info("stopping consensus service")
	}

	// Stop the consensus manager
	if err := cs.manager.OnStop(ctx); err != nil {
		return common.ErrServiceStopFailed("consensus-service", err)
	}

	if cs.logger != nil {
		cs.logger.Info("consensus service stopped successfully")
	}

	return nil
}

// OnHealthCheck performs health check
func (cs *ConsensusService) OnHealthCheck(ctx context.Context) error {
	return cs.manager.OnHealthCheck(ctx)
}

// GetManager returns the consensus manager
func (cs *ConsensusService) GetManager() *ConsensusManager {
	return cs.manager
}

// GetConfig returns the consensus configuration
func (cs *ConsensusService) GetConfig() *ConsensusConfig {
	return cs.config
}

// CreateCluster creates a new consensus cluster
func (cs *ConsensusService) CreateCluster(ctx context.Context, config ClusterConfig) (Cluster, error) {
	return cs.manager.CreateCluster(ctx, config)
}

// GetCluster returns a cluster by ID
func (cs *ConsensusService) GetCluster(clusterID string) (Cluster, error) {
	return cs.manager.GetCluster(clusterID)
}

// GetClusters returns all clusters
func (cs *ConsensusService) GetClusters() []Cluster {
	return cs.manager.GetClusters()
}

// JoinCluster joins a cluster
func (cs *ConsensusService) JoinCluster(ctx context.Context, clusterID string, nodeID string) error {
	return cs.manager.JoinCluster(ctx, clusterID, nodeID)
}

// LeaveCluster leaves a cluster
func (cs *ConsensusService) LeaveCluster(ctx context.Context, clusterID string, nodeID string) error {
	return cs.manager.LeaveCluster(ctx, clusterID, nodeID)
}

// GetStats returns consensus statistics
func (cs *ConsensusService) GetStats() ConsensusStats {
	return cs.manager.GetStats()
}

// IsHealthy returns true if the service is healthy
func (cs *ConsensusService) IsHealthy() bool {
	return cs.manager.IsStarted()
}

// GetNodeID returns the node ID
func (cs *ConsensusService) GetNodeID() string {
	return cs.manager.GetNodeID()
}

// ProposeChange proposes a change to the default cluster
func (cs *ConsensusService) ProposeChange(ctx context.Context, change StateChange) error {
	cluster, err := cs.manager.GetCluster(cs.config.ClusterID)
	if err != nil {
		return fmt.Errorf("failed to get default cluster: %w", err)
	}

	return cluster.ProposeChange(ctx, change)
}

// GetState returns the state of the default cluster
func (cs *ConsensusService) GetState() interface{} {
	cluster, err := cs.manager.GetCluster(cs.config.ClusterID)
	if err != nil {
		return nil
	}

	return cluster.GetState()
}

// Subscribe subscribes to state changes in the default cluster
func (cs *ConsensusService) Subscribe(callback StateChangeCallback) error {
	cluster, err := cs.manager.GetCluster(cs.config.ClusterID)
	if err != nil {
		return fmt.Errorf("failed to get default cluster: %w", err)
	}

	return cluster.Subscribe(callback)
}

// GetHealth returns the health of the default cluster
func (cs *ConsensusService) GetHealth() ClusterHealth {
	cluster, err := cs.manager.GetCluster(cs.config.ClusterID)
	if err != nil {
		return ClusterHealth{
			Status: common.HealthStatusUnhealthy,
			Issues: []string{fmt.Sprintf("failed to get default cluster: %v", err)},
		}
	}

	return cluster.GetHealth()
}

// ServiceDefinition returns the service definition for DI registration
func ServiceDefinition() common.ServiceDefinition {
	return common.ServiceDefinition{
		Name:         "consensus-service",
		Type:         (*ConsensusService)(nil),
		Constructor:  NewConsensusService,
		Singleton:    true,
		Dependencies: []string{"logger", "metrics", "config-manager"},
		Tags: map[string]string{
			"component": "consensus",
			"phase":     "4",
		},
	}
}

// ConsensusManagerDefinition returns the consensus manager service definition
func ConsensusManagerDefinition() common.ServiceDefinition {
	return common.ServiceDefinition{
		Name: "consensus-manager",
		Type: (*ConsensusManager)(nil),
		Constructor: func(service *ConsensusService) *ConsensusManager {
			return service.GetManager()
		},
		Singleton:    true,
		Dependencies: []string{"consensus-service"},
		Tags: map[string]string{
			"component": "consensus",
			"phase":     "4",
			"type":      "manager",
		},
	}
}

// Interface assertions
var (
	_ common.Service = (*ConsensusService)(nil)
)
