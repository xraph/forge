package extras

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/pathspec"
)

// ChiAdapter wraps go-chi/chi router.
type ChiAdapter struct {
	router chi.Router
	// mu guards globalMiddlewares; UseGlobal can be called while ServeHTTP
	// is running on another goroutine.
	mu                sync.Mutex
	globalMiddlewares []func(http.Handler) http.Handler

	// chain is the global middleware chain composed once at registration
	// rather than rebuilt on every request.
	chain atomic.Pointer[http.Handler]
}

// NewChiAdapter creates a Chi router adapter.
func NewChiAdapter() forge.RouterAdapter {
	return &ChiAdapter{
		router: chi.NewRouter(),
	}
}

// Handle registers a route.
func (a *ChiAdapter) Handle(method, path string, handler http.Handler) {
	a.router.Method(method, toChiPath(path), handler)
}

// Mount registers a sub-handler.
func (a *ChiAdapter) Mount(path string, handler http.Handler) {
	a.router.Mount(path, handler)
}

// UseGlobal registers global middleware that runs before routing.
// This middleware will run for ALL requests, even those that don't match any route.
// This is critical for CORS preflight handling.
func (a *ChiAdapter) UseGlobal(middleware func(http.Handler) http.Handler) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.globalMiddlewares = append(a.globalMiddlewares, middleware)

	// Compose in reverse so the first registered middleware is outermost.
	handler := http.Handler(a.router)
	for i := len(a.globalMiddlewares) - 1; i >= 0; i-- {
		handler = a.globalMiddlewares[i](handler)
	}

	a.chain.Store(&handler)
}

// ServeHTTP dispatches requests.
func (a *ChiAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The chain is composed at registration time; nil means no global
	// middleware, so the request goes straight to the router.
	if chain := a.chain.Load(); chain != nil {
		(*chain).ServeHTTP(w, r)

		return
	}

	a.router.ServeHTTP(w, r)
}

// Close cleans up resources.
func (a *ChiAdapter) Close() error {
	return nil
}

// toChiPath renders a forge path in chi's dialect. A parse failure means the
// adapter was driven directly rather than through the router, which validates
// paths at registration, so the raw string is passed through unchanged.
func toChiPath(path string) string {
	p, err := pathspec.Parse(path)
	if err != nil {
		return path
	}

	return p.Render(pathspec.SyntaxBrace)
}
