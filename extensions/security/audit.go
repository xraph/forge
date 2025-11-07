package security

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge"
)

// AuditConfig holds audit logging configuration
type AuditConfig struct {
	// Enabled determines if audit logging is enabled
	Enabled bool

	// Level determines what events to log
	// Options: "all", "auth", "data", "admin"
	Level string

	// IncludePaths is a list of path patterns to audit (empty = all paths)
	// Example: ["/api/admin/*", "/api/users/*"]
	IncludePaths []string

	// ExcludePaths is a list of path patterns to exclude from auditing
	// Example: ["/health", "/metrics"]
	ExcludePaths []string

	// LogRequestBody logs the request body (be careful with sensitive data)
	LogRequestBody bool

	// LogResponseBody logs the response body (be careful with sensitive data)
	LogResponseBody bool

	// SensitiveHeaders are headers that should be redacted in logs
	SensitiveHeaders []string

	// SensitiveFields are JSON fields that should be redacted in logs
	SensitiveFields []string

	// MaxBodySize is the maximum body size to log (in bytes)
	// Default: 1024 (1KB)
	MaxBodySize int

	// AutoApplyMiddleware automatically applies audit middleware globally
	AutoApplyMiddleware bool

	// Handler is called for each audit log entry
	// If not set, uses the default logger
	Handler func(*AuditEntry)
}

// DefaultAuditConfig returns the default audit configuration
func DefaultAuditConfig() AuditConfig {
	return AuditConfig{
		Enabled:             true,
		Level:               "auth",
		IncludePaths:        []string{},
		ExcludePaths:        []string{"/health", "/metrics"},
		LogRequestBody:      false,
		LogResponseBody:     false,
		SensitiveHeaders:    []string{"Authorization", "Cookie", "X-API-Key"},
		SensitiveFields:     []string{"password", "token", "secret", "api_key", "credit_card"},
		MaxBodySize:         1024,
		AutoApplyMiddleware: false, // Default to false for backwards compatibility
	}
}

