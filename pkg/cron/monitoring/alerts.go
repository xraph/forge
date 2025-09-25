package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	cron "github.com/xraph/forge/pkg/cron/core"
	"github.com/xraph/forge/pkg/logger"
)

// AlertManager manages alerts for the cron system
type AlertManager struct {
	manager      cron.Manager
	logger       common.Logger
	metrics      common.Metrics
	notifiers    map[string]Notifier
	rules        []*AlertRule
	activeAlerts map[string]*Alert
	alertHistory []*Alert
	mu           sync.RWMutex
	running      bool
	stopChan     chan struct{}
}

// AlertRule defines a rule for triggering alerts
type AlertRule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Condition   AlertCondition         `json:"condition"`
	Severity    AlertSeverity          `json:"severity"`
	Cooldown    time.Duration          `json:"cooldown"`
	Notifiers   []string               `json:"notifiers"`
	Enabled     bool                   `json:"enabled"`
	Tags        map[string]string      `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// AlertCondition defines when an alert should be triggered
type AlertCondition struct {
	Type       AlertConditionType     `json:"type"`
	JobID      string                 `json:"job_id,omitempty"`
	Threshold  interface{}            `json:"threshold,omitempty"`
	Duration   time.Duration          `json:"duration,omitempty"`
	Operator   AlertOperator          `json:"operator,omitempty"`
	Metric     string                 `json:"metric,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// AlertConditionType represents the type of alert condition
type AlertConditionType string

const (
	AlertConditionJobFailed        AlertConditionType = "job_failed"
	AlertConditionJobTimeout       AlertConditionType = "job_timeout"
	AlertConditionJobMissed        AlertConditionType = "job_missed"
	AlertConditionHighFailureRate  AlertConditionType = "high_failure_rate"
	AlertConditionLongRunningJob   AlertConditionType = "long_running_job"
	AlertConditionClusterUnhealthy AlertConditionType = "cluster_unhealthy"
	AlertConditionLeaderChange     AlertConditionType = "leader_change"
	AlertConditionNodeDown         AlertConditionType = "node_down"
	AlertConditionQueueBacklog     AlertConditionType = "queue_backlog"
	AlertConditionCustomMetric     AlertConditionType = "custom_metric"
)

// AlertOperator represents comparison operators for conditions
type AlertOperator string

const (
	AlertOperatorEquals       AlertOperator = "eq"
	AlertOperatorNotEquals    AlertOperator = "ne"
	AlertOperatorGreaterThan  AlertOperator = "gt"
	AlertOperatorLessThan     AlertOperator = "lt"
	AlertOperatorGreaterEqual AlertOperator = "ge"
	AlertOperatorLessEqual    AlertOperator = "le"
	AlertOperatorContains     AlertOperator = "contains"
	AlertOperatorNotContains  AlertOperator = "not_contains"
)

// AlertSeverity represents the severity of an alert
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityError    AlertSeverity = "error"
	AlertSeverityCritical AlertSeverity = "critical"
)

// Alert represents an active or historical alert
type Alert struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"rule_id"`
	RuleName    string                 `json:"rule_name"`
	JobID       string                 `json:"job_id,omitempty"`
	JobName     string                 `json:"job_name,omitempty"`
	NodeID      string                 `json:"node_id,omitempty"`
	Severity    AlertSeverity          `json:"severity"`
	Status      AlertStatus            `json:"status"`
	Message     string                 `json:"message"`
	Description string                 `json:"description"`
	Details     map[string]interface{} `json:"details"`
	Tags        map[string]string      `json:"tags"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	ResolvedAt  *time.Time             `json:"resolved_at,omitempty"`
	NotifiedAt  *time.Time             `json:"notified_at,omitempty"`
	Notifiers   []string               `json:"notifiers"`
	Attempts    int                    `json:"attempts"`
	LastAttempt time.Time              `json:"last_attempt"`
}

// AlertStatus represents the status of an alert
type AlertStatus string

