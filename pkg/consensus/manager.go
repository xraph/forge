package consensus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/discovery"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/consensus/transport"
	"github.com/xraph/forge/pkg/database"
	"github.com/xraph/forge/pkg/logger"
)

// ConsensusManager implements the common.Service interface for managing consensus clusters
type ConsensusManager struct {
	clusters   map[string]*ClusterImpl
	config     *ConsensusConfig
	logger     common.Logger
	metrics    common.Metrics
	nodeID     string
	discovery  discovery.Discovery
	transport  transport.Transport
	storage    storage.Storage
	database   database.Connection
	mu         sync.RWMutex
	started    bool
	shutdownCh chan struct{}
}

// ConsensusConfig contains configuration for the consensus system
type ConsensusConfig struct {
	NodeID            string                    `yaml:"node_id"`
	ClusterID         string                    `yaml:"cluster_id"`
	ElectionTimeout   time.Duration             `yaml:"election_timeout" default:"5s"`
	HeartbeatInterval time.Duration             `yaml:"heartbeat_interval" default:"1s"`
	RequestTimeout    time.Duration             `yaml:"request_timeout" default:"3s"`
	LogCompaction     LogCompactionConfig       `yaml:"log_compaction"`
	Snapshot          SnapshotConfig            `yaml:"snapshot"`
	Transport         transport.TransportConfig `yaml:"transport"`
	Storage           StorageConfig             `yaml:"storage"`
	Discovery         discovery.DiscoveryConfig `yaml:"discovery"`
}

type LogCompactionConfig struct {
	Enabled       bool `yaml:"enabled" default:"true"`
	RetainEntries int  `yaml:"retain_entries" default:"10000"`
}

type SnapshotConfig struct {
	Interval time.Duration `yaml:"interval" default:"1h"`
	MaxSize  int64         `yaml:"max_size" default:"104857600"` // 100MB
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	CAFile   string `yaml:"ca_file"`
}

type StorageConfig struct {
	Type     string                        `yaml:"type" default:"file"`
	Memory   storage.MemoryStorageConfig   `yaml:"memory"`
	File     storage.FileStorageConfig     `yaml:"file"`
	Database storage.DatabaseStorageConfig `yaml:"database"`
}

type DiscoveryConfig = discovery.DiscoveryConfig

// ConsensusStats contains statistics about the consensus system
type ConsensusStats struct {
	Clusters       int                     `json:"clusters"`
	ActiveClusters int                     `json:"active_clusters"`
	TotalNodes     int                     `json:"total_nodes"`
	ClusterStats   map[string]ClusterStats `json:"cluster_stats"`
	Uptime         time.Duration           `json:"uptime"`
	StartTime      time.Time               `json:"start_time"`
}

// ClusterStats contains statistics about a specific cluster
type ClusterStats struct {
	ID           string        `json:"id"`
	Status       ClusterStatus `json:"status"`
	LeaderID     string        `json:"leader_id"`
	Term         uint64        `json:"term"`
	CommitIndex  uint64        `json:"commit_index"`
	LastApplied  uint64        `json:"last_applied"`
	Nodes        int           `json:"nodes"`
	ActiveNodes  int           `json:"active_nodes"`
	LastElection time.Time     `json:"last_election"`
	LogSize      int64         `json:"log_size"`
	SnapshotSize int64         `json:"snapshot_size"`
}

// ClusterStatus represents the status of a cluster
type ClusterStatus string

const (
	ClusterStatusActive   ClusterStatus = "active"
	ClusterStatusInactive ClusterStatus = "inactive"
	ClusterStatusElecting ClusterStatus = "electing"
	ClusterStatusError    ClusterStatus = "error"
)

// NewConsensusManager creates a new consensus manager
func NewConsensusManager(config *ConsensusConfig, logger common.Logger, metrics common.Metrics, db database.Connection) *ConsensusManager {
	return &ConsensusManager{
		clusters:   make(map[string]*ClusterImpl),
		config:     config,
		logger:     logger,
		metrics:    metrics,
		nodeID:     config.NodeID,
		database:   db,
		shutdownCh: make(chan struct{}),
	}
}

// Name returns the service name
func (cm *ConsensusManager) Name() string {
	return "consensus-manager"
}

