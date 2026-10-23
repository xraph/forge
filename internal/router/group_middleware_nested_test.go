package router

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/router/forgemux"
)

// groupMiddlewareAdapters lists every backend this package can build, so the
// inheritance rules below are checked against each of them. Group middleware is
// composed before the adapter ever sees the handler, but a backend that wraps
// or re-dispatches handlers could still drop a layer.
var groupMiddlewareAdapters = map[string]func() RouterAdapter{
	"forgemux":  func() RouterAdapter { return forgemux.New() },
	"bunrouter": NewBunRouterAdapter,
}

// rejectAll stands in for an auth middleware that refuses every request. A
// route it guards must never reach its handler.
func rejectAll(next Handler) Handler {
	return func(ctx Context) error {
		return ctx.String(http.StatusUnauthorized, "denied")
	}
}

// callLog records the order middleware and handlers run in.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = append(l.calls, name)
}

func (l *callLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.calls = nil
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.calls...)
}

func (l *callLog) mw(name string) Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			l.add(name)

			return next(ctx)
		}
	}
}

func (l *callLog) handler(name string) Handler {
	return func(ctx Context) error {
		l.add(name)

		return ctx.String(http.StatusOK, name)
	}
}

func doGet(t *testing.T, r Router, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	return rec
}

// TestGroupMiddleware_NestedGroupInheritsRejection is the security property: a
// group guarded by WithGroupMiddleware(auth) guards every group nested under
// it, at any depth.
func TestGroupMiddleware_NestedGroupInheritsRejection(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			r := NewRouter(WithAdapter(newAdapter()))
			reached := false
			ok := func(ctx Context) error {
				reached = true

				return ctx.String(http.StatusOK, "secret")
			}

			admin := r.Group("/admin", WithGroupMiddleware(rejectAll))
			v1 := admin.Group("/v1")
			deep := v1.Group("/deep")

			require.NoError(t, admin.GET("/direct", ok))
			require.NoError(t, v1.GET("/users", ok))
			require.NoError(t, deep.GET("/users", ok))

			for _, path := range []string{"/admin/direct", "/admin/v1/users", "/admin/v1/deep/users"} {
				rec := doGet(t, r, path)
				assert.Equal(t, http.StatusUnauthorized, rec.Code, "GET %s", path)
				assert.Equal(t, "denied", rec.Body.String(), "GET %s", path)
			}

			assert.False(t, reached, "a handler behind a rejecting group middleware ran")
		})
	}
}

// TestGroupMiddleware_NestedOrder pins the order: the outer group's middleware,
// whether from Use or WithGroupMiddleware, wraps everything the inner group
// adds. Auth on the outer group therefore runs before anything an inner group
// installs.
func TestGroupMiddleware_NestedOrder(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			log := &callLog{}
			r := NewRouter(WithAdapter(newAdapter()))

			r.Use(log.mw("root-use"))

			parent := r.Group("/p", WithGroupMiddleware(log.mw("parent-group")))
			parent.Use(log.mw("parent-use"))

			child := parent.Group("/c", WithGroupMiddleware(log.mw("child-group")))
			child.Use(log.mw("child-use"))

			grandchild := child.Group("/g", WithGroupMiddleware(log.mw("grandchild-group")))

			require.NoError(t, child.GET("/x", log.handler("child-handler"), WithMiddleware(log.mw("route"))))
			require.NoError(t, grandchild.GET("/x", log.handler("grandchild-handler")))

			rec := doGet(t, r, "/p/c/x")
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, []string{
				"root-use",
				"parent-use",
				"parent-group",
				"child-use",
				"child-group",
				"route",
				"child-handler",
			}, log.snapshot())

			log.reset()

			rec = doGet(t, r, "/p/c/g/x")
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, []string{
				"root-use",
				"parent-use",
				"parent-group",
				"child-use",
				"child-group",
				"grandchild-group",
				"grandchild-handler",
			}, log.snapshot())
		})
	}
}

// TestGroupMiddleware_RunsOncePerRoute guards against the obvious wrong fix:
// copying the parent's group middleware into both the inherited chain and the
// child's group config would run it twice.
func TestGroupMiddleware_RunsOncePerRoute(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			log := &callLog{}
			r := NewRouter(WithAdapter(newAdapter()))

			parent := r.Group("/p", WithGroupMiddleware(log.mw("m")))
			child := parent.Group("/c")
			grandchild := child.Group("/g")

			require.NoError(t, parent.GET("/x", log.handler("h")))
			require.NoError(t, child.GET("/x", log.handler("h")))
			require.NoError(t, grandchild.GET("/x", log.handler("h")))

			for _, path := range []string{"/p/x", "/p/c/x", "/p/c/g/x"} {
				log.reset()

				rec := doGet(t, r, path)
				require.Equal(t, http.StatusOK, rec.Code, "GET %s", path)
				assert.Equal(t, []string{"m", "h"}, log.snapshot(), "GET %s", path)
			}
		})
	}
}

