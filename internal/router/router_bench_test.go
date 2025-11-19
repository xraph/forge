package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/di"
	"github.com/xraph/forge/internal/logger"
)

// Benchmarks for router performance

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
		w.Write([]byte("ok"))
	})

	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_ContextHandler(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)
	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
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

	reqBody := TestRequest{Name: "test"}
	body, _ := json.Marshal(reqBody)

	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_ServiceHandler(b *testing.B) {
	container := di.NewContainer()
	_ = di.RegisterSingleton(container, "github.com/xraph/forge/internal/router.TestUserService", func(c di.Container) (*TestUserService, error) {
		return &TestUserService{users: []string{"user1"}}, nil
	})

	router := NewRouter(WithContainer(container))
	_ = router.GET("/test", func(ctx Context, svc *TestUserService) error {
		users := svc.GetAll()

		return ctx.JSON(200, users)
	})

	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_Middleware(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)

	// Add middleware
	router.Use(func(next Handler) Handler {
		return func(ctx Context) error {
			return next(ctx)
		}
	})

	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_MiddlewareChain(b *testing.B) {
	router := NewRouter(
		WithLogger(logger.NewTestLogger()),
	)

	// Add 5 middleware
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

	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkContext_JSON(b *testing.B) {
	data := map[string]string{"message": "hello"}

	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		ctx := di.NewContext(rec, req, nil)
		_ = ctx.JSON(200, data)
	}
}

func BenchmarkContext_String(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		ctx := di.NewContext(rec, req, nil)
		_ = ctx.String(200, "hello")
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
