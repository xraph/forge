package router

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/shared"
)

type fakeHandle struct {
	sent, received atomic.Int64
	closed         atomic.Int64
	closeErr       atomic.Value
}

func (h *fakeHandle) Sent()     { h.sent.Add(1) }
func (h *fakeHandle) Received() { h.received.Add(1) }
func (h *fakeHandle) Closed(err error) {
	h.closed.Add(1)

	if err != nil {
		h.closeErr.Store(err)
	}
}

type fakeObserver struct {
	mu      sync.Mutex
	opened  []string
	handles []*fakeHandle
}

func (o *fakeObserver) Opened(kind shared.RouteKind, route string) shared.StreamHandle {
	o.mu.Lock()
	defer o.mu.Unlock()

	h := &fakeHandle{}

	o.opened = append(o.opened, kind.String()+" "+route)
	o.handles = append(o.handles, h)

	return h
}

func (o *fakeObserver) only(t *testing.T) *fakeHandle {
	t.Helper()

	require.Eventually(t, func() bool {
		o.mu.Lock()
		defer o.mu.Unlock()

		return len(o.handles) == 1 && o.handles[0].closed.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)

	return o.handles[0]
}

// withHolder installs a route holder the way forge's tracing and metrics
// middleware do. Without one the router has no route name to report and uses
// the no-op handle, so every test here serves through it.
func withHolder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := shared.NewRouteMatch(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// served wraps next and closes the returned channel once a request through it
// has finished, so a test can wait for the whole handler before asserting that
// something never happened.
func served(next http.Handler) (http.Handler, <-chan struct{}) {
	done := make(chan struct{})

	var once sync.Once

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer once.Do(func() { close(done) })

		next.ServeHTTP(w, r)
	}), done
}

func waitServed(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestObserverNoHolderUsesNoop(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))

	var sent atomic.Bool

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		sent.Store(true)

		return s.Send("x", []byte("1"))
	}))

	h, done := served(r) // deliberately no holder

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	waitServed(t, done)
	require.True(t, sent.Load(), "the stream handler must have run")

	obs.mu.Lock()
	defer obs.mu.Unlock()

	require.Empty(t, obs.opened, "without a route name the observer must not be called")
}

func TestObserverWebSocketCounts(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		for range 2 {
			if _, err := conn.Read(); err != nil {
				return err
			}
		}

		for range 3 {
			if err := conn.Write([]byte("x")); err != nil {
				return err
			}
		}

		return nil
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, wsutil.WriteClientText(conn, []byte("a")))
	require.NoError(t, wsutil.WriteClientText(conn, []byte("b")))

	for range 3 {
		_, err := wsutil.ReadServerText(conn)
		require.NoError(t, err)
	}

	h := obs.only(t)
	require.Equal(t, []string{"websocket /ws"}, obs.opened)
	require.EqualValues(t, 3, h.sent.Load())
	require.EqualValues(t, 2, h.received.Load())
	require.EqualValues(t, 1, h.closed.Load())
}

func TestObserverEventStreamSkipsComments(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		for range 4 {
			if err := s.Send("tick", []byte("1")); err != nil {
				return err
			}
		}

		return s.SendComment("keepalive")
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	h := obs.only(t)
	require.EqualValues(t, 4, h.sent.Load())
	require.EqualValues(t, 0, h.received.Load())
}

func TestObserverClosedOnHandlerError(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))
	boom := errors.New("boom")

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error { return boom }))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	resp.Body.Close()

	h := obs.only(t)
	require.ErrorIs(t, h.closeErr.Load().(error), boom)
}

func TestObserverClosedWhenHandlerPanics(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs), WithRecovery())

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		_ = s.Send("one", []byte("1"))

		panic("after establish")
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	h := obs.only(t)
	require.EqualValues(t, 1, h.closed.Load())
	require.NotNil(t, h.closeErr.Load(), "a panic must close the handle with an error")
}

func TestObserverConcurrentWebSocketWrites(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		var wg sync.WaitGroup

		for range 10 {
			wg.Go(func() {
				for range 10 {
					_ = conn.Write([]byte("x"))
				}
			})
		}

		wg.Wait()

		return nil
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	conn, br, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	// The handler writes as soon as the upgrade completes, so the dialer can
	// already hold the first frames in br. Reading conn alone would lose them.
	var rw io.ReadWriter = conn
	if br != nil {
		rw = struct {
			io.Reader
			io.Writer
		}{br, conn}
	}

	for range 100 {
		_, err := wsutil.ReadServerText(rw)
		require.NoError(t, err)
	}

	require.EqualValues(t, 100, obs.only(t).sent.Load())
}

func TestObserverNotOpenedOnRejectedOrigin(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error { return nil }))

	h, done := served(withHolder(r))

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	header := map[string][]string{"Origin": {"https://evil.example"}}

	conn, _, _, err := ws.Dialer{Header: ws.HandshakeHeaderHTTP(header)}.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	if conn != nil {
		conn.Close()
	}

	require.Error(t, err)
	waitServed(t, done)

	obs.mu.Lock()
	defer obs.mu.Unlock()

	require.Empty(t, obs.opened)
}

