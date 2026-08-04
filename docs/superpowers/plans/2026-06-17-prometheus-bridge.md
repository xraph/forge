# Prometheus Bridge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Forge's hand-rolled Prometheus text serializer with a `client_golang`-backed bridge that reads the live merged metric snapshot on each scrape and serves correct exposition at `/_/metrics`.

**Architecture:** A `PrometheusBridge` owns a `prometheus.Registry` holding a custom unchecked `prometheus.Collector` (reads `collector.GetMetrics() map[string]any` per scrape and emits const-metrics) plus the standard Go/Process collectors. The bridge takes a plain `func() map[string]any` so it imports neither the registry nor the app. The collector routes `Export(ExportFormatPrometheus)` to the bridge and exposes `PrometheusHandler()` (via `shared.PrometheusProvider`) so the app mounts `promhttp` at `/_/metrics`.

**Tech Stack:** Go; `github.com/prometheus/client_golang` (prometheus, prometheus/collectors, prometheus/promhttp, prometheus/testutil); `github.com/prometheus/common/expfmt`.

> **API note (verified against the pinned versions):** in `common v0.48.0` the `expfmt` format constants are unexported — use `expfmt.NewFormat(expfmt.TypeTextPlain)`, NOT `expfmt.FmtText`. `collectors.NewGoCollector()` and `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})` exist in `client_golang v1.19.1`.

## Global Constraints

- Module path: `github.com/xraph/forge`.
- Dependencies already present (do NOT add): `github.com/prometheus/client_golang v1.19.1`, `github.com/prometheus/common v0.48.0`.
- `client_golang` may be imported ONLY from `internal/metrics/exporters/prometheus.go` (and its test). The core `internal/metrics` / `internal/shared` packages must not import it.
- Git commit messages: NEVER add `Co-Authored-By` trailers.
- Run tests with `go test ./internal/metrics/...` from repo root.
- Branch: `feat/metrics-prometheus-bridge` (already created).
- Every metric value shape comes from `internal/metrics/registry.go` `RegisteredMetric.GetValue()` (lines 75-112) and `CustomCollector.Collect()` maps:
  - counter → `map[string]any{"value": float64, "_type": "counter"}`
  - gauge → bare `float64`
  - histogram → `map[string]any{"count": uint64, "sum": float64, "mean": float64, "buckets": map[float64]uint64}` (buckets are **per-bucket / non-cumulative**)
  - timer → `map[string]any{"count": uint64, "mean": time.Duration, "min": time.Duration, "max": time.Duration, "p50": time.Duration, "p95": time.Duration, "p99": time.Duration}` (NO `sum` field)
  - custom-collector scalars → `float64` / `int64` / `uint64`
  - registry keys are formatted `name{tag1="v1",tag2="v2"}`; custom-collector keys are dotted (e.g. `system.cpu.usage`).

---

## File Structure

- `internal/metrics/exporters/prometheus.go` — **rewritten**. `PrometheusConfig`, `PrometheusBridge`, `forgeCollector`, helpers. Only file importing `client_golang`.
- `internal/metrics/exporters/prometheus_test.go` — **rewritten**. Bridge unit tests via `testutil`.
- `internal/shared/metrics.go` — add `PrometheusProvider` interface.
- `internal/metrics/collector.go` — wire bridge; route `Export`; add `PrometheusHandler`; drop Forge runtime collector; remove dead push path.
- `app_impl.go` — mount `promhttp` at `/_/metrics`.
- `app_impl_test.go` (or new `metrics_endpoint_test.go`) — endpoint test.
- `examples/observability/` — Grafana dashboard, `prometheus.yml`, `ServiceMonitor`, README.
- `internal/observability/prometheus.go` + `extensions/dashboard/collector/trace_exporter.go` — retire/repoint (final task).

---

### Task 1: Bridge skeleton — config, registry, counter & gauge mapping

**Files:**
- Modify (rewrite): `internal/metrics/exporters/prometheus.go`
- Test: `internal/metrics/exporters/prometheus_test.go`

**Interfaces:**
- Produces:
  - `type SnapshotFunc func() map[string]any`
  - `type PrometheusConfig struct { Namespace string; EnableGoCollector bool; EnableProcessCollector bool }`
  - `func DefaultPrometheusConfig() PrometheusConfig`
  - `func NewPrometheusBridge(snapshot SnapshotFunc, cfg PrometheusConfig) *PrometheusBridge`
  - `func (b *PrometheusBridge) Handler() http.Handler`
  - `func (b *PrometheusBridge) GatherText() ([]byte, error)`

