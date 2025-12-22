package router

import (
	"context"
	"net/http"
	"strings"

	"github.com/uptrace/bunrouter"
)

// BunRouterAdapter wraps uptrace/bunrouter.
type BunRouterAdapter struct {
	router            *bunrouter.Router
	globalMiddlewares []func(http.Handler) http.Handler
}

// NewBunRouterAdapter creates a BunRouter adapter (default).
func NewBunRouterAdapter() RouterAdapter {
	router := bunrouter.New(
		bunrouter.WithNotFoundHandler(func(w http.ResponseWriter, req bunrouter.Request) error {
			http.NotFound(w, req.Request)

			return nil
		}),
	)

	return &BunRouterAdapter{
		router:            router,
		globalMiddlewares: make([]func(http.Handler) http.Handler, 0),
	}
}

// Handle registers a route.
func (a *BunRouterAdapter) Handle(method, path string, handler http.Handler) {
	// Convert path format from :param to {param} for bunrouter
	bunPath := convertPathToBunRouter(path)

	a.router.Handle(method, bunPath, func(w http.ResponseWriter, req bunrouter.Request) error {
		// BunRouter provides params through req.Params()
		httpReq := req.Request

		// Extract params from bunrouter and store in request context
		// This allows forge Context to access them via ctx.Param()
		params := req.Params().Map()

		// Also add support for wildcard parameter accessed as "*"
		// When route has "/*" it gets converted to "/*filepath", so map both
		if filepath, ok := params["filepath"]; ok {
			params["*"] = filepath
		}

		// Store params in request context (ALWAYS store, even if empty)
		ctx := httpReq.Context()
		ctx = context.WithValue(ctx, ParamsContextKey, params)
		httpReq = httpReq.WithContext(ctx)

		// Call the handler with updated request
		handler.ServeHTTP(w, httpReq)

		return nil
	})
}

// Mount registers a sub-handler.
func (a *BunRouterAdapter) Mount(path string, handler http.Handler) {
	// Ensure path ends with a named wildcard parameter for bunrouter
	mountPath := strings.TrimSuffix(path, "/") + "/*filepath"

	a.router.Handle("*", mountPath, func(w http.ResponseWriter, req bunrouter.Request) error {
		httpReq := req.Request

		// Extract params from bunrouter for mounted routes
		params := req.Params().Map()

		// Map filepath to "*" for wildcard access
		if filepath, ok := params["filepath"]; ok {
			params["*"] = filepath
		}

		// Store params in request context
		if len(params) > 0 {
			ctx := httpReq.Context()
			ctx = context.WithValue(ctx, ParamsContextKey, params)
			httpReq = httpReq.WithContext(ctx)
		}

		handler.ServeHTTP(w, httpReq)

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

// ParamsContextKey is used to store route params in request context
// Exported so di.NewContext can use the same key.
const ParamsContextKey = "forge:params"

// convertPathToBunRouter converts path patterns to bunrouter format
// Handles unnamed wildcards by converting them to named wildcards.
func convertPathToBunRouter(path string) string {
	// BunRouter uses :param format (same as our format)
	// But wildcards must be named, e.g., *filepath not just *

	// If path ends with just "/*", convert to "/*filepath"
	if strings.HasSuffix(path, "/*") {
		return path + "filepath"
	}

	// Handle middle wildcards (less common but possible)
	// Replace "/*/" with "/*filepath/"
	path = strings.ReplaceAll(path, "/*/", "/*filepath/")

	return path
}
