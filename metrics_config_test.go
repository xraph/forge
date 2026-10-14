package forge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// metricsFile returns a config manager holding metrics as the "metrics" key,
// the way a forge.yaml block reaches newApp.
func metricsFile(metrics map[string]any) ConfigManager {
	cm := NewDefaultConfigManager(logger.NewTestLogger(), NewNoOpMetrics(), nil)
	cm.Set("metrics", metrics)

	return cm
}

func scrapeFamilies(t *testing.T, a App) map[string]*dto.MetricFamily {
	t.Helper()

	text := scrapeApp(t, a, false)

	p := expfmt.NewTextParser(model.LegacyValidation)

	families, err := p.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatalf("scrape does not parse: %v\n%s", err, text)
	}

	return families
}

func serve(t *testing.T, a App, method, path, body string) {
	t.Helper()

	a.Router().ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body)))
}

// Code turns body sizes on and the file only says metrics are enabled. The
// code value must survive the merge.
func TestMetricsConfigCodeBodySizesSurviveFile(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	code := DefaultMetricsConfig()
	code.Features.HTTPBodySizes = true

	a := New(WithAppName("body-sizes"), WithAppMetricsConfig(code),
		WithAppConfigManager(metricsFile(map[string]any{"enabled": true})))
	_ = a.Router().POST("/echo", func(c Context) error { return c.String(http.StatusOK, "ok") })

	serve(t, a, http.MethodPost, "/echo", "hello")

	if _, ok := scrapeFamilies(t, a)["http_server_request_body_size_bytes"]; !ok {
		t.Fatal("code set Features.HTTPBodySizes but the scrape has no request body size series")
	}
}

// Code caps series per metric and the file only says metrics are enabled.
func TestMetricsConfigCodeSeriesCapSurvivesFile(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	code := DefaultMetricsConfig()
	code.Limits.MaxSeriesPerMetric = 1

	a := New(WithAppName("series-cap"), WithAppMetricsConfig(code),
		WithAppConfigManager(metricsFile(map[string]any{"enabled": true})))

	orders := CounterVec(a.Metrics(), "orders_total", "tier")
	orders.With("free").Inc()
	orders.With("pro").Inc()

	if !strings.Contains(scrapeApp(t, a, false), `otel_metric_overflow="true"`) {
		t.Fatal("code set Limits.MaxSeriesPerMetric=1 but a second label set did not overflow")
	}
}

// With no metrics config in code, a file that turns HTTP metrics off wins.
func TestMetricsConfigFileTurnsHTTPMetricsOff(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("file-http-off"), WithAppConfigManager(metricsFile(map[string]any{
		"enabled":  true,
		"features": map[string]any{"http_metrics": false},
	})))
	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })

	serve(t, a, http.MethodGet, "/x", "")

	if _, ok := scrapeFamilies(t, a)["http_server_request_duration_seconds"]; ok {
		t.Fatal("features.http_metrics: false in the file, but HTTP series were recorded")
	}
}

// A file block without an enabled key still applies on top of the defaults.
func TestMetricsConfigFileWithoutEnabledKeyApplies(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("file-no-enabled"), WithAppConfigManager(metricsFile(map[string]any{
		"features": map[string]any{"http_metrics": false},
	})))
	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })

	serve(t, a, http.MethodGet, "/x", "")

	families := scrapeFamilies(t, a)
	if _, ok := families["http_server_request_duration_seconds"]; ok {
		t.Fatal("features.http_metrics: false in the file, but HTTP series were recorded")
	}

	if _, ok := families["go_goroutines"]; !ok {
		t.Fatal("metrics should stay on when the file has no enabled key")
	}
}

