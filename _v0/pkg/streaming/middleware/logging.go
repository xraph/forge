package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	streaming "github.com/xraph/forge/v0/pkg/streaming/core"
)

// LoggingMiddleware provides comprehensive logging for streaming connections
type LoggingMiddleware struct {
	logger  common.Logger
	metrics common.Metrics
	config  LoggingConfig
	buffer  *LogBuffer
	mu      sync.RWMutex
	stats   LoggingStats
}

// LoggingConfig contains configuration for logging middleware
type LoggingConfig struct {
	Enabled            bool          `yaml:"enabled" default:"true"`
	LogLevel           string        `yaml:"log_level" default:"info"`
	LogConnections     bool          `yaml:"log_connections" default:"true"`
	LogMessages        bool          `yaml:"log_messages" default:"true"`
	LogPresence        bool          `yaml:"log_presence" default:"false"`
	LogErrors          bool          `yaml:"log_errors" default:"true"`
	LogPerformance     bool          `yaml:"log_performance" default:"true"`
	LogRequestHeaders  bool          `yaml:"log_request_headers" default:"false"`
	LogResponseHeaders bool          `yaml:"log_response_headers" default:"false"`
	LogUserAgents      bool          `yaml:"log_user_agents" default:"true"`
	LogIPAddresses     bool          `yaml:"log_ip_addresses" default:"true"`
	LogSensitiveData   bool          `yaml:"log_sensitive_data" default:"false"`
	BufferSize         int           `yaml:"buffer_size" default:"1000"`
	FlushInterval      time.Duration `yaml:"flush_interval" default:"30s"`
	MaxMessageLength   int           `yaml:"max_message_length" default:"1000"`
	StructuredLogging  bool          `yaml:"structured_logging" default:"true"`
	IncludeStackTrace  bool          `yaml:"include_stack_trace" default:"false"`
	LogRotation        bool          `yaml:"log_rotation" default:"true"`
	MaxLogSize         int64         `yaml:"max_log_size" default:"100MB"`
	MaxBackups         int           `yaml:"max_backups" default:"5"`
	CompressBackups    bool          `yaml:"compress_backups" default:"true"`
	AsyncLogging       bool          `yaml:"async_logging" default:"false"`
	LogFormat          string        `yaml:"log_format" default:"json"` // json, text, custom
	TimestampFormat    string        `yaml:"timestamp_format" default:"2006-01-02T15:04:05.000Z07:00"`
	ExcludeFields      []string      `yaml:"exclude_fields"`
	IncludeFields      []string      `yaml:"include_fields"`
	SamplingRate       float64       `yaml:"sampling_rate" default:"1.0"`
	FilterPatterns     []string      `yaml:"filter_patterns"`
}

// DefaultLoggingConfig returns default logging configuration
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Enabled:            true,
		LogLevel:           "info",
		LogConnections:     true,
		LogMessages:        true,
		LogPresence:        false,
		LogErrors:          true,
		LogPerformance:     true,
		LogRequestHeaders:  false,
		LogResponseHeaders: false,
		LogUserAgents:      true,
		LogIPAddresses:     true,
		LogSensitiveData:   false,
		BufferSize:         1000,
		FlushInterval:      30 * time.Second,
		MaxMessageLength:   1000,
		StructuredLogging:  true,
		IncludeStackTrace:  false,
		LogRotation:        true,
		MaxLogSize:         100 * 1024 * 1024, // 100MB
		MaxBackups:         5,
		CompressBackups:    true,
		AsyncLogging:       false,
		LogFormat:          "json",
		TimestampFormat:    "2006-01-02T15:04:05.000Z07:00",
		ExcludeFields:      []string{},
		IncludeFields:      []string{},
		SamplingRate:       1.0,
		FilterPatterns:     []string{},
	}
}

// LogEntry represents a log entry
type LogEntry struct {
	Timestamp    time.Time              `json:"timestamp"`
	Level        string                 `json:"level"`
	Message      string                 `json:"message"`
	ConnectionID string                 `json:"connection_id,omitempty"`
	UserID       string                 `json:"user_id,omitempty"`
	RoomID       string                 `json:"room_id,omitempty"`
	MessageType  string                 `json:"message_type,omitempty"`
	Protocol     string                 `json:"protocol,omitempty"`
	RemoteAddr   string                 `json:"remote_addr,omitempty"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	Duration     time.Duration          `json:"duration,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Data         map[string]interface{} `json:"data,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
}

// LogBuffer manages buffered logging
type LogBuffer struct {
	entries []LogEntry
	mu      sync.RWMutex
	size    int
	maxSize int
}

