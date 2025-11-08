package router

import (
	"context"
	"net/http"
	"strings"

	"github.com/uptrace/bunrouter"
)

// BunRouterAdapter wraps uptrace/bunrouter.
type BunRouterAdapter struct {
	router *bunrouter.Router
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
		router: router,
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

// ServeHTTP dispatches requests.
func (a *BunRouterAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
