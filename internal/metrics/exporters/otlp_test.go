package exporters

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
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

func TestOTLPDeltaOverflowDipKeepsBaseline(t *testing.T) {
	hist := func(count uint64, finite uint64) Sample {
		return Sample{Kind: SampleHistogram, Count: count, Buckets: []Bucket{{UpperBound: 1, Count: finite}}}
	}

	// Overflow is 5, then 4 with the count unchanged (the count was read before a
	// finite-bucket observation landed), then 5 again with one more in the finite bucket.
	run := func(temporality Temporality) (second, third metricdata.HistogramDataPoint[float64]) {
		s := fakeStream{"lat": hist(10, 5)}
		p, clock := newTestProducer(s, temporality)

		produce(t, p)
		clock.advance(time.Minute)

		s["lat"] = hist(10, 6)
		second = produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]

		clock.advance(time.Minute)

		s["lat"] = hist(11, 6)
		third = produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]

		return second, third
	}

	second, third := run(Delta)
	if !equalUints(second.BucketCounts, []uint64{1, 0}) {
		t.Fatalf("delta second counts = %v, want [1 0]: the dip clamps to zero", second.BucketCounts)
	}

	if !equalUints(third.BucketCounts, []uint64{0, 0}) || third.Count != 0 {
		t.Fatalf("delta third counts = %v, count %d; want [0 0], 0: the overflow never grew past its old baseline",
			third.BucketCounts, third.Count)
	}

	// Cumulative reports the counts as read.
	second, third = run(Cumulative)
	if !equalUints(second.BucketCounts, []uint64{6, 4}) || !equalUints(third.BucketCounts, []uint64{6, 5}) {
		t.Fatalf("cumulative counts = %v then %v; want [6 4] then [6 5]", second.BucketCounts, third.BucketCounts)
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

func TestOTLPRuntimeGoCollector(t *testing.T) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())

	p := NewOTLPProducer(nil, OTLPConfig{Namespace: "forge", Runtime: reg})
	got := produce(t, p)

	if g, ok := got["go_goroutines"].Data.(metricdata.Gauge[float64]); !ok || g.DataPoints[0].Value < 1 {
		t.Fatalf("go_goroutines = %+v, want an unprefixed gauge", got["go_goroutines"])
	}

	if _, ok := got["forge_go_goroutines"]; ok {
		t.Fatal("runtime families must not take the namespace prefix")
	}

	if _, ok := got["go_gc_duration_seconds"].Data.(metricdata.Summary); !ok {
		t.Fatalf("go_gc_duration_seconds = %+v, want a summary", got["go_gc_duration_seconds"])
	}
}

func TestOTLPRuntimeHistogramAndCounter(t *testing.T) {
	reg := prometheus.NewRegistry()
	h := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "rt_seconds", Help: "Runtime histogram.", Buckets: []float64{1, 2}})
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "rt_total", Help: "Runtime counter."})
	reg.MustRegister(h, c)

	for _, v := range []float64{0.5, 1.5, 3} {
		h.Observe(v)
	}

	c.Add(2)

	got := produce(t, NewOTLPProducer(nil, OTLPConfig{Runtime: reg}))

	pt := got["rt_seconds"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if !equalFloats(pt.Bounds, []float64{1, 2}) || !equalUints(pt.BucketCounts, []uint64{1, 1, 1}) || pt.Count != 3 {
		t.Fatalf("histogram = bounds %v counts %v count %d", pt.Bounds, pt.BucketCounts, pt.Count)
	}

	if s, ok := got["rt_total"].Data.(metricdata.Sum[float64]); !ok || !s.IsMonotonic || s.DataPoints[0].Value != 2 {
		t.Fatalf("rt_total = %+v", got["rt_total"])
	}
}

