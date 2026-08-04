# Prometheus Bridge for Forge Metrics — Design

**Date:** 2026-06-17
**Status:** Approved (design); pending implementation plan
**Author:** Rex Raphael (with Claude)

## Summary

Forge exposes a Prometheus-style scrape endpoint at `/_/metrics`, but the bytes it
serves are produced by a **hand-rolled text serializer**
(`internal/metrics/exporters/prometheus.go`) with several correctness problems
(most importantly, a per-sample timestamp on every line). A second, more correct
`client_golang`-based Prometheus implementation exists in `internal/observability`
but is orphaned (imported only by the dashboard trace exporter).

This design replaces the hand-rolled serializer with a **bridge**: a
`prometheus.Collector` that reads Forge's existing metrics registry on each scrape
and emits correct Prometheus const-metrics via `client_golang`, served through
`promhttp`. The core `metrics` package stays backend-agnostic — Prometheus becomes
one consumer of the registry behind the existing `Exporter` seam, not a hard
dependency of the core.

## Goals

- Serve spec-correct Prometheus exposition at `/_/metrics` (no per-sample
  timestamps, correct counter typing, cumulative histogram buckets, proper escaping).
- Keep Forge's metrics core decoupled from Prometheus — the registry and the
  `Exporter` abstraction never import `client_golang`.
- Add standard `go_*` / `process_*` runtime metrics for off-the-shelf Grafana
  compatibility.
- Remove the dead push/export loop and the duplicate, orphaned Prometheus stack.
- Ship starter Grafana/scrape assets so users get value immediately.

## Non-goals

- Replacing the whole `internal/metrics` API surface. Extensions keep using the
  Forge `Counter`/`Gauge`/`Histogram`/`Timer` API unchanged.
