package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Command represents an enhanced command with service injection
type Command struct {
	// Cobra command properties
	Use     string
	Short   string
	Long    string
	Example string
	Args    cobra.PositionalArgs

	// Forge-specific properties
	Services    []interface{}     // Services to inject
	Middleware  []CLIMiddleware   // Command-specific middleware
	Config      *CommandConfig    // Command configuration
	Flags       []*FlagDefinition // Command flags
	Subcommands []*Command        // Subcommands

	// Execution
	Run     func(ctx CLIContext, args []string) error
	PreRun  func(ctx CLIContext, args []string) error
	PostRun func(ctx CLIContext, args []string) error

	// Completion
	ValidArgsFunction func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
	FlagCompletions   map[string]func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)

	// Hidden properties
	hidden     bool
	Deprecated string
}

// CommandConfig contains configuration for a command
type CommandConfig struct {
	RequireAuth   bool              `yaml:"require_auth"`
	RequiredFlags []string          `yaml:"required_flags"`
	OutputFormats []string          `yaml:"output_formats"`
	Timeout       time.Duration     `yaml:"timeout"`
	RetryPolicy   *RetryPolicy      `yaml:"retry_policy"`
	Metadata      map[string]string `yaml:"metadata"`
}

// RetryPolicy defines retry behavior for commands
type RetryPolicy struct {
	MaxAttempts  int           `yaml:"max_attempts"`
	InitialDelay time.Duration `yaml:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay"`
	Multiplier   float64       `yaml:"multiplier"`
}

// FlagDefinition defines a command line flag
type FlagDefinition struct {
	Name       string
	Shorthand  string
	Usage      string
	Value      interface{}
	Default    interface{}
	Required   bool
	Hidden     bool
	Deprecated string
	persistent bool
	Completion func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)
}

// commandWrapper wraps a Command with execution context
type commandWrapper struct {
	*Command
	app          *cliApp
	cobraCommand *cobra.Command
	middleware   []CLIMiddleware
}

// toCobraCommand converts a Forge Command to a Cobra command
func (cw *commandWrapper) toCobraCommand() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:        cw.Command.Use,
		Short:      cw.Command.Short,
		Long:       cw.Command.Long,
		Example:    cw.Command.Example,
		Args:       cw.Command.Args,
		Hidden:     cw.Command.hidden,
		Deprecated: cw.Command.Deprecated,
	}

	// Set up completion
	if cw.Command.ValidArgsFunction != nil {
		cmd.ValidArgsFunction = cw.Command.ValidArgsFunction
	}

	// Add flags
	if err := cw.addFlags(cmd); err != nil {
		return nil, err
	}

	// Set up execution handlers
	cw.setupExecutionHandlers(cmd)

	// Add subcommands
	if err := cw.addSubcommands(cmd); err != nil {
		return nil, err
	}

	return cmd, nil
}

// addFlags adds flags to the Cobra command
func (cw *commandWrapper) addFlags(cmd *cobra.Command) error {
	for _, flagDef := range cw.Command.Flags {
		var flagSet *pflag.FlagSet
		if flagDef.persistent {
			flagSet = cmd.PersistentFlags()
		} else {
			flagSet = cmd.Flags()
		}

		if err := cw.addFlag(flagSet, flagDef); err != nil {
			return err
		}

		// Mark as required if specified - this must be done on the command, not the flag set
		if flagDef.Required {
			if flagDef.persistent {
				if err := cmd.MarkPersistentFlagRequired(flagDef.Name); err != nil {
					return fmt.Errorf("failed to mark persistent flag %s as required: %w", flagDef.Name, err)
				}
			} else {
				if err := cmd.MarkFlagRequired(flagDef.Name); err != nil {
					return fmt.Errorf("failed to mark flag %s as required: %w", flagDef.Name, err)
				}
			}
		}

		// Set up completion
		if flagDef.Completion != nil {
			if cw.Command.FlagCompletions == nil {
				cw.Command.FlagCompletions = make(map[string]func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective))
			}
			cw.Command.FlagCompletions[flagDef.Name] = flagDef.Completion
		}
	}

	// Add default output format flag
	cw.addDefaultFlags(cmd)

	return nil
}

