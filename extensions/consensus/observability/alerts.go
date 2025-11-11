package observability

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// AlertManager manages alerts and notifications for consensus issues.
type AlertManager struct {
	nodeID string
	logger forge.Logger

	// Alert rules
	rules   map[string]*AlertRule
	rulesMu sync.RWMutex

	// Active alerts
	alerts   map[string]*Alert
	alertsMu sync.RWMutex

	// Alert history
	history    []AlertEvent
	historyMu  sync.RWMutex
	maxHistory int

	// Configuration
	config AlertConfig

	// Statistics
	stats AlertStatistics

	// Handlers
	handlers []AlertHandler
}

// AlertRule defines conditions for triggering alerts.
type AlertRule struct {
	Name        string
	Condition   AlertCondition
	Severity    AlertSeverity
	Description string
	Enabled     bool
	Cooldown    time.Duration
	lastFired   time.Time
}

// Alert represents an active alert.
type Alert struct {
	ID           string
	Rule         string
	Severity     AlertSeverity
	Message      string
	Details      map[string]any
	StartTime    time.Time
	LastUpdate   time.Time
	FireCount    int
	Acknowledged bool
	Resolved     bool
	ResolvedAt   time.Time
}

// AlertEvent represents an alert event in history.
type AlertEvent struct {
	Timestamp time.Time
	Type      AlertEventType
	AlertID   string
	Rule      string
	Severity  AlertSeverity
	Message   string
}

// AlertCondition is a function that evaluates alert conditions.
type AlertCondition func(ctx context.Context, data map[string]any) bool

// AlertHandler handles alert notifications.
type AlertHandler func(alert *Alert)

// AlertSeverity represents alert severity.
type AlertSeverity string

const (
	// AlertSeverityCritical critical severity.
	AlertSeverityCritical AlertSeverity = "critical"
	// AlertSeverityHigh high severity.
	AlertSeverityHigh AlertSeverity = "high"
	// AlertSeverityMedium medium severity.
	AlertSeverityMedium AlertSeverity = "medium"
	// AlertSeverityLow low severity.
	AlertSeverityLow AlertSeverity = "low"
)

// AlertEventType represents alert event type.
type AlertEventType string

const (
	// AlertEventTypeFired alert fired.
	AlertEventTypeFired AlertEventType = "fired"
	// AlertEventTypeAcknowledged alert acknowledged.
	AlertEventTypeAcknowledged AlertEventType = "acknowledged"
	// AlertEventTypeResolved alert resolved.
	AlertEventTypeResolved AlertEventType = "resolved"
)

// AlertConfig contains alert configuration.
type AlertConfig struct {
	EnableAlerts    bool
	MaxActiveAlerts int
	MaxHistory      int
}

// AlertStatistics contains alert statistics.
type AlertStatistics struct {
	TotalAlerts        int64
	ActiveAlerts       int64
	CriticalAlerts     int64
	HighAlerts         int64
	MediumAlerts       int64
	LowAlerts          int64
	AcknowledgedAlerts int64
	ResolvedAlerts     int64
}

// NewAlertManager creates a new alert manager.
func NewAlertManager(nodeID string, config AlertConfig, logger forge.Logger) *AlertManager {
	// Set defaults
	if config.MaxActiveAlerts == 0 {
		config.MaxActiveAlerts = 100
	}

	if config.MaxHistory == 0 {
		config.MaxHistory = 1000
	}

	am := &AlertManager{
		nodeID:     nodeID,
		logger:     logger,
		rules:      make(map[string]*AlertRule),
		alerts:     make(map[string]*Alert),
		history:    make([]AlertEvent, 0, config.MaxHistory),
		maxHistory: config.MaxHistory,
		config:     config,
		handlers:   make([]AlertHandler, 0),
	}

	// Register default rules
	am.registerDefaultRules()

	return am
}

// RegisterRule registers a new alert rule.
func (am *AlertManager) RegisterRule(rule *AlertRule) {
	am.rulesMu.Lock()
	defer am.rulesMu.Unlock()

	am.rules[rule.Name] = rule

	am.logger.Info("alert rule registered",
		forge.F("rule", rule.Name),
		forge.F("severity", rule.Severity),
	)
}

