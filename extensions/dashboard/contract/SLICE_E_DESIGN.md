# Slice (e) — Built-in Intent Vocabulary v1

> Companion design doc to [SLICE_D_DESIGN.md](SLICE_D_DESIGN.md). Slice (d) shipped the renderer and one example component; slice (e) ships the actual vocabulary the dashboard contract YAML will reach for.

## Context

Slice (d) shipped the React shell with a single concrete intent (`metric.counter`) that proved the pipeline works. Slice (e) builds out the rest of the v1 vocabulary so the pilot's three pages — and any contributor's manifest — can render real admin UI without falling back to `UnknownIntent`. **Every component is built on shadcn/ui** (Base UI primitives + Tailwind), giving the dashboard accessible, themed, polished UI without bespoke styling work.

> **Slice (e.5) note:** the original draft of this slice wired shadcn's Radix variant; the implementation was swapped to shadcn's **Base UI** (`@base-ui-components/react`) variant per a later directive. Public component imports (`@/components/ui/*`) and the v1 vocabulary are unchanged; only the primitive layer underneath shifted.

Slice (e) also retroactively refactors the components written in slice (d) (`PageShell`, `MetricCounter`, fallbacks) onto shadcn — keeping the codebase consistent and avoiding a mixed primitive/non-primitive split.

## Architecture Decisions (locked in)

| Decision | Choice | Rationale |
|---|---|---|
| UI primitive layer | **shadcn/ui** (vendored — copied into `src/components/ui/`, NOT npm dependency) | Industry default for React+Tailwind admin tools; full control over the components since they live in our tree; no upstream dependency churn. |
| Icon set | **lucide-react** | shadcn's standard icon companion; tree-shakable; comprehensive. |
| Path alias | `@/*` → `src/*` | shadcn convention; clean imports across the runtime. |
| Theme tokens | CSS variables (HSL) for `background`, `foreground`, `primary`, `card`, `border`, etc.; `.dark` class flips them. | shadcn convention; supports light + dark out of the box; deployment can override colors via CSS without rebuilding. |
| Dark mode | Class-based (`.dark` on `<html>`); user toggle persisted via Zustand + localStorage | Standard pattern; respects `prefers-color-scheme` on first load. |
| Form library | **react-hook-form** + **zod** for validation, wrapped in shadcn's `Form` primitive | Standard shadcn/ui form pattern; type-safe schemas. |
| Table | shadcn `Table` for `resource.list`; sorting/filtering done client-side for v1 (server-side query params are slice (f)+) | Keeps the v1 component tractable; the contract already supports server-side filters via query params. |
| Drawer / sheet | shadcn `Sheet` for `resource.list.detailDrawer` | Slides in from the right; matches admin-tool conventions. |

## Vocabulary scope (v1)

### Refactored from slice (d)
| Intent | shadcn primitives | Behavior |
|---|---|---|
| `page.shell` | `header`, lucide icon, `Avatar`, `DropdownMenu` for the user menu, `Separator`, theme toggle | Topbar + main slot; principal info + dark mode toggle live in the topbar. |
| `metric.counter` | `Card`, `CardHeader`, `CardTitle`, `CardContent` | Subscribed counter; renders a numeric value with a label. |
| `UnknownIntent` (fallback) | `Alert` (warning variant) | Graceful degradation when the registry doesn't have an intent. |
| `LoadingNode` (fallback) | `Skeleton` | Replaces the bare "Loading…" string. |
| `ErrorNode` (fallback) | `Alert` (destructive variant) | Replaces the bare red box. |

### New in slice (e)
| Intent | shadcn primitives | Behavior |
|---|---|---|
| `resource.list` | `Table`, `TableHead/Body/Row/Cell`, `Sheet` for detailDrawer slot, `Skeleton` while loading | Renders rows from a query intent; columns from `node.props.columns`; row click opens the `detailDrawer` slot in a Sheet; renders the `rowActions` slot per row. |
| `resource.detail` | `Card`, `dl/dt/dd` typography, `Skeleton` | Renders a fetched record's fields. |
| `dashboard.grid` | CSS grid (Tailwind `grid grid-cols-*`), no shadcn-specific primitive | Lays out widget children from the `widgets` slot in a responsive grid. |
| `form.edit` | `Form` (shadcn wrapper around react-hook-form), `Button` for submit | Wraps fields, runs the `op` command on submit. Pre-populates from `node.data` (a query intent). |
| `form.field` | `FormField`, `FormLabel`, `FormControl`, `FormDescription`, `Input`, `Select`, `Checkbox`, `Textarea` | Branches by `node.props.kind`. |
| `action.button` | `Button` (variants: default, destructive, outline) | Issues a `command` envelope when clicked; `confirm` prop opens shadcn `AlertDialog` first. |
| `action.menu` | `DropdownMenu`, `DropdownMenuItem` | Renders a list of action.button-shaped items in a popover. |
| `action.divider` | `DropdownMenuSeparator` (or `Separator` outside menus) | Visual separator. |
| `audit.tail` | `ScrollArea`, monospace `<code>` rows | Append-mode subscription; new entries push onto the bottom; auto-scroll until user scrolls up. |

