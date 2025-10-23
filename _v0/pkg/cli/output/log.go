package output

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/muesli/termenv"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ConsoleLogger implements a beautiful terminal logger using Charm Log + Lipgloss
type ConsoleLogger struct {
	logger    *log.Logger
	config    ConsoleLoggerConfig
	styles    *ConsoleStyles
	namedPath []string
}

// ConsoleLoggerConfig contains configuration for the console logger
type ConsoleLoggerConfig struct {
	Level           logger.LogLevel `yaml:"level" json:"level"`
	ShowTimestamp   bool            `yaml:"show_timestamp" json:"show_timestamp"`
	ShowCaller      bool            `yaml:"show_caller" json:"show_caller"`
	EnableTrueColor bool            `yaml:"enable_true_color" json:"enable_true_color"`
	ShowLevel       bool            `yaml:"show_level" json:"show_level"`
	ColorOutput     bool            `yaml:"color_output" json:"color_output"`
	Prefix          string          `yaml:"prefix" json:"prefix"`
	TimeFormat      string          `yaml:"time_format" json:"time_format"`
	Writer          io.Writer       `yaml:"-" json:"-"`
	Icons           bool            `yaml:"icons" json:"icons"`
	Borders         bool            `yaml:"borders" json:"borders"`
	Compact         bool            `yaml:"compact" json:"compact"`
}

// ConsoleStyles contains lipgloss styles for different log components
type ConsoleStyles struct {
	// Level styles
	DebugStyle lipgloss.Style
	InfoStyle  lipgloss.Style
	WarnStyle  lipgloss.Style
	ErrorStyle lipgloss.Style
	FatalStyle lipgloss.Style

	// Component styles
	TimestampStyle lipgloss.Style
	PrefixStyle    lipgloss.Style
	MessageStyle   lipgloss.Style
	FieldKeyStyle  lipgloss.Style
	FieldValStyle  lipgloss.Style
	CallerStyle    lipgloss.Style

	// Decorative styles
	BorderStyle lipgloss.Style
	IconStyle   lipgloss.Style
}

// consoleSugarLogger implements the SugarLogger interface
type consoleSugarLogger struct {
	logger *ConsoleLogger
}

// NewConsoleLogger creates a new beautiful console logger
func NewConsoleLogger(config ConsoleLoggerConfig) common.Logger {
	if config.Writer == nil {
		config.Writer = os.Stdout
	}
	if config.TimeFormat == "" {
		config.TimeFormat = "15:04:05"
	}

	// Create charm logger
	charmLogger := log.NewWithOptions(config.Writer, log.Options{
		ReportCaller:    config.ShowCaller,
		ReportTimestamp: config.ShowTimestamp,
		TimeFormat:      config.TimeFormat,
		Prefix:          config.Prefix,
	})

	// Set log level
	switch strings.ToLower(string(config.Level)) {
	case "debug":
		charmLogger.SetLevel(log.DebugLevel)
	case "info":
		charmLogger.SetLevel(log.InfoLevel)
	case "warn", "warning":
		charmLogger.SetLevel(log.WarnLevel)
	case "error":
		charmLogger.SetLevel(log.ErrorLevel)
	case "fatal":
		charmLogger.SetLevel(log.FatalLevel)
	default:
		charmLogger.SetLevel(log.InfoLevel)
	}

	// Create styles
	styles := createConsoleStyles(config)

	// Configure charm logger styles
	if config.ColorOutput {
		configureCharmStyles(charmLogger, styles, config)
	}

	return &ConsoleLogger{
		logger: charmLogger,
		config: config,
		styles: styles,
	}
}

// NewDefaultConsoleLogger creates a console logger with sensible defaults
func NewDefaultConsoleLogger() common.Logger {
	return NewConsoleLogger(DefaultConsoleLoggerConfig())
}

// NewCompactConsoleLogger creates a compact console logger for CLI tools
func NewCompactConsoleLogger() common.Logger {
	config := DefaultConsoleLoggerConfig()
	config.Compact = true
	config.ShowTimestamp = false
	config.ShowCaller = false
	config.Icons = true
	return NewConsoleLogger(config)
}

// NewVerboseConsoleLogger creates a verbose console logger for development
func NewVerboseConsoleLogger() common.Logger {
	config := DefaultConsoleLoggerConfig()
	config.Level = "debug"
	config.ShowTimestamp = true
	config.ShowCaller = true
	config.Borders = true
	return NewConsoleLogger(config)
}

