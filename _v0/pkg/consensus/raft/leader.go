package raft

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/consensus/storage"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Leader handles leader-specific operations
type Leader struct {
	nodeID  string
	peers   []string
	term    uint64
	log     *Log
	rpc     *RaftRPC
	logger  common.Logger
	metrics common.Metrics

	// Leader state
	nextIndex   map[string]uint64
	matchIndex  map[string]uint64
	commitIndex uint64
	lastApplied uint64

	// Heartbeat management
	heartbeatInterval time.Duration
	heartbeatTimer    *time.Timer

	// Synchronization
	mu       sync.RWMutex
	shutdown chan struct{}
	started  bool
}

// LeaderConfig contains configuration for a leader
type LeaderConfig struct {
	NodeID            string
	Peers             []string
	Term              uint64
	Log               *Log
	RPC               *RaftRPC
	HeartbeatInterval time.Duration
	Logger            common.Logger
	Metrics           common.Metrics
}

// NewLeader creates a new leader
func NewLeader(config LeaderConfig) *Leader {
	leader := &Leader{
		nodeID:            config.NodeID,
		peers:             config.Peers,
		term:              config.Term,
		log:               config.Log,
		rpc:               config.RPC,
		heartbeatInterval: config.HeartbeatInterval,
		logger:            config.Logger,
		metrics:           config.Metrics,
		nextIndex:         make(map[string]uint64),
		matchIndex:        make(map[string]uint64),
		commitIndex:       0,
		lastApplied:       0,
		shutdown:          make(chan struct{}),
	}

	// Initialize next and match indices
	lastIndex := config.Log.LastIndex()
	for _, peer := range config.Peers {
		leader.nextIndex[peer] = lastIndex + 1
		leader.matchIndex[peer] = 0
	}

	return leader
}

// Start starts the leader
func (l *Leader) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.started {
		return nil
	}

	l.started = true

	// Start heartbeat process
	go l.heartbeatLoop()

	// Start log replication
	go l.replicationLoop()

	if l.logger != nil {
		l.logger.Info("leader started",
			logger.String("node_id", l.nodeID),
			logger.Uint64("term", l.term),
			logger.Int("peers", len(l.peers)),
		)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.leader_started").Inc()
	}

	return nil
}

// Stop stops the leader
func (l *Leader) Stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return nil
	}

	close(l.shutdown)

	if l.heartbeatTimer != nil {
		l.heartbeatTimer.Stop()
	}

	l.started = false

	if l.logger != nil {
		l.logger.Info("leader stopped", logger.String("node_id", l.nodeID))
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.leader_stopped").Inc()
	}

	return nil
}

// heartbeatLoop sends periodic heartbeats to followers
func (l *Leader) heartbeatLoop() {
	l.heartbeatTimer = time.NewTimer(l.heartbeatInterval)
	defer l.heartbeatTimer.Stop()

	for {
		select {
		case <-l.heartbeatTimer.C:
			l.sendHeartbeats()
			l.heartbeatTimer.Reset(l.heartbeatInterval)

		case <-l.shutdown:
			return
		}
	}
}

// sendHeartbeats sends heartbeats to all followers
func (l *Leader) sendHeartbeats() {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, peer := range l.peers {
		go l.sendAppendEntries(peer, true)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.heartbeats_sent").Inc()
	}
}

// replicationLoop handles log replication
func (l *Leader) replicationLoop() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.replicateToFollowers()

		case <-l.shutdown:
			return
		}
	}
}

// replicateToFollowers replicates log entries to followers
func (l *Leader) replicateToFollowers() {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, peer := range l.peers {
		if l.needsReplication(peer) {
			go l.sendAppendEntries(peer, false)
		}
	}
}

// needsReplication checks if a peer needs log replication
func (l *Leader) needsReplication(peer string) bool {
	nextIndex, exists := l.nextIndex[peer]
	if !exists {
		return false
	}

	return nextIndex <= l.log.LastIndex()
}

