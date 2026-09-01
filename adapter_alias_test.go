package forge_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge"
)

// externalAdapter stands in for an adapter written outside this module. If it
// compiles, the public aliases are complete enough for a third party to
// implement the wide interface.
type externalAdapter struct{ configured bool }

func (a *externalAdapter) Handle(method, path string, h http.Handler)       {}
func (a *externalAdapter) Mount(path string, h http.Handler)                {}
func (a *externalAdapter) UseGlobal(m func(http.Handler) http.Handler)      {}
func (a *externalAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
func (a *externalAdapter) Close() error                                     { return nil }

func (a *externalAdapter) Capabilities() forge.Capabilities {
	return forge.Capabilities{MethodNotAllowed: true, TypedParams: true}
}

func (a *externalAdapter) Configure(cfg forge.MatcherConfig) error {
	a.configured = true

	return nil
}

func (a *externalAdapter) HandleRoute(spec forge.RouteSpec) error { return nil }

func TestPublicAliasesAllowAnExternalExtendedAdapter(t *testing.T) {
	var adapter forge.ExtendedAdapter = &externalAdapter{}

	require.NotNil(t, adapter)

	caps := adapter.Capabilities()
	assert.True(t, caps.MethodNotAllowed)
	assert.False(t, caps.Constraints)

	require.NoError(t, adapter.Configure(forge.MatcherConfig{
		NotFound: http.NotFoundHandler(),
	}))

	require.NoError(t, adapter.HandleRoute(forge.RouteSpec{
		Method: http.MethodGet,
		Kind:   forge.KindSSE,
	}))
}

func TestPublicRouteKindConstants(t *testing.T) {
	assert.Equal(t, "sse", forge.KindSSE.String())
	assert.True(t, forge.KindWebSocket.LongLived())
	assert.False(t, forge.KindHTTP.LongLived())
}

func TestNewForgeMuxAdapter(t *testing.T) {
	adapter := forge.NewForgeMuxAdapter()
	require.NotNil(t, adapter)

	caps := adapter.Capabilities()
	assert.True(t, caps.Constraints)
	assert.True(t, caps.MethodNotAllowed)
}

func TestRouterWithForgeMux(t *testing.T) {
	r := forge.NewRouter(forge.WithAdapter(forge.NewForgeMuxAdapter()))

	require.NoError(t, r.GET("/users/{id:int}", func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, ctx.Param("id"))
	}))
	require.NoError(t, r.GET("/users/me", func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "me")
	}))

	for path, want := range map[string]string{"/users/42": "42", "/users/me": "me"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		require.Equalf(t, http.StatusOK, rec.Code, "path %s", path)
		assert.Equal(t, want, rec.Body.String())
	}
}
