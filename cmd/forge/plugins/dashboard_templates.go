// cmd/forge/plugins/dashboard_templates.go
package plugins

// dashboardPackageJSONTemplate is the package.json for a scaffolded external
// dashboard shell. It depends on all three @forge-go packages the dashboard
// front end is split into: dashboard-plugin, dashboard-kit, and
// dashboard-runtime (the last supplies ForgeDashboardProvider and
// PluginErrorBoundary, used in App.tsx below). None of the three are
// published to a registry yet, so pnpm install will not resolve them until
// they are.
//
// The three are pinned to "latest" rather than a range. "^0.0.0" is not the
// wide range it looks like -- caret does not widen on a 0.0.x, so it means
// exactly 0.0.0, and unless the first publish is literally that version every
// scaffolded project fails its first install. Naming a guessed first version
// instead would couple this CLI to a release number nobody has committed to.
// "latest" is what a scaffold wants anyway: one `pnpm install`, and the
// user's own lockfile pins it from then on. That is how create-* tools
// behave.
const dashboardPackageJSONTemplate = `{
  "name": "{{.Name}}",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "typecheck": "tsc --noEmit"
  },
  "dependencies": {
    "@forge-go/dashboard-kit": "latest",
    "@forge-go/dashboard-plugin": "latest",
    "@forge-go/dashboard-runtime": "latest",
    "react": "^19.2.6",
    "react-dom": "^19.2.6",
    "react-router": "^8.3.1"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4",
    "@types/react": "^19",
    "@types/react-dom": "^19",
    "@vitejs/plugin-react": "^6",
    "tailwindcss": "^4",
    "typescript": "~6",
    "vite": "^8"
  }
}
`

// dashboardViteConfigTemplate sets base: "./" for the same reason
// apps/shell/vite.config.ts does, in the xraph/forge-dashboard repo: a
// custom build faces the identical mount problem. The reasoning is carried
// over, not paraphrased away; only the last paragraph differs, because the
// path this build takes to the browser differs -- ShellExternal registers no
// Go-side handler to rewrite anything for it.
const dashboardViteConfigTemplate = `import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  // Relative base, deliberately. This app does not know at build time where
  // it will be mounted -- your own web root, a reverse-proxy path, a CDN
  // subdirectory. An absolute base bakes one specific mount into both the
  // asset URLs in index.html and Vite's preload resolver for lazy chunks, and
  // a deployment on any other path then 404s every script it asks for.
  //
  // Forge's own embedded shell carries this identical comment for the
  // identical reason (extensions/dashboard/shellassets in the forge repo).
  // There, a Go handler rewrites index.html's relative asset URLs to an
  // absolute one on the way out, so deep links still resolve. Nothing does
  // that for this build: WithShellSource(ShellExternal) means Forge mounts no
  // handler at {BasePath}/ui at all, so nobody rewrites this build's
  // index.html for you. If you add client-side routes with deep links, make
  // sure whatever serves this build returns the same index.html (and
  // resolves its relative asset paths against the same directory) for every
  // route your app matches -- a bare static file server that 404s on unknown
  // paths will break them.
  base: "./",
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
})
`

// dashboardTSConfigTemplate is a standard Vite + React + TS config. No
// project references, no separate node config: this is a starting point, not
// a framework, and tsc runs in --noEmit mode only (vite build does the actual
// bundling).
const dashboardTSConfigTemplate = `{
  "compilerOptions": {
    "target": "ES2022",
    "useDefineForClassFields": true,
    "lib": ["ES2023", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "types": ["vite/client"],
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src", "vite.config.ts"]
}
`

