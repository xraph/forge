package router

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	logger "github.com/xraph/go-utils/log"
)

// statusSpy records the status an outer middleware saw leave the chain.
type statusSpy struct {
	http.ResponseWriter

	status int
}

func (s *statusSpy) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}

	s.ResponseWriter.WriteHeader(code)
}

func (s *statusSpy) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}

	return s.ResponseWriter.Write(b)
}

func outerSpy(order *[]string, seen *int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*order = append(*order, "outer")
			spy := &statusSpy{ResponseWriter: w}
			next.ServeHTTP(spy, r)
			*seen = spy.status
		})
	}
}

func TestUseOuterMiddleware_RunsOutsideForgeGlobalChain(t *testing.T) {
	r := NewRouter(WithLogger(logger.NewTestLogger()))

	var (
		order []string
		seen  int
	)

	// Registered before the outer middleware on purpose: the outer slot must
	// still wrap it.
	r.UseGlobal(func(next Handler) Handler {
		return func(ctx Context) error {
			order = append(order, "forge")

			return next(ctx)
		}
	})
	UseOuterMiddleware(r, outerSpy(&order, &seen))

	_ = r.GET("/ok", func(ctx Context) error { return ctx.String(http.StatusOK, "ok") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ok", nil))

	if want := []string{"outer", "forge"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}

	if seen != http.StatusOK || rec.Code != http.StatusOK {
		t.Fatalf("outer saw %d, client got %d, want 200 for both", seen, rec.Code)
	}
}

func TestUseOuterMiddleware_SeesNotFound(t *testing.T) {
	r := NewRouter(WithLogger(logger.NewTestLogger()))

	var (
		order []string
		seen  int
	)

	UseOuterMiddleware(r, outerSpy(&order, &seen))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil))

	if seen != http.StatusNotFound || rec.Code != http.StatusNotFound {
		t.Fatalf("outer saw %d, client got %d, want 404 for both", seen, rec.Code)
	}
}

// A forge global middleware that returns an error used to be swallowed when
// it sat behind a PureMiddleware wrapper, and the client got an empty 200.
func TestUseOuterMiddleware_ForgeGlobalErrorIsMapped(t *testing.T) {
	r := NewRouter(WithLogger(logger.NewTestLogger()))

	var (
		order []string
		seen  int
	)

	UseOuterMiddleware(r, outerSpy(&order, &seen))
	r.UseGlobal(func(next Handler) Handler {
		return func(ctx Context) error { return errors.New("rejected") }
	})

	_ = r.GET("/ok", func(ctx Context) error { return ctx.String(http.StatusOK, "ok") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ok", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("client got %d, want 500", rec.Code)
	}

	if seen != http.StatusInternalServerError {
		t.Fatalf("outer saw %d, want 500", seen)
	}
}

// Outer middleware called on a group still lands on the shared chain.
func TestUseOuterMiddleware_FromGroup(t *testing.T) {
	r := NewRouter(WithLogger(logger.NewTestLogger()))

	var (
		order []string
		seen  int
	)

	UseOuterMiddleware(r.Group("/api"), outerSpy(&order, &seen))

	_ = r.GET("/root", func(ctx Context) error { return ctx.String(http.StatusOK, "ok") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/root", nil))

	if len(order) != 1 || seen != http.StatusOK {
		t.Fatalf("outer ran %d times and saw %d, want once and 200", len(order), seen)
	}
}
