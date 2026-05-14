# Slice (d) — React Shell Rendering Engine

> Companion design doc to [DESIGN.md](DESIGN.md). Slice (d) ships the JavaScript runtime that consumes the contract endpoints slice (a)+(b)+(c) shipped, turning declarative manifests into a working admin UI.

## Context

The contract is fully exercisable from Go and the probe CLI but has no browser consumer. Slice (d) builds the React-based shell that fetches a route's filtered graph from `POST /api/dashboard/v1` (kind=graph), maps each `intent` node to a registered React component, recursively expands slots, runs the named queries each intent declares as data sources, and subscribes via the multiplexed SSE stream for live data. CSRF tokens are fetched on demand from `GET /csrf` and refreshed on 401. Idempotency keys are generated for every command. Auth is handled out-of-band by whatever middleware the deployment already runs (the shell just sends cookies + Authorization headers along with the contract envelope).

Slice (d) is the **runtime**: graph fetcher, slot renderer, query client, SSE multiplex consumer, escape-hatch loader, plus exactly **one** concrete intent component (`metric.counter`) so the pilot's `/metrics/live` route renders end-to-end. The full v1 vocabulary (`resource.list`, `resource.detail`, `dashboard.grid`, `form.edit`, `audit.tail`) lands in slice (e). Until then, unknown intents render a graceful fallback (`<UnknownIntent intent={name}/>`) instead of erroring.

Slice (d) also adds the Go-side glue: `//go:embed`-bundles the production build into the dashboard binary and serves the SPA from `/dashboard/contract/static/*`. No new external service or hosting plane.

## Architecture Decisions (locked in — autonomy mode)

| Decision | Choice | Rationale |
|---|---|---|
| Location | `extensions/dashboard/contract/shell/` (TS/React project), built into `dist/` and embedded via `//go:embed dist/*` in the dashboard extension | Co-located with the contract code it consumes; single Go deployment artifact; mirrors the slice (a)/(b)/(c) layout pattern. |
| Language | TypeScript strict mode | A runtime that consumes a typed contract benefits more from compile-time checking than any other slice. |
| Build tool | Vite 5+ | Fast cold builds, ESM-native, minimal config, well-supported in the React ecosystem. Single config file. |
| Package manager | pnpm | Already used by `docs/`; consistent with the rest of the repo. |
| Framework | React 18 (concurrent features for streaming SSE updates without jank) | Industry default; broad component ecosystem. |
| Routing | React Router v6.4+ data router pattern | Routes from the contract YAML are mapped to a single dynamic route in the SPA; the data router's loader pattern matches the contract's `kind=graph` fetch lifecycle. |
| Server state | React Query (TanStack Query v5) | First-class `staleTime` / cache invalidation aligns with the contract's per-intent `cache.staleTime` declarations and the `meta.invalidates` hint in command responses. |
| Local UI state | Zustand | Hooks-native, zero boilerplate, ~1KB. Used for transient UI state (modals, drawers, selected rows) that doesn't belong in server state. |
| CSS | Tailwind CSS v3 (NOT v4 yet — toolchain stability) | Atomic, no runtime cost, consistent with the docs site's eventual stack. v3 picked because v4 still has stabilization friction with Vite. |
| Component primitives | None for v1; build minimal headless components inline. Optional shadcn/ui adoption is a follow-on. | Avoids a new dependency surface during the runtime's first version. Slice (e) revisits if any vocabulary intent benefits. |
| Testing | Vitest + React Testing Library + Mock Service Worker for HTTP/SSE stubbing | Vitest is Vite-native, fast, ESM-native. MSW handles the contract endpoint boundary cleanly without spinning up a real Go server in JS tests. |
| Bundle target | ES2022, modern browsers (no IE / no legacy polyfills) | Internal admin tool; we control the audience. |
| Auth model | Cookies + headers travel with the contract envelope unchanged. The shell does NOT manage user login. The `auth/dashauth` middleware on the Go side populates `UserInfo`; the shell reads its identity from `GET /api/dashboard/v1/principal` (a new tiny endpoint added by this slice). | Login is out-of-band. The shell just shows the user who they are (for the topbar). |
| CSRF token storage | In-memory, fetched lazily on first command, refreshed on 401 | Stateless HMAC tokens (per slice b) don't need persistence. Memory storage avoids XSS-readable localStorage. |
| Idempotency keys | Auto-generated per command (`crypto.randomUUID()`); user can pass an explicit key via the action descriptor for retry-safe operations | Default-on prevents double-submits; explicit override supports finer control when retry semantics matter. |
| SSE consumer | Single EventSource per page, multiplex demuxes by SSE `event:` field (= subscriptionID) | Matches the broker's wire shape from slice (a). One connection per page, many subscriptions. |
| SPA routing | React Router catch-all under `/dashboard` rewriting to the shell's index.html (Go side) | Standard SPA pattern; the Go static handler serves index.html for any path that doesn't resolve to a static asset. |
| Bundle delivery | Production build to `extensions/dashboard/contract/shell/dist/`; Go `//go:embed dist/*` includes it in the binary; static handler at `/dashboard/contract/static/*` | One binary, no separate hosting; CI rebuilds JS before Go build. |
| Dev mode | Vite dev server on `:5173` proxies `/api/dashboard/*` to the running Go dashboard on `:8080` | Hot reload during development without rebuilding the Go binary. |
| Bundle size budget | <250KB gzipped initial, <500KB total | Admin dashboards don't need a 1MB cold start. Slice (e) expansion stays within budget. |
| Browser support | Chrome 110+, Firefox 110+, Safari 16+ (Sept 2022 baseline) | All support ESM, modern CSS, EventSource with proper headers, and `crypto.randomUUID()`. |

