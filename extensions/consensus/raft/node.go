package raft

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// Node implements the Raft consensus algorithm.
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
	ctx    context.Context //nolint:containedctx // lifecycle context: created in Start, cancelled in Stop
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Election state
	electionTimer    *time.Timer
	heartbeatTicker  *time.Ticker
	electionTimeout  time.Duration
	heartbeatTimeout time.Duration

	// Metrics
	electionCount    atomic.Int64
	appendCount      atomic.Int64
	snapshotCount    atomic.Int64
	lastHeartbeat    time.Time
	lastHeartbeatMux sync.RWMutex

	// Synchronization
	mu         sync.RWMutex
	snapshotCh chan SnapshotMsg

	// Apply pipeline. applyNotify wakes the single applier goroutine when
	// commitIndex moves; applyWaiters holds the Apply calls waiting for their
	// entry, keyed by log index. Both are guarded by mu.
	applyNotify  chan struct{}
	applyWaiters map[uint64]applyWaiter

	// applyMu serialises everything that changes the state machine: applying
	// entries, taking a snapshot of it and restoring one. Lock order is
	// applyMu before mu.
	applyMu sync.Mutex

	// snapshotRecv assembles an incoming chunked snapshot. Guarded by mu.
	snapshotRecv *snapshotReceive

	// lastLeaderContact is when a valid leader last reached this node.
	// Guarded by mu.
	lastLeaderContact time.Time

	// configs is the configuration history: the configuration in force at
	// the snapshot (or the bootstrap one) followed by every configuration
	// entry in the log. The last one is current. Guarded by mu.
	configs []configEntry
	// changing is set while AddNode or RemoveNode runs. Guarded by mu.
	changing bool
	// transferTarget is the node leadership is being handed to. Client
	// requests are refused while it is set. Guarded by mu.
	transferTarget string

	// RPC correlation. Every outgoing request gets a fresh ID from requestSeq,
	// which the peer echoes in its response, so a late response can never be
	// matched to a newer request to the same peer.
	requestSeq      atomic.Uint64
	pendingRequests map[pendingKey]chan any
	requestMu       sync.RWMutex

	// State
	started   bool
	startTime time.Time
}

// PeerState represents the state of a peer node.
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

// pendingKey identifies one outstanding request to one peer.
type pendingKey struct {
	peer string
	id   uint64
}

// snapshotReceive is a snapshot being received from the leader in chunks.
type snapshotReceive struct {
	leaderTerm uint64
	index      uint64
	term       uint64
	data       []byte
	config     *internal.ClusterConfiguration
}

// applyWaiter is an Apply call waiting for its entry to be applied.
type applyWaiter struct {
	term uint64
	ch   chan error
}

// ApplyMsg represents a message to apply to the state machine.
type ApplyMsg struct {
	Index    uint64
	Term     uint64
	Entry    internal.LogEntry
	ResultCh chan error
}

// SnapshotMsg represents a snapshot message.
type SnapshotMsg struct {
	Index    uint64
	Term     uint64
	ResultCh chan error
}

