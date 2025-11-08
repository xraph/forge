package cli

import (
	"fmt"
	"slices"
	"strings"
)

// command implements the Command interface.
type command struct {
	name        string
	description string
	usage       string
	aliases     []string
	handler     CommandHandler
	flags       []Flag
	subcommands []Command
	parent      Command
	before      []MiddlewareFunc
	after       []MiddlewareFunc
}

// CommandOption is a functional option for configuring commands.
type CommandOption func(*command)

// NewCommand creates a new command.
func NewCommand(name, description string, handler CommandHandler, opts ...CommandOption) Command {
	cmd := &command{
		name:        name,
		description: description,
		handler:     handler,
		flags:       []Flag{},
		subcommands: []Command{},
		before:      []MiddlewareFunc{},
		after:       []MiddlewareFunc{},
	}

	for _, opt := range opts {
		opt(cmd)
	}

	return cmd
}

// Identity methods

func (c *command) Name() string        { return c.name }
func (c *command) Description() string { return c.description }
func (c *command) Usage() string {
	if c.usage != "" {
		return c.usage
	}

	return c.generateUsage()
}
func (c *command) Aliases() []string { return c.aliases }

// generateUsage generates a default usage string.
func (c *command) generateUsage() string {
	parts := []string{c.name}

	if len(c.flags) > 0 {
		parts = append(parts, "[flags]")
	}

	if len(c.subcommands) > 0 {
		parts = append(parts, "[subcommand]")
	}

	return strings.Join(parts, " ")
}

// Execution

func (c *command) Run(ctx CommandContext) error {
	if c.handler == nil {
		return fmt.Errorf("no handler defined for command: %s", c.name)
	}

	// Build middleware chain
	handler := c.handler

	// Apply after middleware (in reverse order)
	for i := len(c.after) - 1; i >= 0; i-- {
		handler = c.after[i](handler)
	}

	// Apply before middleware (in order)
	for i := len(c.before) - 1; i >= 0; i-- {
		handler = c.before[i](handler)
	}

	return handler(ctx)
}

// Subcommands

func (c *command) AddSubcommand(cmd Command) error {
	// Check for duplicate
	if _, exists := c.FindSubcommand(cmd.Name()); exists {
		return fmt.Errorf("subcommand already exists: %s", cmd.Name())
	}

	cmd.SetParent(c)
	c.subcommands = append(c.subcommands, cmd)

	return nil
}

func (c *command) Subcommands() []Command {
	return c.subcommands
}

func (c *command) FindSubcommand(name string) (Command, bool) {
	for _, sub := range c.subcommands {
		if sub.Name() == name {
			return sub, true
		}
		// Check aliases
		if slices.Contains(sub.Aliases(), name) {
			return sub, true
		}
	}

	return nil, false
}

// Flags

func (c *command) Flags() []Flag {
	return c.flags
}

func (c *command) AddFlag(flag Flag) {
	c.flags = append(c.flags, flag)
}

// Middleware

func (c *command) Before(fn MiddlewareFunc) Command {
	c.before = append(c.before, fn)

	return c
}

func (c *command) After(fn MiddlewareFunc) Command {
	c.after = append(c.after, fn)

	return c
}

// Parent

func (c *command) SetParent(parent Command) {
	c.parent = parent
}

func (c *command) Parent() Command {
	return c.parent
}

// Command options

// WithUsage sets a custom usage string.
func WithUsage(usage string) CommandOption {
	return func(c *command) {
		c.usage = usage
	}
}

// WithAliases sets command aliases.
func WithAliases(aliases ...string) CommandOption {
	return func(c *command) {
		c.aliases = aliases
	}
}

// WithFlag adds a flag to the command.
func WithFlag(flag Flag) CommandOption {
	return func(c *command) {
		c.flags = append(c.flags, flag)
	}
}

// WithFlags adds multiple flags to the command.
func WithFlags(flags ...Flag) CommandOption {
	return func(c *command) {
		c.flags = append(c.flags, flags...)
	}
}

// WithSubcommand adds a subcommand.
func WithSubcommand(sub Command) CommandOption {
	return func(c *command) {
		sub.SetParent(c)
		c.subcommands = append(c.subcommands, sub)
	}
}

// WithBefore adds a before middleware.
func WithBefore(fn MiddlewareFunc) CommandOption {
	return func(c *command) {
		c.before = append(c.before, fn)
	}
}

// WithAfter adds an after middleware.
func WithAfter(fn MiddlewareFunc) CommandOption {
	return func(c *command) {
		c.after = append(c.after, fn)
	}
}
