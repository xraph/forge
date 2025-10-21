package extras

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xraph/forge/v2"
)

// ChiAdapter wraps go-chi/chi router
type ChiAdapter struct {
	router chi.Router
}

// NewChiAdapter creates a Chi router adapter
func NewChiAdapter() forge.RouterAdapter {
	return &ChiAdapter{
		router: chi.NewRouter(),
	}
}

// Handle registers a route
func (a *ChiAdapter) Handle(method, path string, handler http.Handler) {
	// Convert path format from :param to {param} for chi
	chiPath := convertPathToChi(path)
	a.router.Method(method, chiPath, handler)
}

// Mount registers a sub-handler
func (a *ChiAdapter) Mount(path string, handler http.Handler) {
	a.router.Mount(path, handler)
}

// ServeHTTP dispatches requests
func (a *ChiAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

// Close cleans up resources
func (a *ChiAdapter) Close() error {
	return nil
}

// convertPathToChi converts :param to {param}
func convertPathToChi(path string) string {
	// Chi uses {param} format, we use :param
	result := ""
	i := 0
	for i < len(path) {
		if path[i] == ':' {
			// Find end of parameter name
			j := i + 1
			for j < len(path) && path[j] != '/' {
				j++
			}
			// Convert :param to {param}
			result += "{" + path[i+1:j] + "}"
			i = j
		} else {
			result += string(path[i])
			i++
		}
	}
	return result
}
