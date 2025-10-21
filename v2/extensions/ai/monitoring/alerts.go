package monitoring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	ai "github.com/xraph/forge/v2/extensions/ai/internal"
	"github.com/xraph/forge/v2/internal/logger"
)

// AIAlertManager manages AI-powered alerting for the system
type AIAlertManager struct {
	aiManager        ai.AI
	healthMonitor    *AIHealthMonitor
	metricsCollector *AIMetricsCollector
	logger           logger.Logger
	metrics          forge.Metrics

	// Alert state
	activeAlerts  map[string]*AIAlert
	alertHistory  []*AIAlert
	alertRules    map[string]*AlertRule
	alertChannels map[string]AlertChannel
	alertPolicy   *AlertPolicy

	// AI-powered components
	anomalyDetector *AnomalyDetector
	predictor       *AlertPredictor
	classifier      *AlertClassifier

	// Configuration
	config AIAlertConfig

	// Synchronization
	mu          sync.RWMutex
	started     bool
	alertTicker *time.Ticker
}

// AIAlertConfig contains configuration for AI alerting
type AIAlertConfig struct {
	CheckInterval          time.Duration `yaml:"check_interval" default:"60s"`
	AlertRetention         time.Duration `yaml:"alert_retention" default:"7d"`
	MaxActiveAlerts        int           `yaml:"max_active_alerts" default:"1000"`
	EnablePredictiveAlerts bool          `yaml:"enable_predictive_alerts" default:"true"`
	EnableAnomalyAlerts    bool          `yaml:"enable_anomaly_alerts" default:"true"`
	EnableCorrelation      bool          `yaml:"enable_correlation" default:"true"`
	AlertGroupingWindow    time.Duration `yaml:"alert_grouping_window" default:"5m"`
	AlertSuppressionWindow time.Duration `yaml:"alert_suppression_window" default:"15m"`
	MinConfidenceThreshold float64       `yaml:"min_confidence_threshold" default:"0.7"`
}

// AIAlert represents an intelligent alert with AI-powered analysis
type AIAlert struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Message   string        `json:"message"`
	Severity  AlertSeverity `json:"severity"`
	Priority  AlertPriority `json:"priority"`
	Category  AlertCategory `json:"category"`
	Source    string        `json:"source"`
	Component string        `json:"component"`

	// AI-powered fields
	AIPredicted        bool              `json:"ai_predicted"`
	Confidence         float64           `json:"confidence"`
	RootCause          string            `json:"root_cause"`
	Impact             AlertImpact       `json:"impact"`
	RecommendedActions []AlertAction     `json:"recommended_actions"`
	RelatedAlerts      []string          `json:"related_alerts"`
	Correlation        *AlertCorrelation `json:"correlation"`

	// Timing
	CreatedAt       time.Time  `json:"created_at"`
	FirstOccurrence time.Time  `json:"first_occurrence"`
	LastOccurrence  time.Time  `json:"last_occurrence"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`

	// State
	Status          AlertStatus `json:"status"`
	AcknowledgedBy  string      `json:"acknowledged_by,omitempty"`
	ResolvedBy      string      `json:"resolved_by,omitempty"`
	Suppressed      bool        `json:"suppressed"`
	OccurrenceCount int         `json:"occurrence_count"`

	// Metadata
	Tags     map[string]string      `json:"tags"`
	Metadata map[string]interface{} `json:"metadata"`
	RawData  interface{}            `json:"raw_data"`
}

// AlertSeverity defines alert severity levels
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityError    AlertSeverity = "error"
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityFatal    AlertSeverity = "fatal"
)

// AlertPriority defines alert priority levels
type AlertPriority string

const (
	AlertPriorityLow       AlertPriority = "low"
	AlertPriorityMedium    AlertPriority = "medium"
	AlertPriorityHigh      AlertPriority = "high"
	AlertPriorityCritical  AlertPriority = "critical"
	AlertPriorityEmergency AlertPriority = "emergency"
)

// AlertCategory defines categories of alerts
type AlertCategory string

