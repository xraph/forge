package common

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ForgeError represents a structured error in the Forge framework
type ForgeError struct {
	// Code is a unique error code
	Code string `json:"code"`

	StatusCode int `json:"-"`

	// Message is a human-readable error message
	Message string `json:"message"`

	// Cause is the underlying error that caused this error
	Cause error `json:"cause,omitempty"`

	// Context contains additional context information
	Context map[string]interface{} `json:"context,omitempty"`

	// Timestamp is when the error occurred
	Timestamp time.Time `json:"timestamp"`

	// Stack is the stack trace where the error occurred
	Stack string `json:"stack,omitempty"`

	// Service is the service where the error occurred
	Service string `json:"service,omitempty"`

	// Operation is the operation that failed
	Operation string `json:"operation,omitempty"`
}

// Error implements the error interface
func (e *ForgeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *ForgeError) Unwrap() error {
	return e.Cause
}

// WithContext adds context to the error
func (e *ForgeError) WithContext(key string, value interface{}) *ForgeError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithService sets the service name
func (e *ForgeError) WithService(service string) *ForgeError {
	e.Service = service
	return e
}

// WithOperation sets the operation name
func (e *ForgeError) WithOperation(operation string) *ForgeError {
	e.Operation = operation
	return e
}

// WithStack adds stack trace information
func (e *ForgeError) WithStack() *ForgeError {
	e.Stack = captureStack()
	return e
}

// WithCause sets the underlying cause of the error and returns the updated ForgeError instance.
func (e *ForgeError) WithCause(cause error) *ForgeError {
	e.Cause = cause
	return e
}

// HandleHTTPError sets the underlying cause of the error and returns the updated ForgeError instance.
func (e *ForgeError) HandleHTTPError(statusCode int, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(e)
}

// NewForgeError creates a new ForgeError
func NewForgeError(code, message string, cause error) *ForgeError {
	return &ForgeError{
		Code:      code,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
	}
}

// HandleHTTPError writes the CustomError as a JSON response.
func HandleHTTPError(w http.ResponseWriter, r *http.Request, err error) {
	var customErr *ForgeError
	if errors.As(err, &customErr) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(customErr.StatusCode)
		json.NewEncoder(w).Encode(customErr)
	}
}

// captureStack captures the current stack trace
func captureStack() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

// Common error codes
const (
	ErrCodeServiceNotFound      = "SERVICE_NOT_FOUND"
	ErrCodeServiceAlreadyExists = "SERVICE_ALREADY_EXISTS"
	ErrCodeServiceStartFailed   = "SERVICE_START_FAILED"
	ErrCodeServiceStopFailed    = "SERVICE_STOP_FAILED"
	ErrCodeDependencyNotFound   = "DEPENDENCY_NOT_FOUND"
	ErrCodeCircularDependency   = "CIRCULAR_DEPENDENCY"
	ErrCodeInvalidConfig        = "INVALID_CONFIG"
	ErrCodeHealthCheckFailed    = "HEALTH_CHECK_FAILED"
	ErrCodePluginNotFound       = "PLUGIN_NOT_FOUND"
	ErrCodePluginInitFailed     = "PLUGIN_INIT_FAILED"
	ErrCodeContainerError       = "CONTAINER_ERROR"
	ErrCodeLifecycleError       = "LIFECYCLE_ERROR"
	ErrCodeValidationError      = "VALIDATION_ERROR"
	ErrCodeConfigError          = "CONFIG_ERROR"
	ErrCodeTimeoutError         = "TIMEOUT_ERROR"
	ErrCodeContextCancelled     = "CONTEXT_CANCELLED"
	ErrCodeInternalError        = "INTERNAL_ERROR"
	ErrCodeUnAuthorized         = "UNAUTHORIZED"
)

func ErrServiceUnAuthorized(message string, cause error) *ForgeError {
	return NewForgeError(ErrCodeUnAuthorized, message, cause)
}

func ErrServiceNotFound(serviceName string) *ForgeError {
	return NewForgeError(ErrCodeServiceNotFound, fmt.Sprintf("service '%s' not found", serviceName), nil).
		WithContext("service_name", serviceName)
}

