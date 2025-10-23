package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/consensus"
	"github.com/xraph/forge/pkg/logger"
)

// ConsensusAlerter manages alerts for the consensus system
type ConsensusAlerter struct {
	consensusManager *consensus.ConsensusManager
	healthChecker    *ConsensusHealthChecker
	metricsCollector *ConsensusMetricsCollector
	logger           common.Logger
	metrics          common.Metrics
	config           ConsensusAlertConfig

	// Alert state
	alertRules   map[string]*AlertRule
	activeAlerts map[string]*ActiveAlert
	alertHistory []*AlertEvent
	notifiers    []AlertNotifier
	mu           sync.RWMutex

	// Alert processing
	alertChan   chan *AlertEvent
	stopCh      chan struct{}
	started     bool
	lastCleanup time.Time
}

// ConsensusAlertConfig contains configuration for consensus alerting
type ConsensusAlertConfig struct {
	CheckInterval      time.Duration `yaml:"check_interval" default:"30s"`
	AlertRetention     time.Duration `yaml:"alert_retention" default:"7d"`
	CooldownPeriod     time.Duration `yaml:"cooldown_period" default:"5m"`
	MaxActiveAlerts    int           `yaml:"max_active_alerts" default:"100"`
	EnableDefaultRules bool          `yaml:"enable_default_rules" default:"true"`
	BatchSize          int           `yaml:"batch_size" default:"10"`
	BufferSize         int           `yaml:"buffer_size" default:"1000"`

	// Thresholds
	LeaderElectionTimeout  time.Duration `yaml:"leader_election_timeout" default:"2m"`
	NodeUnhealthyThreshold time.Duration `yaml:"node_unhealthy_threshold" default:"5m"`
	HighElectionRate       float64       `yaml:"high_election_rate" default:"5.0"` // elections per hour
	LowHealthyNodeRatio    float64       `yaml:"low_healthy_node_ratio" default:"0.5"`
	HighErrorRate          float64       `yaml:"high_error_rate" default:"0.1"` // 10% error rate
	HighLatencyThreshold   time.Duration `yaml:"high_latency_threshold" default:"1s"`
	LogReplicationLagLimit int64         `yaml:"log_replication_lag_limit" default:"1000"`
}

// AlertRule defines a rule for triggering alerts
type AlertRule struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Severity    AlertSeverity     `json:"severity"`
	Category    AlertCategory     `json:"category"`
	Condition   AlertCondition    `json:"condition"`
	Enabled     bool              `json:"enabled"`
	Cooldown    time.Duration     `json:"cooldown"`
	Tags        map[string]string `json:"tags"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// AlertSeverity defines the severity levels for alerts
type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertCategory defines categories of alerts
type AlertCategory string

const (
	AlertCategoryElection    AlertCategory = "election"
	AlertCategoryNode        AlertCategory = "node"
	AlertCategoryCluster     AlertCategory = "cluster"
	AlertCategoryPerformance AlertCategory = "performance"
	AlertCategoryHealth      AlertCategory = "health"
	AlertCategoryNetwork     AlertCategory = "network"
)

// AlertCondition defines the condition for triggering an alert
type AlertCondition interface {
	Evaluate(ctx context.Context, data *AlertData) (bool, string, error)
	String() string
}

// AlertData contains data for alert evaluation
type AlertData struct {
	ConsensusStats     consensus.ConsensusStats
	ClusterHealth      map[string]*ClusterHealthResult
	ClusterMetrics     map[string]*ClusterMetrics
	ElectionMetrics    *ElectionMetrics
	PerformanceMetrics *PerformanceMetrics
	Timestamp          time.Time
}

// ActiveAlert represents an active alert
type ActiveAlert struct {
	ID             string                 `json:"id"`
	RuleID         string                 `json:"rule_id"`
	RuleName       string                 `json:"rule_name"`
	Severity       AlertSeverity          `json:"severity"`
	Category       AlertCategory          `json:"category"`
	Message        string                 `json:"message"`
	Details        map[string]interface{} `json:"details"`
	TriggeredAt    time.Time              `json:"triggered_at"`
	LastSeen       time.Time              `json:"last_seen"`
	Count          int64                  `json:"count"`
	Acknowledged   bool                   `json:"acknowledged"`
	AcknowledgedBy string                 `json:"acknowledged_by,omitempty"`
	AcknowledgedAt time.Time              `json:"acknowledged_at,omitempty"`
	Tags           map[string]string      `json:"tags"`
}

// AlertEvent represents an alert event in the history
type AlertEvent struct {
	ID        string                 `json:"id"`
	Type      AlertEventType         `json:"type"`
	Alert     *ActiveAlert           `json:"alert"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details"`
}