const (
	AlertCategoryPerformance  AlertCategory = "performance"
	AlertCategoryAvailability AlertCategory = "availability"
	AlertCategorySecurity     AlertCategory = "security"
	AlertCategoryCapacity     AlertCategory = "capacity"
	AlertCategoryQuality      AlertCategory = "quality"
	AlertCategoryAnomaly      AlertCategory = "anomaly"
	AlertCategoryPredictive   AlertCategory = "predictive"
	AlertCategorySystem       AlertCategory = "system"
)

// AlertStatus defines alert states
type AlertStatus string

const (
	AlertStatusNew           AlertStatus = "new"
	AlertStatusAcknowledged  AlertStatus = "acknowledged"
	AlertStatusInvestigating AlertStatus = "investigating"
	AlertStatusResolving     AlertStatus = "resolving"
	AlertStatusResolved      AlertStatus = "resolved"
	AlertStatusSuppressed    AlertStatus = "suppressed"
	AlertStatusEscalated     AlertStatus = "escalated"
)

// AlertImpact describes the impact of an alert
type AlertImpact struct {
	Scope            string                 `json:"scope"` // "agent", "model", "system", "global"
	AffectedUsers    int                    `json:"affected_users"`
	AffectedServices []string               `json:"affected_services"`
	BusinessImpact   string                 `json:"business_impact"`
	TechnicalImpact  string                 `json:"technical_impact"`
	EstimatedCost    float64                `json:"estimated_cost"`
	SLA              map[string]interface{} `json:"sla"`
}

// AlertAction represents a recommended action
type AlertAction struct {
	ID            string                 `json:"id"`
	Type          ActionType             `json:"type"`
	Description   string                 `json:"description"`
	Command       string                 `json:"command,omitempty"`
	Parameters    map[string]interface{} `json:"parameters,omitempty"`
	Automated     bool                   `json:"automated"`
	Priority      int                    `json:"priority"`
	EstimatedTime time.Duration          `json:"estimated_time"`
	RequiredRole  string                 `json:"required_role,omitempty"`
	Confirmation  bool                   `json:"confirmation"`
	Rollback      bool                   `json:"rollback"`
}

// ActionType defines types of alert actions
type ActionType string

const (
	ActionTypeRestart     ActionType = "restart"
	ActionTypeScale       ActionType = "scale"
	ActionTypeReload      ActionType = "reload"
	ActionTypeInvestigate ActionType = "investigate"
	ActionTypeNotify      ActionType = "notify"
	ActionTypeRunbook     ActionType = "runbook"
	ActionTypeEscalate    ActionType = "escalate"
	ActionTypeSuppress    ActionType = "suppress"
	ActionTypeCustom      ActionType = "custom"
)

// AlertCorrelation contains correlation information
type AlertCorrelation struct {
	CorrelationID   string                 `json:"correlation_id"`
	RelatedAlerts   []string               `json:"related_alerts"`
	RootCauseAlert  string                 `json:"root_cause_alert,omitempty"`
	CorrelationType string                 `json:"correlation_type"`
	Confidence      float64                `json:"confidence"`
	Pattern         string                 `json:"pattern"`
	Metadata        map[string]interface{} `json:"metadata"`
}

// AlertRule defines conditions for triggering alerts
type AlertRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`

	// Conditions
	MetricName         string             `json:"metric_name"`
	Operator           ComparisonOperator `json:"operator"`
	Threshold          float64            `json:"threshold"`
	Duration           time.Duration      `json:"duration"`
	EvaluationInterval time.Duration      `json:"evaluation_interval"`

	// AI-powered conditions
	AnomalyDetection    bool                 `json:"anomaly_detection"`
	TrendAnalysis       bool                 `json:"trend_analysis"`
	PredictiveThreshold *PredictiveThreshold `json:"predictive_threshold,omitempty"`

	// Alert properties
	Severity AlertSeverity `json:"severity"`
	Priority AlertPriority `json:"priority"`
	Category AlertCategory `json:"category"`

	// Actions
	Actions  []AlertAction `json:"actions"`
	Channels []string      `json:"channels"`

	// Suppression
	SuppressionRules []SuppressionRule `json:"suppression_rules"`

	// Metadata
	Tags      map[string]string `json:"tags"`
	CreatedBy string            `json:"created_by"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// ComparisonOperator defines comparison operators for alert rules
