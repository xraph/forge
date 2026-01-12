package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/xraph/forge/internal/di"
	forge_http "github.com/xraph/forge/internal/http"
)

func TestMiddlewareFunc_ToMiddleware(t *testing.T) {
	called := false
	container := di.NewContainer()

	mw := MiddlewareFunc(func(w http.ResponseWriter, r *http.Request, next http.Handler) {
		called = true

		next.ServeHTTP(w, r)
	})

	// Convert to forge Middleware
	forgeMW := mw.ToMiddleware(container)

	// Create a forge handler
	forgeHandler := func(ctx Context) error {
		ctx.Response().WriteHeader(200)

		return nil
	}

	// Apply middleware
	wrappedHandler := forgeMW(forgeHandler)

	// Execute with a test context
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	ctx := forge_http.NewContext(rec, req, container)
	defer ctx.(forge_http.ContextWithClean).Cleanup()

	err := wrappedHandler(ctx)
	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, 200, rec.Code)
}

func TestChain(t *testing.T) {
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

	mw3 := func(next Handler) Handler {
		return func(ctx Context) error {
			order = append(order, "mw3-before")
			err := next(ctx)

			order = append(order, "mw3-after")

			return err
		}
	}

	// Create a forge handler
	handler := func(ctx Context) error {
		order = append(order, "handler")

		ctx.Response().WriteHeader(200)

		return nil
	}

	// Chain middleware
	chained := Chain(mw1, mw2, mw3)(handler)

	// Execute with a test context
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	container := di.NewContainer()

	ctx := forge_http.NewContext(rec, req, container)
	defer ctx.(forge_http.ContextWithClean).Cleanup()

	err := chained(ctx)
	assert.NoError(t, err)

	expected := []string{
		"mw1-before",
		"mw2-before",
		"mw3-before",
		"handler",
		"mw3-after",
		"mw2-after",
		"mw1-after",
	}

	assert.Equal(t, expected, order)
	assert.Equal(t, 200, rec.Code)
}

func TestChain_Empty(t *testing.T) {
	handler := func(ctx Context) error {
		ctx.Response().WriteHeader(200)

		return nil
	}

	chained := Chain()(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	container := di.NewContainer()

	ctx := forge_http.NewContext(rec, req, container)
	defer ctx.(forge_http.ContextWithClean).Cleanup()

	err := chained(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 200, rec.Code)
}

func TestChain_Single(t *testing.T) {
	called := false

	mw := func(next Handler) Handler {
		return func(ctx Context) error {
			called = true

			return next(ctx)
		}
	}

	handler := func(ctx Context) error {
		ctx.Response().WriteHeader(200)

		return nil
	}

	chained := Chain(mw)(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	container := di.NewContainer()

	ctx := forge_http.NewContext(rec, req, container)
	defer ctx.(forge_http.ContextWithClean).Cleanup()

	err := chained(ctx)
	assert.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, 200, rec.Code)
}
