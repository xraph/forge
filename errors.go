package forge

import (
	"github.com/xraph/forge/errors"
)

// Re-export error constructors for backward compatibility.
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

// Extension-specific errors.
var (
	ErrExtensionNotRegistered = errors.New("extension not registered with app")
)

// Re-export sentinel errors for error comparison using errors.Is().
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

// Scope identity errors.
var (
	// ErrNoScope is returned when a Scope is required but not present in the context.
	ErrNoScope = errors.Unauthorized("scope identity required")

	// ErrNoOrg is returned when an organization-level Scope is required
	// but the current Scope has no OrgID.
	ErrNoOrg = errors.Forbidden("organization scope required")
)

// ServiceError represents a service-level error for backward compatibility.
type ServiceError = errors.ServiceError

// NewServiceError creates a new service error for backward compatibility.
var NewServiceError = errors.NewServiceError
