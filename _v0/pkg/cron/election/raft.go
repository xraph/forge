package election

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// RaftBackend implements leader election using Raft consensus algorithm
type RaftBackend struct {
	config    *Config
	nodeID    string
	clusterID string

	// Raft state
	currentTerm   uint64
	votedFor      string
	log           []LogEntry
	commitIndex   uint64
	lastApplied   uint64
	state         RaftState
	leaderID      string
	lastHeartbeat time.Time

	// Volatile state on leaders
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// Peer information
	peers   []string
	peersMu sync.RWMutex

	// Network transport
	transport Transport

	// Election state
	electionTimeout time.Duration
	electionTimer   *time.Timer
	heartbeatTimer  *time.Timer
	voteCount       int
	voteMu          sync.Mutex

	// Persistence
	persistence PersistentState

	// Watchers
	watchers   []func(leaderID string)
	watchersMu sync.RWMutex

	// Lifecycle
	started     bool
	stopChannel chan struct{}
	wg          sync.WaitGroup
	mu          sync.RWMutex

	// Framework integration
	logger  common.Logger
	metrics common.Metrics
}

// RaftState represents the state of a Raft node
type RaftState int

const (
	StateFollower RaftState = iota
	StateCandidate
	StateLeader
)

func (s RaftState) String() string {
	switch s {
	case StateFollower:
		return "follower"
	case StateCandidate:
		return "candidate"
	case StateLeader:
		return "leader"
	default:
		return "unknown"
	}
}

