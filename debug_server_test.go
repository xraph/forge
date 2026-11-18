//go:build forge_debug

package forge

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func newDebugTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("FORGE_DEBUG_PORT", "")

	cfg := DefaultAppConfig()
	cfg.EnableConfigAutoDiscovery = false
	cfg.Logger = NewNoopLogger()

	ds := &debugState{a: New(WithConfig(cfg)).(*app), hub: newDebugHub(), stopCh: make(chan struct{})}

	srv := httptest.NewServer(ds.handler())
	t.Cleanup(srv.Close)

	return srv
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

// Any web page could open a WebSocket to the debug port (browsers do not
// apply CORS to upgrades) and read the app's routes, config and services.
func TestDebugServer_RejectsForeignOrigin(t *testing.T) {
	srv := newDebugTestServer(t)

	d := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Origin": {"https://evil.example"}})}
	if conn, _, _, err := d.Dial(context.Background(), wsURL(srv)); err == nil {
		conn.Close()
		t.Fatal("a cross-origin upgrade was accepted")
	}

	for _, origin := range []string{"", "http://localhost:3000", "http://127.0.0.1:9080"} {
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}

		d := ws.Dialer{Header: ws.HandshakeHeaderHTTP(h)}

		conn, _, _, err := d.Dial(context.Background(), wsURL(srv))
		if err != nil {
			t.Fatalf("origin %q rejected: %v", origin, err)
		}

		if _, err := wsutil.ReadServerText(conn); err != nil {
			t.Fatalf("origin %q got no snapshot: %v", origin, err)
		}

		conn.Close()
	}
}

// DNS rebinding reaches /state through a foreign Host header.
func TestDebugServer_RejectsForeignHost(t *testing.T) {
	srv := newDebugTestServer(t)

	for host, want := range map[string]int{
		"attacker-rebind.example:9080": http.StatusForbidden,
		"localhost:9080":               http.StatusOK,
		"127.0.0.1":                    http.StatusOK,
		"[::1]:9080":                   http.StatusOK,
	} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/state", nil)
		req.Host = host

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		resp.Body.Close()

		if resp.StatusCode != want {
			t.Errorf("Host %q -> %d, want %d", host, resp.StatusCode, want)
		}
	}
}

// A frame header may claim any length; it used to size an allocation.
func TestDebugServer_OversizedFrameClosesConnection(t *testing.T) {
	srv := newDebugTestServer(t)

	conn, _, _, err := ws.Dial(context.Background(), wsURL(srv))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := wsutil.ReadServerText(conn); err != nil {
		t.Fatal(err)
	}

	// A masked binary frame header claiming 1 GiB, with no payload.
	hdr := []byte{0x82, 0xff}
	hdr = binary.BigEndian.AppendUint64(hdr, 1<<30)
	hdr = append(hdr, 1, 2, 3, 4)

	if _, err := conn.Write(hdr); err != nil {
		t.Fatal(err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("the server kept the connection open after an oversized frame")
	} else if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
		t.Fatal("the server is still waiting for the 1 GiB payload")
	}
}