// createConsoleStyles creates beautiful lipgloss styles
func createConsoleStyles(config ConsoleLoggerConfig) *ConsoleStyles {
	styles := &ConsoleStyles{}

	if config.ColorOutput {
		// Level styles with beautiful colors and icons
		styles.DebugStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Bold(false)

		styles.InfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981")).
			Bold(true)

		styles.WarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Bold(true)

		styles.ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")).
			Bold(true)

		styles.FatalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#DC2626")).
			Bold(true).
			Padding(0, 1)

		// Component styles
		styles.TimestampStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Faint(true)

		styles.PrefixStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8B5CF6")).
			Bold(true)

		styles.MessageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F3F4F6"))

		styles.FieldKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#60A5FA")).
			Bold(false)

		styles.FieldValStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#34D399"))

		styles.CallerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A78BFA")).
			Faint(true)

		// Decorative styles
		if config.Borders {
			styles.BorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#6B7280")).
				Padding(0, 1)
		}

		styles.IconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B"))
	} else {
		// Monochrome styles for non-color terminals
		styles.DebugStyle = lipgloss.NewStyle()
		styles.InfoStyle = lipgloss.NewStyle().Bold(true)
		styles.WarnStyle = lipgloss.NewStyle().Bold(true)
		styles.ErrorStyle = lipgloss.NewStyle().Bold(true)
		styles.FatalStyle = lipgloss.NewStyle().Bold(true)
		styles.TimestampStyle = lipgloss.NewStyle()
		styles.PrefixStyle = lipgloss.NewStyle().Bold(true)
		styles.MessageStyle = lipgloss.NewStyle()
		styles.FieldKeyStyle = lipgloss.NewStyle()
		styles.FieldValStyle = lipgloss.NewStyle()
		styles.CallerStyle = lipgloss.NewStyle()
		styles.BorderStyle = lipgloss.NewStyle()
		styles.IconStyle = lipgloss.NewStyle()
	}

	return styles
}

// configureCharmStyles applies lipgloss styles to the charm logger
func configureCharmStyles(charmLogger *log.Logger, styles *ConsoleStyles, config ConsoleLoggerConfig) {
	if config.EnableTrueColor {
		charmLogger.SetColorProfile(termenv.TrueColor)
	}

	// Configure charm logger with custom styles
	charmLogger.SetStyles(&log.Styles{
		Timestamp: styles.TimestampStyle,
		Caller:    styles.CallerStyle,
		Prefix:    styles.PrefixStyle,
		Message:   styles.MessageStyle,
		Key:       styles.FieldKeyStyle,
		Value:     styles.FieldValStyle,
		Separator: lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")),
		Levels: map[log.Level]lipgloss.Style{
			log.DebugLevel: styles.DebugStyle,
			log.InfoLevel:  styles.InfoStyle,
			log.WarnLevel:  styles.WarnStyle,
			log.ErrorLevel: styles.ErrorStyle,
			log.FatalLevel: styles.FatalStyle,
		},
	})

	// Add icons if enabled
	if config.Icons {
		charmLogger.SetPrefix(getLogLevelIcon(string(config.Level)))
	}
}

// getLogLevelIcon returns appropriate icons for log levels
func getLogLevelIcon(level string) string {
	icons := map[string]string{
		"debug": "🔍",
		"info":  "ℹ️",
		"warn":  "⚠️",
		"error": "❌",
		"fatal": "💀",
	}
	if icon, ok := icons[level]; ok {
		return icon
	}
	return "•"
}

// Implementation of common.Logger interface

func (c *ConsoleLogger) Debug(msg string, fields ...common.LogField) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("debug"))
	}
	c.logger.Debug(msg, c.convertFields(fields)...)
}

func (c *ConsoleLogger) Info(msg string, fields ...common.LogField) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("info"))
	}
	c.logger.Info(msg, c.convertFields(fields)...)
}

func (c *ConsoleLogger) Warn(msg string, fields ...common.LogField) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("warn"))
	}
	c.logger.Warn(msg, c.convertFields(fields)...)
}

func (c *ConsoleLogger) Error(msg string, fields ...common.LogField) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("error"))
	}
	c.logger.Error(msg, c.convertFields(fields)...)
}

func (c *ConsoleLogger) Fatal(msg string, fields ...common.LogField) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("fatal"))
	}
	c.logger.Fatal(msg, c.convertFields(fields)...)
}

