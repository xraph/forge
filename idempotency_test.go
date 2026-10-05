package forge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge/internal/idempotency"
)

// signedInAs stands in for the auth middleware: the idempotency middleware
// scopes keys by principal and, by default, ignores a request without one.
func signedInAs(subject string) Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			ctx.Set("auth.subject", subject)

			return next(ctx)
		}
	}
}

func postWithKey(t *testing.T, r Router, path, key string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(`{"total":1}`))
	req.Header.Set("Idempotency-Key", key)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	return rec
}

func TestWithIdempotencyDeduplicatesThroughTheRouter(t *testing.T) {
	var calls atomic.Int32

	r := NewRouter()

	err := r.POST("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
	}, WithMiddleware(signedInAs("alice")), WithIdempotency(IdempotencyBackend(idempotency.NewMemoryStore())))
	if err != nil {
		t.Fatal(err)
	}

	first := postWithKey(t, r, "/orders", "k1")
	second := postWithKey(t, r, "/orders", "k1")

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}

	if second.Code != http.StatusCreated || second.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %q, want %d %q", second.Code, second.Body.String(), first.Code, first.Body.String())
	}

	if second.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("replay is missing the %s header", idempotency.ReplayedHeader)
	}
}

// An anonymous caller shares the empty principal with every other anonymous
// caller, so the middleware runs the handler as if no key was sent. That is the
// default forge.WithIdempotency must keep.
func TestWithIdempotencyDoesNotDeduplicateAnonymousCallersByDefault(t *testing.T) {
	var calls atomic.Int32

	r := NewRouter()

	err := r.POST("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
	}, WithIdempotency(IdempotencyBackend(idempotency.NewMemoryStore())))
	if err != nil {
		t.Fatal(err)
	}

	postWithKey(t, r, "/orders", "k1")
	postWithKey(t, r, "/orders", "k1")

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times for an anonymous caller, want 2 (no dedup without a principal)", calls.Load())
	}
}

// The middleware options reach the middleware: IdempotencyAllowAnonymous is
// the opt-in that makes a route with no auth deduplicate.
func TestWithIdempotencyPassesTheAnonymousOptInThrough(t *testing.T) {
	var calls atomic.Int32

	r := NewRouter()

	err := r.POST("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
	}, WithIdempotency(
		IdempotencyBackend(idempotency.NewMemoryStore()),
		idempotency.AllowAnonymous(),
	))
	if err != nil {
		t.Fatal(err)
	}

	postWithKey(t, r, "/orders", "k1")
	second := postWithKey(t, r, "/orders", "k1")

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times with AllowAnonymous, want 1", calls.Load())
	}

	if second.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("second response was not a replay: %d %q", second.Code, second.Body.String())
	}
}

func TestWithIdempotencyRequireKeyOptionReachesTheMiddleware(t *testing.T) {
	r := NewRouter()

	err := r.POST("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusCreated, map[string]string{"ok": "yes"})
	}, WithMiddleware(signedInAs("alice")), WithIdempotency(
		IdempotencyBackend(idempotency.NewMemoryStore()),
		idempotency.RequireKey(),
	))
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders", strings.NewReader(`{}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a write without a key got %d, want 400", rec.Code)
	}
}

func TestWithGroupIdempotencyCoversEveryRouteAndTheDocument(t *testing.T) {
	var calls atomic.Int32

	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "Orders", Version: "1.0.0", SpecEnabled: true}))
	g := r.Group("/v1",
		WithGroupMiddleware(signedInAs("alice")),
		WithGroupIdempotency(IdempotencyBackend(idempotency.NewMemoryStore())),
	)

	if err := g.POST("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
	}); err != nil {
		t.Fatal(err)
	}

	if err := g.GET("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusOK, []string{})
	}); err != nil {
		t.Fatal(err)
	}

	postWithKey(t, r, "/v1/orders", "k1")
	postWithKey(t, r, "/v1/orders", "k1")

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}

	spec := r.OpenAPISpec()
	if spec == nil {
		t.Fatal("no OpenAPI document")
	}

	item, ok := spec.Paths["/v1/orders"]
	if !ok || item.Post == nil || item.Get == nil {
		t.Fatalf("POST and GET /v1/orders missing from %v", spec.Paths)
	}

	if v, _ := item.Post.Extensions["x-forge-idempotent"].(bool); !v {
		t.Fatalf("POST x-forge-idempotent = %#v, want true", item.Post.Extensions["x-forge-idempotent"])
	}

	if _, marked := item.Get.Extensions["x-forge-idempotent"]; marked {
		t.Fatalf("GET in the group is marked x-forge-idempotent: %#v", item.Get.Extensions)
	}
}

func TestWithGroupIdempotencyPassesTheAnonymousOptInThrough(t *testing.T) {
	var calls atomic.Int32

	r := NewRouter()
	g := r.Group("/v1", WithGroupIdempotency(
		IdempotencyBackend(idempotency.NewMemoryStore()),
		idempotency.AllowAnonymous(),
	))

	if err := g.POST("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
	}); err != nil {
		t.Fatal(err)
	}

	postWithKey(t, r, "/v1/orders", "k1")
	postWithKey(t, r, "/v1/orders", "k1")

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times with AllowAnonymous on the group, want 1", calls.Load())
	}
}

