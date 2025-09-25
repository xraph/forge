package middleware

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/xraph/forge/pkg/cli"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RecoveryMiddleware provides panic recovery for CLI commands
type RecoveryMiddleware struct {
	*cli.BaseMiddleware
	logger common.Logger
	config RecoveryConfig
}

// RecoveryConfig contains recovery middleware configuration
type RecoveryConfig struct {
	LogPanics        bool                            `yaml:"log_panics" json:"log_panics"`
	PrintStack       bool                            `yaml:"print_stack" json:"print_stack"`
	ExitOnPanic      bool                            `yaml:"exit_on_panic" json:"exit_on_panic"`
	ExitCode         int                             `yaml:"exit_code" json:"exit_code"`
	CustomHandler    func(interface{}, []byte) error `yaml:"-" json:"-"`
	SendToSentry     bool                            `yaml:"send_to_sentry" json:"send_to_sentry"`
	IncludeContext   bool                            `yaml:"include_context" json:"include_context"`
	MaxStackDepth    int                             `yaml:"max_stack_depth" json:"max_stack_depth"`
	FilterGoRoutines bool                            `yaml:"filter_goroutines" json:"filter_goroutines"`
}

// NewRecoveryMiddleware creates a new recovery middleware
func NewRecoveryMiddleware(logger common.Logger) cli.CLIMiddleware {
	return NewRecoveryMiddlewareWithConfig(logger, DefaultRecoveryConfig())
}

// NewRecoveryMiddlewareWithConfig creates recovery middleware with custom config
func NewRecoveryMiddlewareWithConfig(logger common.Logger, config RecoveryConfig) cli.CLIMiddleware {
	return &RecoveryMiddleware{
		BaseMiddleware: cli.NewBaseMiddleware("recovery", 1), // Highest priority
		logger:         logger,
		config:         config,
	}
}

// Execute executes the recovery middleware
func (rm *RecoveryMiddleware) Execute(ctx cli.CLIContext, next func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = rm.handlePanic(ctx, r)
		}
	}()

	return next()
}

// handlePanic handles a recovered panic
func (rm *RecoveryMiddleware) handlePanic(ctx cli.CLIContext, recovered interface{}) error {
	// Get stack trace
	stack := debug.Stack()

	// Filter stack if configured
	if rm.config.FilterGoRoutines {
		stack = rm.filterStack(stack)
	}

	// Limit stack depth if configured
	if rm.config.MaxStackDepth > 0 {
		stack = rm.limitStackDepth(stack, rm.config.MaxStackDepth)
	}

	// Create panic info
	panicInfo := &PanicInfo{
		Recovered: recovered,
		Stack:     stack,
		Command:   ctx.Command().Name(),
		Args:      ctx.Args(),
		Context:   rm.extractContext(ctx),
	}

	// Use custom handler if provided
	if rm.config.CustomHandler != nil {
		if err := rm.config.CustomHandler(recovered, stack); err != nil {
			return err
		}
	}

	// Log panic if enabled
	if rm.config.LogPanics {
		rm.logPanic(panicInfo)
	}

	// Print stack if enabled
	if rm.config.PrintStack {
		rm.printStack(ctx, panicInfo)
	}

	// Send to external services if configured
	if rm.config.SendToSentry {
		rm.sendToSentry(panicInfo)
	}

	// Exit if configured
	if rm.config.ExitOnPanic {
		os.Exit(rm.config.ExitCode)
	}

	// Return a wrapped error
	return common.ErrInternalError("command panicked", fmt.Errorf("panic: %v", recovered))
}

// logPanic logs the panic with context
func (rm *RecoveryMiddleware) logPanic(panicInfo *PanicInfo) {
	fields := []common.LogField{
		logger.String("panic", fmt.Sprintf("%v", panicInfo.Recovered)),
		logger.String("command", panicInfo.Command),
		logger.Strings("args", panicInfo.Args),
	}

	if rm.config.IncludeContext && len(panicInfo.Context) > 0 {
		fields = append(fields, logger.Any("context", panicInfo.Context))
	}

	if rm.config.PrintStack {
		fields = append(fields, logger.String("stack", string(panicInfo.Stack)))
	}

	rm.logger.Error("command panic recovered", fields...)
}

// printStack prints the stack trace to the user
func (rm *RecoveryMiddleware) printStack(ctx cli.CLIContext, panicInfo *PanicInfo) {
	fmt.Fprintf(ctx.Output(), "\n=== PANIC RECOVERED ===\n")
	fmt.Fprintf(ctx.Output(), "Command: %s\n", panicInfo.Command)
	fmt.Fprintf(ctx.Output(), "Panic: %v\n", panicInfo.Recovered)
	fmt.Fprintf(ctx.Output(), "\nStack Trace:\n%s\n", string(panicInfo.Stack))
	fmt.Fprintf(ctx.Output(), "=== END PANIC INFO ===\n\n")
}