func (c *ConsoleLogger) Debugf(template string, args ...interface{}) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("debug"))
	}
	c.logger.Debugf(template, args...)
}

func (c *ConsoleLogger) Infof(template string, args ...interface{}) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("info"))
	}
	c.logger.Infof(template, args...)
}

func (c *ConsoleLogger) Warnf(template string, args ...interface{}) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("warn"))
	}
	c.logger.Warnf(template, args...)
}

func (c *ConsoleLogger) Errorf(template string, args ...interface{}) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("error"))
	}
	c.logger.Errorf(template, args...)
}

func (c *ConsoleLogger) Fatalf(template string, args ...interface{}) {
	if c.config.Icons {
		c.logger.SetPrefix(getLogLevelIcon("fatal"))
	}
	c.logger.Fatalf(template, args...)
}

func (c *ConsoleLogger) With(fields ...common.LogField) common.Logger {
	// Create a new logger with additional context
	newLogger := &ConsoleLogger{
		logger:    c.logger.With(c.convertFields(fields)...),
		config:    c.config,
		styles:    c.styles,
		namedPath: c.namedPath,
	}
	return newLogger
}

func (c *ConsoleLogger) WithContext(ctx context.Context) common.Logger {
	// Extract context values and add as fields
	var contextFields []common.LogField

	// Add any context values you want to extract
	if value := ctx.Value("request_id"); value != nil {
		contextFields = append(contextFields, logger.String("request_id", fmt.Sprintf("%v", value)))
	}
	if value := ctx.Value("trace_id"); value != nil {
		contextFields = append(contextFields, logger.String("trace_id", fmt.Sprintf("%v", value)))
	}
	if value := ctx.Value("user_id"); value != nil {
		contextFields = append(contextFields, logger.String("user_id", fmt.Sprintf("%v", value)))
	}

	if len(contextFields) > 0 {
		return c.With(contextFields...)
	}
	return c
}

func (c *ConsoleLogger) Named(name string) common.Logger {
	namedPath := append(c.namedPath, name)
	fullName := strings.Join(namedPath, ".")

	newLogger := &ConsoleLogger{
		logger:    c.logger.With("component", fullName),
		config:    c.config,
		styles:    c.styles,
		namedPath: namedPath,
	}
	return newLogger
}

func (c *ConsoleLogger) Sugar() logger.SugarLogger {
	return &consoleSugarLogger{logger: c}
}

func (c *ConsoleLogger) Sync() error {
	// Charm log doesn't need explicit syncing, but we can implement if needed
	return nil
}

// convertFields converts common.LogField to charm log compatible interface
func (c *ConsoleLogger) convertFields(fields []logger.Field) []interface{} {
	var charmFields []interface{}
	for _, field := range fields {
		if field != nil {
			charmFields = append(charmFields, field.Key(), field.Value())
		}
	}
	return charmFields
}

// Implementation of consoleSugarLogger

func (s *consoleSugarLogger) Debugw(msg string, keysAndValues ...interface{}) {
	if s.logger.config.Icons {
		s.logger.logger.SetPrefix(getLogLevelIcon("debug"))
	}
	s.logger.logger.Debug(msg, keysAndValues...)
}

func (s *consoleSugarLogger) Infow(msg string, keysAndValues ...interface{}) {
	if s.logger.config.Icons {
		s.logger.logger.SetPrefix(getLogLevelIcon("info"))
	}
	s.logger.logger.Info(msg, keysAndValues...)
}

func (s *consoleSugarLogger) Warnw(msg string, keysAndValues ...interface{}) {
	if s.logger.config.Icons {
		s.logger.logger.SetPrefix(getLogLevelIcon("warn"))
	}
	s.logger.logger.Warn(msg, keysAndValues...)
}

func (s *consoleSugarLogger) Errorw(msg string, keysAndValues ...interface{}) {
	if s.logger.config.Icons {
		s.logger.logger.SetPrefix(getLogLevelIcon("error"))
	}
	s.logger.logger.Error(msg, keysAndValues...)
}

func (s *consoleSugarLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	if s.logger.config.Icons {
		s.logger.logger.SetPrefix(getLogLevelIcon("fatal"))
	}
	s.logger.logger.Fatal(msg, keysAndValues...)
}

func (s *consoleSugarLogger) With(args ...interface{}) logger.SugarLogger {
	newLogger := &ConsoleLogger{
		logger:    s.logger.logger.With(args...),
		config:    s.logger.config,
		styles:    s.logger.styles,
		namedPath: s.logger.namedPath,
	}
	return &consoleSugarLogger{logger: newLogger}
}

