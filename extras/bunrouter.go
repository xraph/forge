package extras

import (
	"net/http"
	"strings"

	"github.com/uptrace/bunrouter"
	"github.com/xraph/forge"
)

// BunRouterAdapter wraps uptrace/bunrouter.
type BunRouterAdapter struct {
	router            *bunrouter.Router
	globalMiddlewares []func(http.Handler) http.Handler
}

// NewBunRouterAdapter creates a BunRouter adapter (default).
func NewBunRouterAdapter() forge.RouterAdapter {
	router := bunrouter.New(
		bunrouter.WithNotFoundHandler(func(w http.ResponseWriter, req bunrouter.Request) error {
			http.NotFound(w, req.Request)

			return nil
		}),
	)

	return &BunRouterAdapter{
		router: router,
	}
}

// Handle registers a route.
func (a *BunRouterAdapter) Handle(method, path string, handler http.Handler) {
	// Convert path format from :param to {param} for bunrouter
	bunPath := convertPathToBunRouter(path)

	a.router.Handle(method, bunPath, func(w http.ResponseWriter, req bunrouter.Request) error {
		// BunRouter provides params through req.Params()
		// The handler will access them through Context
		httpReq := req.Request

		// Call the handler
		handler.ServeHTTP(w, httpReq)

		return nil
	})
}

// Mount registers a sub-handler.
func (a *BunRouterAdapter) Mount(path string, handler http.Handler) {
	// Ensure path ends with a named wildcard parameter for bunrouter
	mountPath := strings.TrimSuffix(path, "/") + "/*filepath"

	a.router.Handle("*", mountPath, func(w http.ResponseWriter, req bunrouter.Request) error {
		handler.ServeHTTP(w, req.Request)

		return nil
	})
}

// UseGlobal registers global middleware that runs before routing.
// This middleware will run for ALL requests, even those that don't match any route.
// This is critical for CORS preflight handling.
func (a *BunRouterAdapter) UseGlobal(middleware func(http.Handler) http.Handler) {
	a.globalMiddlewares = append(a.globalMiddlewares, middleware)
}

// ServeHTTP dispatches requests.
func (a *BunRouterAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// If there are global middlewares, apply them first
	if len(a.globalMiddlewares) > 0 {
		// Build the middleware chain
		// Start with the router as the final handler
		handler := http.Handler(a.router)

		// Apply middlewares in reverse order (first added wraps last)
		for i := len(a.globalMiddlewares) - 1; i >= 0; i-- {
			handler = a.globalMiddlewares[i](handler)
		}

		// Execute the chain
		handler.ServeHTTP(w, r)
		return
	}

	// No global middleware, just use the router directly
	a.router.ServeHTTP(w, r)
}

// Close cleans up resources.
func (a *BunRouterAdapter) Close() error {
	return nil
}

// convertPathToBunRouter converts :param to {param}.
func convertPathToBunRouter(path string) string {
	// BunRouter uses :param format (same as our format)
	// But for wildcards, we need to handle them
	return path
}
