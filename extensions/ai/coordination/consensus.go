package coordination

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/logger"
)

// ConsensusAlgorithm defines different consensus algorithms.
type ConsensusAlgorithm string

const (
	ConsensusAlgorithmRaft     ConsensusAlgorithm = "raft"     // Raft consensus
	ConsensusAlgorithmPBFT     ConsensusAlgorithm = "pbft"     // Practical Byzantine Fault Tolerance
	ConsensusAlgorithmMajority ConsensusAlgorithm = "majority" // Simple majority voting
	ConsensusAlgorithmWeighted ConsensusAlgorithm = "weighted" // Weighted voting based on agent priority
	ConsensusAlgorithmQuorum   ConsensusAlgorithm = "quorum"   // Quorum-based consensus
)

// ConsensusConfig contains configuration for consensus management.
type ConsensusConfig struct {
	Algorithm               ConsensusAlgorithm `default:"majority"       yaml:"algorithm"`
	Timeout                 time.Duration      `default:"30s"            yaml:"timeout"`
	MinParticipants         int                `default:"3"              yaml:"min_participants"`
	QuorumSize              int                `default:"2"              yaml:"quorum_size"`
	MaxRetries              int                `default:"3"              yaml:"max_retries"`
	RetryDelay              time.Duration      `default:"1s"             yaml:"retry_delay"`
	ByzantineFaultTolerance bool               `default:"false"          yaml:"byzantine_fault_tolerance"`
	WeightingEnabled        bool               `default:"true"           yaml:"weighting_enabled"`
	ConflictResolution      string             `default:"highest_weight" yaml:"conflict_resolution"`
}