// Configuration helpers

// DefaultConsoleLoggerConfig returns sensible defaults for console logging
func DefaultConsoleLoggerConfig() ConsoleLoggerConfig {
	return ConsoleLoggerConfig{
		Level:         "info",
		ShowTimestamp: true,
		ShowCaller:    false,
		ShowLevel:     true,
		ColorOutput:   true,
		TimeFormat:    "15:04:05",
		Writer:        os.Stdout,
		Icons:         false,
		Borders:       false,
		Compact:       false,
	}
}

// CLIConsoleLoggerConfig returns config optimized for CLI tools
func CLIConsoleLoggerConfig() ConsoleLoggerConfig {
	return ConsoleLoggerConfig{
		Level:         "info",
		ShowTimestamp: false,
		ShowCaller:    false,
		ShowLevel:     false,
		ColorOutput:   true,
		Writer:        os.Stdout,
		Icons:         true,
		Borders:       false,
		Compact:       true,
	}
}

// DevelopmentConsoleLoggerConfig returns config for development
func DevelopmentConsoleLoggerConfig() ConsoleLoggerConfig {
	return ConsoleLoggerConfig{
		Level:         "debug",
		ShowTimestamp: true,
		ShowCaller:    true,
		ShowLevel:     true,
		ColorOutput:   true,
		TimeFormat:    "15:04:05.000",
		Writer:        os.Stdout,
		Icons:         true,
		Borders:       true,
		Compact:       false,
	}
}

// ProductionConsoleLoggerConfig returns config for production console output
func ProductionConsoleLoggerConfig() ConsoleLoggerConfig {
	return ConsoleLoggerConfig{
		Level:         "info",
		ShowTimestamp: true,
		ShowCaller:    false,
		ShowLevel:     true,
		ColorOutput:   false, // Disable colors in production
		TimeFormat:    time.RFC3339,
		Writer:        os.Stdout,
		Icons:         false,
		Borders:       false,
		Compact:       false,
	}
}

// Utility functions for enhanced console logging

// NewCLILogger creates a logger optimized for CLI applications
func NewCLILogger(appName string) common.Logger {
	config := CLIConsoleLoggerConfig()
	config.Prefix = appName
	return NewConsoleLogger(config)
}

// NewSilentCLILogger creates a logger optimized for CLI applications
func NewSilentCLILogger(appName string) common.Logger {
	config := CLIConsoleLoggerConfig()
	config.Prefix = appName
	config.Level = logger.LevelError
	return NewConsoleLogger(config)
}

// NewDevelopmentLogger creates a logger for development with all features enabled
func NewDevelopmentLogger(component string) common.Logger {
	config := DevelopmentConsoleLoggerConfig()
	logger := NewConsoleLogger(config)
	if component != "" {
		return logger.Named(component)
	}
	return logger
}

// LogSuccess logs a success message with a checkmark
func LogSuccess(l common.Logger, message string, fields ...common.LogField) {
	successFields := append([]common.LogField{logger.String("status", "success")}, fields...)
	l.Info("✅ "+message, successFields...)
}

// LogFailure logs a failure message with an X mark
func LogFailure(l common.Logger, message string, err error, fields ...common.LogField) {
	failureFields := append([]common.LogField{
		logger.String("status", "failure"),
		logger.Error(err),
	}, fields...)
	l.Error("❌ "+message, failureFields...)
}

// LogProgress logs a progress message with a progress indicator
func LogProgress(l common.Logger, message string, current, total int, fields ...common.LogField) {
	progressFields := append([]common.LogField{
		logger.Int("current", current),
		logger.Int("total", total),
		logger.String("progress", fmt.Sprintf("%d/%d", current, total)),
	}, fields...)
	l.Info("⏳ "+message, progressFields...)
}

// LogCommand logs a command execution
func LogCommand(l common.Logger, command string, args []string, fields ...common.LogField) {
	cmdFields := append([]common.LogField{
		logger.String("command", command),
		logger.Strings("args", args),
	}, fields...)
	l.Debug("🔧 Executing command", cmdFields...)
}

// LogDuration logs an operation with its duration
func LogDuration(l common.Logger, operation string, duration time.Duration, fields ...common.LogField) {
	durationFields := append([]common.LogField{
		logger.String("operation", operation),
		logger.Duration("duration", duration),
	}, fields...)
	l.Info("⏱️  "+operation+" completed", durationFields...)
}
