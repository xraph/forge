package cli

import (
	"errors"
	"fmt"
)

// Common exit codes
const (
	ExitSuccess       = 0
	ExitError         = 1
	ExitUsageError    = 2
	ExitNotFound      = 127
	ExitUnauthorized  = 126
	ExitInternalError = 3
)

var (
	// ErrCommandNotFound is returned when a command is not found
	ErrCommandNotFound = errors.New("command not found")

	// ErrFlagRequired is returned when a required flag is missing
	ErrFlagRequired = errors.New("required flag missing")

	// ErrFlagInvalid is returned when a flag value is invalid
	ErrFlagInvalid = errors.New("invalid flag value")

	// ErrInvalidArguments is returned when arguments are invalid
	ErrInvalidArguments = errors.New("invalid arguments")

	// ErrPluginNotFound is returned when a plugin is not found
	ErrPluginNotFound = errors.New("plugin not found")

	// ErrPluginAlreadyRegistered is returned when trying to register a duplicate plugin
	ErrPluginAlreadyRegistered = errors.New("plugin already registered")

	// ErrCircularDependency is returned when plugins have circular dependencies
	ErrCircularDependency = errors.New("circular plugin dependency detected")
)

// CLIError represents a CLI-specific error with an exit code
type CLIError struct {
	Message  string
	ExitCode int
	Cause    error
}

func (e *CLIError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *CLIError) Unwrap() error {
	return e.Cause
}

// NewError creates a new CLI error
func NewError(message string, exitCode int) *CLIError {
	return &CLIError{
		Message:  message,
		ExitCode: exitCode,
	}
}

// WrapError wraps an existing error with a CLI error
func WrapError(err error, message string, exitCode int) *CLIError {
	return &CLIError{
		Message:  message,
		ExitCode: exitCode,
		Cause:    err,
	}
}

// GetExitCode extracts the exit code from an error
func GetExitCode(err error) int {
	if err == nil {
		return ExitSuccess
	}

	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return cliErr.ExitCode
	}

	return ExitError
}

// FormatError formats an error for display
func FormatError(err error, colors bool) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	if colors {
		return Red("Error: ") + msg
	}
	return "Error: " + msg
}
