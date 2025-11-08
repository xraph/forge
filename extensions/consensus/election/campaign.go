package election

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// CampaignManager manages election campaigns with strategies.
type CampaignManager struct {
	nodeID string
	logger forge.Logger

	// Campaign state
	activeCampaign *Campaign
	campaignMu     sync.RWMutex

	// Campaign history
	campaigns      []CampaignRecord
	historyMu      sync.RWMutex
	maxHistorySize int

	// Configuration
	strategy       CampaignStrategy
	priorityPeers  []string
	parallelVotes  bool
	earlyAbort     bool
	preVoteEnabled bool
}

// Campaign represents an active election campaign.
type Campaign struct {
	CampaignID    string
	Term          uint64
	StartTime     time.Time
	EndTime       time.Time
	Status        CampaignStatus
	Strategy      CampaignStrategy
	TargetPeers   []string
	VoteResponses map[string]*VoteResponse
	VotesNeeded   int
	VotesGranted  int
	VotesDenied   int
	PreVotePhase  bool
	mu            sync.RWMutex
}

// CampaignRecord represents a historical campaign record.
type CampaignRecord struct {
	CampaignID   string
	Term         uint64
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	Status       CampaignStatus
	VotesGranted int
	VotesDenied  int
	TotalPeers   int
}

// CampaignStatus represents campaign status.
type CampaignStatus int

const (
	// CampaignStatusActive campaign is active.
	CampaignStatusActive CampaignStatus = iota
	// CampaignStatusWon campaign was won.
	CampaignStatusWon
	// CampaignStatusLost campaign was lost.
	CampaignStatusLost
	// CampaignStatusAborted campaign was aborted.
	CampaignStatusAborted
	// CampaignStatusTimeout campaign timed out.
	CampaignStatusTimeout
)

// CampaignStrategy represents campaign strategy.
type CampaignStrategy int

const (
	// CampaignStrategySequential votes sequentially.
	CampaignStrategySequential CampaignStrategy = iota
	// CampaignStrategyParallel votes in parallel.
	CampaignStrategyParallel
	// CampaignStrategyPriority votes priority peers first.
	CampaignStrategyPriority
	// CampaignStrategyAdaptive adapts based on responses.
	CampaignStrategyAdaptive
)

// CampaignManagerConfig contains campaign manager configuration.
type CampaignManagerConfig struct {
	NodeID         string
	Strategy       CampaignStrategy
	PriorityPeers  []string
	ParallelVotes  bool
	EarlyAbort     bool
	PreVoteEnabled bool
	MaxHistorySize int
}

// NewCampaignManager creates a new campaign manager.
func NewCampaignManager(config CampaignManagerConfig, logger forge.Logger) *CampaignManager {
	if config.MaxHistorySize == 0 {
		config.MaxHistorySize = 50
	}

	return &CampaignManager{
		nodeID:         config.NodeID,
		logger:         logger,
		strategy:       config.Strategy,
		priorityPeers:  config.PriorityPeers,
		parallelVotes:  config.ParallelVotes,
		earlyAbort:     config.EarlyAbort,
		preVoteEnabled: config.PreVoteEnabled,
		campaigns:      make([]CampaignRecord, 0, config.MaxHistorySize),
		maxHistorySize: config.MaxHistorySize,
	}
}

// StartCampaign starts a new election campaign.
func (cm *CampaignManager) StartCampaign(term uint64, peers []string) (*Campaign, error) {
	cm.campaignMu.Lock()
	defer cm.campaignMu.Unlock()

	// Check if there's an active campaign
	if cm.activeCampaign != nil && cm.activeCampaign.Status == CampaignStatusActive {
		return nil, fmt.Errorf("campaign already active for term %d", cm.activeCampaign.Term)
	}

	// Create new campaign
	campaign := &Campaign{
		CampaignID:    fmt.Sprintf("%s-term-%d-%d", cm.nodeID, term, time.Now().UnixNano()),
		Term:          term,
		StartTime:     time.Now(),
		Status:        CampaignStatusActive,
		Strategy:      cm.strategy,
		TargetPeers:   peers,
		VoteResponses: make(map[string]*VoteResponse),
		VotesNeeded:   (len(peers) / 2) + 1,
		VotesGranted:  1, // Vote for self
		PreVotePhase:  cm.preVoteEnabled,
	}

	cm.activeCampaign = campaign

	cm.logger.Info("campaign started",
		forge.F("campaign_id", campaign.CampaignID),
		forge.F("term", term),
		forge.F("peers", len(peers)),
		forge.F("votes_needed", campaign.VotesNeeded),
		forge.F("strategy", cm.strategy),
		forge.F("pre_vote", cm.preVoteEnabled),
	)

	return campaign, nil
}

