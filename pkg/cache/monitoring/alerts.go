package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// CacheAlertManager provides comprehensive alerting for cache systems
type CacheAlertManager struct {
	cacheManager      cachecore.CacheManager
	metricsCollector  *CacheMetricsCollector
	healthChecker     *CacheHealthChecker
	logger            common.Logger
	config            *CacheAlertConfig
	rules             map[string]*AlertRule
	activeAlerts      map[string]*ActiveAlert
	suppressedAlerts  map[string]time.Time
	notifiers         []AlertNotifier
	evaluationHistory map[string]*EvaluationHistory
	mu                sync.RWMutex
	started           bool
	stopChan          chan struct{}
}

// CacheAlertConfig contains configuration for cache alerting
type CacheAlertConfig struct {
	Enabled             bool          `yaml:"enabled" json:"enabled" default:"true"`
	EvaluationInterval  time.Duration `yaml:"evaluation_interval" json:"evaluation_interval" default:"30s"`
	MaxAlertHistory     int           `yaml:"max_alert_history" json:"max_alert_history" default:"1000"`
	DefaultCooldown     time.Duration `yaml:"default_cooldown" json:"default_cooldown" default:"5m"`
	EnableNotifications bool          `yaml:"enable_notifications" json:"enable_notifications" default:"true"`
	EnableSuppression   bool          `yaml:"enable_suppression" json:"enable_suppression" default:"true"`
	SuppressionWindow   time.Duration `yaml:"suppression_window" json:"suppression_window" default:"1h"`
	AlertTimeout        time.Duration `yaml:"alert_timeout" json:"alert_timeout" default:"24h"`
	MaxConcurrentAlerts int           `yaml:"max_concurrent_alerts" json:"max_concurrent_alerts" default:"100"`
	EnableEscalation    bool          `yaml:"enable_escalation" json:"enable_escalation" default:"true"`
	EscalationDelay     time.Duration `yaml:"escalation_delay" json:"escalation_delay" default:"15m"`
	NotificationRetries int           `yaml:"notification_retries" json:"notification_retries" default:"3"`
	RetryBackoff        time.Duration `yaml:"retry_backoff" json:"retry_backoff" default:"30s"`
}

