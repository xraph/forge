package forge

import (
	"time"

	"github.com/xraph/forge/v2/internal/errors"
	"github.com/xraph/forge/v2/internal/router"
	"github.com/xraph/forge/v2/internal/shared"
)

// Re-export HTTP error types and constructors for backward compatibility
type HTTPError = errors.HTTPError

var (
	NewHTTPError  = errors.NewHTTPError
	BadRequest    = errors.BadRequest
	Unauthorized  = errors.Unauthorized
	Forbidden     = errors.Forbidden
	NotFound      = errors.NotFound
	InternalError = errors.InternalError
)

// Router provides HTTP routing with multiple backend support
type Router = router.Router

// RouteOption configures a route
type RouteOption = router.RouteOption

// GroupOption configures a route group
type GroupOption = router.GroupOption

// Middleware wraps HTTP handlers
type Middleware = router.Middleware

// RouteConfig holds route configuration
type RouteConfig = router.RouteConfig

// GroupConfig holds route group configuration
type GroupConfig = router.GroupConfig

// RouteInfo provides route information for inspection
type RouteInfo = router.RouteInfo

// RouteExtension represents a route-level extension (e.g., OpenAPI, custom validation)
// Note: This is different from app-level Extension which manages app components
type RouteExtension = router.RouteExtension

// NewRouter creates a new router with options
func NewRouter(opts ...RouterOption) Router {
	return router.NewRouter(opts...)
}

// RouterOption configures the router
type RouterOption = router.RouterOption

// RouterAdapter wraps a routing backend
type RouterAdapter = router.RouterAdapter

// ErrorHandler handles errors from handlers
type ErrorHandler = shared.ErrorHandler

// NewDefaultErrorHandler creates a default error handler
func NewDefaultErrorHandler(l Logger) ErrorHandler {
	return shared.NewDefaultErrorHandler(l)
}

// Route option constructors
func WithName(name string) RouteOption {
	return router.WithName(name)
}

func WithSummary(summary string) RouteOption {
	return router.WithSummary(summary)
}

func WithDescription(desc string) RouteOption {
	return router.WithDescription(desc)
}

func WithTags(tags ...string) RouteOption {
	return router.WithTags(tags...)
}

func WithMiddleware(mw ...Middleware) RouteOption {
	return router.WithMiddleware(mw...)
}

func WithTimeout(d time.Duration) RouteOption {
	return router.WithTimeout(d)
}

func WithMetadata(key string, value any) RouteOption {
	return router.WithMetadata(key, value)
}

func WithExtension(name string, ext Extension) RouteOption {
	return router.WithExtension(name, ext)
}

func WithOperationID(id string) RouteOption {
	return router.WithOperationID(id)
}

func WithDeprecated() RouteOption {
	return router.WithDeprecated()
}

// Group option constructors
func WithGroupMiddleware(mw ...Middleware) GroupOption {
	return router.WithGroupMiddleware(mw...)
}

func WithGroupTags(tags ...string) GroupOption {
	return router.WithGroupTags(tags...)
}

func WithGroupMetadata(key string, value any) GroupOption {
	return router.WithGroupMetadata(key, value)
}

// Router option constructors
func WithAdapter(adapter RouterAdapter) RouterOption {
	return router.WithAdapter(adapter)
}

func WithContainer(container Container) RouterOption {
	return router.WithContainer(container)
}

func WithLogger(logger Logger) RouterOption {
	return router.WithLogger(logger)
}

func WithErrorHandler(handler ErrorHandler) RouterOption {
	return router.WithErrorHandler(handler)
}

func WithRecovery() RouterOption {
	return router.WithRecovery()
}
