package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	croncore "github.com/xraph/forge/v0/pkg/cron/core"
)

// JobHandler defines the interface for job execution handlers
type JobHandler = croncore.JobHandler

// BaseJobHandler provides a base implementation of JobHandler
type BaseJobHandler struct {
	name        string
	description string
	metadata    map[string]interface{}
}

// NewBaseJobHandler creates a new base job handler
func NewBaseJobHandler(name, description string) *BaseJobHandler {
	return &BaseJobHandler{
		name:        name,
		description: description,
		metadata:    make(map[string]interface{}),
	}
}

// Execute is the default implementation that should be overridden
func (h *BaseJobHandler) Execute(ctx context.Context, job *Job) error {
	return fmt.Errorf("execute method not implemented for handler: %s", h.name)
}

// Validate provides basic validation
func (h *BaseJobHandler) Validate(job *Job) error {
	if job == nil {
		return common.ErrValidationError("job", fmt.Errorf("job cannot be nil"))
	}
	if job.Definition == nil {
		return common.ErrValidationError("job.definition", fmt.Errorf("job definition cannot be nil"))
	}
	return nil
}

// OnSuccess provides default success handling
func (h *BaseJobHandler) OnSuccess(ctx context.Context, job *Job, execution *JobExecution) error {
	// Default implementation - can be overridden
	return nil
}

// OnFailure provides default failure handling
func (h *BaseJobHandler) OnFailure(ctx context.Context, job *Job, execution *JobExecution, err error) error {
	// Default implementation - can be overridden
	return nil
}

// OnTimeout provides default timeout handling
func (h *BaseJobHandler) OnTimeout(ctx context.Context, job *Job, execution *JobExecution) error {
	// Default implementation - can be overridden
	return nil
}

// OnRetry provides default retry handling
func (h *BaseJobHandler) OnRetry(ctx context.Context, job *Job, execution *JobExecution, attempt int) error {
	// Default implementation - can be overridden
	return nil
}

// GetMetadata returns the handler metadata
func (h *BaseJobHandler) GetMetadata() map[string]interface{} {
	metadata := make(map[string]interface{})
	metadata["name"] = h.name
	metadata["description"] = h.description

	// Copy custom metadata
	for k, v := range h.metadata {
		metadata[k] = v
	}

	return metadata
}

// SetMetadata sets custom metadata for the handler
func (h *BaseJobHandler) SetMetadata(key string, value interface{}) {
	h.metadata[key] = value
}

// FunctionJobHandler wraps a simple function as a JobHandler
type FunctionJobHandler struct {
	*BaseJobHandler
	fn func(ctx context.Context, job *Job) error
}

// NewFunctionJobHandler creates a new function-based job handler
func NewFunctionJobHandler(name, description string, fn func(ctx context.Context, job *Job) error) *FunctionJobHandler {
	return &FunctionJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, description),
		fn:             fn,
	}
}

// Execute executes the wrapped function
func (h *FunctionJobHandler) Execute(ctx context.Context, job *Job) error {
	if h.fn == nil {
		return fmt.Errorf("handler function not set")
	}
	return h.fn(ctx, job)
}

// ServiceJobHandler wraps a service method as a JobHandler
type ServiceJobHandler struct {
	*BaseJobHandler
	service    interface{}
	methodName string
}

// NewServiceJobHandler creates a new service-based job handler
func NewServiceJobHandler(name, description string, service interface{}, methodName string) *ServiceJobHandler {
	return &ServiceJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, description),
		service:        service,
		methodName:     methodName,
	}
}

// Execute executes the service method using reflection
func (h *ServiceJobHandler) Execute(ctx context.Context, job *Job) error {
	// This would use reflection to call the service method
	// Implementation would be similar to the router's service handler
	return fmt.Errorf("service method execution not implemented yet")
}

// ChainJobHandler executes multiple handlers in sequence
type ChainJobHandler struct {
	*BaseJobHandler
	handlers []JobHandler
}

// NewChainJobHandler creates a new chain job handler
func NewChainJobHandler(name, description string, handlers ...JobHandler) *ChainJobHandler {
	return &ChainJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, description),
		handlers:       handlers,
	}
}

// Execute executes all handlers in sequence
func (h *ChainJobHandler) Execute(ctx context.Context, job *Job) error {
	for i, handler := range h.handlers {
		if err := handler.Execute(ctx, job); err != nil {
			return fmt.Errorf("handler %d failed: %w", i, err)
		}
	}
	return nil
}

