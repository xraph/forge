# Unified streams + hooks generation

**Date:** 2026-08-08
**Status:** Approved, ready for implementation planning
**Scope:** `internal/client`, `cmd/forge/plugins`

## Problem

`forge client generate` reads exactly one specification document. Forge emits its
REST operations to OpenAPI and its stream bindings to AsyncAPI, so no single
invocation can produce a package containing both:

| Source | You get | You do not get |
|---|---|---|
| `openapi.json` | `ops.ts`, `hooks.ts`, `rest.ts` | `websocket.ts`; `streams` is empty |
| `asyncapi.json` | `websocket.ts`, `events.ts` | `ops.ts`, `hooks.ts` |

The consequence is that `{ live: true }` — documented, and with runtime support
built and tested in `@forge-go/client-core` — has a populated manifest only for a
source document carrying both, which Forge's own two documents do not produce
through the CLI. The feature is written and unreachable.

## Current state

Verified against the tree at `95e55b70`:

- `APISpec` (`internal/client/ir.go:6`) already carries `Endpoints`,
  `WebSockets`, `SSEs` and `WebTransports` in one struct. The IR is unified.
- `parseOpenAPI` (`spec_parser.go:104`) fills `Endpoints` and never touches
  streams. `parseAsyncAPI` (`spec_parser.go:222`) fills `WebSockets`/`SSEs` at
  lines 346 and 350. Each parse half-populates the same shape.
- The TypeScript generator is already written for the both-populated case. Its
  emission decisions are per-section and data-driven:
  `isAsyncAPIOnly := config.HasAnyStreamingFeature() && len(spec.Endpoints) == 0`
  (`generators/typescript/generator.go:266`), with `ops`/`rest` gated on
  `len(spec.Endpoints) > 0` (line 285) and `hooks`/manifest on
  `config.HooksEnabled() && len(spec.Endpoints) > 0` (line 300). Feed it a spec
  with both halves and every gate opens by itself.
- `resolveEntityFields` runs *inside* each parse (`spec_parser.go:63`) and once
  in `Introspector.Introspect` (`introspector.go:64`).
- `Introspector` (`introspector.go:21`, taking a `router.Router`) already
  produces a both-populated spec — `Endpoints` at lines 53 and 196, `WebSockets`
  and `SSEs` at 271 and 275 — and has no non-test callers. (The unrelated
  `infra.Introspector` in `cmd/forge/plugins` does app discovery.)
- `SourceConfig` (`cmd/forge/plugins/client_config.go:51`) holds a single `Path`
  or `URL`; `--from-spec` and `--from-url` are singular
  (`cmd/forge/plugins/client.go:93`).
- `spec.Warnings` already exists and is surfaced by the generators
  (`internal/client/envelope.go`, `introspector.go:1162`).

**Therefore this is an input-stage problem, not a codegen problem.** The
generator and the IR already support the destination.

## Design

### Architecture

```
sources[]  →  parse each (resolution deferred)  →  MergeSpecs  →  resolveEntityFields (once)  →  existing generator
```

Resolution must move after the merge. An `Order` defined in the REST document
and a stream binding referencing `Order` in the AsyncAPI document form a
cross-document edge; resolving per-document resolves it before the other half
exists.

The single-source path keeps its current behaviour by composing the two steps.

### Components

| Component | Change |
|---|---|
| `internal/client/merge.go` | **New.** `MergeSpecs(specs ...*APISpec) *APISpec` and the collision policy. The only genuinely new logic. |
| `SpecParser` | Split `Parse` into `parseDocument` (detect + parse, no resolution) and `Parse` (`parseDocument` + resolve). Existing callers of `Parse` are unaffected. |
| `SourceConfig` | Gains one ordered `Sources []SourceEntry` (each entry a type plus a path or a URL), replacing the scalar `Path`/`URL`. A single list rather than parallel `paths`/`urls` arrays, so ordering across mixed file and URL sources is well defined. The scalar `path`/`url` keys keep working, read as a one-element list. CLI takes repeatable `--from-spec` / `--from-url`, appended in argument order. |
| `generationPlan` | Carries source lists rather than a single `specPath`/`specURL`. |
| `resolveWatchSource` (`client_watch.go:216`) | Watches every file source instead of one. |
| `Introspector` | Wired as an optional source. Returns a both-populated spec, so it enters the merge as one element with no special case. |
| TypeScript / Go generators | **Unchanged.** |

### Merge semantics

OpenAPI is authoritative for shared types, because it carries full
request/response schemas; AsyncAPI fills only what is absent.