// sendAppendEntries sends AppendEntries RPC to a follower
func (l *Leader) sendAppendEntries(peer string, heartbeat bool) {
	l.mu.RLock()
	nextIndex := l.nextIndex[peer]
	l.mu.RUnlock()

	// Get previous log entry
	prevLogIndex := nextIndex - 1
	prevLogTerm := uint64(0)

	if prevLogIndex > 0 {
		if term, err := l.log.GetTermAtIndex(prevLogIndex); err == nil {
			prevLogTerm = term
		} else {
			if l.logger != nil {
				l.logger.Error("failed to get term at index",
					logger.Uint64("index", prevLogIndex),
					logger.Error(err),
				)
			}
			return
		}
	}

	// Get entries to send
	var entries []storage.LogEntry
	if !heartbeat && nextIndex <= l.log.LastIndex() {
		var err error
		entries, err = l.log.GetEntriesAfter(prevLogIndex)
		if err != nil {
			if l.logger != nil {
				l.logger.Error("failed to get entries after index",
					logger.Uint64("index", prevLogIndex),
					logger.Error(err),
				)
			}
			return
		}

		// Limit the number of entries to send
		if len(entries) > 100 {
			entries = entries[:100]
		}
	}

	// Create AppendEntries request
	req := &AppendEntriesRequest{
		Term:         l.term,
		LeaderID:     l.nodeID,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: l.commitIndex,
	}

	// Send request
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := l.rpc.SendAppendEntries(ctx, peer, req)
	if err != nil {
		if l.logger != nil {
			l.logger.Warn("failed to send append entries",
				logger.String("peer", peer),
				logger.Error(err),
			)
		}
		return
	}

	// Handle response
	l.handleAppendEntriesResponse(peer, req, resp)
}

// handleAppendEntriesResponse handles AppendEntries response
func (l *Leader) handleAppendEntriesResponse(peer string, req *AppendEntriesRequest, resp *AppendEntriesResponse) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if response is for current term
	if resp.Term > l.term {
		// Step down if we see a higher term
		if l.logger != nil {
			l.logger.Info("stepping down due to higher term",
				logger.String("peer", peer),
				logger.Uint64("our_term", l.term),
				logger.Uint64("peer_term", resp.Term),
			)
		}
		// This would trigger a state change to follower
		return
	}

	if resp.Term < l.term {
		// Ignore response from old term
		return
	}

	if resp.Success {
		// Update next and match indices
		if len(req.Entries) > 0 {
			l.nextIndex[peer] = req.Entries[len(req.Entries)-1].Index + 1
			l.matchIndex[peer] = req.Entries[len(req.Entries)-1].Index
		}

		// Update commit index
		l.updateCommitIndex()

		if l.metrics != nil {
			l.metrics.Counter("forge.consensus.raft.append_entries_success").Inc()
		}
	} else {
		// Decrement next index and retry
		if l.nextIndex[peer] > 1 {
			l.nextIndex[peer]--
		}

		if l.metrics != nil {
			l.metrics.Counter("forge.consensus.raft.append_entries_failed").Inc()
		}

		// Retry immediately
		go l.sendAppendEntries(peer, false)
	}
}

// updateCommitIndex updates the commit index based on match indices
func (l *Leader) updateCommitIndex() {
	// Find the highest index that's been replicated to a majority
	lastIndex := l.log.LastIndex()

	for index := l.commitIndex + 1; index <= lastIndex; index++ {
		replicationCount := 1 // Count self

		for _, matchIndex := range l.matchIndex {
			if matchIndex >= index {
				replicationCount++
			}
		}

		// Check if we have a majority
		if replicationCount > len(l.peers)/2 {
			// Verify that the entry is from current term
			if term, err := l.log.GetTermAtIndex(index); err == nil && term == l.term {
				l.commitIndex = index
				l.log.SetCommitIndex(index)

				if l.metrics != nil {
					l.metrics.Gauge("forge.consensus.raft.commit_index").Set(float64(index))
				}
			}
		} else {
			break
		}
	}
}

// AppendEntry appends a new entry to the log
func (l *Leader) AppendEntry(entry storage.LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return NewRaftError(ErrNotLeader, "not leader")
	}

	// Set term and index
	entry.Term = l.term
	entry.Index = l.log.LastIndex() + 1

	// Append to log
	if err := l.log.AppendEntry(entry); err != nil {
		return err
	}

	// Start replication immediately
	go l.replicateToFollowers()

	if l.metrics != nil {
		l.metrics.Counter("forge.consensus.raft.entries_appended").Inc()
	}

	return nil
}

// GetCommitIndex returns the current commit index
func (l *Leader) GetCommitIndex() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.commitIndex
}

// GetNextIndex returns the next index for a peer
func (l *Leader) GetNextIndex(peer string) uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.nextIndex[peer]
}

// GetMatchIndex returns the match index for a peer
func (l *Leader) GetMatchIndex(peer string) uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.matchIndex[peer]
}