// TestGroupMiddleware_SiblingsStayIsolated checks that inheritance flows down
// only: two groups under the same parent share the parent's middleware and
// nothing of each other's.
func TestGroupMiddleware_SiblingsStayIsolated(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			log := &callLog{}
			r := NewRouter(WithAdapter(newAdapter()))

			parent := r.Group("/p", WithGroupMiddleware(log.mw("parent")))
			a := parent.Group("/a", WithGroupMiddleware(log.mw("a")))
			b := parent.Group("/b", WithGroupMiddleware(log.mw("b")))
			a2 := a.Group("/n")
			b2 := b.Group("/n")
			other := r.Group("/other")

			require.NoError(t, a.GET("/x", log.handler("h")))
			require.NoError(t, b.GET("/x", log.handler("h")))
			require.NoError(t, a2.GET("/x", log.handler("h")))
			require.NoError(t, b2.GET("/x", log.handler("h")))
			require.NoError(t, other.GET("/x", log.handler("h")))

			cases := map[string][]string{
				"/p/a/x":   {"parent", "a", "h"},
				"/p/b/x":   {"parent", "b", "h"},
				"/p/a/n/x": {"parent", "a", "h"},
				"/p/b/n/x": {"parent", "b", "h"},
				"/other/x": {"h"},
			}

			for path, want := range cases {
				log.reset()

				rec := doGet(t, r, path)
				require.Equal(t, http.StatusOK, rec.Code, "GET %s", path)
				assert.Equal(t, want, log.snapshot(), "GET %s", path)
			}
		})
	}
}

// TestGroupMiddleware_NestedRouteInfo checks the inspection surface agrees with
// what runs: a nested route lists the parent's group middleware exactly once.
func TestGroupMiddleware_NestedRouteInfo(t *testing.T) {
	r := NewRouter()
	log := &callLog{}

	parent := r.Group("/p", WithGroupMiddleware(log.mw("m")))
	child := parent.Group("/c", WithGroupMiddleware(log.mw("n")))

	require.NoError(t, child.GET("/y", log.handler("h"), WithName("nested")))

	info, ok := r.RouteByName("nested")
	require.True(t, ok, "route nested not found")
	assert.Len(t, info.Middleware, 2)
}

// TestGroupMiddleware_NestedGroupKeepsIdempotency covers the other option that
// writes group middleware: WithGroupIdempotent must reach nested groups too,
// or a nested write advertises replay safety it does not have.
func TestGroupMiddleware_NestedGroupKeepsIdempotency(t *testing.T) {
	log := &callLog{}
	r := NewRouter()

	parent := r.Group("/p", WithGroupIdempotent(log.mw("idem")))
	child := parent.Group("/c")

	require.NoError(t, child.POST("/x", log.handler("h")))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/p/c/x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"idem", "h"}, log.snapshot())
}

// prefixedController is a ControllerWithPrefix and ControllerWithMiddleware
// with one GET route, for checking that RegisterController on a group keeps the
// group's middleware.
type prefixedController struct {
	prefix string
	mw     []Middleware
	h      Handler
}

func (c *prefixedController) Name() string { return "prefixed" }

func (c *prefixedController) Prefix() string { return c.prefix }

func (c *prefixedController) Middleware() []Middleware { return c.mw }

func (c *prefixedController) Routes(r Router) error { return r.GET("/x", c.h) }

// TestGroupMiddleware_PrefixedControllerOnGroup covers RegisterController with
// a prefix, which builds a nested group under the hood. A rejecting group
// middleware must guard it, and the controller's own middleware runs inside the
// group's.
func TestGroupMiddleware_PrefixedControllerOnGroup(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			r := NewRouter(WithAdapter(newAdapter()))
			log := &callLog{}

			guarded := r.Group("/guarded", WithGroupMiddleware(rejectAll))
			require.NoError(t, guarded.RegisterController(&prefixedController{
				prefix: "/ctl",
				mw:     []Middleware{log.mw("ctl-mw")},
				h:      log.handler("h"),
			}))

			rec := doGet(t, r, "/guarded/ctl/x")
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Empty(t, log.snapshot(), "a controller route behind a rejecting group middleware ran")

			g := r.Group("/g", WithGroupMiddleware(log.mw("g")))
			g.Use(log.mw("g-use"))
			require.NoError(t, g.RegisterController(&prefixedController{
				prefix: "/ctl",
				mw:     []Middleware{log.mw("ctl-mw")},
				h:      log.handler("h"),
			}))

			rec = doGet(t, r, "/g/ctl/x")
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, []string{"g-use", "g", "ctl-mw", "h"}, log.snapshot())
		})
	}
}

// TestGroupMiddleware_LateUseDoesNotReachExistingChild pins the snapshot rule:
// a nested group copies its parent's chain when it is created, so Use on the
// parent afterwards reaches the parent's routes and not the child's.
func TestGroupMiddleware_LateUseDoesNotReachExistingChild(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			r := NewRouter(WithAdapter(newAdapter()))
			log := &callLog{}

			p := r.Group("/p")
			c := p.Group("/c")
			p.Use(log.mw("late"))

			require.NoError(t, p.GET("/x", log.handler("h")))
			require.NoError(t, c.GET("/x", log.handler("h")))

			require.Equal(t, http.StatusOK, doGet(t, r, "/p/x").Code)
			assert.Equal(t, []string{"late", "h"}, log.snapshot())

			log.reset()

			require.Equal(t, http.StatusOK, doGet(t, r, "/p/c/x").Code)
			assert.Equal(t, []string{"h"}, log.snapshot())
		})
	}
}
