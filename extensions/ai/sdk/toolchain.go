package sdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// ToolChain provides a fluent API for sequential tool execution with transformations
// and conditional branching. It simplifies common patterns that would otherwise require
// a full workflow definition.
//
// Example:
//
//	result, err := sdk.NewToolChain(registry).
//	    Step("fetch_user", sdk.WithInput(map[string]any{"id": 123})).
//	    Transform(func(ctx *ChainContext, result any) any {
//	        user := result.(map[string]any)
//	        ctx.Set("user_name", user["name"])
//	        return user["id"]
//	    }).
//	    Step("get_orders").
//	    ConditionalStep("send_notification",
//	        sdk.When(func(ctx *ChainContext) bool {
//	            orders := ctx.GetLastResult().([]any)
//	            return len(orders) > 0
//	        }),
//	    ).
//	    OnStepComplete(func(step string, result any) {
//	        log.Printf("Completed: %s", step)
//	    }).
//	    Execute(ctx)
type ToolChain struct {
	registry *ToolRegistry
	logger   forge.Logger
	metrics  forge.Metrics

	steps        []ChainStep
	context      *ChainContext
	errorHandler func(step string, err error) error

	// Callbacks
	onStepStart     func(step string, input map[string]any)
	onStepComplete  func(step string, result any)
	onChainStart    func()
	onChainComplete func(result *ChainResult)

	// Configuration
	timeout         time.Duration
	continueOnError bool
}

// ChainStep represents a single step in the tool chain.
type ChainStep struct {
	// Step identification
	Name     string
	ToolName string
	Version  string

	// Input configuration
	Input       map[string]any
	InputMapper func(ctx *ChainContext) map[string]any

	// Output transformation
	Transformer func(ctx *ChainContext, result any) any

	// Conditional execution
	Condition func(ctx *ChainContext) bool

	// Branching
	OnSuccess  string // Name of next step on success (for non-linear flows)
	OnFailure  string // Name of step to jump to on failure
	SkipOnFail bool   // Skip this step if a previous step failed

	// Parallel execution
	Parallel []ChainStep // Steps to run in parallel
}

// ChainContext provides shared state across chain steps.
type ChainContext struct {
	mu sync.RWMutex

	// Shared data accessible by all steps
	data map[string]any

	// Results from each step
	results map[string]any

	// Execution tracking
	currentStep string
	lastResult  any
	errors      map[string]error

	// Parent context
	ctx context.Context
}

// ChainResult contains the complete result of chain execution.
type ChainResult struct {
	// Final result from the last step
	FinalResult any

	// Results from each step
	StepResults map[string]any

	// Execution metadata
	StepsExecuted int
	StepsSkipped  int
	TotalDuration time.Duration
	StepDurations map[string]time.Duration

	// Errors encountered
	Errors  map[string]error
	Success bool
}

// ChainOption configures a chain step.
type ChainOption func(*ChainStep)

// NewToolChain creates a new tool chain.
func NewToolChain(registry *ToolRegistry) *ToolChain {
	return &ToolChain{
		registry: registry,
		steps:    make([]ChainStep, 0),
		context: &ChainContext{
			data:    make(map[string]any),
			results: make(map[string]any),
			errors:  make(map[string]error),
		},
		timeout: 5 * time.Minute,
	}
}

// WithLogger sets the logger for the chain.
func (c *ToolChain) WithLogger(logger forge.Logger) *ToolChain {
	c.logger = logger
	return c
}

// WithMetrics sets the metrics for the chain.
func (c *ToolChain) WithMetrics(metrics forge.Metrics) *ToolChain {
	c.metrics = metrics
	return c
}

// WithTimeout sets the overall chain timeout.
func (c *ToolChain) WithTimeout(timeout time.Duration) *ToolChain {
	c.timeout = timeout
	return c
}

// ContinueOnError configures the chain to continue even if a step fails.
func (c *ToolChain) ContinueOnError(continue_ bool) *ToolChain {
	c.continueOnError = continue_
	return c
}

// OnError sets a custom error handler.
func (c *ToolChain) OnError(handler func(step string, err error) error) *ToolChain {
	c.errorHandler = handler
	return c
}