- [ ] **Step 1: Write the failing test**

Replace the entire contents of `internal/metrics/exporters/prometheus_test.go` with:

```go
package exporters

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBridge_CounterAndGauge(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			`http_requests_total{method="GET"}`: map[string]any{"value": float64(5), "_type": "counter"},
			"queue_depth":                       float64(3),
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{Namespace: ""})

	expected := `
# HELP http_requests_total Forge counter http_requests_total
# TYPE http_requests_total counter
http_requests_total{method="GET"} 5
# HELP queue_depth Forge gauge queue_depth
# TYPE queue_depth gauge
queue_depth 3
`
	if err := testutil.CollectAndCompare(b.collector, strings.NewReader(expected),
		"http_requests_total", "queue_depth"); err != nil {
		t.Fatalf("unexpected exposition: %v", err)
	}
}

func TestBridge_GatherTextHasNoTimestamps(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{"queue_depth": float64(3)}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{})
	out, err := b.GatherText()
	if err != nil {
		t.Fatalf("GatherText: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "queue_depth") {
			// "queue_depth 3" -> exactly 2 fields; a trailing timestamp would make 3.
			if fields := strings.Fields(line); len(fields) != 2 {
				t.Fatalf("expected no timestamp, got %q", line)
			}
		}
	}
}
```

