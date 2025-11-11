package consensus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Service implements internal.ConsensusService.
type Service struct {
	config  internal.Config
	logger  forge.Logger
	metrics forge.Metrics

	// Core components
	raftNode       internal.RaftNode
	stateMachine   internal.StateMachine
	clusterManager internal.ClusterManager
	transport      internal.Transport
	discovery      internal.Discovery
	storage        internal.Storage

	// State
	role      internal.NodeRole
	term      uint64
	leader    string
	startTime time.Time
	started   bool
	mu        sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics tracking
	operations       int64
	operationsFailed int64
	elections        int64
	electionsFailed  int64
	snapshots        int64
	lastSnapshot     time.Time
	metricsLock      sync.Mutex
}

// NewConsensusService creates a new consensus service.
func NewConsensusService(config internal.Config, logger forge.Logger, metrics forge.Metrics) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	s := &Service{
		config:    config,
		logger:    logger,
		metrics:   metrics,
		role:      RoleFollower,
		startTime: time.Now(),
	}

	return s, nil
}

// Start starts the consensus service.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return ErrAlreadyStarted
	}

	s.ctx, s.cancel = context.WithCancel(ctx)

	s.logger.Info("starting consensus service",
		forge.F("node_id", s.config.NodeID),
		forge.F("cluster_id", s.config.ClusterID),
		forge.F("bind_addr", s.config.BindAddr),
		forge.F("bind_port", s.config.BindPort),
	)

	// Initialize storage
	if err := s.initStorage(); err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Initialize transport
	if err := s.initTransport(); err != nil {
		return fmt.Errorf("failed to initialize transport: %w", err)
	}

	// Initialize discovery
	if err := s.initDiscovery(); err != nil {
		return fmt.Errorf("failed to initialize discovery: %w", err)
	}

	// Initialize cluster manager
	if err := s.initClusterManager(); err != nil {
		return fmt.Errorf("failed to initialize cluster manager: %w", err)
	}

	// Initialize state machine
	if err := s.initStateMachine(); err != nil {
		return fmt.Errorf("failed to initialize state machine: %w", err)
	}

	// Initialize Raft node
	if err := s.initRaftNode(); err != nil {
		return fmt.Errorf("failed to initialize raft node: %w", err)
	}

	// Start background tasks
	s.startBackgroundTasks()

	s.started = true
	s.startTime = time.Now()

	s.logger.Info("consensus service started",
		forge.F("node_id", s.config.NodeID),
	)

	return nil
}

// Stop stops the consensus service.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()

	if !s.started {
		s.mu.Unlock()

		return nil
	}

	s.started = false
	s.mu.Unlock()

	s.logger.Info("stopping consensus service",
		forge.F("node_id", s.config.NodeID),
	)

	// Cancel context and wait for background tasks
	if s.cancel != nil {
		s.cancel()
	}

	// Wait for background tasks with timeout
	done := make(chan struct{})

	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		s.logger.Warn("timeout waiting for background tasks to stop")
	}

	// Stop components in reverse order
	if s.raftNode != nil {
		if err := s.raftNode.Stop(ctx); err != nil {
			s.logger.Error("failed to stop raft node", forge.F("error", err))
		}
	}

	if s.discovery != nil {
		if err := s.discovery.Stop(ctx); err != nil {
			s.logger.Error("failed to stop discovery", forge.F("error", err))
		}
	}

	if s.transport != nil {
		if err := s.transport.Stop(ctx); err != nil {
			s.logger.Error("failed to stop transport", forge.F("error", err))
		}
	}

	if s.storage != nil {
		if err := s.storage.Stop(ctx); err != nil {
			s.logger.Error("failed to stop storage", forge.F("error", err))
		}
	}

	s.logger.Info("consensus service stopped")

	return nil
}

// IsLeader returns true if this node is the leader.
func (s *Service) IsLeader() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.role == internal.RoleLeader
}

// GetLeader returns the current leader node ID.
func (s *Service) GetLeader() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.leader
}

// GetRole returns the current role of this node.
func (s *Service) GetRole() NodeRole {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.role
}

// GetTerm returns the current term.
func (s *Service) GetTerm() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.term
}

// Apply applies a command to the state machine.
func (s *Service) Apply(ctx context.Context, cmd Command) error {
	s.mu.RLock()

	if !s.started {
		s.mu.RUnlock()

		return ErrNotStarted
	}

	s.mu.RUnlock()

	// Only leader can accept writes
	if !s.IsLeader() {
		return NewNotLeaderError(s.config.NodeID, s.GetLeader())
	}

	start := time.Now()

	defer func() {
		s.recordOperation(time.Since(start), true)
	}()

	// Create log entry
	entry := LogEntry{
		Type:    EntryNormal,
		Data:    s.encodeCommand(cmd),
		Created: time.Now(),
	}

	// Apply through Raft
	if err := s.raftNode.Apply(ctx, entry); err != nil {
		s.recordOperation(time.Since(start), false)

		return fmt.Errorf("failed to apply command: %w", err)
	}

	return nil
}