// Validate validates all handlers in the chain
func (h *ChainJobHandler) Validate(job *Job) error {
	if err := h.BaseJobHandler.Validate(job); err != nil {
		return err
	}

	for i, handler := range h.handlers {
		if err := handler.Validate(job); err != nil {
			return fmt.Errorf("handler %d validation failed: %w", i, err)
		}
	}
	return nil
}

// ConditionalJobHandler executes a handler based on a condition
type ConditionalJobHandler struct {
	*BaseJobHandler
	condition func(ctx context.Context, job *Job) bool
	handler   JobHandler
}

// NewConditionalJobHandler creates a new conditional job handler
func NewConditionalJobHandler(name, description string, condition func(ctx context.Context, job *Job) bool, handler JobHandler) *ConditionalJobHandler {
	return &ConditionalJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, description),
		condition:      condition,
		handler:        handler,
	}
}

// Execute executes the handler if the condition is true
func (h *ConditionalJobHandler) Execute(ctx context.Context, job *Job) error {
	if h.condition != nil && !h.condition(ctx, job) {
		return nil // Skip execution
	}
	return h.handler.Execute(ctx, job)
}

// Validate validates the conditional handler
func (h *ConditionalJobHandler) Validate(job *Job) error {
	if err := h.BaseJobHandler.Validate(job); err != nil {
		return err
	}
	return h.handler.Validate(job)
}

// RetryableJobHandler adds retry logic to a handler
type RetryableJobHandler struct {
	*BaseJobHandler
	handler    JobHandler
	maxRetries int
	backoff    BackoffStrategy
}

// BackoffStrategy defines the interface for retry backoff strategies
type BackoffStrategy interface {
	GetDelay(attempt int) time.Duration
	ShouldRetry(attempt int, err error) bool
}

// ExponentialBackoff implements exponential backoff strategy
type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxRetries   int
	Factor       float64
}

// NewExponentialBackoff creates a new exponential backoff strategy
func NewExponentialBackoff(initialDelay, maxDelay time.Duration, maxRetries int, factor float64) *ExponentialBackoff {
	return &ExponentialBackoff{
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		MaxRetries:   maxRetries,
		Factor:       factor,
	}
}

// GetDelay returns the delay for the given attempt
func (eb *ExponentialBackoff) GetDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	delay := float64(eb.InitialDelay)
	for i := 1; i < attempt; i++ {
		delay *= eb.Factor
	}

	if time.Duration(delay) > eb.MaxDelay {
		return eb.MaxDelay
	}

	return time.Duration(delay)
}

// ShouldRetry returns true if the job should be retried
func (eb *ExponentialBackoff) ShouldRetry(attempt int, err error) bool {
	return attempt < eb.MaxRetries
}

// NewRetryableJobHandler creates a new retryable job handler
func NewRetryableJobHandler(name, description string, handler JobHandler, maxRetries int, backoff BackoffStrategy) *RetryableJobHandler {
	return &RetryableJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, description),
		handler:        handler,
		maxRetries:     maxRetries,
		backoff:        backoff,
	}
}

// Execute executes the handler with retry logic
func (h *RetryableJobHandler) Execute(ctx context.Context, job *Job) error {
	var lastErr error

	for attempt := 1; attempt <= h.maxRetries; attempt++ {
		if attempt > 1 {
			// Apply backoff delay
			delay := h.backoff.GetDelay(attempt)
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		err := h.handler.Execute(ctx, job)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if we should retry
		if !h.backoff.ShouldRetry(attempt, err) {
			break
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// Validate validates the retryable handler
func (h *RetryableJobHandler) Validate(job *Job) error {
	if err := h.BaseJobHandler.Validate(job); err != nil {
		return err
	}
	return h.handler.Validate(job)
}

// TimeoutJobHandler adds timeout logic to a handler
type TimeoutJobHandler struct {
	*BaseJobHandler
	handler JobHandler
	timeout time.Duration
}

// NewTimeoutJobHandler creates a new timeout job handler
func NewTimeoutJobHandler(name, description string, handler JobHandler, timeout time.Duration) *TimeoutJobHandler {
	return &TimeoutJobHandler{
		BaseJobHandler: NewBaseJobHandler(name, description),
		handler:        handler,
		timeout:        timeout,
	}
}

// Execute executes the handler with timeout
func (h *TimeoutJobHandler) Execute(ctx context.Context, job *Job) error {
	if h.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.timeout)
		defer cancel()
	}

	return h.handler.Execute(ctx, job)
}

// Validate validates the timeout handler
func (h *TimeoutJobHandler) Validate(job *Job) error {
	if err := h.BaseJobHandler.Validate(job); err != nil {
		return err
	}
	return h.handler.Validate(job)
}

// HandlerRegistry manages job handlers
type HandlerRegistry struct {
	handlers map[string]JobHandler
}

// NewHandlerRegistry creates a new handler registry
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string]JobHandler),
	}
}

