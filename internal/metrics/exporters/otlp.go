package exporters

import (
	"context"
	"encoding/hex"
	"math"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Temporality selects how the OTLP producer reports counters and histograms.
type Temporality uint8

const (
	// Cumulative reports totals since a series started. It is the OTLP default.
	Cumulative Temporality = iota
	// Delta reports the change since the previous export. Datadog wants it.
	Delta
)

// otlpScope is the instrumentation scope every pushed metric carries.
const otlpScope = "github.com/xraph/forge"

// OTLPConfig configures an OTLPProducer.
type OTLPConfig struct {
	// Namespace prefixes names outside the semconv namespaces, exactly as the
	// Prometheus bridge does, so both paths carry the same names.
	Namespace   string
	Temporality Temporality

	// Now reads the clock. Nil means time.Now.
	Now func() time.Time
}

// OTLPProducer reads the registry stream into OTLP metric data. It implements
// sdkmetric.Producer.
//
// A PeriodicReader hands producer output to the exporter as is: it neither
// aggregates it nor converts temporality. The producer therefore keeps what
// each series needs between exports, its start time and, for delta, its
// previous value.
type OTLPProducer struct {
	stream StreamFunc
	cfg    OTLPConfig

	mu      sync.Mutex
	created time.Time
	lastRun time.Time // zero until the first Produce
	series  map[seriesKey]*seriesState
}

type seriesKey struct {
	name  string
	attrs attribute.Distinct
}

// seriesState is what one series carries from one Produce to the next.
type seriesState struct {
	start   time.Time // StartTime of cumulative points
	last    time.Time // Time of the previous point
	seen    bool      // touched by the current Produce
	value   float64   // previous counter value
	count   uint64    // previous histogram or summary count
	sum     float64   // previous histogram sum
	buckets []uint64  // previous histogram bucket counts, overflow last
}

// NewOTLPProducer builds a producer over stream.
func NewOTLPProducer(stream StreamFunc, cfg OTLPConfig) *OTLPProducer {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	return &OTLPProducer{
		stream:  stream,
		cfg:     cfg,
		created: cfg.Now(),
		series:  make(map[seriesKey]*seriesState),
	}
}

// Produce implements sdkmetric.Producer. It never returns an error: the
// reader skips the whole export when a producer fails, and a partial batch is
// worth more than none.
func (p *OTLPProducer) Produce(ctx context.Context) ([]metricdata.ScopeMetrics, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.cfg.Now()
	b := &otlpBatch{p: p, now: now, fams: make(map[string]*otlpFamily)}

	if p.stream != nil {
		p.stream(func(key string, s Sample) bool {
			name, labels := parseMetricKey(key)
			b.add(p.otlpName(name), s.Unit, labels, s)

			return ctx.Err() == nil
		})
	}

	p.prune()
	p.lastRun = now

	return []metricdata.ScopeMetrics{{
		Scope:   instrumentation.Scope{Name: otlpScope},
		Metrics: b.metrics(),
	}}, nil
}

// otlpName is the scrape name in OTel form: a semconv name keeps its dots and
// its unit travels separately, anything else gets the bridge's prefixed name.
func (p *OTLPProducer) otlpName(name string) string {
	if isSemconv(name) {
		return name
	}

	return buildFQName(p.cfg.Namespace, name)
}

// since is the start time of a series first seen, or seen to reset, in this
// Produce: it began some time after the previous one.
func (p *OTLPProducer) since() time.Time {
	if p.lastRun.IsZero() {
		return p.created
	}

	return p.lastRun
}

// state returns the series' state and whether this is its first sighting.
func (p *OTLPProducer) state(key seriesKey) (*seriesState, bool) {
	if st, ok := p.series[key]; ok {
		st.seen = true

		return st, false
	}

	st := &seriesState{start: p.since(), seen: true}
	p.series[key] = st

	return st, true
}

// prune forgets series the stream no longer reports, so label churn can't
// grow the state without bound, and clears the marks for the next Produce.
func (p *OTLPProducer) prune() {
	for k, st := range p.series {
		if !st.seen {
			delete(p.series, k)

			continue
		}

		st.seen = false
	}
}

func (p *OTLPProducer) sumPoint(key seriesKey, attrs attribute.Set, v float64,
	now time.Time) metricdata.DataPoint[float64] {
	st, fresh := p.state(key)
	if !fresh && v < st.value {
		st.start = p.since() // the registry reset this series
	}

	st.value, st.last = v, now

	return metricdata.DataPoint[float64]{Attributes: attrs, StartTime: st.start, Time: now, Value: v}
}

func (p *OTLPProducer) histPoint(key seriesKey, attrs attribute.Set, s Sample,
	now time.Time) metricdata.HistogramDataPoint[float64] {
	bounds, counts, count := otlpBuckets(s)

	st, fresh := p.state(key)
	if !fresh && (count < st.count || len(counts) != len(st.buckets)) {
		st.start = p.since()
	}

	pt := metricdata.HistogramDataPoint[float64]{
		Attributes:   attrs,
		StartTime:    st.start,
		Time:         now,
		Count:        count,
		Sum:          s.Sum,
		Bounds:       bounds,
		BucketCounts: counts,
		Exemplars:    p.exemplars(s.Exemplars),
	}

	st.count, st.sum, st.buckets, st.last = count, s.Sum, counts, now

	return pt
}

func (p *OTLPProducer) summaryPoint(key seriesKey, attrs attribute.Set, count uint64, sum float64,
	quantiles []metricdata.QuantileValue, now time.Time) metricdata.SummaryDataPoint {
	st, fresh := p.state(key)
	if !fresh && count < st.count {
		st.start = p.since()
	}

	st.count, st.last = count, now

	return metricdata.SummaryDataPoint{
		Attributes:     attrs,
		StartTime:      st.start,
		Time:           now,
		Count:          count,
		Sum:            sum,
		QuantileValues: quantiles,
	}
}

// exemplars converts the exemplars recorded since the previous Produce.
// Sending the same exemplar every interval only makes the backend drop the
// copy. An ID that isn't hex of the right length is dropped.
func (p *OTLPProducer) exemplars(in []Exemplar) []metricdata.Exemplar[float64] {
	var out []metricdata.Exemplar[float64]

	for _, e := range in {
		if !p.lastRun.IsZero() && !e.Timestamp.After(p.lastRun) {
			continue
		}

		traceID, err := hex.DecodeString(e.TraceID)
		if err != nil || len(traceID) != 16 {
			continue
		}

		spanID, err := hex.DecodeString(e.SpanID)
		if err != nil || len(spanID) != 8 {
			continue
		}

		out = append(out, metricdata.Exemplar[float64]{
			Time: e.Timestamp, Value: e.Value, TraceID: traceID, SpanID: spanID,
		})
	}

	return out
}

// otlpBuckets turns a Sample's buckets into OTLP bounds and counts. OTLP
// wants one more count than bounds, for observations above the last bound.
// go-utils keeps that overflow out of Buckets, so it is the total count less
// the bucketed ones. The registry reads the count before the buckets, so an
// observation landing between the two reads can leave the buckets ahead of
// the count; the count then rises to match instead of the overflow wrapping.
// An explicit +Inf bound folds into the overflow, since OTLP bounds are
// finite.
func otlpBuckets(s Sample) ([]float64, []uint64, uint64) {
	bounds := make([]float64, 0, len(s.Buckets))
	counts := make([]uint64, 0, len(s.Buckets)+1)

	var bucketed, inf uint64

	for _, b := range s.Buckets {
		if math.IsInf(b.UpperBound, 1) || math.IsNaN(b.UpperBound) {
			inf += b.Count

			continue
		}

		bounds = append(bounds, b.UpperBound)
		counts = append(counts, b.Count)
		bucketed += b.Count
	}

	overflow := inf
	if s.Count > bucketed && s.Count-bucketed > overflow {
		overflow = s.Count - bucketed
	}

	return bounds, append(counts, overflow), bucketed + overflow
}

func timerQuantiles(s Sample) []metricdata.QuantileValue {
	return []metricdata.QuantileValue{
		{Quantile: 0.5, Value: s.P50},
		{Quantile: 0.95, Value: s.P95},
		{Quantile: 0.99, Value: s.P99},
	}
}

func attributeSet(labels map[string]string) attribute.Set {
	kvs := make([]attribute.KeyValue, 0, len(labels))
	for k, v := range labels {
		kvs = append(kvs, attribute.String(k, v))
	}

	return attribute.NewSet(kvs...)
}

// otlpBatch gathers one Produce's metrics by name, in first-seen order.
type otlpBatch struct {
	p     *OTLPProducer
	now   time.Time
	order []string
	fams  map[string]*otlpFamily
}

// otlpFamily holds one name's points. kind is SampleCounter for a monotonic
// sum, SampleGauge, SampleHistogram or SampleTimer for a summary.
type otlpFamily struct {
	name, unit string
	kind       SampleKind
	index      map[attribute.Distinct]int
	points     []metricdata.DataPoint[float64]
	hists      []metricdata.HistogramDataPoint[float64]
	summaries  []metricdata.SummaryDataPoint
}

// family returns the family for name, or nil when name already holds another
// kind; the first kind seen keeps the name.
func (b *otlpBatch) family(name, unit string, kind SampleKind) *otlpFamily {
	if f, ok := b.fams[name]; ok {
		if f.kind != kind {
			return nil
		}

		return f
	}

	f := &otlpFamily{name: name, unit: unit, kind: kind, index: make(map[attribute.Distinct]int)}
	b.fams[name] = f
	b.order = append(b.order, name)

	return f
}

func (b *otlpBatch) add(name, unit string, labels map[string]string, s Sample) {
	if s.Kind == SampleRaw {
		raw, ok := rawSample(s.Raw)
		if !ok {
			return
		}

		s = raw
	}

	kind := s.Kind
	if kind == SampleGauge && strings.HasSuffix(name, "_total") {
		kind = SampleCounter // the bridge's rule: scrapers read _total as a counter
	}

	if kind == SampleUnknown || kind == SampleRaw {
		return
	}

	f := b.family(name, unit, kind)
	if f == nil {
		return
	}

	attrs := attributeSet(labels)
	key := seriesKey{name: name, attrs: attrs.Equivalent()}

	switch kind {
	case SampleCounter:
		f.putPoint(attrs, b.p.sumPoint(key, attrs, s.Value, b.now))
	case SampleGauge:
		f.putPoint(attrs, metricdata.DataPoint[float64]{Attributes: attrs, Time: b.now, Value: s.Value})
	case SampleHistogram:
		f.putHist(attrs, b.p.histPoint(key, attrs, s, b.now))
	case SampleTimer:
		f.putSummary(attrs, b.p.summaryPoint(key, attrs, s.Count, s.Sum, timerQuantiles(s), b.now))
	}
}

// putPoint and its siblings keep the last point for an attribute set, as the
// bridge keeps the last series for a label set: two registry names can
// sanitize to the same exported name.
func (f *otlpFamily) putPoint(attrs attribute.Set, pt metricdata.DataPoint[float64]) {
	if i, ok := f.index[attrs.Equivalent()]; ok {
		f.points[i] = pt

		return
	}

	f.index[attrs.Equivalent()] = len(f.points)
	f.points = append(f.points, pt)
}

func (f *otlpFamily) putHist(attrs attribute.Set, pt metricdata.HistogramDataPoint[float64]) {
	if i, ok := f.index[attrs.Equivalent()]; ok {
		f.hists[i] = pt

		return
	}

	f.index[attrs.Equivalent()] = len(f.hists)
	f.hists = append(f.hists, pt)
}

func (f *otlpFamily) putSummary(attrs attribute.Set, pt metricdata.SummaryDataPoint) {
	if i, ok := f.index[attrs.Equivalent()]; ok {
		f.summaries[i] = pt

		return
	}

	f.index[attrs.Equivalent()] = len(f.summaries)
	f.summaries = append(f.summaries, pt)
}

func (b *otlpBatch) metrics() []metricdata.Metrics {
	temporality := metricdata.CumulativeTemporality
	if b.p.cfg.Temporality == Delta {
		temporality = metricdata.DeltaTemporality
	}

	out := make([]metricdata.Metrics, 0, len(b.order))

	for _, name := range b.order {
		f := b.fams[name]
		m := metricdata.Metrics{Name: f.name, Unit: f.unit}

		switch f.kind {
		case SampleCounter:
			m.Data = metricdata.Sum[float64]{DataPoints: f.points, Temporality: temporality, IsMonotonic: true}
		case SampleGauge:
			m.Data = metricdata.Gauge[float64]{DataPoints: f.points}
		case SampleHistogram:
			m.Data = metricdata.Histogram[float64]{DataPoints: f.hists, Temporality: temporality}
		case SampleTimer:
			m.Data = metricdata.Summary{DataPoints: f.summaries}
		}

		out = append(out, m)
	}

	return out
}
