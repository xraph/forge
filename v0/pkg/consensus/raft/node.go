package raft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/discovery"
	"github.com/xraph/forge/pkg/consensus/statemachine"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/consensus/transport"
	"github.com/xraph/forge/pkg/logger"
)

// Node implements the RaftNode interface
type Node struct {
	config         NodeConfig
	id             string
	state          NodeState
	currentTerm    uint64
	votedFor       string
	log            *Log
	commitIndex    uint64
	lastApplied    uint64
	nextIndex      map[string]uint64
	matchIndex     map[string]uint64
	storage        storage.Storage
	transport      transport.Transport
	rpc            *RaftRPC
	stateMachine   statemachine.StateMachine
	discovery      discovery.Discovery
	logger         common.Logger
	metrics        common.Metrics
	leaderID       string
	votes          map[string]bool
	electionTimer  *time.Timer
	heartbeatTimer *time.Timer
	stats          RaftStats
	startTime      time.Time
	stopCh         chan struct{}
	applyCh        chan ApplyRequest
	mu             sync.RWMutex
	started        bool
}

// NodeConfig contains configuration for the Raft node
type NodeConfig struct {
	ID                  string        `json:"id"`
	ElectionTimeout     time.Duration `json:"election_timeout"`
	HeartbeatInterval   time.Duration `json:"heartbeat_interval"`
	MaxLogEntries       int           `json:"max_log_entries"`
	SnapshotThreshold   int           `json:"snapshot_threshold"`
	EnableSnapshots     bool          `json:"enable_snapshots"`
	LogCompactionSize   int           `json:"log_compaction_size"`
	BatchSize           int           `json:"batch_size"`
	MaxInflightRequests int           `json:"max_inflight_requests"`
}

// NodeState represents the state of a Raft node
type NodeState string

const (
	StateFollower  NodeState = "follower"
	StateCandidate NodeState = "candidate"
	StateLeader    NodeState = "leader"
	StateStopped   NodeState = "stopped"
)

// ApplyRequest represents a request to apply a log entry
type ApplyRequest struct {
	Entry    storage.LogEntry
	Response chan error
}

// RaftStats contains statistics about the Raft node
type RaftStats struct {
	NodeID             string             `json:"node_id"`
	State              NodeState          `json:"state"`
	CurrentTerm        uint64             `json:"current_term"`
	LeaderID           string             `json:"leader_id"`
	VotedFor           string             `json:"voted_for"`
	LogSize            int64              `json:"log_size"`
	CommitIndex        uint64             `json:"commit_index"`
	LastApplied        uint64             `json:"last_applied"`
	LastLogIndex       uint64             `json:"last_log_index"`
	LastLogTerm        uint64             `json:"last_log_term"`
	SnapshotCount      int64              `json:"snapshot_count"`
	Uptime             time.Duration      `json:"uptime"`
	ElectionCount      int64              `json:"election_count"`
	HeartbeatCount     int64              `json:"heartbeat_count"`
	AppendEntriesCount int64              `json:"append_entries_count"`
	RequestVoteCount   int64              `json:"request_vote_count"`
	LastElection       time.Time          `json:"last_election"`
	LastHeartbeat      time.Time          `json:"last_heartbeat"`
	PeerStatus         map[string]string  `json:"peer_status"`
	ErrorCount         int64              `json:"error_count"`
	LastError          string             `json:"last_error"`
	PerformanceMetrics PerformanceMetrics `json:"performance_metrics"`
}

// PerformanceMetrics contains performance-related metrics
type PerformanceMetrics struct {
	AverageAppendLatency time.Duration `json:"average_append_latency"`
	AverageVoteLatency   time.Duration `json:"average_vote_latency"`
	LogReplicationRate   float64       `json:"log_replication_rate"`
	ElectionLatency      time.Duration `json:"election_latency"`
	SnapshotLatency      time.Duration `json:"snapshot_latency"`
}

