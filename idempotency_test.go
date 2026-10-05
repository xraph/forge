package forge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

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