type ComparisonOperator string

const (
	ComparisonOperatorGT  ComparisonOperator = "gt"  // greater than
	ComparisonOperatorGTE ComparisonOperator = "gte" // greater than or equal
	ComparisonOperatorLT  ComparisonOperator = "lt"  // less than
	ComparisonOperatorLTE ComparisonOperator = "lte" // less than or equal
	ComparisonOperatorEQ  ComparisonOperator = "eq"  // equal
	ComparisonOperatorNE  ComparisonOperator = "ne"  // not equal
)

// PredictiveThreshold defines AI-based predictive thresholds
type PredictiveThreshold struct {
	MetricName          string        `json:"metric_name"`
	PredictionHorizon   time.Duration `json:"prediction_horizon"`
	ConfidenceThreshold float64       `json:"confidence_threshold"`
	ThresholdValue      float64       `json:"threshold_value"`
	Model               string        `json:"model"`
}

// SuppressionRule defines conditions for suppressing alerts
type SuppressionRule struct {
	Condition  string                 `json:"condition"`
	Duration   time.Duration          `json:"duration"`
	Reason     string                 `json:"reason"`
	Parameters map[string]interface{} `json:"parameters"`
}

// AlertChannel defines how alerts are delivered
type AlertChannel interface {
	Name() string
	Type() ChannelType
	Send(ctx context.Context, alert *AIAlert) error
	IsEnabled() bool
	GetConfig() map[string]interface{}
}

// ChannelType defines types of alert channels
type ChannelType string

const (
	ChannelTypeEmail     ChannelType = "email"
	ChannelTypeSlack     ChannelType = "slack"
	ChannelTypeWebhook   ChannelType = "webhook"
	ChannelTypeSMS       ChannelType = "sms"
	ChannelTypePagerDuty ChannelType = "pagerduty"
	ChannelTypeCustom    ChannelType = "custom"
)

// AlertPolicy defines policies for alert management
type AlertPolicy struct {
	AutoAcknowledge    bool               `json:"auto_acknowledge"`
	AutoResolve        bool               `json:"auto_resolve"`
	AutoResolveTimeout time.Duration      `json:"auto_resolve_timeout"`
	EscalationRules    []EscalationRule   `json:"escalation_rules"`
	GroupingRules      []GroupingRule     `json:"grouping_rules"`
	NotificationRules  []NotificationRule `json:"notification_rules"`
}

// EscalationRule defines escalation conditions
type EscalationRule struct {
	Condition string        `json:"condition"`
	Delay     time.Duration `json:"delay"`
	Target    string        `json:"target"`
	Action    string        `json:"action"`
}

// GroupingRule defines how alerts are grouped
type GroupingRule struct {
	Name          string        `json:"name"`
	GroupBy       []string      `json:"group_by"`
	GroupInterval time.Duration `json:"group_interval"`
	GroupWaitTime time.Duration `json:"group_wait_time"`
}

// NotificationRule defines notification conditions
type NotificationRule struct {
	Condition  string                 `json:"condition"`
	Channels   []string               `json:"channels"`
	Template   string                 `json:"template"`
	Throttle   time.Duration          `json:"throttle"`
	Parameters map[string]interface{} `json:"parameters"`
}

// AI-powered components

// AnomalyDetector detects anomalies in metrics for alerting
type AnomalyDetector struct {
	models          map[string]AnomalyModel
	threshold       float64
	windowSize      time.Duration
	learningEnabled bool
}

// AnomalyModel represents a model for detecting anomalies
type AnomalyModel interface {
	Train(data []float64) error
	Detect(value float64) (isAnomaly bool, score float64, err error)
	Update(value float64, isAnomaly bool) error
}

// AlertPredictor predicts future alerts using ML
type AlertPredictor struct {
	models         map[string]PredictionModel
	enabled        bool
	horizonDefault time.Duration
}

// PredictionModel represents a model for predicting alerts
type PredictionModel interface {
	Predict(ctx context.Context, data []float64, horizon time.Duration) (prediction float64, confidence float64, err error)
	Train(ctx context.Context, data []TrainingDataPoint) error
}

