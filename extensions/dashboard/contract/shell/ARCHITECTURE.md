# Architecture: Dashboard Contract Shell

The runtime that turns a Forge dashboard contract YAML into a working React UI.

## Pipeline at a glance

```
┌──────────────┐  POST /api/dashboard/v1     ┌──────────────┐
│ React Router │ ──────────────────────────▶ │ Go contract  │
│  PageRoute   │  { kind: graph, route }     │  registry    │
└──────┬───────┘                             └──────┬───────┘
       │                                            │ filtered
       │  GraphNode tree (typed)                    │ by user perms
       ▼                                            ▼
┌──────────────┐                             (server-side)
│GraphRenderer │
│   walks      │
└──────┬───────┘
       │ for each node
       ▼
┌──────────────┐  registry.resolve(intent)
│IntentRegistry│ ──────────▶  React component
└──────────────┘
       │ renders with
       ▼
┌─────────────────────────────────────────┐
│ <YourIntent node={n} props={p} slots={s}│
│   data={d} />                            │
└──────────────────────────────────────────┘
       │ may call
       ▼
useContractQuery / useContractCommand / useSubscription
       │
       ▼   (back through ContractClient or SubscriptionMux)
   POST /api/dashboard/v1   |   GET /api/dashboard/v1/stream
```

The Go side handles auth, permission filtering, dispatch, and audit. The React shell only handles **rendering** — never authorization decisions.

## Five concepts to know

### 1. The graph

The graph is a typed tree of `GraphNode`s. Every node has an `intent` (string), optional `props` (free-form), `data` (a binding to a query intent), and `slots` (named child arrays). The Go server filters out nodes the current user cannot see *before* sending — the shell trusts the response.

```ts
interface GraphNode {
  intent: string;                            // resolved by IntentRegistry
  title?: string;
  data?: { intent: string; params?: ... };   // data binding
  props?: Record<string, unknown>;
  slots?: Record<string, GraphNode[]>;       // named child arrays
  enabledWhen?: Predicate;                   // for read-only / disabled UI
  op?: string;                               // for action.* — the command intent
  payload?: Record<string, unknown>;         // ParamSource-shaped
}
```

### 2. The IntentRegistry

A `Map<string, React.ComponentType>` keyed by intent name. `App.tsx` builds it once via `buildIntentRegistry()` and threads it through React context.

```ts
const reg = new IntentRegistry();
reg.register("metric.counter", MetricCounter);
reg.register("page.shell", PageShell);
// ...
```

The `GraphRenderer` looks up `node.intent` in the registry and renders the component, falling back to `<UnknownIntent intent={n.intent} />` when the entry is missing. Future contributors can register their own intents at runtime; slice (e) registers only the v1 vocabulary.

### 3. Slots

Slots are how a parent intent invites children. The renderer never decides where children go — the parent does, by calling `<SlotRenderer slot="<name>" slots={slots} />` somewhere in its JSX.

```tsx
function PageShell({ slots }: IntentComponentProps) {
  return (
    <>
      <header>...</header>
      <main>
        <SlotRenderer slot="main" slots={slots} />
      </main>
    </>
  );
}
```

The contract registry (Go side) validates at registration time that contributors only fill slots their parent intent declares — no surprises in production.

### 4. ContributorContext + ParentContext

Two React contexts thread the data leaf intents need to issue commands and resolve bindings:

- **`useContributor()`** — the contributor name owning the current graph subtree (e.g., `"users"`). `action.button` posts its `op` to this contributor by default. `App.tsx` wraps `<PageRoute>` in `<ContributorProvider value={...}>` once per route.
- **`useParent()`** — the nearest enclosing record (a row from `resource.list`, the loaded record from `form.edit`, etc.). Lets payload bindings like `{ from: 'parent.id' }` resolve without a centralized state machine. `resource.list` and `resource.detail` are responsible for setting it via `<ParentProvider value={row}>` when rendering their children.

### 5. Bindings (`resolvePayload`)

Manifest payload values may be literals, `{ value: X }`, or `{ from: "scope.path" }`. `resolvePayload` walks a payload map and returns concrete JS values, pulling from `parent`, `session`, and `route` contexts as needed. Used by `action.button`, `action.menu`, `form.edit`, and (for `data.params`) `resource.list` / `form.edit`.

## Authoring a new intent

Three files. ~50 lines of code for a typical CRUD-shaped widget.

### Step 1: write the component

