package extras

import (
	"net/http"
	"strings"

	"github.com/uptrace/bunrouter"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/pathspec"
)

// BunRouterAdapter wraps uptrace/bunrouter.
type BunRouterAdapter struct {
	router            *bunrouter.Router
	globalMiddlewares []func(http.Handler) http.Handler
}

// NewBunRouterAdapter creates a BunRouter adapter.
//
// forgemux is the default; pass this to forge.WithAdapter to use bunrouter
// instead.
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
	bunPath := toBunPath(path)

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

// normalizeTrailingSlash strips trailing slashes from the request path
// to prevent BunRouter from issuing 301 redirects for paths like "/dashboard/"
// when the route is registered as "/dashboard". The root path "/" is preserved.
func normalizeTrailingSlash(r *http.Request) {
	if len(r.URL.Path) > 1 && r.URL.Path[len(r.URL.Path)-1] == '/' {
		r.URL.Path = strings.TrimRight(r.URL.Path, "/")
	}

	if r.URL.RawPath != "" && len(r.URL.RawPath) > 1 && r.URL.RawPath[len(r.URL.RawPath)-1] == '/' {
		r.URL.RawPath = strings.TrimRight(r.URL.RawPath, "/")
	}
}

// ServeHTTP dispatches requests.
func (a *BunRouterAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Normalize trailing slashes to prevent BunRouter's automatic 301 redirects.
	normalizeTrailingSlash(r)

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

// toBunPath renders a forge path in bunrouter's dialect.
//
// This used to return the path unchanged, which meant a route registered as
// "/users/{id}" was handed to bunrouter verbatim and matched the literal
// string "/users/{id}" instead of capturing a parameter. Rendering through
// pathspec converts it to "/users/:id" and names an unnamed wildcard, matching
// what the default adapter has always done.
func toBunPath(path string) string {
	p, err := pathspec.Parse(path)
	if err != nil {
		return path
	}

	return p.Render(pathspec.SyntaxColon)
}
