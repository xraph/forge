package cli

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/pkg/cli/output"
	"github.com/xraph/forge/pkg/cli/prompt"
	"github.com/xraph/forge/pkg/common"
)

// CLIContext provides enhanced context with Forge capabilities
type CLIContext interface {
	App() CLIApp
	// Cobra context delegation
	Command() *cobra.Command
	Args() []string

	// Service resolution
	Resolve(service interface{}) error
	MustResolve(service interface{}) interface{}

	// Configuration access
	Config() common.ConfigManager
	GetString(key string) string
	GetInt(key string) int
	GetBool(key string) bool
	GetStringSlice(key string) []string

	// Logging
	Logger() common.Logger
	Debug(msg string, fields ...common.LogField)
	Info(msg string, fields ...common.LogField)
	Warn(msg string, fields ...common.LogField)
	Error(msg string, fields ...common.LogField)

	// Metrics
	Metrics() common.Metrics
	Counter(name string, tags ...string) common.Counter
	Timer(name string, tags ...string) common.Timer

	// Output utilities
	Success(message string)
	ErrorMsg(message string)
	Warning(message string)
	InfoMsg(message string)

	// Progress tracking
	Progress(max int) output.ProgressTracker
	Spinner(message string) output.ProgressTracker

	// Table output
	Table(headers []string, rows [][]string)
	OutputData(data interface{}) error

	// Interactive prompts
	Confirm(message string) bool
	Select(message string, options []string) (int, error)
	Input(message string, validate func(string) error) (string, error)
	InteractiveSelect(message string, options []prompt.SelectionOption) (int, *prompt.SelectionOption, error)
	Password(message string) (string, error)

	// Context values
	Set(key string, value interface{})
	Get(key string) interface{}
	Has(key string) bool

	// Flag access
	GetFlag(name string) (*FlagValue, error)
	GetStringFlag(name string) (string, error)
	GetIntFlag(name string) (int, error)
	GetBoolFlag(name string) (bool, error)

	// Output writers
	Output() io.Writer
	ErrorOutput() io.Writer
}

// FlagValue represents a CLI flag value
type FlagValue struct {
	Name        string
	Value       interface{}
	Type        string
	Default     interface{}
	Description string
	Required    bool
	Changed     bool
}

// cliContext implements CLIContext with full Forge integration
type cliContext struct {
	cobra *cobra.Command

	// Forge integration
	container common.Container
	logger    common.Logger
	metrics   common.Metrics
	config    common.ConfigManager

	// CLI-specific
	app    *cliApp
	args   []string
	values map[string]interface{}

	// Output formatters
	formatters map[string]output.OutputFormatter

	// Interactive components
	prompter *prompt.Prompter
}

// NewCLIContext creates a new CLI context
func NewCLIContext(cmd *cobra.Command, app *cliApp, args []string) CLIContext {
	ctx := &cliContext{
		cobra:     cmd,
		container: app.container,
		logger:    app.logger,
		metrics:   app.metrics,
		config:    app.config,
		app:       app,
		args:      args,
		values:    make(map[string]interface{}),
		formatters: map[string]output.OutputFormatter{
			"json":  output.NewJSONFormatter(),
			"yaml":  output.NewYAMLFormatter(),
			"table": output.NewTableFormatter(),
		},
		prompter: prompt.NewPrompter(app.input, app.output),
	}

	return ctx
}

// App returns the underlying Cobra command
func (ctx *cliContext) App() CLIApp {
	return ctx.app
}

// Command returns the underlying Cobra command
func (ctx *cliContext) Command() *cobra.Command {
	return ctx.cobra
}

// Args returns command arguments
func (ctx *cliContext) Args() []string {
	return ctx.args
}

