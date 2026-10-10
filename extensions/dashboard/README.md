# Dashboard Extension

Serves your dashboard's data. It collects health, metrics, services and request traces from the running app, and exposes them, along with whatever your own extensions contribute, through one endpoint: the dashboard contract at `{BasePath}/api/dashboard/v1`.

It serves no web pages. The UI is a React app built from the `@forge-go/dashboard-*` packages, which you build and serve yourself. `forge dashboard new` scaffolds one, standalone with Vite or mounted inside a Next.js app.

## Quick Start

```go
app := forge.New(forge.WithAppName("my-app"))

if err := app.RegisterExtension(dashboard.NewExtension(
    dashboard.WithBasePath("/dashboard"),
)); err != nil {
    log.Fatal(err)
}

// The contract is at http://localhost:8080/dashboard/api/dashboard/v1
app.Run()
```

Then build the UI:

```sh
forge dashboard new my-dashboard
```

## Contributing from an Extension

You contribute in two halves. The Go half registers intents with the dashboard's dispatcher. The UI half is a React plugin that calls them.

For the Go half, implement `ContractContributorAware`. The dashboard finds it during `Start()`:

```go
func (e *ReportsExtension) RegisterContractContributor(
    disp *dispatcher.Dispatcher,
    reg contract.Registry,
    wreg contract.WardenRegistry,
) error {
    return reportscontract.Register(disp, reg, wreg, reportscontract.Deps{Store: e.store})
}
```

If your extension can be installed before it's configured, implement `DashboardStatusAware` as well, and the dashboard will show your plugin's setup guide until you report `Configured: true`.

A contributor can also live in another service. Serve it there with `contract/server`, then either register it with `RegisterRemoteContractContributor`, or turn on `WithDiscovery(true)` and let the dashboard find every service tagged `forge-dashboard-contributor` through `SetDiscoveryService`.

## Local handler admission

Use `dispatcher.BeforeDispatch` when your handler needs to check current permissions before a cached response can be returned. You receive the original `contract.Request` and `contract.Principal`, so your policy can check the request's target and caller scope. These options do not authorize requests by themselves. You supply the policy.

```go
err := dispatcher.RegisterCommand(disp, "reports", "reports.export", 1, exportReport,
    dispatcher.BeforeDispatch(authorizeExport),
    dispatcher.BypassIdempotency(),
)
```

`BeforeDispatch` callbacks run in registration order before any local cache access. The first error stops dispatch, uses the handler error mapping and records a denial metric. Admission runs again after a successful claim, even if the claim returns immediately, before the dispatcher returns a cached response or calls your handler. Keep callbacks safe to repeat. Do not perform the domain mutation in them.

Use `BypassIdempotency` only when your handler owns receipt and replay semantics. It skips all generic cache operations, including tombstone lookup, claim acquisition, storage and release. Kind checks and admission still run. `SecretResponse` commands keep their one-time response tombstones unless you also select bypass. Scope callbacks are skipped on bypass too.

`RegisterQuery` and `RegisterCommand` enforce their declared kinds, even if you pass a conflicting `RequireKind` option. Raw `Register` remains unrestricted unless you pass `RequireKind(contract.KindQuery)` or `RequireKind(contract.KindCommand)`. For raw registrations, the last valid kind option wins. Invalid kinds, nil callbacks and nil options fail registration, including when a later option would override them. A wrong request kind returns `BAD_REQUEST` before admission or cache access.

Requests without a local handler go directly to the remote dispatcher or return `NOT_FOUND`. They never read local cache entries.

## Generic command replay

Configure an atomic `IdempotencyClaimer` for keyed generic commands. The default memory store supports claims. A custom Lookup/Store-only backend can replay a matching bound record, but a miss returns `UNAVAILABLE` with reason `idempotency.claim_required` before your handler runs. Keyless commands are unaffected.

The lookup key and `dashboard.command` namespace remain unchanged: idempotency key plus `User.Subject + ":" + intent`. New ordinary success records carry a comparison digest in a versioned internal `WireBody` envelope (`format: forge.dashboard.idempotency`, `version: 1`, `binding: sha256:<64 lowercase hex>`, `response: <original success>`). Their outer status is 409 so the previous dashboard reader refuses them before decoding. This internal status does not change the successful HTTP response. Do not expose the cache body through a generic HTTP cache reader.

The digest binds the envelope, contributor, intent, resolved version, kind, exact payload bytes, params and full principal. Both claims maps count, along with the user's subject, provider, name, email, avatar, ordered roles/scopes and metadata. Nil and empty values differ. Map insertion order does not matter, but JSON whitespace in a payload does. CSRF, route and correlation metadata are excluded.

Add trusted scope when your handler depends on authority outside those inputs:

```go
err := dispatcher.RegisterCommand(disp, "reports", "reports.export", 1, exportReport,
    dispatcher.BeforeDispatch(authorizeExport),
    dispatcher.IdempotencyScope(func(ctx context.Context, req contract.Request, p contract.Principal) ([]byte, error) {
        return json.Marshal([]string{"reports-scope-v1", installationID, policyTenant})
    }),
)
```

