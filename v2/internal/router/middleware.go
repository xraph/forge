package router

import (
	"net/http"
)

// MiddlewareFunc is a convenience type for middleware functions
// that want to explicitly call the next handler
type MiddlewareFunc func(w http.ResponseWriter, r *http.Request, next http.Handler)

// ToMiddleware converts a MiddlewareFunc to a Middleware
func (f MiddlewareFunc) ToMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f(w, r, next)
		})
	}
}

// Chain combines multiple middleware into a single middleware
// Middleware are applied in the order they are provided
func Chain(middlewares ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		// Build chain in reverse order so first middleware wraps innermost
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
