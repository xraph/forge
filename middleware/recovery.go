package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// RecoveryMiddleware handles panic recovery and error reporting
type RecoveryMiddleware struct {
	*BaseMiddleware
	config RecoveryConfig
}

// RecoveryConfig represents recovery middleware configuration
type RecoveryConfig struct {
	// Basic settings
	Enabled         bool `json:"enabled"`
	PrintStack      bool `json:"print_stack"`
	DisableStackAll bool `json:"disable_stack_all"`

	// Response settings
	ResponseFormat   ResponseFormat `json:"response_format"`
	IncludeStack     bool           `json:"include_stack"`
	IncludeRequest   bool           `json:"include_request"`
	ShowErrorDetails bool           `json:"show_error_details"`

	// Custom handlers
	RecoveryHandler func(http.ResponseWriter, *http.Request, interface{}) `json:"-"`
	ErrorHandler    func(error, *http.Request)                            `json:"-"`
	PanicHandler    func(interface{}, *http.Request)                      `json:"-"`

	// Stack filtering
	SkipFrames     int      `json:"skip_frames"`
	FilterPackages []string `json:"filter_packages"`

	// Development settings
	DevelopmentMode bool `json:"development_mode"`

	// Notification settings
	NotifyOnPanic bool                             `json:"notify_on_panic"`
	Notifier      func(interface{}, *http.Request) `json:"-"`
}

// ResponseFormat represents different response formats for recovery
type ResponseFormat string

const (
	ResponseFormatJSON ResponseFormat = "json"
	ResponseFormatText ResponseFormat = "text"
	ResponseFormatHTML ResponseFormat = "html"
)