// AlertEventType defines types of alert events
type AlertEventType string

const (
	AlertEventTriggered    AlertEventType = "triggered"
	AlertEventResolved     AlertEventType = "resolved"
	AlertEventAcknowledged AlertEventType = "acknowledged"
	AlertEventEscalated    AlertEventType = "escalated"
)

// AlertNotifier defines interface for alert notifications
type AlertNotifier interface {
	Name() string
	Notify(ctx context.Context, event *AlertEvent) error
	SupportsCategory(category AlertCategory) bool
	IsEnabled() bool
}

// NewConsensusAlerter creates a new consensus alerter
func NewConsensusAlerter(
	consensusManager *consensus.ConsensusManager,
	healthChecker *ConsensusHealthChecker,
	metricsCollector *ConsensusMetricsCollector,
	logger common.Logger,
	metrics common.Metrics,
	config ConsensusAlertConfig,
) *ConsensusAlerter {
	alerter := &ConsensusAlerter{
		consensusManager: consensusManager,
		healthChecker:    healthChecker,
		metricsCollector: metricsCollector,
		logger:           logger,
		metrics:          metrics,
		config:           config,
		alertRules:       make(map[string]*AlertRule),
		activeAlerts:     make(map[string]*ActiveAlert),
		alertHistory:     make([]*AlertEvent, 0),
		notifiers:        make([]AlertNotifier, 0),
		alertChan:        make(chan *AlertEvent, config.BufferSize),
		stopCh:           make(chan struct{}),
	}

	// Initialize default alert rules if enabled
	if config.EnableDefaultRules {
		alerter.initializeDefaultRules()
	}

	return alerter
}

// Start starts the consensus alerter
func (ca *ConsensusAlerter) Start(ctx context.Context) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if ca.started {
		return nil
	}

	// Start alert processing goroutine
	go ca.processAlerts(ctx)

	// Start periodic alert evaluation
	go ca.evaluateAlertsLoop(ctx)

	// Start cleanup goroutine
	go ca.cleanupLoop(ctx)

	ca.started = true

	if ca.logger != nil {
		ca.logger.Info("consensus alerter started",
			logger.Int("alert_rules", len(ca.alertRules)),
			logger.Int("notifiers", len(ca.notifiers)),
		)
	}

	return nil
}

// Stop stops the consensus alerter
func (ca *ConsensusAlerter) Stop(ctx context.Context) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if !ca.started {
		return nil
	}

	close(ca.stopCh)
	ca.started = false

	if ca.logger != nil {
		ca.logger.Info("consensus alerter stopped")
	}

	return nil
}

