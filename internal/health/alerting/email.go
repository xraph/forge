package alerting

import (
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"net/smtp"
	"strings"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// EmailNotifier implements AlertNotifier for email notifications
type EmailNotifier struct {
	config  *EmailConfig
	logger  logger.Logger
	metrics shared.Metrics
	name    string
	auth    smtp.Auth
	addr    string
}

// EmailConfig contains configuration for email notifications
type EmailConfig struct {
	SMTPHost           string            `yaml:"smtp_host" json:"smtp_host"`
	SMTPPort           int               `yaml:"smtp_port" json:"smtp_port"`
	Username           string            `yaml:"username" json:"username"`
	Password           string            `yaml:"password" json:"password"`
	From               string            `yaml:"from" json:"from"`
	To                 []string          `yaml:"to" json:"to"`
	CC                 []string          `yaml:"cc" json:"cc"`
	BCC                []string          `yaml:"bcc" json:"bcc"`
	Subject            string            `yaml:"subject" json:"subject"`
	SubjectTemplate    string            `yaml:"subject_template" json:"subject_template"`
	BodyTemplate       string            `yaml:"body_template" json:"body_template"`
	HTMLTemplate       string            `yaml:"html_template" json:"html_template"`
	UseTLS             bool              `yaml:"use_tls" json:"use_tls"`
	UseStartTLS        bool              `yaml:"use_starttls" json:"use_starttls"`
	InsecureSkipVerify bool              `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
	Timeout            time.Duration     `yaml:"timeout" json:"timeout"`
	Headers            map[string]string `yaml:"headers" json:"headers"`
	AuthType           string            `yaml:"auth_type" json:"auth_type"` // plain, login, crammd5
}

// DefaultEmailConfig returns default configuration for email notifications
func DefaultEmailConfig() *EmailConfig {
	return &EmailConfig{
		SMTPHost:        "localhost",
		SMTPPort:        587,
		Subject:         "Health Alert: {{.Title}}",
		UseTLS:          false,
		UseStartTLS:     true,
		Timeout:         30 * time.Second,
		Headers:         make(map[string]string),
		AuthType:        "plain",
		SubjectTemplate: "Health Alert: {{.Title}}",
		BodyTemplate: `
Health Alert Notification

Alert ID: {{.ID}}
Type: {{.Type}}
Severity: {{.Severity}}
Service: {{.Service}}
Title: {{.Title}}
Message: {{.Message}}
Timestamp: {{.Timestamp}}
Resolved: {{.Resolved}}

{{if .Details}}
Details:
{{range $key, $value := .Details}}
- {{$key}}: {{$value}}
{{end}}
{{end}}

{{if .Tags}}
Tags:
{{range $key, $value := .Tags}}
- {{$key}}: {{$value}}
{{end}}
{{end}}

--
Forge Health Alerting System
`,
		HTMLTemplate: `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Health Alert: {{.Title}}</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .alert { border: 1px solid #ddd; border-radius: 8px; padding: 20px; margin: 20px 0; }
        .alert.critical { border-color: #dc3545; background-color: #f8d7da; }
        .alert.error { border-color: #dc3545; background-color: #f8d7da; }
        .alert.warning { border-color: #ffc107; background-color: #fff3cd; }
        .alert.info { border-color: #17a2b8; background-color: #d1ecf1; }
        .header { border-bottom: 1px solid #ddd; padding-bottom: 10px; margin-bottom: 15px; }
        .title { font-size: 24px; font-weight: bold; margin: 0; }
        .severity { display: inline-block; padding: 4px 8px; border-radius: 4px; font-weight: bold; text-transform: uppercase; }
        .severity.critical { background-color: #dc3545; color: white; }
        .severity.error { background-color: #dc3545; color: white; }
        .severity.warning { background-color: #ffc107; color: #212529; }
        .severity.info { background-color: #17a2b8; color: white; }
        .field { margin: 10px 0; }
        .field-label { font-weight: bold; display: inline-block; width: 120px; }
        .details { margin: 15px 0; }
        .details-table { width: 100%; border-collapse: collapse; margin: 10px 0; }
        .details-table th, .details-table td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        .details-table th { background-color: #f8f9fa; }
        .footer { margin-top: 30px; padding-top: 20px; border-top: 1px solid #ddd; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="alert {{.Severity}}">
        <div class="header">
            <h1 class="title">{{.Title}}</h1>
            <span class="severity {{.Severity}}">{{.Severity}}</span>
        </div>
        
        <div class="field">
            <span class="field-label">Service:</span>
            <span>{{.Service}}</span>
        </div>
        
        <div class="field">
            <span class="field-label">Message:</span>
            <span>{{.Message}}</span>
        </div>
        
        <div class="field">
            <span class="field-label">Timestamp:</span>
            <span>{{.Timestamp}}</span>
        </div>
        
        <div class="field">
            <span class="field-label">Alert ID:</span>
            <span>{{.ID}}</span>
        </div>
        
        <div class="field">
            <span class="field-label">Status:</span>
            <span>{{if .Resolved}}Resolved{{else}}Active{{end}}</span>
        </div>
        
        {{if .Details}}
        <div class="details">
            <h3>Details</h3>
            <table class="details-table">
                <thead>
                    <tr>
                        <th>Field</th>
                        <th>Value</th>
                    </tr>
                </thead>
                <tbody>
                    {{range $key, $value := .Details}}
                    <tr>
                        <td>{{$key}}</td>
                        <td>{{$value}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
        {{end}}
        
        {{if .Tags}}
        <div class="details">
            <h3>Tags</h3>
            <table class="details-table">
                <thead>
                    <tr>
                        <th>Key</th>
                        <th>Value</th>
                    </tr>
                </thead>
                <tbody>
                    {{range $key, $value := .Tags}}
                    <tr>
                        <td>{{$key}}</td>
                        <td>{{$value}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
        {{end}}
    </div>
    
    <div class="footer">
        This alert was generated by Forge Health Alerting System at {{.Timestamp}}.
    </div>
</body>
</html>
`,
	}
}

// NewEmailNotifier creates a new email notifier
func NewEmailNotifier(name string, config *EmailConfig, logger logger.Logger, metrics shared.Metrics) (*EmailNotifier, error) {
	if config == nil {
		config = DefaultEmailConfig()
	}

	// Validate required fields
	if config.SMTPHost == "" {
		return nil, fmt.Errorf("SMTP host is required")
	}
	if config.From == "" {
		return nil, fmt.Errorf("from address is required")
	}
	if len(config.To) == 0 {
		return nil, fmt.Errorf("at least one recipient is required")
	}

	// Create SMTP authentication
	var auth smtp.Auth
	if config.Username != "" && config.Password != "" {
		switch config.AuthType {
		case "plain":
			auth = smtp.PlainAuth("", config.Username, config.Password, config.SMTPHost)
		case "login":
			auth = &loginAuth{config.Username, config.Password}
		case "crammd5":
			auth = smtp.CRAMMD5Auth(config.Username, config.Password)
		default:
			auth = smtp.PlainAuth("", config.Username, config.Password, config.SMTPHost)
		}
	}

	addr := fmt.Sprintf("%s:%d", config.SMTPHost, config.SMTPPort)

	return &EmailNotifier{
		config:  config,
		logger:  logger,
		metrics: metrics,
		name:    name,
		auth:    auth,
		addr:    addr,
	}, nil
}

// Name returns the name of the notifier
func (en *EmailNotifier) Name() string {
	return en.name
}

// Send sends an alert via email
func (en *EmailNotifier) Send(ctx context.Context, alert *Alert) error {
	// Create email message
	msg, err := en.createMessage(alert)
	if err != nil {
		return fmt.Errorf("failed to create email message: %w", err)
	}

	// Get recipients
	recipients := en.getAllRecipients()

	// Send email
	start := time.Now()
	err = en.sendEmail(ctx, recipients, msg)
	duration := time.Since(start)

	// Record metrics
	if en.metrics != nil {
		en.metrics.Counter("forge.health.email_requests").Inc()
		en.metrics.Histogram("forge.health.email_duration").Observe(duration.Seconds())
	}

	if err != nil {
		if en.metrics != nil {
			en.metrics.Counter("forge.health.email_errors").Inc()
		}
		return fmt.Errorf("failed to send email: %w", err)
	}

	if en.metrics != nil {
		en.metrics.Counter("forge.health.email_success").Inc()
	}

	if en.logger != nil {
		en.logger.Info("email alert sent successfully",
			logger.String("alert_id", alert.ID),
			logger.String("recipients", strings.Join(recipients, ", ")),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// SendBatch sends multiple alerts in a single email
func (en *EmailNotifier) SendBatch(ctx context.Context, alerts []*Alert) error {
	if len(alerts) == 0 {
		return nil
	}

	// Create batch email message
	msg, err := en.createBatchMessage(alerts)
	if err != nil {
		return fmt.Errorf("failed to create batch email message: %w", err)
	}

	// Get recipients
	recipients := en.getAllRecipients()

	// Send email
	start := time.Now()
	err = en.sendEmail(ctx, recipients, msg)
	duration := time.Since(start)

	// Record metrics
	if en.metrics != nil {
		en.metrics.Counter("forge.health.email_batch_requests").Inc()
		en.metrics.Histogram("forge.health.email_batch_duration").Observe(duration.Seconds())
	}

	if err != nil {
		if en.metrics != nil {
			en.metrics.Counter("forge.health.email_batch_errors").Inc()
		}
		return fmt.Errorf("failed to send batch email: %w", err)
	}

	if en.metrics != nil {
		en.metrics.Counter("forge.health.email_batch_success").Inc()
	}

	if en.logger != nil {
		en.logger.Info("batch email alert sent successfully",
			logger.Int("alert_count", len(alerts)),
			logger.String("recipients", strings.Join(recipients, ", ")),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// Test sends a test email
func (en *EmailNotifier) Test(ctx context.Context) error {
	testAlert := NewAlert(AlertTypeSystemError, AlertSeverityInfo, "Test Alert", "This is a test alert from Forge Health Alerting")
	testAlert.WithSource("test")
	testAlert.WithService("test-service")
	testAlert.WithDetail("test", true)

	return en.Send(ctx, testAlert)
}

// Close closes the email notifier
func (en *EmailNotifier) Close() error {
	// No resources to close for email notifier
	return nil
}

// createMessage creates an email message for a single alert
func (en *EmailNotifier) createMessage(alert *Alert) ([]byte, error) {
	var msg strings.Builder

	// Email headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", en.config.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(en.config.To, ", ")))

	if len(en.config.CC) > 0 {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(en.config.CC, ", ")))
	}

	if len(en.config.BCC) > 0 {
		msg.WriteString(fmt.Sprintf("Bcc: %s\r\n", strings.Join(en.config.BCC, ", ")))
	}

	// Subject
	subject, err := en.renderTemplate(en.config.SubjectTemplate, alert)
	if err != nil {
		subject = en.config.Subject
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))

	// Custom headers
	for key, value := range en.config.Headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}

	// MIME headers
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: multipart/alternative; boundary=\"boundary123\"\r\n")
	msg.WriteString("\r\n")

	// Text part
	msg.WriteString("--boundary123\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")

	textBody, err := en.renderTemplate(en.config.BodyTemplate, alert)
	if err != nil {
		textBody = fmt.Sprintf("Alert: %s\nService: %s\nMessage: %s\nTimestamp: %s",
			alert.Title, alert.Service, alert.Message, alert.Timestamp.Format(time.RFC3339))
	}
	msg.WriteString(textBody)
	msg.WriteString("\r\n")

	// HTML part
	msg.WriteString("--boundary123\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")

	htmlBody, err := en.renderTemplate(en.config.HTMLTemplate, alert)
	if err != nil {
		htmlBody = fmt.Sprintf("<h1>%s</h1><p><strong>Service:</strong> %s</p><p><strong>Message:</strong> %s</p><p><strong>Timestamp:</strong> %s</p>",
			alert.Title, alert.Service, alert.Message, alert.Timestamp.Format(time.RFC3339))
	}
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n")

	// End boundary
	msg.WriteString("--boundary123--\r\n")

	return []byte(msg.String()), nil
}

// createBatchMessage creates an email message for multiple alerts
func (en *EmailNotifier) createBatchMessage(alerts []*Alert) ([]byte, error) {
	var msg strings.Builder

	// Email headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", en.config.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(en.config.To, ", ")))

	if len(en.config.CC) > 0 {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(en.config.CC, ", ")))
	}

	// Subject for batch
	subject := fmt.Sprintf("Health Alert Batch: %d alerts", len(alerts))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))

	// Custom headers
	for key, value := range en.config.Headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
	}

	// MIME headers
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: multipart/alternative; boundary=\"boundary123\"\r\n")
	msg.WriteString("\r\n")

	// Text part
	msg.WriteString("--boundary123\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")

	msg.WriteString(fmt.Sprintf("Health Alert Batch Notification\n\n"))
	msg.WriteString(fmt.Sprintf("Total alerts: %d\n", len(alerts)))
	msg.WriteString(fmt.Sprintf("Timestamp: %s\n\n", time.Now().Format(time.RFC3339)))

	for i, alert := range alerts {
		msg.WriteString(fmt.Sprintf("Alert %d:\n", i+1))
		msg.WriteString(fmt.Sprintf("  ID: %s\n", alert.ID))
		msg.WriteString(fmt.Sprintf("  Type: %s\n", alert.Type))
		msg.WriteString(fmt.Sprintf("  Severity: %s\n", alert.Severity))
		msg.WriteString(fmt.Sprintf("  Service: %s\n", alert.Service))
		msg.WriteString(fmt.Sprintf("  Title: %s\n", alert.Title))
		msg.WriteString(fmt.Sprintf("  Message: %s\n", alert.Message))
		msg.WriteString(fmt.Sprintf("  Timestamp: %s\n", alert.Timestamp.Format(time.RFC3339)))
		msg.WriteString(fmt.Sprintf("  Resolved: %t\n\n", alert.Resolved))
	}

	msg.WriteString("--\nForge Health Alerting System\n")
	msg.WriteString("\r\n")

	// HTML part
	msg.WriteString("--boundary123\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")

	msg.WriteString(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Health Alert Batch</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .batch-header { border-bottom: 2px solid #ddd; padding-bottom: 15px; margin-bottom: 20px; }
        .alert { border: 1px solid #ddd; border-radius: 8px; padding: 15px; margin: 15px 0; }
        .alert.critical { border-color: #dc3545; background-color: #f8d7da; }
        .alert.error { border-color: #dc3545; background-color: #f8d7da; }
        .alert.warning { border-color: #ffc107; background-color: #fff3cd; }
        .alert.info { border-color: #17a2b8; background-color: #d1ecf1; }
        .alert-title { font-size: 18px; font-weight: bold; margin-bottom: 10px; }
        .alert-field { margin: 5px 0; }
        .field-label { font-weight: bold; display: inline-block; width: 100px; }
        .footer { margin-top: 30px; padding-top: 20px; border-top: 1px solid #ddd; color: #666; font-size: 12px; }
    </style>
</head>
<body>
    <div class="batch-header">
        <h1>Health Alert Batch Notification</h1>
        <p><strong>Total alerts:</strong> ` + fmt.Sprintf("%d", len(alerts)) + `</p>
        <p><strong>Timestamp:</strong> ` + time.Now().Format(time.RFC3339) + `</p>
    </div>`)

	for i, alert := range alerts {
		msg.WriteString(fmt.Sprintf(`
    <div class="alert %s">
        <div class="alert-title">Alert %d: %s</div>
        <div class="alert-field">
            <span class="field-label">Service:</span>
            <span>%s</span>
        </div>
        <div class="alert-field">
            <span class="field-label">Severity:</span>
            <span>%s</span>
        </div>
        <div class="alert-field">
            <span class="field-label">Message:</span>
            <span>%s</span>
        </div>
        <div class="alert-field">
            <span class="field-label">Timestamp:</span>
            <span>%s</span>
        </div>
        <div class="alert-field">
            <span class="field-label">Status:</span>
            <span>%s</span>
        </div>
    </div>`,
			alert.Severity, i+1, alert.Title, alert.Service, alert.Severity,
			alert.Message, alert.Timestamp.Format(time.RFC3339),
			func() string {
				if alert.Resolved {
					return "Resolved"
				} else {
					return "Active"
				}
			}()))
	}

	msg.WriteString(`
    <div class="footer">
        This batch alert was generated by Forge Health Alerting System.
    </div>
</body>
</html>`)
	msg.WriteString("\r\n")

	// End boundary
	msg.WriteString("--boundary123--\r\n")

	return []byte(msg.String()), nil
}

// sendEmail sends the email using SMTP
func (en *EmailNotifier) sendEmail(ctx context.Context, recipients []string, msg []byte) error {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, en.config.Timeout)
	defer cancel()

	// Connect to SMTP server
	var client *smtp.Client
	var err error

	if en.config.UseTLS {
		// Direct TLS connection
		// nolint:gosec // G402: InsecureSkipVerify is user-configurable for testing environments
		tlsConfig := &tls.Config{
			ServerName:         en.config.SMTPHost,
			InsecureSkipVerify: en.config.InsecureSkipVerify,
		}
		fmt.Println(tlsConfig)

		// In a real implementation, you would use crypto/tls to dial
		// For now, fall back to regular connection
		client, err = smtp.Dial(en.addr)
	} else {
		// Regular connection
		client, err = smtp.Dial(en.addr)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer client.Close()

	// Start TLS if configured
	if en.config.UseStartTLS {
		// nolint:gosec // G402: InsecureSkipVerify is user-configurable for testing environments
		tlsConfig := &tls.Config{
			ServerName:         en.config.SMTPHost,
			InsecureSkipVerify: en.config.InsecureSkipVerify,
		}

		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to start TLS: %w", err)
		}
	}

	// Authenticate if configured
	if en.auth != nil {
		if err := client.Auth(en.auth); err != nil {
			return fmt.Errorf("failed to authenticate: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(en.config.From); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// Set recipients
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
		}
	}

	// Send message
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to get data writer: %w", err)
	}

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	// Quit
	if err := client.Quit(); err != nil {
		return fmt.Errorf("failed to quit SMTP session: %w", err)
	}

	return nil
}

// renderTemplate renders a template with alert data
func (en *EmailNotifier) renderTemplate(templateStr string, alert *Alert) (string, error) {
	tmpl, err := template.New("email").Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, alert); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// getAllRecipients returns all recipients (To, CC, BCC)
func (en *EmailNotifier) getAllRecipients() []string {
	recipients := make([]string, 0)
	recipients = append(recipients, en.config.To...)
	recipients = append(recipients, en.config.CC...)
	recipients = append(recipients, en.config.BCC...)
	return recipients
}

// loginAuth implements LOGIN authentication
type loginAuth struct {
	username, password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte{}, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch string(fromServer) {
		case "Username:":
			return []byte(a.username), nil
		case "Password:":
			return []byte(a.password), nil
		default:
			return nil, fmt.Errorf("unknown challenge: %s", fromServer)
		}
	}
	return nil, nil
}

// EmailNotifierBuilder helps build email notifiers with fluent interface
type EmailNotifierBuilder struct {
	config  *EmailConfig
	logger  logger.Logger
	name    string
	metrics shared.Metrics
}

// NewEmailNotifierBuilder creates a new email notifier builder
func NewEmailNotifierBuilder(name string) *EmailNotifierBuilder {
	return &EmailNotifierBuilder{
		config: DefaultEmailConfig(),
		name:   name,
	}
}

// WithSMTP sets SMTP server configuration
func (enb *EmailNotifierBuilder) WithSMTP(host string, port int) *EmailNotifierBuilder {
	enb.config.SMTPHost = host
	enb.config.SMTPPort = port
	return enb
}

// WithAuth sets authentication credentials
func (enb *EmailNotifierBuilder) WithAuth(username, password string) *EmailNotifierBuilder {
	enb.config.Username = username
	enb.config.Password = password
	return enb
}

// WithAuthType sets authentication type
func (enb *EmailNotifierBuilder) WithAuthType(authType string) *EmailNotifierBuilder {
	enb.config.AuthType = authType
	return enb
}

// WithFrom sets the sender address
func (enb *EmailNotifierBuilder) WithFrom(from string) *EmailNotifierBuilder {
	enb.config.From = from
	return enb
}

// WithTo sets the recipient addresses
func (enb *EmailNotifierBuilder) WithTo(to ...string) *EmailNotifierBuilder {
	enb.config.To = to
	return enb
}

// WithCC sets the CC addresses
func (enb *EmailNotifierBuilder) WithCC(cc ...string) *EmailNotifierBuilder {
	enb.config.CC = cc
	return enb
}

// WithBCC sets the BCC addresses
func (enb *EmailNotifierBuilder) WithBCC(bcc ...string) *EmailNotifierBuilder {
	enb.config.BCC = bcc
	return enb
}

// WithSubject sets the email subject
func (enb *EmailNotifierBuilder) WithSubject(subject string) *EmailNotifierBuilder {
	enb.config.Subject = subject
	return enb
}

// WithSubjectTemplate sets the subject template
func (enb *EmailNotifierBuilder) WithSubjectTemplate(template string) *EmailNotifierBuilder {
	enb.config.SubjectTemplate = template
	return enb
}

// WithBodyTemplate sets the body template
func (enb *EmailNotifierBuilder) WithBodyTemplate(template string) *EmailNotifierBuilder {
	enb.config.BodyTemplate = template
	return enb
}

// WithHTMLTemplate sets the HTML template
func (enb *EmailNotifierBuilder) WithHTMLTemplate(template string) *EmailNotifierBuilder {
	enb.config.HTMLTemplate = template
	return enb
}

// WithTLS sets TLS configuration
func (enb *EmailNotifierBuilder) WithTLS(useTLS bool) *EmailNotifierBuilder {
	enb.config.UseTLS = useTLS
	return enb
}

// WithStartTLS sets STARTTLS configuration
func (enb *EmailNotifierBuilder) WithStartTLS(useStartTLS bool) *EmailNotifierBuilder {
	enb.config.UseStartTLS = useStartTLS
	return enb
}

// WithInsecureSkipVerify sets TLS verification skip
func (enb *EmailNotifierBuilder) WithInsecureSkipVerify(skip bool) *EmailNotifierBuilder {
	enb.config.InsecureSkipVerify = skip
	return enb
}

// WithTimeout sets the timeout
func (enb *EmailNotifierBuilder) WithTimeout(timeout time.Duration) *EmailNotifierBuilder {
	enb.config.Timeout = timeout
	return enb
}

// WithHeaders sets custom headers
func (enb *EmailNotifierBuilder) WithHeaders(headers map[string]string) *EmailNotifierBuilder {
	enb.config.Headers = headers
	return enb
}

// WithHeader adds a custom header
func (enb *EmailNotifierBuilder) WithHeader(key, value string) *EmailNotifierBuilder {
	if enb.config.Headers == nil {
		enb.config.Headers = make(map[string]string)
	}
	enb.config.Headers[key] = value
	return enb
}

// WithLogger sets the logger
func (enb *EmailNotifierBuilder) WithLogger(logger logger.Logger) *EmailNotifierBuilder {
	enb.logger = logger
	return enb
}

// WithMetrics sets the metrics collector
func (enb *EmailNotifierBuilder) WithMetrics(metrics shared.Metrics) *EmailNotifierBuilder {
	enb.metrics = metrics
	return enb
}

// Build creates the email notifier
func (enb *EmailNotifierBuilder) Build() (*EmailNotifier, error) {
	return NewEmailNotifier(enb.name, enb.config, enb.logger, enb.metrics)
}

// Predefined email configurations

// NewGmailNotifier creates an email notifier configured for Gmail
func NewGmailNotifier(name, username, password, from string, to []string, logger logger.Logger, metrics shared.Metrics) (*EmailNotifier, error) {
	return NewEmailNotifierBuilder(name).
		WithSMTP("smtp.gmail.com", 587).
		WithAuth(username, password).
		WithAuthType("plain").
		WithFrom(from).
		WithTo(to...).
		WithStartTLS(true).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}

// NewOutlookNotifier creates an email notifier configured for Outlook
func NewOutlookNotifier(name, username, password, from string, to []string, logger logger.Logger, metrics shared.Metrics) (*EmailNotifier, error) {
	return NewEmailNotifierBuilder(name).
		WithSMTP("smtp-mail.outlook.com", 587).
		WithAuth(username, password).
		WithAuthType("plain").
		WithFrom(from).
		WithTo(to...).
		WithStartTLS(true).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}

// NewSMTPNotifier creates a generic SMTP email notifier
func NewSMTPNotifier(name, host string, port int, username, password, from string, to []string, logger logger.Logger, metrics shared.Metrics) (*EmailNotifier, error) {
	return NewEmailNotifierBuilder(name).
		WithSMTP(host, port).
		WithAuth(username, password).
		WithFrom(from).
		WithTo(to...).
		WithStartTLS(true).
		WithLogger(logger).
		WithMetrics(metrics).
		Build()
}
