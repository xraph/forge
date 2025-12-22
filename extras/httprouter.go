package extras

import (
	"context"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/xraph/forge"
)

// HTTPRouterAdapter wraps julienschmidt/httprouter.
type HTTPRouterAdapter struct {
	router            *httprouter.Router
	globalMiddlewares []func(http.Handler) http.Handler
}

// NewHTTPRouterAdapter creates an HTTPRouter adapter.
func NewHTTPRouterAdapter() forge.RouterAdapter {
	router := httprouter.New()
	router.HandleMethodNotAllowed = false

	return &HTTPRouterAdapter{
		router: router,
	}
}

// Handle registers a route.
func (a *HTTPRouterAdapter) Handle(method, path string, handler http.Handler) {
	a.router.Handle(method, path, func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// Store params in context for retrieval
		ctx := r.Context()
		ctx = context.WithValue(ctx, paramsKey, ps)
		r = r.WithContext(ctx)

		handler.ServeHTTP(w, r)
	})
}

// Mount registers a sub-handler.
func (a *HTTPRouterAdapter) Mount(path string, handler http.Handler) {
	// HTTPRouter doesn't have built-in Mount, use catch-all
	a.router.Handler("GET", path+"/*filepath", handler)
	a.router.Handler("POST", path+"/*filepath", handler)
	a.router.Handler("PUT", path+"/*filepath", handler)
	a.router.Handler("DELETE", path+"/*filepath", handler)
	a.router.Handler("PATCH", path+"/*filepath", handler)
	a.router.Handler("OPTIONS", path+"/*filepath", handler)
	a.router.Handler("HEAD", path+"/*filepath", handler)
}

// UseGlobal registers global middleware that runs before routing.
// This middleware will run for ALL requests, even those that don't match any route.
// This is critical for CORS preflight handling.
func (a *HTTPRouterAdapter) UseGlobal(middleware func(http.Handler) http.Handler) {
	a.globalMiddlewares = append(a.globalMiddlewares, middleware)
}

// ServeHTTP dispatches requests.
func (a *HTTPRouterAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
func (a *HTTPRouterAdapter) Close() error {
	return nil
}

// paramsKey is the context key for httprouter params.
type contextKey string

const paramsKey contextKey = "httprouter:params"

// GetHTTPRouterParams retrieves httprouter params from context.
func GetHTTPRouterParams(r *http.Request) httprouter.Params {
	if ps := r.Context().Value(paramsKey); ps != nil {
		return ps.(httprouter.Params)
	}

	return nil
}
