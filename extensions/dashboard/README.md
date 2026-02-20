# Dashboard Extension v3.0.0

Extensible micro-frontend shell for building admin dashboards with Forge. Contributors (local or remote) register pages, widgets, and settings that are merged into a unified dashboard powered by ForgeUI.

## Features

- **ForgeUI Integration** -- layouts, routing, theming, and HTMX-powered partial navigation
- **Contributor System** -- local (in-process gomponents) and remote (HTTP fragment proxy) contributors
- **Authentication & Authorization** -- page access levels (public, protected, partial), auth page provider, user menu, HTMX-aware redirects
- **Go-JS Bridge** -- call Go functions from the browser via Alpine.js `$go()` magic helpers
- **SSE Real-Time** -- server-sent events for live metric and health updates
- **Federated Search** -- cross-contributor search with the `SearchableContributor` interface
- **Settings Aggregation** -- contributor settings merged into a unified settings page
- **Data Export** -- JSON, CSV, and Prometheus export formats
- **Service Discovery** -- auto-discover remote contributors via the discovery extension
- **Security** -- Content-Security-Policy headers, CSRF tokens, HTML sanitization
- **Theming** -- auto, light, and dark modes with custom CSS injection

## Quick Start

```bash
go get github.com/xraph/forge/extensions/dashboard
```

```go
package main

import (
    "log"
    "time"

    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/dashboard"
)

func main() {
    app := forge.New(
        forge.WithAppName("my-app"),
        forge.WithAppVersion("1.0.0"),
    )

    if err := app.RegisterExtension(dashboard.NewExtension(
        dashboard.WithTitle("My Dashboard"),
        dashboard.WithBasePath("/dashboard"),
        dashboard.WithRealtime(true),
        dashboard.WithRefreshInterval(30 * time.Second),
        dashboard.WithExport(true),
    )); err != nil {
        log.Fatalf("failed to register dashboard: %v", err)
    }

    if err := app.Run(); err != nil {
        log.Fatalf("application error: %v", err)
    }
}
```

The dashboard is available at `http://localhost:8080/dashboard`.

## Architecture

```
ForgeUI App
  |-- Layouts (root -> dashboard/base/full/settings/auth)
  |-- Pages (overview, health, metrics, services)
  |-- Auth (login, logout, register -- via AuthPageProvider)
  |-- Contributors
  |     |-- Local (in-process, gomponents)
  |     +-- Remote (HTTP fragment proxy)
  |-- Bridge (Go <-> JS function calls)
  |-- SSE Broker (real-time events)
  +-- Search / Settings / Export
```

The dashboard uses ForgeUI's layout and routing system. The **root layout** renders the HTML shell with HTMX, Alpine.js, and the sidebar. On HTMX partial requests (HX-Request header), only the page content is returned without the outer HTML shell.

**Contributors** provide pages, widgets, and settings via a manifest-driven system. Local contributors render gomponents nodes directly. Remote contributors serve HTML fragments over HTTP, which the dashboard proxies and embeds.

## Configuration

All options use the functional options pattern:

| Option | Default | Description |
|---|---|---|
| `WithBasePath(path)` | `"/dashboard"` | HTTP base path |
| `WithTitle(title)` | `"Forge Dashboard"` | Page title |
| `WithRealtime(bool)` | `true` | SSE real-time updates |
| `WithExport(bool)` | `true` | Data export endpoints |
| `WithSearch(bool)` | `true` | Federated search |
| `WithSettings(bool)` | `true` | Settings aggregation |
| `WithDiscovery(bool)` | `false` | Auto-discover remote contributors |
| `WithBridge(bool)` | `true` | Go-JS bridge functions |
| `WithEnableAuth(bool)` | `false` | Authentication support |
| `WithDefaultAccess(s)` | `"public"` | Default page access level |
| `WithLoginPath(path)` | `"/auth/login"` | Login page path |
| `WithLogoutPath(path)` | `"/auth/logout"` | Logout page path |
| `WithRefreshInterval(d)` | `30s` | Data collection interval |
| `WithHistoryDuration(d)` | `1h` | Data retention window |
| `WithMaxDataPoints(n)` | `1000` | Max retained data points |
| `WithTheme(theme)` | `"auto"` | Theme: auto, light, dark |
| `WithCSP(bool)` | `true` | Content-Security-Policy |
| `WithCSRF(bool)` | `true` | CSRF token protection |