const (
	AlertStatusActive   AlertStatus = "active"
	AlertStatusResolved AlertStatus = "resolved"
	AlertStatusMuted    AlertStatus = "muted"
	AlertStatusFailed   AlertStatus = "failed"
)

// Notifier defines the interface for alert notifiers
type Notifier interface {
	Name() string
	SendAlert(ctx context.Context, alert *Alert) error
	TestConnection(ctx context.Context) error
	GetConfig() map[string]interface{}
}

// AlertFilter represents filter criteria for querying alerts
type AlertFilter struct {
	IDs           []string          `json:"ids,omitempty"`
	RuleIDs       []string          `json:"rule_ids,omitempty"`
	JobIDs        []string          `json:"job_ids,omitempty"`
	NodeIDs       []string          `json:"node_ids,omitempty"`
	Severities    []AlertSeverity   `json:"severities,omitempty"`
	Statuses      []AlertStatus     `json:"statuses,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	CreatedAfter  *time.Time        `json:"created_after,omitempty"`
	CreatedBefore *time.Time        `json:"created_before,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	Offset        int               `json:"offset,omitempty"`
}

// AlertStats represents alert statistics
type AlertStats struct {
	TotalAlerts      int64                   `json:"total_alerts"`
	ActiveAlerts     int64                   `json:"active_alerts"`
	ResolvedAlerts   int64                   `json:"resolved_alerts"`
	MutedAlerts      int64                   `json:"muted_alerts"`
	FailedAlerts     int64                   `json:"failed_alerts"`
	AlertsBySeverity map[AlertSeverity]int64 `json:"alerts_by_severity"`
	AlertsByRule     map[string]int64        `json:"alerts_by_rule"`
	AlertsByJob      map[string]int64        `json:"alerts_by_job"`
	LastUpdate       time.Time               `json:"last_update"`
}

// NewAlertManager creates a new alert manager
func NewAlertManager(manager cron.Manager, logger common.Logger, metrics common.Metrics) *AlertManager {
	am := &AlertManager{
		manager:      manager,
		logger:       logger,
		metrics:      metrics,
		notifiers:    make(map[string]Notifier),
		rules:        make([]*AlertRule, 0),
		activeAlerts: make(map[string]*Alert),
		alertHistory: make([]*Alert, 0),
		stopChan:     make(chan struct{}),
	}

	// Register default alert rules
	am.registerDefaultRules()

	return am
}

// RegisterNotifier registers a notifier
func (am *AlertManager) RegisterNotifier(notifier Notifier) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	name := notifier.Name()
	if _, exists := am.notifiers[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	am.notifiers[name] = notifier

	if am.logger != nil {
		am.logger.Info("notifier registered",
			logger.String("name", name),
		)
	}

	return nil
}

// UnregisterNotifier unregisters a notifier
func (am *AlertManager) UnregisterNotifier(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.notifiers[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(am.notifiers, name)

	if am.logger != nil {
		am.logger.Info("notifier unregistered",
			logger.String("name", name),
		)
	}

	return nil
}

// AddRule adds an alert rule
func (am *AlertManager) AddRule(rule *AlertRule) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Check if rule already exists
	for _, existingRule := range am.rules {
		if existingRule.ID == rule.ID {
			return common.ErrServiceAlreadyExists(rule.ID)
		}
	}

	// Validate rule
	if err := am.validateRule(rule); err != nil {
		return err
	}

	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	am.rules = append(am.rules, rule)

	if am.logger != nil {
		am.logger.Info("alert rule added",
			logger.String("id", rule.ID),
			logger.String("name", rule.Name),
			logger.String("condition", string(rule.Condition.Type)),
			logger.String("severity", string(rule.Severity)),
		)
	}

	return nil
}

// RemoveRule removes an alert rule
func (am *AlertManager) RemoveRule(ruleID string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	for i, rule := range am.rules {
		if rule.ID == ruleID {
			am.rules = append(am.rules[:i], am.rules[i+1:]...)

			if am.logger != nil {
				am.logger.Info("alert rule removed",
					logger.String("id", ruleID),
				)
			}

			return nil
		}
	}

	return common.ErrServiceNotFound(ruleID)
}