// NewLogBuffer creates a new log buffer
func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		entries: make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add adds a log entry to the buffer
func (lb *LogBuffer) Add(entry LogEntry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if len(lb.entries) >= lb.maxSize {
		// Remove oldest entry
		lb.entries = lb.entries[1:]
	}

	lb.entries = append(lb.entries, entry)
	lb.size = len(lb.entries)
}

// GetEntries returns all buffered entries
func (lb *LogBuffer) GetEntries() []LogEntry {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	entries := make([]LogEntry, len(lb.entries))
	copy(entries, lb.entries)
	return entries
}

// Clear clears the buffer
func (lb *LogBuffer) Clear() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.entries = lb.entries[:0]
	lb.size = 0
}

// Size returns the current buffer size
func (lb *LogBuffer) Size() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return lb.size
}

// LoggingStats contains logging statistics
type LoggingStats struct {
	TotalLogEntries   int64     `json:"total_log_entries"`
	ConnectionLogs    int64     `json:"connection_logs"`
	MessageLogs       int64     `json:"message_logs"`
	ErrorLogs         int64     `json:"error_logs"`
	BufferSize        int       `json:"buffer_size"`
	LastFlush         time.Time `json:"last_flush"`
	FlushCount        int64     `json:"flush_count"`
	DroppedEntries    int64     `json:"dropped_entries"`
	AverageLogSize    float64   `json:"average_log_size"`
	LogsPerSecond     float64   `json:"logs_per_second"`
	LastLogTime       time.Time `json:"last_log_time"`
	LogLevel          string    `json:"log_level"`
	AsyncMode         bool      `json:"async_mode"`
	BufferUtilization float64   `json:"buffer_utilization"`
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(
	logger common.Logger,
	metrics common.Metrics,
	config LoggingConfig,
) *LoggingMiddleware {
	middleware := &LoggingMiddleware{
		logger:  logger,
		metrics: metrics,
		config:  config,
		buffer:  NewLogBuffer(config.BufferSize),
		stats:   LoggingStats{},
	}

	// Start background flush routine if needed
	if config.AsyncLogging {
		go middleware.flushRoutine()
	}

	return middleware
}

// Handler returns the HTTP middleware handler
func (lm *LoggingMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !lm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			startTime := time.Now()

			// Wrap response writer to capture response data
			wrapper := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				size:           0,
			}

			// Log request
			lm.logRequest(r, startTime)

			// Continue to next handler
			next.ServeHTTP(wrapper, r)

			// Log response
			lm.logResponse(r, wrapper, time.Since(startTime))
		})
	}
}

// LogConnection logs connection events
func (lm *LoggingMiddleware) LogConnection(
	event string,
	conn streaming.Connection,
	details map[string]interface{},
) {
	if !lm.config.Enabled || !lm.config.LogConnections {
		return
	}

	entry := LogEntry{
		Timestamp:    time.Now(),
		Level:        "info",
		Message:      fmt.Sprintf("connection %s", event),
		ConnectionID: conn.ID(),
		UserID:       conn.UserID(),
		RoomID:       conn.RoomID(),
		Protocol:     string(conn.Protocol()),
		RemoteAddr:   conn.RemoteAddr(),
		UserAgent:    conn.UserAgent(),
		Data:         details,
		Tags:         []string{"connection", event},
	}

	lm.processLogEntry(entry)
	lm.updateConnectionStats()
}

// LogMessage logs message events
func (lm *LoggingMiddleware) LogMessage(
	event string,
	conn streaming.Connection,
	message *streaming.Message,
	details map[string]interface{},
) {
	if !lm.config.Enabled || !lm.config.LogMessages {
		return
	}

	// Check if we should log this message type
	if !lm.shouldLogMessage(message) {
		return
	}

	// Apply sampling if configured
	if !lm.shouldSample() {
		return
	}

	messageContent := lm.formatMessageContent(message)

	entry := LogEntry{
		Timestamp:    time.Now(),
		Level:        "info",
		Message:      fmt.Sprintf("message %s", event),
		ConnectionID: conn.ID(),
		UserID:       conn.UserID(),
		RoomID:       message.RoomID,
		MessageType:  string(message.Type),
		Protocol:     string(conn.Protocol()),
		RemoteAddr:   conn.RemoteAddr(),
		Data: map[string]interface{}{
			"message_id":      message.ID,
			"message_content": messageContent,
			"from":            message.From,
			"to":              message.To,
			"priority":        message.Priority,
		},
		Tags: []string{"message", event, string(message.Type)},
	}

	// Add custom details
	if details != nil {
		for k, v := range details {
			entry.Data[k] = v
		}
	}

	lm.processLogEntry(entry)
	lm.updateMessageStats()
}

