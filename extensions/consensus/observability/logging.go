package observability

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// LogManager manages structured logging for consensus operations
type LogManager struct {
	nodeID string
	logger forge.Logger

	// Log buffering
	buffer    []LogEntry
	bufferMu  sync.RWMutex
	maxBuffer int

	// Configuration
	config LogConfig

	// Statistics
	stats LogStatistics
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Operation string
	Message   string
	Fields    map[string]interface{}
	TraceID   string
	SpanID    string
	NodeID    string
}

// LogLevel represents log level
type LogLevel string

const (
	// LogLevelDebug debug level
	LogLevelDebug LogLevel = "debug"
	// LogLevelInfo info level
	LogLevelInfo LogLevel = "info"
	// LogLevelWarn warning level
	LogLevelWarn LogLevel = "warn"
	// LogLevelError error level
	LogLevelError LogLevel = "error"
)

// LogConfig contains logging configuration
type LogConfig struct {
	EnableBuffer     bool
	BufferSize       int
	EnableTracing    bool
	EnableStructured bool
	SampleRate       float64
}

// LogStatistics contains logging statistics
type LogStatistics struct {
	TotalLogs    int64
	DebugLogs    int64
	InfoLogs     int64
	WarnLogs     int64
	ErrorLogs    int64
	BufferedLogs int
}

// NewLogManager creates a new log manager
func NewLogManager(nodeID string, config LogConfig, logger forge.Logger) *LogManager {
	// Set defaults
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.SampleRate == 0 {
		config.SampleRate = 1.0
	}

	return &LogManager{
		nodeID:    nodeID,
		logger:    logger,
		buffer:    make([]LogEntry, 0, config.BufferSize),
		maxBuffer: config.BufferSize,
		config:    config,
	}
}

// Debug logs a debug message
func (lm *LogManager) Debug(ctx context.Context, operation, message string, fields ...interface{}) {
	lm.log(ctx, LogLevelDebug, operation, message, fields...)
	lm.stats.DebugLogs++
}

// Info logs an info message
func (lm *LogManager) Info(ctx context.Context, operation, message string, fields ...interface{}) {
	lm.log(ctx, LogLevelInfo, operation, message, fields...)
	lm.stats.InfoLogs++
}

// Warn logs a warning message
func (lm *LogManager) Warn(ctx context.Context, operation, message string, fields ...interface{}) {
	lm.log(ctx, LogLevelWarn, operation, message, fields...)
	lm.stats.WarnLogs++
}

// Error logs an error message
func (lm *LogManager) Error(ctx context.Context, operation, message string, fields ...interface{}) {
	lm.log(ctx, LogLevelError, operation, message, fields...)
	lm.stats.ErrorLogs++
}

// log performs the actual logging
func (lm *LogManager) log(ctx context.Context, level LogLevel, operation, message string, fields ...interface{}) {
	lm.stats.TotalLogs++

	// Check sample rate
	if !lm.shouldLog() {
		return
	}

	// Build field map
	fieldMap := make(map[string]interface{})
	// Fields are passed as key-value pairs
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			key := fmt.Sprint(fields[i])
			value := fields[i+1]
			fieldMap[key] = value
		}
	}

	// Add standard fields
	fieldMap["node_id"] = lm.nodeID
	fieldMap["operation"] = operation

	// Add trace context if available
	traceID := ""
	spanID := ""
	if lm.config.EnableTracing && ctx != nil {
		if tid, ok := GetTraceContext(ctx); ok {
			traceID = tid
			fieldMap["trace_id"] = traceID
		}
		if sid, ok := GetSpanContext(ctx); ok {
			spanID = sid
			fieldMap["span_id"] = spanID
		}
	}

	// Create log entry
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Operation: operation,
		Message:   message,
		Fields:    fieldMap,
		TraceID:   traceID,
		SpanID:    spanID,
		NodeID:    lm.nodeID,
	}

	// Buffer if enabled
	if lm.config.EnableBuffer {
		lm.bufferEntry(entry)
	}

	// Log through Forge logger
	switch level {
	case LogLevelDebug:
		lm.logger.Debug(message, forge.F("operation", operation))
	case LogLevelInfo:
		lm.logger.Info(message, forge.F("operation", operation))
	case LogLevelWarn:
		lm.logger.Warn(message, forge.F("operation", operation))
	case LogLevelError:
		lm.logger.Error(message, forge.F("operation", operation))
	}
}

// bufferEntry adds entry to buffer
func (lm *LogManager) bufferEntry(entry LogEntry) {
	lm.bufferMu.Lock()
	defer lm.bufferMu.Unlock()

	lm.buffer = append(lm.buffer, entry)

	// Trim if needed
	if len(lm.buffer) > lm.maxBuffer {
		lm.buffer = lm.buffer[len(lm.buffer)-lm.maxBuffer:]
	}

	lm.stats.BufferedLogs = len(lm.buffer)
}

// GetBuffer returns buffered log entries
func (lm *LogManager) GetBuffer(limit int) []LogEntry {
	lm.bufferMu.RLock()
	defer lm.bufferMu.RUnlock()

	if limit <= 0 || limit > len(lm.buffer) {
		limit = len(lm.buffer)
	}

	start := len(lm.buffer) - limit
	result := make([]LogEntry, limit)
	copy(result, lm.buffer[start:])

	return result
}