You supply validated authority from context or static host composition. Client route and filter fields are not trusted tenancy. Multiple scope callbacks append contributions in registration order, with separate framing, and never replace the default binding. They run after admission before cache access and again after a successful claim, including a cached claim result. Use deterministic, repeatable callbacks without domain mutations. A changed binding refuses replay or execution and releases only an acquired claim. Publication keeps the digest captured before the handler, even if the handler changes a referenced map. Concurrent mutation of request/principal inputs is unsupported.

Params, claims and metadata accept this bounded value domain: nil, bool, valid UTF-8 strings, built-in signed and unsigned integer widths including int/uint, finite float32/float64, valid `json.Number` with its exact lexeme, `json.RawMessage`, `[]byte`, `[]string`, `[]any`, `map[string]string` and `map[string]any`. Numeric types and float bits remain distinct. Raw messages and byte slices keep their exact bytes. Other named types, structs, pointers, functions, channels, custom marshalers and other collections are rejected before any cache claim or mutation. No custom marshaler runs to build a digest.

Limits are 64 nested containers, 100000 value nodes, 8 MiB of encoded binding input and 64 KiB per scope contribution. These are generic-cache admission limits, separate from HTTP body limits. Cycles fail the depth limit. A failed binding returns an error, never a reduced digest. Full-principal binding can conservatively conflict after a token refresh, a profile update, or reordered roles/scopes. Claims such as `iat`, `exp` and session IDs cannot safely be omitted because a policy or handler may use them.

Only a supported exact-binding success can replay. Legacy successes, malformed records, duplicate or case-aliased protocol fields, unsupported versions and mismatches return `CONFLICT` with reason `idempotency.binding_conflict`, with no cached data or metadata. Records are not rewritten or renewed on refusal. A no-body 409 marker retains `idempotency.already_ran`; secret responses never enter storage. If a successful response cannot be encoded, the still-owned claim gets the same consumed marker. A claim result with both or neither of `Cached` and `End`, or a lookup hit without a record, fails as a backend protocol error.

A lost claim never falls back to unconditional Store. A successor's result, tombstone or live claim must remain intact. Storage failures cannot undo a completed mutation and may leave no receipt. Lease expiry, TTL expiry, capacity eviction and memory-store restarts also limit deduplication. This cache does not promise exactly-once effects or durable receipt recovery.

Upgrade or drain every reader and writer sharing the scope before qualifying the deployment. New records protect against the inspected previous reader, and new readers refuse old successes. Old readers can still disclose unbound records written by old writers, including late in-flight completions. An old lost-claim fallback can overwrite a protected record. Keep existing records, keys, claims and TTLs during rollout; do not flush, migrate or retry with a new key automatically. Legacy entries remain conflicts until ordinary expiry. Nonexpiring entries need explicit reconciliation against authoritative domain state.

Dispatch integrations must add trusted `InstallationID` and `PolicyTenant` scope to legacy registrations and bypass this generic cache for durable commands. Forge cannot infer that installation scope or detect an omitted integration callback.

## Authentication

The dashboard doesn't sign anyone in. You give it an `AuthChecker`, and it attaches the user to every contract request:

```go
import dashauth "github.com/xraph/forge/extensions/dashboard/auth"

type MyChecker struct{}

func (c *MyChecker) CheckAuth(ctx context.Context, r *http.Request) (*dashauth.UserInfo, error) {
    token := r.Header.Get("Authorization")
    if token == "" {
        return nil, nil // signed out is not an error
    }

    return &dashauth.UserInfo{
        Subject:     "user-123",
        DisplayName: "Jane Doe",
        Roles:       []string{"admin"},
    }, nil
}

dashExt.(*dashboard.Extension).SetAuthChecker(&MyChecker{})
```

With `WithEnableAuth(true)`, `/principal` answers 401 for a signed-out caller and tells the client where the login page is. Auth extensions such as authsome wire all of this themselves through `DashboardAuthAware`.

## HTTP Endpoints

All paths are under the base path (default `/dashboard`).

| Method | Path | Description |
|---|---|---|
| POST | `/api/dashboard/v1` | Contract envelope: queries and commands |
| GET | `/api/dashboard/v1/capabilities` | Registered contributors and their status |
| GET | `/api/dashboard/v1/principal` | The signed-in user |
| GET | `/api/dashboard/v1/csrf` | CSRF token for commands |
| GET | `/api/dashboard/v1/stream` | Subscription updates over SSE |
| POST | `/api/dashboard/v1/stream/control` | Subscribe and unsubscribe on an open stream |
| GET | `/export/json`, `/export/csv`, `/export/prometheus` | Data snapshots, when export is on |

## Examples

- **[basic](examples/basic/)**: the extension with its default intents and nothing else registered

Full docs live at [docs/content/docs/extensions/dashboard](../../docs/content/docs/extensions/dashboard/index.mdx).