See [full configuration reference](../../docs/content/docs/extensions/dashboard/configuration.mdx) for all options.

## Authentication & Authorization

The dashboard supports optional authentication with three page access levels:

| Level | Behavior |
|---|---|
| `public` | Always accessible, no login required |
| `protected` | Requires authentication; redirects to login if unauthenticated |
| `partial` | Always accessible but renders differently based on auth state |

### Enabling Auth

```go
dashExt := dashboard.NewExtension(
    dashboard.WithEnableAuth(true),
    dashboard.WithDefaultAccess("protected"),
    dashboard.WithLoginPath("/auth/login"),
)
```

### Providing an AuthChecker

The `AuthChecker` interface validates requests and returns user info. Implement it to integrate with your authentication system:

```go
import dashauth "github.com/xraph/forge/extensions/dashboard/auth"

// Implement the AuthChecker interface
type MyChecker struct{}

func (c *MyChecker) CheckAuth(ctx context.Context, r *http.Request) (*dashauth.UserInfo, error) {
    // Validate session/token/cookie and return user info
    token := r.Header.Get("Authorization")
    if token == "" {
        return nil, nil // unauthenticated (not an error)
    }

    return &dashauth.UserInfo{
        Subject:     "user-123",
        DisplayName: "Jane Doe",
        Email:       "jane@example.com",
        Roles:       []string{"admin"},
    }, nil
}

// Wire it up after registration
typedDash := dashExt.(*dashboard.Extension)
typedDash.SetAuthChecker(&MyChecker{})
```

### Auth Page Provider

Auth extensions provide login/register pages by implementing `AuthPageProvider`:

```go
import dashauth "github.com/xraph/forge/extensions/dashboard/auth"

type MyAuthPages struct{}

func (p *MyAuthPages) AuthPages() []dashauth.AuthPageDescriptor {
    return []dashauth.AuthPageDescriptor{
        {Type: dashauth.PageLogin, Path: "/login", Title: "Sign In"},
        {Type: dashauth.PageRegister, Path: "/register", Title: "Sign Up"},
    }
}

func (p *MyAuthPages) RenderAuthPage(ctx *router.PageContext, pageType dashauth.AuthPageType) (g.Node, error) {
    // Return gomponents login/register form
}

func (p *MyAuthPages) HandleAuthAction(ctx *router.PageContext, pageType dashauth.AuthPageType) (string, g.Node, error) {
    // Handle POST: return redirect URL on success, or error node on failure
    return "/dashboard", nil, nil
}

// Register the provider
typedDash.SetAuthPageProvider(&MyAuthPages{})
```

Auth pages use the `auth` layout (centered card, no sidebar) and are always publicly accessible.

### Per-Page Access Levels

Contributors can set access levels per `NavItem`:

```go
Nav: []contributor.NavItem{
    {Label: "Public Stats", Path: "/stats", Icon: "bar-chart", Access: "public"},
    {Label: "Admin Panel", Path: "/admin", Icon: "shield", Access: "protected"},
    {Label: "Dashboard", Path: "/", Icon: "home", Access: "partial"},
}
```

Empty `Access` falls back to the dashboard's `DefaultAccess` setting.

### Reading User Info in Pages

Page handlers can read the authenticated user from context:

```go
func (c *MyContributor) RenderPage(ctx context.Context, route string, params contributor.Params) (g.Node, error) {
    user := dashauth.UserFromContext(ctx)
    if user.Authenticated() {
        // Render personalized content
    }
}
```

### How It Works

The auth system uses two middleware layers:

1. **ForgeMiddleware** runs on the forge.Router catch-all and populates `UserInfo` in the request context (never blocks requests)
2. **PageMiddleware** runs per ForgeUI page and enforces the access level (redirects to login for `protected` pages)

For HTMX partial navigation, the middleware returns a 401 with an `HX-Redirect` header. The `AuthRedirectScript` (injected into the root layout) reads this header and performs a full-page redirect to the login page.

## Contributor System

Extensions contribute UI to the dashboard by implementing the `LocalContributor` interface:

