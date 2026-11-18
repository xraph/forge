package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type sampledCounter struct{ sampled atomic.Int64 }

func (c *sampledCounter) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (c *sampledCounter) OnEnd(s sdktrace.ReadOnlySpan) {
	if s.SpanContext().IsSampled() {
		c.sampled.Add(1)
	}
}
func (c *sampledCounter) Shutdown(context.Context) error   { return nil }
func (c *sampledCounter) ForceFlush(context.Context) error { return nil }

// countSampled serves n requests that each carry a client-chosen traceparent
// with the sampled flag set, and returns how many server spans were sampled.
func countSampled(t *testing.T, opts ...AppOption) (int64, string) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_TRACES_SAMPLER", "")

	cfg := DefaultAppConfig()
	cfg.EnableConfigAutoDiscovery = false
	cfg.Logger = NewNoopLogger()

	a := New(append([]AppOption{WithConfig(cfg), WithAppEnvironment("production")}, opts...)...)

	sp := &sampledCounter{}
	if err := RegisterSpanProcessor(a, sp); err != nil {
		t.Fatal(err)
	}

	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })

	const n = 1000

	var lastTraceparent string

	for i := range n {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		r.Header.Set("Traceparent", fmt.Sprintf("00-%032x-%016x-01", i+1, i+1))

		w := httptest.NewRecorder()
		a.Router().ServeHTTP(w, r)

		lastTraceparent = w.Header().Get("Traceparent")
	}

	return sp.sampled.Load(), lastTraceparent
}

// Any client could force 100% sampling by sending traceparent with the
// sampled flag, and sequential trace IDs defeated a trace-ID ratio too. In
// production (ratio 0.1) a client now gets about a tenth sampled, and the
// trace ID it sent is still the one the server continues.
func TestTracing_ClientCannotForceSampling(t *testing.T) {
	sampled, tp := countSampled(t)

	if sampled > 200 {
		t.Fatalf("%d of 1000 requests with a client-set sampled flag were sampled at ratio 0.1", sampled)
	}

	if want := fmt.Sprintf("00-%032x-", 1000); len(tp) < len(want) || tp[:len(want)] != want {
		t.Errorf("response traceparent %q does not continue the client's trace ID", tp)
	}
}

// With trusted callers the incoming decision is followed, as before.
func TestTracing_TrustRemoteSamplingFollowsCaller(t *testing.T) {
	if sampled, _ := countSampled(t, WithTrustRemoteSampling()); sampled != 1000 {
		t.Fatalf("trusted callers: %d of 1000 sampled, want all", sampled)
	}
}

// The forge config key turns trust on too.
func TestTracing_TrustRemoteSamplingFromConfigFile(t *testing.T) {
	cm := NewDefaultConfigManager(NewNoopLogger(), NewNoOpMetrics(), nil)
	cm.Set("tracing", map[string]any{"trust_remote_sampling": true})

	if sampled, _ := countSampled(t, WithAppConfigManager(cm)); sampled != 1000 {
		t.Fatalf("tracing.trust_remote_sampling: %d of 1000 sampled, want all", sampled)
	}
}
