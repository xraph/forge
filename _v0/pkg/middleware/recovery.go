package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Config contains configuration for recovery middleware
type Config struct {
	// Recovery behavior
	EnableStackTrace bool   `yaml:"enable_stack_trace" json:"enable_stack_trace" default:"true"`
	IncludeRequest   bool   `yaml:"include_request" json:"include_request" default:"true"`
	LogPanics        bool   `yaml:"log_panics" json:"log_panics" default:"true"`
	LogLevel         string `yaml:"log_level" json:"log_level" default:"error"`

	// Response configuration
	ErrorMessage    string `yaml:"error_message" json:"error_message" default:"Internal Server Error"`
	ErrorCode       string `yaml:"error_code" json:"error_code" default:"INTERNAL_SERVER_ERROR"`
	IncludeDetails  bool   `yaml:"include_details" json:"include_details" default:"false"`
	DevelopmentMode bool   `yaml:"development_mode" json:"development_mode" default:"false"`

	// Notification
	EnableNotifications bool   `yaml:"enable_notifications" json:"enable_notifications" default:"false"`
	NotificationURL     string `yaml:"notification_url" json:"notification_url"`

	// Filtering
	IgnorePatterns []string `yaml:"ignore_patterns" json:"ignore_patterns"`
	SkipPaths      []string `yaml:"skip_paths" json:"skip_paths"`

	// Rate limiting for panic notifications
	MaxPanicsPerMinute int           `yaml:"max_panics_per_minute" json:"max_panics_per_minute" default:"5"`
	NotificationWindow time.Duration `yaml:"notification_window" json:"notification_window" default:"1m"`
}

// RecoveryMiddleware implements enhanced panic recovery middleware
type RecoveryMiddleware struct {
	*BaseServiceMiddleware
	config       Config
	panicCounter *PanicCounter
	notifier     *PanicNotifier
}

// PanicInfo contains information about a panic
type PanicInfo struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Error       string                 `json:"error"`
	StackTrace  string                 `json:"stack_trace,omitempty"`
	RequestInfo *RequestInfo           `json:"request_info,omitempty"`
	ServerInfo  *ServerInfo            `json:"server_info"`
	Recovery    bool                   `json:"recovery"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// RequestInfo contains information about the request that caused the panic
type RequestInfo struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Path        string            `json:"path"`
	RemoteAddr  string            `json:"remote_addr"`
	UserAgent   string            `json:"user_agent"`
	Headers     map[string]string `json:"headers,omitempty"`
	QueryParams map[string]string `json:"query_params,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Body        string            `json:"body,omitempty"`
}

// ServerInfo contains information about the server state
type ServerInfo struct {
	Hostname    string    `json:"hostname"`
	Version     string    `json:"version"`
	GoVersion   string    `json:"go_version"`
	Goroutines  int       `json:"goroutines"`
	MemoryUsage int64     `json:"memory_usage"`
	Uptime      string    `json:"uptime"`
	Timestamp   time.Time `json:"timestamp"`
}

// PanicCounter tracks panic frequency for rate limiting
type PanicCounter struct {
	counts map[string]*windowCount
	config Config
}

type windowCount struct {
	count     int
	firstSeen time.Time
}

// PanicNotifier handles panic notifications
type PanicNotifier struct {
	config Config
	client *http.Client
}

// NewRecoveryMiddleware creates a new recovery middleware
func NewRecoveryMiddleware(config Config) *RecoveryMiddleware {
	return &RecoveryMiddleware{
		BaseServiceMiddleware: NewBaseServiceMiddleware("recovery", 1, []string{"config-manager"}), // Highest priority
		config:                config,
		panicCounter:          NewPanicCounter(config),
		notifier:              NewPanicNotifier(config),
	}
}

// Initialize initializes the recovery middleware
func (rm *RecoveryMiddleware) Initialize(container common.Container) error {
	if err := rm.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// Load configuration from container if needed
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		var recoveryConfig Config
		if err := configManager.(common.ConfigManager).Bind("recovery", &recoveryConfig); err == nil {
			rm.config = recoveryConfig
		}
	}

	// Set defaults
	if rm.config.ErrorMessage == "" {
		rm.config.ErrorMessage = "Internal Server Error"
	}
	if rm.config.ErrorCode == "" {
		rm.config.ErrorCode = "INTERNAL_SERVER_ERROR"
	}
	if rm.config.LogLevel == "" {
		rm.config.LogLevel = "error"
	}
	if rm.config.MaxPanicsPerMinute == 0 {
		rm.config.MaxPanicsPerMinute = 5
	}
	if rm.config.NotificationWindow == 0 {
		rm.config.NotificationWindow = time.Minute
	}

	return nil
}