// Dependencies returns the service dependencies
func (cm *ConsensusManager) Dependencies() []string {
	return []string{"config-manager", "logger", "metrics"}
}

// OnStart starts the consensus manager
func (cm *ConsensusManager) OnStart(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return common.ErrLifecycleError("start", fmt.Errorf("consensus manager already started"))
	}

	// Initialize discovery
	discovery, err := cm.createDiscovery()
	if err != nil {
		return common.ErrServiceStartFailed("consensus-manager", err)
	}
	cm.discovery = discovery

	// Initialize transport
	transport, err := cm.createTransport()
	if err != nil {
		return common.ErrServiceStartFailed("consensus-manager", err)
	}
	cm.transport = transport

	// Initialize storage
	storage, err := cm.createStorage()
	if err != nil {
		return common.ErrServiceStartFailed("consensus-manager", err)
	}
	cm.storage = storage

	// OnStart transport
	if err := cm.transport.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("consensus-manager", err)
	}

	cm.started = true

	if cm.logger != nil {
		cm.logger.Info("consensus manager started",
			logger.String("node_id", cm.nodeID),
			logger.String("cluster_id", cm.config.ClusterID),
		)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.consensus.manager_started").Inc()
	}

	return nil
}

// OnStop stops the consensus manager
func (cm *ConsensusManager) OnStop(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("consensus manager not started"))
	}

	// Signal shutdown
	close(cm.shutdownCh)

	// OnStop all clusters
	for _, cluster := range cm.clusters {
		if err := cluster.Stop(ctx); err != nil {
			if cm.logger != nil {
				cm.logger.Error("failed to stop cluster",
					logger.String("cluster_id", cluster.ID()),
					logger.Error(err),
				)
			}
		}
	}

	// OnStop transport
	if cm.transport != nil {
		if err := cm.transport.Stop(ctx); err != nil {
			if cm.logger != nil {
				cm.logger.Error("failed to stop transport", logger.Error(err))
			}
		}
	}

	// OnStop storage
	if cm.storage != nil {
		if err := cm.storage.Close(ctx); err != nil {
			if cm.logger != nil {
				cm.logger.Error("failed to close storage", logger.Error(err))
			}
		}
	}

	cm.started = false

	if cm.logger != nil {
		cm.logger.Info("consensus manager stopped")
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.consensus.manager_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs health check
func (cm *ConsensusManager) OnHealthCheck(ctx context.Context) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if !cm.started {
		return common.ErrHealthCheckFailed("consensus-manager", fmt.Errorf("consensus manager not started"))
	}

	// Check cluster health
	for id, cluster := range cm.clusters {
		if err := cluster.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("consensus-manager", fmt.Errorf("cluster %s unhealthy: %w", id, err))
		}
	}

	// Check transport health
	if cm.transport != nil {
		if err := cm.transport.HealthCheck(ctx); err != nil {
			return common.ErrHealthCheckFailed("consensus-manager", fmt.Errorf("transport unhealthy: %w", err))
		}
	}

	return nil
}

// CreateCluster creates a new consensus cluster
func (cm *ConsensusManager) CreateCluster(ctx context.Context, config ClusterConfig) (Cluster, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return nil, common.ErrLifecycleError("create_cluster", fmt.Errorf("consensus manager not started"))
	}

	clusterID := config.ID
	if _, exists := cm.clusters[clusterID]; exists {
		return nil, common.ErrServiceAlreadyExists(clusterID)
	}

	// Create cluster
	cluster := NewCluster(clusterID, config, cm.nodeID, cm.transport, cm.storage, cm.discovery, cm.logger, cm.metrics)

	// OnStart cluster
	if err := cluster.Start(ctx); err != nil {
		return nil, common.ErrContainerError("start_cluster", err)
	}

	cm.clusters[clusterID] = cluster

	if cm.logger != nil {
		cm.logger.Info("cluster created",
			logger.String("cluster_id", clusterID),
			logger.Int("nodes", len(config.Nodes)),
		)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("forge.consensus.clusters_created").Inc()
		cm.metrics.Gauge("forge.consensus.clusters_active").Set(float64(len(cm.clusters)))
	}

	return cluster, nil
}

