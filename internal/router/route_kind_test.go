package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
