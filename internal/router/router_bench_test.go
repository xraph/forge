package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
	"github.com/xraph/vessel"
)

// Benchmarks for router performance.
//
// Requests and response writers are built ONCE, outside the timed loop. They
// used to be constructed per iteration, which made these benchmarks mostly a
// measurement of net/http: profiling showed httptest.NewRequestWithContext at
// 46% of allocations against router.ServeHTTP at 39%. Anyone optimising
// against the old numbers was optimising request parsing.
//
// Keep it that way. If a benchmark needs a fresh request per iteration, say
// why in a comment, because it changes what the number means.

// benchWriter is a discarding ResponseWriter. httptest.ResponseRecorder
// accumulates every response body across iterations, so reusing one both
// allocates and grows without bound.
type benchWriter struct{ h http.Header }

func (d *benchWriter) Header() http.Header {
	if d.h == nil {
		d.h = make(http.Header)
	}

	return d.h
}

func (d *benchWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *benchWriter) WriteHeader(int)             {}

// nopCloser lets a body be rewound between iterations without allocating.
// A single-pointer struct stored in an interface does not escape to the heap.
type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

func BenchmarkRouter_Registration(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		router := NewRouter()
		_ = router.GET("/test", func(ctx Context) error {
			return ctx.String(200, "ok")
		})
	}
}

func BenchmarkRouter_StandardHandler(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)
	_ = router.GET("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_ContextHandler(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)
	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.ServeHTTP(w, req)
	}
}

// ctxHandlerAlias has the same underlying signature as a context handler but
// does not assert to it, so registering one forces the reflect path.
//
// This exists so both paths can be measured in a single binary under identical
// conditions. If the gap between this and BenchmarkRouter_ContextHandler ever
// closes, the direct-call fast path in detectHandlerPattern has stopped firing.
type ctxHandlerAlias func(Context) error

func BenchmarkRouter_ContextHandler_ReflectPath(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)

	var h ctxHandlerAlias = func(ctx Context) error {
		return ctx.String(200, "ok")
	}

	_ = router.GET("/test", h)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_ContextHandlerWithParams(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)
	_ = router.GET("/users/:id/posts/:pid", func(ctx Context) error {
		return ctx.String(200, ctx.Param("id"))
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42/posts/7", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_OpinionatedHandler(b *testing.B) {
	type TestRequest struct {
		Name string `json:"name"`
	}

	type TestResponse struct {
		Name string `json:"name"`
	}

	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)
	_ = router.POST("/test", func(ctx Context, req *TestRequest) (*TestResponse, error) {
		return &TestResponse{Name: req.Name}, nil
	})

	body, err := json.Marshal(TestRequest{Name: "test"})
	if err != nil {
		b.Fatal(err)
	}

	reader := bytes.NewReader(body)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Content-Type", "application/json")

	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		// The body is consumed each request, and forge wraps it in a
		// MaxBytesReader, so it has to be rewound rather than reused as-is.
		reader.Reset(body)
		req.Body = nopCloser{reader}
		req.ContentLength = int64(len(body))

		router.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_ServiceHandler(b *testing.B) {
	container := vessel.New()
	_ = container.Register("github.com/xraph/forge/internal/router.TestUserService", func(c vessel.Vessel) (any, error) {
		return &TestUserService{users: []string{"user1"}}, nil
	})

	router := NewRouter(WithContainer(container))
	_ = router.GET("/test", func(ctx Context, svc *TestUserService) error {
		users := svc.GetAll()

		return ctx.JSON(200, users)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_Middleware(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)

	router.Use(func(next Handler) Handler {
		return func(ctx Context) error {
			return next(ctx)
		}
	})

	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_MiddlewareChain(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)

	for range 5 {
		router.Use(func(next Handler) Handler {
			return func(ctx Context) error {
				return next(ctx)
			}
		})
	}

	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		router.ServeHTTP(w, req)
	}
}

func BenchmarkContext_JSON(b *testing.B) {
	data := map[string]string{"message": "hello"}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		ctx := forge_http.NewContext(w, req, nil)
		_ = ctx.JSON(200, data)
		ctx.(forge_http.ContextWithClean).Cleanup()
	}
}

func BenchmarkContext_String(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		ctx := forge_http.NewContext(w, req, nil)
		_ = ctx.String(200, "hello")
		ctx.(forge_http.ContextWithClean).Cleanup()
	}
}

