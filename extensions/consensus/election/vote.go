package election

import (
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// VoteManager manages vote collection and tracking.
type VoteManager struct {
	nodeID string
	logger forge.Logger

	// Vote tracking
	currentTerm uint64
	votedFor    string
	votedAt     time.Time
	voteHistory []VoteRecord
	historyMu   sync.RWMutex

	// Vote responses
	receivedVotes map[uint64]map[string]*VoteResponse
	votesMu       sync.RWMutex

	// Configuration
	maxHistorySize int
}

// VoteRecord represents a historical vote record.
type VoteRecord struct {
	Term        uint64
	CandidateID string
	Granted     bool
	Timestamp   time.Time
	Reason      string
}

// VoteResponse represents a vote response with metadata.
type VoteResponse struct {
	NodeID    string
	Term      uint64
	Granted   bool
	Timestamp time.Time
	Latency   time.Duration
}

// VoteManagerConfig contains vote manager configuration.
type VoteManagerConfig struct {
	NodeID         string
	MaxHistorySize int
}

// NewVoteManager creates a new vote manager.
func NewVoteManager(config VoteManagerConfig, logger forge.Logger) *VoteManager {
	if config.MaxHistorySize == 0 {
		config.MaxHistorySize = 100
	}

	return &VoteManager{
		nodeID:         config.NodeID,
		logger:         logger,
		receivedVotes:  make(map[uint64]map[string]*VoteResponse),
		voteHistory:    make([]VoteRecord, 0, config.MaxHistorySize),
		maxHistorySize: config.MaxHistorySize,
	}
}

// CastVote casts a vote for a candidate.
func (vm *VoteManager) CastVote(term uint64, candidateID string, granted bool, reason string) error {
	vm.historyMu.Lock()
	defer vm.historyMu.Unlock()

	// Check if already voted in this term
	if term == vm.currentTerm && vm.votedFor != "" && vm.votedFor != candidateID {
		return fmt.Errorf("already voted for %s in term %d", vm.votedFor, term)
	}

	// Update current vote
	if term > vm.currentTerm {
		vm.currentTerm = term
		vm.votedFor = ""
	}

	if granted {
		vm.votedFor = candidateID
		vm.votedAt = time.Now()
	}

	// Record in history
	record := VoteRecord{
		Term:        term,
		CandidateID: candidateID,
		Granted:     granted,
		Timestamp:   time.Now(),
		Reason:      reason,
	}

	vm.voteHistory = append(vm.voteHistory, record)

	// Trim history if needed
	if len(vm.voteHistory) > vm.maxHistorySize {
		vm.voteHistory = vm.voteHistory[len(vm.voteHistory)-vm.maxHistorySize:]
	}

	vm.logger.Info("vote cast",
		forge.F("term", term),
		forge.F("candidate", candidateID),
		forge.F("granted", granted),
		forge.F("reason", reason),
	)

	return nil
}

// RecordVoteResponse records a vote response from a peer.
func (vm *VoteManager) RecordVoteResponse(response VoteResponse) {
	vm.votesMu.Lock()
	defer vm.votesMu.Unlock()

	if _, exists := vm.receivedVotes[response.Term]; !exists {
		vm.receivedVotes[response.Term] = make(map[string]*VoteResponse)
	}

	vm.receivedVotes[response.Term][response.NodeID] = &response

	vm.logger.Debug("vote response recorded",
		forge.F("term", response.Term),
		forge.F("node", response.NodeID),
		forge.F("granted", response.Granted),
		forge.F("latency_ms", response.Latency.Milliseconds()),
	)
}

// GetVoteCount returns vote counts for a term.
func (vm *VoteManager) GetVoteCount(term uint64) (granted, denied, total int) {
	vm.votesMu.RLock()
	defer vm.votesMu.RUnlock()

	votes, exists := vm.receivedVotes[term]
	if !exists {
		return 0, 0, 0
	}

	total = len(votes)
	for _, vote := range votes {
		if vote.Granted {
			granted++
		} else {
			denied++
		}
	}

	return granted, denied, total
}

// GetVotesForTerm returns all votes for a specific term.
func (vm *VoteManager) GetVotesForTerm(term uint64) []*VoteResponse {
	vm.votesMu.RLock()
	defer vm.votesMu.RUnlock()

	votes, exists := vm.receivedVotes[term]
	if !exists {
		return nil
	}

	result := make([]*VoteResponse, 0, len(votes))
	for _, vote := range votes {
		result = append(result, vote)
	}

	return result
}

// HasVotedInTerm checks if we voted in a specific term.
func (vm *VoteManager) HasVotedInTerm(term uint64) (bool, string) {
	vm.historyMu.RLock()
	defer vm.historyMu.RUnlock()

	if term == vm.currentTerm && vm.votedFor != "" {
		return true, vm.votedFor
	}

	return false, ""
}

// GetCurrentVote returns current vote information.
func (vm *VoteManager) GetCurrentVote() (term uint64, votedFor string, votedAt time.Time) {
	vm.historyMu.RLock()
	defer vm.historyMu.RUnlock()

	return vm.currentTerm, vm.votedFor, vm.votedAt
}

// GetVoteHistory returns vote history.
func (vm *VoteManager) GetVoteHistory(limit int) []VoteRecord {
	vm.historyMu.RLock()
	defer vm.historyMu.RUnlock()

	if limit <= 0 || limit > len(vm.voteHistory) {
		limit = len(vm.voteHistory)
	}

	// Return most recent votes
	start := len(vm.voteHistory) - limit
	result := make([]VoteRecord, limit)
	copy(result, vm.voteHistory[start:])

	return result
}

// ClearOldVotes clears vote records for old terms.
func (vm *VoteManager) ClearOldVotes(keepTerms int) {
	vm.votesMu.Lock()
	defer vm.votesMu.Unlock()

	vm.historyMu.RLock()
	currentTerm := vm.currentTerm
	vm.historyMu.RUnlock()

	minTerm := uint64(0)
	if currentTerm > uint64(keepTerms) {
		minTerm = currentTerm - uint64(keepTerms)
	}

	for term := range vm.receivedVotes {
		if term < minTerm {
			delete(vm.receivedVotes, term)
		}
	}

	vm.logger.Debug("cleared old votes",
		forge.F("min_term", minTerm),
	)
}

// GetVoteStatistics returns vote statistics.
func (vm *VoteManager) GetVoteStatistics() VoteStatistics {
	vm.historyMu.RLock()
	totalVotes := len(vm.voteHistory)
	grantedCount := 0
	deniedCount := 0

	for _, record := range vm.voteHistory {
		if record.Granted {
			grantedCount++
		} else {
			deniedCount++
		}
	}

	vm.historyMu.RUnlock()

	vm.votesMu.RLock()
	termsWithVotes := len(vm.receivedVotes)
	vm.votesMu.RUnlock()

	return VoteStatistics{
		TotalVotesCast: totalVotes,
		VotesGranted:   grantedCount,
		VotesDenied:    deniedCount,
		TermsWithVotes: termsWithVotes,
		GrantRate:      float64(grantedCount) / float64(totalVotes),
	}
}

// VoteStatistics contains vote statistics.
type VoteStatistics struct {
	TotalVotesCast int
	VotesGranted   int
	VotesDenied    int
	TermsWithVotes int
	GrantRate      float64
}

// ValidateVoteRequest validates a vote request.
func (vm *VoteManager) ValidateVoteRequest(req internal.RequestVoteRequest) (bool, string) {
	vm.historyMu.RLock()
	defer vm.historyMu.RUnlock()

	// Check term
	if req.Term < vm.currentTerm {
		return false, fmt.Sprintf("stale term (req: %d, current: %d)", req.Term, vm.currentTerm)
	}

	// Check if already voted
	if req.Term == vm.currentTerm && vm.votedFor != "" && vm.votedFor != req.CandidateID {
		return false, fmt.Sprintf("already voted for %s in term %d", vm.votedFor, vm.currentTerm)
	}

	// TODO: Check if candidate's log is up-to-date
	// This would compare req.LastLogIndex and req.LastLogTerm with local log

	return true, "validation passed"
}

// GetAverageVoteLatency returns average vote response latency.
func (vm *VoteManager) GetAverageVoteLatency(term uint64) time.Duration {
	vm.votesMu.RLock()
	defer vm.votesMu.RUnlock()

	votes, exists := vm.receivedVotes[term]
	if !exists || len(votes) == 0 {
		return 0
	}

	var totalLatency time.Duration
	for _, vote := range votes {
		totalLatency += vote.Latency
	}

	return totalLatency / time.Duration(len(votes))
}

// GetVoteResponseRate returns the vote response rate for a term.
func (vm *VoteManager) GetVoteResponseRate(term uint64, totalPeers int) float64 {
	vm.votesMu.RLock()
	defer vm.votesMu.RUnlock()

	votes, exists := vm.receivedVotes[term]
	if !exists || totalPeers == 0 {
		return 0.0
	}

	return float64(len(votes)) / float64(totalPeers) * 100.0
}

// GetSlowVoters returns nodes that took longer than threshold to respond.
func (vm *VoteManager) GetSlowVoters(term uint64, threshold time.Duration) []string {
	vm.votesMu.RLock()
	defer vm.votesMu.RUnlock()

	votes, exists := vm.receivedVotes[term]
	if !exists {
		return nil
	}

	var slowVoters []string

	for _, vote := range votes {
		if vote.Latency > threshold {
			slowVoters = append(slowVoters, vote.NodeID)
		}
	}

	return slowVoters
}

// ResetForNewTerm resets vote state for a new term.
func (vm *VoteManager) ResetForNewTerm(term uint64) {
	vm.historyMu.Lock()
	defer vm.historyMu.Unlock()

	if term > vm.currentTerm {
		vm.currentTerm = term
		vm.votedFor = ""
		vm.votedAt = time.Time{}

		vm.logger.Info("reset for new term",
			forge.F("term", term),
		)
	}
}

// ExportVoteData exports vote data for analysis.
func (vm *VoteManager) ExportVoteData() map[string]any {
	vm.historyMu.RLock()
	history := make([]VoteRecord, len(vm.voteHistory))
	copy(history, vm.voteHistory)
	currentTerm := vm.currentTerm
	votedFor := vm.votedFor
	vm.historyMu.RUnlock()

	vm.votesMu.RLock()
	receivedCount := len(vm.receivedVotes)
	vm.votesMu.RUnlock()

	stats := vm.GetVoteStatistics()

	return map[string]any{
		"current_term":   currentTerm,
		"voted_for":      votedFor,
		"vote_history":   history,
		"received_count": receivedCount,
		"statistics":     stats,
	}
}