// LogError logs error events
func (lm *LoggingMiddleware) LogError(
	event string,
	conn streaming.Connection,
	err error,
	details map[string]interface{},
) {
	if !lm.config.Enabled || !lm.config.LogErrors {
		return
	}

	entry := LogEntry{
		Timestamp:    time.Now(),
		Level:        "error",
		Message:      fmt.Sprintf("error %s", event),
		ConnectionID: conn.ID(),
		UserID:       conn.UserID(),
		RoomID:       conn.RoomID(),
		Protocol:     string(conn.Protocol()),
		RemoteAddr:   conn.RemoteAddr(),
		Error:        err.Error(),
		Data:         details,
		Tags:         []string{"error", event},
	}

	lm.processLogEntry(entry)
	lm.updateErrorStats()
}

// LogPerformance logs performance metrics
func (lm *LoggingMiddleware) LogPerformance(
	operation string,
	duration time.Duration,
	conn streaming.Connection,
	details map[string]interface{},
) {
	if !lm.config.Enabled || !lm.config.LogPerformance {
		return
	}

	entry := LogEntry{
		Timestamp:    time.Now(),
		Level:        "info",
		Message:      fmt.Sprintf("performance %s", operation),
		ConnectionID: conn.ID(),
		UserID:       conn.UserID(),
		RoomID:       conn.RoomID(),
		Protocol:     string(conn.Protocol()),
		Duration:     duration,
		Data: map[string]interface{}{
			"operation":        operation,
			"duration_ms":      duration.Milliseconds(),
			"duration_seconds": duration.Seconds(),
		},
		Tags: []string{"performance", operation},
	}

	// Add custom details
	if details != nil {
		for k, v := range details {
			entry.Data[k] = v
		}
	}

	lm.processLogEntry(entry)
}

// LogPresence logs presence events
func (lm *LoggingMiddleware) LogPresence(
	event string,
	userID string,
	roomID string,
	status streaming.PresenceStatus,
	details map[string]interface{},
) {
	if !lm.config.Enabled || !lm.config.LogPresence {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   fmt.Sprintf("presence %s", event),
		UserID:    userID,
		RoomID:    roomID,
		Data: map[string]interface{}{
			"status": string(status),
		},
		Tags: []string{"presence", event},
	}

	// Add custom details
	if details != nil {
		for k, v := range details {
			entry.Data[k] = v
		}
	}

	lm.processLogEntry(entry)
}

// logRequest logs HTTP request details
func (lm *LoggingMiddleware) logRequest(r *http.Request, startTime time.Time) {
	entry := LogEntry{
		Timestamp:  startTime,
		Level:      "info",
		Message:    "http request",
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
		Data: map[string]interface{}{
			"method":         r.Method,
			"url":            r.URL.String(),
			"proto":          r.Proto,
			"content_length": r.ContentLength,
		},
		Tags: []string{"http", "request"},
	}

	// Add headers if configured
	if lm.config.LogRequestHeaders {
		headers := make(map[string]string)
		for name, values := range r.Header {
			if len(values) > 0 {
				headers[name] = values[0]
			}
		}
		entry.Data["headers"] = headers
	}

	lm.processLogEntry(entry)
}

// logResponse logs HTTP response details
func (lm *LoggingMiddleware) logResponse(r *http.Request, w *responseWriter, duration time.Duration) {
	entry := LogEntry{
		Timestamp:  time.Now(),
		Level:      lm.getLogLevelForStatus(w.statusCode),
		Message:    "http response",
		RemoteAddr: r.RemoteAddr,
		Duration:   duration,
		Data: map[string]interface{}{
			"method":        r.Method,
			"url":           r.URL.String(),
			"status_code":   w.statusCode,
			"response_size": w.size,
			"duration_ms":   duration.Milliseconds(),
		},
		Tags: []string{"http", "response"},
	}

	// Add response headers if configured
	if lm.config.LogResponseHeaders {
		headers := make(map[string]string)
		for name, values := range w.Header() {
			if len(values) > 0 {
				headers[name] = values[0]
			}
		}
		entry.Data["headers"] = headers
	}

	lm.processLogEntry(entry)
}

// processLogEntry processes and outputs a log entry
func (lm *LoggingMiddleware) processLogEntry(entry LogEntry) {
	// Apply filters
	if !lm.shouldLogEntry(entry) {
		return
	}

	// Remove sensitive data if configured
	if !lm.config.LogSensitiveData {
		entry = lm.sanitizeLogEntry(entry)
	}

	// Apply field filters
	entry = lm.applyFieldFilters(entry)

	// Update stats
	lm.updateLogStats(entry)

	// Handle async vs sync logging
	if lm.config.AsyncLogging {
		lm.buffer.Add(entry)
	} else {
		lm.writeLogEntry(entry)
	}
}