```go
type UsersContributor struct{}

func (c *UsersContributor) Manifest() *contributor.Manifest {
    return &contributor.Manifest{
        Name:        "users",
        DisplayName: "User Management",
        Icon:        "users",
        Nav: []contributor.NavItem{
            {Label: "Users", Path: "/", Icon: "users", Group: "Identity"},
            {Label: "Roles", Path: "/roles", Icon: "shield", Group: "Identity"},
        },
        Widgets: []contributor.WidgetDescriptor{
            {ID: "active-users", Title: "Active Users", Size: "sm", RefreshSec: 60},
        },
    }
}

func (c *UsersContributor) RenderPage(ctx context.Context, route string, params contributor.Params) (g.Node, error) {
    // Return gomponents nodes
}

func (c *UsersContributor) RenderWidget(ctx context.Context, widgetID string) (g.Node, error) {
    // Return widget content
}

func (c *UsersContributor) RenderSettings(ctx context.Context, settingID string) (g.Node, error) {
    // Return settings panel
}
```

Register with the dashboard:

```go
dashExt := dashExt.(*dashboard.Extension)
dashExt.RegisterContributor(&UsersContributor{})
```

Contributors can also implement `AuthPageContributor` to provide auth pages inline:

```go
type AuthPageContributor interface {
    DashboardContributor
    RenderAuthPage(ctx context.Context, pageType string, params Params) (g.Node, error)
    HandleAuthAction(ctx context.Context, pageType string, params Params) (redirectURL string, node g.Node, err error)
}
```

Remote contributors are separate HTTP services that expose fragment endpoints. They can be registered manually or auto-discovered via the discovery extension.

## Bridge Functions

The Go-JS bridge lets the dashboard UI call Go functions from Alpine.js:

```go
// Register a custom bridge function
dashExt.RegisterBridgeFunction("myapp.getData", func(ctx bridge.Context, params MyParams) (*MyResult, error) {
    // Go logic here
    return result, nil
}, bridge.WithDescription("Get application data"))
```

From the browser:

```javascript
// Alpine.js
const data = await $go('myapp.getData', { key: 'value' })
```

8 built-in functions are registered automatically: `dashboard.getOverview`, `dashboard.getHealth`, `dashboard.getMetrics`, `dashboard.getServices`, `dashboard.getServiceDetail`, `dashboard.getHistory`, `dashboard.getMetricsReport`, `dashboard.refresh`.

## HTTP Endpoints

All routes are under the configured base path (default `/dashboard`):

| Category | Path | Description |
|---|---|---|
| Pages | `/` | Dashboard overview |
| Pages | `/health` | Health status page |
| Pages | `/metrics` | Metrics page |
| Pages | `/services` | Services page |
| Auth | `/auth/login` | Login page (when auth enabled) |
| Auth | `/auth/logout` | Logout page (when auth enabled) |
| Auth | `/auth/register` | Register page (when provider supplies it) |
| API | `/api/overview` | Overview JSON |
| API | `/api/health` | Health JSON |
| API | `/api/metrics` | Metrics JSON |
| API | `/api/services` | Services JSON |
| API | `/api/service-detail?name=X` | Service detail JSON |
| API | `/api/history` | History JSON |
| API | `/api/metrics-report` | Metrics report JSON |
| Export | `/export/json` | Full JSON export |
| Export | `/export/csv` | CSV export |
| Export | `/export/prometheus` | Prometheus format |
| Real-time | `/sse` | SSE event stream |
| Bridge | `/bridge/call` | Bridge function call (POST) |
| Bridge | `/bridge/stream/` | Bridge streaming (SSE) |
| Search | `/api/search?q=X` | Federated search |
| Contributor | `/ext/:name/pages/*` | Local contributor pages |
| Contributor | `/ext/:name/widgets/:id` | Local contributor widgets |
| Remote | `/remote/:name/pages/*` | Remote contributor pages |
| Remote | `/remote/:name/widgets/:id` | Remote contributor widgets |
| Settings | `/settings` | Settings index |
| Settings | `/ext/:name/settings/:id` | Contributor settings |

## Examples

- **[basic](examples/basic/)** -- Minimal dashboard with built-in pages only
- **[contributor](examples/contributor/)** -- Custom local contributor with pages, widgets, and settings
- **[remote](examples/remote/)** -- Remote contributor registration and service discovery
