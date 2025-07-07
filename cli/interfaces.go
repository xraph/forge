package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/plugins"
)

// Manager represents the CLI command manager interface
type CLIManager interface {
	// Initialization
	Initialize() error
	IsInitialized() bool

	// Command execution
	Execute() error
	ExecuteWithContext(ctx context.Context) error

	// Command management
	AddCommand(cmd *cobra.Command)
	AddCommands(commands ...*cobra.Command)
	RemoveCommand(name string)
	GetCommand(name string) *cobra.Command
	GetRootCommand() *cobra.Command
	ListCommands() map[string]*cobra.Command

	// Configuration
	GetVersion() string
	GetAppName() string
	SetLogger(logger logger.Logger)
	GetLogger() logger.Logger
}

// CommandFactory represents a factory for creating CLI commands
type CommandFactory interface {
	// Built-in commands
	CreateServerCommand() *cobra.Command
	CreateDatabaseCommand() *cobra.Command
	CreateMigrateCommand() *cobra.Command
	CreateHealthCommand() *cobra.Command
	CreateConfigCommand() *cobra.Command
	CreateJobsCommand() *cobra.Command
	CreatePluginCommand(pluginManager plugins.Manager) *cobra.Command
	CreateVersionCommand() *cobra.Command
	CreateCompletionCommand(rootCmd *cobra.Command) *cobra.Command

	// Custom commands
	CreateCustomCommand(definition CommandDefinition) *cobra.Command
	CreateCommandGroup(name, description string) *cobra.Command
}

// CommandDefinition represents a command definition
type CommandDefinition struct {
	Name       string
	Short      string
	Long       string
	Use        string
	Example    string
	Aliases    []string
	Hidden     bool
	Deprecated string
	Args       cobra.PositionalArgs
	ValidArgs  []string
	ArgAliases []string

	// Execution
	RunE               func(cmd *cobra.Command, args []string) error
	PreRunE            func(cmd *cobra.Command, args []string) error
	PostRunE           func(cmd *cobra.Command, args []string) error
	PersistentPreRunE  func(cmd *cobra.Command, args []string) error
	PersistentPostRunE func(cmd *cobra.Command, args []string) error

	// Flags
	Flags           []FlagDefinition
	PersistentFlags []FlagDefinition

	// Subcommands
	Subcommands []CommandDefinition

	// Behavior
	SilenceErrors              bool
	SilenceUsage               bool
	DisableFlagsInUseLine      bool
	DisableSuggestions         bool
	SuggestionsMinimumDistance int

	// Completion
	ValidArgsFunction          func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
	RegisterFlagCompletionFunc map[string]func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

	// Annotations
	Annotations map[string]string

	// Version
	Version string
}

// FlagDefinition represents a flag definition
type FlagDefinition struct {
	Name                string
	Shorthand           string
	Usage               string
	Value               interface{}
	DefaultValue        interface{}
	Required            bool
	Hidden              bool
	Deprecated          string
	ShorthandDeprecated string
	Persistent          bool

	// Type-specific properties
	Type        FlagType
	Options     []string    // For choice flags
	Min, Max    interface{} // For numeric flags
	NoOptDefVal string      // For bool flags that can be used without value

	// Completion
	CompletionFunc func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

	// Validation
	ValidateFunc func(value interface{}) error
}

// FlagType represents the type of a flag
type FlagType int

const (
	FlagTypeString FlagType = iota
	FlagTypeInt
	FlagTypeInt8
	FlagTypeInt16
	FlagTypeInt32
	FlagTypeInt64
	FlagTypeUint
	FlagTypeUint8
	FlagTypeUint16
	FlagTypeUint32
	FlagTypeUint64
	FlagTypeFloat32
	FlagTypeFloat64
	FlagTypeBool
	FlagTypeDuration
	FlagTypeStringSlice
	FlagTypeIntSlice
	FlagTypeCount
	FlagTypeIP
	FlagTypeIPMask
	FlagTypeIPNet
)

// CommandGroup represents a group of related commands
type CommandGroup struct {
	Name        string
	Description string
	Commands    []*cobra.Command
	Priority    int
	Hidden      bool
}

// ExecutionContext represents the context for command execution
type ExecutionContext struct {
	Context     context.Context
	Application interface{}
	Container   core.Container
	Logger      logger.Logger
	Config      *Config
	StartTime   time.Time
	Timeout     time.Duration
}

// ExecutionResult represents the result of command execution
type ExecutionResult struct {
	Success  bool
	ExitCode int
	Duration time.Duration
	Output   string
	Error    error
	Metrics  map[string]interface{}
}

// Middleware represents CLI middleware
type Middleware interface {
	// Middleware execution
	Execute(ctx *ExecutionContext, next func(*ExecutionContext) error) error

	// Middleware metadata
	Name() string
	Priority() int
	Enabled() bool
}