// ConsensusParticipant represents a participant in consensus.
type ConsensusParticipant struct {
	ID         string                 `json:"id"`
	Weight     float64                `json:"weight"`
	Vote       interface{}            `json:"vote"`
	Timestamp  time.Time              `json:"timestamp"`
	Signature  string                 `json:"signature,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
	Online     bool                   `json:"online"`
	Reputation float64                `json:"reputation"`
}

// ConsensusProposal represents a proposal for consensus.
type ConsensusProposal struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Content   interface{}            `json:"content"`
	Proposer  string                 `json:"proposer"`
	Timestamp time.Time              `json:"timestamp"`
	Deadline  time.Time              `json:"deadline"`
	Priority  int                    `json:"priority"`
	Metadata  map[string]interface{} `json:"metadata"`
	Status    ProposalStatus         `json:"status"`
	Votes     []Vote                 `json:"votes"`
}

// ProposalStatus represents the status of a consensus proposal.
type ProposalStatus string

const (
	ProposalStatusPending   ProposalStatus = "pending"
	ProposalStatusActive    ProposalStatus = "active"
	ProposalStatusAccepted  ProposalStatus = "accepted"
	ProposalStatusRejected  ProposalStatus = "rejected"
	ProposalStatusFailed    ProposalStatus = "failed"
	ProposalStatusTimeout   ProposalStatus = "timeout"
	ProposalStatusCancelled ProposalStatus = "cancelled"
)

// Vote represents a vote in the consensus process.
type Vote struct {
	ParticipantID string                 `json:"participant_id"`
	Decision      interface{}            `json:"decision"`
	Vote          bool                   `json:"vote"`
	Timestamp     time.Time              `json:"timestamp"`
	Confidence    float64                `json:"confidence"`
	Signature     string                 `json:"signature,omitempty"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// ConsensusResult represents the result of a consensus process.
type ConsensusResult struct {
	ProposalID    string                           `json:"proposal_id"`
	Decision      interface{}                      `json:"decision"`
	Participants  map[string]*ConsensusParticipant `json:"participants"`
	Algorithm     ConsensusAlgorithm               `json:"algorithm"`
	StartTime     time.Time                        `json:"start_time"`
	EndTime       time.Time                        `json:"end_time"`
	Duration      time.Duration                    `json:"duration"`
	Confidence    float64                          `json:"confidence"`
	Unanimity     bool                             `json:"unanimity"`
	Accepted      bool                             `json:"accepted"`
	VoteCount     int                              `json:"vote_count"`
	YesVotes      int                              `json:"yes_votes"`
	NoVotes       int                              `json:"no_votes"`
	Timestamp     time.Time                        `json:"timestamp"`
	QuorumReached bool                             `json:"quorum_reached"`
	VoteBreakdown map[string]int                   `json:"vote_breakdown"`
	Metadata      map[string]interface{}           `json:"metadata"`
}

// ConsensusStats contains statistics about consensus processes.
type ConsensusStats struct {
	TotalProposals      int64                                 `json:"total_proposals"`
	AcceptedProposals   int64                                 `json:"accepted_proposals"`
	RejectedProposals   int64                                 `json:"rejected_proposals"`
	TimedOutProposals   int64                                 `json:"timed_out_proposals"`
	AverageDecisionTime time.Duration                         `json:"average_decision_time"`
	QuorumSuccessRate   float64                               `json:"quorum_success_rate"`
	ParticipantStats    map[string]ParticipantStats           `json:"participant_stats"`
	AlgorithmStats      map[ConsensusAlgorithm]AlgorithmStats `json:"algorithm_stats"`
	LastUpdated         time.Time                             `json:"last_updated"`
}

// ParticipantStats contains statistics for a consensus participant.
type ParticipantStats struct {
	TotalVotes       int64         `json:"total_votes"`
	AgreedVotes      int64         `json:"agreed_votes"`
	DisagreedVotes   int64         `json:"disagreed_votes"`
	AgreementRate    float64       `json:"agreement_rate"`
	ResponseTime     time.Duration `json:"response_time"`
	Reputation       float64       `json:"reputation"`
	ReliabilityScore float64       `json:"reliability_score"`
}

// AlgorithmStats contains statistics for a consensus algorithm.
type AlgorithmStats struct {
	TimesUsed         int64         `json:"times_used"`
	SuccessRate       float64       `json:"success_rate"`
	AverageTime       time.Duration `json:"average_time"`
	QuorumFailures    int64         `json:"quorum_failures"`
	ByzantineFailures int64         `json:"byzantine_failures"`
}

// ConsensusManager manages consensus-based decision making.
type ConsensusManager struct {
	config       ConsensusConfig
	proposals    map[string]*ConsensusProposal
	participants map[string]*ConsensusParticipant
	results      map[string]*ConsensusResult
	stats        ConsensusStats
	logger       logger.Logger
	started      bool
	mu           sync.RWMutex
}

// NewConsensusManager creates a new consensus manager.
func NewConsensusManager(timeout time.Duration, logger logger.Logger) *ConsensusManager {
	return &ConsensusManager{
		config: ConsensusConfig{
			Algorithm:       ConsensusAlgorithmMajority,
			Timeout:         timeout,
			MinParticipants: 3,
			QuorumSize:      2,
			MaxRetries:      3,
			RetryDelay:      time.Second,
		},
		proposals:    make(map[string]*ConsensusProposal),
		participants: make(map[string]*ConsensusParticipant),
		results:      make(map[string]*ConsensusResult),
		stats: ConsensusStats{
			ParticipantStats: make(map[string]ParticipantStats),
			AlgorithmStats:   make(map[ConsensusAlgorithm]AlgorithmStats),
			LastUpdated:      time.Now(),
		},
		logger: logger,
	}
}

// consensusManager is the concrete implementation.
type consensusManager struct {
	config       ConsensusConfig
	proposals    map[string]*ConsensusProposal
	participants map[string]*ConsensusParticipant
	results      map[string]*ConsensusResult
	stats        ConsensusStats
	logger       logger.Logger
	started      bool
	mu           sync.RWMutex
}

// Start starts the consensus manager.
func (cm *consensusManager) Start(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return errors.New("consensus manager already started")
	}

	// Start background processes
	go cm.monitorProposals(ctx)
	go cm.updateStatistics(ctx)

	cm.started = true

	if cm.logger != nil {
		cm.logger.Info("consensus manager started",
			logger.String("algorithm", string(cm.config.Algorithm)),
			logger.Duration("timeout", cm.config.Timeout),
			logger.Int("min_participants", cm.config.MinParticipants),
		)
	}

	return nil
}

