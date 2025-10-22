package alerting

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/shared"
)

// WebhookNotifier implements AlertNotifier for webhook notifications
type WebhookNotifier struct {
	config  *WebhookConfig
	client  *http.Client
	logger  logger.Logger
	metrics shared.Metrics
	name    string
}

// WebhookConfig contains configuration for webhook notifications
type WebhookConfig struct {
	URL                string            `yaml:"url" json:"url"`
	Method             string            `yaml:"method" json:"method"`
	Headers            map[string]string `yaml:"headers" json:"headers"`
	Timeout            time.Duration     `yaml:"timeout" json:"timeout"`
	Secret             string            `yaml:"secret" json:"secret"`
	SignatureHeader    string            `yaml:"signature_header" json:"signature_header"`
	ContentType        string            `yaml:"content_type" json:"content_type"`
	Template           string            `yaml:"template" json:"template"`
	MaxRetries         int               `yaml:"max_retries" json:"max_retries"`
	RetryDelay         time.Duration     `yaml:"retry_delay" json:"retry_delay"`
	InsecureSkipVerify bool              `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
	ProxyURL           string            `yaml:"proxy_url" json:"proxy_url"`
	Username           string            `yaml:"username" json:"username"`
	Password           string            `yaml:"password" json:"password"`
	CustomPayload      bool              `yaml:"custom_payload" json:"custom_payload"`
}

// DefaultWebhookConfig returns default configuration for webhook notifications
func DefaultWebhookConfig() *WebhookConfig {
	return &WebhookConfig{
		Method:          "POST",
		Headers:         make(map[string]string),
		Timeout:         30 * time.Second,
		ContentType:     "application/json",
		MaxRetries:      3,
		RetryDelay:      1 * time.Second,
		SignatureHeader: "X-Signature",
		CustomPayload:   false,
	}
}

// NewWebhookNotifier creates a new webhook notifier
func NewWebhookNotifier(name string, config *WebhookConfig, logger logger.Logger, metrics shared.Metrics) *WebhookNotifier {
	if config == nil {
		config = DefaultWebhookConfig()
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: config.Timeout,
	}

	// Configure transport if needed
	if config.InsecureSkipVerify || config.ProxyURL != "" {
		// In a real implementation, configure the transport
		// For now, use default client
	}

	// Set default headers
	if config.Headers == nil {
		config.Headers = make(map[string]string)
	}
	config.Headers["Content-Type"] = config.ContentType
	config.Headers["User-Agent"] = "Forge-Health-Alerting/1.0"

	return &WebhookNotifier{
		config:  config,
		client:  client,
		logger:  logger,
		metrics: metrics,
		name:    name,
	}
}

// Name returns the name of the notifier
func (wn *WebhookNotifier) Name() string {
	return wn.name
}

// Send sends an alert via webhook
func (wn *WebhookNotifier) Send(ctx context.Context, alert *Alert) error {
	if wn.config.URL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	// Create payload
	payload, err := wn.createPayload(alert)
	if err != nil {
		return fmt.Errorf("failed to create payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, wn.config.Method, wn.config.URL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	for key, value := range wn.config.Headers {
		req.Header.Set(key, value)
	}

	// Add authentication if configured
	if wn.config.Username != "" && wn.config.Password != "" {
		req.SetBasicAuth(wn.config.Username, wn.config.Password)
	}

	// Add signature if secret is configured
	if wn.config.Secret != "" {
		signature := wn.calculateSignature(payload)
		req.Header.Set(wn.config.SignatureHeader, signature)
	}

	// Send request
	start := time.Now()
	resp, err := wn.client.Do(req)
	duration := time.Since(start)

	// Record metrics
	if wn.metrics != nil {
		wn.metrics.Counter("forge.health.webhook_requests").Inc()
		wn.metrics.Histogram("forge.health.webhook_duration").Observe(duration.Seconds())
	}

	if err != nil {
		if wn.metrics != nil {
			wn.metrics.Counter("forge.health.webhook_errors").Inc()
		}
		return fmt.Errorf("failed to send webhook request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if wn.metrics != nil {
			wn.metrics.Counter("forge.health.webhook_errors").Inc()
		}

		// Read response body for error details
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if wn.metrics != nil {
		wn.metrics.Counter("forge.health.webhook_success").Inc()
	}

	if wn.logger != nil {
		wn.logger.Info("webhook alert sent successfully",
			logger.String("url", wn.config.URL),
			logger.String("alert_id", alert.ID),
			logger.Int("status_code", resp.StatusCode),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// SendBatch sends multiple alerts in a batch
func (wn *WebhookNotifier) SendBatch(ctx context.Context, alerts []*Alert) error {
	if len(alerts) == 0 {
		return nil
	}

	// Create batch payload
	var payload []byte
	var err error

	if wn.config.CustomPayload {
		payload, err = wn.createBatchPayload(alerts)
	} else {
		payload, err = wn.createDefaultBatchPayload(alerts)
	}

	if err != nil {
		return fmt.Errorf("failed to create batch payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, wn.config.Method, wn.config.URL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create batch request: %w", err)
	}

	// Add headers
	for key, value := range wn.config.Headers {
		req.Header.Set(key, value)
	}

	// Add authentication if configured
	if wn.config.Username != "" && wn.config.Password != "" {
		req.SetBasicAuth(wn.config.Username, wn.config.Password)
	}

	// Add signature if secret is configured
	if wn.config.Secret != "" {
		signature := wn.calculateSignature(payload)
		req.Header.Set(wn.config.SignatureHeader, signature)
	}

	// Send request
	start := time.Now()
	resp, err := wn.client.Do(req)
	duration := time.Since(start)

	// Record metrics
	if wn.metrics != nil {
		wn.metrics.Counter("forge.health.webhook_batch_requests").Inc()
		wn.metrics.Histogram("forge.health.webhook_batch_duration").Observe(duration.Seconds())
	}

	if err != nil {
		if wn.metrics != nil {
			wn.metrics.Counter("forge.health.webhook_batch_errors").Inc()
		}
		return fmt.Errorf("failed to send batch webhook request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if wn.metrics != nil {
			wn.metrics.Counter("forge.health.webhook_batch_errors").Inc()
		}

		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("batch webhook request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if wn.metrics != nil {
		wn.metrics.Counter("forge.health.webhook_batch_success").Inc()
	}

	if wn.logger != nil {
		wn.logger.Info("webhook batch alert sent successfully",
			logger.String("url", wn.config.URL),
			logger.Int("alert_count", len(alerts)),
			logger.Int("status_code", resp.StatusCode),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// Test tests the webhook notification
func (wn *WebhookNotifier) Test(ctx context.Context) error {
	testAlert := NewAlert(AlertTypeSystemError, AlertSeverityInfo, "Test Alert", "This is a test alert from Forge Health Alerting")
	testAlert.WithSource("test")
	testAlert.WithService("test-service")
	testAlert.WithDetail("test", true)

	return wn.Send(ctx, testAlert)
}

// Close closes the webhook notifier
func (wn *WebhookNotifier) Close() error {
	// HTTP client doesn't need explicit closing
	return nil
}

// createPayload creates the payload for a single alert
func (wn *WebhookNotifier) createPayload(alert *Alert) ([]byte, error) {
	if wn.config.CustomPayload && wn.config.Template != "" {
		return wn.createCustomPayload(alert)
	}

	return wn.createDefaultPayload(alert)
}

// createDefaultPayload creates the default JSON payload
func (wn *WebhookNotifier) createDefaultPayload(alert *Alert) ([]byte, error) {
	payload := map[string]interface{}{
		"alert_id":    alert.ID,
		"type":        alert.Type,
		"severity":    alert.Severity,
		"title":       alert.Title,
		"message":     alert.Message,
		"source":      alert.Source,
		"service":     alert.Service,
		"timestamp":   alert.Timestamp.Format(time.RFC3339),
		"resolved":    alert.Resolved,
		"details":     alert.Details,
		"tags":        alert.Tags,
		"metadata":    alert.Metadata,
		"fingerprint": alert.GetFingerprint(),
	}

	if alert.ResolvedAt != nil {
		payload["resolved_at"] = alert.ResolvedAt.Format(time.RFC3339)
	}

	return json.Marshal(payload)
}

// createCustomPayload creates a custom payload using the template
func (wn *WebhookNotifier) createCustomPayload(alert *Alert) ([]byte, error) {
	// Simple template replacement - in production, use a proper template engine
	template := wn.config.Template
	template = strings.ReplaceAll(template, "{{.ID}}", alert.ID)
	template = strings.ReplaceAll(template, "{{.Type}}", string(alert.Type))
	template = strings.ReplaceAll(template, "{{.Severity}}", string(alert.Severity))
	template = strings.ReplaceAll(template, "{{.Title}}", alert.Title)
	template = strings.ReplaceAll(template, "{{.Message}}", alert.Message)
	template = strings.ReplaceAll(template, "{{.Source}}", alert.Source)
	template = strings.ReplaceAll(template, "{{.Service}}", alert.Service)
	template = strings.ReplaceAll(template, "{{.Timestamp}}", alert.Timestamp.Format(time.RFC3339))
	template = strings.ReplaceAll(template, "{{.Resolved}}", fmt.Sprintf("%t", alert.Resolved))
	template = strings.ReplaceAll(template, "{{.Fingerprint}}", alert.GetFingerprint())

	return []byte(template), nil
}

// createBatchPayload creates a batch payload using custom template
func (wn *WebhookNotifier) createBatchPayload(alerts []*Alert) ([]byte, error) {
	// For batch processing with custom template, create a wrapper
	batchData := map[string]interface{}{
		"alerts":    alerts,
		"count":     len(alerts),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	return json.Marshal(batchData)
}

// createDefaultBatchPayload creates the default batch payload
func (wn *WebhookNotifier) createDefaultBatchPayload(alerts []*Alert) ([]byte, error) {
	payload := map[string]interface{}{
		"alerts":    alerts,
		"count":     len(alerts),
		"timestamp": time.Now().Format(time.RFC3339),
		"source":    "forge-health-alerting",
	}

	return json.Marshal(payload)
}

// calculateSignature calculates HMAC signature for the payload
func (wn *WebhookNotifier) calculateSignature(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(wn.config.Secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// WebhookNotifierBuilder helps build webhook notifiers with fluent interface
type WebhookNotifierBuilder struct {
	config  *WebhookConfig
	logger  logger.Logger
	metrics shared.Metrics
	name    string
}

// NewWebhookNotifierBuilder creates a new webhook notifier builder
func NewWebhookNotifierBuilder(name string) *WebhookNotifierBuilder {
	return &WebhookNotifierBuilder{
		config: DefaultWebhookConfig(),
		name:   name,
	}
}

// WithURL sets the webhook URL
func (wnb *WebhookNotifierBuilder) WithURL(url string) *WebhookNotifierBuilder {
	wnb.config.URL = url
	return wnb
}

// WithMethod sets the HTTP method
func (wnb *WebhookNotifierBuilder) WithMethod(method string) *WebhookNotifierBuilder {
	wnb.config.Method = method
	return wnb
}

// WithHeaders sets HTTP headers
func (wnb *WebhookNotifierBuilder) WithHeaders(headers map[string]string) *WebhookNotifierBuilder {
	wnb.config.Headers = headers
	return wnb
}

// WithHeader adds a single HTTP header
func (wnb *WebhookNotifierBuilder) WithHeader(key, value string) *WebhookNotifierBuilder {
	if wnb.config.Headers == nil {
		wnb.config.Headers = make(map[string]string)
	}
	wnb.config.Headers[key] = value
	return wnb
}

// WithTimeout sets the request timeout
func (wnb *WebhookNotifierBuilder) WithTimeout(timeout time.Duration) *WebhookNotifierBuilder {
	wnb.config.Timeout = timeout
	return wnb
}

// WithSecret sets the signing secret
func (wnb *WebhookNotifierBuilder) WithSecret(secret string) *WebhookNotifierBuilder {
	wnb.config.Secret = secret
	return wnb
}

// WithSignatureHeader sets the signature header name
func (wnb *WebhookNotifierBuilder) WithSignatureHeader(header string) *WebhookNotifierBuilder {
	wnb.config.SignatureHeader = header
	return wnb
}

// WithContentType sets the content type
func (wnb *WebhookNotifierBuilder) WithContentType(contentType string) *WebhookNotifierBuilder {
	wnb.config.ContentType = contentType
	return wnb
}

// WithTemplate sets the custom payload template
func (wnb *WebhookNotifierBuilder) WithTemplate(template string) *WebhookNotifierBuilder {
	wnb.config.Template = template
	wnb.config.CustomPayload = true
	return wnb
}

// WithBasicAuth sets basic authentication
func (wnb *WebhookNotifierBuilder) WithBasicAuth(username, password string) *WebhookNotifierBuilder {
	wnb.config.Username = username
	wnb.config.Password = password
	return wnb
}

// WithRetry sets retry configuration
func (wnb *WebhookNotifierBuilder) WithRetry(maxRetries int, retryDelay time.Duration) *WebhookNotifierBuilder {
	wnb.config.MaxRetries = maxRetries
	wnb.config.RetryDelay = retryDelay
	return wnb
}

// WithInsecureSkipVerify sets TLS verification skip
func (wnb *WebhookNotifierBuilder) WithInsecureSkipVerify(skip bool) *WebhookNotifierBuilder {
	wnb.config.InsecureSkipVerify = skip
	return wnb
}

// WithProxy sets proxy URL
func (wnb *WebhookNotifierBuilder) WithProxy(proxyURL string) *WebhookNotifierBuilder {
	wnb.config.ProxyURL = proxyURL
	return wnb
}

// WithLogger sets the logger
func (wnb *WebhookNotifierBuilder) WithLogger(logger logger.Logger) *WebhookNotifierBuilder {
	wnb.logger = logger
	return wnb
}

// WithMetrics sets the metrics collector
func (wnb *WebhookNotifierBuilder) WithMetrics(metrics shared.Metrics) *WebhookNotifierBuilder {
	wnb.metrics = metrics
	return wnb
}

// Build creates the webhook notifier
func (wnb *WebhookNotifierBuilder) Build() *WebhookNotifier {
	return NewWebhookNotifier(wnb.name, wnb.config, wnb.logger, wnb.metrics)
}

// Common webhook configurations for popular services

// NewPureSlackWebhookNotifier creates a webhook notifier configured for Slack
func NewPureSlackWebhookNotifier(name, webhookURL string, logger logger.Logger, metrics shared.Metrics) *WebhookNotifier {
	template := `{
		"text": "{{.Title}}",
		"attachments": [
			{
				"color": "{{if eq .Severity \"critical\"}}danger{{else if eq .Severity \"warning\"}}warning{{else}}good{{end}}",
				"fields": [
					{
						"title": "Service",
						"value": "{{.Service}}",
						"short": true
					},
					{
						"title": "Severity",
						"value": "{{.Severity}}",
						"short": true
					},
					{
						"title": "Message",
						"value": "{{.Message}}",
						"short": false
					},
					{
						"title": "Timestamp",
						"value": "{{.Timestamp}}",
						"short": true
					}
				]
			}
		]
	}`

	return NewWebhookNotifierBuilder(name).
		WithURL(webhookURL).
		WithContentType("application/json").
		WithTemplate(template).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}

// NewDiscordWebhookNotifier creates a webhook notifier configured for Discord
func NewDiscordWebhookNotifier(name, webhookURL string, logger logger.Logger, metrics shared.Metrics) *WebhookNotifier {
	template := `{
		"embeds": [
			{
				"title": "{{.Title}}",
				"description": "{{.Message}}",
				"color": {{if eq .Severity "critical"}}15158332{{else if eq .Severity "warning"}}16776960{{else}}3066993{{end}},
				"fields": [
					{
						"name": "Service",
						"value": "{{.Service}}",
						"inline": true
					},
					{
						"name": "Severity",
						"value": "{{.Severity}}",
						"inline": true
					},
					{
						"name": "Timestamp",
						"value": "{{.Timestamp}}",
						"inline": true
					}
				]
			}
		]
	}`

	return NewWebhookNotifierBuilder(name).
		WithURL(webhookURL).
		WithContentType("application/json").
		WithTemplate(template).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}

// NewMSTeamsWebhookNotifier creates a webhook notifier configured for Microsoft Teams
func NewMSTeamsWebhookNotifier(name, webhookURL string, logger logger.Logger, metrics shared.Metrics) *WebhookNotifier {
	template := `{
		"@type": "MessageCard",
		"@context": "https://schema.org/extensions",
		"themeColor": "{{if eq .Severity \"critical\"}}FF0000{{else if eq .Severity \"warning\"}}FFFF00{{else}}00FF00{{end}}",
		"summary": "{{.Title}}",
		"sections": [
			{
				"activityTitle": "{{.Title}}",
				"activitySubtitle": "{{.Service}}",
				"activityImage": "https://forge.example.com/icon.png",
				"facts": [
					{
						"name": "Service",
						"value": "{{.Service}}"
					},
					{
						"name": "Severity",
						"value": "{{.Severity}}"
					},
					{
						"name": "Message",
						"value": "{{.Message}}"
					},
					{
						"name": "Timestamp",
						"value": "{{.Timestamp}}"
					}
				]
			}
		]
	}`

	return NewWebhookNotifierBuilder(name).
		WithURL(webhookURL).
		WithContentType("application/json").
		WithTemplate(template).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}