// JoinCluster joins an existing cluster
func (cm *ConsensusManager) JoinCluster(ctx context.Context, clusterID string, nodeID string) error {
	cm.mu.RLock()
	cluster, exists := cm.clusters[clusterID]
	cm.mu.RUnlock()

	if !exists {
		return common.ErrServiceNotFound(clusterID)
	}

	return cluster.JoinNode(ctx, nodeID)
}

// LeaveCluster leaves a cluster
func (cm *ConsensusManager) LeaveCluster(ctx context.Context, clusterID string, nodeID string) error {
	cm.mu.RLock()
	cluster, exists := cm.clusters[clusterID]
	cm.mu.RUnlock()

	if !exists {
		return common.ErrServiceNotFound(clusterID)
	}

	return cluster.LeaveNode(ctx, nodeID)
}

// GetCluster returns a cluster by ID
func (cm *ConsensusManager) GetCluster(clusterID string) (Cluster, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	cluster, exists := cm.clusters[clusterID]
	if !exists {
		return nil, common.ErrServiceNotFound(clusterID)
	}

	return cluster, nil
}

// GetClusters returns all clusters
func (cm *ConsensusManager) GetClusters() []Cluster {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clusters := make([]Cluster, 0, len(cm.clusters))
	for _, cluster := range cm.clusters {
		clusters = append(clusters, cluster)
	}

	return clusters
}

// GetStats returns consensus statistics
func (cm *ConsensusManager) GetStats() ConsensusStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	stats := ConsensusStats{
		Clusters:       len(cm.clusters),
		ActiveClusters: 0,
		TotalNodes:     0,
		ClusterStats:   make(map[string]ClusterStats),
		StartTime:      time.Now(), // This should be stored when started
	}

	for id, cluster := range cm.clusters {
		clusterStats := cluster.GetStats()
		stats.ClusterStats[id] = clusterStats
		stats.TotalNodes += clusterStats.Nodes

		if clusterStats.Status == ClusterStatusActive {
			stats.ActiveClusters++
		}
	}

	return stats
}

// createDiscovery creates a discovery service based on configuration
func (cm *ConsensusManager) createDiscovery() (discovery.Discovery, error) {
	switch cm.config.Discovery.Type {
	case "static":
		return discovery.NewStaticDiscoveryFactory(cm.logger, cm.metrics).Create(cm.config.Discovery)
	case "dns":
		return discovery.NewDNSDiscoveryFactory(cm.logger, cm.metrics).Create(cm.config.Discovery)
	case "consul":
		return discovery.NewConsulDiscoveryFactory(cm.logger, cm.metrics).Create(cm.config.Discovery)
	case "kubernetes":
		return discovery.NewKubernetesDiscoveryFactory(cm.logger, cm.metrics).Create(cm.config.Discovery)
	default:
		return nil, fmt.Errorf("unsupported discovery type: %s", cm.config.Discovery.Type)
	}
}

// createTransport creates a transport service based on configuration
func (cm *ConsensusManager) createTransport() (transport.Transport, error) {
	switch cm.config.Transport.Type {
	case "grpc":
		return transport.NewGRPCTransportFactory(cm.logger, cm.metrics).Create(cm.config.Transport)
	case "http":
		return transport.NewHTTPTransportFactory(cm.logger, cm.metrics).Create(cm.config.Transport)
	case "tcp":
		return transport.NewTCPTransportFactory(cm.logger, cm.metrics).Create(cm.config.Transport)
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", cm.config.Transport.Type)
	}
}

// createStorage creates a storage service based on configuration
func (cm *ConsensusManager) createStorage() (storage.Storage, error) {
	switch cm.config.Storage.Type {
	case "file":
		return storage.NewFileStorage(cm.config.Storage.File, cm.logger, cm.metrics)
	case "database":
		return storage.NewDatabaseStorage(cm.config.Storage.Database, cm.database, cm.logger, cm.metrics)
	case "memory":
		return storage.NewMemoryStorage(cm.config.Storage.Memory, cm.logger, cm.metrics), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cm.config.Storage.Type)
	}
}

// IsStarted returns true if the consensus manager is started
func (cm *ConsensusManager) IsStarted() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.started
}

// GetNodeID returns the node ID
func (cm *ConsensusManager) GetNodeID() string {
	return cm.nodeID
}

// GetConfig returns the consensus configuration
func (cm *ConsensusManager) GetConfig() *ConsensusConfig {
	return cm.config
}
