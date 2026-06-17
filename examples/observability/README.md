# Observability: Prometheus + Grafana

Forge serves Prometheus metrics at `/_/metrics`.

## Scrape with Prometheus
Use `prometheus.yml` (note `metrics_path: /_/metrics`, not the default `/metrics`).

## Kubernetes (Prometheus Operator)
Apply `servicemonitor.yaml` (adjust the selector to match your Service labels).

## Grafana
Import `grafana-dashboard.json` and pick your Prometheus data source. It includes
HTTP RED panels and Go runtime panels (`go_*`, `process_*`).
