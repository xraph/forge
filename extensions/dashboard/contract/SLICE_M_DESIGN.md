# Slice (m) — Remote contract contributors

**Status:** Active
**Branch:** `dashboard-contract-slice-a` (continuing the stack — slices a/b/c/d/e/f/h/i/j/k/l already pushed)

## Context

The legacy templ-based dashboard supports remote contributors: services tagged
`forge-dashboard-contributor` are discovered via Forge's service registry, their
manifest is fetched from `GET /_forge/dashboard/manifest`, and the dashboard
proxies page/widget/settings render requests through to the upstream over HTTP
(`extensions/dashboard/contributor/remote.go`, `extensions/dashboard/proxy/`).
This is what lets a single dashboard surface UI contributed by 5+ separate
microservices.

The new contract path is in-memory only. The dispatcher's handler table is a
`map[handlerKey]Handler` of in-process Go functions; the registry stores
manifests received via `Registry.Register()` from extensions running inside
the same binary. A remote service has no way to advertise its contract
contributor to the dashboard.

This slice adds remote contributor support to the contract path. Same shape
as the templ flow: a remote service hosts its manifest + a dispatch endpoint;
the dashboard registers the remote, routes intent requests to the upstream,
returns the response transparently. Auto-discovery via the service registry
rides on the same plumbing in a follow-on.

## Recommended Approach

### 1. Registry — remote tracking

Extend `contract.Registry` to track remotes alongside locals. The merged-graph
layer already handles per-contributor lookups; we just need to record the
upstream endpoint so the dispatcher can find it.

```go
type Registry interface {
    // existing methods...
    RegisterRemote(m *ContractManifest, endpoint RemoteEndpoint) error
    IsRemote(contributor string) bool
    Remote(contributor string) (RemoteEndpoint, bool)
}

type RemoteEndpoint struct {
    BaseURL string         // e.g. https://svc.internal:8443/svc
    APIKey  string         // optional, sent as Authorization: Bearer <apiKey>
    Client  *http.Client   // optional; nil = default http.Client with 5s timeout
}
```

`RegisterRemote` calls the same validate/merge path `Register` does so remote
manifests participate in `MergedGraph` / `MatchRoute` exactly like local ones
(the React shell's graph endpoint for `/contributor-x/route` resolves a remote
manifest with no special-casing).

### 2. Dispatcher — remote forwarding

The dispatcher's local handler map keeps working for in-process contributors.
Add a remote-fallback hook:

```go
type RemoteDispatcher interface {
    Dispatch(ctx context.Context, req contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error)
    Subscribe(ctx context.Context, ... ) (<-chan contract.StreamEvent, func(), error)
}

func (d *Dispatcher) SetRemoteDispatcher(rd RemoteDispatcher)
```

The existing `Dispatch` flow:
1. Look up `req.Contributor + req.Intent + req.IntentVersion` in the local
   handler map.
2. If not found and a `RemoteDispatcher` is set, ask it to dispatch.
3. Else return `CodeNotFound` as today.

Subscriptions get the same treatment via `SetRemoteDispatcher` so SSE works
for remote subscription intents.

### 3. `contract/remote` — HTTP forwarding client

New package implements `RemoteDispatcher` by POSTing the envelope to the
remote service.

```go
type ForwardingDispatcher struct {
    reg     contract.Registry            // for looking up RemoteEndpoint per contributor
    headers HeaderForwardFunc            // optional; defaults to nil (no auth forwarding)
}
```

- For each request, looks up the contributor's `RemoteEndpoint` from the
  registry. Falls through to `CodeNotFound` when the contributor isn't
  registered as a remote.
- POSTs the verbatim envelope to `<BaseURL>/_forge/contract/dispatch`.
- Forwards `Authorization` and `Cookie` headers from the inbound request
  (read via `dashauth.RequestFromContext`) so the upstream sees the same
  caller identity — mirrors `WithForwardedHeaders` in the legacy
  `RemoteContributor`.
