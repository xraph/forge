package raft

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// LeaderState manages leader-specific state and operations.
type LeaderState struct {
	nodeID string
	logger forge.Logger

	// Peer replication state
	nextIndex  map[string]uint64
	matchIndex map[string]uint64
	stateMu    sync.RWMutex

	// Heartbeat management
	heartbeatInterval time.Duration
	lastHeartbeat     map[string]time.Time
	heartbeatMu       sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewLeaderState creates a new leader state.
func NewLeaderState(nodeID string, heartbeatInterval time.Duration, logger forge.Logger) *LeaderState {
	return &LeaderState{
		nodeID:            nodeID,
		logger:            logger,
		nextIndex:         make(map[string]uint64),
		matchIndex:        make(map[string]uint64),
		heartbeatInterval: heartbeatInterval,
		lastHeartbeat:     make(map[string]time.Time),
	}
}

// Start starts the leader state.
func (ls *LeaderState) Start(ctx context.Context, lastLogIndex uint64, peers []string) error {
	ls.ctx, ls.cancel = context.WithCancel(ctx)

	// Initialize replication state for all peers
	ls.stateMu.Lock()

	for _, peer := range peers {
		if peer != ls.nodeID {
			ls.nextIndex[peer] = lastLogIndex + 1
			ls.matchIndex[peer] = 0
			ls.lastHeartbeat[peer] = time.Time{}
		}
	}

	ls.stateMu.Unlock()

	ls.logger.Info("leader state started",
		forge.F("node_id", ls.nodeID),
		forge.F("last_log_index", lastLogIndex),
		forge.F("peer_count", len(peers)-1),
	)

	return nil
}

// Stop stops the leader state.
func (ls *LeaderState) Stop(ctx context.Context) error {
	if ls.cancel != nil {
		ls.cancel()
	}

	// Wait for goroutines
	done := make(chan struct{})

	go func() {
		ls.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		ls.logger.Info("leader state stopped")
	case <-ctx.Done():
		ls.logger.Warn("leader state stop timed out")
	}

	return nil
}

// GetNextIndex returns the next index for a peer.
func (ls *LeaderState) GetNextIndex(peerID string) uint64 {
	ls.stateMu.RLock()
	defer ls.stateMu.RUnlock()

	return ls.nextIndex[peerID]
}

// SetNextIndex sets the next index for a peer.
func (ls *LeaderState) SetNextIndex(peerID string, index uint64) {
	ls.stateMu.Lock()
	defer ls.stateMu.Unlock()

	ls.nextIndex[peerID] = index

	ls.logger.Debug("set next index",
		forge.F("peer", peerID),
		forge.F("index", index),
	)
}

// GetMatchIndex returns the match index for a peer.
func (ls *LeaderState) GetMatchIndex(peerID string) uint64 {
	ls.stateMu.RLock()
	defer ls.stateMu.RUnlock()

	return ls.matchIndex[peerID]
}

// SetMatchIndex sets the match index for a peer.
func (ls *LeaderState) SetMatchIndex(peerID string, index uint64) {
	ls.stateMu.Lock()
	defer ls.stateMu.Unlock()

	ls.matchIndex[peerID] = index

	ls.logger.Debug("set match index",
		forge.F("peer", peerID),
		forge.F("index", index),
	)
}

// UpdateMatchIndex updates match index and next index atomically.
func (ls *LeaderState) UpdateMatchIndex(peerID string, matchIndex uint64) {
	ls.stateMu.Lock()
	defer ls.stateMu.Unlock()

	if matchIndex > ls.matchIndex[peerID] {
		ls.matchIndex[peerID] = matchIndex
		ls.nextIndex[peerID] = matchIndex + 1

		ls.logger.Debug("updated match index",
			forge.F("peer", peerID),
			forge.F("match_index", matchIndex),
			forge.F("next_index", matchIndex+1),
		)
	}
}

// DecrementNextIndex decrements the next index for a peer (on replication failure).
func (ls *LeaderState) DecrementNextIndex(peerID string) uint64 {
	ls.stateMu.Lock()
	defer ls.stateMu.Unlock()

	if ls.nextIndex[peerID] > 1 {
		ls.nextIndex[peerID]--
	}

	newIndex := ls.nextIndex[peerID]

	ls.logger.Debug("decremented next index",
		forge.F("peer", peerID),
		forge.F("new_index", newIndex),
	)

	return newIndex
}

// GetAllMatchIndexes returns all match indexes.
func (ls *LeaderState) GetAllMatchIndexes() map[string]uint64 {
	ls.stateMu.RLock()
	defer ls.stateMu.RUnlock()

	result := make(map[string]uint64, len(ls.matchIndex))
	maps.Copy(result, ls.matchIndex)

	return result
}

// CalculateCommitIndex calculates the new commit index based on majority replication.
func (ls *LeaderState) CalculateCommitIndex(currentCommitIndex, lastLogIndex uint64) uint64 {
	ls.stateMu.RLock()
	defer ls.stateMu.RUnlock()

	// Collect all match indexes including our own (last log index)
	indexes := []uint64{lastLogIndex}
	for _, matchIndex := range ls.matchIndex {
		indexes = append(indexes, matchIndex)
	}

	// Sort indexes to find median
	for i := 0; i < len(indexes); i++ {
		for j := i + 1; j < len(indexes); j++ {
			if indexes[i] < indexes[j] {
				indexes[i], indexes[j] = indexes[j], indexes[i]
			}
		}
	}

	// Find majority index (median in sorted array)
	majorityIndex := len(indexes) / 2
	newCommitIndex := indexes[majorityIndex]

	// Commit index can only move forward
	if newCommitIndex > currentCommitIndex {
		ls.logger.Info("commit index advanced",
			forge.F("old_commit", currentCommitIndex),
			forge.F("new_commit", newCommitIndex),
		)

		return newCommitIndex
	}

	return currentCommitIndex
}

// RecordHeartbeat records a successful heartbeat to a peer.
func (ls *LeaderState) RecordHeartbeat(peerID string) {
	ls.heartbeatMu.Lock()
	defer ls.heartbeatMu.Unlock()

	ls.lastHeartbeat[peerID] = time.Now()
}

// GetLastHeartbeat returns the last heartbeat time for a peer.
func (ls *LeaderState) GetLastHeartbeat(peerID string) time.Time {
	ls.heartbeatMu.RLock()
	defer ls.heartbeatMu.RUnlock()

	return ls.lastHeartbeat[peerID]
}

// NeedsHeartbeat checks if a peer needs a heartbeat.
func (ls *LeaderState) NeedsHeartbeat(peerID string) bool {
	ls.heartbeatMu.RLock()
	defer ls.heartbeatMu.RUnlock()

	lastTime := ls.lastHeartbeat[peerID]

	return time.Since(lastTime) >= ls.heartbeatInterval
}

// GetPeersNeedingHeartbeat returns peers that need heartbeats.
func (ls *LeaderState) GetPeersNeedingHeartbeat() []string {
	ls.heartbeatMu.RLock()
	defer ls.heartbeatMu.RUnlock()

	var peers []string

	now := time.Now()

	for peerID, lastTime := range ls.lastHeartbeat {
		if now.Sub(lastTime) >= ls.heartbeatInterval {
			peers = append(peers, peerID)
		}
	}

	return peers
}

// AddPeer adds a new peer to track.
func (ls *LeaderState) AddPeer(peerID string, nextIndex uint64) {
	ls.stateMu.Lock()
	ls.nextIndex[peerID] = nextIndex
	ls.matchIndex[peerID] = 0
	ls.stateMu.Unlock()

	ls.heartbeatMu.Lock()
	ls.lastHeartbeat[peerID] = time.Time{}
	ls.heartbeatMu.Unlock()

	ls.logger.Info("added peer to leader state",
		forge.F("peer", peerID),
		forge.F("next_index", nextIndex),
	)
}

// RemovePeer removes a peer from tracking.
func (ls *LeaderState) RemovePeer(peerID string) {
	ls.stateMu.Lock()
	delete(ls.nextIndex, peerID)
	delete(ls.matchIndex, peerID)
	ls.stateMu.Unlock()

	ls.heartbeatMu.Lock()
	delete(ls.lastHeartbeat, peerID)
	ls.heartbeatMu.Unlock()

	ls.logger.Info("removed peer from leader state",
		forge.F("peer", peerID),
	)
}

// GetReplicationStatus returns replication status for all peers.
func (ls *LeaderState) GetReplicationStatus() map[string]ReplicationStatus {
	ls.stateMu.RLock()
	defer ls.stateMu.RUnlock()

	ls.heartbeatMu.RLock()
	defer ls.heartbeatMu.RUnlock()

	status := make(map[string]ReplicationStatus)

	for peer, nextIdx := range ls.nextIndex {
		matchIdx := ls.matchIndex[peer]
		lastHB := ls.lastHeartbeat[peer]
		lag := nextIdx - matchIdx - 1

		status[peer] = ReplicationStatus{
			PeerID:        peer,
			NextIndex:     nextIdx,
			MatchIndex:    matchIdx,
			Lag:           lag,
			LastHeartbeat: lastHB,
			Healthy:       time.Since(lastHB) < 3*ls.heartbeatInterval,
		}
	}

	return status
}

// ReplicationStatus represents replication status for a peer.
type ReplicationStatus struct {
	PeerID        string
	NextIndex     uint64
	MatchIndex    uint64
	Lag           uint64
	LastHeartbeat time.Time
	Healthy       bool
}

// GetHealthyPeerCount returns the number of healthy peers.
func (ls *LeaderState) GetHealthyPeerCount() int {
	status := ls.GetReplicationStatus()
	count := 0

	for _, s := range status {
		if s.Healthy {
			count++
		}
	}

	return count
}

// HasQuorum checks if we have a healthy quorum.
func (ls *LeaderState) HasQuorum(totalNodes int) bool {
	healthyPeers := ls.GetHealthyPeerCount()
	// +1 for leader itself
	return (healthyPeers + 1) >= (totalNodes/2 + 1)
}
