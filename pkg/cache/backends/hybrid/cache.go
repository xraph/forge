package hybrid

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// HybridCache implements a multi-tier cache with intelligent promotion/demotion
type HybridCache struct {
	name    string
	config  *cachecore.HybridConfig
	logger  common.Logger
	metrics common.Metrics

	// Cache tiers (L1, L2, L3)
	tiers       []cachecore.CacheBackend
	tierNames   []string
	tierConfigs []*cachecore.CacheBackendConfig

	// Promotion/Demotion tracking
	accessTracker  *AccessTracker
	promotionRules *PromotionManager

	// Statistics
	stats     *HybridStats
	startTime time.Time
	running   bool
	mu        sync.RWMutex

	// Background optimization
	optimizer     *TierOptimizer
	stopOptimizer chan struct{}
}

// HybridStats tracks statistics for the hybrid cache
type HybridStats struct {
	// Global stats
	hits       int64
	misses     int64
	sets       int64
	deletes    int64
	promotions int64
	demotions  int64

	// Per-tier stats
	tierStats []TierStats

	mu sync.RWMutex
}

// TierStats tracks statistics for a single tier
type TierStats struct {
	Level       int                  `json:"level"`
	Name        string               `json:"name"`
	Hits        int64                `json:"hits"`
	Misses      int64                `json:"misses"`
	Sets        int64                `json:"sets"`
	Deletes     int64                `json:"deletes"`
	Promotions  int64                `json:"promotions"`
	Demotions   int64                `json:"demotions"`
	Size        int64                `json:"size"`
	HitRate     float64              `json:"hit_rate"`
	MemoryUsage int64                `json:"memory_usage"`
	Backend     cachecore.CacheStats `json:"backend"`
}

// AccessPattern represents access patterns for a key
type AccessPattern struct {
	Key             string      `json:"key"`
	TotalAccesses   int64       `json:"total_accesses"`
	RecentAccesses  []time.Time `json:"recent_accesses"`
	AccessFrequency float64     `json:"access_frequency"`
	LastAccessed    time.Time   `json:"last_accessed"`
	CurrentTier     int         `json:"current_tier"`
	ShouldPromote   bool        `json:"should_promote"`
	ShouldDemote    bool        `json:"should_demote"`
	Score           float64     `json:"score"`
}

// NewHybridCache creates a new hybrid cache
func NewHybridCache(name string, config *cachecore.HybridConfig, l common.Logger, metrics common.Metrics) (*HybridCache, error) {
	if config == nil {
		return nil, fmt.Errorf("hybrid cache config cannot be nil")
	}

	cache := &HybridCache{
		name:          name,
		config:        config,
		logger:        l,
		metrics:       metrics,
		tiers:         make([]cachecore.CacheBackend, 0, 3),
		tierNames:     make([]string, 0, 3),
		tierConfigs:   make([]*cachecore.CacheBackendConfig, 0, 3),
		accessTracker: NewAccessTracker(),
		stats:         &HybridStats{},
		startTime:     time.Now(),
		stopOptimizer: make(chan struct{}),
	}

	// Initialize tiers
	if err := cache.initializeTiers(); err != nil {
		return nil, fmt.Errorf("failed to initialize tiers: %w", err)
	}

	// Initialize promotion rules
	cache.promotionRules = NewPromotionManager(config, cache.accessTracker)

	// Initialize optimizer
	cache.optimizer = NewTierOptimizer(cache, config)

	// Initialize tier stats
	cache.stats.tierStats = make([]TierStats, len(cache.tiers))
	for i := range cache.stats.tierStats {
		cache.stats.tierStats[i] = TierStats{
			Level: i,
			Name:  cache.tierNames[i],
		}
	}

	if l != nil {
		l.Info("hybrid cache created",
			logger.String("name", name),
			logger.Int("tiers", len(cache.tiers)),
			logger.String("consistency", string(config.ConsistencyLevel)),
		)
	}

	return cache, nil
}

// Type returns the cache type
func (hc *HybridCache) Type() cachecore.CacheType {
	return cachecore.CacheTypeHybrid
}

