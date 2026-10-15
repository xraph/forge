package forge

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/require"
)

func TestStreamMetricsEndToEnd(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("streams"))
	open := make(chan struct{})
	release := make(chan struct{})

	require.NoError(t, a.Router().WebSocket("/ws/chat", func(ctx Context, conn Connection) error {
		close(open)
		<-release

		return nil
	}))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/chat")
	require.NoError(t, err)

	defer conn.Close()

	<-open

	body := scrapeApp(t, a, false)
	require.Contains(t, body, `forge_stream_connections_active{http_route="/ws/chat",stream_kind="websocket"} 1`)
	require.Contains(t, body, `http_server_request_duration_seconds_count{error_type="",http_request_method="GET",http_response_status_code="101",http_route="/ws/chat",url_scheme="http"} 1`)

	close(release)

	require.Eventually(t, func() bool {
		return strings.Contains(scrapeApp(t, a, false),
			`forge_stream_connections_active{http_route="/ws/chat",stream_kind="websocket"} 0`)
	}, 2*time.Second, 20*time.Millisecond)
}

func TestStreamMetricsRejectedOriginLeavesNoStreamSeries(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("streams-rejected"))
	require.NoError(t, a.Router().WebSocket("/ws", func(ctx Context, conn Connection) error { return nil }))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	header := map[string][]string{"Origin": {"https://evil.example"}}

	conn, _, _, err := ws.Dialer{Header: ws.HandshakeHeaderHTTP(header)}.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	if conn != nil {
		conn.Close()
	}

	require.Error(t, err)

	// The 403 is recorded once the request has finished, so its series
	// showing up means the handler is done and no stream can still open.
	require.Eventually(t, func() bool {
		return strings.Contains(scrapeApp(t, a, false), `http_response_status_code="403"`)
	}, 2*time.Second, 20*time.Millisecond)

	require.NotContains(t, scrapeApp(t, a, false), "forge_stream_connections_active")
}

func TestStreamMetricsDisconnectIsNotAnError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("streams-disconnect"))
	require.NoError(t, a.Router().WebSocket("/ws", func(ctx Context, conn Connection) error {
		_, err := conn.Read() // returns when the client closes

		return err
	}))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	require.Eventually(t, func() bool {
		return strings.Contains(scrapeApp(t, a, false),
			`forge_stream_connection_duration_seconds_count{error_type="",http_route="/ws",stream_kind="websocket"} 1`)
	}, 2*time.Second, 20*time.Millisecond)
}

func TestStreamMetricsWriteToGoneClientIsNotAnError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("streams-write-gone"))
	handlerErr := make(chan error, 1)

	require.NoError(t, a.Router().EventStream("/events", func(ctx Context, s Stream) error {
		for {
			if err := s.Send("tick", []byte("hello")); err != nil {
				handlerErr <- err

				return err
			}

			time.Sleep(time.Millisecond)
		}
	}))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/events", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	// Read one whole event, then hang up while the handler is still writing.
	r := bufio.NewReader(resp.Body)

	for {
		line, err := r.ReadString('\n')
		require.NoError(t, err)

		if line == "\n" {
			break
		}
	}

	require.NoError(t, resp.Body.Close())

	select {
	case err := <-handlerErr:
		t.Logf("handler returned %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler never saw the client go")
	}

	require.Eventually(t, func() bool {
		return strings.Contains(scrapeApp(t, a, false),
			`forge_stream_connection_duration_seconds_count{error_type="",http_route="/events",stream_kind="sse"} 1`)
	}, 2*time.Second, 20*time.Millisecond)
}

func TestStreamMetricsCloseFrameIsNotAnError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("streams-close-frame"))
	require.NoError(t, a.Router().WebSocket("/ws", func(ctx Context, conn Connection) error {
		_, err := conn.Read() // gobwas returns wsutil.ClosedError for the close frame

		return err
	}))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, wsutil.WriteClientMessage(conn, ws.OpClose, ws.NewCloseFrameBody(ws.StatusNormalClosure, "")))

	require.Eventually(t, func() bool {
		return strings.Contains(scrapeApp(t, a, false),
			`forge_stream_connection_duration_seconds_count{error_type="",http_route="/ws",stream_kind="websocket"} 1`)
	}, 2*time.Second, 20*time.Millisecond)
}

func TestStreamMetricsWithTracingAndHTTPMetricsOff(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	off := false
	cfg := DefaultMetricsConfig()
	cfg.Features.HTTPMetrics = false

	a := New(
		WithAppName("streams-bare"),
		WithAppTracingConfig(TracingConfig{Enabled: &off}),
		WithAppMetricsConfig(cfg),
	)
	open := make(chan struct{})
	release := make(chan struct{})

	require.NoError(t, a.Router().WebSocket("/ws", func(ctx Context, conn Connection) error {
		close(open)
		<-release

		return nil
	}))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	<-open

	body := scrapeApp(t, a, false)

	close(release)

	require.NotContains(t, body, "http_server_request_duration_seconds", "HTTP metrics are off")
	require.Contains(t, body, `forge_stream_connections_active{http_route="/ws",stream_kind="websocket"} 1`)
}