func ErrServiceAlreadyExists(serviceName string) *ForgeError {
	return NewForgeError(ErrCodeServiceAlreadyExists, fmt.Sprintf("service '%s' already exists", serviceName), nil).
		WithContext("service_name", serviceName)
}

func ErrServiceStartFailed(serviceName string, cause error) *ForgeError {
	return NewForgeError(ErrCodeServiceStartFailed, fmt.Sprintf("failed to start service '%s'", serviceName), cause).
		WithContext("service_name", serviceName)
}

func ErrServiceStopFailed(serviceName string, cause error) *ForgeError {
	return NewForgeError(ErrCodeServiceStopFailed, fmt.Sprintf("failed to stop service '%s'", serviceName), cause).
		WithContext("service_name", serviceName)
}

func ErrDependencyNotFound(serviceName, dependency string) *ForgeError {
	return NewForgeError(ErrCodeDependencyNotFound, fmt.Sprintf("dependency '%s' not found for service '%s'", dependency, serviceName), nil).
		WithContext("service_name", serviceName).
		WithContext("dependency", dependency)
}

func ErrCircularDependency(services []string) *ForgeError {
	return NewForgeError(ErrCodeCircularDependency, fmt.Sprintf("circular dependency detected: %s", strings.Join(services, " -> ")), nil).
		WithContext("services", services)
}

func ErrInvalidConfig(configKey string, cause error) *ForgeError {
	return NewForgeError(ErrCodeInvalidConfig, fmt.Sprintf("invalid configuration for key '%s'", configKey), cause).
		WithContext("config_key", configKey)
}

func ErrHealthCheckFailed(serviceName string, cause error) *ForgeError {
	return NewForgeError(ErrCodeHealthCheckFailed, fmt.Sprintf("health check failed for service '%s'", serviceName), cause).
		WithContext("service_name", serviceName)
}

func ErrPluginNotFound(pluginName string) *ForgeError {
	return NewForgeError(ErrCodePluginNotFound, fmt.Sprintf("plugin '%s' not found", pluginName), nil).
		WithContext("plugin_name", pluginName)
}

func ErrPluginInitFailed(pluginName string, cause error) *ForgeError {
	return NewForgeError(ErrCodePluginInitFailed, fmt.Sprintf("failed to initialize plugin '%s'", pluginName), cause).
		WithContext("plugin_name", pluginName)
}

func ErrContainerError(operation string, cause error) *ForgeError {
	return NewForgeError(ErrCodeContainerError, fmt.Sprintf("container error during %s", operation), cause).
		WithContext("operation", operation)
}

func ErrLifecycleError(phase string, cause error) *ForgeError {
	return NewForgeError(ErrCodeLifecycleError, fmt.Sprintf("lifecycle error during %s", phase), cause).
		WithContext("phase", phase)
}

func ErrValidationError(field string, cause error) *ForgeError {
	return NewForgeError(ErrCodeValidationError, fmt.Sprintf("validation error for field '%s'", field), cause).
		WithContext("field", field)
}

func ErrConfigError(message string, cause error) *ForgeError {
	return NewForgeError(ErrCodeConfigError, message, cause)
}

func ErrTimeoutError(operation string, timeout time.Duration) *ForgeError {
	return NewForgeError(ErrCodeTimeoutError, fmt.Sprintf("timeout during %s after %v", operation, timeout), nil).
		WithContext("operation", operation).
		WithContext("timeout", timeout.String())
}

func ErrContextCancelled(operation string) *ForgeError {
	return NewForgeError(ErrCodeContextCancelled, fmt.Sprintf("context cancelled during %s", operation), nil).
		WithContext("operation", operation)
}

func ErrInternalError(message string, cause error) *ForgeError {
	return NewForgeError(ErrCodeInternalError, message, cause)
}

func ErrServiceNotStarted(serviceName string, cause error) *ForgeError {
	return NewForgeError(ErrCodeServiceStartFailed, fmt.Sprintf("service '%s' not started", serviceName), nil).
		WithContext("service_name", serviceName)
}

func ErrServiceAlreadyStarted(serviceName string, cause error) *ForgeError {
	return NewForgeError(ErrCodeServiceStartFailed, fmt.Sprintf("service '%s' already started", serviceName), nil).
		WithContext("service_name", serviceName)
}