// AlertRule defines a cache alerting rule
type AlertRule struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Enabled     bool              `yaml:"enabled" json:"enabled" default:"true"`
	Severity    AlertSeverity     `yaml:"severity" json:"severity"`
	Condition   AlertCondition    `yaml:"condition" json:"condition"`
	Threshold   AlertThreshold    `yaml:"threshold" json:"threshold"`
	Duration    time.Duration     `yaml:"duration" json:"duration" default:"1m"`
	Cooldown    time.Duration     `yaml:"cooldown" json:"cooldown"`
	Labels      map[string]string `yaml:"labels" json:"labels"`
	Annotations map[string]string `yaml:"annotations" json:"annotations"`
	Targets     AlertTargets      `yaml:"targets" json:"targets"`
	Actions     []AlertAction     `yaml:"actions" json:"actions"`
	Escalation  *EscalationRule   `yaml:"escalation" json:"escalation"`
	CreatedAt   time.Time         `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time         `yaml:"updated_at" json:"updated_at"`
}

// AlertSeverity defines alert severity levels
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityFatal    AlertSeverity = "fatal"
)

// AlertCondition defines the condition type for alerting
type AlertCondition string

const (
	AlertConditionGreaterThan      AlertCondition = "greater_than"
	AlertConditionLessThan         AlertCondition = "less_than"
	AlertConditionEquals           AlertCondition = "equals"
	AlertConditionNotEquals        AlertCondition = "not_equals"
	AlertConditionContains         AlertCondition = "contains"
	AlertConditionNotContains      AlertCondition = "not_contains"
	AlertConditionRegex            AlertCondition = "regex"
	AlertConditionPercentChange    AlertCondition = "percent_change"
	AlertConditionAnomalyDetection AlertCondition = "anomaly_detection"
)

// AlertThreshold defines threshold configuration
type AlertThreshold struct {
	Value      interface{}   `yaml:"value" json:"value"`
	Percentage float64       `yaml:"percentage" json:"percentage"`
	Baseline   string        `yaml:"baseline" json:"baseline"` // "average", "median", "previous"
	Window     time.Duration `yaml:"window" json:"window"`
}

// AlertTargets defines what to monitor
type AlertTargets struct {
	CacheNames []string          `yaml:"cache_names" json:"cache_names"`
	CacheTypes []string          `yaml:"cache_types" json:"cache_types"`
	Metrics    []string          `yaml:"metrics" json:"metrics"`
	Tags       map[string]string `yaml:"tags" json:"tags"`
	AllCaches  bool              `yaml:"all_caches" json:"all_caches"`
}

// AlertAction defines actions to take when an alert fires
type AlertAction struct {
	Type       string                 `yaml:"type" json:"type"` // "notify", "webhook", "script", "auto_heal"
	Config     map[string]interface{} `yaml:"config" json:"config"`
	Enabled    bool                   `yaml:"enabled" json:"enabled" default:"true"`
	OnFire     bool                   `yaml:"on_fire" json:"on_fire" default:"true"`
	OnResolve  bool                   `yaml:"on_resolve" json:"on_resolve" default:"true"`
	Conditions map[string]string      `yaml:"conditions" json:"conditions"`
}

// EscalationRule defines escalation behavior
type EscalationRule struct {
	Enabled       bool          `yaml:"enabled" json:"enabled" default:"true"`
	Delay         time.Duration `yaml:"delay" json:"delay" default:"15m"`
	MaxLevel      int           `yaml:"max_level" json:"max_level" default:"3"`
	Multiplier    float64       `yaml:"multiplier" json:"multiplier" default:"2.0"`
	Actions       []AlertAction `yaml:"actions" json:"actions"`
	StopOnResolve bool          `yaml:"stop_on_resolve" json:"stop_on_resolve" default:"true"`
}

// ActiveAlert represents an active alert instance
type ActiveAlert struct {
	ID                string                 `json:"id"`
	RuleID            string                 `json:"rule_id"`
	CacheName         string                 `json:"cache_name"`
	Severity          AlertSeverity          `json:"severity"`
	State             AlertState             `json:"state"`
	Message           string                 `json:"message"`
	Description       string                 `json:"description"`
	Value             interface{}            `json:"value"`
	Threshold         interface{}            `json:"threshold"`
	Labels            map[string]string      `json:"labels"`
	Annotations       map[string]string      `json:"annotations"`
	FiredAt           time.Time              `json:"fired_at"`
	ResolvedAt        *time.Time             `json:"resolved_at,omitempty"`
	LastEvaluation    time.Time              `json:"last_evaluation"`
	EvaluationCount   int64                  `json:"evaluation_count"`
	NotificationsSent int                    `json:"notifications_sent"`
	EscalationLevel   int                    `json:"escalation_level"`
	SuppressionEnd    *time.Time             `json:"suppression_end,omitempty"`
	Context           map[string]interface{} `json:"context"`
}

// AlertState defines the state of an alert
type AlertState string

const (
	AlertStatePending    AlertState = "pending"
	AlertStateFiring     AlertState = "firing"
	AlertStateResolved   AlertState = "resolved"
	AlertStateSuppressed AlertState = "suppressed"
	AlertStateExpired    AlertState = "expired"
)

// EvaluationHistory tracks alert rule evaluation history
type EvaluationHistory struct {
	RuleID      string                `json:"rule_id"`
	Evaluations []*AlertEvaluation    `json:"evaluations"`
	Statistics  *EvaluationStatistics `json:"statistics"`
	mu          sync.RWMutex          `json:"-"`
}

// AlertEvaluation represents a single evaluation of an alert rule
type AlertEvaluation struct {
	Timestamp   time.Time     `json:"timestamp"`
	CacheName   string        `json:"cache_name"`
	MetricValue interface{}   `json:"metric_value"`
	Threshold   interface{}   `json:"threshold"`
	Result      bool          `json:"result"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
}

// EvaluationStatistics contains statistics about rule evaluations
type EvaluationStatistics struct {
	TotalEvaluations      int64         `json:"total_evaluations"`
	SuccessfulEvaluations int64         `json:"successful_evaluations"`
	FailedEvaluations     int64         `json:"failed_evaluations"`
	TrueResults           int64         `json:"true_results"`
	FalseResults          int64         `json:"false_results"`
	AverageLatency        time.Duration `json:"average_latency"`
	LastEvaluation        time.Time     `json:"last_evaluation"`
	LastError             string        `json:"last_error,omitempty"`
}

