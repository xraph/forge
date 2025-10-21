package agents

import (
	"context"
	"fmt"
	"reflect"
	"time"

	ai "github.com/xraph/forge/v2/extensions/ai/internal"
	"github.com/xraph/forge/v2/internal/logger"
)

// CacheAgent optimizes caching strategies and performance
type CacheAgent struct {
	*ai.BaseAgent
	cacheManager      interface{} // Cache manager from Phase 4
	hitRateThreshold  float64
	evictionPolicy    string
	warmupStrategies  []WarmupStrategy
	optimizationStats CacheOptimizationStats
}

// WarmupStrategy defines cache warming strategies
type WarmupStrategy struct {
	Name        string                 `json:"name"`
	Priority    int                    `json:"priority"`
	Condition   string                 `json:"condition"`
	Parameters  map[string]interface{} `json:"parameters"`
	Enabled     bool                   `json:"enabled"`
	LastUsed    time.Time              `json:"last_used"`
	SuccessRate float64                `json:"success_rate"`
}

// CacheOptimizationStats tracks cache optimization metrics
type CacheOptimizationStats struct {
	HitRateImprovement    float64   `json:"hit_rate_improvement"`
	EvictionOptimizations int64     `json:"eviction_optimizations"`
	WarmupOperations      int64     `json:"warmup_operations"`
	StorageOptimizations  int64     `json:"storage_optimizations"`
	LastOptimization      time.Time `json:"last_optimization"`
	TotalSavings          float64   `json:"total_savings"` // In cost or time
}

// CacheInput represents cache optimization input
type CacheInput struct {
	CacheMetrics      CacheMetrics        `json:"cache_metrics"`
	AccessPatterns    []AccessPattern     `json:"access_patterns"`
	StorageMetrics    StorageMetrics      `json:"storage_metrics"`
	PerformanceData   PerformanceData     `json:"performance_data"`
	UserBehavior      UserBehaviorMetrics `json:"user_behavior"`
	TimeContext       TimeContext         `json:"time_context"`
	ResourceUsage     ResourceUsage       `json:"resource_usage"`
	OptimizationGoals []OptimizationGoal  `json:"optimization_goals"`
}

// CacheMetrics contains cache performance metrics
type CacheMetrics struct {
	HitRate          float64          `json:"hit_rate"`
	MissRate         float64          `json:"miss_rate"`
	EvictionRate     float64          `json:"eviction_rate"`
	FillRate         float64          `json:"fill_rate"`
	AverageLatency   time.Duration    `json:"average_latency"`
	KeyDistribution  map[string]int64 `json:"key_distribution"`
	SizeDistribution map[string]int64 `json:"size_distribution"`
	TTLDistribution  map[string]int64 `json:"ttl_distribution"`
}

// AccessPattern represents cache access patterns
type AccessPattern struct {
	Pattern        string        `json:"pattern"`
	Frequency      int64         `json:"frequency"`
	Locality       string        `json:"locality"` // temporal, spatial
	Predictability float64       `json:"predictability"`
	TimeWindow     time.Duration `json:"time_window"`
	Keys           []string      `json:"keys"`
}

// StorageMetrics contains storage-related metrics
type StorageMetrics struct {
	TotalSize        int64         `json:"total_size"`
	UsedSize         int64         `json:"used_size"`
	Fragmentation    float64       `json:"fragmentation"`
	CompressionRatio float64       `json:"compression_ratio"`
	IOLatency        time.Duration `json:"io_latency"`
	Throughput       int64         `json:"throughput"`
}

// PerformanceData contains performance metrics
type PerformanceData struct {
	ResponseTime   time.Duration `json:"response_time"`
	Throughput     int64         `json:"throughput"`
	CPUUsage       float64       `json:"cpu_usage"`
	MemoryUsage    float64       `json:"memory_usage"`
	NetworkLatency time.Duration `json:"network_latency"`
	ErrorRate      float64       `json:"error_rate"`
}

// UserBehaviorMetrics contains user behavior analysis
type UserBehaviorMetrics struct {
	ActiveUsers     int64                  `json:"active_users"`
	SessionLength   time.Duration          `json:"session_length"`
	RequestPatterns []UserRequestPattern   `json:"request_patterns"`
	Preferences     map[string]interface{} `json:"preferences"`
}

