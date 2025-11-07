package router

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test controller
type testController struct{}

func (c *testController) Name() string {
	return "test-controller"
}

func (c *testController) Routes(r Router) error {
	return r.GET("/test", func(ctx Context) error {
		return ctx.String(200, "controller route")
	})
}

// Controller with prefix
type testControllerWithPrefix struct{}

func (c *testControllerWithPrefix) Name() string {
	return "prefix-controller"
}

func (c *testControllerWithPrefix) Prefix() string {
	return "/api"
}

func (c *testControllerWithPrefix) Routes(r Router) error {
	return r.GET("/test", func(ctx Context) error {
		return ctx.String(200, "prefixed route")
	})
}

// Controller with middleware
type testControllerWithMiddleware struct {
	middlewareCalled bool
}

func (c *testControllerWithMiddleware) Name() string {
	return "middleware-controller"
}

func (c *testControllerWithMiddleware) Middleware() []Middleware {
	return []Middleware{
		func(next Handler) Handler {
			return func(ctx Context) error {
				c.middlewareCalled = true
				return next(ctx)
			}
		},
	}
}

func (c *testControllerWithMiddleware) Routes(r Router) error {
	return r.GET("/test", func(ctx Context) error {
		return ctx.String(200, "middleware route")
	})
}

func TestRouter_RegisterController(t *testing.T) {
	router := NewRouter()
	controller := &testController{}

	err := router.RegisterController(controller)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "controller route", rec.Body.String())
}

func TestRouter_RegisterController_WithPrefix(t *testing.T) {
	router := NewRouter()
	controller := &testControllerWithPrefix{}

	err := router.RegisterController(controller)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "prefixed route", rec.Body.String())
}

func TestRouter_RegisterController_WithMiddleware(t *testing.T) {
	router := NewRouter()
	controller := &testControllerWithMiddleware{}

	err := router.RegisterController(controller)
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	assert.True(t, controller.middlewareCalled)
}
