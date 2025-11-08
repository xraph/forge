package observability

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// Monitor provides comprehensive monitoring and alerting.
type Monitor struct {
	config    MonitoringConfig
	metrics   map[string]*Metric
	alerts    map[string]*Alert
	handlers  map[string]AlertHandler
	mu        sync.RWMutex
	logger    logger.Logger
	stopC     chan struct{}
	wg        sync.WaitGroup
	startTime time.Time
}

// MonitoringConfig contains monitoring configuration.
type MonitoringConfig struct {
	EnableMetrics     bool          `default:"true"  yaml:"enable_metrics"`
	EnableAlerts      bool          `default:"true"  yaml:"enable_alerts"`
	EnableHealth      bool          `default:"true"  yaml:"enable_health"`
	EnableUptime      bool          `default:"true"  yaml:"enable_uptime"`
	EnablePerformance bool          `default:"true"  yaml:"enable_performance"`
	CheckInterval     time.Duration `default:"30s"   yaml:"check_interval"`
	AlertTimeout      time.Duration `default:"5m"    yaml:"alert_timeout"`
	RetentionPeriod   time.Duration `default:"7d"    yaml:"retention_period"`
	MaxMetrics        int           `default:"10000" yaml:"max_metrics"`
	MaxAlerts         int           `default:"1000"  yaml:"max_alerts"`
	Logger            logger.Logger `yaml:"-"`
}

// Metric represents a monitoring metric.
type Metric struct {
	Name        string            `json:"name"`
	Type        MetricType        `json:"type"`
	Value       float64           `json:"value"`
	Unit        string            `json:"unit"`
	Labels      map[string]string `json:"labels"`
	Timestamp   time.Time         `json:"timestamp"`
	Description string            `json:"description"`
}

// MetricType represents the type of metric.
type MetricType int

const (
	MetricTypeCounter MetricType = iota
	MetricTypeGauge
	MetricTypeHistogram
	MetricTypeSummary
)

// Alert represents an alert.
type Alert struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Severity    AlertSeverity     `json:"severity"`
	Status      AlertStatus       `json:"status"`
	Condition   string            `json:"condition"`
	Threshold   float64           `json:"threshold"`
	Duration    time.Duration     `json:"duration"`
	Labels      map[string]string `json:"labels"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	FiredAt     time.Time         `json:"fired_at,omitempty"`
	ResolvedAt  time.Time         `json:"resolved_at,omitempty"`
}

// AlertSeverity represents the severity of an alert.
type AlertSeverity int

const (
	AlertSeverityInfo AlertSeverity = iota
	AlertSeverityWarning
	AlertSeverityCritical
	AlertSeverityEmergency
)

// AlertStatus represents the status of an alert.
type AlertStatus int

const (
	AlertStatusInactive AlertStatus = iota
	AlertStatusPending
	AlertStatusFiring
	AlertStatusResolved
)

// AlertHandler handles alert notifications.
type AlertHandler interface {
	HandleAlert(ctx context.Context, alert *Alert) error
	GetName() string
}

// HealthCheck represents a health check.
type HealthCheck struct {
	Name       string            `json:"name"`
	Status     HealthStatus      `json:"status"`
	Message    string            `json:"message"`
	Duration   time.Duration     `json:"duration"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

// HealthStatus represents the status of a health check.
type HealthStatus int

const (
	HealthStatusUnknown HealthStatus = iota
	HealthStatusHealthy
	HealthStatusDegraded
	HealthStatusUnhealthy
)

// NewMonitor creates a new monitor.
func NewMonitor(config MonitoringConfig) *Monitor {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	monitor := &Monitor{
		config:    config,
		metrics:   make(map[string]*Metric),
		alerts:    make(map[string]*Alert),
		handlers:  make(map[string]AlertHandler),
		logger:    config.Logger,
		stopC:     make(chan struct{}),
		startTime: time.Now(),
	}

	// Start monitoring goroutines
	if config.EnableAlerts {
		monitor.wg.Add(1)

		go monitor.startAlerting()
	}

	if config.EnableHealth {
		monitor.wg.Add(1)

		go monitor.startHealthChecks()
	}

	return monitor
}

