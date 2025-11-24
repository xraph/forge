package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/di"
)

// Test service for DI injection.
type TestUserService struct {
	users []string
}

func (s *TestUserService) GetAll() []string {
	return s.users
}

func (s *TestUserService) GetByID(id string) string {
	for _, user := range s.users {
		if user == id {
			return user
		}
	}

	return ""
}

// Test request/response types.
type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CreateUserResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestNewRouter(t *testing.T) {
	router := NewRouter()
	assert.NotNil(t, router)
}

func TestNewRouter_WithOptions(t *testing.T) {
	container := di.NewContainer()

	router := NewRouter(
		WithContainer(container),
		WithRecovery(),
	)

	assert.NotNil(t, router)
}

// Pattern 1: Standard HTTP Handler Tests.
func TestRouter_StandardHandler(t *testing.T) {
	router := NewRouter()

	err := router.GET("/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("standard handler"))
	})
	require.NoError(t, err)

	// Test request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "standard handler", rec.Body.String())
}

// Pattern 2: Context Handler Tests.
func TestRouter_ContextHandler(t *testing.T) {
	router := NewRouter()

	err := router.GET("/test", func(ctx Context) error {
		return ctx.String(http.StatusOK, "context handler")
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "context handler", rec.Body.String())
}

func TestRouter_ContextHandler_Error(t *testing.T) {
	router := NewRouter()

	err := router.GET("/test", func(ctx Context) error {
		return NotFound("not found")
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "not found")
}

// Pattern 3: Opinionated Handler Tests.
func TestRouter_OpinionatedHandler(t *testing.T) {
	router := NewRouter()

	err := router.POST("/users", func(ctx Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:    "123",
			Name:  req.Name,
			Email: req.Email,
		}, nil
	})
	require.NoError(t, err)

	// Create request
	reqBody := CreateUserRequest{Name: "John", Email: "john@example.com"}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp CreateUserResponse

	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "123", resp.ID)
	assert.Equal(t, "John", resp.Name)
	assert.Equal(t, "john@example.com", resp.Email)
}

func TestRouter_OpinionatedHandler_BadRequest(t *testing.T) {
	router := NewRouter()

	err := router.POST("/users", func(ctx Context, req *CreateUserRequest) (*CreateUserResponse, error) {
		return nil, nil
	})
	require.NoError(t, err)

	// Send invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Pattern 4: Service Handler Tests.
func TestRouter_ServiceHandler(t *testing.T) {
	container := di.NewContainer()

	// Register service with full type name
	err := di.RegisterSingleton(container, "github.com/xraph/forge/internal/router.TestUserService", func(c di.Container) (*TestUserService, error) {
		return &TestUserService{users: []string{"user1", "user2"}}, nil
	})
	require.NoError(t, err)

	router := NewRouter(WithContainer(container))

	err = router.GET("/users", func(ctx Context, svc *TestUserService) error {
		users := svc.GetAll()

		return ctx.JSON(http.StatusOK, users)
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var users []string

	err = json.Unmarshal(rec.Body.Bytes(), &users)
	require.NoError(t, err)
	assert.Equal(t, []string{"user1", "user2"}, users)
}

// Pattern 5: Combined Handler Tests.
func TestRouter_CombinedHandler(t *testing.T) {
	container := di.NewContainer()

	// Register with full type name as DI expects
	err := di.RegisterSingleton(container, "github.com/xraph/forge/internal/router.TestUserService", func(c di.Container) (*TestUserService, error) {
		return &TestUserService{users: []string{}}, nil
	})
	require.NoError(t, err)

	router := NewRouter(WithContainer(container))

	err = router.POST("/users", func(
		ctx Context,
		svc *TestUserService,
		req *CreateUserRequest,
	) (*CreateUserResponse, error) {
		return &CreateUserResponse{
			ID:    "123",
			Name:  req.Name,
			Email: req.Email,
		}, nil
	})
	require.NoError(t, err)

	reqBody := CreateUserRequest{Name: "Jane", Email: "jane@example.com"}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp CreateUserResponse

	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Jane", resp.Name)
}

// HTTP Methods Tests.
func TestRouter_AllHTTPMethods(t *testing.T) {
	router := NewRouter()

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

	for _, method := range methods {
		var err error

		handler := func(ctx Context) error {
			return ctx.String(http.StatusOK, method)
		}

		switch method {
		case "GET":
			err = router.GET("/test", handler)
		case "POST":
			err = router.POST("/test", handler)
		case "PUT":
			err = router.PUT("/test", handler)
		case "DELETE":
			err = router.DELETE("/test", handler)
		case "PATCH":
			err = router.PATCH("/test", handler)
		case "OPTIONS":
			err = router.OPTIONS("/test", handler)
		case "HEAD":
			err = router.HEAD("/test", handler)
		}

		require.NoError(t, err, "Failed to register %s", method)

		req := httptest.NewRequest(method, "/test", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "Method %s failed", method)
	}
}

// Route Options Tests.
func TestRouter_WithName(t *testing.T) {
	router := NewRouter()

	err := router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	}, WithName("test-route"))
	require.NoError(t, err)

	info, found := router.RouteByName("test-route")
	assert.True(t, found)
	assert.Equal(t, "test-route", info.Name)
}

func TestRouter_WithTags(t *testing.T) {
	router := NewRouter()

	err := router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	}, WithTags("api", "v1"))
	require.NoError(t, err)

	routes := router.RoutesByTag("api")
	assert.Len(t, routes, 1)
	assert.Contains(t, routes[0].Tags, "api")
	assert.Contains(t, routes[0].Tags, "v1")
}