func TestObserverCountsReplayedEvents(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))
	log := NewMemoryEventLog(MemoryEventLogOptions{})

	require.NoError(t, r.EventStream("/events", func(_ Context, s Stream) error {
		return s.Send("created", []byte("c"))
	}, WithEventLog(log, func(Context) string { return "orders" })))

	for _, data := range []string{"a", "b"} {
		_, err := log.Append(t.Context(), "orders", "created", []byte(data))
		require.NoError(t, err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/events", nil)
	req.Header.Set("Last-Event-ID", formatEventID(log.epoch, 0))

	rec := httptest.NewRecorder()
	withHolder(r).ServeHTTP(rec, req)

	require.Contains(t, rec.Body.String(), "event: "+EventResumed)

	// Markers count: the metric reports what the client received, and the
	// client received forge.resumed.
	h := obs.only(t)
	require.EqualValues(t, 4, h.sent.Load(), "2 replayed + forge.resumed + 1 handler send")
}

// lateHandle counts any Sent or Received that arrives after Closed. Closed
// lingers, so a stream still open past it has time to show up.
type lateHandle struct {
	closed atomic.Bool
	sent   atomic.Int64
	late   atomic.Int64
	done   chan struct{}
}

func (h *lateHandle) Sent() {
	if h.closed.Load() {
		h.late.Add(1)
	}

	h.sent.Add(1)
}

func (h *lateHandle) Received() {
	if h.closed.Load() {
		h.late.Add(1)
	}
}

func (h *lateHandle) Closed(error) {
	h.closed.Store(true)
	time.Sleep(100 * time.Millisecond)
	close(h.done)
}

type lateObserver struct{ h *lateHandle }

func (o lateObserver) Opened(shared.RouteKind, string) shared.StreamHandle { return o.h }

// waitSent polls from a handler goroutine, where require cannot be used. A
// timeout only means the late check below has less to work with.
func waitSent(h *lateHandle, n int64) {
	deadline := time.Now().Add(2 * time.Second)

	for h.sent.Load() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func waitClosed(t *testing.T, h *lateHandle) {
	t.Helper()

	select {
	case <-h.done:
	case <-time.After(2 * time.Second):
		t.Fatal("handle never closed")
	}
}

func TestObserverNoSendAfterClosedEventStream(t *testing.T) {
	h := &lateHandle{done: make(chan struct{})}
	r := newRouter(WithStreamObserver(lateObserver{h}))
	stopped := make(chan struct{})

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		// A sender the handler does not wait for, which keeps sending until
		// the stream refuses.
		go func() {
			defer close(stopped)

			for s.Send("tick", []byte("1")) == nil {
			}
		}()

		waitSent(h, 3)

		return nil
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	waitClosed(t, h)
	<-stopped

	require.Zero(t, h.late.Load(), "Sent reported after Closed")
}

func TestObserverNoSendAfterClosedWebSocket(t *testing.T) {
	h := &lateHandle{done: make(chan struct{})}
	r := newRouter(WithStreamObserver(lateObserver{h}))
	stopped := make(chan struct{})

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		go func() {
			defer close(stopped)

			for conn.Write([]byte("x")) == nil {
			}
		}()

		waitSent(h, 3)

		return nil
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	go func() {
		for {
			if _, err := wsutil.ReadServerText(conn); err != nil {
				return
			}
		}
	}()

	waitClosed(t, h)
	<-stopped

	require.Zero(t, h.late.Load(), "Sent reported after Closed")
}

type nilObserver struct{ opened atomic.Int64 }

func (o *nilObserver) Opened(shared.RouteKind, string) shared.StreamHandle {
	o.opened.Add(1)

	return nil
}

func TestObserverNilHandleUsesNoop(t *testing.T) {
	obs := &nilObserver{}
	r := newRouter(WithStreamObserver(obs))

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		return s.Send("tick", []byte("1"))
	}))
	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		if _, err := conn.Read(); err != nil {
			return err
		}

		return conn.Write([]byte("pong"))
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()

	require.NoError(t, err)
	require.Contains(t, string(body), "event: tick")

	conn, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, wsutil.WriteClientText(conn, []byte("ping")))

	msg, err := wsutil.ReadServerText(conn)
	require.NoError(t, err)
	require.Equal(t, "pong", string(msg))

	require.EqualValues(t, 2, obs.opened.Load())
}

func TestObserverPanicErrorIsWrapped(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs), WithRecovery())
	boom := errors.New("boom")

	require.NoError(t, r.EventStream("/events", func(ctx Context, s Stream) error {
		panic(boom)
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	resp := openSSE(t, srv.URL+"/events")
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	h := obs.only(t)
	require.ErrorIs(t, h.closeErr.Load().(error), boom)
}

func TestObserverClientCloseFrameIsEOF(t *testing.T) {
	obs := &fakeObserver{}
	r := newRouter(WithStreamObserver(obs))

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		_, err := conn.Read() // returns once the client's close frame arrives

		return err
	}))

	srv := httptest.NewServer(withHolder(r))
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, wsutil.WriteClientMessage(conn, ws.OpClose, ws.NewCloseFrameBody(ws.StatusNormalClosure, "bye")))

	h := obs.only(t)
	require.Equal(t, io.EOF, h.closeErr.Load(), "a client close frame means the client went away")
}
