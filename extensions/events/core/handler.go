package core

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// EventHandler defines the interface for handling events.
type EventHandler interface {
	// Handle processes an event
	Handle(ctx context.Context, event *Event) error

	// CanHandle checks if this handler can process the event
	CanHandle(event *Event) bool

	// Name returns the handler name
	Name() string
}

// EventHandlerFunc is a function adapter for EventHandler.
type EventHandlerFunc func(ctx context.Context, event *Event) error

// Handle implements EventHandler.
func (f EventHandlerFunc) Handle(ctx context.Context, event *Event) error {
	return f(ctx, event)
}

// CanHandle implements EventHandler - always returns true for function handlers.
func (f EventHandlerFunc) CanHandle(event *Event) bool {
	return true
}

// Name implements EventHandler.
func (f EventHandlerFunc) Name() string {
	return "anonymous-handler"
}

// TypedEventHandler is a handler for specific event types.
type TypedEventHandler struct {
	name        string
	eventTypes  map[string]bool
	handler     EventHandlerFunc
	middleware  []HandlerMiddleware
	retryPolicy *RetryPolicy
	metrics     forge.Metrics
	logger      forge.Logger
}

// NewTypedEventHandler creates a new typed event handler.
func NewTypedEventHandler(name string, eventTypes []string, handler EventHandlerFunc) *TypedEventHandler {
	typeMap := make(map[string]bool)
	for _, eventType := range eventTypes {
		typeMap[eventType] = true
	}

	return &TypedEventHandler{
		name:       name,
		eventTypes: typeMap,
		handler:    handler,
		middleware: make([]HandlerMiddleware, 0),
	}
}

// Handle implements EventHandler.
func (h *TypedEventHandler) Handle(ctx context.Context, event *Event) error {
	if !h.CanHandle(event) {
		return fmt.Errorf("handler %s cannot handle event type %s", h.name, event.Type)
	}

	start := time.Now()

	// Apply middleware chain
	finalHandler := h.handler
	for i := len(h.middleware) - 1; i >= 0; i-- {
		finalHandler = h.middleware[i](finalHandler)
	}

	// Execute with retry policy if configured
	var err error
	if h.retryPolicy != nil {
		err = h.executeWithRetry(ctx, event, finalHandler)
	} else {
		err = finalHandler(ctx, event)
	}

	// Record metrics
	if h.metrics != nil {
		duration := time.Since(start)
		h.metrics.Histogram("forge.events.handler_duration", forge.WithLabel("handler", h.name)).Observe(duration.Seconds())

		if err != nil {
			h.metrics.Counter("forge.events.handler_errors", forge.WithLabel("handler", h.name)).Inc()
		} else {
			h.metrics.Counter("forge.events.handler_success", forge.WithLabel("handler", h.name)).Inc()
		}
	}

	// Log execution
	if h.logger != nil {
		if err != nil {
			h.logger.Error("event handler failed",
				logger.String("handler", h.name),
				logger.String("event_type", event.Type),
				logger.String("event_id", event.ID),
				logger.Error(err),
				logger.Duration("duration", time.Since(start)),
			)
		} else {
			h.logger.Debug("event handler executed",
				logger.String("handler", h.name),
				logger.String("event_type", event.Type),
				logger.String("event_id", event.ID),
				logger.Duration("duration", time.Since(start)),
			)
		}
	}

	return err
}

// CanHandle implements EventHandler.
func (h *TypedEventHandler) CanHandle(event *Event) bool {
	return h.eventTypes[event.Type]
}

// Name implements EventHandler.
func (h *TypedEventHandler) Name() string {
	return h.name
}

// WithMiddleware adds middleware to the handler.
func (h *TypedEventHandler) WithMiddleware(middleware ...HandlerMiddleware) *TypedEventHandler {
	h.middleware = append(h.middleware, middleware...)

	return h
}

// WithRetryPolicy sets the retry policy for the handler.
func (h *TypedEventHandler) WithRetryPolicy(policy *RetryPolicy) *TypedEventHandler {
	h.retryPolicy = policy

	return h
}

// WithMetrics sets the metrics collector for the handler.
func (h *TypedEventHandler) WithMetrics(metrics forge.Metrics) *TypedEventHandler {
	h.metrics = metrics

	return h
}