// NewNode creates a new Raft node
func NewNode(config NodeConfig, storage storage.Storage, transport transport.Transport, stateMachine statemachine.StateMachine, discovery discovery.Discovery, l common.Logger, metrics common.Metrics) (*Node, error) {
	// Validate configuration
	if err := validateNodeConfig(config); err != nil {
		return nil, fmt.Errorf("invalid node configuration: %w", err)
	}

	// Set defaults
	if config.ElectionTimeout == 0 {
		config.ElectionTimeout = 150 * time.Millisecond
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 50 * time.Millisecond
	}
	if config.MaxLogEntries == 0 {
		config.MaxLogEntries = 10000
	}
	if config.SnapshotThreshold == 0 {
		config.SnapshotThreshold = 1000
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.MaxInflightRequests == 0 {
		config.MaxInflightRequests = 1000
	}

	// Create log
	log, err := NewLog(storage, l, metrics)
	if err != nil {
		return nil, err
	}

	node := &Node{
		config:       config,
		id:           config.ID,
		state:        StateFollower,
		log:          log,
		storage:      storage,
		transport:    transport,
		stateMachine: stateMachine,
		discovery:    discovery,
		logger:       l,
		metrics:      metrics,
		nextIndex:    make(map[string]uint64),
		matchIndex:   make(map[string]uint64),
		votes:        make(map[string]bool),
		startTime:    time.Now(),
		stopCh:       make(chan struct{}),
		applyCh:      make(chan ApplyRequest, 1000),
		stats: RaftStats{
			NodeID:       config.ID,
			State:        StateFollower,
			PeerStatus:   make(map[string]string),
			LastElection: time.Now(),
		},
	}

	// Load persistent state
	if err := node.loadPersistentState(); err != nil {
		return nil, fmt.Errorf("failed to load persistent state: %w", err)
	}

	// Create RPC layer
	rpc := NewRaftRPC(config.ID, transport, node, l, metrics)
	node.rpc = rpc

	if l != nil {
		l.Info("raft node created",
			logger.String("node_id", config.ID),
			logger.String("state", string(StateFollower)),
			logger.Duration("election_timeout", config.ElectionTimeout),
			logger.Duration("heartbeat_interval", config.HeartbeatInterval),
		)
	}

	if l != nil {
		l.Info("raft node created",
			logger.String("node_id", config.ID),
			logger.String("state", string(StateFollower)),
			logger.Duration("election_timeout", config.ElectionTimeout),
			logger.Duration("heartbeat_interval", config.HeartbeatInterval),
		)
	}

	return node, nil
}

// Start starts the Raft node
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.started {
		return fmt.Errorf("node already started")
	}

	// Start RPC layer
	if err := n.rpc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start RPC layer: %w", err)
	}

	// Start state machine
	if err := n.stateMachine.Apply(ctx, storage.LogEntry{
		Type:      storage.EntryTypeNoOp,
		Data:      []byte("start"),
		Timestamp: time.Now(),
	}); err != nil {
		return fmt.Errorf("failed to start state machine: %w", err)
	}

	// Start background routines
	go n.applyLogEntries()
	go n.runElectionTimer()
	go n.periodicTasks()

	n.started = true
	n.startTime = time.Now()
	n.updateStats()

	if n.logger != nil {
		n.logger.Info("raft node started",
			logger.String("node_id", n.id),
			logger.String("state", string(n.state)),
			logger.Uint64("term", n.currentTerm),
		)
	}

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.nodes_started").Inc()
	}

	return nil
}

// Stop stops the Raft node
func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.started {
		return fmt.Errorf("node not started")
	}

	// Signal stop
	close(n.stopCh)

	// Stop timers
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
	if n.heartbeatTimer != nil {
		n.heartbeatTimer.Stop()
	}

	// Stop RPC layer
	if err := n.rpc.Stop(ctx); err != nil {
		if n.logger != nil {
			n.logger.Error("failed to stop RPC layer", logger.Error(err))
		}
	}

	// Save persistent state
	if err := n.savePersistentState(); err != nil {
		if n.logger != nil {
			n.logger.Error("failed to save persistent state", logger.Error(err))
		}
	}

	n.started = false
	n.state = StateStopped
	n.updateStats()

	if n.logger != nil {
		n.logger.Info("raft node stopped",
			logger.String("node_id", n.id),
		)
	}

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.nodes_stopped").Inc()
	}

	return nil
}

// IsLeader returns true if this node is the leader
func (n *Node) IsLeader() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.state == StateLeader
}

// GetLeader returns the current leader ID
func (n *Node) GetLeader() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.leaderID
}

// GetTerm returns the current term
func (n *Node) GetTerm() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.currentTerm
}

