package raft

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Node implements the Raft consensus algorithm
type Node struct {
	// Configuration
	config Config
	logger forge.Logger

	// Identity
	id        string
	clusterID string

	// Persistent state (must be persisted before responding to RPCs)
	currentTerm uint64 // Latest term server has seen
	votedFor    string // CandidateId that received vote in current term
	log         *Log   // Log entries

	// Volatile state on all servers
	commitIndex uint64       // Index of highest log entry known to be committed
	lastApplied uint64       // Index of highest log entry applied to state machine
	role        atomic.Value // Current role (NodeRole)

	// Volatile state on leaders (reinitialized after election)
	nextIndex  map[string]uint64 // For each server, index of the next log entry to send
	matchIndex map[string]uint64 // For each server, index of highest log entry known to be replicated

	// State machine
	stateMachine internal.StateMachine

	// Transport layer
	transport internal.Transport

	// Storage
	storage internal.Storage

	// Cluster members
	peers     map[string]*PeerState
	peersLock sync.RWMutex

	// Leader information
	leader     string
	leaderLock sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Election state
	electionTimer    *time.Timer
	heartbeatTicker  *time.Ticker
	electionTimeout  time.Duration
	heartbeatTimeout time.Duration

	// Metrics
	electionCount    int64
	appendCount      int64
	snapshotCount    int64
	lastHeartbeat    time.Time
	lastHeartbeatMux sync.RWMutex

	// Synchronization
	mu             sync.RWMutex
	applyCh        chan ApplyMsg
	snapshotCh     chan SnapshotMsg
	configChangeCh chan ConfigChangeMsg

	// RPC response channels
	pendingRequests map[string]chan interface{}
	requestMu       sync.RWMutex

	// State
	started   bool
	startTime time.Time
}

// PeerState represents the state of a peer node
type PeerState struct {
	ID            string
	Address       string
	Port          int
	NextIndex     uint64
	MatchIndex    uint64
	LastContact   time.Time
	Replicating   bool
	ReplicationMu sync.Mutex
}

// ApplyMsg represents a message to apply to the state machine
type ApplyMsg struct {
	Index    uint64
	Term     uint64
	Entry    internal.LogEntry
	ResultCh chan error
}

// SnapshotMsg represents a snapshot message
type SnapshotMsg struct {
	Index    uint64
	Term     uint64
	ResultCh chan error
}

// ConfigChangeMsg represents a configuration change message
type ConfigChangeMsg struct {
	Type     ConfigChangeType
	NodeID   string
	Address  string
	Port     int
	ResultCh chan error
}

// ConfigChangeType represents the type of configuration change
type ConfigChangeType int

const (
	// ConfigChangeAdd adds a node to the cluster
	ConfigChangeAdd ConfigChangeType = iota
	// ConfigChangeRemove removes a node from the cluster
	ConfigChangeRemove
)

// NewNode creates a new Raft node
func NewNode(
	config Config,
	logger forge.Logger,
	stateMachine internal.StateMachine,
	transport internal.Transport,
	storage internal.Storage,
) (*Node, error) {
	if config.NodeID == "" {
		return nil, fmt.Errorf("node ID is required")
	}

	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 1 * time.Second
	}

	if config.ElectionTimeoutMin == 0 {
		config.ElectionTimeoutMin = 5 * time.Second
	}

	if config.ElectionTimeoutMax == 0 {
		config.ElectionTimeoutMax = 10 * time.Second
	}

	if config.MaxAppendEntries == 0 {
		config.MaxAppendEntries = 64
	}

	if config.LogCacheSize == 0 {
		config.LogCacheSize = 1024
	}

	if config.TrailingLogs == 0 {
		config.TrailingLogs = 10000
	}

	n := &Node{
		config:          config,
		logger:          logger,
		id:              config.NodeID,
		clusterID:       config.ClusterID,
		currentTerm:     0,
		votedFor:        "",
		commitIndex:     0,
		lastApplied:     0,
		nextIndex:       make(map[string]uint64),
		matchIndex:      make(map[string]uint64),
		stateMachine:    stateMachine,
		transport:       transport,
		storage:         storage,
		peers:           make(map[string]*PeerState),
		applyCh:         make(chan ApplyMsg, 100),
		snapshotCh:      make(chan SnapshotMsg, 10),
		configChangeCh:  make(chan ConfigChangeMsg, 10),
		pendingRequests: make(map[string]chan interface{}),
	}

	// Initialize role as follower
	n.role.Store(internal.RoleFollower)

	// Create log
	log, err := NewLog(logger, storage, config.LogCacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create log: %w", err)
	}
	n.log = log

	// Restore persistent state
	if err := n.restoreState(); err != nil {
		return nil, fmt.Errorf("failed to restore state: %w", err)
	}

	return n, nil
}