// extractContext extracts relevant context information
func (rm *RecoveryMiddleware) extractContext(ctx cli.CLIContext) map[string]interface{} {
	if !rm.config.IncludeContext {
		return nil
	}

	context := make(map[string]interface{})

	// Add common context values
	if userID := ctx.Get("user_id"); userID != nil {
		context["user_id"] = userID
	}

	if requestID := ctx.Get("request_id"); requestID != nil {
		context["request_id"] = requestID
	}

	if sessionID := ctx.Get("session_id"); sessionID != nil {
		context["session_id"] = sessionID
	}

	// Add flag values (be careful with sensitive data)
	flags := make(map[string]string)
	ctx.Command().Flags().VisitAll(func(flag *pflag.Flag) {
		if !rm.isSensitiveFlag(flag.Name) {
			flags[flag.Name] = flag.Value.String()
		} else {
			flags[flag.Name] = "[REDACTED]"
		}
	})
	context["flags"] = flags

	// Add runtime information
	context["go_version"] = runtime.Version()
	context["goos"] = runtime.GOOS
	context["goarch"] = runtime.GOARCH
	context["num_goroutine"] = runtime.NumGoroutine()

	return context
}

// filterStack filters the stack trace to remove irrelevant goroutines
func (rm *RecoveryMiddleware) filterStack(stack []byte) []byte {
	lines := strings.Split(string(stack), "\n")
	var filtered []string

	inRelevantGoroutine := true
	for _, line := range lines {
		// Start of new goroutine
		if strings.HasPrefix(line, "goroutine ") {
			// Check if this goroutine is relevant (contains our code)
			inRelevantGoroutine = rm.isRelevantGoroutine(line)
		}

		if inRelevantGoroutine {
			filtered = append(filtered, line)
		}
	}

	return []byte(strings.Join(filtered, "\n"))
}

// isRelevantGoroutine determines if a goroutine is relevant to show
func (rm *RecoveryMiddleware) isRelevantGoroutine(line string) bool {
	// This is a simplified heuristic
	// You might want to make this more sophisticated based on your needs
	return !strings.Contains(line, "[IO wait]") &&
		!strings.Contains(line, "[GC worker]") &&
		!strings.Contains(line, "[runnable]:")
}

// limitStackDepth limits the stack trace to a maximum number of frames
func (rm *RecoveryMiddleware) limitStackDepth(stack []byte, maxDepth int) []byte {
	lines := strings.Split(string(stack), "\n")
	if len(lines) <= maxDepth*2 { // Each frame is typically 2 lines
		return stack
	}

	// Take first line (goroutine info) plus maxDepth frames
	limited := []string{lines[0]}
	frameCount := 0

	for i := 1; i < len(lines) && frameCount < maxDepth; i += 2 {
		if i+1 < len(lines) {
			limited = append(limited, lines[i], lines[i+1])
			frameCount++
		}
	}

	return []byte(strings.Join(limited, "\n"))
}

// isSensitiveFlag checks if a flag might contain sensitive information
func (rm *RecoveryMiddleware) isSensitiveFlag(flagName string) bool {
	sensitivePatterns := []string{
		"password", "passwd", "pwd",
		"token", "key", "secret",
		"auth", "credential",
	}

	flagLower := strings.ToLower(flagName)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(flagLower, pattern) {
			return true
		}
	}

	return false
}

// sendToSentry sends panic information to Sentry (if configured)
func (rm *RecoveryMiddleware) sendToSentry(panicInfo *PanicInfo) {
	// This would integrate with Sentry SDK
	// Implementation would depend on the Sentry Go SDK
	if rm.logger != nil {
		rm.logger.Debug("panic would be sent to Sentry",
			logger.String("command", panicInfo.Command),
		)
	}
}

// PanicInfo contains information about a panic
type PanicInfo struct {
	Recovered interface{}
	Stack     []byte
	Command   string
	Args      []string
	Context   map[string]interface{}
	Timestamp time.Time
}

// DefaultRecoveryConfig returns default recovery configuration
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		LogPanics:        true,
		PrintStack:       false, // Don't show stack to users by default
		ExitOnPanic:      false,
		ExitCode:         1,
		CustomHandler:    nil,
		SendToSentry:     false,
		IncludeContext:   true,
		MaxStackDepth:    10,
		FilterGoRoutines: true,
	}
}

// DevelopmentRecoveryConfig returns configuration suitable for development
func DevelopmentRecoveryConfig() RecoveryConfig {
	config := DefaultRecoveryConfig()
	config.PrintStack = true
	config.MaxStackDepth = 20
	config.FilterGoRoutines = false
	return config
}

// ProductionRecoveryConfig returns configuration suitable for production
func ProductionRecoveryConfig() RecoveryConfig {
	config := DefaultRecoveryConfig()
	config.PrintStack = false
	config.SendToSentry = true
	config.MaxStackDepth = 10
	config.FilterGoRoutines = true
	return config
}