## Theme tokens

```css
:root {
  --background: 0 0% 100%;
  --foreground: 222.2 84% 4.9%;
  --card: 0 0% 100%;
  --card-foreground: 222.2 84% 4.9%;
  --primary: 222.2 47.4% 11.2%;
  --primary-foreground: 210 40% 98%;
  --muted: 210 40% 96.1%;
  --muted-foreground: 215.4 16.3% 46.9%;
  --border: 214.3 31.8% 91.4%;
  --input: 214.3 31.8% 91.4%;
  --ring: 222.2 84% 4.9%;
  --destructive: 0 84.2% 60.2%;
  --destructive-foreground: 210 40% 98%;
  --radius: 0.5rem;
}
.dark {
  --background: 222.2 84% 4.9%;
  --foreground: 210 40% 98%;
  --card: 222.2 84% 4.9%;
  --card-foreground: 210 40% 98%;
  --primary: 210 40% 98%;
  --primary-foreground: 222.2 47.4% 11.2%;
  --muted: 217.2 32.6% 17.5%;
  --muted-foreground: 215 20.2% 65.1%;
  --border: 217.2 32.6% 17.5%;
  --input: 217.2 32.6% 17.5%;
  --ring: 212.7 26.8% 83.9%;
  --destructive: 0 62.8% 30.6%;
  --destructive-foreground: 210 40% 98%;
}
```

These mirror shadcn's "slate" defaults. Deployments override via a CSS file shipped alongside the embedded bundle.

## File Structure (additions to slice d)

```
extensions/dashboard/contract/shell/
  components.json                    # shadcn config
  src/lib/utils.ts                   # cn() helper
  src/lib/theme.ts                   # Zustand theme store with localStorage persistence
  src/components/ui/                 # vendored shadcn primitives (one file per component)
    button.tsx
    card.tsx
    alert.tsx
    skeleton.tsx
    separator.tsx
    avatar.tsx
    dropdown-menu.tsx
    sheet.tsx
    table.tsx
    scroll-area.tsx
    form.tsx
    input.tsx
    label.tsx
    select.tsx
    checkbox.tsx
    textarea.tsx
    alert-dialog.tsx
  src/components/theme-toggle.tsx    # Sun/Moon button
  src/intents/
    page.shell.tsx                   # REFACTORED on shadcn
    metric.counter.tsx               # REFACTORED on shadcn
    resource.list.tsx                # NEW
    resource.detail.tsx              # NEW
    dashboard.grid.tsx               # NEW
    form.edit.tsx                    # NEW
    form.field.tsx                   # NEW
    action.button.tsx                # NEW
    action.menu.tsx                  # NEW
    action.divider.tsx               # NEW
    audit.tail.tsx                   # NEW
    register.ts                      # UPDATED to register all of the above
  src/runtime/fallbacks.tsx          # REFACTORED on shadcn (Alert / Skeleton)

extensions/dashboard/contract/shell/ARCHITECTURE.md   # NEW: how to author intent components
extensions/dashboard/contract/shell/README.md         # EXPANDED
```

## Verification

- Existing 13 tests stay green after the shadcn refactor.
- 1 smoke test per new intent (~9 new tests): renders given a representative `GraphNode` + props.
- `pnpm build` clean, bundle stays under 350KB gzipped (shadcn pulls primitive deps — budget bumps from 250KB).
- `pnpm lint` clean.
- `go build ./...` and `go test ./extensions/dashboard/...` clean.

## Out of Scope (still future)

- Slice (f): contributor migration + templ retirement.
- Server-side filtering/sorting/pagination for `resource.list` (client-side covers the v1 cases).
- Custom column rendering via the `customCell.<col>` slot (designed in slice (a), but no concrete component yet).
- Iframe escape-hatch component.
- Browser E2E (Playwright).
- Internationalization.
