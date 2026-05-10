# Dashboard Contract Shell

The React/TypeScript runtime that consumes the Forge dashboard contract. It fetches a graph from the Go side, walks each intent through a registered React component, and renders the result. shadcn/ui (Base UI + Tailwind) provides the primitives.

## At a glance

- **Stack:** TypeScript 5 strict, React 18, Vite 5, React Router v6 data router, TanStack Query 5, Zustand 4, Tailwind CSS 3, Vitest + MSW.
- **UI primitives:** shadcn/ui (vendored) + lucide-react icons. Light / dark / system theme baked in.
- **Bundle size:** ~120KB gzipped JS + ~5KB CSS for the v1 vocabulary; well within the 350KB budget.
- **Built-in intent vocabulary (v1):** `page.shell`, `metric.counter`, `action.button`, `action.menu`, `action.divider`, `form.edit`, `form.field`, `resource.list`, `resource.detail`, `dashboard.grid`, `audit.tail`. Unknown intents render a graceful fallback.
- **Embedded into the Go binary:** `pnpm build` emits `dist/`, which the dashboard extension serves under `/dashboard/contract/static/*` and `/dashboard/contract/app/*` (SPA fallback).

For the architecture deep-dive (how the renderer + registry work, how to author new intents), see [ARCHITECTURE.md](./ARCHITECTURE.md). For the design rationale across slices, see [SLICE_D_DESIGN.md](../SLICE_D_DESIGN.md) and [SLICE_E_DESIGN.md](../SLICE_E_DESIGN.md).

## Development

```bash
pnpm install
pnpm dev    # Vite dev server on :5173, proxies /api/dashboard/* to :8080
```

The dev server expects the dashboard binary running on `localhost:8080`. Start it from the repo root:

```bash
go run ./cmd/forge ...    # whatever your dashboard entrypoint is
```

Then browse to `http://localhost:5173/dashboard/contract/app/extensions` (or any other pilot route).

## Build

```bash
pnpm build    # tsc --noEmit && vite build → dist/
```

Run `pnpm build` whenever you change shell source before `go build` from the repo root, since the Go side embeds `dist/` via `//go:embed`. CI does this automatically.

## Test

```bash
pnpm test            # vitest run
pnpm test:watch      # vitest in watch mode
pnpm lint            # tsc --noEmit (strict mode + noUncheckedIndexedAccess)
pnpm format          # prettier --write
```

All tests use Vitest + React Testing Library. HTTP/SSE is intercepted with MSW; jsdom polyfills (ResizeObserver, EventSource stub, pointer-capture) live in [test/setup.ts](./test/setup.ts).

## Project structure