// AlertNotifier defines the interface for alert notifications
type AlertNotifier interface {
	Name() string
	SendAlert(ctx context.Context, alert *ActiveAlert) error
	SendResolution(ctx context.Context, alert *ActiveAlert) error
	HealthCheck(ctx context.Context) error
}

// AlertSummary provides a summary of alerting status
type AlertSummary struct {
	TotalRules        int                   `json:"total_rules"`
	ActiveRules       int                   `json:"active_rules"`
	TotalAlerts       int                   `json:"total_alerts"`
	FiringAlerts      int                   `json:"firing_alerts"`
	PendingAlerts     int                   `json:"pending_alerts"`
	SuppressedAlerts  int                   `json:"suppressed_alerts"`
	AlertsByCache     map[string]int        `json:"alerts_by_cache"`
	AlertsBySeverity  map[AlertSeverity]int `json:"alerts_by_severity"`
	RecentAlerts      []*ActiveAlert        `json:"recent_alerts"`
	TopAlertingCaches []string              `json:"top_alerting_caches"`
	LastEvaluation    time.Time             `json:"last_evaluation"`
}

// NewCacheAlertManager creates a new cache alert manager
func NewCacheAlertManager(
	cacheManager cachecore.CacheManager,
	metricsCollector *CacheMetricsCollector,
	healthChecker *CacheHealthChecker,
	logger common.Logger,
	config *CacheAlertConfig,
) *CacheAlertManager {
	if config == nil {
		config = &CacheAlertConfig{
			Enabled:             true,
			EvaluationInterval:  30 * time.Second,
			MaxAlertHistory:     1000,
			DefaultCooldown:     5 * time.Minute,
			EnableNotifications: true,
			EnableSuppression:   true,
			SuppressionWindow:   time.Hour,
			AlertTimeout:        24 * time.Hour,
			MaxConcurrentAlerts: 100,
			EnableEscalation:    true,
			EscalationDelay:     15 * time.Minute,
			NotificationRetries: 3,
			RetryBackoff:        30 * time.Second,
		}
	}

	return &CacheAlertManager{
		cacheManager:      cacheManager,
		metricsCollector:  metricsCollector,
		healthChecker:     healthChecker,
		logger:            logger,
		config:            config,
		rules:             make(map[string]*AlertRule),
		activeAlerts:      make(map[string]*ActiveAlert),
		suppressedAlerts:  make(map[string]time.Time),
		notifiers:         make([]AlertNotifier, 0),
		evaluationHistory: make(map[string]*EvaluationHistory),
		stopChan:          make(chan struct{}),
	}
}

// Start starts the cache alert manager
func (cam *CacheAlertManager) Start(ctx context.Context) error {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	if cam.started {
		return fmt.Errorf("cache alert manager already started")
	}

	if !cam.config.Enabled {
		return fmt.Errorf("cache alert manager is disabled")
	}

	cam.started = true

	// Register default alert rules
	cam.registerDefaultRules()

	// Start evaluation loop
	go cam.evaluationLoop(ctx)

	// Start cleanup loop
	go cam.cleanupLoop(ctx)

	if cam.logger != nil {
		cam.logger.Info("cache alert manager started",
			logger.Duration("evaluation_interval", cam.config.EvaluationInterval),
			logger.Int("rules", len(cam.rules)),
		)
	}

	return nil
}

// Stop stops the cache alert manager
func (cam *CacheAlertManager) Stop() error {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	if !cam.started {
		return fmt.Errorf("cache alert manager not started")
	}

	close(cam.stopChan)
	cam.started = false

	if cam.logger != nil {
		cam.logger.Info("cache alert manager stopped")
	}

	return nil
}