// TrainingDataPoint represents training data for prediction models
type TrainingDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
	Label     bool      `json:"label"` // true if an alert occurred
}

// AlertClassifier classifies alerts into categories using ML
type AlertClassifier struct {
	model         ClassificationModel
	enabled       bool
	categories    []AlertCategory
	confidenceMin float64
}

// ClassificationModel represents a model for classifying alerts
type ClassificationModel interface {
	Classify(ctx context.Context, alert *AIAlert) (category AlertCategory, confidence float64, err error)
	Train(ctx context.Context, trainingData []AlertTrainingData) error
}

// AlertTrainingData represents training data for alert classification
type AlertTrainingData struct {
	Alert    *AIAlert      `json:"alert"`
	Category AlertCategory `json:"category"`
	Feedback bool          `json:"feedback"` // true if classification was correct
}

// NewAIAlertManager creates a new AI alert manager
func NewAIAlertManager(aiManager ai.AI, healthMonitor *AIHealthMonitor, metricsCollector *AIMetricsCollector, logger logger.Logger, metrics forge.Metrics, config AIAlertConfig) *AIAlertManager {
	return &AIAlertManager{
		aiManager:        aiManager,
		healthMonitor:    healthMonitor,
		metricsCollector: metricsCollector,
		logger:           logger,
		metrics:          metrics,
		config:           config,
		activeAlerts:     make(map[string]*AIAlert),
		alertHistory:     make([]*AIAlert, 0),
		alertRules:       make(map[string]*AlertRule),
		alertChannels:    make(map[string]AlertChannel),
		anomalyDetector:  NewAnomalyDetector(0.95, 5*time.Minute, true),
		predictor:        NewAlertPredictor(true, 1*time.Hour),
		classifier:       NewAlertClassifier(0.8),
		alertPolicy: &AlertPolicy{
			AutoResolve:        true,
			AutoResolveTimeout: 24 * time.Hour,
		},
	}
}

// Start starts the AI alert manager
func (m *AIAlertManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("AI alert manager already started")
	}

	// Initialize default alert rules
	if err := m.initializeDefaultRules(); err != nil {
		return fmt.Errorf("failed to initialize default alert rules: %w", err)
	}

	// Start alert checking ticker
	m.alertTicker = time.NewTicker(m.config.CheckInterval)
	m.started = true

	// Start alert processing goroutine
	go m.processAlerts(ctx)

	if m.logger != nil {
		m.logger.Info("AI alert manager started",
			logger.Duration("check_interval", m.config.CheckInterval),
			logger.Bool("predictive_enabled", m.config.EnablePredictiveAlerts),
			logger.Bool("anomaly_detection_enabled", m.config.EnableAnomalyAlerts),
			logger.Bool("correlation_enabled", m.config.EnableCorrelation),
		)
	}

	return nil
}

// Stop stops the AI alert manager
func (m *AIAlertManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("AI alert manager not started")
	}

	if m.alertTicker != nil {
		m.alertTicker.Stop()
	}
	m.started = false

	if m.logger != nil {
		m.logger.Info("AI alert manager stopped")
	}

	return nil
}

