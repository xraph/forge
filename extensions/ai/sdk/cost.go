package sdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// CostTracker tracks AI usage costs and provides optimization insights
type CostTracker struct {
	logger  forge.Logger
	metrics forge.Metrics

	mu     sync.RWMutex
	usages []UsageRecord
	budgets map[string]*Budget
	alerts []AlertRule
}

// UsageRecord represents a single usage event with cost
type UsageRecord struct {
	Timestamp    time.Time
	Provider     string
	Model        string
	Operation    string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cost         float64
	CacheHit     bool
	Metadata     map[string]interface{}
}

// Budget represents a spending budget
type Budget struct {
	Name          string
	Limit         float64
	Period        time.Duration
	StartTime     time.Time
	CurrentSpend  float64
	AlertAt       float64 // Alert when spending reaches this percentage (0-1)
	LastReset     time.Time
}

// AlertRule defines when to trigger cost alerts
type AlertRule struct {
	Name      string
	Threshold float64 // Alert when cost exceeds this
	Period    time.Duration
	Notified  bool
	LastCheck time.Time
}

// ModelPricing defines pricing for a model
type ModelPricing struct {
	Provider         string
	Model            string
	InputPer1KTokens float64 // Cost per 1K input tokens
	OutputPer1KTokens float64 // Cost per 1K output tokens
	MinCost          float64 // Minimum cost per request
}

// CostTrackerOptions configures the cost tracker
type CostTrackerOptions struct {
	RetentionPeriod time.Duration // How long to keep usage records
	EnableCache     bool
}

// Common model pricing (as of 2024 - update as needed)
var DefaultModelPricing = map[string]ModelPricing{
	"openai/gpt-4": {
		Provider:          "openai",
		Model:             "gpt-4",
		InputPer1KTokens:  0.03,
		OutputPer1KTokens: 0.06,
	},
	"openai/gpt-4-turbo": {
		Provider:          "openai",
		Model:             "gpt-4-turbo",
		InputPer1KTokens:  0.01,
		OutputPer1KTokens: 0.03,
	},
	"openai/gpt-3.5-turbo": {
		Provider:          "openai",
		Model:             "gpt-3.5-turbo",
		InputPer1KTokens:  0.0005,
		OutputPer1KTokens: 0.0015,
	},
	"anthropic/claude-3-opus": {
		Provider:          "anthropic",
		Model:             "claude-3-opus",
		InputPer1KTokens:  0.015,
		OutputPer1KTokens: 0.075,
	},
	"anthropic/claude-3-sonnet": {
		Provider:          "anthropic",
		Model:             "claude-3-sonnet",
		InputPer1KTokens:  0.003,
		OutputPer1KTokens: 0.015,
	},
}

// NewCostTracker creates a new cost tracker
func NewCostTracker(logger forge.Logger, metrics forge.Metrics, opts *CostTrackerOptions) *CostTracker {
	ct := &CostTracker{
		logger:  logger,
		metrics: metrics,
		usages:  make([]UsageRecord, 0),
		budgets: make(map[string]*Budget),
		alerts:  make([]AlertRule, 0),
	}

	if opts != nil && opts.RetentionPeriod > 0 {
		// Start cleanup goroutine
		go ct.cleanupOldRecords(opts.RetentionPeriod)
	}

	return ct
}

// RecordUsage records a usage event and calculates cost
func (ct *CostTracker) RecordUsage(ctx context.Context, usage UsageRecord) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Calculate cost if not provided
	if usage.Cost == 0 {
		pricing, ok := DefaultModelPricing[fmt.Sprintf("%s/%s", usage.Provider, usage.Model)]
		if ok {
			inputCost := float64(usage.InputTokens) / 1000.0 * pricing.InputPer1KTokens
			outputCost := float64(usage.OutputTokens) / 1000.0 * pricing.OutputPer1KTokens
			usage.Cost = inputCost + outputCost
			
			if usage.Cost < pricing.MinCost {
				usage.Cost = pricing.MinCost
			}
		}
	}

	usage.Timestamp = time.Now()
	ct.usages = append(ct.usages, usage)

	// Update budgets
	for _, budget := range ct.budgets {
		if time.Since(budget.LastReset) > budget.Period {
			budget.CurrentSpend = 0
			budget.LastReset = time.Now()
		}
		budget.CurrentSpend += usage.Cost

		// Check alert threshold
		if budget.AlertAt > 0 && budget.CurrentSpend >= budget.Limit*budget.AlertAt {
			if ct.logger != nil {
				ct.logger.Warn("Budget alert triggered",
					F("budget", budget.Name),
					F("spend", budget.CurrentSpend),
					F("limit", budget.Limit),
					F("threshold", budget.AlertAt*100),
				)
			}
		}
	}

	// Record metrics
	if ct.metrics != nil {
		ct.metrics.Counter("forge.ai.sdk.cost.requests",
			"provider", usage.Provider,
			"model", usage.Model,
		).Inc()
		
		ct.metrics.Histogram("forge.ai.sdk.cost.amount",
			"provider", usage.Provider,
			"model", usage.Model,
		).Observe(usage.Cost)
		
		ct.metrics.Histogram("forge.ai.sdk.cost.tokens",
			"provider", usage.Provider,
			"model", usage.Model,
			"type", "total",
		).Observe(float64(usage.TotalTokens))

		if usage.CacheHit {
			ct.metrics.Counter("forge.ai.sdk.cost.cache_hits",
				"provider", usage.Provider,
				"model", usage.Model,
			).Inc()
		}
	}

	return nil
}

