package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/shared"
)

func TestRouteInfo_KindIsSetByTheStreamingConstructors(t *testing.T) {
	r := NewRouter()

	require.NoError(t, r.GET("/plain", func(ctx Context) error {
		return ctx.String(http.StatusOK, "ok")
	}))

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		return nil
	}))

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, c Connection) error {
		return nil
	}))

	byPath := map[string]RouteInfo{}
	for _, info := range r.Routes() {
		byPath[info.Path] = info
	}

	require.Contains(t, byPath, "/plain")
	require.Contains(t, byPath, "/events")
	require.Contains(t, byPath, "/ws")

	assert.Equal(t, KindHTTP, byPath["/plain"].Kind)
	assert.Equal(t, KindSSE, byPath["/events"].Kind)
	assert.Equal(t, KindWebSocket, byPath["/ws"].Kind)

	assert.False(t, byPath["/plain"].Kind.LongLived())
	assert.True(t, byPath["/events"].Kind.LongLived())
	assert.True(t, byPath["/ws"].Kind.LongLived())
}

// Every RouteInfo accessor projects through newRouteInfo, so Kind must survive
// all of them. This test exists because five open-coded projections once
// drifted; see the comment on newRouteInfo.
func TestRouteInfo_KindSurvivesEveryAccessor(t *testing.T) {
	r := NewRouter()

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		return nil
	}, WithName("events"), WithTags("stream")))

	byName, ok := r.RouteByName("events")
	require.True(t, ok)
	assert.Equal(t, KindSSE, byName.Kind, "RouteByName must carry Kind")

	byTag := r.RoutesByTag("stream")
	require.Len(t, byTag, 1)
	assert.Equal(t, KindSSE, byTag[0].Kind, "RoutesByTag must carry Kind")
}

// WebTransport sessions were given a handler deadline because the timeout
// check compared "route.type" against "sse" and "websocket" only, and
// router_webtransport.go writes "webtransport". Kind.LongLived() covers all
// three by construction.
func TestRouteKind_LongLivedCoversWebTransport(t *testing.T) {
	assert.True(t, KindWebTransport.LongLived(),
		"a WebTransport session outlives the handler and must not get a timeout")
	assert.True(t, KindSSE.LongLived())
	assert.True(t, KindWebSocket.LongLived())
	assert.False(t, KindHTTP.LongLived())
}

// The metadata string is gone. This test fails loudly if someone reintroduces
// it, which would resurrect the class of bug above.
func TestRouteKind_NoRouteTypeMetadataRemains(t *testing.T) {
	r := NewRouter()

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		return nil
	}))

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, c Connection) error {
		return nil
	}))

	for _, info := range r.Routes() {
		_, found := info.Metadata["route.type"]
		assert.Falsef(t, found, "route %s still carries a route.type metadata string", info.Path)
	}
}

func TestRouterStampsRouteKind(t *testing.T) {
	r := newRouter()

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error { return nil }))
	require.NoError(t, r.GET("/plain", func(ctx Context) error { return ctx.String(http.StatusOK, "ok") }))

	for path, want := range map[string]shared.RouteKind{"/events": shared.KindSSE, "/plain": shared.KindHTTP} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		ctx, m := shared.NewRouteMatch(req.Context())

		r.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))

		if m.Kind != want {
			t.Errorf("%s: Kind = %v, want %v", path, m.Kind, want)
		}
	}
}

// The router reports the match before the route's own middleware runs, so a
// callback registered by outer middleware sees it first.
func TestRouterRunsOnMatchedBeforeRouteMiddleware(t *testing.T) {
	r := newRouter()

	var order []string

	require.NoError(t, r.GET("/users/:id", func(ctx Context) error {
		order = append(order, "handler")

		return ctx.String(http.StatusOK, "ok")
	}, WithMiddleware(func(next Handler) Handler {
		return func(ctx Context) error {
			order = append(order, "route middleware")

			return next(ctx)
		}
	})))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/users/7", nil)
	ctx, m := shared.NewRouteMatch(req.Context())
	m.OnMatched(func(p string) { order = append(order, "matched "+p) })

	r.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))

	assert.Equal(t, []string{"matched /users/:id", "route middleware", "handler"}, order)
}