// Read performs a consistent read operation.
func (s *Service) Read(ctx context.Context, query any) (any, error) {
	s.mu.RLock()

	if !s.started {
		s.mu.RUnlock()

		return nil, ErrNotStarted
	}

	s.mu.RUnlock()

	// For now, allow reads from any node (eventually consistent)
	// TODO: Implement linearizable reads with read index

	return s.stateMachine.Query(query)
}

// GetStats returns consensus statistics.
func (s *Service) GetStats() internal.ConsensusStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.metricsLock.Lock()
	defer s.metricsLock.Unlock()

	var raftStats internal.RaftStats
	if s.raftNode != nil {
		raftStats = s.raftNode.GetStats()
	}

	var clusterSize, healthyNodes int
	if s.clusterManager != nil {
		clusterSize = s.clusterManager.GetClusterSize()
		healthyNodes = s.clusterManager.GetHealthyNodes()
	}

	uptime := time.Since(s.startTime)

	var opsPerSec float64
	if uptime.Seconds() > 0 {
		opsPerSec = float64(s.operations) / uptime.Seconds()
	}

	var errorRate float64
	if s.operations > 0 {
		errorRate = float64(s.operationsFailed) / float64(s.operations)
	}

	return internal.ConsensusStats{
		NodeID:           s.config.NodeID,
		ClusterID:        s.config.ClusterID,
		Role:             s.role,
		Status:           internal.StatusActive,
		Term:             s.term,
		LeaderID:         s.leader,
		CommitIndex:      raftStats.CommitIndex,
		LastApplied:      raftStats.LastApplied,
		LastLogIndex:     raftStats.LastLogIndex,
		LastLogTerm:      raftStats.LastLogTerm,
		ClusterSize:      clusterSize,
		HealthyNodes:     healthyNodes,
		HasQuorum:        s.clusterManager != nil && s.clusterManager.HasQuorum(),
		ElectionsTotal:   s.elections,
		ElectionsFailed:  s.electionsFailed,
		OperationsTotal:  s.operations,
		OperationsFailed: s.operationsFailed,
		OperationsPerSec: opsPerSec,
		ErrorRate:        errorRate,
		SnapshotsTotal:   s.snapshots,
		LastSnapshotTime: s.lastSnapshot,
		Uptime:           uptime,
		StartTime:        s.startTime,
	}
}

// HealthCheck performs a health check.
func (s *Service) HealthCheck(ctx context.Context) error {
	s.mu.RLock()

	if !s.started {
		s.mu.RUnlock()

		return ErrNotStarted
	}

	s.mu.RUnlock()

	// Check storage
	if s.storage == nil {
		return errors.New("storage not initialized")
	}

	// Check transport
	if s.transport == nil {
		return errors.New("transport not initialized")
	}

	// Check if we have quorum (if not leader)
	if !s.IsLeader() && s.clusterManager != nil && !s.clusterManager.HasQuorum() {
		return errors.New("no quorum")
	}

	return nil
}

// GetHealthStatus returns detailed health status.
func (s *Service) GetHealthStatus(ctx context.Context) HealthStatus {
	s.mu.RLock()
	started := s.started
	s.mu.RUnlock()

	checks := make([]HealthCheck, 0)
	healthy := true

	// Service started check
	checks = append(checks, HealthCheck{
		Name:      "service_started",
		Healthy:   started,
		Message:   "Consensus service is running",
		CheckedAt: time.Now(),
	})
	if !started {
		healthy = false
	}

	// Storage check
	storageHealthy := s.storage != nil

	checks = append(checks, HealthCheck{
		Name:      "storage",
		Healthy:   storageHealthy,
		Message:   "Storage backend is available",
		CheckedAt: time.Now(),
	})
	if !storageHealthy {
		healthy = false
	}

	// Transport check
	transportHealthy := s.transport != nil

	checks = append(checks, HealthCheck{
		Name:      "transport",
		Healthy:   transportHealthy,
		Message:   "Transport layer is available",
		CheckedAt: time.Now(),
	})
	if !transportHealthy {
		healthy = false
	}

	// Quorum check
	hasQuorum := s.clusterManager != nil && s.clusterManager.HasQuorum()
	checks = append(checks, HealthCheck{
		Name:      "quorum",
		Healthy:   hasQuorum,
		Message:   "Cluster has quorum",
		CheckedAt: time.Now(),
	})

	var totalNodes, activeNodes int
	if s.clusterManager != nil {
		totalNodes = s.clusterManager.GetClusterSize()
		activeNodes = s.clusterManager.GetHealthyNodes()
	}

	status := "healthy"
	if !healthy {
		status = "unhealthy"
	} else if !hasQuorum {
		status = "degraded"
	}

	return HealthStatus{
		Healthy:     healthy,
		Status:      status,
		Leader:      s.IsLeader(),
		HasQuorum:   hasQuorum,
		TotalNodes:  totalNodes,
		ActiveNodes: activeNodes,
		Details:     checks,
		LastCheck:   time.Now(),
		Checks:      make(map[string]any),
	}
}