// NewNode creates a new Raft node.
func NewNode(
	config Config,
	logger forge.Logger,
	stateMachine internal.StateMachine,
	transport internal.Transport,
	storage internal.Storage,
) (*Node, error) {
	if config.NodeID == "" {
		return nil, errors.New("node ID is required")
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

	if config.SnapshotChunkSize <= 0 {
		config.SnapshotChunkSize = 1 << 20
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
		snapshotCh:      make(chan SnapshotMsg, 10),
		applyNotify:     make(chan struct{}, 1),
		applyWaiters:    make(map[uint64]applyWaiter),
		pendingRequests: make(map[pendingKey]chan any),
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

	if err := n.restoreSnapshot(); err != nil {
		return nil, fmt.Errorf("failed to restore snapshot: %w", err)
	}

	n.restoreConfigs()

	return n, nil
}

// Start starts the Raft node.
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

// Stop stops the Raft node.
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

// IsLeader returns true if this node is the leader.
func (n *Node) IsLeader() bool {
	return n.GetRole() == internal.RoleLeader
}

// GetLeader returns the current leader ID.
func (n *Node) GetLeader() string {
	n.leaderLock.RLock()
	defer n.leaderLock.RUnlock()

	return n.leader
}

// GetTerm returns the current term.
func (n *Node) GetTerm() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.currentTerm
}

// GetRole returns the current role.
func (n *Node) GetRole() internal.NodeRole {
	return n.role.Load().(internal.NodeRole)
}

// GetCurrentTerm returns the current term (alias for GetTerm for RPC handler compatibility).
func (n *Node) GetCurrentTerm() uint64 {
	return n.GetTerm()
}

// handleAppendEntries handles an AppendEntries request following section 5.3
// of the Raft paper.
//
// Entries the log already holds with the same term are skipped, so a
// retransmitted or delayed request never duplicates entries or moves the log
// backwards. The first entry whose term conflicts truncates the log from that
// index before the remaining entries are appended. Success is only reported
// once the new entries are durable, and commitIndex only advances to the last
// entry this request vouched for, never to a stale tail that happens to follow.
func (n *Node) handleAppendEntries(req internal.AppendEntriesRequest) internal.AppendEntriesResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := internal.AppendEntriesResponse{
		Term:    n.currentTerm,
		Success: false,
		NodeID:  n.id,
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
		resp.Term = n.currentTerm

		if err := n.persistState(); err != nil {
			n.logger.Error("failed to persist term from AppendEntries, rejecting request",
				forge.F("node_id", n.id),
				forge.F("term", n.currentTerm),
				forge.F("error", err),
			)

			return resp
		}
	}

	// A valid leader exists for this term, so a candidate or a stale leader
	// returns to follower.
	n.setRole(internal.RoleFollower)
	n.resetElectionTimerUnsafe()
	n.setLeader(req.LeaderID)
	n.lastLeaderContact = time.Now()

	resp.LastLogIndex = n.log.LastIndex()

	// Reply false if log doesn't contain an entry at prevLogIndex
	// whose term matches prevLogTerm (§5.3). Entries covered by the snapshot
	// are committed, so they match by the Log Matching property.
	if req.PrevLogIndex > n.log.SnapshotIndex() && !n.log.MatchesTerm(req.PrevLogIndex, req.PrevLogTerm) {
		return resp
	}

	if req.PrevLogIndex == n.log.SnapshotIndex() && req.PrevLogIndex > 0 && req.PrevLogTerm != n.log.SnapshotTerm() {
		return resp
	}

	// The entries must continue directly from prevLogIndex.
	for i, entry := range req.Entries {
		if entry.Index != req.PrevLogIndex+1+uint64(i) {
			n.logger.Warn("rejecting AppendEntries with non-contiguous entries",
				forge.F("node_id", n.id),
				forge.F("leader", req.LeaderID),
				forge.F("position", i),
				forge.F("index", entry.Index),
			)

			return resp
		}
	}

	if err := n.reconcileEntriesLocked(req.Entries); err != nil {
		n.logger.Error("failed to store entries from AppendEntries, rejecting request",
			forge.F("node_id", n.id),
			forge.F("leader", req.LeaderID),
			forge.F("error", err),
		)

		resp.LastLogIndex = n.log.LastIndex()

		return resp
	}

	// If leaderCommit > commitIndex, set commitIndex =
	// min(leaderCommit, index of last new entry)
	lastNewIndex := req.PrevLogIndex + uint64(len(req.Entries))
	if req.LeaderCommit > n.commitIndex {
		if newCommit := min(req.LeaderCommit, lastNewIndex); newCommit > n.commitIndex {
			n.commitIndex = newCommit
			n.signalApply()
		}
	}

	n.appendCount.Add(1)

	resp.Success = true
	resp.Term = n.currentTerm
	resp.MatchIndex = lastNewIndex
	resp.LastLogIndex = n.log.LastIndex()

	return resp
}