// Stop stops the consensus manager.
func (cm *consensusManager) Stop(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return errors.New("consensus manager not started")
	}

	cm.started = false

	if cm.logger != nil {
		cm.logger.Info("consensus manager stopped")
	}

	return nil
}

// RegisterParticipant registers a participant for consensus.
func (cm *consensusManager) RegisterParticipant(id string, weight float64) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.participants[id]; exists {
		return fmt.Errorf("participant %s already registered", id)
	}

	participant := &ConsensusParticipant{
		ID:         id,
		Weight:     weight,
		Timestamp:  time.Now(),
		Metadata:   make(map[string]interface{}),
		Online:     true,
		Reputation: 1.0, // Start with perfect reputation
	}

	cm.participants[id] = participant

	if cm.logger != nil {
		cm.logger.Info("consensus participant registered",
			logger.String("participant_id", id),
			logger.Float64("weight", weight),
		)
	}

	return nil
}

// UnregisterParticipant removes a participant from consensus.
func (cm *consensusManager) UnregisterParticipant(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.participants[id]; !exists {
		return fmt.Errorf("participant %s not found", id)
	}

	delete(cm.participants, id)

	if cm.logger != nil {
		cm.logger.Info("consensus participant unregistered",
			logger.String("participant_id", id),
		)
	}

	return nil
}

// ProposeDecision proposes a decision for consensus.
func (cm *consensusManager) ProposeDecision(proposal *ConsensusProposal) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return errors.New("consensus manager not started")
	}

	proposal.ID = cm.generateProposalID()
	proposal.Timestamp = time.Now()
	proposal.Deadline = time.Now().Add(cm.config.Timeout)
	proposal.Status = ProposalStatusActive

	cm.proposals[proposal.ID] = proposal

	if cm.logger != nil {
		cm.logger.Info("consensus proposal submitted",
			logger.String("proposal_id", proposal.ID),
			logger.String("type", proposal.Type),
			logger.String("proposer", proposal.Proposer),
		)
	}

	cm.stats.TotalProposals++

	return nil
}

// ReachConsensus attempts to reach consensus on a decision.
func (cm *consensusManager) ReachConsensus(ctx context.Context, decision *CoordinationDecision) error {
	// Create proposal from decision
	proposal := &ConsensusProposal{
		Type:     decision.Type,
		Content:  decision,
		Proposer: "coordinator",
		Priority: 1,
		Metadata: decision.Metadata,
	}

	// Submit proposal
	if err := cm.ProposeDecision(proposal); err != nil {
		return err
	}

	// Wait for consensus
	return cm.waitForConsensus(ctx, proposal.ID, decision)
}

// VoteOnProposal allows a participant to vote on a proposal.
func (cm *consensusManager) VoteOnProposal(proposalID, participantID string, vote interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	proposal, exists := cm.proposals[proposalID]
	if !exists {
		return fmt.Errorf("proposal %s not found", proposalID)
	}

	participant, exists := cm.participants[participantID]
	if !exists {
		return fmt.Errorf("participant %s not found", participantID)
	}

	if proposal.Status != ProposalStatusActive {
		return fmt.Errorf("proposal %s is not active", proposalID)
	}

	if time.Now().After(proposal.Deadline) {
		proposal.Status = ProposalStatusTimeout

		return fmt.Errorf("proposal %s has timed out", proposalID)
	}

	// Record vote
	participant.Vote = vote
	participant.Timestamp = time.Now()

	if cm.logger != nil {
		cm.logger.Debug("vote recorded",
			logger.String("proposal_id", proposalID),
			logger.String("participant_id", participantID),
			logger.Any("vote", vote),
		)
	}

	// Check if consensus is reached
	if cm.checkConsensus(proposal) {
		cm.finalizeConsensus(proposal)
	}

	return nil
}

// GetConsensusResult returns the result of a consensus process.
func (cm *consensusManager) GetConsensusResult(proposalID string) (*ConsensusResult, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result, exists := cm.results[proposalID]
	if !exists {
		return nil, fmt.Errorf("consensus result for proposal %s not found", proposalID)
	}

	return result, nil
}

// GetStats returns consensus statistics.
func (cm *consensusManager) GetStats() ConsensusStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.stats
}

