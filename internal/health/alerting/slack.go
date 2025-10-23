package alerting

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// SlackNotifier implements AlertNotifier for Slack notifications
type SlackNotifier struct {
	config  *SlackConfig
	client  *http.Client
	logger  logger.Logger
	metrics shared.Metrics
	name    string
}

// SlackConfig contains configuration for Slack notifications
type SlackConfig struct {
	// Webhook URL for Slack incoming webhooks
	WebhookURL string `yaml:"webhook_url" json:"webhook_url"`

	// Bot Token for Slack Bot API (alternative to webhook)
	BotToken string `yaml:"bot_token" json:"bot_token"`

	// Channel to send notifications to (for Bot API)
	Channel string `yaml:"channel" json:"channel"`

	// Username for the bot (webhook only)
	Username string `yaml:"username" json:"username"`

	// Icon emoji for the bot (webhook only)
	IconEmoji string `yaml:"icon_emoji" json:"icon_emoji"`

	// Icon URL for the bot (webhook only)
	IconURL string `yaml:"icon_url" json:"icon_url"`

	// Template for custom message formatting
	Template string `yaml:"template" json:"template"`

	// Timeout for HTTP requests
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// Enable threaded messages
	EnableThreading bool `yaml:"enable_threading" json:"enable_threading"`

	// Thread timestamp for replies
	ThreadTS string `yaml:"thread_ts" json:"thread_ts"`

	// Color coding for different severity levels
	Colors map[AlertSeverity]string `yaml:"colors" json:"colors"`

	// Mention users on critical alerts
	MentionUsers []string `yaml:"mention_users" json:"mention_users"`

	// Mention channels on critical alerts
	MentionChannels []string `yaml:"mention_channels" json:"mention_channels"`

	// Enable attachment formatting
	EnableAttachments bool `yaml:"enable_attachments" json:"enable_attachments"`

	// Enable blocks formatting (modern Slack UI)
	EnableBlocks bool `yaml:"enable_blocks" json:"enable_blocks"`

	// Maximum message length (Slack limit is 4000 characters)
	MaxMessageLength int `yaml:"max_message_length" json:"max_message_length"`

	// Retry configuration
	MaxRetries int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay time.Duration `yaml:"retry_delay" json:"retry_delay"`

	// Rate limiting
	RateLimitEnabled bool          `yaml:"rate_limit_enabled" json:"rate_limit_enabled"`
	RateLimitWindow  time.Duration `yaml:"rate_limit_window" json:"rate_limit_window"`
	RateLimitMax     int           `yaml:"rate_limit_max" json:"rate_limit_max"`
}

// DefaultSlackConfig returns default configuration for Slack notifications
func DefaultSlackConfig() *SlackConfig {
	return &SlackConfig{
		Username:          "Forge Health Bot",
		IconEmoji:         ":hospital:",
		Timeout:           30 * time.Second,
		EnableThreading:   false,
		EnableAttachments: true,
		EnableBlocks:      true,
		MaxMessageLength:  3900, // Leave room for formatting
		MaxRetries:        3,
		RetryDelay:        2 * time.Second,
		RateLimitEnabled:  true,
		RateLimitWindow:   time.Minute,
		RateLimitMax:      20,
		Colors: map[AlertSeverity]string{
			AlertSeverityInfo:     "good",
			AlertSeverityWarning:  "warning",
			AlertSeverityError:    "danger",
			AlertSeverityCritical: "danger",
		},
	}
}

// NewSlackNotifier creates a new Slack notifier
func NewSlackNotifier(name string, config *SlackConfig, logger logger.Logger, metrics shared.Metrics) *SlackNotifier {
	if config == nil {
		config = DefaultSlackConfig()
	}

	// Validate configuration
	if config.WebhookURL == "" && config.BotToken == "" {
		if logger != nil {
			logger.Error("Slack configuration error: either webhook_url or bot_token must be provided")
		}
	}

	if config.BotToken != "" && config.Channel == "" {
		if logger != nil {
			logger.Error("Slack configuration error: channel must be provided when using bot_token")
		}
	}

	// Create HTTP client
	client := &http.Client{
		Timeout: config.Timeout,
	}

	return &SlackNotifier{
		config:  config,
		client:  client,
		logger:  logger,
		metrics: metrics,
		name:    name,
	}
}

