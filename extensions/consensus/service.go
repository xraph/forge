package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
	"github.com/xraph/forge/extensions/consensus/raft"
)

// Service implements internal.ConsensusService.
type Service struct {
	config  internal.Config
	logger  forge.Logger
	metrics forge.Metrics

	// Core components
	node           *raft.Node
	raftNode       internal.RaftNode
	stateMachine   internal.StateMachine
	clusterManager internal.ClusterManager
	transport      internal.Transport
	discovery      internal.Discovery
	storage        internal.Storage

	// State
	startTime time.Time
	started   bool
	mu        sync.RWMutex

	// Lifecycle
	ctx    context.Context //nolint:containedctx // lifecycle context: created in Start, cancelled in Stop
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

	if err := checkSupported(config); err != nil {
		return nil, err
	}

	s := &Service{
		config:    config,
		logger:    logger,
		metrics:   metrics,
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

	s.ctx, s.cancel = context.WithCancel(context.WithoutCancel(ctx))

	s.logger.Info("starting consensus service",
		forge.F("node_id", s.config.NodeID),
		forge.F("cluster_id", s.config.ClusterID),
		forge.F("bind_addr", s.config.BindAddr),
		forge.F("bind_port", s.config.BindPort),
		forge.F("transport", s.config.Transport.Type),
		forge.F("storage", s.config.Storage.Type),
	)

	if err := s.startComponents(ctx); err != nil {
		s.cancel()
		s.stopComponents(context.WithoutCancel(ctx))

		return err
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

	s.stopComponents(ctx)

	s.logger.Info("consensus service stopped")

	return nil
}

// IsLeader returns true if this node is the leader.
func (s *Service) IsLeader() bool {
	if node := s.raft(); node != nil {
		return node.IsLeader()
	}

	return false
}

// GetLeader returns the current leader node ID.
func (s *Service) GetLeader() string {
	if node := s.raft(); node != nil {
		return node.GetLeader()
	}

	return ""
}

// GetRole returns the current role of this node.
func (s *Service) GetRole() NodeRole {
	if node := s.raft(); node != nil {
		return node.GetRole()
	}

	return internal.RoleFollower
}

// GetTerm returns the current term.
func (s *Service) GetTerm() uint64 {
	if node := s.raft(); node != nil {
		return node.GetTerm()
	}

	return 0
}

// raft returns the running raft node, or nil before Start.
func (s *Service) raft() *raft.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.node
}

// hasQuorum reports whether this node is in touch with a working majority.
func (s *Service) hasQuorum() bool {
	node := s.raft()

	return node != nil && node.HasQuorum()
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

	data, err := json.Marshal(cmd)
	if err != nil {
		s.recordOperation(time.Since(start), false)

		return fmt.Errorf("failed to encode command: %w", err)
	}

	entry := LogEntry{
		Type:    EntryNormal,
		Data:    data,
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

	// Reads are served from the local state machine, so a follower may lag
	// the leader. Linearizable reads (read index) are not implemented.
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

	clusterSize, healthyNodes := s.clusterCounts(s.node)

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
		Role:             raftStats.Role,
		Status:           internal.StatusActive,
		Term:             raftStats.Term,
		LeaderID:         raftStats.Leader,
		CommitIndex:      raftStats.CommitIndex,
		LastApplied:      raftStats.LastApplied,
		LastLogIndex:     raftStats.LastLogIndex,
		LastLogTerm:      raftStats.LastLogTerm,
		ClusterSize:      clusterSize,
		HealthyNodes:     healthyNodes,
		HasQuorum:        s.node != nil && s.node.HasQuorum(),
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

	if !s.hasQuorum() {
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
	hasQuorum := s.hasQuorum()
	checks = append(checks, HealthCheck{
		Name:      "quorum",
		Healthy:   hasQuorum,
		Message:   "Cluster has quorum",
		CheckedAt: time.Now(),
	})

	totalNodes, activeNodes := s.clusterCounts(s.raft())

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
	node := s.raft()
	if node == nil {
		return ClusterInfo{ID: s.config.ClusterID}
	}

	stats := node.GetStats()
	nodes := s.clusterNodes(node)

	activeNodes := 0

	for _, n := range nodes {
		if n.Status == StatusActive {
			activeNodes++
		}
	}

	return ClusterInfo{
		ID:          s.config.ClusterID,
		Leader:      stats.Leader,
		Term:        stats.Term,
		Nodes:       nodes,
		TotalNodes:  len(nodes),
		ActiveNodes: activeNodes,
		HasQuorum:   node.HasQuorum(),
		CommitIndex: stats.CommitIndex,
		LastApplied: stats.LastApplied,
	}
}

// AddNode is not supported: cluster membership comes from the static peer
// list, and the raft implementation has no membership change protocol.
// Changing membership outside the log would risk two majorities.
func (s *Service) AddNode(ctx context.Context, nodeID, address string, port int) error {
	return ErrMembershipChangeUnsupported
}

// RemoveNode is not supported, for the same reason as AddNode.
func (s *Service) RemoveNode(ctx context.Context, nodeID string) error {
	return ErrMembershipChangeUnsupported
}

// TransferLeadership is not implemented. Use StepDown to let another node
// win the next election.
func (s *Service) TransferLeadership(ctx context.Context, targetNodeID string) error {
	if !s.IsLeader() {
		return ErrNotLeader
	}

	return errors.New("leadership transfer is not implemented; use StepDown")
}

// StepDown makes the leader return to follower so another node can be
// elected.
func (s *Service) StepDown(ctx context.Context) error {
	node := s.raft()
	if node == nil {
		return ErrNotStarted
	}

	return node.StepDown(ctx)
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

	if err := s.raft().Snapshot(); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	s.metricsLock.Lock()
	s.snapshots++
	s.lastSnapshot = time.Now()
	s.metricsLock.Unlock()

	s.logger.Info("snapshot created",
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

// GetClusterManager returns nil: membership comes from the static peer list
// and cluster state is read from the raft node (see GetClusterInfo).
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