// Start starts the Raft node
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.started {
		n.mu.Unlock()
		return internal.ErrAlreadyStarted
	}

	n.ctx, n.cancel = context.WithCancel(ctx)
	n.startTime = time.Now()
	n.started = true
	n.mu.Unlock()

	n.logger.Info("starting raft node",
		forge.F("node_id", n.id),
		forge.F("cluster_id", n.clusterID),
		forge.F("term", n.currentTerm),
	)

	// Start background goroutines
	n.wg.Add(5)
	go n.runStateMachine()
	go n.runSnapshotManager()
	go n.runElectionTimer()
	go n.runHeartbeat()
	go n.runMessageProcessor()

	return nil
}

// Stop stops the Raft node
func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	if !n.started {
		n.mu.Unlock()
		return internal.ErrNotStarted
	}
	n.mu.Unlock()

	n.logger.Info("stopping raft node", forge.F("node_id", n.id))

	// Cancel context
	if n.cancel != nil {
		n.cancel()
	}

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		n.logger.Info("raft node stopped gracefully", forge.F("node_id", n.id))
	case <-ctx.Done():
		n.logger.Warn("raft node stop timed out", forge.F("node_id", n.id))
	}

	// Persist final state
	if err := n.persistState(); err != nil {
		n.logger.Error("failed to persist final state", forge.F("error", err))
	}

	return nil
}

// IsLeader returns true if this node is the leader
func (n *Node) IsLeader() bool {
	return n.GetRole() == internal.RoleLeader
}

// GetLeader returns the current leader ID
func (n *Node) GetLeader() string {
	n.leaderLock.RLock()
	defer n.leaderLock.RUnlock()
	return n.leader
}

// GetTerm returns the current term
func (n *Node) GetTerm() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.currentTerm
}

// GetRole returns the current role
func (n *Node) GetRole() internal.NodeRole {
	return n.role.Load().(internal.NodeRole)
}

// GetCurrentTerm returns the current term (alias for GetTerm for RPC handler compatibility)
func (n *Node) GetCurrentTerm() uint64 {
	return n.GetTerm()
}

// handleAppendEntries handles an AppendEntries request
func (n *Node) handleAppendEntries(req internal.AppendEntriesRequest) internal.AppendEntriesResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := internal.AppendEntriesResponse{
		Term:    n.currentTerm,
		Success: false,
	}

	// Reply false if term < currentTerm (§5.1)
	if req.Term < n.currentTerm {
		return resp
	}

	// If RPC request or response contains term T > currentTerm:
	// set currentTerm = T, convert to follower (§5.1)
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.setRole(internal.RoleFollower)
		n.persistState()
	}

	// Reset election timer on valid AppendEntries
	n.resetElectionTimerUnsafe()
	n.setLeader(req.LeaderID)

	// Reply false if log doesn't contain an entry at prevLogIndex
	// whose term matches prevLogTerm (§5.3)
	if req.PrevLogIndex > 0 {
		entry, err := n.log.Get(req.PrevLogIndex)
		if err != nil || entry.Term != req.PrevLogTerm {
			return resp
		}
	}

	// If an existing entry conflicts with a new one (same index
	// but different terms), delete the existing entry and all that
	// follow it (§5.3)
	for _, entry := range req.Entries {
		existing, err := n.log.Get(entry.Index)
		if err == nil && existing.Term != entry.Term {
			// Delete conflicting entry and all that follow
			n.log.TruncateAfter(entry.Index - 1)
		}
		// Append new entry
		n.log.Append(entry)
	}

	// If leaderCommit > commitIndex, set commitIndex =
	// min(leaderCommit, index of last new entry)
	if req.LeaderCommit > n.commitIndex {
		lastNewIndex := n.log.LastIndex()
		if req.LeaderCommit < lastNewIndex {
			n.commitIndex = req.LeaderCommit
		} else {
			n.commitIndex = lastNewIndex
		}
	}

	resp.Success = true
	resp.Term = n.currentTerm
	return resp
}