// addFlag adds a specific flag to the flag set
func (cw *commandWrapper) addFlag(flagSet *pflag.FlagSet, flagDef *FlagDefinition) error {
	switch v := flagDef.Value.(type) {
	case *string:
		if flagDef.Default != nil {
			*v = flagDef.Default.(string)
		}
		flagSet.StringVarP(v, flagDef.Name, flagDef.Shorthand, *v, flagDef.Usage)
	case *int:
		if flagDef.Default != nil {
			*v = flagDef.Default.(int)
		}
		flagSet.IntVarP(v, flagDef.Name, flagDef.Shorthand, *v, flagDef.Usage)
	case *bool:
		if flagDef.Default != nil {
			*v = flagDef.Default.(bool)
		}
		flagSet.BoolVarP(v, flagDef.Name, flagDef.Shorthand, *v, flagDef.Usage)
	case *[]string:
		if flagDef.Default != nil {
			*v = flagDef.Default.([]string)
		}
		flagSet.StringSliceVarP(v, flagDef.Name, flagDef.Shorthand, *v, flagDef.Usage)
	default:
		return fmt.Errorf("unsupported flag type: %T", flagDef.Value)
	}

	// Set flag properties
	flag := flagSet.Lookup(flagDef.Name)
	if flag != nil {
		flag.Hidden = flagDef.Hidden
		flag.Deprecated = flagDef.Deprecated
	}

	return nil
}

// addDefaultFlags adds default flags like output format
func (cw *commandWrapper) addDefaultFlags(cmd *cobra.Command) {
	// Add output format flag
	outputFormat := ""
	cmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "json", "Output format (json, yaml, table)")

	// Add verbose flag
	verbose := false
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	// Add quiet flag
	quiet := false
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Quiet output")
}

// setupExecutionHandlers sets up the execution handlers with middleware support
func (cw *commandWrapper) setupExecutionHandlers(cmd *cobra.Command) {
	// PreRun handler
	if cw.Command.PreRun != nil {
		cmd.PreRunE = func(cobraCmd *cobra.Command, args []string) error {
			ctx := NewCLIContext(cobraCmd, cw.app, args)
			return cw.Command.PreRun(ctx, args)
		}
	}

	// Main run handler with middleware chain
	if cw.Command.Run != nil {
		cmd.RunE = func(cobraCmd *cobra.Command, args []string) error {
			ctx := NewCLIContext(cobraCmd, cw.app, args)

			// Create middleware chain
			chain := &middlewareChain{
				middleware: append(cw.app.middleware, cw.middleware...),
				handler: func() error {
					start := time.Now()
					err := cw.Command.Run(ctx, args)
					duration := time.Since(start)

					// Record metrics
					if cw.app.metrics != nil {
						cw.app.metrics.Counter("cli.command.executed", "command", cw.Command.Use).Inc()
						cw.app.metrics.Timer("cli.command.duration", "command", cw.Command.Use).Record(duration)
						if err != nil {
							cw.app.metrics.Counter("cli.command.errors", "command", cw.Command.Use).Inc()
						}
					}

					return err
				},
			}

			return chain.Execute(ctx)
		}
	} else {
		// If no Run function is defined, set a default RunE that does nothing
		// This prevents the root command from falling back to Help()
		cmd.RunE = func(cobraCmd *cobra.Command, args []string) error {
			// For commands without a Run function, just return nil
			return nil
		}
	}

	// PostRun handler
	if cw.Command.PostRun != nil {
		cmd.PostRunE = func(cobraCmd *cobra.Command, args []string) error {
			ctx := NewCLIContext(cobraCmd, cw.app, args)
			return cw.Command.PostRun(ctx, args)
		}
	}
}

// addSubcommands adds subcommands to the Cobra command
func (cw *commandWrapper) addSubcommands(cmd *cobra.Command) error {
	for _, subCmd := range cw.Command.Subcommands {
		subWrapper := &commandWrapper{
			Command:    subCmd,
			app:        cw.app,
			middleware: cw.middleware, // Inherit parent middleware
		}

		cobraSubCmd, err := subWrapper.toCobraCommand()
		if err != nil {
			return err
		}

		subWrapper.cobraCommand = cobraSubCmd
		cmd.AddCommand(cobraSubCmd)
	}

	return nil
}

// NewCommand creates a new Command with sensible defaults
func NewCommand(use, short string) *Command {
	return &Command{
		Use:         use,
		Short:       short,
		Config:      &CommandConfig{},
		Flags:       make([]*FlagDefinition, 0),
		Subcommands: make([]*Command, 0),
		Services:    make([]interface{}, 0),
		Middleware:  make([]CLIMiddleware, 0),
	}
}

// WithLong sets the long description
func (c *Command) WithLong(long string) *Command {
	c.Long = long
	return c
}

// WithExample sets the example
func (c *Command) WithExample(example string) *Command {
	c.Example = example
	return c
}

// WithArgs sets the argument validation
func (c *Command) WithArgs(args cobra.PositionalArgs) *Command {
	c.Args = args
	return c
}

