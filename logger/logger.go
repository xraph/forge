package logger

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ANSI color codes for development logging
const (
	Reset      = "\033[0m"
	DebugColor = "\033[36m" // Cyan
	InfoColor  = "\033[32m" // Green
	WarnColor  = "\033[33m" // Yellow
	ErrorColor = "\033[31m" // Red
	FatalColor = "\033[35m" // Magenta
)

var (
	// Global logger instance
	globalLogger *logger
)

// logger implements the Logger interface using zap
type logger struct {
	zap *zap.Logger
}

// Context keys
type contextKey int

const (
	loggerKey contextKey = iota
	requestIDKey
	traceIDKey
	userIDKey
)

// NewLogger creates a new logger with the given configuration
func NewLogger(config LoggingConfig) Logger {
	var zapLogger *zap.Logger

	// Determine log level
	logLevel := zapcore.InfoLevel
	switch strings.ToLower(config.Level) {
	case "debug":
		logLevel = zapcore.DebugLevel
	case "info":
		logLevel = zapcore.InfoLevel
	case "warn", "warning":
		logLevel = zapcore.WarnLevel
	case "error":
		logLevel = zapcore.ErrorLevel
	case "fatal":
		logLevel = zapcore.FatalLevel
	}

	// Configure logger based on environment
	if config.Environment == "production" || config.Format == "json" {
		zapConfig := zap.NewProductionConfig()
		zapConfig.Level = zap.NewAtomicLevelAt(logLevel)
		zapLogger, _ = zapConfig.Build(zap.AddCallerSkip(1))
	} else {
		zapLogger = createDevelopmentLogger(logLevel)
	}

	return &logger{zap: zapLogger}
}

// NewDevelopmentLogger creates a development logger with colors
func NewDevelopmentLogger() Logger {
	return &logger{zap: createDevelopmentLogger(zapcore.DebugLevel)}
}

// NewProductionLogger creates a production logger
func NewProductionLogger() Logger {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	zapLogger, _ := config.Build(zap.AddCallerSkip(1))
	return &logger{zap: zapLogger}
}

// createDevelopmentLogger creates a development logger with custom formatting
func createDevelopmentLogger(level zapcore.Level) *zap.Logger {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    customColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Create console encoder with colors
	encoder := zapcore.NewConsoleEncoder(encoderConfig)

	// Create core
	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		zap.NewAtomicLevelAt(level),
	)

	return zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
}

// customColorLevelEncoder adds colors to log levels
func customColorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	var color string
	switch level {
	case zapcore.DebugLevel:
		color = DebugColor
	case zapcore.InfoLevel:
		color = InfoColor
	case zapcore.WarnLevel:
		color = WarnColor
	case zapcore.ErrorLevel:
		color = ErrorColor
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		color = FatalColor
	default:
		color = Reset
	}

	levelText := level.CapitalString()
	enc.AppendString(color + levelText + Reset)
}

// GetGlobalLogger returns the global logger instance
func GetGlobalLogger() Logger {
	if globalLogger == nil {
		globalLogger = NewDevelopmentLogger().(*logger)
	}
	return globalLogger
}

// SetGlobalLogger sets the global logger instance
func SetGlobalLogger(l Logger) {
	if lg, ok := l.(*logger); ok {
		globalLogger = lg
	}
}

// Implementation of Logger interface

func (l *logger) Debug(msg string, fields ...Field) {
	l.zap.Debug(msg, fieldsToZap(fields)...)
}

func (l *logger) Info(msg string, fields ...Field) {
	l.zap.Info(msg, fieldsToZap(fields)...)
}

func (l *logger) Warn(msg string, fields ...Field) {
	l.zap.Warn(msg, fieldsToZap(fields)...)
}

func (l *logger) Error(msg string, fields ...Field) {
	l.zap.Error(msg, fieldsToZap(fields)...)
}

func (l *logger) Fatal(msg string, fields ...Field) {
	l.zap.Fatal(msg, fieldsToZap(fields)...)
}

func (l *logger) Debugf(template string, args ...interface{}) {
	l.zap.Debug(fmt.Sprintf(template, args...))
}