// WithLogger sets the logger for the handler.
func (h *TypedEventHandler) WithLogger(logger forge.Logger) *TypedEventHandler {
	h.logger = logger

	return h
}

// executeWithRetry executes the handler with retry logic.
func (h *TypedEventHandler) executeWithRetry(ctx context.Context, event *Event, handler EventHandlerFunc) error {
	var lastErr error

	for attempt := 0; attempt <= h.retryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate delay
			delay := h.retryPolicy.CalculateDelay(attempt)

			if h.logger != nil {
				h.logger.Info("retrying event handler",
					logger.String("handler", h.name),
					logger.String("event_id", event.ID),
					logger.Int("attempt", attempt),
					logger.Duration("delay", delay),
				)
			}

			// Wait before retry
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := handler(ctx, event)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !h.retryPolicy.ShouldRetry(err) {
			break
		}
	}

	return lastErr
}

// HandlerMiddleware defines middleware for event handlers.
type HandlerMiddleware func(next EventHandlerFunc) EventHandlerFunc

// LoggingMiddleware creates logging middleware for handlers.
func LoggingMiddleware(l forge.Logger) HandlerMiddleware {
	return func(next EventHandlerFunc) EventHandlerFunc {
		return func(ctx context.Context, event *Event) error {
			start := time.Now()

			l.Info("handling event",
				logger.String("event_type", event.Type),
				logger.String("event_id", event.ID),
				logger.String("aggregate_id", event.AggregateID),
			)

			err := next(ctx, event)
			if err != nil {
				l.Error("event handling failed",
					logger.String("event_type", event.Type),
					logger.String("event_id", event.ID),
					logger.Error(err),
					logger.Duration("duration", time.Since(start)),
				)
			} else {
				l.Info("event handled successfully",
					logger.String("event_type", event.Type),
					logger.String("event_id", event.ID),
					logger.Duration("duration", time.Since(start)),
				)
			}

			return err
		}
	}
}

// MetricsMiddleware creates metrics middleware for handlers.
func MetricsMiddleware(metrics forge.Metrics) HandlerMiddleware {
	return func(next EventHandlerFunc) EventHandlerFunc {
		return func(ctx context.Context, event *Event) error {
			start := time.Now()

			err := next(ctx, event)

			duration := time.Since(start)
			metrics.Histogram("forge.events.handler_execution_time").Observe(duration.Seconds())

			if err != nil {
				metrics.Counter("forge.events.handler_errors", forge.WithLabel("event_type", event.Type)).Inc()
			} else {
				metrics.Counter("forge.events.handler_success", forge.WithLabel("event_type", event.Type)).Inc()
			}

			return err
		}
	}
}

// ValidationMiddleware creates validation middleware for handlers.
func ValidationMiddleware() HandlerMiddleware {
	return func(next EventHandlerFunc) EventHandlerFunc {
		return func(ctx context.Context, event *Event) error {
			if err := event.Validate(); err != nil {
				return fmt.Errorf("invalid event: %w", err)
			}

			return next(ctx, event)
		}
	}
}

// RetryPolicy defines retry behavior for event handlers.
type RetryPolicy struct {
	MaxRetries      int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors []error
	ShouldRetryFunc func(error) bool
}

// NewRetryPolicy creates a new retry policy.
func NewRetryPolicy(maxRetries int, initialDelay time.Duration) *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:    maxRetries,
		InitialDelay:  initialDelay,
		MaxDelay:      time.Minute * 5,
		BackoffFactor: 2.0,
	}
}

// WithMaxDelay sets the maximum delay between retries.
func (rp *RetryPolicy) WithMaxDelay(maxDelay time.Duration) *RetryPolicy {
	rp.MaxDelay = maxDelay

	return rp
}

// WithBackoffFactor sets the backoff factor.
func (rp *RetryPolicy) WithBackoffFactor(factor float64) *RetryPolicy {
	rp.BackoffFactor = factor

	return rp
}

// WithRetryableErrors sets specific errors that should trigger retries.
func (rp *RetryPolicy) WithRetryableErrors(errors ...error) *RetryPolicy {
	rp.RetryableErrors = errors

	return rp
}