| Field | Rule |
|---|---|
| `Info` | First OpenAPI source wins; with no OpenAPI source, the first source in merge order. Others ignored |
| `Endpoints` | Union |
| `WebSockets`, `SSEs`, `WebTransports` | Union |
| `Schemas` | OpenAPI wins by name; differing shape emits a warning |
| `Entities` | OpenAPI wins by name; differing `IDField` emits a warning |
| `RoutingTypes` | Discarded before merge; rebuilt by `resolveEntityFields` |
| `Servers` | Union, deduped by URL |
| `Security` | Union, deduped by scheme name; OpenAPI wins |
| `Tags` | Union, deduped by name |
| `Warnings` | Concatenated, plus collision warnings |

**Precedence follows document type, not argument order.** Sources are ordered
OpenAPI-first before merging, so `--from-spec async.json --from-spec openapi.json`
produces byte-identical output to the reverse. `determinism_test.go` exists;
making output depend on typing order would undercut it.

**`RoutingTypes` is rebuilt, not merged.** Its doc comment
(`ir.go:18`) states the two maps "are disjoint by construction:
`resolveEntityFields` builds this one as the useful types MINUS the entities, and
it is the only writer." Merging two pre-built maps breaks that invariant — a type
that is routing-only in one document and a full entity in the other would land in
both — and `spec.Entities[name]` is read at several call sites as the question
"is this an entity". Dropping and rebuilding preserves the invariant.

## Error handling

**Warnings** (generation proceeds), via the existing `spec.Warnings`:

- A schema or entity name declared in both documents with a differing shape or
  `IDField`. Reports the name, both sources, and which won.
- Duplicate `path` + `method` across two OpenAPI sources.

Identical redeclaration across documents is silent — it is the normal case, not
a conflict.

**Hard errors** (generation stops):

- No sources resolved.
- Any single source fails to parse. Partial generation is the exact failure this
  feature removes: a package with a silently-empty `streams` table. A broken
  AsyncAPI document must not degrade into a REST-only package.
- Merged spec has neither endpoints nor streams.

**Merging a single spec is identity.** A lone AsyncAPI source still produces
`websocket.ts`/`events.ts` with no `ops.ts`, and `isAsyncAPIOnly` still evaluates
true. This is a regression risk, not a feature, and is pinned by tests.

## Testing

- **Unit** — `MergeSpecs` table tests per field rule: union, dedup,
  OpenAPI-wins precedence, warning emitted on genuine shape disagreement and not
  on identical redeclaration.
- **Determinism** — extend `determinism_test.go`: the same two sources in either
  argument order produce byte-identical output.
- **Regression goldens** — single-source OpenAPI and single-source AsyncAPI
  outputs unchanged. The merge path reroutes both, so this is non-negotiable.
- **E2E** — extend `e2e_specfile_test.go` with a two-document fixture asserting
  one package containing `ops.ts`, `hooks.ts`, `rest.ts`, `websocket.ts` and
  `events.ts`, **and a non-empty `streams` manifest**. The file list alone would
  pass while `{ live: true }` still did nothing.
- **Cross-document entity resolution** — a stream binding in the AsyncAPI
  document referencing an entity defined only in the OpenAPI document resolves
  its field edges. This is the case per-document resolution gets wrong, so it is
  the test that proves the deferral was necessary.

## Scope

**In:** `merge.go`; the `SpecParser` split; multi-source `SourceConfig` and CLI
flags; `generationPlan` carrying lists; `resolveWatchSource` watching every file
source; the introspector as an optional source.

**Out, deliberately:**

- `forge client diff` stays one document per side. Diff compares two *versions*;
  making each side independently multi-source is a second feature, and
  conflating them would make a cache-breaking-change report depend on merge
  precedence.
- Capability gating, SSR `dehydrate`/`hydrate`, and optimistic overlays.
- The seven runtime edges listed in `packages/client-core/README.md`.
- Changing Forge's spec emission to a single combined document. That option
  stays available later; this design does not foreclose it.

## Follow-up

The **field-renaming bug** sits on the same path and should be the next spec.
Under any non-`preserve` naming, hooks return wire-cased fields while `rest.ts`
returns renamed ones from the same package, contradicting the generated types.
Unified generation puts both in one package for the first time, which makes the
inconsistency far easier to hit. Per the runtime README, closing it needs the two
codec ids on `OperationMeta` **and** the `entities` table renamed in the same
change — `opsmanifest.go` emits `idField` and `fields` as verbatim wire names, so
renaming one without the other silently stops the normalizer finding ids, and a
type whose id field is absent is not an entity, so nothing reports it.