// AddRule adds an alert rule
func (cam *CacheAlertManager) AddRule(rule *AlertRule) error {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	if rule.ID == "" {
		return fmt.Errorf("rule ID cannot be empty")
	}

	if _, exists := cam.rules[rule.ID]; exists {
		return fmt.Errorf("rule with ID %s already exists", rule.ID)
	}

	// Set defaults
	if rule.Cooldown == 0 {
		rule.Cooldown = cam.config.DefaultCooldown
	}

	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	cam.rules[rule.ID] = rule

	// Initialize evaluation history
	cam.evaluationHistory[rule.ID] = &EvaluationHistory{
		RuleID:      rule.ID,
		Evaluations: make([]*AlertEvaluation, 0),
		Statistics:  &EvaluationStatistics{},
	}

	if cam.logger != nil {
		cam.logger.Info("alert rule added",
			logger.String("rule_id", rule.ID),
			logger.String("name", rule.Name),
			logger.String("severity", string(rule.Severity)),
		)
	}

	return nil
}

// RemoveRule removes an alert rule
func (cam *CacheAlertManager) RemoveRule(ruleID string) error {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	if _, exists := cam.rules[ruleID]; !exists {
		return fmt.Errorf("rule with ID %s not found", ruleID)
	}

	delete(cam.rules, ruleID)
	delete(cam.evaluationHistory, ruleID)

	// Resolve any active alerts for this rule
	for alertID, alert := range cam.activeAlerts {
		if alert.RuleID == ruleID {
			cam.resolveAlert(alertID, "rule removed")
		}
	}

	if cam.logger != nil {
		cam.logger.Info("alert rule removed",
			logger.String("rule_id", ruleID),
		)
	}

	return nil
}

// GetRules returns all alert rules
func (cam *CacheAlertManager) GetRules() map[string]*AlertRule {
	cam.mu.RLock()
	defer cam.mu.RUnlock()

	rules := make(map[string]*AlertRule)
	for id, rule := range cam.rules {
		rules[id] = rule
	}

	return rules
}

// GetActiveAlerts returns all active alerts
func (cam *CacheAlertManager) GetActiveAlerts() map[string]*ActiveAlert {
	cam.mu.RLock()
	defer cam.mu.RUnlock()

	alerts := make(map[string]*ActiveAlert)
	for id, alert := range cam.activeAlerts {
		alerts[id] = alert
	}

	return alerts
}

// GetAlertSummary returns a summary of alerting status
func (cam *CacheAlertManager) GetAlertSummary() *AlertSummary {
	cam.mu.RLock()
	defer cam.mu.RUnlock()

	summary := &AlertSummary{
		TotalRules:       len(cam.rules),
		TotalAlerts:      len(cam.activeAlerts),
		AlertsByCache:    make(map[string]int),
		AlertsBySeverity: make(map[AlertSeverity]int),
		RecentAlerts:     make([]*ActiveAlert, 0),
	}

	activeRules := 0
	for _, rule := range cam.rules {
		if rule.Enabled {
			activeRules++
		}
	}
	summary.ActiveRules = activeRules

	// Count alerts by state and cache
	for _, alert := range cam.activeAlerts {
		switch alert.State {
		case AlertStateFiring:
			summary.FiringAlerts++
		case AlertStatePending:
			summary.PendingAlerts++
		case AlertStateSuppressed:
			summary.SuppressedAlerts++
		}

		summary.AlertsByCache[alert.CacheName]++
		summary.AlertsBySeverity[alert.Severity]++

		// Add to recent alerts (last 10)
		if len(summary.RecentAlerts) < 10 {
			summary.RecentAlerts = append(summary.RecentAlerts, alert)
		}
	}

	return summary
}

// AddNotifier adds an alert notifier
func (cam *CacheAlertManager) AddNotifier(notifier AlertNotifier) {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	cam.notifiers = append(cam.notifiers, notifier)

	if cam.logger != nil {
		cam.logger.Info("alert notifier added",
			logger.String("notifier", notifier.Name()),
		)
	}
}