// Internal methods

func (cm *consensusManager) monitorProposals(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.checkExpiredProposals()
		}
	}
}

func (cm *consensusManager) updateStatistics(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.calculateStatistics()
		}
	}
}

func (cm *consensusManager) checkExpiredProposals() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	now := time.Now()
	for _, proposal := range cm.proposals {
		if proposal.Status == ProposalStatusActive && now.After(proposal.Deadline) {
			proposal.Status = ProposalStatusTimeout

			// Create timeout result
			result := &ConsensusResult{
				ProposalID:    proposal.ID,
				Decision:      "timeout",
				Algorithm:     cm.config.Algorithm,
				StartTime:     proposal.Timestamp,
				EndTime:       now,
				Duration:      now.Sub(proposal.Timestamp),
				Confidence:    0.0,
				QuorumReached: false,
			}

			cm.results[proposal.ID] = result
			cm.stats.TimedOutProposals++

			if cm.logger != nil {
				cm.logger.Warn("proposal timed out",
					logger.String("proposal_id", proposal.ID),
					logger.Duration("duration", result.Duration),
				)
			}
		}
	}
}

func (cm *consensusManager) waitForConsensus(ctx context.Context, proposalID string, decision *CoordinationDecision) error {
	// Poll for consensus result
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(cm.config.Timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("consensus timeout for proposal %s", proposalID)
		case <-ticker.C:
			if result, err := cm.GetConsensusResult(proposalID); err == nil {
				// Update decision with consensus result
				decision.Result = result.Decision
				decision.Confidence = result.Confidence
				decision.Votes = make(map[string]interface{})

				for id, participant := range result.Participants {
					decision.Votes[id] = participant.Vote
				}

				return nil
			}
		}
	}
}

func (cm *consensusManager) checkConsensus(proposal *ConsensusProposal) bool {
	switch cm.config.Algorithm {
	case ConsensusAlgorithmMajority:
		return cm.checkMajorityConsensus(proposal)
	case ConsensusAlgorithmWeighted:
		return cm.checkWeightedConsensus(proposal)
	case ConsensusAlgorithmQuorum:
		return cm.checkQuorumConsensus(proposal)
	case ConsensusAlgorithmRaft:
		return cm.checkRaftConsensus(proposal)
	case ConsensusAlgorithmPBFT:
		return cm.checkPBFTConsensus(proposal)
	default:
		return cm.checkMajorityConsensus(proposal)
	}
}

func (cm *consensusManager) checkMajorityConsensus(proposal *ConsensusProposal) bool {
	votesNeeded := (len(cm.participants) / 2) + 1
	voteCounts := make(map[string]int)
	totalVotes := 0

	for _, participant := range cm.participants {
		if participant.Vote != nil && participant.Online {
			voteStr := fmt.Sprintf("%v", participant.Vote)
			voteCounts[voteStr]++
			totalVotes++
		}
	}

	// Check if any vote has majority
	for _, count := range voteCounts {
		if count >= votesNeeded {
			return true
		}
	}

	return false
}

func (cm *consensusManager) checkWeightedConsensus(proposal *ConsensusProposal) bool {
	if !cm.config.WeightingEnabled {
		return cm.checkMajorityConsensus(proposal)
	}

	totalWeight := 0.0
	voteWeights := make(map[string]float64)

	// Calculate total weight
	for _, participant := range cm.participants {
		if participant.Online {
			totalWeight += participant.Weight
		}
	}

	// Calculate vote weights
	for _, participant := range cm.participants {
		if participant.Vote != nil && participant.Online {
			voteStr := fmt.Sprintf("%v", participant.Vote)
			voteWeights[voteStr] += participant.Weight
		}
	}

	// Check if any vote has majority weight
	majorityWeight := totalWeight / 2
	for _, weight := range voteWeights {
		if weight > majorityWeight {
			return true
		}
	}

	return false
}

func (cm *consensusManager) checkQuorumConsensus(proposal *ConsensusProposal) bool {
	votesReceived := 0

	for _, participant := range cm.participants {
		if participant.Vote != nil && participant.Online {
			votesReceived++
		}
	}

	return votesReceived >= cm.config.QuorumSize
}

