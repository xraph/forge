package exporters

import (
	"context"
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeStream is a registry stand-in. Tests mutate the map between produces.
type fakeStream map[string]Sample

func (f fakeStream) stream(yield func(string, Sample) bool) {
	for k, s := range f {
		if !yield(k, s) {
			return
		}
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestProducer(s fakeStream, temporality Temporality) (*OTLPProducer, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}

	return NewOTLPProducer(s.stream, OTLPConfig{Namespace: "forge", Temporality: temporality, Now: clock.now}), clock
}

func produce(t *testing.T, p *OTLPProducer) map[string]metricdata.Metrics {
	t.Helper()

	sms, err := p.Produce(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if len(sms) != 1 || sms[0].Scope.Name != otlpScope {
		t.Fatalf("scopes = %+v, want one named %q", sms, otlpScope)
	}

	out := make(map[string]metricdata.Metrics)
	for _, m := range sms[0].Metrics {
		out[m.Name] = m
	}

	return out
}

func attr(set attribute.Set, key string) string {
	v, _ := set.Value(attribute.Key(key))

	return v.AsString()
}

func TestOTLPCounterIsCumulativeMonotonicSum(t *testing.T) {
	s := fakeStream{`orders_created_total{tier="pro"}`: {Kind: SampleCounter, Value: 5}}
	p, clock := newTestProducer(s, Cumulative)
	created := clock.now()

	m, ok := produce(t, p)["forge_orders_created_total"]
	if !ok {
		t.Fatal("forge_orders_created_total missing; non-semconv names take the scrape name")
	}

	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok || !sum.IsMonotonic || sum.Temporality != metricdata.CumulativeTemporality || len(sum.DataPoints) != 1 {
		t.Fatalf("data = %+v, want one monotonic cumulative sum point", m.Data)
	}

	pt := sum.DataPoints[0]
	if pt.Value != 5 || attr(pt.Attributes, "tier") != "pro" || !pt.StartTime.Equal(created) {
		t.Fatalf("point = %+v", pt)
	}

	clock.advance(time.Minute)

	s[`orders_created_total{tier="pro"}`] = Sample{Kind: SampleCounter, Value: 8}

	pt = produce(t, p)["forge_orders_created_total"].Data.(metricdata.Sum[float64]).DataPoints[0]
	if pt.Value != 8 || !pt.StartTime.Equal(created) || !pt.Time.Equal(clock.now()) {
		t.Fatalf("second point = %+v, want 8 with the first start time", pt)
	}
}

func TestOTLPSemconvHistogramKeepsNameUnitAndBuckets(t *testing.T) {
	s := fakeStream{`http.server.request.duration{http.route="/users/:id",http.request.method="GET"}`: {
		Kind: SampleHistogram, Unit: "s", Count: 4, Sum: 1.2,
		Buckets: []Bucket{{UpperBound: 0.1, Count: 1}, {UpperBound: 0.5, Count: 2}},
	}}
	p, _ := newTestProducer(s, Cumulative)

	m, ok := produce(t, p)["http.server.request.duration"]
	if !ok || m.Unit != "s" {
		t.Fatalf("metric = %+v, want http.server.request.duration with unit s", m)
	}

	h := m.Data.(metricdata.Histogram[float64])
	if h.Temporality != metricdata.CumulativeTemporality || len(h.DataPoints) != 1 {
		t.Fatalf("histogram = %+v", h)
	}

	pt := h.DataPoints[0]
	if pt.Count != 4 || pt.Sum != 1.2 {
		t.Fatalf("count, sum = %d, %v", pt.Count, pt.Sum)
	}

	if want := []float64{0.1, 0.5}; !equalFloats(pt.Bounds, want) {
		t.Fatalf("bounds = %v, want %v", pt.Bounds, want)
	}

	if want := []uint64{1, 2, 1}; !equalUints(pt.BucketCounts, want) {
		t.Fatalf("bucket counts = %v, want %v (the last is the overflow)", pt.BucketCounts, want)
	}

	if attr(pt.Attributes, "http.route") != "/users/:id" || attr(pt.Attributes, "http.request.method") != "GET" {
		t.Fatalf("attributes = %v, want dotted keys kept", pt.Attributes)
	}
}

func TestOTLPHistogramBucketsAheadOfCount(t *testing.T) {
	// The registry reads the count before the buckets, so an observation can
	// land in a bucket the count hasn't seen yet.
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 2, Buckets: []Bucket{{UpperBound: 1, Count: 3}}}}
	p, _ := newTestProducer(s, Cumulative)

	pt := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if want := []uint64{3, 0}; !equalUints(pt.BucketCounts, want) || pt.Count != 3 {
		t.Fatalf("counts = %v, count = %d; want %v and 3, never a wrapped overflow", pt.BucketCounts, pt.Count, want)
	}
}

func TestOTLPHistogramInfBoundFolds(t *testing.T) {
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 3, Buckets: []Bucket{
		{UpperBound: 1, Count: 1},
		{UpperBound: math.Inf(1), Count: 2},
	}}}
	p, _ := newTestProducer(s, Cumulative)

	pt := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if !equalFloats(pt.Bounds, []float64{1}) || !equalUints(pt.BucketCounts, []uint64{1, 2}) || pt.Count != 3 {
		t.Fatalf("bounds = %v, counts = %v, count = %d", pt.Bounds, pt.BucketCounts, pt.Count)
	}
}

