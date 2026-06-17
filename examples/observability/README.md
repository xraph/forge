# Observability: Prometheus + Grafana

Forge serves Prometheus metrics at `/_/metrics`.

## Scrape with Prometheus
Use `prometheus.yml` (note `metrics_path: /_/metrics`, not the default `/metrics`).

## Kubernetes (Prometheus Operator)
Apply `servicemonitor.yaml` (adjust the selector to match your Service labels).

## Grafana
Import `grafana-dashboard.json` and pick your Prometheus data source. It includes
HTTP RED panels and Go runtime panels (`go_*`, `process_*`).

## Metric naming
Forge application metrics (HTTP request counts, latencies, etc.) are emitted under
the `forge_` namespace prefix — e.g. `forge_http_requests_total`,
`forge_http_request_duration_bucket`. The namespace is configurable via
`metrics.collection.namespace` in your Forge config.

Go runtime and process metrics (`go_goroutines`, `go_memstats_*`,
`process_cpu_seconds_total`, etc.) use their standard unprefixed names; they come
from the Go and Process collectors registered directly with the Prometheus registry.
