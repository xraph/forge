package raft

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/consensus/storage"
	"github.com/xraph/forge/v0/pkg/consensus/transport"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Follower handles follower-specific operations
type Follower struct {
	nodeID    string
	clusterID string
	term      uint64
	leader    string
	votedFor  string
	log       *Log
	storage   storage.Storage
	transport transport.Transport
	logger    common.Logger
	metrics   common.Metrics

	// Follower state
	lastHeartbeat time.Time
	commitIndex   uint64
	lastApplied   uint64

	// Election timeout
	electionTimeout time.Duration
	electionTimer   *time.Timer

	// Synchronization
	mu       sync.RWMutex
	shutdown chan struct{}
	started  bool
}

// FollowerConfig contains configuration for a follower
type FollowerConfig struct {
	NodeID          string
	ClusterID       string
	Term            uint64
	Leader          string
	VotedFor        string
	Log             *Log
	Storage         storage.Storage
	Transport       transport.Transport
	ElectionTimeout time.Duration
	Logger          common.Logger
	Metrics         common.Metrics
}

// NewFollower creates a new follower
func NewFollower(config FollowerConfig) *Follower {
	return &Follower{
		nodeID:          config.NodeID,
		clusterID:       config.ClusterID,
		term:            config.Term,
		leader:          config.Leader,
		votedFor:        config.VotedFor,
		log:             config.Log,
		storage:         config.Storage,
		transport:       config.Transport,
		electionTimeout: config.ElectionTimeout,
		logger:          config.Logger,
		metrics:         config.Metrics,
		lastHeartbeat:   time.Now(),
		commitIndex:     0,
		lastApplied:     0,
		shutdown:        make(chan struct{}),
	}
}

// Start starts the follower
func (f *Follower) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.started {
		return nil
	}

	f.started = true
	f.resetElectionTimer()

	// Start election timeout monitoring
	go f.monitorElectionTimeout()

	// Start log application
	go f.applyEntries()

	if f.logger != nil {
		f.logger.Info("follower started",
			logger.String("node_id", f.nodeID),
			logger.String("cluster_id", f.clusterID),
			logger.Uint64("term", f.term),
			logger.String("leader", f.leader),
		)
	}

	if f.metrics != nil {
		f.metrics.Counter("forge.consensus.raft.follower_started").Inc()
	}

	return nil
}

// Stop stops the follower
func (f *Follower) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.started {
		return nil
	}

	close(f.shutdown)

	if f.electionTimer != nil {
		f.electionTimer.Stop()
	}

	f.started = false

	if f.logger != nil {
		f.logger.Info("follower stopped", logger.String("node_id", f.nodeID))
	}

	if f.metrics != nil {
		f.metrics.Counter("forge.consensus.raft.follower_stopped").Inc()
	}

	return nil
}

// HandleAppendEntries handles AppendEntries RPC from leader
func (f *Follower) HandleAppendEntries(req *AppendEntriesRequest) *AppendEntriesResponse {
	f.mu.Lock()
	defer f.mu.Unlock()

	resp := &AppendEntriesResponse{
		Term:    f.term,
		Success: false,
		NodeID:  f.nodeID,
	}

	// Reply false if term < currentTerm
	if req.Term < f.term {
		return resp
	}

	// If RPC request contains term T > currentTerm: set currentTerm = T, convert to follower
	if req.Term > f.term {
		f.term = req.Term
		f.votedFor = ""
		f.saveState()
	}

	// Reset election timer - we heard from leader
	f.resetElectionTimer()
	f.lastHeartbeat = time.Now()
	f.leader = req.LeaderID

	// Reply false if log doesn't contain an entry at prevLogIndex whose term matches prevLogTerm
	if req.PrevLogIndex > 0 {
		if f.log.LastIndex() < req.PrevLogIndex {
			return resp
		}

		prevEntry, err := f.log.GetEntry(req.PrevLogIndex)
		if err != nil || prevEntry.Term != req.PrevLogTerm {
			return resp
		}
	}

	// If an existing entry conflicts with a new one, delete the existing entry and all that follow it
	if len(req.Entries) > 0 {
		for i, entry := range req.Entries {
			index := req.PrevLogIndex + uint64(i) + 1
			if f.log.LastIndex() >= index {
				existingEntry, err := f.log.GetEntry(index)
				if err == nil && existingEntry.Term != entry.Term {
					// Delete conflicting entry and all that follow
					if err := f.log.TruncateAfter(index - 1); err != nil {
						if f.logger != nil {
							f.logger.Error("failed to truncate log",
								logger.Uint64("index", index-1),
								logger.Error(err),
							)
						}
						return resp
					}
					break
				}
			}
		}

		// Append new entries
		for _, entry := range req.Entries {
			if err := f.log.AppendEntry(entry); err != nil {
				if f.logger != nil {
					f.logger.Error("failed to append entry",
						logger.Uint64("index", entry.Index),
						logger.Error(err),
					)
				}
				return resp
			}
		}
	}

	// If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	if req.LeaderCommit > f.commitIndex {
		f.commitIndex = min(req.LeaderCommit, f.log.LastIndex())
	}

	resp.Success = true
	resp.MatchIndex = f.log.LastIndex()

	// Save state after successful append
	f.saveState()

	if f.metrics != nil {
		f.metrics.Counter("forge.consensus.raft.append_entries_processed").Inc()
		if len(req.Entries) > 0 {
			f.metrics.Counter("forge.consensus.raft.entries_received").Add(float64(len(req.Entries)))
		}
	}

	return resp
}