// A group that opted in and a route inside it that opted in again are two
// layers of the same middleware over one store. The inner layer must see that
// the outer one is already handling the request and step aside. Before it did,
// the inner layer found the key claimed by the outer layer, waited out the
// timeout, answered 409 without running the handler, and the outer layer then
// stored that 409 for the whole TTL.
func TestGroupAndRouteIdempotencyShareOneClaim(t *testing.T) {
	var calls atomic.Int32

	store := idempotency.NewMemoryStore()
	opts := []IdempotencyOption{IdempotencyBackend(store), idempotency.WaitTimeout(200 * time.Millisecond)}

	r := NewRouter()
	g := r.Group("/v1", WithGroupMiddleware(signedInAs("alice")), WithGroupIdempotency(opts...))

	if err := g.POST("/orders", func(ctx Context) error {
		return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
	}, WithIdempotency(opts...)); err != nil {
		t.Fatal(err)
	}

	first := postWithKey(t, r, "/v1/orders", "k1")
	if first.Code != http.StatusCreated || strings.TrimSpace(first.Body.String()) != `{"n":1}` {
		t.Fatalf("first = %d %q, want 201 {\"n\":1}", first.Code, first.Body.String())
	}

	second := postWithKey(t, r, "/v1/orders", "k1")

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}

	if second.Code != http.StatusCreated || second.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %q, want %d %q", second.Code, second.Body.String(), first.Code, first.Body.String())
	}

	if second.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("the repeat was not a replay: %d %q", second.Code, second.Body.String())
	}
}

// authContext has the shape of the auth extension's AuthContext, which that
// extension stores under "auth_context". The extension is its own module, so
// the root package cannot import the real type; the middleware reads only the
// Subject field, by name.
type authContext struct {
	Subject     string
	Claims      map[string]any
	Scopes      []string
	Roles       []string
	Permissions []string
	Metadata    map[string]any
}

func TestWithIdempotencyReadsThePrincipalFromTheAuthContext(t *testing.T) {
	var calls atomic.Int32

	signIn := func(subject string) Middleware {
		return func(next Handler) Handler {
			return func(ctx Context) error {
				ctx.Set("auth_context", &authContext{Subject: subject, Roles: []string{"admin"}})

				return next(ctx)
			}
		}
	}

	store := idempotency.NewMemoryStore()
	r := NewRouter()

	for _, who := range []string{"alice", "bob"} {
		if err := r.POST("/as-"+who, func(ctx Context) error {
			return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
		}, WithMiddleware(signIn(who)), WithIdempotency(IdempotencyBackend(store))); err != nil {
			t.Fatal(err)
		}
	}

	postWithKey(t, r, "/as-alice", "k1")
	postWithKey(t, r, "/as-alice", "k1")

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times for one principal, want 1", calls.Load())
	}

	// The same key from another principal is a different operation.
	postWithKey(t, r, "/as-bob", "k1")

	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times across two principals, want 2", calls.Load())
	}
}

// Middleware runs in this order: router Use, group middleware, then route
// middleware in option order. The principal is read when the idempotency
// middleware runs, so auth registered after it has not run yet, every request
// looks anonymous, and nothing is deduplicated. These pin that order so a
// change to it is deliberate.
func TestIdempotencyNeedsAuthToRunBeforeIt(t *testing.T) {
	handler := func(calls *atomic.Int32) Handler {
		return func(ctx Context) error {
			return ctx.JSON(http.StatusCreated, map[string]int32{"n": calls.Add(1)})
		}
	}

	cases := []struct {
		name  string
		setup func(r Router, calls *atomic.Int32, opt IdempotencyOption) error
		want  int32
	}{
		{"route: auth before idempotency", func(r Router, calls *atomic.Int32, opt IdempotencyOption) error {
			return r.POST("/orders", handler(calls), WithMiddleware(signedInAs("alice")), WithIdempotency(opt))
		}, 1},
		{"router: auth in Use before route idempotency", func(r Router, calls *atomic.Int32, opt IdempotencyOption) error {
			r.Use(signedInAs("alice"))

			return r.POST("/orders", handler(calls), WithIdempotency(opt))
		}, 1},
		{"route: auth after idempotency", func(r Router, calls *atomic.Int32, opt IdempotencyOption) error {
			return r.POST("/orders", handler(calls), WithIdempotency(opt), WithMiddleware(signedInAs("alice")))
		}, 2},
		{"group: auth in the group before idempotency", func(r Router, calls *atomic.Int32, opt IdempotencyOption) error {
			g := r.Group("/v1", WithGroupMiddleware(signedInAs("alice")), WithGroupIdempotency(opt))

			return g.POST("/orders", handler(calls))
		}, 1},
		{"group: auth in the group after idempotency", func(r Router, calls *atomic.Int32, opt IdempotencyOption) error {
			g := r.Group("/v1", WithGroupIdempotency(opt), WithGroupMiddleware(signedInAs("alice")))

			return g.POST("/orders", handler(calls))
		}, 2},
		{"group idempotency, auth on the route", func(r Router, calls *atomic.Int32, opt IdempotencyOption) error {
			g := r.Group("/v1", WithGroupIdempotency(opt))

			return g.POST("/orders", handler(calls), WithMiddleware(signedInAs("alice")))
		}, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32

			r := NewRouter()
			// Its own store, so a repeated run (go test -count=2) starts clean.
			if err := tc.setup(r, &calls, IdempotencyBackend(idempotency.NewMemoryStore())); err != nil {
				t.Fatal(err)
			}

			path := "/orders"
			if strings.Contains(tc.name, "group") {
				path = "/v1/orders"
			}

			postWithKey(t, r, path, tc.name)
			postWithKey(t, r, path, tc.name)

			if calls.Load() != tc.want {
				t.Fatalf("handler ran %d times, want %d", calls.Load(), tc.want)
			}
		})
	}
}