// handleRequestVote handles a RequestVote request
func (n *Node) handleRequestVote(req internal.RequestVoteRequest) internal.RequestVoteResponse {
	resp, _ := n.RequestVote(context.Background(), &req)
	return *resp
}

// handleInstallSnapshot handles an InstallSnapshot request
func (n *Node) handleInstallSnapshot(req internal.InstallSnapshotRequest) internal.InstallSnapshotResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := internal.InstallSnapshotResponse{
		Term: n.currentTerm,
	}

	// Reply immediately if term < currentTerm
	if req.Term < n.currentTerm {
		return resp
	}

	// Update term if needed
	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		n.setRole(internal.RoleFollower)
		n.persistState()
	}

	// Reset election timer
	n.resetElectionTimerUnsafe()
	n.setLeader(req.LeaderID)

	// Install snapshot
	// In a real implementation, this would save the snapshot and update state
	// For now, just acknowledge receipt
	resp.Term = n.currentTerm

	return resp
}

// Apply applies a log entry
func (n *Node) Apply(ctx context.Context, entry internal.LogEntry) error {
	if !n.IsLeader() {
		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	// Append to log
	n.mu.Lock()
	entry.Term = n.currentTerm
	entry.Index = n.log.LastIndex() + 1
	entry.Created = time.Now()

	if err := n.log.Append(entry); err != nil {
		n.mu.Unlock()
		return fmt.Errorf("failed to append log entry: %w", err)
	}

	// Persist immediately
	if err := n.persistState(); err != nil {
		n.mu.Unlock()
		return fmt.Errorf("failed to persist state: %w", err)
	}

	n.mu.Unlock()

	// Trigger replication
	n.triggerReplication()

	// Wait for commitment with timeout
	resultCh := make(chan error, 1)
	msg := ApplyMsg{
		Index:    entry.Index,
		Term:     entry.Term,
		Entry:    entry,
		ResultCh: resultCh,
	}

	select {
	case n.applyCh <- msg:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-resultCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(30 * time.Second):
		return internal.NewTimeoutError("apply operation timed out")
	}
}

// GetStats returns Raft statistics
func (n *Node) GetStats() internal.RaftStats {
	n.mu.RLock()
	defer n.mu.RUnlock()

	n.lastHeartbeatMux.RLock()
	lastHeartbeat := n.lastHeartbeat
	n.lastHeartbeatMux.RUnlock()

	return internal.RaftStats{
		NodeID:        n.id,
		Role:          n.GetRole(),
		Term:          n.currentTerm,
		Leader:        n.GetLeader(),
		CommitIndex:   n.commitIndex,
		LastApplied:   n.lastApplied,
		LastLogIndex:  n.log.LastIndex(),
		LastLogTerm:   n.log.LastTerm(),
		VotedFor:      n.votedFor,
		LastHeartbeat: lastHeartbeat,
	}
}

// Propose proposes a new command to the cluster
func (n *Node) Propose(ctx context.Context, command []byte) error {
	if !n.IsLeader() {
		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	// Create log entry
	entry := internal.LogEntry{
		Type:    internal.EntryNormal,
		Data:    command,
		Created: time.Now(),
	}

	return n.Apply(ctx, entry)
}

// GetCommitIndex returns the current commit index
func (n *Node) GetCommitIndex() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.commitIndex
}

// restoreState restores persistent state from storage
func (n *Node) restoreState() error {
	// Restore current term
	termBytes, err := n.storage.Get([]byte("currentTerm"))
	if err == nil && len(termBytes) == 8 {
		n.currentTerm = bytesToUint64(termBytes)
	}

	// Restore voted for
	votedForBytes, err := n.storage.Get([]byte("votedFor"))
	if err == nil {
		n.votedFor = string(votedForBytes)
	}

	n.logger.Info("restored raft state",
		forge.F("node_id", n.id),
		forge.F("term", n.currentTerm),
		forge.F("voted_for", n.votedFor),
	)

	return nil
}

// persistState persists current state to storage
func (n *Node) persistState() error {
	// Persist current term
	if err := n.storage.Set([]byte("currentTerm"), uint64ToBytes(n.currentTerm)); err != nil {
		return fmt.Errorf("failed to persist current term: %w", err)
	}

	// Persist voted for
	if err := n.storage.Set([]byte("votedFor"), []byte(n.votedFor)); err != nil {
		return fmt.Errorf("failed to persist voted for: %w", err)
	}

	return nil
}

// randomElectionTimeout returns a random election timeout
func (n *Node) randomElectionTimeout() time.Duration {
	min := n.config.ElectionTimeoutMin.Milliseconds()
	max := n.config.ElectionTimeoutMax.Milliseconds()
	timeout := min + rand.Int63n(max-min+1)
	return time.Duration(timeout) * time.Millisecond
}

// setLeader sets the current leader
func (n *Node) setLeader(leaderID string) {
	n.leaderLock.Lock()
	defer n.leaderLock.Unlock()

	if n.leader != leaderID {
		n.logger.Info("leader changed",
			forge.F("node_id", n.id),
			forge.F("old_leader", n.leader),
			forge.F("new_leader", leaderID),
			forge.F("term", n.currentTerm),
		)
		n.leader = leaderID
	}
}

// setRole sets the current role
func (n *Node) setRole(role internal.NodeRole) {
	oldRole := n.GetRole()
	if oldRole != role {
		n.role.Store(role)
		n.logger.Info("role changed",
			forge.F("node_id", n.id),
			forge.F("old_role", oldRole),
			forge.F("new_role", role),
			forge.F("term", n.currentTerm),
		)
	}
}

// Helper functions for byte conversion
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (56 - uint(i)*8))
	}
	return b
}

