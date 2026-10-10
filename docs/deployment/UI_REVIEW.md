# Deployment dashboard UI review

Scope: `docs/public/mock/deploy.html`, its seven navigation views, dialogs, file browser and themes. This is a static HTML/CSS/JavaScript proposal served by the docs app. The separate `workbench.html` mock keeps its existing flow.

The shell follows the [shadcn sidebar composition and widths](https://ui.shadcn.com/docs/components/base/sidebar): 256px desktop navigation, 288px mobile navigation, a header, content groups and a footer. The compact controls use the docs product's 36px button size. Color roles follow [shadcn's theme model](https://ui.shadcn.com/docs/theming), translated into local CSS tokens without adding a component dependency.

Project conventions inspected: the supplied AGENTS.md, `docs/src/components/ui/button.tsx`, `docs/src/app/global.css`, `docs/package.json` and `docs/biome.json`. The mock preserves Forge's orange brand treatment, compact spacing and plain labels.

## Scope and coverage

The better-interface skill requested six owning skill reviews. Only better-ui is installed. The independent browser and source checks below were performed; they do not substitute for the unavailable domain reviews.

| Domain        | Evidence inspected                                                                                            | Result                                                                                                    |
| ------------- | ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Accessibility | Keyboard tabs, file-tree focus, native dialogs and named scrolling regions                                    | Not reviewed under better-accessibility; the owning skill is unavailable. Targeted checks passed.         |
| Layout        | Desktop, 390px and 320px renders, sidebar and drawer                                                          | Not reviewed under better-layout; the owning skill is unavailable. Targeted checks passed.                |
| Writing       | Existing proposal labels and updated mock guide paragraph                                                     | Not reviewed under better-writing; the owning skill is unavailable. Rex voice and humanizer were applied. |
| Typography    | Control, table and diagram rendering in both themes                                                           | Not reviewed under better-typography; the owning skill is unavailable. Targeted checks passed.            |
| Color         | Named palette pairs and theme rendering                                                                       | Not reviewed under better-colors; the owning skill is unavailable. Targeted checks passed.                |
| UI polish     | Surface hierarchy, segmented tabs, icon sizing, modal containment, hover and theme transitions in deploy.html | Clear in inspected states.                                                                                |

## Findings

No actionable UI-polish findings remain in the inspected states. The refinement replaces scroll-only navigation with views, gives the file editor its own layout, and keeps the service diagram's text readable through contained scrolling on mobile. Sidebar, tabs, inputs, dialogs, status badges and code surfaces share the theme tokens.

## Verification

- Inspect the normal desktop viewport and 390px/320px layouts. The document width matches the viewport; tables and the diagram scroll inside their own panels.
- Open the mobile navigation drawer and choose a view. It closes and returns navigation to the desktop shell when the viewport changes.
- Select Light, Dark and System. The rendered preference is correct and survives reload; System matches the browser's current color-scheme preference.
- Collapse navigation, reload and expand it. Accessible control names remain available in the icon rail.
- Use arrow keys and Home on the release and plan tabs. Selection and focus update together.
- Choose a file with Enter. The preview and selected row update while keyboard focus stays on that row.
- Scroll the narrow connection diagram with arrow keys. Labels retain their normal rendered size.
- Open the worker inspector and close it with Escape. Its summary shows the type, exposure, health strategy and bindings; the full contract remains available.
- Answer both decisions, save through the diff dialog and reload. The browser draft persists and the deployment review control remains available.
- Switch all four provider presets. Rehearse a failed migration and a healthy local rollout. Activity remains explicitly simulated.
- Palette checks: the selected normal text/status pairs exceed 4.5:1 in both themes. Input borders against their panel exceed 3:1. This is a bounded token check, not an exhaustive accessibility audit.
- Scoped Biome, JavaScript syntax, docs type checks and production build pass. The existing broad lint failures are recorded in [VERIFICATION.md](VERIFICATION.md).

Not verified: screen-reader operation, forced-colors rendering, live changes to the OS theme preference, actual file writes or deployment. Reduced-motion handling is inspected in source; no movement animation is added.

## Verdict

Approve the inspected UI-polish scope. Accessibility, layout, writing, typography and color owning-skill reviews were not available; the targeted checks above define the verification boundary.

## Target and delivery configuration follow-up

This pass adds large target cards ahead of the workbench, saved profiles, service selection, broker settings, image/registry and Git delivery screens, and environment/persistence settings. It uses the existing neutral tokens, 256px sidebar, native controls and Light/Dark/System themes. Header, file and registry actions use 36px icons with accessible names and focus/hover tooltips. Source/Dockerfile details sit in a disclosure so the Git screen stays compact.

Desktop, 390px and 320px checks cover the new views, mobile drawer, saved profiles, Git triggers, dependency diagnostics, empty selections and icon sizing. Counts, topology, operations and overlays follow the apply scope, with shared data retained. The reusable mock ZeroState adapter provides a left-aligned illustration, explanation and working action for empty resource/connection views. No walkthrough button is added.

Authentication and deployment results remain simulated or unqualified. This UI review does not qualify a provider, registry, database or messaging integration. The earlier limits on formal multi-domain skill coverage still apply. Checks and broader lint failures are recorded in [VERIFICATION.md](VERIFICATION.md).

## Implemented embedded interface

`cmd/forge/internal/deploy/workbench/_ui` contains the React workspace embedded
by the CLI. It uses published `@forge-go/dashboard-kit` 0.2.0 primitives and tokens,
including the shared Sidebar, Tabs, Table, Sheet, Dialog, Tooltip and illustrated
ZeroState. The sidebar is 240px, primary controls are 32px and the review panel
stays beside working content at desktop widths. The target gate uses larger cards.

The actual authenticated page was checked at 1280px and 390px in light and dark
mode. Target selection, real service-subset save/reload, persistent theme/profile,
keyboard mobile navigation, Escape dismissal and registry form fields passed.
The narrow document has no horizontal overflow; wide service tables scroll inside
their panels. No browser console errors or warnings were observed. Credentials
were not entered into a real third-party registry during these UI checks.

Behavior tests cover exact approval invalidation, stale server rejection, dirty
conflict recovery, removed profile recovery, fresh project initialization, nullable
wire arrays, named targets, service scope and Git source settings. The SQL plan
handoff uses the full authority hash, so it does not assume a local JSON file.
The historical mock review above remains limited to that mock.