func TestOTLPGaugeAndTotalGauge(t *testing.T) {
	s := fakeStream{
		"queue_depth":       {Kind: SampleGauge, Value: 3},
		"queue_items_total": {Kind: SampleGauge, Value: 9},
	}
	p, _ := newTestProducer(s, Cumulative)
	got := produce(t, p)

	if g, ok := got["forge_queue_depth"].Data.(metricdata.Gauge[float64]); !ok || g.DataPoints[0].Value != 3 {
		t.Fatalf("queue_depth = %+v, want a gauge of 3", got["forge_queue_depth"].Data)
	}

	if s, ok := got["forge_queue_items_total"].Data.(metricdata.Sum[float64]); !ok || !s.IsMonotonic {
		t.Fatalf("queue_items_total = %+v, want a monotonic sum, as the scrape calls it a counter", got["forge_queue_items_total"].Data)
	}
}

func TestOTLPTimerIsSummary(t *testing.T) {
	s := fakeStream{"job_time": {Kind: SampleTimer, Count: 2, Sum: 3, P50: 1, P95: 2, P99: 2.5}}
	p, _ := newTestProducer(s, Cumulative)

	sum, ok := produce(t, p)["forge_job_time"].Data.(metricdata.Summary)
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("data = %+v, want a summary", sum)
	}

	pt := sum.DataPoints[0]
	want := []metricdata.QuantileValue{{Quantile: 0.5, Value: 1}, {Quantile: 0.95, Value: 2}, {Quantile: 0.99, Value: 2.5}}

	if pt.Count != 2 || pt.Sum != 3 || len(pt.QuantileValues) != 3 {
		t.Fatalf("point = %+v", pt)
	}

	for i, q := range want {
		if pt.QuantileValues[i] != q {
			t.Fatalf("quantile %d = %+v, want %+v", i, pt.QuantileValues[i], q)
		}
	}
}

func TestOTLPOverflowLabelCarried(t *testing.T) {
	s := fakeStream{`orders{otel_metric_overflow="true"}`: {Kind: SampleCounter, Value: 1}}
	p, _ := newTestProducer(s, Cumulative)

	pt := produce(t, p)["forge_orders"].Data.(metricdata.Sum[float64]).DataPoints[0]
	if attr(pt.Attributes, "otel_metric_overflow") != "true" {
		t.Fatalf("attributes = %v, want otel_metric_overflow=true", pt.Attributes)
	}
}

