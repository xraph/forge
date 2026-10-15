package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/xraph/forge/internal/shared"
)

// go-utils' Timer.Percentile takes a fraction between 0 and 1 and returns 0
// for anything outside it, so asking for 50 instead of 0.5 zeroes every
// quantile while the sum and count stay right.
func TestTimerQuantilesReachTheScrape(t *testing.T) {
	c := New(DefaultCollectorConfig(), nil)

	timer := c.Timer("db.query")
	for i := 1; i <= 100; i++ {
		timer.Record(time.Duration(i) * time.Millisecond)
	}

	out, err := c.Export(shared.ExportFormatPrometheus)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	parser := expfmt.NewTextParser(model.LegacyValidation)

	families, err := parser.TextToMetricFamilies(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("output is not valid prometheus text: %v\n%s", err, out)
	}

	family, ok := families["forge_db_query"]
	if !ok {
		t.Fatalf("forge_db_query missing from scrape:\n%s", out)
	}

	summary := family.GetMetric()[0].GetSummary()
	if summary.GetSampleCount() != 100 {
		t.Fatalf("count = %d, want 100", summary.GetSampleCount())
	}

	quantiles := map[float64]float64{}
	for _, q := range summary.GetQuantile() {
		quantiles[q.GetQuantile()] = q.GetValue()
	}

	// go-utils reports a quantile as the upper bound of the bucket it falls
	// in, so the values are coarse. They must still be non-zero, ordered, and
	// in seconds rather than milliseconds.
	p50, p95, p99 := quantiles[0.5], quantiles[0.95], quantiles[0.99]
	if p50 < 0.025 || p50 > 0.1 {
		t.Errorf("p50 = %v, want between 0.025 and 0.1 seconds", p50)
	}

	if p95 < p50 || p99 < p95 {
		t.Errorf("quantiles out of order: p50=%v p95=%v p99=%v", p50, p95, p99)
	}

	if p99 > 1 {
		t.Errorf("p99 = %v, looks like milliseconds rather than seconds", p99)
	}
}