// With no metrics config in code, the file's namespace prefixes custom metrics.
func TestMetricsConfigFileNamespace(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("file-ns"), WithAppConfigManager(metricsFile(map[string]any{
		"enabled":    true,
		"collection": map[string]any{"namespace": "myapp"},
	})))

	a.Metrics().Counter("orders_total").Inc()

	families := scrapeFamilies(t, a)
	if _, ok := families["myapp_orders_total"]; !ok {
		names := make([]string, 0, len(families))
		for n := range families {
			names = append(names, n)
		}

		t.Fatalf("collection.namespace: myapp in the file, want myapp_orders_total, got %v", names)
	}
}

// A file that only says enabled keeps every default feature on.
func TestMetricsConfigEnabledOnlyFileKeepsDefaults(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("enabled-only"), WithAppConfigManager(metricsFile(map[string]any{"enabled": true})))
	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })

	serve(t, a, http.MethodGet, "/x", "")

	f := a.(*app).config.MetricsConfig.Features
	if !f.HTTPMetrics || !f.RuntimeMetrics || !f.SystemMetrics {
		t.Fatalf("features = %+v, want http, runtime and system metrics on", f)
	}

	families := scrapeFamilies(t, a)
	for _, name := range []string{"http_server_request_duration_seconds", "go_goroutines"} {
		if _, ok := families[name]; !ok {
			t.Errorf("enabled-only file: scrape has no %s", name)
		}
	}
}

// A file that only says enabled: false turns metrics off when code set none.
func TestMetricsConfigFileDisables(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("file-disabled"), WithAppConfigManager(metricsFile(map[string]any{"enabled": false})))

	if _, ok := a.Metrics().(shared.PrometheusProvider); ok {
		t.Fatal("metrics.enabled: false in the file, but the app built a live collector")
	}
}

// When code sets a metrics config, code wins: the file can't turn HTTP
// metrics off.
func TestMetricsConfigCodeWinsOverFile(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("code-wins"), WithAppMetricsConfig(DefaultMetricsConfig()),
		WithAppConfigManager(metricsFile(map[string]any{
			"enabled":  true,
			"features": map[string]any{"http_metrics": false},
		})))
	_ = a.Router().GET("/x", func(c Context) error { return c.String(http.StatusOK, "ok") })

	serve(t, a, http.MethodGet, "/x", "")

	if _, ok := scrapeFamilies(t, a)["http_server_request_duration_seconds"]; !ok {
		t.Fatal("code set HTTPMetrics: true, the file must not turn it off")
	}
}

type reloadSpy struct {
	Metrics

	got *shared.MetricsConfig
}

func (r *reloadSpy) Reload(cfg *shared.MetricsConfig) error {
	r.got = cfg

	return nil
}

// Start reloads the metrics config from the manager. It must reload the
// resolved config, not the raw file block.
func TestMetricsConfigReloadUsesResolvedConfig(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("reload"), WithAppConfigManager(metricsFile(map[string]any{"enabled": true}))).(*app)

	spy := &reloadSpy{Metrics: a.metrics}
	a.metrics = spy

	if err := a.reloadConfigsFromManager(); err != nil {
		t.Fatal(err)
	}

	if spy.got == nil {
		t.Fatal("reload did not call Metrics.Reload")
	}

	if !spy.got.Features.HTTPMetrics || spy.got.Collection.Namespace != "forge" {
		t.Fatalf("reloaded config = %+v, want the defaults with the file applied", spy.got)
	}
}

// NewWithConfig callers set MetricsConfig without the option. A config that
// differs from the defaults still counts as code and wins over the file.
func TestMetricsConfigNewWithConfigCodeWins(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	cfg := DefaultAppConfig()
	cfg.Name = "with-config"
	cfg.MetricsConfig.Collection.Namespace = "code"
	cfg.ConfigManager = metricsFile(map[string]any{
		"enabled":    true,
		"collection": map[string]any{"namespace": "file"},
	})

	a := NewWithConfig(cfg)
	a.Metrics().Counter("orders_total").Inc()

	if _, ok := scrapeFamilies(t, a)["code_orders_total"]; !ok {
		t.Fatal("namespace set in code should win over the file")
	}
}
