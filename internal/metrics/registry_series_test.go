package metrics

import (
	"strconv"
	"testing"

	"github.com/xraph/forge/internal/metrics/exporters"
	gometrics "github.com/xraph/go-utils/metrics"
)

func newCappedRegistry(limit int, onOverflow func(string, int)) Registry {
	return NewRegistryWithConfig(&RegistryConfig{
		MaxMetrics:         10000,
		EnableIndexing:     true,
		MaxSeriesPerMetric: limit,
		OnOverflow:         onOverflow,
	})
}

func TestSeriesCapFoldsIntoOverflow(t *testing.T) {
	warned := 0
	r := newCappedRegistry(3, func(name string, limit int) {
		if name != "orders" || limit != 3 {
			t.Errorf("overflow hook got %q %d", name, limit)
		}

		warned++
	})

	early := r.GetOrCreateCounter("orders", gometrics.WithLabels(map[string]string{"user": "0"}))

	for i := range 10 {
		r.GetOrCreateCounter("orders", gometrics.WithLabels(map[string]string{"user": strconv.Itoa(i)})).Inc()
	}

	early.Inc() // a child created before the cap keeps working

	series := map[string]float64{}

	r.Stream(func(key string, s exporters.Sample) bool {
		series[key] = s.Value

		return true
	})

	if len(series) != 4 { // three real children plus the overflow series
		t.Fatalf("series = %v", series)
	}

	if series["orders{otel_metric_overflow=true}"] != 7 {
		t.Fatalf("overflow = %v, want 7", series["orders{otel_metric_overflow=true}"])
	}

	if series["orders{user=0}"] != 2 {
		t.Fatalf("early child = %v, want 2", series["orders{user=0}"])
	}

	if warned != 1 {
		t.Fatalf("overflow hook called %d times, want 1", warned)
	}
}

func TestWithLabelsGoesThroughRegistry(t *testing.T) {
	r := newCappedRegistry(0, nil)

	parent := r.GetOrCreateCounter("jobs", gometrics.WithLabels(map[string]string{"queue": "mail"}))
	a := parent.WithLabels(map[string]string{"status": "ok"})
	b := parent.WithLabels(map[string]string{"status": "ok"})

	a.Inc()
	b.Inc()

	var got float64

	r.Stream(func(key string, s exporters.Sample) bool {
		if key == "jobs{queue=mail,status=ok}" {
			got = s.Value
		}

		return true
	})

	if got != 2 {
		t.Fatalf("labelled child value = %v, want 2 (same child, visible in the stream)", got)
	}
}

func TestUnitRecorded(t *testing.T) {
	r := newCappedRegistry(0, nil)
	r.GetOrCreateHistogram("http.server.request.duration", gometrics.WithUnit("s"))

	for _, rm := range r.GetRegisteredMetrics() {
		if rm.Name == "http.server.request.duration" && rm.Unit != "s" {
			t.Fatalf("unit = %q", rm.Unit)
		}
	}
}

func TestRoutePatternSurvivesKey(t *testing.T) {
	r := newCappedRegistry(0, nil)
	r.GetOrCreateCounter("hits", gometrics.WithLabels(map[string]string{
		"http.route": "/files/{name}",
		"z":          "last",
	})).Inc()

	r.Stream(func(key string, _ exporters.Sample) bool {
		name, labels := exporters.ParseMetricKeyForTest(key)
		if name != "hits" || labels["http.route"] != "/files/{name}" || labels["z"] != "last" {
			t.Fatalf("key %q parsed to %q %v", key, name, labels)
		}

		return true
	})
}