(`b.collector` is the unexported `*forgeCollector`; the test is in-package so it can reach it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/exporters/ -run TestBridge -v`
Expected: compile failure — `NewPrometheusBridge`, `PrometheusConfig`, etc. undefined.

- [ ] **Step 3: Write minimal implementation**

Replace the entire contents of `internal/metrics/exporters/prometheus.go` with:

```go
package exporters

import (
	"bytes"
	"net/http"
	"sort"
	"strings"

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

// Collect implements prometheus.Collector.
func (c *forgeCollector) Collect(ch chan<- prometheus.Metric) {
	if c.snapshot == nil {
		return
	}

	for key, value := range c.snapshot() {
		name, labels := parseMetricKey(key)
		fqName := buildFQName(c.namespace, name)

		switch v := value.(type) {
		case float64:
			c.emitScalar(ch, fqName, prometheus.GaugeValue, "gauge", v, labels)
		case int64:
			c.emitScalar(ch, fqName, prometheus.GaugeValue, "gauge", float64(v), labels)
		case uint64:
			c.emitScalar(ch, fqName, prometheus.GaugeValue, "gauge", float64(v), labels)
		case map[string]any:
			c.emitComplex(ch, fqName, v, labels)
		}
		// Unknown shapes are skipped.
	}
}

func (c *forgeCollector) emitScalar(ch chan<- prometheus.Metric, fqName string,
	vt prometheus.ValueType, kind string, value float64, labels map[string]string) {
	keys, vals := sortedLabels(labels)
	desc := prometheus.NewDesc(fqName, helpFor(kind, fqName), keys, nil)
	ch <- prometheus.MustNewConstMetric(desc, vt, value, vals...)
}

func (c *forgeCollector) emitComplex(ch chan<- prometheus.Metric, fqName string,
	v map[string]any, labels map[string]string) {
	if t, _ := v["_type"].(string); t == "counter" {
		if val, ok := toFloat(v["value"]); ok {
			c.emitScalar(ch, fqName, prometheus.CounterValue, "counter", val, labels)
		}
		return
	}
	// Histogram / timer mapping is added in later tasks.
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

// sortedLabels returns label keys (sanitized, sorted) and values in matching order.
func sortedLabels(labels map[string]string) ([]string, []string) {
	if len(labels) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	outKeys := make([]string, len(keys))
	outVals := make([]string, len(keys))
	for i, k := range keys {
		outKeys[i] = sanitizeName(k)
		outVals[i] = labels[k]
	}
	return outKeys, outVals
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics/exporters/ -run TestBridge -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/exporters/prometheus.go internal/metrics/exporters/prometheus_test.go
git commit -m "feat(metrics): client_golang prometheus bridge with counter/gauge mapping"
```

---

### Task 2: Histogram mapping (per-bucket → cumulative)

**Files:**
- Modify: `internal/metrics/exporters/prometheus.go` (extend `emitComplex`, add `emitHistogram`)
- Test: `internal/metrics/exporters/prometheus_test.go`

**Interfaces:**
- Consumes: `forgeCollector`, `emitComplex`, `sortedLabels`, `helpFor` (Task 1)
- Produces: histogram exposition (`_bucket`/`_sum`/`_count`) from a snapshot map entry with `buckets`/`count`/`sum`.

- [ ] **Step 1: Write the failing test**

Append to `internal/metrics/exporters/prometheus_test.go`:

```go
func TestBridge_Histogram(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			"request_latency_seconds": map[string]any{
				"count": uint64(6),
				"sum":   float64(7.5),
				"buckets": map[float64]uint64{ // per-bucket (non-cumulative)
					0.1: 1,
					0.5: 2,
					1.0: 3,
				},
			},
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{})

	expected := `
# HELP request_latency_seconds Forge histogram request_latency_seconds
# TYPE request_latency_seconds histogram
request_latency_seconds_bucket{le="0.1"} 1
request_latency_seconds_bucket{le="0.5"} 3
request_latency_seconds_bucket{le="1"} 6
request_latency_seconds_bucket{le="+Inf"} 6
request_latency_seconds_sum 7.5
request_latency_seconds_count 6
`
	if err := testutil.CollectAndCompare(b.collector, strings.NewReader(expected),
		"request_latency_seconds"); err != nil {
		t.Fatalf("unexpected histogram exposition: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/exporters/ -run TestBridge_Histogram -v`
Expected: FAIL — no histogram output produced.

- [ ] **Step 3: Write minimal implementation**

In `internal/metrics/exporters/prometheus.go`, replace the `emitComplex` body's trailing comment with histogram handling, and add `emitHistogram` + `toUint64`:

```go
func (c *forgeCollector) emitComplex(ch chan<- prometheus.Metric, fqName string,
	v map[string]any, labels map[string]string) {
	if t, _ := v["_type"].(string); t == "counter" {
		if val, ok := toFloat(v["value"]); ok {
			c.emitScalar(ch, fqName, prometheus.CounterValue, "counter", val, labels)
		}
		return
	}

	if raw, ok := v["buckets"].(map[float64]uint64); ok {
		c.emitHistogram(ch, fqName, v, raw, labels)
		return
	}
	// Timer / summary mapping is added in the next task.
}

func (c *forgeCollector) emitHistogram(ch chan<- prometheus.Metric, fqName string,
	v map[string]any, perBucket map[float64]uint64, labels map[string]string) {
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

	count, _ := toUint64(v["count"])
	sum, _ := toFloat(v["sum"])

	keys, vals := sortedLabels(labels)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics/exporters/ -run TestBridge -v`
Expected: PASS (all bridge tests).

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/exporters/prometheus.go internal/metrics/exporters/prometheus_test.go
git commit -m "feat(metrics): map forge histograms to cumulative prometheus buckets"
```

---

### Task 3: Timer → summary mapping

**Files:**
- Modify: `internal/metrics/exporters/prometheus.go` (extend `emitComplex`, add `emitTimer`)
- Test: `internal/metrics/exporters/prometheus_test.go`

**Interfaces:**
- Consumes: `forgeCollector`, `emitComplex`, `sortedLabels` (Tasks 1-2)
- Produces: summary exposition (quantiles + `_sum` + `_count`) for timer map entries (`count` + `p50/p95/p99`, values `time.Duration`, no `sum`/`buckets`).

- [ ] **Step 1: Write the failing test**

Append to `internal/metrics/exporters/prometheus_test.go` (add `"time"` to imports):

```go
func TestBridge_Timer(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			"op_duration_seconds": map[string]any{
				"count": uint64(10),
				"mean":  200 * time.Millisecond,
				"p50":   150 * time.Millisecond,
				"p95":   400 * time.Millisecond,
				"p99":   900 * time.Millisecond,
			},
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{})

	// sum = mean.Seconds() * count = 0.2 * 10 = 2
	expected := `
# HELP op_duration_seconds Forge summary op_duration_seconds
# TYPE op_duration_seconds summary
op_duration_seconds{quantile="0.5"} 0.15
op_duration_seconds{quantile="0.95"} 0.4
op_duration_seconds{quantile="0.99"} 0.9
op_duration_seconds_sum 2
op_duration_seconds_count 10
`
	if err := testutil.CollectAndCompare(b.collector, strings.NewReader(expected),
		"op_duration_seconds"); err != nil {
		t.Fatalf("unexpected timer exposition: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/exporters/ -run TestBridge_Timer -v`
Expected: FAIL — no summary output.

- [ ] **Step 3: Write minimal implementation**

In `internal/metrics/exporters/prometheus.go`, add `"time"` to the import block, replace the trailing comment in `emitComplex` with timer handling, and add `emitTimer` + `durationSeconds`:

```go
	if _, ok := v["count"]; ok {
		c.emitTimer(ch, fqName, v, labels)
		return
	}
}

func (c *forgeCollector) emitTimer(ch chan<- prometheus.Metric, fqName string,
	v map[string]any, labels map[string]string) {
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

	keys, vals := sortedLabels(labels)
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
```

Note: place the new `if _, ok := v["count"]; ok { ... }` block as the last branch of `emitComplex`, after the histogram branch, so histograms (which also have `count`) are matched first.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics/exporters/ -run TestBridge -v`
Expected: PASS (all bridge tests).

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/exporters/prometheus.go internal/metrics/exporters/prometheus_test.go
git commit -m "feat(metrics): map forge timers to prometheus summaries"
```

---

### Task 4: Per-family label union + dedup

**Files:**
- Modify: `internal/metrics/exporters/prometheus.go` (rework `Collect` to a two-pass family grouping)
- Test: `internal/metrics/exporters/prometheus_test.go`

**Why:** Within one `Gather`, every series under the same `fqName` must share the same label key set or `client_golang` errors with "inconsistent label cardinality". Forge allows series with differing tag keys. Solution: group by `fqName`, compute the union of label keys, emit every series with that full key set (missing → `""`), and dedup identical `(fqName, labelValues)` (last wins).

**Interfaces:**
- Consumes: emit helpers from Tasks 1-3.
- Produces: `Collect` that never errors on heterogeneous label sets.

- [ ] **Step 1: Write the failing test**

Append to `internal/metrics/exporters/prometheus_test.go`:

```go
func TestBridge_LabelUnion(t *testing.T) {
	snapshot := func() map[string]any {
		return map[string]any{
			`hits_total{a="1"}`:        map[string]any{"value": float64(1), "_type": "counter"},
			`hits_total{b="2"}`:        map[string]any{"value": float64(2), "_type": "counter"},
		}
	}
	b := NewPrometheusBridge(snapshot, PrometheusConfig{})

	// Both series get the union {a,b}; the missing key is filled with "".
	expected := `
# HELP hits_total Forge counter hits_total
# TYPE hits_total counter
hits_total{a="1",b=""} 1
hits_total{a="",b="2"} 2
`
	if err := testutil.CollectAndCompare(b.collector, strings.NewReader(expected),
		"hits_total"); err != nil {
		t.Fatalf("unexpected label-union exposition: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/exporters/ -run TestBridge_LabelUnion -v`
Expected: FAIL — `Gather` returns "inconsistent label cardinality", or output lacks union labels.

- [ ] **Step 3: Write minimal implementation**

In `internal/metrics/exporters/prometheus.go`, replace `Collect` with a grouped two-pass version and update the emit helpers to accept an explicit ordered key set. Replace `Collect`, `emitScalar`, `emitComplex`, `emitHistogram`, `emitTimer` and add `series`/grouping helpers:

```go
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
		keys := unionKeys(list)        // sanitized, sorted union of label keys
		seen := make(map[string]bool)  // dedup by joined label values
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

	count, _ := toUint64(v["count"])
	sum, _ := toFloat(v["sum"])
	desc := prometheus.NewDesc(fqName, helpFor("histogram", fqName), keys, nil)
	ch <- prometheus.MustNewConstHistogram(desc, count, sum, cumulative, vals...)
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
```

Then DELETE the now-unused `sortedLabels` function (replaced by `unionKeys`/`alignValues`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics/exporters/ -run TestBridge -v`
Expected: PASS (all bridge tests, including the earlier counter/gauge/histogram/timer ones).

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/exporters/prometheus.go internal/metrics/exporters/prometheus_test.go
git commit -m "feat(metrics): per-family label union and dedup in prometheus bridge"
```

---

### Task 5: Wire the bridge into the collector

**Files:**
- Modify: `internal/shared/metrics.go` (add `PrometheusProvider`)
- Modify: `internal/metrics/collector.go` (field, construct in `New`, route `Export`, add `PrometheusHandler`, drop runtime collector)
- Test: `internal/metrics/collector_prometheus_test.go` (new)

**Interfaces:**
- Consumes: `exporters.NewPrometheusBridge`, `exporters.DefaultPrometheusConfig`, `(*PrometheusBridge).GatherText`, `(*PrometheusBridge).Handler` (Tasks 1-4)
- Produces:
  - `shared.PrometheusProvider interface { PrometheusHandler() http.Handler }`
  - `collector.promBridge *exporters.PrometheusBridge`
  - `(*collector).PrometheusHandler() http.Handler`
  - `Export(ExportFormatPrometheus)` returns bridge output.

- [ ] **Step 1: Write the failing test**

Create `internal/metrics/collector_prometheus_test.go`:

```go
package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	"github.com/xraph/forge/internal/shared"
)

func TestCollector_ExportPrometheusParses(t *testing.T) {
	c := New(DefaultCollectorConfig(), nil)

	c.Counter("widget_built_total").Inc()
	c.Gauge("widget_pending").Set(4)

	out, err := c.Export(shared.ExportFormatPrometheus)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var parser expfmt.TextParser
	if _, err := parser.TextToMetricFamilies(strings.NewReader(string(out))); err != nil {
		t.Fatalf("output is not valid prometheus text: %v", err)
	}
	if !strings.Contains(string(out), "widget_built_total") {
		t.Fatalf("expected widget_built_total in output, got:\n%s", out)
	}
}

func TestCollector_ImplementsPrometheusProvider(t *testing.T) {
	c := New(DefaultCollectorConfig(), nil)
	if _, ok := c.(shared.PrometheusProvider); !ok {
		t.Fatal("collector does not implement shared.PrometheusProvider")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/ -run TestCollector_ -v`
Expected: FAIL — `shared.PrometheusProvider` undefined / collector does not implement it.

- [ ] **Step 3: Add the `PrometheusProvider` interface**

In `internal/shared/metrics.go`, add `"net/http"` to imports and append:

```go
// PrometheusProvider is implemented by metrics collectors that can serve a
// Prometheus scrape endpoint via an http.Handler.
type PrometheusProvider interface {
	PrometheusHandler() http.Handler
}
```

- [ ] **Step 4: Wire the bridge into the collector**

In `internal/metrics/collector.go`:

(a) Add `"net/http"` is already imported. Add the field to the `collector` struct (after `tsStorage`):

```go
	promBridge *exporters.PrometheusBridge
```

(b) In `New`, immediately after `c.initializeExporters()`, add:

```go
	// Prometheus bridge: reads the merged snapshot fresh on each scrape.
	c.promBridge = exporters.NewPrometheusBridge(c.GetMetrics, exporters.PrometheusConfig{
		Namespace:              c.config.Collection.Namespace,
		EnableGoCollector:      true,
		EnableProcessCollector: true,
	})
```

(c) Replace the body of `Export` so Prometheus routes through the bridge:

```go
func (c *collector) Export(format metrics.ExportFormat) ([]byte, error) {
	if format == metrics.ExportFormatPrometheus && c.promBridge != nil {
		return c.promBridge.GatherText()
	}

	c.mu.RLock()
	exporter, exists := c.exporters[format]
	c.mu.RUnlock()

	if !exists {
		return nil, errors.ErrServiceNotFound(string(format))
	}

	metrics := c.GetMetrics()

	return exporter.Export(metrics)
}
```

(d) Add the handler method (anywhere among the collector methods):

```go
// PrometheusHandler returns an http.Handler that serves the Prometheus scrape
// endpoint. Implements shared.PrometheusProvider.
func (c *collector) PrometheusHandler() http.Handler {
	return c.promBridge.Handler()
}
```

(e) In `initializeBuiltinCollectors`, DELETE the runtime-collector block (the bridge's `GoCollector` replaces it):

```go
	// Register runtime metrics collector
	if c.config.Features.RuntimeMetrics {
		runtimeCollector := collectors.NewRuntimeCollector()
		if err := c.registerCollectorLocked(runtimeCollector); err != nil {
			return fmt.Errorf("failed to register runtime collector: %w", err)
		}
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/metrics/ -run TestCollector_ -v`
Expected: PASS (both tests).

- [ ] **Step 6: Run the full metrics package tests**

Run: `go test ./internal/metrics/...`
Expected: PASS. If any pre-existing test asserted the old hand-rolled text (timestamps, non-cumulative buckets), update it to parse with `expfmt.TextParser` instead of comparing exact bytes; note the change in the commit body.

- [ ] **Step 7: Commit**

```bash
git add internal/shared/metrics.go internal/metrics/collector.go internal/metrics/collector_prometheus_test.go
git commit -m "feat(metrics): route prometheus export through the bridge; drop forge runtime collector"
```

---

### Task 6: Serve `promhttp` at `/_/metrics`

**Files:**
- Modify: `app_impl.go` (`handleMetrics`)
- Test: `metrics_endpoint_test.go` (new, package `forge`)

**Interfaces:**
- Consumes: `shared.PrometheusProvider` (Task 5)
- Produces: `/_/metrics` served by `promhttp`, with `go_*` runtime metrics present.

- [ ] **Step 1: Write the failing test**

Create `metrics_endpoint_test.go` at repo root. Use the existing app construction pattern (check a sibling `*_test.go` such as `app_impl_test.go` for the exact `New(...)`/builder call and adjust the constructor line if needed):

```go
package forge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
)

func TestMetricsEndpoint_ServesPrometheus(t *testing.T) {
	a := newTestApp(t) // helper that builds an app with metrics enabled; see app_impl_test.go
	a.metrics.Counter("probe_total").Inc()

	req := httptest.NewRequest(http.MethodGet, "/_/metrics", nil)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") && !strings.Contains(ct, "openmetrics") {
		t.Fatalf("unexpected content-type %q", ct)
	}

	body := rec.Body.String()
	var parser expfmt.TextParser
	if !strings.Contains(ct, "openmetrics") {
		if _, err := parser.TextToMetricFamilies(strings.NewReader(body)); err != nil {
			t.Fatalf("body not valid prometheus text: %v", err)
		}
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Fatalf("expected go_goroutines (GoCollector) in output")
	}
	if !strings.Contains(body, "probe_total") {
		t.Fatalf("expected probe_total in output")
	}
}
```

If no `newTestApp`/`a.router.ServeHTTP` helper exists, add a minimal one in this test file following the construction used elsewhere in the package; do not invent unexported fields beyond `a.metrics` and `a.router`, which exist (`app_impl.go:39` and the router field).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestMetricsEndpoint_ServesPrometheus -v`
Expected: FAIL — `go_goroutines` absent (current handler uses the hand-rolled `Export`).

- [ ] **Step 3: Update the handler**

In `app_impl.go`, replace the body of `handleMetrics` (lines ~1424-1448) with:

```go
func (a *app) handleMetrics(ctx Context) error {
	if a.metrics == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "metrics not available",
		})
	}

	// Prefer the promhttp handler when the collector provides one (correct
	// exposition, content negotiation, Go/process collectors).
	if p, ok := a.metrics.(shared.PrometheusProvider); ok {
		p.PrometheusHandler().ServeHTTP(ctx.Response(), ctx.Request())
		return nil
	}

	// Fallback: text export.
	data, err := a.metrics.Export(shared.ExportFormatPrometheus)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to export metrics",
		})
	}

	ctx.Response().Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	ctx.Response().WriteHeader(http.StatusOK)
	if _, err := ctx.Response().Write(data); err != nil {
		return fmt.Errorf("failed to write metrics data: %w", err)
	}
	return nil
}
```

Confirm `Context` exposes `Request() *http.Request` and `Response() http.ResponseWriter`; if the accessor names differ, use the names from the `Context` definition (grep `func.*Request()` / `func.*Response()` in the router/context implementation).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestMetricsEndpoint_ServesPrometheus -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add app_impl.go metrics_endpoint_test.go
git commit -m "feat(app): serve /_/metrics via promhttp bridge"
```

---

### Task 7: Remove the dead push/export loop and old serializer remnants

**Files:**
- Modify: `internal/metrics/collector.go` (delete `startExporters`, `startExporter`, `exporterLoop`, `performExport`, `processExportedData`, `stopExporters`, and their call sites in `Start`/`Stop`)

**Interfaces:**
- Consumes: nothing new.
- Produces: a collector with no dead push path; Prometheus exporter map entry no longer needed.

- [ ] **Step 1: Find the call sites**

Run: `grep -n "startExporters\|startExporter\|exporterLoop\|performExport\|processExportedData\|stopExporters" internal/metrics/collector.go`
Expected: definitions (lines ~862-987) plus calls inside `Start` and `Stop`.

- [ ] **Step 2: Remove the call sites**

In `Start`, delete the `if err := c.startExporters(ctx); err != nil { ... }` block (and any log line referencing exporters starting). In `Stop`, delete the `c.stopExporters(ctx)` call. Leave the rest of `Start`/`Stop` intact.

- [ ] **Step 3: Remove the dead functions**

Delete the function definitions `startExporters`, `startExporter`, `exporterLoop`, `performExport`, `processExportedData`, `stopExporters`, and `getActiveExporters` if it is now unused (verify with `grep -n getActiveExporters internal/metrics/`). Remove the `prometheus` entry from `initializeExporters` (keep json/influx/statsd):

```go
func (c *collector) initializeExporters() {
	c.exporters[metrics.ExportFormatJSON] = exporters.NewJSONExporter()
	c.exporters[metrics.ExportFormatInflux] = exporters.NewInfluxExporter()
	c.exporters[metrics.ExportFormatStatsD] = exporters.NewStatsDExporter()
}
```

- [ ] **Step 4: Verify build + tests**

Run: `go build ./... && go test ./internal/metrics/...`
Expected: builds clean; tests PASS. Fix any remaining references to the deleted functions or the removed `NewPrometheusExporter` (the bridge replaces it — if `testing.go` references `exporters.NewPrometheusExporter`, update those to `NewPrometheusBridge` or remove them).

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/collector.go internal/metrics/testing.go
git commit -m "refactor(metrics): remove dead exporter push loop and legacy prometheus serializer"
```

---

### Task 8: Grafana dashboard + scrape config + docs

**Files:**
- Create: `examples/observability/prometheus.yml`
- Create: `examples/observability/servicemonitor.yaml`
- Create: `examples/observability/grafana-dashboard.json`
- Create: `examples/observability/README.md`

**Interfaces:** none (static assets).

- [ ] **Step 1: Scrape config**

Create `examples/observability/prometheus.yml`:

```yaml
scrape_configs:
  - job_name: forge
    metrics_path: /_/metrics
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:8080']  # replace with your Forge app host:port
```

- [ ] **Step 2: ServiceMonitor**

Create `examples/observability/servicemonitor.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: forge
  labels:
    release: prometheus
spec:
  selector:
    matchLabels:
      app: forge
  endpoints:
    - port: http
      path: /_/metrics
      interval: 15s
```

- [ ] **Step 3: Grafana dashboard**

Create `examples/observability/grafana-dashboard.json` with a minimal dashboard (HTTP RED + Go runtime). Use these panel queries (a worker may expand the JSON skeleton, but these targets are required):

- Request rate: `sum(rate(http_requests_total[5m]))`
- Request p95 latency: `histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))`
- Goroutines: `go_goroutines`
- Heap in use: `go_memstats_heap_inuse_bytes`
- Process CPU: `rate(process_cpu_seconds_total[5m])`

Minimal valid dashboard JSON skeleton (fill `panels` with the queries above as `timeseries` panels):

```json
{
  "title": "Forge Overview",
  "schemaVersion": 39,
  "version": 1,
  "time": { "from": "now-1h", "to": "now" },
  "templating": {
    "list": [
      {
        "name": "datasource",
        "type": "datasource",
        "query": "prometheus",
        "current": {}
      }
    ]
  },
  "panels": [
    {
      "type": "timeseries",
      "title": "Request rate",
      "datasource": "${datasource}",
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 0 },
      "targets": [
        { "expr": "sum(rate(http_requests_total[5m]))", "legendFormat": "rps" }
      ]
    },
    {
      "type": "timeseries",
      "title": "Request p95 latency",
      "datasource": "${datasource}",
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 0 },
      "targets": [
        { "expr": "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))", "legendFormat": "p95" }
      ]
    },
    {
      "type": "timeseries",
      "title": "Goroutines",
      "datasource": "${datasource}",
      "gridPos": { "h": 8, "w": 8, "x": 0, "y": 8 },
      "targets": [ { "expr": "go_goroutines", "legendFormat": "goroutines" } ]
    },
    {
      "type": "timeseries",
      "title": "Heap in use",
      "datasource": "${datasource}",
      "gridPos": { "h": 8, "w": 8, "x": 8, "y": 8 },
      "targets": [ { "expr": "go_memstats_heap_inuse_bytes", "legendFormat": "heap" } ]
    },
    {
      "type": "timeseries",
      "title": "Process CPU",
      "datasource": "${datasource}",
      "gridPos": { "h": 8, "w": 8, "x": 16, "y": 8 },
      "targets": [ { "expr": "rate(process_cpu_seconds_total[5m])", "legendFormat": "cpu" } ]
    }
  ]
}
```

- [ ] **Step 4: README**

Create `examples/observability/README.md`:

```markdown
# Observability: Prometheus + Grafana

