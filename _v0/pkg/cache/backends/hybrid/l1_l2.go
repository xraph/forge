package hybrid

import (
	"context"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/v0/pkg/cache/core"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// AccessTracker tracks access patterns for keys across tiers
type AccessTracker struct {
	patterns      map[string]*KeyPattern
	windowSize    time.Duration
	maxPatterns   int
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
	mu            sync.RWMutex
}

// KeyPattern represents access pattern for a single key
type KeyPattern struct {
	Key            string
	AccessTimes    []time.Time
	TotalAccesses  int64
	LastAccessed   time.Time
	CurrentTier    int
	PromotionScore float64
	DemotionScore  float64
	mu             sync.RWMutex
}

// PromotionManager manages promotion and demotion rules
type PromotionManager struct {
	config        *cachecore.HybridConfig
	accessTracker *AccessTracker
	rules         []PromotionRule
	mu            sync.RWMutex
}

// PromotionRule defines rules for promoting/demoting keys
type PromotionRule interface {
	ShouldPromote(pattern *KeyPattern, currentTier int) bool
	ShouldDemote(pattern *KeyPattern, currentTier int) bool
	Name() string
	Priority() int
}

// TierOptimizer performs background optimization of tier allocation
type TierOptimizer struct {
	hybridCache   *HybridCache
	config        *cachecore.HybridConfig
	lastOptimized time.Time
	stats         *OptimizerStats
	mu            sync.RWMutex
}

// OptimizerStats tracks optimization statistics
type OptimizerStats struct {
	TotalOptimizations int64         `json:"total_optimizations"`
	LastOptimization   time.Time     `json:"last_optimization"`
	KeysPromoted       int64         `json:"keys_promoted"`
	KeysDemoted        int64         `json:"keys_demoted"`
	OptimizationTime   time.Duration `json:"optimization_time"`
	EfficiencyGain     float64       `json:"efficiency_gain"`
}

// NewAccessTracker creates a new access tracker
func NewAccessTracker() *AccessTracker {
	tracker := &AccessTracker{
		patterns:    make(map[string]*KeyPattern),
		windowSize:  time.Hour,
		maxPatterns: 100000,
		stopCleanup: make(chan struct{}),
	}

	// Start cleanup routine
	tracker.cleanupTicker = time.NewTicker(10 * time.Minute)
	go tracker.runCleanup()

	return tracker
}

// RecordAccess records an access to a key
func (at *AccessTracker) RecordAccess(key string, accessTime time.Time) {
	at.mu.Lock()
	defer at.mu.Unlock()

	pattern, exists := at.patterns[key]
	if !exists {
		// Check if we need to evict patterns to stay under limit
		if len(at.patterns) >= at.maxPatterns {
			at.evictOldestPattern()
		}

		pattern = &KeyPattern{
			Key:           key,
			AccessTimes:   make([]time.Time, 0, 100),
			TotalAccesses: 0,
			CurrentTier:   -1, // Unknown
		}
		at.patterns[key] = pattern
	}

	pattern.mu.Lock()
	defer pattern.mu.Unlock()

	// Add access time
	pattern.AccessTimes = append(pattern.AccessTimes, accessTime)
	pattern.TotalAccesses++
	pattern.LastAccessed = accessTime

	// Keep only recent accesses within window
	cutoff := accessTime.Add(-at.windowSize)
	newAccessTimes := make([]time.Time, 0, len(pattern.AccessTimes))
	for _, t := range pattern.AccessTimes {
		if t.After(cutoff) {
			newAccessTimes = append(newAccessTimes, t)
		}
	}
	pattern.AccessTimes = newAccessTimes

	// Update scores
	at.updateScores(pattern)
}

// GetAccessPattern returns the access pattern for a key
func (at *AccessTracker) GetAccessPattern(key string) *AccessPattern {
	at.mu.RLock()
	defer at.mu.RUnlock()

	pattern, exists := at.patterns[key]
	if !exists {
		return &AccessPattern{
			Key:             key,
			TotalAccesses:   0,
			AccessFrequency: 0,
			CurrentTier:     -1,
		}
	}

	pattern.mu.RLock()
	defer pattern.mu.RUnlock()

	// Calculate frequency
	frequency := float64(len(pattern.AccessTimes)) / at.windowSize.Hours()

	return &AccessPattern{
		Key:             key,
		TotalAccesses:   pattern.TotalAccesses,
		RecentAccesses:  append([]time.Time{}, pattern.AccessTimes...),
		AccessFrequency: frequency,
		LastAccessed:    pattern.LastAccessed,
		CurrentTier:     pattern.CurrentTier,
		ShouldPromote:   pattern.PromotionScore > 0.7,
		ShouldDemote:    pattern.DemotionScore > 0.7,
		Score:           pattern.PromotionScore,
	}
}

// SetCurrentTier updates the current tier for a key
func (at *AccessTracker) SetCurrentTier(key string, tier int) {
	at.mu.RLock()
	pattern, exists := at.patterns[key]
	at.mu.RUnlock()

	if exists {
		pattern.mu.Lock()
		pattern.CurrentTier = tier
		pattern.mu.Unlock()
	}
}

// RemoveKey removes a key from tracking
func (at *AccessTracker) RemoveKey(key string) {
	at.mu.Lock()
	defer at.mu.Unlock()
	delete(at.patterns, key)
}

// Clear removes all tracked patterns
func (at *AccessTracker) Clear() {
	at.mu.Lock()
	defer at.mu.Unlock()
	at.patterns = make(map[string]*KeyPattern)
}

// GetTopAccessedKeys returns the most accessed keys
func (at *AccessTracker) GetTopAccessedKeys(limit int) []string {
	at.mu.RLock()
	defer at.mu.RUnlock()

	type keyScore struct {
		key   string
		score float64
	}

	scores := make([]keyScore, 0, len(at.patterns))
	for key, pattern := range at.patterns {
		pattern.mu.RLock()
		scores = append(scores, keyScore{key: key, score: pattern.PromotionScore})
		pattern.mu.RUnlock()
	}

	// Sort by score (descending)
	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].score < scores[j].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	// Return top keys
	size := limit
	if size > len(scores) {
		size = len(scores)
	}

	result := make([]string, size)
	for i := 0; i < size; i++ {
		result[i] = scores[i].key
	}

	return result
}

