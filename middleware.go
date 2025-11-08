package forge

import (
	"github.com/xraph/forge/internal/router"
)

// MiddlewareFunc is a convenience type for middleware functions
// that want to explicitly call the next handler.
type MiddlewareFunc = router.MiddlewareFunc

// PureMiddleware wraps HTTP handlers.
type PureMiddleware = router.PureMiddleware

// FromHTTPMiddleware converts a legacy http.Handler middleware to a ForgeMiddleware
// This allows existing http.Handler middlewares to work with forge handlers.
func FromMiddleware(m Middleware) PureMiddleware {
	return router.FromMiddleware(m)
}

// ToPureMiddleware converts a Middleware (forge middleware) to a PureMiddleware (http middleware)
// This allows forge middlewares to work with http.Handler based systems.
func ToPureMiddleware(m Middleware, container Container, errorHandler ErrorHandler) PureMiddleware {
	return router.ToPureMiddleware(m, container, errorHandler)
}