Forge serves Prometheus metrics at `/_/metrics`.

## Scrape with Prometheus
Use `prometheus.yml` (note `metrics_path: /_/metrics`, not the default `/metrics`).

## Kubernetes (Prometheus Operator)
Apply `servicemonitor.yaml` (adjust the selector to match your Service labels).

## Grafana
Import `grafana-dashboard.json` and pick your Prometheus data source. It includes
HTTP RED panels and Go runtime panels (`go_*`, `process_*`).
```

- [ ] **Step 5: Verify the dashboard JSON is valid**

Run: `python3 -c "import json,sys; json.load(open('examples/observability/grafana-dashboard.json'))" && echo OK`
Expected: `OK`.

- [ ] **Step 6: Commit**

```bash
git add examples/observability/
git commit -m "docs(observability): add prometheus scrape config, servicemonitor, grafana dashboard"
```

---

### Task 9: Retire the orphaned `internal/observability` Prometheus code

**Files:**
- Inspect: `extensions/dashboard/collector/trace_exporter.go`
- Modify/Delete: `internal/observability/prometheus.go` (and references)

**Interfaces:** depends on inspection.

- [ ] **Step 1: Inspect the dependency**

Run: `grep -n "observability\." extensions/dashboard/collector/trace_exporter.go`
Read what symbols it uses. Determine whether it references the metrics `PrometheusExporter` / `PrometheusConfig` or only tracing types.

- [ ] **Step 2: Decide and act**

- If `trace_exporter.go` uses ONLY tracing types (not the metrics `PrometheusExporter`): delete `internal/observability/prometheus.go` and any now-unused `PrometheusConfig` reference in `internal/observability/observability.go` (`observability.go:36`, `observability.go:247`). Run `grep -rn "observability.*Prometheus" --include=*.go .` to find all references and remove/adjust them.
- If it genuinely needs metric export: replace its use with the new bridge — construct `exporters.NewPrometheusBridge(...)` where appropriate, or expose the app's `shared.PrometheusProvider`. Do NOT keep two `client_golang` metric stacks.

- [ ] **Step 3: Verify build + tests**

Run: `go build ./... && go test ./internal/... ./extensions/dashboard/...`
Expected: builds clean; tests PASS.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor(observability): retire duplicate prometheus exporter stack"
```