// CreateAlert creates a new alert with AI-powered analysis
func (m *AIAlertManager) CreateAlert(ctx context.Context, title, message string, severity AlertSeverity, source, component string, metadata map[string]interface{}) (*AIAlert, error) {
	alertID, err := generateAlertID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate alert ID: %w", err)
	}

	alert := &AIAlert{
		ID:              alertID,
		Title:           title,
		Message:         message,
		Severity:        severity,
		Source:          source,
		Component:       component,
		CreatedAt:       time.Now(),
		FirstOccurrence: time.Now(),
		LastOccurrence:  time.Now(),
		Status:          AlertStatusNew,
		OccurrenceCount: 1,
		Tags:            make(map[string]string),
		Metadata:        metadata,
	}

	// Apply AI-powered analysis
	if err := m.analyzeAlert(ctx, alert); err != nil {
		if m.logger != nil {
			m.logger.Warn("failed to perform AI analysis on alert",
				logger.String("alert_id", alertID),
				logger.Error(err),
			)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for existing similar alerts
	if existingAlert := m.findSimilarAlert(alert); existingAlert != nil {
		// Update existing alert
		existingAlert.LastOccurrence = time.Now()
		existingAlert.OccurrenceCount++
		return existingAlert, nil
	}

	// Add to active alerts
	m.activeAlerts[alertID] = alert

	// Send notifications
	go m.sendAlertNotifications(ctx, alert)

	if m.logger != nil {
		m.logger.Info("alert created",
			logger.String("alert_id", alertID),
			logger.String("title", title),
			logger.String("severity", string(severity)),
			logger.String("component", component),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.ai.alerts.created_total", "severity", string(severity), "component", component).Inc()
	}

	return alert, nil
}

// analyzeAlert performs AI-powered analysis on an alert
func (m *AIAlertManager) analyzeAlert(ctx context.Context, alert *AIAlert) error {
	// Classify the alert
	if m.classifier.enabled {
		category, confidence, err := m.classifier.model.Classify(ctx, alert)
		if err == nil && confidence >= m.classifier.confidenceMin {
			alert.Category = category
			alert.Confidence = confidence
		}
	}

	// Determine priority based on severity and impact analysis
	alert.Priority = m.calculatePriority(alert)

	// Analyze impact
	alert.Impact = m.analyzeImpact(alert)

	// Generate root cause analysis
	alert.RootCause = m.analyzeRootCause(alert)

	// Generate recommended actions
	alert.RecommendedActions = m.generateRecommendedActions(alert)

	// Find correlations with other alerts
	if m.config.EnableCorrelation {
		alert.Correlation = m.findAlertCorrelations(alert)
	}

	return nil
}

// processAlerts runs the main alert processing loop
func (m *AIAlertManager) processAlerts(ctx context.Context) {
	defer m.alertTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.alertTicker.C:
			m.evaluateAlertRules(ctx)
			m.processAnomalyAlerts(ctx)
			m.processPredictiveAlerts(ctx)
			m.correlateAlerts(ctx)
			m.cleanupResolvedAlerts()
		}
	}
}

// evaluateAlertRules evaluates all alert rules against current metrics
func (m *AIAlertManager) evaluateAlertRules(ctx context.Context) {
	m.mu.RLock()
	rules := make([]*AlertRule, 0, len(m.alertRules))
	for _, rule := range m.alertRules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	m.mu.RUnlock()

	metricsReport, err := m.metricsCollector.GetMetricsReport(ctx)
	if err != nil {
		if m.logger != nil {
			m.logger.Error("failed to get metrics report for alert evaluation", logger.Error(err))
		}
		return
	}

	for _, rule := range rules {
		if m.shouldEvaluateRule(rule) {
			m.evaluateRule(ctx, rule, metricsReport)
		}
	}
}

// Additional methods would be implemented here for:
// - processAnomalyAlerts
// - processPredictiveAlerts
// - correlateAlerts
// - sendAlertNotifications
// - findSimilarAlert
// - calculatePriority
// - analyzeImpact
// - analyzeRootCause
// - generateRecommendedActions
// - findAlertCorrelations
// - etc.

// Utility functions

// generateAlertID generates a unique alert ID
func generateAlertID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("alert_%s", hex.EncodeToString(bytes)[:12]), nil
}

