package collectors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge/internal/metrics/collectors"
	"github.com/xraph/forge/internal/shared"
)

func TestHTTPMetricsHandshakeOnlyForStreams(t *testing.T) {
	m := newCollector(t)
	mw := collectors.NewHTTPServerMetrics(m, collectors.HTTPServerOptions{FreshRouteMatch: true}).Middleware()

	open := make(chan struct{})
	release := make(chan struct{})

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		match := shared.RouteMatchFromContext(r.Context())
		match.Pattern, match.Kind = "/ws/chat", shared.KindWebSocket

		w.WriteHeader(http.StatusSwitchingProtocols)
		match.Establish(http.StatusSwitchingProtocols)

		close(open)
		<-release
	})

	done := make(chan struct{})

	go func() {
		mw(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ws/chat", nil))
		close(done)
	}()

	<-open

	got := series(t, m)
	if got["GET /ws/chat 101 "] != 1 {
		t.Fatalf("handshake not observed while stream open: %v", got)
	}

	if active := activeRequests(t, m); active != 0 {
		t.Fatalf("active requests = %v while stream open, want 0", active)
	}

	close(release)
	<-done

	if got := series(t, m); got["GET /ws/chat 101 "] != 1 {
		t.Fatalf("observation count changed after close: %v", got)
	}

	if active := activeRequests(t, m); active != 0 {
		t.Fatalf("active requests = %v after close, want 0 (no double decrement)", active)
	}
}
