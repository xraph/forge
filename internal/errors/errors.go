package errors

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// =============================================================================
// ERROR CODES
// =============================================================================

// Error code constants for structured errors
const (
	CodeConfigError          = "CONFIG_ERROR"
	CodeValidationError      = "VALIDATION_ERROR"
	CodeLifecycleError       = "LIFECYCLE_ERROR"
	CodeContextCancelled     = "CONTEXT_CANCELLED"
	CodeServiceNotFound      = "SERVICE_NOT_FOUND"
	CodeServiceAlreadyExists = "SERVICE_ALREADY_EXISTS"
	CodeCircularDependency   = "CIRCULAR_DEPENDENCY"
	CodeInvalidConfig        = "INVALID_CONFIG"
	CodeTimeoutError         = "TIMEOUT_ERROR"
	CodeHealthCheckFailed    = "HEALTH_CHECK_FAILED"
	CodeServiceStartFailed   = "SERVICE_START_FAILED"
)

// =============================================================================
// DI/SERVICE ERRORS
// =============================================================================

// Standard DI/service errors
var (
	// ErrServiceNotFound      = errors.New("service not found")
	// ErrServiceAlreadyExists = errors.New("service already registered")
	// ErrCircularDependency   = errors.New("circular dependency detected")
	ErrInvalidFactory   = errors.New("factory must be a function")
	ErrTypeMismatch     = errors.New("service type mismatch")
	ErrLifecycleTimeout = errors.New("lifecycle operation timed out")
	ErrContainerStarted = errors.New("container already started")
	ErrContainerStopped = errors.New("container already stopped")
	ErrScopeEnded       = errors.New("scope already ended")
)

// ServiceError wraps service-specific errors
type ServiceError struct {
	Service   string
	Operation string
	Err       error
}

func (e *ServiceError) Error() string {
	return fmt.Sprintf("service %s: %s: %v", e.Service, e.Operation, e.Err)
}

func (e *ServiceError) Unwrap() error {
	return e.Err
}

// Is implements errors.Is interface for ServiceError
func (e *ServiceError) Is(target error) bool {
	t, ok := target.(*ServiceError)
	if !ok {
		return false
	}
	return (e.Service == "" || t.Service == "" || e.Service == t.Service) &&
		(e.Operation == "" || t.Operation == "" || e.Operation == t.Operation)
}

// NewServiceError creates a new service error
func NewServiceError(service, operation string, err error) *ServiceError {
	return &ServiceError{
		Service:   service,
		Operation: operation,
		Err:       err,
	}
}

// =============================================================================
// FORGE ERROR (STRUCTURED ERROR)
// =============================================================================

// ForgeError represents a structured error with context
type ForgeError struct {
	Code      string
	Message   string
	Cause     error
	Timestamp time.Time
	Context   map[string]interface{}
}

func (e *ForgeError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *ForgeError) Unwrap() error {
	return e.Cause
}

// Is implements errors.Is interface for ForgeError
// Compares by error code, allowing matching against sentinel errors
func (e *ForgeError) Is(target error) bool {
	t, ok := target.(*ForgeError)
	if !ok {
		return false
	}
	// Match if codes are the same (and not empty)
	return e.Code != "" && e.Code == t.Code
}

// WithContext adds context to the error
func (e *ForgeError) WithContext(key string, value interface{}) *ForgeError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// ErrConfigError creates a config error
func ErrConfigError(message string, cause error) *ForgeError {
	return &ForgeError{
		Code:      CodeConfigError,
		Message:   message,
		Cause:     cause,
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
	}
}

// ErrValidationError creates a validation error
func ErrValidationError(field string, cause error) *ForgeError {
	return &ForgeError{
		Code:      CodeValidationError,
		Message:   fmt.Sprintf("validation error for field '%s'", field),
		Cause:     cause,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"field": field},
	}
}

// ErrLifecycleError creates a lifecycle error
func ErrLifecycleError(phase string, cause error) *ForgeError {
	return &ForgeError{
		Code:      CodeLifecycleError,
		Message:   "lifecycle error during " + phase,
		Cause:     cause,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"phase": phase},
	}
}