// SearchLogs searches buffered logs
func (lm *LogManager) SearchLogs(filter LogFilter) []LogEntry {
	lm.bufferMu.RLock()
	defer lm.bufferMu.RUnlock()

	var results []LogEntry

	for _, entry := range lm.buffer {
		if filter.Matches(entry) {
			results = append(results, entry)
		}
	}

	return results
}

// LogFilter filters log entries
type LogFilter struct {
	Level     LogLevel
	Operation string
	TraceID   string
	StartTime time.Time
	EndTime   time.Time
}

// Matches checks if entry matches filter
func (lf LogFilter) Matches(entry LogEntry) bool {
	if lf.Level != "" && entry.Level != lf.Level {
		return false
	}
	if lf.Operation != "" && entry.Operation != lf.Operation {
		return false
	}
	if lf.TraceID != "" && entry.TraceID != lf.TraceID {
		return false
	}
	if !lf.StartTime.IsZero() && entry.Timestamp.Before(lf.StartTime) {
		return false
	}
	if !lf.EndTime.IsZero() && entry.Timestamp.After(lf.EndTime) {
		return false
	}
	return true
}

// ClearBuffer clears the log buffer
func (lm *LogManager) ClearBuffer() {
	lm.bufferMu.Lock()
	defer lm.bufferMu.Unlock()

	lm.buffer = lm.buffer[:0]
	lm.stats.BufferedLogs = 0

	lm.logger.Debug("log buffer cleared")
}

// GetStatistics returns logging statistics
func (lm *LogManager) GetStatistics() LogStatistics {
	lm.bufferMu.RLock()
	defer lm.bufferMu.RUnlock()

	stats := lm.stats
	stats.BufferedLogs = len(lm.buffer)
	return stats
}

// shouldLog determines if log should be written based on sample rate
func (lm *LogManager) shouldLog() bool {
	if lm.config.SampleRate >= 1.0 {
		return true
	}
	if lm.config.SampleRate <= 0.0 {
		return false
	}

	return (time.Now().UnixNano() % 100) < int64(lm.config.SampleRate*100)
}

// ExportLogs exports logs in structured format
func (lm *LogManager) ExportLogs(filter LogFilter) []map[string]interface{} {
	logs := lm.SearchLogs(filter)

	exported := make([]map[string]interface{}, len(logs))
	for i, log := range logs {
		exported[i] = map[string]interface{}{
			"timestamp": log.Timestamp,
			"level":     log.Level,
			"operation": log.Operation,
			"message":   log.Message,
			"fields":    log.Fields,
			"trace_id":  log.TraceID,
			"span_id":   log.SpanID,
			"node_id":   log.NodeID,
		}
	}

	return exported
}

// LogElection logs election-related events
func (lm *LogManager) LogElection(ctx context.Context, event string, term uint64, candidateID string) {
	lm.Info(ctx, "election", event,
		forge.F("term", term),
		forge.F("candidate", candidateID),
	)
}

// LogReplication logs replication events
func (lm *LogManager) LogReplication(ctx context.Context, event string, followerID string, index uint64) {
	lm.Debug(ctx, "replication", event,
		forge.F("follower", followerID),
		forge.F("index", index),
	)
}

// LogSnapshot logs snapshot events
func (lm *LogManager) LogSnapshot(ctx context.Context, event string, index uint64, size int64) {
	lm.Info(ctx, "snapshot", event,
		forge.F("index", index),
		forge.F("size_bytes", size),
	)
}

// LogStateChange logs state machine changes
func (lm *LogManager) LogStateChange(ctx context.Context, event string, index uint64) {
	lm.Debug(ctx, "statemachine", event,
		forge.F("index", index),
	)
}

// LogClusterChange logs cluster membership changes
func (lm *LogManager) LogClusterChange(ctx context.Context, event string, nodeID string, memberCount int) {
	lm.Info(ctx, "cluster", event,
		forge.F("affected_node", nodeID),
		forge.F("member_count", memberCount),
	)
}

// LogPerformance logs performance metrics
func (lm *LogManager) LogPerformance(ctx context.Context, operation string, duration time.Duration, success bool) {
	level := LogLevelDebug
	if !success {
		level = LogLevelWarn
	}

	lm.log(ctx, level, "performance", fmt.Sprintf("%s completed", operation),
		forge.F("operation", operation),
		forge.F("duration_ms", duration.Milliseconds()),
		forge.F("success", success),
	)
}

// GetLogsByLevel returns logs filtered by level
func (lm *LogManager) GetLogsByLevel(level LogLevel, limit int) []LogEntry {
	return lm.SearchLogs(LogFilter{Level: level})
}

// GetErrorLogs returns error logs
func (lm *LogManager) GetErrorLogs(limit int) []LogEntry {
	return lm.GetLogsByLevel(LogLevelError, limit)
}

// GetWarningLogs returns warning logs
func (lm *LogManager) GetWarningLogs(limit int) []LogEntry {
	return lm.GetLogsByLevel(LogLevelWarn, limit)
}

// GetLogsByOperation returns logs for a specific operation
func (lm *LogManager) GetLogsByOperation(operation string, limit int) []LogEntry {
	return lm.SearchLogs(LogFilter{Operation: operation})
}

// GetLogsByTrace returns logs for a specific trace
func (lm *LogManager) GetLogsByTrace(traceID string) []LogEntry {
	return lm.SearchLogs(LogFilter{TraceID: traceID})
}

// GetRecentLogs returns most recent logs
func (lm *LogManager) GetRecentLogs(duration time.Duration, limit int) []LogEntry {
	return lm.SearchLogs(LogFilter{
		StartTime: time.Now().Add(-duration),
	})
}
