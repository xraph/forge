# Changelog

## [1.9.8](https://github.com/xraph/forge/compare/v1.9.7...v1.9.8) (2026-08-20)


### ⚠ BREAKING CHANGES

* docs(cli): document the grove migration commands and the breaking changes
* docs(client): document the generated auth surface and its breaking change


### Features

* **models:** feat(models)!: port the nine base models to grove ([ccca2ba8](https://github.com/xraph/forge/commit/ccca2ba8))
* **cli:** add forge db adopt to carry bun migration state to grove ([c095f251](https://github.com/xraph/forge/commit/c095f251))
* **cli:** run lock, unlock and mark-applied through grove ([9aec2b17](https://github.com/xraph/forge/commit/9aec2b17))
* **cli:** run db migration commands through the grove runner ([af970fae](https://github.com/xraph/forge/commit/af970fae))
* **cli:** build the migration runner on grove instead of bun ([8a104686](https://github.com/xraph/forge/commit/8a104686))
* **cli:** emit grove migrations from create-go ([22872635](https://github.com/xraph/forge/commit/22872635))
* **cli:** register SQL migrations with grove via go:embed ([eef46028](https://github.com/xraph/forge/commit/eef46028))
* **cli:** scaffold the migrations package on grove's registry ([dd0db736](https://github.com/xraph/forge/commit/dd0db736))
* **cli:** split bun migration names into grove name and version ([37633728](https://github.com/xraph/forge/commit/37633728))
* **cli:** resolve grove drivers from a DSN scheme ([68539b47](https://github.com/xraph/forge/commit/68539b47))
* **client:** give the Go capability surface CanCall and MissingCapabilities ([5921c2a2](https://github.com/xraph/forge/commit/5921c2a2))
* **client:** add roles and permissions to the TypeScript capability surface ([50fdd0a7](https://github.com/xraph/forge/commit/50fdd0a7))
* **client:** give the Go client a capability surface ([ac88a097](https://github.com/xraph/forge/commit/ac88a097))
* **client:** collect declared roles and permissions for the generators ([f1dd8a89](https://github.com/xraph/forge/commit/f1dd8a89))
* **client:** resolve declared authorization into the IR ([524161e9](https://github.com/xraph/forge/commit/524161e9))
* **router:** carry declared roles and permissions as x-forge-authz ([fb869c74](https://github.com/xraph/forge/commit/fb869c74))
* **router:** declare role and permission requirements on routes ([d48ae310](https://github.com/xraph/forge/commit/d48ae310))
* **client-core:** close six of the runtime's known gaps ([45063890](https://github.com/xraph/forge/commit/45063890))
* **client:** let a transport send cross-origin cookies ([71d04c81](https://github.com/xraph/forge/commit/71d04c81))
* **client:** emit the security scheme table the manifest already declared ([c660dbab](https://github.com/xraph/forge/commit/c660dbab))
* **client:** one auth path for every transport, and opt-in session jars ([7ec7c585](https://github.com/xraph/forge/commit/7ec7c585))
* **client:** emit one auth field per scheme and apply each in its own location ([8e156b8f](https://github.com/xraph/forge/commit/8e156b8f))
* **client:** derive Go field names that survive camelCase scheme keys ([8e8c2922](https://github.com/xraph/forge/commit/8e8c2922))


### Bug Fixes

* **auth:** fix(auth)!: restore the module go.mod deleted in a7d5e338 ([7386f70a](https://github.com/xraph/forge/commit/7386f70a))
* **cli:** port forge generate to grove ([810dd4ab](https://github.com/xraph/forge/commit/810dd4ab))
* **cli:** make forge db adopt work on real bun tables and reachable after an upgrade ([450fd408](https://github.com/xraph/forge/commit/450fd408))
* **cli:** pin the generated runner's grove version and normalize DSN scheme case ([5e0e9235](https://github.com/xraph/forge/commit/5e0e9235))
* **cli:** let dry-run adopt survive a missing grove migration table ([c91d814b](https://github.com/xraph/forge/commit/c91d814b))
* **cli:** scope adopt's applied-check by group and stop losing partial reads ([cf49ce01](https://github.com/xraph/forge/commit/cf49ce01))
* **cli:** thread the resolved DSN into the grove runner ([2462cfd7](https://github.com/xraph/forge/commit/2462cfd7))
* **cli:** scope grove migration groups by app instead of table names ([e081ee41](https://github.com/xraph/forge/commit/e081ee41))
* **cli:** gofmt database_grove_test.go ([52d8454e](https://github.com/xraph/forge/commit/52d8454e))
* **client:** skip apiKey schemes whose location apply cannot encode ([88c4e37c](https://github.com/xraph/forge/commit/88c4e37c))
* **client:** normalise roles and permissions in EndpointAuthorization ([bcd4df74](https://github.com/xraph/forge/commit/bcd4df74))
* **router:** read subject scopes from "auth.subject.scopes" ([396f5340](https://github.com/xraph/forge/commit/396f5340))
* **client:** stop citing WithRequiredRole/WithRequiredPermission in generated comments ([127d1f65](https://github.com/xraph/forge/commit/127d1f65))
* **client:** populate Authorization on the fallback IR path ([8b9b8b5f](https://github.com/xraph/forge/commit/8b9b8b5f))
* **client:** warn instead of silently dropping colliding capability identifiers ([14e607fd](https://github.com/xraph/forge/commit/14e607fd))
* **client:** read the auth metadata key the router actually writes ([3f9ac55d](https://github.com/xraph/forge/commit/3f9ac55d))
* **client:** make the generated Go client compile in every configuration ([fb6fe635](https://github.com/xraph/forge/commit/fb6fe635))
* **client:** close the gaps the whole-branch review found in generated auth ([be0c40c8](https://github.com/xraph/forge/commit/be0c40c8))
* **client:** make the generated Go client compile ([225a63ca](https://github.com/xraph/forge/commit/225a63ca))
* **client:** revert auth field collision detection to exact-match ([38300ada](https://github.com/xraph/forge/commit/38300ada))
* **client:** handle multi-byte UTF-8 in goFieldName; add non-ASCII and all-digit regression tests ([0553f880](https://github.com/xraph/forge/commit/0553f880))
* **client:** keep cookie parameters in the introspector's own IR builder too ([2f6a7bf5](https://github.com/xraph/forge/commit/2f6a7bf5))
* **client:** keep cookie parameters, warn on unknown locations, sort introspected security schemes ([41c2dcc9](https://github.com/xraph/forge/commit/41c2dcc9))
* **client:** document the real apiKey parameter name, not the scheme key ([352d24c9](https://github.com/xraph/forge/commit/352d24c9))
* **client:** keep an apiKey scheme's wire name through the parser ([3b30f77c](https://github.com/xraph/forge/commit/3b30f77c))


### Refactoring

* **extensions:** refactor(extensions)!: remove the database, ai and gateway extensions ([7715de1e](https://github.com/xraph/forge/commit/7715de1e))
* **cli:** remove the last uptrace/bun dependency from database.go ([ce31c850](https://github.com/xraph/forge/commit/ce31c850))
* **cli:** drop the database extension connection code ([64a11572](https://github.com/xraph/forge/commit/64a11572))
* **extensions:** refactor(extensions)!: remove storage and cron, move hls onto trove ([c0a21853](https://github.com/xraph/forge/commit/c0a21853))
* **auth:** read roles from one place ([d3e9cf6a](https://github.com/xraph/forge/commit/d3e9cf6a))


### Maintenance

* **release:** drop removed extensions from every release surface ([84d0f8aa](https://github.com/xraph/forge/commit/84d0f8aa))
* gen go mods update ([38f6795b](https://github.com/xraph/forge/commit/38f6795b))
* **cli:** correct the go.work setup in cmd/forge/go.mod ([345a9c8e](https://github.com/xraph/forge/commit/345a9c8e))
* **cli:** exercise the generated grove runner end to end ([f6afa234](https://github.com/xraph/forge/commit/f6afa234))
* ignore browser automation scratch at the repo root ([cb4227ac](https://github.com/xraph/forge/commit/cb4227ac))
* **client:** cover the permissions-only arm of capabilitiesNeeded's gate ([afeb76da](https://github.com/xraph/forge/commit/afeb76da))
* **router:** cover WithGroup* role/permission options and sortedUniqueStrings ([c76ab8a2](https://github.com/xraph/forge/commit/c76ab8a2))
* **router:** stop implying WithAnyRole/WithAllPermissions enforce anything ([2ad4b787](https://github.com/xraph/forge/commit/2ad4b787))
* **client:** pin authorization parity between the two IR builders ([890d5d43](https://github.com/xraph/forge/commit/890d5d43))
* stop a degraded GitHub API from failing commit validation ([7b36deca](https://github.com/xraph/forge/commit/7b36deca))
* ran go fix ([43d20ad2](https://github.com/xraph/forge/commit/43d20ad2))
* **client-core:** raise two size budgets for the runtime gap-closing work ([cd335ed8](https://github.com/xraph/forge/commit/cd335ed8))
* ignore node_modules at any depth ([53c360d8](https://github.com/xraph/forge/commit/53c360d8))
* **client:** cover the security dedup and omission branches review flagged ([f17c217d](https://github.com/xraph/forge/commit/f17c217d))
* **changelog:** update CHANGELOG.md for v1.9.7 ([7a10d76d](https://github.com/xraph/forge/commit/7a10d76d))

## [1.9.7](https://github.com/xraph/forge/compare/v1.9.6...v1.9.7) (2026-08-14)


### Bug Fixes

* improved the client gen with service splitting output ([cd4a8ffb](https://github.com/xraph/forge/commit/cd4a8ffb))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.9.6 ([faaca269](https://github.com/xraph/forge/commit/faaca269))

## [1.9.6](https://github.com/xraph/forge/compare/v1.9.5...v1.9.6) (2026-08-14)


### Maintenance

* Revert "chore: relicense Forge under MIT" ([a0f6e766](https://github.com/xraph/forge/commit/a0f6e766))
* **changelog:** update CHANGELOG.md for v1.9.5 ([07fe2904](https://github.com/xraph/forge/commit/07fe2904))

## [1.9.5](https://github.com/xraph/forge/compare/v1.9.4...v1.9.5) (2026-08-09)


### Features

* **react:** hydrate during render, and emit real markup from a server render ([de92a9a9](https://github.com/xraph/forge/commit/de92a9a9))
* **client:** give a query handle a real server snapshot, and export the SSR surface ([2d63f3d6](https://github.com/xraph/forge/commit/2d63f3d6))
* **client:** add hydrate, reviving a payload into a warm cache ([3464fb3e](https://github.com/xraph/forge/commit/3464fb3e))
* **client:** add dehydrate, emitting only entities the exported queries reach ([f3e41d48](https://github.com/xraph/forge/commit/f3e41d48))
* **client:** add peek, settledQueries and restore to the query cache ([9db6836d](https://github.com/xraph/forge/commit/9db6836d))
* **client:** let a settle supply resolved tags instead of a response ([b5f2075b](https://github.com/xraph/forge/commit/b5f2075b))
* **client:** add the SSR wire encoding, with reference-shaped data escaped ([443a0552](https://github.com/xraph/forge/commit/443a0552))
* **client:** emit response and entity types on mutation bindings ([a3a71825](https://github.com/xraph/forge/commit/a3a71825))
* **client:** surface pending optimistic state through every adapter ([addca460](https://github.com/xraph/forge/commit/addca460))
* **client:** project pending placements onto a query's value ([ab38624d](https://github.com/xraph/forge/commit/ab38624d))
* **client:** show a mutation's declared change before the server answers ([d59189f5](https://github.com/xraph/forge/commit/d59189f5))
* **client:** derive a mutation's optimistic target from its tags ([af135637](https://github.com/xraph/forge/commit/af135637))
* **client:** add the overlay stack's entity plane ([3041e0c7](https://github.com/xraph/forge/commit/3041e0c7))
* **client:** open one overlay resolution point in the entity store ([9957bf06](https://github.com/xraph/forge/commit/9957bf06))
* **client:** emit declared scopes as typed capabilities and can() ([0637c181](https://github.com/xraph/forge/commit/0637c181))
* **client:** rank introspected specs with OpenAPI ([7332ab16](https://github.com/xraph/forge/commit/7332ab16))
* **cli:** watch every spec source ([3a840ea9](https://github.com/xraph/forge/commit/3a840ea9))
* **cli:** merge several spec sources into one package ([be85a544](https://github.com/xraph/forge/commit/be85a544))
* **cli:** accept several spec sources ([2b4e8437](https://github.com/xraph/forge/commit/2b4e8437))
* **client:** add ParseFileUnresolved for multi-source merging ([9c1366c0](https://github.com/xraph/forge/commit/9c1366c0))
* **client-core:** skip gap recovery when the server replayed it ([ae33e82c](https://github.com/xraph/forge/commit/ae33e82c))
* **client:** report merge collisions through spec.Warnings ([18706d92](https://github.com/xraph/forge/commit/18706d92))
* **router:** replay missed SSE events on reconnect ([87b97323](https://github.com/xraph/forge/commit/87b97323))
* **router:** WithEventLog route option ([06fbb603](https://github.com/xraph/forge/commit/06fbb603))
* **client:** add MergeSpecs and SourceKind ([3d5bbd71](https://github.com/xraph/forge/commit/3d5bbd71))
* **router:** bounded in-memory event log with honest gap reporting ([f704934d](https://github.com/xraph/forge/commit/f704934d))
* **client:** add MergeSpecs and SourceKind ([25b79ce4](https://github.com/xraph/forge/commit/25b79ce4))
* **router:** event log position codec ([f2e1dea2](https://github.com/xraph/forge/commit/f2e1dea2))


### Bug Fixes

* **client:** thread the entity type through all three adapters ([c19688c5](https://github.com/xraph/forge/commit/c19688c5))
* **client:** do not import a mutation type that types.ts never exports ([57f94934](https://github.com/xraph/forge/commit/57f94934))
* **client:** keep a frame's value when an optimistic overlay is promoted ([794ba2ef](https://github.com/xraph/forge/commit/794ba2ef))
* **client:** stop canonicalising mutation binding type names ([f93b98b0](https://github.com/xraph/forge/commit/f93b98b0))
* **client:** keep client-react buildable and close the size gate ([6cc3fe8f](https://github.com/xraph/forge/commit/6cc3fe8f))
* **client:** keep placement's shape check and entry.value entity-plane only ([acf431e0](https://github.com/xraph/forge/commit/acf431e0))
* **client:** close the optimistic-overlay leak into base, and two related hazards ([b80d2ff8](https://github.com/xraph/forge/commit/b80d2ff8))
* **client:** make touch bump version only on real invalidation ([a281f91f](https://github.com/xraph/forge/commit/a281f91f))
* **client:** keep a schema named Capability from breaking the package ([2f4be706](https://github.com/xraph/forge/commit/2f4be706))
* **client-core:** decode the streaming extension's envelope ([954ed4b2](https://github.com/xraph/forge/commit/954ed4b2))
* **cli:** refresh every source's tracker after a watch regeneration ([7dd06e59](https://github.com/xraph/forge/commit/7dd06e59))
* **cli:** say which source `client list` used when given several ([eb8ba847](https://github.com/xraph/forge/commit/eb8ba847))
* **client:** carry spec warnings through the Go generator ([4d1e71f6](https://github.com/xraph/forge/commit/4d1e71f6))
* **client:** drop the duplicate declarations the merge only warned about ([3099edae](https://github.com/xraph/forge/commit/3099edae))
* **router:** warn once when another layer's event IDs are refused ([f4baf78c](https://github.com/xraph/forge/commit/f4baf78c))
* **router:** make event log sequence numbers global to the log ([4255314e](https://github.com/xraph/forge/commit/4255314e))
* **cli:** stop the shared watch debouncer from dropping a source's change ([54fe56ca](https://github.com/xraph/forge/commit/54fe56ca))
* **cli:** restore loud config-source validation and cover the merge path ([67a9e07a](https://github.com/xraph/forge/commit/67a9e07a))
* **sse-replay:** close the seams between log, replay wiring, and client ([5df0814f](https://github.com/xraph/forge/commit/5df0814f))
* **client:** correct ParseFileUnresolved rationale and test after review ([8fb48181](https://github.com/xraph/forge/commit/8fb48181))
* **client-core:** key deferred gap recovery by endpoint, validate resumed payload ([56feb1b0](https://github.com/xraph/forge/commit/56feb1b0))
* **client:** widen sameSchemaShape to catch enum, ref, and polymorphism drift ([9108b64b](https://github.com/xraph/forge/commit/9108b64b))
* **streaming:** survive an SSE route that owns its own event IDs ([a1d8889e](https://github.com/xraph/forge/commit/a1d8889e))
* **router:** reject caller IDs and serialize append-send on resumable streams ([42dc940e](https://github.com/xraph/forge/commit/42dc940e))
* **client-core:** read event before type in the default frame decoder ([4d7e91ef](https://github.com/xraph/forge/commit/4d7e91ef))
* **streaming:** decode the extension's envelope on the client ([cb773c34](https://github.com/xraph/forge/commit/cb773c34))
* **router:** binary frames, write deadlines, and SSE event IDs ([095a3887](https://github.com/xraph/forge/commit/095a3887))


### Refactoring

* **client:** drop the Forge prefix from the adapter APIs ([72dc7be4](https://github.com/xraph/forge/commit/72dc7be4))
* **react:** name the boundary HydrationBoundary, not ForgeHydrationBoundary ([6501d7c5](https://github.com/xraph/forge/commit/6501d7c5))


### Maintenance

* relicense Forge under MIT ([bb9df170](https://github.com/xraph/forge/commit/bb9df170))
* allow CONTRIBUTING.md past the blanket markdown ignore ([b9eb1540](https://github.com/xraph/forge/commit/b9eb1540))
* delete LICENSING.md, add CONTRIBUTING.md, drop the roadmap ([6f783cce](https://github.com/xraph/forge/commit/6f783cce))
* rewrite the README against what the repo actually contains ([c2bcf19c](https://github.com/xraph/forge/commit/c2bcf19c))
* **client:** document SSR dehydrate/hydrate and retire the not-yet-shipped section ([2f3601e4](https://github.com/xraph/forge/commit/2f3601e4))
* **client:** assert the SSR round trip, with reference-shaped keys generated ([f1a8801a](https://github.com/xraph/forge/commit/f1a8801a))
* **client:** document isOptimistic and the OPTIMISTIC symbol ([812430e2](https://github.com/xraph/forge/commit/812430e2))
* **client:** correct two claims that shipped wrong in the last commit ([6d2be467](https://github.com/xraph/forge/commit/6d2be467))
* **client:** document optimistic overlays as shipped ([10b6e169](https://github.com/xraph/forge/commit/10b6e169))
* **client:** cover promote's raw-base evaluation of computed sources ([ecc888c0](https://github.com/xraph/forge/commit/ecc888c0))
* stop tracking superpowers workflow artifacts ([a7e37502](https://github.com/xraph/forge/commit/a7e37502))
* **web-client:** say which field names a stream frame ([8b9e11ee](https://github.com/xraph/forge/commit/8b9e11ee))
* **client:** document repeatable sources, append semantics and watch ([da493008](https://github.com/xraph/forge/commit/da493008))
* **client:** name the merge-resolve test after what it asserts ([efab6068](https://github.com/xraph/forge/commit/efab6068))
* **client:** make the un-decoded parse guard fixture-independent ([eb8f713b](https://github.com/xraph/forge/commit/eb8f713b))
* **web-client:** the default decoder now reads event first ([edd2d665](https://github.com/xraph/forge/commit/edd2d665))
* **web-client:** say which field names a stream frame ([5463788c](https://github.com/xraph/forge/commit/5463788c))
* **deps:** bring the three drifted submodules up to current versions ([aca683fa](https://github.com/xraph/forge/commit/aca683fa))
* **changelog:** update CHANGELOG.md for v1.9.4 ([75ce08b7](https://github.com/xraph/forge/commit/75ce08b7))
* **web-client:** drop the unpublished-packages caveats ([e30a4cf0](https://github.com/xraph/forge/commit/e30a4cf0))

## Unreleased

### Features

* **cmd/forge:** merge several `--from-spec`/`--from-url` sources (and a `.forge-client.yml` `source.sources` list) into one generated client package, so a REST document and a stream document produce a package whose `ops.ts` `streams` table is actually populated instead of staying empty under `--hooks`. `--from-spec`/`--from-url` flag values now **append** to a configured `source.path`/`source.url`/`source.sources` instead of replacing it — a `.forge-client.yml` naming one source plus a flag naming another now generates from both, where previously the flag would have silently dropped the configured source. A source that fails to parse, or a merged specification with neither endpoints nor streams, aborts the run rather than degrading to a partial package; `forge client check` follows generate's merge exactly, so it can never under-verify a multi-source configuration. `forge client watch` now watches **every** configured source rather than only the first — a change to any one of them regenerates from all of them, and a burst of simultaneous changes costs one regeneration rather than one per source.

### Bug Fixes

* **cmd/forge:** drop, rather than merely warn about, a route or stream endpoint declared by more than one source. The warning said the first declaration won, but both survived into the generated package — duplicated operations and hooks in TypeScript, and two identically named methods in Go, which does not compile. Collision warnings now also reach the Go generator's output (previously only TypeScript printed them, so the default language was silent), and name the document the kept definition actually came from. `forge client list`, which inspects a single document, now says which source it used when given more than one instead of silently describing the first.

## [1.9.4](https://github.com/xraph/forge/compare/v1.9.2...v1.9.4) (2026-08-07)


### Bug Fixes

* **release:** skip the docker pipe so the CLI release can ship ([47820280](https://github.com/xraph/forge/commit/47820280))

## [1.9.1](https://github.com/xraph/forge/compare/v1.9.0...v1.9.1) (2026-07-31)


### Bug Fixes

* **metrics:** name the expfmt validation scheme after prometheus bump ([ccb6ac88](https://github.com/xraph/forge/commit/ccb6ac88))


### Maintenance

* bumped forge deps versions ([c31078f6](https://github.com/xraph/forge/commit/c31078f6))
* **changelog:** update CHANGELOG.md for v1.9.0 ([5084c97f](https://github.com/xraph/forge/commit/5084c97f))

## [1.9.0](https://github.com/xraph/forge/compare/v1.8.3...v1.9.0) (2026-07-30)


### Features

* **app:** add body-size, WebSocket-origin and pprof-guard config ([800a2dde](https://github.com/xraph/forge/commit/800a2dde))


### Bug Fixes

* **router:** validate upgrade origins and harden streaming paths ([dfa114ec](https://github.com/xraph/forge/commit/dfa114ec))
* **router:** prevent cross-request context and interceptor corruption ([ee076e16](https://github.com/xraph/forge/commit/ee076e16))
* **errors:** stop echoing internal error detail to clients ([f3948086](https://github.com/xraph/forge/commit/f3948086))
* **middleware:** close CORS, rate-limit and recovery security gaps ([e5eeac95](https://github.com/xraph/forge/commit/e5eeac95))
* **router:** map middleware errors like handler errors ([0ba5d843](https://github.com/xraph/forge/commit/0ba5d843))


### Maintenance

* updated go mods ([0c6ac3c4](https://github.com/xraph/forge/commit/0c6ac3c4))
* **deps:** bump go-utils to v1.1.2 for pooled-context ownership ([1d220562](https://github.com/xraph/forge/commit/1d220562))
* **changelog:** update CHANGELOG.md for v1.8.3 ([1c087b0b](https://github.com/xraph/forge/commit/1c087b0b))

## [1.8.3](https://github.com/xraph/forge/compare/v1.8.2...v1.8.3) (2026-07-29)


### Bug Fixes

* **cmd/forge:** drop replace directives so go install works ([71e51b11](https://github.com/xraph/forge/commit/71e51b11))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.8.2 ([b9002d0f](https://github.com/xraph/forge/commit/b9002d0f))

## [1.8.2](https://github.com/xraph/forge/compare/v1.8.1...v1.8.2) (2026-07-29)


### ⚠ BREAKING CHANGES

* docs(client): document FieldNaming/FieldOverrides and the breaking rename


### Features

* **cmd/forge:** add --field-naming and --field-overrides to client generate ([31026fdf](https://github.com/xraph/forge/commit/31026fdf))
* **client/typescript:** skip codec emission under preserve naming ([70984530](https://github.com/xraph/forge/commit/70984530))
* **client/typescript:** encode and decode payloads at the HTTP boundary ([30bacdd4](https://github.com/xraph/forge/commit/30bacdd4))
* **client/typescript:** feat(client/typescript)!: derive client-side property names from FieldNaming ([819b86b2](https://github.com/xraph/forge/commit/819b86b2))
* **client/typescript:** resolve undiscriminated unions structurally ([30db1c37](https://github.com/xraph/forge/commit/30db1c37))
* **client/typescript:** fail generation on field-name collisions ([c0ecef10](https://github.com/xraph/forge/commit/c0ecef10))
* **client:** add FieldNaming and FieldOverrides configuration ([1afb487d](https://github.com/xraph/forge/commit/1afb487d))
* **client/typescript:** generate the per-schema codec table ([86b262ca](https://github.com/xraph/forge/commit/86b262ca))
* **client/typescript:** support multipart, binary and text request bodies ([4d9b41df](https://github.com/xraph/forge/commit/4d9b41df))
* **client/typescript:** type all 2xx responses including non-JSON bodies ([8c94d228](https://github.com/xraph/forge/commit/8c94d228))
* **client/typescript:** emit polymorphic schemas as unions ([463b9464](https://github.com/xraph/forge/commit/463b9464))
* **client/typescript:** emit additionalProperties as index types ([cfdf6c20](https://github.com/xraph/forge/commit/cfdf6c20))
* **client/typescript:** support numeric and boolean enums with escaped literals ([172c41a5](https://github.com/xraph/forge/commit/172c41a5))
* **client/typescript:** map binary and 64-bit integer formats to precise types ([48bb49ec](https://github.com/xraph/forge/commit/48bb49ec))
* **client/typescript:** emit property descriptions and deprecations as JSDoc ([a84b237a](https://github.com/xraph/forge/commit/a84b237a))


### Bug Fixes

* **ci:** install esbuild and skip runtime tests when it is unavailable ([8a1d5cfe](https://github.com/xraph/forge/commit/8a1d5cfe))
* **client/typescript:** emit WebTransport stream classes unconditionally ([033537f9](https://github.com/xraph/forge/commit/033537f9))
* **client/typescript:** register array-of-$ref codecs for streaming endpoints ([91ba992e](https://github.com/xraph/forge/commit/91ba992e))
* **client/typescript:** decode and encode WebTransport payloads ([8f501253](https://github.com/xraph/forge/commit/8f501253))
* **client/typescript:** decode and encode streaming payloads ([5c3b4b2f](https://github.com/xraph/forge/commit/5c3b4b2f))
* **client/typescript:** catch FieldOverrides collisions under preserve naming ([47790027](https://github.com/xraph/forge/commit/47790027))
* **client/typescript:** try every candidate discriminator tag name on encode ([29edb979](https://github.com/xraph/forge/commit/29edb979))
* **client/typescript:** rename union tags/required by TS name when encoding ([974ae4d4](https://github.com/xraph/forge/commit/974ae4d4))
* **client/typescript:** propagate allOf namespace through inline members and fix two data-loss gaps ([8c869a5e](https://github.com/xraph/forge/commit/8c869a5e))
* **client/typescript:** key allOf $ref-member renames under the ref target, not the composition ([e9ea554f](https://github.com/xraph/forge/commit/e9ea554f))
* **client/typescript:** stop printing phantom FieldOverrides keys for nested allOf collisions ([db2d1edc](https://github.com/xraph/forge/commit/db2d1edc))
* **client/typescript:** detect field collisions across the flattened allOf namespace ([d01dd116](https://github.com/xraph/forge/commit/d01dd116))
* **client:** print generation warnings after the CLI spinner stops ([7df1d3b5](https://github.com/xraph/forge/commit/7df1d3b5))
* **client/typescript:** close two codec-table safety gaps found in review ([2c718c5d](https://github.com/xraph/forge/commit/2c718c5d))
* **client/typescript:** recognize allOf as compositional in the codec table ([dd2ec34a](https://github.com/xraph/forge/commit/dd2ec34a))
* **client/typescript:** detect field-name collisions inside nested composites ([1d91bd2c](https://github.com/xraph/forge/commit/1d91bd2c))
* **client/typescript:** split runs of capitals when converting case ([da836c39](https://github.com/xraph/forge/commit/da836c39))
* **client/typescript:** complete the request-body type enumeration ([aa24043a](https://github.com/xraph/forge/commit/aa24043a))
* **client/typescript:** keep fetch timeout/abort live through the body read ([3dd4863a](https://github.com/xraph/forge/commit/3dd4863a))
* **client/typescript:** gate empty-body-to-undefined conversion on the spec, not the bytes ([f2f961eb](https://github.com/xraph/forge/commit/f2f961eb))
* **client/typescript:** resolve empty response bodies to undefined, not {}/Blob ([a72cf07f](https://github.com/xraph/forge/commit/a72cf07f))
* **shared:** tag OAuthFlows fields and close yaml-tag-parity guard blind spot ([f53569b1](https://github.com/xraph/forge/commit/f53569b1))
* **shared:** replace hardcoded struct list with AST walk in yaml tag guard ([c9db9e92](https://github.com/xraph/forge/commit/c9db9e92))
* **shared:** add yaml tags so OpenAPI/AsyncAPI YAML specs parse correctly ([fd795fc8](https://github.com/xraph/forge/commit/fd795fc8))
* **client:** normalise additionalProperties raw decoder output at the parser ([5788bd50](https://github.com/xraph/forge/commit/5788bd50))
* **client/typescript:** fail generation on schema names reserved by streaming types ([1f576617](https://github.com/xraph/forge/commit/1f576617))
* **client/typescript:** gate AuthConfig in websocket.go and sse.go generators ([865c6cf9](https://github.com/xraph/forge/commit/865c6cf9))
* **client/typescript:** gate auth in the generated example test ([97d12567](https://github.com/xraph/forge/commit/97d12567))
* **client/typescript:** gate AuthConfig in the streaming generators ([20ccca8d](https://github.com/xraph/forge/commit/20ccca8d))
* **client/typescript:** dispose fallback abort listeners to stop them leaking on reused signals ([8587005c](https://github.com/xraph/forge/commit/8587005c))
* **client/typescript:** forward abort signals manually when AbortSignal.any is missing ([dc615cb6](https://github.com/xraph/forge/commit/dc615cb6))
* **client/typescript:** keep timeouts with caller signals, throw real errors ([6b5fbbea](https://github.com/xraph/forge/commit/6b5fbbea))
* **client/typescript:** slice first rune, not first byte, in case conversion ([69aa504a](https://github.com/xraph/forge/commit/69aa504a))
* **client/typescript:** preserve interior caps in case conversion ([032caac8](https://github.com/xraph/forge/commit/032caac8))
* **client/typescript:** stop leaf insertion from discarding a namespace ([bfbb5eb8](https://github.com/xraph/forge/commit/bfbb5eb8))
* **client/typescript:** url-encode path parameters ([84545c05](https://github.com/xraph/forge/commit/84545c05))
* **client/typescript:** sort map iteration for deterministic output ([4cf62b0a](https://github.com/xraph/forge/commit/4cf62b0a))
* **client/typescript:** declare require in the Node fallback path ([b93b8f26](https://github.com/xraph/forge/commit/b93b8f26))
* **client/typescript:** escape quotes and backslashes in property keys ([7812acb3](https://github.com/xraph/forge/commit/7812acb3))
* **client/typescript:** quote non-identifier property keys in types.ts ([c8e873c2](https://github.com/xraph/forge/commit/c8e873c2))
* **client/typescript:** extend the configured client class in rest.ts ([99d2d119](https://github.com/xraph/forge/commit/99d2d119))
* **client/typescript:** emit AuthConfig whenever it is referenced ([c89efa11](https://github.com/xraph/forge/commit/c89efa11))
* fixed tests bug ([ada23f6a](https://github.com/xraph/forge/commit/ada23f6a))


### Refactoring

* **router:** remove unreachable ValidateQueryParams ([232cbbf4](https://github.com/xraph/forge/commit/232cbbf4))


### Maintenance

* **client/typescript:** pin codec-id escaping against hostile schema names ([0ee3373e](https://github.com/xraph/forge/commit/0ee3373e))
* **client/typescript:** pin warning-order determinism; fix stale comments ([b6a3da60](https://github.com/xraph/forge/commit/b6a3da60))
* **client/typescript:** add a WebTransport gate fixture ([ab530a38](https://github.com/xraph/forge/commit/ab530a38))
* **client/typescript:** add a NamingPreserve x WS/SSE gate fixture ([0cc62ab3](https://github.com/xraph/forge/commit/0cc62ab3))
* **client/typescript:** use case-shape-sensitive record keys in the e2e proof ([cac5b145](https://github.com/xraph/forge/commit/cac5b145))
* **client/typescript:** prove the naming codec end to end ([f47dfee4](https://github.com/xraph/forge/commit/f47dfee4))
* **client/typescript:** require generated clients to type-check in CI ([3357b4fa](https://github.com/xraph/forge/commit/3357b4fa))
* **client/typescript:** cover websocket and sse generation in the gate ([b84ed6b2](https://github.com/xraph/forge/commit/b84ed6b2))
* **client/typescript:** remove unreachable generateEndpointMethod ([e314ed9b](https://github.com/xraph/forge/commit/e314ed9b))
* **client/typescript:** add generator fixture corpus ([b8fe3f65](https://github.com/xraph/forge/commit/b8fe3f65))
* **client/typescript:** add tsc type-check harness ([351dda71](https://github.com/xraph/forge/commit/351dda71))
* **changelog:** update CHANGELOG.md for v1.8.1 ([b6150470](https://github.com/xraph/forge/commit/b6150470))

## [1.8.1](https://github.com/xraph/forge/compare/v1.8.0...v1.8.1) (2026-07-24)


### Bug Fixes

* **client/typescript:** emit valid TypeScript from the client generator ([cc3364ed](https://github.com/xraph/forge/commit/cc3364ed))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.8.0 ([887f65b5](https://github.com/xraph/forge/commit/887f65b5))

## [1.8.0](https://github.com/xraph/forge/compare/v1.7.2...v1.8.0) (2026-06-17)


### Features

* **metrics:** serve /_/metrics via promhttp with content negotiation ([9483b3c8](https://github.com/xraph/forge/commit/9483b3c8))
* **metrics:** route prometheus export through the bridge; drop forge runtime collector ([51191931](https://github.com/xraph/forge/commit/51191931))
* **metrics:** per-family label union and dedup in prometheus bridge ([364d3cfe](https://github.com/xraph/forge/commit/364d3cfe))
* **metrics:** map forge timers to prometheus summaries ([a12cc0b2](https://github.com/xraph/forge/commit/a12cc0b2))
* **metrics:** map forge histograms to cumulative prometheus buckets ([43319be3](https://github.com/xraph/forge/commit/43319be3))
* **metrics:** client_golang prometheus bridge with counter/gauge mapping ([a467b113](https://github.com/xraph/forge/commit/a467b113))


### Bug Fixes

* **metrics:** _total counter detection, runtime-metrics flag wiring, dashboard metric names ([b1405740](https://github.com/xraph/forge/commit/b1405740))
* **metrics:** nil-guard PrometheusHandler for consistency with Export ([55eda8e7](https://github.com/xraph/forge/commit/55eda8e7))
* **metrics:** last-wins dedup in prometheus bridge collect ([175de970](https://github.com/xraph/forge/commit/175de970))
* **metrics:** skip histogram emission when count is missing ([a1210618](https://github.com/xraph/forge/commit/a1210618))


### Refactoring

* **observability:** retire duplicate prometheus exporter stack ([1146cbb6](https://github.com/xraph/forge/commit/1146cbb6))
* **metrics:** remove dead exporter push loop and legacy prometheus serializer ([d321a1e8](https://github.com/xraph/forge/commit/d321a1e8))


### Maintenance

* fixed mod version ([4b766d7c](https://github.com/xraph/forge/commit/4b766d7c))
* **metrics:** remove dead runtime collector ([6c4a3d91](https://github.com/xraph/forge/commit/6c4a3d91))
* **observability:** add prometheus scrape config, servicemonitor, grafana dashboard ([8f254c53](https://github.com/xraph/forge/commit/8f254c53))
* **changelog:** update CHANGELOG.md for v1.7.2 ([1475ed45](https://github.com/xraph/forge/commit/1475ed45))

## [1.7.2](https://github.com/xraph/forge/compare/v1.7.1...v1.7.2) (2026-06-17)


### Features

* **app:** CentralMigrator interface + CLI central-mode migrate routing ([1d6db7b](https://github.com/xraph/forge/commit/1d6db7b))
* **app:** opt-in CentralMigrations split-phase startup (Register-all -> migrate -> Start-all) ([a87eeed](https://github.com/xraph/forge/commit/a87eeed))


### Bug Fixes

* **cli:** keep default status output unchanged; test central migrate routing ([32bd21b](https://github.com/xraph/forge/commit/32bd21b))


### Refactoring

* refactored migrations to avoid process locks ([8044099](https://github.com/xraph/forge/commit/8044099))


### Maintenance

* **app:** gofmt central migrations files ([d11e40c](https://github.com/xraph/forge/commit/d11e40c))
* **changelog:** update CHANGELOG.md for v1.7.1 ([f8007e2](https://github.com/xraph/forge/commit/f8007e2))

## [1.7.1](https://github.com/xraph/forge/compare/v1.7.0...v1.7.1) (2026-06-16)


### Maintenance

* **changelog:** update CHANGELOG.md for v1.7.0 ([45a5635](https://github.com/xraph/forge/commit/45a5635))

## [1.7.0](https://github.com/xraph/forge/compare/v1.6.9...v1.7.0) (2026-06-13)


### Maintenance

* fixed mods ([4cf17e9](https://github.com/xraph/forge/commit/4cf17e9))
* bumped confy ([9c23773](https://github.com/xraph/forge/commit/9c23773))
* **changelog:** update CHANGELOG.md for v1.6.9 ([0785c9a](https://github.com/xraph/forge/commit/0785c9a))

## [1.6.9](https://github.com/xraph/forge/compare/v1.6.8...v1.6.9) (2026-06-11)


### Maintenance

* **changelog:** update CHANGELOG.md for v1.6.8 ([83d2286](https://github.com/xraph/forge/commit/83d2286))

## [1.6.8](https://github.com/xraph/forge/compare/v1.6.6...v1.6.8) (2026-06-04)


### Maintenance

* updated mods ([ea91617](https://github.com/xraph/forge/commit/ea91617))
* **changelog:** update CHANGELOG.md for v1.6.6 ([2a042b5](https://github.com/xraph/forge/commit/2a042b5))

## [1.6.6](https://github.com/xraph/forge/compare/v1.6.5...v1.6.6) (2026-06-01)


### Features

* add HTTP response wrapper methods for WebSocket hijacking, flushing, and HTTP/2 push support ([10c789d](https://github.com/xraph/forge/commit/10c789d))


### Bug Fixes

* improve PR title and commit validation logic in workflow ([c5a6b49](https://github.com/xraph/forge/commit/c5a6b49))


### Refactoring

* streamline Go module caching in CI workflow ([7783bec](https://github.com/xraph/forge/commit/7783bec))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.6.5 ([3362108](https://github.com/xraph/forge/commit/3362108))
* **deps:** update Kubernetes dependencies to v0.35.5 across multiple extensions ([5099528](https://github.com/xraph/forge/commit/5099528))

## [1.6.5](https://github.com/xraph/forge/compare/v1.6.4...v1.6.5) (2026-06-01)


### Maintenance

* update console message to match CLI command syntax for app generation in multiple plugins ([adb5fea](https://github.com/xraph/forge/commit/adb5fea))
* **changelog:** update CHANGELOG.md for v1.6.4 ([253de3c](https://github.com/xraph/forge/commit/253de3c))

## [1.6.4](https://github.com/xraph/forge/compare/v1.6.2...v1.6.4) (2026-05-14)


### Features

* add OpenTelemetry dependencies for tracing and metrics ([2b49ce7](https://github.com/xraph/forge/commit/2b49ce7))
* **streaming:** implement contract-based dashboard integration ([89b4d7b](https://github.com/xraph/forge/commit/89b4d7b))
* **dashboard/contract/shell:** minimal React app skeleton ([83eee34](https://github.com/xraph/forge/commit/83eee34))
* **cmd:** add dashboard-contract-probe CLI for raw envelope testing ([4b05bb8](https://github.com/xraph/forge/commit/4b05bb8))


### Maintenance

* Add initial HTML structure for Forge Dashboard in dist/index.html ([8a25863](https://github.com/xraph/forge/commit/8a25863))
* **dashboard/contract/shell:** expanded README + ARCHITECTURE.md ([1a3268a](https://github.com/xraph/forge/commit/1a3268a))
* **dashboard/contract:** add slice-a design spec and implementation plan ([30f166c](https://github.com/xraph/forge/commit/30f166c))
* **changelog:** update CHANGELOG.md for v1.6.2 ([1b85cc9](https://github.com/xraph/forge/commit/1b85cc9))

## [1.6.2](https://github.com/xraph/forge/compare/v1.6.1...v1.6.2) (2026-05-05)


### Maintenance

* **changelog:** update CHANGELOG.md for v1.6.1 ([e3ad829](https://github.com/xraph/forge/commit/e3ad829))

## [1.6.1](https://github.com/xraph/forge/compare/v1.6.0...v1.6.1) (2026-05-01)


### Maintenance

* **changelog:** update CHANGELOG.md for v1.6.0 ([dfb1d91](https://github.com/xraph/forge/commit/dfb1d91))

## [1.6.0](https://github.com/xraph/forge/compare/v1.4.5...v1.6.0) (2026-04-13)


### Features

* **pprof:** add pprofIndexPage for runtime profiling visualization ([074bfc7](https://github.com/xraph/forge/commit/074bfc7))
* **metrics:** add time-series storage and querying capabilities ([1109d4c](https://github.com/xraph/forge/commit/1109d4c))


### Bug Fixes

* **health:** update service health check to return healthy status for registered services ([0adf268](https://github.com/xraph/forge/commit/0adf268))


### Maintenance

* Update vessel dependency to v1.0.2 across multiple extensions and examples ([9b0d86a](https://github.com/xraph/forge/commit/9b0d86a))
* Update vessel dependency to v1.0.1 across multiple extensions ([6733054](https://github.com/xraph/forge/commit/6733054))
* **changelog:** update CHANGELOG.md for v1.4.5 ([fc08cce](https://github.com/xraph/forge/commit/fc08cce))

## [1.4.5](https://github.com/xraph/forge/compare/v1.4.4...v1.4.5) (2026-04-03)


### Refactoring

* **cli:** Refactor CLI to support lazy app resolution and enhance migration command handling ([43df33e](https://github.com/xraph/forge/commit/43df33e))


### Maintenance

* Update dependencies across multiple modules to latest versions ([d4285a1](https://github.com/xraph/forge/commit/d4285a1))
* Update dependencies in go.mod and go.sum ([a7d5e33](https://github.com/xraph/forge/commit/a7d5e33))
* **changelog:** update CHANGELOG.md for v1.4.4 ([ae5f172](https://github.com/xraph/forge/commit/ae5f172))

## [1.4.4](https://github.com/xraph/forge/compare/v1.4.3...v1.4.4) (2026-03-31)


### Features

* add health grace period to improve startup stability ([a607adf](https://github.com/xraph/forge/commit/a607adf))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.4.3 ([de7c82d](https://github.com/xraph/forge/commit/de7c82d))

## [1.4.3](https://github.com/xraph/forge/compare/v1.4.2...v1.4.3) (2026-03-30)


### Features

* add panic recovery middleware to enhance server stability ([fbd9743](https://github.com/xraph/forge/commit/fbd9743))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.4.2 ([c54c6f9](https://github.com/xraph/forge/commit/c54c6f9))

## [1.4.2](https://github.com/xraph/forge/compare/v1.4.1...v1.4.2) (2026-03-30)


### Bug Fixes

* update forgeui dependency version to v1.4.1 ([1f32a00](https://github.com/xraph/forge/commit/1f32a00))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.4.1 ([6ab5469](https://github.com/xraph/forge/commit/6ab5469))

## [1.4.1](https://github.com/xraph/forge/compare/v1.4.0...v1.4.1) (2026-03-29)


### Bug Fixes

* update confy dependency version to v0.5.0 across all modules ([8374cea](https://github.com/xraph/forge/commit/8374cea))
* update confy dependency version to v0.5.0 ([ab669be](https://github.com/xraph/forge/commit/ab669be))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.4.0 ([0c24688](https://github.com/xraph/forge/commit/0c24688))

## [1.4.0](https://github.com/xraph/forge/compare/v1.3.1...v1.4.0) (2026-03-28)


### Features

* **discovery:** add HTTP polling-based service discovery configuration ([f0fc074](https://github.com/xraph/forge/commit/f0fc074))
* update dependencies in go.mod and add new extensions to the documentation ([8adbf19](https://github.com/xraph/forge/commit/8adbf19))


### Bug Fixes

* go mod update ([ff97b31](https://github.com/xraph/forge/commit/ff97b31))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.3.1 ([97f66ef](https://github.com/xraph/forge/commit/97f66ef))

## [1.3.1](https://github.com/xraph/forge/compare/v1.3.0...v1.3.1) (2026-03-14)


### Features

* implement SchemaTyper and SchemaFormatter interfaces for enhanced OpenAPI schema generation ([aaf8990](https://github.com/xraph/forge/commit/aaf8990))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.3.0 ([edea267](https://github.com/xraph/forge/commit/edea267))

## [1.3.0](https://github.com/xraph/forge/compare/v1.2.0...v1.3.0) (2026-03-12)


### Features

* update dependencies and improve dashboard extension ([f2dced3](https://github.com/xraph/forge/commit/f2dced3))
* Introduce streaming hooks and enhance connection management ([4e92fe3](https://github.com/xraph/forge/commit/4e92fe3))


### Maintenance

* **changelog:** update CHANGELOG.md for v1.2.0 release ([86e7232](https://github.com/xraph/forge/commit/86e7232))
* Update dependencies to version 1.2.0 for various extensions and examples ([504ff77](https://github.com/xraph/forge/commit/504ff77))
* **changelog:** update CHANGELOG.md for v1.2.0 ([c945502](https://github.com/xraph/forge/commit/c945502))

## [1.2.0](https://github.com/xraph/forge/compare/v1.0.0...v1.2.0) (2026-03-07)


### Features

* **metrics:** Update metrics collectors and exporters to support HTTP metrics ([59cd526](https://github.com/xraph/forge/commit/59cd526))


### Bug Fixes

* **router:** fix trailing slash normalization in BunRouter adapter to prevent 301 redirects for paths like `/dashboard/` when route is registered as `/dashboard`, ensuring consistent behavior for both forms and fixing 404s on API endpoints with trailing slashes


### Maintenance

* Merge branch 'main' of github.com:xraph/forge ([f52633d](https://github.com/xraph/forge/commit/f52633d))
* **changelog:** update CHANGELOG.md for v1.0.0 ([120f1ef](https://github.com/xraph/forge/commit/120f1ef))

## [1.0.0](https://github.com/xraph/forge/compare/v0.10.0...v1.0.0) (2026-03-01)


### Features

* update config discovery to support multiple paths and improve logging ([c1ea988](https://github.com/xraph/forge/commit/c1ea988))
* add configuration files, schemas, and icons for Forge app and contributor ([f84964c](https://github.com/xraph/forge/commit/f84964c))
* add Ctrl Plane extension and update routing name handling for OpenAPI spec ([d75fb29](https://github.com/xraph/forge/commit/d75fb29))
* add MIT License to the project ([5143257](https://github.com/xraph/forge/commit/5143257))
* update SVG graphic to new design ([54b07fa](https://github.com/xraph/forge/commit/54b07fa))
* add .gitignore to exclude build artifacts and dependencies ([0b321ad](https://github.com/xraph/forge/commit/0b321ad))


### Maintenance

* Update dependencies and improve health check functionality ([6300c71](https://github.com/xraph/forge/commit/6300c71))
* Update confy dependency to v0.1.0 across multiple extensions ([f6cd3f3](https://github.com/xraph/forge/commit/f6cd3f3))
* removed node modules ([a1b0610](https://github.com/xraph/forge/commit/a1b0610))
* **changelog:** update CHANGELOG.md for v0.10.0 ([c38e18d](https://github.com/xraph/forge/commit/c38e18d))

## [0.10.0](https://github.com/xraph/forge/compare/v0.9.12...v0.10.0) (2026-02-24)


### Features

* add GitHub Actions workflow for VSCode extension validation and publishing ([61f5320](https://github.com/xraph/forge/commit/61f5320))
* update dependencies and add lifecycle helper functions ([6554ee1](https://github.com/xraph/forge/commit/6554ee1))
* **streaming:** implement in-memory session store for connection resumption ([bc7fec1](https://github.com/xraph/forge/commit/bc7fec1))
* add contributor adapters for Astro and Next.js frameworks ([4021ec2](https://github.com/xraph/forge/commit/4021ec2))
* **auth:** implement authentication and authorization framework for dashboard ([0f40da2](https://github.com/xraph/forge/commit/0f40da2))


### Bug Fixes

* **go.mod:** revert Go version to 1.25.3 ([4600bfe](https://github.com/xraph/forge/commit/4600bfe))


### Refactoring

* **webtransport:** simplify stream logging and remove StreamID method ([9cc86f6](https://github.com/xraph/forge/commit/9cc86f6))
* clean up code by adding missing newlines and improving comments for clarity ([d7dccb3](https://github.com/xraph/forge/commit/d7dccb3))


### Maintenance

* Update dependencies in go.mod and go.sum ([a989efd](https://github.com/xraph/forge/commit/a989efd))
* **changelog:** update CHANGELOG.md for v0.9.12 ([e54fcb3](https://github.com/xraph/forge/commit/e54fcb3))

## [0.9.12](https://github.com/xraph/forge/compare/v0.9.11...v0.9.12) (2026-02-18)


### Maintenance

* **deps:** remove gorilla/websocket dependency from go.mod and go.sum ([3457e7c](https://github.com/xraph/forge/commit/3457e7c))
* **changelog:** update CHANGELOG.md for v0.9.11 ([39f418c](https://github.com/xraph/forge/commit/39f418c))
* add .worktrees/ to gitignore ([ed4ad90](https://github.com/xraph/forge/commit/ed4ad90))

## [0.9.11](https://github.com/xraph/forge/compare/v0.9.10...v0.9.11) (2026-02-18)


### Features

* **build:** add build-modules target and enhance CI workflow ([05ae5c5](https://github.com/xraph/forge/commit/05ae5c5))


### Maintenance

* Merge branch 'main' of github.com:xraph/forge ([2b900fd](https://github.com/xraph/forge/commit/2b900fd))
* **changelog:** update CHANGELOG.md for v0.9.10 ([67abd3e](https://github.com/xraph/forge/commit/67abd3e))

## [0.9.10](https://github.com/xraph/forge/compare/v0.9.9...v0.9.10) (2026-02-16)


### Features

* **database:** implement app-scoped migration management and configuration ([58eb2bd](https://github.com/xraph/forge/commit/58eb2bd))


### Refactoring

* enhance dependency injection with new methods and update documentation ([cee3d89](https://github.com/xraph/forge/commit/cee3d89))
* streamline dependency injection with Provide and update FARP configuration ([8215292](https://github.com/xraph/forge/commit/8215292))


### Maintenance

* **changelog:** update CHANGELOG.md for v0.9.9 ([0706004](https://github.com/xraph/forge/commit/0706004))

## [0.9.9](https://github.com/xraph/forge/compare/v0.9.8...v0.9.9) (2026-02-15)


### Features

* **discovery:** add FARP and mDNS configuration options with comprehensive tests ([33d74ab](https://github.com/xraph/forge/commit/33d74ab))


### Maintenance

* **changelog:** update CHANGELOG.md for v0.9.8 ([e955a2e](https://github.com/xraph/forge/commit/e955a2e))

## [0.9.8](https://github.com/xraph/forge/compare/v0.9.7...v0.9.8) (2026-02-14)


### Features

* **config:** add configuration validation for Forge projects ([e76119d](https://github.com/xraph/forge/commit/e76119d))
* **docs:** enhance build, deploy, and development command documentation ([1e14b5d](https://github.com/xraph/forge/commit/1e14b5d))
* **docker:** integrate Docker support into development configuration ([c9d0681](https://github.com/xraph/forge/commit/c9d0681))


### Maintenance

* simplify test execution in CI workflow ([aba63f6](https://github.com/xraph/forge/commit/aba63f6))
* Add new dependencies and build artifacts to the project. ([13e51ff](https://github.com/xraph/forge/commit/13e51ff))
* Update dependencies and build artifacts. ([692f9de](https://github.com/xraph/forge/commit/692f9de))

## [0.9.7](https://github.com/xraph/forge/compare/v0.9.6...v0.9.7) (2026-02-10)


### Features

* **makefile:** enhance formatting and vetting for all Go modules ([5c25cc5](https://github.com/xraph/forge/commit/5c25cc5))


### Refactoring

* **config:** standardize formatting and improve readability in configuration files ([3907b6e](https://github.com/xraph/forge/commit/3907b6e))

## [0.9.6](https://github.com/xraph/forge/compare/v0.9.5...v0.9.6) (2026-02-10)


### Features

* **gateway:** add new gateway extension with access logging, authentication, caching, and circuit breaker ([c11ca75](https://github.com/xraph/forge/commit/c11ca75))

## [0.9.5](https://github.com/xraph/forge/compare/v0.9.4...v0.9.5) (2026-02-10)


### Bug Fixes

* **health:** reduce log noise by changing periodic health reports to DEBUG level ([bf23a23](https://github.com/xraph/forge/commit/bf23a23))

## [0.9.4](https://github.com/xraph/forge/compare/v0.9.3...v0.9.4) (2026-02-10)


### Bug Fixes

* **cli:** add missing app_config.go file and update .gitignore ([d205473](https://github.com/xraph/forge/commit/d205473))

## [0.9.3](https://github.com/xraph/forge/compare/v0.9.2...v0.9.3) (2026-02-10)


### Features

* **config:** implement loading of .forge.yaml for app-level configuration ([6ef25cd](https://github.com/xraph/forge/commit/6ef25cd))


### Maintenance

* **dependencies:** update forge and ai-sdk versions across multiple modules ([04ffee4](https://github.com/xraph/forge/commit/04ffee4))

## [0.9.2](https://github.com/xraph/forge/compare/v0.9.1...v0.9.2) (2026-02-09)


### Features

* **generate:** add CLI command generation and improve app structure handling ([bce139e](https://github.com/xraph/forge/commit/bce139e))

## [0.9.1](https://github.com/xraph/forge/compare/v0.9.0...v0.9.1) (2026-02-08)


### Features

* **release:** enhance release management for extensions and update workflows ([beb1b34](https://github.com/xraph/forge/commit/beb1b34))
* **database:** integrate dotenv for environment variable management ([837433b](https://github.com/xraph/forge/commit/837433b))


### Bug Fixes

* **workflows:** improve test command in GitHub Actions workflow ([5b36d70](https://github.com/xraph/forge/commit/5b36d70))
* **manifest:** correct formatting in release-please manifest file ([6e74d3f](https://github.com/xraph/forge/commit/6e74d3f))
* **dev:** ensure goroutine completion in file watcher implementation ([f090469](https://github.com/xraph/forge/commit/f090469))


### Refactoring

* **router:** migrate to vessel for dependency injection ([6acc086](https://github.com/xraph/forge/commit/6acc086))
* **router:** enhance router functionality and improve test coverage ([de411d0](https://github.com/xraph/forge/commit/de411d0))
* **router:** introduce Any method for multi-method route registration ([7b5cdef](https://github.com/xraph/forge/commit/7b5cdef))
* **http:** replace di context with http context in tests and middleware ([732475a](https://github.com/xraph/forge/commit/732475a))
* **extensions:** overhaul AI extension structure and improve modularity ([4bf291c](https://github.com/xraph/forge/commit/4bf291c))
* **extensions:** enhance modularity and update dependencies across multiple extensions ([25d3f34](https://github.com/xraph/forge/commit/25d3f34))
* **dependencies:** migrate to vessel and update dependencies ([e029f3e](https://github.com/xraph/forge/commit/e029f3e))
* **tests:** update error handling tests and remove deprecated code ([0a47571](https://github.com/xraph/forge/commit/0a47571))


### Maintenance

* **goreleaser:** update build configuration and pre-build hooks ([c026e47](https://github.com/xraph/forge/commit/c026e47))
* **dependencies:** update forge dependency and clean up go.sum ([dc280e8](https://github.com/xraph/forge/commit/dc280e8))
* **dependencies:** update Go version and quic-go dependencies ([fc67b88](https://github.com/xraph/forge/commit/fc67b88))
* **dependencies:** update forgeui dependency and enhance README documentation ([83a7a40](https://github.com/xraph/forge/commit/83a7a40))
* **dependencies:** update toml and k8s libraries across examples ([e151c50](https://github.com/xraph/forge/commit/e151c50))
* **examples:** remove outdated example binaries from the repository ([2a7ea5a](https://github.com/xraph/forge/commit/2a7ea5a))
* **examples:** remove database-demo example and related resources ([c31a35d](https://github.com/xraph/forge/commit/c31a35d))

## [0.9.0](https://github.com/xraph/forge/compare/v0.8.6...v0.9.0) (2026-02-08)


### ⚠ BREAKING CHANGES

* **router:** Migrated dependency injection from custom DI to Vessel. HTTP context is now used instead of DI context in middleware and handlers.

### Refactoring

* **router:** migrate to vessel for dependency injection ([6acc086](https://github.com/xraph/forge/commit/6acc086))
* **http:** replace di context with http context in tests and middleware ([732475a](https://github.com/xraph/forge/commit/732475a))


### Bug Fixes

* **goreleaser:** wrap CLI tidy hook in shell invocation ([22b1827](https://github.com/xraph/forge/commit/22b1827))
* **goreleaser:** use shell command for CLI module tidy hook ([5202498](https://github.com/xraph/forge/commit/5202498))

## [0.8.6](https://github.com/xraph/forge/compare/v0.8.5...v0.8.6) (2026-01-03)


### Refactoring

* **database:** enhance database config loading with ConfigManager ([05de831](https://github.com/xraph/forge/commit/05de831))

## [0.8.5](https://github.com/xraph/forge/compare/v0.8.4...v0.8.5) (2026-01-03)


### Features

* **config:** add environment variable expansion with defaults ([25c7546](https://github.com/xraph/forge/commit/25c7546))

## [0.8.4](https://github.com/xraph/forge/compare/v0.8.3...v0.8.4) (2026-01-02)


### Bug Fixes

* **database:** implement lazy migration discovery for improved startup in Docker ([1c1b9de](https://github.com/xraph/forge/commit/1c1b9de))

## [0.8.3](https://github.com/xraph/forge/compare/v0.8.2...v0.8.3) (2026-01-02)


### Features

* **config:** add environment variable source configuration options ([3089e74](https://github.com/xraph/forge/commit/3089e74))

## [0.8.2](https://github.com/xraph/forge/compare/v0.8.1...v0.8.2) (2025-12-31)


### Maintenance

* Minor dependency updates and internal improvements.

## [0.8.1](https://github.com/xraph/forge/compare/v0.8.0...v0.8.1) (2025-12-31)


### Features

* **database:** improve migration path handling and directory creation ([e0a3f59](https://github.com/xraph/forge/commit/e0a3f59))
* **database:** add migration checks and verbose output ([5ef93a7](https://github.com/xraph/forge/commit/5ef93a7))


### Maintenance

* **go.mod:** downgrade Go version from 1.25.3 to 1.24.4 ([788a5c1](https://github.com/xraph/forge/commit/788a5c1))

## [0.8.0](https://github.com/xraph/forge/compare/v0.7.5...v0.8.0) (2025-12-29)


### Features

* enhance AI extension with streaming support and new SDK features ([b9e03ff](https://github.com/xraph/forge/commit/b9e03ff))
* enhance AI extension with new LLM providers and improved configuration ([62f9d4e](https://github.com/xraph/forge/commit/62f9d4e))


### Maintenance

* **release:** bump version to 0.8.0 ([19f1329](https://github.com/xraph/forge/commit/19f1329))
* update .gitignore to exclude additional files and directories ([9eedded](https://github.com/xraph/forge/commit/9eedded))

## [0.7.5](https://github.com/xraph/forge/compare/v0.7.4...v0.7.5) (2025-12-21)


### Maintenance

* **cleanup:** remove trailing whitespace in client_config.go and client.go ([0333877](https://github.com/xraph/forge/commit/0333877))
* **cleanup:** clean up trailing whitespace in multiple files ([558e1cb](https://github.com/xraph/forge/commit/558e1cb))

## [0.7.4](https://github.com/xraph/forge/compare/v0.7.3...v0.7.4) (2025-12-19)


### Features

* **cron:** add cron extension with job scheduling, execution history, and metrics ([a3101e8](https://github.com/xraph/forge/commit/a3101e8))

## [0.7.3](https://github.com/xraph/forge/compare/v0.7.2...v0.7.3) (2025-12-12)


### Bug Fixes

* **validation:** resolve zero-value validation bug for all primitive types - Fixed critical validation bug where required query/header/path parameters with zero values (`false`, `0`, `0.0`) were incorrectly rejected as missing. The validator now properly skips zero-value validation for parameter fields since they're already validated during binding where we can distinguish between missing and explicit zero values. Adds 9 comprehensive test cases covering all primitive types.

## [0.7.2](https://github.com/xraph/forge/compare/v0.7.1...v0.7.2) (2025-12-10)


### Maintenance

* **cleanup:** remove test artifacts and temporary files from boolean validation fix

## [0.7.1](https://github.com/xraph/forge/compare/v0.7.0...v0.7.1) (2025-12-10)


### Bug Fixes

* **validation:** fix required boolean query parameters incorrectly failing when set to false - Previously, required boolean query parameters would fail validation with "field is required" error when explicitly set to `false`. The validator was incorrectly treating Go's zero value (`false`) as a missing parameter. This fix excludes boolean fields from the zero-value required check since they are already validated during the binding phase.

## [0.7.0](https://github.com/xraph/forge/compare/v0.6.0...v0.7.0) (2025-12-08)


### Features

* enhance JSON response handling with struct tags ([ed6d411](https://github.com/xraph/forge/commit/ed6d411f3f5edb488124e1d2591a567ea80d34bb))
* enhance logging and configuration in tests and examples ([75273c6](https://github.com/xraph/forge/commit/75273c6a3608cc3ae9d0cbbaa353f058fa0862a4))
* implement sensitive field cleaning in JSON responses ([f1985cf](https://github.com/xraph/forge/commit/f1985cfeff3c7ad5c8f158c1d3f8573957962ba2))


### Bug Fixes

* add comprehensive documentation for Context and Error Handling ([9f3b27e](https://github.com/xraph/forge/commit/9f3b27e5f8ef49fae77dc4c749b991472d3600cd))
* initialize appWatcher with configuration in tests ([42d7458](https://github.com/xraph/forge/commit/42d7458b7ad58c5cef964016d509749aece95c7c))
* make OpenAPIServer fields optional in OpenAPI spec ([32bc9da](https://github.com/xraph/forge/commit/32bc9da6280b6691ec8eb2f50daabd3df52b6e1c))
* replace NoopLogger with TestLogger in WebRTC and router benchmarks ([e797f9f](https://github.com/xraph/forge/commit/e797f9f79ee21f4a8221413ed1f430326ff71b01))

## [0.6.0](https://github.com/xraph/forge/compare/forge-v0.5.0...forge-v0.6.0) (2025-11-19)


### ⚠ BREAKING CHANGES

* **ci:** Release automation now uses Release Please instead of custom workflow. Version tracking moved from .github/version.json to .release-please-manifest.json.

### Features

* add initial documentation structure and configuration files ([a14bc4f](https://github.com/xraph/forge/commit/a14bc4fdaf5db9c6689edc08a7fc7e35751edfad))
* **app:** introduce functional options for AppConfig and update app creation methods ([69e2319](https://github.com/xraph/forge/commit/69e2319b8265b71865af2614d163983ff09ef20c))
* **banner:** implement startup banner display with configuration options ([c1faf4d](https://github.com/xraph/forge/commit/c1faf4d53311a3226c923b58b083bf1d1a713df8))
* **ci:** implement comprehensive CI/CD workflows and documentation ([5e3c81e](https://github.com/xraph/forge/commit/5e3c81e571812b50ed8d2a172b8cabeab8d7cd54))
* **ci:** migrate to Release Please for automated releases ([412e43b](https://github.com/xraph/forge/commit/412e43ba39e9b69056076cd9c0a523225dcbf46f))
* **config:** enhance DI container integration and update license ([f41a2ba](https://github.com/xraph/forge/commit/f41a2babbc20ddf7647e8d3e8387332ec2031efd))
* **consensus:** enhance RaftNode interface and implement new methods ([75423fe](https://github.com/xraph/forge/commit/75423fe039e2bd51b097544fd10e9a0b9a3b52ed))
* **dev:** implement hot reload functionality and update command syntax ([5ee99c6](https://github.com/xraph/forge/commit/5ee99c6b725f5347fa749744d3405c48c6b46858))
* **discovery:** remove outdated discovery examples and introduce database helpers ([25f2ab0](https://github.com/xraph/forge/commit/25f2ab07fc7ed7d17a7beb2c476e9e03c2a67f7d))
* **docs:** add comprehensive documentation and branding for Forge framework ([cf770d2](https://github.com/xraph/forge/commit/cf770d205cd5875e94758fe7e14bbb8a8b80621f))
* **docs:** add themed logo component and update extensions documentation ([b6a9838](https://github.com/xraph/forge/commit/b6a98380b1de22d801337220648c988e5fc387bb))
* **docs:** update metrics and add logo assets ([d1e4c55](https://github.com/xraph/forge/commit/d1e4c55f4d3cc5ce982a1cf6996fd44c8d74f1fc))
* enhance client generator with new features and error handling ([51692e5](https://github.com/xraph/forge/commit/51692e573f68a92c60c5948953faeec4f8be7471))
* **extension:** enhance process management with wait channel ([76427c0](https://github.com/xraph/forge/commit/76427c0076cc0505ba9f048643938a38eaa43ea9))
* **farp:** add new FARP extension with initial implementation ([67fed23](https://github.com/xraph/forge/commit/67fed23eb5620914a4b435c083b40698f5a820ca))
* **health:** add Windows-specific disk and system metrics collectors ([d61f475](https://github.com/xraph/forge/commit/d61f475c091df0df11fc33f57eb9fcedec9e22e2))
* introduce new CLI framework and dashboard extension ([c64dac8](https://github.com/xraph/forge/commit/c64dac8351f17444040c26fb65351d648c8474a3))
* **license:** add Forge License Decision Tree for quick licensing guidance ([e73443c](https://github.com/xraph/forge/commit/e73443c683a94e3eb4bfe1cf07ccb7037e16ecbc))
* **lifecycle:** introduce LifecycleManager for managing application lifecycle hooks ([b831e50](https://github.com/xraph/forge/commit/b831e509a2012ed75cd987eb5082ba1744877d17))
* **lifecycle:** introduce LifecycleManager for managing application lifecycle hooks ([52b2cff](https://github.com/xraph/forge/commit/52b2cffeae98b6f7ed53dd782b75bfdb5ceaf7b3))
* **local:** add stubs for presence and room store methods ([fe50c28](https://github.com/xraph/forge/commit/fe50c28107d6151ab7238035af5a63323314cbf9))
* **logger:** introduce BeautifulLogger for enhanced logging experience ([e2415d9](https://github.com/xraph/forge/commit/e2415d993ba143b2a1d25248598f2b610216834a))
* **logo:** add new SVG logo asset ([3103a69](https://github.com/xraph/forge/commit/3103a692b5d04fd4e1adbca9fb93b1d2a8f5ceec))
* **memory:** enhance memory manager with embedding function and consolidation testing ([69a56d5](https://github.com/xraph/forge/commit/69a56d5e81c33ab9ff8c5b456028330e61bdbe52))
* **observability:** add metrics and health endpoints to app ([a253e7d](https://github.com/xraph/forge/commit/a253e7da28bb9537a4ad5fb1f08f66c041c9dc7b))
* **process:** implement platform-specific process management for Unix and Windows ([7ae559e](https://github.com/xraph/forge/commit/7ae559ee3c92d0906b086180d35b75c17cd77cc2))
* **scripts:** add script to fix gosec SARIF file format ([96ce0a0](https://github.com/xraph/forge/commit/96ce0a06f426e5aa3e3392b2d70fe4cf62acf602))
* **tests:** add logger and metrics configuration to runnable extension tests ([4704e6b](https://github.com/xraph/forge/commit/4704e6b318bc5cb4ff13f03786090b64dbbae98e))


### Bug Fixes

* add comprehensive README for Forge v2 framework ([fc02c41](https://github.com/xraph/forge/commit/fc02c41a28b56e55b9853174a3222fdd270c1b3c))
* **app:** enhance error handling for endpoint setup and response writing ([5316849](https://github.com/xraph/forge/commit/531684992b11dbbeabe4f91edab579736116427f))
* cast page.data to any to avoid type errors ([004a2d3](https://github.com/xraph/forge/commit/004a2d320f5781ee15aea51f7879b189e456370f))
* **ci:** add continue-on-error to quality job and improve vulnerability check step ([d295ec6](https://github.com/xraph/forge/commit/d295ec661282de39524dd9e9e1369acb48f60a9a))
* **ci:** disable snapcraft builds in GoReleaser - snapcraft not available in GitHub Actions ([0fffc76](https://github.com/xraph/forge/commit/0fffc7675fbe5b6aee8d399e6afa60c95b2969f6))
* **ci:** enable snapcraft builds and install snapcraft in release workflow ([9ad702e](https://github.com/xraph/forge/commit/9ad702e231215ae5bf7b0b299f8e7fc06cf55d56))
* **ci:** exclude Windows from release tests due to flaky AI extension tests ([27a5b6d](https://github.com/xraph/forge/commit/27a5b6d6e9d25a8a5d6b9050063da900ea5a125d))
* **ci:** improve create-or-update-release-pr workflow robustness ([7301d16](https://github.com/xraph/forge/commit/7301d16a5ad257d418de1619a866955712341d5d))
* **ci:** install snapcraft via snap instead of apt ([4d19aad](https://github.com/xraph/forge/commit/4d19aade361977387b7cc97ddff7ae5a326f6734))
* **ci:** make quality checks non-blocking in release workflow ([e73f75e](https://github.com/xraph/forge/commit/e73f75ee906a3f0393fb2c4390768fb3d30a0aa8))
* **ci:** make Windows tests optional in multi-module release workflow ([1ada987](https://github.com/xraph/forge/commit/1ada9872f81392b920701cf5846f43c49a00d968))
* **ci:** prevent auto-release on direct release commits ([d481cbb](https://github.com/xraph/forge/commit/d481cbb8fa344ffe99402354e244081feb10d32e))
* **ci:** resolve bash syntax error in Release Please workflow summary ([3931722](https://github.com/xraph/forge/commit/393172215af9f8119e0a29c1ec04fd997d7a2f84))
* **ci:** trigger auto-release on PR merge event ([77c635b](https://github.com/xraph/forge/commit/77c635b936fbeba71588c0832a4eec9bfb33c775))
* **config:** add range checks for type conversions in GetInt8, GetInt16, and GetUint8 methods ([e0c7451](https://github.com/xraph/forge/commit/e0c745116d4b710d717cf15926415d69917ede35))
* correct loop iteration and enhance comments for clarity ([500ee2b](https://github.com/xraph/forge/commit/500ee2bb90c1e1e5ef1f81fe9ec588d6176fbae6))
* **docs:** update button variants and type assertions ([08305d6](https://github.com/xraph/forge/commit/08305d641aa7aa008066ec0ac2f7d5eba54b1159))
* fixed tests ([c3e812c](https://github.com/xraph/forge/commit/c3e812c1efa70518ec1c778e9f6bdfe1110da0e4))
* force release v0.0.3 ([29c2674](https://github.com/xraph/forge/commit/29c2674ef46231db26bbc1801513b2e8507b7923))
* forced release ([b3fe39f](https://github.com/xraph/forge/commit/b3fe39fe55e760ae07e9809e31c8428f3f6db908))
* forced release ([5f98a16](https://github.com/xraph/forge/commit/5f98a16fbc93a13275ea852895dec896dfc28617))
* **hero:** correct typo in hero component text ([b6a9838](https://github.com/xraph/forge/commit/b6a98380b1de22d801337220648c988e5fc387bb))
* improve GoReleaser config validation in release workflow ([5e81e7f](https://github.com/xraph/forge/commit/5e81e7f9a4b48cd9b5c893a0f2f068e82e5831ff))
* **init:** correct substring length for single-module layout check in project initialization ([ac12030](https://github.com/xraph/forge/commit/ac12030bbd4afce242c0ca08c3134facd394a3a8))
* resolve cross-platform test timing issues in consensus cluster tests ([6c47900](https://github.com/xraph/forge/commit/6c479001d438eaa6c6cb2d2b5b8a57736815513f))
* resolve deadlock between metrics and health manager during concurrent access ([f1a750d](https://github.com/xraph/forge/commit/f1a750d507b8149a7deb5466469216730d6df434))
* update Go version and GitHub Actions dependencies ([9f1777c](https://github.com/xraph/forge/commit/9f1777cdc86ac1e7582518b9fb87df900e500ae1))
* update TypeScript client generator to use HTTPClient ([f95192c](https://github.com/xraph/forge/commit/f95192c1b109697399650de9e3e497e04aafabbd))


### Documentation

* **ci:** add Release Please migration summary ([802d84b](https://github.com/xraph/forge/commit/802d84be8222f8c10e6a260aade720288e3de9a3))
* **extensions:** add comprehensive documentation for core, consensus, events, and hls extensions ([b6a9838](https://github.com/xraph/forge/commit/b6a98380b1de22d801337220648c988e5fc387bb))
* **forge:** add icons to documentation pages ([2e87a10](https://github.com/xraph/forge/commit/2e87a10da8a7306a0c6dc0578c6482c55939d7df))
* update documentation structure and content ([409dd57](https://github.com/xraph/forge/commit/409dd57a44ddf1b242329b1d972318a3815ae639))

### Bug Fixes

* update TypeScript client generator to use HTTPClient ([f95192c](https://github.com/xraph/forge/commit/f95192c1b109697399650de9e3e497e04aafabbd))
