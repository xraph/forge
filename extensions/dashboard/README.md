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

Use `BypassIdempotency` only when your handler owns receipt and replay semantics. It skips all generic cache operations, including tombstone lookup, claim acquisition, storage and release. Kind checks and admission still run. Ordinary commands keep their existing cache behavior, and `SecretResponse` commands keep their one-time response tombstones unless you also select bypass.

`RegisterQuery` and `RegisterCommand` enforce their declared kinds, even if you pass a conflicting `RequireKind` option. Raw `Register` remains unrestricted unless you pass `RequireKind(contract.KindQuery)` or `RequireKind(contract.KindCommand)`. For raw registrations, the last valid kind option wins. Invalid kinds, nil callbacks and nil options fail registration, including when a later option would override them. A wrong request kind returns `BAD_REQUEST` before admission or cache access.

Requests without a local handler go directly to the remote dispatcher or return `NOT_FOUND`. They never read local cache entries. Generic cache keys still bind only the idempotency key, user subject and intent; they do not bind contributor, version, target or caller scope. Admission alone does not fix those collisions. If you require that binding, your receipt implementation must provide it and bypass the generic cache.

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