func (cm *consensusManager) checkRaftConsensus(proposal *ConsensusProposal) bool {
	// Simplified Raft-like consensus - in practice, would need full Raft implementation
	return cm.checkMajorityConsensus(proposal)
}

func (cm *consensusManager) checkPBFTConsensus(proposal *ConsensusProposal) bool {
	// Simplified PBFT-like consensus - in practice, would need full PBFT implementation
	if !cm.config.ByzantineFaultTolerance {
		return cm.checkMajorityConsensus(proposal)
	}

	// PBFT requires (3f + 1) nodes to tolerate f Byzantine failures
	minNodes := len(cm.participants)
	f := (minNodes - 1) / 3
	votesNeeded := 2*f + 1

	agreementVotes := 0

	for _, participant := range cm.participants {
		if participant.Vote != nil && participant.Online && participant.Reputation > 0.5 {
			agreementVotes++
		}
	}

	return agreementVotes >= votesNeeded
}

func (cm *consensusManager) finalizeConsensus(proposal *ConsensusProposal) {
	result := cm.calculateConsensusResult(proposal)
	cm.results[proposal.ID] = result

	if result.QuorumReached {
		proposal.Status = ProposalStatusAccepted
		cm.stats.AcceptedProposals++
	} else {
		proposal.Status = ProposalStatusRejected
		cm.stats.RejectedProposals++
	}

	// Update participant statistics
	for id, participant := range result.Participants {
		if stats, exists := cm.stats.ParticipantStats[id]; exists {
			stats.TotalVotes++
			if cm.isAgreementVote(participant.Vote, result.Decision) {
				stats.AgreedVotes++
			} else {
				stats.DisagreedVotes++
			}

			stats.AgreementRate = float64(stats.AgreedVotes) / float64(stats.TotalVotes)
			cm.stats.ParticipantStats[id] = stats
		} else {
			cm.stats.ParticipantStats[id] = ParticipantStats{
				TotalVotes:    1,
				AgreedVotes:   1,
				AgreementRate: 1.0,
				Reputation:    1.0,
			}
		}
	}

	if cm.logger != nil {
		cm.logger.Info("consensus finalized",
			logger.String("proposal_id", proposal.ID),
			logger.String("status", string(proposal.Status)),
			logger.Any("decision", result.Decision),
			logger.Float64("confidence", result.Confidence),
		)
	}
}

func (cm *consensusManager) calculateConsensusResult(proposal *ConsensusProposal) *ConsensusResult {
	startTime := proposal.Timestamp
	endTime := time.Now()

	// Collect participant votes
	participants := make(map[string]*ConsensusParticipant)
	voteBreakdown := make(map[string]int)

	for id, participant := range cm.participants {
		if participant.Vote != nil {
			participantCopy := *participant // Copy participant
			participants[id] = &participantCopy

			voteStr := fmt.Sprintf("%v", participant.Vote)
			voteBreakdown[voteStr]++
		}
	}

	// Determine winning decision
	var (
		decision      interface{}
		confidence    float64
		quorumReached bool
		unanimity     bool
	)

	switch cm.config.Algorithm {
	case ConsensusAlgorithmWeighted:
		decision, confidence = cm.calculateWeightedDecision(participants)
	default:
		decision, confidence = cm.calculateMajorityDecision(participants)
	}

	quorumReached = len(participants) >= cm.config.QuorumSize
	unanimity = len(voteBreakdown) <= 1

	return &ConsensusResult{
		ProposalID:    proposal.ID,
		Decision:      decision,
		Participants:  participants,
		Algorithm:     cm.config.Algorithm,
		StartTime:     startTime,
		EndTime:       endTime,
		Duration:      endTime.Sub(startTime),
		Confidence:    confidence,
		Unanimity:     unanimity,
		QuorumReached: quorumReached,
		VoteBreakdown: voteBreakdown,
		Metadata:      make(map[string]interface{}),
	}
}

