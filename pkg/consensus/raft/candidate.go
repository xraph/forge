package raft

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus/storage"
	"github.com/xraph/forge/pkg/logger"
)

// Candidate handles candidate-specific operations during elections
type Candidate struct {
	nodeID    string
	clusterID string
	peers     []string
	term      uint64
	votedFor  string
	log       *Log
	storage   storage.Storage
	rpc       *RaftRPC
	logger    common.Logger
	metrics   common.Metrics

	// Election state
	votesReceived   int
	votesNeeded     int
	electionStart   time.Time
	electionTimeout time.Duration
	electionTimer   *time.Timer

	// Vote tracking
	voteRequests map[string]*RequestVoteRequest
	voteResults  map[string]*RequestVoteResponse
	voteErrors   map[string]error

	// Channels
	voteResponseCh chan *voteResponse
	electionResult chan *ElectionResult

	// Synchronization
	mu       sync.RWMutex
	shutdown chan struct{}
	started  bool
}

// CandidateConfig contains configuration for a candidate
type CandidateConfig struct {
	NodeID          string
	ClusterID       string
	Peers           []string
	Term            uint64
	VotedFor        string
	Log             *Log
	Storage         storage.Storage
	RPC             *RaftRPC
	ElectionTimeout time.Duration
	Logger          common.Logger
	Metrics         common.Metrics
}

// voteResponse represents a vote response with metadata
type voteResponse struct {
	PeerID   string
	Response *RequestVoteResponse
	Error    error
}

// NewCandidate creates a new candidate
func NewCandidate(config CandidateConfig) *Candidate {
	return &Candidate{
		nodeID:          config.NodeID,
		clusterID:       config.ClusterID,
		peers:           config.Peers,
		term:            config.Term,
		votedFor:        config.VotedFor,
		log:             config.Log,
		storage:         config.Storage,
		rpc:             config.RPC,
		electionTimeout: config.ElectionTimeout,
		logger:          config.Logger,
		metrics:         config.Metrics,
		votesReceived:   1,                           // Vote for self
		votesNeeded:     (len(config.Peers) + 2) / 2, // Majority including self
		voteRequests:    make(map[string]*RequestVoteRequest),
		voteResults:     make(map[string]*RequestVoteResponse),
		voteErrors:      make(map[string]error),
		voteResponseCh:  make(chan *voteResponse, len(config.Peers)),
		electionResult:  make(chan *ElectionResult, 1),
		shutdown:        make(chan struct{}),
	}
}

// Start starts the candidate election process
func (c *Candidate) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	c.started = true
	c.electionStart = time.Now()

	// OnStart election timer
	c.electionTimer = time.NewTimer(c.electionTimeout)

	// OnStart election process
	go c.conductElection()

	if c.logger != nil {
		c.logger.Info("candidate started election",
			logger.String("node_id", c.nodeID),
			logger.String("cluster_id", c.clusterID),
			logger.Uint64("term", c.term),
			logger.Int("votes_needed", c.votesNeeded),
			logger.Int("peers", len(c.peers)),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.raft.candidate_started").Inc()
		c.metrics.Counter("forge.consensus.raft.elections_started").Inc()
	}

	return nil
}

// Stop stops the candidate
func (c *Candidate) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	close(c.shutdown)

	if c.electionTimer != nil {
		c.electionTimer.Stop()
	}

	c.started = false

	if c.logger != nil {
		c.logger.Info("candidate stopped", logger.String("node_id", c.nodeID))
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.raft.candidate_stopped").Inc()
	}

	return nil
}

// conductElection conducts the election process
func (c *Candidate) conductElection() {
	// Send vote requests to all peers
	c.sendVoteRequests()

	// Wait for responses or timeout
	for {
		select {
		case response := <-c.voteResponseCh:
			c.handleVoteResponse(response)

			// Check if we won the election
			if c.hasWonElection() {
				c.electionResult <- &ElectionResult{
					Success:       true,
					Term:          c.term,
					VotesReceived: c.votesReceived,
					VotesNeeded:   c.votesNeeded,
					Duration:      time.Since(c.electionStart),
				}
				return
			}

		case <-c.electionTimer.C:
			// Election timeout
			c.handleElectionTimeout()
			return

		case <-c.shutdown:
			return
		}
	}
}

// sendVoteRequests sends vote requests to all peers
func (c *Candidate) sendVoteRequests() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create vote request
	voteReq := &RequestVoteRequest{
		Term:         c.term,
		CandidateID:  c.nodeID,
		LastLogIndex: c.log.LastIndex(),
		LastLogTerm:  c.log.LastTerm(),
	}

	// Send to all peers
	for _, peerID := range c.peers {
		c.voteRequests[peerID] = voteReq
		go c.sendVoteRequest(peerID, voteReq)
	}

	if c.logger != nil {
		c.logger.Debug("sent vote requests",
			logger.String("node_id", c.nodeID),
			logger.Uint64("term", c.term),
			logger.Int("peers", len(c.peers)),
		)
	}
}

