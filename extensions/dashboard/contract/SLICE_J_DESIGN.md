# Slice (j) — Wire the graph endpoint, add deep-link detail routes

**Status:** Active
**Branch:** `dashboard-contract-slice-a`
**Predecessors:** (a)–(i)

## What this slice fixes

Slice (j) addresses two problems uncovered while planning detail routes:

1. **The graph endpoint never actually returned graphs.** `POST /api/dashboard/v1` with `kind: "graph", intent: "page.shell"` 404s with `intent page.shell not registered`. The transport handler's intent-table lookup runs for every kind, but `page.shell` is a vocabulary marker for slot validation — never declared in `intents:` and never registered with the dispatcher. The React shell's smoke tests passed because they mock the response. Production had no working graph fetch.

2. **No deep-link routes for detail pages.** Slice (i) collapsed `/dashboard/traces/:id` to `/contract/app/traces?id=…` because the React shell only had a `*` catch-all route and the manifest only had list routes. The contract manifest already speaks `:id`-style placeholders in extension targets, but `MergedGraph` matches routes with literal string equality.

Both gaps are interlinked: deep-link routes need the graph endpoint to actually work, and the graph endpoint needs route-param matching to honor `/traces/:id` style paths.

## Approach

### 1. Special-case `KindGraph` in transport handler

`transport/http.go::ServeHTTP` currently treats every kind identically — same intent-table lookup, same dispatcher hop. Add a graph branch *before* the intent lookup:

```go
if req.Kind == contract.KindGraph {
    h.serveGraph(w, r, req)
    return
}
```

`serveGraph` reads the route from `req.Payload.route`, calls `GraphBuilder.Build(reg, wardens).Build(ctx, contributor, route, principal)`, JSON-marshals the result, and writes the standard envelope. No CSRF, no idempotency, no command rules — graph is a read concern. The principal still feeds the `visibleWhen` filter.

The handler needs the warden registry it already holds (line 56–57). Constructing the builder per-request is fine — it's a thin wrapper over the registry.

### 2. Add route-param matching to `MergedGraph`

Today's matcher (registry.go:149):

```go
for i := range g {
    if g[i].Route == route {
        return &g[i], true
    }
}
```

Change to a two-pass match:
- Exact match wins.
- Otherwise, walk routes that contain `:`-segments and try to match against the request URL. On a hit, extract `name → value` for each `:name` placeholder.

`MergedGraph` returns a single bool today; extend the signature so callers receive the params:

```go
MergedGraph(contributor, route string) (*GraphNode, map[string]string, bool)
```

Test fixtures and `slots_test.go` get a third return value. `GraphBuilder.Build` propagates the params through.

Cardinality: top-level routes only. We do not support `/foo/:a/bar/:b` deep matches in a single segment — the slot system handles that via inline params and `parent.X`. One layer of `:id`-segments is enough for slice (j).

### 3. Surface route params on the wire

Extend `ResponseMeta` (or include a parallel `routeParams`) so the client can read the matched params back. Concretely:

```go
type ResponseMeta struct {
    IntentVersion int               `json:"intentVersion,omitempty"`
    RouteParams   map[string]string `json:"routeParams,omitempty"`
    // ...existing
}
```

`serveGraph` populates `RouteParams` from step 2. The graph response payload remains the GraphNode tree (the manifest tree, with no inline param substitution — substitution happens client-side).

### 4. Wire React shell to use route params

`bindings.ts::resolvePath` already accepts `route.X`. Today nothing populates `ctx.route`. The hook layer needs to:

- `useContractGraph` returns both `data` (the GraphNode) and `routeParams` (from response meta).
- `App.tsx::PageRoute` exposes the params via React context (or threads them through GraphRenderer).
- `GraphRenderer` includes `route` in the `BindingContext` it builds for child intent components.
- The intent components (`resource.detail`, `form.edit`, `action.button`) already call `resolvePayload(node.payload, ctx)`. They get `ctx.route` for free once the renderer threads it.

