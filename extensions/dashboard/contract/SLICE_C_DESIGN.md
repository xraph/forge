# Slice (c) — Dispatcher Infrastructure & Pilot Migration

> Companion design doc to [DESIGN.md](DESIGN.md). Slice (c) builds on slice (a)'s contract package; it is the first slice that produces a *working* end-to-end contract round-trip.

## Context

Slice (a) shipped the contract type system, registry, transport, and wiring, but the runtime path stops at `NilDispatcher{}` — every request returns `UNAVAILABLE`. Slice (c) replaces that with a real dispatcher, defines how contributors register handler functions against intents, and migrates a small but real surface (Extensions list, Services list/detail, a CPU metric subscription) so the contract is exercised end-to-end. The pilot also flushes out concrete ergonomic and integration issues that should inform the React shell (slice (d)) and remaining migrations (slice (f)).

This is *not* a security or audit completeness pass. CSRF token validation, the chronicle integration, and OpenTelemetry spans are explicit non-goals — slice (b) owns those.

## Scope

**In scope (this spec):**
- A `Dispatcher` implementation that backs the existing `transport.Dispatcher` interface from slice (a).
- A registration API contributors call to bind their handlers to intents.
- Generic helpers (`RegisterQuery`, `RegisterCommand`, `RegisterSubscription`) for type-safe ergonomic handlers.
- A `SubscriptionSource` adapter that fulfils slice (a)'s broker interface.
- A `MetricsEmitter` interface + noop default — observability hook only, no Prometheus wiring.
- Three migrated intents wired against the dashboard's existing `collector.DataCollector` plumbing:
  - `extensions.list` (query)
  - `services.list` (query) and `services.detail` (query, used in a detail drawer slot)
  - `metrics.cpu` (subscription, `replace` mode)
- An embedded YAML manifest in the dashboard package describing the pilot contributor.
- End-to-end test that drives the contract HTTP and SSE endpoints with the wired dispatcher.

**Out of scope (other slices):**
- Real CSRF validation (slice (b))
- Real audit emission to chronicles (slice (b))
- OpenTelemetry tracing (later)
- Prometheus integration for `MetricsEmitter` (slice (b))
- React shell consuming the contract endpoints (slice (d))
- Removal of legacy templ-rendered Extensions / Services pages (slice (f))
- Migration of any external contributor (e.g., the streaming extension) — explicitly deferred

## Design Decisions

| Decision | Choice |
|---|---|
| Dispatcher API layers | (a) function-table foundation + (b) interface-based registration via `ContractHandlers()` + (c) generic typed wrappers — all three, layered |
| Handler signature | Narrow + `*Result`: `func(ctx, payload, params, p) (*Result, error)` where `Result{Data, ExtraInvalidates, CacheOverride}` lets handlers influence response meta when needed without forcing every handler to construct one |
| Error model | Handler returns `*contract.Error` → propagated verbatim; any other error → wrapped as `CodeInternal` with the original chained for server-side logs; nil error + nil Data is valid (`{ok: true, data: null}`) |
| Subscription handler | `func(ctx, params, p) (<-chan StreamEvent, func(), error)` — channel + stop func; ctx cancellation = handler should stop emitting and close; stop = force-stop hook the broker calls on disconnect |
| Subscription registration | Separate from query/command: `disp.RegisterSubscription(c, i, v, subHandler)` indexed by `(contributor, intent, version)` like ordinary handlers |
| Observability | `MetricsEmitter` interface emits latency + error-count per dispatch; noop default; Prometheus impl is slice (b)'s problem |
| Manifest delivery | `//go:embed` of a YAML file shipped in the dashboard package — single source of truth for the pilot contributor's intents and graph |
| Migration coexistence | Legacy templ Extensions/Services pages continue working at `/dashboard/extensions`, `/dashboard/services`. New contract surface lives at `/dashboard/contract/...`. Both read from the same `collector.DataCollector`. Slice (f) retires templ |
| Pilot route prefix | `/{dashboardBase}/contract/{contributor}/...` so the legacy and contract paths never collide. The contract contributor's `name` is `"core-contract"` to disambiguate from the legacy `core` contributor |

## The Dispatcher

### Package layout

```
extensions/dashboard/contract/dispatcher/
  dispatcher.go      # Dispatcher struct, Register, Dispatch, SubscriptionSource adapter
  handler.go         # Handler, Result types
  generic.go         # RegisterQuery[I,O], RegisterCommand[I,O], RegisterSubscription[I,E]
  metrics.go         # MetricsEmitter interface, NoopMetricsEmitter
  dispatcher_test.go
  generic_test.go
  metrics_test.go
```

The dispatcher lives in a sub-package so the contract package itself stays free of dispatch logic and the `transport` package depends only on the abstract interface.