func TestOTLPRuntimeSkippedWhenCutShortKeepsState(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "rt_total", Help: "Runtime counter."})
	reg.MustRegister(c)

	clock := &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	p := NewOTLPProducer(nil, OTLPConfig{Runtime: reg, Now: clock.now})
	created := clock.now()

	first := produce(t, p)["rt_total"].Data.(metricdata.Sum[float64]).DataPoints[0]
	if !first.StartTime.Equal(created) {
		t.Fatalf("first start = %v, want %v", first.StartTime, created)
	}

	// A complete Produce a minute later moves lastRun past created. A series
	// pruned and then seen again would now start at lastRun, not at created.
	clock.advance(time.Minute)

	second := produce(t, p)["rt_total"].Data.(metricdata.Sum[float64]).DataPoints[0]
	if !second.StartTime.Equal(created) {
		t.Fatalf("second start = %v, want %v", second.StartTime, created)
	}

	// A cancelled Produce skips the gather, so it must not prune the series.
	clock.advance(time.Minute)
	c.Add(1)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	sms, err := p.Produce(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range sms[0].Metrics {
		if m.Name == "rt_total" {
			t.Fatal("a cut-short Produce must not emit runtime families")
		}
	}

	clock.advance(time.Minute)
	c.Add(1)

	pt := produce(t, p)["rt_total"].Data.(metricdata.Sum[float64]).DataPoints[0]
	if !pt.StartTime.Equal(created) || pt.Value != 2 {
		t.Fatalf("after cut-short produce: start %v value %v, want %v and 2", pt.StartTime, pt.Value, created)
	}
}

// flippingStream yields the same two samples in the opposite order each time
// it is walked, the worst case for a producer that lets the last or first
// sample win.
func flippingStream(a, b kv) StreamFunc {
	var calls int

	return func(yield func(string, Sample) bool) {
		first, second := a, b
		if calls%2 == 1 {
			first, second = b, a
		}

		calls++

		if !yield(first.key, first.s) {
			return
		}

		yield(second.key, second.s)
	}
}

type kv struct {
	key string
	s   Sample
}

func TestOTLPSanitizeCollisionLowestKeyWins(t *testing.T) {
	for _, temporality := range []Temporality{Cumulative, Delta} {
		stream := flippingStream(
			kv{"jobs_done", Sample{Kind: SampleCounter, Value: 3}},
			kv{"jobs.done", Sample{Kind: SampleCounter, Value: 10}},
		)
		clock := &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
		p := NewOTLPProducer(stream, OTLPConfig{Namespace: "forge", Temporality: temporality, Now: clock.now})
		created := clock.now()

		wantDelta := []float64{10, 0, 0}

		for i := range 3 {
			sum := produce(t, p)["forge_jobs_done"].Data.(metricdata.Sum[float64])
			if len(sum.DataPoints) != 1 {
				t.Fatalf("temporality %v produce %d: %d points, want 1", temporality, i, len(sum.DataPoints))
			}

			pt := sum.DataPoints[0]

			switch temporality {
			case Cumulative:
				if pt.Value != 10 || !pt.StartTime.Equal(created) {
					t.Fatalf("cumulative produce %d = %+v, want 10 since %v: jobs.done sorts first and always wins", i, pt, created)
				}
			case Delta:
				if pt.Value != wantDelta[i] {
					t.Fatalf("delta produce %d = %v, want %v", i, pt.Value, wantDelta[i])
				}
			}

			clock.advance(time.Minute)
		}
	}
}

