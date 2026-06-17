package exporters

import (
	"bytes"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
)

// SnapshotFunc returns the current merged metric snapshot. The keys are either
// registry keys (`name{tag="v"}`) or dotted custom-collector keys (`system.cpu`).
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
func NewPrometheusBridge(snapshot SnapshotFunc, cfg PrometheusConfig) *PrometheusBridge {
	fc := &forgeCollector{snapshot: snapshot, namespace: cfg.Namespace}
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
	namespace string
}

// Describe implements prometheus.Collector. Intentionally emits no descriptors.
func (c *forgeCollector) Describe(chan<- *prometheus.Desc) {}

type series struct {
	value  any
	labels map[string]string
}

// Collect implements prometheus.Collector.
func (c *forgeCollector) Collect(ch chan<- prometheus.Metric) {
	if c.snapshot == nil {
		return
	}

	families := make(map[string][]series) // fqName -> series
	for key, value := range c.snapshot() {
		name, labels := parseMetricKey(key)
		fqName := buildFQName(c.namespace, name)
		families[fqName] = append(families[fqName], series{value: value, labels: labels})
	}

	for fqName, list := range families {
		keys := unionKeys(list)       // sanitized, sorted union of label keys
		seen := make(map[string]bool) // dedup by joined label values
		for _, s := range list {
			vals := alignValues(keys, s.labels)
			sig := strings.Join(vals, "\x1f")
			if seen[sig] {
				continue
			}
			seen[sig] = true
			c.emit(ch, fqName, keys, vals, s.value)
		}
	}
}

func (c *forgeCollector) emit(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, value any) {
	switch v := value.(type) {
	case float64:
		c.emitScalar(ch, fqName, keys, vals, prometheus.GaugeValue, "gauge", v)
	case int64:
		c.emitScalar(ch, fqName, keys, vals, prometheus.GaugeValue, "gauge", float64(v))
	case uint64:
		c.emitScalar(ch, fqName, keys, vals, prometheus.GaugeValue, "gauge", float64(v))
	case map[string]any:
		c.emitComplex(ch, fqName, keys, vals, v)
	}
}

func (c *forgeCollector) emitScalar(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, vt prometheus.ValueType, kind string, value float64) {
	desc := prometheus.NewDesc(fqName, helpFor(kind, fqName), keys, nil)
	ch <- prometheus.MustNewConstMetric(desc, vt, value, vals...)
}

func (c *forgeCollector) emitComplex(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, v map[string]any) {
	if t, _ := v["_type"].(string); t == "counter" {
		if val, ok := toFloat(v["value"]); ok {
			c.emitScalar(ch, fqName, keys, vals, prometheus.CounterValue, "counter", val)
		}
		return
	}

	if raw, ok := v["buckets"].(map[float64]uint64); ok {
		c.emitHistogram(ch, fqName, keys, vals, v, raw)
		return
	}

	if _, ok := v["count"]; ok {
		c.emitTimer(ch, fqName, keys, vals, v)
		return
	}
}

func (c *forgeCollector) emitTimer(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, v map[string]any) {
	count, _ := toUint64(v["count"])

	quantiles := make(map[float64]float64)
	for label, q := range map[string]float64{"p50": 0.5, "p95": 0.95, "p99": 0.99} {
		if s, ok := durationSeconds(v[label]); ok {
			quantiles[q] = s
		}
	}

	var sum float64
	if mean, ok := durationSeconds(v["mean"]); ok {
		sum = mean * float64(count)
	}

	desc := prometheus.NewDesc(fqName, helpFor("summary", fqName), keys, nil)
	ch <- prometheus.MustNewConstSummary(desc, count, sum, quantiles, vals...)
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

func (c *forgeCollector) emitHistogram(ch chan<- prometheus.Metric, fqName string,
	keys, vals []string, v map[string]any, perBucket map[float64]uint64) {
	bounds := make([]float64, 0, len(perBucket))
	for b := range perBucket {
		bounds = append(bounds, b)
	}
	sort.Float64s(bounds)

	cumulative := make(map[float64]uint64, len(bounds))
	var running uint64
	for _, b := range bounds {
		running += perBucket[b]
		cumulative[b] = running
	}

	count, countOK := toUint64(v["count"])
	if !countOK {
		return
	}
	sum, _ := toFloat(v["sum"])

	desc := prometheus.NewDesc(fqName, helpFor("histogram", fqName), keys, nil)
	ch <- prometheus.MustNewConstHistogram(desc, count, sum, cumulative, vals...)
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