// shouldLogEntry determines if an entry should be logged
func (lm *LoggingMiddleware) shouldLogEntry(entry LogEntry) bool {
	// Check log level
	if !lm.isLogLevelEnabled(entry.Level) {
		return false
	}

	// Check filter patterns
	for _, pattern := range lm.config.FilterPatterns {
		if strings.Contains(entry.Message, pattern) {
			return false
		}
	}

	return true
}

// shouldLogMessage determines if a message should be logged
func (lm *LoggingMiddleware) shouldLogMessage(message *streaming.Message) bool {
	// Skip presence messages if not configured
	if message.Type == streaming.MessageTypePresence && !lm.config.LogPresence {
		return false
	}

	return true
}

// shouldSample determines if this log entry should be sampled
func (lm *LoggingMiddleware) shouldSample() bool {
	if lm.config.SamplingRate >= 1.0 {
		return true
	}

	// Simple sampling based on current time
	return time.Now().UnixNano()%100 < int64(lm.config.SamplingRate*100)
}

// formatMessageContent formats message content for logging
func (lm *LoggingMiddleware) formatMessageContent(message *streaming.Message) string {
	content := fmt.Sprintf("%v", message.Data)

	// Truncate if too long
	if len(content) > lm.config.MaxMessageLength {
		content = content[:lm.config.MaxMessageLength] + "..."
	}

	return content
}

// sanitizeLogEntry removes sensitive data from log entry
func (lm *LoggingMiddleware) sanitizeLogEntry(entry LogEntry) LogEntry {
	// Remove sensitive fields
	sensitiveFields := []string{
		"password", "token", "secret", "key", "auth", "authorization",
		"credential", "private", "confidential",
	}

	if entry.Data != nil {
		for _, field := range sensitiveFields {
			if _, exists := entry.Data[field]; exists {
				entry.Data[field] = "[REDACTED]"
			}
		}
	}

	return entry
}

// applyFieldFilters applies include/exclude field filters
func (lm *LoggingMiddleware) applyFieldFilters(entry LogEntry) LogEntry {
	if entry.Data == nil {
		return entry
	}

	// Apply exclude filters
	for _, field := range lm.config.ExcludeFields {
		delete(entry.Data, field)
	}

	// Apply include filters (if specified, only include these fields)
	if len(lm.config.IncludeFields) > 0 {
		filteredData := make(map[string]interface{})
		for _, field := range lm.config.IncludeFields {
			if value, exists := entry.Data[field]; exists {
				filteredData[field] = value
			}
		}
		entry.Data = filteredData
	}

	return entry
}

// writeLogEntry writes a log entry to the logger
func (lm *LoggingMiddleware) writeLogEntry(entry LogEntry) {
	if lm.logger == nil {
		return
	}

	// Convert to logger fields
	fields := lm.convertToLoggerFields(entry)

	// Write based on log level
	switch entry.Level {
	case "debug":
		lm.logger.Debug(entry.Message, fields...)
	case "info":
		lm.logger.Info(entry.Message, fields...)
	case "warn":
		lm.logger.Warn(entry.Message, fields...)
	case "error":
		lm.logger.Error(entry.Message, fields...)
	default:
		lm.logger.Info(entry.Message, fields...)
	}
}

// convertToLoggerFields converts log entry to logger fields
func (lm *LoggingMiddleware) convertToLoggerFields(entry LogEntry) []logger.Field {
	fields := []logger.Field{
		logger.String("timestamp", entry.Timestamp.Format(lm.config.TimestampFormat)),
	}

	if entry.ConnectionID != "" {
		fields = append(fields, logger.String("connection_id", entry.ConnectionID))
	}
	if entry.UserID != "" {
		fields = append(fields, logger.String("user_id", entry.UserID))
	}
	if entry.RoomID != "" {
		fields = append(fields, logger.String("room_id", entry.RoomID))
	}
	if entry.MessageType != "" {
		fields = append(fields, logger.String("message_type", entry.MessageType))
	}
	if entry.Protocol != "" {
		fields = append(fields, logger.String("protocol", entry.Protocol))
	}
	if entry.RemoteAddr != "" {
		fields = append(fields, logger.String("remote_addr", entry.RemoteAddr))
	}
	if entry.UserAgent != "" {
		fields = append(fields, logger.String("user_agent", entry.UserAgent))
	}
	if entry.Duration > 0 {
		fields = append(fields, logger.Duration("duration", entry.Duration))
	}
	if entry.Error != "" {
		fields = append(fields, logger.String("error", entry.Error))
	}
	if len(entry.Tags) > 0 {
		fields = append(fields, logger.String("tags", strings.Join(entry.Tags, ",")))
	}

	// Add data fields
	if entry.Data != nil {
		for k, v := range entry.Data {
			fields = append(fields, logger.Any(k, v))
		}
	}

	return fields
}

