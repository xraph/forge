# Deployment implementation verification

Checked on 9 October 2026 in the primary checkout on `main`.

## Implemented workbench

`forge deploy start` serves the authenticated embedded React page and calls the
same Engine as the CLI. The page uses dashboard-kit 0.2.0 shared components.
Files are the default; SQLite and PostgreSQL own deployment settings and state
when selected. SQL authority never falls back to files during an outage. Schema 2
stores only the deployment section and removes runtime fields from older SQL
settings while advancing their revision. It does not rewrite old database backups.

`TestWorkbenchComposeLifecycle` launches the actual CLI binary against disposable
Atlas stacks, with separate files and SQLite runs. Both runs authenticate, serve
the embedded page, save settings with CAS, deploy the complete stack, receive
provider progress, observe healthy workloads, apply only the worker while retaining
API and gateway, read retained API logs, cancel a provider run and restart the page
with saved settings and release history. The files run also proves a concurrent
editor conflict. Owned containers, volumes and images are cleaned up.

The separate PostgreSQL container contracts cover revision CAS, fencing, lease
cancellation, journals and retired authority. Server tests cover token replay,
Host/Origin/header denial, malformed and oversized input, anchored paths,
concurrent mutations, stale approval, cancellation, event replay and shutdown.
The UI tests cover saved target gates, service scope, conflicts, exact approval,
failed operation display, Git settings, SQL plan handoff and late status responses
from another target.

The actual page was checked at 1280px and 390px in light and dark mode. Keyboard
mobile navigation, Escape dismissal, profile/theme persistence and an actual
service-subset save/reload passed. A separate browser session built a plan, approved
its exact hash and applied API plus gateway against an owned Compose stack. The
page streamed the build, backend, bucket, migration and rollout operations, then
showed both workloads ready and a healthy recorded release. Its temporary stack
was removed after verification. The narrow document has no horizontal overflow,
and no browser console warnings or errors were observed. These are targeted
checks, not a screen-reader or forced-colors qualification.

## Checks and limits

UI tests, ESLint, Prettier, TypeScript and the production asset build pass. CLI
build, deployment tests and workbench race checks pass. Documentation MDX/route
and TypeScript checks, production build and changed-document formatting pass.
The new CI workbench job checks the committed asset build, server boundaries,
real Compose workbench lifecycle, required registry authentication and PostgreSQL
contracts. Workflow syntax passes actionlint.