// registerDefaultRules registers default alert rules
func (cam *CacheAlertManager) registerDefaultRules() {
	defaultRules := []*AlertRule{
		{
			ID:          "high_miss_rate",
			Name:        "High Cache Miss Rate",
			Description: "Cache miss rate is above threshold",
			Enabled:     true,
			Severity:    AlertSeverityWarning,
			Condition:   AlertConditionGreaterThan,
			Threshold: AlertThreshold{
				Value: 0.3, // 30% miss rate
			},
			Duration: time.Minute,
			Targets: AlertTargets{
				AllCaches: true,
				Metrics:   []string{"miss_rate"},
			},
		},
		{
			ID:          "low_hit_rate",
			Name:        "Low Cache Hit Rate",
			Description: "Cache hit rate is below threshold",
			Enabled:     true,
			Severity:    AlertSeverityWarning,
			Condition:   AlertConditionLessThan,
			Threshold: AlertThreshold{
				Value: 0.7, // 70% hit rate
			},
			Duration: time.Minute,
			Targets: AlertTargets{
				AllCaches: true,
				Metrics:   []string{"hit_ratio"},
			},
		},
		{
			ID:          "high_error_rate",
			Name:        "High Cache Error Rate",
			Description: "Cache error rate is above threshold",
			Enabled:     true,
			Severity:    AlertSeverityCritical,
			Condition:   AlertConditionGreaterThan,
			Threshold: AlertThreshold{
				Value: 0.05, // 5% error rate
			},
			Duration: 30 * time.Second,
			Targets: AlertTargets{
				AllCaches: true,
				Metrics:   []string{"error_rate"},
			},
		},
		{
			ID:          "high_memory_usage",
			Name:        "High Cache Memory Usage",
			Description: "Cache memory usage is above threshold",
			Enabled:     true,
			Severity:    AlertSeverityWarning,
			Condition:   AlertConditionGreaterThan,
			Threshold: AlertThreshold{
				Value: 0.9, // 90% memory usage
			},
			Duration: 2 * time.Minute,
			Targets: AlertTargets{
				AllCaches: true,
				Metrics:   []string{"memory_utilization"},
			},
		},
		{
			ID:          "cache_unavailable",
			Name:        "Cache Unavailable",
			Description: "Cache health check failed",
			Enabled:     true,
			Severity:    AlertSeverityCritical,
			Condition:   AlertConditionEquals,
			Threshold: AlertThreshold{
				Value: "unhealthy",
			},
			Duration: 30 * time.Second,
			Targets: AlertTargets{
				AllCaches: true,
				Metrics:   []string{"health_status"},
			},
		},
	}

	for _, rule := range defaultRules {
		cam.AddRule(rule)
	}
}

// evaluationLoop runs the alert evaluation loop
func (cam *CacheAlertManager) evaluationLoop(ctx context.Context) {
	ticker := time.NewTicker(cam.config.EvaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cam.evaluateRules(ctx)

		case <-cam.stopChan:
			return

		case <-ctx.Done():
			return
		}
	}
}

// cleanupLoop runs the cleanup loop for expired alerts
func (cam *CacheAlertManager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cam.cleanupExpiredAlerts()

		case <-cam.stopChan:
			return

		case <-ctx.Done():
			return
		}
	}
}

// evaluateRules evaluates all alert rules
func (cam *CacheAlertManager) evaluateRules(ctx context.Context) {
	cam.mu.RLock()
	rules := make([]*AlertRule, 0, len(cam.rules))
	for _, rule := range cam.rules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	cam.mu.RUnlock()

	for _, rule := range rules {
		cam.evaluateRule(ctx, rule)
	}
}

// evaluateRule evaluates a single alert rule
func (cam *CacheAlertManager) evaluateRule(ctx context.Context, rule *AlertRule) {
	startTime := time.Now()

	// Get target caches
	caches := cam.getTargetCaches(rule.Targets)

	for _, cache := range caches {
		result := cam.evaluateRuleForCache(ctx, rule, cache)
		cam.recordEvaluation(rule.ID, cache.Name(), result)
		cam.handleEvaluationResult(rule, cache, result)
	}

	// Update evaluation statistics
	cam.updateEvaluationStatistics(rule.ID, time.Since(startTime))
}