// WithService adds a service dependency
func (c *Command) WithService(service interface{}) *Command {
	c.Services = append(c.Services, service)
	return c
}

// WithMiddleware adds middleware to the command
func (c *Command) WithMiddleware(middleware ...CLIMiddleware) *Command {
	c.Middleware = append(c.Middleware, middleware...)
	return c
}

// WithFlags adds flags to the command
func (c *Command) WithFlags(flags ...*FlagDefinition) *Command {
	c.Flags = append(c.Flags, flags...)
	return c
}

// WithSubcommands adds subcommands
func (c *Command) WithSubcommands(commands ...*Command) *Command {
	c.Subcommands = append(c.Subcommands, commands...)
	return c
}

// WithConfig sets the command configuration
func (c *Command) WithConfig(config *CommandConfig) *Command {
	c.Config = config
	return c
}

// WithTimeout sets a timeout for the command
func (c *Command) WithTimeout(timeout time.Duration) *Command {
	if c.Config == nil {
		c.Config = &CommandConfig{}
	}
	c.Config.Timeout = timeout
	return c
}

// WithRetry sets retry policy for the command
func (c *Command) WithRetry(policy *RetryPolicy) *Command {
	if c.Config == nil {
		c.Config = &CommandConfig{}
	}
	c.Config.RetryPolicy = policy
	return c
}

// WithRequiredFlags marks flags as required
func (c *Command) WithRequiredFlags(flags ...string) *Command {
	if c.Config == nil {
		c.Config = &CommandConfig{}
	}
	c.Config.RequiredFlags = append(c.Config.RequiredFlags, flags...)
	return c
}

// WithOutputFormats sets supported output formats
func (c *Command) WithOutputFormats(formats ...string) *Command {
	if c.Config == nil {
		c.Config = &CommandConfig{}
	}
	c.Config.OutputFormats = formats
	return c
}

// Hidden hides the command from help
func (c *Command) Hidden() *Command {
	c.hidden = true
	return c
}

// WithDeprecation marks the command as deprecated
func (c *Command) WithDeprecation(message string) *Command {
	c.Deprecated = message
	return c
}

// Flag definition helpers

// StringFlag creates a string flag
func StringFlag(name, shorthand, usage string, required bool) *FlagDefinition {
	value := ""
	return &FlagDefinition{
		Name:      name,
		Shorthand: shorthand,
		Usage:     usage,
		Value:     &value,
		Required:  required,
	}
}

// IntFlag creates an integer flag
func IntFlag(name, shorthand, usage string, required bool) *FlagDefinition {
	value := 0
	return &FlagDefinition{
		Name:      name,
		Shorthand: shorthand,
		Usage:     usage,
		Value:     &value,
		Required:  required,
	}
}

// BoolFlag creates a boolean flag
func BoolFlag(name, shorthand, usage string) *FlagDefinition {
	value := false
	return &FlagDefinition{
		Name:      name,
		Shorthand: shorthand,
		Usage:     usage,
		Value:     &value,
	}
}

// StringSliceFlag creates a string slice flag
func StringSliceFlag(name, shorthand string, value []string, usage string, required bool) *FlagDefinition {

	return &FlagDefinition{
		Name:      name,
		Shorthand: shorthand,
		Usage:     usage,
		Value:     &value,
		Required:  required,
	}
}

// WithDefault sets the default value for a flag
func (f *FlagDefinition) WithDefault(defaultValue interface{}) *FlagDefinition {
	f.Default = defaultValue

	// Set the current value to default
	switch v := f.Value.(type) {
	case *string:
		if str, ok := defaultValue.(string); ok {
			*v = str
		}
	case *int:
		if i, ok := defaultValue.(int); ok {
			*v = i
		}
	case *bool:
		if b, ok := defaultValue.(bool); ok {
			*v = b
		}
	case *[]string:
		if slice, ok := defaultValue.([]string); ok {
			*v = slice
		}
	}

	return f
}

// WithCompletion sets the completion function for a flag
func (f *FlagDefinition) WithCompletion(completion func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)) *FlagDefinition {
	f.Completion = completion
	return f
}

// Persistent makes the flag persistent across subcommands
func (f *FlagDefinition) Persistent() *FlagDefinition {
	f.persistent = true
	return f
}

// Hide hides the flag from help
func (f *FlagDefinition) Hide() *FlagDefinition {
	f.Hidden = true
	return f
}

// Deprecate marks the flag as deprecated
func (f *FlagDefinition) Deprecate(message string) *FlagDefinition {
	f.Deprecated = message
	return f
}