// Name returns the cache name
func (hc *HybridCache) Name() string {
	return hc.name
}

// Configure configures the cache
func (hc *HybridCache) Configure(config interface{}) error {
	hybridConfig, ok := config.(*cachecore.HybridConfig)
	if !ok {
		return fmt.Errorf("invalid config type for hybrid cache")
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.config = hybridConfig
	return nil
}

// Start starts the cache
func (hc *HybridCache) Start(ctx context.Context) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.running {
		return nil
	}

	// OnStart all tiers
	for i, tier := range hc.tiers {
		if err := tier.Start(ctx); err != nil {
			return fmt.Errorf("failed to start tier %d: %w", i, err)
		}
	}

	// OnStart background optimizer
	if hc.config.AutoOptimize {
		go hc.runOptimizer()
	}

	hc.running = true

	if hc.logger != nil {
		hc.logger.Info("hybrid cache started", logger.String("name", hc.name))
	}

	return nil
}

// Stop stops the cache
func (hc *HybridCache) Stop(ctx context.Context) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.running {
		return nil
	}

	// OnStop optimizer
	close(hc.stopOptimizer)

	// OnStop all tiers
	for i, tier := range hc.tiers {
		if err := tier.Stop(ctx); err != nil {
			if hc.logger != nil {
				hc.logger.Error("failed to stop tier",
					logger.Int("tier", i),
					logger.Error(err),
				)
			}
		}
	}

	hc.running = false

	if hc.logger != nil {
		hc.logger.Info("hybrid cache stopped", logger.String("name", hc.name))
	}

	return nil
}

// Get retrieves a value from the cache
func (hc *HybridCache) Get(ctx context.Context, key string) (interface{}, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		return nil, err
	}

	start := time.Now()
	defer func() {
		if hc.metrics != nil {
			hc.metrics.Histogram("cache.get_duration", "cache", hc.name).Observe(time.Since(start).Seconds())
		}
	}()

	// Track access
	hc.accessTracker.RecordAccess(key, time.Now())

	// Try each tier in order (L1 -> L2 -> L3)
	for tierLevel, tier := range hc.tiers {
		value, err := tier.Get(ctx, key)
		if err == nil {
			// Found in this tier
			hc.recordTierHit(tierLevel)

			// Consider promoting to higher tier if not in L1
			if tierLevel > 0 {
				if hc.shouldPromote(key, tierLevel) {
					go hc.promoteKey(ctx, key, value, tierLevel-1)
				}
			}

			// Ensure key exists in lower tiers (replication)
			if hc.config.ReplicationMode == "sync" {
				go hc.replicateToLowerTiers(ctx, key, value, tierLevel)
			}

			return value, nil
		}

		// Record miss for this tier
		hc.recordTierMiss(tierLevel)
	}

	// Not found in any tier
	hc.recordMiss()
	return nil, cachecore.ErrCacheNotFound
}

// Set stores a value in the cache
func (hc *HybridCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	if err := cachecore.ValidateKey(key); err != nil {
		return err
	}

	if err := cachecore.ValidateTTL(ttl); err != nil {
		return err
	}

	start := time.Now()
	defer func() {
		if hc.metrics != nil {
			hc.metrics.Histogram("cache.set_duration", "cache", hc.name).Observe(time.Since(start).Seconds())
		}
	}()

	// Track access
	hc.accessTracker.RecordAccess(key, time.Now())

	// Determine which tier to store in initially
	targetTier := hc.determineInitialTier(key, value)

	// Store in target tier
	if err := hc.tiers[targetTier].Set(ctx, key, value, ttl); err != nil {
		return err
	}

	hc.recordTierSet(targetTier)

	// Handle replication based on consistency level
	switch hc.config.ConsistencyLevel {
	case cachecore.ConsistencyLevelStrong:
		// Synchronously replicate to all tiers
		return hc.replicateToAllTiers(ctx, key, value, ttl, targetTier)
	case cachecore.ConsistencyLevelEventual:
		// Asynchronously replicate
		go hc.replicateToAllTiers(ctx, key, value, ttl, targetTier)
	case cachecore.ConsistencyLevelWeakly:
		// No replication
	}

	hc.recordSet()
	return nil
}