---

## Self-Review

**Spec coverage:**
- Correct exposition, no timestamps → Tasks 1-4 (client_golang/expfmt encoding), asserted in Task 1.
- Cumulative histogram buckets → Task 2.
- Counter typing, label union → Tasks 1 & 4.
- Decoupled bridge (`func() map[string]any`, no cycle) → Tasks 1 & 5.
- `Export()` delegation + `PrometheusProvider` → Task 5.
- promhttp at `/_/metrics`, Go/Process collectors, drop Forge runtime collector → Tasks 5 & 6.
- Remove dead push loop → Task 7.
- Grafana/scrape/docs → Task 8.
- Retire orphaned stack → Task 9.

**Placeholder scan:** Task 8's dashboard JSON is a complete, valid skeleton with required queries (not a TODO). Task 9 is conditional by necessity (depends on inspection) but specifies both branches concretely. No other placeholders.

**Type consistency:** `emitScalar`/`emitComplex`/`emitHistogram`/`emitTimer` signatures are redefined consistently in Task 4 (the canonical versions). `SnapshotFunc`, `PrometheusConfig`, `NewPrometheusBridge`, `GatherText`, `Handler`, `PrometheusHandler`, `shared.PrometheusProvider` names match across Tasks 1, 5, 6.

**Note on Task 4 refactor:** Tasks 2-3 add `emitHistogram`/`emitTimer` with a `(labels map)` signature, then Task 4 rewrites them to `(keys, vals []string)`. This is intentional incremental development; the Task 4 versions are final. A worker executing strictly in order will replace them as instructed.
