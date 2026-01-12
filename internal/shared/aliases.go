package shared

import (
	"time"

	ferrors "github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
)

type Logger = logger.Logger
type ForgeError = ferrors.ForgeError

func ErrValidationError(field string, cause error) *ferrors.ForgeError {
	return ferrors.ErrValidationError(field, cause)
}

func ErrLifecycleError(phase string, cause error) *ferrors.ForgeError {
	return ferrors.ErrLifecycleError(phase, cause)
}

func ErrInvalidConfig(configKey string, cause error) *ferrors.ForgeError {
	return ferrors.ErrInvalidConfig(configKey, cause)
}

func ErrServiceStartFailed(serviceName string, cause error) *ferrors.ForgeError {
	return ferrors.ErrServiceStartFailed(serviceName, cause)
}

func ErrServiceNotFound(serviceName string) *ferrors.ForgeError {
	return ferrors.ErrServiceNotFound(serviceName)
}

func ErrServiceAlreadyExists(serviceName string) *ferrors.ForgeError {
	return ferrors.ErrServiceAlreadyExists(serviceName)
}

func ErrHealthCheckFailed(serviceName string, cause error) *ferrors.ForgeError {
	return ferrors.ErrHealthCheckFailed(serviceName, cause)
}

func ErrTimeoutError(operation string, timeout time.Duration) *ferrors.ForgeError {
	return ferrors.ErrTimeoutError(operation, timeout)
}

func ErrContextCancelled(operation string) *ferrors.ForgeError {
	return ferrors.ErrContextCancelled(operation)
}

// ErrServiceStopFailed preserves compatibility; map to lifecycle stop error.
func ErrServiceStopFailed(serviceName string, cause error) *ferrors.ForgeError {
	return ferrors.ErrLifecycleError("stop:"+serviceName, cause)
}

// ErrContainerError preserves compatibility for code expecting common.ErrContainerError in v0.
// There is no direct equivalent code in v2 errors; we map it to a lifecycle error for now.
func ErrContainerError(operation string, cause error) *ferrors.ForgeError {
	return ferrors.ErrLifecycleError(operation, cause)
}