// UpdateRule updates an alert rule
func (am *AlertManager) UpdateRule(rule *AlertRule) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	for i, existingRule := range am.rules {
		if existingRule.ID == rule.ID {
			// Validate rule
			if err := am.validateRule(rule); err != nil {
				return err
			}

			rule.CreatedAt = existingRule.CreatedAt
			rule.UpdatedAt = time.Now()
			am.rules[i] = rule

			if am.logger != nil {
				am.logger.Info("alert rule updated",
					logger.String("id", rule.ID),
					logger.String("name", rule.Name),
				)
			}

			return nil
		}
	}

	return common.ErrServiceNotFound(rule.ID)
}

// GetRule returns an alert rule
func (am *AlertManager) GetRule(ruleID string) (*AlertRule, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	for _, rule := range am.rules {
		if rule.ID == ruleID {
			return rule, nil
		}
	}

	return nil, common.ErrServiceNotFound(ruleID)
}

// ListRules returns all alert rules
func (am *AlertManager) ListRules() []*AlertRule {
	am.mu.RLock()
	defer am.mu.RUnlock()

	rules := make([]*AlertRule, len(am.rules))
	copy(rules, am.rules)
	return rules
}

// Start starts the alert manager
func (am *AlertManager) Start(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.running {
		return common.ErrLifecycleError("start", fmt.Errorf("alert manager already running"))
	}

	am.running = true
	am.stopChan = make(chan struct{})

	// OnStart background alert monitoring
	go am.monitorAlerts()

	if am.logger != nil {
		am.logger.Info("alert manager started",
			logger.Int("rules_count", len(am.rules)),
			logger.Int("notifiers_count", len(am.notifiers)),
		)
	}

	return nil
}

// Stop stops the alert manager
func (am *AlertManager) Stop(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if !am.running {
		return common.ErrLifecycleError("stop", fmt.Errorf("alert manager not running"))
	}

	close(am.stopChan)
	am.running = false

	if am.logger != nil {
		am.logger.Info("alert manager stopped")
	}

	return nil
}

// IsRunning returns true if the alert manager is running
func (am *AlertManager) IsRunning() bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.running
}

// TriggerAlert triggers an alert based on job execution
func (am *AlertManager) TriggerAlert(jobID string, execution *cron.JobExecution) {
	am.mu.RLock()
	rules := make([]*AlertRule, len(am.rules))
	copy(rules, am.rules)
	am.mu.RUnlock()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// Check if rule condition matches
		if am.evaluateCondition(rule.Condition, jobID, execution) {
			am.createAlert(rule, jobID, execution)
		}
	}
}

// GetActiveAlerts returns all active alerts
func (am *AlertManager) GetActiveAlerts() []*Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	alerts := make([]*Alert, 0, len(am.activeAlerts))
	for _, alert := range am.activeAlerts {
		alerts = append(alerts, alert)
	}

	return alerts
}