```
shell/
  src/
    main.tsx                React entry; mounts <App/>.
    App.tsx                 Providers + React Router. Loads principal + theme on mount.
    index.css               Tailwind layer + shadcn theme tokens (light + dark CSS variables).
    contract/
      types.ts              TypeScript mirror of the Go envelope (Request, Response, GraphNode, ...)
      client.ts             ContractClient — POST envelope sender with auto-CSRF + idempotency.
      sse.ts                SubscriptionMux — single EventSource, demuxed by SSE event name.
      hooks.ts              React Query bindings: useContractGraph / useContractQuery /
                            useContractCommand / useSubscription.
    runtime/
      registry.ts           IntentRegistry: name → React component map.
      context.tsx           IntentRegistryProvider, ContributorProvider, ParentProvider.
      renderer.tsx          GraphRenderer — dispatches a node to its registered component.
      slots.tsx             SlotRenderer — recursively renders children of a named slot.
      fallbacks.tsx         UnknownIntent / LoadingNode / ErrorNode (shadcn Alert + Skeleton).
      bindings.ts           resolvePayload / resolveValue — turns ParamSource references like
                            { from: 'parent.id' } into concrete JS values.
    auth/
      principal.ts          Zustand store for the current user (loads /api/dashboard/v1/principal).
    lib/
      utils.ts              cn() — clsx + tailwind-merge.
      theme.ts              Zustand theme store (light / dark / system, localStorage-backed).
    components/
      ui/                   shadcn primitives (Button, Card, Alert, Sheet, Table, Form, ...).
      theme-toggle.tsx      Sun / Moon / System dropdown built on shadcn DropdownMenu.
    intents/                Registered intent components. One file per intent.
      register.ts           Builds the IntentRegistry consumed by App.tsx.
      page.shell.tsx        Topbar + main slot wrapper.
      metric.counter.tsx    Subscribed numeric value in a Card.
      action.button.tsx     Issues a kind=command with optional confirm dialog.
      action.menu.tsx       DropdownMenu of actions.
      action.divider.tsx    Separator (used standalone or inside action.menu).
      form.edit.tsx         Form container; preloads via query intent, submits via op command.
      form.field.tsx        Labeled input — text, email, number, password, textarea, checkbox.
      resource.list.tsx     shadcn Table with rowActions slot + detailDrawer (Sheet) slot.
      resource.detail.tsx   dl/dt/dd of a record's fields.
      dashboard.grid.tsx    Responsive widget grid.
      audit.tail.tsx        Append-mode subscription with sticky-bottom auto-scroll.
  test/
    setup.ts                MSW + ResizeObserver + EventSource + pointer-capture polyfills.
    contract.test.ts        ContractClient round-trip tests.
    sse.test.ts             SubscriptionMux dispatch tests.
    renderer.test.tsx       Registry, GraphRenderer, SlotRenderer.
    smoke.test.tsx          Full app mount through the contract endpoint.
    actions.test.tsx        action.button: dispatch, payload binding, confirm dialog.
    form.test.tsx           form.edit + form.field: submit, prefill from query.
    resource.test.tsx       resource.list, resource.detail, dashboard.grid.
  embed.go                  //go:embed dist/* for the Go side.
  components.json           shadcn config (used if you ever run `shadcn add`).
  vite.config.ts            Vite build + dev proxy + @ alias.
  vitest.config.ts          Vitest config (jsdom env, jsdom URL = http://localhost:3000).
  tailwind.config.ts        Tailwind tokens + animate plugin.
  tsconfig.json             TS strict mode + noUncheckedIndexedAccess + paths.
```

## Theming

The shell ships shadcn's "slate" defaults across CSS variables in `src/index.css`. Three modes:

- **light** — default `:root` tokens.
- **dark** — `.dark` class on `<html>` flips the tokens.
- **system** — follows `prefers-color-scheme`.

User selection persists via localStorage (`forge.dashboard.theme`). The topbar's theme toggle is a `DropdownMenu` of Sun / Moon / Monitor.

To override colors for a deployment, ship a CSS file that re-declares the variables and load it via the dashboard config — or fork `src/index.css`.

## Embedding

The Go side serves the built shell from two route groups (registered in `extensions/dashboard/extension.go`):

| URL pattern | Purpose | Cache |
|---|---|---|
| `/dashboard/contract/static/*` | Hashed assets (JS, CSS, fonts) from `dist/` | Immutable for `/assets/*`, no-cache otherwise |
| `/dashboard/contract/app[/*]` | SPA index.html — React Router handles client-side routing | no-cache |

`pnpm build` is required before `go build` so `embed.FS` picks up the latest assets.

## Adding a new intent

See [ARCHITECTURE.md](./ARCHITECTURE.md). The TL;DR:

1. Drop `src/intents/<intent.name>.tsx` with a function component matching `IntentComponentProps`.
2. Register it in `src/intents/register.ts`.
3. Add a Vitest smoke test under `test/`.

## What's NOT in this slice

- Server-side filtering / sorting / pagination for `resource.list` (client-side only for v1).
- Custom column rendering (`customCell.<col>` slot designed in slice (a) but no concrete renderer yet).
- iframe escape hatch component.
- Browser E2E (Playwright).
- Internationalization, advanced accessibility audit beyond RTL defaults.