// Stop stops the access tracker
func (at *AccessTracker) Stop() {
	close(at.stopCleanup)
	if at.cleanupTicker != nil {
		at.cleanupTicker.Stop()
	}
}

// updateScores updates promotion and demotion scores for a pattern
func (at *AccessTracker) updateScores(pattern *KeyPattern) {
	now := time.Now()
	recentCount := len(pattern.AccessTimes)

	if recentCount == 0 {
		pattern.PromotionScore = 0
		pattern.DemotionScore = 1.0
		return
	}

	// Calculate recency score (0-1, 1 = very recent)
	timeSinceLastAccess := now.Sub(pattern.LastAccessed)
	recencyScore := 1.0 / (1.0 + timeSinceLastAccess.Hours())

	// Calculate frequency score (accesses per hour)
	frequencyScore := float64(recentCount) / at.windowSize.Hours()
	if frequencyScore > 1.0 {
		frequencyScore = 1.0
	}

	// Calculate consistency score (how regular are the accesses)
	consistencyScore := at.calculateConsistencyScore(pattern.AccessTimes)

	// Weighted promotion score
	pattern.PromotionScore = (recencyScore * 0.4) + (frequencyScore * 0.4) + (consistencyScore * 0.2)

	// Demotion score is inverse of promotion factors
	pattern.DemotionScore = 1.0 - pattern.PromotionScore
}