// GetCommitIndex returns the current commit index
func (n *Node) GetCommitIndex() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.commitIndex
}

// AppendEntries appends entries to the log
func (n *Node) AppendEntries(ctx context.Context, entries []storage.LogEntry) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateLeader {
		return &RaftError{
			Code:    ErrNotLeader,
			Message: "not the leader",
			NodeID:  n.id,
		}
	}

	startTime := time.Now()
	defer func() {
		n.stats.PerformanceMetrics.AverageAppendLatency = time.Since(startTime)
	}()

	// Append entries to log
	for _, entry := range entries {
		entry.Term = n.currentTerm
		entry.Index = n.log.LastIndex() + 1
		entry.Timestamp = time.Now()

		if err := n.log.AppendEntry(entry); err != nil {
			n.incrementErrorCount(err)
			return fmt.Errorf("failed to append entry: %w", err)
		}
	}

	// Replicate to followers
	if err := n.replicateToFollowers(ctx); err != nil {
		n.incrementErrorCount(err)
		return fmt.Errorf("failed to replicate to followers: %w", err)
	}

	n.stats.AppendEntriesCount++
	n.updateStats()

	if n.logger != nil {
		n.logger.Debug("entries appended",
			logger.String("node_id", n.id),
			logger.Int("count", len(entries)),
			logger.Uint64("term", n.currentTerm),
		)
	}

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.entries_appended").Add(float64(len(entries)))
		n.metrics.Histogram("forge.consensus.raft.append_latency").Observe(time.Since(startTime).Seconds())
	}

	return nil
}

// RequestVote handles a vote request
func (n *Node) RequestVote(ctx context.Context, req *RequestVoteRequest) (*RequestVoteResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	startTime := time.Now()
	defer func() {
		n.stats.PerformanceMetrics.AverageVoteLatency = time.Since(startTime)
	}()

	response := &RequestVoteResponse{
		Term:        n.currentTerm,
		VoteGranted: false,
		VoterID:     n.id,
	}

	// If request term is older, reject
	if req.Term < n.currentTerm {
		n.stats.RequestVoteCount++
		return response, nil
	}

	// If request term is newer, update term and become follower
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.state = StateFollower
		n.leaderID = ""
		n.savePersistentState()
	}

	// Check if we can vote for this candidate
	canVote := (n.votedFor == "" || n.votedFor == req.CandidateID) &&
		n.isLogUpToDate(req.LastLogIndex, req.LastLogTerm)

	if canVote {
		n.votedFor = req.CandidateID
		response.VoteGranted = true
		response.Term = n.currentTerm
		n.resetElectionTimer()
		n.savePersistentState()

		if n.logger != nil {
			n.logger.Info("vote granted",
				logger.String("node_id", n.id),
				logger.String("candidate", req.CandidateID),
				logger.Uint64("term", req.Term),
			)
		}
	}

	n.stats.RequestVoteCount++
	n.updateStats()

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.vote_requests_received").Inc()
		if response.VoteGranted {
			n.metrics.Counter("forge.consensus.raft.votes_granted").Inc()
		}
		n.metrics.Histogram("forge.consensus.raft.vote_latency").Observe(time.Since(startTime).Seconds())
	}

	return response, nil
}

// InstallSnapshot installs a snapshot
func (n *Node) InstallSnapshot(ctx context.Context, snapshot *storage.Snapshot) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	startTime := time.Now()
	defer func() {
		n.stats.PerformanceMetrics.SnapshotLatency = time.Since(startTime)
	}()

	if n.state == StateLeader {
		return &RaftError{
			Code:    ErrNotLeader,
			Message: "leader cannot install snapshot",
			NodeID:  n.id,
		}
	}

	// Restore snapshot to state machine
	if err := n.stateMachine.RestoreSnapshot(snapshot); err != nil {
		n.incrementErrorCount(err)
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	// Update indices
	n.lastApplied = snapshot.Index
	n.commitIndex = snapshot.Index

	// Truncate log
	if err := n.log.TruncateLog(snapshot.Index); err != nil {
		n.incrementErrorCount(err)
		return fmt.Errorf("failed to truncate log: %w", err)
	}

	n.stats.SnapshotCount++
	n.updateStats()

	if n.logger != nil {
		n.logger.Info("snapshot installed",
			logger.String("node_id", n.id),
			logger.Uint64("last_included_index", snapshot.Index),
			logger.Uint64("last_included_term", snapshot.Term),
		)
	}

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.snapshots_installed").Inc()
		n.metrics.Histogram("forge.consensus.raft.snapshot_latency").Observe(time.Since(startTime).Seconds())
	}

	return nil
}