// sendVoteRequest sends a vote request to a specific peer
func (c *Candidate) sendVoteRequest(peerID string, req *RequestVoteRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.rpc.SendRequestVote(ctx, peerID, req)

	select {
	case c.voteResponseCh <- &voteResponse{
		PeerID:   peerID,
		Response: resp,
		Error:    err,
	}:
	case <-c.shutdown:
		return
	case <-time.After(time.Second):
		// Channel full, log warning
		if c.logger != nil {
			c.logger.Warn("vote response channel full",
				logger.String("peer_id", peerID),
			)
		}
	}
}

// handleVoteResponse handles a vote response from a peer
func (c *Candidate) handleVoteResponse(response *voteResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if response.Error != nil {
		c.voteErrors[response.PeerID] = response.Error
		if c.logger != nil {
			c.logger.Warn("vote request failed",
				logger.String("peer_id", response.PeerID),
				logger.Error(response.Error),
			)
		}
		return
	}

	c.voteResults[response.PeerID] = response.Response

	// Check if peer has higher term
	if response.Response.Term > c.term {
		if c.logger != nil {
			c.logger.Info("discovered higher term, stepping down",
				logger.String("peer_id", response.PeerID),
				logger.Uint64("our_term", c.term),
				logger.Uint64("peer_term", response.Response.Term),
			)
		}

		// Step down to follower
		c.electionResult <- &ElectionResult{
			Success:       false,
			Term:          response.Response.Term,
			VotesReceived: c.votesReceived,
			VotesNeeded:   c.votesNeeded,
			Duration:      time.Since(c.electionStart),
		}
		return
	}

	// Count the vote
	if response.Response.VoteGranted {
		c.votesReceived++

		if c.logger != nil {
			c.logger.Debug("vote granted",
				logger.String("peer_id", response.PeerID),
				logger.Uint64("term", response.Response.Term),
				logger.Int("votes_received", c.votesReceived),
				logger.Int("votes_needed", c.votesNeeded),
			)
		}

		if c.metrics != nil {
			c.metrics.Counter("forge.consensus.raft.votes_received").Inc()
		}
	} else {
		if c.logger != nil {
			c.logger.Debug("vote denied",
				logger.String("peer_id", response.PeerID),
				logger.Uint64("term", response.Response.Term),
			)
		}

		if c.metrics != nil {
			c.metrics.Counter("forge.consensus.raft.votes_denied").Inc()
		}
	}
}

// handleElectionTimeout handles election timeout
func (c *Candidate) handleElectionTimeout() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.logger != nil {
		c.logger.Info("election timeout",
			logger.String("node_id", c.nodeID),
			logger.Uint64("term", c.term),
			logger.Int("votes_received", c.votesReceived),
			logger.Int("votes_needed", c.votesNeeded),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.consensus.raft.election_timeouts").Inc()
	}

	c.electionResult <- &ElectionResult{
		Success:       false,
		Term:          c.term,
		VotesReceived: c.votesReceived,
		VotesNeeded:   c.votesNeeded,
		Duration:      time.Since(c.electionStart),
	}
}

// hasWonElection checks if the candidate has won the election
func (c *Candidate) hasWonElection() bool {
	return c.votesReceived >= c.votesNeeded
}

// HandleAppendEntries handles AppendEntries RPC while candidate
func (c *Candidate) HandleAppendEntries(req *AppendEntriesRequest) *AppendEntriesResponse {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp := &AppendEntriesResponse{
		Term:    c.term,
		Success: false,
		NodeID:  c.nodeID,
	}

	// If we receive AppendEntries from a leader with equal or higher term, step down
	if req.Term >= c.term {
		if c.logger != nil {
			c.logger.Info("received append entries from leader, stepping down",
				logger.String("leader_id", req.LeaderID),
				logger.Uint64("leader_term", req.Term),
				logger.Uint64("our_term", c.term),
			)
		}

		// Step down to follower
		c.electionResult <- &ElectionResult{
			Success:       false,
			Term:          req.Term,
			VotesReceived: c.votesReceived,
			VotesNeeded:   c.votesNeeded,
			Duration:      time.Since(c.electionStart),
		}
	}

	return resp
}

// HandleRequestVote handles RequestVote RPC while candidate
func (c *Candidate) HandleRequestVote(req *RequestVoteRequest) *RequestVoteResponse {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp := &RequestVoteResponse{
		Term:        c.term,
		VoteGranted: false,
		VoterID:     c.nodeID,
	}

	// If we receive RequestVote from a candidate with higher term, step down
	if req.Term > c.term {
		if c.logger != nil {
			c.logger.Info("received vote request with higher term, stepping down",
				logger.String("candidate_id", req.CandidateID),
				logger.Uint64("candidate_term", req.Term),
				logger.Uint64("our_term", c.term),
			)
		}

		// Step down to follower
		c.electionResult <- &ElectionResult{
			Success:       false,
			Term:          req.Term,
			VotesReceived: c.votesReceived,
			VotesNeeded:   c.votesNeeded,
			Duration:      time.Since(c.electionStart),
		}
	}

	return resp
}