// Delete removes a value from the cache
func (hc *HybridCache) Delete(ctx context.Context, key string) error {
	if err := cachecore.ValidateKey(key); err != nil {
		return err
	}

	start := time.Now()
	defer func() {
		if hc.metrics != nil {
			hc.metrics.Histogram("cache.delete_duration", "cache", hc.name).Observe(time.Since(start).Seconds())
		}
	}()

	// Remove from access tracker
	hc.accessTracker.RemoveKey(key)

	// Delete from all tiers
	var errors []error
	for tierLevel, tier := range hc.tiers {
		if err := tier.Delete(ctx, key); err != nil && !cachecore.IsNotFound(err) {
			errors = append(errors, err)
		} else {
			hc.recordTierDelete(tierLevel)
		}
	}

	if len(errors) > 0 {
		return errors[0] // Return first error
	}

	hc.recordDelete()
	return nil
}

// Exists checks if a key exists in any tier
func (hc *HybridCache) Exists(ctx context.Context, key string) (bool, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		return false, err
	}

	// Check each tier
	for _, tier := range hc.tiers {
		exists, err := tier.Exists(ctx, key)
		if err != nil {
			continue
		}
		if exists {
			return true, nil
		}
	}

	return false, nil
}

// GetMulti retrieves multiple values
func (hc *HybridCache) GetMulti(ctx context.Context, keys []string) (map[string]interface{}, error) {
	if len(keys) == 0 {
		return make(map[string]interface{}), nil
	}

	result := make(map[string]interface{})
	remaining := make([]string, 0, len(keys))

	// Try each tier in order
	for tierLevel, tier := range hc.tiers {
		if len(remaining) == 0 {
			// Check all keys initially, or remaining keys for subsequent tiers
			if tierLevel == 0 {
				remaining = keys
			}
		}

		if len(remaining) == 0 {
			break
		}

		tierResult, err := tier.GetMulti(ctx, remaining)
		if err != nil {
			continue
		}

		// Add found items to result and remove from remaining
		newRemaining := make([]string, 0, len(remaining))
		for _, key := range remaining {
			if value, found := tierResult[key]; found {
				result[key] = value
				hc.recordTierHit(tierLevel)

				// Track access
				hc.accessTracker.RecordAccess(key, time.Now())

				// Consider promotion
				if tierLevel > 0 && hc.shouldPromote(key, tierLevel) {
					go hc.promoteKey(ctx, key, value, tierLevel-1)
				}
			} else {
				newRemaining = append(newRemaining, key)
				hc.recordTierMiss(tierLevel)
			}
		}
		remaining = newRemaining
	}

	// Record overall misses for keys not found in any tier
	for _, key := range remaining {
		result[key] = nil
		hc.recordMiss()
	}

	return result, nil
}

// SetMulti stores multiple values
func (hc *HybridCache) SetMulti(ctx context.Context, items map[string]cachecore.CacheItem) error {
	for key, item := range items {
		if err := hc.Set(ctx, key, item.Value, item.TTL); err != nil {
			return err
		}
	}
	return nil
}