// calculateConsistencyScore calculates how consistent access patterns are
func (at *AccessTracker) calculateConsistencyScore(accessTimes []time.Time) float64 {
	if len(accessTimes) < 2 {
		return 0.5 // Neutral score for insufficient data
	}

	// Calculate intervals between accesses
	intervals := make([]float64, len(accessTimes)-1)
	for i := 1; i < len(accessTimes); i++ {
		intervals[i-1] = accessTimes[i].Sub(accessTimes[i-1]).Hours()
	}

	// Calculate standard deviation of intervals
	mean := 0.0
	for _, interval := range intervals {
		mean += interval
	}
	mean /= float64(len(intervals))

	variance := 0.0
	for _, interval := range intervals {
		diff := interval - mean
		variance += diff * diff
	}
	variance /= float64(len(intervals))

	stdDev := variance // Simplified sqrt

	// Convert to consistency score (lower std dev = higher consistency)
	consistency := 1.0 / (1.0 + stdDev)
	return consistency
}

// evictOldestPattern removes the least recently accessed pattern
func (at *AccessTracker) evictOldestPattern() {
	var oldestKey string
	var oldestTime time.Time

	for key, pattern := range at.patterns {
		pattern.mu.RLock()
		if oldestKey == "" || pattern.LastAccessed.Before(oldestTime) {
			oldestKey = key
			oldestTime = pattern.LastAccessed
		}
		pattern.mu.RUnlock()
	}

	if oldestKey != "" {
		delete(at.patterns, oldestKey)
	}
}

// runCleanup runs the background cleanup routine
func (at *AccessTracker) runCleanup() {
	for {
		select {
		case <-at.stopCleanup:
			return
		case <-at.cleanupTicker.C:
			at.cleanup()
		}
	}
}

// cleanup removes old patterns
func (at *AccessTracker) cleanup() {
	at.mu.Lock()
	defer at.mu.Unlock()

	cutoff := time.Now().Add(-2 * at.windowSize)
	toDelete := make([]string, 0)

	for key, pattern := range at.patterns {
		pattern.mu.RLock()
		if pattern.LastAccessed.Before(cutoff) {
			toDelete = append(toDelete, key)
		}
		pattern.mu.RUnlock()
	}

	for _, key := range toDelete {
		delete(at.patterns, key)
	}
}

// NewPromotionManager creates a new promotion manager
func NewPromotionManager(config *cachecore.HybridConfig, tracker *AccessTracker) *PromotionManager {
	pm := &PromotionManager{
		config:        config,
		accessTracker: tracker,
		rules:         make([]PromotionRule, 0),
	}

	// Add default rules
	pm.AddRule(&FrequencyBasedRule{threshold: float64(config.PromotionThreshold)})
	pm.AddRule(&RecencyBasedRule{threshold: time.Duration(config.PromotionInterval)})
	pm.AddRule(&SizeBasedRule{})

	return pm
}

// AddRule adds a promotion rule
func (pm *PromotionManager) AddRule(rule PromotionRule) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.rules = append(pm.rules, rule)
}