// initializeDefaultRules initializes default alert rules
func (ca *ConsensusAlerter) initializeDefaultRules() {
	defaultRules := []*AlertRule{
		{
			ID:          "no-leader",
			Name:        "No Leader Present",
			Description: "Cluster has no elected leader",
			Severity:    AlertSeverityCritical,
			Category:    AlertCategoryElection,
			Condition:   &NoLeaderCondition{Timeout: ca.config.LeaderElectionTimeout},
			Enabled:     true,
			Cooldown:    ca.config.CooldownPeriod,
			Tags:        map[string]string{"type": "leadership"},
			CreatedAt:   time.Now(),
		},
		{
			ID:          "high-election-rate",
			Name:        "High Election Rate",
			Description: "Frequent leader elections detected",
			Severity:    AlertSeverityWarning,
			Category:    AlertCategoryElection,
			Condition:   &HighElectionRateCondition{Threshold: ca.config.HighElectionRate},
			Enabled:     true,
			Cooldown:    ca.config.CooldownPeriod,
			Tags:        map[string]string{"type": "election_frequency"},
			CreatedAt:   time.Now(),
		},
		{
			ID:          "node-down",
			Name:        "Node Down",
			Description: "Consensus node is unreachable",
			Severity:    AlertSeverityCritical,
			Category:    AlertCategoryNode,
			Condition:   &NodeDownCondition{Threshold: ca.config.NodeUnhealthyThreshold},
			Enabled:     true,
			Cooldown:    ca.config.CooldownPeriod,
			Tags:        map[string]string{"type": "node_failure"},
			CreatedAt:   time.Now(),
		},
		{
			ID:          "low-healthy-nodes",
			Name:        "Low Healthy Node Ratio",
			Description: "Insufficient healthy nodes in cluster",
			Severity:    AlertSeverityWarning,
			Category:    AlertCategoryCluster,
			Condition:   &LowHealthyNodesCondition{Threshold: ca.config.LowHealthyNodeRatio},
			Enabled:     true,
			Cooldown:    ca.config.CooldownPeriod,
			Tags:        map[string]string{"type": "cluster_health"},
			CreatedAt:   time.Now(),
		},
		{
			ID:          "high-error-rate",
			Name:        "High Error Rate",
			Description: "High error rate in consensus operations",
			Severity:    AlertSeverityWarning,
			Category:    AlertCategoryPerformance,
			Condition:   &HighErrorRateCondition{Threshold: ca.config.HighErrorRate},
			Enabled:     true,
			Cooldown:    ca.config.CooldownPeriod,
			Tags:        map[string]string{"type": "performance"},
			CreatedAt:   time.Now(),
		},
		{
			ID:          "high-latency",
			Name:        "High Operation Latency",
			Description: "High latency in consensus operations",
			Severity:    AlertSeverityWarning,
			Category:    AlertCategoryPerformance,
			Condition:   &HighLatencyCondition{Threshold: ca.config.HighLatencyThreshold},
			Enabled:     true,
			Cooldown:    ca.config.CooldownPeriod,
			Tags:        map[string]string{"type": "latency"},
			CreatedAt:   time.Now(),
		},
		{
			ID:          "cluster-unhealthy",
			Name:        "Cluster Unhealthy",
			Description: "Consensus cluster is in unhealthy state",
			Severity:    AlertSeverityCritical,
			Category:    AlertCategoryHealth,
			Condition:   &ClusterUnhealthyCondition{},
			Enabled:     true,
			Cooldown:    ca.config.CooldownPeriod,
			Tags:        map[string]string{"type": "cluster_health"},
			CreatedAt:   time.Now(),
		},
	}

	for _, rule := range defaultRules {
		ca.alertRules[rule.ID] = rule
	}
}

// AddRule adds a new alert rule
func (ca *ConsensusAlerter) AddRule(rule *AlertRule) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if _, exists := ca.alertRules[rule.ID]; exists {
		return fmt.Errorf("alert rule with ID %s already exists", rule.ID)
	}

	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	ca.alertRules[rule.ID] = rule

	if ca.logger != nil {
		ca.logger.Info("alert rule added",
			logger.String("rule_id", rule.ID),
			logger.String("rule_name", rule.Name),
			logger.String("severity", string(rule.Severity)),
		)
	}

	return nil
}

// RemoveRule removes an alert rule
func (ca *ConsensusAlerter) RemoveRule(ruleID string) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if _, exists := ca.alertRules[ruleID]; !exists {
		return fmt.Errorf("alert rule with ID %s not found", ruleID)
	}

	delete(ca.alertRules, ruleID)

	if ca.logger != nil {
		ca.logger.Info("alert rule removed", logger.String("rule_id", ruleID))
	}

	return nil
}

// AddNotifier adds an alert notifier
func (ca *ConsensusAlerter) AddNotifier(notifier AlertNotifier) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	ca.notifiers = append(ca.notifiers, notifier)

	if ca.logger != nil {
		ca.logger.Info("alert notifier added", logger.String("notifier", notifier.Name()))
	}
}

