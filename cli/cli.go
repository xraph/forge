package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/xraph/forge"
)

// cli implements the CLI interface
type cli struct {
	name         string
	version      string
	description  string
	commands     []Command
	globalFlags  []Flag
	output       io.Writer
	errorHandler func(error)
	logger       *CLILogger
	plugins      []Plugin
	app          forge.App // Optional Forge app integration
}

// Config configures a CLI application
type Config struct {
	Name         string
	Version      string
	Description  string
	Output       io.Writer
	ErrorHandler func(error)
	Logger       *CLILogger
	App          forge.App // Optional Forge app integration
}

// New creates a new CLI application
func New(config Config) CLI {
	if config.Output == nil {
		config.Output = os.Stdout
	}

	if config.Logger == nil {
		config.Logger = NewCLILogger(WithOutput(config.Output))
	}

	if config.ErrorHandler == nil {
		config.ErrorHandler = defaultErrorHandler(config.Logger)
	}

	return &cli{
		name:         config.Name,
		version:      config.Version,
		description:  config.Description,
		output:       config.Output,
		logger:       config.Logger,
		errorHandler: config.ErrorHandler,
		commands:     []Command{},
		globalFlags:  []Flag{},
		plugins:      []Plugin{},
		app:          config.App,
	}
}

// defaultErrorHandler returns a default error handler
func defaultErrorHandler(logger *CLILogger) func(error) {
	return func(err error) {
		if err != nil {
			logger.Error("%s", err.Error())
			os.Exit(GetExitCode(err))
		}
	}
}

// Identity

func (c *cli) Name() string        { return c.name }
func (c *cli) Version() string     { return c.version }
func (c *cli) Description() string { return c.description }

// Commands

func (c *cli) AddCommand(cmd Command) error {
	// Check for duplicate
	for _, existing := range c.commands {
		if existing.Name() == cmd.Name() {
			return fmt.Errorf("command already exists: %s", cmd.Name())
		}
	}

	c.commands = append(c.commands, cmd)
	return nil
}

func (c *cli) Commands() []Command {
	return c.commands
}

// Run executes the CLI application
func (c *cli) Run(args []string) error {
	// Remove program name from args
	if len(args) > 0 {
		args = args[1:]
	}

	// Check for version flag
	if hasVersionFlag(args) {
		c.printVersion()
		return nil
	}

	// Check for help flag with no command
	if len(args) == 0 || (len(args) == 1 && isHelpFlag(args[0])) {
		c.printHelp()
		return nil
	}

	// Parse command path
	commandPath, remainingArgs := parseCommandPath(args)

	// Find the command
	var cmd Command
	var cmdArgs []string

	if len(commandPath) > 0 {
		// Try to find the command
		rootCmd := c.findRootCommand(commandPath[0])
		if rootCmd == nil {
			return NewError(fmt.Sprintf("unknown command: %s", commandPath[0]), ExitUsageError)
		}

		// Navigate to subcommands
		cmd, cmdArgs, _ = findCommand(rootCmd, commandPath[1:])

		// Append remaining args
		cmdArgs = append(cmdArgs, remainingArgs...)
	} else {
		// No command specified
		c.printHelp()
		return nil
	}

	// Check for help flag for specific command
	if hasHelpFlag(cmdArgs) {
		c.printCommandHelp(cmd)
		return nil
	}

	// Parse flags for the command
	flagValues, positionalArgs, err := parseFlagsForCommand(cmd, cmdArgs)
	if err != nil {
		return WrapError(err, "failed to parse flags", ExitUsageError)
	}

	// Create command context
	ctx := newCommandContext(
		context.Background(),
		cmd,
		positionalArgs,
		flagValues,
		c.logger,
		c.app,
		c,
	)

	// Execute the command
	return cmd.Run(ctx)
}

// findRootCommand finds a root-level command by name or alias
func (c *cli) findRootCommand(name string) Command {
	for _, cmd := range c.commands {
		if cmd.Name() == name {
			return cmd
		}
		for _, alias := range cmd.Aliases() {
			if alias == name {
				return cmd
			}
		}
	}
	return nil
}

// Global flags

func (c *cli) Flag(name string, opts ...FlagOption) Flag {
	flag := NewFlag(name, StringFlagType, opts...)
	c.globalFlags = append(c.globalFlags, flag)
	return flag
}

// Configuration

func (c *cli) SetOutput(w io.Writer) {
	c.output = w
	c.logger.SetOutput(w)
}

func (c *cli) SetErrorHandler(handler func(error)) {
	c.errorHandler = handler
}

// Plugin support

func (c *cli) RegisterPlugin(plugin Plugin) error {
	// Check for duplicate
	for _, p := range c.plugins {
		if p.Name() == plugin.Name() {
			return ErrPluginAlreadyRegistered
		}
	}

	// Initialize plugin
	if err := plugin.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize plugin %s: %w", plugin.Name(), err)
	}

	// Add plugin
	c.plugins = append(c.plugins, plugin)

	// Register plugin commands
	for _, cmd := range plugin.Commands() {
		if err := c.AddCommand(cmd); err != nil {
			return fmt.Errorf("failed to add command from plugin %s: %w", plugin.Name(), err)
		}
	}

	c.logger.Debug("Registered plugin: %s (v%s)", plugin.Name(), plugin.Version())

	return nil
}

func (c *cli) Plugins() []Plugin {
	return c.plugins
}

// Helper methods

// printVersion prints the version information
func (c *cli) printVersion() {
	fmt.Fprintf(c.output, "%s version %s\n", c.name, c.version)
}

// printHelp prints the main help message
func (c *cli) printHelp() {
	help := generateHelp(c, nil)
	fmt.Fprint(c.output, help)
}

// printCommandHelp prints help for a specific command
func (c *cli) printCommandHelp(cmd Command) {
	help := generateHelp(c, cmd)
	fmt.Fprint(c.output, help)
}

// hasVersionFlag checks if arguments contain a version flag
func hasVersionFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-v" || arg == "--version" || arg == "version" {
			return true
		}
	}
	return false
}
