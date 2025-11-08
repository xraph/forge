package election

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ElectionManager manages leader elections.
type ElectionManager struct {
	nodeID   string
	raftNode internal.RaftNode
	logger   forge.Logger

	// Election state
	currentTerm       uint64
	votedFor          string
	electionTimeout   time.Duration
	heartbeatInterval time.Duration
	electionTimer     *time.Timer
	votesMu           sync.RWMutex
	votesReceived     map[uint64]map[string]bool // term -> nodeID -> granted

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// ElectionManagerConfig contains election manager configuration.
type ElectionManagerConfig struct {
	NodeID             string
	MinElectionTimeout time.Duration
	MaxElectionTimeout time.Duration
	HeartbeatInterval  time.Duration
}

// NewElectionManager creates a new election manager.
func NewElectionManager(config ElectionManagerConfig, raftNode internal.RaftNode, logger forge.Logger) *ElectionManager {
	if config.MinElectionTimeout == 0 {
		config.MinElectionTimeout = 150 * time.Millisecond
	}

	if config.MaxElectionTimeout == 0 {
		config.MaxElectionTimeout = 300 * time.Millisecond
	}

	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 50 * time.Millisecond
	}

	timeout := randomTimeout(config.MinElectionTimeout, config.MaxElectionTimeout)

	return &ElectionManager{
		nodeID:            config.NodeID,
		raftNode:          raftNode,
		logger:            logger,
		electionTimeout:   timeout,
		heartbeatInterval: config.HeartbeatInterval,
		votesReceived:     make(map[uint64]map[string]bool),
	}
}

// Start starts the election manager.
func (em *ElectionManager) Start(ctx context.Context) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.ctx, em.cancel = context.WithCancel(ctx)

	// Start election timeout
	em.resetElectionTimer()

	em.logger.Info("election manager started",
		forge.F("node_id", em.nodeID),
		forge.F("election_timeout", em.electionTimeout),
	)

	return nil
}

// Stop stops the election manager.
func (em *ElectionManager) Stop(ctx context.Context) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if em.cancel != nil {
		em.cancel()
	}

	if em.electionTimer != nil {
		em.electionTimer.Stop()
	}

	em.logger.Info("election manager stopped")

	return nil
}

// StartElection starts a new election.
func (em *ElectionManager) StartElection() error {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.currentTerm++
	em.votedFor = em.nodeID

	// Initialize vote tracking for this term
	em.votesMu.Lock()
	em.votesReceived[em.currentTerm] = map[string]bool{
		em.nodeID: true, // Vote for self
	}
	em.votesMu.Unlock()

	em.logger.Info("starting election",
		forge.F("node_id", em.nodeID),
		forge.F("term", em.currentTerm),
	)

	// Reset election timer
	em.resetElectionTimer()

	return nil
}

// ResetElectionTimeout resets the election timeout.
func (em *ElectionManager) ResetElectionTimeout() {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.resetElectionTimer()
}

// HandleVoteRequest handles a vote request.
func (em *ElectionManager) HandleVoteRequest(req internal.RequestVoteRequest) internal.RequestVoteResponse {
	em.mu.Lock()
	defer em.mu.Unlock()

	response := internal.RequestVoteResponse{
		Term:        em.currentTerm,
		VoteGranted: false,
	}

	// If request term is older, reject
	if req.Term < em.currentTerm {
		em.logger.Debug("rejecting vote - stale term",
			forge.F("candidate", req.CandidateID),
			forge.F("req_term", req.Term),
			forge.F("current_term", em.currentTerm),
		)

		return response
	}

	// If request term is newer, update term
	if req.Term > em.currentTerm {
		em.currentTerm = req.Term
		em.votedFor = ""
	}

	// Grant vote if we haven't voted or already voted for this candidate
	if em.votedFor == "" || em.votedFor == req.CandidateID {
		// TODO: Check log is up-to-date
		em.votedFor = req.CandidateID
		response.VoteGranted = true
		response.Term = em.currentTerm

		em.logger.Info("granted vote",
			forge.F("candidate", req.CandidateID),
			forge.F("term", em.currentTerm),
		)

		// Reset election timeout since we granted a vote
		em.resetElectionTimer()
	} else {
		em.logger.Debug("rejecting vote - already voted",
			forge.F("candidate", req.CandidateID),
			forge.F("voted_for", em.votedFor),
			forge.F("term", em.currentTerm),
		)
	}

	return response
}

