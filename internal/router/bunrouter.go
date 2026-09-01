package router

import (
	"context"
	"net/http"
	"strings"

	"github.com/uptrace/bunrouter"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/shared"
	forge_http "github.com/xraph/go-utils/http"
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
	bunPath := toBunPath(path)

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

		// Store params in request context (ALWAYS store, even if empty).
		httpReq = publishParams(httpReq, params)

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

		// Store params in request context.
		if len(params) > 0 {
			httpReq = publishParams(httpReq, params)
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

	// Determine the mount path. Rendering through pathspec gives the wildcard
	// the name bunrouter requires, and matches whatever Handle would produce
	// for the same input.
	var mountPath string

	if strings.HasSuffix(path, "/*") {
		mountPath = toBunPath(path)
	} else {
		// Register the exact path too, so a request to /path reaches the
		// handler as well as /path/sub.
		for _, method := range methods {
			a.router.Handle(method, toBunPath(path), handlerFunc)
		}

		mountPath = toBunPath(strings.TrimSuffix(path, "/") + "/*")
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
	// This must run before global middleware so middleware sees normalized paths.
	normalizeTrailingSlash(r)

	// One wrap per request, ahead of global middleware, so anything downstream
	// can register a before-write callback (see shared.BeforeWriter). Idempotent
	// — a mounted sub-router re-entering here shares the same hook list instead
	// of stacking another layer.
	w = shared.WrapBeforeWrite(w)
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
// Registration validates the path first (see router_impl.go), so a parse
// failure here means an adapter was driven directly. Falling back to the raw
// string keeps that caller's behavior unchanged rather than panicking on a
// path forge never approved.
func toBunPath(path string) string {
	p, err := pathspec.Parse(path)
	if err != nil {
		return path
	}

	return p.Render(pathspec.SyntaxColon)
}

// publishParams puts path parameters on the request in both forms.
//
// The typed carrier is what go-utils reads first; the map keeps an older
// go-utils working. The carrier is NOT pooled here: this adapter hands control
// to bunrouter and never sees the handler return, so it has no safe point to
// release. forgemux owns dispatch and does pool.
func publishParams(req *http.Request, params map[string]string) *http.Request {
	rp := &forge_http.RouteParams{}
	for k, v := range params {
		rp.Set(k, v)
	}

	ctx := req.Context()
	ctx = context.WithValue(ctx, forge_http.RouteParamsKey, rp)
	ctx = context.WithValue(ctx, "forge:params", params) //nolint:staticcheck // legacy contract, read as a fallback

	return req.WithContext(ctx)
}