// registerDefaultRules registers default consensus alert rules.
func (am *AlertManager) registerDefaultRules() {
	// Leader election timeout
	am.RegisterRule(&AlertRule{
		Name:        "LeaderElectionTimeout",
		Severity:    AlertSeverityHigh,
		Description: "Leader election is taking too long",
		Enabled:     true,
		Cooldown:    1 * time.Minute,
		Condition: func(ctx context.Context, data map[string]any) bool {
			duration, ok := data["election_duration"].(time.Duration)

			return ok && duration > 5*time.Second
		},
	})

	// High replication lag
	am.RegisterRule(&AlertRule{
		Name:        "HighReplicationLag",
		Severity:    AlertSeverityMedium,
		Description: "Replication lag is high",
		Enabled:     true,
		Cooldown:    5 * time.Minute,
		Condition: func(ctx context.Context, data map[string]any) bool {
			lag, ok := data["replication_lag"].(int64)

			return ok && lag > 1000
		},
	})

	// Quorum loss
	am.RegisterRule(&AlertRule{
		Name:        "QuorumLoss",
		Severity:    AlertSeverityCritical,
		Description: "Cluster has lost quorum",
		Enabled:     true,
		Cooldown:    30 * time.Second,
		Condition: func(ctx context.Context, data map[string]any) bool {
			hasQuorum, ok := data["has_quorum"].(bool)

			return ok && !hasQuorum
		},
	})

	// Snapshot failure
	am.RegisterRule(&AlertRule{
		Name:        "SnapshotFailure",
		Severity:    AlertSeverityHigh,
		Description: "Snapshot operation failed",
		Enabled:     true,
		Cooldown:    10 * time.Minute,
		Condition: func(ctx context.Context, data map[string]any) bool {
			failed, ok := data["snapshot_failed"].(bool)

			return ok && failed
		},
	})

	// High error rate
	am.RegisterRule(&AlertRule{
		Name:        "HighErrorRate",
		Severity:    AlertSeverityMedium,
		Description: "Error rate is above threshold",
		Enabled:     true,
		Cooldown:    5 * time.Minute,
		Condition: func(ctx context.Context, data map[string]any) bool {
			errorRate, ok := data["error_rate"].(float64)

			return ok && errorRate > 10.0 // 10% error rate
		},
	})

	// Disk space low
	am.RegisterRule(&AlertRule{
		Name:        "DiskSpaceLow",
		Severity:    AlertSeverityHigh,
		Description: "Disk space is running low",
		Enabled:     true,
		Cooldown:    15 * time.Minute,
		Condition: func(ctx context.Context, data map[string]any) bool {
			freePercent, ok := data["disk_free_percent"].(float64)

			return ok && freePercent < 10.0 // Less than 10% free
		},
	})
}

// EvaluateRule evaluates an alert rule.
func (am *AlertManager) EvaluateRule(ctx context.Context, ruleName string, data map[string]any) {
	if !am.config.EnableAlerts {
		return
	}

	am.rulesMu.RLock()
	rule, exists := am.rules[ruleName]
	am.rulesMu.RUnlock()

	if !exists || !rule.Enabled {
		return
	}

	// Check cooldown
	if time.Since(rule.lastFired) < rule.Cooldown {
		return
	}

	// Evaluate condition
	if rule.Condition(ctx, data) {
		am.fireAlert(rule, data)
		rule.lastFired = time.Now()
	}
}

// fireAlert fires a new alert.
func (am *AlertManager) fireAlert(rule *AlertRule, data map[string]any) {
	am.alertsMu.Lock()
	defer am.alertsMu.Unlock()

	// Check if alert already exists
	alertID := fmt.Sprintf("%s-%s", am.nodeID, rule.Name)
	if alert, exists := am.alerts[alertID]; exists {
		// Update existing alert
		alert.LastUpdate = time.Now()
		alert.FireCount++
		alert.Details = data
	} else {
		// Create new alert
		alert := &Alert{
			ID:         alertID,
			Rule:       rule.Name,
			Severity:   rule.Severity,
			Message:    rule.Description,
			Details:    data,
			StartTime:  time.Now(),
			LastUpdate: time.Now(),
			FireCount:  1,
		}

		am.alerts[alertID] = alert
		am.stats.TotalAlerts++
		am.stats.ActiveAlerts++

		// Update severity stats
		switch rule.Severity {
		case AlertSeverityCritical:
			am.stats.CriticalAlerts++
		case AlertSeverityHigh:
			am.stats.HighAlerts++
		case AlertSeverityMedium:
			am.stats.MediumAlerts++
		case AlertSeverityLow:
			am.stats.LowAlerts++
		}

		// Record event
		am.recordEvent(AlertEvent{
			Timestamp: time.Now(),
			Type:      AlertEventTypeFired,
			AlertID:   alertID,
			Rule:      rule.Name,
			Severity:  rule.Severity,
			Message:   rule.Description,
		})

		// Notify handlers
		for _, handler := range am.handlers {
			go handler(alert)
		}

		am.logger.Warn("alert fired",
			forge.F("alert_id", alertID),
			forge.F("rule", rule.Name),
			forge.F("severity", rule.Severity),
			forge.F("message", rule.Description),
		)
	}
}

// AcknowledgeAlert acknowledges an alert.
func (am *AlertManager) AcknowledgeAlert(alertID string) error {
	am.alertsMu.Lock()
	defer am.alertsMu.Unlock()

	alert, exists := am.alerts[alertID]
	if !exists {
		return fmt.Errorf("alert not found: %s", alertID)
	}

	if alert.Acknowledged {
		return errors.New("alert already acknowledged")
	}

	alert.Acknowledged = true
	am.stats.AcknowledgedAlerts++

	// Record event
	am.recordEvent(AlertEvent{
		Timestamp: time.Now(),
		Type:      AlertEventTypeAcknowledged,
		AlertID:   alertID,
		Rule:      alert.Rule,
		Severity:  alert.Severity,
	})

	am.logger.Info("alert acknowledged",
		forge.F("alert_id", alertID),
	)

	return nil
}

