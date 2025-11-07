package router

import (
	"net/http"

	"github.com/xraph/forge/internal/di"
)

// MiddlewareFunc is a convenience type for middleware functions
// that want to explicitly call the next handler
// Note: This is a legacy type for http.Handler based middleware
// For new code, use Middleware directly (func(next Handler) Handler)
type MiddlewareFunc func(w http.ResponseWriter, r *http.Request, next http.Handler)

// ToMiddleware converts a MiddlewareFunc to a Middleware
// This requires a container to create forge contexts
func (f MiddlewareFunc) ToMiddleware(container di.Container) Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			// Wrap the forge.Handler as an http.Handler
			httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Create a new context from the http request with the same container
				forgeCtx := di.NewContext(w, r, container)
				defer forgeCtx.(di.ContextWithClean).Cleanup()

				// Copy context values from original context
				// This ensures session, cookies, etc. are preserved
				if ctxValues := ctx.Get("values"); ctxValues != nil {
					if values, ok := ctxValues.(map[string]any); ok {
						for k, v := range values {
							forgeCtx.Set(k, v)
						}
					}
				}

				// Execute the forge handler
				if err := next(forgeCtx); err != nil {
					// Error is handled by the router
					_ = err
				}
			})

			// Apply the http middleware function
			f(ctx.Response(), ctx.Request(), httpHandler)

			return nil
		}
	}
}

// Chain combines multiple middleware into a single middleware
// Middleware are applied in the order they are provided
// The first middleware in the list wraps the outermost, last wraps the innermost
func Chain(middlewares ...Middleware) Middleware {
	return func(final Handler) Handler {
		// Build chain in reverse order so first middleware wraps innermost
		// This means: m1(m2(m3(final))) where m1 is first in the list
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// ToMiddleware converts a ForgeMiddleware to a legacy Middleware
// This allows forge middlewares to work with the existing router
func (m PureMiddleware) ToMiddleware() Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			// Wrap the forge.Handler as an http.Handler
			httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Use the existing context's container
				container := ctx.Container()

				// Create a new context from the http request with the same container
				forgeCtx := di.NewContext(w, r, container)
				defer forgeCtx.(di.ContextWithClean).Cleanup()

				// Copy context values from original context
				// This ensures session, cookies, etc. are preserved
				if ctxValues := ctx.Get("values"); ctxValues != nil {
					if values, ok := ctxValues.(map[string]any); ok {
						for k, v := range values {
							forgeCtx.Set(k, v)
						}
					}
				}

				// Execute the forge handler
				if err := next(forgeCtx); err != nil {
					// Error is handled by the router
					_ = err
				}
			})

			// Apply the http middleware
			wrappedHandler := m(httpHandler)

			// Execute the wrapped handler with the original request/response
			wrappedHandler.ServeHTTP(ctx.Response(), ctx.Request())

			return nil
		}
	}

	// return func(next http.Handler) http.Handler {
	// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// 		// Get container from request context if available
	// 		var container Container
	// 		if c := r.Context().Value("forge:container"); c != nil {
	// 			if cont, ok := c.(Container); ok {
	// 				container = cont
	// 			}
	// 		}

	// 		// Create forge context from http request
	// 		ctx := di.NewContext(w, r, container)
	// 		defer ctx.(di.ContextWithClean).Cleanup()

	// 		// Wrap the http.Handler as a forge.Handler
	// 		forgeHandler := func(ctx Context) error {
	// 			next.ServeHTTP(ctx.Response(), ctx.Request())
	// 			return nil
	// 		}

	// 		// Apply the forge middleware
	// 		wrappedHandler := m(forgeHandler)

	// 		// Execute the wrapped handler
	// 		if err := wrappedHandler(ctx); err != nil {
	// 			// Error handling is done by the router
	// 			_ = err
	// 		}
	// 	})
	// }
}

// ToPureMiddleware converts a Middleware (forge middleware) to a PureMiddleware (http middleware)
// This allows forge middlewares to work with http.Handler based systems
func ToPureMiddleware(m Middleware, container di.Container, errorHandler ErrorHandler) PureMiddleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create forge context from http request
			ctx := di.NewContext(w, r, container)
			defer ctx.(di.ContextWithClean).Cleanup()

			// Wrap the http.Handler as a forge.Handler
			forgeHandler := func(ctx Context) error {
				next.ServeHTTP(ctx.Response(), ctx.Request())
				return nil
			}

			// Apply the forge middleware
			wrappedHandler := m(forgeHandler)

			// Execute the wrapped handler
			if err := wrappedHandler(ctx); err != nil {
				// Error handling
				if errorHandler != nil {
					errorHandler.HandleError(ctx.Context(), err)
				} else {
					// Default error handling
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}
		})
	}
}

// FromMiddleware converts a Middleware to PureMiddleware
// This is a convenience wrapper that requires container and errorHandler
// For use cases where container is not available, use ToPureMiddleware directly
func FromMiddleware(m Middleware) PureMiddleware {
	// This function signature is kept for backward compatibility
	// but it requires container and errorHandler to be provided
	// For now, return a middleware that will panic if used without proper setup
	// Users should use ToPureMiddleware instead
	panic("FromMiddleware requires container and errorHandler. Use ToPureMiddleware instead.")
}
