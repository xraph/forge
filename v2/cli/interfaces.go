package cli

import (
	"context"
	"io"

	"github.com/xraph/forge/v2"
)

// CLI represents a command-line application
type CLI interface {
	// Identity
	Name() string
	Version() string
	Description() string

	// Commands
	AddCommand(cmd Command) error
	Commands() []Command
	Run(args []string) error

	// Global flags
	Flag(name string, opts ...FlagOption) Flag

	// Configuration
	SetOutput(w io.Writer)
	SetErrorHandler(handler func(error))

	// Plugin support
	RegisterPlugin(plugin Plugin) error
	Plugins() []Plugin
}

// Command represents a CLI command
type Command interface {
	// Identity
	Name() string
	Description() string
	Usage() string
	Aliases() []string

	// Execution
	Run(ctx CommandContext) error

	// Subcommands
	AddSubcommand(cmd Command) error
	Subcommands() []Command
	FindSubcommand(name string) (Command, bool)

	// Flags
	Flags() []Flag
	AddFlag(flag Flag)

	// Middleware
	Before(fn MiddlewareFunc) Command
	After(fn MiddlewareFunc) Command

	// Parent (for navigation)
	SetParent(parent Command)
	Parent() Command
}

// CommandContext provides context to command execution
type CommandContext interface {
	// Arguments
	Args() []string
	Arg(index int) string
	NArgs() int

	// Flags
	Flag(name string) FlagValue
	String(name string) string
	Int(name string) int
	Bool(name string) bool
	StringSlice(name string) []string
	Duration(name string) int64

	// Output
	Println(a ...any)
	Printf(format string, a ...any)
	Error(err error)
	Success(msg string)
	Warning(msg string)
	Info(msg string)

	// Input
	Prompt(question string) (string, error)
	Confirm(question string) (bool, error)
	Select(question string, options []string) (string, error)
	MultiSelect(question string, options []string) ([]string, error)

	// Async Input (with spinner feedback)
	SelectAsync(question string, loader OptionsLoader) (string, error)
	MultiSelectAsync(question string, loader OptionsLoader) ([]string, error)
	SelectWithRetry(question string, loader OptionsLoader, maxRetries int) (string, error)
	MultiSelectWithRetry(question string, loader OptionsLoader, maxRetries int) ([]string, error)

	// Progress
	ProgressBar(total int) ProgressBar
	Spinner(message string) Spinner

	// Tables
	Table() TableWriter

	// Context
	Context() context.Context

	// App integration (optional, returns nil if not running with Forge app)
	App() forge.App

	// Command reference
	Command() Command

	// Logger
	Logger() *CLILogger
}

// CommandHandler is a function that handles command execution
type CommandHandler func(ctx CommandContext) error

// MiddlewareFunc is a function that wraps a CommandHandler
type MiddlewareFunc func(next CommandHandler) CommandHandler
