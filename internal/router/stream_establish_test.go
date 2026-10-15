package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/shared"
)

// establishProbe records what Establish reported and when it fired.
type establishProbe struct {
	mu        sync.Mutex
	status    int
	at        time.Time
	fired     bool
	handlerAt time.Time
}

func (p *establishProbe) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, m := shared.NewRouteMatch(r.Context())
		m.OnEstablished(func(status int) {
			p.mu.Lock()
			p.status, p.at, p.fired = status, time.Now(), true
			p.mu.Unlock()
		})

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestEstablishWebSocket(t *testing.T) {
	r := newRouter()
	probe := &establishProbe{}
	done := make(chan struct{})

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		time.Sleep(150 * time.Millisecond)
		close(done)

		return nil
	}))

	srv := httptest.NewServer(probe.wrap(r))
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	<-done

	probe.mu.Lock()
	defer probe.mu.Unlock()

	require.True(t, probe.fired)
	require.Equal(t, http.StatusSwitchingProtocols, probe.status)
}

func TestEstablishEventStreamBeforeHandlerFinishes(t *testing.T) {
	r := newRouter()
	probe := &establishProbe{}

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		time.Sleep(150 * time.Millisecond)

		probe.mu.Lock()
		probe.handlerAt = time.Now()
		probe.mu.Unlock()

		return nil
	}))

	srv := httptest.NewServer(probe.wrap(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	probe.mu.Lock()
	defer probe.mu.Unlock()

	require.True(t, probe.fired)
	require.Equal(t, http.StatusOK, probe.status)
	require.True(t, probe.at.Before(probe.handlerAt), "Establish must fire before the handler finishes")
}

func TestEstablishSSE(t *testing.T) {
	r := newRouter()
	probe := &establishProbe{}

	require.NoError(t, r.SSE("/sse", func(ctx Context) error { return nil }))

	srv := httptest.NewServer(probe.wrap(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/sse")
	resp.Body.Close()

	probe.mu.Lock()
	defer probe.mu.Unlock()

	require.True(t, probe.fired)
	require.Equal(t, http.StatusOK, probe.status)
}

func TestEstablishNotFiredOnRejectedOrigin(t *testing.T) {
	r := newRouter(WithWebSocketOrigins("https://good.example"))
	probe := &establishProbe{}

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error { return nil }))

	srv := httptest.NewServer(probe.wrap(r))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/ws", nil)
	require.NoError(t, err)

	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "https://evil.example")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	probe.mu.Lock()
	defer probe.mu.Unlock()

	require.False(t, probe.fired)
}