// Register registers a job handler
func (hr *HandlerRegistry) Register(name string, handler JobHandler) error {
	if name == "" {
		return common.ErrValidationError("name", fmt.Errorf("handler name cannot be empty"))
	}
	if handler == nil {
		return common.ErrValidationError("handler", fmt.Errorf("handler cannot be nil"))
	}

	if _, exists := hr.handlers[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	hr.handlers[name] = handler
	return nil
}

// Unregister unregisters a job handler
func (hr *HandlerRegistry) Unregister(name string) error {
	if _, exists := hr.handlers[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(hr.handlers, name)
	return nil
}

// Get retrieves a job handler
func (hr *HandlerRegistry) Get(name string) (JobHandler, error) {
	handler, exists := hr.handlers[name]
	if !exists {
		return nil, common.ErrServiceNotFound(name)
	}
	return handler, nil
}

// List returns all registered handlers
func (hr *HandlerRegistry) List() map[string]JobHandler {
	result := make(map[string]JobHandler)
	for name, handler := range hr.handlers {
		result[name] = handler
	}
	return result
}

// HandlerFactory defines the interface for creating job handlers
type HandlerFactory interface {
	CreateHandler(config map[string]interface{}) (JobHandler, error)
	GetHandlerType() string
}

// JobHandlerWrapper wraps a handler with additional functionality
type JobHandlerWrapper struct {
	handler    JobHandler
	middleware []JobMiddleware
	logger     common.Logger
	metrics    common.Metrics
}

// JobMiddleware defines the interface for job middleware
type JobMiddleware interface {
	Handle(ctx context.Context, job *Job, next func(ctx context.Context, job *Job) error) error
}

// NewJobHandlerWrapper creates a new job handler wrapper
func NewJobHandlerWrapper(handler JobHandler, logger common.Logger, metrics common.Metrics) *JobHandlerWrapper {
	return &JobHandlerWrapper{
		handler:    handler,
		middleware: make([]JobMiddleware, 0),
		logger:     logger,
		metrics:    metrics,
	}
}

// AddMiddleware adds middleware to the handler
func (jhw *JobHandlerWrapper) AddMiddleware(middleware JobMiddleware) {
	jhw.middleware = append(jhw.middleware, middleware)
}

// Execute executes the handler with middleware
func (jhw *JobHandlerWrapper) Execute(ctx context.Context, job *Job) error {
	// Create middleware chain
	next := func(ctx context.Context, job *Job) error {
		return jhw.handler.Execute(ctx, job)
	}

	// Apply middleware in reverse order
	for i := len(jhw.middleware) - 1; i >= 0; i-- {
		middleware := jhw.middleware[i]
		currentNext := next
		next = func(ctx context.Context, job *Job) error {
			return middleware.Handle(ctx, job, currentNext)
		}
	}

	return next(ctx, job)
}

// Validate validates the wrapped handler
func (jhw *JobHandlerWrapper) Validate(job *Job) error {
	return jhw.handler.Validate(job)
}

// OnSuccess delegates to the wrapped handler
func (jhw *JobHandlerWrapper) OnSuccess(ctx context.Context, job *Job, execution *JobExecution) error {
	return jhw.handler.OnSuccess(ctx, job, execution)
}

// OnFailure delegates to the wrapped handler
func (jhw *JobHandlerWrapper) OnFailure(ctx context.Context, job *Job, execution *JobExecution, err error) error {
	return jhw.handler.OnFailure(ctx, job, execution, err)
}

// OnTimeout delegates to the wrapped handler
func (jhw *JobHandlerWrapper) OnTimeout(ctx context.Context, job *Job, execution *JobExecution) error {
	return jhw.handler.OnTimeout(ctx, job, execution)
}

// OnRetry delegates to the wrapped handler
func (jhw *JobHandlerWrapper) OnRetry(ctx context.Context, job *Job, execution *JobExecution, attempt int) error {
	return jhw.handler.OnRetry(ctx, job, execution, attempt)
}

// GetMetadata returns the handler metadata
func (jhw *JobHandlerWrapper) GetMetadata() map[string]interface{} {
	return jhw.handler.GetMetadata()
}
