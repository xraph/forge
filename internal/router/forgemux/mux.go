package forgemux

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	forge_http "github.com/xraph/go-utils/http"

	"github.com/xraph/forge/internal/pathspec"
	"github.com/xraph/forge/internal/shared"
)

// Mux is forge's in-house matcher, implementing shared.ExtendedAdapter.
type Mux struct {
	tree       *tree
	cfg        shared.MatcherConfig
	registered bool

	// mu guards global. UseGlobal can run after the server is already serving,
	// as when an extension registers middleware at runtime, while ServeHTTP
	// reads the composed chain from another goroutine.
	mu     sync.Mutex
	global []func(http.Handler) http.Handler

	// chain is the global middleware chain composed once at registration.
	// Composing it per request cost one closure allocation per middleware on
	// every request, so an app with ten extension middlewares paid ten
	// allocations before routing began.
	chain atomic.Pointer[http.Handler]
}

// New creates a matcher with the default configuration.
func New() *Mux { return &Mux{tree: newTree()} }

// Capabilities reports everything, because this matcher is the reason the
// wide interface exists.
func (m *Mux) Capabilities() shared.Capabilities {
	return shared.Capabilities{
		MethodNotAllowed:  true,
		Constraints:       true,
		ConflictDetection: true,
		TypedParams:       true,
		AnyMethod:         true,
	}
}

// Configure applies router-level settings. It is construction-only: changing
// the miss handlers after routes exist would reinterpret them silently.
func (m *Mux) Configure(cfg shared.MatcherConfig) error {
	if m.registered {
		return errors.New("forgemux: Configure called after a route was registered")
	}

	m.cfg = cfg

	return nil
}

// HandleRoute registers a parsed route.
func (m *Mux) HandleRoute(spec shared.RouteSpec) error {
	m.registered = true

	return m.tree.insert(spec.Method, spec.Pattern, spec.Handler, spec.Kind)
}

// Handle satisfies the RouterAdapter floor for a caller driving the adapter
// directly. It parses and delegates; a malformed path is dropped rather than
// panicking, because this method cannot report an error.
func (m *Mux) Handle(method, path string, handler http.Handler) {
	pattern, err := pathspec.Parse(path)
	if err != nil {
		return
	}

	_ = m.HandleRoute(shared.RouteSpec{Method: method, Pattern: pattern, Handler: handler})
}

// Mount registers a sub-handler for every method, which is one node rather
// than the seven-verb loop the older adapters need.
func (m *Mux) Mount(path string, handler http.Handler) {
	base := strings.TrimSuffix(path, "/")

	if exact, err := pathspec.Parse(base); err == nil {
		_ = m.HandleRoute(shared.RouteSpec{Pattern: exact, Handler: handler})
	}

	if sub, err := pathspec.Parse(base + "/*"); err == nil {
		_ = m.HandleRoute(shared.RouteSpec{Pattern: sub, Handler: handler})
	}
}

func (m *Mux) UseGlobal(middleware func(http.Handler) http.Handler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.global = append(m.global, middleware)

	// Compose in reverse so the first registered middleware is outermost.
	handler := http.Handler(http.HandlerFunc(m.dispatch))
	for i := len(m.global) - 1; i >= 0; i-- {
		handler = m.global[i](handler)
	}

	m.chain.Store(&handler)
}

func (m *Mux) Close() error { return nil }

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w = shared.WrapBeforeWrite(w)

	// The chain is composed at registration time; nil means no global
	// middleware, so the request dispatches directly.
	if chain := m.chain.Load(); chain != nil {
		(*chain).ServeHTTP(w, r)

		return
	}

	m.dispatch(w, r)
}

func (m *Mux) dispatch(w http.ResponseWriter, r *http.Request) {
	// "/users/" and "/users" always reach the same route. The slash is dropped
	// from a local copy only: r.URL.Path is never written, so a handler still
	// sees the path the client sent. That mutation is what the old adapter did
	// and what this design exists to remove.
	lookupPath := r.URL.Path
	if len(lookupPath) > 1 && strings.HasSuffix(lookupPath, "/") {
		lookupPath = strings.TrimRight(lookupPath, "/")
		if lookupPath == "" {
			lookupPath = "/"
		}
	}

	var c capture

	ref, res, allowed := m.tree.lookup(r.Method, lookupPath, &c)

	switch res {
	case resultNotFound:
		m.notFound(w, r)

		return

	case resultMethodNotAllowed:
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		m.methodNotAllowed(w, r)

		return
	}

	params := forge_http.AcquireRouteParams()

	for i, name := range ref.pattern.Params {
		if i >= c.len() {
			break
		}

		start, end := c.at(i)
		params.Set(name, lookupPath[start:end])
	}

	// The wildcard value is also reachable as "*". That alias is the contract
	// the bunrouter adapter established (it maps a param named "filepath" onto
	// "*"), and existing routes read it, so the matcher owes them both names.
	if n := len(ref.pattern.Segments); n > 0 && ref.pattern.Segments[n-1].Kind == pathspec.KindWildcard {
		if i := len(ref.pattern.Params) - 1; i >= 0 && i < c.len() {
			start, end := c.at(i)
			params.Set("*", lookupPath[start:end])
		}
	}

	// A long-lived route keeps its carrier: the handler may hold parameters
	// for the life of the connection, so recycling would hand it memory that
	// another request is using.
	if !ref.kind.LongLived() {
		defer forge_http.ReleaseRouteParams(params)
	}

	ctx := context.WithValue(r.Context(), forge_http.RouteParamsKey, params)
	ref.handler.ServeHTTP(w, r.WithContext(ctx))
}

func (m *Mux) notFound(w http.ResponseWriter, r *http.Request) {
	if m.cfg.NotFound != nil {
		m.cfg.NotFound.ServeHTTP(w, r)

		return
	}

	http.NotFound(w, r)
}

func (m *Mux) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	if m.cfg.MethodNotAllowed != nil {
		m.cfg.MethodNotAllowed.ServeHTTP(w, r)

		return
	}

	if m.cfg.NotFound != nil {
		m.cfg.NotFound.ServeHTTP(w, r)

		return
	}

	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}