// MiddlewareStack represents a stack of CLI middleware
type MiddlewareStack interface {
	// Middleware management
	Add(middleware Middleware)
	Remove(name string)
	Get(name string) Middleware
	List() []Middleware

	// Execution
	Execute(ctx *ExecutionContext, handler func(*ExecutionContext) error) error
}

// CLIPlugin represents a CLI plugin
type CLIPlugin interface {
	// Plugin metadata
	Name() string
	Version() string
	Description() string

	// CLI integration
	RegisterCommands(manager CLIManager) error
	RegisterMiddleware(stack MiddlewareStack) error
	RegisterCompletion(rootCmd *cobra.Command) error

	// Lifecycle
	Initialize(ctx *ExecutionContext) error
	Cleanup(ctx *ExecutionContext) error
}

// Completion represents command completion functionality
type Completion interface {
	// Completion registration
	RegisterCommandCompletion(cmd *cobra.Command, completionFunc func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective))
	RegisterFlagCompletion(cmd *cobra.Command, flagName string, completionFunc func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective))

	// Completion generation
	GenerateCompletion(shell string, writer interface{}) error

	// Completion types
	NoCompletion() ([]string, cobra.ShellCompDirective)
	FileCompletion(extensions []string) ([]string, cobra.ShellCompDirective)
	DirCompletion() ([]string, cobra.ShellCompDirective)
	FixedCompletion(values []string) ([]string, cobra.ShellCompDirective)
	DynamicCompletion(completionFunc func() []string) ([]string, cobra.ShellCompDirective)
}

// Help represents help functionality
type Help interface {
	// Help generation
	GenerateHelp(cmd *cobra.Command) string
	GenerateUsage(cmd *cobra.Command) string
	GenerateExamples(cmd *cobra.Command) string

	// Help customization
	SetHelpTemplate(template string)
	SetUsageTemplate(template string)
	SetHelpFunction(helpFunc func(*cobra.Command, []string))
	SetUsageFunction(usageFunc func(*cobra.Command) error)

	// Help formatting
	FormatHelp(content string) string
	FormatUsage(content string) string
}

// Validation represents command validation functionality
type Validation interface {
	// Argument validation
	ValidateArgs(cmd *cobra.Command, args []string) error
	ValidateRequiredFlags(cmd *cobra.Command) error
	ValidateFlagValues(cmd *cobra.Command) error

	// Custom validation
	RegisterValidator(name string, validator func(interface{}) error)
	ValidateWithValidator(name string, value interface{}) error

	// Validation rules
	Required() func(interface{}) error
	MinLength(min int) func(string) error
	MaxLength(max int) func(string) error
	Range(min, max interface{}) func(interface{}) error
	OneOf(options []interface{}) func(interface{}) error
	Email() func(string) error
	URL() func(string) error
	Pattern(pattern string) func(string) error
}

// Output represents output formatting functionality
type Output interface {
	// Output formatting
	FormatOutput(data interface{}, format string) (string, error)
	FormatTable(headers []string, rows [][]string) string
	FormatList(items []interface{}) string
	FormatJSON(data interface{}) (string, error)
	FormatYAML(data interface{}) (string, error)
	FormatXML(data interface{}) (string, error)
	FormatCSV(headers []string, rows [][]string) string

	// Output styling
	Style(text string, style string) string
	Color(text string, color string) string
	Bold(text string) string
	Italic(text string) string
	Underline(text string) string

	// Progress indicators
	ProgressBar(current, total int) string
	Spinner(message string) interface{}

	// Interactive output
	Prompt(message string) (string, error)
	Confirm(message string) (bool, error)
	Select(message string, options []string) (string, error)
	MultiSelect(message string, options []string) ([]string, error)
}

// Configuration types

