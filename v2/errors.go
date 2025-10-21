package forge

import (
	"fmt"

	"github.com/xraph/forge/v2/internal/errors"
)

// Re-export error constructors for backward compatibility
var (
	ErrServiceNotFound      = errors.ErrServiceNotFound
	ErrServiceAlreadyExists = errors.ErrServiceAlreadyExists
	ErrCircularDependency   = errors.ErrCircularDependency
	ErrInvalidFactory       = errors.ErrInvalidFactory
	ErrTypeMismatch         = errors.ErrTypeMismatch
	ErrLifecycleTimeout     = errors.ErrLifecycleTimeout
	ErrContainerStarted     = errors.ErrContainerStarted
	ErrContainerStopped     = errors.ErrContainerStopped
	ErrScopeEnded           = errors.ErrScopeEnded
)

// Extension-specific errors
var (
	ErrExtensionNotRegistered = fmt.Errorf("extension not registered with app")
)

// Re-export sentinel errors for error comparison using errors.Is()
var (
	ErrServiceNotFoundSentinel      = errors.ErrServiceNotFoundSentinel
	ErrServiceAlreadyExistsSentinel = errors.ErrServiceAlreadyExistsSentinel
	ErrCircularDependencySentinel   = errors.ErrCircularDependencySentinel
	ErrInvalidConfigSentinel        = errors.ErrInvalidConfigSentinel
	ErrValidationErrorSentinel      = errors.ErrValidationErrorSentinel
	ErrLifecycleErrorSentinel       = errors.ErrLifecycleErrorSentinel
	ErrContextCancelledSentinel     = errors.ErrContextCancelledSentinel
	ErrTimeoutErrorSentinel         = errors.ErrTimeoutErrorSentinel
	ErrConfigErrorSentinel          = errors.ErrConfigErrorSentinel
)

// Re-export error types for backward compatibility
type ServiceError = errors.ServiceError

// Re-export error constructors for backward compatibility
var NewServiceError = errors.NewServiceError