// HealthCheck performs a health check
func (n *Node) HealthCheck(ctx context.Context) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.started {
		return fmt.Errorf("node not started")
	}

	// Check if we've received heartbeats recently (for followers)
	if n.state == StateFollower {
		if time.Since(n.stats.LastHeartbeat) > n.config.ElectionTimeout*2 {
			return fmt.Errorf("no heartbeat received in %v", time.Since(n.stats.LastHeartbeat))
		}
	}

	// Check error rate
	if n.stats.ErrorCount > 0 {
		totalOperations := n.stats.AppendEntriesCount + n.stats.RequestVoteCount
		if totalOperations > 0 {
			errorRate := float64(n.stats.ErrorCount) / float64(totalOperations)
			if errorRate > 0.1 { // 10% error rate threshold
				return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
			}
		}
	}

	// Check storage health
	if err := n.storage.HealthCheck(ctx); err != nil {
		return fmt.Errorf("storage health check failed: %w", err)
	}

	// Check RPC health
	if err := n.rpc.HealthCheck(ctx); err != nil {
		return fmt.Errorf("RPC health check failed: %w", err)
	}

	// Check state machine health
	if err := n.stateMachine.HealthCheck(ctx); err != nil {
		return fmt.Errorf("state machine health check failed: %w", err)
	}

	return nil
}

// GetStats returns node statistics
func (n *Node) GetStats() RaftStats {
	n.mu.RLock()
	defer n.mu.RUnlock()

	stats := n.stats
	stats.Uptime = time.Since(n.startTime)
	stats.LogSize = n.storage.GetStats().StorageSize
	stats.LastLogIndex = n.log.LastIndex()
	stats.LastLogTerm = n.log.LastTerm()

	return stats
}

// Transport handler methods

// HandleAppendEntries handles an AppendEntries RPC
func (n *Node) HandleAppendEntries(req *AppendEntriesRequest) *AppendEntriesResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	response := &AppendEntriesResponse{
		Term:    n.currentTerm,
		Success: false,
		NodeID:  n.id,
	}

	// If request term is older, reject
	if req.Term < n.currentTerm {
		return response
	}

	// Update term and become follower if necessary
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.state = StateFollower
		n.leaderID = req.LeaderID
		n.savePersistentState()
	}

	// Update leader and reset election timer
	n.leaderID = req.LeaderID
	n.resetElectionTimer()
	n.stats.LastHeartbeat = time.Now()
	n.stats.HeartbeatCount++

	// Check log consistency
	if !n.log.IsConsistent(req.PrevLogIndex, req.PrevLogTerm) {
		response.MatchIndex = n.log.LastIndex()
		return response
	}

	// Append entries
	if len(req.Entries) > 0 {
		// Convert consensus core entries to storage entries
		storageEntries := make([]storage.LogEntry, len(req.Entries))
		for i, entry := range req.Entries {
			storageEntries[i] = storage.LogEntry{
				Index:     entry.Index,
				Term:      entry.Term,
				Type:      storage.EntryType(entry.Type),
				Data:      entry.Data,
				Metadata:  entry.Metadata,
				Timestamp: entry.Timestamp,
				Checksum:  entry.Checksum,
			}
		}

		if err := n.log.AppendEntriesByIndex(req.PrevLogIndex, storageEntries); err != nil {
			n.incrementErrorCount(err)
			return response
		}
	}

	// Update commit index
	if req.LeaderCommit > n.commitIndex {
		n.commitIndex = min(req.LeaderCommit, n.log.LastIndex())
		n.applyCommittedEntries()
	}

	response.Success = true
	response.MatchIndex = n.log.LastIndex()
	response.Term = n.currentTerm

	n.updateStats()

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.append_entries_received").Inc()
		n.metrics.Counter("forge.consensus.raft.heartbeats_received").Inc()
	}

	return response
}

