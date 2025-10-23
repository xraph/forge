package election

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/consensus/storage"
	"github.com/xraph/forge/v0/pkg/logger"
)

// VoteCollector manages vote collection and validation
type VoteCollector struct {
	nodeID  string
	term    uint64
	logger  common.Logger
	metrics common.Metrics
	mu      sync.RWMutex

	// Vote state
	votes         map[string]*Vote
	voteResponses map[string]*VoteResponse
	totalNodes    int
	votesNeeded   int
	votesReceived int
	votesGranted  int
	votesDenied   int
	startTime     time.Time
	endTime       time.Time

	// Vote validation
	validator VoteValidator
	storage   storage.Storage
	config    VoteCollectorConfig
}

// Vote represents a vote cast by a node
type Vote struct {
	NodeID      string    `json:"node_id"`
	Term        uint64    `json:"term"`
	CandidateID string    `json:"candidate_id"`
	Granted     bool      `json:"granted"`
	Reason      string    `json:"reason"`
	Timestamp   time.Time `json:"timestamp"`
	Signature   string    `json:"signature,omitempty"`
}

// VoteValidator validates votes
type VoteValidator interface {
	ValidateVote(ctx context.Context, vote *Vote) error
	ValidateVoteRequest(ctx context.Context, request *VoteRequest) error
	VerifyVoteSignature(ctx context.Context, vote *Vote) error
}

// VoteCollectorConfig contains configuration for vote collection
type VoteCollectorConfig struct {
	NodeID              string        `json:"node_id"`
	TotalNodes          int           `json:"total_nodes"`
	VoteTimeout         time.Duration `json:"vote_timeout"`
	RequireSignatures   bool          `json:"require_signatures"`
	AllowDuplicateVotes bool          `json:"allow_duplicate_votes"`
	PersistVotes        bool          `json:"persist_votes"`
}

// NewVoteCollector creates a new vote collector
func NewVoteCollector(
	config VoteCollectorConfig,
	validator VoteValidator,
	storage storage.Storage,
	l common.Logger,
	metrics common.Metrics,
) *VoteCollector {
	if config.VoteTimeout <= 0 {
		config.VoteTimeout = 30 * time.Second
	}

	if config.TotalNodes <= 0 {
		config.TotalNodes = 1
	}

	votesNeeded := (config.TotalNodes + 1) / 2 // Majority

	vc := &VoteCollector{
		nodeID:        config.NodeID,
		logger:        l,
		metrics:       metrics,
		votes:         make(map[string]*Vote),
		voteResponses: make(map[string]*VoteResponse),
		totalNodes:    config.TotalNodes,
		votesNeeded:   votesNeeded,
		validator:     validator,
		storage:       storage,
		config:        config,
	}

	if l != nil {
		l.Info("vote collector created",
			logger.String("node_id", config.NodeID),
			logger.Int("total_nodes", config.TotalNodes),
			logger.Int("votes_needed", votesNeeded),
			logger.Duration("vote_timeout", config.VoteTimeout),
		)
	}

	return vc
}

// StartCollection starts vote collection for a specific term
func (vc *VoteCollector) StartCollection(ctx context.Context, term uint64) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.term = term
	vc.votes = make(map[string]*Vote)
	vc.voteResponses = make(map[string]*VoteResponse)
	vc.votesReceived = 0
	vc.votesGranted = 0
	vc.votesDenied = 0
	vc.startTime = time.Now()
	vc.endTime = time.Time{}

	// Vote for self
	selfVote := &Vote{
		NodeID:      vc.nodeID,
		Term:        term,
		CandidateID: vc.nodeID,
		Granted:     true,
		Reason:      "self-vote",
		Timestamp:   time.Now(),
	}

	if err := vc.recordVote(ctx, selfVote); err != nil {
		return fmt.Errorf("failed to record self vote: %w", err)
	}

	if vc.logger != nil {
		vc.logger.Info("vote collection started",
			logger.String("node_id", vc.nodeID),
			logger.Uint64("term", term),
			logger.Int("votes_needed", vc.votesNeeded),
		)
	}

	if vc.metrics != nil {
		vc.metrics.Counter("forge.consensus.election.vote_collection_started").Inc()
	}

	return nil
}

// RecordVote records a vote
func (vc *VoteCollector) RecordVote(ctx context.Context, vote *Vote) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	return vc.recordVote(ctx, vote)
}