func (cm *consensusManager) calculateMajorityDecision(participants map[string]*ConsensusParticipant) (interface{}, float64) {
	voteCounts := make(map[string]int)

	for _, participant := range participants {
		voteStr := fmt.Sprintf("%v", participant.Vote)
		voteCounts[voteStr]++
	}

	// Find majority vote
	maxVotes := 0

	var majorityVote string

	for vote, count := range voteCounts {
		if count > maxVotes {
			maxVotes = count
			majorityVote = vote
		}
	}

	confidence := float64(maxVotes) / float64(len(participants))

	return majorityVote, confidence
}

func (cm *consensusManager) calculateWeightedDecision(participants map[string]*ConsensusParticipant) (interface{}, float64) {
	voteWeights := make(map[string]float64)
	totalWeight := 0.0

	for _, participant := range participants {
		voteStr := fmt.Sprintf("%v", participant.Vote)
		voteWeights[voteStr] += participant.Weight
		totalWeight += participant.Weight
	}

	// Find weighted majority
	var majorityVote string

	maxWeight := 0.0
	for vote, weight := range voteWeights {
		if weight > maxWeight {
			maxWeight = weight
			majorityVote = vote
		}
	}

	confidence := maxWeight / totalWeight

	return majorityVote, confidence
}

func (cm *consensusManager) isAgreementVote(vote, decision interface{}) bool {
	return fmt.Sprintf("%v", vote) == fmt.Sprintf("%v", decision)
}

func (cm *consensusManager) calculateStatistics() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Calculate average decision time
	var totalTime time.Duration

	completedProposals := int64(0)

	for _, result := range cm.results {
		totalTime += result.Duration
		completedProposals++
	}

	if completedProposals > 0 {
		cm.stats.AverageDecisionTime = totalTime / time.Duration(completedProposals)
	}

	// Calculate quorum success rate
	quorumSuccesses := int64(0)

	for _, result := range cm.results {
		if result.QuorumReached {
			quorumSuccesses++
		}
	}

	if cm.stats.TotalProposals > 0 {
		cm.stats.QuorumSuccessRate = float64(quorumSuccesses) / float64(cm.stats.TotalProposals)
	}

	cm.stats.LastUpdated = time.Now()
}

func (cm *consensusManager) generateProposalID() string {
	return fmt.Sprintf("proposal-%d", time.Now().UnixNano())
}

// UpdateParticipantReputation updates a participant's reputation.
func (cm *consensusManager) UpdateParticipantReputation(participantID string, delta float64) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	participant, exists := cm.participants[participantID]
	if !exists {
		return fmt.Errorf("participant %s not found", participantID)
	}

	newReputation := participant.Reputation + delta
	if newReputation < 0 {
		newReputation = 0
	} else if newReputation > 1 {
		newReputation = 1
	}

	participant.Reputation = newReputation

	if cm.logger != nil {
		cm.logger.Debug("participant reputation updated",
			logger.String("participant_id", participantID),
			logger.Float64("old_reputation", participant.Reputation-delta),
			logger.Float64("new_reputation", newReputation),
		)
	}

	return nil
}

// SetParticipantOnlineStatus sets the online status of a participant.
func (cm *consensusManager) SetParticipantOnlineStatus(participantID string, online bool) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	participant, exists := cm.participants[participantID]
	if !exists {
		return fmt.Errorf("participant %s not found", participantID)
	}

	participant.Online = online

	if cm.logger != nil {
		cm.logger.Debug("participant status updated",
			logger.String("participant_id", participantID),
			logger.Bool("online", online),
		)
	}

	return nil
}

// Start starts the consensus manager.
func (cm *ConsensusManager) Start(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return errors.New("consensus manager already started")
	}

	// Start background processes
	go cm.healthMonitor(ctx)
	go cm.statisticsCollector(ctx)

	cm.started = true

	if cm.logger != nil {
		cm.logger.Info("consensus manager started",
			logger.String("algorithm", string(cm.config.Algorithm)),
			logger.Duration("timeout", cm.config.Timeout),
			logger.Int("min_participants", cm.config.MinParticipants),
		)
	}

	return nil
}

// Stop stops the consensus manager.
func (cm *ConsensusManager) Stop(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return errors.New("consensus manager not started")
	}

	// Cancel all pending proposals
	for _, proposal := range cm.proposals {
		if proposal.Status == ProposalStatusPending {
			proposal.Status = ProposalStatusCancelled
		}
	}

	cm.started = false

	if cm.logger != nil {
		cm.logger.Info("consensus manager stopped")
	}

	return nil
}

