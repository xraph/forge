package streaming

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/xraph/forge"
)

// newLeakTestServer serves the extension's real WebSocket and SSE handlers
// through a real router, so the tests below exercise the production
// connect/disconnect path rather than a stand-in for it.
func newLeakTestServer(t *testing.T) (*Extension, *httptest.Server) {
	t.Helper()

	app := forge.NewApp(forge.AppConfig{Name: "leak"})
	ext := NewExtension(WithLocalBackend())

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	if err := ext.RegisterRoutes(app.Router(), "/ws", "/sse"); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(app.Router().(http.Handler))
	t.Cleanup(srv.Close)

	return ext, srv
}

// settleGoroutines waits for every connection to be unregistered and for the
// goroutine count to fall back to base, then returns the count it settled at.
func settleGoroutines(t *testing.T, ext *Extension, base int) int {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ext.Manager().ConnectionCount() > 0 {
		time.Sleep(10 * time.Millisecond)
	}

	runtime.GC()

	return waitForGoroutineCount(base)
}

func sendQueueWriters() int {
	buf := make([]byte, 4<<20)

	return strings.Count(string(buf[:runtime.Stack(buf, true)]), "(*sendQueue).run")
}

// Every WebSocket connection starts a send queue writer goroutine. The handler
// unregistered the connection on disconnect but never closed it, so each
// disconnect left that goroutine, and its stack, behind for good.
func TestHandleWebSocket_DisconnectReleasesWriterGoroutine(t *testing.T) {
	ext, srv := newLeakTestServer(t)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	dial := func() {
		c, _, _, err := ws.Dial(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(time.Millisecond)

		_ = c.Close()
	}

	// Warm up so lazily started background goroutines are part of the base.
	dial()
	settleGoroutines(t, ext, 0)

	base := runtime.NumGoroutine()
	baseWriters := sendQueueWriters()

	const cycles = 300
	for range cycles {
		dial()
	}

	after := settleGoroutines(t, ext, base)

	// Relative to the start: other tests in the package may leave their own
	// connections open.
	if writers := sendQueueWriters() - baseWriters; writers > 0 || after-base > cycles/10 {
		t.Fatalf("after %d connect/disconnect cycles: %d goroutines over base, %d send queue writers still running",
			cycles, after-base, writers)
	}
}

// The SSE handler had the same leak as the WebSocket one.
func TestHandleSSE_DisconnectReleasesWriterGoroutine(t *testing.T) {
	ext, srv := newLeakTestServer(t)

	connect := func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/sse", nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		// Wait for the "connected" event, which proves registration happened.
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: connected") {
				break
			}
		}
	}

	connect()
	settleGoroutines(t, ext, 0)

	base := runtime.NumGoroutine()
	baseWriters := sendQueueWriters()

	const cycles = 150
	for range cycles {
		connect()
	}

	http.DefaultClient.CloseIdleConnections()

	after := settleGoroutines(t, ext, base)

	// Relative to the start: other tests in the package may leave their own
	// connections open.
	if writers := sendQueueWriters() - baseWriters; writers > 0 || after-base > cycles/10 {
		t.Fatalf("after %d connect/disconnect cycles: %d goroutines over base, %d send queue writers still running",
			cycles, after-base, writers)
	}
}

// ctxConn is a recording transport whose context the test controls.
type ctxConn struct {
	recordingConn

	ctx context.Context //nolint:containedctx // stands in for a transport's connection context
}

func (c *ctxConn) Context() context.Context { return c.ctx }

// The writer must not depend on its owner remembering to call Close: once the
// transport's context ends, it stops by itself and the connection reports
// closed.
func TestEnhancedConn_WriterStopsWhenTransportContextEnds(t *testing.T) {
	base := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	tc := &ctxConn{recordingConn: recordingConn{id: "c1"}, ctx: ctx}
	conn := NewConnection(tc)

	if err := conn.WriteJSON(&Message{ID: "m1", Type: MessageTypeMessage}); err != nil {
		t.Fatal(err)
	}

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !conn.IsClosed() {
		time.Sleep(time.Millisecond)
	}

	if !conn.IsClosed() {
		t.Fatal("connection still open after its transport context ended")
	}

	if n := waitForGoroutineCount(base); n > base {
		t.Fatalf("writer goroutine still running: %d goroutines, base %d", n, base)
	}

	tc.mu.Lock()
	closed := tc.closed
	tc.mu.Unlock()

	if !closed {
		t.Fatal("underlying transport was not closed")
	}
}
