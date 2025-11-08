package logger

import (
	"context"
	"time"
)

// StructuredLog provides a fluent interface for structured logging.
type StructuredLog struct {
	logger Logger
	fields []Field
}

// NewStructuredLog creates a new structured log.
func NewStructuredLog(logger Logger) *StructuredLog {
	return &StructuredLog{
		logger: logger,
		fields: make([]Field, 0),
	}
}

// WithField adds a field to the structured log.
func (sl *StructuredLog) WithField(field Field) *StructuredLog {
	sl.fields = append(sl.fields, field)

	return sl
}

// WithFields adds multiple fields to the structured log.
func (sl *StructuredLog) WithFields(fields ...Field) *StructuredLog {
	sl.fields = append(sl.fields, fields...)

	return sl
}

// WithGroup adds a field group to the structured log.
func (sl *StructuredLog) WithGroup(group *FieldGroup) *StructuredLog {
	sl.fields = append(sl.fields, group.Fields()...)

	return sl
}

// WithContext adds context fields to the structured log.
func (sl *StructuredLog) WithContext(ctx context.Context) *StructuredLog {
	contextFields := ContextFields(ctx)
	sl.fields = append(sl.fields, contextFields...)

	return sl
}

// WithHTTPRequest adds HTTP request fields.
func (sl *StructuredLog) WithHTTPRequest(method, path, userAgent string, status int) *StructuredLog {
	group := HTTPRequestGroup(method, path, userAgent, status)

	return sl.WithGroup(group)
}

// WithDatabaseQuery adds database query fields.
func (sl *StructuredLog) WithDatabaseQuery(query, table string, rows int64, duration time.Duration) *StructuredLog {
	group := DatabaseQueryGroup(query, table, rows, duration)

	return sl.WithGroup(group)
}

// WithService adds service information fields.
func (sl *StructuredLog) WithService(name, version, environment string) *StructuredLog {
	group := ServiceInfoGroup(name, version, environment)

	return sl.WithGroup(group)
}

// Debug logs at debug level.
func (sl *StructuredLog) Debug(msg string) {
	sl.logger.Debug(msg, sl.fields...)
}

// Info logs at info level.
func (sl *StructuredLog) Info(msg string) {
	sl.logger.Info(msg, sl.fields...)
}

// Warn logs at warn level.
func (sl *StructuredLog) Warn(msg string) {
	sl.logger.Warn(msg, sl.fields...)
}

// Error logs at error level.
func (sl *StructuredLog) Error(msg string) {
	sl.logger.Error(msg, sl.fields...)
}

// Fatal logs at fatal level.
func (sl *StructuredLog) Fatal(msg string) {
	sl.logger.Fatal(msg, sl.fields...)
}

// Logger returns a logger with all accumulated fields.
func (sl *StructuredLog) Logger() Logger {
	return sl.logger.With(sl.fields...)
}