// recordVote records a vote (internal, assumes lock is held)
func (vc *VoteCollector) recordVote(ctx context.Context, vote *Vote) error {
	// Validate vote
	if vc.validator != nil {
		if err := vc.validator.ValidateVote(ctx, vote); err != nil {
			return fmt.Errorf("vote validation failed: %w", err)
		}
	}

	// Check for duplicate votes
	if existingVote, exists := vc.votes[vote.NodeID]; exists {
		if existingVote.Term == vote.Term {
			if !vc.config.AllowDuplicateVotes {
				return fmt.Errorf("duplicate vote from node %s for term %d", vote.NodeID, vote.Term)
			}
		}
	}

	// Store vote
	vc.votes[vote.NodeID] = vote
	vc.votesReceived++

	if vote.Granted {
		vc.votesGranted++
	} else {
		vc.votesDenied++
	}

	// Persist vote if storage is available and persistence is enabled
	if vc.storage != nil && vc.config.PersistVotes {
		// Store using the storage interface which expects term and candidateID
		if err := vc.storage.StoreVote(ctx, vote.Term, vote.CandidateID); err != nil {
			if vc.logger != nil {
				vc.logger.Warn("failed to persist vote",
					logger.String("node_id", vote.NodeID),
					logger.Uint64("term", vote.Term),
					logger.Error(err),
				)
			}
		}
	}

	if vc.logger != nil {
		vc.logger.Debug("vote recorded",
			logger.String("voter", vote.NodeID),
			logger.Uint64("term", vote.Term),
			logger.String("candidate", vote.CandidateID),
			logger.Bool("granted", vote.Granted),
			logger.String("reason", vote.Reason),
			logger.Int("votes_received", vc.votesReceived),
			logger.Int("votes_granted", vc.votesGranted),
			logger.Int("votes_needed", vc.votesNeeded),
		)
	}

	if vc.metrics != nil {
		vc.metrics.Counter("forge.consensus.election.votes_recorded").Inc()
		if vote.Granted {
			vc.metrics.Counter("forge.consensus.election.votes_granted").Inc()
		} else {
			vc.metrics.Counter("forge.consensus.election.votes_denied").Inc()
		}
	}

	return nil
}

// RecordVoteResponse records a vote response
func (vc *VoteCollector) RecordVoteResponse(ctx context.Context, response *VoteResponse) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	// Check for duplicate responses
	if _, exists := vc.voteResponses[response.VoterID]; exists {
		return fmt.Errorf("duplicate vote response from node %s", response.VoterID)
	}

	vc.voteResponses[response.VoterID] = response

	// Convert response to vote
	vote := &Vote{
		NodeID:      response.VoterID,
		Term:        response.Term,
		CandidateID: vc.nodeID,
		Granted:     response.VoteGranted,
		Reason:      fmt.Sprintf("vote_response"),
		Timestamp:   time.Now(),
	}

	return vc.recordVote(ctx, vote)
}

// HasMajority checks if we have a majority of votes
func (vc *VoteCollector) HasMajority() bool {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	return vc.votesGranted >= vc.votesNeeded
}

// HasFailed checks if the election has failed (majority denied)
func (vc *VoteCollector) HasFailed() bool {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	maxPossibleVotes := vc.totalNodes - vc.votesDenied
	return maxPossibleVotes < vc.votesNeeded
}

// IsComplete checks if vote collection is complete
func (vc *VoteCollector) IsComplete() bool {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	return vc.votesReceived >= vc.totalNodes || vc.HasMajority() || vc.HasFailed()
}

// GetResult returns the current vote collection result
func (vc *VoteCollector) GetResult() VoteResult {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	result := VoteResult{
		Term:          vc.term,
		TotalNodes:    vc.totalNodes,
		VotesNeeded:   vc.votesNeeded,
		VotesReceived: vc.votesReceived,
		VotesGranted:  vc.votesGranted,
		VotesDenied:   vc.votesDenied,
		StartTime:     vc.startTime,
		EndTime:       vc.endTime,
	}

	// Determine result
	if vc.HasMajority() {
		result.Success = true
		result.Reason = "majority_achieved"
	} else if vc.HasFailed() {
		result.Success = false
		result.Reason = "majority_impossible"
	} else {
		result.Success = false
		result.Reason = "incomplete"
	}

	if !vc.endTime.IsZero() {
		result.Duration = vc.endTime.Sub(vc.startTime)
	} else {
		result.Duration = time.Since(vc.startTime)
	}

	return result
}