// Handler returns the recovery handler
func (rm *RecoveryMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			rm.UpdateStats(1, 0, 0, nil)

			// Check if path should be skipped
			if rm.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				rm.UpdateStats(0, 0, time.Since(start), nil)
				return
			}

			// Set up panic recovery
			defer func() {
				if recovered := recover(); recovered != nil {
					duration := time.Since(start)

					// Create panic info
					panicInfo := rm.createPanicInfo(recovered, r)

					// Log the panic
					if rm.config.LogPanics {
						rm.logPanic(panicInfo)
					}

					// Send notifications if enabled and not rate limited
					if rm.config.EnableNotifications && !rm.panicCounter.IsRateLimited(panicInfo.ID) {
						go rm.notifier.SendNotification(panicInfo)
					}

					// Record panic in counter
					rm.panicCounter.RecordPanic(panicInfo.ID)

					// Update middleware statistics
					err := fmt.Errorf("panic recovered: %v", recovered)
					rm.UpdateStats(0, 1, duration, err)

					// Write error response
					rm.writeErrorResponse(w, r, panicInfo)
				}
			}()

			// Execute the request
			next.ServeHTTP(w, r)

			// Update successful request statistics
			rm.UpdateStats(0, 0, time.Since(start), nil)
		})
	}
}

// createPanicInfo creates detailed panic information
func (rm *RecoveryMiddleware) createPanicInfo(recovered interface{}, r *http.Request) *PanicInfo {
	panicInfo := &PanicInfo{
		ID:        fmt.Sprintf("panic-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Error:     fmt.Sprintf("%v", recovered),
		Recovery:  true,
		Metadata:  make(map[string]interface{}),
	}

	// Add stack trace if enabled
	if rm.config.EnableStackTrace {
		panicInfo.StackTrace = string(debug.Stack())
	}

	// Add request information if enabled
	if rm.config.IncludeRequest {
		panicInfo.RequestInfo = rm.extractRequestInfo(r)
	}

	// Add server information
	panicInfo.ServerInfo = rm.extractServerInfo()

	// Add runtime information
	panicInfo.Metadata["goroutines"] = runtime.NumGoroutine()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	panicInfo.Metadata["memory_alloc"] = memStats.Alloc
	panicInfo.Metadata["memory_sys"] = memStats.Sys
	panicInfo.Metadata["gc_count"] = memStats.NumGC

	return panicInfo
}

// extractRequestInfo extracts relevant request information
func (rm *RecoveryMiddleware) extractRequestInfo(r *http.Request) *RequestInfo {
	requestInfo := &RequestInfo{
		Method:     r.Method,
		URL:        r.URL.String(),
		Path:       r.URL.Path,
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	}

	// Extract content type
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		requestInfo.ContentType = contentType
	}

	// Extract headers (filtered for security)
	if rm.config.DevelopmentMode {
		requestInfo.Headers = make(map[string]string)
		for name, values := range r.Header {
			// Skip sensitive headers
			if rm.isSensitiveHeader(name) {
				continue
			}
			if len(values) > 0 {
				requestInfo.Headers[name] = values[0]
			}
		}
	}

	// Extract query parameters
	if len(r.URL.Query()) > 0 {
		requestInfo.QueryParams = make(map[string]string)
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				requestInfo.QueryParams[key] = values[0]
			}
		}
	}

	return requestInfo
}

// extractServerInfo extracts server information
func (rm *RecoveryMiddleware) extractServerInfo() *ServerInfo {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return &ServerInfo{
		GoVersion:   runtime.Version(),
		Goroutines:  runtime.NumGoroutine(),
		MemoryUsage: int64(memStats.Alloc),
		Timestamp:   time.Now(),
	}
}

// isSensitiveHeader checks if a header contains sensitive information
func (rm *RecoveryMiddleware) isSensitiveHeader(name string) bool {
	sensitiveHeaders := []string{
		"authorization", "cookie", "x-api-key", "x-auth-token",
		"x-access-token", "x-csrf-token", "x-forwarded-for",
	}

	name = strings.ToLower(name)
	for _, sensitive := range sensitiveHeaders {
		if strings.Contains(name, sensitive) {
			return true
		}
	}

	return false
}

