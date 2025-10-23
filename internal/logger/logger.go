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

// Extended ANSI color codes and styles
const (
	// Reset and basic colors
	Reset = "\033[0m"

	// Text styles
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"

	// Foreground colors
	Black   = "\033[30m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"

	// Bright foreground colors
	BrightBlack   = "\033[90m"
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"

	// Background colors
	BgBlack   = "\033[40m"
	BgRed     = "\033[41m"
	BgGreen   = "\033[42m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgWhite   = "\033[47m"
)

// Log level color schemes
var (
	DebugColors = ColorScheme{
		Level:     Dim + Cyan,
		Timestamp: Dim + BrightBlack,
		Message:   Cyan,
		Fields:    Dim + Cyan,
		Key:       BrightCyan,
		Value:     Cyan,
	}

	InfoColors = ColorScheme{
		Level:     Bold + Green,
		Timestamp: Dim + BrightBlack,
		Message:   White,
		Fields:    Green,
		Key:       BrightGreen,
		Value:     Green,
	}

	WarnColors = ColorScheme{
		Level:     Bold + Yellow,
		Timestamp: Dim + BrightBlack,
		Message:   Yellow,
		Fields:    Yellow,
		Key:       BrightYellow,
		Value:     Yellow,
	}

	ErrorColors = ColorScheme{
		Level:     Bold + Red,
		Timestamp: Dim + BrightBlack,
		Message:   BrightRed,
		Fields:    Red,
		Key:       BrightRed,
		Value:     Red,
	}

	FatalColors = ColorScheme{
		Level:     Bold + BgRed + White,
		Timestamp: Dim + BrightBlack,
		Message:   Bold + Magenta,
		Fields:    Magenta,
		Key:       BrightMagenta,
		Value:     Magenta,
	}
)

// ColorScheme defines colors for different log components
type ColorScheme struct {
	Level     string
	Timestamp string
	Message   string
	Fields    string
	Key       string
	Value     string
}

var (
	// Global logger instance
	globalLogger *logger
)

// logger implements the Logger interface using zap
type logger struct {
	zap *zap.Logger
}

// noopLogger implements Logger interface but does nothing
type noopLogger struct{}

// Context keys
type contextKey int

const (
	loggerKey contextKey = iota
	requestIDKey
	traceIDKey
	userIDKey
)

type LogLevel string

const (
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelFatal LogLevel = "fatal"
	LevelDebug LogLevel = "debug"
)

// NewLogger creates a new logger with the given configuration
func NewLogger(config LoggingConfig) Logger {
	var zapLogger *zap.Logger

	// Determine log level
	logLevel := zapcore.InfoLevel
	switch strings.ToLower(string(config.Level)) {
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

// NewDevelopmentLogger creates a development logger with enhanced colors
func NewDevelopmentLogger() Logger {
	return &logger{zap: createDevelopmentLogger(zapcore.DebugLevel)}
}

// NewDevelopmentLoggerWithLevel creates a development logger with specified level
func NewDevelopmentLoggerWithLevel(level zapcore.Level) Logger {
	return &logger{zap: createDevelopmentLogger(level)}
}

// NewProductionLogger creates a production logger
func NewProductionLogger() Logger {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	zapLogger, _ := config.Build(zap.AddCallerSkip(1))
	return &logger{zap: zapLogger}
}

// NewNoopLogger creates a logger that does nothing
func NewNoopLogger() Logger {
	return &noopLogger{}
}

// createDevelopmentLogger creates a development logger with enhanced formatting
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
		EncodeLevel:    zapcore.CapitalLevelEncoder, // Will be enhanced by our encoder
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Create colored write syncer to handle full-line coloring
	writeSyncer := &ColoredWriteSyncer{
		WriteSyncer: zapcore.AddSync(os.Stdout),
	}

	// Create core with colored encoder and write syncer
	core := zapcore.NewCore(
		createColoredEncoder(encoderConfig),
		writeSyncer,
		zap.NewAtomicLevelAt(level),
	)

	return zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
}

// enhancedColorLevelEncoder adds enhanced colors to log levels
func enhancedColorLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	colorCode := colorForLevel(level)
	levelText := level.CapitalString()
	// Pad level text for clean alignment
	paddedLevel := fmt.Sprintf("%-5s", levelText)
	enc.AppendString(colorCode + paddedLevel + Reset)
}

// enhancedTimeEncoder formats timestamps with subtle coloring
func enhancedTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	timestamp := t.Format("2006-01-02 15:04:05.000")
	enc.AppendString(BrightBlack + timestamp + Reset)
}

// enhancedDurationEncoder formats durations with performance-based coloring
func enhancedDurationEncoder(d time.Duration, enc zapcore.PrimitiveArrayEncoder) {
	var color string
	if d > time.Second {
		color = Red // Slow
	} else if d > 100*time.Millisecond {
		color = Yellow // Moderate
	} else {
		color = Green // Fast
	}
	enc.AppendString(color + d.String() + Reset)
}

// enhancedCallerEncoder formats caller information with subtle highlighting
func enhancedCallerEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	if !caller.Defined {
		enc.AppendString(BrightBlack + "undefined" + Reset)
		return
	}
	enc.AppendString(Blue + caller.TrimmedPath() + Reset)
}