// HandleRequestVote handles RequestVote RPC from candidate
func (f *Follower) HandleRequestVote(req *RequestVoteRequest) *RequestVoteResponse {
	f.mu.Lock()
	defer f.mu.Unlock()

	resp := &RequestVoteResponse{
		Term:        f.term,
		VoteGranted: false,
		VoterID:     f.nodeID,
	}

	// Reply false if term < currentTerm
	if req.Term < f.term {
		return resp
	}

	// If RPC request contains term T > currentTerm: set currentTerm = T, convert to follower
	if req.Term > f.term {
		f.term = req.Term
		f.votedFor = ""
		f.saveState()
	}

	// Grant vote if haven't voted for anyone else and candidate's log is at least as up-to-date
	if (f.votedFor == "" || f.votedFor == req.CandidateID) && f.isLogUpToDate(req.LastLogIndex, req.LastLogTerm) {
		f.votedFor = req.CandidateID
		resp.VoteGranted = true
		f.resetElectionTimer()
		f.saveState()

		if f.logger != nil {
			f.logger.Info("granted vote",
				logger.String("candidate", req.CandidateID),
				logger.Uint64("term", req.Term),
			)
		}

		if f.metrics != nil {
			f.metrics.Counter("forge.consensus.raft.votes_granted").Inc()
		}
	} else {
		if f.logger != nil {
			f.logger.Debug("denied vote",
				logger.String("candidate", req.CandidateID),
				logger.Uint64("term", req.Term),
				logger.String("voted_for", f.votedFor),
				logger.Bool("log_up_to_date", f.isLogUpToDate(req.LastLogIndex, req.LastLogTerm)),
			)
		}

		if f.metrics != nil {
			f.metrics.Counter("forge.consensus.raft.votes_denied").Inc()
		}
	}

	return resp
}

// HandleInstallSnapshot handles InstallSnapshot RPC from leader
func (f *Follower) HandleInstallSnapshot(req *InstallSnapshotRequest) *InstallSnapshotResponse {
	f.mu.Lock()
	defer f.mu.Unlock()

	resp := &InstallSnapshotResponse{
		Term:   f.term,
		NodeID: f.nodeID,
	}

	// Reply immediately if term < currentTerm
	if req.Term < f.term {
		return resp
	}

	// If RPC request contains term T > currentTerm: set currentTerm = T, convert to follower
	if req.Term > f.term {
		f.term = req.Term
		f.votedFor = ""
		f.saveState()
	}

	// Reset election timer - we heard from leader
	f.resetElectionTimer()
	f.lastHeartbeat = time.Now()
	f.leader = req.LeaderID

	// Create snapshot from request
	snapshot := &storage.Snapshot{
		Index:     req.LastIncludedIndex,
		Term:      req.LastIncludedTerm,
		Data:      req.Data,
		Timestamp: time.Now(),
		Size:      int64(len(req.Data)),
	}

	// Install snapshot
	if err := f.installSnapshot(snapshot); err != nil {
		if f.logger != nil {
			f.logger.Error("failed to install snapshot",
				logger.Uint64("last_included_index", req.LastIncludedIndex),
				logger.Error(err),
			)
		}
		return resp
	}

	if f.logger != nil {
		f.logger.Info("installed snapshot",
			logger.Uint64("last_included_index", req.LastIncludedIndex),
			logger.Uint64("last_included_term", req.LastIncludedTerm),
			logger.Int64("size", snapshot.Size),
		)
	}

	if f.metrics != nil {
		f.metrics.Counter("forge.consensus.raft.snapshots_installed").Inc()
		f.metrics.Histogram("forge.consensus.raft.snapshot_size").Observe(float64(snapshot.Size))
	}

	return resp
}

// monitorElectionTimeout monitors election timeout
func (f *Follower) monitorElectionTimeout() {
	for {
		select {
		case <-f.electionTimer.C:
			f.handleElectionTimeout()
		case <-f.shutdown:
			return
		}
	}
}

// handleElectionTimeout handles election timeout
func (f *Follower) handleElectionTimeout() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.started {
		return
	}

	if f.logger != nil {
		f.logger.Info("election timeout, becoming candidate",
			logger.String("node_id", f.nodeID),
			logger.Uint64("term", f.term),
			logger.String("leader", f.leader),
		)
	}

	if f.metrics != nil {
		f.metrics.Counter("forge.consensus.raft.election_timeouts").Inc()
	}

	// This would trigger a state transition to candidate
	// In a full implementation, this would be handled by the node's state machine
}

// applyEntries applies committed entries to the state machine
func (f *Follower) applyEntries() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			f.applyCommittedEntries()
		case <-f.shutdown:
			return
		}
	}
}

