package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/xraph/forge/internal/shared"
)

func TestCollector_ExportPrometheusParses(t *testing.T) {
	c := New(DefaultCollectorConfig(), nil)

	c.Counter("widget_built_total").Inc()
	c.Gauge("widget_pending").Set(4)

	out, err := c.Export(shared.ExportFormatPrometheus)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// A zero-value TextParser carries UnsetValidation since prometheus/common
	// v0.70 and panics on use, so the scheme has to be named explicitly.
	// LegacyValidation is what this test asserted before the upgrade, and it is
	// the stricter of the two: exporter output stays consumable by scrapers
	// without UTF-8 name support.
	parser := expfmt.NewTextParser(model.LegacyValidation)
	if _, err := parser.TextToMetricFamilies(strings.NewReader(string(out))); err != nil {
		t.Fatalf("output is not valid prometheus text: %v", err)
	}
	if !strings.Contains(string(out), "widget_built_total") {
		t.Fatalf("expected widget_built_total in output, got:\n%s", out)
	}
}

func TestCollector_ImplementsPrometheusProvider(t *testing.T) {
	c := New(DefaultCollectorConfig(), nil)
	if _, ok := c.(shared.PrometheusProvider); !ok {
		t.Fatal("collector does not implement shared.PrometheusProvider")
	}
}
