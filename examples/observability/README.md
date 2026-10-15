# Observability: Prometheus, Grafana and Tempo

The quickest way to see this working is from a Forge project:

```sh
forge dev --observability
```

That starts a local Grafana stack, points your app at it and prints the Grafana URL. The [development docs](https://www.xraph.com/docs/forge/development#local-observability-stack) have the details.

The files here are for when you run your own Prometheus and Grafana.

| File | What it is |
|---|---|
| `grafana-dashboard.json` | The Forge Service dashboard, the same one `forge obs` provisions. Import it and pick your Prometheus, Tempo and Loki data sources. |
| `forge-rules.yaml` | Prometheus recording rules and alerts: error budget burn (fast and slow) for a 99.9% objective and a p95 latency alert at one second. |
| `prometheus.yml` | A scrape config. Note `metrics_path: /_/metrics`, not the default `/metrics`. |
| `servicemonitor.yaml` | A Prometheus Operator ServiceMonitor. Adjust the selector to your Service's labels. `forge deploy` can render one for you with `observability.service_monitor`. |

The dashboard and rules are generated from the CLI, so don't edit them here. To get rules for a different objective or threshold, run:

```sh
forge obs export --out ./obs --slo 99.5 --p95-threshold 500ms
```

If you imported the older version of this dashboard, its uid was `forge-overview`. The new one is `forge-service`, so importing it adds a second dashboard; delete the old one.

## Metric naming

HTTP server metrics follow the OpenTelemetry semantic conventions and carry no prefix: `http_server_request_duration_seconds` (a histogram, so its `_count` is the request count) and `http_server_active_requests`. They're labelled with `http_route`, `http_request_method`, `http_response_status_code`, `url_scheme` and `error_type`, so you can break any panel down by route.

Metrics you create yourself get the `forge_` namespace prefix. You can change it with `metrics.collection.namespace` in your Forge config.

Go runtime and process metrics (`go_goroutines`, `go_memstats_*`, `process_cpu_seconds_total` and so on) keep their standard unprefixed names; they come from the Go and Process collectors registered directly with the Prometheus registry.