func TestOTLPExemplarsSentOnce(t *testing.T) {
	p, clock := newTestProducer(nil, Cumulative)
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 1, Buckets: []Bucket{{UpperBound: 1, Count: 1}}, Exemplars: []Exemplar{
		{Value: 0.2, TraceID: "0102030405060708090a0b0c0d0e0f10", SpanID: "0102030405060708", Timestamp: clock.now().Add(-time.Second)},
		{Value: 0.3, TraceID: "not-hex", SpanID: "0102030405060708", Timestamp: clock.now().Add(-time.Second)},
	}}}
	p.stream = s.stream

	pt := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if len(pt.Exemplars) != 1 {
		t.Fatalf("exemplars = %+v, want only the decodable one", pt.Exemplars)
	}

	if e := pt.Exemplars[0]; len(e.TraceID) != 16 || e.TraceID[0] != 1 || len(e.SpanID) != 8 || e.Value != 0.2 {
		t.Fatalf("exemplar = %+v", e)
	}

	clock.advance(time.Minute)

	pt = produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if len(pt.Exemplars) != 0 {
		t.Fatalf("exemplars = %+v, want none: they were sent last time", pt.Exemplars)
	}
}

func TestOTLPRawCustomCollectorValues(t *testing.T) {
	s := fakeStream{
		"system.cpu":      {Kind: SampleRaw, Raw: 0.4},
		"jobs_done_total": {Kind: SampleRaw, Raw: map[string]any{"_type": "counter", "value": 12.0}},
		"mystery":         {Kind: SampleUnknown},
	}
	p, _ := newTestProducer(s, Cumulative)
	got := produce(t, p)

	if g, ok := got["forge_system_cpu"].Data.(metricdata.Gauge[float64]); !ok || g.DataPoints[0].Value != 0.4 {
		t.Fatalf("system.cpu = %+v", got["forge_system_cpu"])
	}

	if s, ok := got["forge_jobs_done_total"].Data.(metricdata.Sum[float64]); !ok || s.DataPoints[0].Value != 12 {
		t.Fatalf("jobs_done_total = %+v", got["forge_jobs_done_total"])
	}

	if _, ok := got["forge_mystery"]; ok {
		t.Fatal("an unknown sample was exported")
	}
}

func TestOTLPSeriesThatReturnsGetsNewStartTime(t *testing.T) {
	s := fakeStream{"c": {Kind: SampleCounter, Value: 1}}
	p, clock := newTestProducer(s, Cumulative)

	produce(t, p)
	clock.advance(time.Minute)
	delete(s, "c")
	produce(t, p)

	gone := clock.now()

	clock.advance(time.Minute)

	s["c"] = Sample{Kind: SampleCounter, Value: 1}

	pt := produce(t, p)["forge_c"].Data.(metricdata.Sum[float64]).DataPoints[0]
	if !pt.StartTime.Equal(gone) {
		t.Fatalf("start = %v, want %v: the series was forgotten and began after that produce", pt.StartTime, gone)
	}
}