// UserRequestPattern represents user request patterns
type UserRequestPattern struct {
	UserID    string      `json:"user_id"`
	Requests  []string    `json:"requests"`
	Frequency int64       `json:"frequency"`
	Timing    []time.Time `json:"timing"`
	Locality  string      `json:"locality"`
}

// TimeContext provides temporal context for optimization
type TimeContext struct {
	CurrentTime  time.Time `json:"current_time"`
	DayOfWeek    string    `json:"day_of_week"`
	TimeOfDay    string    `json:"time_of_day"`
	Season       string    `json:"season"`
	IsHoliday    bool      `json:"is_holiday"`
	EventContext string    `json:"event_context"`
}

// ResourceUsage tracks resource utilization
type ResourceUsage struct {
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	Disk    float64 `json:"disk"`
	Network float64 `json:"network"`
	Cost    float64 `json:"cost"`
}

// OptimizationGoal defines optimization objectives
type OptimizationGoal struct {
	Type     string  `json:"type"` // hit_rate, latency, cost, storage
	Target   float64 `json:"target"`
	Priority int     `json:"priority"`
	Weight   float64 `json:"weight"`
}

// CacheOutput represents cache optimization output
type CacheOutput struct {
	Recommendations  []CacheRecommendation `json:"recommendations"`
	WarmupPlan       WarmupPlan            `json:"warmup_plan"`
	EvictionStrategy EvictionStrategy      `json:"eviction_strategy"`
	SizingAdvice     SizingAdvice          `json:"sizing_advice"`
	PredictedImpact  PredictedImpact       `json:"predicted_impact"`
	Actions          []CacheAction         `json:"actions"`
}

// CacheRecommendation contains cache optimization recommendations
type CacheRecommendation struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Impact      string                 `json:"impact"`
	Confidence  float64                `json:"confidence"`
	Priority    int                    `json:"priority"`
	Parameters  map[string]interface{} `json:"parameters"`
	Rationale   string                 `json:"rationale"`
}

