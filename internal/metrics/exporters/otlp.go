package exporters

import (
	"cmp"
	"context"
	"encoding/hex"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
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

	// Runtime holds the Go and process collectors the scrape also serves.
	// Nil leaves them out.
	Runtime prometheus.Gatherer

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
	owner   string    // original registry key whose samples feed this series
	value   float64   // previous counter value
	count   uint64    // previous histogram or summary count
	sum     float64   // previous histogram sum
	bounds  []float64 // previous histogram bounds
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
	b := &otlpBatch{
		p: p, now: now,
		pending: make([]otlpEntry, 0, len(p.series)+16),
		fams:    make(map[string]*otlpFamily),
	}

	complete := true

	if p.stream != nil {
		p.stream(func(key string, s Sample) bool {
			name, labels := parseMetricKey(key)
			b.queue(key, p.otlpName(name), s.Unit, labels, s, false)

			if ctx.Err() != nil {
				complete = false

				return false
			}

			return true
		})
	}

	// A cancelled context after a full walk still counts as cut short, so the
	// runtime families are skipped too and no series gets pruned.
	if complete && ctx.Err() != nil {
		complete = false
	}

	if complete && p.cfg.Runtime != nil {
		b.addGathered(p.cfg.Runtime)
	}

	// Nothing has touched series state yet: flush settles name collisions first.
	b.flush(complete)

	// A walk cut short didn't see every series, so an unvisited one isn't gone.
	// Pruning it, or moving lastRun, would restart its start time and look to
	// the backend like a counter reset.
	if complete {
		p.prune()
		p.lastRun = now
	} else {
		p.clearSeen()
	}

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

// clearSeen unmarks every series without forgetting any.
func (p *OTLPProducer) clearSeen() {
	for _, st := range p.series {
		st.seen = false
	}
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

	reset := !fresh && v < st.value // the registry reset this series
	if reset {
		st.start = p.since()
	}

	pt := metricdata.DataPoint[float64]{Attributes: attrs, StartTime: st.start, Time: now, Value: v}
	if p.cfg.Temporality == Delta && !fresh && !reset {
		pt.StartTime, pt.Value = st.last, v-st.value
	}

	st.value, st.last = v, now

	return pt
}

func (p *OTLPProducer) histPoint(key seriesKey, attrs attribute.Set, s Sample,
	now time.Time) metricdata.HistogramDataPoint[float64] {
	bounds, counts, count := otlpBuckets(s)

	st, fresh := p.state(key)

	sameBounds := slices.Equal(bounds, st.bounds)

	reset := !fresh && (count < st.count || !sameBounds)
	if reset {
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

	// The state keeps copies of what it needs: the point's slices belong to the
	// consumer, and a write through them must not reach the next delta.
	if !sameBounds {
		st.bounds = slices.Clone(bounds)
	}

	if p.cfg.Temporality == Delta && !fresh && !reset {
		// The bounds match, so the state's counts line up with these.
		deltas := make([]uint64, len(counts))

		var total uint64

		for i, c := range counts {
			switch prev := st.buckets[i]; {
			case c > prev:
				deltas[i] = c - prev
				st.buckets[i] = c
			case c < prev:
				// The registry reads a histogram's count before its buckets, so a
				// derived overflow bucket can read one low. Report nothing for it and
				// keep the old baseline, or the next push reports an observation that
				// never happened.
			}

			total += deltas[i]
		}

		pt.StartTime, pt.Count, pt.Sum, pt.BucketCounts = st.last, total, s.Sum-st.sum, deltas
	} else {
		st.buckets = append(st.buckets[:0], counts...)
	}

	st.count, st.sum, st.last = count, s.Sum, now

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
// finite. A -Inf bound folds into the first finite bucket, or into the
// overflow when there is none.
func otlpBuckets(s Sample) ([]float64, []uint64, uint64) {
	bounds := make([]float64, 0, len(s.Buckets))
	counts := make([]uint64, 0, len(s.Buckets)+1)

	var bucketed, inf, negInf uint64

	for _, b := range s.Buckets {
		if math.IsInf(b.UpperBound, 1) || math.IsNaN(b.UpperBound) {
			inf += b.Count

			continue
		}

		if math.IsInf(b.UpperBound, -1) {
			negInf += b.Count

			continue
		}

		bounds = append(bounds, b.UpperBound)
		counts = append(counts, b.Count)
		bucketed += b.Count
	}

	if len(counts) > 0 {
		counts[0] += negInf
		bucketed += negInf
	} else {
		inf += negInf
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

// otlpBatch gathers one Produce's metrics by name, in the order of the keys
// they came from.
type otlpBatch struct {
	p       *OTLPProducer
	now     time.Time
	pending []otlpEntry
	order   []string
	fams    map[string]*otlpFamily
}

// otlpEntry is one sample waiting to become a point. Its kind and series are
// settled, and no series state has been touched.
type otlpEntry struct {
	origKey    string // the registry key, or a runtime family's name
	name, unit string
	kind       SampleKind
	attrs      attribute.Set
	key        seriesKey
	sample     Sample

	// quantiles are a runtime summary's own, flagged by ownQuantiles because a
	// summary with every quantile NaN has none left. A timer reports its three.
	quantiles    []metricdata.QuantileValue
	ownQuantiles bool
}

// otlpFamily holds one name's points. kind is SampleCounter for a monotonic
// sum, SampleGauge, SampleHistogram or SampleTimer for a summary.
type otlpFamily struct {
	name, unit string
	kind       SampleKind
	seen       map[attribute.Distinct]struct{}
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

	f := &otlpFamily{name: name, unit: unit, kind: kind, seen: make(map[attribute.Distinct]struct{})}
	b.fams[name] = f
	b.order = append(b.order, name)

	return f
}

// queue settles what one sample is and holds it for flush. typed says the
// sample's kind is declared, as a client_golang family's is, rather than
// guessed from a registry value.
func (b *otlpBatch) queue(origKey, name, unit string, labels map[string]string, s Sample, typed bool) {
	if s.Kind == SampleRaw {
		raw, ok := rawSample(s.Raw)
		if !ok {
			return
		}

		s = raw
	}

	kind := s.Kind
	if !typed && kind == SampleGauge && strings.HasSuffix(name, "_total") {
		kind = SampleCounter // the bridge's rule: scrapers read _total as a counter
	}

	if kind == SampleUnknown || kind == SampleRaw {
		return
	}

	attrs := attributeSet(labels)

	b.pending = append(b.pending, otlpEntry{
		origKey: origKey, name: name, unit: unit, kind: kind, attrs: attrs,
		key: seriesKey{name: name, attrs: attrs.Equivalent()}, sample: s,
	})
}

// flush turns the queued samples into points. Two registry names can sanitize
// to the same exported name and so to the same series, and the registry hands
// them over in a different order every time. Walking the entries by original
// key and keeping the first one per attribute set makes the lowest key win
// whenever every sample is present, so a full walk never flips a series between
// two values and fakes a reset.
//
// A walk cut short may reach only the loser. A series remembers the key that
// last fed it, and a cut-short flush skips an entry from any other key, so the
// loser can't write its value into the state the winner's next delta is
// measured against.
func (b *otlpBatch) flush(complete bool) {
	idx := make([]int, len(b.pending))
	for i := range idx {
		idx[i] = i
	}

	slices.SortFunc(idx, func(i, j int) int {
		return cmp.Or(strings.Compare(b.pending[i].origKey, b.pending[j].origKey), cmp.Compare(i, j))
	})

	for _, i := range idx {
		b.add(&b.pending[i], complete)
	}

	b.pending = nil
}

func (b *otlpBatch) add(e *otlpEntry, complete bool) {
	if st, ok := b.p.series[e.key]; ok && !complete && st.owner != e.origKey {
		return
	}

	f := b.family(e.name, e.unit, e.kind)
	if f == nil {
		return
	}

	if _, taken := f.seen[e.key.attrs]; taken {
		return // a lower key already supplied this series
	}

	f.seen[e.key.attrs] = struct{}{}

	switch e.kind {
	case SampleCounter:
		f.points = append(f.points, b.p.sumPoint(e.key, e.attrs, e.sample.Value, b.now))
	case SampleGauge:
		f.points = append(f.points, metricdata.DataPoint[float64]{Attributes: e.attrs, Time: b.now, Value: e.sample.Value})
	case SampleHistogram:
		f.hists = append(f.hists, b.p.histPoint(e.key, e.attrs, e.sample, b.now))
	case SampleTimer:
		quantiles := e.quantiles
		if !e.ownQuantiles {
			quantiles = timerQuantiles(e.sample)
		}

		f.summaries = append(f.summaries, b.p.summaryPoint(e.key, e.attrs, e.sample.Count, e.sample.Sum, quantiles, b.now))
	case SampleUnknown, SampleRaw:
		// queue never lets these through.
	}

	if st, ok := b.p.series[e.key]; ok {
		st.owner = e.origKey
	}
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

// addGathered queues the Go and process collectors' families. They keep their
// Prometheus names, as on the scrape: no namespace prefix and no unit, because
// the name already carries it. It runs under the producer's mutex, so a slow
// collector delays this Produce and any other call waiting on the lock.
func (b *otlpBatch) addGathered(g prometheus.Gatherer) {
	mfs, _ := g.Gather() // a partial gather is still worth sending

	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			labels := make(map[string]string, len(m.GetLabel()))
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}

			b.addDTO(mf.GetName(), mf.GetType(), m, labels)
		}
	}
}

func (b *otlpBatch) addDTO(name string, typ dto.MetricType, m *dto.Metric, labels map[string]string) {
	switch typ {
	case dto.MetricType_COUNTER:
		b.queue(name, name, "", labels, Sample{Kind: SampleCounter, Value: m.GetCounter().GetValue()}, true)
	case dto.MetricType_GAUGE:
		b.queue(name, name, "", labels, Sample{Kind: SampleGauge, Value: m.GetGauge().GetValue()}, true)
	case dto.MetricType_UNTYPED:
		b.queue(name, name, "", labels, Sample{Kind: SampleGauge, Value: m.GetUntyped().GetValue()}, true)
	case dto.MetricType_HISTOGRAM:
		b.queue(name, name, "", labels, dtoHistogram(m.GetHistogram()), true)
	case dto.MetricType_SUMMARY:
		b.addSummary(name, labels, m.GetSummary())
	case dto.MetricType_GAUGE_HISTOGRAM:
		// No OTLP equivalent, and the Go and process collectors never emit one.
	}
}

// dtoHistogram turns client_golang's cumulative buckets into the per-bucket
// counts a Sample holds.
func dtoHistogram(h *dto.Histogram) Sample {
	s := Sample{
		Kind:    SampleHistogram,
		Count:   h.GetSampleCount(),
		Sum:     h.GetSampleSum(),
		Buckets: make([]Bucket, 0, len(h.GetBucket())),
	}

	var prev uint64

	for _, bk := range h.GetBucket() {
		cum := bk.GetCumulativeCount()

		var own uint64
		if cum > prev {
			own = cum - prev
		}

		s.Buckets = append(s.Buckets, Bucket{UpperBound: bk.GetUpperBound(), Count: own})
		prev = cum
	}

	return s
}

// addSummary queues a client_golang summary with its own quantiles, which
// differ from a timer's three.
func (b *otlpBatch) addSummary(name string, labels map[string]string, s *dto.Summary) {
	qs := make([]metricdata.QuantileValue, 0, len(s.GetQuantile()))

	for _, q := range s.GetQuantile() {
		if math.IsNaN(q.GetValue()) {
			continue
		}

		qs = append(qs, metricdata.QuantileValue{Quantile: q.GetQuantile(), Value: q.GetValue()})
	}

	attrs := attributeSet(labels)

	b.pending = append(b.pending, otlpEntry{
		origKey: name, name: name, kind: SampleTimer, attrs: attrs,
		key:       seriesKey{name: name, attrs: attrs.Equivalent()},
		sample:    Sample{Kind: SampleTimer, Count: s.GetSampleCount(), Sum: s.GetSampleSum()},
		quantiles: qs, ownQuantiles: true,
	})
}