// RecordVoteResponse records a vote response for the active campaign.
func (cm *CampaignManager) RecordVoteResponse(response *VoteResponse) error {
	cm.campaignMu.RLock()
	campaign := cm.activeCampaign
	cm.campaignMu.RUnlock()

	if campaign == nil {
		return errors.New("no active campaign")
	}

	campaign.mu.Lock()
	defer campaign.mu.Unlock()

	// Check if response is for current campaign
	if response.Term != campaign.Term {
		return errors.New("vote response for different term")
	}

	// Record response
	campaign.VoteResponses[response.NodeID] = response

	if response.Granted {
		campaign.VotesGranted++
	} else {
		campaign.VotesDenied++
	}

	cm.logger.Debug("vote response recorded",
		forge.F("campaign_id", campaign.CampaignID),
		forge.F("node", response.NodeID),
		forge.F("granted", response.Granted),
		forge.F("votes_granted", campaign.VotesGranted),
		forge.F("votes_needed", campaign.VotesNeeded),
	)

	// Check if campaign is won
	if campaign.VotesGranted >= campaign.VotesNeeded {
		cm.campaignWon(campaign)
	}

	// Check for early abort if enabled
	if cm.earlyAbort && cm.shouldAbort(campaign) {
		cm.campaignLost(campaign)
	}

	return nil
}

// GetActiveCampaign returns the active campaign.
func (cm *CampaignManager) GetActiveCampaign() *Campaign {
	cm.campaignMu.RLock()
	defer cm.campaignMu.RUnlock()

	return cm.activeCampaign
}

// EndCampaign ends the active campaign with a status.
func (cm *CampaignManager) EndCampaign(status CampaignStatus) {
	cm.campaignMu.Lock()
	defer cm.campaignMu.Unlock()

	if cm.activeCampaign == nil {
		return
	}

	campaign := cm.activeCampaign
	campaign.mu.Lock()
	campaign.Status = status
	campaign.EndTime = time.Now()
	campaign.mu.Unlock()

	// Record in history
	cm.recordCampaign(campaign)

	cm.activeCampaign = nil

	cm.logger.Info("campaign ended",
		forge.F("campaign_id", campaign.CampaignID),
		forge.F("term", campaign.Term),
		forge.F("status", status),
		forge.F("duration", campaign.EndTime.Sub(campaign.StartTime)),
		forge.F("votes_granted", campaign.VotesGranted),
		forge.F("votes_needed", campaign.VotesNeeded),
	)
}

// campaignWon marks campaign as won.
func (cm *CampaignManager) campaignWon(campaign *Campaign) {
	campaign.Status = CampaignStatusWon
	campaign.EndTime = time.Now()

	cm.logger.Info("campaign won",
		forge.F("campaign_id", campaign.CampaignID),
		forge.F("term", campaign.Term),
		forge.F("votes_granted", campaign.VotesGranted),
		forge.F("duration", campaign.EndTime.Sub(campaign.StartTime)),
	)
}

// campaignLost marks campaign as lost.
func (cm *CampaignManager) campaignLost(campaign *Campaign) {
	campaign.Status = CampaignStatusLost
	campaign.EndTime = time.Now()

	cm.logger.Info("campaign lost",
		forge.F("campaign_id", campaign.CampaignID),
		forge.F("term", campaign.Term),
		forge.F("votes_granted", campaign.VotesGranted),
		forge.F("votes_denied", campaign.VotesDenied),
	)
}

// shouldAbort checks if campaign should be aborted early.
func (cm *CampaignManager) shouldAbort(campaign *Campaign) bool {
	// Calculate maximum possible votes
	totalPeers := len(campaign.TargetPeers)
	responsesReceived := len(campaign.VoteResponses)
	remainingPeers := totalPeers - responsesReceived
	maxPossibleVotes := campaign.VotesGranted + remainingPeers

	// Abort if we can't possibly win
	return maxPossibleVotes < campaign.VotesNeeded
}

// GetPeersToContact returns peers to contact based on strategy.
func (cm *CampaignManager) GetPeersToContact(campaign *Campaign) []string {
	campaign.mu.RLock()
	defer campaign.mu.RUnlock()

	switch cm.strategy {
	case CampaignStrategyPriority:
		return cm.getPriorityPeers(campaign)

	case CampaignStrategyAdaptive:
		return cm.getAdaptivePeers(campaign)

	default:
		return campaign.TargetPeers
	}
}

// getPriorityPeers returns peers in priority order.
func (cm *CampaignManager) getPriorityPeers(campaign *Campaign) []string {
	// Contacted priority peers first, then others
	var result []string

	// Add priority peers that haven't been contacted
	for _, peer := range cm.priorityPeers {
		if _, contacted := campaign.VoteResponses[peer]; !contacted {
			result = append(result, peer)
		}
	}

	// Add remaining peers
	for _, peer := range campaign.TargetPeers {
		if _, contacted := campaign.VoteResponses[peer]; !contacted {
			isPriority := slices.Contains(cm.priorityPeers, peer)

			if !isPriority {
				result = append(result, peer)
			}
		}
	}

	return result
}

// getAdaptivePeers returns peers using adaptive strategy.
func (cm *CampaignManager) getAdaptivePeers(campaign *Campaign) []string {
	// Contact fast responders first based on historical data
	// For now, just return all uncontacted peers
	var result []string

	for _, peer := range campaign.TargetPeers {
		if _, contacted := campaign.VoteResponses[peer]; !contacted {
			result = append(result, peer)
		}
	}

	return result
}