// evaluateRuleForCache evaluates a rule for a specific cache
func (cam *CacheAlertManager) evaluateRuleForCache(ctx context.Context, rule *AlertRule, cache cachecore.Cache) *AlertEvaluation {
	evaluation := &AlertEvaluation{
		Timestamp: time.Now(),
		CacheName: cache.Name(),
	}

	// Get metric value based on rule targets
	metricValue, err := cam.getMetricValue(cache, rule.Targets.Metrics[0])
	if err != nil {
		evaluation.Error = err.Error()
		return evaluation
	}

	evaluation.MetricValue = metricValue
	evaluation.Threshold = rule.Threshold.Value

	// Evaluate condition
	result, err := cam.evaluateCondition(rule.Condition, metricValue, rule.Threshold)
	if err != nil {
		evaluation.Error = err.Error()
		return evaluation
	}

	evaluation.Result = result
	evaluation.Duration = time.Since(evaluation.Timestamp)

	return evaluation
}

// getMetricValue retrieves a metric value for a cache
func (cam *CacheAlertManager) getMetricValue(cache cachecore.Cache, metricName string) (interface{}, error) {
	stats := cache.Stats()

	switch metricName {
	case "hit_ratio":
		return stats.HitRatio, nil
	case "miss_rate":
		total := stats.Hits + stats.Misses
		if total == 0 {
			return 0.0, nil
		}
		return float64(stats.Misses) / float64(total), nil
	case "error_rate":
		total := stats.Hits + stats.Misses + stats.Sets + stats.Deletes
		if total == 0 {
			return 0.0, nil
		}
		return float64(stats.Errors) / float64(total), nil
	case "memory_utilization":
		// This would need to be enhanced based on cache implementation
		return float64(stats.Memory) / (1024 * 1024 * 1024), nil // Convert to GB
	case "health_status":
		if err := cache.HealthCheck(context.Background()); err != nil {
			return "unhealthy", nil
		}
		return "healthy", nil
	default:
		return nil, fmt.Errorf("unknown metric: %s", metricName)
	}
}

// evaluateCondition evaluates an alert condition
func (cam *CacheAlertManager) evaluateCondition(condition AlertCondition, value interface{}, threshold AlertThreshold) (bool, error) {
	switch condition {
	case AlertConditionGreaterThan:
		return cam.compareValues(value, threshold.Value, ">")
	case AlertConditionLessThan:
		return cam.compareValues(value, threshold.Value, "<")
	case AlertConditionEquals:
		return cam.compareValues(value, threshold.Value, "==")
	case AlertConditionNotEquals:
		return cam.compareValues(value, threshold.Value, "!=")
	default:
		return false, fmt.Errorf("unsupported condition: %s", condition)
	}
}

// compareValues compares two values based on operator
func (cam *CacheAlertManager) compareValues(value1, value2 interface{}, operator string) (bool, error) {
	// Handle string comparisons
	if str1, ok := value1.(string); ok {
		if str2, ok := value2.(string); ok {
			switch operator {
			case "==":
				return str1 == str2, nil
			case "!=":
				return str1 != str2, nil
			default:
				return false, fmt.Errorf("operator %s not supported for strings", operator)
			}
		}
	}

	// Handle numeric comparisons
	num1, err := cam.toFloat64(value1)
	if err != nil {
		return false, err
	}

	num2, err := cam.toFloat64(value2)
	if err != nil {
		return false, err
	}

	switch operator {
	case ">":
		return num1 > num2, nil
	case "<":
		return num1 < num2, nil
	case "==":
		return num1 == num2, nil
	case "!=":
		return num1 != num2, nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

// toFloat64 converts a value to float64
func (cam *CacheAlertManager) toFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", value)
	}
}

// getTargetCaches returns caches that match the alert targets
func (cam *CacheAlertManager) getTargetCaches(targets AlertTargets) []cachecore.Cache {
	allCaches := cam.cacheManager.ListCaches()

	if targets.AllCaches {
		return allCaches
	}

	var targetCaches []cachecore.Cache

	for _, cache := range allCaches {
		// Check if cache name matches
		if len(targets.CacheNames) > 0 {
			found := false
			for _, name := range targets.CacheNames {
				if cache.Name() == name {
					found = true
					break
				}
			}
			if found {
				targetCaches = append(targetCaches, cache)
				continue
			}
		}

		// Check if cache type matches
		if len(targets.CacheTypes) > 0 {
			stats := cache.Stats()
			for _, cacheType := range targets.CacheTypes {
				if stats.Type == cacheType {
					targetCaches = append(targetCaches, cache)
					break
				}
			}
		}
	}

	return targetCaches
}