// shouldSkipPath checks if recovery should be skipped for this path
func (rm *RecoveryMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range rm.config.SkipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// shouldIgnorePanic checks if this panic should be ignored based on patterns
func (rm *RecoveryMiddleware) shouldIgnorePanic(panicMsg string) bool {
	for _, pattern := range rm.config.IgnorePatterns {
		if strings.Contains(panicMsg, pattern) {
			return true
		}
	}
	return false
}

// logPanic logs the panic information
func (rm *RecoveryMiddleware) logPanic(panicInfo *PanicInfo) {
	if rm.logger == nil {
		return
	}

	// Check if this panic should be ignored
	if rm.shouldIgnorePanic(panicInfo.Error) {
		return
	}

	fields := []logger.Field{
		logger.String("panic_id", panicInfo.ID),
		logger.String("panic_error", panicInfo.Error),
		logger.Time("timestamp", panicInfo.Timestamp),
		logger.Bool("recovered", panicInfo.Recovery),
	}

	// Add request information
	if panicInfo.RequestInfo != nil {
		fields = append(fields,
			logger.String("method", panicInfo.RequestInfo.Method),
			logger.String("path", panicInfo.RequestInfo.Path),
			logger.String("remote_addr", panicInfo.RequestInfo.RemoteAddr),
			logger.String("user_agent", panicInfo.RequestInfo.UserAgent),
		)
	}

	// Add server information
	if panicInfo.ServerInfo != nil {
		fields = append(fields,
			logger.Int("goroutines", panicInfo.ServerInfo.Goroutines),
			logger.Int64("memory_usage", panicInfo.ServerInfo.MemoryUsage),
		)
	}

	// Add stack trace if enabled and in development mode
	if rm.config.EnableStackTrace && rm.config.DevelopmentMode {
		fields = append(fields, logger.String("stack_trace", panicInfo.StackTrace))
	}

	// Log based on configured level
	switch strings.ToLower(rm.config.LogLevel) {
	case "debug":
		rm.logger.Debug("panic recovered", fields...)
	case "info":
		rm.logger.Info("panic recovered", fields...)
	case "warn":
		rm.logger.Warn("panic recovered", fields...)
	case "error":
		fallthrough
	default:
		rm.logger.Error("panic recovered", fields...)
	}
}

// writeErrorResponse writes an error response for the panic
func (rm *RecoveryMiddleware) writeErrorResponse(w http.ResponseWriter, r *http.Request, panicInfo *PanicInfo) {
	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)

	// Create response
	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    rm.config.ErrorCode,
			"message": rm.config.ErrorMessage,
		},
	}

	// Include details in development mode or if explicitly configured
	if rm.config.IncludeDetails || rm.config.DevelopmentMode {
		details := map[string]interface{}{
			"panic_id":  panicInfo.ID,
			"timestamp": panicInfo.Timestamp.Format(time.RFC3339),
			"recovered": panicInfo.Recovery,
		}

		// Include error details in development mode
		if rm.config.DevelopmentMode {
			details["panic_error"] = panicInfo.Error

			if rm.config.EnableStackTrace && panicInfo.StackTrace != "" {
				// Truncate stack trace for response (full trace is in logs)
				stackLines := strings.Split(panicInfo.StackTrace, "\n")
				if len(stackLines) > 20 {
					stackLines = stackLines[:20]
					stackLines = append(stackLines, "... (truncated, see logs for full trace)")
				}
				details["stack_trace"] = strings.Join(stackLines, "\n")
			}

			if panicInfo.RequestInfo != nil {
				details["request"] = map[string]interface{}{
					"method": panicInfo.RequestInfo.Method,
					"path":   panicInfo.RequestInfo.Path,
				}
			}
		}

		response["error"].(map[string]interface{})["details"] = details
	}

	// Write response
	json.NewEncoder(w).Encode(response)
}

// GetPanicStats returns panic statistics
func (rm *RecoveryMiddleware) GetPanicStats() map[string]interface{} {
	return map[string]interface{}{
		"total_panics":    rm.panicCounter.GetTotalCount(),
		"recent_panics":   rm.panicCounter.GetRecentCount(),
		"rate_limited":    rm.panicCounter.GetRateLimitedCount(),
		"window_duration": rm.config.NotificationWindow.String(),
		"max_per_window":  rm.config.MaxPanicsPerMinute,
	}
}