// initializeDefaultRules initializes default alert rules
func (m *AIAlertManager) initializeDefaultRules() error {
	defaultRules := []*AlertRule{
		{
			ID:                 "high_error_rate",
			Name:               "High Error Rate",
			Description:        "Triggers when system error rate exceeds threshold",
			Enabled:            true,
			MetricName:         "error_rate",
			Operator:           ComparisonOperatorGT,
			Threshold:          0.05, // 5%
			Duration:           5 * time.Minute,
			EvaluationInterval: 1 * time.Minute,
			Severity:           AlertSeverityError,
			Priority:           AlertPriorityHigh,
			Category:           AlertCategoryPerformance,
			Actions: []AlertAction{
				{
					Type:        ActionTypeInvestigate,
					Description: "Investigate high error rate in logs",
					Priority:    1,
				},
			},
		},
		{
			ID:                 "critical_latency",
			Name:               "Critical Latency",
			Description:        "Triggers when average latency exceeds critical threshold",
			Enabled:            true,
			MetricName:         "average_latency",
			Operator:           ComparisonOperatorGT,
			Threshold:          5.0, // 5 seconds
			Duration:           3 * time.Minute,
			EvaluationInterval: 30 * time.Second,
			Severity:           AlertSeverityCritical,
			Priority:           AlertPriorityCritical,
			Category:           AlertCategoryPerformance,
			Actions: []AlertAction{
				{
					Type:        ActionTypeScale,
					Description: "Scale inference workers",
					Priority:    1,
					Automated:   true,
				},
			},
		},
		{
			ID:                 "agent_health_degraded",
			Name:               "Agent Health Degraded",
			Description:        "Triggers when too many agents are unhealthy",
			Enabled:            true,
			MetricName:         "unhealthy_agents_ratio",
			Operator:           ComparisonOperatorGT,
			Threshold:          0.3, // 30%
			Duration:           2 * time.Minute,
			EvaluationInterval: 1 * time.Minute,
			Severity:           AlertSeverityWarning,
			Priority:           AlertPriorityMedium,
			Category:           AlertCategoryAvailability,
			Actions: []AlertAction{
				{
					Type:        ActionTypeRestart,
					Description: "Restart unhealthy agents",
					Priority:    1,
				},
			},
		},
	}

	for _, rule := range defaultRules {
		rule.CreatedAt = time.Now()
		rule.UpdatedAt = time.Now()
		rule.Tags = make(map[string]string)
		rule.Tags["default"] = "true"
		m.alertRules[rule.ID] = rule
	}

	return nil
}

// Placeholder implementations for AI components

// NewAnomalyDetector creates a new anomaly detector
func NewAnomalyDetector(threshold float64, windowSize time.Duration, learningEnabled bool) *AnomalyDetector {
	return &AnomalyDetector{
		models:          make(map[string]AnomalyModel),
		threshold:       threshold,
		windowSize:      windowSize,
		learningEnabled: learningEnabled,
	}
}

// NewAlertPredictor creates a new alert predictor
func NewAlertPredictor(enabled bool, defaultHorizon time.Duration) *AlertPredictor {
	return &AlertPredictor{
		models:         make(map[string]PredictionModel),
		enabled:        enabled,
		horizonDefault: defaultHorizon,
	}
}

// NewAlertClassifier creates a new alert classifier
func NewAlertClassifier(confidenceMin float64) *AlertClassifier {
	return &AlertClassifier{
		enabled:       true,
		confidenceMin: confidenceMin,
		categories: []AlertCategory{
			AlertCategoryPerformance,
			AlertCategoryAvailability,
			AlertCategorySecurity,
			AlertCategoryCapacity,
			AlertCategoryQuality,
			AlertCategoryAnomaly,
		},
	}
}

// Additional helper methods

func (m *AIAlertManager) shouldEvaluateRule(rule *AlertRule) bool {
	// Check if enough time has passed since last evaluation
	return time.Since(rule.UpdatedAt) >= rule.EvaluationInterval
}