// colorForLevel returns the appropriate color for a log level (simple version)
func colorForLevel(level zapcore.Level) string {
	switch level {
	case zapcore.DebugLevel:
		return Cyan
	case zapcore.InfoLevel:
		return Green
	case zapcore.WarnLevel:
		return Yellow
	case zapcore.ErrorLevel:
		return Red
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return Magenta
	default:
		return Reset
	}
}

// ColoredWriteSyncer wraps WriteSyncer to add full-line coloring and fix spacing
type ColoredWriteSyncer struct {
	zapcore.WriteSyncer
}

// Write implements io.Writer with enhanced line coloring and spacing fixes
func (w *ColoredWriteSyncer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Fix spacing issues - replace excessive tabs with single spaces
	var fixedLog []byte
	var lastWasTab bool

	for i := 0; i < len(p); i++ {
		if p[i] == '\t' {
			if !lastWasTab {
				fixedLog = append(fixedLog, ' ')
				lastWasTab = true
			}
		} else {
			fixedLog = append(fixedLog, p[i])
			lastWasTab = false
		}
	}

	// Try to determine the log level from the content and apply color
	var colorCode string
	content := string(fixedLog)

	// Look for level strings in the content
	for i := 0; i < len(content)-6; i++ {
		if content[i] == '[' || (i > 0 && content[i-1] == ' ') {
			if i+5 < len(content) && content[i:i+5] == "DEBUG" {
				colorCode = Cyan
				break
			} else if i+4 < len(content) && content[i:i+4] == "INFO" {
				colorCode = Green
				break
			} else if i+4 < len(content) && content[i:i+4] == "WARN" {
				colorCode = Yellow
				break
			} else if i+5 < len(content) && content[i:i+5] == "ERROR" {
				colorCode = Red
				break
			} else if i+5 < len(content) && content[i:i+5] == "FATAL" {
				colorCode = Magenta
				break
			}
		}
	}

	// If we couldn't determine the level, just write without additional coloring
	if colorCode == "" {
		return w.WriteSyncer.Write(fixedLog)
	}

	// Write with color prefix
	colorPrefix := []byte(colorCode)
	colorSuffix := []byte(Reset)

	// Write color prefix
	_, err = w.WriteSyncer.Write(colorPrefix)
	if err != nil {
		return 0, err
	}

	// Write the content
	n, err = w.WriteSyncer.Write(fixedLog)
	if err != nil {
		return n, err
	}

	// Write color suffix (reset)
	_, err = w.WriteSyncer.Write(colorSuffix)
	if err != nil {
		return n, err
	}

	return n, nil
}

// createColoredEncoder creates an encoder with enhanced color support
func createColoredEncoder(encoderConfig zapcore.EncoderConfig) zapcore.Encoder {
	// Override the level encoder to add colors and proper formatting
	encoderConfig.EncodeLevel = enhancedColorLevelEncoder
	encoderConfig.EncodeTime = enhancedTimeEncoder
	encoderConfig.EncodeDuration = enhancedDurationEncoder
	encoderConfig.EncodeCaller = enhancedCallerEncoder

	return zapcore.NewConsoleEncoder(encoderConfig)
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

// Implementation of Logger interface for logger

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

// Implementation of Logger interface for noopLogger

func (l *noopLogger) Debug(msg string, fields ...Field)           {}
func (l *noopLogger) Info(msg string, fields ...Field)            {}
func (l *noopLogger) Warn(msg string, fields ...Field)            {}
func (l *noopLogger) Error(msg string, fields ...Field)           {}
func (l *noopLogger) Fatal(msg string, fields ...Field)           {}
func (l *noopLogger) Debugf(template string, args ...interface{}) {}
func (l *noopLogger) Infof(template string, args ...interface{})  {}
func (l *noopLogger) Warnf(template string, args ...interface{})  {}
func (l *noopLogger) Errorf(template string, args ...interface{}) {}
func (l *noopLogger) Fatalf(template string, args ...interface{}) {}
func (l *noopLogger) With(fields ...Field) Logger                 { return l }
func (l *noopLogger) WithContext(ctx context.Context) Logger      { return l }
func (l *noopLogger) Named(name string) Logger                    { return l }
func (l *noopLogger) Sugar() SugarLogger                          { return &noopSugarLogger{} }
func (l *noopLogger) Sync() error                                 { return nil }

// noopSugarLogger implements SugarLogger interface but does nothing
type noopSugarLogger struct{}

func (s *noopSugarLogger) Debugw(msg string, keysAndValues ...interface{}) {}
func (s *noopSugarLogger) Infow(msg string, keysAndValues ...interface{})  {}
func (s *noopSugarLogger) Warnw(msg string, keysAndValues ...interface{})  {}
func (s *noopSugarLogger) Errorw(msg string, keysAndValues ...interface{}) {}
func (s *noopSugarLogger) Fatalw(msg string, keysAndValues ...interface{}) {}
func (s *noopSugarLogger) With(args ...interface{}) SugarLogger            { return s }

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
		zapFields[i] = field.ZapField()
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
