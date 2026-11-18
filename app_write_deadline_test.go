package forge

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// newDeadlineServer serves the app the way Run does, through serverHandler.
func newDeadlineServer(t *testing.T, a *app) *httptest.Server {
	t.Helper()

	srv := httptest.NewUnstartedServer(a.serverHandler())
	srv.Start()
	t.Cleanup(srv.Close)

	return srv
}

func newDeadlineApp(t *testing.T, timeout time.Duration) *app {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	cfg := DefaultAppConfig()
	cfg.EnableConfigAutoDiscovery = false
	cfg.Logger = NewNoopLogger()
	cfg.HTTPWriteTimeout = timeout

	return New(WithConfig(cfg)).(*app)
}

// A client that asks for a large body and never reads it used to pin the
// handler and the connection forever. The write deadline now cuts it off.
func TestWriteDeadline_SlowReaderIsCutOff(t *testing.T) {
	a := newDeadlineApp(t, 500*time.Millisecond)

	var entered, returned atomic.Int32

	body := bytes.Repeat([]byte("x"), 32<<20)

	_ = a.Router().GET("/big", func(c Context) error {
		entered.Add(1)
		defer returned.Add(1)

		c.Response().Header().Set("Content-Type", "application/octet-stream")
		_, err := c.Response().Write(body)

		return err
	})

	srv := newDeadlineServer(t, a)
	addr := strings.TrimPrefix(srv.URL, "http://")

	const clients = 5

	for range clients {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", addr)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { _ = conn.Close() })

		_ = conn.(*net.TCPConn).SetReadBuffer(4096)
		_, _ = fmt.Fprintf(conn, "GET /big HTTP/1.1\r\nHost: x\r\n\r\n")
		// Never read.
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if entered.Load() == clients && returned.Load() == clients {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("handlers entered=%d returned=%d: slow readers still pin handlers past the write deadline",
		entered.Load(), returned.Load())
}

// An SSE stream outlives the write deadline, both when the client says it
// wants an event stream and when it does not (the route kind decides).
func TestWriteDeadline_SSEKeepsStreaming(t *testing.T) {
	a := newDeadlineApp(t, 200*time.Millisecond)

	_ = a.Router().SSE("/events", func(c Context) error {
		for i := range 8 {
			if err := c.WriteSSE("tick", strconv.Itoa(i)); err != nil {
				return err
			}

			time.Sleep(100 * time.Millisecond)
		}

		return nil
	})

	srv := newDeadlineServer(t, a)

	for _, accept := range []string{"text/event-stream", ""} {
		t.Run("accept="+accept, func(t *testing.T) {
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/events", nil)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}

			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			events := 0

			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				if strings.HasPrefix(sc.Text(), "event: tick") {
					events++
				}
			}

			// Eight events over 800ms, four times the write deadline.
			if events != 8 {
				t.Errorf("got %d of 8 events (scan err %v): the stream was cut by the write deadline", events, sc.Err())
			}
		})
	}
}

// A plain handler that streams with Accept: text/event-stream is exempt too,
// including on a keep-alive connection whose previous request had a deadline
// that has since expired.
func TestWriteDeadline_StreamAfterOrdinaryRequestOnSameConnection(t *testing.T) {
	a := newDeadlineApp(t, 200*time.Millisecond)

	_ = a.Router().GET("/ping", func(c Context) error { return c.String(http.StatusOK, "pong") })
	_ = a.Router().GET("/manual", func(c Context) error {
		w := c.Response()
		w.Header().Set("Content-Type", "text/event-stream")

		for i := range 6 {
			_, _ = fmt.Fprintf(w, "data: %d\n\n", i)
			_ = http.NewResponseController(w).Flush()

			time.Sleep(100 * time.Millisecond)
		}

		return nil
	})

	srv := newDeadlineServer(t, a)
	client := srv.Client()

	pingReq, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/ping", nil)

	resp, err := client.Do(pingReq)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = bufio.NewReader(resp.Body).ReadString('\n')
	_ = resp.Body.Close()

	// Let the first request's deadline expire on the idle connection.
	time.Sleep(300 * time.Millisecond)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/manual", nil)
	req.Header.Set("Accept", "text/event-stream")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	n := 0

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data:") {
			n++
		}
	}

	if n != 6 {
		t.Errorf("got %d of 6 events: the stream inherited a write deadline", n)
	}
}

// A WebSocket connection keeps working well past the write deadline.
func TestWriteDeadline_WebSocketKeepsWorking(t *testing.T) {
	a := newDeadlineApp(t, 200*time.Millisecond)

	_ = a.Router().WebSocket("/ws", func(c Context, conn Connection) error {
		for {
			msg, err := conn.Read()
			if err != nil {
				return nil //nolint:nilerr // the client hung up
			}

			if err := conn.Write(msg); err != nil {
				return err
			}
		}
	})

	srv := newDeadlineServer(t, a)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Same-origin, so the router's Origin check accepts it.
	d := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Origin": {srv.URL}})}

	conn, _, _, err := d.Dial(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for i := range 4 {
		time.Sleep(150 * time.Millisecond)

		want := fmt.Sprintf("hello %d", i)
		if err := wsutil.WriteClientText(conn, []byte(want)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}

		got, err := wsutil.ReadServerText(conn)
		if err != nil {
			t.Fatalf("read %d after %v: %v", i, time.Duration(i+1)*150*time.Millisecond, err)
		}

		if string(got) != want {
			t.Fatalf("echo %d = %q, want %q", i, got, want)
		}
	}
}

// A negative timeout turns the deadline off and serves the router directly.
func TestWriteDeadline_NegativeDisables(t *testing.T) {
	a := newDeadlineApp(t, -1)
	if a.serverHandler() != http.Handler(a.router) {
		t.Fatal("a negative HTTPWriteTimeout should serve the router unwrapped")
	}
}