const dashboardIndexHTMLTemplate = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>{{.DisplayName}}</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
`

const dashboardGitignoreTemplate = `node_modules
dist
*.local
.DS_Store
`

const dashboardMainTSXTemplate = `import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import "@forge-go/dashboard-kit/globals.css"
import { App } from "./App"

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>
)
`

// dashboardAppTSXTemplate renders a host in the shape apps/shell's own
// PluginHost uses -- sidebar chrome, a config provider, a capabilities
// fetch, per-plugin resolution (hidden / mismatch / setup / ready), and
// routed pages, each wrapped in a client scoped to its own extension AND in
// PluginErrorBoundary, at both call sites apps/shell wraps: the setup panel
// and the route element. Third-party plugins are exactly what a custom build
// installs, and an uncontained throw from one must not blank the whole
// dashboard -- that is what the boundary is for, and it matters more here
// than in the first-party shell, not less.
const dashboardAppTSXTemplate = `import { useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"
import { BrowserRouter, Link, Navigate, Route, Routes } from "react-router"
import {
  ForgeDashboardProvider,
  PluginErrorBoundary,
  configFromWindow,
  useDashboardConfig,
} from "@forge-go/dashboard-runtime"
import {
  createScopedClient,
  MismatchPanel,
  PluginProvider,
  resolvePluginState,
  SetupPanel,
} from "@forge-go/dashboard-plugin"
import type {
  Capabilities,
  ForgePlugin,
  PluginNavItem,
  PluginState,
  ScopedClient,
} from "@forge-go/dashboard-plugin"
import { AppSidebar } from "@forge-go/dashboard-kit/components/app-sidebar"
import { SiteHeader } from "@forge-go/dashboard-kit/components/site-header"
import {
  SidebarInset,
  SidebarProvider,
} from "@forge-go/dashboard-kit/components/sidebar"
import { TooltipProvider } from "@forge-go/dashboard-kit/components/tooltip"

// Add your own plugins here, each built with definePlugin from
// @forge-go/dashboard-plugin -- the same function the shipping Forge shell's
// own core plugin is built on. See README.md for the pnpm add step.
//
// Hoisted beside config below: ForgeDashboardProvider memoizes on config
// identity, so an inline object literal in JSX would re-derive the config
// and cascade a re-render to every consumer on each render of App.
const plugins: ForgePlugin[] = []

// configFromWindow() reads window.__FORGE_DASHBOARD__ -- what Forge's own
// embedded shell has injected for it before the bundle loads. Nothing injects
// that for this build: WithShellSource(ShellExternal) means Forge mounts no
// handler at {BasePath}/ui, so configFromWindow() falls through to its own
// empty-object default and basePath below picks the same default Forge
// itself uses. Wire up the same injection yourself if you serve this build
// from a Go handler that knows a different BasePath.
const injected = configFromWindow()
const config = { basePath: injected.basePath ?? "/dashboard", ...injected }

/** The chrome every host state renders inside: sidebar, header, content. */
function HostShell({ children }: { children: ReactNode }) {
  return (
    <SidebarProvider>
      <AppSidebar variant="inset" />
      <SidebarInset>
        <SiteHeader />
        <div className="flex flex-1 flex-col gap-4 p-4">{children}</div>
      </SidebarInset>
    </SidebarProvider>
  )
}

type CapabilitiesState =
  | { status: "loading" }
  | { status: "ready"; capabilities: Capabilities }
  | { status: "error"; message: string }

function Host() {
  const { contractBase } = useDashboardConfig()
  const [state, setState] = useState<CapabilitiesState>({ status: "loading" })

  useEffect(() => {
    let cancelled = false

    void (async () => {
      try {
        const res = await fetch(` + "`${contractBase}/capabilities`" + `, {
          credentials: "same-origin",
        })
        if (!res.ok) {
          throw new Error(` + "`capabilities request failed with HTTP ${res.status}`" + `)
        }
        const capabilities = (await res.json()) as Capabilities
        if (!Array.isArray(capabilities?.contributors)) {
          throw new Error("capabilities response carried no contributors array")
        }
        if (!cancelled) setState({ status: "ready", capabilities })
      } catch (error) {
        if (!cancelled) {
          setState({
            status: "error",
            message: error instanceof Error ? error.message : String(error),
          })
        }
      }
    })()

    return () => {
      cancelled = true
    }
  }, [contractBase])

  const clients = useMemo(() => {
    const byExtension = new Map<string, ScopedClient>()
    for (const plugin of plugins) {
      byExtension.set(plugin.extension, createScopedClient(contractBase, plugin.extension))
    }
    return byExtension
  }, [contractBase])

  if (state.status === "loading") {
    return (
      <HostShell>
        <p role="status" className="text-sm text-muted-foreground">
          Loading dashboard capabilities…
        </p>
      </HostShell>
    )
  }

  if (state.status === "error") {
    return (
      <HostShell>
        <div
          role="alert"
          className="rounded-md border border-destructive/50 px-3 py-2 text-sm text-destructive"
        >
          Could not reach the dashboard server: {state.message}
        </div>
      </HostShell>
    )
  }

  const resolved: { plugin: ForgePlugin; pluginState: PluginState }[] = plugins.map(
    (plugin) => ({
      plugin,
      pluginState: resolvePluginState(plugin, state.capabilities),
    })
  )

  const ready = resolved.filter((r) => r.pluginState.kind === "ready")

  const nav: { plugin: ForgePlugin; item: PluginNavItem }[] = ready.flatMap(({ plugin }) =>
    [...plugin.nav]
      .sort((a, b) => (a.priority ?? 0) - (b.priority ?? 0))
      .map((item) => ({ plugin, item }))
  )

  const home = nav[0]?.item.to ?? ready[0]?.plugin.routes[0]?.path
  const rootIsClaimed = ready.some(({ plugin }) =>
    plugin.routes.some((route) => route.path === "/")
  )

  return (
    <HostShell>
      {nav.length > 0 && (
        <nav aria-label="Plugin pages" className="flex flex-wrap gap-2">
          {nav.map(({ plugin, item }) => (
            <Link
              key={` + "`${plugin.extension}:${item.to}`" + `}
              to={item.to}
              className="rounded-md border px-3 py-1.5 text-sm hover:bg-accent"
            >
              {item.label}
            </Link>
          ))}
        </nav>
      )}

      {resolved.map(({ plugin, pluginState }) => {
        if (pluginState.kind === "mismatch") {
          return (
            <MismatchPanel
              key={plugin.extension}
              required={pluginState.required}
              reported={pluginState.reported}
            />
          )
        }
        if (pluginState.kind === "setup") {
          const Setup = plugin.setup ?? SetupPanel
          return (
            // plugin.setup is third-party code exactly as a route element is,
            // so it gets the same containment: a throw here must take down
            // only this plugin's box, not the whole dashboard.
            <PluginErrorBoundary key={plugin.extension} plugin={plugin.extension}>
              <Setup message={pluginState.message} />
            </PluginErrorBoundary>
          )
        }
        return null
      })}

      {plugins.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No plugins installed yet. pnpm add one, then list it in the{" "}
          <code>plugins</code> array in <code>src/App.tsx</code>.
        </p>
      )}

      {ready.length > 0 && (
        <Routes>
          {ready.flatMap(({ plugin }) =>
            plugin.routes.map((route) => {
              const Page = route.element
              return (
                <Route
                  key={` + "`${plugin.extension}:${route.path}`" + `}
                  path={route.path}
                  element={
                    // Same containment as the setup branch above: a
                    // third-party bundle throwing during render takes down
                    // its own page, not every other plugin's.
                    <PluginErrorBoundary plugin={plugin.extension}>
                      <PluginProvider client={clients.get(plugin.extension)!}>
                        <Page />
                      </PluginProvider>
                    </PluginErrorBoundary>
                  }
                />
              )
            })
          )}
          {home && !rootIsClaimed && (
            <Route path="/" element={<Navigate to={home} replace />} />
          )}
        </Routes>
      )}
    </HostShell>
  )
}