// RecordMetric records a metric.
func (m *Monitor) RecordMetric(metric *Metric) {
	if !m.config.EnableMetrics {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we've reached the maximum number of metrics
	if len(m.metrics) >= m.config.MaxMetrics {
		// Remove oldest metric
		var (
			oldestKey  string
			oldestTime time.Time
		)

		for key, metric := range m.metrics {
			if oldestTime.IsZero() || metric.Timestamp.Before(oldestTime) {
				oldestTime = metric.Timestamp
				oldestKey = key
			}
		}

		if oldestKey != "" {
			delete(m.metrics, oldestKey)
		}
	}

	metric.Timestamp = time.Now()
	m.metrics[metric.Name] = metric
}

// GetMetric gets a metric by name.
func (m *Monitor) GetMetric(name string) (*Metric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metric, exists := m.metrics[name]
	if !exists {
		return nil, fmt.Errorf("metric %s not found", name)
	}

	return metric, nil
}

// ListMetrics returns all metrics.
func (m *Monitor) ListMetrics() []*Metric {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make([]*Metric, 0, len(m.metrics))
	for _, metric := range m.metrics {
		metrics = append(metrics, metric)
	}

	return metrics
}

// AddAlert adds a new alert.
func (m *Monitor) AddAlert(alert *Alert) error {
	if !m.config.EnableAlerts {
		return errors.New("alerts are disabled")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if we've reached the maximum number of alerts
	if len(m.alerts) >= m.config.MaxAlerts {
		return errors.New("maximum number of alerts reached")
	}

	alert.CreatedAt = time.Now()
	alert.UpdatedAt = time.Now()
	alert.Status = AlertStatusInactive
	m.alerts[alert.ID] = alert

	return nil
}

// UpdateAlert updates an existing alert.
func (m *Monitor) UpdateAlert(alert *Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.alerts[alert.ID]
	if !exists {
		return fmt.Errorf("alert %s not found", alert.ID)
	}

	alert.CreatedAt = existing.CreatedAt
	alert.UpdatedAt = time.Now()
	m.alerts[alert.ID] = alert

	return nil
}

// RemoveAlert removes an alert.
func (m *Monitor) RemoveAlert(alertID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.alerts, alertID)

	return nil
}

// GetAlert gets an alert by ID.
func (m *Monitor) GetAlert(alertID string) (*Alert, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alert, exists := m.alerts[alertID]
	if !exists {
		return nil, fmt.Errorf("alert %s not found", alertID)
	}

	return alert, nil
}

// ListAlerts returns all alerts.
func (m *Monitor) ListAlerts() []*Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alerts := make([]*Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		alerts = append(alerts, alert)
	}

	return alerts
}

// RegisterAlertHandler registers an alert handler.
func (m *Monitor) RegisterAlertHandler(handler AlertHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.handlers[handler.GetName()] = handler
}

// UnregisterAlertHandler unregisters an alert handler.
func (m *Monitor) UnregisterAlertHandler(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.handlers, name)
}

// startAlerting starts the alerting goroutine.
func (m *Monitor) startAlerting() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkAlerts()
		case <-m.stopC:
			return
		}
	}
}

// checkAlerts checks all alerts for firing conditions.
func (m *Monitor) checkAlerts() {
	m.mu.RLock()

	alerts := make([]*Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		alerts = append(alerts, alert)
	}

	m.mu.RUnlock()

	for _, alert := range alerts {
		if m.shouldFireAlert(alert) {
			m.fireAlert(alert)
		} else if alert.Status == AlertStatusFiring {
			m.resolveAlert(alert)
		}
	}
}

// shouldFireAlert determines if an alert should fire.
func (m *Monitor) shouldFireAlert(alert *Alert) bool {
	// Simple threshold-based alerting
	// In a real implementation, this would be more sophisticated
	metric, err := m.GetMetric(alert.Condition)
	if err != nil {
		return false
	}

	switch alert.Condition {
	case "cpu_usage":
		return metric.Value > alert.Threshold
	case "memory_usage":
		return metric.Value > alert.Threshold
	case "response_time":
		return metric.Value > alert.Threshold
	case "error_rate":
		return metric.Value > alert.Threshold
	default:
		return false
	}
}

// fireAlert fires an alert.
func (m *Monitor) fireAlert(alert *Alert) {
	if alert.Status == AlertStatusFiring {
		return // Already firing
	}

	alert.Status = AlertStatusFiring
	alert.FiredAt = time.Now()
	alert.UpdatedAt = time.Now()

	m.mu.Lock()
	m.alerts[alert.ID] = alert
	m.mu.Unlock()

	// Notify handlers
	m.notifyHandlers(context.Background(), alert)
}

// resolveAlert resolves an alert.
func (m *Monitor) resolveAlert(alert *Alert) {
	if alert.Status != AlertStatusFiring {
		return // Not firing
	}

	alert.Status = AlertStatusResolved
	alert.ResolvedAt = time.Now()
	alert.UpdatedAt = time.Now()

	m.mu.Lock()
	m.alerts[alert.ID] = alert
	m.mu.Unlock()
}

// notifyHandlers notifies all registered handlers.
func (m *Monitor) notifyHandlers(ctx context.Context, alert *Alert) {
	m.mu.RLock()

	handlers := make([]AlertHandler, 0, len(m.handlers))
	for _, handler := range m.handlers {
		handlers = append(handlers, handler)
	}

	m.mu.RUnlock()

	for _, handler := range handlers {
		go func(h AlertHandler) {
			if err := h.HandleAlert(ctx, alert); err != nil {
				m.logger.Error("alert handler failed",
					logger.String("handler", h.GetName()),
					logger.String("alert", alert.ID),
					logger.String("error", err.Error()))
			}
		}(handler)
	}
}

// startHealthChecks starts the health check goroutine.
func (m *Monitor) startHealthChecks() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.performHealthChecks()
		case <-m.stopC:
			return
		}
	}
}