func bytesToUint64(b []byte) uint64 {
	var v uint64
	for i := 0; i < 8 && i < len(b); i++ {
		v |= uint64(b[i]) << (56 - uint(i)*8)
	}
	return v
}

// generateRequestID generates a unique request ID
func (n *Node) generateRequestID() string {
	return fmt.Sprintf("%s-%d", n.id, time.Now().UnixNano())
}

// waitForResponse waits for a response to a pending request
func (n *Node) waitForResponse(requestID string, timeout time.Duration) (interface{}, error) {
	n.requestMu.RLock()
	responseCh, exists := n.pendingRequests[requestID]
	n.requestMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("request %s not found", requestID)
	}

	select {
	case response := <-responseCh:
		// Clean up
		n.requestMu.Lock()
		delete(n.pendingRequests, requestID)
		n.requestMu.Unlock()
		return response, nil
	case <-time.After(timeout):
		// Clean up
		n.requestMu.Lock()
		delete(n.pendingRequests, requestID)
		n.requestMu.Unlock()
		return nil, fmt.Errorf("request %s timed out", requestID)
	}
}

// sendResponse sends a response to a pending request
func (n *Node) sendResponse(requestID string, response interface{}) {
	n.requestMu.RLock()
	responseCh, exists := n.pendingRequests[requestID]
	n.requestMu.RUnlock()

	if exists {
		select {
		case responseCh <- response:
		default:
			// Channel is full or closed, clean up
			n.requestMu.Lock()
			delete(n.pendingRequests, requestID)
			n.requestMu.Unlock()
		}
	}
}

// AddPeer adds a peer to the Raft node
func (n *Node) AddPeer(peerID string) {
	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if _, exists := n.peers[peerID]; !exists {
		n.peers[peerID] = &PeerState{
			ID:            peerID,
			NextIndex:     1,
			MatchIndex:    0,
			LastContact:   time.Now(),
			ReplicationMu: sync.Mutex{},
		}

		// Initialize nextIndex and matchIndex for the new peer
		n.nextIndex[peerID] = 1
		n.matchIndex[peerID] = 0

		n.logger.Info("added peer to raft node",
			forge.F("node_id", n.id),
			forge.F("peer_id", peerID),
		)
	}
}
