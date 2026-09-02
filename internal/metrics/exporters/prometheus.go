package exporters

import (
	"bytes"
	"cmp"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
)

// SnapshotFunc returns the current merged metric snapshot. The keys are either
// registry keys (`name{tag="v"}`) or dotted custom-collector keys (`system.cpu`).
//
// Deprecated: this builds the whole snapshot as nested maps before the bridge
// reads any of it. Use StreamFunc, which hands the bridge one Sample at a time.
type SnapshotFunc func() map[string]any

// PrometheusConfig configures the Prometheus bridge.
type PrometheusConfig struct {
	Namespace              string
	EnableGoCollector      bool
	EnableProcessCollector bool
}

// DefaultPrometheusConfig returns the default bridge configuration.
func DefaultPrometheusConfig() PrometheusConfig {
	return PrometheusConfig{
		Namespace:              "forge",
		EnableGoCollector:      true,
		EnableProcessCollector: true,
	}
}

// PrometheusBridge exposes Forge metrics through a client_golang registry.
type PrometheusBridge struct {
	registry  *prometheus.Registry
	collector *forgeCollector
}

// NewPrometheusBridge builds a bridge that reads `snapshot` fresh on each scrape.
//
// Deprecated: prefer NewStreamingPrometheusBridge, which does not materialize
// the snapshot.
func NewPrometheusBridge(snapshot SnapshotFunc, cfg PrometheusConfig) *PrometheusBridge {
	return newBridge(&forgeCollector{snapshot: snapshot, namespace: cfg.Namespace}, cfg)
}

// NewStreamingPrometheusBridge builds a bridge that walks `stream` on each
// scrape, reading one metric at a time instead of materializing them all.
func NewStreamingPrometheusBridge(stream StreamFunc, cfg PrometheusConfig) *PrometheusBridge {
	return newBridge(&forgeCollector{stream: stream, namespace: cfg.Namespace}, cfg)
}

func newBridge(fc *forgeCollector, cfg PrometheusConfig) *PrometheusBridge {
	reg := prometheus.NewRegistry()
	reg.MustRegister(fc)

	if cfg.EnableGoCollector {
		reg.MustRegister(collectors.NewGoCollector())
	}
	if cfg.EnableProcessCollector {
		reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}

	return &PrometheusBridge{registry: reg, collector: fc}
}

