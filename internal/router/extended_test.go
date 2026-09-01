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

func TestRouter_RegistersThroughHandleRouteWhenWide(t *testing.T) {
	adapter := newFakeExtended(Capabilities{Constraints: true, AnyMethod: true})

	r := NewRouter(WithAdapter(adapter))

	require.NoError(t, r.GET("/users/{id:int}", func(ctx Context) error { return nil }))
	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error { return nil }))

	require.Empty(t, adapter.plainCalls, "a wide backend must not receive Handle")
	require.Len(t, adapter.specs, 2)

	users := adapter.specs[0]
	assert.Equal(t, http.MethodGet, users.Method)
	assert.Equal(t, "/users/{id:int}", users.Pattern.Raw)
	assert.Equal(t, KindHTTP, users.Kind)
	require.Len(t, users.Pattern.Params, 1)
	assert.Equal(t, "id", users.Pattern.Params[0])
	require.NotNil(t, users.Handler)

	events := adapter.specs[1]
	assert.Equal(t, "/events", events.Pattern.Raw)
	assert.Equal(t, KindSSE, events.Kind, "streaming Kind must reach the backend")
	assert.True(t, events.Kind.LongLived())
}

// A conflict reported by the backend has to reach the caller, which is the
// whole reason HandleRoute returns an error and Handle does not.
func TestRouter_SurfacesAHandleRouteError(t *testing.T) {
	adapter := newFakeExtended(Capabilities{ConflictDetection: true})
	adapter.handleErr = errors.New("route conflicts with /users/{name}")

	r := NewRouter(WithAdapter(adapter))

	err := r.GET("/users/{id}", func(ctx Context) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts with")
}

func TestRouter_StillUsesHandleForAPlainAdapter(t *testing.T) {
	// A fake that deliberately does NOT implement ExtendedAdapter.
	plain := &plainFake{}

	r := NewRouter(WithAdapter(plain))

	require.NoError(t, r.GET("/a", func(ctx Context) error { return nil }))
	assert.Equal(t, []string{"GET /a"}, plain.calls)
}

type plainFake struct{ calls []string }

func (p *plainFake) Handle(method, path string, h http.Handler) {
	p.calls = append(p.calls, method+" "+path)
}

func (p *plainFake) Mount(path string, h http.Handler)                {}
func (p *plainFake) UseGlobal(m func(http.Handler) http.Handler)      {}
func (p *plainFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {}
func (p *plainFake) Close() error                                     { return nil }