// HandleRequestVote handles a RequestVote RPC
func (n *Node) HandleRequestVote(req *RequestVoteRequest) *RequestVoteResponse {
	response, _ := n.RequestVote(context.Background(), req)
	return response
}

// HandleInstallSnapshot handles an InstallSnapshot RPC
func (n *Node) HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	response := &InstallSnapshotResponse{
		Term:   n.currentTerm,
		NodeID: n.id,
	}

	// If request term is older, reject
	if req.Term < n.currentTerm {
		return response
	}

	// Update term and become follower if necessary
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.state = StateFollower
		n.leaderID = req.LeaderID
		n.savePersistentState()
	}

	// Install snapshot
	snapshot := &storage.Snapshot{
		Index:     req.LastIncludedIndex,
		Term:      req.LastIncludedTerm,
		Data:      req.Data,
		Timestamp: time.Now(),
		Size:      int64(len(req.Data)),
	}

	if err := n.InstallSnapshot(context.Background(), snapshot); err != nil {
		n.incrementErrorCount(err)
		if n.logger != nil {
			n.logger.Error("failed to install snapshot", logger.Error(err))
		}
	}

	response.Term = n.currentTerm
	return response
}

// Private methods

// loadPersistentState loads persistent state from storage
func (n *Node) loadPersistentState() error {
	state, err := n.storage.LoadState(context.Background())
	if err != nil {
		return err
	}

	if state != nil {
		n.currentTerm = state.CurrentTerm
		n.votedFor = state.VotedFor
	}

	return nil
}

// savePersistentState saves persistent state to storage
func (n *Node) savePersistentState() error {
	state := &storage.PersistentState{
		CurrentTerm: n.currentTerm,
		VotedFor:    n.votedFor,
		NodeID:      n.id,
		UpdatedAt:   time.Now(),
	}

	return n.storage.StoreState(context.Background(), state)
}

// isLogUpToDate checks if the candidate's log is up to date
func (n *Node) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	myLastIndex := n.log.LastIndex()
	myLastTerm := n.log.LastTerm()

	if lastLogTerm > myLastTerm {
		return true
	}

	if lastLogTerm == myLastTerm {
		return lastLogIndex >= myLastIndex
	}

	return false
}

// resetElectionTimer resets the election timer
func (n *Node) resetElectionTimer() {
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}

	timeout := n.config.ElectionTimeout + time.Duration(n.id[0])%n.config.ElectionTimeout
	n.electionTimer = time.AfterFunc(timeout, func() {
		n.startElection()
	})
}

// startElection starts a new election
func (n *Node) startElection() {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state == StateLeader {
		return
	}

	n.state = StateCandidate
	n.currentTerm++
	n.votedFor = n.id
	n.leaderID = ""
	n.votes = make(map[string]bool)
	n.votes[n.id] = true
	n.stats.ElectionCount++
	n.stats.LastElection = time.Now()

	if err := n.savePersistentState(); err != nil {
		n.incrementErrorCount(err)
		return
	}

	n.resetElectionTimer()
	n.updateStats()

	if n.logger != nil {
		n.logger.Info("election started",
			logger.String("node_id", n.id),
			logger.Uint64("term", n.currentTerm),
		)
	}

	// Send vote requests to all peers
	go n.sendVoteRequests()
}

// sendVoteRequests sends vote requests to all peers
func (n *Node) sendVoteRequests() {
	nodes, err := n.discovery.GetNodes(context.Background())
	if err != nil {
		n.incrementErrorCount(err)
		return
	}

	req := &RequestVoteRequest{
		Term:         n.currentTerm,
		CandidateID:  n.id,
		LastLogIndex: n.log.LastIndex(),
		LastLogTerm:  n.log.LastTerm(),
	}

	for _, node := range nodes {
		if node.ID == n.id {
			continue
		}

		go func(nodeID string) {
			response, err := n.rpc.SendRequestVote(context.Background(), nodeID, req)
			if err != nil {
				n.incrementErrorCount(err)
				return
			}

			n.handleVoteResponse(nodeID, response)
		}(node.ID)
	}
}