// applyCommittedEntries applies committed entries to the state machine
func (f *Follower) applyCommittedEntries() {
	f.mu.Lock()
	defer f.mu.Unlock()

	for f.lastApplied < f.commitIndex {
		f.lastApplied++
		entry, err := f.log.GetEntry(f.lastApplied)
		if err != nil {
			if f.logger != nil {
				f.logger.Error("failed to get entry for application",
					logger.Uint64("index", f.lastApplied),
					logger.Error(err),
				)
			}
			break
		}

		// Apply entry to state machine
		// This would be implemented by the state machine
		if f.logger != nil {
			f.logger.Debug("applied entry",
				logger.Uint64("index", f.lastApplied),
				logger.String("type", string(entry.Type)),
			)
		}

		if f.metrics != nil {
			f.metrics.Counter("forge.consensus.raft.entries_applied").Inc()
		}
	}
}

// isLogUpToDate checks if the candidate's log is at least as up-to-date
func (f *Follower) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	ourLastTerm := f.log.LastTerm()
	ourLastIndex := f.log.LastIndex()

	if lastLogTerm != ourLastTerm {
		return lastLogTerm > ourLastTerm
	}

	return lastLogIndex >= ourLastIndex
}

// resetElectionTimer resets the election timer with a random timeout
func (f *Follower) resetElectionTimer() {
	if f.electionTimer != nil {
		f.electionTimer.Stop()
	}

	// Add randomization to prevent split votes
	randomTimeout := f.electionTimeout + time.Duration(randInt(150))*time.Millisecond
	f.electionTimer = time.NewTimer(randomTimeout)
}

// installSnapshot installs a snapshot
func (f *Follower) installSnapshot(snapshot *storage.Snapshot) error {
	// Store snapshot
	if err := f.storage.StoreSnapshot(context.Background(), *snapshot); err != nil {
		return err
	}

	// Update log state
	f.log.SetCommitIndex(snapshot.Index)
	f.commitIndex = snapshot.Index
	f.lastApplied = snapshot.Index

	// Truncate log entries covered by snapshot
	if err := f.log.TruncateAfter(snapshot.Index); err != nil {
		return err
	}

	return nil
}

// saveState saves persistent state
func (f *Follower) saveState() {
	state := &storage.PersistentState{
		CurrentTerm: f.term,
		VotedFor:    f.votedFor,
		NodeID:      f.nodeID,
		ClusterID:   f.clusterID,
		UpdatedAt:   time.Now(),
	}

	if err := f.storage.StoreState(context.Background(), state); err != nil {
		if f.logger != nil {
			f.logger.Error("failed to save state", logger.Error(err))
		}
	}
}

// GetTerm returns the current term
func (f *Follower) GetTerm() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.term
}

// SetTerm sets the current term
func (f *Follower) SetTerm(term uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.term = term
}

// GetLeader returns the current leader
func (f *Follower) GetLeader() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.leader
}

// SetLeader sets the current leader
func (f *Follower) SetLeader(leader string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leader = leader
}

// GetVotedFor returns who we voted for
func (f *Follower) GetVotedFor() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.votedFor
}

// SetVotedFor sets who we voted for
func (f *Follower) SetVotedFor(votedFor string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.votedFor = votedFor
}

// GetCommitIndex returns the commit index
func (f *Follower) GetCommitIndex() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.commitIndex
}

// GetLastApplied returns the last applied index
func (f *Follower) GetLastApplied() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastApplied
}

// GetLastHeartbeat returns the last heartbeat time
func (f *Follower) GetLastHeartbeat() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastHeartbeat
}

// IsStarted returns true if the follower is started
func (f *Follower) IsStarted() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.started
}

// GetFollowerStats returns follower statistics
func (f *Follower) GetFollowerStats() FollowerStats {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return FollowerStats{
		NodeID:        f.nodeID,
		Term:          f.term,
		Leader:        f.leader,
		VotedFor:      f.votedFor,
		CommitIndex:   f.commitIndex,
		LastApplied:   f.lastApplied,
		LastHeartbeat: f.lastHeartbeat,
		LogSize:       f.log.Size(),
	}
}

// FollowerStats contains follower statistics
type FollowerStats struct {
	NodeID        string    `json:"node_id"`
	Term          uint64    `json:"term"`
	Leader        string    `json:"leader"`
	VotedFor      string    `json:"voted_for"`
	CommitIndex   uint64    `json:"commit_index"`
	LastApplied   uint64    `json:"last_applied"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	LogSize       int64     `json:"log_size"`
}

// GetNodeID returns the node ID
func (f *Follower) GetNodeID() string {
	return f.nodeID
}

// GetElectionTimeout returns the election timeout
func (f *Follower) GetElectionTimeout() time.Duration {
	return f.electionTimeout
}

// SetElectionTimeout sets the election timeout
func (f *Follower) SetElectionTimeout(timeout time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.electionTimeout = timeout
}

// Helper function for random integers
func randInt(n int) int {
	return int(time.Now().UnixNano() % int64(n))
}