// NewPanicCounter creates a new panic counter
func NewPanicCounter(config Config) *PanicCounter {
	return &PanicCounter{
		counts: make(map[string]*windowCount),
		config: config,
	}
}

// RecordPanic records a panic occurrence
func (pc *PanicCounter) RecordPanic(panicID string) {
	now := time.Now()
	key := now.Format("2006-01-02-15-04") // Group by minute

	if count, exists := pc.counts[key]; exists {
		count.count++
	} else {
		pc.counts[key] = &windowCount{
			count:     1,
			firstSeen: now,
		}
	}

	// Clean up old entries
	pc.cleanup()
}

// IsRateLimited checks if panic notifications should be rate limited
func (pc *PanicCounter) IsRateLimited(panicID string) bool {
	now := time.Now()
	key := now.Format("2006-01-02-15-04")

	if count, exists := pc.counts[key]; exists {
		return count.count >= pc.config.MaxPanicsPerMinute
	}

	return false
}

// GetTotalCount returns the total panic count
func (pc *PanicCounter) GetTotalCount() int {
	total := 0
	for _, count := range pc.counts {
		total += count.count
	}
	return total
}

// GetRecentCount returns the panic count in the current window
func (pc *PanicCounter) GetRecentCount() int {
	now := time.Now()
	key := now.Format("2006-01-02-15-04")

	if count, exists := pc.counts[key]; exists {
		return count.count
	}

	return 0
}

// GetRateLimitedCount returns the number of rate-limited windows
func (pc *PanicCounter) GetRateLimitedCount() int {
	rateLimited := 0
	for _, count := range pc.counts {
		if count.count >= pc.config.MaxPanicsPerMinute {
			rateLimited++
		}
	}
	return rateLimited
}

// cleanup removes old entries from the counter
func (pc *PanicCounter) cleanup() {
	now := time.Now()
	cutoff := now.Add(-pc.config.NotificationWindow * 10) // Keep 10 windows of history

	for key, count := range pc.counts {
		if count.firstSeen.Before(cutoff) {
			delete(pc.counts, key)
		}
	}
}

// NewPanicNotifier creates a new panic notifier
func NewPanicNotifier(config Config) *PanicNotifier {
	return &PanicNotifier{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SendNotification sends a panic notification to external systems
func (pn *PanicNotifier) SendNotification(panicInfo *PanicInfo) {
	if pn.config.NotificationURL == "" {
		return
	}

	// Create notification payload
	payload := map[string]interface{}{
		"type":      "panic_alert",
		"severity":  "high",
		"timestamp": panicInfo.Timestamp.Format(time.RFC3339),
		"panic_id":  panicInfo.ID,
		"error":     panicInfo.Error,
		"service":   "forge-app", // This could be configurable
		"recovered": panicInfo.Recovery,
	}

	// Add request info if available
	if panicInfo.RequestInfo != nil {
		payload["request"] = map[string]interface{}{
			"method": panicInfo.RequestInfo.Method,
			"path":   panicInfo.RequestInfo.Path,
			"ip":     panicInfo.RequestInfo.RemoteAddr,
		}
	}

	// Add server info
	if panicInfo.ServerInfo != nil {
		payload["server"] = map[string]interface{}{
			"goroutines":   panicInfo.ServerInfo.Goroutines,
			"memory_usage": panicInfo.ServerInfo.MemoryUsage,
			"go_version":   panicInfo.ServerInfo.GoVersion,
		}
	}

	// Send HTTP notification
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return // Silently fail to avoid causing more panics
	}

	// Make HTTP request (fire and forget)
	go func() {
		req, err := http.NewRequest("POST", pn.config.NotificationURL, strings.NewReader(string(jsonPayload)))
		if err != nil {
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "forge-recovery-middleware")

		_, _ = pn.client.Do(req) // Ignore response to avoid blocking
	}()
}

// Reset resets all panic counters (useful for testing)
func (pc *PanicCounter) Reset() {
	pc.counts = make(map[string]*windowCount)
}

// ForceFlush forces a cleanup of old entries
func (pc *PanicCounter) ForceFlush() {
	pc.cleanup()
}