// PanicInfo represents information about a panic
type PanicInfo struct {
	Error     interface{}            `json:"error"`
	Stack     string                 `json:"stack,omitempty"`
	Request   *RequestSnapshot       `json:"request,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// RequestSnapshot captures relevant request information
type RequestSnapshot struct {
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	Headers    http.Header `json:"headers,omitempty"`
	RemoteAddr string      `json:"remote_addr"`
	UserAgent  string      `json:"user_agent"`
	Body       string      `json:"body,omitempty"`
}

// NewRecoveryMiddleware creates a new recovery middleware
func NewRecoveryMiddleware(logger logger.Logger, config ...RecoveryConfig) Middleware {
	cfg := DefaultRecoveryConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	return &RecoveryMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"recovery",
			PriorityRecovery,
			"Panic recovery and error handling middleware",
		),
		config: cfg,
	}
}

// DefaultRecoveryConfig returns default recovery configuration
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		Enabled:          true,
		PrintStack:       true,
		DisableStackAll:  false,
		ResponseFormat:   ResponseFormatJSON,
		IncludeStack:     false, // Don't include stack in production by default
		IncludeRequest:   false,
		ShowErrorDetails: false,
		SkipFrames:       3, // Skip recovery middleware frames
		FilterPackages:   []string{"runtime", "net/http"},
		DevelopmentMode:  false,
		NotifyOnPanic:    false,
	}
}

// DevelopmentRecoveryConfig returns development-optimized recovery configuration
func DevelopmentRecoveryConfig() RecoveryConfig {
	config := DefaultRecoveryConfig()
	config.DevelopmentMode = true
	config.IncludeStack = true
	config.IncludeRequest = true
	config.ShowErrorDetails = true
	config.PrintStack = true
	return config
}

// ProductionRecoveryConfig returns production-optimized recovery configuration
func ProductionRecoveryConfig() RecoveryConfig {
	config := DefaultRecoveryConfig()
	config.DevelopmentMode = false
	config.IncludeStack = false
	config.IncludeRequest = false
	config.ShowErrorDetails = false
	config.PrintStack = false
	config.NotifyOnPanic = true
	return config
}

// Handle implements the Middleware interface
func (rm *RecoveryMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rm.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		defer func() {
			if rec := recover(); rec != nil {
				rm.handlePanic(w, r, rec)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// handlePanic handles a recovered panic
func (rm *RecoveryMiddleware) handlePanic(w http.ResponseWriter, r *http.Request, recovered interface{}) {
	// Get request ID from context
	requestID := router.GetRequestID(r.Context())

	// Create panic info
	panicInfo := &PanicInfo{
		Error:     recovered,
		Timestamp: time.Now(),
		RequestID: requestID,
		Metadata:  make(map[string]interface{}),
	}

	// Capture stack trace
	stack := debug.Stack()
	if rm.config.PrintStack || rm.config.IncludeStack {
		filteredStack := rm.filterStack(string(stack))
		panicInfo.Stack = filteredStack
	}

	// Capture request information
	if rm.config.IncludeRequest {
		panicInfo.Request = rm.captureRequest(r)
	}

	// Log the panic
	rm.logPanic(panicInfo)

	// Call custom panic handler if provided
	if rm.config.PanicHandler != nil {
		rm.config.PanicHandler(recovered, r)
	}

	// Send notification if enabled
	if rm.config.NotifyOnPanic && rm.config.Notifier != nil {
		rm.config.Notifier(recovered, r)
	}

	// Use custom recovery handler if provided
	if rm.config.RecoveryHandler != nil {
		rm.config.RecoveryHandler(w, r, recovered)
		return
	}

	// Default response handling
	rm.writeErrorResponse(w, r, panicInfo)
}

// logPanic logs the panic information
func (rm *RecoveryMiddleware) logPanic(panicInfo *PanicInfo) {
	fields := []logger.Field{
		logger.Any("panic", panicInfo.Error),
		logger.String("timestamp", panicInfo.Timestamp.Format(time.RFC3339)),
	}

	if panicInfo.RequestID != "" {
		fields = append(fields, logger.String("request_id", panicInfo.RequestID))
	}

	if panicInfo.Request != nil {
		fields = append(fields,
			logger.String("method", panicInfo.Request.Method),
			logger.String("url", panicInfo.Request.URL),
			logger.String("remote_addr", panicInfo.Request.RemoteAddr),
		)
	}

	if panicInfo.Stack != "" {
		if rm.config.PrintStack {
			fields = append(fields, logger.String("stack", panicInfo.Stack))
		}
	}

	rm.logger.Error("Panic recovered", fields...)
}

// captureRequest captures relevant request information
func (rm *RecoveryMiddleware) captureRequest(r *http.Request) *RequestSnapshot {
	snapshot := &RequestSnapshot{
		Method:     r.Method,
		URL:        r.URL.String(),
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	}

	// Include headers if configured
	if rm.config.DevelopmentMode || rm.config.IncludeRequest {
		snapshot.Headers = r.Header
	}

	// Capture request body if it's small enough and safe to read
	if r.ContentLength > 0 && r.ContentLength < 1024*1024 { // 1MB limit
		if r.Body != nil {
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(r.Body); err == nil {
				snapshot.Body = buf.String()
			}
		}
	}

	return snapshot
}

// filterStack filters the stack trace based on configuration
func (rm *RecoveryMiddleware) filterStack(stack string) string {
	if rm.config.DisableStackAll {
		return ""
	}

	lines := strings.Split(stack, "\n")
	filtered := make([]string, 0, len(lines))

	skipCount := 0
	for i, line := range lines {
		// Skip initial frames
		if skipCount < rm.config.SkipFrames {
			if strings.Contains(line, "runtime.panic") ||
				strings.Contains(line, "recovery.go") {
				skipCount++
				continue
			}
		}

		// Filter out unwanted packages
		shouldSkip := false
		for _, pkg := range rm.config.FilterPackages {
			if strings.Contains(line, pkg) {
				shouldSkip = true
				break
			}
		}

		if !shouldSkip {
			filtered = append(filtered, line)
		}

		// Include the next line if it's a file path
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "\t") {
			filtered = append(filtered, lines[i+1])
		}
	}

	return strings.Join(filtered, "\n")
}

// writeErrorResponse writes the error response to the client
func (rm *RecoveryMiddleware) writeErrorResponse(w http.ResponseWriter, r *http.Request, panicInfo *PanicInfo) {
	w.WriteHeader(http.StatusInternalServerError)

	switch rm.config.ResponseFormat {
	case ResponseFormatJSON:
		rm.writeJSONResponse(w, r, panicInfo)
	case ResponseFormatHTML:
		rm.writeHTMLResponse(w, r, panicInfo)
	default:
		rm.writeTextResponse(w, r, panicInfo)
	}
}

// writeJSONResponse writes a JSON error response
func (rm *RecoveryMiddleware) writeJSONResponse(w http.ResponseWriter, r *http.Request, panicInfo *PanicInfo) {
	w.Header().Set("Content-Type", "application/json")

	response := map[string]interface{}{
		"error":     "Internal Server Error",
		"message":   "An unexpected error occurred",
		"timestamp": panicInfo.Timestamp.Format(time.RFC3339),
		"status":    http.StatusInternalServerError,
	}

	if panicInfo.RequestID != "" {
		response["request_id"] = panicInfo.RequestID
	}

	if rm.config.ShowErrorDetails {
		response["panic"] = panicInfo.Error

		if rm.config.IncludeStack && panicInfo.Stack != "" {
			response["stack"] = panicInfo.Stack
		}

		if rm.config.IncludeRequest && panicInfo.Request != nil {
			response["request"] = panicInfo.Request
		}
	}

	json.NewEncoder(w).Encode(response)
}

// writeHTMLResponse writes an HTML error response
func (rm *RecoveryMiddleware) writeHTMLResponse(w http.ResponseWriter, r *http.Request, panicInfo *PanicInfo) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if rm.config.DevelopmentMode {
		rm.writeDetailedHTMLResponse(w, r, panicInfo)
	} else {
		rm.writeSimpleHTMLResponse(w, r, panicInfo)
	}
}

// writeDetailedHTMLResponse writes a detailed HTML error page for development
func (rm *RecoveryMiddleware) writeDetailedHTMLResponse(w http.ResponseWriter, r *http.Request, panicInfo *PanicInfo) {
	html := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>Panic Recovered</title>
    <style>
        body { font-family: 'Courier New', monospace; margin: 20px; }
        .error { color: #d32f2f; }
        .stack { background: #f5f5f5; padding: 10px; white-space: pre-wrap; }
        .request { background: #e3f2fd; padding: 10px; margin: 10px 0; }
        .timestamp { color: #666; }
    </style>
</head>
<body>
    <h1 class="error">Panic Recovered</h1>
    <p class="timestamp">Time: %s</p>
    <p class="error">Error: %v</p>
    %s
    %s
</body>
</html>`,
		panicInfo.Timestamp.Format(time.RFC3339),
		panicInfo.Error,
		rm.formatStackForHTML(panicInfo.Stack),
		rm.formatRequestForHTML(panicInfo.Request),
	)

	w.Write([]byte(html))
}