// performHealthChecks performs health checks.
func (m *Monitor) performHealthChecks() {
	// In a real implementation, this would check various system components
	// For now, we'll just record a basic health metric
	healthMetric := &Metric{
		Name:        "system_health",
		Type:        MetricTypeGauge,
		Value:       1.0, // 1.0 = healthy, 0.5 = degraded, 0.0 = unhealthy
		Unit:        "status",
		Labels:      map[string]string{"component": "system"},
		Description: "Overall system health status",
	}

	m.RecordMetric(healthMetric)
}

// GetHealthStatus returns the current health status.
func (m *Monitor) GetHealthStatus() *HealthCheck {
	// Simple health check implementation
	// In a real implementation, this would check various components
	startTime := time.Now()
	status := HealthStatusHealthy
	message := "All systems operational"

	// Check if there are any critical alerts
	m.mu.RLock()

	criticalAlerts := 0

	for _, alert := range m.alerts {
		if alert.Severity == AlertSeverityCritical && alert.Status == AlertStatusFiring {
			criticalAlerts++
		}
	}

	m.mu.RUnlock()

	if criticalAlerts > 0 {
		status = HealthStatusUnhealthy
		message = fmt.Sprintf("%d critical alerts active", criticalAlerts)
	}

	return &HealthCheck{
		Name:      "system",
		Status:    status,
		Message:   message,
		Duration:  time.Since(startTime),
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"critical_alerts": strconv.Itoa(criticalAlerts),
		},
	}
}

// GetStats returns monitoring statistics.
func (m *Monitor) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Count alerts by status
	alertCounts := make(map[string]int)

	for _, alert := range m.alerts {
		switch alert.Status {
		case AlertStatusInactive:
			alertCounts["inactive"]++
		case AlertStatusPending:
			alertCounts["pending"]++
		case AlertStatusFiring:
			alertCounts["firing"]++
		case AlertStatusResolved:
			alertCounts["resolved"]++
		}
	}

	return map[string]interface{}{
		"metrics_count":  len(m.metrics),
		"alerts_count":   len(m.alerts),
		"handlers_count": len(m.handlers),
		"alert_counts":   alertCounts,
		"config":         m.config,
	}
}

// Shutdown shuts down the monitor.
func (m *Monitor) Shutdown(ctx context.Context) error {
	close(m.stopC)
	m.wg.Wait()

	return nil
}

// Built-in alert handlers

// LogAlertHandler logs alerts to the logger.
type LogAlertHandler struct {
	logger logger.Logger
}

func NewLogAlertHandler(logger logger.Logger) *LogAlertHandler {
	return &LogAlertHandler{logger: logger}
}

func (h *LogAlertHandler) HandleAlert(ctx context.Context, alert *Alert) error {
	h.logger.Warn("alert fired",
		logger.String("alert_id", alert.ID),
		logger.String("alert_name", alert.Name),
		logger.String("severity", alert.Severity.String()),
		logger.String("condition", alert.Condition),
		logger.Float64("threshold", alert.Threshold))

	return nil
}

func (h *LogAlertHandler) GetName() string {
	return "log"
}

// EmailAlertHandler sends alerts via email.
type EmailAlertHandler struct {
	recipients []string
	smtpConfig map[string]string
}

func NewEmailAlertHandler(recipients []string, smtpConfig map[string]string) *EmailAlertHandler {
	return &EmailAlertHandler{
		recipients: recipients,
		smtpConfig: smtpConfig,
	}
}

func (h *EmailAlertHandler) HandleAlert(ctx context.Context, alert *Alert) error {
	// In a real implementation, this would send an email
	return nil
}

func (h *EmailAlertHandler) GetName() string {
	return "email"
}

// SlackAlertHandler sends alerts to Slack.
type SlackAlertHandler struct {
	webhookURL string
	channel    string
}

func NewSlackAlertHandler(webhookURL, channel string) *SlackAlertHandler {
	return &SlackAlertHandler{
		webhookURL: webhookURL,
		channel:    channel,
	}
}

func (h *SlackAlertHandler) HandleAlert(ctx context.Context, alert *Alert) error {
	// In a real implementation, this would send a Slack message
	return nil
}

func (h *SlackAlertHandler) GetName() string {
	return "slack"
}

// String methods for enums

func (s AlertSeverity) String() string {
	switch s {
	case AlertSeverityInfo:
		return "info"
	case AlertSeverityWarning:
		return "warning"
	case AlertSeverityCritical:
		return "critical"
	case AlertSeverityEmergency:
		return "emergency"
	default:
		return "unknown"
	}
}

func (s AlertStatus) String() string {
	switch s {
	case AlertStatusInactive:
		return "inactive"
	case AlertStatusPending:
		return "pending"
	case AlertStatusFiring:
		return "firing"
	case AlertStatusResolved:
		return "resolved"
	default:
		return "unknown"
	}
}

func (s HealthStatus) String() string {
	switch s {
	case HealthStatusUnknown:
		return "unknown"
	case HealthStatusHealthy:
		return "healthy"
	case HealthStatusDegraded:
		return "degraded"
	case HealthStatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}