// reconcileEntriesLocked merges a contiguous run of leader entries into the
// log. Entries already present with the same term are skipped; the first
// conflicting entry truncates the log there; everything after it is appended.
// The caller holds n.mu.
func (n *Node) reconcileEntriesLocked(entries []internal.LogEntry) error {
	snapshotIndex := n.log.SnapshotIndex()
	lastIndex := n.log.LastIndex()

	for i, entry := range entries {
		if entry.Index <= snapshotIndex {
			continue
		}

		if entry.Index > lastIndex {
			return n.appendFromLeaderLocked(entries[i:])
		}

		term, _ := n.log.Term(entry.Index)
		if term == entry.Term {
			continue
		}

		if entry.Index <= n.commitIndex {
			return fmt.Errorf("leader entry %d (term %d) conflicts with committed entry (term %d)",
				entry.Index, entry.Term, term)
		}

		if err := n.log.TruncateAfter(entry.Index - 1); err != nil {
			return fmt.Errorf("truncate conflicting entries from %d: %w", entry.Index, err)
		}

		// Configurations take effect on append, so truncating their entries
		// returns to the configuration before them.
		n.truncateConfigsLocked(entry.Index - 1)

		return n.appendFromLeaderLocked(entries[i:])
	}

	return nil
}

// appendFromLeaderLocked appends entries and puts any configuration among
// them into effect.
func (n *Node) appendFromLeaderLocked(entries []internal.LogEntry) error {
	if err := n.log.AppendBatch(entries); err != nil {
		return err
	}

	n.applyAppendedConfigsLocked(entries)

	return nil
}

// handleRequestVote handles a RequestVote request.
func (n *Node) handleRequestVote(req internal.RequestVoteRequest) internal.RequestVoteResponse {
	resp, _ := n.RequestVote(context.Background(), &req)

	return *resp
}

// handleInstallSnapshot handles one chunk of an InstallSnapshot request
// (section 7 of the Raft paper). Chunks must arrive in order; a chunk that does
// not continue the snapshot being assembled is refused and the leader starts
// again from offset 0. When the last chunk arrives the snapshot is persisted,
// restored into the state machine, and the log prefix it covers is dropped.
func (n *Node) handleInstallSnapshot(req internal.InstallSnapshotRequest) internal.InstallSnapshotResponse {
	n.mu.Lock()

	resp := internal.InstallSnapshotResponse{
		Term:   n.currentTerm,
		NodeID: n.id,
	}

	if req.Term < n.currentTerm {
		n.mu.Unlock()

		return resp
	}

	if req.Term > n.currentTerm {
		n.currentTerm = req.Term
		n.votedFor = ""
		resp.Term = n.currentTerm

		if err := n.persistState(); err != nil {
			n.mu.Unlock()
			n.logger.Error("failed to persist term from InstallSnapshot, rejecting it",
				forge.F("node_id", n.id),
				forge.F("error", err),
			)

			return resp
		}
	}

	n.setRole(internal.RoleFollower)
	n.resetElectionTimerUnsafe()
	n.setLeader(req.LeaderID)
	n.lastLeaderContact = time.Now()

	if req.Offset == 0 {
		n.snapshotRecv = &snapshotReceive{
			leaderTerm: req.Term,
			index:      req.LastIncludedIndex,
			term:       req.LastIncludedTerm,
			config:     req.Configuration,
		}
	}

	recv := n.snapshotRecv
	if recv == nil || recv.leaderTerm != req.Term || recv.index != req.LastIncludedIndex ||
		recv.term != req.LastIncludedTerm || uint64(len(recv.data)) != req.Offset {
		n.snapshotRecv = nil
		n.mu.Unlock()

		n.logger.Warn("rejecting out of order snapshot chunk",
			forge.F("node_id", n.id),
			forge.F("leader", req.LeaderID),
			forge.F("index", req.LastIncludedIndex),
			forge.F("offset", req.Offset),
		)

		return resp
	}

	recv.data = append(recv.data, req.Data...)

	if !req.Done {
		n.mu.Unlock()

		resp.Success = true

		return resp
	}

	n.snapshotRecv = nil
	n.mu.Unlock()

	err := n.installSnapshot(recv.index, recv.term, recv.data, recv.config)
	if err != nil {
		n.logger.Error("failed to install snapshot from leader",
			forge.F("node_id", n.id),
			forge.F("leader", req.LeaderID),
			forge.F("index", recv.index),
			forge.F("error", err),
		)
	} else {
		n.logger.Info("installed snapshot from leader",
			forge.F("node_id", n.id),
			forge.F("leader", req.LeaderID),
			forge.F("index", recv.index),
			forge.F("term", recv.term),
			forge.F("size", len(recv.data)),
		)
	}

	n.mu.RLock()
	resp.Term = n.currentTerm
	n.mu.RUnlock()

	resp.Success = err == nil

	return resp
}

