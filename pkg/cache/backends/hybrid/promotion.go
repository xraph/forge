package hybrid

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// PromotionEngine manages intelligent promotion and demotion of cache entries
type PromotionEngine struct {
	config         *cachecore.HybridConfig
	accessTracker  *AccessTracker
	rules          []PromotionRule
	policies       []PromotionPolicy
	analytics      *PromotionAnalytics
	predictor      *AccessPredictor
	costCalculator *CostCalculator
	logger         common.Logger
	mu             sync.RWMutex
}

// PromotionDecision represents a decision to promote or demote a key
type PromotionDecision struct {
	Key        string                 `json:"key"`
	Action     PromotionAction        `json:"action"`
	SourceTier int                    `json:"source_tier"`
	TargetTier int                    `json:"target_tier"`
	Confidence float64                `json:"confidence"`
	Reason     string                 `json:"reason"`
	Cost       float64                `json:"cost"`
	Benefit    float64                `json:"benefit"`
	Score      float64                `json:"score"`
	Urgency    PromotionUrgency       `json:"urgency"`
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// PromotionAction defines the type of promotion action
type PromotionAction string

const (
	ActionPromote PromotionAction = "promote"
	ActionDemote  PromotionAction = "demote"
	ActionStay    PromotionAction = "stay"
)

// PromotionUrgency defines the urgency of a promotion decision
type PromotionUrgency string

const (
	UrgencyLow      PromotionUrgency = "low"
	UrgencyMedium   PromotionUrgency = "medium"
	UrgencyHigh     PromotionUrgency = "high"
	UrgencyCritical PromotionUrgency = "critical"
)

// PromotionPolicy defines policies for promotion decisions
type PromotionPolicy interface {
	Name() string
	Evaluate(pattern *AccessPattern, sourceKey string, sourceTier int) *PromotionDecision
	Priority() int
	IsEnabled() bool
}

// PromotionAnalytics tracks analytics for promotion decisions
type PromotionAnalytics struct {
	decisions     map[string]*PromotionDecision
	outcomes      map[string]*PromotionOutcome
	statistics    *PromotionStatistics
	historyWindow time.Duration
	maxHistory    int
	mu            sync.RWMutex
}

// PromotionOutcome tracks the outcome of a promotion decision
type PromotionOutcome struct {
	Decision      *PromotionDecision `json:"decision"`
	Success       bool               `json:"success"`
	ActualBenefit float64            `json:"actual_benefit"`
	ActualCost    float64            `json:"actual_cost"`
	Duration      time.Duration      `json:"duration"`
	Timestamp     time.Time          `json:"timestamp"`
	Error         string             `json:"error,omitempty"`
}

// PromotionStatistics contains statistics about promotion decisions
type PromotionStatistics struct {
	TotalDecisions       int64                      `json:"total_decisions"`
	SuccessfulPromotions int64                      `json:"successful_promotions"`
	SuccessfulDemotions  int64                      `json:"successful_demotions"`
	FailedPromotions     int64                      `json:"failed_promotions"`
	FailedDemotions      int64                      `json:"failed_demotions"`
	AverageConfidence    float64                    `json:"average_confidence"`
	AverageBenefit       float64                    `json:"average_benefit"`
	AverageCost          float64                    `json:"average_cost"`
	PolicyStats          map[string]int64           `json:"policy_stats"`
	TierStats            map[int]TierPromotionStats `json:"tier_stats"`
}

// TierPromotionStats contains promotion statistics for a specific tier
type TierPromotionStats struct {
	Tier                 int           `json:"tier"`
	PromotionsIn         int64         `json:"promotions_in"`
	PromotionsOut        int64         `json:"promotions_out"`
	DemotionsIn          int64         `json:"demotions_in"`
	DemotionsOut         int64         `json:"demotions_out"`
	AverageResidenceTime time.Duration `json:"average_residence_time"`
	SuccessRate          float64       `json:"success_rate"`
}

// AccessPredictor predicts future access patterns
type AccessPredictor struct {
	models     map[string]*PredictionModel
	features   *FeatureExtractor
	confidence float64
	mu         sync.RWMutex
}

// PredictionModel represents a machine learning model for access prediction
type PredictionModel struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Accuracy    float64                `json:"accuracy"`
	LastTrained time.Time              `json:"last_trained"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// FeatureExtractor extracts features from access patterns
type FeatureExtractor struct {
	features map[string]float64
}

// CostCalculator calculates the cost and benefit of promotion decisions
type CostCalculator struct {
	tierCosts    map[int]float64
	transferCost float64
	storageCost  float64
	accessCost   float64
}

// NewPromotionEngine creates a new promotion engine
func NewPromotionEngine(config *cachecore.HybridConfig, tracker *AccessTracker, logger common.Logger) *PromotionEngine {
	engine := &PromotionEngine{
		config:         config,
		accessTracker:  tracker,
		rules:          make([]PromotionRule, 0),
		policies:       make([]PromotionPolicy, 0),
		logger:         logger,
		analytics:      NewPromotionAnalytics(),
		predictor:      NewAccessPredictor(),
		costCalculator: NewCostCalculator(),
	}

	// Add default promotion policies
	engine.AddPolicy(&HotKeyPolicy{threshold: 10.0})
	engine.AddPolicy(&ColdKeyPolicy{threshold: 2.0})
	engine.AddPolicy(&SizeAwarePolicy{maxSizeL1: 1024 * 1024}) // 1MB
	engine.AddPolicy(&CapacityAwarePolicy{})
	engine.AddPolicy(&LatencyOptimizedPolicy{})
	engine.AddPolicy(&CostOptimizedPolicy{})

	return engine
}

// AddPolicy adds a promotion policy
func (pe *PromotionEngine) AddPolicy(policy PromotionPolicy) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.policies = append(pe.policies, policy)
}

// EvaluatePromotion evaluates whether a key should be promoted or demoted
func (pe *PromotionEngine) EvaluatePromotion(key string, currentTier int) *PromotionDecision {
	pattern := pe.accessTracker.GetAccessPattern(key)
	if pattern == nil {
		return &PromotionDecision{
			Key:        key,
			Action:     ActionStay,
			SourceTier: currentTier,
			TargetTier: currentTier,
			Confidence: 0.0,
			Reason:     "no access pattern data",
			Timestamp:  time.Now(),
		}
	}

	// Get decisions from all policies
	decisions := make([]*PromotionDecision, 0, len(pe.policies))

	pe.mu.RLock()
	for _, policy := range pe.policies {
		if policy.IsEnabled() {
			decision := policy.Evaluate(pattern, key, currentTier)
			if decision != nil {
				decisions = append(decisions, decision)
			}
		}
	}
	pe.mu.RUnlock()

	// Aggregate decisions
	finalDecision := pe.aggregateDecisions(decisions, key, currentTier)

	// Enhance with prediction
	pe.enhanceWithPrediction(finalDecision, pattern)

	// Calculate cost-benefit
	pe.calculateCostBenefit(finalDecision, pattern)

	// Record decision
	pe.analytics.RecordDecision(finalDecision)

	return finalDecision
}

// ExecutePromotionDecision executes a promotion decision
func (pe *PromotionEngine) ExecutePromotionDecision(ctx context.Context, decision *PromotionDecision, cache *HybridCache) error {
	start := time.Now()

	var err error
	outcome := &PromotionOutcome{
		Decision:  decision,
		Timestamp: start,
	}

	defer func() {
		outcome.Duration = time.Since(start)
		outcome.Success = err == nil
		if err != nil {
			outcome.Error = err.Error()
		}
		pe.analytics.RecordOutcome(outcome)
	}()

	switch decision.Action {
	case ActionPromote:
		err = cache.PromoteToTier(ctx, decision.Key, decision.TargetTier)
		if err == nil && pe.logger != nil {
			pe.logger.Info("key promoted",
				logger.String("key", decision.Key),
				logger.Int("from_tier", decision.SourceTier),
				logger.Int("to_tier", decision.TargetTier),
				logger.Float64("confidence", decision.Confidence),
				logger.String("reason", decision.Reason),
			)
		}
	case ActionDemote:
		err = cache.DemoteFromTier(ctx, decision.Key, decision.SourceTier)
		if err == nil && pe.logger != nil {
			pe.logger.Info("key demoted",
				logger.String("key", decision.Key),
				logger.Int("from_tier", decision.SourceTier),
				logger.Int("to_tier", decision.TargetTier),
				logger.Float64("confidence", decision.Confidence),
				logger.String("reason", decision.Reason),
			)
		}
	case ActionStay:
		// No action needed
		err = nil
	default:
		err = fmt.Errorf("unknown promotion action: %s", decision.Action)
	}

	return err
}

// GetPromotionCandidates returns candidates for promotion from a tier
func (pe *PromotionEngine) GetPromotionCandidates(tier int, limit int) []*PromotionDecision {
	// Get access patterns for keys in this tier
	candidates := make([]*PromotionDecision, 0)

	// This would typically iterate through keys in the tier
	// For now, return empty slice as implementation depends on tier access
	return candidates
}

// GetDemotionCandidates returns candidates for demotion from a tier
func (pe *PromotionEngine) GetDemotionCandidates(tier int, limit int) []*PromotionDecision {
	// Get access patterns for keys in this tier that should be demoted
	candidates := make([]*PromotionDecision, 0)

	// This would typically iterate through keys in the tier
	// For now, return empty slice as implementation depends on tier access
	return candidates
}

// GetAnalytics returns promotion analytics
func (pe *PromotionEngine) GetAnalytics() *PromotionAnalytics {
	return pe.analytics
}

// aggregateDecisions combines multiple policy decisions into a final decision
func (pe *PromotionEngine) aggregateDecisions(decisions []*PromotionDecision, key string, currentTier int) *PromotionDecision {
	if len(decisions) == 0 {
		return &PromotionDecision{
			Key:        key,
			Action:     ActionStay,
			SourceTier: currentTier,
			TargetTier: currentTier,
			Confidence: 0.0,
			Reason:     "no policy decisions",
			Timestamp:  time.Now(),
		}
	}

	// Sort decisions by priority and confidence
	sort.Slice(decisions, func(i, j int) bool {
		if decisions[i].Confidence != decisions[j].Confidence {
			return decisions[i].Confidence > decisions[j].Confidence
		}
		return decisions[i].Score > decisions[j].Score
	})

	// Use weighted voting based on confidence
	var promoteVotes, demoteVotes, stayVotes float64
	var totalWeight float64

	reasons := make([]string, 0, len(decisions))

	for _, decision := range decisions {
		weight := decision.Confidence
		totalWeight += weight

		switch decision.Action {
		case ActionPromote:
			promoteVotes += weight
		case ActionDemote:
			demoteVotes += weight
		case ActionStay:
			stayVotes += weight
		}

		if decision.Reason != "" {
			reasons = append(reasons, decision.Reason)
		}
	}

	// Determine final action
	var finalAction PromotionAction
	var finalConfidence float64
	var targetTier int

	if promoteVotes > demoteVotes && promoteVotes > stayVotes {
		finalAction = ActionPromote
		finalConfidence = promoteVotes / totalWeight
		targetTier = currentTier - 1
		if targetTier < 0 {
			targetTier = 0
		}
	} else if demoteVotes > stayVotes {
		finalAction = ActionDemote
		finalConfidence = demoteVotes / totalWeight
		targetTier = currentTier + 1
	} else {
		finalAction = ActionStay
		finalConfidence = stayVotes / totalWeight
		targetTier = currentTier
	}

	// Calculate urgency based on confidence and vote margin
	maxVote := math.Max(math.Max(promoteVotes, demoteVotes), stayVotes)
	secondMaxVote := 0.0
	if maxVote == promoteVotes {
		secondMaxVote = math.Max(demoteVotes, stayVotes)
	} else if maxVote == demoteVotes {
		secondMaxVote = math.Max(promoteVotes, stayVotes)
	} else {
		secondMaxVote = math.Max(promoteVotes, demoteVotes)
	}

	margin := (maxVote - secondMaxVote) / totalWeight
	urgency := UrgencyLow
	if finalConfidence > 0.9 && margin > 0.5 {
		urgency = UrgencyCritical
	} else if finalConfidence > 0.7 && margin > 0.3 {
		urgency = UrgencyHigh
	} else if finalConfidence > 0.5 && margin > 0.2 {
		urgency = UrgencyMedium
	}

	return &PromotionDecision{
		Key:        key,
		Action:     finalAction,
		SourceTier: currentTier,
		TargetTier: targetTier,
		Confidence: finalConfidence,
		Reason:     fmt.Sprintf("aggregated: %v", reasons),
		Score:      maxVote,
		Urgency:    urgency,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"promote_votes": promoteVotes,
			"demote_votes":  demoteVotes,
			"stay_votes":    stayVotes,
			"total_weight":  totalWeight,
			"margin":        margin,
		},
	}
}

// enhanceWithPrediction enhances decision with access prediction
func (pe *PromotionEngine) enhanceWithPrediction(decision *PromotionDecision, pattern *AccessPattern) {
	prediction := pe.predictor.PredictFutureAccess(pattern)

	// Adjust confidence based on prediction
	if prediction.Confidence > 0.8 {
		if prediction.ExpectedAccesses > 10 && decision.Action == ActionPromote {
			decision.Confidence = math.Min(1.0, decision.Confidence*1.2)
		} else if prediction.ExpectedAccesses < 2 && decision.Action == ActionDemote {
			decision.Confidence = math.Min(1.0, decision.Confidence*1.1)
		}
	}

	// Add prediction metadata
	if decision.Metadata == nil {
		decision.Metadata = make(map[string]interface{})
	}
	decision.Metadata["prediction"] = prediction
}

// calculateCostBenefit calculates cost and benefit of the decision
func (pe *PromotionEngine) calculateCostBenefit(decision *PromotionDecision, pattern *AccessPattern) {
	cost := pe.costCalculator.CalculateCost(decision, pattern)
	benefit := pe.costCalculator.CalculateBenefit(decision, pattern)

	decision.Cost = cost
	decision.Benefit = benefit

	// Adjust score based on cost-benefit ratio
	if cost > 0 {
		decision.Score = decision.Score * (benefit / cost)
	}
}

// Promotion Policies Implementation

// HotKeyPolicy promotes frequently accessed keys
type HotKeyPolicy struct {
	threshold float64
}

func (p *HotKeyPolicy) Name() string {
	return "hot_key_policy"
}

func (p *HotKeyPolicy) Priority() int {
	return 100
}

func (p *HotKeyPolicy) IsEnabled() bool {
	return true
}

func (p *HotKeyPolicy) Evaluate(pattern *AccessPattern, key string, currentTier int) *PromotionDecision {
	if pattern.AccessFrequency >= p.threshold {
		return &PromotionDecision{
			Key:        key,
			Action:     ActionPromote,
			SourceTier: currentTier,
			TargetTier: currentTier - 1,
			Confidence: math.Min(1.0, pattern.AccessFrequency/p.threshold),
			Reason:     fmt.Sprintf("hot key (frequency: %.2f)", pattern.AccessFrequency),
			Score:      pattern.AccessFrequency,
			Timestamp:  time.Now(),
		}
	}
	return nil
}

// ColdKeyPolicy demotes infrequently accessed keys
type ColdKeyPolicy struct {
	threshold float64
}

func (p *ColdKeyPolicy) Name() string {
	return "cold_key_policy"
}

func (p *ColdKeyPolicy) Priority() int {
	return 90
}

func (p *ColdKeyPolicy) IsEnabled() bool {
	return true
}

func (p *ColdKeyPolicy) Evaluate(pattern *AccessPattern, key string, currentTier int) *PromotionDecision {
	if pattern.AccessFrequency < p.threshold && time.Since(pattern.LastAccessed) > time.Hour {
		return &PromotionDecision{
			Key:        key,
			Action:     ActionDemote,
			SourceTier: currentTier,
			TargetTier: currentTier + 1,
			Confidence: math.Min(1.0, (p.threshold-pattern.AccessFrequency)/p.threshold),
			Reason:     fmt.Sprintf("cold key (frequency: %.2f)", pattern.AccessFrequency),
			Score:      p.threshold - pattern.AccessFrequency,
			Timestamp:  time.Now(),
		}
	}
	return nil
}

// SizeAwarePolicy considers data size in decisions
type SizeAwarePolicy struct {
	maxSizeL1 int64
}

func (p *SizeAwarePolicy) Name() string {
	return "size_aware_policy"
}

func (p *SizeAwarePolicy) Priority() int {
	return 80
}

func (p *SizeAwarePolicy) IsEnabled() bool {
	return true
}

func (p *SizeAwarePolicy) Evaluate(pattern *AccessPattern, key string, currentTier int) *PromotionDecision {
	// This would need access to value size information
	// For now, return nil (no decision)
	return nil
}

// CapacityAwarePolicy considers tier capacity
type CapacityAwarePolicy struct{}

func (p *CapacityAwarePolicy) Name() string {
	return "capacity_aware_policy"
}

func (p *CapacityAwarePolicy) Priority() int {
	return 70
}

func (p *CapacityAwarePolicy) IsEnabled() bool {
	return true
}

func (p *CapacityAwarePolicy) Evaluate(pattern *AccessPattern, key string, currentTier int) *PromotionDecision {
	// This would need access to tier capacity information
	// For now, return nil (no decision)
	return nil
}

// LatencyOptimizedPolicy optimizes for latency
type LatencyOptimizedPolicy struct{}

func (p *LatencyOptimizedPolicy) Name() string {
	return "latency_optimized_policy"
}

func (p *LatencyOptimizedPolicy) Priority() int {
	return 85
}

func (p *LatencyOptimizedPolicy) IsEnabled() bool {
	return true
}

func (p *LatencyOptimizedPolicy) Evaluate(pattern *AccessPattern, key string, currentTier int) *PromotionDecision {
	// Promote keys that would benefit most from lower latency
	if pattern.AccessFrequency > 5.0 && currentTier > 0 {
		return &PromotionDecision{
			Key:        key,
			Action:     ActionPromote,
			SourceTier: currentTier,
			TargetTier: currentTier - 1,
			Confidence: 0.7,
			Reason:     "latency optimization",
			Score:      pattern.AccessFrequency * 0.8,
			Timestamp:  time.Now(),
		}
	}
	return nil
}

// CostOptimizedPolicy optimizes for cost
type CostOptimizedPolicy struct{}

func (p *CostOptimizedPolicy) Name() string {
	return "cost_optimized_policy"
}

func (p *CostOptimizedPolicy) Priority() int {
	return 60
}

func (p *CostOptimizedPolicy) IsEnabled() bool {
	return true
}

func (p *CostOptimizedPolicy) Evaluate(pattern *AccessPattern, key string, currentTier int) *PromotionDecision {
	// This would implement cost-based promotion logic
	// For now, return nil (no decision)
	return nil
}

// NewPromotionAnalytics creates a new promotion analytics tracker
func NewPromotionAnalytics() *PromotionAnalytics {
	return &PromotionAnalytics{
		decisions: make(map[string]*PromotionDecision),
		outcomes:  make(map[string]*PromotionOutcome),
		statistics: &PromotionStatistics{
			PolicyStats: make(map[string]int64),
			TierStats:   make(map[int]TierPromotionStats),
		},
		historyWindow: 24 * time.Hour,
		maxHistory:    10000,
	}
}

// RecordDecision records a promotion decision
func (pa *PromotionAnalytics) RecordDecision(decision *PromotionDecision) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	pa.decisions[decision.Key] = decision
	pa.statistics.TotalDecisions++

	// Clean up old decisions
	if len(pa.decisions) > pa.maxHistory {
		pa.cleanupOldDecisions()
	}
}

// RecordOutcome records the outcome of a promotion decision
func (pa *PromotionAnalytics) RecordOutcome(outcome *PromotionOutcome) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	pa.outcomes[outcome.Decision.Key] = outcome

	// Update statistics
	if outcome.Success {
		switch outcome.Decision.Action {
		case ActionPromote:
			pa.statistics.SuccessfulPromotions++
		case ActionDemote:
			pa.statistics.SuccessfulDemotions++
		}
	} else {
		switch outcome.Decision.Action {
		case ActionPromote:
			pa.statistics.FailedPromotions++
		case ActionDemote:
			pa.statistics.FailedDemotions++
		}
	}

	// Clean up old outcomes
	if len(pa.outcomes) > pa.maxHistory {
		pa.cleanupOldOutcomes()
	}
}

// GetStatistics returns current statistics
func (pa *PromotionAnalytics) GetStatistics() *PromotionStatistics {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	// Calculate averages
	if pa.statistics.TotalDecisions > 0 {
		totalConfidence := 0.0
		totalBenefit := 0.0
		totalCost := 0.0
		count := 0

		for _, decision := range pa.decisions {
			totalConfidence += decision.Confidence
			totalBenefit += decision.Benefit
			totalCost += decision.Cost
			count++
		}

		if count > 0 {
			pa.statistics.AverageConfidence = totalConfidence / float64(count)
			pa.statistics.AverageBenefit = totalBenefit / float64(count)
			pa.statistics.AverageCost = totalCost / float64(count)
		}
	}

	// Return a copy
	return &PromotionStatistics{
		TotalDecisions:       pa.statistics.TotalDecisions,
		SuccessfulPromotions: pa.statistics.SuccessfulPromotions,
		SuccessfulDemotions:  pa.statistics.SuccessfulDemotions,
		FailedPromotions:     pa.statistics.FailedPromotions,
		FailedDemotions:      pa.statistics.FailedDemotions,
		AverageConfidence:    pa.statistics.AverageConfidence,
		AverageBenefit:       pa.statistics.AverageBenefit,
		AverageCost:          pa.statistics.AverageCost,
		PolicyStats:          pa.statistics.PolicyStats,
		TierStats:            pa.statistics.TierStats,
	}
}

// cleanupOldDecisions removes old decisions to stay under memory limits
func (pa *PromotionAnalytics) cleanupOldDecisions() {
	cutoff := time.Now().Add(-pa.historyWindow)

	for key, decision := range pa.decisions {
		if decision.Timestamp.Before(cutoff) {
			delete(pa.decisions, key)
		}
	}
}

// cleanupOldOutcomes removes old outcomes to stay under memory limits
func (pa *PromotionAnalytics) cleanupOldOutcomes() {
	cutoff := time.Now().Add(-pa.historyWindow)

	for key, outcome := range pa.outcomes {
		if outcome.Timestamp.Before(cutoff) {
			delete(pa.outcomes, key)
		}
	}
}

// NewAccessPredictor creates a new access predictor
func NewAccessPredictor() *AccessPredictor {
	return &AccessPredictor{
		models:     make(map[string]*PredictionModel),
		features:   &FeatureExtractor{features: make(map[string]float64)},
		confidence: 0.5,
	}
}

// PredictFutureAccess predicts future access patterns for a key
func (ap *AccessPredictor) PredictFutureAccess(pattern *AccessPattern) *AccessPrediction {
	// Simple prediction based on recent access frequency
	// In practice, this would use machine learning models

	if len(pattern.RecentAccesses) == 0 {
		return &AccessPrediction{
			Key:               pattern.Key,
			ExpectedAccesses:  0,
			Confidence:        0.0,
			PredictionHorizon: time.Hour,
		}
	}

	// Calculate expected accesses based on recent frequency
	recentFrequency := pattern.AccessFrequency
	expectedAccesses := int64(recentFrequency * 1.0) // Next hour

	// Simple confidence based on pattern consistency
	confidence := 0.5
	if recentFrequency > 10 {
		confidence = 0.8
	} else if recentFrequency > 5 {
		confidence = 0.7
	} else if recentFrequency > 1 {
		confidence = 0.6
	}

	return &AccessPrediction{
		Key:               pattern.Key,
		ExpectedAccesses:  expectedAccesses,
		Confidence:        confidence,
		PredictionHorizon: time.Hour,
	}
}

// AccessPrediction represents a prediction of future access
type AccessPrediction struct {
	Key               string        `json:"key"`
	ExpectedAccesses  int64         `json:"expected_accesses"`
	Confidence        float64       `json:"confidence"`
	PredictionHorizon time.Duration `json:"prediction_horizon"`
}

// NewCostCalculator creates a new cost calculator
func NewCostCalculator() *CostCalculator {
	return &CostCalculator{
		tierCosts: map[int]float64{
			0: 1.0, // L1 (most expensive)
			1: 0.5, // L2
			2: 0.1, // L3 (least expensive)
		},
		transferCost: 0.01,
		storageCost:  0.001,
		accessCost:   0.0001,
	}
}

// CalculateCost calculates the cost of a promotion decision
func (cc *CostCalculator) CalculateCost(decision *PromotionDecision, pattern *AccessPattern) float64 {
	var cost float64

	// Base tier cost difference
	sourceCost := cc.tierCosts[decision.SourceTier]
	targetCost := cc.tierCosts[decision.TargetTier]
	cost += math.Abs(targetCost - sourceCost)

	// Transfer cost
	if decision.Action == ActionPromote || decision.Action == ActionDemote {
		cost += cc.transferCost
	}

	// Storage cost based on expected residence time
	expectedResidenceTime := 24.0 // hours
	cost += cc.storageCost * targetCost * expectedResidenceTime

	return cost
}

// CalculateBenefit calculates the benefit of a promotion decision
func (cc *CostCalculator) CalculateBenefit(decision *PromotionDecision, pattern *AccessPattern) float64 {
	var benefit float64

	// Access latency improvement benefit
	if decision.Action == ActionPromote {
		// Benefit from faster access
		benefit += pattern.AccessFrequency * 0.1 // 0.1 per access
	} else if decision.Action == ActionDemote {
		// Benefit from freeing up faster tier space
		benefit += 0.05
	}

	// Capacity optimization benefit
	benefit += 0.02

	return benefit
}