// GetVotes returns all votes
func (vc *VoteCollector) GetVotes() map[string]*Vote {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	votes := make(map[string]*Vote)
	for nodeID, vote := range vc.votes {
		votes[nodeID] = vote
	}

	return votes
}

// GetVoteByNode returns a vote by node ID
func (vc *VoteCollector) GetVoteByNode(nodeID string) (*Vote, bool) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	vote, exists := vc.votes[nodeID]
	return vote, exists
}

// GetVotingNodes returns the list of nodes that have voted
func (vc *VoteCollector) GetVotingNodes() []string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	nodes := make([]string, 0, len(vc.votes))
	for nodeID := range vc.votes {
		nodes = append(nodes, nodeID)
	}

	return nodes
}

// GetMissingVotes returns the list of nodes that haven't voted
func (vc *VoteCollector) GetMissingVotes() []string {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	// This is a simplified implementation
	// In practice, you'd need to track all expected nodes
	allNodes := make([]string, 0, vc.totalNodes)
	for i := 0; i < vc.totalNodes; i++ {
		allNodes = append(allNodes, fmt.Sprintf("node-%d", i))
	}

	missing := make([]string, 0)
	for _, nodeID := range allNodes {
		if _, exists := vc.votes[nodeID]; !exists {
			missing = append(missing, nodeID)
		}
	}

	return missing
}

// EndCollection ends vote collection
func (vc *VoteCollector) EndCollection() {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.endTime = time.Now()

	if vc.logger != nil {
		result := vc.GetResult()
		vc.logger.Info("vote collection ended",
			logger.String("node_id", vc.nodeID),
			logger.Uint64("term", vc.term),
			logger.Bool("success", result.Success),
			logger.String("reason", result.Reason),
			logger.Int("votes_received", vc.votesReceived),
			logger.Int("votes_granted", vc.votesGranted),
			logger.Int("votes_denied", vc.votesDenied),
			logger.Duration("duration", result.Duration),
		)
	}

	if vc.metrics != nil {
		vc.metrics.Counter("forge.consensus.election.vote_collection_ended").Inc()
		vc.metrics.Histogram("forge.consensus.election.vote_collection_duration").Observe(time.Since(vc.startTime).Seconds())
	}
}

// GetStats returns vote collection statistics
func (vc *VoteCollector) GetStats() VoteStats {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	return VoteStats{
		Term:          vc.term,
		TotalNodes:    vc.totalNodes,
		VotesNeeded:   vc.votesNeeded,
		VotesReceived: vc.votesReceived,
		VotesGranted:  vc.votesGranted,
		VotesDenied:   vc.votesDenied,
		StartTime:     vc.startTime,
		EndTime:       vc.endTime,
		Duration:      time.Since(vc.startTime),
		HasMajority:   vc.HasMajority(),
		HasFailed:     vc.HasFailed(),
		IsComplete:    vc.IsComplete(),
	}
}

// VoteResult represents the result of vote collection
type VoteResult struct {
	Term          uint64        `json:"term"`
	Success       bool          `json:"success"`
	Reason        string        `json:"reason"`
	TotalNodes    int           `json:"total_nodes"`
	VotesNeeded   int           `json:"votes_needed"`
	VotesReceived int           `json:"votes_received"`
	VotesGranted  int           `json:"votes_granted"`
	VotesDenied   int           `json:"votes_denied"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	Duration      time.Duration `json:"duration"`
}

// VoteStats contains vote collection statistics
type VoteStats struct {
	Term          uint64        `json:"term"`
	TotalNodes    int           `json:"total_nodes"`
	VotesNeeded   int           `json:"votes_needed"`
	VotesReceived int           `json:"votes_received"`
	VotesGranted  int           `json:"votes_granted"`
	VotesDenied   int           `json:"votes_denied"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	Duration      time.Duration `json:"duration"`
	HasMajority   bool          `json:"has_majority"`
	HasFailed     bool          `json:"has_failed"`
	IsComplete    bool          `json:"is_complete"`
}

