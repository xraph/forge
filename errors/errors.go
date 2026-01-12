package errors

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/go-utils/errs"
)

// =============================================================================
// ERROR CODES
// =============================================================================

// Error code constants for structured errors.
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

// Standard DI/service errors.
var (
	// ErrServiceNotFound      = errors.New("service not found")
	// ErrServiceAlreadyExists = errors.New("service already registered")
	// ErrCircularDependency   = errors.New("circular dependency detected").
	ErrInvalidFactory   = errs.New("factory must be a function")
	ErrTypeMismatch     = errs.New("service type mismatch")
	ErrLifecycleTimeout = errs.New("lifecycle operation timed out")
	ErrContainerStarted = errs.New("container already started")
	ErrContainerStopped = errs.New("container already stopped")
	ErrScopeEnded       = errs.New("scope already ended")
)

// ServiceError wraps service-specific errors.
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

// Is implements errors.Is interface for ServiceError.
func (e *ServiceError) Is(target error) bool {
	t, ok := target.(*ServiceError)
	if !ok {
		return false
	}

	return (e.Service == "" || t.Service == "" || e.Service == t.Service) &&
		(e.Operation == "" || t.Operation == "" || e.Operation == t.Operation)
}

// NewServiceError creates a new service error.
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

// ForgeError represents a structured error with context.
type ForgeError = errs.Error

// ErrConfigError creates a config error.
func ErrConfigError(message string, cause error) *ForgeError {
	return errs.NewError(CodeConfigError, message, cause)
}

// ErrValidationError creates a validation error.
func ErrValidationError(field string, cause error) *ForgeError {
	return errs.NewError(CodeValidationError, fmt.Sprintf("validation error for field '%s'", field), cause)
}

// ErrLifecycleError creates a lifecycle error.
func ErrLifecycleError(phase string, cause error) *ForgeError {
	return errs.NewError(CodeLifecycleError, "lifecycle error during "+phase, cause)
}

func ErrContextCancelled(operation string) *ForgeError {
	return errs.NewError(CodeContextCancelled, "context cancelled during "+operation, nil)
}

func ErrServiceNotFound(serviceName string) *ForgeError {
	return errs.NewError(CodeServiceNotFound, "service '"+serviceName+"' not found", nil)
}

func ErrDependencyNotFound(deps ...string) *ForgeError {
	return errs.NewError(CodeServiceNotFound, "dependency not found: "+strings.Join(deps, ", "), nil)
}

func ErrServiceAlreadyExists(serviceName string) *ForgeError {
	return errs.NewError(CodeServiceAlreadyExists, "service '"+serviceName+"' already exists", nil)
}

func ErrCircularDependency(services []string) *ForgeError {
	return errs.NewError(CodeCircularDependency, "circular dependency detected: "+strings.Join(services, " -> "), nil)
}

func ErrInvalidConfig(configKey string, cause error) *ForgeError {
	return errs.NewError(CodeInvalidConfig, "invalid configuration for key '"+configKey+"'", cause)
}

func ErrContainerError(operation string, cause error) *ForgeError {
	return errs.NewError(CodeLifecycleError, "container error during "+operation, cause)
}

func ErrTimeoutError(operation string, timeout time.Duration) *ForgeError {
	return errs.NewError(CodeTimeoutError, "timeout during "+operation+" after "+timeout.String(), nil)
}

func ErrHealthCheckFailed(serviceName string, cause error) *ForgeError {
	return errs.NewError(CodeHealthCheckFailed, "health check failed for service '"+serviceName+"'", cause)
}

func ErrServiceStartFailed(serviceName string, cause error) *ForgeError {
	return errs.NewError(CodeServiceStartFailed, "failed to start service '"+serviceName+"'", cause)
}

// =============================================================================
// VALIDATION ERROR
// =============================================================================

// Severity represents the severity of a validation issue.
type Severity = errs.Severity

const (
	SeverityError   = errs.SeverityError
	SeverityWarning = errs.SeverityWarning
	SeverityInfo    = errs.SeverityInfo
)