func TestOTLPRuntimeFamilyCollidingWithRegistrySeries(t *testing.T) {
	// Both resolve to jobs_done_total with no namespace. The runtime family keeps its
	// own name as its key, and the lowest key wins: "jobs.done_total" sorts below it,
	// "jobs~done_total" above. Neither value may ever flip to the other, so a
	// cumulative series keeps its start time and a delta series reports 0.
	for _, tt := range []struct {
		regKey string
		want   float64
	}{{"jobs.done_total", 4}, {"jobs~done_total", 99}} {
		for _, temporality := range []Temporality{Cumulative, Delta} {
			reg := prometheus.NewRegistry()
			c := prometheus.NewCounter(prometheus.CounterOpts{Name: "jobs_done_total", Help: "Runtime counter."})
			reg.MustRegister(c)
			c.Add(99)

			s := fakeStream{tt.regKey: {Kind: SampleCounter, Value: 4}}
			clock := &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
			p := NewOTLPProducer(s.stream, OTLPConfig{Temporality: temporality, Runtime: reg, Now: clock.now})
			created := clock.now()

			for i := range 3 {
				pt := produce(t, p)["jobs_done_total"].Data.(metricdata.Sum[float64]).DataPoints[0]

				switch {
				case temporality == Cumulative && (pt.Value != tt.want || !pt.StartTime.Equal(created)):
					t.Fatalf("key %q cumulative produce %d = %+v, want %v since %v", tt.regKey, i, pt, tt.want, created)
				case temporality == Delta && i == 0 && pt.Value != tt.want:
					t.Fatalf("key %q delta produce 0 = %v, want %v", tt.regKey, pt.Value, tt.want)
				case temporality == Delta && i > 0 && pt.Value != 0:
					t.Fatalf("key %q delta produce %d = %v, want 0: the winner never changes", tt.regKey, i, pt.Value)
				}

				clock.advance(time.Minute)
			}
		}
	}
}

// scriptedStream yields a different list of samples on each walk and honours a
// false return from yield, like the registry does.
func scriptedStream(walks ...[]kv) StreamFunc {
	var calls int

	return func(yield func(string, Sample) bool) {
		walk := walks[min(calls, len(walks)-1)]
		calls++

		for _, e := range walk {
			if !yield(e.key, e.s) {
				return
			}
		}
	}
}

func TestOTLPCutShortProduceDoesNotLetLoserClaimSeries(t *testing.T) {
	winner := kv{"jobs.done", Sample{Kind: SampleCounter, Value: 10}}
	loser := kv{"jobs_done", Sample{Kind: SampleCounter, Value: 3}}

	for _, temporality := range []Temporality{Cumulative, Delta} {
		// Walk 2 reaches only the loser before the context is cancelled.
		stream := scriptedStream([]kv{winner, loser}, []kv{loser, winner}, []kv{winner, loser})
		clock := &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
		p := NewOTLPProducer(stream, OTLPConfig{Namespace: "forge", Temporality: temporality, Now: clock.now})
		created := clock.now()

		first := produce(t, p)["forge_jobs_done"].Data.(metricdata.Sum[float64]).DataPoints[0]
		if first.Value != 10 {
			t.Fatalf("temporality %v first = %+v, want 10", temporality, first)
		}

		clock.advance(time.Minute)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		sms, err := p.Produce(ctx)
		if err != nil {
			t.Fatal(err)
		}

		for _, m := range sms[0].Metrics {
			if m.Name == "forge_jobs_done" {
				t.Fatalf("temporality %v: the cut-short produce exported %+v; the loser must not claim the series", temporality, m.Data)
			}
		}

		clock.advance(time.Minute)

		third := produce(t, p)["forge_jobs_done"].Data.(metricdata.Sum[float64]).DataPoints[0]

		switch temporality {
		case Cumulative:
			if third.Value != 10 || !third.StartTime.Equal(created) {
				t.Fatalf("cumulative third = %+v, want 10 since %v", third, created)
			}
		case Delta:
			if third.Value != 0 {
				t.Fatalf("delta third = %v, want 0: no phantom 7 from a loser that was never the owner", third.Value)
			}
		}
	}
}

func TestOTLPKindClashDoesNotBlockCompatibleCollider(t *testing.T) {
	// All three export as forge_a_b. The lowest key is a counter, the next a
	// gauge the counter family refuses, and the last a counter with another label.
	s := fakeStream{
		"a.b":        {Kind: SampleCounter, Value: 1},
		`a.b{x="1"}`: {Kind: SampleGauge, Value: 2},
		`a_b{x="1"}`: {Kind: SampleCounter, Value: 3},
	}
	p, _ := newTestProducer(s, Cumulative)

	sum, ok := produce(t, p)["forge_a_b"].Data.(metricdata.Sum[float64])
	if !ok || len(sum.DataPoints) != 2 {
		t.Fatalf("forge_a_b = %+v, want a counter with the {} and {x=1} points", sum)
	}

	values := map[string]float64{}
	for _, pt := range sum.DataPoints {
		values[attr(pt.Attributes, "x")] = pt.Value
	}

	if values[""] != 1 || values["1"] != 3 {
		t.Fatalf("values = %v, want {}=1 and {x=1}=3", values)
	}
}