// DeleteMulti removes multiple values
func (hc *HybridCache) DeleteMulti(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := hc.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// Increment increments a numeric value
func (hc *HybridCache) Increment(ctx context.Context, key string, delta int64) (int64, error) {
	// Try L1 first for fastest access
	if len(hc.tiers) > 0 {
		if result, err := hc.tiers[0].Increment(ctx, key, delta); err == nil {
			// Propagate to other tiers asynchronously
			go hc.propagateIncrement(ctx, key, delta, 0)
			return result, nil
		}
	}

	// Fallback to Set operation
	current, err := hc.Get(ctx, key)
	if err != nil && !cachecore.IsNotFound(err) {
		return 0, err
	}

	var newValue int64 = delta
	if current != nil {
		if currentInt, ok := current.(int64); ok {
			newValue = currentInt + delta
		} else {
			return 0, cachecore.NewCacheError("INVALID_VALUE", "value is not numeric", key, "incr", nil)
		}
	}

	if err := hc.Set(ctx, key, newValue, 0); err != nil {
		return 0, err
	}

	return newValue, nil
}

// Decrement decrements a numeric value
func (hc *HybridCache) Decrement(ctx context.Context, key string, delta int64) (int64, error) {
	return hc.Increment(ctx, key, -delta)
}

// Touch updates the TTL of a key
func (hc *HybridCache) Touch(ctx context.Context, key string, ttl time.Duration) error {
	if err := cachecore.ValidateKey(key); err != nil {
		return err
	}

	if err := cachecore.ValidateTTL(ttl); err != nil {
		return err
	}

	// Update TTL in all tiers where the key exists
	var errors []error
	for _, tier := range hc.tiers {
		if err := tier.Touch(ctx, key, ttl); err != nil && !cachecore.IsNotFound(err) {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return errors[0]
	}

	return nil
}

// TTL returns the TTL of a key from the highest tier
func (hc *HybridCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		return 0, err
	}

	// Check each tier
	for _, tier := range hc.tiers {
		if ttl, err := tier.TTL(ctx, key); err == nil {
			return ttl, nil
		}
	}

	return 0, cachecore.ErrCacheNotFound
}

// Keys returns keys matching a pattern from all tiers
func (hc *HybridCache) Keys(ctx context.Context, pattern string) ([]string, error) {
	keySet := make(map[string]bool)

	// Collect keys from all tiers
	for _, tier := range hc.tiers {
		keys, err := tier.Keys(ctx, pattern)
		if err != nil {
			continue
		}
		for _, key := range keys {
			keySet[key] = true
		}
	}

	// Convert to slice
	result := make([]string, 0, len(keySet))
	for key := range keySet {
		result = append(result, key)
	}

	return result, nil
}

// DeletePattern deletes keys matching a pattern
func (hc *HybridCache) DeletePattern(ctx context.Context, pattern string) error {
	keys, err := hc.Keys(ctx, pattern)
	if err != nil {
		return err
	}

	return hc.DeleteMulti(ctx, keys)
}

// Flush clears all tiers
func (hc *HybridCache) Flush(ctx context.Context) error {
	var errors []error

	for i, tier := range hc.tiers {
		if err := tier.Flush(ctx); err != nil {
			errors = append(errors, fmt.Errorf("tier %d: %w", i, err))
		}
	}

	// Clear access tracker
	hc.accessTracker.Clear()

	// Reset stats
	hc.stats.mu.Lock()
	hc.stats.hits = 0
	hc.stats.misses = 0
	hc.stats.sets = 0
	hc.stats.deletes = 0
	hc.stats.promotions = 0
	hc.stats.demotions = 0
	for i := range hc.stats.tierStats {
		hc.stats.tierStats[i] = TierStats{
			Level: i,
			Name:  hc.tierNames[i],
		}
	}
	hc.stats.mu.Unlock()

	if len(errors) > 0 {
		return errors[0]
	}

	return nil
}

// Size returns the total size across all tiers
func (hc *HybridCache) Size(ctx context.Context) (int64, error) {
	var total int64

	for _, tier := range hc.tiers {
		size, err := tier.Size(ctx)
		if err != nil {
			continue
		}
		total += size
	}

	return total, nil
}

// Close closes the cache
func (hc *HybridCache) Close() error {
	return hc.Stop(context.Background())
}

// Stats returns cache statistics
func (hc *HybridCache) Stats() cachecore.CacheStats {
	hc.stats.mu.RLock()
	defer hc.stats.mu.RUnlock()

	// Update tier stats
	for i, tier := range hc.tiers {
		backendStats := tier.Stats()
		hc.stats.tierStats[i].Backend = backendStats
		hc.stats.tierStats[i].Size = backendStats.Size
		hc.stats.tierStats[i].MemoryUsage = backendStats.Memory
		hc.stats.tierStats[i].HitRate = cachecore.CalculateHitRatio(
			hc.stats.tierStats[i].Hits,
			hc.stats.tierStats[i].Misses,
		)
	}

	return cachecore.CacheStats{
		Name:     hc.name,
		Type:     string(cachecore.CacheTypeHybrid),
		Hits:     hc.stats.hits,
		Misses:   hc.stats.misses,
		Sets:     hc.stats.sets,
		Deletes:  hc.stats.deletes,
		HitRatio: cachecore.CalculateHitRatio(hc.stats.hits, hc.stats.misses),
		Uptime:   time.Since(hc.startTime),
		Custom: map[string]interface{}{
			"tiers":       len(hc.tiers),
			"promotions":  hc.stats.promotions,
			"demotions":   hc.stats.demotions,
			"tier_stats":  hc.stats.tierStats,
			"consistency": string(hc.config.ConsistencyLevel),
		},
	}
}

// HealthCheck performs a health check
func (hc *HybridCache) HealthCheck(ctx context.Context) error {
	if !hc.running {
		return fmt.Errorf("hybrid cache is not running")
	}

	// Check all tiers
	for i, tier := range hc.tiers {
		if err := tier.HealthCheck(ctx); err != nil {
			return fmt.Errorf("tier %d unhealthy: %w", i, err)
		}
	}

	return nil
}

// Multi-tier specific methods

// GetTier returns a specific tier
func (hc *HybridCache) GetTier(level int) (cachecore.Cache, error) {
	if level < 0 || level >= len(hc.tiers) {
		return nil, fmt.Errorf("invalid tier level: %d", level)
	}
	return hc.tiers[level], nil
}

// PromoteToTier promotes a key to a specific tier
func (hc *HybridCache) PromoteToTier(ctx context.Context, key string, targetTier int) error {
	if targetTier < 0 || targetTier >= len(hc.tiers) {
		return fmt.Errorf("invalid target tier: %d", targetTier)
	}

	// Find the key in lower tiers
	for tier := targetTier + 1; tier < len(hc.tiers); tier++ {
		value, err := hc.tiers[tier].Get(ctx, key)
		if err == nil {
			// Found the key, promote it
			return hc.promoteKey(ctx, key, value, targetTier)
		}
	}

	return cachecore.ErrCacheNotFound
}

// DemoteFromTier demotes a key from a specific tier
func (hc *HybridCache) DemoteFromTier(ctx context.Context, key string, sourceTier int) error {
	if sourceTier < 0 || sourceTier >= len(hc.tiers) {
		return fmt.Errorf("invalid source tier: %d", sourceTier)
	}

	if sourceTier == len(hc.tiers)-1 {
		// Already in lowest tier, just delete
		return hc.tiers[sourceTier].Delete(ctx, key)
	}

	// Get value from source tier
	value, err := hc.tiers[sourceTier].Get(ctx, key)
	if err != nil {
		return err
	}

	// Move to next lower tier
	targetTier := sourceTier + 1
	if err := hc.tiers[targetTier].Set(ctx, key, value, 0); err != nil {
		return err
	}

	// Remove from source tier
	if err := hc.tiers[sourceTier].Delete(ctx, key); err != nil {
		return err
	}

	hc.recordDemotion()
	return nil
}

// TierCount returns the number of tiers
func (hc *HybridCache) TierCount() int {
	return len(hc.tiers)
}

// TierStats returns statistics for a specific tier
func (hc *HybridCache) TierStats(level int) cachecore.CacheStats {
	if level < 0 || level >= len(hc.tiers) {
		return cachecore.CacheStats{}
	}

	hc.stats.mu.RLock()
	defer hc.stats.mu.RUnlock()

	return hc.stats.tierStats[level].Backend
}

// OptimizeTiers performs tier optimization
func (hc *HybridCache) OptimizeTiers(ctx context.Context) error {
	return hc.optimizer.Optimize(ctx)
}

// GetPromotionCandidates returns keys that should be promoted
func (hc *HybridCache) GetPromotionCandidates(tier int) []string {
	return hc.promotionRules.GetPromotionCandidates(tier)
}

// GetDemotionCandidates returns keys that should be demoted
func (hc *HybridCache) GetDemotionCandidates(tier int) []string {
	return hc.promotionRules.GetDemotionCandidates(tier)
}

// Helper methods

func (hc *HybridCache) initializeTiers() error {
	// Initialize L1 cache
	if hc.config.L1Cache.Enabled {
		l1Cache, err := hc.createTierCache("l1", &hc.config.L1Cache)
		if err != nil {
			return fmt.Errorf("failed to create L1 cache: %w", err)
		}
		hc.tiers = append(hc.tiers, l1Cache)
		hc.tierNames = append(hc.tierNames, "L1")
		hc.tierConfigs = append(hc.tierConfigs, &hc.config.L1Cache)
	}

	// Initialize L2 cache
	if hc.config.L2Cache.Enabled {
		l2Cache, err := hc.createTierCache("l2", &hc.config.L2Cache)
		if err != nil {
			return fmt.Errorf("failed to create L2 cache: %w", err)
		}
		hc.tiers = append(hc.tiers, l2Cache)
		hc.tierNames = append(hc.tierNames, "L2")
		hc.tierConfigs = append(hc.tierConfigs, &hc.config.L2Cache)
	}

	// Initialize L3 cache
	if hc.config.L3Cache.Enabled {
		l3Cache, err := hc.createTierCache("l3", &hc.config.L3Cache)
		if err != nil {
			return fmt.Errorf("failed to create L3 cache: %w", err)
		}
		hc.tiers = append(hc.tiers, l3Cache)
		hc.tierNames = append(hc.tierNames, "L3")
		hc.tierConfigs = append(hc.tierConfigs, &hc.config.L3Cache)
	}

	if len(hc.tiers) == 0 {
		return fmt.Errorf("no tiers configured")
	}

	return nil
}

func (hc *HybridCache) createTierCache(tierName string, config *cachecore.CacheBackendConfig) (cachecore.CacheBackend, error) {
	// This would typically use a cache factory to create the appropriate cache type
	// For now, return a placeholder
	return nil, fmt.Errorf("cache factory not implemented")
}

func (hc *HybridCache) shouldPromote(key string, currentTier int) bool {
	pattern := hc.accessTracker.GetAccessPattern(key)
	return hc.promotionRules.ShouldPromote(pattern, currentTier)
}

func (hc *HybridCache) promoteKey(ctx context.Context, key string, value interface{}, targetTier int) error {
	if targetTier < 0 || targetTier >= len(hc.tiers) {
		return fmt.Errorf("invalid target tier: %d", targetTier)
	}

	// Set in target tier
	if err := hc.tiers[targetTier].Set(ctx, key, value, 0); err != nil {
		return err
	}

	hc.recordPromotion()

	if hc.logger != nil {
		hc.logger.Debug("key promoted",
			logger.String("key", key),
			logger.Int("target_tier", targetTier),
		)
	}

	return nil
}

func (hc *HybridCache) replicateToLowerTiers(ctx context.Context, key string, value interface{}, sourceTier int) {
	for tier := sourceTier + 1; tier < len(hc.tiers); tier++ {
		if err := hc.tiers[tier].Set(ctx, key, value, 0); err != nil {
			if hc.logger != nil {
				hc.logger.Error("failed to replicate to lower tier",
					logger.String("key", key),
					logger.Int("tier", tier),
					logger.Error(err),
				)
			}
		}
	}
}

func (hc *HybridCache) replicateToAllTiers(ctx context.Context, key string, value interface{}, ttl time.Duration, sourceTier int) error {
	var errors []error

	for i, tier := range hc.tiers {
		if i == sourceTier {
			continue // Skip source tier
		}

		if err := tier.Set(ctx, key, value, ttl); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return errors[0]
	}

	return nil
}

func (hc *HybridCache) propagateIncrement(ctx context.Context, key string, delta int64, sourceTier int) {
	for i, tier := range hc.tiers {
		if i == sourceTier {
			continue
		}

		if _, err := tier.Increment(ctx, key, delta); err != nil {
			if hc.logger != nil {
				hc.logger.Error("failed to propagate increment",
					logger.String("key", key),
					logger.Int("tier", i),
					logger.Error(err),
				)
			}
		}
	}
}

func (hc *HybridCache) determineInitialTier(key string, value interface{}) int {
	// Simple logic: start with L1 for new keys
	// In practice, this could be more sophisticated based on value size, type, etc.
	return 0
}

func (hc *HybridCache) runOptimizer() {
	ticker := time.NewTicker(hc.config.OptimizeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hc.stopOptimizer:
			return
		case <-ticker.C:
			if err := hc.optimizer.Optimize(context.Background()); err != nil {
				if hc.logger != nil {
					hc.logger.Error("tier optimization failed", logger.Error(err))
				}
			}
		}
	}
}

