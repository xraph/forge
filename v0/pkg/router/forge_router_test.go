package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Mock logger for tests
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Info(msg string, fields ...logger.Field)       {}
func (m *mockLogger) Warn(msg string, fields ...logger.Field)       {}
func (m *mockLogger) Error(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Fatal(msg string, fields ...logger.Field)      {}
func (m *mockLogger) Debugf(template string, args ...interface{})   {}
func (m *mockLogger) Infof(template string, args ...interface{})    {}
func (m *mockLogger) Warnf(template string, args ...interface{})    {}
func (m *mockLogger) Errorf(template string, args ...interface{})   {}
func (m *mockLogger) Fatalf(template string, args ...interface{})   {}
func (m *mockLogger) Named(name string) logger.Logger               { return m }
func (m *mockLogger) Sugar() logger.SugarLogger                     { return nil }
func (m *mockLogger) Sync() error                                   { return nil }
func (m *mockLogger) With(fields ...logger.Field) common.Logger     { return m }
func (m *mockLogger) WithContext(ctx context.Context) common.Logger { return m }

func createTestRouter() *ForgeRouter {
	config := ForgeRouterConfig{
		Logger: &mockLogger{},
	}
	return NewForgeRouter(config).(*ForgeRouter)
}

func TestRouterBasicFunctionality(t *testing.T) {
	router := createTestRouter()

	// Test GET route using Mount
	router.Mount("/test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("GET test"))
		} else if r.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("POST test"))
		} else {
			http.NotFound(w, r)
		}
	}))

	// Test route with parameters using Mount
	router.Mount("/users/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract user ID from path
		path := strings.TrimPrefix(r.URL.Path, "/users/")
		if path == "" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("User: " + path))
	}))

	// Test routes
	t.Run("GET /test", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		if w.Body.String() != "GET test" {
			t.Errorf("Expected body 'GET test', got '%s'", w.Body.String())
		}
	})

	t.Run("POST /test", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
		}
		if w.Body.String() != "POST test" {
			t.Errorf("Expected body 'POST test', got '%s'", w.Body.String())
		}
	})

	t.Run("GET /users/123", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/123", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		if w.Body.String() != "User: 123" {
			t.Errorf("Expected body 'User: 123', got '%s'", w.Body.String())
		}
	})
}

func TestRouterMiddleware(t *testing.T) {
	router := createTestRouter()

	// Add middleware
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Middleware", "test")
			next.ServeHTTP(w, r)
		})
	})

	// Add route
	router.GET("/middleware-test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("middleware test"))
	})

	req := httptest.NewRequest("GET", "/middleware-test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	if w.Header().Get("X-Middleware") != "test" {
		t.Errorf("Expected X-Middleware header 'test', got '%s'", w.Header().Get("X-Middleware"))
	}
}

func TestRouterConcurrentAccess(t *testing.T) {
	router := createTestRouter()

	// Add a route that takes some time
	router.GET("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("slow response"))
	})

	// Test concurrent requests
	var wg sync.WaitGroup
	numRequests := 10

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/slow", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}
		}()
	}

	wg.Wait()
}

func TestRouterLifecycle(t *testing.T) {
	router := createTestRouter()

	// Test start
	ctx := context.Background()
	err := router.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start router: %v", err)
	}

	// Test stop
	err = router.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop router: %v", err)
	}
}

func TestRouterRaceConditions(t *testing.T) {
	router := createTestRouter()

	// Test concurrent route registration and serving
	var wg sync.WaitGroup

	// Concurrent route registration
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			router.GET("/concurrent/"+string(rune(i)), func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("concurrent"))
			})
		}
	}()

	// Concurrent requests
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			req := httptest.NewRequest("GET", fmt.Sprintf("/concurrent/%d", i), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
		}
	}()

	wg.Wait()
}

func TestRouterErrorHandling(t *testing.T) {
	router := createTestRouter()

	// Test route that panics
	router.GET("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	// Test route that returns error
	router.GET("/error", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "test error", http.StatusInternalServerError)
	})

	// Test panic recovery
	t.Run("panic route", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/panic", nil)
		w := httptest.NewRecorder()

		// This should not panic the test
		router.ServeHTTP(w, req)

		// Should return 500 status
		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
		}
	})

	// Test error handling
	t.Run("error route", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/error", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
		}
		if w.Body.String() != "test error\n" {
			t.Errorf("Expected body 'test error\\n', got '%s'", w.Body.String())
		}
	})
}

func TestRouterNotFound(t *testing.T) {
	router := createTestRouter()

	// Test 404 handling
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRouterMethodNotAllowed(t *testing.T) {
	router := createTestRouter()

	// Register only GET route
	router.GET("/method-test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("GET only"))
	})

	// Test POST to GET-only route
	req := httptest.NewRequest("POST", "/method-test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}