func (m *AIAlertManager) evaluateRule(ctx context.Context, rule *AlertRule, metricsReport *AIMetricsReport) {
	// Extract metric value based on rule
	var metricValue float64
	var found bool

	switch rule.MetricName {
	case "error_rate":
		metricValue = metricsReport.SystemMetrics.OverallErrorRate
		found = true
	case "average_latency":
		metricValue = metricsReport.SystemMetrics.OverallLatency.Seconds()
		found = true
	case "unhealthy_agents_ratio":
		if metricsReport.SystemMetrics.TotalAgents > 0 {
			unhealthyAgents := metricsReport.SystemMetrics.TotalAgents - metricsReport.SystemMetrics.HealthyAgents
			metricValue = float64(unhealthyAgents) / float64(metricsReport.SystemMetrics.TotalAgents)
			found = true
		}
	}

	if !found {
		return
	}

	// Evaluate condition
	triggered := false
	switch rule.Operator {
	case ComparisonOperatorGT:
		triggered = metricValue > rule.Threshold
	case ComparisonOperatorGTE:
		triggered = metricValue >= rule.Threshold
	case ComparisonOperatorLT:
		triggered = metricValue < rule.Threshold
	case ComparisonOperatorLTE:
		triggered = metricValue <= rule.Threshold
	case ComparisonOperatorEQ:
		triggered = metricValue == rule.Threshold
	case ComparisonOperatorNE:
		triggered = metricValue != rule.Threshold
	}

	if triggered {
		// Create alert
		title := fmt.Sprintf("%s: %s", rule.Name, rule.MetricName)
		message := fmt.Sprintf("Metric %s is %v %s %v", rule.MetricName, metricValue, rule.Operator, rule.Threshold)

		metadata := map[string]interface{}{
			"rule_id":      rule.ID,
			"metric_name":  rule.MetricName,
			"metric_value": metricValue,
			"threshold":    rule.Threshold,
			"operator":     rule.Operator,
		}

		alert, err := m.CreateAlert(ctx, title, message, rule.Severity, "alert_rule", rule.MetricName, metadata)
		if err != nil && m.logger != nil {
			m.logger.Error("failed to create rule-based alert",
				logger.String("rule_id", rule.ID),
				logger.Error(err),
			)
		} else if alert != nil {
			alert.Priority = rule.Priority
			alert.Category = rule.Category
			alert.RecommendedActions = rule.Actions
		}
	}
}

func (m *AIAlertManager) findSimilarAlert(alert *AIAlert) *AIAlert {
	// Simple similarity check - in production this would be more sophisticated
	for _, existingAlert := range m.activeAlerts {
		if existingAlert.Component == alert.Component &&
			existingAlert.Severity == alert.Severity &&
			existingAlert.Status != AlertStatusResolved &&
			strings.Contains(existingAlert.Title, alert.Title) {
			return existingAlert
		}
	}
	return nil
}

func (m *AIAlertManager) calculatePriority(alert *AIAlert) AlertPriority {
	switch alert.Severity {
	case AlertSeverityFatal:
		return AlertPriorityEmergency
	case AlertSeverityCritical:
		return AlertPriorityCritical
	case AlertSeverityError:
		return AlertPriorityHigh
	case AlertSeverityWarning:
		return AlertPriorityMedium
	case AlertSeverityInfo:
		return AlertPriorityLow
	default:
		return AlertPriorityMedium
	}
}

func (m *AIAlertManager) analyzeImpact(alert *AIAlert) AlertImpact {
	impact := AlertImpact{
		Scope:            "component",
		AffectedUsers:    0,
		AffectedServices: []string{alert.Component},
		BusinessImpact:   "Low",
		TechnicalImpact:  "Component degradation",
		EstimatedCost:    0.0,
		SLA:              make(map[string]interface{}),
	}

	// Enhance impact analysis based on severity and component
	switch alert.Severity {
	case AlertSeverityFatal, AlertSeverityCritical:
		impact.Scope = "system"
		impact.BusinessImpact = "High"
		impact.TechnicalImpact = "System-wide impact"
		impact.AffectedUsers = 1000 // Estimated
	case AlertSeverityError:
		impact.BusinessImpact = "Medium"
		impact.TechnicalImpact = "Service degradation"
		impact.AffectedUsers = 100
	}

	return impact
}

func (m *AIAlertManager) analyzeRootCause(alert *AIAlert) string {
	// Simple root cause analysis - would be enhanced with ML in production
	rootCause := "Unknown"

	if strings.Contains(strings.ToLower(alert.Message), "error rate") {
		rootCause = "High error rate indicating potential issues with request processing or downstream dependencies"
	} else if strings.Contains(strings.ToLower(alert.Message), "latency") {
		rootCause = "High latency suggesting resource contention or network issues"
	} else if strings.Contains(strings.ToLower(alert.Message), "agent") {
		rootCause = "Agent health issues potentially due to resource constraints or configuration problems"
	}

	return rootCause
}

