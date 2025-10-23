package raft

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// CandidateState manages candidate-specific state and operations
type CandidateState struct {
	nodeID string
	logger forge.Logger

	// Election state
	currentTerm     uint64
	votesReceived   map[string]bool
	votesGranted    int
	votesDenied     int
	votesNeeded     int
	electionStarted time.Time
	electionMu      sync.RWMutex

	// Election timeout
	electionTimeout time.Duration

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewCandidateState creates a new candidate state
func NewCandidateState(nodeID string, electionTimeout time.Duration, logger forge.Logger) *CandidateState {
	return &CandidateState{
		nodeID:          nodeID,
		logger:          logger,
		electionTimeout: electionTimeout,
		votesReceived:   make(map[string]bool),
	}
}

// Start starts a new election
func (cs *CandidateState) Start(ctx context.Context, term uint64, clusterSize int) error {
	cs.ctx, cs.cancel = context.WithCancel(ctx)

	cs.electionMu.Lock()
	defer cs.electionMu.Unlock()

	cs.currentTerm = term
	cs.votesReceived = make(map[string]bool)
	cs.votesGranted = 1 // Vote for self
	cs.votesDenied = 0
	cs.votesNeeded = (clusterSize / 2) + 1
	cs.electionStarted = time.Now()

	// Record self-vote
	cs.votesReceived[cs.nodeID] = true

	cs.logger.Info("candidate election started",
		forge.F("node_id", cs.nodeID),
		forge.F("term", term),
		forge.F("cluster_size", clusterSize),
		forge.F("votes_needed", cs.votesNeeded),
	)

	return nil
}

// Stop stops the candidate state
func (cs *CandidateState) Stop(ctx context.Context) error {
	if cs.cancel != nil {
		cs.cancel()
	}

	// Wait for goroutines
	done := make(chan struct{})
	go func() {
		cs.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		cs.logger.Info("candidate state stopped")
	case <-ctx.Done():
		cs.logger.Warn("candidate state stop timed out")
	}

	return nil
}

// RecordVote records a vote response
func (cs *CandidateState) RecordVote(voterID string, granted bool) {
	cs.electionMu.Lock()
	defer cs.electionMu.Unlock()

	// Skip if we've already received a vote from this node
	if _, exists := cs.votesReceived[voterID]; exists {
		return
	}

	cs.votesReceived[voterID] = granted

	if granted {
		cs.votesGranted++
		cs.logger.Info("vote granted",
			forge.F("voter", voterID),
			forge.F("total_granted", cs.votesGranted),
			forge.F("votes_needed", cs.votesNeeded),
		)
	} else {
		cs.votesDenied++
		cs.logger.Info("vote denied",
			forge.F("voter", voterID),
			forge.F("total_denied", cs.votesDenied),
		)
	}
}

// HasWonElection checks if the candidate has won the election
func (cs *CandidateState) HasWonElection() bool {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()

	won := cs.votesGranted >= cs.votesNeeded

	if won {
		cs.logger.Info("election won",
			forge.F("node_id", cs.nodeID),
			forge.F("term", cs.currentTerm),
			forge.F("votes_granted", cs.votesGranted),
			forge.F("votes_needed", cs.votesNeeded),
			forge.F("duration", time.Since(cs.electionStarted)),
		)
	}

	return won
}

// HasLostElection checks if the candidate has definitively lost
func (cs *CandidateState) HasLostElection(totalNodes int) bool {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()

	// Calculate maximum possible votes
	remainingNodes := totalNodes - len(cs.votesReceived)
	maxPossibleVotes := cs.votesGranted + remainingNodes

	lost := maxPossibleVotes < cs.votesNeeded

	if lost {
		cs.logger.Info("election lost",
			forge.F("node_id", cs.nodeID),
			forge.F("term", cs.currentTerm),
			forge.F("votes_granted", cs.votesGranted),
			forge.F("votes_needed", cs.votesNeeded),
			forge.F("max_possible", maxPossibleVotes),
		)
	}

	return lost
}

// HasElectionTimedOut checks if the election has timed out
func (cs *CandidateState) HasElectionTimedOut() bool {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()

	elapsed := time.Since(cs.electionStarted)
	timedOut := elapsed >= cs.electionTimeout

	if timedOut {
		cs.logger.Warn("election timed out",
			forge.F("node_id", cs.nodeID),
			forge.F("term", cs.currentTerm),
			forge.F("elapsed", elapsed),
			forge.F("timeout", cs.electionTimeout),
		)
	}

	return timedOut
}

// GetVoteCount returns the current vote counts
func (cs *CandidateState) GetVoteCount() (granted, denied, needed int) {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()
	return cs.votesGranted, cs.votesDenied, cs.votesNeeded
}

// GetTerm returns the election term
func (cs *CandidateState) GetTerm() uint64 {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()
	return cs.currentTerm
}

// GetElectionDuration returns how long the election has been running
func (cs *CandidateState) GetElectionDuration() time.Duration {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()
	return time.Since(cs.electionStarted)
}

// GetStatus returns candidate status
func (cs *CandidateState) GetStatus() CandidateStatus {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()

	return CandidateStatus{
		NodeID:           cs.nodeID,
		Term:             cs.currentTerm,
		VotesGranted:     cs.votesGranted,
		VotesDenied:      cs.votesDenied,
		VotesNeeded:      cs.votesNeeded,
		TotalVotes:       len(cs.votesReceived),
		ElectionStarted:  cs.electionStarted,
		ElectionDuration: time.Since(cs.electionStarted),
		ElectionTimeout:  cs.electionTimeout,
		TimeRemaining:    cs.electionTimeout - time.Since(cs.electionStarted),
	}
}

// CandidateStatus represents candidate status
type CandidateStatus struct {
	NodeID           string
	Term             uint64
	VotesGranted     int
	VotesDenied      int
	VotesNeeded      int
	TotalVotes       int
	ElectionStarted  time.Time
	ElectionDuration time.Duration
	ElectionTimeout  time.Duration
	TimeRemaining    time.Duration
}

// GetVoters returns all nodes that have voted and their decisions
func (cs *CandidateState) GetVoters() map[string]bool {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()

	voters := make(map[string]bool, len(cs.votesReceived))
	for voter, granted := range cs.votesReceived {
		voters[voter] = granted
	}

	return voters
}

// GetQuorumProgress returns progress towards quorum as a percentage
func (cs *CandidateState) GetQuorumProgress() float64 {
	cs.electionMu.RLock()
	defer cs.electionMu.RUnlock()

	if cs.votesNeeded == 0 {
		return 0
	}

	return float64(cs.votesGranted) / float64(cs.votesNeeded) * 100.0
}

// IsStillViable checks if the election is still viable
func (cs *CandidateState) IsStillViable(totalNodes int) bool {
	return !cs.HasWonElection() && !cs.HasLostElection(totalNodes) && !cs.HasElectionTimedOut()
}