// Resolve resolves a service from the DI container
func (ctx *cliContext) Resolve(service interface{}) error {
	if ctx.container == nil {
		return common.ErrContainerError("resolve", common.NewForgeError("NO_CONTAINER", "no DI container available", nil))
	}

	resolved, err := ctx.container.Resolve(service)
	if err != nil {
		return err
	}

	// Use reflection to set the resolved service
	serviceValue := reflect.ValueOf(service)
	if serviceValue.Kind() != reflect.Ptr {
		return common.ErrInvalidConfig("service", common.NewForgeError("NOT_POINTER", "service must be a pointer", nil))
	}

	serviceValue.Elem().Set(reflect.ValueOf(resolved))
	return nil
}

// MustResolve resolves a service or panics
func (ctx *cliContext) MustResolve(service interface{}) interface{} {
	if err := ctx.Resolve(service); err != nil {
		panic(fmt.Sprintf("failed to resolve service: %v", err))
	}

	serviceValue := reflect.ValueOf(service)
	if serviceValue.Kind() == reflect.Ptr {
		return serviceValue.Elem().Interface()
	}
	return serviceValue.Interface()
}

// Config returns the configuration manager
func (ctx *cliContext) Config() common.ConfigManager {
	return ctx.config
}

// GetString gets a string value from config or flags
func (ctx *cliContext) GetString(key string) string {
	// Try flag first
	if value, err := ctx.cobra.Flags().GetString(key); err == nil && value != "" {
		return value
	}
	// Fallback to config
	if ctx.config != nil {
		return ctx.config.GetString(key)
	}
	return ""
}

// GetInt gets an integer value from config or flags
func (ctx *cliContext) GetInt(key string) int {
	// Try flag first
	if value, err := ctx.cobra.Flags().GetInt(key); err == nil {
		return value
	}
	// Fallback to config
	if ctx.config != nil {
		return ctx.config.GetInt(key)
	}
	return 0
}

// GetBool gets a boolean value from config or flags
func (ctx *cliContext) GetBool(key string) bool {
	// Try flag first
	if value, err := ctx.cobra.Flags().GetBool(key); err == nil {
		return value
	}
	// Fallback to config
	if ctx.config != nil {
		return ctx.config.GetBool(key)
	}
	return false
}

// GetStringSlice gets a string slice from flags
func (ctx *cliContext) GetStringSlice(key string) []string {
	if value, err := ctx.cobra.Flags().GetStringSlice(key); err == nil {
		return value
	}
	return []string{}
}

// Logger returns the logger
func (ctx *cliContext) Logger() common.Logger {
	return ctx.logger
}

// Debug logs a debug message
func (ctx *cliContext) Debug(msg string, fields ...common.LogField) {
	if ctx.logger != nil {
		ctx.logger.Debug(msg, fields...)
	}
}

// Info logs an info message
func (ctx *cliContext) Info(msg string, fields ...common.LogField) {
	if ctx.logger != nil {
		ctx.logger.Info(msg, fields...)
	}
}

// Warn logs a warning message
func (ctx *cliContext) Warn(msg string, fields ...common.LogField) {
	if ctx.logger != nil {
		ctx.logger.Warn(msg, fields...)
	}
}

// Error logs an error message
func (ctx *cliContext) Error(msg string, fields ...common.LogField) {
	if ctx.logger != nil {
		ctx.logger.Error(msg, fields...)
	}
}

// Metrics returns the metrics collector
func (ctx *cliContext) Metrics() common.Metrics {
	return ctx.metrics
}

// Counter returns a counter metric
func (ctx *cliContext) Counter(name string, tags ...string) common.Counter {
	if ctx.metrics != nil {
		return ctx.metrics.Counter(name, tags...)
	}
	return nil
}

// Timer returns a timer metric
func (ctx *cliContext) Timer(name string, tags ...string) common.Timer {
	if ctx.metrics != nil {
		return ctx.metrics.Timer(name, tags...)
	}
	return nil
}

// Success prints a success message
func (ctx *cliContext) Success(message string) {
	fmt.Fprintf(ctx.app.output, "✅ %s\n", message)
}

// ErrorMsg prints an error message
func (ctx *cliContext) ErrorMsg(message string) {
	fmt.Fprintf(ctx.app.output, "❌ %s\n", message)
}

