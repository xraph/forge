package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/shared"
)

func streamHandler(pattern string, kind shared.RouteKind, status int, hold time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := shared.RouteMatchFromContext(r.Context())
		m.Pattern, m.Kind = pattern, kind

		m.Establish(status)
		time.Sleep(hold)
	})
}

func TestStreamSpanEndsAtEstablishment(t *testing.T) {
	exp, mw := setup(t, Settings{ExcludePaths: []string{"/_/"}})

	start := time.Now()

	mw(streamHandler("/ws/chat", shared.KindWebSocket, http.StatusSwitchingProtocols, 200*time.Millisecond)).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws/chat", nil))

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}

	s := spans[0]

	if s.Name != "GET /ws/chat" {
		t.Fatalf("name = %q", s.Name)
	}

	if s.EndTime.Sub(start) >= 200*time.Millisecond {
		t.Fatalf("span lasted %v, want it to end at the handshake", s.EndTime.Sub(start))
	}

	if v, _ := attrOf(s.Attributes, "forge.stream.kind"); v.AsString() != "websocket" {
		t.Fatalf("forge.stream.kind = %q", v.AsString())
	}

	if v, _ := attrOf(s.Attributes, "http.response.status_code"); v.AsInt64() != 101 {
		t.Fatalf("status = %d", v.AsInt64())
	}
}

func TestPlainRouteHasNoStreamKind(t *testing.T) {
	exp, mw := setup(t, Settings{ExcludePaths: []string{"/_/"}})
	mw(routed("/a", http.StatusOK)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/a", nil))

	if _, ok := attrOf(exp.GetSpans()[0].Attributes, "forge.stream.kind"); ok {
		t.Fatal("ordinary route carries forge.stream.kind")
	}
}

func TestRejectedUpgradeIsOrdinarySpan(t *testing.T) {
	exp, mw := setup(t, Settings{ExcludePaths: []string{"/_/"}})

	// A real router, so the WebSocket kind is stamped on the holder before
	// the Origin check turns the upgrade away.
	r := router.NewRouter(router.WithWebSocketOrigins("https://good.example"))
	if err := r.WebSocket("/ws/chat", func(ctx router.Context, conn router.Connection) error { return nil }); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws/chat", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "https://evil.example")

	rec := httptest.NewRecorder()
	mw(r).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("response = %d, want 403", rec.Code)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}

	s := spans[0]
	if v, _ := attrOf(s.Attributes, "http.response.status_code"); v.AsInt64() != 403 {
		t.Fatalf("status = %d", v.AsInt64())
	}

	if v, ok := attrOf(s.Attributes, "forge.stream.kind"); ok {
		t.Fatalf("rejected upgrade carries forge.stream.kind=%q", v.AsString())
	}
}

func TestRouteMatchMiddlewareSkipsExcludedPaths(t *testing.T) {
	var got *shared.RouteMatch

	mw := RouteMatchMiddleware([]string{"/_/"})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = shared.RouteMatchFromContext(r.Context())
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws/chat", nil))

	if got == nil {
		t.Fatal("no holder on an ordinary path")
	}

	got = nil

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_/stream", nil))

	if got != nil {
		t.Fatal("holder installed on an excluded path")
	}
}