## Scope

### In scope (this slice)
- TypeScript/React project at `extensions/dashboard/contract/shell/` with full toolchain (Vite, TS, Tailwind, React Query, React Router, Zustand, Vitest, MSW).
- Contract client SDK: typed `Request`/`Response` types matching the Go envelope, send functions for `query`/`command`/`graph`, automatic CSRF handling, error envelope handling, idempotency-key generation.
- SSE multiplex client: opens one EventSource per page, sends control messages, demuxes events by subscription ID, exposes a hook (`useSubscription(intent, params)`).
- Intent component registry: name → React component map; lazy-imported components for code-splitting; fallback for unknown intents.
- Slot renderer: walks the graph response, recursively renders, evaluates `enabledWhen` (visibility is server-side per slice a's design).
- Page shell: renders nav + main outlet + topbar with principal info.
- One concrete intent component: `metric.counter` — proves the renderer works end-to-end against the pilot's `/metrics/live` page.
- Fallback components: `UnknownIntent` (placeholder when intent name isn't registered), `LoadingNode`, `ErrorNode`.
- Auth/principal hook: fetches `GET /api/dashboard/v1/principal` once on mount, exposes via Zustand store.
- Go side: `principal` endpoint, embed directive, static + SPA fallback handler, contributor route forwarding.
- Tests: unit tests for the client SDK, the slot renderer, the intent registry, the SSE consumer (with MSW + EventSource stub).
- Integration smoke: a full Vitest test that mounts a route's graph, asserts `metric.counter` renders with subscription data.

### Out of scope (later slices)
- **Slice (e)**: concrete components for `resource.list`, `resource.detail`, `dashboard.grid`, `form.edit`, `audit.tail`, `action.button`, `form.field`, `form.edit`, etc.
- **Slice (f)**: contributor migration + templ retirement — the legacy templ pages keep serving until (e)+(f) are ready.
- Component design polish (shadcn, lucide icons beyond the bare minimum, animations, transitions).
- Browser E2E tests via Playwright — Vitest + MSW covers the runtime; browser tests are a follow-on.
- A login/auth UI — auth stays middleware-driven.
- Internationalization, dark/light theme runtime switching, accessibility audit beyond the React Testing Library defaults.
- iframe escape-hatch implementation — design ships, but no concrete iframe-rendered intent until a contributor needs one.

## Components

### Project layout

```
extensions/dashboard/contract/shell/
  package.json
  pnpm-lock.yaml             # generated
  tsconfig.json
  tsconfig.node.json         # for vite.config.ts
  vite.config.ts
  tailwind.config.ts
  postcss.config.js
  index.html                 # entry HTML, includes <div id="root">
  vitest.config.ts
  .gitignore                 # excludes dist/, node_modules/, .vite cache

  src/
    main.tsx                 # React entry, mounts <App/>
    App.tsx                  # routing + providers
    index.css                # tailwind directives + minimal globals

    contract/
      types.ts               # TS mirrors of Go envelope: Request, Response, ErrorResponse, StreamEvent, etc.
      client.ts              # send(envelope), CSRF, idempotency, error decoding
      sse.ts                 # SubscriptionMux: one EventSource per page, demux by subscriptionID
      hooks.ts               # useGraph, useQuery, useCommand, useSubscription

    runtime/
      registry.ts            # IntentRegistry: name -> React component
      renderer.tsx           # GraphRenderer: walks graph nodes, dispatches to registry
      slots.tsx              # SlotRenderer
      fallbacks.tsx          # UnknownIntent, LoadingNode, ErrorNode

    auth/
      principal.ts           # Zustand store + fetcher

    intents/
      page.shell.tsx         # registered as "page.shell"
      metric.counter.tsx     # registered as "metric.counter"
      register.ts            # registers built-in intents at app startup

  test/
    setup.ts                 # MSW config, Vitest global setup
    contract.test.ts         # client SDK round-trip
    sse.test.ts              # SubscriptionMux dispatch
    renderer.test.tsx        # slot expansion, registry lookup, fallback path
    smoke.test.tsx           # mounts a page, verifies render
```

### Build pipeline

`pnpm build` runs `vite build` → emits to `extensions/dashboard/contract/shell/dist/`. The Go side embeds via:

```go
// extensions/dashboard/contract/shell/embed.go
package shell

import "embed"

//go:embed all:dist
var Dist embed.FS
```

The dashboard extension's `registerRoutes` mounts a static handler at `/dashboard/contract/static/*` that reads from `shell.Dist` and falls back to `dist/index.html` for any path not matching a static asset (SPA routing). The static handler is added in this slice.

The SPA URL space:
- `/dashboard/contract/static/*` — static assets (JS, CSS, fonts, images), browser-cached aggressively (immutable hashes from Vite).
- `/dashboard/contract/app/*` — index.html SPA entry, all routes under here render the React shell. The shell's React Router dispatches.

Today's legacy `/dashboard` paths and `/dashboard/contract/extensions`, `/dashboard/contract/services`, `/dashboard/contract/metrics/live` (pilot routes) are unchanged; the React shell at `/dashboard/contract/app/*` is the new entry.

### Contract types (TS mirror of Go envelope)

```typescript
// shell/src/contract/types.ts
export type Kind = "graph" | "query" | "command" | "subscribe";

export interface Request {
  envelope: "v1";
  kind: Kind;
  contributor: string;
  intent: string;
  intentVersion?: number;
  payload?: unknown;
  params?: Record<string, unknown>;
  context: { route: string; correlationID: string };
  csrf?: string;
  idempotencyKey?: string;
}

export interface Response<T = unknown> {
  ok: true;
  envelope: "v1";
  kind: Kind;
  data: T;
  meta: ResponseMeta;
}

export interface ResponseMeta {
  intentVersion?: number;
  deprecation?: { intentVersion: number; removeAfter: string };
  cacheControl?: { staleTime?: string };
  invalidates?: string[];
}

export interface ErrorResponse {
  ok: false;
  envelope: "v1";
  error: ContractError;
}

export interface ContractError {
  code: string;
  message?: string;
  details?: Record<string, unknown>;
  retryable?: boolean;
  correlationID?: string;
  redactions?: string[];
}

// Graph types — mirror Go's GraphNode minus server-internal fields
export interface GraphNode {
  intent: string;
  title?: string;
  route?: string;
  data?: DataBinding;
  props?: Record<string, unknown>;
  slots?: Record<string, GraphNode[]>;
  enabledWhen?: Predicate;
  op?: string;
  payload?: Record<string, unknown>;
  // server filters visibleWhen before sending — never appears in shell graph
}

export interface DataBinding {
  queryRef?: string;
  intent?: string;
  params?: Record<string, unknown>;
}

// StreamEvent (SSE wire shape)
export interface StreamEvent<T = unknown> {
  intent: string;
  mode: "replace" | "append" | "snapshot+delta";
  payload: T;
  seq: number;
}
```

### Contract client (`shell/src/contract/client.ts`)

```typescript
export class ContractClient {
  constructor(private baseURL: string = "/api/dashboard/v1") {}

  async send<T>(req: Omit<Request, "envelope" | "context"> & Partial<Pick<Request, "context">>): Promise<T> {
    // 1. inject envelope: "v1" and context (correlationID = crypto.randomUUID(), route from window.location)
    // 2. for kind=command, attach CSRF token (lazily fetched + cached) and idempotencyKey (auto-generated unless caller provides one)
    // 3. POST to baseURL with Content-Type: application/json
    // 4. parse the response envelope:
    //    - ok=true: return data as T
    //    - ok=false: throw ContractError with the wire details attached
    // 5. on 401 (CSRF expired): refresh token, retry once
  }

  // helpers per kind for ergonomics:
  async query<T>(contributor: string, intent: string, payload?: unknown, params?: Record<string, unknown>): Promise<T>
  async command<T>(contributor: string, intent: string, payload?: unknown, opts?: { idempotencyKey?: string }): Promise<T>
  async graph(contributor: string, route: string): Promise<GraphNode>
}
```

### SSE multiplex (`shell/src/contract/sse.ts`)

```typescript
export class SubscriptionMux {
  private es: EventSource | null = null;
  private streamID: string | null = null;
  private subs = new Map<string, (ev: StreamEvent) => void>();

  // open() lazily on first subscribe(); reconnect with backoff on close
  open(): Promise<void>

  // returns an unsubscribe() function
  subscribe<T>(contributor: string, intent: string, params: Record<string, unknown>, onEvent: (ev: StreamEvent<T>) => void): () => void

  close(): void
}
```

The mux dispatches by SSE `event: <subscriptionID>` field, parses the JSON `data:` payload into `StreamEvent`, and calls the registered handler. On `event: hello`, captures the streamID for control messages. Reconnect strategy: exponential backoff to 30s max, replay outstanding subscriptions automatically.

### Intent registry (`shell/src/runtime/registry.ts`)

```typescript
export interface IntentComponentProps<TData = unknown, TProps = Record<string, unknown>> {
  node: GraphNode;
  data?: TData;
  props: TProps;
  slots: Record<string, GraphNode[]>;
}

export type IntentComponent = React.ComponentType<IntentComponentProps>;

export class IntentRegistry {
  private byName = new Map<string, IntentComponent>();
  register(name: string, component: IntentComponent): void
  resolve(name: string): IntentComponent | undefined
}
```

The registry is created at app startup (in `intents/register.ts`) and threaded via React context (`<IntentRegistryProvider value={registry}>`).

### Slot renderer (`shell/src/runtime/renderer.tsx`)

```typescript
export function GraphRenderer({ node }: { node: GraphNode }) {
  const registry = useIntentRegistry();
  const Component = registry.resolve(node.intent);
  if (!Component) {
    return <UnknownIntent intent={node.intent} />;
  }
  // resolve data binding (if node.data is a queryRef, look it up in queries; if inline, run the query)
  const data = useNodeData(node.data);
  return <Component node={node} data={data.value} props={node.props ?? {}} slots={node.slots ?? {}} />;
}
```

Each component decides which slots to render where (e.g., `<SlotRenderer slot="main" slots={slots} />` to render that slot's children).

### `metric.counter` (`shell/src/intents/metric.counter.tsx`)

The single concrete component this slice ships. Wraps the data in a card, displays the value with a label. If the data binding is a subscription intent (replace mode), uses `useSubscription`; if it's a query, uses `useQuery`. The component is declarative — the manifest tells it what to fetch.

### `page.shell` (`shell/src/intents/page.shell.tsx`)

Top-level page wrapper. Renders the topbar (with principal info), nav (from the registry's nav metadata exposed in another graph fetch), and the main slot. Most of the dashboard's structure flows through this.

### Auth principal endpoint

New Go-side route `GET /api/dashboard/v1/principal` returns:

```json
{
  "subject": "alice",
  "displayName": "Alice Smith",
  "roles": ["admin"],
  "scopes": ["users.read", "users.write"]
}
```

Reads from the request context's `dashauth.UserFromContext`. When unauthenticated, returns 401. The shell's principal store fetches once on mount; failure is surfaced as "not signed in" in the topbar.

## Files Affected

### New (this slice)

```
extensions/dashboard/contract/shell/
  package.json, pnpm-lock.yaml, tsconfig.json, tsconfig.node.json,
  vite.config.ts, tailwind.config.ts, postcss.config.js,
  vitest.config.ts, index.html, .gitignore, README.md

  src/main.tsx, src/App.tsx, src/index.css
  src/contract/{types.ts, client.ts, sse.ts, hooks.ts}
  src/runtime/{registry.ts, renderer.tsx, slots.tsx, fallbacks.tsx}
  src/auth/principal.ts
  src/intents/{page.shell.tsx, metric.counter.tsx, register.ts}

  test/{setup.ts, contract.test.ts, sse.test.ts, renderer.test.tsx, smoke.test.tsx}

  embed.go              # //go:embed directive

extensions/dashboard/handlers/
  principal.go          # GET /api/dashboard/v1/principal handler

extensions/dashboard/contract/SLICE_D_DESIGN.md     # this file
extensions/dashboard/contract/SLICE_D_PLAN.md       # produced via writing-plans skill
```

### Modified

- `extensions/dashboard/extension.go` — register the static SPA handler under `/dashboard/contract/static/*` and the SPA fallback at `/dashboard/contract/app/*`; register the principal endpoint.
- `extensions/dashboard/handlers/api.go` — none (principal lives in its own file).
- `Makefile` (if exists) or new `extensions/dashboard/contract/shell/Makefile` — `pnpm install && pnpm build` step that runs before `go build` for the dashboard binary. CI pipeline likewise.

### Reused

- `transport.NewHandler` (slice a) — the shell's contract client posts to the existing endpoint.
- `transport.StreamBroker` (slice a) — the shell's SubscriptionMux opens a connection to the existing stream.
- `dispatcher.Dispatcher` (slice c) — handles the actual dispatch on the Go side.
- `pilot.Register` (slice c) — keeps registering its three pages; the shell renders them via the new path.
- `dashauth.UserFromContext` — the new principal handler reads from this.

## Verification

1. **JS unit tests** under `shell/test/`:
   - **Contract client** round-trips: query, command (with auto-CSRF + auto-idempotency), graph fetch, error envelope decode.
   - **CSRF refresh on 401**: first request 401s; the client fetches `/csrf`, retries; second request succeeds.
   - **SubscriptionMux**: subscribe → control message sent; events arrive → handler invoked with parsed StreamEvent; unsubscribe → control message sent; close → all subscriptions cleared; reconnect → outstanding subs replayed.
   - **Intent registry**: register → resolve hits; unknown intent → resolves to undefined.
   - **GraphRenderer**: registered intent renders its component; unknown intent renders UnknownIntent fallback; nested slots resolve recursively.
   - **`metric.counter`**: renders title, observes a `metrics.summary` subscription event, updates the displayed value.

2. **Integration smoke** (`shell/test/smoke.test.tsx`):
   - MSW stubs `POST /api/dashboard/v1` to return a graph for `/metrics/live` matching the pilot fixture.
   - MSW stubs the SSE stream with a fake event source that emits one `metrics.summary` event.
   - Mounts `<App/>` at the route, asserts `metric.counter` renders with the event payload's `totalMetrics` value.

3. **Go side**: existing 226+ tests stay green. New `principal_test.go` covers the new endpoint (200 with UserInfo present, 401 without).

4. **Manual smoke** (post-merge):
   - `pnpm dev` in shell/, dashboard running, browse to `http://localhost:5173/dashboard/contract/app/metrics/live`. Counter widget renders, refreshes every 5s via SSE.
   - `pnpm build` then `go build && ./forge`, browse to `http://localhost:8080/dashboard/contract/app/metrics/live`. Same result, served from embedded assets.

## Out of Scope — Future Slices

- **Slice (e)**: rest of the v1 intent vocabulary (`resource.list`, `resource.detail`, `dashboard.grid`, `form.edit`, `audit.tail`, `action.button`, `action.menu`, `action.divider`, `form.field`).
- **Slice (f)**: contributor migration to the new contract + retire templ + remove HTML-fragment proxying.
- iframe escape-hatch component for novel UX (design from slice a is preserved; first contributor that needs one prompts the slice).
- Component-library adoption (shadcn/ui or similar) — first reach for this when a vocabulary intent benefits.
- Playwright browser E2E.
- Internationalization, dark/light theme switching, advanced accessibility.
- A login/registration UI — out-of-band (auth middleware on the Go side handles it).
