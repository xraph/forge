# Deployment proposal verification

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
