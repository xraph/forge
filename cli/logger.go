package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// LogLevel represents the severity level of a log message
type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	SuccessLevel
	WarningLevel
	ErrorLevel
)

// CLILogger is a CLI-friendly logger with color support
type CLILogger struct {
	output io.Writer
	level  LogLevel
	prefix string
	colors bool
	mu     sync.Mutex
}

// NewCLILogger creates a new CLI logger
func NewCLILogger(opts ...LoggerOption) *CLILogger {
	config := &loggerConfig{
		output: os.Stdout,
		level:  InfoLevel,
		colors: DefaultColorConfig().Enabled && !DefaultColorConfig().NoColor,
	}

	for _, opt := range opts {
		opt(config)
	}

	return &CLILogger{
		output: config.output,
		level:  config.level,
		prefix: config.prefix,
		colors: config.colors,
	}
}

// loggerConfig holds logger configuration
type loggerConfig struct {
	output io.Writer
	level  LogLevel
	prefix string
	colors bool
}

// LoggerOption is a functional option for configuring the logger
type LoggerOption func(*loggerConfig)

// WithOutput sets the output writer
func WithOutput(w io.Writer) LoggerOption {
	return func(c *loggerConfig) {
		c.output = w
	}
}

// WithLevel sets the minimum log level
func WithLevel(level LogLevel) LoggerOption {
	return func(c *loggerConfig) {
		c.level = level
	}
}

// WithPrefix sets a prefix for all log messages
func WithPrefix(prefix string) LoggerOption {
	return func(c *loggerConfig) {
		c.prefix = prefix
	}
}

// WithColors enables or disables colored output
func WithColors(enabled bool) LoggerOption {
	return func(c *loggerConfig) {
		c.colors = enabled
	}
}

// Debug logs a debug message (gray)
func (l *CLILogger) Debug(msg string, args ...any) {
	if l.level > DebugLevel {
		return
	}
	l.log(DebugLevel, msg, args...)
}

// Info logs an informational message (blue)
func (l *CLILogger) Info(msg string, args ...any) {
	if l.level > InfoLevel {
		return
	}
	l.log(InfoLevel, msg, args...)
}

// Success logs a success message (green)
func (l *CLILogger) Success(msg string, args ...any) {
	if l.level > SuccessLevel {
		return
	}
	l.log(SuccessLevel, msg, args...)
}

// Warning logs a warning message (yellow)
func (l *CLILogger) Warning(msg string, args ...any) {
	if l.level > WarningLevel {
		return
	}
	l.log(WarningLevel, msg, args...)
}

// Error logs an error message (red)
func (l *CLILogger) Error(msg string, args ...any) {
	l.log(ErrorLevel, msg, args...)
}

// log is the internal logging function
func (l *CLILogger) log(level LogLevel, msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Format the message
	message := msg
	if len(args) > 0 {
		message = fmt.Sprintf(msg, args...)
	}

	// Add prefix if set
	if l.prefix != "" {
		message = l.prefix + " " + message
	}

	// Format with level and color
	formatted := l.formatMessage(level, message)

	// Write to output
	fmt.Fprintln(l.output, formatted)
}

// formatMessage formats a log message with level indicator and colors
func (l *CLILogger) formatMessage(level LogLevel, msg string) string {
	var levelStr, colorFunc func(...any) string

	switch level {
	case DebugLevel:
		levelStr = func(a ...any) string { return "[DEBUG]" }
		colorFunc = Gray
	case InfoLevel:
		levelStr = func(a ...any) string { return "[INFO]" }
		colorFunc = Blue
	case SuccessLevel:
		levelStr = func(a ...any) string { return "[✓]" }
		colorFunc = Green
	case WarningLevel:
		levelStr = func(a ...any) string { return "[!]" }
		colorFunc = Yellow
	case ErrorLevel:
		levelStr = func(a ...any) string { return "[✗]" }
		colorFunc = Red
	}

	if l.colors {
		return fmt.Sprintf("%s %s", colorFunc(levelStr()), msg)
	}
	return fmt.Sprintf("%s %s", levelStr(), msg)
}

// Print writes a message without any formatting
func (l *CLILogger) Print(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	message := msg
	if len(args) > 0 {
		message = fmt.Sprintf(msg, args...)
	}

	fmt.Fprintln(l.output, message)
}

// Printf writes a formatted message without any log level
func (l *CLILogger) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintf(l.output, format, args...)
}

// Println writes a message followed by a newline
func (l *CLILogger) Println(args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprintln(l.output, args...)
}

// SetLevel sets the minimum log level
func (l *CLILogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput sets the output writer
func (l *CLILogger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
}

// SetColors enables or disables colored output
func (l *CLILogger) SetColors(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.colors = enabled
}

// NewLine writes a blank line
func (l *CLILogger) NewLine() {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.output)
}

// Separator writes a separator line
func (l *CLILogger) Separator() {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintln(l.output, strings.Repeat("-", 60))
}