func TestRouter_WithMetadata(t *testing.T) {
	router := NewRouter()

	err := router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	}, WithMetadata("key", "value"))
	require.NoError(t, err)

	routes := router.RoutesByMetadata("key", "value")
	assert.Len(t, routes, 1)
	assert.Equal(t, "value", routes[0].Metadata["key"])
}

func TestRouter_WithMiddleware(t *testing.T) {
	router := NewRouter()

	called := false
	middleware := func(next Handler) Handler {
		return func(ctx Context) error {
			called = true

			return next(ctx)
		}
	}

	err := router.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	}, WithMiddleware(middleware))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.True(t, called, "Middleware was not called")
}

// Route Groups Tests.
func TestRouter_Group(t *testing.T) {
	router := NewRouter()

	api := router.Group("/api")
	err := api.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouter_NestedGroups(t *testing.T) {
	router := NewRouter()

	api := router.Group("/api")
	v1 := api.Group("/v1")

	err := v1.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouter_GroupWithMiddleware(t *testing.T) {
	router := NewRouter()

	called := false
	middleware := func(next Handler) Handler {
		return func(ctx Context) error {
			called = true

			return next(ctx)
		}
	}

	api := router.Group("/api", WithGroupMiddleware(middleware))
	err := api.GET("/test", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.True(t, called, "Group middleware not called")
}

// TestRouter_GroupMiddlewareIsolation verifies that middleware added to a group
// does not affect routes outside that group (bug fix for group middleware leaking globally).
func TestRouter_GroupMiddlewareIsolation(t *testing.T) {
	router := NewRouter()

	// Track which middleware was called
	groupMiddlewareCalled := false
	rootMiddlewareCalled := false

	// Add root middleware
	rootMiddleware := func(next Handler) Handler {
		return func(ctx Context) error {
			rootMiddlewareCalled = true
			return next(ctx)
		}
	}
	router.Use(rootMiddleware)

	// Create a protected group with its own middleware
	protectedRoutes := router.Group("")
	protectedMiddleware := func(next Handler) Handler {
		return func(ctx Context) error {
			groupMiddlewareCalled = true
			return next(ctx)
		}
	}
	protectedRoutes.Use(protectedMiddleware)

	// Register route in the protected group
	err := protectedRoutes.GET("/protected", func(ctx Context) error {
		return ctx.String(200, "protected")
	})
	require.NoError(t, err)

	// Register route directly on root (not in the group)
	err = router.GET("/public", func(ctx Context) error {
		return ctx.String(200, "public")
	})
	require.NoError(t, err)

	// Test 1: Protected route should have BOTH root and group middleware
	t.Run("protected route has both middlewares", func(t *testing.T) {
		groupMiddlewareCalled = false
		rootMiddlewareCalled = false

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, rootMiddlewareCalled, "Root middleware should be called for protected route")
		assert.True(t, groupMiddlewareCalled, "Group middleware should be called for protected route")
	})

	// Test 2: Public route should have ONLY root middleware (NOT group middleware)
	t.Run("public route has only root middleware", func(t *testing.T) {
		groupMiddlewareCalled = false
		rootMiddlewareCalled = false

		req := httptest.NewRequest(http.MethodGet, "/public", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, rootMiddlewareCalled, "Root middleware should be called for public route")
		assert.False(t, groupMiddlewareCalled, "Group middleware should NOT be called for public route (bug: middleware leaking)")
	})
}

// TestRouter_UseGlobal verifies that UseGlobal applies middleware to ALL routes.
func TestRouter_UseGlobal(t *testing.T) {
	router := NewRouter()

	globalCalled := false
	globalMiddleware := func(next Handler) Handler {
		return func(ctx Context) error {
			globalCalled = true
			return next(ctx)
		}
	}

	// Create a group
	group := router.Group("/api")

	// Apply global middleware from the group
	group.UseGlobal(globalMiddleware)

	// Register route on root (not in group)
	err := router.GET("/public", func(ctx Context) error {
		return ctx.String(200, "public")
	})
	require.NoError(t, err)

	// Register route in group
	err = group.GET("/protected", func(ctx Context) error {
		return ctx.String(200, "protected")
	})
	require.NoError(t, err)

	// Test that global middleware applies to root route
	t.Run("global middleware applies to root route", func(t *testing.T) {
		globalCalled = false
		req := httptest.NewRequest(http.MethodGet, "/public", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.True(t, globalCalled, "Global middleware should apply to root route")
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	// Test that global middleware applies to group route
	t.Run("global middleware applies to group route", func(t *testing.T) {
		globalCalled = false
		req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.True(t, globalCalled, "Global middleware should apply to group route")
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

// TestRouter_UseVsUseGlobal verifies the difference between Use and UseGlobal.
func TestRouter_UseVsUseGlobal(t *testing.T) {
	router := NewRouter()

	scopedCalled := false
	globalCalled := false

	scopedMiddleware := func(next Handler) Handler {
		return func(ctx Context) error {
			scopedCalled = true
			return next(ctx)
		}
	}

	globalMiddleware := func(next Handler) Handler {
		return func(ctx Context) error {
			globalCalled = true
			return next(ctx)
		}
	}

	// Apply scoped middleware to root router
	router.Use(scopedMiddleware)

	// Apply global middleware
	router.UseGlobal(globalMiddleware)

	// Create a group
	group := router.Group("/api")

	// Register route on root router
	err := router.GET("/root-route", func(ctx Context) error {
		return ctx.String(200, "root")
	})
	require.NoError(t, err)

	// Register route in group
	err = group.GET("/group-route", func(ctx Context) error {
		return ctx.String(200, "group")
	})
	require.NoError(t, err)

	// Test root route has both scoped and global middleware
	t.Run("root route has scoped and global middleware", func(t *testing.T) {
		scopedCalled = false
		globalCalled = false

		req := httptest.NewRequest(http.MethodGet, "/root-route", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.True(t, scopedCalled, "Scoped middleware should apply to root route")
		assert.True(t, globalCalled, "Global middleware should apply to root route")
	})

	// Test group route has both middleware (inherits scoped from parent + global)
	t.Run("group route inherits parent scoped middleware plus global", func(t *testing.T) {
		scopedCalled = false
		globalCalled = false

		req := httptest.NewRequest(http.MethodGet, "/api/group-route", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.True(t, scopedCalled, "Group inherits parent's scoped middleware")
		assert.True(t, globalCalled, "Global middleware should apply to group route")
	})
}

// Middleware Tests.
func TestRouter_Use(t *testing.T) {
	router := NewRouter()

	order := []string{}

	mw1 := func(next Handler) Handler {
		return func(ctx Context) error {
			order = append(order, "mw1-before")
			err := next(ctx)

			order = append(order, "mw1-after")

			return err
		}
	}

	mw2 := func(next Handler) Handler {
		return func(ctx Context) error {
			order = append(order, "mw2-before")
			err := next(ctx)

			order = append(order, "mw2-after")

			return err
		}
	}

	router.Use(mw1, mw2)

	err := router.GET("/test", func(ctx Context) error {
		order = append(order, "handler")

		return ctx.String(200, "ok")
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	assert.Equal(t, expected, order)
}

// Route Inspection Tests.
func TestRouter_Routes(t *testing.T) {
	router := NewRouter()

	err := router.GET("/test1", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	require.NoError(t, err)

	err = router.POST("/test2", func(ctx Context) error {
		return ctx.String(200, "ok")
	})
	require.NoError(t, err)

	routes := router.Routes()
	assert.Len(t, routes, 2)

	methods := []string{routes[0].Method, routes[1].Method}
	assert.Contains(t, methods, "GET")
	assert.Contains(t, methods, "POST")
}

func TestRouter_RouteByName_NotFound(t *testing.T) {
	router := NewRouter()

	_, found := router.RouteByName("nonexistent")
	assert.False(t, found)
}

func TestRouter_RoutesByTag_Empty(t *testing.T) {
	router := NewRouter()

	routes := router.RoutesByTag("nonexistent")
	assert.Empty(t, routes)
}

// Lifecycle Tests.
func TestRouter_StartStop(t *testing.T) {
	router := NewRouter()

	ctx := context.Background()

	err := router.Start(ctx)
	assert.NoError(t, err)

	err = router.Stop(ctx)
	assert.NoError(t, err)
}

// Handler Tests.
func TestRouter_Handler(t *testing.T) {
	router := NewRouter()

	handler := router.Handler()
	assert.NotNil(t, handler)
	assert.Implements(t, (*http.Handler)(nil), handler)
}

// // simpleAdapter is a basic in-memory adapter for testing
// type simpleAdapter struct {
// 	routes map[string]map[string]http.Handler // method -> path -> handler
// }

// func (a *simpleAdapter) Handle(method, path string, handler http.Handler) {
// 	if a.routes[method] == nil {
// 		a.routes[method] = make(map[string]http.Handler)
// 	}
// 	a.routes[method][path] = handler
// }

// func (a *simpleAdapter) Mount(path string, handler http.Handler) {
// 	// Simple implementation: mount on all methods
// 	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"} {
// 		a.Handle(method, path, handler)
// 	}
// }

// func (a *simpleAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
// 	if handlers, ok := a.routes[r.Method]; ok {
// 		if handler, ok := handlers[r.URL.Path]; ok {
// 			handler.ServeHTTP(w, r)
// 			return
// 		}
// 	}
// 	http.NotFound(w, r)
// }

// func (a *simpleAdapter) Close() error {
// 	a.routes = nil
// 	return nil
// }
