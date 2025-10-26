package logger

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TestLogger provides a test logger implementation
type TestLogger struct {
	logs []LogEntry
	mu   sync.RWMutex
}

// LogEntry represents a log entry
type LogEntry struct {
	Level   string
	Message string
	Fields  map[string]interface{}
	Time    time.Time
}

func NewTestLogger() Logger {
	return &TestLogger{
		logs: make([]LogEntry, 0),
	}
}

// Debug logs a debug message
func (tl *TestLogger) Debug(msg string, fields ...Field) {
	tl.addLog("DEBUG", msg, fields)
}

// Info logs an info message
func (tl *TestLogger) Info(msg string, fields ...Field) {
	tl.addLog("INFO", msg, fields)
}

// Warn logs a warning message
func (tl *TestLogger) Warn(msg string, fields ...Field) {
	tl.addLog("WARN", msg, fields)
}

// Error logs an error message
func (tl *TestLogger) Error(msg string, fields ...Field) {
	tl.addLog("ERROR", msg, fields)
}

// WithContext returns a logger with context
func (tl *TestLogger) WithContext(ctx context.Context) Logger {
	return tl
}

// addLog adds a log entry
func (tl *TestLogger) addLog(level, msg string, fields []Field) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	fieldMap := make(map[string]interface{})
	for _, field := range fields {
		// Convert field to map - simplified for testing
		fieldMap[fmt.Sprintf("field_%d", len(fieldMap))] = field
	}

	tl.logs = append(tl.logs, LogEntry{
		Level:   level,
		Message: msg,
		Fields:  fieldMap,
		Time:    time.Now(),
	})
}

// GetLogs returns all logged entries
func (tl *TestLogger) GetLogs() []LogEntry {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	logs := make([]LogEntry, len(tl.logs))
	copy(logs, tl.logs)
	return logs
}

// GetLogsByLevel returns logs filtered by level
func (tl *TestLogger) GetLogsByLevel(level string) []LogEntry {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	var filtered []LogEntry
	for _, log := range tl.logs {
		if log.Level == level {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// Clear clears all log entries
func (tl *TestLogger) Clear() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.logs = nil
}

func (tl *TestLogger) Fatal(msg string, fields ...Field) {
	tl.addLog("FATAL", msg, fields)
}

func (tl *TestLogger) Debugf(template string, args ...interface{}) {
	tl.addLog("DEBUG", fmt.Sprintf(template, args...), nil)
}

func (tl *TestLogger) Infof(template string, args ...interface{}) {
	tl.addLog("INFO", fmt.Sprintf(template, args...), nil)
}

func (tl *TestLogger) Warnf(template string, args ...interface{}) {
	tl.addLog("WARN", fmt.Sprintf(template, args...), nil)
}

func (tl *TestLogger) Errorf(template string, args ...interface{}) {
	tl.addLog("ERROR", fmt.Sprintf(template, args...), nil)
}

func (tl *TestLogger) Fatalf(template string, args ...interface{}) {
	tl.addLog("FATAL", fmt.Sprintf(template, args...), nil)
}

func (tl *TestLogger) With(fields ...Field) Logger {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl
}

func (tl *TestLogger) Named(name string) Logger {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl
}

func (tl *TestLogger) Sugar() SugarLogger {
	return nil
}

func (tl *TestLogger) Sync() error {
	return nil
}

// AssertLogs checks if expected logs were recorded (only works with TestLogger)
func (tl *TestLogger) AssertHasLog(level, message string) bool {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	for _, log := range tl.logs {
		if log.Level == level && log.Message == message {
			return true
		}
	}
	return false
}

// CountLogs returns count of logs at a specific level
func (tl *TestLogger) CountLogs(level string) int {
	tl.mu.RLock()
	defer tl.mu.RUnlock()

	count := 0
	for _, log := range tl.logs {
		if log.Level == level {
			count++
		}
	}
	return count
}