// ResolveAlert resolves an alert.
func (am *AlertManager) ResolveAlert(alertID string) error {
	am.alertsMu.Lock()
	defer am.alertsMu.Unlock()

	alert, exists := am.alerts[alertID]
	if !exists {
		return fmt.Errorf("alert not found: %s", alertID)
	}

	if alert.Resolved {
		return errors.New("alert already resolved")
	}

	alert.Resolved = true
	alert.ResolvedAt = time.Now()
	am.stats.ResolvedAlerts++
	am.stats.ActiveAlerts--

	// Record event
	am.recordEvent(AlertEvent{
		Timestamp: time.Now(),
		Type:      AlertEventTypeResolved,
		AlertID:   alertID,
		Rule:      alert.Rule,
		Severity:  alert.Severity,
	})

	// Remove from active alerts
	delete(am.alerts, alertID)

	am.logger.Info("alert resolved",
		forge.F("alert_id", alertID),
		forge.F("duration", time.Since(alert.StartTime)),
	)

	return nil
}

// GetActiveAlerts returns all active alerts.
func (am *AlertManager) GetActiveAlerts() []*Alert {
	am.alertsMu.RLock()
	defer am.alertsMu.RUnlock()

	alerts := make([]*Alert, 0, len(am.alerts))
	for _, alert := range am.alerts {
		if !alert.Resolved {
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

// GetAlertsBySeverity returns alerts by severity.
func (am *AlertManager) GetAlertsBySeverity(severity AlertSeverity) []*Alert {
	am.alertsMu.RLock()
	defer am.alertsMu.RUnlock()

	var alerts []*Alert
	for _, alert := range am.alerts {
		if alert.Severity == severity && !alert.Resolved {
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

// GetCriticalAlerts returns critical alerts.
func (am *AlertManager) GetCriticalAlerts() []*Alert {
	return am.GetAlertsBySeverity(AlertSeverityCritical)
}

// RegisterHandler registers an alert handler.
func (am *AlertManager) RegisterHandler(handler AlertHandler) {
	am.handlers = append(am.handlers, handler)
}

// recordEvent records an alert event.
func (am *AlertManager) recordEvent(event AlertEvent) {
	am.historyMu.Lock()
	defer am.historyMu.Unlock()

	am.history = append(am.history, event)

	// Trim history
	if len(am.history) > am.maxHistory {
		am.history = am.history[len(am.history)-am.maxHistory:]
	}
}

// GetHistory returns alert history.
func (am *AlertManager) GetHistory(limit int) []AlertEvent {
	am.historyMu.RLock()
	defer am.historyMu.RUnlock()

	if limit <= 0 || limit > len(am.history) {
		limit = len(am.history)
	}

	start := len(am.history) - limit
	result := make([]AlertEvent, limit)
	copy(result, am.history[start:])

	return result
}

// GetStatistics returns alert statistics.
func (am *AlertManager) GetStatistics() AlertStatistics {
	am.alertsMu.RLock()
	defer am.alertsMu.RUnlock()

	return am.stats
}

// ClearResolvedAlerts clears resolved alerts from memory.
func (am *AlertManager) ClearResolvedAlerts() int {
	am.alertsMu.Lock()
	defer am.alertsMu.Unlock()

	cleared := 0

	for alertID, alert := range am.alerts {
		if alert.Resolved {
			delete(am.alerts, alertID)

			cleared++
		}
	}

	am.logger.Debug("resolved alerts cleared",
		forge.F("cleared", cleared),
	)

	return cleared
}

// MonitorAlerts monitors and evaluates alert rules.
func (am *AlertManager) MonitorAlerts(ctx context.Context, interval time.Duration, dataProvider func() map[string]any) {
	if !am.config.EnableAlerts {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data := dataProvider()

			am.rulesMu.RLock()

			rules := make([]*AlertRule, 0, len(am.rules))
			for _, rule := range am.rules {
				if rule.Enabled {
					rules = append(rules, rule)
				}
			}

			am.rulesMu.RUnlock()

			// Evaluate all rules
			for _, rule := range rules {
				am.EvaluateRule(ctx, rule.Name, data)
			}
		}
	}
}

// EnableRule enables an alert rule.
func (am *AlertManager) EnableRule(ruleName string) error {
	am.rulesMu.Lock()
	defer am.rulesMu.Unlock()

	rule, exists := am.rules[ruleName]
	if !exists {
		return fmt.Errorf("rule not found: %s", ruleName)
	}

	rule.Enabled = true

	am.logger.Info("alert rule enabled",
		forge.F("rule", ruleName),
	)

	return nil
}

// DisableRule disables an alert rule.
func (am *AlertManager) DisableRule(ruleName string) error {
	am.rulesMu.Lock()
	defer am.rulesMu.Unlock()

	rule, exists := am.rules[ruleName]
	if !exists {
		return fmt.Errorf("rule not found: %s", ruleName)
	}

	rule.Enabled = false

	am.logger.Info("alert rule disabled",
		forge.F("rule", ruleName),
	)

	return nil
}