func (l *logger) Infof(template string, args ...interface{}) {
	l.zap.Info(fmt.Sprintf(template, args...))
}

func (l *logger) Warnf(template string, args ...interface{}) {
	l.zap.Warn(fmt.Sprintf(template, args...))
}

func (l *logger) Errorf(template string, args ...interface{}) {
	l.zap.Error(fmt.Sprintf(template, args...))
}

func (l *logger) Fatalf(template string, args ...interface{}) {
	l.zap.Fatal(fmt.Sprintf(template, args...))
}

func (l *logger) With(fields ...Field) Logger {
	return &logger{zap: l.zap.With(fieldsToZap(fields)...)}
}

func (l *logger) WithContext(ctx context.Context) Logger {
	if ctx == nil {
		return l
	}

	// Use the new context-aware field constructors
	contextFields := ContextFields(ctx)
	if len(contextFields) > 0 {
		return &logger{zap: l.zap.With(fieldsToZap(contextFields)...)}
	}

	return l
}

func (l *logger) Named(name string) Logger {
	return &logger{zap: l.zap.Named(name)}
}

func (l *logger) Sugar() SugarLogger {
	return &sugarLogger{sugar: l.zap.Sugar()}
}

func (l *logger) Sync() error {
	return l.zap.Sync()
}

// TrackWithFields logs the execution time with additional fields
func TrackWithFields(ctx context.Context, name string, fields ...Field) func() {
	start := time.Now()
	logger := LoggerFromContext(ctx)

	return func() {
		duration := time.Since(start)
		allFields := append(fields,
			String("function", name),
			Duration("duration", duration),
		)
		logger.Debug("Function execution completed", allFields...)
	}
}

// LogPanicWithFields logs a panic with additional fields
func LogPanicWithFields(logger Logger, recovered interface{}, fields ...Field) {
	allFields := append(fields,
		Any("panic", recovered),
		Stack("stacktrace"),
	)
	logger.Error("Panic recovered", allFields...)
}

// HTTPRequestLogger creates a logger with HTTP request fields
func HTTPRequestLogger(logger Logger, method, path, userAgent string, status int) Logger {
	group := HTTPRequestGroup(method, path, userAgent, status)
	return logger.With(group.Fields()...)
}

// DatabaseQueryLogger creates a logger with database query fields
func DatabaseQueryLogger(logger Logger, query, table string, rows int64, duration time.Duration) Logger {
	group := DatabaseQueryGroup(query, table, rows, duration)
	return logger.With(group.Fields()...)
}

// ServiceLogger creates a logger with service information fields
func ServiceLogger(logger Logger, name, version, environment string) Logger {
	group := ServiceInfoGroup(name, version, environment)
	return logger.With(group.Fields()...)
}

// sugarLogger implements the SugarLogger interface
type sugarLogger struct {
	sugar *zap.SugaredLogger
}

// Implementation of SugarLogger interface

func (s *sugarLogger) Debugw(msg string, keysAndValues ...interface{}) {
	s.sugar.Debugw(msg, keysAndValues...)
}

func (s *sugarLogger) Infow(msg string, keysAndValues ...interface{}) {
	s.sugar.Infow(msg, keysAndValues...)
}

func (s *sugarLogger) Warnw(msg string, keysAndValues ...interface{}) {
	s.sugar.Warnw(msg, keysAndValues...)
}

func (s *sugarLogger) Errorw(msg string, keysAndValues ...interface{}) {
	s.sugar.Errorw(msg, keysAndValues...)
}

func (s *sugarLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	s.sugar.Fatalw(msg, keysAndValues...)
}

func (s *sugarLogger) With(args ...interface{}) SugarLogger {
	return &sugarLogger{sugar: s.sugar.With(args...)}
}

// Context helper functions

