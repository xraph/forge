package forge

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

func TestRouterStampsRoutePattern(t *testing.T) {
	r := NewRouter()

	if err := r.GET("/users/:id", func(c Context) error {
		return c.String(http.StatusOK, "ok")
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	ctx, match := shared.EnsureRouteMatch(req.Context())
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	if match.Pattern != "/users/:id" {
		t.Fatalf("Pattern = %q, want /users/:id", match.Pattern)
	}
}

func TestRouterLeavesPatternEmptyOn404(t *testing.T) {
	r := NewRouter()

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	ctx, match := shared.EnsureRouteMatch(req.Context())

	r.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))

	if match.Pattern != "" {
		t.Fatalf("Pattern = %q on an unmatched request, want empty", match.Pattern)
	}
}