### Public surface

```go
package dispatcher

// Dispatcher is the concrete implementation of transport.Dispatcher and
// transport.SubscriptionSource. Contributors register handlers against
// (contributor, intent, version) keys; the dispatcher routes requests at runtime.
type Dispatcher struct { /* unexported */ }

func New(metrics MetricsEmitter) *Dispatcher

// Function-table registration (layer a).
func (d *Dispatcher) Register(contributor, intent string, version int, h Handler) error
func (d *Dispatcher) RegisterSubscription(contributor, intent string, version int, h SubscriptionHandler) error

// Interface registration (layer b) — called once per contributor by the wire-up code.
func (d *Dispatcher) RegisterContributor(c Contributor) error

// Slice (a) interfaces — the dispatcher implements both.
func (d *Dispatcher) Dispatch(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error)
func (d *Dispatcher) Subscribe(ctx context.Context, p contract.Principal, contributor string, intent contract.Intent, params map[string]contract.ParamSource) (<-chan contract.StreamEvent, func(), error)

// Handler types.
type Handler func(ctx context.Context, payload json.RawMessage, params map[string]any, p contract.Principal) (*Result, error)

type SubscriptionHandler func(ctx context.Context, params map[string]any, p contract.Principal) (<-chan contract.StreamEvent, func(), error)

type Result struct {
    Data             json.RawMessage
    ExtraInvalidates []string
    CacheOverride    *contract.CacheHint
}

// Layer (b): contributors implement this interface. RegisterContributor walks the
// returned maps and calls Register/RegisterSubscription internally.
type Contributor interface {
    Name() string
    Handlers() map[IntentRef]Handler
    Subscriptions() map[IntentRef]SubscriptionHandler
}

type IntentRef struct {
    Intent  string
    Version int
}
```

### Generic helpers (layer c)

```go
package dispatcher

// RegisterQuery wraps a typed handler. Decoding the payload, marshalling the
// output, and constructing the Result are handled by the wrapper.
func RegisterQuery[I, O any](d *Dispatcher, contributor, intent string, version int,
    fn func(ctx context.Context, in I, p contract.Principal) (O, error)) error

func RegisterCommand[I, O any](d *Dispatcher, contributor, intent string, version int,
    fn func(ctx context.Context, in I, p contract.Principal) (O, error)) error

func RegisterSubscription[P, E any](d *Dispatcher, contributor, intent string, version int,
    fn func(ctx context.Context, in P, p contract.Principal) (<-chan E, func(), error)) error
```

Implementation: each helper allocates a `Handler` (or `SubscriptionHandler`) closure that JSON-decodes the typed input from `payload`, calls the inner typed function, and JSON-encodes the output back into `Result.Data`. Subscription wrappers spawn a small pump goroutine that JSON-encodes each typed event before forwarding into the broker's channel.

If a contributor wants influence over `ExtraInvalidates` or `CacheOverride`, they bypass the helper and use `d.Register` directly. The helpers are sugar, not a wall.

### Error mapping at dispatch time

```go
data, meta, err := h(ctx, req.Payload, paramsMap, principal)
switch {
case err == nil:
    // success path
case errors.As(err, &contractErr):
    // *contract.Error — propagate verbatim
case errors.Is(err, context.Canceled):
    // map to CodeUnavailable, retryable=true
default:
    // wrap as CodeInternal
    log.Printf("contract dispatch error: %v", err) // server-side detail
    err = &contract.Error{Code: contract.CodeInternal, Message: "internal error"}
}
```

The Wire boundary always returns `*contract.Error` shapes; original error chains never leak to clients.

### Concurrency

- `Register*` calls hold a `sync.Mutex` while mutating the handler tables; `Dispatch`/`Subscribe` acquire `sync.RWMutex.RLock`. After startup, registration is rare and dispatch is hot.
- A registration after first `Dispatch` is allowed (some contributors may register late) but is not optimised.
- Subscription handlers spawn their own goroutines for event production. The dispatcher only routes the resulting channel to the broker; goroutine ownership stays with the handler.

## Pilot Manifest

### File: `extensions/dashboard/contract/pilot/manifest.yaml`

