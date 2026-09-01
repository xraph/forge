package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forge_http "github.com/xraph/go-utils/http"
)

func TestBunRouterAdapter_PublishesTheTypedCarrier(t *testing.T) {
	adapter := NewBunRouterAdapter()

	var (
		typedID  string
		typedOK  bool
		legacyID string
	)

	adapter.Handle(http.MethodGet, "/users/:id", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rp, ok := r.Context().Value(forge_http.RouteParamsKey).(*forge_http.RouteParams); ok {
			typedID, typedOK = rp.Get("id")
		}

		if m, ok := r.Context().Value("forge:params").(map[string]string); ok { //nolint:staticcheck // legacy contract
			legacyID = m["id"]
		}

		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	adapter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, typedOK, "the typed carrier must be published")
	assert.Equal(t, "42", typedID)
	assert.Equal(t, "42", legacyID, "the legacy map must still be published during the transition")
}

// End to end through the router, which is what a handler actually sees.
func TestRouter_ParamReachesTheHandlerThroughTheCarrier(t *testing.T) {
	r := NewRouter()

	require.NoError(t, r.GET("/users/:id", func(ctx Context) error {
		return ctx.String(http.StatusOK, ctx.Param("id"))
	}))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "42", rec.Body.String())
}
