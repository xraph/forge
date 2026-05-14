# Slice (i) — Retire CoreContributor templ pages

**Status:** Active
**Branch:** `dashboard-contract-slice-a`
**Depends on:** slice (h) — `core-contract` pilot covers Overview/Health/Metrics/Traces/Extensions/Services
**Predecessors:** (a) contract foundation, (b) security+observability, (c) dispatcher+pilot, (d) React shell, (e) shadcn/Base UI vocabulary, (f) streaming contract, (h) CoreContributor migration

## Why now

Slice (h) shipped the `core-contract` pilot covering every page CoreContributor served:
`/`, `/health`, `/metrics`, `/traces`, `/extensions`, `/services`. The React shell at
`/dashboard/contract/app/*` renders all of them via the contract envelope. The legacy
templ pages now exist purely as a parallel path with no unique data.

Two-system coexistence was the right move during slices (b)–(h). Now it's overhead:
duplicate routes, ~2,000 LOC of templ that has to keep compiling, and an active
CoreContributor that hits the same data sources twice.

## Scope

**In scope — remove:**
- `extensions/dashboard/core_contributor.go` (CoreContributor type + manifest + RenderPage/Widget/Settings impls)
- The `NewCoreContributor` registration in `extension.go::Register`
- Ten CoreContributor-only templ pages and their `_templ.go` artifacts:
  - `overview.templ`, `health.templ`, `metrics.templ`, `metrics_all.templ`,
    `metrics_collector_detail.templ`, `metrics_detail.templ`, `services.templ`,
    `extensions.templ`, `traces.templ`, `trace_detail.templ`
- The matching `*_helpers.go` files that exist solely to feed those pages:
  - `overview_helpers.go`, `health_helpers.go`, `metrics_helpers.go`,
    `metrics_detail_helpers.go`, `services_helpers.go`, `traces_helpers.go`,
    `extensions_helpers.go`, `chart_helpers.go`
- The ten templ-rendering page methods on `PagesManager` in `pages/pages.go`:
  `OverviewPage`, `HealthPage`, `MetricsPage`, `MetricsAllPage`,
  `MetricsCollectorDetailPage`, `MetricsDetailPage`, `ServicesPage`,
  `ExtensionsPage`, `TracesPage`, `TraceDetailPage`

**In scope — replace with redirects:**
- `pages.go::RegisterPages` registers the same ten routes as native HTTP 302 handlers
  on the underlying `forge.Router` (not as `forgeui.Page` handlers — the redirect must
  beat ForgeUI's catch-all). Each redirect points to the equivalent React shell route:

  | Old templ path                          | Redirect target                                   |
  | ---                                     | ---                                               |
  | `/dashboard/`                           | `/dashboard/contract/app/`                        |
  | `/dashboard/health`                     | `/dashboard/contract/app/health`                  |
  | `/dashboard/metrics`                    | `/dashboard/contract/app/metrics`                 |
  | `/dashboard/metrics/all`                | `/dashboard/contract/app/metrics`                 |
  | `/dashboard/metrics/collectors/:name`   | `/dashboard/contract/app/metrics`                 |
  | `/dashboard/metrics/detail/*name`       | `/dashboard/contract/app/metrics`                 |
  | `/dashboard/services`                   | `/dashboard/contract/app/services`                |
  | `/dashboard/extensions`                 | `/dashboard/contract/app/extensions`              |
  | `/dashboard/traces`                     | `/dashboard/contract/app/traces`                  |
  | `/dashboard/traces/:id`                 | `/dashboard/contract/app/traces?id=:id`           |

  `metrics/all`, `/metrics/collectors/:name`, `/metrics/detail/*name` collapse into the
  single `/metrics` shell route — slice (j) will add deep-linked detail routes when we
  rebuild those pages on the React side.

**Out of scope — keep:**
- `ui/shell/*.templ` — sidebar/topbar/breadcrumbs/scripts still feed extension contributors (auth, settings) through `LayoutManager`.
- `layouts/*.templ` — layout chrome for extension contributors.
- `ui/components.templ`, `ui/widgets.templ`, `ui/metrics.templ`, `ui/tables.templ` — shared utility templ widgets used by remaining contributors.
- `ui/pages/error.templ` + `error_templ.go` — `pages.go` still uses `uipages.ErrorPage` for non-CoreContributor error responses (extension page lookups, settings unavailable).
- `pages.go` settings + extension page handlers — extensions still render templ.
- `core-contract` (the contributor name on the contract pilot) — this is the slice-(c)/h thing, *not* the legacy CoreContributor. Different system. Keeps its `core-contract` name.

## Non-goals

- Migrating extension contributors (auth, settings sub-pages) off templ. That's a separate stream — extension authors own their UI choice.
- Adding `/metrics/collectors/:name` or `/traces/:id` deep links on the React side. Folded in to slice (j) when we rebuild those pages with proper detail routes.
- Removing `templ` as a Go dependency. Extension contributors still use it.

## Verification

- `go build ./...` clean
- `go test ./extensions/dashboard/...` all green (registry tests use `"core"` as a stub name in `registry_test.go` — those don't touch the deleted `core_contributor.go`, just the string `"core"`. They keep passing.)
- Manual smoke: `curl -sIL http://localhost:8080/dashboard/health` returns 302 → `/dashboard/contract/app/health`; the React shell loads at the new URL.
- LOC delta: removes ~2,000 lines of templ + helpers.

## Risks / mitigations

- **Bookmarks.** Old links to `/dashboard/health` etc. still work via 302 — no broken bookmarks.
- **CLI/API tooling that scrapes templ HTML.** Anything in that bucket already had to stop in slice (h) when the React shell appeared. If we missed a tool, the 302 is detectable.
- **Extension contributor pages that depended on `pages.go` templ helpers re-exported.** Cross-check: `extensions_helpers.go::IsCore` references `name == "core"` for badge rendering. With CoreContributor gone, the registry has no `"core"` entry, so the bool is always false — harmless. But that file gets deleted as part of this slice anyway.
- **Test breakage in `contributor/registry_test.go`.** Those tests build their own `newStub("core", …)` instances; they don't reference `core_contributor.go` or any deleted templ. No change required.

## Out of this slice (j) follow-on

- Deep-link routes on the React side for trace/metric detail (`/contract/app/traces/:id`, `/contract/app/metrics/:name`).
- Mutable settings via the contract write path (slice (k)).
- Replace the in-memory idempotency store with Redis when running multi-replica.