// DefaultVoteValidator is a basic vote validator
type DefaultVoteValidator struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewDefaultVoteValidator creates a new default vote validator
func NewDefaultVoteValidator(logger common.Logger, metrics common.Metrics) *DefaultVoteValidator {
	return &DefaultVoteValidator{
		logger:  logger,
		metrics: metrics,
	}
}

// ValidateVote validates a vote
func (dvv *DefaultVoteValidator) ValidateVote(ctx context.Context, vote *Vote) error {
	if vote == nil {
		return fmt.Errorf("vote cannot be nil")
	}

	if vote.NodeID == "" {
		return fmt.Errorf("node ID cannot be empty")
	}

	if vote.Term == 0 {
		return fmt.Errorf("term cannot be zero")
	}

	if vote.CandidateID == "" {
		return fmt.Errorf("candidate ID cannot be empty")
	}

	if vote.Timestamp.IsZero() {
		return fmt.Errorf("timestamp cannot be zero")
	}

	// Check if vote is too old
	if time.Since(vote.Timestamp) > 5*time.Minute {
		return fmt.Errorf("vote is too old")
	}

	// Check if vote is from the future
	if vote.Timestamp.After(time.Now().Add(time.Minute)) {
		return fmt.Errorf("vote is from the future")
	}

	if dvv.metrics != nil {
		dvv.metrics.Counter("forge.consensus.election.votes_validated").Inc()
	}

	return nil
}

// ValidateVoteRequest validates a vote request
func (dvv *DefaultVoteValidator) ValidateVoteRequest(ctx context.Context, request *VoteRequest) error {
	if request == nil {
		return fmt.Errorf("vote request cannot be nil")
	}

	if request.CandidateID == "" {
		return fmt.Errorf("candidate ID cannot be empty")
	}

	if request.Term == 0 {
		return fmt.Errorf("term cannot be zero")
	}

	if dvv.metrics != nil {
		dvv.metrics.Counter("forge.consensus.election.vote_requests_validated").Inc()
	}

	return nil
}

// VerifyVoteSignature verifies vote signature
func (dvv *DefaultVoteValidator) VerifyVoteSignature(ctx context.Context, vote *Vote) error {
	// This is a simplified implementation
	// In practice, you'd implement proper cryptographic signature verification
	if vote.Signature == "" {
		return fmt.Errorf("vote signature is required")
	}

	if dvv.metrics != nil {
		dvv.metrics.Counter("forge.consensus.election.vote_signatures_verified").Inc()
	}

	return nil
}

// MemoryVoteStorage is an in-memory vote storage implementation
type MemoryVoteStorage struct {
	votes map[string]map[uint64]*Vote // nodeID -> term -> vote
	mu    sync.RWMutex
}

// NewMemoryVoteStorage creates a new in-memory vote storage
func NewMemoryVoteStorage() *MemoryVoteStorage {
	return &MemoryVoteStorage{
		votes: make(map[string]map[uint64]*Vote),
	}
}

// StoreVote stores a vote
func (mvs *MemoryVoteStorage) StoreVote(ctx context.Context, vote *Vote) error {
	mvs.mu.Lock()
	defer mvs.mu.Unlock()

	if mvs.votes[vote.NodeID] == nil {
		mvs.votes[vote.NodeID] = make(map[uint64]*Vote)
	}

	mvs.votes[vote.NodeID][vote.Term] = vote
	return nil
}

// GetVote retrieves a vote
func (mvs *MemoryVoteStorage) GetVote(ctx context.Context, nodeID string, term uint64) (*Vote, error) {
	mvs.mu.RLock()
	defer mvs.mu.RUnlock()

	if nodeVotes, exists := mvs.votes[nodeID]; exists {
		if vote, exists := nodeVotes[term]; exists {
			return vote, nil
		}
	}

	return nil, fmt.Errorf("vote not found for node %s, term %d", nodeID, term)
}

// GetVotesForTerm retrieves all votes for a term
func (mvs *MemoryVoteStorage) GetVotesForTerm(ctx context.Context, term uint64) ([]*Vote, error) {
	mvs.mu.RLock()
	defer mvs.mu.RUnlock()

	var votes []*Vote
	for _, nodeVotes := range mvs.votes {
		if vote, exists := nodeVotes[term]; exists {
			votes = append(votes, vote)
		}
	}

	return votes, nil
}