export function App() {
  return (
    <ForgeDashboardProvider config={config}>
      <TooltipProvider>
        <BrowserRouter>
          <Host />
        </BrowserRouter>
      </TooltipProvider>
    </ForgeDashboardProvider>
  )
}

export default App
`

// dashboardReadmeTemplate covers exactly the three things the scaffold's
// consumer needs: adding a plugin, building, and pointing Forge at the
// result. The ShellExternal section states what actually happens --
// {BasePath}/ui 404s through ForgeUI's own catch-all -- rather than implying
// Forge will serve this build for them, which it does not.
const dashboardReadmeTemplate = `# {{.DisplayName}}

A standalone Vite + React + TypeScript dashboard shell, scaffolded by
` + "`forge dashboard new`" + `. Reach for this when you have private or
third-party dashboard plugins and want to build and serve the shell
yourself, instead of the prebuilt shell Forge embeds and serves at
` + "`{BasePath}/ui`" + ` by default.

## Add a plugin

` + "```sh" + `
pnpm add <plugin-package>
` + "```" + `

Then import it in ` + "`src/App.tsx`" + ` and list it in the ` + "`plugins`" + ` array:

` + "```ts" + `
import { yourPlugin } from "<plugin-package>"

const plugins: ForgePlugin[] = [yourPlugin]
` + "```" + `