func TestOTLPCutShortProduceKeepsSeriesState(t *testing.T) {
	s := fakeStream{
		"a": {Kind: SampleCounter, Value: 10},
		"b": {Kind: SampleCounter, Value: 20},
	}
	p, clock := newTestProducer(s, Cumulative)
	created := clock.now()

	produce(t, p)
	clock.advance(time.Minute)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := p.Produce(ctx); err != nil {
		t.Fatalf("a cancelled produce returned %v, want a partial batch and no error", err)
	}

	clock.advance(time.Minute)

	got := produce(t, p)
	for _, name := range []string{"forge_a", "forge_b"} {
		pt := got[name].Data.(metricdata.Sum[float64]).DataPoints[0]
		if !pt.StartTime.Equal(created) {
			t.Fatalf("%s start = %v, want %v: a cut short walk must not fake a reset", name, pt.StartTime, created)
		}
	}
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func equalUints(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestOTLPDeltaCounter(t *testing.T) {
	s := fakeStream{"c": {Kind: SampleCounter, Value: 5}}
	p, clock := newTestProducer(s, Delta)
	created := clock.now()

	sum := produce(t, p)["forge_c"].Data.(metricdata.Sum[float64])
	if sum.Temporality != metricdata.DeltaTemporality {
		t.Fatalf("temporality = %v, want delta", sum.Temporality)
	}

	if pt := sum.DataPoints[0]; pt.Value != 5 || !pt.StartTime.Equal(created) {
		t.Fatalf("first point = %+v, want 5 since creation", pt)
	}

	first := clock.now()

	clock.advance(time.Minute)

	s["c"] = Sample{Kind: SampleCounter, Value: 8}

	if pt := produce(t, p)["forge_c"].Data.(metricdata.Sum[float64]).DataPoints[0]; pt.Value != 3 || !pt.StartTime.Equal(first) {
		t.Fatalf("second point = %+v, want 3 since the first produce", pt)
	}
}

func TestOTLPDeltaCounterReset(t *testing.T) {
	s := fakeStream{"c": {Kind: SampleCounter, Value: 8}}
	p, clock := newTestProducer(s, Delta)

	produce(t, p)

	prev := clock.now()

	clock.advance(time.Minute)

	s["c"] = Sample{Kind: SampleCounter, Value: 2}

	if pt := produce(t, p)["forge_c"].Data.(metricdata.Sum[float64]).DataPoints[0]; pt.Value != 2 || !pt.StartTime.Equal(prev) {
		t.Fatalf("point = %+v, want 2 since the previous produce: the registry reset", pt)
	}
}

func TestOTLPCumulativeResetMovesStartTime(t *testing.T) {
	s := fakeStream{"c": {Kind: SampleCounter, Value: 8}}
	p, clock := newTestProducer(s, Cumulative)

	produce(t, p)

	prev := clock.now()

	clock.advance(time.Minute)

	s["c"] = Sample{Kind: SampleCounter, Value: 2}

	if pt := produce(t, p)["forge_c"].Data.(metricdata.Sum[float64]).DataPoints[0]; pt.Value != 2 || !pt.StartTime.Equal(prev) {
		t.Fatalf("point = %+v, want 2 with a new start time", pt)
	}
}

func TestOTLPDeltaHistogram(t *testing.T) {
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 3, Sum: 1, Buckets: []Bucket{{UpperBound: 1, Count: 2}}}}
	p, clock := newTestProducer(s, Delta)

	// Creation comes before the first produce, so a start time taken from
	// creation would not match the previous produce.
	clock.advance(time.Second)
	produce(t, p)

	prev := clock.now()

	clock.advance(time.Minute)

	s["lat"] = Sample{Kind: SampleHistogram, Count: 7, Sum: 4, Buckets: []Bucket{{UpperBound: 1, Count: 5}}}

	h := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64])
	if h.Temporality != metricdata.DeltaTemporality {
		t.Fatalf("temporality = %v", h.Temporality)
	}

	pt := h.DataPoints[0]
	if !equalUints(pt.BucketCounts, []uint64{3, 1}) || pt.Count != 4 || pt.Sum != 3 {
		t.Fatalf("point = counts %v, count %d, sum %v; want [3 1], 4, 3", pt.BucketCounts, pt.Count, pt.Sum)
	}

	if !pt.StartTime.Equal(prev) {
		t.Fatalf("start = %v, want %v: the previous produce", pt.StartTime, prev)
	}
}

