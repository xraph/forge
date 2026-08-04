# TypeScript Client Generator — Phase 3: Naming Codec Wired into REST

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give generated TypeScript idiomatic property names, and make the codec table emitted in Phase 2 actually do work — encode on the way out, decode on the way in, at the HTTP boundary.

**Architecture:** Work spans `internal/client/config.go` (two new config fields) and `internal/client/generators/typescript/`. Every task is guarded by the `tsc --noEmit` gate, which covers 8 fixtures and fails on any generated file that does not compile under `strict`.

**Tech Stack:** Go 1.26, TypeScript 5.8, `stretchr/testify`, Node + esbuild for execution tests.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-24-ts-client-generator-design.md`.
- Continue on branch `fix/ts-client-generator-phase1`. Do not create a new branch. PR #33 is open against `main`.
- **Strict TDD.** No implementation code before a failing test exists for it. Run the test, confirm it fails for the expected reason, quote the output, then implement.
- The 8 gate fixtures must report **zero** tsc errors after every task. `TestGeneratedClientsTypeCheck` is the backstop — never weaken it.
- Generation must stay deterministic. Any new map iteration reaching output goes through `sortedKeys`.
- Conventional-commit prefixes. No `Co-Authored-By` trailers.
- Commit after every task.
- **Behavioural claims about generated runtime code must be proven by executing it** — bundle with esbuild, run under Node. String assertions alone are insufficient; this was established across Phase 2 and caught several defects that string tests passed.

## Current State

Verified before writing this plan, not assumed:

- `GeneratorConfig` (`internal/client/config.go:11`) has **no** `FieldNaming` or `FieldOverrides` field. Both are new.
- Schema properties are emitted with their **wire names verbatim** — `generateTypes` → `schemaToTypeScript` → `objectPropsLiteral`. Nothing derives a client-side name today.
- The "four copies of `toCamelCase`" the design doc describes **no longer exist**. Phase 1 Task 10 consolidated them: `rest.go:724` and `pagination.go:286` are one-line shims delegating to the shared `toCamel` in `naming.go:72`. Deleting the shims is cosmetic, not the point of this phase.
- The design doc's claimed bug `toCamelCase("userId") -> "userid"` is **already fixed**. `toCamel("userId")` returns `userId`.
- **Two real naming bugs remain, found by probing `toCamel` directly.** Both become user-visible the moment property renaming turns on:
  - `toCamel("USER_ID")` → `uSERID` (want `userId`)
  - `toCamel("HTTPStatus")` → `hTTPStatus` (want `httpStatus`)
  `splitWords` does not split runs of capitals, and `lowerFirst` lowercases only the first rune of an all-caps word. `SCREAMING_SNAKE` is a plausible wire casing, so this is not hypothetical.
- `naming.go` has `toCamel` and `toPascal`. There is **no** `toSnake` — the `NamingSnake` strategy needs one.
- `CODECS` entries currently set `ts` equal to the wire name for every field (`codecs.go`), so `encode`/`decode` are identity. Flipping `ts` is this phase's job.
- `RequestConfig` (`fetch_client.go:40`) has no `bodyCodec`/`responseCodec` field.
- The codec runtime already implements the three safety rules and they are execution-tested. A union **without** a discriminator currently falls back to passthrough.

## Scoping Decision: undiscriminated unions get structural matching

The design doc specifies that a union with no discriminator tries members in order, picks the
first whose required wire fields are all present, and emits a generation-time warning. Phase 2
implemented passthrough instead. **The user chose to build structural matching as designed**
(Task 4 below), rather than keep the safer passthrough.

The risk being accepted: a wrong structural match renames fields based on a guess. Task 4 must
therefore make "no member matched" fall back to passthrough rather than to a best-effort guess,
and must warn at generation time for every undiscriminated union so the ambiguity is visible.

---

### Task 1: Fix all-caps word splitting

**Files:**
- Modify: `internal/client/generators/typescript/naming.go`
- Test: `internal/client/generators/typescript/naming_test.go`

Prerequisite for everything else: renaming is worthless if the rename is wrong.

- [ ] **Step 1: Write the failing test**

Table-driven, covering at minimum: `USER_ID`→`userId`, `HTTPStatus`→`httpStatus`,
`userID`→`userId`, `HTTP_STATUS_CODE`→`httpStatusCode`, `ID`→`id`, `A`→`a`,
plus the existing passing cases as regression guards (`userId`, `user_id`, `a`).
Assert `toPascal` equivalents too (`USER_ID`→`UserId`, `HTTPStatus`→`HTTPStatus` or
`HttpStatus` — pick one, justify it, and be consistent).

- [ ] **Step 2: Run test to verify it fails**

Quote the actual failing output.

- [ ] **Step 3: Implement**

Fix `splitWords` to break a run of capitals before a following lowercase
(`HTTPStatus` → `HTTP` + `Status`), and make `lowerFirst`/`upperFirst` normalise a
word that is entirely uppercase rather than touching only the first rune.

Be careful not to regress `userID` → `userID` (currently correct as camel) — decide
whether the trailing acronym normalises to `userId` and say why.

- [ ] **Step 4: Verify** — `go test ./internal/client/generators/typescript/ -count=1` passes, 8 fixtures at zero tsc errors.

- [ ] **Step 5: Commit** — `fix(client/typescript): split runs of capitals when converting case`

---

### Task 2: `FieldNaming` and `FieldOverrides` config

**Files:**
- Modify: `internal/client/config.go`
- Create: `internal/client/generators/typescript/fieldname.go`, `fieldname_test.go`

**Interfaces:**
- Produces: `NamingStrategy` type with `NamingCamel`/`NamingPascal`/`NamingSnake`/`NamingPreserve` constants; `GeneratorConfig.FieldNaming`, `GeneratorConfig.FieldOverrides`.
- Produces: `func tsFieldName(schemaName, wireName string, config client.GeneratorConfig) string`.

- [ ] **Step 1: Write the failing test**

Cover: each strategy; a schema-scoped override (`"User.user_id"`) beating a global one
(`"user_id"`) for the same wire name; an override bypassing the strategy entirely and being
used verbatim; `NamingPreserve` returning the wire name unchanged; and the default when
`FieldNaming` is the zero value.

- [ ] **Step 2: Run test to verify it fails** (`undefined: tsFieldName`).

- [ ] **Step 3: Implement**

Add the two config fields with the doc comments from the design doc. Default resolution:
`FieldNaming` zero value means camel when `Language == "typescript"`, preserve otherwise —
so no other language generator changes behaviour. Decide where that defaulting lives
(`DefaultConfig()` vs. resolved at read time) and justify; a zero value arriving from a
caller that never set the field must still behave correctly.

Add `toSnake` to `naming.go`.

- [ ] **Step 4: Verify** — package tests pass; **no generated output changes yet** (nothing calls `tsFieldName`). Confirm by diffing an emitted fixture tree against the previous commit.

- [ ] **Step 5: Commit** — `feat(client): add FieldNaming and FieldOverrides configuration`

---

### Task 3: Collision detection that fails the build

**Files:**
- Modify: `internal/client/generators/typescript/fieldname.go`, `generator.go`
- Test: `fieldname_test.go`

Must land **before** properties are renamed, so the first rename cannot silently drop a field.

- [ ] **Step 1: Write the failing test**

A schema with both `user_id` and `userId` under `NamingCamel` must make
`Generator.Generate` return an error. Assert the error names the schema, **both** wire
names, and the `FieldOverrides` key that resolves it. Assert generation produces no files.
Also assert the negative: a collision resolved by an override does **not** error.

- [ ] **Step 2: Run test to verify it fails.**

- [ ] **Step 3: Implement**

Walk every schema's properties in `sortedKeys` order, derive each name, and detect two wire
names mapping to the same client name within one schema. Collisions across different schemas
are fine and must not error. Report **all** collisions found, not just the first — a caller
fixing them one regeneration at a time is a bad experience.

- [ ] **Step 4: Verify** — package tests pass; all 8 fixtures still generate and type-check.

- [ ] **Step 5: Commit** — `feat(client/typescript): fail generation on field-name collisions`

---

### Task 4: Structural matching for undiscriminated unions

**Files:**
- Modify: `internal/client/generators/typescript/codecs.go`
- Test: `codecs_test.go`

Per the scoping decision above. Do this before flipping `ts`, so the union path is correct
when renaming starts mattering.

- [ ] **Step 1: Write the failing test**

Table entry shape for an undiscriminated union must become
`{kind:"union", members:[...]}` with **no** `discriminator`, and the runtime must try
members in order. Execution test: a payload matching member B's required fields but not
A's decodes as B; a payload matching neither passes through verbatim; a payload matching
**both** picks the first in declared order (assert the order is deterministic).

Also assert a generation-time warning is emitted naming the schema.

- [ ] **Step 2: Run test to verify it fails** (today: passthrough, no members, no warning).

- [ ] **Step 3: Implement**

Emit `members` for undiscriminated unions and extend the runtime `union` branch: when
`discriminator` is absent, try each member id in order, testing that every **required** wire
field of that member is present on the value. First match wins; no match falls back to
passthrough — never a best-effort guess.

Required-field data must reach the table: `codecEntry` for an object needs to carry which
wire fields are required. Decide how (a `required` list on the object entry is the obvious
shape) and keep the table deterministic.

Warnings: `Generate` currently returns only a string. Decide how a warning surfaces — the
generator has no logger today. State the mechanism and justify it; do not invent a global.

- [ ] **Step 4: Verify** — package tests pass; 8 fixtures zero tsc errors; determinism holds.

- [ ] **Step 5: Commit** — `feat(client/typescript): resolve undiscriminated unions structurally`

---

### Task 5: Rename schema properties and flip the codec `ts` names

**Files:**
- Modify: `internal/client/generators/typescript/generator.go`, `codecs.go`
- Test: `generator_test.go`, `codecs_test.go`

The breaking change. Types get client-cased property names; the codec table records the
wire↔client mapping that makes them work.

- [ ] **Step 1: Write the failing test**

`objectPropsLiteral` must emit `userId` for wire `user_id` under camel. The emitted
`CODECS` entry for that schema must have `fields: {"user_id": {"ts": "userId"}}`.
Round-trip execution test: `decode({user_id:'x'}, 'User')` yields `{userId:'x'}` and
`encode` reverses it. `NamingPreserve` leaves both unchanged.

Required-property lists, JSDoc, `@deprecated`, and quoted non-identifier keys (all Phase 2
work) must survive the rename — assert each explicitly.

- [ ] **Step 2: Run test to verify it fails.**

- [ ] **Step 3: Implement**

Thread `config` into the property-emitting path and apply `tsFieldName`. A derived name that
is not a valid TS identifier must still be quoted by the existing `tsPropertyKey` logic.

- [ ] **Step 4: Verify** — 8 fixtures zero tsc errors. Generated output **will** change; that is expected and is the breaking change the design doc calls out. Diff a fixture tree and confirm the change is only property names and codec `ts` values.

- [ ] **Step 5: Commit** — `feat(client/typescript)!: derive client-side property names from FieldNaming`

---

### Task 6: Codec refs on `RequestConfig` and encode/decode at the boundary

**Files:**
- Modify: `internal/client/generators/typescript/fetch_client.go`, `rest.go`
- Test: `fetch_client_test.go`, `rest_test.go`

- [ ] **Step 1: Write the failing test**

Execution tests, since this is runtime behaviour:
- a request whose body is `{userId:'x'}` with `bodyCodec:'User'` must put `{"user_id":"x"}` on the wire;
- a response of `{"user_id":"x"}` with `responseCodec:'User'` must resolve to `{userId:'x'}`;
- a request with **no** codec ref must pass the body through untouched;
- `rest.ts` must populate both refs from statically known schema ids.

- [ ] **Step 2: Run test to verify it fails.**

- [ ] **Step 3: Implement**

Add `bodyCodec?: string` and `responseCodec?: string` to `RequestConfig`. In `executeRequest`,
apply `encode` before serialisation and `decode` after parsing.

**Interaction hazards — handle explicitly, they are the whole risk of this task:**
- Encoding must happen **only** for JSON bodies. A `FormData`, `Blob`, `URLSearchParams`, `ArrayBuffer`, `TypedArray` or `ReadableStream` body must never be walked by `encode` — Phase 2 Task 8 established that enumeration; reuse it, do not re-derive it.
- Decoding must not run on a `void`/empty-body response, a `Blob` response, or a `string` response from a `text/*` endpoint. Phase 2 Task 7's `allowEmptyBody` and content-type branching define those cases.
- Query, header and path parameters are **not** routed through the codec — their wire names already come from the spec at the call site.

- [ ] **Step 4: Verify** — package tests pass; 8 fixtures zero tsc errors; the Phase 2 execution tests for body serialisation and response parsing must all still pass unchanged.

- [ ] **Step 5: Commit** — `feat(client/typescript): encode and decode payloads at the HTTP boundary`

---

### Task 7: `NamingPreserve` emits no codec at all

**Files:**
- Modify: `internal/client/generators/typescript/generator.go`
- Test: `codecs_test.go`, `gate_test.go`

- [ ] **Step 1: Write the failing test**

Under `FieldNaming: NamingPreserve`, `out.Files` must **not** contain `src/codecs.ts`,
`src/index.ts` must not export it, and no emitted file may import it. Add a gate fixture
with `NamingPreserve` so this is type-checked, and assert the fixture count grew.

- [ ] **Step 2: Run test to verify it fails** (today `codecs.ts` is always emitted).

- [ ] **Step 3: Implement**

Gate emission, the index export, and the `rest.ts` codec refs on the strategy. With preserve,
every codec would be identity — emitting a dead table and runtime is pure weight.

- [ ] **Step 4: Verify** — all fixtures (now 9) at zero tsc errors; determinism holds.

- [ ] **Step 5: Commit** — `feat(client/typescript): skip codec emission under preserve naming`

---

### Task 8: End-to-end proof and documentation

**Files:**
- Test: `runtime_test.go`
- Modify: generator README/docs if one exists (check first; do not invent a docs tree)

- [ ] **Step 1: Write the test**

One execution test that generates a full client for a spec with snake_case wire names,
bundles it, and drives a mocked round trip: call a generated method with camelCase input,
assert snake_case reached the wire, assert the camelCase result came back. This is the proof
the phase actually delivered its goal, end to end, through real generated code.

- [ ] **Step 2: Verify** — full suite `go test -short -race -timeout=10m $(go list ./... | grep -v '/bk/')` exits 0.

- [ ] **Step 3: Commit** — `test(client/typescript): prove the naming codec end to end`

---

## Self-Review

**Spec coverage.** Design doc Phase 3 lists: `FieldNaming`/`FieldOverrides` (Task 2), codec refs
on `RequestConfig` (Task 6), encode/decode at the HTTP boundary (Task 6), collision detection
that fails the build (Task 3). Added beyond the doc: the all-caps splitting bug (Task 1, a real
defect found by probing, prerequisite to any rename), structural union matching (Task 4, the
user's explicit choice over Phase 2's passthrough), and the preserve escape hatch (Task 7, which
the doc specifies under the codec section rather than the phase list).

**Ordering rationale.** Task 1 before everything (a wrong rename is worse than no rename).
Task 3 before Task 5 (collision detection must exist before the first rename can drop a field).
Task 4 before Task 5 (get the union path right before renaming makes it matter). Task 6 after
Task 5 (nothing to encode until `ts` differs from wire). Task 7 last of the behavioural tasks,
since it gates everything the earlier ones added.

**Known hazards.**
- Task 5 is a deliberate breaking change to generated output. Every downstream assertion in the
  existing test suite that mentions a wire-cased property name will need updating — expect the
  blast radius to be large and do not weaken assertions to make it pass.
- Task 6 touches `executeRequest`, which Phase 2 Tasks 7, 7b and 8 all modified across five fix
  rounds. Re-read the file rather than working from this plan's description of it, and keep the
  body-type enumeration and the empty-body/content-type branching intact.
- Task 4 needs a warning mechanism the generator does not currently have. Do not invent a global
  logger; surface it through the existing return path or an explicit collector.
