package events

import (
	"time"

	forgeCore "github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/events/core"
)

// EventHandler defines the interface for handling events
type EventHandler = core.EventHandler

// EventHandlerFunc is a function adapter for EventHandler
type EventHandlerFunc = core.EventHandlerFunc

// TypedEventHandler is a handler for specific event types
type TypedEventHandler = core.TypedEventHandler

// NewTypedEventHandler creates a new typed event handler
func NewTypedEventHandler(name string, eventTypes []string, handler EventHandlerFunc) *TypedEventHandler {
	return core.NewTypedEventHandler(name, eventTypes, handler)
}

// HandlerMiddleware defines middleware for event handlers
type HandlerMiddleware = core.HandlerMiddleware

// LoggingMiddleware creates logging middleware for handlers
func LoggingMiddleware(l forgeCore.Logger) HandlerMiddleware {
	return core.LoggingMiddleware(l)
}

// MetricsMiddleware creates metrics middleware for handlers
func MetricsMiddleware(metrics forgeCore.Metrics) HandlerMiddleware {
	return core.MetricsMiddleware(metrics)
}

// ValidationMiddleware creates validation middleware for handlers
func ValidationMiddleware() HandlerMiddleware {
	return core.ValidationMiddleware()
}

// RetryPolicy defines retry behavior for event handlers
type RetryPolicy = core.RetryPolicy

// NewRetryPolicy creates a new retry policy
func NewRetryPolicy(maxRetries int, initialDelay time.Duration) *RetryPolicy {
	return core.NewRetryPolicy(maxRetries, initialDelay)
}

// HandlerRegistry manages event handlers
type HandlerRegistry = core.HandlerRegistry

// NewHandlerRegistry creates a new handler registry
func NewHandlerRegistry(logger forgeCore.Logger, metrics forgeCore.Metrics) *HandlerRegistry {
	return core.NewHandlerRegistry(logger, metrics)
}

// DomainEventHandler is a specialized handler for domain events
type DomainEventHandler = core.DomainEventHandler

// NewDomainEventHandler creates a new domain event handler
func NewDomainEventHandler(name, aggregateType string, eventTypes []string, handler EventHandlerFunc) *DomainEventHandler {
	return core.NewDomainEventHandler(name, aggregateType, eventTypes, handler)
}

// ReflectionEventHandler uses reflection to call methods based on event type
type ReflectionEventHandler = core.ReflectionEventHandler

// NewReflectionEventHandler creates a new reflection-based event handler
func NewReflectionEventHandler(name string, target interface{}) *ReflectionEventHandler {
	return core.NewReflectionEventHandler(name, target)
}
