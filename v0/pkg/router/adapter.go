// pkg/router/adapter.go
package router

import (
	"context"
	"net/http"
)

type ParamFormat string

const (
	ParamFormatColon ParamFormat = ":param"
	ParamFormatStar  ParamFormat = "*param"
	ParamFormatBrace ParamFormat = "{param}"
)

// RouterAdapter abstracts different router implementations
type RouterAdapter interface {

	// GET registers a route with the GET HTTP method and assigns it a specified handler function.
	GET(path string, handler http.HandlerFunc)

	// POST registers a route with the POST HTTP method and a corresponding handler function.
	POST(path string, handler http.HandlerFunc)

	// PUT registers a handler function for HTTP PUT requests at the specified path. It is used for updating or replacing resources.
	PUT(path string, handler http.HandlerFunc)

	// DELETE registers a route with the HTTP DELETE method at the specified path and associates it with a handler function.
	DELETE(path string, handler http.HandlerFunc)

	// PATCH registers a route that handles HTTP PATCH requests for the specified path with the provided handler function.
	PATCH(path string, handler http.HandlerFunc)

	// HEAD registers a handler for HTTP HEAD requests at the specified path.
	HEAD(path string, handler http.HandlerFunc)

	// OPTIONS registers a route that matches HTTP OPTIONS requests for the specified path and associates it with a handler.
	OPTIONS(path string, handler http.HandlerFunc)

	// Handle registers a route with the specified HTTP method, path, and handler function to the router.
	Handle(method, path string, handler http.HandlerFunc)

	// Mount registers a sub-router or handler on the specified URL pattern to group or organize routes efficiently.
	Mount(pattern string, handler http.Handler)

	// Use adds a middleware function to the HTTP handler chain, wrapping the provided handler with the specified middleware.
	Use(middleware func(http.Handler) http.Handler)

	// Group creates a sub-router with a specific URL prefix. Routes added to the sub-router will inherit the prefix.
	Group(prefix string) RouterAdapter

	// ServeHTTP dispatches the request to the handler that matches the request's method and path.
	ServeHTTP(w http.ResponseWriter, r *http.Request)

	// Handler returns the underlying http.Handler allowing direct access to the router implementation for customization.
	Handler() http.Handler

	// ExtractParams extracts path parameters from the given HTTP request and returns them as a map of key-value pairs.
	ExtractParams(r *http.Request) map[string]string

	// GetRouterInfo retrieves metadata about the underlying router, including its name, version, and supported features.
	GetRouterInfo() RouterInfo

	SupportsGroups() bool

	ParamFormat() ParamFormat

	// Start initializes and runs the required process, managing its lifecycle within the provided context. Returns an error if it fails.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the router, releasing resources and completing active requests if possible.
	Stop(ctx context.Context) error
}

// RouterInfo provides metadata about the underlying router
type RouterInfo struct {
	Name           string
	Version        string
	SupportsGroups bool
	SupportsMount  bool
	ParamFormat    string // ":param" vs "{param}" vs "*param"
}

// RouterConfig contains configuration for router adapters
type RouterConfig struct {
	RouterType string                 // "steel", "chi", "gin", "echo", etc.
	Options    map[string]interface{} // Router-specific options
}