A plugin package exports something built with ` + "`definePlugin`" + ` from
` + "`@forge-go/dashboard-plugin`" + ` -- the same function the shipping Forge
shell's own core plugin is built on.

## Build

` + "```sh" + `
pnpm install
pnpm build
` + "```" + `

**Not yet installable.** The three ` + "`@forge-go/dashboard-*`" + ` packages in
` + "`package.json`" + ` are not published to npm yet, so ` + "`pnpm install`" + `
cannot resolve them today. Everything else in here is ready; the scaffold is
correct and waiting on the publish, not broken.

Once it installs, this produces a static ` + "`dist/`" + `. ` + "`vite.config.ts`" + ` sets
` + "`base: \"./\"`" + ` on purpose (see the comment there): this shell does
not know at build time where you will mount it, and a deployment under any
base path other than the default one 404s every asset it asks for unless the
base stays relative.

## Point Forge at it

` + "```go" + `
dashboard.NewExtension(
    dashboard.WithShellSource(dashboard.ShellExternal),
)
` + "```" + `

` + "`ShellExternal`" + ` means Forge registers no shell at
` + "`{BasePath}/ui`" + ` -- not that the path is left unrouted. A request
there gets a real 404, produced by ForgeUI's own catch-all route, because
nothing registers a page there. Forge does not serve ` + "`dist/`" + ` for
you: you are responsible for that yourself, however you already serve static
assets -- your own file server, a reverse proxy, a CDN. The rest of the
dashboard extension is unaffected either way; this shell talks to the same
data contract under ` + "`{BasePath}/api/dashboard/v1`" + `.
`

// dashboardNextPageTemplate is the App Router mount. "use client" is required
// because the host renders a BrowserRouter, which needs the browser. The
// dashboard pages therefore get no RSC benefit; only the shell around them
// does. next-sanity makes the same tradeoff for the same reason.
//
// corePlugin carries root: true, so it claims "/" inside the mount and any
// scoped plugin the user adds lands under its own "/@namespace".
const dashboardNextPageTemplate = `"use client"

import { ForgeDashboard } from "@forge-go/dashboard-host"
import corePlugin from "@forge-go/dashboard-plugin-core"

// contractBase is a path on this app's own origin, so every request is
// same-origin and the client's CSRF handshake works untouched. Point it at
// wherever the proxy route below is mounted.
const config = { basePath: "/admin", contractBase: "/api/forge/dashboard/v1" }
const plugins = [corePlugin]

export default function Page() {
  return <ForgeDashboard basename="/admin" config={config} plugins={plugins} />
}
`

// dashboardNextRouteTemplate is the proxy. FORGE_URL is read server-side and
// never reaches the browser.
const dashboardNextRouteTemplate = `import { createForgeProxy } from "@forge-go/dashboard-next"

export const { GET, POST } = createForgeProxy({
  target: process.env.FORGE_URL!,
})
`

// dashboardNextPackageJSONTemplate is the package.json for a dashboard
// mounted inside an existing Next.js app, as an alternative to the
// standalone Vite shell dashboardPackageJSONTemplate produces. It names all
// six @forge-go packages the Next-embedded dashboard front end is split
// into: dashboard-host, dashboard-next and dashboard-plugin-core (built in
// this change's companion tasks in the forge-dashboard repo, and consumed
// here only as strings -- nothing is imported at Go build time), plus the
// three shared packages the standalone shell also depends on.
const dashboardNextPackageJSONTemplate = `{
  "name": "{{ .Name }}",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start"
  },
  "dependencies": {
    "@forge-go/dashboard-host": "latest",
    "@forge-go/dashboard-kit": "latest",
    "@forge-go/dashboard-next": "latest",
    "@forge-go/dashboard-plugin": "latest",
    "@forge-go/dashboard-plugin-core": "latest",
    "@forge-go/dashboard-runtime": "latest",
    "next": "^15",
    "react": "^19.2.0",
    "react-dom": "^19.2.0",
    "react-router": "^8.3.1"
  }
}
`