// handleVoteResponse handles a vote response
func (n *Node) handleVoteResponse(nodeID string, response *RequestVoteResponse) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateCandidate || response.Term < n.currentTerm {
		return
	}

	if response.Term > n.currentTerm {
		n.currentTerm = response.Term
		n.votedFor = ""
		n.state = StateFollower
		n.leaderID = ""
		n.savePersistentState()
		n.updateStats()
		return
	}

	if response.VoteGranted {
		n.votes[nodeID] = true

		// Check if we have majority
		nodes, err := n.discovery.GetNodes(context.Background())
		if err != nil {
			n.incrementErrorCount(err)
			return
		}

		majority := len(nodes)/2 + 1
		if len(n.votes) >= majority {
			n.becomeLeader()
		}
	}
}

// becomeLeader transitions to leader state
func (n *Node) becomeLeader() {
	n.state = StateLeader
	n.leaderID = n.id
	n.nextIndex = make(map[string]uint64)
	n.matchIndex = make(map[string]uint64)

	// Initialize nextIndex and matchIndex
	nodes, err := n.discovery.GetNodes(context.Background())
	if err != nil {
		n.incrementErrorCount(err)
		return
	}

	lastLogIndex := n.log.LastIndex()
	for _, node := range nodes {
		if node.ID != n.id {
			n.nextIndex[node.ID] = lastLogIndex + 1
			n.matchIndex[node.ID] = 0
		}
	}

	n.updateStats()

	if n.logger != nil {
		n.logger.Info("became leader",
			logger.String("node_id", n.id),
			logger.Uint64("term", n.currentTerm),
		)
	}

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.leader_elections_won").Inc()
	}

	// Start sending heartbeats
	go n.sendHeartbeats()
}

// sendHeartbeats sends heartbeats to all followers
func (n *Node) sendHeartbeats() {
	ticker := time.NewTicker(n.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			n.mu.RLock()
			if n.state != StateLeader {
				n.mu.RUnlock()
				return
			}
			n.mu.RUnlock()

			n.replicateToFollowers(context.Background())

		case <-n.stopCh:
			return
		}
	}
}

// replicateToFollowers replicates log entries to followers
func (n *Node) replicateToFollowers(ctx context.Context) error {
	nodes, err := n.discovery.GetNodes(ctx)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if node.ID == n.id {
			continue
		}

		go func(nodeID string) {
			n.replicateToFollower(ctx, nodeID)
		}(node.ID)
	}

	return nil
}

// replicateToFollower replicates log entries to a specific follower
func (n *Node) replicateToFollower(ctx context.Context, nodeID string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateLeader {
		return
	}

	nextIndex := n.nextIndex[nodeID]
	prevLogIndex := nextIndex - 1
	prevLogTerm, _ := n.log.GetTermAtIndex(prevLogIndex)

	// Get entries to send
	entries, _ := n.log.GetEntriesFrom(nextIndex)

	req := &AppendEntriesRequest{
		Term:         n.currentTerm,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: n.commitIndex,
	}

	response, err := n.rpc.SendAppendEntries(ctx, nodeID, req)
	if err != nil {
		n.incrementErrorCount(err)
		return
	}

	n.handleAppendEntriesResponse(nodeID, response)
}

// handleAppendEntriesResponse handles an AppendEntries response
func (n *Node) handleAppendEntriesResponse(nodeID string, response *AppendEntriesResponse) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != StateLeader || response.Term < n.currentTerm {
		return
	}

	if response.Term > n.currentTerm {
		n.currentTerm = response.Term
		n.votedFor = ""
		n.state = StateFollower
		n.leaderID = ""
		n.savePersistentState()
		n.updateStats()
		return
	}

	if response.Success {
		// Update indices
		n.nextIndex[nodeID] = response.MatchIndex + 1
		n.matchIndex[nodeID] = response.MatchIndex

		// Update commit index
		n.updateCommitIndex()
	} else {
		// Decrement nextIndex and retry
		if n.nextIndex[nodeID] > 1 {
			n.nextIndex[nodeID]--
		}
	}
}

// updateCommitIndex updates the commit index based on majority
func (n *Node) updateCommitIndex() {
	if n.state != StateLeader {
		return
	}

	// Find the highest index that is replicated to majority
	nodes, err := n.discovery.GetNodes(context.Background())
	if err != nil {
		n.incrementErrorCount(err)
		return
	}

	majority := len(nodes)/2 + 1
	lastLogIndex := n.log.LastIndex()

	for index := lastLogIndex; index > n.commitIndex; index-- {
		count := 1 // Count this node
		for _, matchIndex := range n.matchIndex {
			if matchIndex >= index {
				count++
			}
		}

		idx, _ := n.log.GetTermAtIndex(index)

		if count >= majority && idx == n.currentTerm {
			n.commitIndex = index
			n.applyCommittedEntries()
			break
		}
	}
}