// LogEntry represents a Raft log entry
type LogEntry struct {
	Index     uint64                 `json:"index"`
	Term      uint64                 `json:"term"`
	Type      string                 `json:"type"`
	Data      []byte                 `json:"data"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// VoteRequest represents a vote request in Raft
type VoteRequest struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// VoteResponse represents a vote response in Raft
type VoteResponse struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	VoterID     string `json:"voter_id"`
}

// AppendEntriesRequest represents an append entries request
type AppendEntriesRequest struct {
	Term         uint64     `json:"term"`
	LeaderID     string     `json:"leader_id"`
	PrevLogIndex uint64     `json:"prev_log_index"`
	PrevLogTerm  uint64     `json:"prev_log_term"`
	Entries      []LogEntry `json:"entries"`
	LeaderCommit uint64     `json:"leader_commit"`
}

// AppendEntriesResponse represents an append entries response
type AppendEntriesResponse struct {
	Term    uint64 `json:"term"`
	Success bool   `json:"success"`
	NodeID  string `json:"node_id"`
}

// Transport defines the interface for Raft network communication
type Transport interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	SendVoteRequest(ctx context.Context, nodeID string, req VoteRequest) (VoteResponse, error)
	SendAppendEntries(ctx context.Context, nodeID string, req AppendEntriesRequest) (AppendEntriesResponse, error)
	SetVoteHandler(handler func(req VoteRequest) VoteResponse)
	SetAppendHandler(handler func(req AppendEntriesRequest) AppendEntriesResponse)
	GetPeers() []string
}

// PersistentState defines the interface for Raft persistent state
type PersistentState interface {
	GetCurrentTerm() uint64
	SetCurrentTerm(term uint64) error
	GetVotedFor() string
	SetVotedFor(nodeID string) error
	GetLog() []LogEntry
	AppendLog(entries []LogEntry) error
	TruncateLog(index uint64) error
	GetLastLogIndex() uint64
	GetLastLogTerm() uint64
	Persist() error
	Restore() error
}

// RaftConfig contains Raft-specific configuration
type RaftConfig struct {
	Peers                []string               `json:"peers"`
	ElectionTimeoutMin   time.Duration          `json:"election_timeout_min"`
	ElectionTimeoutMax   time.Duration          `json:"election_timeout_max"`
	HeartbeatInterval    time.Duration          `json:"heartbeat_interval"`
	TransportType        string                 `json:"transport_type"`
	TransportConfig      map[string]interface{} `json:"transport_config"`
	PersistenceType      string                 `json:"persistence_type"`
	PersistenceConfig    map[string]interface{} `json:"persistence_config"`
	EnablePreVote        bool                   `json:"enable_pre_vote"`
	MaxLogEntries        int                    `json:"max_log_entries"`
	LogCompactionEnabled bool                   `json:"log_compaction_enabled"`
}

// NewRaftBackend creates a new Raft-based leader election backend
func NewRaftBackend(config *Config, logger common.Logger, metrics common.Metrics) (Backend, error) {
	if config == nil {
		return nil, common.ErrInvalidConfig("config", fmt.Errorf("config cannot be nil"))
	}

	// Parse Raft-specific configuration
	raftConfig, err := parseRaftConfig(config.BackendConfig)
	if err != nil {
		return nil, common.ErrInvalidConfig("raft_config", err)
	}

	// Create transport
	transport, err := createTransport(raftConfig.TransportType, raftConfig.TransportConfig, logger, metrics)
	if err != nil {
		return nil, common.ErrInvalidConfig("transport", err)
	}

	// Create persistence
	persistence, err := createPersistence(raftConfig.PersistenceType, raftConfig.PersistenceConfig, logger)
	if err != nil {
		return nil, common.ErrInvalidConfig("persistence", err)
	}

	backend := &RaftBackend{
		config:      config,
		nodeID:      config.NodeID,
		clusterID:   config.ClusterID,
		state:       StateFollower,
		peers:       raftConfig.Peers,
		transport:   transport,
		persistence: persistence,
		nextIndex:   make(map[string]uint64),
		matchIndex:  make(map[string]uint64),
		stopChannel: make(chan struct{}),
		logger:      logger,
		metrics:     metrics,
	}

	// Set random election timeout
	backend.resetElectionTimeout()

	// Set up transport handlers
	transport.SetVoteHandler(backend.handleVoteRequest)
	transport.SetAppendHandler(backend.handleAppendEntries)

	return backend, nil
}

// Start starts the Raft backend
func (rb *RaftBackend) Start(ctx context.Context) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.started {
		return common.ErrLifecycleError("start", fmt.Errorf("raft backend already started"))
	}

	if rb.logger != nil {
		rb.logger.Info("starting raft backend",
			logger.String("node_id", rb.nodeID),
			logger.String("cluster_id", rb.clusterID),
		)
	}

	// Restore persistent state
	if err := rb.persistence.Restore(); err != nil {
		return common.ErrServiceStartFailed("raft-backend", err)
	}

	rb.currentTerm = rb.persistence.GetCurrentTerm()
	rb.votedFor = rb.persistence.GetVotedFor()
	rb.log = rb.persistence.GetLog()

	// Start transport
	if err := rb.transport.Start(ctx); err != nil {
		return common.ErrServiceStartFailed("raft-backend", err)
	}

	// Start main loop
	rb.wg.Add(1)
	go rb.mainLoop(ctx)

	// Start election timer
	rb.electionTimer = time.NewTimer(rb.electionTimeout)
	rb.wg.Add(1)
	go rb.electionTimerLoop(ctx)

	rb.started = true

	if rb.logger != nil {
		rb.logger.Info("raft backend started")
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.raft_started").Inc()
	}

	return nil
}

// Stop stops the Raft backend
func (rb *RaftBackend) Stop(ctx context.Context) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.started {
		return common.ErrLifecycleError("stop", fmt.Errorf("raft backend not started"))
	}

	if rb.logger != nil {
		rb.logger.Info("stopping raft backend")
	}

	// Signal stop
	close(rb.stopChannel)

	// Stop timers
	if rb.electionTimer != nil {
		rb.electionTimer.Stop()
	}
	if rb.heartbeatTimer != nil {
		rb.heartbeatTimer.Stop()
	}

	// Wait for goroutines to finish
	rb.wg.Wait()

	// Stop transport
	if err := rb.transport.Stop(ctx); err != nil {
		if rb.logger != nil {
			rb.logger.Error("failed to stop transport", logger.Error(err))
		}
	}

	// Persist final state
	if err := rb.persistence.Persist(); err != nil {
		if rb.logger != nil {
			rb.logger.Error("failed to persist final state", logger.Error(err))
		}
	}

	rb.started = false

	if rb.logger != nil {
		rb.logger.Info("raft backend stopped")
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.raft_stopped").Inc()
	}

	return nil
}

// Campaign starts a campaign to become leader
func (rb *RaftBackend) Campaign(ctx context.Context, nodeID string, ttl time.Duration) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if nodeID != rb.nodeID {
		return common.ErrValidationError("node_id", fmt.Errorf("can only campaign for self"))
	}

	if rb.state == StateLeader {
		return nil // Already leader
	}

	// Become candidate
	rb.becomeCandidate()

	return nil
}

// Resign resigns from leadership
func (rb *RaftBackend) Resign(ctx context.Context, nodeID string) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if nodeID != rb.nodeID {
		return common.ErrValidationError("node_id", fmt.Errorf("can only resign for self"))
	}

	if rb.state == StateLeader {
		rb.becomeFollower(rb.currentTerm)
	}

	return nil
}

// GetLeader returns the current leader
func (rb *RaftBackend) GetLeader(ctx context.Context) (string, error) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return rb.leaderID, nil
}

// IsLeader returns true if the specified node is the leader
func (rb *RaftBackend) IsLeader(ctx context.Context, nodeID string) (bool, error) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return rb.state == StateLeader && rb.nodeID == nodeID, nil
}

// Heartbeat sends heartbeat (no-op for Raft as it's handled internally)
func (rb *RaftBackend) Heartbeat(ctx context.Context, nodeID string, ttl time.Duration) error {
	// Heartbeats are handled internally by the leader
	return nil
}

// Watch registers a callback for leadership changes
func (rb *RaftBackend) Watch(ctx context.Context, callback func(leaderID string)) error {
	rb.watchersMu.Lock()
	defer rb.watchersMu.Unlock()

	rb.watchers = append(rb.watchers, callback)
	return nil
}

// HealthCheck performs a health check
func (rb *RaftBackend) HealthCheck(ctx context.Context) error {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if !rb.started {
		return common.ErrHealthCheckFailed("raft-backend", fmt.Errorf("backend not started"))
	}

	// Check if we have recent contact with leader (if not leader ourselves)
	if rb.state != StateLeader {
		timeSinceLastHeartbeat := time.Since(rb.lastHeartbeat)
		if timeSinceLastHeartbeat > rb.config.ElectionTimeout*2 {
			return common.ErrHealthCheckFailed("raft-backend", fmt.Errorf("no recent contact with leader"))
		}
	}

	return nil
}

// mainLoop runs the main Raft loop
func (rb *RaftBackend) mainLoop(ctx context.Context) {
	defer rb.wg.Done()

	for {
		select {
		case <-rb.stopChannel:
			return
		case <-ctx.Done():
			return
		default:
			// State-specific behavior
			rb.mu.RLock()
			state := rb.state
			rb.mu.RUnlock()

			switch state {
			case StateLeader:
				rb.leaderLoop(ctx)
			case StateFollower:
				rb.followerLoop(ctx)
			case StateCandidate:
				rb.candidateLoop(ctx)
			}

			// Small delay to prevent tight loop
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// electionTimerLoop handles election timeout
func (rb *RaftBackend) electionTimerLoop(ctx context.Context) {
	defer rb.wg.Done()

	for {
		select {
		case <-rb.stopChannel:
			return
		case <-ctx.Done():
			return
		case <-rb.electionTimer.C:
			rb.handleElectionTimeout()
		}
	}
}

// handleElectionTimeout handles election timeout
func (rb *RaftBackend) handleElectionTimeout() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.state != StateLeader {
		if rb.logger != nil {
			rb.logger.Info("election timeout, becoming candidate")
		}
		rb.becomeCandidate()
	}
}

// becomeFollower transitions to follower state
func (rb *RaftBackend) becomeFollower(term uint64) {
	oldState := rb.state
	rb.state = StateFollower
	rb.currentTerm = term
	rb.votedFor = ""
	rb.leaderID = ""

	// Stop heartbeat timer if running
	if rb.heartbeatTimer != nil {
		rb.heartbeatTimer.Stop()
		rb.heartbeatTimer = nil
	}

	// Reset election timer
	rb.resetElectionTimeout()
	rb.electionTimer.Reset(rb.electionTimeout)

	// Persist state
	rb.persistence.SetCurrentTerm(rb.currentTerm)
	rb.persistence.SetVotedFor(rb.votedFor)
	rb.persistence.Persist()

	if rb.logger != nil {
		rb.logger.Info("became follower",
			logger.String("old_state", oldState.String()),
			logger.Uint64("term", rb.currentTerm),
		)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.raft_became_follower").Inc()
	}

	// Notify watchers
	rb.notifyWatchers("")
}

// becomeCandidate transitions to candidate state
func (rb *RaftBackend) becomeCandidate() {
	oldState := rb.state
	rb.state = StateCandidate
	rb.currentTerm++
	rb.votedFor = rb.nodeID
	rb.voteCount = 1 // Vote for self

	// Stop heartbeat timer if running
	if rb.heartbeatTimer != nil {
		rb.heartbeatTimer.Stop()
		rb.heartbeatTimer = nil
	}

	// Reset election timer
	rb.resetElectionTimeout()
	rb.electionTimer.Reset(rb.electionTimeout)

	// Persist state
	rb.persistence.SetCurrentTerm(rb.currentTerm)
	rb.persistence.SetVotedFor(rb.votedFor)
	rb.persistence.Persist()

	if rb.logger != nil {
		rb.logger.Info("became candidate",
			logger.String("old_state", oldState.String()),
			logger.Uint64("term", rb.currentTerm),
		)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.raft_became_candidate").Inc()
	}

	// Start election
	go rb.startElection()
}

// becomeLeader transitions to leader state
func (rb *RaftBackend) becomeLeader() {
	oldState := rb.state
	rb.state = StateLeader
	rb.leaderID = rb.nodeID

	// Stop election timer
	rb.electionTimer.Stop()

	// Initialize leader state
	rb.peersMu.RLock()
	for _, peer := range rb.peers {
		rb.nextIndex[peer] = rb.persistence.GetLastLogIndex() + 1
		rb.matchIndex[peer] = 0
	}
	rb.peersMu.RUnlock()

	// Start heartbeat timer
	rb.heartbeatTimer = time.NewTimer(rb.config.HeartbeatInterval)
	rb.wg.Add(1)
	go rb.heartbeatLoop()

	if rb.logger != nil {
		rb.logger.Info("became leader",
			logger.String("old_state", oldState.String()),
			logger.Uint64("term", rb.currentTerm),
		)
	}

	if rb.metrics != nil {
		rb.metrics.Counter("forge.cron.raft_became_leader").Inc()
	}

	// Notify watchers
	rb.notifyWatchers(rb.nodeID)

	// Send initial heartbeat
	go rb.sendHeartbeats()
}

// startElection starts an election
func (rb *RaftBackend) startElection() {
	rb.mu.RLock()
	term := rb.currentTerm
	lastLogIndex := rb.persistence.GetLastLogIndex()
	lastLogTerm := rb.persistence.GetLastLogTerm()
	peers := make([]string, len(rb.peers))
	copy(peers, rb.peers)
	rb.mu.RUnlock()

	req := VoteRequest{
		Term:         term,
		CandidateID:  rb.nodeID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	// Send vote requests to all peers
	for _, peer := range peers {
		if peer == rb.nodeID {
			continue // Skip self
		}

		go func(peerID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := rb.transport.SendVoteRequest(ctx, peerID, req)
			if err != nil {
				if rb.logger != nil {
					rb.logger.Debug("failed to send vote request",
						logger.String("peer", peerID),
						logger.Error(err),
					)
				}
				return
			}

			rb.handleVoteResponse(resp)
		}(peer)
	}
}

// handleVoteResponse handles vote response
func (rb *RaftBackend) handleVoteResponse(resp VoteResponse) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Check if we're still a candidate
	if rb.state != StateCandidate {
		return
	}

	// Check term
	if resp.Term > rb.currentTerm {
		rb.becomeFollower(resp.Term)
		return
	}

	// Count vote
	if resp.VoteGranted {
		rb.voteCount++

		// Check if we have majority
		majority := len(rb.peers)/2 + 1
		if rb.voteCount >= majority {
			rb.becomeLeader()
		}
	}
}

// handleVoteRequest handles vote request
func (rb *RaftBackend) handleVoteRequest(req VoteRequest) VoteResponse {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	resp := VoteResponse{
		Term:        rb.currentTerm,
		VoteGranted: false,
		VoterID:     rb.nodeID,
	}

	// Check term
	if req.Term > rb.currentTerm {
		rb.becomeFollower(req.Term)
		resp.Term = rb.currentTerm
	}

	// Check if we can vote
	if req.Term == rb.currentTerm &&
		(rb.votedFor == "" || rb.votedFor == req.CandidateID) &&
		rb.isLogUpToDate(req.LastLogIndex, req.LastLogTerm) {

		resp.VoteGranted = true
		rb.votedFor = req.CandidateID
		rb.persistence.SetVotedFor(rb.votedFor)
		rb.persistence.Persist()

		// Reset election timer
		rb.resetElectionTimeout()
		rb.electionTimer.Reset(rb.electionTimeout)

		if rb.logger != nil {
			rb.logger.Info("voted for candidate",
				logger.String("candidate", req.CandidateID),
				logger.Uint64("term", req.Term),
			)
		}
	}

	return resp
}

// handleAppendEntries handles append entries request
func (rb *RaftBackend) handleAppendEntries(req AppendEntriesRequest) AppendEntriesResponse {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	resp := AppendEntriesResponse{
		Term:    rb.currentTerm,
		Success: false,
		NodeID:  rb.nodeID,
	}

	// Check term
	if req.Term > rb.currentTerm {
		rb.becomeFollower(req.Term)
		resp.Term = rb.currentTerm
	}

	// Valid heartbeat/append entries
	if req.Term == rb.currentTerm {
		if rb.state == StateCandidate {
			rb.becomeFollower(req.Term)
		}

		rb.leaderID = req.LeaderID
		rb.lastHeartbeat = time.Now()

		// Reset election timer
		rb.resetElectionTimeout()
		rb.electionTimer.Reset(rb.electionTimeout)

		resp.Success = true

		// Notify watchers if leader changed
		rb.notifyWatchers(rb.leaderID)
	}

	return resp
}

// leaderLoop handles leader-specific behavior
func (rb *RaftBackend) leaderLoop(ctx context.Context) {
	// Leader behavior is handled by heartbeat timer
}

// followerLoop handles follower-specific behavior
func (rb *RaftBackend) followerLoop(ctx context.Context) {
	// Follower behavior is handled by election timer
}

// candidateLoop handles candidate-specific behavior
func (rb *RaftBackend) candidateLoop(ctx context.Context) {
	// Candidate behavior is handled by election timer and vote responses
}

// heartbeatLoop sends periodic heartbeats
func (rb *RaftBackend) heartbeatLoop() {
	defer rb.wg.Done()

	for {
		select {
		case <-rb.stopChannel:
			return
		case <-rb.heartbeatTimer.C:
			rb.sendHeartbeats()
			rb.heartbeatTimer.Reset(rb.config.HeartbeatInterval)
		}
	}
}

// sendHeartbeats sends heartbeats to all peers
func (rb *RaftBackend) sendHeartbeats() {
	rb.mu.RLock()
	if rb.state != StateLeader {
		rb.mu.RUnlock()
		return
	}

	term := rb.currentTerm
	leaderID := rb.nodeID
	peers := make([]string, len(rb.peers))
	copy(peers, rb.peers)
	rb.mu.RUnlock()

	for _, peer := range peers {
		if peer == rb.nodeID {
			continue // Skip self
		}

		go func(peerID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			req := AppendEntriesRequest{
				Term:         term,
				LeaderID:     leaderID,
				PrevLogIndex: 0,
				PrevLogTerm:  0,
				Entries:      []LogEntry{}, // Empty for heartbeat
				LeaderCommit: 0,
			}

			_, err := rb.transport.SendAppendEntries(ctx, peerID, req)
			if err != nil {
				if rb.logger != nil {
					rb.logger.Debug("failed to send heartbeat",
						logger.String("peer", peerID),
						logger.Error(err),
					)
				}
			}
		}(peer)
	}
}

// isLogUpToDate checks if candidate's log is up to date
func (rb *RaftBackend) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	myLastIndex := rb.persistence.GetLastLogIndex()
	myLastTerm := rb.persistence.GetLastLogTerm()

	if lastLogTerm != myLastTerm {
		return lastLogTerm > myLastTerm
	}

	return lastLogIndex >= myLastIndex
}

// resetElectionTimeout resets election timeout with randomization
func (rb *RaftBackend) resetElectionTimeout() {
	min := rb.config.ElectionTimeout.Milliseconds()
	max := min * 2
	timeout := time.Duration(rand.Int63n(max-min)+min) * time.Millisecond
	rb.electionTimeout = timeout
}

// notifyWatchers notifies all watchers of leadership change
func (rb *RaftBackend) notifyWatchers(leaderID string) {
	rb.watchersMu.RLock()
	watchers := make([]func(string), len(rb.watchers))
	copy(watchers, rb.watchers)
	rb.watchersMu.RUnlock()

	for _, watcher := range watchers {
		go func(w func(string)) {
			defer func() {
				if r := recover(); r != nil {
					if rb.logger != nil {
						rb.logger.Error("watcher panic", logger.Any("panic", r))
					}
				}
			}()
			w(leaderID)
		}(watcher)
	}
}

// parseRaftConfig parses Raft-specific configuration
func parseRaftConfig(config map[string]interface{}) (*RaftConfig, error) {
	raftConfig := &RaftConfig{
		ElectionTimeoutMin:   150 * time.Millisecond,
		ElectionTimeoutMax:   300 * time.Millisecond,
		HeartbeatInterval:    50 * time.Millisecond,
		TransportType:        "memory",
		TransportConfig:      make(map[string]interface{}),
		PersistenceType:      "memory",
		PersistenceConfig:    make(map[string]interface{}),
		EnablePreVote:        false,
		MaxLogEntries:        1000,
		LogCompactionEnabled: true,
	}

	// Parse peers
	if peers, ok := config["peers"].([]interface{}); ok {
		for _, peer := range peers {
			if peerStr, ok := peer.(string); ok {
				raftConfig.Peers = append(raftConfig.Peers, peerStr)
			}
		}
	}

	// Parse other config values
	if val, ok := config["election_timeout_min"].(string); ok {
		if dur, err := time.ParseDuration(val); err == nil {
			raftConfig.ElectionTimeoutMin = dur
		}
	}

	if val, ok := config["election_timeout_max"].(string); ok {
		if dur, err := time.ParseDuration(val); err == nil {
			raftConfig.ElectionTimeoutMax = dur
		}
	}

	if val, ok := config["heartbeat_interval"].(string); ok {
		if dur, err := time.ParseDuration(val); err == nil {
			raftConfig.HeartbeatInterval = dur
		}
	}

	if val, ok := config["transport_type"].(string); ok {
		raftConfig.TransportType = val
	}

	if val, ok := config["transport_config"].(map[string]interface{}); ok {
		raftConfig.TransportConfig = val
	}

	if val, ok := config["persistence_type"].(string); ok {
		raftConfig.PersistenceType = val
	}

	if val, ok := config["persistence_config"].(map[string]interface{}); ok {
		raftConfig.PersistenceConfig = val
	}

	return raftConfig, nil
}

// createTransport creates the appropriate transport
func createTransport(transportType string, config map[string]interface{}, logger common.Logger, metrics common.Metrics) (Transport, error) {
	switch transportType {
	case "memory":
		return NewMemoryTransport(config, logger, metrics)
	case "grpc":
		return NewGRPCTransport(config, logger, metrics)
	case "http":
		return NewHTTPTransport(config, logger, metrics)
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", transportType)
	}
}

// createPersistence creates the appropriate persistence
func createPersistence(persistenceType string, config map[string]interface{}, logger common.Logger) (PersistentState, error) {
	switch persistenceType {
	case "memory":
		return NewMemoryPersistence(config, logger)
	case "file":
		return NewFilePersistence(config, logger)
	case "database":
		return NewDatabasePersistence(config, logger)
	default:
		return nil, fmt.Errorf("unsupported persistence type: %s", persistenceType)
	}
}

// Placeholder implementations for transport and persistence
// These would be implemented in separate files

// MemoryTransport is a simple in-memory transport for testing
type MemoryTransport struct {
	peers         []string
	voteHandler   func(VoteRequest) VoteResponse
	appendHandler func(AppendEntriesRequest) AppendEntriesResponse
	logger        common.Logger
	metrics       common.Metrics
}

func NewMemoryTransport(config map[string]interface{}, logger common.Logger, metrics common.Metrics) (Transport, error) {
	return &MemoryTransport{
		logger:  logger,
		metrics: metrics,
	}, nil
}

func (mt *MemoryTransport) Start(ctx context.Context) error { return nil }
func (mt *MemoryTransport) Stop(ctx context.Context) error  { return nil }
func (mt *MemoryTransport) GetPeers() []string              { return mt.peers }
func (mt *MemoryTransport) SetVoteHandler(handler func(VoteRequest) VoteResponse) {
	mt.voteHandler = handler
}
func (mt *MemoryTransport) SetAppendHandler(handler func(AppendEntriesRequest) AppendEntriesResponse) {
	mt.appendHandler = handler
}
func (mt *MemoryTransport) SendVoteRequest(ctx context.Context, nodeID string, req VoteRequest) (VoteResponse, error) {
	// Simulate network call
	return VoteResponse{VoteGranted: false}, nil
}
func (mt *MemoryTransport) SendAppendEntries(ctx context.Context, nodeID string, req AppendEntriesRequest) (AppendEntriesResponse, error) {
	// Simulate network call
	return AppendEntriesResponse{Success: true}, nil
}

// MemoryPersistence is a simple in-memory persistence for testing
type MemoryPersistence struct {
	currentTerm uint64
	votedFor    string
	log         []LogEntry
	logger      common.Logger
}

func NewMemoryPersistence(config map[string]interface{}, logger common.Logger) (PersistentState, error) {
	return &MemoryPersistence{
		log:    make([]LogEntry, 0),
		logger: logger,
	}, nil
}

func (mp *MemoryPersistence) GetCurrentTerm() uint64           { return mp.currentTerm }
func (mp *MemoryPersistence) SetCurrentTerm(term uint64) error { mp.currentTerm = term; return nil }
func (mp *MemoryPersistence) GetVotedFor() string              { return mp.votedFor }
func (mp *MemoryPersistence) SetVotedFor(nodeID string) error  { mp.votedFor = nodeID; return nil }
func (mp *MemoryPersistence) GetLog() []LogEntry               { return mp.log }
func (mp *MemoryPersistence) AppendLog(entries []LogEntry) error {
	mp.log = append(mp.log, entries...)
	return nil
}
func (mp *MemoryPersistence) TruncateLog(index uint64) error { return nil }
func (mp *MemoryPersistence) GetLastLogIndex() uint64 {
	if len(mp.log) == 0 {
		return 0
	}
	return mp.log[len(mp.log)-1].Index
}
func (mp *MemoryPersistence) GetLastLogTerm() uint64 {
	if len(mp.log) == 0 {
		return 0
	}
	return mp.log[len(mp.log)-1].Term
}
func (mp *MemoryPersistence) Persist() error { return nil }
func (mp *MemoryPersistence) Restore() error { return nil }

// NewGRPCTransport Placeholder implementations for other transport types
func NewGRPCTransport(config map[string]interface{}, logger common.Logger, metrics common.Metrics) (Transport, error) {
	return nil, fmt.Errorf("GRPC transport not implemented")
}

func NewHTTPTransport(config map[string]interface{}, logger common.Logger, metrics common.Metrics) (Transport, error) {
	return nil, fmt.Errorf("HTTP transport not implemented")
}

func NewFilePersistence(config map[string]interface{}, logger common.Logger) (PersistentState, error) {
	return nil, fmt.Errorf("file persistence not implemented")
}

func NewDatabasePersistence(config map[string]interface{}, logger common.Logger) (PersistentState, error) {
	return nil, fmt.Errorf("database persistence not implemented")
}
