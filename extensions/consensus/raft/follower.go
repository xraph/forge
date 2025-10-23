package raft

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// FollowerState manages follower-specific state and operations
type FollowerState struct {
	nodeID string
	logger forge.Logger

	// Leader tracking
	currentLeader string
	lastHeartbeat time.Time
	leaderMu      sync.RWMutex

	// Election timeout
	electionTimeout time.Duration
	timeoutMu       sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewFollowerState creates a new follower state
func NewFollowerState(nodeID string, electionTimeout time.Duration, logger forge.Logger) *FollowerState {
	return &FollowerState{
		nodeID:          nodeID,
		logger:          logger,
		electionTimeout: electionTimeout,
		lastHeartbeat:   time.Now(),
	}
}

// Start starts the follower state
func (fs *FollowerState) Start(ctx context.Context) error {
	fs.ctx, fs.cancel = context.WithCancel(ctx)

	fs.logger.Info("follower state started",
		forge.F("node_id", fs.nodeID),
		forge.F("election_timeout", fs.electionTimeout),
	)

	return nil
}

// Stop stops the follower state
func (fs *FollowerState) Stop(ctx context.Context) error {
	if fs.cancel != nil {
		fs.cancel()
	}

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		fs.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fs.logger.Info("follower state stopped")
	case <-ctx.Done():
		fs.logger.Warn("follower state stop timed out")
	}

	return nil
}

// RecordHeartbeat records a heartbeat from the leader
func (fs *FollowerState) RecordHeartbeat(leaderID string) {
	fs.leaderMu.Lock()
	defer fs.leaderMu.Unlock()

	fs.currentLeader = leaderID
	fs.lastHeartbeat = time.Now()

	fs.logger.Debug("recorded heartbeat",
		forge.F("leader", leaderID),
	)
}

// GetCurrentLeader returns the current leader
func (fs *FollowerState) GetCurrentLeader() string {
	fs.leaderMu.RLock()
	defer fs.leaderMu.RUnlock()
	return fs.currentLeader
}

// GetLastHeartbeat returns the last heartbeat time
func (fs *FollowerState) GetLastHeartbeat() time.Time {
	fs.leaderMu.RLock()
	defer fs.leaderMu.RUnlock()
	return fs.lastHeartbeat
}

// HasHeartbeatTimedOut checks if election timeout has expired
func (fs *FollowerState) HasHeartbeatTimedOut() bool {
	fs.leaderMu.RLock()
	defer fs.leaderMu.RUnlock()

	elapsed := time.Since(fs.lastHeartbeat)
	timedOut := elapsed >= fs.electionTimeout

	if timedOut {
		fs.logger.Warn("heartbeat timeout",
			forge.F("elapsed", elapsed),
			forge.F("timeout", fs.electionTimeout),
		)
	}

	return timedOut
}

// ClearLeader clears the current leader
func (fs *FollowerState) ClearLeader() {
	fs.leaderMu.Lock()
	defer fs.leaderMu.Unlock()

	oldLeader := fs.currentLeader
	fs.currentLeader = ""

	if oldLeader != "" {
		fs.logger.Info("cleared leader",
			forge.F("old_leader", oldLeader),
		)
	}
}

// SetElectionTimeout sets the election timeout
func (fs *FollowerState) SetElectionTimeout(timeout time.Duration) {
	fs.timeoutMu.Lock()
	defer fs.timeoutMu.Unlock()
	fs.electionTimeout = timeout

	fs.logger.Debug("election timeout updated",
		forge.F("timeout", timeout),
	)
}

// GetElectionTimeout returns the election timeout
func (fs *FollowerState) GetElectionTimeout() time.Duration {
	fs.timeoutMu.RLock()
	defer fs.timeoutMu.RUnlock()
	return fs.electionTimeout
}

// ResetHeartbeatTimer resets the heartbeat timer
func (fs *FollowerState) ResetHeartbeatTimer() {
	fs.leaderMu.Lock()
	defer fs.leaderMu.Unlock()
	fs.lastHeartbeat = time.Now()
}

// GetTimeSinceLastHeartbeat returns time since last heartbeat
func (fs *FollowerState) GetTimeSinceLastHeartbeat() time.Duration {
	fs.leaderMu.RLock()
	defer fs.leaderMu.RUnlock()
	return time.Since(fs.lastHeartbeat)
}

// IsHealthy checks if follower is healthy (receiving heartbeats)
func (fs *FollowerState) IsHealthy() bool {
	return !fs.HasHeartbeatTimedOut()
}

// GetStatus returns follower status
func (fs *FollowerState) GetStatus() FollowerStatus {
	fs.leaderMu.RLock()
	defer fs.leaderMu.RUnlock()

	return FollowerStatus{
		NodeID:             fs.nodeID,
		CurrentLeader:      fs.currentLeader,
		LastHeartbeat:      fs.lastHeartbeat,
		TimeSinceHeartbeat: time.Since(fs.lastHeartbeat),
		ElectionTimeout:    fs.electionTimeout,
		TimeUntilTimeout:   fs.electionTimeout - time.Since(fs.lastHeartbeat),
		Healthy:            time.Since(fs.lastHeartbeat) < fs.electionTimeout,
	}
}

// FollowerStatus represents follower status
type FollowerStatus struct {
	NodeID             string
	CurrentLeader      string
	LastHeartbeat      time.Time
	TimeSinceHeartbeat time.Duration
	ElectionTimeout    time.Duration
	TimeUntilTimeout   time.Duration
	Healthy            bool
}