func ErrContextCancelled(operation string) *ForgeError {
	return &ForgeError{
		Code:      CodeContextCancelled,
		Message:   "context cancelled during " + operation,
		Cause:     nil,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"operation": operation},
	}
}

func ErrServiceNotFound(serviceName string) *ForgeError {
	return &ForgeError{
		Code:      CodeServiceNotFound,
		Message:   "service '" + serviceName + "' not found",
		Cause:     nil,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"service_name": serviceName},
	}
}

func ErrDependencyNotFound(deps ...string) *ForgeError {
	return &ForgeError{
		Code:      CodeServiceNotFound,
		Message:   "dependency not found: " + strings.Join(deps, ", "),
		Cause:     nil,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"dependencies": deps},
	}
}

func ErrServiceAlreadyExists(serviceName string) *ForgeError {
	return &ForgeError{
		Code:      CodeServiceAlreadyExists,
		Message:   "service '" + serviceName + "' already exists",
		Cause:     nil,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"service_name": serviceName},
	}
}

func ErrCircularDependency(services []string) *ForgeError {
	return &ForgeError{
		Code:      CodeCircularDependency,
		Message:   "circular dependency detected: " + strings.Join(services, " -> "),
		Cause:     nil,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"services": services},
	}
}

func ErrInvalidConfig(configKey string, cause error) *ForgeError {
	return &ForgeError{
		Code:      CodeInvalidConfig,
		Message:   "invalid configuration for key '" + configKey + "'",
		Cause:     cause,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"config_key": configKey},
	}
}

func ErrContainerError(operation string, cause error) *ForgeError {
	return &ForgeError{
		Code:    CodeLifecycleError,
		Message: "container error during " + operation,
		Cause:   cause,
	}

}

func ErrTimeoutError(operation string, timeout time.Duration) *ForgeError {
	return &ForgeError{
		Code:      CodeTimeoutError,
		Message:   "timeout during " + operation + " after " + timeout.String(),
		Cause:     nil,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"operation": operation, "timeout": timeout.String()},
	}
}

func ErrHealthCheckFailed(serviceName string, cause error) *ForgeError {
	return &ForgeError{
		Code:      CodeHealthCheckFailed,
		Message:   "health check failed for service '" + serviceName + "'",
		Cause:     cause,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"service_name": serviceName},
	}
}

func ErrServiceStartFailed(serviceName string, cause error) *ForgeError {
	return &ForgeError{
		Code:      CodeServiceStartFailed,
		Message:   "failed to start service '" + serviceName + "'",
		Cause:     cause,
		Timestamp: time.Now(),
		Context:   map[string]interface{}{"service_name": serviceName},
	}
}

// =============================================================================
// VALIDATION ERROR
// =============================================================================

// Severity represents the severity of a validation issue
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// ValidationError represents a validation error
type ValidationError struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value,omitempty"`
	Rule       string      `json:"rule"`
	Message    string      `json:"message"`
	Severity   Severity    `json:"severity"`
	Suggestion string      `json:"suggestion,omitempty"`
}

// =============================================================================
// HTTP ERRORS
// =============================================================================

// HTTPError represents an HTTP error with status code
type HTTPError struct {
	Code    int
	Message string
	Err     error
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return http.StatusText(e.Code)
}

func (e *HTTPError) Unwrap() error {
	return e.Err
}

