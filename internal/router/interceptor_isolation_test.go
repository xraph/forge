package router

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	forge_http "github.com/xraph/go-utils/http"
	"github.com/xraph/vessel"
)

// recordingInterceptor allows a request and records that it ran.
func recordingInterceptor(name string, seen *[]string, mu *sync.Mutex) Interceptor {
	return NewInterceptor(name, func(Context, RouteInfo) InterceptorResult {
		mu.Lock()

		*seen = append(*seen, name)

		mu.Unlock()

		return Allow()
	})
}

// Group interceptors are inherited by appending onto the group's slice. If that
// slice is not cloned first, each route writes its own interceptor into the
// group's spare capacity and the previously registered route silently picks up
// the later route's interceptor — dropping, for example, a route's auth check.
func TestGroupInterceptorsAreNotSharedBetweenRoutes(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)

	r := NewRouter(WithContainer(vessel.New()))

	// Three separate options so the group's slice grows incrementally and ends
	// up with capacity beyond its length — the condition that exposes aliasing.
	group := r.Group("/api",
		WithGroupInterceptor(recordingInterceptor("group1", &seen, &mu)),
		WithGroupInterceptor(recordingInterceptor("group2", &seen, &mu)),
		WithGroupInterceptor(recordingInterceptor("group3", &seen, &mu)),
	)

	handler := func(ctx Context) error { return ctx.String(http.StatusOK, "ok") }

	if err := group.GET("/private", handler,
		WithInterceptor(recordingInterceptor("requireAuth", &seen, &mu))); err != nil {
		t.Fatal(err)
	}

	if err := group.GET("/public", handler,
		WithInterceptor(recordingInterceptor("publicOnly", &seen, &mu))); err != nil {
		t.Fatal(err)
	}

	// Hit the route registered FIRST — it is the one a later registration can
	// have clobbered.
	mu.Lock()
	seen = nil
	mu.Unlock()

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/private", nil))

	mu.Lock()

	got := append([]string(nil), seen...)

	mu.Unlock()

	want := []string{"group1", "group2", "group3", "requireAuth"}
	if len(got) != len(want) {
		t.Fatalf("interceptors run = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("interceptor %d = %q, want %q (full chain %v)", i, got[i], want[i], got)
		}
	}
}

// shareContextValues aliases the lender's values map into the borrower. If the
// borrower is returned to the shared pool still holding that alias, the next
// unrelated request draws it out, and NewContext's clear() wipes — then
// overwrites — the lender's map while the lender is still in flight. The map
// holds request identity and tenant scope, so this crosses request boundaries.
func TestBorrowedValuesMapIsNotReturnedToThePool(t *testing.T) {
	newCtx := func() Context {
		return forge_http.NewContext(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/", nil),
			nil,
		)
	}

	// Request 1: an outer context plus a middleware-bridge context that borrows
	// its values map, then finishes and is pooled.
	lender := newCtx()
	lender.Set("tenant", "acme")

	borrower := newCtx()
	release := shareContextValues(lender, borrower)
	cleanupBorrowedContext(borrower, release)

	// Request 2: fresh context, likely drawn from the pool.
	other := newCtx()

	if got := other.Get("tenant"); got != nil {
		t.Errorf("new request observed the previous request's data: tenant=%v", got)
	}

	other.Set("tenant", "evilcorp")

	if got := lender.Get("tenant"); got != "acme" {
		t.Errorf("in-flight request's context was overwritten by a later request: tenant=%v, want acme", got)
	}
}

// Parallel and ParallelAny hand the same context to N goroutines. The context
// has no internal synchronization, and an unsynchronized map write is a fatal
// runtime error that recover() cannot catch — it takes the process down. Run
// under -race to catch the regression.
func TestParallelInterceptorsDoNotRaceOnContext(t *testing.T) {
	ctx := forge_http.NewContext(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/dashboard", nil),
		nil,
	)

	touch := func(key string) Interceptor {
		return NewInterceptor("touch:"+key, func(c Context, _ RouteInfo) InterceptorResult {
			for i := range 200 {
				c.Set(key, i)
				_ = c.Get(key)
			}

			return AllowWithValues(map[string]any{key: "done"})
		})
	}

	route := RouteInfo{Path: "/dashboard"}

	if res := Parallel(touch("a"), touch("b"), touch("c"), touch("d")).Intercept(ctx, route); res.Blocked {
		t.Errorf("Parallel blocked unexpectedly: %v", res.Error)
	}

	if res := ParallelAny(touch("e"), touch("f"), touch("g")).Intercept(ctx, route); res.Blocked {
		t.Errorf("ParallelAny blocked unexpectedly: %v", res.Error)
	}
}

// Once a fan-out has resolved, stragglers must not keep mutating a context the
// handler has already returned — the pool may have reassigned it.
func TestDetachedSyncContextStopsWriting(t *testing.T) {
	inner := forge_http.NewContext(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil),
		nil,
	)
	inner.Set("owner", "request-1")

	guard := &contextGuard{}
	wrapped := newSyncContext(inner, guard)

	guard.detach()

	wrapped.Set("owner", "straggler")

	if got := inner.Get("owner"); got != "request-1" {
		t.Errorf("detached wrapper still wrote through: owner=%v, want request-1", got)
	}

	if got := wrapped.Get("owner"); got != nil {
		t.Errorf("detached wrapper still read through: owner=%v, want nil", got)
	}
}