// Config represents CLI configuration
type Config struct {
	// Application info
	AppName        string `mapstructure:"app_name" yaml:"app_name" json:"app_name"`
	Version        string `mapstructure:"version" yaml:"version" json:"version"`
	Description    string `mapstructure:"description" yaml:"description" json:"description"`
	DefaultCommand string `mapstructure:"default_command" yaml:"default_command" json:"default_command"`

	// Configuration
	ConfigFile  string   `mapstructure:"config_file" yaml:"config_file" json:"config_file"`
	ConfigPaths []string `mapstructure:"config_paths" yaml:"config_paths" json:"config_paths"`

	// Logging
	LogLevel  string `mapstructure:"log_level" yaml:"log_level" json:"log_level"`
	LogFormat string `mapstructure:"log_format" yaml:"log_format" json:"log_format"`
	LogOutput string `mapstructure:"log_output" yaml:"log_output" json:"log_output"`

	// Execution
	Timeout    time.Duration `mapstructure:"timeout" yaml:"timeout" json:"timeout"`
	MaxRetries int           `mapstructure:"max_retries" yaml:"max_retries" json:"max_retries"`
	RetryDelay time.Duration `mapstructure:"retry_delay" yaml:"retry_delay" json:"retry_delay"`

	// Features
	EnableBuiltins bool `mapstructure:"enable_builtins" yaml:"enable_builtins" json:"enable_builtins"`
	EnablePlugins  bool `mapstructure:"enable_plugins" yaml:"enable_plugins" json:"enable_plugins"`
	EnableColors   bool `mapstructure:"enable_colors" yaml:"enable_colors" json:"enable_colors"`
	EnableTiming   bool `mapstructure:"enable_timing" yaml:"enable_timing" json:"enable_timing"`
	EnableMetrics  bool `mapstructure:"enable_metrics" yaml:"enable_metrics" json:"enable_metrics"`
	EnableTracing  bool `mapstructure:"enable_tracing" yaml:"enable_tracing" json:"enable_tracing"`

	// UI/UX
	EnableProgress bool `mapstructure:"enable_progress" yaml:"enable_progress" json:"enable_progress"`
	EnableSpinner  bool `mapstructure:"enable_spinner" yaml:"enable_spinner" json:"enable_spinner"`
	EnablePrompts  bool `mapstructure:"enable_prompts" yaml:"enable_prompts" json:"enable_prompts"`

	// Completion
	EnableCompletion bool   `mapstructure:"enable_completion" yaml:"enable_completion" json:"enable_completion"`
	CompletionStyle  string `mapstructure:"completion_style" yaml:"completion_style" json:"completion_style"`

	// Security
	AllowUnsafeCommands bool     `mapstructure:"allow_unsafe_commands" yaml:"allow_unsafe_commands" json:"allow_unsafe_commands"`
	RequireConfirmation []string `mapstructure:"require_confirmation" yaml:"require_confirmation" json:"require_confirmation"`

	// Environment
	Environment string `mapstructure:"environment" yaml:"environment" json:"environment"`
	Profile     string `mapstructure:"profile" yaml:"profile" json:"profile"`

	// Extensions
	Extensions map[string]interface{} `mapstructure:"extensions" yaml:"extensions" json:"extensions"`
}

// Builder represents a CLI builder
type Builder interface {
	// Configuration
	WithConfig(config *Config) Builder
	WithApplication(app interface{}) Builder
	WithContainer(container core.Container) Builder
	WithLogger(logger logger.Logger) Builder
	WithPlugins(plugins plugins.Manager) Builder

	// Command building
	WithCommand(cmd *cobra.Command) Builder
	WithCommands(commands ...*cobra.Command) Builder
	WithCommandGroup(group *CommandGroup) Builder
	WithBuiltinCommands() Builder
	WithPluginCommands() Builder

	// Middleware
	WithMiddleware(middleware ...Middleware) Builder
	WithMiddlewareStack(stack MiddlewareStack) Builder

	// Features
	WithCompletion(completion Completion) Builder
	WithHelp(help Help) Builder
	WithValidation(validation Validation) Builder
	WithOutput(output Output) Builder

	// Building
	Build() (CLIManager, error)
	MustBuild() CLIManager
}

// Error types
var (
	ErrCommandNotFound     = fmt.Errorf("command not found")
	ErrInvalidArguments    = fmt.Errorf("invalid arguments")
	ErrInvalidFlags        = fmt.Errorf("invalid flags")
	ErrCommandFailed       = fmt.Errorf("command failed")
	ErrTimeout             = fmt.Errorf("command timeout")
	ErrPermissionDenied    = fmt.Errorf("permission denied")
	ErrConfigurationError  = fmt.Errorf("configuration error")
	ErrInitializationError = fmt.Errorf("initialization error")
	ErrValidationError     = fmt.Errorf("validation error")
	ErrExecutionError      = fmt.Errorf("execution error")
	ErrPluginError         = fmt.Errorf("plugin error")
	ErrMiddlewareError     = fmt.Errorf("middleware error")
	ErrCompletionError     = fmt.Errorf("completion error")
	ErrOutputError         = fmt.Errorf("output error")
)