// installSnapshot replaces the state machine with a snapshot covering the
// log up to index, persists it, and drops the log entries it covers. Entries
// after index are kept when the log agrees with the snapshot at index;
// otherwise the whole log is discarded (section 7).
func (n *Node) installSnapshot(index, term uint64, data []byte, config *internal.ClusterConfiguration) error {
	n.applyMu.Lock()
	defer n.applyMu.Unlock()

	n.mu.RLock()
	applied := n.lastApplied
	n.mu.RUnlock()

	// The state machine already reflects this snapshot or a later point.
	if index <= applied {
		return nil
	}

	snapshot := &internal.Snapshot{Index: index, Term: term, Data: data, Size: int64(len(data))}

	if err := n.stateMachine.Restore(snapshot); err != nil {
		return fmt.Errorf("state machine rejected snapshot %d: %w", index, err)
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	// The state machine now reflects index whatever happens below, so the
	// applier must not apply the covered entries again.
	n.lastApplied = index
	n.commitIndex = max(n.commitIndex, index)

	for waitIndex, waiter := range n.applyWaiters {
		if waitIndex <= index {
			waiter.ch <- errors.New("entry was superseded by a snapshot from the leader; its result is unknown")

			delete(n.applyWaiters, waitIndex)
		}
	}

	// A leader always sends its configuration; fall back to what this node
	// knew at that index for one that did not.
	cfg := n.configAtLocked(index).Clone()
	if config != nil {
		cfg = config.Clone()
	}

	if err := n.saveSnapshot(snapshot, cfg); err != nil {
		return fmt.Errorf("failed to persist snapshot %d: %w", index, err)
	}

	if err := n.log.InstallSnapshot(index, term); err != nil {
		return fmt.Errorf("failed to drop log entries covered by snapshot %d: %w", index, err)
	}

	n.rebuildConfigsLocked(configEntry{index: index, cfg: cfg})

	n.signalApply()

	return nil
}

// Snapshot snapshots the state machine at the last applied entry and
// compacts the log up to it.
func (n *Node) Snapshot() error {
	return n.createSnapshot()
}

// Apply appends an entry on the leader and waits until it is committed and
// applied to the local state machine. It returns the state machine's error for
// that entry, or an error when leadership is lost and the entry is replaced.
func (n *Node) Apply(ctx context.Context, entry internal.LogEntry) error {
	if entry.Type == internal.EntryConfig {
		return errors.New("raft: configuration entries are written by AddNode and RemoveNode")
	}

	return n.appendAndWait(ctx, entry, false, nil)
}

// appendAndWait appends an entry on the leader and waits until it is
// committed and applied. onAppend runs under mu right after the append.
// Client entries are refused during a leadership transfer; membership
// changes have their own guard and pass skipTransferCheck.
func (n *Node) appendAndWait(ctx context.Context, entry internal.LogEntry, skipTransferCheck bool, onAppend func(index uint64)) error {
	n.mu.Lock()

	if n.GetRole() != internal.RoleLeader {
		n.mu.Unlock()

		return internal.NewNotLeaderError(n.id, n.GetLeader())
	}

	if !skipTransferCheck && n.transferBlocksLocked() {
		n.mu.Unlock()

		return ErrLeadershipTransferInProgress
	}

	entry.Term = n.currentTerm
	entry.Index = n.log.LastIndex() + 1
	entry.Created = time.Now()

	if err := n.log.Append(entry); err != nil {
		n.mu.Unlock()

		return fmt.Errorf("failed to append log entry: %w", err)
	}

	if onAppend != nil {
		onAppend(entry.Index)
	}

	resultCh := make(chan error, 1)
	n.applyWaiters[entry.Index] = applyWaiter{term: entry.Term, ch: resultCh}

	// A single-node cluster is its own majority.
	n.advanceCommitIndex()
	n.mu.Unlock()

	n.triggerReplication()

	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()

	var err error

	select {
	case err = <-resultCh:
		return err
	case <-ctx.Done():
		err = ctx.Err()
	case <-n.doneCh():
		err = internal.ErrNotStarted
	case <-timer.C:
		err = internal.NewTimeoutError("apply operation timed out")
	}

	n.mu.Lock()
	if w, ok := n.applyWaiters[entry.Index]; ok && w.ch == resultCh {
		delete(n.applyWaiters, entry.Index)
	}
	n.mu.Unlock()

	return err
}

// doneCh returns a channel closed when the node stops, or nil before Start.
func (n *Node) doneCh() <-chan struct{} {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.ctx == nil {
		return nil
	}

	return n.ctx.Done()
}

// GetStats returns Raft statistics.
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

// Propose proposes a new command to the cluster.
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

// GetCommitIndex returns the current commit index.
func (n *Node) GetCommitIndex() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.commitIndex
}