// GetAlertHistory returns alert history
func (am *AlertManager) GetAlertHistory(filter *AlertFilter) []*Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	alerts := make([]*Alert, 0)
	for _, alert := range am.alertHistory {
		if am.matchesFilter(alert, filter) {
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

// ResolveAlert resolves an active alert
func (am *AlertManager) ResolveAlert(alertID string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	alert, exists := am.activeAlerts[alertID]
	if !exists {
		return common.ErrServiceNotFound(alertID)
	}

	// Mark as resolved
	alert.Status = AlertStatusResolved
	now := time.Now()
	alert.ResolvedAt = &now
	alert.UpdatedAt = now

	// Move to history
	am.alertHistory = append(am.alertHistory, alert)
	delete(am.activeAlerts, alertID)

	if am.logger != nil {
		am.logger.Info("alert resolved",
			logger.String("id", alertID),
			logger.String("rule_id", alert.RuleID),
		)
	}

	return nil
}

// MuteAlert mutes an active alert
func (am *AlertManager) MuteAlert(alertID string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	alert, exists := am.activeAlerts[alertID]
	if !exists {
		return common.ErrServiceNotFound(alertID)
	}

	alert.Status = AlertStatusMuted
	alert.UpdatedAt = time.Now()

	if am.logger != nil {
		am.logger.Info("alert muted",
			logger.String("id", alertID),
			logger.String("rule_id", alert.RuleID),
		)
	}

	return nil
}

// GetStats returns alert statistics
func (am *AlertManager) GetStats() *AlertStats {
	am.mu.RLock()
	defer am.mu.RUnlock()

	stats := &AlertStats{
		AlertsBySeverity: make(map[AlertSeverity]int64),
		AlertsByRule:     make(map[string]int64),
		AlertsByJob:      make(map[string]int64),
		LastUpdate:       time.Now(),
	}

	// Count active alerts
	for _, alert := range am.activeAlerts {
		stats.TotalAlerts++
		switch alert.Status {
		case AlertStatusActive:
			stats.ActiveAlerts++
		case AlertStatusMuted:
			stats.MutedAlerts++
		case AlertStatusFailed:
			stats.FailedAlerts++
		}

		stats.AlertsBySeverity[alert.Severity]++
		stats.AlertsByRule[alert.RuleID]++
		if alert.JobID != "" {
			stats.AlertsByJob[alert.JobID]++
		}
	}

	// Count historical alerts
	for _, alert := range am.alertHistory {
		stats.TotalAlerts++
		if alert.Status == AlertStatusResolved {
			stats.ResolvedAlerts++
		}

		stats.AlertsBySeverity[alert.Severity]++
		stats.AlertsByRule[alert.RuleID]++
		if alert.JobID != "" {
			stats.AlertsByJob[alert.JobID]++
		}
	}

	return stats
}

// monitorAlerts monitors for alert conditions
func (am *AlertManager) monitorAlerts() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			am.checkAlertConditions()
		case <-am.stopChan:
			return
		}
	}
}

// checkAlertConditions checks all alert conditions
func (am *AlertManager) checkAlertConditions() {
	am.mu.RLock()
	rules := make([]*AlertRule, len(am.rules))
	copy(rules, am.rules)
	am.mu.RUnlock()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// Check system-wide conditions
		if am.evaluateSystemCondition(rule.Condition) {
			am.createSystemAlert(rule)
		}
	}
}

// evaluateCondition evaluates if a condition matches
func (am *AlertManager) evaluateCondition(condition AlertCondition, jobID string, execution *cron.JobExecution) bool {
	// Check if condition applies to this job
	if condition.JobID != "" && condition.JobID != jobID {
		return false
	}

	switch condition.Type {
	case AlertConditionJobFailed:
		return execution.IsFailure()
	case AlertConditionJobTimeout:
		return execution.Status == cron.ExecutionStatusTimeout
	case AlertConditionLongRunningJob:
		if threshold, ok := condition.Threshold.(time.Duration); ok {
			return execution.Duration > threshold
		}
		return false
	default:
		return false
	}
}

// evaluateSystemCondition evaluates system-wide conditions
func (am *AlertManager) evaluateSystemCondition(condition AlertCondition) bool {
	switch condition.Type {
	case AlertConditionClusterUnhealthy:
		stats := am.manager.GetStats()
		return !stats.ClusterHealthy
	case AlertConditionHighFailureRate:
		stats := am.manager.GetStats()
		if stats.TotalExecutions > 0 {
			failureRate := float64(stats.FailedExecutions) / float64(stats.TotalExecutions)
			if threshold, ok := condition.Threshold.(float64); ok {
				return failureRate > threshold
			}
		}
		return false
	default:
		return false
	}
}