// RecordVote records a vote response.
func (em *ElectionManager) RecordVote(term uint64, nodeID string, granted bool) {
	em.votesMu.Lock()
	defer em.votesMu.Unlock()

	if _, exists := em.votesReceived[term]; !exists {
		em.votesReceived[term] = make(map[string]bool)
	}

	em.votesReceived[term][nodeID] = granted

	em.logger.Debug("recorded vote",
		forge.F("term", term),
		forge.F("node", nodeID),
		forge.F("granted", granted),
	)
}

// TallyVotes tallies votes for the given term.
func (em *ElectionManager) TallyVotes(term uint64, quorumSize int) (votesFor, votesAgainst int, wonElection bool) {
	em.votesMu.RLock()
	defer em.votesMu.RUnlock()

	votes, exists := em.votesReceived[term]
	if !exists {
		return 0, 0, false
	}

	for _, granted := range votes {
		if granted {
			votesFor++
		} else {
			votesAgainst++
		}
	}

	wonElection = votesFor >= quorumSize

	return votesFor, votesAgainst, wonElection
}

// GetCurrentTerm returns the current term.
func (em *ElectionManager) GetCurrentTerm() uint64 {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.currentTerm
}

// SetCurrentTerm sets the current term.
func (em *ElectionManager) SetCurrentTerm(term uint64) {
	em.mu.Lock()
	defer em.mu.Unlock()

	if term > em.currentTerm {
		em.currentTerm = term
		em.votedFor = ""
		em.logger.Info("updated term",
			forge.F("new_term", term),
		)
	}
}

// GetVotedFor returns who we voted for in current term.
func (em *ElectionManager) GetVotedFor() string {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.votedFor
}

// GetElectionTimeout returns the current election timeout.
func (em *ElectionManager) GetElectionTimeout() time.Duration {
	em.mu.RLock()
	defer em.mu.RUnlock()

	return em.electionTimeout
}

// GetHeartbeatInterval returns the heartbeat interval.
func (em *ElectionManager) GetHeartbeatInterval() time.Duration {
	return em.heartbeatInterval
}

// resetElectionTimer resets the election timer (must be called with lock held).
func (em *ElectionManager) resetElectionTimer() {
	if em.electionTimer != nil {
		em.electionTimer.Stop()
	}

	// Randomize timeout to prevent split votes
	timeout := randomTimeout(em.electionTimeout, em.electionTimeout*2)

	em.electionTimer = time.AfterFunc(timeout, func() {
		// Election timeout fired - start election if we're a follower
		if em.raftNode != nil {
			em.logger.Debug("election timeout fired",
				forge.F("node_id", em.nodeID),
			)
			// The Raft node will handle this
		}
	})
}

// randomTimeout returns a random timeout between min and max.
func randomTimeout(min, max time.Duration) time.Duration {
	if min >= max {
		return min
	}

	delta := max - min

	return min + time.Duration(rand.Int63n(int64(delta)))
}

// CleanupOldVotes removes vote records for old terms.
func (em *ElectionManager) CleanupOldVotes(keepTerms int) {
	em.votesMu.Lock()
	defer em.votesMu.Unlock()

	currentTerm := em.GetCurrentTerm()

	minTerm := uint64(0)
	if currentTerm > uint64(keepTerms) {
		minTerm = currentTerm - uint64(keepTerms)
	}

	for term := range em.votesReceived {
		if term < minTerm {
			delete(em.votesReceived, term)
		}
	}
}