// Statistics recording methods

func (hc *HybridCache) recordHit() {
	atomic.AddInt64(&hc.stats.hits, 1)
	if hc.metrics != nil {
		hc.metrics.Counter("cache.hits", "cache", hc.name).Inc()
	}
}

func (hc *HybridCache) recordMiss() {
	atomic.AddInt64(&hc.stats.misses, 1)
	if hc.metrics != nil {
		hc.metrics.Counter("cache.misses", "cache", hc.name).Inc()
	}
}

func (hc *HybridCache) recordSet() {
	atomic.AddInt64(&hc.stats.sets, 1)
	if hc.metrics != nil {
		hc.metrics.Counter("cache.sets", "cache", hc.name).Inc()
	}
}

func (hc *HybridCache) recordDelete() {
	atomic.AddInt64(&hc.stats.deletes, 1)
	if hc.metrics != nil {
		hc.metrics.Counter("cache.deletes", "cache", hc.name).Inc()
	}
}

func (hc *HybridCache) recordPromotion() {
	atomic.AddInt64(&hc.stats.promotions, 1)
	if hc.metrics != nil {
		hc.metrics.Counter("cache.promotions", "cache", hc.name).Inc()
	}
}

func (hc *HybridCache) recordDemotion() {
	atomic.AddInt64(&hc.stats.demotions, 1)
	if hc.metrics != nil {
		hc.metrics.Counter("cache.demotions", "cache", hc.name).Inc()
	}
}