// Constants
const (
	// Exit codes
	ExitCodeSuccess          = 0
	ExitCodeGeneralError     = 1
	ExitCodeMisuseShellBuilt = 2
	ExitCodeCannotExecute    = 126
	ExitCodeCommandNotFound  = 127
	ExitCodeInvalidArgument  = 128
	ExitCodeFatalError       = 130
	ExitCodeScriptTerminated = 130

	// Output formats
	OutputFormatJSON  = "json"
	OutputFormatYAML  = "yaml"
	OutputFormatXML   = "xml"
	OutputFormatCSV   = "csv"
	OutputFormatTable = "table"
	OutputFormatList  = "list"
	OutputFormatText  = "text"

	// Colors
	ColorReset     = "\033[0m"
	ColorRed       = "\033[31m"
	ColorGreen     = "\033[32m"
	ColorYellow    = "\033[33m"
	ColorBlue      = "\033[34m"
	ColorPurple    = "\033[35m"
	ColorCyan      = "\033[36m"
	ColorWhite     = "\033[37m"
	ColorBold      = "\033[1m"
	ColorUnderline = "\033[4m"

	// Symbols
	SymbolSuccess = "✅"
	SymbolError   = "❌"
	SymbolWarning = "⚠️"
	SymbolInfo    = "ℹ️"
	SymbolSpinner = "⏳"
	SymbolArrow   = "→"
	SymbolBullet  = "•"
)

// Utility functions

// DefaultConfig returns a default CLI configuration
func DefaultConfig() *Config {
	return &Config{
		AppName:             "forge-app",
		Version:             "1.0.0",
		Description:         "A Forge-based application",
		DefaultCommand:      "server",
		LogLevel:            "info",
		LogFormat:           "console",
		LogOutput:           "stderr",
		Timeout:             30 * time.Second,
		MaxRetries:          3,
		RetryDelay:          time.Second,
		EnableBuiltins:      true,
		EnablePlugins:       true,
		EnableColors:        true,
		EnableTiming:        false,
		EnableMetrics:       false,
		EnableTracing:       false,
		EnableProgress:      true,
		EnableSpinner:       true,
		EnablePrompts:       true,
		EnableCompletion:    true,
		CompletionStyle:     "bash",
		AllowUnsafeCommands: false,
		RequireConfirmation: []string{"delete", "drop", "destroy"},
		Environment:         "development",
		Extensions:          make(map[string]interface{}),
	}
}

// NewBuilder creates a new CLI builder
func NewBuilder() Builder {
	return &cliBuilder{
		config: DefaultConfig(),
	}
}

// cliBuilder implements the Builder interface
type cliBuilder struct {
	config      *Config
	app         interface{}
	container   core.Container
	logger      logger.Logger
	plugins     plugins.Manager
	commands    []*cobra.Command
	groups      []*CommandGroup
	middlewares []Middleware
	completion  Completion
	help        Help
	validation  Validation
	output      Output
}

// Builder implementation methods would go here...
func (b *cliBuilder) WithConfig(config *Config) Builder {
	b.config = config
	return b
}

func (b *cliBuilder) WithApplication(app interface{}) Builder {
	b.app = app
	return b
}

func (b *cliBuilder) WithContainer(container core.Container) Builder {
	b.container = container
	return b
}

func (b *cliBuilder) WithLogger(logger logger.Logger) Builder {
	b.logger = logger
	return b
}

func (b *cliBuilder) WithPlugins(plugins plugins.Manager) Builder {
	b.plugins = plugins
	return b
}

func (b *cliBuilder) WithCommand(cmd *cobra.Command) Builder {
	b.commands = append(b.commands, cmd)
	return b
}

func (b *cliBuilder) WithCommands(commands ...*cobra.Command) Builder {
	b.commands = append(b.commands, commands...)
	return b
}

func (b *cliBuilder) WithCommandGroup(group *CommandGroup) Builder {
	b.groups = append(b.groups, group)
	return b
}

func (b *cliBuilder) WithBuiltinCommands() Builder {
	b.config.EnableBuiltins = true
	return b
}

func (b *cliBuilder) WithPluginCommands() Builder {
	b.config.EnablePlugins = true
	return b
}

func (b *cliBuilder) WithMiddleware(middleware ...Middleware) Builder {
	b.middlewares = append(b.middlewares, middleware...)
	return b
}

func (b *cliBuilder) WithMiddlewareStack(stack MiddlewareStack) Builder {
	// Implementation would set the middleware stack
	return b
}

func (b *cliBuilder) WithCompletion(completion Completion) Builder {
	b.completion = completion
	return b
}

func (b *cliBuilder) WithHelp(help Help) Builder {
	b.help = help
	return b
}

func (b *cliBuilder) WithValidation(validation Validation) Builder {
	b.validation = validation
	return b
}

func (b *cliBuilder) WithOutput(output Output) Builder {
	b.output = output
	return b
}

func (b *cliBuilder) Build() (CLIManager, error) {
	manager, err := NewManager(b.app, b.config)
	if err != nil {
		return nil, err
	}

	// Add commands
	for _, cmd := range b.commands {
		manager.AddCommand(cmd)
	}

	// Additional setup would go here...

	return manager, nil
}

func (b *cliBuilder) MustBuild() CLIManager {
	manager, err := b.Build()
	if err != nil {
		panic(err)
	}
	return manager
}