// restoreState restores the persisted term and vote. A missing key means a
// fresh node; any other read error fails startup, because guessing a term or
// vote could let the node vote twice in one term.
func (n *Node) restoreState() error {
	termBytes, err := n.storage.Get([]byte("currentTerm"))

	switch {
	case err == nil:
		if len(termBytes) != 8 {
			return fmt.Errorf("stored currentTerm has %d bytes, want 8", len(termBytes))
		}

		n.currentTerm = bytesToUint64(termBytes)
	case !isNotFound(err):
		return fmt.Errorf("failed to read currentTerm: %w", err)
	}

	votedForBytes, err := n.storage.Get([]byte("votedFor"))

	switch {
	case err == nil:
		n.votedFor = string(votedForBytes)
	case !isNotFound(err):
		return fmt.Errorf("failed to read votedFor: %w", err)
	}

	n.logger.Info("restored raft state",
		forge.F("node_id", n.id),
		forge.F("term", n.currentTerm),
		forge.F("voted_for", n.votedFor),
		forge.F("last_log_index", n.log.LastIndex()),
		forge.F("last_log_term", n.log.LastTerm()),
	)

	return nil
}

// restoreSnapshot loads the latest snapshot into the state machine so the
// node resumes applying from the first entry after it.
func (n *Node) restoreSnapshot() error {
	index := n.log.SnapshotIndex()
	if index == 0 {
		return nil
	}

	data, err := n.storage.Get(fmt.Appendf(nil, "snapshot_%d", index))
	if err != nil {
		return fmt.Errorf("snapshot %d is recorded in metadata but cannot be read: %w", index, err)
	}

	if err := n.stateMachine.Restore(&internal.Snapshot{
		Index: index,
		Term:  n.log.SnapshotTerm(),
		Data:  data,
		Size:  int64(len(data)),
	}); err != nil {
		return fmt.Errorf("state machine rejected snapshot %d: %w", index, err)
	}

	n.commitIndex = index
	n.lastApplied = index

	return nil
}

// persistState persists current state to storage.
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

// persistStateOrLog persists term and vote for RPC paths that have no error
// return. A failure here means the in-memory term or vote may be lost on
// restart, so it is logged loudly instead of being dropped.
func (n *Node) persistStateOrLog(reason string) {
	if err := n.persistState(); err != nil {
		n.logger.Error("failed to persist raft state",
			forge.F("node_id", n.id),
			forge.F("term", n.currentTerm),
			forge.F("voted_for", n.votedFor),
			forge.F("reason", reason),
			forge.F("error", err),
		)
	}
}

// randomElectionTimeout returns a random election timeout.
func (n *Node) randomElectionTimeout() time.Duration {
	minTimeout := n.config.ElectionTimeoutMin.Milliseconds()
	maxTimeout := n.config.ElectionTimeoutMax.Milliseconds()
	timeout := minTimeout + rand.Int63n(maxTimeout-minTimeout+1) //nolint:gosec // G404: election jitter only needs to desynchronise nodes, not be unpredictable

	return time.Duration(timeout) * time.Millisecond
}