// GetInsights returns cost analytics and insights
func (ct *CostTracker) GetInsights() CostInsights {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	now := time.Now()
	insights := CostInsights{
		TopExpensiveModels: make([]ModelCost, 0),
	}

	// Calculate today's cost
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for _, usage := range ct.usages {
		if usage.Timestamp.After(startOfDay) {
			insights.CostToday += usage.Cost
		}
	}

	// Calculate this month's cost
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthCost := 0.0
	cacheHits := 0
	totalRequests := 0
	
	modelCosts := make(map[string]*ModelCost)

	for _, usage := range ct.usages {
		if usage.Timestamp.After(startOfMonth) {
			monthCost += usage.Cost
			totalRequests++
			
			if usage.CacheHit {
				cacheHits++
			}

			// Aggregate by model
			key := fmt.Sprintf("%s/%s", usage.Provider, usage.Model)
			if mc, exists := modelCosts[key]; exists {
				mc.Cost += usage.Cost
				mc.Calls++
			} else {
				modelCosts[key] = &ModelCost{
					Model: key,
					Cost:  usage.Cost,
					Calls: 1,
				}
			}
		}
	}

	insights.CostThisMonth = monthCost

	// Project monthly cost
	daysInMonth := float64(time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day())
	dayOfMonth := float64(now.Day())
	if dayOfMonth > 0 {
		insights.ProjectedMonthly = (monthCost / dayOfMonth) * daysInMonth
	}

	// Cache hit rate
	if totalRequests > 0 {
		insights.CacheHitRate = float64(cacheHits) / float64(totalRequests)
	}

	// Potential savings (rough estimate)
	insights.PotentialSavings = insights.ProjectedMonthly * 0.2 // Assume 20% optimization potential

	// Top expensive models
	for _, mc := range modelCosts {
		insights.TopExpensiveModels = append(insights.TopExpensiveModels, *mc)
	}

	// Sort by cost (descending)
	for i := 0; i < len(insights.TopExpensiveModels); i++ {
		for j := i + 1; j < len(insights.TopExpensiveModels); j++ {
			if insights.TopExpensiveModels[j].Cost > insights.TopExpensiveModels[i].Cost {
				insights.TopExpensiveModels[i], insights.TopExpensiveModels[j] = 
					insights.TopExpensiveModels[j], insights.TopExpensiveModels[i]
			}
		}
	}

	return insights
}

// SetBudget sets a spending budget
func (ct *CostTracker) SetBudget(name string, limit float64, period time.Duration, alertAt float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.budgets[name] = &Budget{
		Name:         name,
		Limit:        limit,
		Period:       period,
		AlertAt:      alertAt,
		CurrentSpend: 0,
		StartTime:    time.Now(),
		LastReset:    time.Now(),
	}

	if ct.logger != nil {
		ct.logger.Info("Budget set",
			F("name", name),
			F("limit", limit),
			F("period", period),
			F("alert_at", alertAt*100),
		)
	}
}

// CheckBudget checks if any budget is exceeded
func (ct *CostTracker) CheckBudget(ctx context.Context) error {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	for name, budget := range ct.budgets {
		if budget.CurrentSpend >= budget.Limit {
			return fmt.Errorf("budget '%s' exceeded: %.2f/%.2f", name, budget.CurrentSpend, budget.Limit)
		}
	}

	return nil
}

// GetBudgetStatus returns current status of all budgets
func (ct *CostTracker) GetBudgetStatus() map[string]BudgetStatus {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	status := make(map[string]BudgetStatus)
	for name, budget := range ct.budgets {
		status[name] = BudgetStatus{
			Name:           name,
			Limit:          budget.Limit,
			CurrentSpend:   budget.CurrentSpend,
			RemainingBudget: budget.Limit - budget.CurrentSpend,
			PercentUsed:    (budget.CurrentSpend / budget.Limit) * 100,
			Period:         budget.Period,
			TimeRemaining:  budget.Period - time.Since(budget.LastReset),
		}
	}

	return status
}