// writeSimpleHTMLResponse writes a simple HTML error page for production
func (rm *RecoveryMiddleware) writeSimpleHTMLResponse(w http.ResponseWriter, r *http.Request, panicInfo *PanicInfo) {
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>Internal Server Error</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; }
        .error { color: #d32f2f; }
    </style>
</head>
<body>
    <h1 class="error">Internal Server Error</h1>
    <p>An unexpected error occurred. Please try again later.</p>
</body>
</html>`

	w.Write([]byte(html))
}

// writeTextResponse writes a plain text error response
func (rm *RecoveryMiddleware) writeTextResponse(w http.ResponseWriter, r *http.Request, panicInfo *PanicInfo) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if rm.config.ShowErrorDetails {
		fmt.Fprintf(w, "Panic: %v\n", panicInfo.Error)
		fmt.Fprintf(w, "Time: %s\n", panicInfo.Timestamp.Format(time.RFC3339))

		if panicInfo.RequestID != "" {
			fmt.Fprintf(w, "Request ID: %s\n", panicInfo.RequestID)
		}

		if rm.config.IncludeStack && panicInfo.Stack != "" {
			fmt.Fprintf(w, "\nStack trace:\n%s", panicInfo.Stack)
		}
	} else {
		fmt.Fprint(w, "Internal Server Error")
	}
}

// formatStackForHTML formats stack trace for HTML display
func (rm *RecoveryMiddleware) formatStackForHTML(stack string) string {
	if stack == "" {
		return ""
	}
	return fmt.Sprintf(`
    <h3>Stack Trace</h3>
    <div class="stack">%s</div>
    `, stack)
}

// formatRequestForHTML formats request info for HTML display
func (rm *RecoveryMiddleware) formatRequestForHTML(req *RequestSnapshot) string {
	if req == nil {
		return ""
	}

	return fmt.Sprintf(`
    <h3>Request Information</h3>
    <div class="request">
        <p><strong>Method:</strong> %s</p>
        <p><strong>URL:</strong> %s</p>
        <p><strong>Remote Address:</strong> %s</p>
        <p><strong>User Agent:</strong> %s</p>
    </div>
    `, req.Method, req.URL, req.RemoteAddr, req.UserAgent)
}

// Configure implements the Middleware interface
func (rm *RecoveryMiddleware) Configure(config map[string]interface{}) error {
	if enabled, ok := config["enabled"].(bool); ok {
		rm.config.Enabled = enabled
	}
	if printStack, ok := config["print_stack"].(bool); ok {
		rm.config.PrintStack = printStack
	}
	if includeStack, ok := config["include_stack"].(bool); ok {
		rm.config.IncludeStack = includeStack
	}
	if includeRequest, ok := config["include_request"].(bool); ok {
		rm.config.IncludeRequest = includeRequest
	}
	if showErrorDetails, ok := config["show_error_details"].(bool); ok {
		rm.config.ShowErrorDetails = showErrorDetails
	}
	if developmentMode, ok := config["development_mode"].(bool); ok {
		rm.config.DevelopmentMode = developmentMode
	}
	if responseFormat, ok := config["response_format"].(string); ok {
		rm.config.ResponseFormat = ResponseFormat(responseFormat)
	}
	if skipFrames, ok := config["skip_frames"].(int); ok {
		rm.config.SkipFrames = skipFrames
	}
	if filterPackages, ok := config["filter_packages"].([]string); ok {
		rm.config.FilterPackages = filterPackages
	}
	if notifyOnPanic, ok := config["notify_on_panic"].(bool); ok {
		rm.config.NotifyOnPanic = notifyOnPanic
	}

	return nil
}

// Health implements the Middleware interface
func (rm *RecoveryMiddleware) Health(ctx context.Context) error {
	if !rm.config.Enabled {
		return fmt.Errorf("recovery middleware is disabled")
	}
	return nil
}

// Utility functions

// RecoveryMiddlewareFunc creates a simple recovery middleware function
func RecoveryMiddlewareFunc(logger logger.Logger) func(http.Handler) http.Handler {
	middleware := NewRecoveryMiddleware(logger)
	return middleware.Handle
}

// WithRecoveryConfig creates a recovery middleware with specific configuration
func WithRecoveryConfig(logger logger.Logger, config RecoveryConfig) func(http.Handler) http.Handler {
	middleware := NewRecoveryMiddleware(logger, config)
	return middleware.Handle
}

// WithDevelopmentRecovery creates a recovery middleware optimized for development
func WithDevelopmentRecovery(logger logger.Logger) func(http.Handler) http.Handler {
	config := DevelopmentRecoveryConfig()
	return WithRecoveryConfig(logger, config)
}

// WithProductionRecovery creates a recovery middleware optimized for production
func WithProductionRecovery(logger logger.Logger) func(http.Handler) http.Handler {
	config := ProductionRecoveryConfig()
	return WithRecoveryConfig(logger, config)
}

// PanicNotifier represents a function that can send notifications about panics
type PanicNotifier func(interface{}, *http.Request)

// EmailNotifier creates a notifier that sends emails on panic
func EmailNotifier(smtpHost, from, to string) PanicNotifier {
	return func(recovered interface{}, r *http.Request) {
		// Implementation would send email notification
		// This is a placeholder
	}
}

// SlackNotifier creates a notifier that sends Slack messages on panic
func SlackNotifier(webhookURL string) PanicNotifier {
	return func(recovered interface{}, r *http.Request) {
		// Implementation would send Slack notification
		// This is a placeholder
	}
}

// WebhookNotifier creates a notifier that sends webhook notifications on panic
func WebhookNotifier(webhookURL string) PanicNotifier {
	return func(recovered interface{}, r *http.Request) {
		// Implementation would send webhook notification
		// This is a placeholder
	}
}