- Push-based delivery (Pushgateway / remote write). Scrape (pull) only.
- Host-level metrics beyond what Forge's system collector already provides
  (that's node_exporter territory).

## Background: current state (verified)

- `/_/metrics` handler: `app_impl.go:1424` calls `a.metrics.Export(ExportFormatPrometheus)`
  and writes the bytes with `Content-Type: text/plain; version=0.0.4`.
- Hand-rolled serializer: `internal/metrics/exporters/prometheus.go`. Issues:
  - Writes `time.Now().UnixMilli()` on every sample (`prometheus.go:356-357`) — breaks
    Prometheus staleness handling; should never be present on a scrape endpoint.
  - Counter typing relies on a `{"_type":"counter"}` tag injected by the registry
    (`registry.go:81`); everything else infers to `gauge`.
  - Emits histogram buckets directly from `Buckets()` — but those are **per-bucket
    (non-cumulative)** counts, so the histogram is malformed for Prometheus.
- Dead push path: `collector.go` `exporterLoop` → `performExport` → `processExportedData`
  (`collector.go:952`) is a no-op placeholder; configuring a "prometheus exporter"
  with an interval generates text and discards it.
- Registry read surface (the seam we use): `registry.go:526`
  `GetRegisteredMetrics() []*RegisteredMetric`, where `RegisteredMetric` carries
  `Name`, `Type`, `Tags map[string]string`, typed `Metric any`, and `Metadata`.
- go-utils metric types (`github.com/xraph/go-utils@v1.1.1/metrics`):
  - `Counter.Value() float64`, `Gauge.Value() float64`
  - `Histogram.Count() uint64`, `Sum() float64`, `Buckets() map[float64]uint64`
    (**per-bucket**, confirmed at `metrics_impl.go:434-435`: `h.counts[idx].Add(1)`)
  - `Timer.Count() uint64`, `Sum() time.Duration`, `Percentile(float64) time.Duration`
- Orphaned stack: `internal/observability/prometheus.go` (real `client_golang` +
  `promhttp`, own `:9090` server, Go/Process collectors). Imported only by
  `extensions/dashboard/collector/trace_exporter.go:6`.

## Data source (revised after code review)

The bridge consumes the **complete merged snapshot** `collector.GetMetrics()
map[string]any`, NOT `registry.GetRegisteredMetrics()`. Reason: system, HTTP,
runtime, and extension collectors implement `CustomCollector.Collect() map[string]any`
and emit values **only as a map** (dotted keys like `system.cpu.usage`,
`http.requests.total`); nothing writes those into the typed registry
(`collectors/system.go` keeps its own internal map). Reading only the typed registry
would silently drop those metrics. `collector.GetMetrics()` merges
`registry.GetAllMetrics()` with every custom collector's `Collect()` output, so it is
the only complete source.

The map is still rich enough to produce correct Prometheus output: counters carry
`{"_type":"counter"}`, histograms carry `{count,sum,buckets}` (per-bucket), gauges are
bare scalars. `client_golang`/`expfmt` does the actual encoding, which is what
eliminates the per-sample-timestamp bug and gives proper escaping.

Trade-off vs a pure-typed approach: generic synthesized HELP text for map-only
metrics, and no exemplars in v1. Correctness, completeness, and decoupling all hold.

## Architecture

```
internal/metrics (core, prometheus-agnostic)
        │  collector.GetMetrics() map[string]any   (complete merged snapshot)
        ▼
internal/metrics/exporters/prometheus  (the bridge — only place importing client_golang)
        │  - NewPrometheusBridge(snapshot func() map[string]any, cfg)
        │  - forgeCollector implements prometheus.Collector; Collect(ch) calls
        │    the snapshot func fresh per scrape → const metrics
        │  - owns a *prometheus.Registry (+ GoCollector + ProcessCollector)
        │  - Handler() http.Handler (promhttp) and GatherText() ([]byte, error)
        ▼
promhttp.HandlerFor(registry)  ──►  served at /_/metrics
```

- **Pull-on-scrape, stateless.** `forgeCollector.Collect()` invokes the snapshot func
  and emits `prometheus.MustNewConstMetric(...)` / `MustNewConstHistogram(...)` /
  `MustNewConstSummary(...)` fresh on each scrape. No retained state, no interval
  loop, no written timestamps.
- **The bridge takes a plain `func() map[string]any`** — it imports neither the
  registry nor the app, so there is no import cycle and Prometheus stays a swappable
  consumer.
- **Legacy `Export(ExportFormatPrometheus) []byte` delegates to the bridge.**
  `collector.Export` special-cases Prometheus to call `bridge.GatherText()` (gather +
  `expfmt` encode), so existing callers (`debug_server.go:322`, `endpoints.go`
  `/export/prometheus`) keep working with correct output. JSON/Influx/StatsD continue
  through the existing `Exporter` map unchanged.
- **The app reaches the handler via a small interface** in `shared`:
  `PrometheusProvider { PrometheusHandler() http.Handler }`, which the collector
  implements; `app_impl.go` type-asserts it to mount `promhttp`.

## Type mapping

The bridge inspects each map value to decide the Prometheus type:

| Map value shape | Prometheus | Notes |
|---|---|---|
| `map` with `_type=="counter"` (or name ends `_total`) | `CounterValue` const metric | uses the `value` field |
| bare scalar `float64`/`int64`/`uint64` | `GaugeValue` const metric | default for scalars |
| `map` with `buckets` (`map[float64]uint64`), `count`, `sum` | `MustNewConstHistogram` | convert **per-bucket → cumulative**: sort boundaries ascending, running sum; pass `count`, `sum`, cumulative map |
| `map` with `count` + percentile keys (`p50`/`p95`/`p99`), no `buckets` | `MustNewConstSummary` | quantiles from the percentile fields; durations already seconds in map. Caveat: pre-aggregated quantiles cannot be re-aggregated across replicas (documented) |

- **HELP** text is synthesized generically per type (map-only metrics carry no
  metadata). Unit suffix conventions (`_seconds`, `_bytes`, `_total`) are preserved
  from existing metric names where present.
- **Cumulative conversion** is the explicit fix for the malformed-histogram bug.
- Unknown/unsupported value shapes are skipped (logged at debug), never panic.

## Label-set consistency

`client_golang` requires every series within a metric family to share the same
label key set, or `Collect()` errors with "inconsistent label cardinality." Forge
permits arbitrary per-series tags under a name. The bridge therefore:

1. First pass per metric family (base name): compute the **union of all tag keys**.
2. Emit every series with that full key set, filling absent keys with `""`.
3. Sanitize metric names and label names to `[a-zA-Z0-9_]` (reuse existing sanitize
   helper); escape label values.

## Decoupling & packaging

- Bridge rewritten in place at `internal/metrics/exporters/prometheus.go` — the only
  file importing `client_golang`.
- The bridge depends on nothing Forge-specific: its constructor is
  `NewPrometheusBridge(snapshot func() map[string]any, cfg PrometheusConfig)`. It has
  no knowledge of the registry, app, router, or DI container, so there is no import
  cycle (`exporters` already imports only `shared` + go-utils + now client_golang).
- The collector owns a `*exporters.PrometheusBridge`, constructed in `New()` with
  `c.GetMetrics` as the snapshot func. The collector:
  - routes `Export(ExportFormatPrometheus)` to `bridge.GatherText()`;
  - implements `shared.PrometheusProvider` via `PrometheusHandler() http.Handler`
    returning `bridge.Handler()`.
- A small `shared.PrometheusProvider { PrometheusHandler() http.Handler }` interface
  lets `app_impl.go` (package `forge`) reach the handler without importing
  `internal/metrics` internals.

## Endpoint, runtime collectors & config

- **`/_/metrics`** (`app_impl.go:1424`): when `a.metrics` satisfies
  `shared.PrometheusProvider`, serve its `PrometheusHandler()` (promhttp) via
  `handler.ServeHTTP(ctx.Response(), ctx.Request())` rather than writing `Export()`
  bytes manually. Falls back to the current `Export()` path otherwise.
- **Runtime collectors:** register `collectors.NewGoCollector()` +
  `collectors.NewProcessCollector(...)` in the bridge registry. Disable Forge's
  **runtime** collector by default to avoid duplicate `go_*` series. **Keep** Forge's
  **system** collector (host CPU/disk/network/load — no `client_golang` overlap).
- **Config** (extends existing `MetricsConfig`; all defaulted, backward compatible):
  ```yaml
  metrics:
    prometheus:
      enabled: true
      namespace: "forge"
      go_collector: true
      process_collector: true
  ```
  Absent config = current behavior (enabled, namespace `forge`).

## Retiring the orphaned `internal/observability` Prometheus code

`internal/observability/prometheus.go` is used only by
`extensions/dashboard/collector/trace_exporter.go`. Plan (last/most-isolated phase):

- Inspect what `trace_exporter.go` actually consumes.
  - If it only needs tracing types → remove `PrometheusExporter` from
    `internal/observability`, leave tracing intact.
  - If it genuinely needs metric export → repoint it at the new bridge.
- Lands after the core work is green to keep risk isolated.

## Grafana deliverables

Under `examples/observability/` (or `docs/`):

- `prometheus.yml` scrape snippet with `metrics_path: /_/metrics`.
- A `ServiceMonitor` example for the Prometheus Operator.
- One starter Grafana dashboard JSON: HTTP RED (`http_requests_total`,
  `http_request_duration_seconds`) + Go runtime (`go_goroutines`, `go_memstats_*`,
  `process_*`).
- A short docs page tying it together.

## Testing

- **Unit (bridge):** feed a fake snapshot func returning a `map[string]any` with one
  of each metric value shape (including a histogram with known per-bucket counts and
  series with differing tag sets); gather and assert exposition with `client_golang`'s
  `testutil.GatherAndCompare` against golden text. Explicitly verifies: cumulative
  buckets, counter `# TYPE`, label-union fill, and **no per-sample timestamps**.
- **Endpoint:** `httptest` against `/_/metrics`; assert
  `Content-Type: text/plain; version=0.0.4` and that `expfmt.TextParser` accepts the
  body with zero errors.
- **Regression:** scrape twice; confirm counters are monotonic and histograms parse
  as valid Prometheus histograms.

## Implementation phases

1. Bridge: `NewPrometheusBridge(func() map[string]any, cfg)` + `forgeCollector`
   (`prometheus.Collector`) + type mapping (incl. cumulative buckets + label union) +
   unit tests with `testutil.GatherAndCompare`.
2. Wire bridge into collector: `shared.PrometheusProvider`, route
   `Export(ExportFormatPrometheus)` → `bridge.GatherText()`, mount `promhttp` at
   `/_/metrics`, register Go/Process collectors, disable Forge runtime collector.
3. Remove the dead `exporterLoop`/`performExport`/`processExportedData` push path and
   the old hand-rolled serializer code.
4. Grafana dashboard + scrape/ServiceMonitor examples + docs.
5. Retire orphaned `internal/observability` Prometheus code (repoint or remove
   trace_exporter dependency).

## Risks & mitigations

- **Inconsistent label cardinality** → handled by per-family label union.
- **Histogram correctness** → explicit per-bucket→cumulative conversion, asserted in
  tests.
- **Duplicate runtime series** → disable Forge runtime collector when Go/Process
  collectors are enabled.
- **Breaking the dashboard trace exporter** → isolate retirement to the final phase,
  after inspecting actual usage.
