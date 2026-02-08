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
		// Use plain string key to match go-utils/http Context extraction
		ctx := httpReq.Context()
		ctx = context.WithValue(ctx, "forge:params", params) //nolint:staticcheck // Required for compatibility with go-utils/http
		httpReq = httpReq.WithContext(ctx)

		// Call the handler with updated request
		handler.ServeHTTP(w, httpReq)

		return nil
	})
}

// Mount registers a sub-handler.
func (a *BunRouterAdapter) Mount(path string, handler http.Handler) {
	// Create the handler function
	handlerFunc := func(w http.ResponseWriter, req bunrouter.Request) error {
		httpReq := req.Request

		// Extract params from bunrouter for mounted routes
		params := req.Params().Map()

		// Map filepath to "*" for wildcard access
		if filepath, ok := params["filepath"]; ok {
			params["*"] = filepath
		}

		// Store params in request context
		// Use plain string key to match go-utils/http Context extraction
		if len(params) > 0 {
			ctx := httpReq.Context()
			ctx = context.WithValue(ctx, "forge:params", params) //nolint:staticcheck // Required for compatibility with go-utils/http
			httpReq = httpReq.WithContext(ctx)
		}

		handler.ServeHTTP(w, httpReq)

		return nil
	}

	// Register for all HTTP methods
	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
		http.MethodOptions,
		http.MethodHead,
	}

	// Determine the mount path
	var mountPath string
	if strings.HasSuffix(path, "/*") {
		// Path already has wildcard, convert it to named wildcard
		mountPath = path + "filepath"
	} else {
		// Path doesn't have wildcard - register both exact path and wildcard path
		// This allows the handler to receive requests to both /path and /path/*
		for _, method := range methods {
			// Register exact path
			a.router.Handle(method, path, handlerFunc)
		}
		// Also register wildcard path for sub-paths
		mountPath = strings.TrimSuffix(path, "/") + "/*filepath"
	}

	// Register the wildcard path
	for _, method := range methods {
		a.router.Handle(method, mountPath, handlerFunc)
	}
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

// convertPathToBunRouter converts path patterns to bunrouter format.
// Handles both :param (Echo-style) and {param} (Chi/Gorilla-style) syntax.
// Normalizes to bunrouter's :param format and handles unnamed wildcards.
func convertPathToBunRouter(path string) string {
	// BunRouter uses :param format
	// Convert {param} to :param for users who prefer Chi/Gorilla style

	// Process the path character by character to handle {param} conversion
	var result strings.Builder

	inBraces := false

	for i := range len(path) {
		ch := path[i]

		switch ch {
		case '{':
			// Start of Chi/Gorilla-style parameter - convert to :param
			inBraces = true

			result.WriteByte(':')
		case '}':
			// End of Chi/Gorilla-style parameter
			if inBraces {
				inBraces = false
				// Don't write the closing brace
			} else {
				// Not in braces, keep the character
				result.WriteByte(ch)
			}
		default:
			result.WriteByte(ch)
		}
	}

	path = result.String()

	// Handle unnamed wildcards - bunrouter requires named wildcards
	// If path ends with just "/*", convert to "/*filepath"
	if strings.HasSuffix(path, "/*") {
		path += "filepath"
	}

	// Handle middle wildcards (less common but possible)
	// Replace "/*/" with "/*filepath/"
	path = strings.ReplaceAll(path, "/*/", "/*filepath/")

	return path
}