// Step adds a tool execution step to the chain.
func (c *ToolChain) Step(toolName string, opts ...ChainOption) *ToolChain {
	step := ChainStep{
		Name:     fmt.Sprintf("step_%d_%s", len(c.steps)+1, toolName),
		ToolName: toolName,
		Version:  "1.0.0",
	}

	for _, opt := range opts {
		opt(&step)
	}

	c.steps = append(c.steps, step)
	return c
}

// NamedStep adds a named step to the chain (useful for conditional jumps).
func (c *ToolChain) NamedStep(name, toolName string, opts ...ChainOption) *ToolChain {
	step := ChainStep{
		Name:     name,
		ToolName: toolName,
		Version:  "1.0.0",
	}

	for _, opt := range opts {
		opt(&step)
	}

	c.steps = append(c.steps, step)
	return c
}

// ConditionalStep adds a step that only executes if the condition is true.
func (c *ToolChain) ConditionalStep(toolName string, condition func(ctx *ChainContext) bool, opts ...ChainOption) *ToolChain {
	opts = append(opts, func(s *ChainStep) {
		s.Condition = condition
	})
	return c.Step(toolName, opts...)
}

// Transform adds a transformer after the last step.
func (c *ToolChain) Transform(transformer func(ctx *ChainContext, result any) any) *ToolChain {
	if len(c.steps) > 0 {
		c.steps[len(c.steps)-1].Transformer = transformer
	}
	return c
}

// ParallelSteps adds multiple steps to run in parallel.
func (c *ToolChain) ParallelSteps(steps ...ChainStep) *ToolChain {
	if len(steps) > 0 {
		parentStep := ChainStep{
			Name:     fmt.Sprintf("parallel_%d", len(c.steps)+1),
			Parallel: steps,
		}
		c.steps = append(c.steps, parentStep)
	}
	return c
}

// OnStepStart registers a callback for step start.
func (c *ToolChain) OnStepStart(fn func(step string, input map[string]any)) *ToolChain {
	c.onStepStart = fn
	return c
}

// OnStepComplete registers a callback for step completion.
func (c *ToolChain) OnStepComplete(fn func(step string, result any)) *ToolChain {
	c.onStepComplete = fn
	return c
}

// OnChainStart registers a callback for chain start.
func (c *ToolChain) OnChainStart(fn func()) *ToolChain {
	c.onChainStart = fn
	return c
}

// OnChainComplete registers a callback for chain completion.
func (c *ToolChain) OnChainComplete(fn func(result *ChainResult)) *ToolChain {
	c.onChainComplete = fn
	return c
}