// Handler returns an HTTP handler that serves the registry on scrape.
func (b *PrometheusBridge) Handler() http.Handler {
	return promhttp.HandlerFor(b.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// GatherText gathers the registry and encodes it in Prometheus text format.
func (b *PrometheusBridge) GatherText() ([]byte, error) {
	mfs, err := b.registry.Gather()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// =============================================================================
// forgeCollector
// =============================================================================

// forgeCollector adapts a Forge metric snapshot to prometheus.Collector. It is an
// "unchecked" collector: Describe emits nothing so the registry permits metric and
// label sets that vary between scrapes.
type forgeCollector struct {
	snapshot  SnapshotFunc
	stream    StreamFunc
	namespace string

	// descs caches prometheus.Desc by metric name and label keys. A Desc is
	// immutable and building one escapes the name and every label through
	// validation and hashing, which was being paid once per metric on every
	// scrape for descriptors that never change.
	descs sync.Map // descKey -> *prometheus.Desc

	// lastCount remembers how many series the previous scrape saw, so the
	// families map starts at roughly the right size instead of growing through
	// every power of two on each scrape.
	lastCount atomic.Int64
}

// descKey identifies a cached descriptor. Label keys are already sorted and
// joined by the caller, so the struct is comparable and usable as a map key.
type descKey struct {
	fqName string
	kind   string
	labels string
}

// desc returns the cached descriptor for this name and label set, building it
// on first use.
func (c *forgeCollector) desc(fqName, kind string, keys []string) *prometheus.Desc {
	k := descKey{fqName: fqName, kind: kind, labels: strings.Join(keys, "\x1f")}

	if cached, ok := c.descs.Load(k); ok {
		return cached.(*prometheus.Desc)
	}

	built := prometheus.NewDesc(fqName, helpFor(kind, fqName), keys, nil)
	actual, _ := c.descs.LoadOrStore(k, built)

	return actual.(*prometheus.Desc)
}

// Describe implements prometheus.Collector. Intentionally emits no descriptors.
func (c *forgeCollector) Describe(chan<- *prometheus.Desc) {}

type series struct {
	sample Sample
	labels map[string]string
}

// Collect implements prometheus.Collector.
func (c *forgeCollector) Collect(ch chan<- prometheus.Metric) {
	var count int64

	families := make(map[string][]series, c.lastCount.Load()) // fqName -> series

	add := func(key string, sample Sample) bool {
		if sample.Kind == SampleUnknown {
			return true
		}

		name, labels := parseMetricKey(key)
		fqName := buildFQName(c.namespace, name)
		families[fqName] = append(families[fqName], series{sample: sample, labels: labels})
		count++

		return true
	}

	switch {
	case c.stream != nil:
		c.stream(add)
	case c.snapshot != nil:
		for key, value := range c.snapshot() {
			add(key, Sample{Kind: SampleRaw, Raw: value})
		}
	default:
		return
	}

	c.lastCount.Store(count)

	for fqName, list := range families {
		keys := unionKeys(list) // sanitized, sorted union of label keys

		// Last-wins dedup: a later series with the same (fqName, labelValues)
		// signature overwrites an earlier one, preventing duplicate-metric errors
		// from client_golang's Gather while honouring the documented contract.
		unique := make(map[string]series)
		var order []string
		for _, s := range list {
			vals := alignValues(keys, s.labels)
			sig := strings.Join(vals, "\x1f")
			if _, exists := unique[sig]; !exists {
				order = append(order, sig)
			}
			unique[sig] = s // last assignment wins
		}
		for _, sig := range order {
			s := unique[sig]
			vals := alignValues(keys, s.labels)
			c.emit(ch, fqName, keys, vals, s.sample)
		}
	}
}

func (c *forgeCollector) emit(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, sample Sample) {
	switch sample.Kind {
	case SampleCounter:
		c.emitScalar(ch, fqName, keys, vals, prometheus.CounterValue, "counter", sample.Value)
	case SampleGauge:
		// The registry knows this is a gauge, but a name ending in _total is
		// a counter by Prometheus convention and scrapers read it as one.
		vt, kind := scalarValueType(fqName)
		c.emitScalar(ch, fqName, keys, vals, vt, kind, sample.Value)
	case SampleHistogram:
		c.emitHistogram(ch, fqName, keys, vals, sample)
	case SampleTimer:
		c.emitTimer(ch, fqName, keys, vals, sample)
	case SampleRaw:
		c.emitRaw(ch, fqName, keys, vals, sample.Raw)
	}
}

// emitRaw handles a value from a custom collector, which reports untyped maps.
func (c *forgeCollector) emitRaw(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, value any) {
	switch v := value.(type) {
	case float64:
		vt, kind := scalarValueType(fqName)
		c.emitScalar(ch, fqName, keys, vals, vt, kind, v)
	case int64:
		vt, kind := scalarValueType(fqName)
		c.emitScalar(ch, fqName, keys, vals, vt, kind, float64(v))
	case uint64:
		vt, kind := scalarValueType(fqName)
		c.emitScalar(ch, fqName, keys, vals, vt, kind, float64(v))
	case map[string]any:
		c.emitComplex(ch, fqName, keys, vals, v)
	}
}

// scalarValueType returns the Prometheus value type and kind string for a scalar
// metric based on its fully-qualified name. Names ending in "_total" are treated
// as counters per the Prometheus naming convention; all others are gauges.
func scalarValueType(fqName string) (prometheus.ValueType, string) {
	if strings.HasSuffix(fqName, "_total") {
		return prometheus.CounterValue, "counter"
	}
	return prometheus.GaugeValue, "gauge"
}

func (c *forgeCollector) emitScalar(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, vt prometheus.ValueType, kind string, value float64) {
	ch <- prometheus.MustNewConstMetric(c.desc(fqName, kind, keys), vt, value, vals...)
}

// emitComplex reads one of the map shapes a custom collector or the legacy
// snapshot produces and emits it through the typed path.
func (c *forgeCollector) emitComplex(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, v map[string]any) {
	if t, _ := v["_type"].(string); t == "counter" {
		if val, ok := toFloat(v["value"]); ok {
			c.emitScalar(ch, fqName, keys, vals, prometheus.CounterValue, "counter", val)
		}

		return
	}

	if raw, ok := v["buckets"].(map[float64]uint64); ok {
		count, countOK := toUint64(v["count"])
		if !countOK {
			return
		}

		sum, _ := toFloat(v["sum"])

		sample := Sample{Kind: SampleHistogram, Count: count, Sum: sum,
			Buckets: make([]Bucket, 0, len(raw))}
		for bound, n := range raw {
			sample.Buckets = append(sample.Buckets, Bucket{UpperBound: bound, Count: n})
		}

		slices.SortFunc(sample.Buckets, func(a, b Bucket) int {
			return cmp.Compare(a.UpperBound, b.UpperBound)
		})

		c.emitHistogram(ch, fqName, keys, vals, sample)

		return
	}

	if _, ok := v["count"]; ok {
		count, _ := toUint64(v["count"])
		sample := Sample{Kind: SampleTimer, Count: count}

		if mean, ok := durationSeconds(v["mean"]); ok {
			sample.Sum = mean * float64(count)
		}

		sample.P50, _ = durationSeconds(v["p50"])
		sample.P95, _ = durationSeconds(v["p95"])
		sample.P99, _ = durationSeconds(v["p99"])

		c.emitTimer(ch, fqName, keys, vals, sample)

		return
	}
}

func (c *forgeCollector) emitTimer(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, sample Sample) {
	quantiles := map[float64]float64{
		0.5:  sample.P50,
		0.95: sample.P95,
		0.99: sample.P99,
	}

	ch <- prometheus.MustNewConstSummary(c.desc(fqName, "summary", keys),
		sample.Count, sample.Sum, quantiles, vals...)
}

func durationSeconds(v any) (float64, bool) {
	switch n := v.(type) {
	case time.Duration:
		return n.Seconds(), true
	case float64:
		return n, true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// emitHistogram converts per-bucket counts to the cumulative counts Prometheus
// expects. Sample.Buckets arrives sorted by upper bound.
func (c *forgeCollector) emitHistogram(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, sample Sample) {
	cumulative := make(map[float64]uint64, len(sample.Buckets))

	var running uint64
	for _, b := range sample.Buckets {
		running += b.Count
		cumulative[b.UpperBound] = running
	}

	ch <- prometheus.MustNewConstHistogram(c.desc(fqName, "histogram", keys),
		sample.Count, sample.Sum, cumulative, vals...)
}

func toUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case int64:
		if n >= 0 {
			return uint64(n), true
		}
	case float64:
		if n >= 0 {
			return uint64(n), true
		}
	}
	return 0, false
}

// =============================================================================
// helpers
// =============================================================================

// parseMetricKey splits `name{tag="v",...}` into the base name and label map.
// Keys without a brace (dotted custom-collector keys) return no labels.
func parseMetricKey(key string) (string, map[string]string) {
	brace := strings.Index(key, "{")
	if brace == -1 {
		return key, nil
	}

	name := key[:brace]
	tagsStr := strings.TrimSuffix(key[brace+1:], "}")
	labels := make(map[string]string)

	for _, pair := range strings.Split(tagsStr, ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		if k != "" {
			labels[k] = val
		}
	}

	return name, labels
}

// buildFQName joins the namespace and sanitized metric name.
func buildFQName(namespace, name string) string {
	n := sanitizeName(name)
	if namespace == "" {
		return n
	}
	return sanitizeName(namespace) + "_" + n
}

// sanitizeName replaces characters invalid in Prometheus names with underscore.
func sanitizeName(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == ':':
			b.WriteRune(r)
		case r >= '0' && r <= '9' && i > 0:
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// unionKeys returns the sanitized, sorted union of label keys across all series.
func unionKeys(list []series) []string {
	set := make(map[string]struct{})
	for _, s := range list {
		for k := range s.labels {
			set[sanitizeName(k)] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// alignValues returns label values ordered to match keys, "" for absent keys.
func alignValues(keys []string, labels map[string]string) []string {
	sanitized := make(map[string]string, len(labels))
	for k, v := range labels {
		sanitized[sanitizeName(k)] = v
	}
	vals := make([]string, len(keys))
	for i, k := range keys {
		vals[i] = sanitized[k]
	}
	return vals
}

func helpFor(kind, fqName string) string {
	return "Forge " + kind + " " + fqName
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}