- Unmarshals the wire envelope back into `(data, meta, err)`.

Subscriptions are a follow-on: the upstream-stream multiplexing needs more
plumbing than a single POST and is out of slice (m).

### 4. `contract/server` — helper for non-dashboard services

A remote contract contributor is typically a service that *isn't* mounting
the full dashboard — it just wants to advertise some intents. Provide a
thin server helper that exposes the two endpoints the dashboard expects:

```go
package server

func New(reg contract.Registry, wreg contract.WardenRegistry, disp transport.Dispatcher, audit contract.AuditEmitter) *Server

func (s *Server) RegisterRoutes(r forge.Router, prefix string)
```

`prefix` defaults to `/_forge/contract`. Mounts:
- `GET <prefix>/manifest?contributor=<name>` — returns the contributor's
  manifest as JSON. Without `?contributor`, returns all registered manifests.
- `POST <prefix>/dispatch` — accepts the contract envelope, dispatches via
  the supplied dispatcher, returns the response envelope. Uses the same
  CSRF/idempotency rules as the dashboard's own `/api/dashboard/v1`.

### 5. Dashboard extension — manual registration API

Add `Extension.RegisterRemoteContractContributor(ctx, baseURL, apiKey) error`
that:
- Fetches `GET <baseURL>/_forge/contract/manifest`
- Validates + registers via `Registry.RegisterRemote`
- Stitches a `ForwardingDispatcher` into the dispatcher if one isn't already
  there.

The dashboard's own `/api/dashboard/v1` endpoint stays unchanged: requests
for remote contributors hit the dispatcher, which forwards via the
`ForwardingDispatcher` set up at registration time.

### 6. Discovery — pluggable, deferred for auto-detection

A `discovery.Watcher` interface lets deployments wire any of:
- The existing forge service-discovery package (poll for tag, register)
- A static config (list of `{baseURL, apiKey}` from YAML)
- A bespoke watcher (e.g. Kubernetes endpoints, mTLS service mesh)

Ship a static-config watcher and a no-op default this slice. Forge-discovery
integration is its own follow-on (slice m2) since it requires wiring the
discovery client into the contract registry and reusing the templ-path
discovery package machinery.

## Files

### New
- `extensions/dashboard/contract/remote/client.go`
- `extensions/dashboard/contract/remote/client_test.go`
- `extensions/dashboard/contract/remote/manifest.go` (helper to fetch + parse remote manifest)
- `extensions/dashboard/contract/server/server.go`
- `extensions/dashboard/contract/server/server_test.go`

### Modified
- `extensions/dashboard/contract/registry.go` — `RegisterRemote`, `IsRemote`, `Remote`
- `extensions/dashboard/contract/dispatcher/dispatcher.go` — `SetRemoteDispatcher`, fallback in `Dispatch`
- `extensions/dashboard/extension.go` — `RegisterRemoteContractContributor` API, default forwarding dispatcher wiring

## Tests

- **Registry:** registering a remote populates `IsRemote` + `Remote` + `MergedGraph`.
- **Forwarding dispatcher:** dispatches local handlers locally; falls through to remote when not registered locally; CodeNotFound when neither knows.
- **HTTP round-trip:** `httptest.Server` hosting a server.New(...) → host dispatcher with `ForwardingDispatcher` → query + command both round-trip end to end including error envelopes.
- **Manifest fetcher:** unmarshals JSON manifest, surfaces errors clearly.
- **Auth forwarding:** Authorization header on the inbound request appears on the outbound to the upstream.

## Out of scope (slice m2)

- Auto-discovery via the forge service registry. The legacy templ flow
  already does this; mirror it once the static-config path is solid.
- Subscription forwarding (SSE multiplexing across services).
- Remote-side caching / stale-on-error fallback.
- Per-contributor warden authorization on the dispatch path
  (the upstream service applies its own warden; the host doesn't double-check).