func TestOTLPDeltaBucketLayoutChangeIsReset(t *testing.T) {
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 2, Buckets: []Bucket{{UpperBound: 1, Count: 2}}}}
	p, clock := newTestProducer(s, Delta)

	// Creation comes before the first produce, so a start time taken from
	// creation would not match the previous produce.
	clock.advance(time.Second)
	produce(t, p)

	prev := clock.now()

	clock.advance(time.Minute)

	s["lat"] = Sample{Kind: SampleHistogram, Count: 5, Buckets: []Bucket{{UpperBound: 1, Count: 2}, {UpperBound: 2, Count: 3}}}

	pt := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if !equalUints(pt.BucketCounts, []uint64{2, 3, 0}) || pt.Count != 5 {
		t.Fatalf("counts = %v, count = %d; want the full new counts", pt.BucketCounts, pt.Count)
	}

	if !pt.StartTime.Equal(prev) {
		t.Fatalf("start = %v, want %v: the reset began at the previous produce", pt.StartTime, prev)
	}
}

func TestOTLPDeltaThirdProduceAndClamp(t *testing.T) {
	s := fakeStream{
		"c":   {Kind: SampleCounter, Value: 5},
		"lat": {Kind: SampleHistogram, Count: 2, Sum: 1, Buckets: []Bucket{{UpperBound: 1, Count: 2}}},
	}
	p, clock := newTestProducer(s, Delta)

	produce(t, p)

	first := clock.now()

	clock.advance(time.Minute)

	s["c"] = Sample{Kind: SampleCounter, Value: 8}
	// The overflow bucket is 1 here and 0 in the next produce, while the
	// total count rises from 4 to 5: a dip the delta has to clamp.
	s["lat"] = Sample{Kind: SampleHistogram, Count: 4, Sum: 3, Buckets: []Bucket{{UpperBound: 1, Count: 3}}}

	got := produce(t, p)
	second := clock.now()

	if pt := got["forge_c"].Data.(metricdata.Sum[float64]).DataPoints[0]; pt.Value != 3 || !pt.StartTime.Equal(first) {
		t.Fatalf("counter second point = %+v, want 3 since the first produce", pt)
	}

	if pt := got["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]; !equalUints(pt.BucketCounts, []uint64{1, 1}) || pt.Count != 2 || pt.Sum != 2 {
		t.Fatalf("histogram second point = counts %v, count %d, sum %v; want [1 1], 2, 2", pt.BucketCounts, pt.Count, pt.Sum)
	}

	clock.advance(time.Minute)

	s["c"] = Sample{Kind: SampleCounter, Value: 12}
	s["lat"] = Sample{Kind: SampleHistogram, Count: 5, Sum: 6, Buckets: []Bucket{{UpperBound: 1, Count: 5}}}

	got = produce(t, p)

	// Against the second cumulative value, not the delta of the second delta.
	if pt := got["forge_c"].Data.(metricdata.Sum[float64]).DataPoints[0]; pt.Value != 4 || !pt.StartTime.Equal(second) {
		t.Fatalf("counter third point = %+v, want 4 since the second produce", pt)
	}

	pt := got["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if !equalUints(pt.BucketCounts, []uint64{2, 0}) || pt.Sum != 3 || !pt.StartTime.Equal(second) {
		t.Fatalf("histogram third point = counts %v, sum %v, start %v; want [2 0], 3, since the second produce",
			pt.BucketCounts, pt.Sum, pt.StartTime)
	}

	var total uint64
	for _, c := range pt.BucketCounts {
		total += c
	}

	if pt.Count != total {
		t.Fatalf("count = %d, want the sum of the bucket counts %d", pt.Count, total)
	}
}

func TestOTLPDeltaLeavesGaugesAlone(t *testing.T) {
	s := fakeStream{"g": {Kind: SampleGauge, Value: 4}}
	p, clock := newTestProducer(s, Delta)

	produce(t, p)
	clock.advance(time.Minute)

	if pt := produce(t, p)["forge_g"].Data.(metricdata.Gauge[float64]).DataPoints[0]; pt.Value != 4 {
		t.Fatalf("gauge = %v, want 4", pt.Value)
	}
}