// GetClusterInfo returns cluster information.
func (s *Service) GetClusterInfo() ClusterInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var (
		nodes                   []NodeInfo
		totalNodes, activeNodes int
	)

	if s.clusterManager != nil {
		nodes = s.clusterManager.GetNodes()

		totalNodes = len(nodes)
		for _, node := range nodes {
			if node.Status == StatusActive {
				activeNodes++
			}
		}
	}

	var commitIndex, lastApplied uint64

	if s.raftNode != nil {
		stats := s.raftNode.GetStats()
		commitIndex = stats.CommitIndex
		lastApplied = stats.LastApplied
	}

	return ClusterInfo{
		ID:          s.config.ClusterID,
		Leader:      s.leader,
		Term:        s.term,
		Nodes:       nodes,
		TotalNodes:  totalNodes,
		ActiveNodes: activeNodes,
		HasQuorum:   s.clusterManager != nil && s.clusterManager.HasQuorum(),
		CommitIndex: commitIndex,
		LastApplied: lastApplied,
	}
}

// AddNode adds a node to the cluster.
func (s *Service) AddNode(ctx context.Context, nodeID, address string, port int) error {
	if !s.IsLeader() {
		return NewNotLeaderError(s.config.NodeID, s.GetLeader())
	}

	if s.clusterManager == nil {
		return errors.New("cluster manager not initialized")
	}

	return s.clusterManager.AddNode(nodeID, address, port)
}

// RemoveNode removes a node from the cluster.
func (s *Service) RemoveNode(ctx context.Context, nodeID string) error {
	if !s.IsLeader() {
		return NewNotLeaderError(s.config.NodeID, s.GetLeader())
	}

	if s.clusterManager == nil {
		return errors.New("cluster manager not initialized")
	}

	return s.clusterManager.RemoveNode(nodeID)
}

// TransferLeadership transfers leadership to another node.
func (s *Service) TransferLeadership(ctx context.Context, targetNodeID string) error {
	if !s.IsLeader() {
		return ErrNotLeader
	}

	// TODO: Implement leadership transfer
	return errors.New("leadership transfer not yet implemented")
}

// StepDown causes the leader to step down.
func (s *Service) StepDown(ctx context.Context) error {
	if !s.IsLeader() {
		return ErrNotLeader
	}

	// TODO: Implement step down
	return errors.New("step down not yet implemented")
}

// Snapshot creates a snapshot.
func (s *Service) Snapshot(ctx context.Context) error {
	s.mu.RLock()

	if !s.started {
		s.mu.RUnlock()

		return ErrNotStarted
	}

	s.mu.RUnlock()

	start := time.Now()

	snapshot, err := s.stateMachine.Snapshot()
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	s.metricsLock.Lock()
	s.snapshots++
	s.lastSnapshot = time.Now()
	s.metricsLock.Unlock()

	s.logger.Info("snapshot created",
		forge.F("index", snapshot.Index),
		forge.F("term", snapshot.Term),
		forge.F("size", snapshot.Size),
		forge.F("duration", time.Since(start)),
	)

	return nil
}

// UpdateConfig updates the configuration.
func (s *Service) UpdateConfig(ctx context.Context, config Config) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Apply hot-reloadable settings
	s.config.Observability = config.Observability
	s.config.Health = config.Health
	s.config.AdminAPI = config.AdminAPI

	s.logger.Info("configuration updated")

	return nil
}

// GetRaftNode returns the Raft node.
func (s *Service) GetRaftNode() RaftNode {
	return s.raftNode
}

// GetStateMachine returns the state machine.
func (s *Service) GetStateMachine() StateMachine {
	return s.stateMachine
}

// GetClusterManager returns the cluster manager.
func (s *Service) GetClusterManager() ClusterManager {
	return s.clusterManager
}

// GetTransport returns the transport.
func (s *Service) GetTransport() Transport {
	return s.transport
}

// GetDiscovery returns the discovery service.
func (s *Service) GetDiscovery() Discovery {
	return s.discovery
}