// handleEvaluationResult handles the result of a rule evaluation
func (cam *CacheAlertManager) handleEvaluationResult(rule *AlertRule, cache cachecore.Cache, evaluation *AlertEvaluation) {
	alertID := fmt.Sprintf("%s_%s", rule.ID, cache.Name())

	cam.mu.Lock()
	defer cam.mu.Unlock()

	existingAlert, exists := cam.activeAlerts[alertID]

	if evaluation.Result {
		// Condition is true - alert should fire or continue firing
		if !exists {
			// Create new alert
			alert := &ActiveAlert{
				ID:              alertID,
				RuleID:          rule.ID,
				CacheName:       cache.Name(),
				Severity:        rule.Severity,
				State:           AlertStatePending,
				Message:         cam.generateAlertMessage(rule, cache, evaluation),
				Description:     rule.Description,
				Value:           evaluation.MetricValue,
				Threshold:       evaluation.Threshold,
				Labels:          rule.Labels,
				Annotations:     rule.Annotations,
				FiredAt:         time.Now(),
				LastEvaluation:  evaluation.Timestamp,
				EvaluationCount: 1,
				Context:         make(map[string]interface{}),
			}

			cam.activeAlerts[alertID] = alert

			if cam.logger != nil {
				cam.logger.Info("new alert created",
					logger.String("alert_id", alertID),
					logger.String("rule_id", rule.ID),
					logger.String("cache", cache.Name()),
					logger.String("severity", string(rule.Severity)),
				)
			}
		} else {
			// Update existing alert
			existingAlert.LastEvaluation = evaluation.Timestamp
			existingAlert.EvaluationCount++
			existingAlert.Value = evaluation.MetricValue

			// Check if alert should transition to firing
			if existingAlert.State == AlertStatePending {
				if time.Since(existingAlert.FiredAt) >= rule.Duration {
					existingAlert.State = AlertStateFiring
					cam.sendNotifications(existingAlert)

					if cam.logger != nil {
						cam.logger.Warn("alert is now firing",
							logger.String("alert_id", alertID),
							logger.String("rule_id", rule.ID),
							logger.String("cache", cache.Name()),
							logger.String("severity", string(rule.Severity)),
						)
					}
				}
			}
		}
	} else {
		// Condition is false - resolve alert if it exists
		if exists {
			cam.resolveAlert(alertID, "condition resolved")
		}
	}
}

// generateAlertMessage generates a message for an alert
func (cam *CacheAlertManager) generateAlertMessage(rule *AlertRule, cache cachecore.Cache, evaluation *AlertEvaluation) string {
	return fmt.Sprintf("Cache %s: %s - Value: %v, Threshold: %v",
		cache.Name(), rule.Name, evaluation.MetricValue, evaluation.Threshold)
}

// resolveAlert resolves an active alert
func (cam *CacheAlertManager) resolveAlert(alertID, reason string) {
	alert, exists := cam.activeAlerts[alertID]
	if !exists {
		return
	}

	now := time.Now()
	alert.State = AlertStateResolved
	alert.ResolvedAt = &now

	// Send resolution notifications
	cam.sendResolutionNotifications(alert)

	// Remove from active alerts
	delete(cam.activeAlerts, alertID)

	if cam.logger != nil {
		cam.logger.Info("alert resolved",
			logger.String("alert_id", alertID),
			logger.String("reason", reason),
			logger.Duration("duration", now.Sub(alert.FiredAt)),
		)
	}
}

// sendNotifications sends notifications for an alert
func (cam *CacheAlertManager) sendNotifications(alert *ActiveAlert) {
	if !cam.config.EnableNotifications {
		return
	}

	for _, notifier := range cam.notifiers {
		go func(n AlertNotifier) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			for retry := 0; retry < cam.config.NotificationRetries; retry++ {
				if err := n.SendAlert(ctx, alert); err != nil {
					if cam.logger != nil {
						cam.logger.Error("failed to send alert notification",
							logger.String("notifier", n.Name()),
							logger.String("alert_id", alert.ID),
							logger.Int("retry", retry),
							logger.Error(err),
						)
					}

					if retry < cam.config.NotificationRetries-1 {
						time.Sleep(cam.config.RetryBackoff)
					}
					continue
				}

				alert.NotificationsSent++
				break
			}
		}(notifier)
	}
}