// createAlert creates a new alert
func (am *AlertManager) createAlert(rule *AlertRule, jobID string, execution *cron.JobExecution) {
	alertID := fmt.Sprintf("%s-%s-%d", rule.ID, jobID, time.Now().UnixNano())

	// Check cooldown
	if am.isInCooldown(rule.ID, jobID) {
		return
	}

	alert := &Alert{
		ID:          alertID,
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		JobID:       jobID,
		NodeID:      execution.NodeID,
		Severity:    rule.Severity,
		Status:      AlertStatusActive,
		Message:     am.generateAlertMessage(rule, jobID, execution),
		Description: rule.Description,
		Details:     am.generateAlertDetails(rule, jobID, execution),
		Tags:        rule.Tags,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Notifiers:   rule.Notifiers,
		Attempts:    0,
	}

	am.mu.Lock()
	am.activeAlerts[alertID] = alert
	am.mu.Unlock()

	// Send notifications
	go am.sendNotifications(alert)

	if am.logger != nil {
		am.logger.Warn("alert created",
			logger.String("id", alertID),
			logger.String("rule_id", rule.ID),
			logger.String("job_id", jobID),
			logger.String("severity", string(rule.Severity)),
		)
	}
}

// createSystemAlert creates a system-wide alert
func (am *AlertManager) createSystemAlert(rule *AlertRule) {
	alertID := fmt.Sprintf("%s-system-%d", rule.ID, time.Now().UnixNano())

	// Check cooldown
	if am.isInCooldown(rule.ID, "system") {
		return
	}

	alert := &Alert{
		ID:          alertID,
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Severity:    rule.Severity,
		Status:      AlertStatusActive,
		Message:     am.generateSystemAlertMessage(rule),
		Description: rule.Description,
		Details:     am.generateSystemAlertDetails(rule),
		Tags:        rule.Tags,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Notifiers:   rule.Notifiers,
		Attempts:    0,
	}

	am.mu.Lock()
	am.activeAlerts[alertID] = alert
	am.mu.Unlock()

	// Send notifications
	go am.sendNotifications(alert)

	if am.logger != nil {
		am.logger.Warn("system alert created",
			logger.String("id", alertID),
			logger.String("rule_id", rule.ID),
			logger.String("severity", string(rule.Severity)),
		)
	}
}

// sendNotifications sends notifications for an alert
func (am *AlertManager) sendNotifications(alert *Alert) {
	am.mu.RLock()
	notifiers := make(map[string]Notifier)
	for _, notifierName := range alert.Notifiers {
		if notifier, exists := am.notifiers[notifierName]; exists {
			notifiers[notifierName] = notifier
		}
	}
	am.mu.RUnlock()

	for notifierName, notifier := range notifiers {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		err := notifier.SendAlert(ctx, alert)
		cancel()

		am.mu.Lock()
		alert.Attempts++
		alert.LastAttempt = time.Now()

		if err != nil {
			if am.logger != nil {
				am.logger.Error("failed to send alert notification",
					logger.String("alert_id", alert.ID),
					logger.String("notifier", notifierName),
					logger.Error(err),
				)
			}
		} else {
			now := time.Now()
			alert.NotifiedAt = &now

			if am.logger != nil {
				am.logger.Info("alert notification sent",
					logger.String("alert_id", alert.ID),
					logger.String("notifier", notifierName),
				)
			}
		}
		am.mu.Unlock()
	}
}