```yaml
schemaVersion: 1
contributor:
  name: core-contract
  envelope:
    supports: [v1]
    preferred: v1
  capabilities: [dashboard.read]

queries:
  extensionList:
    intent: extensions.list
    cache: { staleTime: 10s }
  serviceList:
    intent: services.list
    cache: { staleTime: 5s }

intents:
  - name: extensions.list
    kind: query
    version: 1
    capability: read
    schema:
      output: { extensions: ExtensionInfo[] }

  - name: services.list
    kind: query
    version: 1
    capability: read
    schema:
      output: { services: ServiceInfo[] }

  - name: services.detail
    kind: query
    version: 1
    capability: read
    schema:
      input:  { name: string }
      output: ServiceDetail

  - name: metrics.cpu
    kind: subscription
    version: 1
    capability: read
    mode: replace
    schema:
      output: { cpuPercent: number, ts: int64 }

graph:
  - route: /extensions
    intent: page.shell
    title: Extensions
    nav: { group: Operations, icon: package, priority: 20 }
    slots:
      main:
        - intent: resource.list
          data: queries.extensionList
          props:
            columns: [name, version, status]

  - route: /services
    intent: page.shell
    title: Services
    nav: { group: Operations, icon: server, priority: 21 }
    slots:
      main:
        - intent: resource.list
          data: queries.serviceList
          props:
            columns: [name, status, uptime]
          slots:
            detailDrawer:
              - intent: resource.detail
                data:
                  intent: services.detail
                  params: { name: { from: parent.name } }

  - route: /metrics/live
    intent: page.shell
    title: Live Metrics
    nav: { group: Operations, icon: activity, priority: 22 }
    slots:
      main:
        - intent: dashboard.grid
          slots:
            widgets:
              - intent: metric.counter
                title: CPU %
                data:
                  intent: metrics.cpu
```

### Routes and namespacing

The pilot contributor name is `core-contract` (distinct from the existing legacy `core` contributor). Per slice (a)'s namespace-by-default rule, its routes mount at:

- `/dashboard/contract/core-contract/extensions`
- `/dashboard/contract/core-contract/services`
- `/dashboard/contract/core-contract/metrics/live`

Legacy `core` continues to serve `/dashboard/extensions`, `/dashboard/services`, etc. Until slice (f) retires templ, both are reachable.

The `Root: true` flag on the pilot's intents is **not** set — the pilot lives under its own namespace, which is exactly what we want for an opt-in test surface that won't collide with legacy URLs.

## Pilot Handlers

Lives at `extensions/dashboard/contract/pilot/handlers.go`. Implementations:

- `extensions.list`: read from `collector.DataCollector.GetExtensions()`. Output: `{extensions: []ExtensionInfo}` where `ExtensionInfo` is the existing collector type.
- `services.list`: read from `DataCollector.GetServices()`.
- `services.detail`: read from `DataCollector.GetServiceDetail(name)`. Returns `nil, &contract.Error{Code: CodeNotFound}` when the service isn't registered.
- `metrics.cpu`: subscription handler that polls `DataCollector.GetSnapshot()` at a 5s interval and emits `replace`-mode events with `{cpuPercent, ts}`.

Each handler uses the layer-(c) generic wrapper for ergonomics:

```go
package pilot

func Register(d *dispatcher.Dispatcher, c *collector.DataCollector) error {
    if err := dispatcher.RegisterQuery(d, "core-contract", "extensions.list", 1,
        func(ctx context.Context, _ struct{}, _ contract.Principal) (ExtensionsList, error) {
            return ExtensionsList{Extensions: c.GetExtensions()}, nil
        }); err != nil {
        return err
    }
    // ... services.list, services.detail similarly ...
    return dispatcher.RegisterSubscription(d, "core-contract", "metrics.cpu", 1, cpuSub(c))
}

func cpuSub(c *collector.DataCollector) func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan CPUEvent, func(), error) {
    return func(ctx context.Context, _ struct{}, _ contract.Principal) (<-chan CPUEvent, func(), error) {
        ch := make(chan CPUEvent, 4)
        ticker := time.NewTicker(5 * time.Second)
        go func() {
            defer close(ch)
            defer ticker.Stop()
            for {
                select {
                case <-ctx.Done():
                    return
                case <-ticker.C:
                    snap := c.GetSnapshot()
                    select {
                    case ch <- CPUEvent{CPUPercent: snap.CPU, TS: time.Now().Unix()}:
                    case <-ctx.Done():
                        return
                    }
                }
            }
        }()
        return ch, func() {}, nil
    }
}
```

The empty `func(){}` stop function is a no-op because the goroutine cleanup is driven by ctx. We provide it explicitly to keep the contract's broker shape uniform — slice (b) may want to add a fast-path stop for resource cleanup.

## Wire-up changes in the dashboard extension

`extensions/dashboard/extension.go`:

- Replace `transport.NilDispatcher{}` with the real dispatcher.
- Construct the dispatcher once at extension startup; pass the same instance into both `transport.NewHandler` (for HTTP) and a new `streamBroker` instantiation (since the dispatcher implements `SubscriptionSource`, the broker can finally be wired).
- After the contract registry is set up, load the embedded pilot YAML, validate it, register it with the contract registry, and register the pilot handlers against the dispatcher.