```tsx
// src/intents/users.profile-card.tsx
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { useContractQuery } from "../contract/hooks";
import { useContributor, useParent } from "../runtime/context";
import { LoadingNode, ErrorNode } from "../runtime/fallbacks";
import type { IntentComponentProps } from "../runtime/registry";

interface ProfileCardProps {
  size?: "sm" | "md";
}

interface ProfileData {
  email: string;
  joinedAt: string;
  // ...
}

export function ProfileCard({ node, props }: IntentComponentProps<unknown, ProfileCardProps>) {
  const contributor = useContributor();
  const parent = useParent();
  const userId = parent?.id;
  const query = useContractQuery<ProfileData>(contributor, "user.profile", undefined, { id: userId });

  if (query.isLoading) return <LoadingNode />;
  if (query.error) return <ErrorNode message={(query.error as Error).message} />;
  if (!query.data) return null;

  return (
    <Card>
      <CardHeader><CardTitle>{node.title ?? "Profile"}</CardTitle></CardHeader>
      <CardContent>
        <p>{query.data.email}</p>
        <p>Joined {query.data.joinedAt}</p>
      </CardContent>
    </Card>
  );
}
```

### Step 2: register it

```ts
// src/intents/register.ts
import { ProfileCard } from "./users.profile-card";
// ...
reg.register("users.profile-card", ProfileCard as unknown as IntentComponent);
```

### Step 3: smoke test

```tsx
// test/users.profile-card.test.tsx
// ...standard MSW + render setup, see test/resource.test.tsx for a template
```

That's it. The Go server's contract registry doesn't need to know about your component — it just emits the manifest, and the shell's React tree picks it up by name.

## Adding a shadcn primitive

If you need a new shadcn component (e.g., `Tooltip`):

```bash
pnpm add @base-ui-components/react-tooltip
```

Then drop the component file into `src/components/ui/tooltip.tsx`. Use the existing primitives in `src/components/ui/` as templates — they all follow the same pattern (forwardRef + cn() for class merging + tailwind-merge for variant combos).

## Testing strategy

- **Unit / integration:** Vitest + RTL + MSW. Mount the component, intercept the contract endpoint, assert.
- **Test setup polyfills:** [test/setup.ts](./test/setup.ts) provides `ResizeObserver`, `EventSource`, and pointer-capture stubs that Base UI primitives need under jsdom.
- **No browser E2E yet.** Playwright is on the roadmap. For now, the smoke test (`test/smoke.test.tsx`) gives end-to-end coverage through the runtime.

## Performance budget

- **JS:** ≤ 300KB gzipped initial. Currently ~120KB (44KB index + 13KB query-vendor + 49KB react-vendor + 14KB Base UI primitives spread across chunks).
- **CSS:** ≤ 10KB gzipped. Currently ~5KB.
- **Cold load:** target < 1s on a 3G connection (most admin tools live on faster networks but this keeps the budget honest).

The Vite config splits `react-vendor` and `query-vendor` into their own chunks so they cache across deploys.

## Known limitations / future work

- **`resource.list`** does client-side filtering only. Server-side pagination + sort is slice (f).
- **`form.edit`** uses controlled state, not react-hook-form. zod-based validation can layer on later if forms grow complex.
- **Custom column cells:** `customCell.<col>` slot is designed in slice (a) but not implemented; today, all cells go through the default renderer.
- **iframe escape hatch:** designed but no concrete component until a contributor needs one.
- **Action error handling:** `action.button` swallows command errors silently for v1. A toast pattern is the natural follow-on.
- **Per-event subscription tracing:** explicitly punted (cardinality concerns).

## Why these choices

- **shadcn/ui (vendored)** — accessible Base UI primitives styled with Tailwind and copied into our tree, so we own the components and can adjust without upstream churn. Standard for React+TS admin tools.
- **TanStack Query** — pairs naturally with the contract's per-intent `staleTime` declarations and the `meta.invalidates` hint commands return.
- **Zustand for local state** — small (~1KB), hooks-native, no Provider boilerplate. Used only for transient UI state (theme, principal); server state lives in TanStack Query.
- **CSS variables for theming** — same pattern shadcn ships with; allows runtime theme overrides without rebuilding.
- **Vendored components, not npm package** — shadcn's defining philosophy. Component code lives in `src/components/ui/` so we control everything.
- **No custom design system** — we adopt shadcn's defaults rather than build a parallel system. Slice (f) or a follow-on can fork specific components if an admin-tool need demands it.