// AuditEntry represents a security audit log entry
type AuditEntry struct {
	// Timestamp of the event
	Timestamp time.Time `json:"timestamp"`

	// Event type (e.g., "auth.login", "auth.logout", "data.create", "data.update")
	EventType string `json:"event_type"`

	// Event category ("auth", "data", "admin", "security")
	Category string `json:"category"`

	// Result of the event ("success", "failure", "error")
	Result string `json:"result"`

	// User information
	UserID   string `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`

	// Request information
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Query     string            `json:"query,omitempty"`
	IPAddress string            `json:"ip_address"`
	UserAgent string            `json:"user_agent,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	SessionID string            `json:"session_id,omitempty"`

	// Request/Response data
	RequestBody  string `json:"request_body,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
	StatusCode   int    `json:"status_code,omitempty"`

	// Timing information
	Duration time.Duration `json:"duration,omitempty"`

	// Additional context
	Message  string                 `json:"message,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Error information
	Error string `json:"error,omitempty"`
}

// AuditLogger handles security audit logging
type AuditLogger struct {
	config AuditConfig
	logger forge.Logger
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(config AuditConfig, logger forge.Logger) *AuditLogger {
	if config.MaxBodySize == 0 {
		config.MaxBodySize = 1024
	}
	if len(config.SensitiveHeaders) == 0 {
		config.SensitiveHeaders = []string{"Authorization", "Cookie", "X-API-Key"}
	}
	if len(config.SensitiveFields) == 0 {
		config.SensitiveFields = []string{"password", "token", "secret", "api_key"}
	}

	return &AuditLogger{
		config: config,
		logger: logger,
	}
}

// Log writes an audit log entry
func (al *AuditLogger) Log(entry *AuditEntry) {
	if !al.config.Enabled {
		return
	}

	// Filter by level
	if !al.shouldLog(entry.Category) {
		return
	}

	// Set timestamp if not set
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	// Redact sensitive data
	al.redactSensitiveData(entry)

	// Use custom handler if provided
	if al.config.Handler != nil {
		al.config.Handler(entry)
		return
	}

	// Default: log using the forge logger
	al.logger.Info("audit",
		forge.F("event_type", entry.EventType),
		forge.F("category", entry.Category),
		forge.F("result", entry.Result),
		forge.F("user_id", entry.UserID),
		forge.F("method", entry.Method),
		forge.F("path", entry.Path),
		forge.F("ip_address", entry.IPAddress),
		forge.F("status_code", entry.StatusCode),
		forge.F("duration_ms", entry.Duration.Milliseconds()),
		forge.F("message", entry.Message),
	)
}

// shouldLog checks if an event should be logged based on level and category
func (al *AuditLogger) shouldLog(category string) bool {
	switch al.config.Level {
	case "all":
		return true
	case "auth":
		return category == "auth" || category == "security"
	case "data":
		return category == "data" || category == "auth" || category == "security"
	case "admin":
		return category == "admin" || category == "security"
	case "security":
		return category == "security"
	default:
		return true
	}
}

// shouldAuditPath checks if a path should be audited
func (al *AuditLogger) shouldAuditPath(path string) bool {
	// Check exclude paths first
	for _, excludePath := range al.config.ExcludePaths {
		if matchPath(path, excludePath) {
			return false
		}
	}

	// If no include paths specified, audit all (except excluded)
	if len(al.config.IncludePaths) == 0 {
		return true
	}

	// Check include paths
	for _, includePath := range al.config.IncludePaths {
		if matchPath(path, includePath) {
			return true
		}
	}

	return false
}

// matchPath checks if a path matches a pattern (supports * wildcard)
func matchPath(path, pattern string) bool {
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	return false
}

// redactSensitiveData redacts sensitive information from audit entry
func (al *AuditLogger) redactSensitiveData(entry *AuditEntry) {
	// Redact sensitive headers
	if entry.Headers != nil {
		for _, sensitiveHeader := range al.config.SensitiveHeaders {
			if _, exists := entry.Headers[sensitiveHeader]; exists {
				entry.Headers[sensitiveHeader] = "[REDACTED]"
			}
		}
	}

	// Redact sensitive fields in request/response bodies
	if entry.RequestBody != "" {
		entry.RequestBody = al.redactJSONFields(entry.RequestBody)
	}
	if entry.ResponseBody != "" {
		entry.ResponseBody = al.redactJSONFields(entry.ResponseBody)
	}
}

// redactJSONFields redacts sensitive fields in JSON
func (al *AuditLogger) redactJSONFields(jsonStr string) string {
	if jsonStr == "" {
		return jsonStr
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		// Not valid JSON, return as-is
		return jsonStr
	}

	// Redact sensitive fields
	for _, field := range al.config.SensitiveFields {
		if _, exists := data[field]; exists {
			data[field] = "[REDACTED]"
		}
	}

	// Marshal back to JSON
	redacted, err := json.Marshal(data)
	if err != nil {
		return jsonStr
	}

	return string(redacted)
}

// extractIPAddress extracts the client IP address from request
func extractIPAddress(r *http.Request) string {
	// Try X-Forwarded-For header (for proxied requests)
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		ips := strings.Split(ip, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Try X-Real-IP header
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// AuditMiddleware returns a middleware for audit logging
func AuditMiddleware(auditLogger *AuditLogger) forge.Middleware {
	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if !auditLogger.config.Enabled {
				return next(ctx)
			}

			r := ctx.Request()

			// Check if path should be audited
			if !auditLogger.shouldAuditPath(r.URL.Path) {
				return next(ctx)
			}

			start := time.Now()

			// Create audit entry
			entry := &AuditEntry{
				Timestamp: start,
				Method:    r.Method,
				Path:      r.URL.Path,
				Query:     r.URL.RawQuery,
				IPAddress: extractIPAddress(r),
				UserAgent: r.Header.Get("User-Agent"),
			}

			// Extract user information from session or JWT
			if session, ok := GetSession(ctx.Context()); ok {
				entry.UserID = session.UserID
				entry.SessionID = session.ID
			} else if claims, ok := GetJWTClaims(ctx); ok {
				entry.UserID = claims.UserID
				entry.Username = claims.Username
				entry.Email = claims.Email
			}

			// Execute handler
			err := next(ctx)

			// Calculate duration
			entry.Duration = time.Since(start)

			// Set result based on error
			if err != nil {
				entry.Result = "error"
				entry.Message = err.Error()
			} else {
				entry.Result = "success"
			}
			entry.Category = "data"
			entry.EventType = "request." + entry.Result

			// Log the entry
			auditLogger.Log(entry)

			return err
		}
	}
}

// LogAuthEvent logs an authentication event
func LogAuthEvent(auditLogger *AuditLogger, ctx context.Context, eventType string, result string, userID string, metadata map[string]interface{}) {
	entry := &AuditEntry{
		EventType: "auth." + eventType,
		Category:  "auth",
		Result:    result,
		UserID:    userID,
		Metadata:  metadata,
	}
	auditLogger.Log(entry)
}

// LogDataEvent logs a data access/modification event
func LogDataEvent(auditLogger *AuditLogger, ctx context.Context, eventType string, result string, userID string, resource string, metadata map[string]interface{}) {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["resource"] = resource

	entry := &AuditEntry{
		EventType: "data." + eventType,
		Category:  "data",
		Result:    result,
		UserID:    userID,
		Metadata:  metadata,
	}
	auditLogger.Log(entry)
}

// LogSecurityEvent logs a security-related event
func LogSecurityEvent(auditLogger *AuditLogger, ctx context.Context, eventType string, result string, message string, metadata map[string]interface{}) {
	entry := &AuditEntry{
		EventType: "security." + eventType,
		Category:  "security",
		Result:    result,
		Message:   message,
		Metadata:  metadata,
	}
	auditLogger.Log(entry)
}

// LogAdminEvent logs an administrative action
func LogAdminEvent(auditLogger *AuditLogger, ctx context.Context, eventType string, result string, userID string, action string, metadata map[string]interface{}) {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["action"] = action

	entry := &AuditEntry{
		EventType: "admin." + eventType,
		Category:  "admin",
		Result:    result,
		UserID:    userID,
		Metadata:  metadata,
	}
	auditLogger.Log(entry)
}