// Execute runs the tool chain.
func (c *ToolChain) Execute(ctx context.Context) (*ChainResult, error) {
	startTime := time.Now()

	// Apply timeout
	execCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	c.context.ctx = execCtx

	if c.onChainStart != nil {
		c.onChainStart()
	}

	if c.logger != nil {
		c.logger.Debug("Starting tool chain execution",
			F("steps", len(c.steps)),
			F("timeout", c.timeout),
		)
	}

	result := &ChainResult{
		StepResults:   make(map[string]any),
		StepDurations: make(map[string]time.Duration),
		Errors:        make(map[string]error),
		Success:       true,
	}

	// Execute steps
	for i, step := range c.steps {
		select {
		case <-execCtx.Done():
			result.Errors["chain"] = execCtx.Err()
			result.Success = false
			return result, fmt.Errorf("chain execution timed out: %w", execCtx.Err())
		default:
		}

		stepStart := time.Now()
		c.context.currentStep = step.Name

		// Check condition
		if step.Condition != nil && !step.Condition(c.context) {
			if c.logger != nil {
				c.logger.Debug("Skipping step (condition not met)",
					F("step", step.Name),
					F("index", i),
				)
			}
			result.StepsSkipped++
			continue
		}

		// Check if this is a parallel step group
		if len(step.Parallel) > 0 {
			if err := c.executeParallelSteps(execCtx, step.Parallel, result); err != nil {
				if !c.continueOnError {
					result.Success = false
					return result, err
				}
			}
			result.StepsExecuted++
			continue
		}

		// Build input
		input := c.buildStepInput(step)

		if c.onStepStart != nil {
			c.onStepStart(step.Name, input)
		}

		// Execute tool
		toolResult, err := c.registry.ExecuteTool(execCtx, step.ToolName, step.Version, input)

		stepDuration := time.Since(stepStart)
		result.StepDurations[step.Name] = stepDuration

		if err != nil {
			result.Errors[step.Name] = err

			if c.errorHandler != nil {
				err = c.errorHandler(step.Name, err)
			}

			if err != nil && !c.continueOnError {
				result.Success = false
				if c.logger != nil {
					c.logger.Error("Tool chain step failed",
						F("step", step.Name),
						F("tool", step.ToolName),
						F("error", err.Error()),
					)
				}
				return result, fmt.Errorf("step %s failed: %w", step.Name, err)
			}

			if c.logger != nil {
				c.logger.Warn("Tool chain step failed (continuing)",
					F("step", step.Name),
					F("tool", step.ToolName),
					F("error", err.Error()),
				)
			}

			result.StepsExecuted++
			continue
		}

		// Apply transformer if present
		stepResult := toolResult.Result
		if step.Transformer != nil {
			stepResult = step.Transformer(c.context, stepResult)
		}

		// Store result
		c.context.results[step.Name] = stepResult
		c.context.lastResult = stepResult
		result.StepResults[step.Name] = stepResult

		if c.onStepComplete != nil {
			c.onStepComplete(step.Name, stepResult)
		}

		if c.logger != nil {
			c.logger.Debug("Tool chain step completed",
				F("step", step.Name),
				F("tool", step.ToolName),
				F("duration", stepDuration),
			)
		}

		result.StepsExecuted++
	}

	result.FinalResult = c.context.lastResult
	result.TotalDuration = time.Since(startTime)

	if c.onChainComplete != nil {
		c.onChainComplete(result)
	}

	if c.metrics != nil {
		c.metrics.Counter("forge.ai.sdk.toolchain.executions").Inc()
		c.metrics.Histogram("forge.ai.sdk.toolchain.duration").Observe(result.TotalDuration.Seconds())
		c.metrics.Histogram("forge.ai.sdk.toolchain.steps").Observe(float64(result.StepsExecuted))
	}

	if c.logger != nil {
		c.logger.Info("Tool chain completed",
			F("steps_executed", result.StepsExecuted),
			F("steps_skipped", result.StepsSkipped),
			F("duration", result.TotalDuration),
			F("success", result.Success),
		)
	}

	return result, nil
}

// executeParallelSteps executes steps in parallel.
func (c *ToolChain) executeParallelSteps(ctx context.Context, steps []ChainStep, result *ChainResult) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, step := range steps {
		wg.Add(1)
		go func(s ChainStep) {
			defer wg.Done()

			input := c.buildStepInput(s)
			toolResult, err := c.registry.ExecuteTool(ctx, s.ToolName, s.Version, input)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.Errors[s.Name] = err
				if firstErr == nil {
					firstErr = err
				}
				return
			}

			stepResult := toolResult.Result
			if s.Transformer != nil {
				stepResult = s.Transformer(c.context, stepResult)
			}

			c.context.mu.Lock()
			c.context.results[s.Name] = stepResult
			c.context.mu.Unlock()

			result.StepResults[s.Name] = stepResult
		}(step)
	}

	wg.Wait()
	return firstErr
}

// buildStepInput builds the input for a step.
func (c *ToolChain) buildStepInput(step ChainStep) map[string]any {
	// Use input mapper if provided
	if step.InputMapper != nil {
		return step.InputMapper(c.context)
	}

	// Use static input if provided
	if step.Input != nil {
		return step.Input
	}

	// Default: use last result as input
	if c.context.lastResult != nil {
		if m, ok := c.context.lastResult.(map[string]any); ok {
			return m
		}
		return map[string]any{"input": c.context.lastResult}
	}

	return make(map[string]any)
}

// --- ChainContext methods ---

// Set stores a value in the context.
func (cc *ChainContext) Set(key string, value any) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.data[key] = value
}