// setLeader sets the current leader.
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

// setRole sets the current role.
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

// Helper functions for byte conversion.
func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)

	return b
}

func bytesToUint64(b []byte) uint64 {
	var v uint64
	for i := 0; i < 8 && i < len(b); i++ {
		v |= uint64(b[i]) << (56 - uint(i)*8)
	}

	return v
}

// call sends a request to a peer and waits for the response carrying the
// same request ID. Responses to earlier requests, which may still be in
// flight, are dropped instead of being mistaken for this one.
func (n *Node) call(ctx context.Context, peerID string, msgType internal.MessageType, payload any) (any, error) {
	key := pendingKey{peer: peerID, id: n.requestSeq.Add(1)}
	responseCh := make(chan any, 1)

	n.requestMu.Lock()
	n.pendingRequests[key] = responseCh
	n.requestMu.Unlock()

	defer func() {
		n.requestMu.Lock()
		delete(n.pendingRequests, key)
		n.requestMu.Unlock()
	}()

	msg := internal.Message{
		Type:      msgType,
		From:      n.id,
		To:        peerID,
		Payload:   payload,
		Timestamp: time.Now().UnixNano(),
		RequestID: key.id,
	}

	if err := n.transport.Send(ctx, peerID, msg); err != nil {
		return nil, err
	}

	select {
	case response := <-responseCh:
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// deliverResponse hands a response to the request waiting for it. A response
// nobody is waiting for any more is stale and is dropped.
func (n *Node) deliverResponse(from string, requestID uint64, response any) {
	n.requestMu.RLock()
	responseCh, exists := n.pendingRequests[pendingKey{peer: from, id: requestID}]
	n.requestMu.RUnlock()

	if !exists {
		n.logger.Debug("dropping response with no pending request",
			forge.F("node_id", n.id),
			forge.F("from", from),
			forge.F("request_id", requestID),
		)

		return
	}

	select {
	case responseCh <- response:
	default:
		// A duplicate response for a request that already has one.
	}
}

// AddPeer adds a voter to the bootstrap configuration, the membership used
// until the log or a snapshot holds a configuration entry. Call it before
// Start for every other initial member. Later changes go through AddNode and
// RemoveNode; AddPeer is ignored once a configuration has been recorded, and
// on a node configured to join an existing cluster.
func (n *Node) AddPeer(peerID string) {
	n.addBootstrapPeer(peerID, "", 0)
}

// PeerStatus is what this node knows about one peer.
type PeerStatus struct {
	ID string
	// LastContact is when this node last got a response from the peer while
	// leading. It is zero on followers.
	LastContact time.Time
	// MatchIndex is the highest entry known to be replicated on the peer. It
	// is only maintained on the leader.
	MatchIndex uint64
}

// Peers returns the status of every peer.
func (n *Node) Peers() []PeerStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	out := make([]PeerStatus, 0, len(n.peers))
	for id, p := range n.peers {
		out = append(out, PeerStatus{ID: id, LastContact: p.LastContact, MatchIndex: n.matchIndex[id]})
	}

	return out
}

// HasQuorum reports whether this node is in touch with a working majority
// of the voting members. A leader needs responses from a majority (itself
// included when it votes) within the maximum election timeout; a follower
// needs to have heard from a leader within that time.
func (n *Node) HasQuorum() bool {
	window := n.config.ElectionTimeoutMax
	now := time.Now()

	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.GetRole() != internal.RoleLeader {
		return n.GetLeader() != "" && now.Sub(n.lastLeaderContact) <= window
	}

	others, selfVotes := n.votersLocked()

	reachable := 0
	if selfVotes {
		reachable++
	}

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	for _, id := range others {
		if p, ok := n.peers[id]; ok && now.Sub(p.LastContact) <= window {
			reachable++
		}
	}

	return reachable >= quorumSize(len(others), selfVotes)
}

// quorumSize is the majority of the voting members.
func quorumSize(otherVoters int, selfVotes bool) int {
	voters := otherVoters
	if selfVotes {
		voters++
	}

	return voters/2 + 1
}