`TestWorkbenchRegistryConnection` is skipped on the local Docker Desktop endpoint,
which cannot reach the private loopback registry fixture. Native Linux CI requires
that test to run, so a skip cannot qualify the connection. [CI run 38016386197](https://github.com/xraph/forge/actions/runs/38016386197) passed
the private-registry login/denial, files/SQLite Compose lifecycle and PostgreSQL
contracts on Linux. A real GitHub Container
Registry account was not supplied. The browser check inspects the login form and
separate pull credentials without claiming a successful third-party login.

Root `GOWORK=off make l` remains blocked by baseline/concurrent findings outside
this deployment scope. The final run failed in 20 modules and passed in 19.
Full documentation lint reports 100 errors, 39 warnings and 113 informational
diagnostics in existing files/configuration. Scoped deployment lint and the changed
Markdown/MDX formatting and compiler checks are the relevant green checks.

Managed account apply, controller reconciliation and hosted provider qualification
remain separate implementation gates. Capability levels must reflect the evidence
for each adapter.

The historical mock checks below describe the static proposal only.

# Historical mock verification

Checked on 9 October 2026 in the primary Forge checkout on `main`. This change adds review artifacts, documentation and a static interactive mock. It does not implement provider deployment or the proposed CLI commands.

## Passing checks

- `GOWORK=off make f`: formatted all 38 modules successfully. The formatting diff contains no Go changes.
- Prettier 3.6.2 format/check on the changed Markdown, MDX, HTML and navigation JSON files.
- `npm run lint -- public/mock/deploy.html 'content/docs/forge/(cli)/meta.json'` in `docs`: both changed supported files pass Biome. Markdown/MDX formatting is checked with Prettier.
- `npm run types:check` in `docs`: MDX generation, route type generation and TypeScript checks pass.
- `npm run build` in `docs`: production documentation build passes.
- `node --check` on the mock's extracted inline JavaScript.
- Parse the downloaded JSON bundle and its seven YAML files. The bundle has eight files and identifies itself as `proposal-not-deployable`.
- `git diff --check`.

## Browser checks

The local docs app serves the mock at `/mock/deploy.html` and the guide at `/docs/forge/deployment-workbench`.

- Inspect the mock at desktop width and 390px. The narrow page has no document overflow; the service table can scroll inside its own container without shrinking its text.
- Switch Kubernetes, Compose, Render and DigitalOcean presets. Service counts, data placement, network labels, file output and target options update.
- Require Redis JSON/search on a managed preset. The mock reports the unverified capability and blocks the rehearsal.
- Save a renamed project and replica count, reload, and verify the browser draft survives.
- Download the proposal bundle and inspect named Grove, Trove metadata/store and Redis bindings.
- Open the worker contract. It declares no listener and shows the proposed heartbeat requirement.
- Navigate plan tabs with the keyboard.
- Rehearse a migration failure. App rollout stops and the event says data is retained.
- Rehearse a healthy rollout. Every operation is labeled as simulated.
- Inspect the rendered docs page and sidebar link. The mock link resolves to the local UI.
- Check the mock's browser error log. No errors were reported.

## Existing check failures

The first `make l` run uses the existing local `go.work`, which contains only the root and CLI modules. Nested modules fail workspace membership checks. Running and rerunning `GOWORK=off make l` after formatting removes that workspace problem but still fails in 21 modules; 17 pass. Reported issues include existing unchecked errors, type/style diagnostics and test lint in files untouched by this change.

The full `npm run lint` in `docs` reports 100 errors, 39 warnings and 4 informational diagnostics in unchanged files/configuration after the mock's own diagnostics are fixed. These include existing formatting/import problems, SVG accessibility diagnostics and a configuration schema-version mismatch. The scoped mock/navigation lint passes.

Those failures remain open. No unrelated Go, shared docs component, dependency or lint configuration changes are included here. The development docs shell also reports an existing SVG hydration mismatch; the new guide renders and the production build passes.

## Not verified

No actual project discovery, file-write server, deployment compiler, provider credentials, cluster access, resource provisioning, migration execution, persisted cloud data, live service authorization, rollback or provider recovery was tested. The mock's browser save is not repository-file persistence. Its files are configuration proposals, and its deployment log is a rehearsal.

Implementation and live qualification gates remain in [the deployment plan](PLAN.md).

## Dashboard UI refinement

The dashboard at `/mock/deploy.html` now has a grouped sidebar, separate navigation views, a desktop icon rail, a mobile drawer, a full-width file browser, keyboard tabs and Light/Dark/System themes. The separate `workbench.html` flow remains available.

The refinement passes scoped Biome, Prettier, inline JavaScript syntax, docs type checks and the production docs build. Desktop, 390px and 320px browser checks cover navigation, theme/sidebar persistence, System theme selection, keyboard scrolling and file selection, service inspection, save/reload and healthy/failed rehearsals. The narrow document has no horizontal overflow. The mock browser error log reports no errors. The downloaded bundle contains eight files and seven parseable YAML documents; secret placeholders in runtime overlays are quoted so flow mappings remain valid.

`GOWORK=off make f` passes with no Go diff. The initial `GOWORK=off make l` reruns fail in 21 modules and pass in 17. The final rerun fails in 23 modules and passes in 15 while concurrent auth changes are present in the checkout. Those auth files are outside this UI change and remain untouched by its commit. The full docs lint rerun reports 100 errors, 39 warnings and 113 informational diagnostics outside the refined mock; its scoped lint is clean. These broader failures remain open.

See [the UI review](UI_REVIEW.md) for source conventions, bounded palette checks, skill coverage and checks that remain unverified. The deployment engine remains proposed.

## Saved targets and delivery workflows

Browser checks cover target cards ahead of the sidebar, three saved profiles and reload, service selection, an excluded API binding, empty-selection blocking, working ZeroState actions, and worker-only resource/overlay scope. The worker inspector assigns no Trove store or API migration. The Render CI-check trigger survives reload, and per-service Git source/Dockerfile fields appear in a disclosure. Kubernetes GitOps delivery records a separate manifest repository/path proposal.

Registry authentication/access buttons explicitly rehearse their results. Host-only images are blocked on a remote platform. Invalid database secret references block saving and deployment review. Files are the default; PostgreSQL and SQLite choices update proposed CLI arguments without connecting a database. Unqualified broker container recipes and platform environment creation block review. A worker-only healthy rehearsal creates no gateway route or API migration. A Git-backed API failure rehearsal stops at migration and retains data. Every event is simulated.

The worker-only download contains eight files and seven parseable YAML documents, with only a worker overlay and Grove, Redis and NATS bindings. The final Render download contains ten files and nine parseable YAML documents. It preserves three target profiles, Compose's worker selection, Render's Git trigger and service build paths, Kubernetes GitOps delivery, broker placement and default file persistence. Both bundles say `proposal-not-deployable`.

The foreground preview was checked at its normal 1113px width, 390px and 320px. Narrow documents have no horizontal overflow. Mobile navigation returns to the working content, and the header icons stay 36px square in a horizontal row. Light, Dark and System themes work. The viewport override was reset. The final preview reports no browser errors; an early topology redraw error found during implementation was corrected. The updated guide renders its link to the tracked workflow design.

Scoped `npm run lint -- public/mock/deploy.html`, Prettier 3.6.2 format/check, extracted inline JavaScript syntax, `npm run types:check`, `npm run build` and `git diff --check` pass. These are the documented docs lint/format equivalents. No Go source is changed.

`GOWORK=off make f` returns zero and reports 38 modules, with no Go formatting diff. Its log also reports existing `go.mod` update requirements for webtransport, streaming and webrtc. The Make target does not propagate those errors, so its zero exit does not establish that every module's formatter completed. `GOWORK=off make l` fails in 21 modules and passes in 17. Full docs lint reports 100 errors, 39 warnings and 113 informational diagnostics outside this mock. Those broader failures remain open; their files are outside this commit.

Actual provider Git connections, registry authentication/build/push/pull, controller reconciliation, broker provisioning/recovery, environment creation, repository writes, database persistence/migration/locking and live service calls remain unverified. See [the workflow design](WORKBENCH_WORKFLOWS.md) for implementation and acceptance gates.

## Managed exports, publication and controller handoff

Render and DigitalOcean App Platform exports validate against embedded, pinned upstream schemas. Tests cover internal service references, provider secret placeholders, existing DigitalOcean database clusters, branch build triggers and rejection of unsupported resource features. No disposable cloud account has been qualified; both adapters stay at `validated` and reject lifecycle operations. Kubernetes GitOps exports pass Kustomize parsing and enumerate existing Secret requirements. Controller sync, pruning and migration ordering remain outside this handoff.

The [Deploy Workbench job](https://github.com/xraph/forge/actions/runs/38019154594) passes on native Linux. It qualifies private-registry login, denied login, reviewed image publication, CLI replay, immutable digests and unchanged workload state. Files and SQLite lifecycle fixtures pass in 200.80 seconds; registry publication passes in 13.27 seconds and PostgreSQL authority contracts in 1.51 seconds. The complete repository workflow still fails unrelated baseline jobs. Docker Desktop's loopback registry fixture and actual GHCR credentials remain unqualified.
