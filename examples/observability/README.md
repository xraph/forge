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
HTTP server metrics follow the OpenTelemetry semantic conventions and carry no
prefix: `http_server_request_duration_seconds` (a histogram, so its `_count` is the
request count) and `http_server_active_requests`. They're labelled with
`http_route`, `http_request_method`, `http_response_status_code`, `url_scheme` and
`error_type`, so you can break any panel down by route.

Metrics you create yourself are emitted under the `forge_` namespace prefix. The
namespace is configurable via `metrics.collection.namespace` in your Forge config.

Go runtime and process metrics (`go_goroutines`, `go_memstats_*`,
`process_cpu_seconds_total`, etc.) use their standard unprefixed names; they come
from the Go and Process collectors registered directly with the Prometheus registry.