// GetReplicationStatus returns replication status for all peers
func (l *Leader) GetReplicationStatus() map[string]ReplicationStatus {
	l.mu.RLock()
	defer l.mu.RUnlock()

	status := make(map[string]ReplicationStatus)
	lastIndex := l.log.LastIndex()

	for _, peer := range l.peers {
		nextIndex := l.nextIndex[peer]
		matchIndex := l.matchIndex[peer]

		status[peer] = ReplicationStatus{
			PeerID:     peer,
			NextIndex:  nextIndex,
			MatchIndex: matchIndex,
			IsCaughtUp: matchIndex >= lastIndex,
			Lag:        int64(lastIndex - matchIndex),
		}
	}

	return status
}

// ReplicationStatus represents the replication status of a peer
type ReplicationStatus struct {
	PeerID     string `json:"peer_id"`
	NextIndex  uint64 `json:"next_index"`
	MatchIndex uint64 `json:"match_index"`
	IsCaughtUp bool   `json:"is_caught_up"`
	Lag        int64  `json:"lag"`
}

// GetLeaderStats returns leader statistics
func (l *Leader) GetLeaderStats() LeaderStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	stats := LeaderStats{
		NodeID:      l.nodeID,
		Term:        l.term,
		CommitIndex: l.commitIndex,
		LastApplied: l.lastApplied,
		Peers:       len(l.peers),
		Replication: l.GetReplicationStatus(),
	}

	// Calculate average lag
	totalLag := int64(0)
	for _, status := range stats.Replication {
		totalLag += status.Lag
	}

	if len(stats.Replication) > 0 {
		stats.AverageLag = totalLag / int64(len(stats.Replication))
	}

	return stats
}

// LeaderStats contains leader statistics
type LeaderStats struct {
	NodeID      string                       `json:"node_id"`
	Term        uint64                       `json:"term"`
	CommitIndex uint64                       `json:"commit_index"`
	LastApplied uint64                       `json:"last_applied"`
	Peers       int                          `json:"peers"`
	AverageLag  int64                        `json:"average_lag"`
	Replication map[string]ReplicationStatus `json:"replication"`
}

// IsStarted returns true if the leader is started
func (l *Leader) IsStarted() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.started
}

// GetTerm returns the current term
func (l *Leader) GetTerm() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.term
}

// SetTerm sets the current term
func (l *Leader) SetTerm(term uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.term = term
}

// GetNodeID returns the node ID
func (l *Leader) GetNodeID() string {
	return l.nodeID
}

// GetPeers returns the list of peers
func (l *Leader) GetPeers() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	peers := make([]string, len(l.peers))
	copy(peers, l.peers)
	return peers
}

// AddPeer adds a new peer
func (l *Leader) AddPeer(peerID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.peers = append(l.peers, peerID)
	l.nextIndex[peerID] = l.log.LastIndex() + 1
	l.matchIndex[peerID] = 0

	if l.logger != nil {
		l.logger.Info("peer added",
			logger.String("peer_id", peerID),
			logger.Int("total_peers", len(l.peers)),
		)
	}
}

// RemovePeer removes a peer
func (l *Leader) RemovePeer(peerID string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Remove from peers slice
	for i, peer := range l.peers {
		if peer == peerID {
			l.peers = append(l.peers[:i], l.peers[i+1:]...)
			break
		}
	}

	// Remove from indices
	delete(l.nextIndex, peerID)
	delete(l.matchIndex, peerID)

	if l.logger != nil {
		l.logger.Info("peer removed",
			logger.String("peer_id", peerID),
			logger.Int("total_peers", len(l.peers)),
		)
	}
}

// CheckQuorum checks if we have a quorum of responsive peers
func (l *Leader) CheckQuorum() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// A leader always has its own vote
	activeCount := 1

	// Count peers that are reasonably caught up
	for _, peer := range l.peers {
		matchIndex := l.matchIndex[peer]
		if l.log.LastIndex()-matchIndex <= 10 { // Within 10 entries
			activeCount++
		}
	}

	required := (len(l.peers) + 2) / 2 // Majority including self
	return activeCount >= required
}

// GetHeartbeatInterval returns the heartbeat interval
func (l *Leader) GetHeartbeatInterval() time.Duration {
	return l.heartbeatInterval
}

// SetHeartbeatInterval sets the heartbeat interval
func (l *Leader) SetHeartbeatInterval(interval time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.heartbeatInterval = interval

	if l.heartbeatTimer != nil {
		l.heartbeatTimer.Reset(interval)
	}
}
