package extras

import (
	"context"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/xraph/forge/v2"
)

// HTTPRouterAdapter wraps julienschmidt/httprouter
type HTTPRouterAdapter struct {
	router *httprouter.Router
}

// NewHTTPRouterAdapter creates an HTTPRouter adapter
func NewHTTPRouterAdapter() forge.RouterAdapter {
	router := httprouter.New()
	router.HandleMethodNotAllowed = false

	return &HTTPRouterAdapter{
		router: router,
	}
}

// Handle registers a route
func (a *HTTPRouterAdapter) Handle(method, path string, handler http.Handler) {
	a.router.Handle(method, path, func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		// Store params in context for retrieval
		ctx := r.Context()
		ctx = context.WithValue(ctx, paramsKey, ps)
		r = r.WithContext(ctx)

		handler.ServeHTTP(w, r)
	})
}

// Mount registers a sub-handler
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

// ServeHTTP dispatches requests
func (a *HTTPRouterAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.router.ServeHTTP(w, r)
}

// Close cleans up resources
func (a *HTTPRouterAdapter) Close() error {
	return nil
}

// paramsKey is the context key for httprouter params
type contextKey string

const paramsKey contextKey = "httprouter:params"

// GetHTTPRouterParams retrieves httprouter params from context
func GetHTTPRouterParams(r *http.Request) httprouter.Params {
	if ps := r.Context().Value(paramsKey); ps != nil {
		return ps.(httprouter.Params)
	}
	return nil
}
