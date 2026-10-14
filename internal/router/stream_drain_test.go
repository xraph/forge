package router

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/vessel"
)

// drainWithin drains r and fails the test if that takes longer than limit.
func drainWithin(t *testing.T, r *router, limit time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()

	require.NoError(t, r.DrainStreams(ctx))
}

// openSSE starts a GET against an event stream and waits for its headers.
func openSSE(t *testing.T, url string) *http.Response {
	t.Helper()

	resp, err := http.Get(url) //nolint:noctx // the server ends this stream
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	return resp
}

// waitForComment reads the stream until the given comment arrives. The stream
// opens with its retry interval, so the comment is not the first line.
func waitForComment(t *testing.T, resp *http.Response, comment string) {
	t.Helper()

	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)

		if line == ": "+comment+"\n" {
			return
		}
	}
}

func TestDrainStreams_endsAnEventStreamThatWaitsOnItsContext(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	returned := make(chan struct{})

	require.NoError(t, r.EventStream("/events", func(ctx Context, stream Stream) error {
		defer close(returned)

		_ = stream.SendComment("ready")

		<-ctx.Context().Done()

		return nil
	}))

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp := openSSE(t, srv.URL+"/events")
	defer resp.Body.Close()

	waitForComment(t, resp, "ready")

	drainWithin(t, r, 2*time.Second)

	select {
	case <-returned:
	default:
		t.Fatal("DrainStreams returned before the stream's handler did")
	}

	// The body ends cleanly once the handler is gone.
	_, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
}

func TestDrainStreams_endsAnSSERouteToo(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	started := make(chan struct{})
	returned := make(chan struct{})

	require.NoError(t, r.SSE("/sse", func(ctx Context) error {
		defer close(returned)

		close(started)

		<-ctx.Context().Done()

		return nil
	}))

	srv := httptest.NewServer(r)
	defer srv.Close()

	go func() {
		resp, err := http.Get(srv.URL + "/sse") //nolint:noctx // the server ends this stream
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()

	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = r.DrainStreams(ctx)

	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("the SSE route's handler was still running after the drain")
	}
}

func TestDrainStreams_sendsASocketGoingAwayAndUnblocksItsRead(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	readErr := make(chan error, 1)

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		_, err := conn.Read()
		readErr <- err

		return nil
	}))

	srv := httptest.NewServer(r)
	defer srv.Close()

	conn, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	drainWithin(t, r, 2*time.Second)

	select {
	case err := <-readErr:
		require.Error(t, err)
	default:
		t.Fatal("the handler's Read was still blocked after the drain")
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	frame, err := ws.ReadFrame(conn)
	require.NoError(t, err)
	require.Equal(t, ws.OpClose, frame.Header.OpCode)

	if frame.Header.Masked {
		ws.Cipher(frame.Payload, frame.Header.Mask, 0)
	}

	require.GreaterOrEqual(t, len(frame.Payload), 2)
	assert.Equal(t, uint16(ws.StatusGoingAway), binary.BigEndian.Uint16(frame.Payload[:2]))
}

func TestDrainStreams_cancelsTheSocketsContext(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	done := make(chan struct{})

	require.NoError(t, r.WebSocket("/ws", func(ctx Context, conn Connection) error {
		<-conn.Context().Done()
		<-ctx.Context().Done()
		close(done)

		return nil
	}))

	srv := httptest.NewServer(r)
	defer srv.Close()

	conn, _, _, err := ws.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	drainWithin(t, r, 2*time.Second)

	select {
	case <-done:
	default:
		t.Fatal("the socket's contexts were not cancelled by the drain")
	}
}

func TestDrainStreams_leavesAPlainRequestToFinish(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	entered := make(chan struct{})
	release := make(chan struct{})

	var ctxErr error

	require.NoError(t, r.GET("/slow", func(ctx Context) error {
		close(entered)
		<-release

		ctxErr = ctx.Context().Err()

		return ctx.String(http.StatusOK, "done")
	}))

	srv := httptest.NewServer(r)
	defer srv.Close()

	type result struct {
		status int
		body   string
		err    error
	}

	got := make(chan result, 1)

	go func() {
		resp, err := http.Get(srv.URL + "/slow") //nolint:noctx // bounded by the test
		if err != nil {
			got <- result{err: err}

			return
		}

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		got <- result{status: resp.StatusCode, body: string(body), err: err}
	}()

	<-entered
	drainWithin(t, r, 2*time.Second)
	close(release)

	res := <-got
	require.NoError(t, res.err)
	assert.Equal(t, http.StatusOK, res.status)
	assert.Equal(t, "done", res.body)
	assert.NoError(t, ctxErr)
}

func TestDrainStreams_endsAStreamThatOpensAfterTheDrain(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	require.NoError(t, r.EventStream("/events", func(ctx Context, stream Stream) error {
		<-ctx.Context().Done()

		return nil
	}))

	drainWithin(t, r, time.Second)

	srv := httptest.NewServer(r)
	defer srv.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(srv.URL + "/events") //nolint:noctx // bounded by the client timeout
	require.NoError(t, err)

	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
}

func TestDrainStreams_givesUpWhenAHandlerIgnoresItsContext(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	started := make(chan struct{})
	release := make(chan struct{})

	require.NoError(t, r.EventStream("/stuck", func(ctx Context, stream Stream) error {
		close(started)
		<-release

		return nil
	}))

	srv := httptest.NewServer(r)
	defer srv.Close()
	defer close(release)

	go func() {
		resp, err := http.Get(srv.URL + "/stuck") //nolint:noctx // released by the test
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()

	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := r.DrainStreams(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestDrainStreams_isSharedWithGroups(t *testing.T) {
	r := newRouter(WithContainer(vessel.New()))

	returned := make(chan struct{})

	require.NoError(t, r.Group("/api").EventStream("/events", func(ctx Context, stream Stream) error {
		defer close(returned)

		_ = stream.SendComment("ready")

		<-ctx.Context().Done()

		return nil
	}))

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp := openSSE(t, srv.URL+"/api/events")
	defer resp.Body.Close()

	waitForComment(t, resp, "ready")

	drainWithin(t, r, 2*time.Second)

	select {
	case <-returned:
	default:
		t.Fatal("a group's stream was not drained by its parent router")
	}
}

func TestGoAway_doesNotWaitBehindAWriteToAStalledClient(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	c := newWSConnection("stalled", server, context.Background())

	// net.Pipe has no buffer, so with nobody reading the client end this write
	// blocks while holding the connection lock, like a peer that stopped reading.
	writeErr := make(chan error, 1)

	go func() { writeErr <- c.Write([]byte("never read")) }()

	require.Eventually(t, func() bool {
		if !c.mu.TryLock() {
			return true
		}

		c.mu.Unlock()

		return false
	}, time.Second, time.Millisecond, "the write never took the lock")

	start := time.Now()

	c.goAway()

	assert.Less(t, time.Since(start), 500*time.Millisecond)

	select {
	case err := <-writeErr:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("the blocked write was not released by goAway")
	}

	require.Error(t, c.Context().Err())
}
