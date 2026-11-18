package router

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readAllBody is what a buffering global middleware does: read everything,
// then hand the next handler a fresh reader over what it got.
func readAllBody(got *int, gotErr *error) Middleware {
	return func(next Handler) Handler {
		return func(ctx Context) error {
			b, err := io.ReadAll(ctx.Request().Body)
			*got, *gotErr = len(b), err

			ctx.Request().Body = io.NopCloser(bytes.NewReader(b))

			return next(ctx)
		}
	}
}

func post(t *testing.T, h http.Handler, path string, size int) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, bytes.NewReader(make([]byte, size)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// The body cap was installed only in the per-route wrapper, so UseGlobal
// middleware read an unbounded body, on matched and unmatched paths alike.
func TestBodyLimit_AppliesToGlobalMiddleware(t *testing.T) {
	const limit = 1 << 10

	var (
		read int
		err  error
	)

	r := NewRouter(WithMaxRequestBodySize(limit))
	r.UseGlobal(readAllBody(&read, &err))
	require.NoError(t, r.POST("/x", func(ctx Context) error { return ctx.String(http.StatusOK, "ok") }))

	for _, path := range []string{"/x", "/does-not-exist"} {
		t.Run(path, func(t *testing.T) {
			read, err = 0, nil

			post(t, r, path, 64*limit)

			assert.LessOrEqual(t, read, limit, "global middleware read past the cap")

			var tooLarge *http.MaxBytesError
			require.ErrorAs(t, err, &tooLarge)
			assert.Equal(t, int64(limit), tooLarge.Limit)
		})
	}
}

func TestBodyLimit_AppliesToMountedHandler(t *testing.T) {
	const limit = 1 << 10

	var (
		read int
		err  error
	)

	r := NewRouter(WithMaxRequestBodySize(limit))
	require.NoError(t, r.Handle("/mount/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, e := io.ReadAll(req.Body)
		read, err = len(b), e
	})))

	post(t, r, "/mount/upload", 8*limit)

	assert.LessOrEqual(t, read, limit)

	var tooLarge *http.MaxBytesError
	assert.ErrorAs(t, err, &tooLarge)
}

func TestBodyLimit_MountedHandlerCanRaiseLimit(t *testing.T) {
	const limit = 1 << 10

	var (
		read  int
		err   error
		moved bool
	)

	r := NewRouter(WithMaxRequestBodySize(limit))
	require.NoError(t, r.Handle("/upload/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		moved = SetRequestBodyLimit(req, 16*limit)
		b, e := io.ReadAll(req.Body)
		read, err = len(b), e
	})))

	post(t, r, "/upload/big", 8*limit)

	assert.True(t, moved)
	require.NoError(t, err)
	assert.Equal(t, 8*limit, read)
}

// A route can still raise or lower the router's cap with WithMaxBodySize.
func TestBodyLimit_RouteOverridesRouterCap(t *testing.T) {
	const limit = 1 << 10

	type result struct {
		read int
		err  error
	}

	results := map[string]*result{}
	handler := func(name string) func(Context) error {
		results[name] = &result{}

		return func(ctx Context) error {
			b, err := io.ReadAll(ctx.Request().Body)
			results[name].read, results[name].err = len(b), err

			return ctx.String(http.StatusOK, "ok")
		}
	}

	r := NewRouter(WithMaxRequestBodySize(limit))
	require.NoError(t, r.POST("/default", handler("default")))
	require.NoError(t, r.POST("/raised", handler("raised"), WithMaxBodySize(16*limit)))
	require.NoError(t, r.POST("/lowered", handler("lowered"), WithMaxBodySize(limit/4)))
	require.NoError(t, r.POST("/unlimited", handler("unlimited"), WithMaxBodySize(-1)))

	post(t, r, "/default", 2*limit)
	post(t, r, "/raised", 8*limit)
	post(t, r, "/lowered", limit/2)
	post(t, r, "/unlimited", 32*limit)

	var tooLarge *http.MaxBytesError

	require.ErrorAs(t, results["default"].err, &tooLarge)
	assert.Equal(t, int64(limit), tooLarge.Limit)

	require.NoError(t, results["raised"].err)
	assert.Equal(t, 8*limit, results["raised"].read)

	require.ErrorAs(t, results["lowered"].err, &tooLarge)
	assert.Equal(t, int64(limit/4), tooLarge.Limit)

	require.NoError(t, results["unlimited"].err)
	assert.Equal(t, 32*limit, results["unlimited"].read)
}

// Bytes a global middleware has already read count against the route's cap.
func TestBodyLimit_RouteCapCountsBytesAlreadyRead(t *testing.T) {
	const limit = 1 << 10

	var (
		read int
		err  error
	)

	r := NewRouter(WithMaxRequestBodySize(limit))
	r.UseGlobal(func(next Handler) Handler {
		return func(ctx Context) error {
			// Peek at the first 100 bytes without replacing the body.
			_, _ = io.ReadFull(ctx.Request().Body, make([]byte, 100))

			return next(ctx)
		}
	})
	require.NoError(t, r.POST("/x", func(ctx Context) error {
		b, e := io.ReadAll(ctx.Request().Body)
		read, err = len(b), e

		return nil
	}, WithMaxBodySize(4*limit)))

	post(t, r, "/x", 4*limit)
	require.NoError(t, err)
	assert.Equal(t, 4*limit-100, read)

	post(t, r, "/x", 4*limit+1)

	var tooLarge *http.MaxBytesError
	require.ErrorAs(t, err, &tooLarge)
	assert.Equal(t, int64(4*limit), tooLarge.Limit)
	assert.LessOrEqual(t, read, 4*limit-100)
}

func TestBodyLimit_NegativeRouterLimitInstallsNothing(t *testing.T) {
	var body io.ReadCloser

	r := NewRouter(WithMaxRequestBodySize(-1))
	require.NoError(t, r.Handle("/m/*", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body = req.Body
	})))

	post(t, r, "/m/x", 10)

	_, wrapped := body.(*limitedBody)
	assert.False(t, wrapped)
	assert.False(t, SetRequestBodyLimit(httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil), 1))
}

func TestLimitedBody_StaysFailedAfterCap(t *testing.T) {
	b := newLimitedBody(httptest.NewRecorder(), io.NopCloser(bytes.NewReader(make([]byte, 100))), 10)

	_, err := io.ReadAll(b)

	var tooLarge *http.MaxBytesError
	require.ErrorAs(t, err, &tooLarge)

	b.setLimit(1000)

	_, err = b.Read(make([]byte, 10))
	assert.True(t, errors.As(err, &tooLarge), "raising the cap after it was hit must not resume the body")
}
