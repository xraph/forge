package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/di"
)

// Benchmarks for router performance

func BenchmarkRouter_Registration(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		router := NewRouter()
		_ = router.GET("/test", func(ctx Context) error {
			return ctx.String(200, "ok")
		})
	}
}

func BenchmarkRouter_StandardHandler(b *testing.B) {
	router := NewRouter()
	_ = router.GET("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_ContextHandler(b *testing.B) {
	router := NewRouter()
	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
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

	router := NewRouter()
	_ = router.POST("/test", func(ctx Context, req *TestRequest) (*TestResponse, error) {
		return &TestResponse{Name: req.Name}, nil
	})

	reqBody := TestRequest{Name: "test"}
	body, _ := json.Marshal(reqBody)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/test", bytes.NewReader(body))
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

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_Middleware(b *testing.B) {
	router := NewRouter()

	// Add middleware
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	})

	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkRouter_MiddlewareChain(b *testing.B) {
	router := NewRouter()

	// Add 5 middleware
	for j := 0; j < 5; j++ {
		router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		})
	}

	_ = router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkContext_JSON(b *testing.B) {
	data := map[string]string{"message": "hello"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		ctx := di.NewContext(rec, req, nil)
		_ = ctx.JSON(200, data)
	}
}

func BenchmarkContext_String(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
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

	for i := 0; i < b.N; i++ {
		_, _ = detectHandlerPattern(handler)
	}
}

func BenchmarkHandler_Conversion(b *testing.B) {
	handler := func(ctx Context) error {
		return ctx.String(200, "ok")
	}

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = convertHandler(handler, nil, nil)
	}
}
