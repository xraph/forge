package forge

import (
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/shared"
)

// streamSeries returns the value of the gauge or counter in family whose
// labels match want exactly, and whether there is one.
func streamSeries(t *testing.T, a App, family string, want map[string]string) (float64, bool) {
	t.Helper()

	p := expfmt.NewTextParser(model.LegacyValidation)

	families, err := p.TextToMetricFamilies(strings.NewReader(scrapeApp(t, a, false)))
	require.NoError(t, err)

	f, ok := families[family]
	if !ok {
		return 0, false
	}

next:
	for _, m := range f.GetMetric() {
		if len(m.GetLabel()) != len(want) {
			continue
		}

		for _, l := range m.GetLabel() {
			if want[l.GetName()] != l.GetValue() {
				continue next
			}
		}

		if g := m.GetGauge(); g != nil {
			return g.GetValue(), true
		}

		return m.GetCounter().GetValue(), true
	}

	return 0, false
}

func TestStreamMetricsConcurrentConnections(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	const (
		clients  = 20
		messages = 5
	)

	a := New(WithAppName("streams-concurrent"))

	// Echo every message back until the client closes.
	require.NoError(t, a.Router().WebSocket("/ws/echo", func(ctx Context, conn Connection) error {
		for {
			msg, err := conn.Read()
			if err != nil {
				return err
			}

			if err := conn.Write(msg); err != nil {
				return err
			}
		}
	}))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/echo"

	var wg sync.WaitGroup

	for c := range clients {
		wg.Go(func() {
			conn, _, _, err := ws.Dial(t.Context(), url)
			if err != nil {
				t.Errorf("client %d: dial: %v", c, err)

				return
			}

			defer conn.Close()

			for i := range messages {
				want := fmt.Sprintf("client %d message %d", c, i)

				if err := wsutil.WriteClientText(conn, []byte(want)); err != nil {
					t.Errorf("client %d: write: %v", c, err)

					return
				}

				got, err := wsutil.ReadServerText(conn)
				if err != nil {
					t.Errorf("client %d: read: %v", c, err)

					return
				}

				if string(got) != want {
					t.Errorf("client %d: echo = %q, want %q", c, got, want)

					return
				}
			}

			if err := wsutil.WriteClientMessage(conn, ws.OpClose, ws.NewCloseFrameBody(ws.StatusNormalClosure, "")); err != nil {
				t.Errorf("client %d: close frame: %v", c, err)
			}
		})
	}

	wg.Wait()

	open := map[string]string{"http_route": "/ws/echo", "stream_kind": "websocket"}

	require.Eventually(t, func() bool {
		v, ok := streamSeries(t, a, "forge_stream_connections_active", open)

		return ok && v == 0
	}, 5*time.Second, 20*time.Millisecond, "every connection should close")

	for _, direction := range []string{"sent", "received"} {
		v, ok := streamSeries(t, a, "forge_stream_messages_total",
			map[string]string{"http_route": "/ws/echo", "stream_kind": "websocket", "direction": direction})
		require.True(t, ok, "no %s series", direction)
		require.InDelta(t, float64(clients*messages), v, 0, "%s messages", direction)
	}

	lifetimes := histogramLabels(t, a, "forge_stream_connection_duration_seconds")
	require.Len(t, lifetimes, 1, "every close should be clean: %v", lifetimes)
	require.Equal(t, "", lifetimes[0]["error_type"])
	require.Equal(t, strconv.Itoa(clients), lifetimes[0]["_count"])
}

func TestStreamWithMetricsDisabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	cfg := DefaultMetricsConfig()
	cfg.Enabled = false

	a := New(WithAppName("streams-no-metrics"), WithAppMetricsConfig(cfg))
	done := make(chan error, 1)

	require.NoError(t, a.Router().WebSocket("/ws", func(ctx Context, conn Connection) error {
		msg, err := conn.Read()
		if err != nil {
			done <- err

			return err
		}

		err = conn.Write(msg)
		done <- err

		return err
	}))

	srv := httptest.NewServer(a.Router())
	t.Cleanup(srv.Close)

	conn, _, _, err := ws.Dial(t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws")
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, wsutil.WriteClientText(conn, []byte("ping")))

	got, err := wsutil.ReadServerText(conn)
	require.NoError(t, err)
	require.Equal(t, "ping", string(got))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler never returned")
	}

	// With metrics off the app holds the no-op collector, which has no
	// Prometheus handler to scrape, so list what it has instead.
	_, scrapeable := a.Metrics().(shared.PrometheusProvider)
	require.False(t, scrapeable, "metrics are off")

	for name := range a.Metrics().ListMetrics() {
		require.NotContains(t, name, "stream_", "metrics are off")
	}
}
