// cmd/forge/plugins/dashboard_templates.go
package plugins

// dashboardPackageJSONTemplate is the package.json for a scaffolded external
// dashboard shell. It depends on exactly the two packages Task 2 published
// under @forge-go: dashboard-plugin and dashboard-kit. It is not published
// to a registry, so pnpm install will not resolve those two dependencies
// until they are.
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
    "@forge-go/dashboard-kit": "^0.0.0",
    "@forge-go/dashboard-plugin": "^0.0.0",
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
// PluginHost uses -- sidebar chrome, a capabilities fetch, per-plugin
// resolution (hidden / mismatch / setup / ready), and routed pages, each
// wrapped in a client scoped to its own extension.
//
// It is deliberately smaller than apps/shell's real host: it skips
// @forge-go/dashboard-runtime entirely (ForgeDashboardProvider,
// useDashboardConfig, PluginErrorBoundary), because this scaffold names only
// two @forge-go dependencies, dashboard-plugin and dashboard-kit. Everything
// this file needs from the resolver, the scoped client, and the plugin
// contract comes from dashboard-plugin alone; the config that would normally
// come from the runtime's provider is read directly from
// window.__FORGE_DASHBOARD__ instead.
const dashboardAppTSXTemplate = `import { useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"
import { BrowserRouter, Link, Navigate, Route, Routes } from "react-router"
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
const plugins: ForgePlugin[] = []

/**
 * Forge's embedded shell has this injected by the Go handler that serves it
 * before the bundle loads. WithShellSource(ShellExternal) never does that --
 * Forge mounts no handler at {BasePath}/ui in that mode -- so this is only
 * populated if you wire up the same injection yourself on whatever page
 * serves this build. Absent that, contractBase falls back to the default
 * Forge dashboard mount.
 */
declare global {
  interface Window {
    __FORGE_DASHBOARD__?: { basePath?: string; contractBase?: string }
  }
}

function readConfig() {
  const injected = window.__FORGE_DASHBOARD__ ?? {}
  return {
    contractBase: injected.contractBase ?? "/dashboard/api/dashboard/v1",
  }
}

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
  const { contractBase } = readConfig()
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
          return <Setup key={plugin.extension} message={pluginState.message} />
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
                    <PluginProvider client={clients.get(plugin.extension)!}>
                      <Page />
                    </PluginProvider>
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
    <TooltipProvider>
      <BrowserRouter>
        <Host />
      </BrowserRouter>
    </TooltipProvider>
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

This produces a static ` + "`dist/`" + `. ` + "`vite.config.ts`" + ` sets
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