// Warning prints a warning message
func (ctx *cliContext) Warning(message string) {
	fmt.Fprintf(ctx.app.output, "⚠️  %s\n", message)
}

// InfoMsg prints an info message
func (ctx *cliContext) InfoMsg(message string) {
	fmt.Fprintf(ctx.app.output, "ℹ️  %s\n", message)
}

// Progress creates a progress tracker
func (ctx *cliContext) Progress(max int) output.ProgressTracker {
	return output.NewProgressBar(ctx.app.output, max)
}

// Spinner creates a spinner
func (ctx *cliContext) Spinner(message string) output.ProgressTracker {
	return output.NewSpinner(ctx.app.output, message)
}

// Table outputs data as a table
func (ctx *cliContext) Table(headers []string, rows [][]string) {
	formatter := output.NewTableFormatter()
	data := map[string]interface{}{
		"headers": headers,
		"rows":    rows,
	}

	if formatted, err := formatter.Format(data); err == nil {
		ctx.app.output.Write(formatted)
	}
}

// OutputData outputs data in the requested format
func (ctx *cliContext) OutputData(data interface{}) error {
	format := ctx.GetString("output")
	if format == "" {
		format = "json"
	}

	formatter, exists := ctx.formatters[format]
	if !exists {
		formatter = ctx.formatters["json"]
	}

	formatted, err := formatter.Format(data)
	if err != nil {
		return err
	}

	_, err = ctx.app.output.Write(formatted)
	return err
}

// Confirm shows a confirmation prompt
func (ctx *cliContext) Confirm(message string) bool {
	return ctx.prompter.Confirm(message)
}

// Select shows a selection prompt
func (ctx *cliContext) Select(message string, options []string) (int, error) {
	return ctx.prompter.Select(message, options)
}

// InteractiveSelect shows a selection prompt
func (ctx *cliContext) InteractiveSelect(message string, options []prompt.SelectionOption) (int, *prompt.SelectionOption, error) {
	return ctx.prompter.InteractiveSelect(message, options)
}

// Input shows an input prompt with validation
func (ctx *cliContext) Input(message string, validate func(string) error) (string, error) {
	return ctx.prompter.Input(message, validate)
}

// Password shows a password input prompt
func (ctx *cliContext) Password(message string) (string, error) {
	return ctx.prompter.Password(message)
}

// Set sets a context value
func (ctx *cliContext) Set(key string, value interface{}) {
	ctx.values[key] = value
}

// Get gets a context value
func (ctx *cliContext) Get(key string) interface{} {
	return ctx.values[key]
}

// Has checks if a context value exists
func (ctx *cliContext) Has(key string) bool {
	_, exists := ctx.values[key]
	return exists
}

// GetFlag gets a flag value with metadata
func (ctx *cliContext) GetFlag(name string) (*FlagValue, error) {
	flag := ctx.cobra.Flags().Lookup(name)
	if flag == nil {
		return nil, fmt.Errorf("flag %s not found", name)
	}

	return &FlagValue{
		Name:        flag.Name,
		Value:       flag.Value.String(),
		Type:        flag.Value.Type(),
		Default:     flag.DefValue,
		Description: flag.Usage,
		Required:    strings.Contains(flag.Usage, "required"),
		Changed:     flag.Changed,
	}, nil
}

// GetStringFlag gets a string flag value
func (ctx *cliContext) GetStringFlag(name string) (string, error) {
	return ctx.cobra.Flags().GetString(name)
}

// GetIntFlag gets an integer flag value
func (ctx *cliContext) GetIntFlag(name string) (int, error) {
	return ctx.cobra.Flags().GetInt(name)
}

// GetBoolFlag gets a boolean flag value
func (ctx *cliContext) GetBoolFlag(name string) (bool, error) {
	return ctx.cobra.Flags().GetBool(name)
}

// Output returns the output writer
func (ctx *cliContext) Output() io.Writer {
	return ctx.app.output
}

// ErrorOutput returns the error output writer
func (ctx *cliContext) ErrorOutput() io.Writer {
	return ctx.app.output // For now, use same output
}
