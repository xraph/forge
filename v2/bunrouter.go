package forge

import (
	"net/http"
	"strings"

	"github.com/uptrace/bunrouter"
)

// BunRouterAdapter wraps uptrace/bunrouter
type BunRouterAdapter struct {
	router *bunrouter.Router
}

// NewBunRouterAdapter creates a BunRouter adapter (default)
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

// Handle registers a route
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

// Mount registers a sub-handler
func (a *BunRouterAdapter) Mount(path string, handler http.Handler) {
	// Ensure path ends with a named wildcard parameter for bunrouter
	mountPath := strings.TrimSuffix(path, "/") + "/*filepath"

	a.router.Handle("*", mountPath, func(w http.ResponseWriter, req bunrouter.Request) error {
		handler.ServeHTTP(w, req.Request)
		return nil
	})
}

// ServeHTTP dispatches requests
func (a *BunRouterAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

// Close cleans up resources
func (a *BunRouterAdapter) Close() error {
	return nil
}

// convertPathToBunRouter converts :param to {param}
func convertPathToBunRouter(path string) string {
	// BunRouter uses :param format (same as our format)
	// But for wildcards, we need to handle them
	return path
}