func BenchmarkHandler_PatternDetection(b *testing.B) {
	handler := func(ctx Context) error {
		return ctx.String(200, "ok")
	}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = detectHandlerPattern(handler)
	}
}

func BenchmarkHandler_Conversion(b *testing.B) {
	handler := func(ctx Context) error {
		return ctx.String(200, "ok")
	}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = convertHandler(handler, nil, nil)
	}
}

// Ensure the benchmarks measure a working router, not a 404. A benchmark that
// silently stopped matching would look like a large improvement.
func TestBenchmarkFixturesActuallyMatch(t *testing.T) {
	cases := []struct {
		name   string
		build  func() Router
		method string
		path   string
	}{
		{
			name: "standard",
			build: func() Router {
				r := NewRouter()
				_ = r.GET("/test", func(w http.ResponseWriter, req *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("ok"))
				})

				return r
			},
			method: http.MethodGet, path: "/test",
		},
		{
			name: "context",
			build: func() Router {
				r := NewRouter()
				_ = r.GET("/test", func(ctx Context) error { return ctx.String(200, "ok") })

				return r
			},
			method: http.MethodGet, path: "/test",
		},
		{
			name: "context via the reflect path",
			build: func() Router {
				r := NewRouter()

				var h ctxHandlerAlias = func(ctx Context) error { return ctx.String(200, "ok") }
				_ = r.GET("/test", h)

				return r
			},
			method: http.MethodGet, path: "/test",
		},
		{
			name: "context with params",
			build: func() Router {
				r := NewRouter()
				_ = r.GET("/users/:id/posts/:pid", func(ctx Context) error {
					return ctx.String(200, ctx.Param("id"))
				})

				return r
			},
			method: http.MethodGet, path: "/users/42/posts/7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.build().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("benchmark fixture does not match: got %d, want 200", rec.Code)
			}

			if rec.Body.Len() == 0 {
				t.Fatal("benchmark fixture produced no body; the handler may not be running")
			}
		})
	}
}

// The rewound-body fixture has to survive more than one pass, or the
// opinionated benchmark measures a 400 after its first iteration.
func TestOpinionatedBenchmarkFixtureRepeats(t *testing.T) {
	type TestRequest struct {
		Name string `json:"name"`
	}

	type TestResponse struct {
		Name string `json:"name"`
	}

	r := NewRouter()
	_ = r.POST("/test", func(ctx Context, req *TestRequest) (*TestResponse, error) {
		return &TestResponse{Name: req.Name}, nil
	})

	body, err := json.Marshal(TestRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}

	reader := bytes.NewReader(body)

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Content-Type", "application/json")

	for i := range 3 {
		reader.Reset(body)
		req.Body = nopCloser{reader}
		req.ContentLength = int64(len(body))

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("iteration %d: got %d, want 200: %s", i, rec.Code, rec.Body.String())
		}
	}
}

// BenchmarkRouter_OpinionatedHandler binds a single json field, which makes it
// blind to most of what binding actually costs. Request structs in real
// services carry path, query and header fields too, and the binder's cost
// scales with that count.
//
// This fixture exists so work on the bind path is visible here rather than
// only in go-utils' own benchmarks.
func BenchmarkRouter_OpinionatedHandler_RealisticRequest(b *testing.B) {
	type ListRequest struct {
		OrgID     string `path:"orgId"          validate:"required"`
		UserID    string `path:"userId"         validate:"required"`
		Page      int    `query:"page"`
		PerPage   int    `query:"perPage"`
		Search    string `query:"search"`
		RequestID string `header:"X-Request-Id"`
		Name      string `json:"name"           validate:"required"`
		Note      string `json:"note"`
	}

	type ListResponse struct {
		Name string `json:"name"`
	}

	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)
	_ = router.POST("/orgs/:orgId/users/:userId", func(ctx Context, req *ListRequest) (*ListResponse, error) {
		return &ListResponse{Name: req.Name}, nil
	})

	body, err := json.Marshal(map[string]any{"name": "rex", "note": "hello"})
	if err != nil {
		b.Fatal(err)
	}

	reader := bytes.NewReader(body)

	req := httptest.NewRequest(
		http.MethodPost, "/orgs/o1/users/u1?page=2&perPage=50&search=abc", nil,
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req-123")

	w := &benchWriter{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		reader.Reset(body)

		req.Body = nopCloser{reader}
		req.ContentLength = int64(len(body))

		router.ServeHTTP(w, req)
	}
}