// ReachConsensus attempts to reach consensus on a decision.
func (cm *ConsensusManager) ReachConsensus(ctx context.Context, decision *CoordinationDecision) error {
	if decision == nil {
		return errors.New("decision cannot be nil")
	}

	// Create a new proposal
	proposal := &ConsensusProposal{
		ID:        generateProposalID(),
		Type:      "decision",
		Content:   decision,
		Proposer:  "system",
		Timestamp: time.Now(),
		Status:    ProposalStatusPending,
		Metadata:  make(map[string]interface{}),
	}

	// Add proposal to tracking
	cm.mu.Lock()
	cm.proposals[proposal.ID] = proposal
	cm.mu.Unlock()

	// Start consensus process
	return cm.processConsensus(ctx, proposal)
}

// processConsensus processes a consensus proposal.
func (cm *ConsensusManager) processConsensus(ctx context.Context, proposal *ConsensusProposal) error {
	// Get active participants
	cm.mu.RLock()

	participants := make([]string, 0)
	for participantID, participant := range cm.participants {
		if participant.Online {
			participants = append(participants, participantID)
		}
	}

	cm.mu.RUnlock()

	// Check if we have enough participants
	if len(participants) < cm.config.MinParticipants {
		proposal.Status = ProposalStatusFailed

		return fmt.Errorf("insufficient participants for consensus: %d < %d", len(participants), cm.config.MinParticipants)
	}

	// Add participants to proposal metadata
	proposal.Metadata["participants"] = participants

	// Start voting process
	ctx, cancel := context.WithTimeout(ctx, cm.config.Timeout)
	defer cancel()

	// Send decision to all participants
	for _, participantID := range participants {
		// In a real implementation, this would send the decision to the participant
		// For now, we'll simulate the voting process
		vote := &Vote{
			ParticipantID: participantID,
			Decision:      proposal.Content,
			Vote:          true, // Simplified - always vote yes
			Timestamp:     time.Now(),
			Confidence:    1.0,
		}
		// Store votes in proposal
		proposal.Votes = append(proposal.Votes, *vote)
	}

	// Count votes
	yesVotes := 0
	totalVotes := len(proposal.Votes)

	for _, vote := range proposal.Votes {
		if vote.Vote {
			yesVotes++
		}
	}

	// Determine consensus result
	if yesVotes >= cm.config.QuorumSize {
		proposal.Status = ProposalStatusAccepted

		// Create consensus result
		result := &ConsensusResult{
			ProposalID:    proposal.ID,
			Decision:      proposal.Content,
			Accepted:      true,
			VoteCount:     totalVotes,
			YesVotes:      yesVotes,
			NoVotes:       totalVotes - yesVotes,
			Timestamp:     time.Now(),
			QuorumReached: true,
			VoteBreakdown: make(map[string]int),
			Metadata:      make(map[string]interface{}),
		}

		cm.mu.Lock()
		cm.results[proposal.ID] = result
		cm.mu.Unlock()

		return nil
	} else {
		proposal.Status = ProposalStatusRejected

		return fmt.Errorf("consensus not reached: %d/%d votes", yesVotes, totalVotes)
	}
}

// generateProposalID generates a unique proposal ID.
func generateProposalID() string {
	return fmt.Sprintf("proposal-%d", time.Now().UnixNano())
}

// healthMonitor monitors consensus health.
func (cm *ConsensusManager) healthMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.checkHealth()
		}
	}
}

// statisticsCollector collects consensus statistics.
func (cm *ConsensusManager) statisticsCollector(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.updateStatistics()
		}
	}
}

// checkHealth checks consensus health.
func (cm *ConsensusManager) checkHealth() {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check participant health
	for participantID, participant := range cm.participants {
		if !participant.Online {
			// Handle offline participant
			if cm.logger != nil {
				cm.logger.Warn("participant offline", logger.String("participant_id", participantID))
			}
		}
	}
}

// updateStatistics updates consensus statistics.
func (cm *ConsensusManager) updateStatistics() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.stats.LastUpdated = time.Now()
}