// Get retrieves a value from the context.
func (cc *ChainContext) Get(key string) any {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.data[key]
}

// GetString retrieves a string value from the context.
func (cc *ChainContext) GetString(key string) string {
	if v := cc.Get(key); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetInt retrieves an int value from the context.
func (cc *ChainContext) GetInt(key string) int {
	if v := cc.Get(key); v != nil {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

// GetBool retrieves a bool value from the context.
func (cc *ChainContext) GetBool(key string) bool {
	if v := cc.Get(key); v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// GetLastResult returns the result of the previous step.
func (cc *ChainContext) GetLastResult() any {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.lastResult
}

// GetStepResult returns the result of a specific step.
func (cc *ChainContext) GetStepResult(stepName string) any {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.results[stepName]
}

// GetError returns the error from a specific step.
func (cc *ChainContext) GetError(stepName string) error {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.errors[stepName]
}

// HasError checks if any step has errored.
func (cc *ChainContext) HasError() bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return len(cc.errors) > 0
}

// Context returns the underlying context.Context.
func (cc *ChainContext) Context() context.Context {
	return cc.ctx
}

// --- Chain options ---

// WithInput sets static input for a step.
func WithInput(input map[string]any) ChainOption {
	return func(s *ChainStep) {
		s.Input = input
	}
}

// WithInputMapper sets a dynamic input mapper for a step.
func WithInputMapper(mapper func(ctx *ChainContext) map[string]any) ChainOption {
	return func(s *ChainStep) {
		s.InputMapper = mapper
	}
}

// WithVersion sets the tool version for a step.
func WithVersion(version string) ChainOption {
	return func(s *ChainStep) {
		s.Version = version
	}
}

// WithStepName sets a custom name for the step.
func WithStepName(name string) ChainOption {
	return func(s *ChainStep) {
		s.Name = name
	}
}

// When creates a condition function for conditional steps.
func When(condition func(ctx *ChainContext) bool) func(ctx *ChainContext) bool {
	return condition
}

// IfPreviousSucceeded creates a condition that checks if the previous step succeeded.
func IfPreviousSucceeded() func(ctx *ChainContext) bool {
	return func(ctx *ChainContext) bool {
		return !ctx.HasError()
	}
}

// IfResultContains creates a condition that checks if the last result contains a key.
func IfResultContains(key string) func(ctx *ChainContext) bool {
	return func(ctx *ChainContext) bool {
		if result, ok := ctx.GetLastResult().(map[string]any); ok {
			_, exists := result[key]
			return exists
		}
		return false
	}
}

// IfContextValue creates a condition based on a context value.
func IfContextValue(key string, expected any) func(ctx *ChainContext) bool {
	return func(ctx *ChainContext) bool {
		return ctx.Get(key) == expected
	}
}

// --- Pipeline helper ---

// Pipeline creates a simple sequential chain with automatic input passing.
// Each tool's output becomes the next tool's input.
func Pipeline(registry *ToolRegistry, tools ...string) *ToolChain {
	chain := NewToolChain(registry)
	for _, tool := range tools {
		chain.Step(tool)
	}
	return chain
}

// --- MapReduce helper ---

// MapReduce executes a tool over multiple inputs and reduces the results.
func MapReduce(
	registry *ToolRegistry,
	toolName string,
	inputs []map[string]any,
	reducer func(results []any) any,
) *ToolChain {
	chain := NewToolChain(registry)

	// Create parallel steps for each input
	parallelSteps := make([]ChainStep, len(inputs))
	for i, input := range inputs {
		parallelSteps[i] = ChainStep{
			Name:     fmt.Sprintf("map_%d", i),
			ToolName: toolName,
			Version:  "1.0.0",
			Input:    input,
		}
	}

	chain.ParallelSteps(parallelSteps...)

	// Add reducer as final transformer
	chain.Transform(func(ctx *ChainContext, _ any) any {
		results := make([]any, len(inputs))
		for i := range inputs {
			results[i] = ctx.GetStepResult(fmt.Sprintf("map_%d", i))
		}
		return reducer(results)
	})

	return chain
}