// sendResolutionNotifications sends resolution notifications for an alert
func (cam *CacheAlertManager) sendResolutionNotifications(alert *ActiveAlert) {
	if !cam.config.EnableNotifications {
		return
	}

	for _, notifier := range cam.notifiers {
		go func(n AlertNotifier) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := n.SendResolution(ctx, alert); err != nil {
				if cam.logger != nil {
					cam.logger.Error("failed to send resolution notification",
						logger.String("notifier", n.Name()),
						logger.String("alert_id", alert.ID),
						logger.Error(err),
					)
				}
			}
		}(notifier)
	}
}

// recordEvaluation records an evaluation in the history
func (cam *CacheAlertManager) recordEvaluation(ruleID, cacheName string, evaluation *AlertEvaluation) {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	history, exists := cam.evaluationHistory[ruleID]
	if !exists {
		return
	}

	history.mu.Lock()
	defer history.mu.Unlock()

	// Add evaluation to history
	history.Evaluations = append(history.Evaluations, evaluation)

	// Trim history if needed
	if len(history.Evaluations) > cam.config.MaxAlertHistory {
		history.Evaluations = history.Evaluations[len(history.Evaluations)-cam.config.MaxAlertHistory:]
	}

	// Update statistics
	history.Statistics.TotalEvaluations++
	if evaluation.Error != "" {
		history.Statistics.FailedEvaluations++
		history.Statistics.LastError = evaluation.Error
	} else {
		history.Statistics.SuccessfulEvaluations++
		if evaluation.Result {
			history.Statistics.TrueResults++
		} else {
			history.Statistics.FalseResults++
		}
	}

	history.Statistics.LastEvaluation = evaluation.Timestamp

	// Update average latency
	if history.Statistics.TotalEvaluations > 0 {
		totalLatency := time.Duration(0)
		count := 0
		for _, eval := range history.Evaluations {
			if eval.Duration > 0 {
				totalLatency += eval.Duration
				count++
			}
		}
		if count > 0 {
			history.Statistics.AverageLatency = totalLatency / time.Duration(count)
		}
	}
}

// updateEvaluationStatistics updates evaluation statistics for a rule
func (cam *CacheAlertManager) updateEvaluationStatistics(ruleID string, duration time.Duration) {
	// This is handled in recordEvaluation
}

// cleanupExpiredAlerts cleans up expired alerts
func (cam *CacheAlertManager) cleanupExpiredAlerts() {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	now := time.Now()
	expiredAlerts := make([]string, 0)

	for alertID, alert := range cam.activeAlerts {
		if now.Sub(alert.FiredAt) > cam.config.AlertTimeout {
			expiredAlerts = append(expiredAlerts, alertID)
		}
	}

	for _, alertID := range expiredAlerts {
		alert := cam.activeAlerts[alertID]
		alert.State = AlertStateExpired
		delete(cam.activeAlerts, alertID)

		if cam.logger != nil {
			cam.logger.Info("alert expired",
				logger.String("alert_id", alertID),
				logger.Duration("age", now.Sub(alert.FiredAt)),
			)
		}
	}

	// Cleanup suppressed alerts
	for alertID, suppressUntil := range cam.suppressedAlerts {
		if now.After(suppressUntil) {
			delete(cam.suppressedAlerts, alertID)
		}
	}
}

// SuppressAlert suppresses an alert for a given duration
func (cam *CacheAlertManager) SuppressAlert(alertID string, duration time.Duration) error {
	cam.mu.Lock()
	defer cam.mu.Unlock()

	alert, exists := cam.activeAlerts[alertID]
	if !exists {
		return fmt.Errorf("alert not found: %s", alertID)
	}

	suppressUntil := time.Now().Add(duration)
	alert.State = AlertStateSuppressed
	alert.SuppressionEnd = &suppressUntil
	cam.suppressedAlerts[alertID] = suppressUntil

	if cam.logger != nil {
		cam.logger.Info("alert suppressed",
			logger.String("alert_id", alertID),
			logger.Duration("duration", duration),
		)
	}

	return nil
}
