package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// healthyService implements only what the health route needs.
type healthyService struct {
	internal.ConsensusService
}

func (healthyService) HealthCheck(context.Context) error { return nil }

func serve(t *testing.T, api *API, header, value string) int {
	t.Helper()

	router := forge.NewRouter()
	api.RegisterRoutes(router, "/consensus")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/consensus/health", nil)
	if header != "" {
		req.Header.Set(header, value)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec.Code
}

func TestAdminRoutesRequireAPIKeyWhenAuthEnabled(t *testing.T) {
	api, err := NewAPIFromConfig(healthyService{}, nil, nil, forge.NewNoopLogger(),
		internal.AdminAPIConfig{EnableAuth: true, APIKey: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		header, value string
		want          int
	}{
		{"", "", http.StatusUnauthorized},
		{"Authorization", "Bearer wrong", http.StatusUnauthorized},
		{"X-API-Key", "s3cre", http.StatusUnauthorized},
		{"Authorization", "Bearer s3cret", http.StatusOK},
		{"X-API-Key", "s3cret", http.StatusOK},
	}

	for _, c := range cases {
		if got := serve(t, api, c.header, c.value); got != c.want {
			t.Fatalf("%s=%q: status %d, want %d", c.header, c.value, got, c.want)
		}
	}
}

func TestAdminAuthWithoutKeyIsAnError(t *testing.T) {
	if _, err := NewAPIFromConfig(healthyService{}, nil, nil, forge.NewNoopLogger(),
		internal.AdminAPIConfig{EnableAuth: true}); err == nil {
		t.Fatal("enable_auth without an api_key produced an open admin API")
	}
}

func TestAdminRoutesOpenWithoutAuth(t *testing.T) {
	api := NewAPI(healthyService{}, nil, nil, forge.NewNoopLogger())

	if got := serve(t, api, "", ""); got != http.StatusOK {
		t.Fatalf("status %d without auth configured", got)
	}
}
