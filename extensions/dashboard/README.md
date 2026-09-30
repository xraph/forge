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

A contributor can also live in another service. Serve it there with `contract/server` and register it with `RegisterRemoteContractContributor`.

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