// evaluateAlertsLoop periodically evaluates alert rules
func (ca *ConsensusAlerter) evaluateAlertsLoop(ctx context.Context) {
	ticker := time.NewTicker(ca.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ca.evaluateAlerts(ctx)
		case <-ca.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// evaluateAlerts evaluates all alert rules
func (ca *ConsensusAlerter) evaluateAlerts(ctx context.Context) {
	// Gather data for alert evaluation
	data := ca.gatherAlertData(ctx)

	ca.mu.RLock()
	rules := make([]*AlertRule, 0, len(ca.alertRules))
	for _, rule := range ca.alertRules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	ca.mu.RUnlock()

	// Evaluate each rule
	for _, rule := range rules {
		ca.evaluateRule(ctx, rule, data)
	}
}

// gatherAlertData gathers data needed for alert evaluation
func (ca *ConsensusAlerter) gatherAlertData(ctx context.Context) *AlertData {
	data := &AlertData{
		Timestamp: time.Now(),
	}

	// Get consensus stats
	if ca.consensusManager != nil {
		data.ConsensusStats = ca.consensusManager.GetStats()
	}

	// Get cluster health
	if ca.healthChecker != nil {
		data.ClusterHealth = ca.healthChecker.checkClusterHealth(ctx)
	}

	// Get metrics
	if ca.metricsCollector != nil {
		data.ElectionMetrics = ca.metricsCollector.GetElectionMetrics()
		data.PerformanceMetrics = ca.metricsCollector.GetPerformanceMetrics()

		// Get cluster metrics
		data.ClusterMetrics = make(map[string]*ClusterMetrics)
		for _, cluster := range ca.consensusManager.GetClusters() {
			if metrics := ca.metricsCollector.GetClusterMetrics(cluster.ID()); metrics != nil {
				data.ClusterMetrics[cluster.ID()] = metrics
			}
		}
	}

	return data
}

// evaluateRule evaluates a single alert rule
func (ca *ConsensusAlerter) evaluateRule(ctx context.Context, rule *AlertRule, data *AlertData) {
	// Check if rule is in cooldown
	if ca.isRuleInCooldown(rule.ID) {
		return
	}

	// Evaluate condition
	triggered, message, err := rule.Condition.Evaluate(ctx, data)
	if err != nil {
		if ca.logger != nil {
			ca.logger.Error("failed to evaluate alert rule",
				logger.String("rule_id", rule.ID),
				logger.Error(err),
			)
		}
		return
	}

	ca.mu.Lock()
	existingAlert, exists := ca.activeAlerts[rule.ID]
	ca.mu.Unlock()

	if triggered {
		if exists {
			// Update existing alert
			ca.updateAlert(existingAlert, message)
		} else {
			// Create new alert
			ca.createAlert(rule, message, data)
		}
	} else if exists {
		// Resolve alert
		ca.resolveAlert(existingAlert)
	}
}

// createAlert creates a new alert
func (ca *ConsensusAlerter) createAlert(rule *AlertRule, message string, data *AlertData) {
	alert := &ActiveAlert{
		ID:          fmt.Sprintf("%s-%d", rule.ID, time.Now().UnixNano()),
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Severity:    rule.Severity,
		Category:    rule.Category,
		Message:     message,
		Details:     ca.buildAlertDetails(rule, data),
		TriggeredAt: time.Now(),
		LastSeen:    time.Now(),
		Count:       1,
		Tags:        rule.Tags,
	}

	ca.mu.Lock()
	ca.activeAlerts[rule.ID] = alert
	ca.mu.Unlock()

	// Create alert event
	event := &AlertEvent{
		ID:        fmt.Sprintf("event-%d", time.Now().UnixNano()),
		Type:      AlertEventTriggered,
		Alert:     alert,
		Timestamp: time.Now(),
		Details:   map[string]interface{}{"trigger_data": data},
	}

	// Send event for processing
	select {
	case ca.alertChan <- event:
	default:
		if ca.logger != nil {
			ca.logger.Warn("alert channel full, dropping event",
				logger.String("event_id", event.ID),
			)
		}
	}

	if ca.logger != nil {
		ca.logger.Warn("alert triggered",
			logger.String("alert_id", alert.ID),
			logger.String("rule_name", rule.Name),
			logger.String("severity", string(rule.Severity)),
			logger.String("message", message),
		)
	}

	if ca.metrics != nil {
		ca.metrics.Counter("forge.consensus.alerts_triggered",
			"severity", string(rule.Severity),
			"category", string(rule.Category),
		).Inc()
	}
}

// updateAlert updates an existing alert
func (ca *ConsensusAlerter) updateAlert(alert *ActiveAlert, message string) {
	ca.mu.Lock()
	alert.Message = message
	alert.LastSeen = time.Now()
	alert.Count++
	ca.mu.Unlock()
}

// resolveAlert resolves an active alert
func (ca *ConsensusAlerter) resolveAlert(alert *ActiveAlert) {
	ca.mu.Lock()
	delete(ca.activeAlerts, alert.RuleID)
	ca.mu.Unlock()

	// Create resolution event
	event := &AlertEvent{
		ID:        fmt.Sprintf("event-%d", time.Now().UnixNano()),
		Type:      AlertEventResolved,
		Alert:     alert,
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"duration": time.Since(alert.TriggeredAt).String(),
			"count":    alert.Count,
		},
	}

	// Send event for processing
	select {
	case ca.alertChan <- event:
	default:
		if ca.logger != nil {
			ca.logger.Warn("alert channel full, dropping resolution event",
				logger.String("event_id", event.ID),
			)
		}
	}

	if ca.logger != nil {
		ca.logger.Info("alert resolved",
			logger.String("alert_id", alert.ID),
			logger.String("rule_name", alert.RuleName),
			logger.Duration("duration", time.Since(alert.TriggeredAt)),
		)
	}

	if ca.metrics != nil {
		ca.metrics.Counter("forge.consensus.alerts_resolved",
			"severity", string(alert.Severity),
			"category", string(alert.Category),
		).Inc()
	}
}

// buildAlertDetails builds details for an alert
func (ca *ConsensusAlerter) buildAlertDetails(rule *AlertRule, data *AlertData) map[string]interface{} {
	details := make(map[string]interface{})

	// Add basic consensus stats
	if data.ConsensusStats.Clusters > 0 {
		details["total_clusters"] = data.ConsensusStats.Clusters
		details["active_clusters"] = data.ConsensusStats.ActiveClusters
		details["total_nodes"] = data.ConsensusStats.TotalNodes
	}

	// Add category-specific details
	switch rule.Category {
	case AlertCategoryElection:
		if data.ElectionMetrics != nil {
			details["total_elections"] = data.ElectionMetrics.TotalElections
			details["failed_elections"] = data.ElectionMetrics.FailedElections
			details["last_election"] = data.ElectionMetrics.LastElection
		}
	case AlertCategoryPerformance:
		if data.PerformanceMetrics != nil {
			details["error_rate"] = data.PerformanceMetrics.ErrorRate
			details["operations_per_second"] = data.PerformanceMetrics.OperationsPerSecond
			details["average_latency"] = data.PerformanceMetrics.AverageLatency.String()
		}
	case AlertCategoryHealth:
		healthyClusters := 0
		unhealthyClusters := 0
		for _, health := range data.ClusterHealth {
			if health.Status == common.HealthStatusHealthy {
				healthyClusters++
			} else if health.Status == common.HealthStatusUnhealthy {
				unhealthyClusters++
			}
		}
		details["healthy_clusters"] = healthyClusters
		details["unhealthy_clusters"] = unhealthyClusters
	}

	return details
}

// processAlerts processes alert events
func (ca *ConsensusAlerter) processAlerts(ctx context.Context) {
	for {
		select {
		case event := <-ca.alertChan:
			ca.processAlertEvent(ctx, event)
		case <-ca.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// processAlertEvent processes a single alert event
func (ca *ConsensusAlerter) processAlertEvent(ctx context.Context, event *AlertEvent) {
	// Add to history
	ca.mu.Lock()
	ca.alertHistory = append(ca.alertHistory, event)
	ca.mu.Unlock()

	// Notify all applicable notifiers
	for _, notifier := range ca.notifiers {
		if notifier.IsEnabled() && notifier.SupportsCategory(event.Alert.Category) {
			go func(n AlertNotifier) {
				if err := n.Notify(ctx, event); err != nil && ca.logger != nil {
					ca.logger.Error("failed to send alert notification",
						logger.String("notifier", n.Name()),
						logger.String("alert_id", event.Alert.ID),
						logger.Error(err),
					)
				}
			}(notifier)
		}
	}
}

// isRuleInCooldown checks if a rule is in cooldown period
func (ca *ConsensusAlerter) isRuleInCooldown(ruleID string) bool {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	rule, exists := ca.alertRules[ruleID]
	if !exists {
		return false
	}

	// Check if there's an active alert for this rule
	if alert, exists := ca.activeAlerts[ruleID]; exists {
		return time.Since(alert.LastSeen) < rule.Cooldown
	}

	// Check recent history for cooldown
	cutoff := time.Now().Add(-rule.Cooldown)
	for i := len(ca.alertHistory) - 1; i >= 0; i-- {
		event := ca.alertHistory[i]
		if event.Timestamp.Before(cutoff) {
			break
		}
		if event.Alert.RuleID == ruleID && event.Type == AlertEventResolved {
			return true
		}
	}

	return false
}

// cleanupLoop performs periodic cleanup of old data
func (ca *ConsensusAlerter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour) // Cleanup every hour
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ca.cleanup()
		case <-ca.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// cleanup removes old alert history
func (ca *ConsensusAlerter) cleanup() {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	cutoff := time.Now().Add(-ca.config.AlertRetention)

	// Remove old history entries
	var newHistory []*AlertEvent
	for _, event := range ca.alertHistory {
		if event.Timestamp.After(cutoff) {
			newHistory = append(newHistory, event)
		}
	}

	removed := len(ca.alertHistory) - len(newHistory)
	ca.alertHistory = newHistory
	ca.lastCleanup = time.Now()

	if ca.logger != nil && removed > 0 {
		ca.logger.Debug("cleaned up old alert history",
			logger.Int("removed", removed),
			logger.Int("remaining", len(newHistory)),
		)
	}
}

// AcknowledgeAlert acknowledges an active alert
func (ca *ConsensusAlerter) AcknowledgeAlert(alertID, acknowledgedBy string) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	// Find the alert by alert ID
	var alert *ActiveAlert
	for _, a := range ca.activeAlerts {
		if a.ID == alertID {
			alert = a
			break
		}
	}

	if alert == nil {
		return fmt.Errorf("alert with ID %s not found", alertID)
	}

	alert.Acknowledged = true
	alert.AcknowledgedBy = acknowledgedBy
	alert.AcknowledgedAt = time.Now()

	// Create acknowledgment event
	event := &AlertEvent{
		ID:        fmt.Sprintf("event-%d", time.Now().UnixNano()),
		Type:      AlertEventAcknowledged,
		Alert:     alert,
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"acknowledged_by": acknowledgedBy,
		},
	}

	// Send event for processing
	select {
	case ca.alertChan <- event:
	default:
		if ca.logger != nil {
			ca.logger.Warn("alert channel full, dropping acknowledgment event")
		}
	}

	if ca.logger != nil {
		ca.logger.Info("alert acknowledged",
			logger.String("alert_id", alertID),
			logger.String("acknowledged_by", acknowledgedBy),
		)
	}

	return nil
}

// GetActiveAlerts returns all active alerts
func (ca *ConsensusAlerter) GetActiveAlerts() []*ActiveAlert {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	alerts := make([]*ActiveAlert, 0, len(ca.activeAlerts))
	for _, alert := range ca.activeAlerts {
		alerts = append(alerts, alert)
	}

	return alerts
}

// GetAlertHistory returns alert history
func (ca *ConsensusAlerter) GetAlertHistory(limit int) []*AlertEvent {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	if limit <= 0 || limit > len(ca.alertHistory) {
		limit = len(ca.alertHistory)
	}

	// Return the most recent events
	start := len(ca.alertHistory) - limit
	history := make([]*AlertEvent, limit)
	copy(history, ca.alertHistory[start:])

	return history
}

// GetAlertRules returns all alert rules
func (ca *ConsensusAlerter) GetAlertRules() []*AlertRule {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	rules := make([]*AlertRule, 0, len(ca.alertRules))
	for _, rule := range ca.alertRules {
		rules = append(rules, rule)
	}

	return rules
}

// GetAlertStats returns alerting statistics
func (ca *ConsensusAlerter) GetAlertStats() *AlertStats {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	stats := &AlertStats{
		TotalRules:       len(ca.alertRules),
		EnabledRules:     0,
		ActiveAlerts:     len(ca.activeAlerts),
		TotalHistory:     len(ca.alertHistory),
		LastCleanup:      ca.lastCleanup,
		AlertsByCategory: make(map[AlertCategory]int),
		AlertsBySeverity: make(map[AlertSeverity]int),
	}

	// Count enabled rules
	for _, rule := range ca.alertRules {
		if rule.Enabled {
			stats.EnabledRules++
		}
	}

	// Count active alerts by category and severity
	for _, alert := range ca.activeAlerts {
		stats.AlertsByCategory[alert.Category]++
		stats.AlertsBySeverity[alert.Severity]++
	}

	return stats
}

// AlertStats contains alerting statistics
type AlertStats struct {
	TotalRules       int                   `json:"total_rules"`
	EnabledRules     int                   `json:"enabled_rules"`
	ActiveAlerts     int                   `json:"active_alerts"`
	TotalHistory     int                   `json:"total_history"`
	LastCleanup      time.Time             `json:"last_cleanup"`
	AlertsByCategory map[AlertCategory]int `json:"alerts_by_category"`
	AlertsBySeverity map[AlertSeverity]int `json:"alerts_by_severity"`
}

// Alert condition implementations

// NoLeaderCondition checks if there's no leader for too long
type NoLeaderCondition struct {
	Timeout time.Duration
}

func (c *NoLeaderCondition) Evaluate(ctx context.Context, data *AlertData) (bool, string, error) {
	for clusterID, health := range data.ClusterHealth {
		if !health.LeaderPresent {
			timeSinceElection := time.Since(health.LastElection)
			if timeSinceElection > c.Timeout {
				return true, fmt.Sprintf("Cluster %s has no leader for %v", clusterID, timeSinceElection), nil
			}
		}
	}
	return false, "", nil
}

func (c *NoLeaderCondition) String() string {
	return fmt.Sprintf("no_leader_timeout > %v", c.Timeout)
}

// HighElectionRateCondition checks for high election frequency
type HighElectionRateCondition struct {
	Threshold float64
}

func (c *HighElectionRateCondition) Evaluate(ctx context.Context, data *AlertData) (bool, string, error) {
	if data.ElectionMetrics != nil && data.ElectionMetrics.ElectionsPerHour > c.Threshold {
		return true, fmt.Sprintf("High election rate: %.2f elections/hour", data.ElectionMetrics.ElectionsPerHour), nil
	}
	return false, "", nil
}

func (c *HighElectionRateCondition) String() string {
	return fmt.Sprintf("elections_per_hour > %.2f", c.Threshold)
}

// NodeDownCondition checks for nodes that have been down too long
type NodeDownCondition struct {
	Threshold time.Duration
}

func (c *NodeDownCondition) Evaluate(ctx context.Context, data *AlertData) (bool, string, error) {
	for clusterID, health := range data.ClusterHealth {
		for nodeID, nodeHealth := range health.NodeHealth {
			if !nodeHealth.IsReachable {
				downTime := time.Since(nodeHealth.LastHeartbeat)
				if downTime > c.Threshold {
					return true, fmt.Sprintf("Node %s in cluster %s down for %v", nodeID, clusterID, downTime), nil
				}
			}
		}
	}
	return false, "", nil
}

func (c *NodeDownCondition) String() string {
	return fmt.Sprintf("node_down_time > %v", c.Threshold)
}

// LowHealthyNodesCondition checks for low healthy node ratio
type LowHealthyNodesCondition struct {
	Threshold float64
}

func (c *LowHealthyNodesCondition) Evaluate(ctx context.Context, data *AlertData) (bool, string, error) {
	for clusterID, health := range data.ClusterHealth {
		if health.TotalNodes > 0 {
			ratio := float64(health.HealthyNodes) / float64(health.TotalNodes)
			if ratio < c.Threshold {
				return true, fmt.Sprintf("Low healthy node ratio in cluster %s: %.2f", clusterID, ratio), nil
			}
		}
	}
	return false, "", nil
}

func (c *LowHealthyNodesCondition) String() string {
	return fmt.Sprintf("healthy_node_ratio < %.2f", c.Threshold)
}

// HighErrorRateCondition checks for high error rates
type HighErrorRateCondition struct {
	Threshold float64
}

func (c *HighErrorRateCondition) Evaluate(ctx context.Context, data *AlertData) (bool, string, error) {
	if data.PerformanceMetrics != nil && data.PerformanceMetrics.ErrorRate > c.Threshold {
		return true, fmt.Sprintf("High error rate: %.2f%%", data.PerformanceMetrics.ErrorRate*100), nil
	}
	return false, "", nil
}

func (c *HighErrorRateCondition) String() string {
	return fmt.Sprintf("error_rate > %.2f", c.Threshold)
}

// HighLatencyCondition checks for high operation latency
type HighLatencyCondition struct {
	Threshold time.Duration
}

func (c *HighLatencyCondition) Evaluate(ctx context.Context, data *AlertData) (bool, string, error) {
	if data.PerformanceMetrics != nil && data.PerformanceMetrics.AverageLatency > c.Threshold {
		return true, fmt.Sprintf("High average latency: %v", data.PerformanceMetrics.AverageLatency), nil
	}
	return false, "", nil
}

func (c *HighLatencyCondition) String() string {
	return fmt.Sprintf("average_latency > %v", c.Threshold)
}

// ClusterUnhealthyCondition checks for unhealthy clusters
type ClusterUnhealthyCondition struct{}

func (c *ClusterUnhealthyCondition) Evaluate(ctx context.Context, data *AlertData) (bool, string, error) {
	for clusterID, health := range data.ClusterHealth {
		if health.Status == common.HealthStatusUnhealthy {
			return true, fmt.Sprintf("Cluster %s is unhealthy: %s", clusterID, health.Message), nil
		}
	}
	return false, "", nil
}

func (c *ClusterUnhealthyCondition) String() string {
	return "cluster_health == unhealthy"
}

// RegisterConsensusAlerts registers consensus alerting system
func RegisterConsensusAlerts(
	consensusManager *consensus.ConsensusManager,
	healthChecker *ConsensusHealthChecker,
	metricsCollector *ConsensusMetricsCollector,
	logger common.Logger,
	metrics common.Metrics,
) (*ConsensusAlerter, error) {
	config := ConsensusAlertConfig{
		CheckInterval:          30 * time.Second,
		AlertRetention:         7 * 24 * time.Hour,
		CooldownPeriod:         5 * time.Minute,
		MaxActiveAlerts:        100,
		EnableDefaultRules:     true,
		BatchSize:              10,
		BufferSize:             1000,
		LeaderElectionTimeout:  2 * time.Minute,
		NodeUnhealthyThreshold: 5 * time.Minute,
		HighElectionRate:       5.0,
		LowHealthyNodeRatio:    0.5,
		HighErrorRate:          0.1,
		HighLatencyThreshold:   1 * time.Second,
		LogReplicationLagLimit: 1000,
	}

	alerter := NewConsensusAlerter(consensusManager, healthChecker, metricsCollector, logger, metrics, config)
	return alerter, nil
}
