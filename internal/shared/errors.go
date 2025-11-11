package shared

import (
	"context"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
)

// ErrorHandler defines how errors should be handled.
type ErrorHandler interface {
	HandleError(ctx context.Context, err error) error
	ShouldRetry(err error) bool
	GetRetryDelay(attempt int, err error) time.Duration
}

// DefaultErrorHandler is the default error handler.
type DefaultErrorHandler struct {
	logger logger.Logger
}

// NewDefaultErrorHandler creates a new default error handler.
func NewDefaultErrorHandler(l logger.Logger) ErrorHandler {
	return &DefaultErrorHandler{
		logger: l,
	}
}

// HandleError handles an error.
func (h *DefaultErrorHandler) HandleError(ctx context.Context, err error) error {
	var forgeErr errors.ForgeError
	if errors.Is(err, &forgeErr) {
		h.logger.Error(forgeErr.Message,
			logger.String("error_code", forgeErr.Code),
			logger.Any("context", forgeErr.Context),
			logger.Error(forgeErr.Cause),
		)
	}

	return err
}

// ShouldRetry determines if an operation should be retried.
func (h *DefaultErrorHandler) ShouldRetry(err error) bool {
	var forgeErr errors.ForgeError
	if errors.Is(err, &forgeErr) {
		switch forgeErr.Code {
		case errors.CodeTimeoutError, errors.CodeContextCancelled:
			return true
		case errors.CodeServiceNotFound, errors.CodeServiceAlreadyExists, errors.CodeCircularDependency:
			return false
		default:
			return true
		}
	}

	return true
}

// GetRetryDelay returns the delay before retrying.
func (h *DefaultErrorHandler) GetRetryDelay(attempt int, err error) time.Duration {
	// Exponential backoff: 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, max 10s
	delay := min(time.Duration(100*attempt*attempt)*time.Millisecond, 10*time.Second)

	return delay
}