// GetElectionResult returns the election result channel
func (c *Candidate) GetElectionResult() <-chan *ElectionResult {
	return c.electionResult
}

// GetTerm returns the current term
func (c *Candidate) GetTerm() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.term
}

// GetVotesReceived returns the number of votes received
func (c *Candidate) GetVotesReceived() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.votesReceived
}

// GetVotesNeeded returns the number of votes needed
func (c *Candidate) GetVotesNeeded() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.votesNeeded
}

// GetElectionDuration returns the duration of the current election
func (c *Candidate) GetElectionDuration() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.electionStart)
}

// GetVoteResults returns the vote results
func (c *Candidate) GetVoteResults() map[string]*RequestVoteResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	results := make(map[string]*RequestVoteResponse)
	for peerID, result := range c.voteResults {
		results[peerID] = result
	}
	return results
}

// GetVoteErrors returns the vote errors
func (c *Candidate) GetVoteErrors() map[string]error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	errors := make(map[string]error)
	for peerID, err := range c.voteErrors {
		errors[peerID] = err
	}
	return errors
}

// GetCandidateStats returns candidate statistics
func (c *Candidate) GetCandidateStats() CandidateStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CandidateStats{
		NodeID:          c.nodeID,
		Term:            c.term,
		VotesReceived:   c.votesReceived,
		VotesNeeded:     c.votesNeeded,
		ElectionStart:   c.electionStart,
		ElectionTimeout: c.electionTimeout,
		Peers:           len(c.peers),
		VoteResults:     make(map[string]bool),
		VoteErrors:      make(map[string]string),
	}

	if !c.electionStart.IsZero() {
		stats.ElectionDuration = time.Since(c.electionStart)
	}

	// Copy vote results
	for peerID, result := range c.voteResults {
		stats.VoteResults[peerID] = result.VoteGranted
	}

	// Copy vote errors
	for peerID, err := range c.voteErrors {
		stats.VoteErrors[peerID] = err.Error()
	}

	return stats
}

// CandidateStats contains candidate statistics
type CandidateStats struct {
	NodeID           string            `json:"node_id"`
	Term             uint64            `json:"term"`
	VotesReceived    int               `json:"votes_received"`
	VotesNeeded      int               `json:"votes_needed"`
	ElectionStart    time.Time         `json:"election_start"`
	ElectionDuration time.Duration     `json:"election_duration"`
	ElectionTimeout  time.Duration     `json:"election_timeout"`
	Peers            int               `json:"peers"`
	VoteResults      map[string]bool   `json:"vote_results"`
	VoteErrors       map[string]string `json:"vote_errors"`
}

// IsStarted returns true if the candidate is started
func (c *Candidate) IsStarted() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started
}

// GetNodeID returns the node ID
func (c *Candidate) GetNodeID() string {
	return c.nodeID
}

// GetClusterID returns the cluster ID
func (c *Candidate) GetClusterID() string {
	return c.clusterID
}

// GetPeers returns the list of peers
func (c *Candidate) GetPeers() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	peers := make([]string, len(c.peers))
	copy(peers, c.peers)
	return peers
}

// GetElectionTimeout returns the election timeout
func (c *Candidate) GetElectionTimeout() time.Duration {
	return c.electionTimeout
}

// SetElectionTimeout sets the election timeout
func (c *Candidate) SetElectionTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.electionTimeout = timeout
}

// saveState saves persistent state
func (c *Candidate) saveState() {
	state := &storage.PersistentState{
		CurrentTerm: c.term,
		VotedFor:    c.votedFor,
		NodeID:      c.nodeID,
		ClusterID:   c.clusterID,
		UpdatedAt:   time.Now(),
	}

	if err := c.storage.StoreState(context.Background(), state); err != nil {
		if c.logger != nil {
			c.logger.Error("failed to save state", logger.Error(err))
		}
	}
}

// HasQuorum returns true if we have received responses from a majority of peers
func (c *Candidate) HasQuorum() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	responsesReceived := len(c.voteResults) + len(c.voteErrors)
	return responsesReceived >= (len(c.peers) / 2)
}

// GetResponseRate returns the response rate from peers
func (c *Candidate) GetResponseRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.peers) == 0 {
		return 1.0
	}

	responsesReceived := len(c.voteResults) + len(c.voteErrors)
	return float64(responsesReceived) / float64(len(c.peers))
}

// GetSuccessRate returns the success rate of vote requests
func (c *Candidate) GetSuccessRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.voteResults) == 0 {
		return 0.0
	}

	successfulVotes := 0
	for _, result := range c.voteResults {
		if result.VoteGranted {
			successfulVotes++
		}
	}

	return float64(successfulVotes) / float64(len(c.voteResults))
}
