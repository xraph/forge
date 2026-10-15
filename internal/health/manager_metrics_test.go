package health

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/go-utils/metrics"
)

// The exporter puts its namespace, forge_ by default, in front of every
// metric name. The manager's names used to start with "forge." as well, so
// they reached the scrape as forge_forge_health_*.
func TestManagerMetrics_LeaveTheNamespaceToTheExporter(t *testing.T) {
	m := metrics.NewMetricsCollector("test")
	mgr := New(nil, nil, m, nil).(*ManagerImpl)
	registerN(t, mgr, 2)
	mgr.Check(context.Background())

	names := m.ListMetrics()
	for name := range names {
		if strings.HasPrefix(name, "forge.") {
			t.Errorf("metric %q starts with forge., the exporter would double the prefix", name)
		}
	}

	for _, want := range []string{
		"health.service.registered_checks",
		"health.service.checks_registered",
		"health.service.checks_performed",
		"health.service.check_duration",
		"health.service.overall_status",
	} {
		if !hasMetric(names, want) {
			t.Errorf("missing metric %q", want)
		}
	}

	if hasMetric(names, "health.registered_checks") {
		t.Error("health.registered_checks duplicates health.service.registered_checks")
	}
}

// Check durations are observed in seconds. On go-utils' default buckets, which
// run from 0 to 10000, all of them would land in the first bucket.
func TestManagerMetrics_CheckDurationUsesSecondsBuckets(t *testing.T) {
	m := metrics.NewMetricsCollector("test")
	mgr := New(nil, nil, m, nil).(*ManagerImpl)
	registerN(t, mgr, 1)
	mgr.Check(context.Background())
	mgr.CheckOne(context.Background(), "ext-0")

	for _, name := range []string{"health.service.check_duration", "health.service.single_check_duration"} {
		buckets := m.Histogram(name).Buckets()

		if _, ok := buckets[0.005]; !ok {
			t.Errorf("%s has no 0.005 bucket: %v", name, buckets)
		}

		if _, ok := buckets[10000]; ok {
			t.Errorf("%s still has the default 10000 bucket", name)
		}
	}
}

func hasMetric(names map[string]any, want string) bool {
	for name := range names {
		if name == want || strings.HasPrefix(name, want+"{") {
			return true
		}
	}

	return false
}