// Helper methods
func (am *AlertManager) registerDefaultRules() {
	// Job failure rule
	am.rules = append(am.rules, &AlertRule{
		ID:          "job-failure",
		Name:        "Job Failure",
		Description: "Triggered when a job execution fails",
		Condition: AlertCondition{
			Type: AlertConditionJobFailed,
		},
		Severity:  AlertSeverityError,
		Cooldown:  5 * time.Minute,
		Enabled:   true,
		Tags:      map[string]string{"type": "job", "category": "execution"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// Job timeout rule
	am.rules = append(am.rules, &AlertRule{
		ID:          "job-timeout",
		Name:        "Job Timeout",
		Description: "Triggered when a job execution times out",
		Condition: AlertCondition{
			Type: AlertConditionJobTimeout,
		},
		Severity:  AlertSeverityWarning,
		Cooldown:  5 * time.Minute,
		Enabled:   true,
		Tags:      map[string]string{"type": "job", "category": "execution"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// High failure rate rule
	am.rules = append(am.rules, &AlertRule{
		ID:          "high-failure-rate",
		Name:        "High Failure Rate",
		Description: "Triggered when the overall failure rate is high",
		Condition: AlertCondition{
			Type:      AlertConditionHighFailureRate,
			Threshold: 0.2, // 20% failure rate
		},
		Severity:  AlertSeverityCritical,
		Cooldown:  10 * time.Minute,
		Enabled:   true,
		Tags:      map[string]string{"type": "system", "category": "performance"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
}

func (am *AlertManager) validateRule(rule *AlertRule) error {
	if rule.ID == "" {
		return common.ErrValidationError("id", fmt.Errorf("rule ID cannot be empty"))
	}
	if rule.Name == "" {
		return common.ErrValidationError("name", fmt.Errorf("rule name cannot be empty"))
	}
	if rule.Cooldown < 0 {
		return common.ErrValidationError("cooldown", fmt.Errorf("cooldown cannot be negative"))
	}
	return nil
}

func (am *AlertManager) isInCooldown(ruleID, jobID string) bool {
	// Check if there's a recent alert for this rule and job
	for _, alert := range am.activeAlerts {
		if alert.RuleID == ruleID && alert.JobID == jobID {
			// Find the rule
			for _, rule := range am.rules {
				if rule.ID == ruleID {
					return time.Since(alert.CreatedAt) < rule.Cooldown
				}
			}
		}
	}
	return false
}

func (am *AlertManager) generateAlertMessage(rule *AlertRule, jobID string, execution *cron.JobExecution) string {
	switch rule.Condition.Type {
	case AlertConditionJobFailed:
		return fmt.Sprintf("Job %s failed: %s", jobID, execution.Error)
	case AlertConditionJobTimeout:
		return fmt.Sprintf("Job %s timed out after %v", jobID, execution.Duration)
	default:
		return fmt.Sprintf("Alert triggered for job %s", jobID)
	}
}

func (am *AlertManager) generateSystemAlertMessage(rule *AlertRule) string {
	switch rule.Condition.Type {
	case AlertConditionClusterUnhealthy:
		return "Cron cluster is unhealthy"
	case AlertConditionHighFailureRate:
		return "High job failure rate detected"
	default:
		return "System alert triggered"
	}
}

func (am *AlertManager) generateAlertDetails(rule *AlertRule, jobID string, execution *cron.JobExecution) map[string]interface{} {
	details := make(map[string]interface{})
	details["job_id"] = jobID
	details["execution_id"] = execution.ID
	details["node_id"] = execution.NodeID
	details["status"] = execution.Status
	details["duration"] = execution.Duration
	details["attempt"] = execution.Attempt
	if execution.Error != "" {
		details["error"] = execution.Error
	}
	return details
}

func (am *AlertManager) generateSystemAlertDetails(rule *AlertRule) map[string]interface{} {
	details := make(map[string]interface{})
	stats := am.manager.GetStats()
	details["total_jobs"] = stats.TotalJobs
	details["active_jobs"] = stats.ActiveJobs
	details["failed_jobs"] = stats.FailedJobs
	details["total_executions"] = stats.TotalExecutions
	details["failed_executions"] = stats.FailedExecutions
	details["cluster_healthy"] = stats.ClusterHealthy
	details["active_nodes"] = stats.ActiveNodes
	details["leader_node"] = stats.LeaderNode
	return details
}

func (am *AlertManager) matchesFilter(alert *Alert, filter *AlertFilter) bool {
	if filter == nil {
		return true
	}

	if len(filter.IDs) > 0 {
		found := false
		for _, id := range filter.IDs {
			if alert.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Severities) > 0 {
		found := false
		for _, severity := range filter.Severities {
			if alert.Severity == severity {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.Statuses) > 0 {
		found := false
		for _, status := range filter.Statuses {
			if alert.Status == status {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