// WarmupPlan defines cache warming strategy
type WarmupPlan struct {
	Strategy    string                 `json:"strategy"`
	Keys        []string               `json:"keys"`
	Priority    []int                  `json:"priority"`
	Schedule    []time.Time            `json:"schedule"`
	BatchSize   int                    `json:"batch_size"`
	Parallelism int                    `json:"parallelism"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// EvictionStrategy defines cache eviction strategy
type EvictionStrategy struct {
	Policy     string                 `json:"policy"`
	Parameters map[string]interface{} `json:"parameters"`
	Thresholds map[string]float64     `json:"thresholds"`
	Conditions []string               `json:"conditions"`
	Scheduling string                 `json:"scheduling"`
}

// SizingAdvice provides cache sizing recommendations
type SizingAdvice struct {
	RecommendedSize int64                  `json:"recommended_size"`
	MinSize         int64                  `json:"min_size"`
	MaxSize         int64                  `json:"max_size"`
	OptimalRatio    float64                `json:"optimal_ratio"`
	Partitioning    map[string]int64       `json:"partitioning"`
	Rationale       string                 `json:"rationale"`
	Parameters      map[string]interface{} `json:"parameters"`
}

// PredictedImpact shows expected optimization impact
type PredictedImpact struct {
	HitRateImprovement  float64       `json:"hit_rate_improvement"`
	LatencyReduction    time.Duration `json:"latency_reduction"`
	CostSavings         float64       `json:"cost_savings"`
	StorageOptimization float64       `json:"storage_optimization"`
	Confidence          float64       `json:"confidence"`
}

// CacheAction represents a cache optimization action
type CacheAction struct {
	Type       string                 `json:"type"`
	Target     string                 `json:"target"`
	Parameters map[string]interface{} `json:"parameters"`
	Priority   int                    `json:"priority"`
	Condition  string                 `json:"condition"`
	Timeout    time.Duration          `json:"timeout"`
	Rollback   bool                   `json:"rollback"`
}

// NewCacheAgent creates a new cache optimization agent
func NewCacheAgent() ai.AIAgent {
	capabilities := []ai.Capability{
		{
			Name:        "predictive-warming",
			Description: "Predict and warm cache entries before they're needed",
			InputType:   reflect.TypeOf(CacheInput{}),
			OutputType:  reflect.TypeOf(CacheOutput{}),
			Metadata: map[string]interface{}{
				"accuracy": 0.85,
				"latency":  "50ms",
			},
		},
		{
			Name:        "intelligent-eviction",
			Description: "Optimize cache eviction policies based on access patterns",
			InputType:   reflect.TypeOf(CacheInput{}),
			OutputType:  reflect.TypeOf(CacheOutput{}),
			Metadata: map[string]interface{}{
				"efficiency":   0.92,
				"adaptability": "high",
			},
		},
		{
			Name:        "hit-rate-optimization",
			Description: "Optimize cache hit rates through intelligent caching strategies",
			InputType:   reflect.TypeOf(CacheInput{}),
			OutputType:  reflect.TypeOf(CacheOutput{}),
			Metadata: map[string]interface{}{
				"improvement": 0.25,
				"consistency": "high",
			},
		},
		{
			Name:        "storage-optimization",
			Description: "Optimize cache storage utilization and costs",
			InputType:   reflect.TypeOf(CacheInput{}),
			OutputType:  reflect.TypeOf(CacheOutput{}),
			Metadata: map[string]interface{}{
				"savings":     0.30,
				"reliability": "high",
			},
		},
	}

	baseAgent := ai.NewBaseAgent("cache-optimizer", "Cache Optimization Agent", ai.AgentTypeCacheManager, capabilities)

	return &CacheAgent{
		BaseAgent:         baseAgent,
		hitRateThreshold:  0.80,
		evictionPolicy:    "intelligent-lru",
		warmupStrategies:  []WarmupStrategy{},
		optimizationStats: CacheOptimizationStats{},
	}
}

// Initialize initializes the cache agent
func (a *CacheAgent) Initialize(ctx context.Context, config ai.AgentConfig) error {
	if err := a.BaseAgent.Initialize(ctx, config); err != nil {
		return err
	}

	// Initialize cache-specific configuration
	if cacheConfig, ok := config.Metadata["cache"]; ok {
		if configMap, ok := cacheConfig.(map[string]interface{}); ok {
			if threshold, ok := configMap["hit_rate_threshold"].(float64); ok {
				a.hitRateThreshold = threshold
			}
			if policy, ok := configMap["eviction_policy"].(string); ok {
				a.evictionPolicy = policy
			}
		}
	}

	// Initialize warmup strategies
	a.warmupStrategies = []WarmupStrategy{
		{
			Name:        "predictive-warmup",
			Priority:    1,
			Condition:   "hit_rate < 0.8",
			Parameters:  map[string]interface{}{"lookahead": "30m", "confidence": 0.7},
			Enabled:     true,
			SuccessRate: 0.0,
		},
		{
			Name:        "temporal-warmup",
			Priority:    2,
			Condition:   "time_of_day in peak_hours",
			Parameters:  map[string]interface{}{"peak_hours": []string{"9-11", "14-16"}},
			Enabled:     true,
			SuccessRate: 0.0,
		},
		{
			Name:        "user-behavior-warmup",
			Priority:    3,
			Condition:   "user_activity > threshold",
			Parameters:  map[string]interface{}{"threshold": 1000, "batch_size": 100},
			Enabled:     true,
			SuccessRate: 0.0,
		},
	}

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Info("cache agent initialized",
			logger.String("agent_id", a.ID()),
			logger.Float64("hit_rate_threshold", a.hitRateThreshold),
			logger.String("eviction_policy", a.evictionPolicy),
			logger.Int("warmup_strategies", len(a.warmupStrategies)),
		)
	}

	return nil
}

// Process processes cache optimization input
func (a *CacheAgent) Process(ctx context.Context, input ai.AgentInput) (ai.AgentOutput, error) {
	// Acquire processing slot
	startTime := time.Now()

	// Convert input to cache-specific input
	cacheInput, ok := input.Data.(CacheInput)
	if !ok {
		return ai.AgentOutput{}, fmt.Errorf("invalid input type for cache agent")
	}

	// Analyze cache performance
	analysis := a.analyzeCachePerformance(cacheInput)

	// Generate optimization recommendations
	recommendations := a.generateRecommendations(cacheInput, analysis)

	// Create warmup plan
	warmupPlan := a.createWarmupPlan(cacheInput, analysis)

	// Optimize eviction strategy
	evictionStrategy := a.optimizeEvictionStrategy(cacheInput, analysis)

	// Provide sizing advice
	sizingAdvice := a.provideSizingAdvice(cacheInput, analysis)

	// Predict impact
	predictedImpact := a.predictImpact(cacheInput, recommendations)

	// Create actions
	actions := a.createActions(recommendations, warmupPlan, evictionStrategy)

	// Create output
	output := CacheOutput{
		Recommendations:  recommendations,
		WarmupPlan:       warmupPlan,
		EvictionStrategy: evictionStrategy,
		SizingAdvice:     sizingAdvice,
		PredictedImpact:  predictedImpact,
		Actions:          actions,
	}

	// Update statistics
	a.updateOptimizationStats(output)

	// Create agent output
	agentOutput := ai.AgentOutput{
		Type:        "cache-optimization",
		Data:        output,
		Confidence:  a.calculateConfidence(cacheInput, analysis),
		Explanation: a.generateExplanation(output),
		Actions:     a.convertToAgentActions(actions),
		Metadata: map[string]interface{}{
			"processing_time":       time.Since(startTime),
			"recommendations_count": len(recommendations),
			"predicted_improvement": predictedImpact.HitRateImprovement,
		},
		Timestamp: time.Now(),
	}

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Debug("cache optimization processed",
			logger.String("agent_id", a.ID()),
			logger.String("request_id", input.RequestID),
			logger.Float64("hit_rate", cacheInput.CacheMetrics.HitRate),
			logger.Float64("predicted_improvement", predictedImpact.HitRateImprovement),
			logger.Duration("processing_time", time.Since(startTime)),
		)
	}

	return agentOutput, nil
}

// analyzeCachePerformance analyzes current cache performance
func (a *CacheAgent) analyzeCachePerformance(input CacheInput) CachePerformanceAnalysis {
	analysis := CachePerformanceAnalysis{
		CurrentHitRate:   input.CacheMetrics.HitRate,
		PerformanceGrade: a.calculatePerformanceGrade(input.CacheMetrics),
		Bottlenecks:      a.identifyBottlenecks(input),
		Patterns:         a.analyzeAccessPatterns(input.AccessPatterns),
		Efficiency:       a.calculateEfficiency(input),
		Recommendations:  []string{},
	}

	// Identify specific issues
	if analysis.CurrentHitRate < a.hitRateThreshold {
		analysis.Recommendations = append(analysis.Recommendations, "improve-hit-rate")
	}

	if input.CacheMetrics.EvictionRate > 0.1 {
		analysis.Recommendations = append(analysis.Recommendations, "optimize-eviction")
	}

	if input.StorageMetrics.Fragmentation > 0.3 {
		analysis.Recommendations = append(analysis.Recommendations, "defragment-storage")
	}

	return analysis
}

// CachePerformanceAnalysis contains cache performance analysis
type CachePerformanceAnalysis struct {
	CurrentHitRate   float64           `json:"current_hit_rate"`
	PerformanceGrade string            `json:"performance_grade"`
	Bottlenecks      []string          `json:"bottlenecks"`
	Patterns         []PatternAnalysis `json:"patterns"`
	Efficiency       float64           `json:"efficiency"`
	Recommendations  []string          `json:"recommendations"`
}

// PatternAnalysis contains pattern analysis results
type PatternAnalysis struct {
	Pattern    string  `json:"pattern"`
	Confidence float64 `json:"confidence"`
	Impact     string  `json:"impact"`
	Frequency  int64   `json:"frequency"`
	Trend      string  `json:"trend"`
}

// calculatePerformanceGrade calculates cache performance grade
func (a *CacheAgent) calculatePerformanceGrade(metrics CacheMetrics) string {
	if metrics.HitRate >= 0.95 {
		return "A+"
	} else if metrics.HitRate >= 0.90 {
		return "A"
	} else if metrics.HitRate >= 0.80 {
		return "B"
	} else if metrics.HitRate >= 0.70 {
		return "C"
	} else if metrics.HitRate >= 0.60 {
		return "D"
	}
	return "F"
}

// identifyBottlenecks identifies cache performance bottlenecks
func (a *CacheAgent) identifyBottlenecks(input CacheInput) []string {
	bottlenecks := []string{}

	if input.CacheMetrics.HitRate < 0.70 {
		bottlenecks = append(bottlenecks, "low-hit-rate")
	}

	if input.CacheMetrics.AverageLatency > 100*time.Millisecond {
		bottlenecks = append(bottlenecks, "high-latency")
	}

	if input.StorageMetrics.Fragmentation > 0.4 {
		bottlenecks = append(bottlenecks, "fragmentation")
	}

	if input.ResourceUsage.Memory > 0.9 {
		bottlenecks = append(bottlenecks, "memory-pressure")
	}

	return bottlenecks
}

// analyzeAccessPatterns analyzes cache access patterns
func (a *CacheAgent) analyzeAccessPatterns(patterns []AccessPattern) []PatternAnalysis {
	analyses := []PatternAnalysis{}

	for _, pattern := range patterns {
		analysis := PatternAnalysis{
			Pattern:    pattern.Pattern,
			Confidence: pattern.Predictability,
			Frequency:  pattern.Frequency,
		}

		if pattern.Locality == "temporal" {
			analysis.Impact = "high"
			analysis.Trend = "predictable"
		} else if pattern.Locality == "spatial" {
			analysis.Impact = "medium"
			analysis.Trend = "clustered"
		} else {
			analysis.Impact = "low"
			analysis.Trend = "random"
		}

		analyses = append(analyses, analysis)
	}

	return analyses
}

// calculateEfficiency calculates cache efficiency
func (a *CacheAgent) calculateEfficiency(input CacheInput) float64 {
	// Weighted efficiency calculation
	hitRateWeight := 0.4
	latencyWeight := 0.3
	storageWeight := 0.3

	hitRateScore := input.CacheMetrics.HitRate
	latencyScore := 1.0 - (float64(input.CacheMetrics.AverageLatency.Milliseconds()) / 1000.0)
	if latencyScore < 0 {
		latencyScore = 0
	}

	storageScore := 1.0 - input.StorageMetrics.Fragmentation
	if storageScore < 0 {
		storageScore = 0
	}

	return hitRateWeight*hitRateScore + latencyWeight*latencyScore + storageWeight*storageScore
}

// generateRecommendations generates cache optimization recommendations
func (a *CacheAgent) generateRecommendations(input CacheInput, analysis CachePerformanceAnalysis) []CacheRecommendation {
	recommendations := []CacheRecommendation{}

	// Hit rate improvement
	if analysis.CurrentHitRate < a.hitRateThreshold {
		recommendations = append(recommendations, CacheRecommendation{
			Type:        "hit-rate-improvement",
			Description: "Implement predictive caching to improve hit rate",
			Impact:      "high",
			Confidence:  0.85,
			Priority:    1,
			Parameters: map[string]interface{}{
				"target_hit_rate": 0.90,
				"strategy":        "predictive-warmup",
			},
			Rationale: fmt.Sprintf("Current hit rate %.2f is below threshold %.2f", analysis.CurrentHitRate, a.hitRateThreshold),
		})
	}

	// Eviction optimization
	if input.CacheMetrics.EvictionRate > 0.1 {
		recommendations = append(recommendations, CacheRecommendation{
			Type:        "eviction-optimization",
			Description: "Optimize eviction policy based on access patterns",
			Impact:      "medium",
			Confidence:  0.80,
			Priority:    2,
			Parameters: map[string]interface{}{
				"policy":             "intelligent-lru",
				"eviction_threshold": 0.05,
			},
			Rationale: "High eviction rate indicates suboptimal eviction policy",
		})
	}

	// Storage optimization
	if input.StorageMetrics.Fragmentation > 0.3 {
		recommendations = append(recommendations, CacheRecommendation{
			Type:        "storage-optimization",
			Description: "Defragment cache storage to improve efficiency",
			Impact:      "medium",
			Confidence:  0.75,
			Priority:    3,
			Parameters: map[string]interface{}{
				"compaction_threshold":  0.25,
				"background_compaction": true,
			},
			Rationale: "High fragmentation is reducing storage efficiency",
		})
	}

	// Size optimization
	if input.ResourceUsage.Memory > 0.9 {
		recommendations = append(recommendations, CacheRecommendation{
			Type:        "size-optimization",
			Description: "Optimize cache size based on usage patterns",
			Impact:      "high",
			Confidence:  0.90,
			Priority:    1,
			Parameters: map[string]interface{}{
				"size_adjustment":  "increase",
				"recommended_size": input.StorageMetrics.TotalSize * 12 / 10,
			},
			Rationale: "High memory usage indicates cache is undersized",
		})
	}

	return recommendations
}

// createWarmupPlan creates a cache warmup plan
func (a *CacheAgent) createWarmupPlan(input CacheInput, analysis CachePerformanceAnalysis) WarmupPlan {
	// Identify high-priority keys for warmup
	keys := a.identifyWarmupKeys(input, analysis)

	// Create priority ordering
	priorities := make([]int, len(keys))
	for i := range keys {
		priorities[i] = i + 1
	}

	// Schedule warmup operations
	schedule := a.scheduleWarmupOperations(keys, input.TimeContext)

	return WarmupPlan{
		Strategy:    "predictive-temporal",
		Keys:        keys,
		Priority:    priorities,
		Schedule:    schedule,
		BatchSize:   50,
		Parallelism: 5,
		Parameters: map[string]interface{}{
			"confidence_threshold": 0.7,
			"lookahead_window":     "30m",
		},
	}
}

// identifyWarmupKeys identifies keys that should be warmed up
func (a *CacheAgent) identifyWarmupKeys(input CacheInput, analysis CachePerformanceAnalysis) []string {
	keys := []string{}

	// Analyze access patterns to identify frequently accessed keys
	for _, pattern := range input.AccessPatterns {
		if pattern.Predictability > 0.7 && pattern.Frequency > 10 {
			keys = append(keys, pattern.Keys...)
		}
	}

	// Limit to top 100 keys
	if len(keys) > 100 {
		keys = keys[:100]
	}

	return keys
}

// scheduleWarmupOperations schedules warmup operations
func (a *CacheAgent) scheduleWarmupOperations(keys []string, timeContext TimeContext) []time.Time {
	schedule := []time.Time{}

	// Schedule based on time context
	baseTime := timeContext.CurrentTime
	interval := 5 * time.Minute

	for i := 0; i < len(keys); i += 10 { // Batch of 10 keys
		schedule = append(schedule, baseTime.Add(time.Duration(i/10)*interval))
	}

	return schedule
}

// optimizeEvictionStrategy optimizes cache eviction strategy
func (a *CacheAgent) optimizeEvictionStrategy(input CacheInput, analysis CachePerformanceAnalysis) EvictionStrategy {
	policy := "lru"

	// Choose optimal eviction policy based on access patterns
	if a.hasTemporalLocality(input.AccessPatterns) {
		policy = "temporal-lru"
	} else if a.hasSpatialLocality(input.AccessPatterns) {
		policy = "spatial-lru"
	} else {
		policy = "intelligent-lru"
	}

	return EvictionStrategy{
		Policy: policy,
		Parameters: map[string]interface{}{
			"eviction_threshold": 0.90,
			"batch_size":         10,
			"aging_factor":       0.1,
		},
		Thresholds: map[string]float64{
			"memory_usage":  0.90,
			"hit_rate":      0.70,
			"eviction_rate": 0.05,
		},
		Conditions: []string{
			"memory_usage > 0.85",
			"hit_rate < 0.75",
		},
		Scheduling: "background",
	}
}

// hasTemporalLocality checks if access patterns have temporal locality
func (a *CacheAgent) hasTemporalLocality(patterns []AccessPattern) bool {
	for _, pattern := range patterns {
		if pattern.Locality == "temporal" && pattern.Predictability > 0.7 {
			return true
		}
	}
	return false
}

// hasSpatialLocality checks if access patterns have spatial locality
func (a *CacheAgent) hasSpatialLocality(patterns []AccessPattern) bool {
	for _, pattern := range patterns {
		if pattern.Locality == "spatial" && pattern.Predictability > 0.7 {
			return true
		}
	}
	return false
}

// provideSizingAdvice provides cache sizing recommendations
func (a *CacheAgent) provideSizingAdvice(input CacheInput, analysis CachePerformanceAnalysis) SizingAdvice {
	currentSize := input.StorageMetrics.TotalSize
	usedSize := input.StorageMetrics.UsedSize
	utilizationRate := float64(usedSize) / float64(currentSize)

	var recommendedSize int64
	var rationale string

	if utilizationRate > 0.9 {
		recommendedSize = currentSize * 13 / 10 // Increase by 30%
		rationale = "High utilization indicates cache is undersized"
	} else if utilizationRate < 0.5 {
		recommendedSize = currentSize * 8 / 10 // Decrease by 20%
		rationale = "Low utilization indicates cache is oversized"
	} else {
		recommendedSize = currentSize
		rationale = "Current size is appropriate"
	}

	return SizingAdvice{
		RecommendedSize: recommendedSize,
		MinSize:         currentSize / 2,
		MaxSize:         currentSize * 2,
		OptimalRatio:    0.80,
		Partitioning: map[string]int64{
			"hot":  recommendedSize * 3 / 10,
			"warm": recommendedSize * 5 / 10,
			"cold": recommendedSize * 2 / 10,
		},
		Rationale: rationale,
		Parameters: map[string]interface{}{
			"growth_rate":        0.1,
			"utilization_target": 0.8,
		},
	}
}

// predictImpact predicts the impact of optimization recommendations
func (a *CacheAgent) predictImpact(input CacheInput, recommendations []CacheRecommendation) PredictedImpact {
	hitRateImprovement := 0.0
	latencyReduction := time.Duration(0)
	costSavings := 0.0
	storageOptimization := 0.0

	for _, rec := range recommendations {
		switch rec.Type {
		case "hit-rate-improvement":
			hitRateImprovement += 0.15 * rec.Confidence
		case "eviction-optimization":
			latencyReduction += time.Duration(float64(20*time.Millisecond) * rec.Confidence)
		case "storage-optimization":
			storageOptimization += 0.25 * rec.Confidence
		case "size-optimization":
			costSavings += 0.20 * rec.Confidence
		}
	}

	return PredictedImpact{
		HitRateImprovement:  hitRateImprovement,
		LatencyReduction:    latencyReduction,
		CostSavings:         costSavings,
		StorageOptimization: storageOptimization,
		Confidence:          0.80,
	}
}

// createActions creates cache optimization actions
func (a *CacheAgent) createActions(recommendations []CacheRecommendation, warmupPlan WarmupPlan, evictionStrategy EvictionStrategy) []CacheAction {
	actions := []CacheAction{}

	// Create actions from recommendations
	for _, rec := range recommendations {
		action := CacheAction{
			Type:       rec.Type,
			Target:     "cache-system",
			Parameters: rec.Parameters,
			Priority:   rec.Priority,
			Condition:  fmt.Sprintf("confidence > 0.7"),
			Timeout:    30 * time.Second,
			Rollback:   true,
		}
		actions = append(actions, action)
	}

	// Add warmup action
	if len(warmupPlan.Keys) > 0 {
		actions = append(actions, CacheAction{
			Type:   "warmup-cache",
			Target: "cache-system",
			Parameters: map[string]interface{}{
				"keys":        warmupPlan.Keys,
				"batch_size":  warmupPlan.BatchSize,
				"parallelism": warmupPlan.Parallelism,
			},
			Priority:  len(actions) + 1,
			Condition: "hit_rate < 0.8",
			Timeout:   5 * time.Minute,
			Rollback:  false,
		})
	}

	// Add eviction strategy action
	actions = append(actions, CacheAction{
		Type:   "update-eviction-policy",
		Target: "cache-system",
		Parameters: map[string]interface{}{
			"policy":     evictionStrategy.Policy,
			"parameters": evictionStrategy.Parameters,
			"thresholds": evictionStrategy.Thresholds,
		},
		Priority:  len(actions) + 1,
		Condition: "eviction_rate > 0.1",
		Timeout:   1 * time.Minute,
		Rollback:  true,
	})

	return actions
}

// calculateConfidence calculates overall confidence in recommendations
func (a *CacheAgent) calculateConfidence(input CacheInput, analysis CachePerformanceAnalysis) float64 {
	confidence := 0.5 // Base confidence

	// Increase confidence based on data quality
	if len(input.AccessPatterns) > 5 {
		confidence += 0.1
	}

	// Increase confidence based on performance grade
	switch analysis.PerformanceGrade {
	case "A+", "A":
		confidence += 0.3
	case "B":
		confidence += 0.2
	case "C":
		confidence += 0.1
	}

	// Increase confidence based on pattern predictability
	avgPredictability := 0.0
	for _, pattern := range analysis.Patterns {
		avgPredictability += pattern.Confidence
	}
	if len(analysis.Patterns) > 0 {
		avgPredictability /= float64(len(analysis.Patterns))
		confidence += avgPredictability * 0.2
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// generateExplanation generates human-readable explanation
func (a *CacheAgent) generateExplanation(output CacheOutput) string {
	explanation := fmt.Sprintf("Cache optimization analysis completed. ")

	if len(output.Recommendations) > 0 {
		explanation += fmt.Sprintf("Generated %d recommendations for improvement. ", len(output.Recommendations))
	}

	if output.PredictedImpact.HitRateImprovement > 0 {
		explanation += fmt.Sprintf("Expected hit rate improvement: %.1f%%. ", output.PredictedImpact.HitRateImprovement*100)
	}

	if output.PredictedImpact.LatencyReduction > 0 {
		explanation += fmt.Sprintf("Expected latency reduction: %v. ", output.PredictedImpact.LatencyReduction)
	}

	explanation += fmt.Sprintf("Warmup plan includes %d keys. ", len(output.WarmupPlan.Keys))
	explanation += fmt.Sprintf("Recommended eviction policy: %s.", output.EvictionStrategy.Policy)

	return explanation
}

// convertToAgentActions converts cache actions to agent actions
func (a *CacheAgent) convertToAgentActions(cacheActions []CacheAction) []ai.AgentAction {
	actions := []ai.AgentAction{}

	for _, cacheAction := range cacheActions {
		action := ai.AgentAction{
			Type:       cacheAction.Type,
			Target:     cacheAction.Target,
			Parameters: cacheAction.Parameters,
			Priority:   cacheAction.Priority,
			Condition:  cacheAction.Condition,
			Timeout:    cacheAction.Timeout,
		}
		actions = append(actions, action)
	}

	return actions
}

// updateOptimizationStats updates optimization statistics
func (a *CacheAgent) updateOptimizationStats(output CacheOutput) {
	a.optimizationStats.LastOptimization = time.Now()

	// Update based on recommendations
	for _, rec := range output.Recommendations {
		switch rec.Type {
		case "hit-rate-improvement":
			a.optimizationStats.HitRateImprovement += output.PredictedImpact.HitRateImprovement
		case "eviction-optimization":
			a.optimizationStats.EvictionOptimizations++
		case "storage-optimization":
			a.optimizationStats.StorageOptimizations++
		}
	}

	// Update warmup operations
	if len(output.WarmupPlan.Keys) > 0 {
		a.optimizationStats.WarmupOperations++
	}

	// Update total savings
	a.optimizationStats.TotalSavings += output.PredictedImpact.CostSavings
}

// Learn learns from cache optimization feedback
func (a *CacheAgent) Learn(ctx context.Context, feedback ai.AgentFeedback) error {
	if err := a.BaseAgent.Learn(ctx, feedback); err != nil {
		return err
	}

	// Update warmup strategy success rates
	if actionType, ok := feedback.Context["action_type"].(string); ok {
		if actionType == "warmup-cache" {
			for i, strategy := range a.warmupStrategies {
				if strategy.Name == feedback.Context["strategy"] {
					if feedback.Success {
						a.warmupStrategies[i].SuccessRate = a.warmupStrategies[i].SuccessRate*0.9 + 0.1
					} else {
						a.warmupStrategies[i].SuccessRate = a.warmupStrategies[i].SuccessRate * 0.9
					}
					a.warmupStrategies[i].LastUsed = time.Now()
					break
				}
			}
		}
	}

	// Adjust hit rate threshold based on feedback
	if hitRateImprovement, ok := feedback.Metrics["hit_rate_improvement"]; ok {
		if hitRateImprovement > 0 {
			a.hitRateThreshold = a.hitRateThreshold * 1.01 // Slightly increase threshold
		} else {
			a.hitRateThreshold = a.hitRateThreshold * 0.99 // Slightly decrease threshold
		}

		// Keep threshold within reasonable bounds
		if a.hitRateThreshold > 0.95 {
			a.hitRateThreshold = 0.95
		} else if a.hitRateThreshold < 0.60 {
			a.hitRateThreshold = 0.60
		}
	}

	if a.BaseAgent.GetConfiguration().Logger != nil {
		a.BaseAgent.GetConfiguration().Logger.Debug("cache agent learned from feedback",
			logger.String("agent_id", a.ID()),
			logger.String("action_id", feedback.ActionID),
			logger.Bool("success", feedback.Success),
			logger.Float64("updated_threshold", a.hitRateThreshold),
		)
	}

	return nil
}

// GetCacheOptimizationStats returns cache optimization statistics
func (a *CacheAgent) GetCacheOptimizationStats() CacheOptimizationStats {
	return a.optimizationStats
}

// GetWarmupStrategies returns current warmup strategies
func (a *CacheAgent) GetWarmupStrategies() []WarmupStrategy {
	return a.warmupStrategies
}

// UpdateWarmupStrategy updates a warmup strategy
func (a *CacheAgent) UpdateWarmupStrategy(name string, strategy WarmupStrategy) error {
	for i, s := range a.warmupStrategies {
		if s.Name == name {
			a.warmupStrategies[i] = strategy
			return nil
		}
	}
	return fmt.Errorf("warmup strategy %s not found", name)
}