// Is implements errors.Is interface for HTTPError
// Compares by HTTP status code
func (e *HTTPError) Is(target error) bool {
	t, ok := target.(*HTTPError)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// HTTP error constructors
func NewHTTPError(code int, message string) *HTTPError {
	return &HTTPError{Code: code, Message: message}
}

func BadRequest(message string) *HTTPError {
	return &HTTPError{Code: http.StatusBadRequest, Message: message}
}

func Unauthorized(message string) *HTTPError {
	return &HTTPError{Code: http.StatusUnauthorized, Message: message}
}

func Forbidden(message string) *HTTPError {
	return &HTTPError{Code: http.StatusForbidden, Message: message}
}

func NotFound(message string) *HTTPError {
	return &HTTPError{Code: http.StatusNotFound, Message: message}
}

func InternalError(err error) *HTTPError {
	return &HTTPError{Code: http.StatusInternalServerError, Err: err}
}

// =============================================================================
// STANDARD ERRORS PACKAGE INTEGRATION
// =============================================================================

// Is reports whether any error in err's chain matches target.
// This is a convenience wrapper around errors.Is from the standard library.
//
// Example:
//
//	err := ErrServiceNotFound("auth")
//	if Is(err, &ForgeError{Code: "SERVICE_NOT_FOUND"}) {
//	    // handle service not found
//	}
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target, and if so,
// sets target to that error value and returns true. Otherwise, it returns false.
// This is a convenience wrapper around errors.As from the standard library.
//
// Example:
//
//	var httpErr *HTTPError
//	if As(err, &httpErr) {
//	    // handle HTTP error with httpErr.Code
//	}
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type contains an Unwrap method returning error. Otherwise, Unwrap returns nil.
// This is a convenience wrapper around errors.Unwrap from the standard library.
func Unwrap(err error) error {
	return errors.Unwrap(err)
}

// New returns an error that formats as the given text.
// This is a convenience wrapper around errors.New from the standard library.
func New(text string) error {
	return errors.New(text)
}

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
// This is a convenience wrapper around errors.Join from the standard library.
// Requires Go 1.20+
func Join(errs ...error) error {
	return errors.Join(errs...)
}

// =============================================================================
// SENTINEL ERRORS (for use with Is)
// =============================================================================

// Sentinel errors that can be used with errors.Is comparisons
var (
	// ErrServiceNotFoundSentinel is a sentinel error for service not found
	ErrServiceNotFoundSentinel = &ForgeError{Code: CodeServiceNotFound}

	// ErrServiceAlreadyExistsSentinel is a sentinel error for service already exists
	ErrServiceAlreadyExistsSentinel = &ForgeError{Code: CodeServiceAlreadyExists}

	// ErrCircularDependencySentinel is a sentinel error for circular dependency
	ErrCircularDependencySentinel = &ForgeError{Code: CodeCircularDependency}

	// ErrInvalidConfigSentinel is a sentinel error for invalid config
	ErrInvalidConfigSentinel = &ForgeError{Code: CodeInvalidConfig}

	// ErrValidationErrorSentinel is a sentinel error for validation errors
	ErrValidationErrorSentinel = &ForgeError{Code: CodeValidationError}

	// ErrLifecycleErrorSentinel is a sentinel error for lifecycle errors
	ErrLifecycleErrorSentinel = &ForgeError{Code: CodeLifecycleError}

	// ErrContextCancelledSentinel is a sentinel error for context cancellation
	ErrContextCancelledSentinel = &ForgeError{Code: CodeContextCancelled}

	// ErrTimeoutErrorSentinel is a sentinel error for timeout errors
	ErrTimeoutErrorSentinel = &ForgeError{Code: CodeTimeoutError}

	// ErrConfigErrorSentinel is a sentinel error for config errors
	ErrConfigErrorSentinel = &ForgeError{Code: CodeConfigError}
)

// =============================================================================
// ERROR HELPERS
// =============================================================================

// IsServiceNotFound checks if the error is a service not found error
func IsServiceNotFound(err error) bool {
	return Is(err, ErrServiceNotFoundSentinel)
}

// IsServiceAlreadyExists checks if the error is a service already exists error
func IsServiceAlreadyExists(err error) bool {
	return Is(err, ErrServiceAlreadyExistsSentinel)
}

// IsCircularDependency checks if the error is a circular dependency error
func IsCircularDependency(err error) bool {
	return Is(err, ErrCircularDependencySentinel)
}

// IsValidationError checks if the error is a validation error
func IsValidationError(err error) bool {
	return Is(err, ErrValidationErrorSentinel)
}

// IsContextCancelled checks if the error is a context cancelled error
func IsContextCancelled(err error) bool {
	return Is(err, ErrContextCancelledSentinel)
}

// IsTimeout checks if the error is a timeout error
func IsTimeout(err error) bool {
	return Is(err, ErrTimeoutErrorSentinel)
}

// GetHTTPStatusCode extracts HTTP status code from error, returns 500 if not found
func GetHTTPStatusCode(err error) int {
	var httpErr *HTTPError
	if As(err, &httpErr) {
		return httpErr.Code
	}
	return http.StatusInternalServerError
}