// BudgetStatus represents the current status of a budget
type BudgetStatus struct {
	Name            string
	Limit           float64
	CurrentSpend    float64
	RemainingBudget float64
	PercentUsed     float64
	Period          time.Duration
	TimeRemaining   time.Duration
}

// GetUsageByModel returns usage statistics grouped by model
func (ct *CostTracker) GetUsageByModel(since time.Time) map[string]ModelUsageStats {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	stats := make(map[string]ModelUsageStats)

	for _, usage := range ct.usages {
		if usage.Timestamp.After(since) {
			key := fmt.Sprintf("%s/%s", usage.Provider, usage.Model)
			
			s := stats[key]
			s.Model = key
			s.TotalCost += usage.Cost
			s.RequestCount++
			s.TotalInputTokens += usage.InputTokens
			s.TotalOutputTokens += usage.OutputTokens
			
			if usage.CacheHit {
				s.CacheHits++
			}
			
			stats[key] = s
		}
	}

	// Calculate averages
	for key, s := range stats {
		if s.RequestCount > 0 {
			s.AvgCostPerRequest = s.TotalCost / float64(s.RequestCount)
			s.AvgInputTokens = float64(s.TotalInputTokens) / float64(s.RequestCount)
			s.AvgOutputTokens = float64(s.TotalOutputTokens) / float64(s.RequestCount)
			
			if s.RequestCount > 0 {
				s.CacheHitRate = float64(s.CacheHits) / float64(s.RequestCount)
			}
		}
		stats[key] = s
	}

	return stats
}

// ModelUsageStats represents usage statistics for a model
type ModelUsageStats struct {
	Model              string
	RequestCount       int
	TotalCost          float64
	AvgCostPerRequest  float64
	TotalInputTokens   int
	TotalOutputTokens  int
	AvgInputTokens     float64
	AvgOutputTokens    float64
	CacheHits          int
	CacheHitRate       float64
}

// GetOptimizationRecommendations returns cost optimization suggestions
func (ct *CostTracker) GetOptimizationRecommendations() []Recommendation {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	recommendations := make([]Recommendation, 0)

	// Analyze usage patterns
	modelStats := ct.GetUsageByModel(time.Now().Add(-30 * 24 * time.Hour))

	for _, stats := range modelStats {
		// Recommend cheaper models if usage is high
		if stats.TotalCost > 100 && stats.CacheHitRate < 0.3 {
			recommendations = append(recommendations, Recommendation{
				Type:             "model_switch",
				Title:            fmt.Sprintf("Consider caching for %s", stats.Model),
				Description:      fmt.Sprintf("Low cache hit rate (%.1f%%) on expensive model. Implementing caching could save ~$%.2f/month", stats.CacheHitRate*100, stats.TotalCost*0.3),
				PotentialSavings: stats.TotalCost * 0.3,
				Priority:         "high",
			})
		}

		// Recommend batch processing
		if stats.AvgInputTokens < 100 && stats.RequestCount > 1000 {
			recommendations = append(recommendations, Recommendation{
				Type:        "batch_processing",
				Title:       "Enable batch processing",
				Description: fmt.Sprintf("Model %s has many small requests (avg %.0f tokens). Batching could reduce costs by 15%%", stats.Model, stats.AvgInputTokens),
				PotentialSavings: stats.TotalCost * 0.15,
				Priority:    "medium",
			})
		}
	}

	return recommendations
}

// Recommendation represents a cost optimization recommendation
type Recommendation struct {
	Type             string
	Title            string
	Description      string
	PotentialSavings float64
	Priority         string // "low", "medium", "high"
}

// cleanupOldRecords removes old usage records
func (ct *CostTracker) cleanupOldRecords(retentionPeriod time.Duration) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		ct.mu.Lock()
		cutoff := time.Now().Add(-retentionPeriod)
		newUsages := make([]UsageRecord, 0)
		
		for _, usage := range ct.usages {
			if usage.Timestamp.After(cutoff) {
				newUsages = append(newUsages, usage)
			}
		}
		
		ct.usages = newUsages
		ct.mu.Unlock()
	}
}

// ExportUsage exports usage records for external analysis
func (ct *CostTracker) ExportUsage(since time.Time) []UsageRecord {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	exported := make([]UsageRecord, 0)
	for _, usage := range ct.usages {
		if usage.Timestamp.After(since) {
			exported = append(exported, usage)
		}
	}

	return exported
}

