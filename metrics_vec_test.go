package forge_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/shared"
	forgetesting "github.com/xraph/forge/testing"
)

func scrapeText(t *testing.T, m forge.Metrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	m.(shared.PrometheusProvider).PrometheusHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	return rec.Body.String()
}

func TestCounterVecReachesScrape(t *testing.T) {
	a := forge.New(forge.WithAppName("vec"))
	orders := forge.CounterVec(a.Metrics(), "orders_created_total", "tier", "region")

	orders.With("pro", "eu").Inc()
	orders.With("pro", "eu").Inc()

	body := scrapeText(t, a.Metrics())
	if !strings.Contains(body, `forge_orders_created_total{region="eu",tier="pro"} 2`) {
		t.Fatalf("scrape:\n%s", body)
	}
}

func TestStrictMetricsFailsOnWrongArity(t *testing.T) {
	a := forge.New(forge.WithAppName("strict"))
	v := forge.CounterVec(a.Metrics(), "x_total", "a", "b")

	ft := &fakeTB{TB: t}
	forgetesting.StrictMetrics(ft)
	v.With("only-one").Inc()

	if !ft.failed {
		t.Fatal("StrictMetrics did not fail the test on a wrong-arity With")
	}
}

// fakeTB records Errorf instead of failing the real test.
type fakeTB struct {
	testing.TB

	failed bool
}

func (f *fakeTB) Errorf(string, ...any) { f.failed = true }
func (f *fakeTB) Helper()               {}

func TestHistogramVecUnitGivesSemconvSuffix(t *testing.T) {
	a := forge.New(forge.WithAppName("vec-unit"))
	h := forge.HistogramVec(a.Metrics(), "rpc.server.call.duration",
		forge.HistogramOpts{Buckets: []float64{0.1, 1}, Unit: "s"}, "rpc.method")

	h.With("Ping").Observe(0.05)

	if body := scrapeText(t, a.Metrics()); !strings.Contains(body, `rpc_server_call_duration_seconds_count{rpc_method="Ping"} 1`) {
		t.Fatalf("scrape:\n%s", body)
	}
}