func TestOTLPHistogramNegInfBoundFolds(t *testing.T) {
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 5, Buckets: []Bucket{
		{UpperBound: math.Inf(-1), Count: 2},
		{UpperBound: 1, Count: 1},
		{UpperBound: 2, Count: 2},
	}}}
	p, _ := newTestProducer(s, Cumulative)

	pt := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if !equalFloats(pt.Bounds, []float64{1, 2}) || !equalUints(pt.BucketCounts, []uint64{3, 2, 0}) || pt.Count != 5 {
		t.Fatalf("bounds = %v, counts = %v, count = %d; want [1 2], [3 2 0], 5", pt.Bounds, pt.BucketCounts, pt.Count)
	}

	// With no finite bucket the -Inf count has nowhere lower to go but the overflow.
	s["lat"] = Sample{Kind: SampleHistogram, Count: 2, Buckets: []Bucket{{UpperBound: math.Inf(-1), Count: 2}}}

	pt = produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if len(pt.Bounds) != 0 || !equalUints(pt.BucketCounts, []uint64{2}) || pt.Count != 2 {
		t.Fatalf("bounds = %v, counts = %v, count = %d; want none, [2], 2", pt.Bounds, pt.BucketCounts, pt.Count)
	}
}

func TestOTLPDeltaBoundsChangeIsReset(t *testing.T) {
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 4, Buckets: []Bucket{{UpperBound: 1, Count: 1}, {UpperBound: 2, Count: 3}}}}
	p, clock := newTestProducer(s, Delta)

	clock.advance(time.Second)
	produce(t, p)

	prev := clock.now()

	clock.advance(time.Minute)

	// Same bucket count, different bounds: the counts are not comparable.
	s["lat"] = Sample{Kind: SampleHistogram, Count: 6, Buckets: []Bucket{{UpperBound: 1, Count: 2}, {UpperBound: 5, Count: 4}}}

	pt := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if !equalFloats(pt.Bounds, []float64{1, 5}) || !equalUints(pt.BucketCounts, []uint64{2, 4, 0}) || pt.Count != 6 {
		t.Fatalf("bounds = %v, counts = %v, count = %d; want the full counts for [1 5]", pt.Bounds, pt.BucketCounts, pt.Count)
	}

	if !pt.StartTime.Equal(prev) {
		t.Fatalf("start = %v, want %v: the bounds change began a new series", pt.StartTime, prev)
	}
}

func TestOTLPMutatingExportedSlicesDoesNotCorruptNextDelta(t *testing.T) {
	s := fakeStream{"lat": {Kind: SampleHistogram, Count: 3, Buckets: []Bucket{{UpperBound: 1, Count: 2}}}}
	p, clock := newTestProducer(s, Delta)

	first := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	for i := range first.BucketCounts {
		first.BucketCounts[i] = 1000
	}

	for i := range first.Bounds {
		first.Bounds[i] = 1000
	}

	clock.advance(time.Minute)

	s["lat"] = Sample{Kind: SampleHistogram, Count: 7, Buckets: []Bucket{{UpperBound: 1, Count: 5}}}

	pt := produce(t, p)["forge_lat"].Data.(metricdata.Histogram[float64]).DataPoints[0]
	if !equalUints(pt.BucketCounts, []uint64{3, 1}) || pt.Count != 4 || !equalFloats(pt.Bounds, []float64{1}) {
		t.Fatalf("bounds = %v, counts = %v, count = %d; want [1], [3 1], 4", pt.Bounds, pt.BucketCounts, pt.Count)
	}
}

