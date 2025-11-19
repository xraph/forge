package shared

import (
	"context"
	"errors"

	"github.com/xraph/forge/internal/logger"
)

// HTTPResponder represents an error that knows how to format itself as an HTTP response.
// This interface provides a unified way to handle errors across the framework, allowing
// different error types (HTTPError, ValidationErrors, ForgeError) to format themselves
// consistently for HTTP responses.
type HTTPResponder interface {
	error

	// StatusCode returns the HTTP status code
	StatusCode() int

	// ResponseBody returns the response body (typically a struct/map for JSON)
	ResponseBody() any
}

// ErrorHandler handles errors from HTTP handlers.
type ErrorHandler interface {
	// HandleError handles an error and returns the formatted error response
	HandleError(ctx context.Context, err error) error
}

// DefaultErrorHandler is the default implementation of ErrorHandler.
type DefaultErrorHandler struct {
	logger Logger
}

// NewDefaultErrorHandler creates a new default error handler.
func NewDefaultErrorHandler(log Logger) ErrorHandler {
	return &DefaultErrorHandler{
		logger: log,
	}
}

// HandleError handles an error and returns it formatted for HTTP response.
// It checks if the error implements HTTPResponder and uses that for formatting,
// otherwise falls back to default error formatting.
func (h *DefaultErrorHandler) HandleError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	// Log the error
	if h.logger != nil {
		h.logger.Error("handler error occurred",
			logger.Any("error", err.Error()),
		)
	}

	// Check if error implements HTTPResponder
	var responder HTTPResponder
	if errors.As(err, &responder) {
		// Error knows how to format itself
		if c, ok := ctx.(Context); ok {
			return c.JSON(responder.StatusCode(), responder.ResponseBody())
		}
	}

	// Return the error as-is for the caller to handle
	return err
}