// WithShouldRetryFunc sets a custom function to determine if an error should trigger a retry.
func (rp *RetryPolicy) WithShouldRetryFunc(fn func(error) bool) *RetryPolicy {
	rp.ShouldRetryFunc = fn

	return rp
}

// CalculateDelay calculates the delay for a retry attempt.
func (rp *RetryPolicy) CalculateDelay(attempt int) time.Duration {
	delay := rp.InitialDelay
	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * rp.BackoffFactor)
		if delay > rp.MaxDelay {
			delay = rp.MaxDelay

			break
		}
	}

	return delay
}

// ShouldRetry determines if an error should trigger a retry.
func (rp *RetryPolicy) ShouldRetry(err error) bool {
	if rp.ShouldRetryFunc != nil {
		return rp.ShouldRetryFunc(err)
	}

	// Check for specific retryable errors
	if slices.Contains(rp.RetryableErrors, err) {
		return true
	}

	// Default: retry on most errors except context cancellation
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// HandlerRegistry manages event handlers.
type HandlerRegistry struct {
	handlers map[string][]EventHandler
	mu       sync.RWMutex
	logger   forge.Logger
	metrics  forge.Metrics
}

// NewHandlerRegistry creates a new handler registry.
func NewHandlerRegistry(logger forge.Logger, metrics forge.Metrics) *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string][]EventHandler),
		logger:   logger,
		metrics:  metrics,
	}
}

// Register registers a handler for specific event types.
func (hr *HandlerRegistry) Register(eventType string, handler EventHandler) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	if hr.handlers[eventType] == nil {
		hr.handlers[eventType] = make([]EventHandler, 0)
	}

	hr.handlers[eventType] = append(hr.handlers[eventType], handler)

	if hr.logger != nil {
		hr.logger.Info("event handler registered",
			logger.String("event_type", eventType),
			logger.String("handler", handler.Name()),
		)
	}

	if hr.metrics != nil {
		hr.metrics.Counter("forge.events.handlers_registered").Inc()
	}

	return nil
}

// Unregister removes a handler for an event type.
func (hr *HandlerRegistry) Unregister(eventType string, handlerName string) error {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	handlers, exists := hr.handlers[eventType]
	if !exists {
		return fmt.Errorf("no handlers registered for event type %s", eventType)
	}

	for i, handler := range handlers {
		if handler.Name() == handlerName {
			hr.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)

			if hr.logger != nil {
				hr.logger.Info("event handler unregistered",
					logger.String("event_type", eventType),
					logger.String("handler", handlerName),
				)
			}

			if hr.metrics != nil {
				hr.metrics.Counter("forge.events.handlers_unregistered").Inc()
			}

			return nil
		}
	}

	return fmt.Errorf("handler %s not found for event type %s", handlerName, eventType)
}

// GetHandlers returns all handlers for an event type.
func (hr *HandlerRegistry) GetHandlers(eventType string) []EventHandler {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	handlers, exists := hr.handlers[eventType]
	if !exists {
		return nil
	}

	// Return a copy to avoid concurrent modification
	result := make([]EventHandler, len(handlers))
	copy(result, handlers)

	return result
}

// GetAllHandlers returns all registered handlers.
func (hr *HandlerRegistry) GetAllHandlers() map[string][]EventHandler {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	result := make(map[string][]EventHandler)
	for eventType, handlers := range hr.handlers {
		result[eventType] = make([]EventHandler, len(handlers))
		copy(result[eventType], handlers)
	}

	return result
}

// HandleEvent dispatches an event to all registered handlers.
func (hr *HandlerRegistry) HandleEvent(ctx context.Context, event *Event) error {
	handlers := hr.GetHandlers(event.Type)
	if len(handlers) == 0 {
		if hr.logger != nil {
			hr.logger.Warn("no handlers registered for event type",
				logger.String("event_type", event.Type),
				logger.String("event_id", event.ID),
			)
		}

		return nil
	}

	var wg sync.WaitGroup

	errorChan := make(chan error, len(handlers))

	// Execute handlers concurrently
	for _, handler := range handlers {
		if !handler.CanHandle(event) {
			continue
		}

		wg.Add(1)

		go func(h EventHandler) {
			defer wg.Done()

			if err := h.Handle(ctx, event); err != nil {
				errorChan <- fmt.Errorf("handler %s failed: %w", h.Name(), err)
			}
		}(handler)
	}

	wg.Wait()
	close(errorChan)

	// Collect errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("handler execution failed: %v", errors)
	}

	return nil
}

