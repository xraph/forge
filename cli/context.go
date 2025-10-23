package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
)

// commandContext implements the CommandContext interface
type commandContext struct {
	ctx    context.Context
	cmd    Command
	args   []string
	flags  map[string]*flagValue
	logger *CLILogger
	app    forge.App
	cli    CLI
}

// newCommandContext creates a new command context
func newCommandContext(ctx context.Context, cmd Command, args []string, flags map[string]*flagValue, logger *CLILogger, app forge.App, cli CLI) *commandContext {
	return &commandContext{
		ctx:    ctx,
		cmd:    cmd,
		args:   args,
		flags:  flags,
		logger: logger,
		app:    app,
		cli:    cli,
	}
}

// Arguments

func (c *commandContext) Args() []string {
	return c.args
}

func (c *commandContext) Arg(index int) string {
	if index < 0 || index >= len(c.args) {
		return ""
	}
	return c.args[index]
}

func (c *commandContext) NArgs() int {
	return len(c.args)
}

// Flags

func (c *commandContext) Flag(name string) FlagValue {
	if fv, ok := c.flags[name]; ok {
		return fv
	}
	// Return empty flag value
	return &flagValue{}
}

func (c *commandContext) String(name string) string {
	return c.Flag(name).String()
}

func (c *commandContext) Int(name string) int {
	return c.Flag(name).Int()
}

func (c *commandContext) Bool(name string) bool {
	return c.Flag(name).Bool()
}

func (c *commandContext) StringSlice(name string) []string {
	return c.Flag(name).StringSlice()
}

func (c *commandContext) Duration(name string) int64 {
	return int64(c.Flag(name).Duration())
}

// Output

func (c *commandContext) Println(a ...any) {
	c.logger.Println(a...)
}

func (c *commandContext) Printf(format string, a ...any) {
	c.logger.Printf(format, a...)
}

func (c *commandContext) Error(err error) {
	c.logger.Error("%s", err.Error())
}

func (c *commandContext) Success(msg string) {
	c.logger.Success("%s", msg)
}

func (c *commandContext) Warning(msg string) {
	c.logger.Warning("%s", msg)
}

func (c *commandContext) Info(msg string) {
	c.logger.Info("%s", msg)
}

// Input

func (c *commandContext) Prompt(question string) (string, error) {
	return prompt(question)
}

func (c *commandContext) Confirm(question string) (bool, error) {
	return confirm(question)
}

func (c *commandContext) Select(question string, options []string) (string, error) {
	return selectPrompt(question, options)
}

func (c *commandContext) MultiSelect(question string, options []string) ([]string, error) {
	return multiSelectPrompt(question, options)
}

// Async Input

func (c *commandContext) SelectAsync(question string, loader OptionsLoader) (string, error) {
	return SelectAsync(c.ctx, question, loader)
}

func (c *commandContext) MultiSelectAsync(question string, loader OptionsLoader) ([]string, error) {
	return MultiSelectAsync(c.ctx, question, loader)
}

func (c *commandContext) SelectWithRetry(question string, loader OptionsLoader, maxRetries int) (string, error) {
	return SelectWithRetry(c.ctx, question, loader, maxRetries)
}

func (c *commandContext) MultiSelectWithRetry(question string, loader OptionsLoader, maxRetries int) ([]string, error) {
	return MultiSelectWithRetry(c.ctx, question, loader, maxRetries)
}

// Progress

func (c *commandContext) ProgressBar(total int) ProgressBar {
	return newProgressBar(total, c.logger.output)
}

func (c *commandContext) Spinner(message string) Spinner {
	return newSpinner(message, c.logger.output)
}

// Tables

func (c *commandContext) Table() TableWriter {
	return newTable(c.logger.output, c.logger.colors)
}

// Context

func (c *commandContext) Context() context.Context {
	return c.ctx
}

// App integration

func (c *commandContext) App() forge.App {
	return c.app
}

// Command reference

func (c *commandContext) Command() Command {
	return c.cmd
}

// Logger

func (c *commandContext) Logger() *CLILogger {
	return c.logger
}

// Helper function to get flag by name or short name
func (c *commandContext) findFlag(name string) (*flagValue, bool) {
	// Try exact match
	if fv, ok := c.flags[name]; ok {
		return fv, true
	}

	// Try to find by short name
	for _, flag := range c.cmd.Flags() {
		if flag.ShortName() == name {
			if fv, ok := c.flags[flag.Name()]; ok {
				return fv, true
			}
		}
	}

	return nil, false
}

// parseFlagsForCommand parses flags for a specific command
func parseFlagsForCommand(cmd Command, args []string) (map[string]*flagValue, []string, error) {
	flagValues := make(map[string]*flagValue)
	remainingArgs := []string{}

	// Initialize all flags with default values
	for _, flag := range cmd.Flags() {
		flagValues[flag.Name()] = &flagValue{
			rawValue: flag.DefaultValue(),
			isSet:    false,
		}
	}

	// Parse command-line arguments
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Check if it's a flag
		if !isFlag(arg) {
			remainingArgs = append(remainingArgs, arg)
			continue
		}

		// Parse the flag
		name, value, hasValue := parseFlag(arg)

		// Find the flag definition
		var flagDef Flag
		for _, f := range cmd.Flags() {
			if f.Name() == name || f.ShortName() == name {
				flagDef = f
				break
			}
		}

		if flagDef == nil {
			return nil, nil, fmt.Errorf("unknown flag: %s", name)
		}

		// Handle boolean flags
		if flagDef.Type() == BoolFlagType {
			flagValues[flagDef.Name()] = &flagValue{
				rawValue: true,
				isSet:    true,
			}
			continue
		}

		// For non-boolean flags, get the value
		if !hasValue {
			// Value should be in the next argument
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("flag requires a value: %s", name)
			}
			i++
			value = args[i]
		}

		// Parse and validate the value
		parsedValue, err := parseValue(value, flagDef.Type())
		if err != nil {
			return nil, nil, fmt.Errorf("invalid value for flag %s: %w", name, err)
		}

		// Validate
		if err := flagDef.Validate(parsedValue); err != nil {
			return nil, nil, fmt.Errorf("validation failed for flag %s: %w", name, err)
		}

		flagValues[flagDef.Name()] = &flagValue{
			rawValue: parsedValue,
			isSet:    true,
		}
	}

	// Check required flags
	for _, flag := range cmd.Flags() {
		if flag.Required() && !flagValues[flag.Name()].IsSet() {
			return nil, nil, fmt.Errorf("required flag missing: %s", flag.Name())
		}
	}

	return flagValues, remainingArgs, nil
}

// parseValue parses a string value according to the flag type
func parseValue(value string, flagType FlagType) (any, error) {
	switch flagType {
	case StringFlagType:
		return value, nil
	case IntFlagType:
		var i int
		_, err := fmt.Sscanf(value, "%d", &i)
		return i, err
	case BoolFlagType:
		return value == "true" || value == "1" || value == "yes", nil
	case StringSliceFlagType:
		// Support comma-separated values
		if value == "" {
			return []string{}, nil
		}
		return []string{value}, nil // Single value, can be called multiple times
	case DurationFlagType:
		d, err := time.ParseDuration(value)
		return d, err
	default:
		return value, nil
	}
}
