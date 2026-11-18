package streaming

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/xraph/forge"
)

// newPresenceServer serves the real handlers, with a stand-in auth middleware
// that takes the user id from ?uid=.
func newPresenceServer(t *testing.T) (*Extension, *httptest.Server) {
	t.Helper()

	app := forge.NewApp(forge.AppConfig{Name: "presence"})
	ext := NewExtension(WithLocalBackend())

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	app.Router().Use(func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if uid := ctx.Request().URL.Query().Get("uid"); uid != "" {
				ctx.Set("user_id", uid)
			}

			return next(ctx)
		}
	})

	if err := ext.RegisterRoutes(app.Router(), "/ws", "/sse"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(app.Router().(http.Handler))
	t.Cleanup(srv.Close)

	return ext, srv
}

func presenceOf(t *testing.T, ext *Extension, userID string) string {
	t.Helper()

	p, err := ext.Manager().GetPresence(context.Background(), userID)
	if err != nil || p == nil {
		return ""
	}

	return p.Status
}

// waitPresence polls until the user's presence is want, or fails.
func waitPresence(t *testing.T, ext *Extension, userID, want string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if presenceOf(t, ext, userID) == want {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("presence of %s = %q, want %q", userID, presenceOf(t, ext, userID), want)
}

// waitConns polls until the user has n registered connections.
func waitConns(t *testing.T, ext *Extension, userID string, n int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(ext.Manager().GetUserConnections(userID)) == n {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("%s has %d connections, want %d", userID, len(ext.Manager().GetUserConnections(userID)), n)
}

// assertStaysOnline checks presence does not flip to offline for a while.
func assertStaysOnline(t *testing.T, ext *Extension, userID string) {
	t.Helper()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := presenceOf(t, ext, userID); got != StatusOnline {
			t.Fatalf("presence of %s = %q while another connection is open, want online", userID, got)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// openSSE connects an SSE client and returns a function that disconnects it.
func openSSE(t *testing.T, srv *httptest.Server, uid string) func() {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/sse?uid="+uid, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "event: connected") {
			break
		}
	}

	closed := false

	stop := func() {
		if !closed {
			closed = true

			cancel()

			_ = resp.Body.Close()
		}
	}

	t.Cleanup(stop)

	return stop
}

func openWS(t *testing.T, srv *httptest.Server, uid string) func() {
	t.Helper()

	c, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws?uid="+uid)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = c.Close() })

	return func() { _ = c.Close() }
}

// The SSE handler marked the user offline on every disconnect, so closing one
// of two tabs showed the user offline, and it wrote with the request context,
// which is already cancelled at that point.
func TestHandleSSE_PresenceOfflineOnlyAfterLastConnection(t *testing.T) {
	ext, srv := newPresenceServer(t)

	closeFirst := openSSE(t, srv, "alice")
	closeSecond := openSSE(t, srv, "alice")

	waitConns(t, ext, "alice", 2)
	waitPresence(t, ext, "alice", StatusOnline)

	closeFirst()
	waitConns(t, ext, "alice", 1)
	assertStaysOnline(t, ext, "alice")

	closeSecond()
	waitConns(t, ext, "alice", 0)
	waitPresence(t, ext, "alice", StatusOffline)
}

// The WebSocket handler had the opposite bug. Its offline check ran before
// Unregister (deferred calls run last in, first out), so it always counted
// the closing connection itself and never marked anyone offline.
func TestHandleWebSocket_PresenceOfflineOnlyAfterLastConnection(t *testing.T) {
	ext, srv := newPresenceServer(t)

	closeFirst := openWS(t, srv, "bob")
	closeSecond := openWS(t, srv, "bob")

	waitConns(t, ext, "bob", 2)
	waitPresence(t, ext, "bob", StatusOnline)

	closeFirst()
	waitConns(t, ext, "bob", 1)
	assertStaysOnline(t, ext, "bob")

	closeSecond()
	waitConns(t, ext, "bob", 0)
	waitPresence(t, ext, "bob", StatusOffline)
}

// Mixed transports count together: closing the SSE stream must not mark a
// user offline who still has a WebSocket open.
func TestPresence_MixedTransportsCountTogether(t *testing.T) {
	ext, srv := newPresenceServer(t)

	closeWS := openWS(t, srv, "carol")
	closeSSE := openSSE(t, srv, "carol")

	waitConns(t, ext, "carol", 2)

	closeSSE()
	waitConns(t, ext, "carol", 1)
	assertStaysOnline(t, ext, "carol")

	closeWS()
	waitConns(t, ext, "carol", 0)
	waitPresence(t, ext, "carol", StatusOffline)
}