// Stats returns handler registry statistics.
func (hr *HandlerRegistry) Stats() map[string]any {
	hr.mu.RLock()
	defer hr.mu.RUnlock()

	stats := map[string]any{
		"total_event_types": len(hr.handlers),
		"total_handlers":    0,
		"handlers_by_type":  make(map[string]int),
	}

	totalHandlers := 0
	handlersByType := make(map[string]int)

	for eventType, handlers := range hr.handlers {
		count := len(handlers)
		totalHandlers += count
		handlersByType[eventType] = count
	}

	stats["total_handlers"] = totalHandlers
	stats["handlers_by_type"] = handlersByType

	return stats
}

// DomainEventHandler is a specialized handler for domain events.
type DomainEventHandler struct {
	*TypedEventHandler

	aggregateType string
}

// NewDomainEventHandler creates a new domain event handler.
func NewDomainEventHandler(name, aggregateType string, eventTypes []string, handler EventHandlerFunc) *DomainEventHandler {
	typedHandler := NewTypedEventHandler(name, eventTypes, handler)

	return &DomainEventHandler{
		TypedEventHandler: typedHandler,
		aggregateType:     aggregateType,
	}
}

// CanHandle implements EventHandler with aggregate type checking.
func (dh *DomainEventHandler) CanHandle(event *Event) bool {
	if !dh.TypedEventHandler.CanHandle(event) {
		return false
	}

	// Additional check for aggregate type if specified
	if dh.aggregateType != "" {
		aggregateType, exists := event.GetMetadata("aggregate_type")
		if !exists {
			return false
		}

		return aggregateType == dh.aggregateType
	}

	return true
}

// ReflectionEventHandler uses reflection to call methods based on event type.
type ReflectionEventHandler struct {
	name    string
	target  any
	methods map[string]reflect.Method
	logger  logger.Logger
	metrics shared.Metrics
}

// NewReflectionEventHandler creates a new reflection-based event handler.
func NewReflectionEventHandler(name string, target any) *ReflectionEventHandler {
	handler := &ReflectionEventHandler{
		name:    name,
		target:  target,
		methods: make(map[string]reflect.Method),
	}

	// Discover handler methods
	handler.discoverMethods()

	return handler
}

// discoverMethods discovers handler methods using reflection.
func (rh *ReflectionEventHandler) discoverMethods() {
	targetType := reflect.TypeOf(rh.target)
	// targetValue := reflect.ValueOf(rh.target)

	for i := 0; i < targetType.NumMethod(); i++ {
		method := targetType.Method(i)

		// Check if method follows the naming convention: Handle<EventType>
		if method.Name[:6] == "Handle" && len(method.Name) > 6 {
			eventType := method.Name[6:] // Remove "Handle" prefix
			rh.methods[eventType] = method
		}
	}
}

// Handle implements EventHandler using reflection.
func (rh *ReflectionEventHandler) Handle(ctx context.Context, event *Event) error {
	// Find method for event type
	method, exists := rh.methods[event.Type]
	if !exists {
		return fmt.Errorf("no handler method found for event type %s", event.Type)
	}

	// Call method using reflection
	targetValue := reflect.ValueOf(rh.target)
	args := []reflect.Value{
		reflect.ValueOf(ctx),
		reflect.ValueOf(event),
	}

	results := method.Func.Call(append([]reflect.Value{targetValue}, args...))

	// Check for error return
	if len(results) > 0 {
		if err, ok := results[0].Interface().(error); ok && err != nil {
			return err
		}
	}

	return nil
}

// CanHandle implements EventHandler.
func (rh *ReflectionEventHandler) CanHandle(event *Event) bool {
	_, exists := rh.methods[event.Type]

	return exists
}

// Name implements EventHandler.
func (rh *ReflectionEventHandler) Name() string {
	return rh.name
}