// WithLogger adds a logger to the context
func WithLogger(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext extracts a logger from the context
func LoggerFromContext(ctx context.Context) Logger {
	if ctx == nil {
		return GetGlobalLogger()
	}
	if l, ok := ctx.Value(loggerKey).(Logger); ok {
		return l
	}
	return GetGlobalLogger()
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext extracts the request ID from the context
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithTraceID adds a trace ID to the context
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// TraceIDFromContext extracts the trace ID from the context
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

// WithUserID adds a user ID to the context
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext extracts the user ID from the context
func UserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(userIDKey).(string); ok {
		return id
	}
	return ""
}

// Utility functions

// fieldsToZap converts Field interfaces to zap.Field
func fieldsToZap(fields []Field) []zap.Field {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = zap.Any(field.Key(), field.Value())
	}
	return zapFields
}

// NewField creates a new field
func NewField(key string, value interface{}) Field {
	return &CustomField{key: key, value: value}
}

// Track logs the execution time of a function
func Track(ctx context.Context, name string) func() {
	start := time.Now()
	logger := LoggerFromContext(ctx)

	return func() {
		duration := time.Since(start)
		logger.Debug("Function execution completed",
			String("function", name),
			Duration("duration", duration),
		)
	}
}

// TrackWithLogger logs the execution time using a specific logger
func TrackWithLogger(logger Logger, name string) func() {
	start := time.Now()

	return func() {
		duration := time.Since(start)
		logger.Debug("Function execution completed",
			String("function", name),
			Duration("duration", duration),
		)
	}
}

// LogPanic logs a panic with stack trace
func LogPanic(logger Logger, recovered interface{}) {
	logger.Error("Panic recovered",
		Any("panic", recovered),
		Stack("stacktrace"),
	)
}

// ConditionalLog logs only if condition is true
func ConditionalLog(condition bool, logger Logger, level string, msg string, fields ...Field) {
	if !condition {
		return
	}

	switch strings.ToLower(level) {
	case "debug":
		logger.Debug(msg, fields...)
	case "info":
		logger.Info(msg, fields...)
	case "warn", "warning":
		logger.Warn(msg, fields...)
	case "error":
		logger.Error(msg, fields...)
	case "fatal":
		logger.Fatal(msg, fields...)
	}
}

// Must wraps a function call and logs any error fatally
func Must(err error, logger Logger, msg string, fields ...Field) {
	if err != nil {
		allFields := append(fields, Error(err))
		logger.Fatal(msg, allFields...)
	}
}

// MustNotNil logs fatally if value is nil
func MustNotNil(value interface{}, logger Logger, msg string, fields ...Field) {
	if value == nil {
		logger.Fatal(msg, fields...)
	}
}

// ErrorHandler provides a callback-based error handler with logging
type ErrorHandler struct {
	logger   Logger
	callback func(error)
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(logger Logger, callback func(error)) *ErrorHandler {
	return &ErrorHandler{
		logger:   logger,
		callback: callback,
	}
}

// Handle handles an error by logging it and calling the callback
func (eh *ErrorHandler) Handle(err error, msg string, fields ...Field) {
	if err == nil {
		return
	}

	allFields := append(fields, Error(err))
	eh.logger.Error(msg, allFields...)

	if eh.callback != nil {
		eh.callback(err)
	}
}

// HandleWithLevel handles an error at a specific log level
func (eh *ErrorHandler) HandleWithLevel(err error, level string, msg string, fields ...Field) {
	if err == nil {
		return
	}

	allFields := append(fields, Error(err))

	switch strings.ToLower(level) {
	case "debug":
		eh.logger.Debug(msg, allFields...)
	case "info":
		eh.logger.Info(msg, allFields...)
	case "warn", "warning":
		eh.logger.Warn(msg, allFields...)
	case "error":
		eh.logger.Error(msg, allFields...)
	case "fatal":
		eh.logger.Fatal(msg, allFields...)
	}

	if eh.callback != nil {
		eh.callback(err)
	}
}

// LoggingWriter is an io.Writer that logs each write
type LoggingWriter struct {
	logger Logger
	level  string
}

// NewLoggingWriter creates a new logging writer
func NewLoggingWriter(logger Logger, level string) *LoggingWriter {
	return &LoggingWriter{
		logger: logger,
		level:  level,
	}
}

// Write implements io.Writer
func (lw *LoggingWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		ConditionalLog(true, lw.logger, lw.level, msg)
	}
	return len(p), nil
}
