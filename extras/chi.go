package extras

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xraph/forge"
)

// ChiAdapter wraps go-chi/chi router.
type ChiAdapter struct {
	router            chi.Router
	globalMiddlewares []func(http.Handler) http.Handler
}

// NewChiAdapter creates a Chi router adapter.
func NewChiAdapter() forge.RouterAdapter {
	return &ChiAdapter{
		router: chi.NewRouter(),
	}
}

// Handle registers a route.
func (a *ChiAdapter) Handle(method, path string, handler http.Handler) {
	// Convert path format from :param to {param} for chi
	chiPath := convertPathToChi(path)
	a.router.Method(method, chiPath, handler)
}

// Mount registers a sub-handler.
func (a *ChiAdapter) Mount(path string, handler http.Handler) {
	a.router.Mount(path, handler)
}

// UseGlobal registers global middleware that runs before routing.
// This middleware will run for ALL requests, even those that don't match any route.
// This is critical for CORS preflight handling.
func (a *ChiAdapter) UseGlobal(middleware func(http.Handler) http.Handler) {
	a.globalMiddlewares = append(a.globalMiddlewares, middleware)
}

// ServeHTTP dispatches requests.
func (a *ChiAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
func (a *ChiAdapter) Close() error {
	return nil
}

// convertPathToChi converts :param to {param}.
func convertPathToChi(path string) string {
	// Chi uses {param} format, we use :param
	result := ""

	i := 0

	var resultSb49 strings.Builder

	for i < len(path) {
		if path[i] == ':' {
			// Find end of parameter name
			j := i + 1
			for j < len(path) && path[j] != '/' {
				j++
			}
			// Convert :param to {param}
			resultSb49.WriteString("{" + path[i+1:j] + "}")
			i = j
		} else {
			resultSb49.WriteString(string(path[i]))
			i++
		}
	}

	result += resultSb49.String()

	return result
}