// ShouldPromote determines if a key should be promoted
func (pm *PromotionManager) ShouldPromote(pattern *AccessPattern, currentTier int) bool {
	if currentTier <= 0 {
		return false // Already in highest tier
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Check all rules
	for _, rule := range pm.rules {
		if pattern != nil {
			keyPattern := &KeyPattern{
				Key:            pattern.Key,
				AccessTimes:    pattern.RecentAccesses,
				TotalAccesses:  pattern.TotalAccesses,
				LastAccessed:   pattern.LastAccessed,
				CurrentTier:    pattern.CurrentTier,
				PromotionScore: pattern.Score,
			}
			if rule.ShouldPromote(keyPattern, currentTier) {
				return true
			}
		}
	}

	return false
}

// ShouldDemote determines if a key should be demoted
func (pm *PromotionManager) ShouldDemote(pattern *AccessPattern, currentTier int) bool {
	maxTiers := 3 // Assuming max 3 tiers
	if currentTier >= maxTiers-1 {
		return false // Already in lowest tier
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Check all rules
	for _, rule := range pm.rules {
		if pattern != nil {
			keyPattern := &KeyPattern{
				Key:           pattern.Key,
				AccessTimes:   pattern.RecentAccesses,
				TotalAccesses: pattern.TotalAccesses,
				LastAccessed:  pattern.LastAccessed,
				CurrentTier:   pattern.CurrentTier,
				DemotionScore: 1.0 - pattern.Score,
			}
			if rule.ShouldDemote(keyPattern, currentTier) {
				return true
			}
		}
	}

	return false
}

// GetPromotionCandidates returns keys that should be promoted from a tier
func (pm *PromotionManager) GetPromotionCandidates(tier int) []string {
	// This would analyze access patterns and return candidates
	// For now, return empty slice
	return []string{}
}

// GetDemotionCandidates returns keys that should be demoted from a tier
func (pm *PromotionManager) GetDemotionCandidates(tier int) []string {
	// This would analyze access patterns and return candidates
	// For now, return empty slice
	return []string{}
}

// Promotion Rules Implementation

// FrequencyBasedRule promotes based on access frequency
type FrequencyBasedRule struct {
	threshold float64
}

func (r *FrequencyBasedRule) Name() string {
	return "frequency_based"
}

func (r *FrequencyBasedRule) Priority() int {
	return 100
}

func (r *FrequencyBasedRule) ShouldPromote(pattern *KeyPattern, currentTier int) bool {
	pattern.mu.RLock()
	defer pattern.mu.RUnlock()

	frequency := float64(len(pattern.AccessTimes))
	return frequency >= r.threshold
}

func (r *FrequencyBasedRule) ShouldDemote(pattern *KeyPattern, currentTier int) bool {
	pattern.mu.RLock()
	defer pattern.mu.RUnlock()

	frequency := float64(len(pattern.AccessTimes))
	return frequency < r.threshold/2 // Demote if frequency is significantly lower
}

// RecencyBasedRule promotes based on recent access
type RecencyBasedRule struct {
	threshold time.Duration
}

func (r *RecencyBasedRule) Name() string {
	return "recency_based"
}

func (r *RecencyBasedRule) Priority() int {
	return 90
}

func (r *RecencyBasedRule) ShouldPromote(pattern *KeyPattern, currentTier int) bool {
	pattern.mu.RLock()
	defer pattern.mu.RUnlock()

	return time.Since(pattern.LastAccessed) < r.threshold
}

func (r *RecencyBasedRule) ShouldDemote(pattern *KeyPattern, currentTier int) bool {
	pattern.mu.RLock()
	defer pattern.mu.RUnlock()

	return time.Since(pattern.LastAccessed) > r.threshold*2
}

// SizeBasedRule considers data size for promotion decisions
type SizeBasedRule struct{}

func (r *SizeBasedRule) Name() string {
	return "size_based"
}

func (r *SizeBasedRule) Priority() int {
	return 80
}

func (r *SizeBasedRule) ShouldPromote(pattern *KeyPattern, currentTier int) bool {
	// Promote smaller items more readily to higher tiers
	// This would need access to value size information
	return true // Simplified for now
}

func (r *SizeBasedRule) ShouldDemote(pattern *KeyPattern, currentTier int) bool {
	// Demote larger items from higher tiers
	// This would need access to value size information
	return false // Simplified for now
}

// NewTierOptimizer creates a new tier optimizer
func NewTierOptimizer(hybridCache *HybridCache, config *cachecore.HybridConfig) *TierOptimizer {
	return &TierOptimizer{
		hybridCache: hybridCache,
		config:      config,
		stats: &OptimizerStats{
			LastOptimization: time.Now(),
		},
	}
}

// Optimize performs tier optimization
func (to *TierOptimizer) Optimize(ctx context.Context) error {
	to.mu.Lock()
	defer to.mu.Unlock()

	start := time.Now()
	defer func() {
		to.stats.OptimizationTime = time.Since(start)
		to.stats.LastOptimization = time.Now()
		to.stats.TotalOptimizations++
	}()

	l := to.hybridCache.logger
	if l != nil {
		l.Debug("starting tier optimization", logger.String("cache", to.hybridCache.name))
	}

	// Get promotion candidates from each tier
	var totalPromotions, totalDemotions int64

	for tierLevel := 1; tierLevel < to.hybridCache.TierCount(); tierLevel++ {
		// Get candidates for promotion to higher tier
		promotionCandidates := to.getPromotionCandidates(tierLevel)
		for _, key := range promotionCandidates {
			if err := to.hybridCache.PromoteToTier(ctx, key, tierLevel-1); err != nil {
				if l != nil {
					l.Error("failed to promote key",
						logger.String("key", key),
						logger.Int("tier", tierLevel-1),
						logger.Error(err),
					)
				}
			} else {
				totalPromotions++
			}
		}

		// Get candidates for demotion to lower tier
		demotionCandidates := to.getDemotionCandidates(tierLevel)
		for _, key := range demotionCandidates {
			if err := to.hybridCache.DemoteFromTier(ctx, key, tierLevel); err != nil {
				if l != nil {
					l.Error("failed to demote key",
						logger.String("key", key),
						logger.Int("tier", tierLevel),
						logger.Error(err),
					)
				}
			} else {
				totalDemotions++
			}
		}
	}

	to.stats.KeysPromoted += totalPromotions
	to.stats.KeysDemoted += totalDemotions

	// Calculate efficiency gain (simplified)
	to.stats.EfficiencyGain = float64(totalPromotions-totalDemotions) / float64(totalPromotions+totalDemotions+1)

	if l != nil {
		l.Info("tier optimization completed",
			logger.String("cache", to.hybridCache.name),
			logger.Int64("promotions", totalPromotions),
			logger.Int64("demotions", totalDemotions),
			logger.Duration("duration", time.Since(start)),
		)
	}

	return nil
}

// GetStats returns optimizer statistics
func (to *TierOptimizer) GetStats() *OptimizerStats {
	to.mu.RLock()
	defer to.mu.RUnlock()

	// Return a copy
	return &OptimizerStats{
		TotalOptimizations: to.stats.TotalOptimizations,
		LastOptimization:   to.stats.LastOptimization,
		KeysPromoted:       to.stats.KeysPromoted,
		KeysDemoted:        to.stats.KeysDemoted,
		OptimizationTime:   to.stats.OptimizationTime,
		EfficiencyGain:     to.stats.EfficiencyGain,
	}
}

// getPromotionCandidates identifies keys that should be promoted from a tier
func (to *TierOptimizer) getPromotionCandidates(tier int) []string {
	// Get top accessed keys from this tier
	candidates := to.hybridCache.accessTracker.GetTopAccessedKeys(10)

	// Filter based on promotion rules
	promotionCandidates := make([]string, 0)
	for _, key := range candidates {
		pattern := to.hybridCache.accessTracker.GetAccessPattern(key)
		if to.hybridCache.promotionRules.ShouldPromote(pattern, tier) {
			promotionCandidates = append(promotionCandidates, key)
		}
	}

	return promotionCandidates
}

// getDemotionCandidates identifies keys that should be demoted from a tier
func (to *TierOptimizer) getDemotionCandidates(tier int) []string {
	// This would analyze the tier for infrequently accessed keys
	// For now, return empty slice
	return []string{}
}

// TierCoordinator manages coordination between L1 and L2 caches
type TierCoordinator struct {
	l1Cache cachecore.Cache
	l2Cache cachecore.Cache
	config  *cachecore.HybridConfig
	tracker *AccessTracker
	logger  common.Logger
	mu      sync.RWMutex
}

// NewTierCoordinator creates a new tier coordinator
func NewTierCoordinator(l1, l2 cachecore.Cache, config *cachecore.HybridConfig, tracker *AccessTracker, logger common.Logger) *TierCoordinator {
	return &TierCoordinator{
		l1Cache: l1,
		l2Cache: l2,
		config:  config,
		tracker: tracker,
		logger:  logger,
	}
}

// CoordinatedGet performs a coordinated get across tiers
func (tc *TierCoordinator) CoordinatedGet(ctx context.Context, key string) (interface{}, error) {
	// Try L1 first
	if value, err := tc.l1Cache.Get(ctx, key); err == nil {
		tc.tracker.RecordAccess(key, time.Now())
		tc.tracker.SetCurrentTier(key, 0)
		return value, nil
	}

	// Try L2
	if value, err := tc.l2Cache.Get(ctx, key); err == nil {
		tc.tracker.RecordAccess(key, time.Now())
		tc.tracker.SetCurrentTier(key, 1)

		// Consider promoting to L1
		pattern := tc.tracker.GetAccessPattern(key)
		if pattern.ShouldPromote {
			go tc.promoteToL1(ctx, key, value)
		}

		return value, nil
	}

	return nil, cachecore.ErrCacheNotFound
}

// CoordinatedSet performs a coordinated set across tiers
func (tc *TierCoordinator) CoordinatedSet(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	// Record access
	tc.tracker.RecordAccess(key, time.Now())

	// Determine initial placement
	tier := tc.determineInitialTier(key, value)

	if tier == 0 {
		// Store in L1
		if err := tc.l1Cache.Set(ctx, key, value, ttl); err != nil {
			return err
		}
		tc.tracker.SetCurrentTier(key, 0)

		// Replicate to L2 based on replication mode
		if tc.config.ReplicationMode == "sync" {
			return tc.l2Cache.Set(ctx, key, value, ttl)
		} else if tc.config.ReplicationMode == "async" {
			go tc.l2Cache.Set(ctx, key, value, ttl)
		}
	} else {
		// Store in L2
		if err := tc.l2Cache.Set(ctx, key, value, ttl); err != nil {
			return err
		}
		tc.tracker.SetCurrentTier(key, 1)
	}

	return nil
}

// CoordinatedDelete performs a coordinated delete across tiers
func (tc *TierCoordinator) CoordinatedDelete(ctx context.Context, key string) error {
	// Remove from tracker
	tc.tracker.RemoveKey(key)

	// Delete from both tiers
	var errors []error

	if err := tc.l1Cache.Delete(ctx, key); err != nil && !cachecore.IsNotFound(err) {
		errors = append(errors, err)
	}

	if err := tc.l2Cache.Delete(ctx, key); err != nil && !cachecore.IsNotFound(err) {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return errors[0]
	}

	return nil
}

// promoteToL1 promotes a key from L2 to L1
func (tc *TierCoordinator) promoteToL1(ctx context.Context, key string, value interface{}) {
	if err := tc.l1Cache.Set(ctx, key, value, 0); err != nil {
		if tc.logger != nil {
			tc.logger.Error("failed to promote key to L1",
				logger.String("key", key),
				logger.Error(err),
			)
		}
		return
	}

	tc.tracker.SetCurrentTier(key, 0)

	if tc.logger != nil {
		tc.logger.Debug("key promoted to L1", logger.String("key", key))
	}
}

// determineInitialTier determines which tier to initially place a key in
func (tc *TierCoordinator) determineInitialTier(key string, value interface{}) int {
	// Simple logic: new keys go to L1
	// In practice, this could consider factors like:
	// - Value size
	// - Key patterns
	// - Current L1 capacity
	// - Historical access patterns for similar keys

	return 0 // Default to L1
}

// RebalanceTiers performs rebalancing between tiers
func (tc *TierCoordinator) RebalanceTiers(ctx context.Context) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Get top accessed keys from L2 that should be in L1
	topKeys := tc.tracker.GetTopAccessedKeys(50)

	promoted := 0
	for _, key := range topKeys {
		pattern := tc.tracker.GetAccessPattern(key)
		if pattern.CurrentTier == 1 && pattern.ShouldPromote {
			// Try to get from L2 and promote to L1
			if value, err := tc.l2Cache.Get(ctx, key); err == nil {
				if err := tc.l1Cache.Set(ctx, key, value, 0); err == nil {
					tc.tracker.SetCurrentTier(key, 0)
					promoted++

					if tc.logger != nil {
						tc.logger.Debug("rebalanced key to L1", logger.String("key", key))
					}
				}
			}
		}
	}

	if tc.logger != nil {
		tc.logger.Info("tier rebalancing completed",
			logger.Int("promoted", promoted),
		)
	}

	return nil
}
