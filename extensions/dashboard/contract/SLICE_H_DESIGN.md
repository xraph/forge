# Slice (h) — CoreContributor Migration

> Companion design doc to [DESIGN.md](DESIGN.md). Slice (h) extends the `core-contract` pilot from slice (c) to cover every page the legacy `CoreContributor` serves today, so the dashboard's own pages run on the contract path end-to-end.

## Context

The pilot in slice (c) shipped three intents (`extensions.list`, `services.list`/`services.detail`, `metrics.summary` subscription) backed by three routes (`/extensions`, `/services`, `/metrics/live`). Slice (e+) added the React vocabulary that renders them on shadcn/Base UI. Slice (f) migrated the streaming extension as the first external contributor.

Slice (h) closes the loop on the dashboard's own pages: Overview, Health, Metrics report, Traces, plus tightening Extensions and Services. After this slice lands, every page `CoreContributor` serves via templ has a contract equivalent. The legacy templ paths keep working in parallel; slice (i) is the retirement step.

## Architecture Decisions (locked in)

| Decision | Choice |
|---|---|
| Contributor name | Stays `core-contract` (extends the existing pilot rather than creating a new contributor). |
| Sub-package layout | Keep everything under `extensions/dashboard/contract/pilot/`. The "pilot" name is now historical; this slice promotes it to the canonical CoreContributor replacement. |
| New intents | `overview`, `health`, `metrics-report`, `traces.list`, `traces.detail`. All `query` kind, capability `read`. |
| Reused intents | `extensions.list`, `services.list`, `services.detail`, `metrics.summary` (subscription) — unchanged. |
| New routes | `/`, `/health`, `/metrics`, `/traces` added to the manifest. Existing `/extensions`, `/services`, `/metrics/live` unchanged. |
| Trace detail | `resource.list` + `detailDrawer` slot — same admin-tool pattern used for services and rooms. Deep-linked `/traces/{id}` deferred to slice (j). |
| Overview page | `dashboard.grid` of `metric.counter` cards backed by the `overview` query. Mirrors today's templ overview. |
| Health page | `resource.list` of services with status, message, duration columns. Empty state when no services registered. |
| Metrics page | Two-column `dashboard.grid`: left a `resource.list` of collectors, right a `resource.list` of top metrics. Metrics report data flows through one `metrics-report` intent. |
| Settings descriptor | Unchanged — settings are wired through the dashboard's existing settings registry, not the contract. |

## Scope

### In scope (this slice)
- Five new query handlers in `extensions/dashboard/contract/pilot/`:
  - `overview.go` — overall health summary + service counts.
  - `health.go` — health check results per service.
  - `metrics_report.go` — collectors + top metrics.
  - `traces.go` — `traces.list` + `traces.detail`.
- `pilot.Deps` gains a `Traces TracesProvider` field for the trace store.
- Manifest YAML extended with the four new routes plus their graph trees.
- Unit test per new handler (5 new tests).
- Integration test extension covering one of the new routes end-to-end.

### Out of scope (future slices)
- **(i)** Retire the legacy templ contributor (`extensions/dashboard/core_contributor.go` + the `extensions/dashboard/ui/*.templ` files) once the contract path is validated in production.
- **(j)** Deep-linked detail routes (`/traces/{id}`, `/services/{name}`).
- **(k)** Mutable settings via `form.edit`.
- Subscription intents for live overview / health (today's pages poll; subscriptions are an optimization).

## Vocabulary mapping

| Legacy page | New route | Intent tree |
|---|---|---|
| Overview (`/`) | `/` | `page.shell` → `dashboard.grid` (cols=4) → 6× `metric.counter` (overall health badge, services, healthy services, metrics count, uptime, version) |
| Health (`/health`) | `/health` | `page.shell` → `resource.list` (data: health) with columns: name, status, message, duration, critical |
| Metrics (`/metrics`) | `/metrics` | `page.shell` → `dashboard.grid` (cols=2) → two `resource.list`s (collectors and top metrics) reading from one `metrics-report` query |
| Traces (`/traces`) | `/traces` | `page.shell` → `resource.list` (data: traces.list) + `detailDrawer` → `resource.detail` (data: traces.detail using parent.id) |
| Services (`/services`) | unchanged | already present from slice (c) |
| Extensions (`/extensions`) | unchanged | already present from slice (c) |
| Live Metrics (`/metrics/live`) | unchanged | already present from slice (c) |

## Files Affected

### New
```
extensions/dashboard/contract/pilot/
  overview.go            + overview_test.go
  health.go              + health_test.go
  metrics_report.go      + metrics_report_test.go
  traces.go              + traces_test.go
extensions/dashboard/contract/SLICE_H_DESIGN.md
```

### Modified
- `extensions/dashboard/contract/pilot/manifest.yaml` — add the four new routes + graph trees + new intent declarations.
- `extensions/dashboard/contract/pilot/pilot.go` — register the new query handlers and wire `Deps.Traces`.
- `extensions/dashboard/contract/pilot/types.go` — add wire shapes (`OverviewResponse`, `HealthList`, `MetricsReportResponse`, `TraceSummary`, `TraceDetailResponse`).
- `extensions/dashboard/extension.go` — pass the existing `e.traceStore` into `pilot.Deps.Traces`.

### Reused (do not duplicate)
- `collector.DataCollector.CollectOverview / CollectHealth / CollectMetricsReport`
- `collector.TraceStore.ListTraces / GetTrace`
- All slice (e) React vocabulary intents (page.shell, resource.list, resource.detail, dashboard.grid, metric.counter)

## Verification

- 5 new unit tests under pilot/ — table-driven, fixture collectors stubbing the data calls.
- Existing 16 pilot tests stay green.
- `go build ./...` clean across the forge module.
- Manual: browse to `/dashboard/contract/app/`, `/health`, `/metrics`, `/traces`. Verify each renders, traces detail drawer opens.

## After this slice

The contract has full surface coverage of CoreContributor. Slice (i) retires the templ path:
- Remove `extensions/dashboard/core_contributor.go`
- Remove `extensions/dashboard/ui/*.templ` (~2,266 LOC)
- Remove HTML-fragment proxying in `RemoteContributor` (`Fetch/Post Page/Widget/Settings`)
- Remove the legacy `LocalContributor` rendering interface

Slice (i) is delete-only — nothing new to design.
