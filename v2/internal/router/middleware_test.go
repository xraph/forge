package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMiddlewareFunc_ToMiddleware(t *testing.T) {
	called := false

	mw := MiddlewareFunc(func(w http.ResponseWriter, r *http.Request, next http.Handler) {
		called = true
		next.ServeHTTP(w, r)
	})

	handler := mw.ToMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, 200, rec.Code)
}

func TestChain(t *testing.T) {
	order := []string{}

	mw1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw1-after")
		})
	}

	mw2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw2-after")
		})
	}

	mw3 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw3-before")
			next.ServeHTTP(w, r)
			order = append(order, "mw3-after")
		})
	}

	handler := Chain(mw1, mw2, mw3)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

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
	handler := Chain()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
}

func TestChain_Single(t *testing.T) {
	called := false

	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}

	handler := Chain(mw)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, 200, rec.Code)
}
