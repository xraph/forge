package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	mw(routed("/ws/chat", http.StatusForbidden)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws/chat", nil))

	s := exp.GetSpans()[0]
	if v, _ := attrOf(s.Attributes, "http.response.status_code"); v.AsInt64() != 403 {
		t.Fatalf("status = %d", v.AsInt64())
	}

	if _, ok := attrOf(s.Attributes, "forge.stream.kind"); ok {
		t.Fatal("rejected upgrade carries forge.stream.kind")
	}
}