Approximate diff sketch:

```go
import _ "embed"

//go:embed contract/pilot/manifest.yaml
var pilotManifestYAML []byte

// in NewExtension constructor, after wardenRegistry init:
disp := dispatcher.New(dispatcher.NoopMetricsEmitter{})
// streamBroker wires now that we have a SubscriptionSource:
broker := transport.NewStreamBroker(contractRegistry, wardenRegistry, disp)

// ... (existing init continues) ...
e.dispatcher = disp
e.streamBroker = broker

// in Start (or wherever OnRegister callbacks fire), after the registry is built:
m, err := loader.Load(bytes.NewReader(pilotManifestYAML), "pilot/manifest.yaml")
// validate, register with contract registry, register handlers via pilot.Register(disp, e.collector)
```

`handleContractPOST` swaps `transport.NilDispatcher{}` for `e.dispatcher`. The broker registration block in `registerRoutes` already exists (slice a) and starts working as soon as `streamBroker` is non-nil.

## Files Affected

### New

```
extensions/dashboard/contract/dispatcher/
  dispatcher.go
  handler.go
  generic.go
  metrics.go
  dispatcher_test.go
  generic_test.go
  metrics_test.go

extensions/dashboard/contract/pilot/
  manifest.yaml
  handlers.go
  types.go              # ExtensionsList, ServicesList, ServiceDetail, CPUEvent
  pilot.go              # Register(disp, collector) entry point
  handlers_test.go
  pilot_e2e_test.go     # spins up the contract handler and exercises all four intents
```

### Modified

- `extensions/dashboard/extension.go` — wire the dispatcher, broker, pilot registration; replace `NilDispatcher{}` callsites.

### Reused (do not duplicate)

- `transport.Dispatcher` interface and `transport.NewHandler` from slice (a).
- `transport.SubscriptionSource` interface and `transport.StreamBroker` from slice (a).
- `contract.Registry`, `contract.WardenRegistry`, `loader.Load`, `loader.Validate` from slice (a).
- `collector.DataCollector` and its `GetExtensions`/`GetServices`/`GetServiceDetail`/`GetSnapshot` methods (existing dashboard internals).
- `contract.NewLogAuditEmitter(os.Stdout)` continues to back command audit.

## Verification

1. **Unit tests** under `dispatcher/`:
   - Register / Dispatch round-trip for query, command, and subscription kinds.
   - Generic helper round-trip — typed input/output verified to encode/decode through the wire layer.
   - Error mapping table-driven: handler returns `*contract.Error`, plain error, `context.Canceled`; expected wire codes asserted.
   - Concurrent registration + dispatch race check (`go test -race`).
   - Metrics emission verified via a stub `MetricsEmitter`.

2. **Pilot handler tests** under `pilot/`:
   - Each handler called with a fixture `DataCollector` produces the expected output.
   - `services.detail` for an unknown name returns `*contract.Error{Code: CodeNotFound}`.
   - `metrics.cpu` subscription emits an event after a tick (use a 50ms test ticker via dependency injection).

3. **End-to-end test** `pilot_e2e_test.go`:
   - Stand up: contract registry, warden registry, dispatcher, transport handler, stream broker — all in-process.
   - Load pilot YAML, validate, register, register handlers.
   - Drive `POST /api/dashboard/v1` with `kind=query` for `extensions.list` and assert the envelope shape + the data shape.
   - Drive `kind=graph` for `/services` and assert the filtered tree includes the detail-drawer slot.
   - Open SSE stream, subscribe to `metrics.cpu`, assert at least one event arrives within 200ms (with the test-injected ticker).
   - Run with `-race` to flush any subscription goroutine ordering issues.

4. **Probe CLI manual smoke** (not automated, but documented):
   ```bash
   go run ./cmd/dashboard-contract-probe \
     -base=http://localhost:8080 \
     -kind=query -contributor=core-contract -intent=extensions.list
   ```
   Expected: HTTP 200 with `{"ok":true, "data":{"extensions":[...]}}`.

## Out of Scope — Future Slices

- **Slice (b)** — security: real CSRF middleware integration, idempotency-key persistence (so retried commands are deduped), chronicle integration for `AuditEmitter`, Prometheus impl for `MetricsEmitter`, OpenTelemetry tracing wrapper around `Dispatcher.Dispatch`.
- **Slice (d)** — React shell rendering engine: consumes the contract endpoints this slice exposes. The pilot's three pages become real renderable UIs.
- **Slice (e)** — built-in intent vocabulary v1: concrete React implementations of `resource.list`, `resource.detail`, `dashboard.grid`, `metric.counter`, etc.
- **Slice (f)** — migration of remaining contributors and removal of templ. Once the React shell is real, the legacy templ pages get retired.