// ValidationError represents a validation error.
type ValidationError = errs.ValidationError

// =============================================================================
// HTTP ERRORS
// =============================================================================

// HTTPError represents an HTTP error with status code.
// type HTTPError = errs.HTTPError

// HTTPError represents an HTTP error with status code.
type IHTTPError = errs.HTTPError

type HTTPError struct {
	Code    int
	Message string
	Err     error
}

// HTTP error constructors.
func NewHTTPError(code int, message string) IHTTPError {
	return errs.NewHTTPError(code, message)
}

func BadRequest(message string) IHTTPError {
	return errs.BadRequest(message)
}

func Unauthorized(message string) IHTTPError {
	return errs.Unauthorized(message)
}

func Forbidden(message string) IHTTPError {
	return errs.Forbidden(message)
}

func NotFound(message string) IHTTPError {
	return errs.NotFound(message)
}

func InternalError(err error) IHTTPError {
	return errs.InternalError(err)
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
func As(err error, target any) bool {
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
// Requires Go 1.20+.
func Join(errs ...error) error {
	return errors.Join(errs...)
}

// =============================================================================
// SENTINEL ERRORS (for use with Is)
// =============================================================================

// Sentinel errors that can be used with errors.Is comparisons.
var (
	// ErrServiceNotFoundSentinel is a sentinel error for service not found.
	ErrServiceNotFoundSentinel = &ForgeError{Code: CodeServiceNotFound}

	// ErrServiceAlreadyExistsSentinel is a sentinel error for service already exists.
	ErrServiceAlreadyExistsSentinel = &ForgeError{Code: CodeServiceAlreadyExists}

	// ErrCircularDependencySentinel is a sentinel error for circular dependency.
	ErrCircularDependencySentinel = &ForgeError{Code: CodeCircularDependency}

	// ErrInvalidConfigSentinel is a sentinel error for invalid config.
	ErrInvalidConfigSentinel = &ForgeError{Code: CodeInvalidConfig}

	// ErrValidationErrorSentinel is a sentinel error for validation errors.
	ErrValidationErrorSentinel = &ForgeError{Code: CodeValidationError}

	// ErrLifecycleErrorSentinel is a sentinel error for lifecycle errors.
	ErrLifecycleErrorSentinel = &ForgeError{Code: CodeLifecycleError}

	// ErrContextCancelledSentinel is a sentinel error for context cancellation.
	ErrContextCancelledSentinel = &ForgeError{Code: CodeContextCancelled}

	// ErrTimeoutErrorSentinel is a sentinel error for timeout errors.
	ErrTimeoutErrorSentinel = &ForgeError{Code: CodeTimeoutError}

	// ErrConfigErrorSentinel is a sentinel error for config errors.
	ErrConfigErrorSentinel = &ForgeError{Code: CodeConfigError}
)

// =============================================================================
// ERROR HELPERS
// =============================================================================

// IsServiceNotFound checks if the error is a service not found error.
func IsServiceNotFound(err error) bool {
	return Is(err, ErrServiceNotFoundSentinel)
}

// IsServiceAlreadyExists checks if the error is a service already exists error.
func IsServiceAlreadyExists(err error) bool {
	return Is(err, ErrServiceAlreadyExistsSentinel)
}

// IsCircularDependency checks if the error is a circular dependency error.
func IsCircularDependency(err error) bool {
	return Is(err, ErrCircularDependencySentinel)
}

// IsValidationError checks if the error is a validation error.
func IsValidationError(err error) bool {
	return Is(err, ErrValidationErrorSentinel)
}

// IsContextCancelled checks if the error is a context cancelled error.
func IsContextCancelled(err error) bool {
	return Is(err, ErrContextCancelledSentinel)
}

// IsTimeout checks if the error is a timeout error.
func IsTimeout(err error) bool {
	return Is(err, ErrTimeoutErrorSentinel)
}

// GetHTTPStatusCode extracts HTTP status code from error, returns 500 if not found.
func GetHTTPStatusCode(err error) int {
	return errs.GetHTTPStatusCode(err)
}