### 5. Add `/traces/:id` to the pilot manifest

```yaml
- route: /traces/:id
  intent: resource.detail
  data:
    intent: traces.detail
    params: { id: { from: route.id } }
  title: Trace
  props:
    fields: [traceID, root_span, span_count, duration, status, start_time, protocol]
```

The slice-(h) `traces.detail` query handler already exists. The drawer-fed-from-list pattern from slice (h) keeps working in parallel — the deep-link route is additive, not replacing.

`/metrics/collectors/:name` and `/metrics/:name` deep links: out of scope for slice (j). Those would need new `metrics.collector` and `metrics.detail` query handlers; the slice (i) redirect collapsing them onto `/metrics` is fine for now. Add as slice (j2) when the React shell grows dedicated detail components for them.

### 6. Update slice (i) redirect

```go
pm.fuiApp.Page("/traces/:id").Handler(redirectTo(shellBase + "/traces/:id"))...
```

Have to interpolate `:id` from the request URL — small adjustment to `redirectTraceDetail`.

## Files

- `extensions/dashboard/contract/transport/http.go` — add `serveGraph` branch, take a graph builder factory or wreg dep.
- `extensions/dashboard/contract/registry.go` — `MergedGraph` returns params; route matcher with `:`-segments.
- `extensions/dashboard/contract/graph.go` — `Build` returns params; thread through `filter`.
- `extensions/dashboard/contract/envelope.go` — `ResponseMeta.RouteParams`.
- `extensions/dashboard/contract/pilot/manifest.yaml` — add `/traces/:id` route.
- `extensions/dashboard/contract/shell/src/contract/types.ts` — `GraphResponse` shape with `routeParams`.
- `extensions/dashboard/contract/shell/src/contract/client.ts` — `graph()` returns `{ node, routeParams }`.
- `extensions/dashboard/contract/shell/src/contract/hooks.ts` — `useContractGraph` returns the same.
- `extensions/dashboard/contract/shell/src/App.tsx` — pass routeParams down.
- `extensions/dashboard/contract/shell/src/runtime/renderer.tsx` — accept route context, weave into BindingContext.
- `extensions/dashboard/contract/shell/src/runtime/context.tsx` (new) — `RouteParamsProvider`.
- `extensions/dashboard/pages/pages.go` — fix `/traces/:id` redirect to interpolate.

## Tests

- **Transport:** `TestGraphHandler_ReturnsGraphForRoute` — POST kind=graph for `/health` returns the right node tree.
- **Transport:** `TestGraphHandler_ParamRouteExtractsID` — POST for `/traces/abc123` returns the `/traces/:id` node + meta.routeParams `{id:abc123}`.
- **Transport:** `TestGraphHandler_NotFoundUnknownRoute` — `/nothere` returns CodeNotFound.
- **Registry:** `TestMergedGraph_ExactBeforeParam` — exact route wins over a sibling param route.
- **Registry:** `TestMergedGraph_ParamMatch_ExtractsValue`.
- **React shell:** smoke test that renders a `/traces/:id` graph and confirms the inner `resource.detail` intent receives `route.id` in its bindings (no real fetch — mocked).

## Out of scope

- Multi-segment params (`/a/:x/b/:y`) — single-segment is enough for slice (j).
- Optional/glob route segments (`*filepath`).
- New detail intents for metrics collectors / individual metrics.
- Cache key changes for `/traces/:id` graph fetches — slice (b)'s graph cache treats route as opaque, so `/traces/abc` and `/traces/def` get separate keys naturally.

## Why fold the graph fix into slice (j)

I considered a separate `slice-j-fix-graph` followed by `slice-j-detail-routes`. Decided against: the fix is small (~30 LOC in transport.go), the detail-route work *needs* the fix, and the test that proves the fix is the same test that exercises detail routing. Splitting wastes a commit.