func (m *AIAlertManager) generateRecommendedActions(alert *AIAlert) []AlertAction {
	var actions []AlertAction

	// Generate actions based on alert category and severity
	switch alert.Category {
	case AlertCategoryPerformance:
		if alert.Severity == AlertSeverityCritical {
			actions = append(actions, AlertAction{
				Type:        ActionTypeScale,
				Description: "Scale up resources to handle increased load",
				Priority:    1,
				Automated:   true,
			})
		}
		actions = append(actions, AlertAction{
			Type:        ActionTypeInvestigate,
			Description: "Check logs and metrics for performance bottlenecks",
			Priority:    2,
		})

	case AlertCategoryAvailability:
		actions = append(actions, AlertAction{
			Type:        ActionTypeRestart,
			Description: "Restart affected components",
			Priority:    1,
		})

	case AlertCategorySecurity:
		actions = append(actions, AlertAction{
			Type:         ActionTypeNotify,
			Description:  "Notify security team immediately",
			Priority:     1,
			RequiredRole: "security",
		})
	}

	// Add generic investigation action if no specific actions
	if len(actions) == 0 {
		actions = append(actions, AlertAction{
			Type:        ActionTypeInvestigate,
			Description: "Investigate the issue and determine appropriate action",
			Priority:    1,
		})
	}

	return actions
}

func (m *AIAlertManager) findAlertCorrelations(alert *AIAlert) *AlertCorrelation {
	// Simple correlation analysis - would use ML for pattern detection in production
	correlationID, _ := generateAlertID()

	correlation := &AlertCorrelation{
		CorrelationID:   correlationID,
		RelatedAlerts:   make([]string, 0),
		CorrelationType: "temporal",
		Confidence:      0.5,
		Pattern:         "time-based",
		Metadata:        make(map[string]interface{}),
	}

	// Find alerts in the same time window
	timeWindow := 5 * time.Minute
	for _, existingAlert := range m.activeAlerts {
		if existingAlert.ID != alert.ID &&
			existingAlert.CreatedAt.After(alert.CreatedAt.Add(-timeWindow)) &&
			existingAlert.CreatedAt.Before(alert.CreatedAt.Add(timeWindow)) {
			correlation.RelatedAlerts = append(correlation.RelatedAlerts, existingAlert.ID)
		}
	}

	if len(correlation.RelatedAlerts) == 0 {
		return nil
	}

	return correlation
}

func (m *AIAlertManager) sendAlertNotifications(ctx context.Context, alert *AIAlert) {
	// Send to all configured channels based on alert severity and policy
	for _, channel := range m.alertChannels {
		if channel.IsEnabled() {
			if err := channel.Send(ctx, alert); err != nil && m.logger != nil {
				m.logger.Error("failed to send alert notification",
					logger.String("alert_id", alert.ID),
					logger.String("channel", channel.Name()),
					logger.Error(err),
				)
			}
		}
	}
}

func (m *AIAlertManager) cleanupResolvedAlerts() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoffTime := time.Now().Add(-m.config.AlertRetention)

	// Move resolved alerts older than retention period to history
	for alertID, alert := range m.activeAlerts {
		if alert.Status == AlertStatusResolved &&
			alert.ResolvedAt != nil &&
			alert.ResolvedAt.Before(cutoffTime) {

			m.alertHistory = append(m.alertHistory, alert)
			delete(m.activeAlerts, alertID)
		}
	}

	// Limit history size
	maxHistorySize := 10000
	if len(m.alertHistory) > maxHistorySize {
		m.alertHistory = m.alertHistory[len(m.alertHistory)-maxHistorySize:]
	}
}

// processAnomalyAlerts processes anomaly-based alerts
func (m *AIAlertManager) processAnomalyAlerts(ctx context.Context) {
	// Implementation for processing anomaly alerts
	// This would analyze anomaly detection results and generate alerts
}

// processPredictiveAlerts processes predictive alerts
func (m *AIAlertManager) processPredictiveAlerts(ctx context.Context) {
	// Implementation for processing predictive alerts
	// This would analyze predictive analytics results and generate alerts
}

// correlateAlerts correlates related alerts
func (m *AIAlertManager) correlateAlerts(ctx context.Context) {
	// Implementation for correlating alerts
	// This would identify related alerts and group them together
}