// GetCampaignProgress returns campaign progress information.
func (cm *CampaignManager) GetCampaignProgress() *CampaignProgress {
	cm.campaignMu.RLock()
	campaign := cm.activeCampaign
	cm.campaignMu.RUnlock()

	if campaign == nil {
		return nil
	}

	campaign.mu.RLock()
	defer campaign.mu.RUnlock()

	progress := &CampaignProgress{
		CampaignID:        campaign.CampaignID,
		Term:              campaign.Term,
		Status:            campaign.Status,
		VotesGranted:      campaign.VotesGranted,
		VotesDenied:       campaign.VotesDenied,
		VotesNeeded:       campaign.VotesNeeded,
		TotalPeers:        len(campaign.TargetPeers),
		ResponsesReceived: len(campaign.VoteResponses),
		Duration:          time.Since(campaign.StartTime),
	}

	if progress.VotesNeeded > 0 {
		progress.ProgressPercent = float64(progress.VotesGranted) / float64(progress.VotesNeeded) * 100.0
	}

	return progress
}

// CampaignProgress represents campaign progress.
type CampaignProgress struct {
	CampaignID        string
	Term              uint64
	Status            CampaignStatus
	VotesGranted      int
	VotesDenied       int
	VotesNeeded       int
	TotalPeers        int
	ResponsesReceived int
	Duration          time.Duration
	ProgressPercent   float64
}

// recordCampaign records a campaign in history.
func (cm *CampaignManager) recordCampaign(campaign *Campaign) {
	cm.historyMu.Lock()
	defer cm.historyMu.Unlock()

	record := CampaignRecord{
		CampaignID:   campaign.CampaignID,
		Term:         campaign.Term,
		StartTime:    campaign.StartTime,
		EndTime:      campaign.EndTime,
		Duration:     campaign.EndTime.Sub(campaign.StartTime),
		Status:       campaign.Status,
		VotesGranted: campaign.VotesGranted,
		VotesDenied:  campaign.VotesDenied,
		TotalPeers:   len(campaign.TargetPeers),
	}

	cm.campaigns = append(cm.campaigns, record)

	// Trim history if needed
	if len(cm.campaigns) > cm.maxHistorySize {
		cm.campaigns = cm.campaigns[len(cm.campaigns)-cm.maxHistorySize:]
	}
}

// GetCampaignHistory returns campaign history.
func (cm *CampaignManager) GetCampaignHistory(limit int) []CampaignRecord {
	cm.historyMu.RLock()
	defer cm.historyMu.RUnlock()

	if limit <= 0 || limit > len(cm.campaigns) {
		limit = len(cm.campaigns)
	}

	start := len(cm.campaigns) - limit
	result := make([]CampaignRecord, limit)
	copy(result, cm.campaigns[start:])

	return result
}

// GetCampaignStatistics returns campaign statistics.
func (cm *CampaignManager) GetCampaignStatistics() CampaignStatistics {
	cm.historyMu.RLock()
	defer cm.historyMu.RUnlock()

	stats := CampaignStatistics{
		TotalCampaigns: len(cm.campaigns),
	}

	if len(cm.campaigns) == 0 {
		return stats
	}

	var totalDuration time.Duration

	wonCount := 0
	lostCount := 0
	abortedCount := 0

	for _, campaign := range cm.campaigns {
		totalDuration += campaign.Duration

		switch campaign.Status {
		case CampaignStatusWon:
			wonCount++
		case CampaignStatusLost:
			lostCount++
		case CampaignStatusAborted:
			abortedCount++
		}
	}

	stats.CampaignsWon = wonCount
	stats.CampaignsLost = lostCount
	stats.CampaignsAborted = abortedCount
	stats.AverageDuration = totalDuration / time.Duration(len(cm.campaigns))

	if stats.TotalCampaigns > 0 {
		stats.WinRate = float64(wonCount) / float64(stats.TotalCampaigns) * 100.0
	}

	return stats
}

// CampaignStatistics contains campaign statistics.
type CampaignStatistics struct {
	TotalCampaigns   int
	CampaignsWon     int
	CampaignsLost    int
	CampaignsAborted int
	AverageDuration  time.Duration
	WinRate          float64
}

// String returns string representation of campaign status.
func (cs CampaignStatus) String() string {
	switch cs {
	case CampaignStatusActive:
		return "active"
	case CampaignStatusWon:
		return "won"
	case CampaignStatusLost:
		return "lost"
	case CampaignStatusAborted:
		return "aborted"
	case CampaignStatusTimeout:
		return "timeout"
	default:
		return "unknown"
	}
}

// String returns string representation of campaign strategy.
func (cs CampaignStrategy) String() string {
	switch cs {
	case CampaignStrategySequential:
		return "sequential"
	case CampaignStrategyParallel:
		return "parallel"
	case CampaignStrategyPriority:
		return "priority"
	case CampaignStrategyAdaptive:
		return "adaptive"
	default:
		return "unknown"
	}
}
