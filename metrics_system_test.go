package forge

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// A plain app has system_metrics on by default, so once it starts the scrape
// carries the host series. Filesystem usage is the probe every platform
// supports (statfs on Unix, GetDiskFreeSpaceExW on Windows), so it is the one
// this test can rely on. The collector always measures the root volume: / on
// Unix and C:\ on Windows.
func TestStartedAppExportsSystemSeries(t *testing.T) {
	rootMount := "/"
	if runtime.GOOS == "windows" {
		rootMount = `C:\`
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("system-metrics"))
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}

	t.Cleanup(func() { _ = a.Stop(context.Background()) })

	text := scrapeApp(t, a, false)

	p := expfmt.NewTextParser(model.LegacyValidation)

	families, err := p.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatalf("scrape does not parse: %v", err)
	}

	size, ok := families["forge_system_filesystem_size_bytes"]
	if !ok {
		t.Fatalf("no forge_system_filesystem_size_bytes family:\n%s", text)
	}

	var root float64

	for _, m := range size.GetMetric() {
		for _, l := range m.GetLabel() {
			if l.GetName() == "mountpoint" && l.GetValue() == rootMount {
				root = m.GetGauge().GetValue()
			}
		}
	}

	if root <= 0 {
		t.Fatalf("forge_system_filesystem_size_bytes{mountpoint=%q} = %v, want a positive size", rootMount, root)
	}

	// The old series put the mount point in the metric name. None may remain.
	for name := range families {
		if strings.HasPrefix(name, "forge_system_disk_") || strings.HasPrefix(name, "forge_system_network_") {
			t.Errorf("legacy system series %s still exported", name)
		}
	}
}

// Stopping the app stops the collector, and starting a second app is not
// confused by the first one's state.
func TestAppStartStopsMetricsCollector(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	a := New(WithAppName("system-metrics-lifecycle"))
	if err := a.Start(t.Context()); err != nil {
		t.Fatalf("start: %v", err)
	}

	started, ok := a.Metrics().(interface{ IsStarted() bool })
	if !ok {
		t.Fatal("metrics collector does not report IsStarted")
	}

	if !started.IsStarted() {
		t.Fatal("metrics collector not started by app.Start")
	}

	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if started.IsStarted() {
		t.Fatal("metrics collector still started after app.Stop")
	}
}