// isLogLevelEnabled checks if the log level is enabled
func (lm *LoggingMiddleware) isLogLevelEnabled(level string) bool {
	levels := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
	}

	configLevel, exists := levels[lm.config.LogLevel]
	if !exists {
		configLevel = 1 // default to info
	}

	entryLevel, exists := levels[level]
	if !exists {
		entryLevel = 1 // default to info
	}

	return entryLevel >= configLevel
}

// getLogLevelForStatus returns appropriate log level for HTTP status code
func (lm *LoggingMiddleware) getLogLevelForStatus(statusCode int) string {
	if statusCode >= 500 {
		return "error"
	} else if statusCode >= 400 {
		return "warn"
	}
	return "info"
}

// flushRoutine runs the background flush routine for async logging
func (lm *LoggingMiddleware) flushRoutine() {
	ticker := time.NewTicker(lm.config.FlushInterval)
	defer ticker.Stop()

	for range ticker.C {
		lm.flush()
	}
}

// flush writes all buffered log entries
func (lm *LoggingMiddleware) flush() {
	entries := lm.buffer.GetEntries()

	for _, entry := range entries {
		lm.writeLogEntry(entry)
	}

	lm.buffer.Clear()

	lm.mu.Lock()
	lm.stats.LastFlush = time.Now()
	lm.stats.FlushCount++
	lm.mu.Unlock()
}

// updateLogStats updates logging statistics
func (lm *LoggingMiddleware) updateLogStats(entry LogEntry) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lm.stats.TotalLogEntries++
	lm.stats.LastLogTime = time.Now()
	lm.stats.BufferSize = lm.buffer.Size()
	lm.stats.BufferUtilization = float64(lm.buffer.Size()) / float64(lm.config.BufferSize)
}

// updateConnectionStats updates connection logging statistics
func (lm *LoggingMiddleware) updateConnectionStats() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.stats.ConnectionLogs++
}

// updateMessageStats updates message logging statistics
func (lm *LoggingMiddleware) updateMessageStats() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.stats.MessageLogs++
}

// updateErrorStats updates error logging statistics
func (lm *LoggingMiddleware) updateErrorStats() {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.stats.ErrorLogs++
}

// GetStats returns logging statistics
func (lm *LoggingMiddleware) GetStats() LoggingStats {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.stats
}

// GetBufferedEntries returns current buffered log entries
func (lm *LoggingMiddleware) GetBufferedEntries() []LogEntry {
	return lm.buffer.GetEntries()
}

// UpdateConfig updates the logging configuration
func (lm *LoggingMiddleware) UpdateConfig(config LoggingConfig) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.config = config
}

// GetConfig returns the current configuration
func (lm *LoggingMiddleware) GetConfig() LoggingConfig {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.config
}

// responseWriter wraps http.ResponseWriter to capture response data
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

// ConnectionLogger provides connection-specific logging
type ConnectionLogger struct {
	middleware *LoggingMiddleware
	connection streaming.Connection
	context    map[string]interface{}
}

// NewConnectionLogger creates a new connection logger
func NewConnectionLogger(middleware *LoggingMiddleware, conn streaming.Connection) *ConnectionLogger {
	return &ConnectionLogger{
		middleware: middleware,
		connection: conn,
		context:    make(map[string]interface{}),
	}
}

// AddContext adds context to the connection logger
func (cl *ConnectionLogger) AddContext(key string, value interface{}) {
	cl.context[key] = value
}

// LogMessage logs a message with connection context
func (cl *ConnectionLogger) LogMessage(event string, message *streaming.Message) {
	cl.middleware.LogMessage(event, cl.connection, message, cl.context)
}

// LogError logs an error with connection context
func (cl *ConnectionLogger) LogError(event string, err error) {
	cl.middleware.LogError(event, cl.connection, err, cl.context)
}

// LogPerformance logs performance metrics with connection context
func (cl *ConnectionLogger) LogPerformance(operation string, duration time.Duration) {
	cl.middleware.LogPerformance(operation, duration, cl.connection, cl.context)
}