// applyCommittedEntries applies committed entries to the state machine
func (n *Node) applyCommittedEntries() {
	for n.lastApplied < n.commitIndex {
		n.lastApplied++
		entry, err := n.log.GetEntry(n.lastApplied)
		if err != nil {
			n.incrementErrorCount(err)
			break
		}

		// Send to apply channel
		select {
		case n.applyCh <- ApplyRequest{Entry: entry, Response: make(chan error, 1)}:
		default:
			// Channel full, skip
		}
	}
}

// applyLogEntries applies log entries from the apply channel
func (n *Node) applyLogEntries() {
	for {
		select {
		case req := <-n.applyCh:
			err := n.stateMachine.Apply(context.Background(), req.Entry)
			if req.Response != nil {
				req.Response <- err
			}

		case <-n.stopCh:
			return
		}
	}
}

// runElectionTimer runs the election timer
func (n *Node) runElectionTimer() {
	n.resetElectionTimer()
	<-n.stopCh
}

// periodicTasks runs periodic maintenance tasks
func (n *Node) periodicTasks() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			n.performMaintenance()

		case <-n.stopCh:
			return
		}
	}
}

// performMaintenance performs periodic maintenance tasks
func (n *Node) performMaintenance() {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Check if snapshot is needed
	if n.config.EnableSnapshots && n.log.LastIndex()-n.lastApplied > uint64(n.config.SnapshotThreshold) {
		if err := n.createSnapshot(); err != nil {
			n.incrementErrorCount(err)
		}
	}

	// Update stats
	n.updateStats()
}

// createSnapshot creates a snapshot of the current state
func (n *Node) createSnapshot() error {
	if n.state != StateLeader {
		return nil
	}

	snapshot, err := n.stateMachine.CreateSnapshot()
	if err != nil {
		return err
	}

	// Store snapshot
	if err := n.storage.StoreSnapshot(context.Background(), *snapshot); err != nil {
		return err
	}

	// Truncate log
	if err := n.log.TruncateLog(snapshot.Index); err != nil {
		return err
	}

	n.stats.SnapshotCount++

	if n.logger != nil {
		n.logger.Info("snapshot created",
			logger.String("node_id", n.id),
			logger.Uint64("last_included_index", snapshot.Index),
		)
	}

	if n.metrics != nil {
		n.metrics.Counter("forge.consensus.raft.snapshots_created").Inc()
	}

	return nil
}

// updateStats updates node statistics
func (n *Node) updateStats() {
	n.stats.NodeID = n.id
	n.stats.State = n.state
	n.stats.CurrentTerm = n.currentTerm
	n.stats.LeaderID = n.leaderID
	n.stats.VotedFor = n.votedFor
	n.stats.CommitIndex = n.commitIndex
	n.stats.LastApplied = n.lastApplied

	// Update peer status
	if n.discovery != nil {
		if nodes, err := n.discovery.GetNodes(context.Background()); err == nil {
			for _, node := range nodes {
				if node.ID != n.id {
					n.stats.PeerStatus[node.ID] = string(node.Status)
				}
			}
		}
	}
}

// incrementErrorCount increments the error count
func (n *Node) incrementErrorCount(err error) {
	n.stats.ErrorCount++
	if err != nil {
		n.stats.LastError = err.Error()
	}
}

// validateNodeConfig validates the node configuration
func validateNodeConfig(config NodeConfig) error {
	if config.ID == "" {
		return fmt.Errorf("node ID is required")
	}

	if config.ElectionTimeout < 0 {
		return fmt.Errorf("election timeout must be positive")
	}

	if config.HeartbeatInterval < 0 {
		return fmt.Errorf("heartbeat interval must be positive")
	}

	if config.HeartbeatInterval >= config.ElectionTimeout {
		return fmt.Errorf("heartbeat interval must be less than election timeout")
	}

	return nil
}

// min returns the minimum of two uint64 values
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
