package alerting

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	health "github.com/xraph/forge/v0/pkg/health/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// AlertSeverity represents the severity of an alert
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityError    AlertSeverity = "error"
	AlertSeverityCritical AlertSeverity = "critical"
)

// String returns the string representation of alert severity
func (as AlertSeverity) String() string {
	return string(as)
}

// AlertType represents the type of alert
type AlertType string

const (
	AlertTypeServiceDown     AlertType = "service_down"
	AlertTypeServiceUp       AlertType = "service_up"
	AlertTypeServiceDegraded AlertType = "service_degraded"
	AlertTypeHealthCheck     AlertType = "health_check"
	AlertTypeSystemError     AlertType = "system_error"
	AlertTypeThreshold       AlertType = "threshold"
)

// String returns the string representation of alert type
func (at AlertType) String() string {
	return string(at)
}

// Alert represents an alert notification
type Alert struct {
	ID          string                 `json:"id"`
	Type        AlertType              `json:"type"`
	Severity    AlertSeverity          `json:"severity"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details"`
	Source      string                 `json:"source"`
	Service     string                 `json:"service"`
	Timestamp   time.Time              `json:"timestamp"`
	Resolved    bool                   `json:"resolved"`
	ResolvedAt  *time.Time             `json:"resolved_at,omitempty"`
	Tags        map[string]string      `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
	Fingerprint string                 `json:"fingerprint"`
}

// NewAlert creates a new alert
func NewAlert(alertType AlertType, severity AlertSeverity, title, message string) *Alert {
	return &Alert{
		ID:          generateAlertID(),
		Type:        alertType,
		Severity:    severity,
		Title:       title,
		Message:     message,
		Details:     make(map[string]interface{}),
		Timestamp:   time.Now(),
		Resolved:    false,
		Tags:        make(map[string]string),
		Metadata:    make(map[string]interface{}),
		Fingerprint: "",
	}
}

// WithDetails adds details to the alert
func (a *Alert) WithDetails(details map[string]interface{}) *Alert {
	for k, v := range details {
		a.Details[k] = v
	}
	return a
}

// WithDetail adds a single detail to the alert
func (a *Alert) WithDetail(key string, value interface{}) *Alert {
	a.Details[key] = value
	return a
}

// WithSource sets the alert source
func (a *Alert) WithSource(source string) *Alert {
	a.Source = source
	return a
}

// WithService sets the alert service
func (a *Alert) WithService(service string) *Alert {
	a.Service = service
	return a
}

// WithTags adds tags to the alert
func (a *Alert) WithTags(tags map[string]string) *Alert {
	for k, v := range tags {
		a.Tags[k] = v
	}
	return a
}

// WithTag adds a single tag to the alert
func (a *Alert) WithTag(key, value string) *Alert {
	a.Tags[key] = value
	return a
}

// WithMetadata adds metadata to the alert
func (a *Alert) WithMetadata(metadata map[string]interface{}) *Alert {
	for k, v := range metadata {
		a.Metadata[k] = v
	}
	return a
}

// WithFingerprint sets the alert fingerprint
func (a *Alert) WithFingerprint(fingerprint string) *Alert {
	a.Fingerprint = fingerprint
	return a
}

// Resolve marks the alert as resolved
func (a *Alert) Resolve() *Alert {
	a.Resolved = true
	now := time.Now()
	a.ResolvedAt = &now
	return a
}

// IsResolved returns true if the alert is resolved
func (a *Alert) IsResolved() bool {
	return a.Resolved
}

// GetFingerprint returns the alert fingerprint for deduplication
func (a *Alert) GetFingerprint() string {
	if a.Fingerprint != "" {
		return a.Fingerprint
	}

	// Generate fingerprint from key fields
	parts := []string{
		string(a.Type),
		a.Service,
		a.Title,
	}

	a.Fingerprint = strings.Join(parts, ":")
	return a.Fingerprint
}

// AlertNotifier defines the interface for alert notification systems
type AlertNotifier interface {
	// Name returns the name of the notifier
	Name() string

	// Send sends an alert notification
	Send(ctx context.Context, alert *Alert) error

	// SendBatch sends multiple alerts in a batch
	SendBatch(ctx context.Context, alerts []*Alert) error

	// Test tests the notification system
	Test(ctx context.Context) error

	// Close closes the notifier
	Close() error
}

// AlertRule defines conditions for triggering alerts
type AlertRule struct {
	Name       string                 `json:"name"`
	Enabled    bool                   `json:"enabled"`
	Services   []string               `json:"services"`
	Statuses   []health.HealthStatus  `json:"statuses"`
	Severity   AlertSeverity          `json:"severity"`
	Threshold  int                    `json:"threshold"` // Number of failures
	Duration   time.Duration          `json:"duration"`  // Time window
	Cooldown   time.Duration          `json:"cooldown"`  // Cooldown period
	Template   string                 `json:"template"`  // Message template
	Tags       map[string]string      `json:"tags"`
	Metadata   map[string]interface{} `json:"metadata"`
	Conditions []AlertCondition       `json:"conditions"`
	Actions    []AlertAction          `json:"actions"`
	LastFired  time.Time              `json:"last_fired"`
	FireCount  int                    `json:"fire_count"`
	mu         sync.RWMutex
}

// AlertCondition defines a condition for alert rules
type AlertCondition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

// AlertAction defines an action to take when an alert is triggered
type AlertAction struct {
	Type       string                 `json:"type"`
	Notifier   string                 `json:"notifier"`
	Template   string                 `json:"template"`
	Parameters map[string]interface{} `json:"parameters"`
}

// ShouldFire determines if the rule should fire based on health results
func (ar *AlertRule) ShouldFire(results []*health.HealthResult) bool {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	if !ar.Enabled {
		return false
	}

	// Check cooldown
	if time.Since(ar.LastFired) < ar.Cooldown {
		return false
	}

	// Check threshold
	failureCount := 0
	for _, result := range results {
		if ar.matchesRule(result) {
			failureCount++
		}
	}

	return failureCount >= ar.Threshold
}

// matchesRule checks if a health result matches the rule conditions
func (ar *AlertRule) matchesRule(result *health.HealthResult) bool {
	// Check services filter
	if len(ar.Services) > 0 {
		found := false
		for _, service := range ar.Services {
			if service == result.Name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check status filter
	if len(ar.Statuses) > 0 {
		found := false
		for _, status := range ar.Statuses {
			if status == result.Status {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check custom conditions
	for _, condition := range ar.Conditions {
		if !ar.evaluateCondition(condition, result) {
			return false
		}
	}

	return true
}

// evaluateCondition evaluates a single condition
func (ar *AlertRule) evaluateCondition(condition AlertCondition, result *health.HealthResult) bool {
	// Get field value from result
	var fieldValue interface{}

	switch condition.Field {
	case "status":
		fieldValue = string(result.Status)
	case "duration":
		fieldValue = result.Duration.Seconds()
	case "critical":
		fieldValue = result.Critical
	case "error":
		fieldValue = result.Error != ""
	case "message":
		fieldValue = result.Message
	default:
		// Check in details
		if val, exists := result.Details[condition.Field]; exists {
			fieldValue = val
		} else {
			return false
		}
	}

	// Evaluate condition based on operator
	switch condition.Operator {
	case "eq", "==":
		return fieldValue == condition.Value
	case "ne", "!=":
		return fieldValue != condition.Value
	case "gt", ">":
		return compareValues(fieldValue, condition.Value) > 0
	case "gte", ">=":
		return compareValues(fieldValue, condition.Value) >= 0
	case "lt", "<":
		return compareValues(fieldValue, condition.Value) < 0
	case "lte", "<=":
		return compareValues(fieldValue, condition.Value) <= 0
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", fieldValue), fmt.Sprintf("%v", condition.Value))
	case "matches":
		// Regex matching would go here
		return false
	default:
		return false
	}
}

// Fire marks the rule as fired
func (ar *AlertRule) Fire() {
	ar.mu.Lock()
	defer ar.mu.Unlock()
	ar.LastFired = time.Now()
	ar.FireCount++
}

// AlertManager manages alert notifications and rules
type AlertManager struct {
	notifiers map[string]AlertNotifier
	rules     map[string]*AlertRule
	alerts    map[string]*Alert // Active alerts by fingerprint
	config    *AlertManagerConfig
	logger    common.Logger
	metrics   common.Metrics
	mu        sync.RWMutex
	stopCh    chan struct{}
	started   bool
}

// AlertManagerConfig contains configuration for the alert manager
type AlertManagerConfig struct {
	Enabled             bool              `yaml:"enabled" json:"enabled"`
	DefaultSeverity     AlertSeverity     `yaml:"default_severity" json:"default_severity"`
	BatchSize           int               `yaml:"batch_size" json:"batch_size"`
	BatchTimeout        time.Duration     `yaml:"batch_timeout" json:"batch_timeout"`
	RetryAttempts       int               `yaml:"retry_attempts" json:"retry_attempts"`
	RetryDelay          time.Duration     `yaml:"retry_delay" json:"retry_delay"`
	DeduplicationWindow time.Duration     `yaml:"deduplication_window" json:"deduplication_window"`
	GlobalTags          map[string]string `yaml:"global_tags" json:"global_tags"`
	Templates           map[string]string `yaml:"templates" json:"templates"`
}

// DefaultAlertManagerConfig returns default configuration
func DefaultAlertManagerConfig() *AlertManagerConfig {
	return &AlertManagerConfig{
		Enabled:             true,
		DefaultSeverity:     AlertSeverityWarning,
		BatchSize:           10,
		BatchTimeout:        5 * time.Second,
		RetryAttempts:       3,
		RetryDelay:          1 * time.Second,
		DeduplicationWindow: 5 * time.Minute,
		GlobalTags:          make(map[string]string),
		Templates:           make(map[string]string),
	}
}

// NewAlertManager creates a new alert manager
func NewAlertManager(config *AlertManagerConfig, logger common.Logger, metrics common.Metrics) *AlertManager {
	if config == nil {
		config = DefaultAlertManagerConfig()
	}

	return &AlertManager{
		notifiers: make(map[string]AlertNotifier),
		rules:     make(map[string]*AlertRule),
		alerts:    make(map[string]*Alert),
		config:    config,
		logger:    logger,
		metrics:   metrics,
		stopCh:    make(chan struct{}),
	}
}

// AddNotifier adds a notifier to the alert manager
func (am *AlertManager) AddNotifier(name string, notifier AlertNotifier) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.notifiers[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	am.notifiers[name] = notifier

	if am.logger != nil {
		am.logger.Info("alert notifier added",
			logger.String("name", name),
			logger.String("type", notifier.Name()),
		)
	}

	return nil
}

// RemoveNotifier removes a notifier from the alert manager
func (am *AlertManager) RemoveNotifier(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	notifier, exists := am.notifiers[name]
	if !exists {
		return common.ErrServiceNotFound(name)
	}

	if err := notifier.Close(); err != nil {
		if am.logger != nil {
			am.logger.Error("failed to close notifier",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	delete(am.notifiers, name)

	if am.logger != nil {
		am.logger.Info("alert notifier removed",
			logger.String("name", name),
		)
	}

	return nil
}

// AddRule adds an alert rule
func (am *AlertManager) AddRule(rule *AlertRule) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.rules[rule.Name]; exists {
		return common.ErrServiceAlreadyExists(rule.Name)
	}

	am.rules[rule.Name] = rule

	if am.logger != nil {
		am.logger.Info("alert rule added",
			logger.String("name", rule.Name),
			logger.String("severity", rule.Severity.String()),
			logger.Bool("enabled", rule.Enabled),
		)
	}

	return nil
}

// RemoveRule removes an alert rule
func (am *AlertManager) RemoveRule(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.rules[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(am.rules, name)

	if am.logger != nil {
		am.logger.Info("alert rule removed",
			logger.String("name", name),
		)
	}

	return nil
}

// ProcessHealthResults processes health results and triggers alerts
func (am *AlertManager) ProcessHealthResults(ctx context.Context, results []*health.HealthResult) error {
	if !am.config.Enabled {
		return nil
	}

	am.mu.RLock()
	rules := make([]*AlertRule, 0, len(am.rules))
	for _, rule := range am.rules {
		rules = append(rules, rule)
	}
	am.mu.RUnlock()

	// Process each rule
	for _, rule := range rules {
		if rule.ShouldFire(results) {
			alert := am.createAlertFromRule(rule, results)
			if err := am.SendAlert(ctx, alert); err != nil {
				if am.logger != nil {
					am.logger.Error("failed to send alert",
						logger.String("rule", rule.Name),
						logger.Error(err),
					)
				}
			}
			rule.Fire()
		}
	}

	return nil
}

// SendAlert sends an alert notification
func (am *AlertManager) SendAlert(ctx context.Context, alert *Alert) error {
	if !am.config.Enabled {
		return nil
	}

	// Add global tags
	for k, v := range am.config.GlobalTags {
		alert.WithTag(k, v)
	}

	// Check for deduplication
	fingerprint := alert.GetFingerprint()
	am.mu.Lock()
	if existing, exists := am.alerts[fingerprint]; exists {
		if time.Since(existing.Timestamp) < am.config.DeduplicationWindow {
			am.mu.Unlock()
			return nil // Skip duplicate alert
		}
	}
	am.alerts[fingerprint] = alert
	am.mu.Unlock()

	// Send to all notifiers
	am.mu.RLock()
	notifiers := make([]AlertNotifier, 0, len(am.notifiers))
	for _, notifier := range am.notifiers {
		notifiers = append(notifiers, notifier)
	}
	am.mu.RUnlock()

	for _, notifier := range notifiers {
		if err := am.sendWithRetry(ctx, notifier, alert); err != nil {
			if am.logger != nil {
				am.logger.Error("failed to send alert",
					logger.String("notifier", notifier.Name()),
					logger.String("alert", alert.ID),
					logger.Error(err),
				)
			}
		}
	}

	if am.metrics != nil {
		am.metrics.Counter("forge.health.alerts_sent").Inc()
	}

	return nil
}

// sendWithRetry sends an alert with retry logic
func (am *AlertManager) sendWithRetry(ctx context.Context, notifier AlertNotifier, alert *Alert) error {
	var lastErr error

	for attempt := 0; attempt < am.config.RetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(am.config.RetryDelay):
			}
		}

		if err := notifier.Send(ctx, alert); err != nil {
			lastErr = err
			continue
		}

		return nil
	}

	return lastErr
}

// createAlertFromRule creates an alert from a rule and health results
func (am *AlertManager) createAlertFromRule(rule *AlertRule, results []*health.HealthResult) *Alert {
	// Find the first matching result for context
	var matchingResult *health.HealthResult
	for _, result := range results {
		if rule.matchesRule(result) {
			matchingResult = result
			break
		}
	}

	if matchingResult == nil {
		// This shouldn't happen if ShouldFire returned true
		matchingResult = results[0]
	}

	alert := NewAlert(AlertTypeHealthCheck, rule.Severity, rule.Name, "Health check alert")
	alert.WithService(matchingResult.Name)
	alert.WithSource("health-checker")
	alert.WithTags(rule.Tags)
	alert.WithMetadata(rule.Metadata)

	// Use template if provided
	if rule.Template != "" {
		alert.Message = am.renderTemplate(rule.Template, matchingResult)
	} else {
		alert.Message = fmt.Sprintf("Health check failed for service %s: %s", matchingResult.Name, matchingResult.Message)
	}

	// Add result details
	alert.WithDetail("status", matchingResult.Status)
	alert.WithDetail("duration", matchingResult.Duration)
	alert.WithDetail("critical", matchingResult.Critical)
	alert.WithDetail("error", matchingResult.Error)

	return alert
}

// renderTemplate renders an alert message template
func (am *AlertManager) renderTemplate(template string, result *health.HealthResult) string {
	// Simple template rendering - in production, use a proper template engine
	message := template
	message = strings.ReplaceAll(message, "{{.Service}}", result.Name)
	message = strings.ReplaceAll(message, "{{.Status}}", string(result.Status))
	message = strings.ReplaceAll(message, "{{.Message}}", result.Message)
	message = strings.ReplaceAll(message, "{{.Duration}}", result.Duration.String())
	return message
}

// GetActiveAlerts returns all active alerts
func (am *AlertManager) GetActiveAlerts() []*Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	alerts := make([]*Alert, 0, len(am.alerts))
	for _, alert := range am.alerts {
		if !alert.IsResolved() {
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

// GetNotifiers returns all registered notifiers
func (am *AlertManager) GetNotifiers() map[string]AlertNotifier {
	am.mu.RLock()
	defer am.mu.RUnlock()

	notifiers := make(map[string]AlertNotifier)
	for name, notifier := range am.notifiers {
		notifiers[name] = notifier
	}

	return notifiers
}

// GetRules returns all alert rules
func (am *AlertManager) GetRules() []*AlertRule {
	am.mu.RLock()
	defer am.mu.RUnlock()

	rules := make([]*AlertRule, 0, len(am.rules))
	for _, rule := range am.rules {
		rules = append(rules, rule)
	}

	return rules
}

// Test tests all notifiers
func (am *AlertManager) Test(ctx context.Context) error {
	am.mu.RLock()
	notifiers := make([]AlertNotifier, 0, len(am.notifiers))
	for _, notifier := range am.notifiers {
		notifiers = append(notifiers, notifier)
	}
	am.mu.RUnlock()

	for _, notifier := range notifiers {
		if err := notifier.Test(ctx); err != nil {
			return fmt.Errorf("notifier %s test failed: %w", notifier.Name(), err)
		}
	}

	return nil
}

// Start starts the alert manager
func (am *AlertManager) Start(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.started {
		return common.ErrServiceAlreadyExists("alert-manager")
	}

	am.started = true

	if am.logger != nil {
		am.logger.Info("alert manager started",
			logger.Int("notifiers", len(am.notifiers)),
			logger.Int("rules", len(am.rules)),
		)
	}

	return nil
}

// Stop stops the alert manager
func (am *AlertManager) Stop(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if !am.started {
		return nil
	}

	am.started = false
	close(am.stopCh)

	// Close all notifiers
	for name, notifier := range am.notifiers {
		if err := notifier.Close(); err != nil {
			if am.logger != nil {
				am.logger.Error("failed to close notifier",
					logger.String("name", name),
					logger.Error(err),
				)
			}
		}
	}

	if am.logger != nil {
		am.logger.Info("alert manager stopped")
	}

	return nil
}

// Helper functions

func generateAlertID() string {
	return fmt.Sprintf("alert-%d", time.Now().UnixNano())
}

func compareValues(a, b interface{}) int {
	// Simple comparison - in production, use proper type comparison
	switch va := a.(type) {
	case int:
		if vb, ok := b.(int); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case float64:
		if vb, ok := b.(float64); ok {
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	case string:
		if vb, ok := b.(string); ok {
			return strings.Compare(va, vb)
		}
	}

	return 0
}