// DeleteVotesForTerm deletes all votes for a term
func (mvs *MemoryVoteStorage) DeleteVotesForTerm(ctx context.Context, term uint64) error {
	mvs.mu.Lock()
	defer mvs.mu.Unlock()

	for nodeID, nodeVotes := range mvs.votes {
		delete(nodeVotes, term)
		if len(nodeVotes) == 0 {
			delete(mvs.votes, nodeID)
		}
	}

	return nil
}

// VoteAnalyzer analyzes vote patterns
type VoteAnalyzer struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewVoteAnalyzer creates a new vote analyzer
func NewVoteAnalyzer(logger common.Logger, metrics common.Metrics) *VoteAnalyzer {
	return &VoteAnalyzer{
		logger:  logger,
		metrics: metrics,
	}
}

// AnalyzeVotes analyzes vote patterns
func (va *VoteAnalyzer) AnalyzeVotes(votes map[string]*Vote) VoteAnalysis {
	analysis := VoteAnalysis{
		TotalVotes:        len(votes),
		GrantedVotes:      0,
		DeniedVotes:       0,
		VotesByReason:     make(map[string]int),
		VotesByNode:       make(map[string]bool),
		VotingPattern:     "unknown",
		ConsensusStrength: 0.0,
	}

	if len(votes) == 0 {
		return analysis
	}

	for nodeID, vote := range votes {
		analysis.VotesByNode[nodeID] = vote.Granted
		analysis.VotesByReason[vote.Reason]++

		if vote.Granted {
			analysis.GrantedVotes++
		} else {
			analysis.DeniedVotes++
		}
	}

	// Calculate consensus strength
	analysis.ConsensusStrength = float64(analysis.GrantedVotes) / float64(analysis.TotalVotes)

	// Determine voting pattern
	if analysis.GrantedVotes == analysis.TotalVotes {
		analysis.VotingPattern = "unanimous_support"
	} else if analysis.DeniedVotes == analysis.TotalVotes {
		analysis.VotingPattern = "unanimous_opposition"
	} else if analysis.GrantedVotes > analysis.DeniedVotes {
		analysis.VotingPattern = "majority_support"
	} else if analysis.DeniedVotes > analysis.GrantedVotes {
		analysis.VotingPattern = "majority_opposition"
	} else {
		analysis.VotingPattern = "split"
	}

	return analysis
}

// VoteAnalysis contains vote analysis results
type VoteAnalysis struct {
	TotalVotes        int             `json:"total_votes"`
	GrantedVotes      int             `json:"granted_votes"`
	DeniedVotes       int             `json:"denied_votes"`
	VotesByReason     map[string]int  `json:"votes_by_reason"`
	VotesByNode       map[string]bool `json:"votes_by_node"`
	VotingPattern     string          `json:"voting_pattern"`
	ConsensusStrength float64         `json:"consensus_strength"`
}

// GetVoteHealth returns vote health information
func (vc *VoteCollector) GetVoteHealth() VoteHealth {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	health := VoteHealth{
		Term:          vc.term,
		TotalNodes:    vc.totalNodes,
		VotesReceived: vc.votesReceived,
		VotesNeeded:   vc.votesNeeded,
		VotesGranted:  vc.votesGranted,
		VotesDenied:   vc.votesDenied,
		ResponseRate:  float64(vc.votesReceived) / float64(vc.totalNodes),
		SuccessRate:   float64(vc.votesGranted) / float64(vc.votesReceived),
	}

	// Determine health status
	if vc.HasMajority() {
		health.Status = "successful"
	} else if vc.HasFailed() {
		health.Status = "failed"
	} else if health.ResponseRate < 0.5 {
		health.Status = "poor_response"
	} else {
		health.Status = "in_progress"
	}

	return health
}

// VoteHealth contains vote health information
type VoteHealth struct {
	Status        string  `json:"status"`
	Term          uint64  `json:"term"`
	TotalNodes    int     `json:"total_nodes"`
	VotesReceived int     `json:"votes_received"`
	VotesNeeded   int     `json:"votes_needed"`
	VotesGranted  int     `json:"votes_granted"`
	VotesDenied   int     `json:"votes_denied"`
	ResponseRate  float64 `json:"response_rate"`
	SuccessRate   float64 `json:"success_rate"`
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