// Name returns the name of the notifier
func (sn *SlackNotifier) Name() string {
	return sn.name
}

// Send sends an alert to Slack
func (sn *SlackNotifier) Send(ctx context.Context, alert *Alert) error {
	// Check rate limiting
	if sn.config.RateLimitEnabled {
		if err := sn.checkRateLimit(); err != nil {
			return fmt.Errorf("rate limit exceeded: %w", err)
		}
	}

	// Choose API method based on configuration
	if sn.config.BotToken != "" {
		return sn.sendViaAPI(ctx, alert)
	} else if sn.config.WebhookURL != "" {
		return sn.sendViaWebhook(ctx, alert)
	} else {
		return fmt.Errorf("no valid Slack configuration provided")
	}
}

// SendBatch sends multiple alerts in a batch
func (sn *SlackNotifier) SendBatch(ctx context.Context, alerts []*Alert) error {
	if len(alerts) == 0 {
		return nil
	}

	// For Slack, we'll send individual messages to avoid hitting limits
	// In a real implementation, you might want to combine related alerts
	for _, alert := range alerts {
		if err := sn.Send(ctx, alert); err != nil {
			if sn.logger != nil {
				sn.logger.Error("failed to send alert in batch",
					logger.String("alert_id", alert.ID),
					logger.Error(err),
				)
			}
		}

		// Add small delay between messages to avoid rate limiting
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// Test tests the Slack notification
func (sn *SlackNotifier) Test(ctx context.Context) error {
	testAlert := NewAlert(AlertTypeSystemError, AlertSeverityInfo, "Test Alert", "This is a test alert from Forge Health Alerting")
	testAlert.WithSource("test")
	testAlert.WithService("test-service")
	testAlert.WithDetail("test", true)

	return sn.Send(ctx, testAlert)
}

// Close closes the Slack notifier
func (sn *SlackNotifier) Close() error {
	// HTTP client doesn't need explicit closing
	return nil
}

// sendViaWebhook sends alert via Slack webhook
func (sn *SlackNotifier) sendViaWebhook(ctx context.Context, alert *Alert) error {
	payload, err := sn.createWebhookPayload(alert)
	if err != nil {
		return fmt.Errorf("failed to create webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", sn.config.WebhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Forge-Health-Alerting/1.0")

	start := time.Now()
	resp, err := sn.client.Do(req)
	duration := time.Since(start)

	// Record metrics
	if sn.metrics != nil {
		sn.metrics.Counter("forge.health.slack_webhook_requests").Inc()
		sn.metrics.Histogram("forge.health.slack_webhook_duration").Observe(duration.Seconds())
	}

	if err != nil {
		if sn.metrics != nil {
			sn.metrics.Counter("forge.health.slack_webhook_errors").Inc()
		}
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if sn.metrics != nil {
			sn.metrics.Counter("forge.health.slack_webhook_errors").Inc()
		}
		return fmt.Errorf("webhook request failed with status %d", resp.StatusCode)
	}

	if sn.metrics != nil {
		sn.metrics.Counter("forge.health.slack_webhook_success").Inc()
	}

	if sn.logger != nil {
		sn.logger.Info("slack webhook alert sent successfully",
			logger.String("alert_id", alert.ID),
			logger.Int("status_code", resp.StatusCode),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// sendViaAPI sends alert via Slack Bot API
func (sn *SlackNotifier) sendViaAPI(ctx context.Context, alert *Alert) error {
	payload, err := sn.createAPIPayload(alert)
	if err != nil {
		return fmt.Errorf("failed to create API payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.postMessage", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create API request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sn.config.BotToken)
	req.Header.Set("User-Agent", "Forge-Health-Alerting/1.0")

	start := time.Now()
	resp, err := sn.client.Do(req)
	duration := time.Since(start)

	// Record metrics
	if sn.metrics != nil {
		sn.metrics.Counter("forge.health.slack_api_requests").Inc()
		sn.metrics.Histogram("forge.health.slack_api_duration").Observe(duration.Seconds())
	}

	if err != nil {
		if sn.metrics != nil {
			sn.metrics.Counter("forge.health.slack_api_errors").Inc()
		}
		return fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var response struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		TS    string `json:"ts"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		if sn.metrics != nil {
			sn.metrics.Counter("forge.health.slack_api_errors").Inc()
		}
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.OK {
		if sn.metrics != nil {
			sn.metrics.Counter("forge.health.slack_api_errors").Inc()
		}
		return fmt.Errorf("Slack API error: %s", response.Error)
	}

	if sn.metrics != nil {
		sn.metrics.Counter("forge.health.slack_api_success").Inc()
	}

	if sn.logger != nil {
		sn.logger.Info("slack API alert sent successfully",
			logger.String("alert_id", alert.ID),
			logger.String("message_ts", response.TS),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// createWebhookPayload creates payload for Slack webhook
func (sn *SlackNotifier) createWebhookPayload(alert *Alert) ([]byte, error) {
	if sn.config.Template != "" {
		return sn.createCustomPayload(alert)
	}

	message := sn.formatMessage(alert)

	payload := map[string]interface{}{
		"text":     message,
		"username": sn.config.Username,
	}

	if sn.config.IconEmoji != "" {
		payload["icon_emoji"] = sn.config.IconEmoji
	}

	if sn.config.IconURL != "" {
		payload["icon_url"] = sn.config.IconURL
	}

	if sn.config.EnableAttachments {
		payload["attachments"] = []map[string]interface{}{
			sn.createAttachment(alert),
		}
	}

	if sn.config.EnableBlocks {
		payload["blocks"] = sn.createBlocks(alert)
	}

	return json.Marshal(payload)
}

// createAPIPayload creates payload for Slack API
func (sn *SlackNotifier) createAPIPayload(alert *Alert) ([]byte, error) {
	if sn.config.Template != "" {
		return sn.createCustomPayload(alert)
	}

	message := sn.formatMessage(alert)

	payload := map[string]interface{}{
		"channel": sn.config.Channel,
		"text":    message,
	}

	if sn.config.EnableThreading && sn.config.ThreadTS != "" {
		payload["thread_ts"] = sn.config.ThreadTS
	}

	if sn.config.EnableAttachments {
		payload["attachments"] = []map[string]interface{}{
			sn.createAttachment(alert),
		}
	}

	if sn.config.EnableBlocks {
		payload["blocks"] = sn.createBlocks(alert)
	}

	return json.Marshal(payload)
}

// createCustomPayload creates a custom payload using template
func (sn *SlackNotifier) createCustomPayload(alert *Alert) ([]byte, error) {
	template := sn.config.Template
	template = strings.ReplaceAll(template, "{{.ID}}", alert.ID)
	template = strings.ReplaceAll(template, "{{.Type}}", string(alert.Type))
	template = strings.ReplaceAll(template, "{{.Severity}}", string(alert.Severity))
	template = strings.ReplaceAll(template, "{{.Title}}", alert.Title)
	template = strings.ReplaceAll(template, "{{.Message}}", alert.Message)
	template = strings.ReplaceAll(template, "{{.Source}}", alert.Source)
	template = strings.ReplaceAll(template, "{{.Service}}", alert.Service)
	template = strings.ReplaceAll(template, "{{.Timestamp}}", alert.Timestamp.Format(time.RFC3339))

	return []byte(template), nil
}

// formatMessage formats the alert message for Slack
func (sn *SlackNotifier) formatMessage(alert *Alert) string {
	var message strings.Builder

	// Add mentions for critical alerts
	if alert.Severity == AlertSeverityCritical {
		for _, user := range sn.config.MentionUsers {
			message.WriteString(fmt.Sprintf("<@%s> ", user))
		}
		for _, channel := range sn.config.MentionChannels {
			message.WriteString(fmt.Sprintf("<!channel|%s> ", channel))
		}
	}

	// Add severity emoji
	switch alert.Severity {
	case AlertSeverityInfo:
		message.WriteString(":information_source: ")
	case AlertSeverityWarning:
		message.WriteString(":warning: ")
	case AlertSeverityError:
		message.WriteString(":x: ")
	case AlertSeverityCritical:
		message.WriteString(":rotating_light: ")
	}

	// Add title and message
	message.WriteString(fmt.Sprintf("*%s*\n", alert.Title))
	message.WriteString(alert.Message)

	// Truncate if too long
	result := message.String()
	if len(result) > sn.config.MaxMessageLength {
		result = result[:sn.config.MaxMessageLength-3] + "..."
	}

	return result
}

// createAttachment creates a Slack attachment
func (sn *SlackNotifier) createAttachment(alert *Alert) map[string]interface{} {
	color := sn.config.Colors[alert.Severity]
	if color == "" {
		color = "good"
	}

	fields := []map[string]interface{}{
		{
			"title": "Service",
			"value": alert.Service,
			"short": true,
		},
		{
			"title": "Severity",
			"value": strings.ToUpper(string(alert.Severity)),
			"short": true,
		},
		{
			"title": "Source",
			"value": alert.Source,
			"short": true,
		},
		{
			"title": "Type",
			"value": string(alert.Type),
			"short": true,
		},
		{
			"title": "Timestamp",
			"value": alert.Timestamp.Format("2006-01-02 15:04:05 MST"),
			"short": false,
		},
	}

	// Add additional fields from details
	for key, value := range alert.Details {
		if len(fields) >= 20 { // Slack limit
			break
		}
		fields = append(fields, map[string]interface{}{
			"title": strings.Title(key),
			"value": fmt.Sprintf("%v", value),
			"short": true,
		})
	}

	attachment := map[string]interface{}{
		"color":     color,
		"fields":    fields,
		"timestamp": alert.Timestamp.Unix(),
		"footer":    "Forge Health Alerting",
		"mrkdwn_in": []string{"text", "pretext", "fields"},
	}

	return attachment
}

// createBlocks creates Slack blocks (modern UI)
func (sn *SlackNotifier) createBlocks(alert *Alert) []map[string]interface{} {
	blocks := []map[string]interface{}{
		{
			"type": "header",
			"text": map[string]interface{}{
				"type": "plain_text",
				"text": alert.Title,
			},
		},
		{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": alert.Message,
			},
		},
		{
			"type": "section",
			"fields": []map[string]interface{}{
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Service:*\n%s", alert.Service),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Severity:*\n%s", strings.ToUpper(string(alert.Severity))),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Source:*\n%s", alert.Source),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Type:*\n%s", string(alert.Type)),
				},
			},
		},
		{
			"type": "context",
			"elements": []map[string]interface{}{
				{
					"type": "plain_text",
					"text": fmt.Sprintf("Alert ID: %s | %s", alert.ID, alert.Timestamp.Format("2006-01-02 15:04:05 MST")),
				},
			},
		},
	}

	return blocks
}

// checkRateLimit checks if we're within rate limits
func (sn *SlackNotifier) checkRateLimit() error {
	// Simple rate limiting - in production, use a proper rate limiter
	// This is a placeholder implementation
	return nil
}

// SlackNotifierBuilder helps build Slack notifiers with fluent interface
type SlackNotifierBuilder struct {
	config  *SlackConfig
	logger  logger.Logger
	metrics shared.Metrics
	name    string
}

// NewSlackNotifierBuilder creates a new Slack notifier builder
func NewSlackNotifierBuilder(name string) *SlackNotifierBuilder {
	return &SlackNotifierBuilder{
		config: DefaultSlackConfig(),
		name:   name,
	}
}

// WithWebhookURL sets the webhook URL
func (snb *SlackNotifierBuilder) WithWebhookURL(url string) *SlackNotifierBuilder {
	snb.config.WebhookURL = url
	return snb
}

// WithBotToken sets the bot token
func (snb *SlackNotifierBuilder) WithBotToken(token string) *SlackNotifierBuilder {
	snb.config.BotToken = token
	return snb
}

// WithChannel sets the channel
func (snb *SlackNotifierBuilder) WithChannel(channel string) *SlackNotifierBuilder {
	snb.config.Channel = channel
	return snb
}

// WithUsername sets the username
func (snb *SlackNotifierBuilder) WithUsername(username string) *SlackNotifierBuilder {
	snb.config.Username = username
	return snb
}

// WithIconEmoji sets the icon emoji
func (snb *SlackNotifierBuilder) WithIconEmoji(emoji string) *SlackNotifierBuilder {
	snb.config.IconEmoji = emoji
	return snb
}

// WithIconURL sets the icon URL
func (snb *SlackNotifierBuilder) WithIconURL(url string) *SlackNotifierBuilder {
	snb.config.IconURL = url
	return snb
}

// WithTemplate sets the custom template
func (snb *SlackNotifierBuilder) WithTemplate(template string) *SlackNotifierBuilder {
	snb.config.Template = template
	return snb
}

// WithTimeout sets the timeout
func (snb *SlackNotifierBuilder) WithTimeout(timeout time.Duration) *SlackNotifierBuilder {
	snb.config.Timeout = timeout
	return snb
}

// WithThreading enables threading
func (snb *SlackNotifierBuilder) WithThreading(enabled bool) *SlackNotifierBuilder {
	snb.config.EnableThreading = enabled
	return snb
}

// WithAttachments enables attachments
func (snb *SlackNotifierBuilder) WithAttachments(enabled bool) *SlackNotifierBuilder {
	snb.config.EnableAttachments = enabled
	return snb
}

// WithBlocks enables blocks
func (snb *SlackNotifierBuilder) WithBlocks(enabled bool) *SlackNotifierBuilder {
	snb.config.EnableBlocks = enabled
	return snb
}

// WithMentionUsers sets users to mention on critical alerts
func (snb *SlackNotifierBuilder) WithMentionUsers(users ...string) *SlackNotifierBuilder {
	snb.config.MentionUsers = users
	return snb
}

// WithMentionChannels sets channels to mention on critical alerts
func (snb *SlackNotifierBuilder) WithMentionChannels(channels ...string) *SlackNotifierBuilder {
	snb.config.MentionChannels = channels
	return snb
}

// WithColors sets severity colors
func (snb *SlackNotifierBuilder) WithColors(colors map[AlertSeverity]string) *SlackNotifierBuilder {
	snb.config.Colors = colors
	return snb
}

// WithRetry sets retry configuration
func (snb *SlackNotifierBuilder) WithRetry(maxRetries int, retryDelay time.Duration) *SlackNotifierBuilder {
	snb.config.MaxRetries = maxRetries
	snb.config.RetryDelay = retryDelay
	return snb
}

// WithRateLimit sets rate limiting
func (snb *SlackNotifierBuilder) WithRateLimit(enabled bool, window time.Duration, max int) *SlackNotifierBuilder {
	snb.config.RateLimitEnabled = enabled
	snb.config.RateLimitWindow = window
	snb.config.RateLimitMax = max
	return snb
}

// WithLogger sets the logger
func (snb *SlackNotifierBuilder) WithLogger(logger logger.Logger) *SlackNotifierBuilder {
	snb.logger = logger
	return snb
}

// WithMetrics sets the metrics collector
func (snb *SlackNotifierBuilder) WithMetrics(metrics shared.Metrics) *SlackNotifierBuilder {
	snb.metrics = metrics
	return snb
}

// Build creates the Slack notifier
func (snb *SlackNotifierBuilder) Build() *SlackNotifier {
	return NewSlackNotifier(snb.name, snb.config, snb.logger, snb.metrics)
}

// Convenience functions for common Slack configurations

// NewSlackWebhookNotifier creates a simple webhook-based Slack notifier
func NewSlackWebhookNotifier(name, webhookURL string, logger logger.Logger, metrics shared.Metrics) *SlackNotifier {
	return NewSlackNotifierBuilder(name).
		WithWebhookURL(webhookURL).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}

// NewSlackBotNotifier creates a bot-based Slack notifier
func NewSlackBotNotifier(name, botToken, channel string, logger logger.Logger, metrics shared.Metrics) *SlackNotifier {
	return NewSlackNotifierBuilder(name).
		WithBotToken(botToken).
		WithChannel(channel).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}

// NewSlackNotifierWithMentions creates a Slack notifier with mention capabilities
func NewSlackNotifierWithMentions(name, webhookURL string, mentionUsers, mentionChannels []string, logger logger.Logger, metrics shared.Metrics) *SlackNotifier {
	return NewSlackNotifierBuilder(name).
		WithWebhookURL(webhookURL).
		WithMentionUsers(mentionUsers...).
		WithMentionChannels(mentionChannels...).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}