func TestOTLPTypedRuntimeGaugeNamedTotalStaysGauge(t *testing.T) {
	reg := prometheus.NewRegistry()
	//nolint:promlinter // a typed gauge with a _total name is the case under test
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "queue_items_total", Help: "A gauge despite the name."})
	reg.MustRegister(g)
	g.Set(7)

	got := produce(t, NewOTLPProducer(nil, OTLPConfig{Runtime: reg}))

	if gauge, ok := got["queue_items_total"].Data.(metricdata.Gauge[float64]); !ok || gauge.DataPoints[0].Value != 7 {
		t.Fatalf("queue_items_total = %+v, want a gauge of 7: the family is typed", got["queue_items_total"].Data)
	}
}

func TestOTLPRuntimeSummaryKeepsQuantilesAndDropsNaN(t *testing.T) {
	reg := prometheus.NewRegistry()
	objectives := map[float64]float64{0.5: 0.05, 0.9: 0.01}
	seen := prometheus.NewSummary(prometheus.SummaryOpts{Name: "rt_seen_seconds", Help: "Observed.", Objectives: objectives})
	empty := prometheus.NewSummary(prometheus.SummaryOpts{Name: "rt_empty_seconds", Help: "No observations.", Objectives: objectives})
	reg.MustRegister(seen, empty)

	for _, v := range []float64{1, 2, 3} {
		seen.Observe(v)
	}

	got := produce(t, NewOTLPProducer(nil, OTLPConfig{Runtime: reg}))

	pt := got["rt_seen_seconds"].Data.(metricdata.Summary).DataPoints[0]
	if pt.Count != 3 || pt.Sum != 6 || len(pt.QuantileValues) != 2 {
		t.Fatalf("seen = %+v, want count 3, sum 6 and two quantiles", pt)
	}

	for _, q := range pt.QuantileValues {
		if math.IsNaN(q.Value) || q.Value < 1 || q.Value > 3 {
			t.Fatalf("quantile %v = %v, want a value from the observations", q.Quantile, q.Value)
		}
	}

	if pt.QuantileValues[0].Quantile != 0.5 || pt.QuantileValues[1].Quantile != 0.9 {
		t.Fatalf("quantiles = %+v, want 0.5 then 0.9", pt.QuantileValues)
	}

	pt = got["rt_empty_seconds"].Data.(metricdata.Summary).DataPoints[0]
	if len(pt.QuantileValues) != 0 {
		t.Fatalf("empty = %+v, want the NaN quantiles dropped", pt.QuantileValues)
	}
}

func TestOTLPRuntimeLabelsBecomeAttributes(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "rt_info", Help: "Labelled."}, []string{"version"})
	reg.MustRegister(g)
	g.WithLabelValues("go1.26").Set(1)

	pt := produce(t, NewOTLPProducer(nil, OTLPConfig{Runtime: reg}))["rt_info"].Data.(metricdata.Gauge[float64]).DataPoints[0]
	if attr(pt.Attributes, "version") != "go1.26" {
		t.Fatalf("attributes = %v, want version=go1.26", pt.Attributes)
	}
}

func TestOTLPDeltaRuntimeCounter(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "rt_total", Help: "Runtime counter."})
	reg.MustRegister(c)

	clock := &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	p := NewOTLPProducer(nil, OTLPConfig{Temporality: Delta, Runtime: reg, Now: clock.now})

	c.Add(5)

	if pt := produce(t, p)["rt_total"].Data.(metricdata.Sum[float64]).DataPoints[0]; pt.Value != 5 {
		t.Fatalf("first = %v, want 5", pt.Value)
	}

	first := clock.now()

	clock.advance(time.Minute)
	c.Add(3)

	pt := produce(t, p)["rt_total"].Data.(metricdata.Sum[float64]).DataPoints[0]
	if pt.Value != 3 || !pt.StartTime.Equal(first) {
		t.Fatalf("second = %+v, want 3 since the first produce", pt)
	}
}