func (hc *HybridCache) recordTierHit(tier int) {
	if tier >= 0 && tier < len(hc.stats.tierStats) {
		atomic.AddInt64(&hc.stats.tierStats[tier].Hits, 1)
		if hc.metrics != nil {
			hc.metrics.Counter("cache.tier_hits", "cache", hc.name, "tier", fmt.Sprintf("L%d", tier+1)).Inc()
		}
	}
	hc.recordHit()
}

func (hc *HybridCache) recordTierMiss(tier int) {
	if tier >= 0 && tier < len(hc.stats.tierStats) {
		atomic.AddInt64(&hc.stats.tierStats[tier].Misses, 1)
		if hc.metrics != nil {
			hc.metrics.Counter("cache.tier_misses", "cache", hc.name, "tier", fmt.Sprintf("L%d", tier+1)).Inc()
		}
	}
}

func (hc *HybridCache) recordTierSet(tier int) {
	if tier >= 0 && tier < len(hc.stats.tierStats) {
		atomic.AddInt64(&hc.stats.tierStats[tier].Sets, 1)
		if hc.metrics != nil {
			hc.metrics.Counter("cache.tier_sets", "cache", hc.name, "tier", fmt.Sprintf("L%d", tier+1)).Inc()
		}
	}
}

func (hc *HybridCache) recordTierDelete(tier int) {
	if tier >= 0 && tier < len(hc.stats.tierStats) {
		atomic.AddInt64(&hc.stats.tierStats[tier].Deletes, 1)
		if hc.metrics != nil {
			hc.metrics.Counter("cache.tier_deletes", "cache", hc.name, "tier", fmt.Sprintf("L%d", tier+1)).Inc()
		}
	}
}