// ErrorHandler defines how errors should be handled
type ErrorHandler interface {
	// HandleError handles an error
	HandleError(ctx Context, err error) error

	// ShouldRetry determines if an operation should be retried
	ShouldRetry(err error) bool

	// GetRetryDelay returns the delay before retrying
	GetRetryDelay(attempt int, err error) time.Duration
}

// DefaultErrorHandler is the default error handler
type DefaultErrorHandler struct {
	logger Logger
}

// NewDefaultErrorHandler creates a new default error handler
func NewDefaultErrorHandler(logger Logger) *DefaultErrorHandler {
	return &DefaultErrorHandler{
		logger: logger,
	}
}

// HandleError handles an error
func (h *DefaultErrorHandler) HandleError(ctx Context, err error) error {
	var forgeErr *ForgeError
	if errors.As(err, &forgeErr) {
		h.logger.Error(forgeErr.Message,
			logger.String("error_code", forgeErr.Code),
			logger.String("service", forgeErr.Service),
			logger.String("operation", forgeErr.Operation),
			logger.Any("context", forgeErr.Context),
			logger.Any("cause", forgeErr.Cause),
		)
	}
	return err
}

// ShouldRetry determines if an operation should be retried
func (h *DefaultErrorHandler) ShouldRetry(err error) bool {
	var forgeErr *ForgeError
	if errors.As(err, &forgeErr) {
		switch forgeErr.Code {
		case ErrCodeTimeoutError, ErrCodeContextCancelled:
			return true
		case ErrCodeServiceNotFound, ErrCodeServiceAlreadyExists, ErrCodeCircularDependency:
			return false
		default:
			return true
		}
	}
	return true
}

// GetRetryDelay returns the delay before retrying
func (h *DefaultErrorHandler) GetRetryDelay(attempt int, err error) time.Duration {
	// Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, max 10s
	delay := time.Duration(100*attempt*attempt) * time.Millisecond
	if delay > 10*time.Second {
		delay = 10 * time.Second
	}
	return delay
}

// Result represents the result of an operation
type Result[T any] struct {
	Value T
	Error error
}

// IsSuccess returns true if the result is successful
func (r Result[T]) IsSuccess() bool {
	return r.Error == nil
}

// IsFailure returns true if the result is a failure
func (r Result[T]) IsFailure() bool {
	return r.Error != nil
}

// Unwrap returns the value and error
func (r Result[T]) Unwrap() (T, error) {
	return r.Value, r.Error
}

// Success creates a successful result
func Success[T any](value T) Result[T] {
	return Result[T]{Value: value, Error: nil}
}

// Failure creates a failed result
func Failure[T any](err error) Result[T] {
	var zero T
	return Result[T]{Value: zero, Error: err}
}

// Option represents an optional value
type Option[T any] struct {
	value   T
	present bool
}

// Some creates an Option with a value
func Some[T any](value T) Option[T] {
	return Option[T]{value: value, present: true}
}

// None creates an empty Option
func None[T any]() Option[T] {
	var zero T
	return Option[T]{value: zero, present: false}
}

// IsPresent returns true if the option has a value
func (o Option[T]) IsPresent() bool {
	return o.present
}

// IsEmpty returns true if the option is empty
func (o Option[T]) IsEmpty() bool {
	return !o.present
}

// Get returns the value if present, otherwise panics
func (o Option[T]) Get() T {
	if !o.present {
		panic("cannot get value from empty option")
	}
	return o.value
}

// GetOrElse returns the value if present, otherwise returns the default
func (o Option[T]) GetOrElse(defaultValue T) T {
	if o.present {
		return o.value
	}
	return defaultValue
}

// Map applies a function to the value if present
func (o Option[T]) Map(fn func(T) interface{}) Option[interface{}] {
	if o.present {
		return Some(fn(o.value))
	}
	return None[interface{}]()
}

// Filter filters the option based on a predicate
func (o Option[T]) Filter(predicate func(T) bool) Option[T] {
	if o.present && predicate(o.value) {
		return o
	}
	return None[T]()
}
