package router

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExtended records what forge asks of a wide backend.
type fakeExtended struct {
	caps       Capabilities
	configured []MatcherConfig
	specs      []RouteSpec
	handleErr  error
	configErr  error
	plainCalls []string
}

func newFakeExtended(caps Capabilities) *fakeExtended {
	return &fakeExtended{caps: caps}
}

func (f *fakeExtended) Handle(method, path string, h http.Handler) {
	f.plainCalls = append(f.plainCalls, method+" "+path)
}

func (f *fakeExtended) Mount(path string, h http.Handler)                {}
func (f *fakeExtended) UseGlobal(m func(http.Handler) http.Handler)      {}
func (f *fakeExtended) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
func (f *fakeExtended) Close() error                                     { return nil }

func (f *fakeExtended) Capabilities() Capabilities { return f.caps }

func (f *fakeExtended) Configure(cfg MatcherConfig) error {
	if f.configErr != nil {
		return f.configErr
	}

	f.configured = append(f.configured, cfg)

	return nil
}

func (f *fakeExtended) HandleRoute(spec RouteSpec) error {
	if f.handleErr != nil {
		return f.handleErr
	}

	f.specs = append(f.specs, spec)

	return nil
}

func TestRouter_ConfiguresAWideAdapterExactlyOnce(t *testing.T) {
	adapter := newFakeExtended(Capabilities{MethodNotAllowed: true})

	r := NewRouter(WithAdapter(adapter))
	require.NotNil(t, r)

	require.Len(t, adapter.configured, 1, "Configure must be called once, at construction")

	require.NoError(t, r.GET("/a", func(ctx Context) error { return nil }))
	require.NoError(t, r.GET("/b", func(ctx Context) error { return nil }))

	assert.Len(t, adapter.configured, 1, "registering routes must not reconfigure the backend")
}

// A backend without the wide interface must be left entirely alone.
func TestRouter_LeavesAPlainAdapterAlone(t *testing.T) {
	r := NewRouter(WithAdapter(NewBunRouterAdapter()))

	require.NoError(t, r.GET("/a", func(ctx Context) error {
		return ctx.String(http.StatusOK, "ok")
	}))
}

func TestRouter_SurfacesAConfigureFailure(t *testing.T) {
	adapter := newFakeExtended(Capabilities{})
	adapter.configErr = errors.New("boom")

	// NewRouter returns a Router, not an error, so a Configure failure has to
	// surface somewhere observable. Registering a route is the first thing
	// that can report it.
	r := NewRouter(WithAdapter(adapter))

	err := r.GET("/a", func(ctx Context) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