// GetStorage returns the storage backend.
func (s *Service) GetStorage() Storage {
	return s.storage
}

// Initialize methods (placeholders - will be implemented in separate files)

func (s *Service) initStorage() error {
	// Will be implemented with actual storage backends
	s.logger.Info("initializing storage",
		forge.F("type", s.config.Storage.Type),
		forge.F("path", s.config.Storage.Path),
	)

	return nil
}

func (s *Service) initTransport() error {
	// Will be implemented with actual transport implementations
	s.logger.Info("initializing transport",
		forge.F("type", s.config.Transport.Type),
		forge.F("bind", fmt.Sprintf("%s:%d", s.config.BindAddr, s.config.BindPort)),
	)

	return nil
}

func (s *Service) initDiscovery() error {
	// Will be implemented with actual discovery implementations
	s.logger.Info("initializing discovery",
		forge.F("type", s.config.Discovery.Type),
	)

	return nil
}

func (s *Service) initClusterManager() error {
	// Will be implemented with actual cluster manager
	s.logger.Info("initializing cluster manager")

	return nil
}

func (s *Service) initStateMachine() error {
	// Will be implemented with actual state machine
	s.logger.Info("initializing state machine")

	return nil
}

func (s *Service) initRaftNode() error {
	// Will be implemented with actual Raft implementation
	s.logger.Info("initializing raft node",
		forge.F("node_id", s.config.NodeID),
	)

	return nil
}

func (s *Service) startBackgroundTasks() {
	// Health check task
	if s.config.Health.Enabled {
		s.wg.Add(1)

		go s.healthCheckLoop()
	}

	// Metrics collection task
	if s.config.Observability.Metrics.Enabled {
		s.wg.Add(1)

		go s.metricsCollectionLoop()
	}

	// Auto snapshot task
	if s.config.Advanced.EnableAutoSnapshot {
		s.wg.Add(1)

		go s.autoSnapshotLoop()
	}
}

func (s *Service) healthCheckLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.Health.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.HealthCheck(s.ctx); err != nil {
				s.logger.Warn("health check failed", forge.F("error", err))
			}
		}
	}
}

func (s *Service) metricsCollectionLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.Observability.Metrics.CollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.collectMetrics()
		}
	}
}

func (s *Service) autoSnapshotLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.Raft.SnapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.Snapshot(s.ctx); err != nil {
				s.logger.Error("auto snapshot failed", forge.F("error", err))
			}
		}
	}
}

func (s *Service) collectMetrics() {
	if s.metrics == nil {
		return
	}

	stats := s.GetStats()

	// Record metrics
	namespace := s.config.Observability.Metrics.Namespace

	s.metrics.Gauge(namespace + ".cluster.size").Set(float64(stats.ClusterSize))
	s.metrics.Gauge(namespace + ".cluster.healthy_nodes").Set(float64(stats.HealthyNodes))
	s.metrics.Gauge(namespace + ".term").Set(float64(stats.Term))
	s.metrics.Gauge(namespace + ".commit_index").Set(float64(stats.CommitIndex))
	s.metrics.Gauge(namespace + ".last_applied").Set(float64(stats.LastApplied))
	s.metrics.Counter(namespace + ".operations.total").Add(float64(stats.OperationsTotal))
	s.metrics.Counter(namespace + ".operations.failed").Add(float64(stats.OperationsFailed))
	s.metrics.Gauge(namespace + ".operations.per_sec").Set(stats.OperationsPerSec)
	s.metrics.Gauge(namespace + ".error_rate").Set(stats.ErrorRate)

	isLeader := 0.0
	if s.IsLeader() {
		isLeader = 1.0
	}

	s.metrics.Gauge(namespace + ".is_leader").Set(isLeader)

	hasQuorum := 0.0
	if stats.HasQuorum {
		hasQuorum = 1.0
	}

	s.metrics.Gauge(namespace + ".has_quorum").Set(hasQuorum)
}

func (s *Service) recordOperation(duration time.Duration, success bool) {
	s.metricsLock.Lock()
	defer s.metricsLock.Unlock()

	s.operations++
	if !success {
		s.operationsFailed++
	}

	if s.metrics != nil {
		namespace := s.config.Observability.Metrics.Namespace
		s.metrics.Histogram(namespace + ".operation.duration").Observe(duration.Seconds())
		s.metrics.Counter(namespace + ".operations.total").Inc()

		if !success {
			s.metrics.Counter(namespace + ".operations.failed").Inc()
		}
	}
}

func (s *Service) encodeCommand(cmd Command) []byte {
	// TODO: Implement proper encoding
	return fmt.Appendf(nil, "%v", cmd)
}
