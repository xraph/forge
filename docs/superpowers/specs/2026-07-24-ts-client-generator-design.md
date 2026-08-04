# TypeScript Client Generator: Type Generation, Streaming, and Field-Case Mapping

Date: 2026-07-24
Status: Approved, pending implementation plan

## Problem

The TypeScript client generator (`internal/client/generators/typescript/`) emits code that
does not always compile, produces different output on every run, and gives consumers a
client that is half-mapped: parameters are camelCased while response and request-body
fields keep server casing.

```ts
const user = await client.users.get(userId);   // camelCase parameter
console.log(user.user_id, user.created_at);    // snake_case fields
```

Four defects were confirmed empirically by generating a client and inspecting the output:

1. `rest.go` hardcodes `import { Client } from './client'`, but `generator.go` emits
   `export class <APIName>`. Any `APIName` other than `"Client"` yields an unresolved
   import. The generator's own tests use `APIClient`, `ChatClient`, and `WTClient`.
2. `client.ts` always imports `AuthConfig`, but `types.ts` emits it only when
   `IncludeAuth && NeedsAuthConfig(spec)`. `ClientConfig.auth?: AuthConfig` dangles too.
3. `types.ts` property keys are unquoted, so a schema property named `content-type`
   emits as `content-type?: string;` — a syntax error. `tsPropertyKey` exists in
   `rest.go` but `generator.go` never calls it.
4. Output is non-deterministic. `spec.Schemas`, `schema.Properties`, `sse.EventSchemas`,
   and `ws.MessageTypes` are Go maps iterated directly, so types, SSE listeners, and WS
   handlers reorder between runs. Confirmed to differ between run 0 and run 1 of 12.

The shared root cause is that nothing type-checks the generated output — every assertion
is `strings.Contains`. This is also why commit `cc3364e` had to be written reactively
after a downstream `tsc --noEmit` broke.

Measured by generating five fixtures and running `tsc --noEmit` against each, **every
fixture fails today, including the default configuration**:

| Fixture | `tsc` errors | Dominant cause |
| --- | --- | --- |
| `default` | 12 | missing `AuthConfig`, undeclared `require` |
| `apiname` (`APIName: "APIClient"`) | 15 | the above plus `Module './client' has no exported member 'Client'` |
| `odd-keys` (`content-type`, `3dtiles`) | 5 | `TS1131` — syntax error |
| `with-auth` (spec has a security scheme) | 4 | undeclared `require` |
| `no-streaming` | 3 | missing `AuthConfig` |

Defect 5, undeclared `require` in the Node fallback, was found by this measurement rather
than by code reading: the streaming generators emit `require(...)` into a package
declaring `"type": "module"`, and the generated `tsconfig.json` sets no `types`, so
`require` is unresolved (`TS2580`) in `channels.ts`, `presence.ts`, `rooms.ts`,
`typing.ts`, `sse.ts`, and `websocket.ts`. It affects even the otherwise-clean
`with-auth` fixture.

Further gaps, from code reading:

- Path parameters are interpolated without `encodeURIComponent` (`rest.go:543`).
- `insertIntoTree` (`rest.go:49`) converts leaf to branch but not branch to leaf, so
  inserting `users.list` after `users.active.list` discards the whole `users` namespace.
  Duplicate operation IDs also overwrite silently.
- `toCamelCase("userId")` returns `"userid"` (`rest.go:629`) — the tail of every part is
  lowercased, destroying an already-camelCase wire name.
- `generateEndpointMethod` (`rest.go:320`–423) is dead code still carrying the
  pre-`cc3364e` `path` collision and unconditional-`body` bugs.
- `fetch_client.go:155`: a caller-supplied `signal` replaces the timeout signal, silently
  disabling the timeout. `handleErrorResponse` throws a plain object rather than an
  `Error`, so the `error.name === 'AbortError'` retry check can never match it.
- The IR carries `Discriminator`, `AdditionalProperties`, `ReadOnly`/`WriteOnly`,
  `Format`, and per-property `Description`/`Deprecated`, none of which the generator emits.
  Non-string enums collapse to `number`. Return types inspect only 200/201 JSON;
  everything else becomes `any`. Request bodies are `application/json`-only.
- Schema names are neither sanitized nor deduped, so a user schema named `Message`,
  `Member`, `Room`, `RoomOptions`, `HistoryQuery`, or `UserPresence` collides with the
  hardcoded streaming interfaces in `generateStreamingTypes`.
- SSE path parameters are never substituted (`sse.go:213` emits `sse.Path` literally), so
  `{roomId}` ships in the URL. Neither SSE nor WS endpoints carry query parameters in the IR.
- Three divergent `EventEmitter` implementations exist: private copies in `sse.ts` and
  `websocket.ts` plus the exported one in `events.ts`, with `emit` public in two and
  `protected` in the third.
- Streaming events are stringly typed — `on(event: string, handler: Function)`.
- SSE `connect()` can settle twice (`onerror` rejects after `onopen` resolved), and
  reconnect failures become unhandled rejections.
- Browser SSE auth is hardcoded to `?token=` / `?apiKey=` rather than derived from the
  spec's security scheme.
- `require()` is emitted into a package declaring `"type": "module"`, so the Node
  fallback path throws at runtime.
- The hardcoded streaming types are internally inconsistent: rooms use `room_id` and
  `user_id`, presence uses `userId` and `lastSeen`.

## Goals

A TypeScript client that compiles under `strict`, generates byte-identical output for a
given spec, exposes idiomatic camelCase types while speaking the server's wire casing, and
gives SSE and WebSocket consumers payload types inferred from the event name.

## Non-goals

Other language generators (Go, Rust) are untouched. The IR gains fields but its shape is
not redesigned. `deepObject` and other exotic OpenAPI query-serialization styles remain
unsupported.

## Design

### Field-case mapping: runtime codec with generated per-schema maps

Types stay pure TypeScript with TS-cased property names. Wire names live entirely in a
generated codec table, so `types.ts` never mentions server casing and the mapping is one
authoritative artifact rather than logic scattered across five generators.

New generated file `src/codecs.ts`, keyed by schema id so recursive and mutually-recursive
schemas are cycle-safe:

```ts
type CodecRef = string;                     // key into CODECS

type Codec =
  | { kind: 'passthrough' }
  | { kind: 'object'; fields: Record<string /*wire*/, { ts: string; codec?: CodecRef }> }
  | { kind: 'array';  items: CodecRef }
  | { kind: 'record'; values: CodecRef }
  | { kind: 'union';  discriminator?: { wire: string; map: Record<string, CodecRef> };
                      members: CodecRef[] };

export const CODECS: Record<CodecRef, Codec>;
export function decode<T>(value: unknown, ref?: CodecRef): T;    // wire -> TS
export function encode(value: unknown, ref?: CodecRef): unknown;  // TS -> wire
```

Three rules make this safe, and are why per-schema maps were chosen over generic string
conversion:

1. **Unknown keys pass through verbatim.** A field the server adds later arrives intact
   rather than being mangled by a guess, keeping generated clients forward-compatible.
2. **`record` renames values, never keys.** `additionalProperties` and `Record<string, T>`
   hold user-controlled keys; a metadata bag with a `user_id` key keeps that key.
3. **Unions resolve by discriminator when the spec has one** (the IR already carries
   `Discriminator`). Without one, members are tried in order and the first whose required
   wire fields are all present wins; if none match, the value passes through. This
   ambiguity emits a generation-time warning naming the schema.

Applied at the boundaries: `RequestConfig` gains `bodyCodec?: CodecRef` and
`responseCodec?: CodecRef`, which `rest.ts` populates from statically known ids and
`HTTPClient` acts on. SSE decodes each event payload; WebSocket encodes outgoing sends and
decodes incoming frames. Query, header, and path parameters are **not** routed through the
codec — their wire names already come straight from the spec at the call site, which is
the one part of the current generator that is correct.

Under `FieldNaming: preserve` every codec is `passthrough` and `codecs.ts` is not emitted
at all: no runtime, no table, no import.

### Naming configuration

`GeneratorConfig` gains:

```go
// NamingStrategy selects a target identifier style.
type NamingStrategy string

const (
    NamingCamel    NamingStrategy = "camel"
    NamingPascal   NamingStrategy = "pascal"
    NamingSnake    NamingStrategy = "snake"
    NamingPreserve NamingStrategy = "preserve"
)

// FieldNaming selects the client-side identifier style for schema properties.
// The wire name always comes from the spec. Only the TypeScript generator reads
// this field in this change; other language generators ignore it and are
// unaffected. Defaults to NamingCamel when Language is "typescript", and to
// NamingPreserve otherwise, so no existing generator changes behaviour.
FieldNaming NamingStrategy

// FieldOverrides maps a wire name to an explicit client-side name. A key of
// "Schema.wire_name" applies to that schema only; a bare "wire_name" applies
// globally. A schema-scoped entry wins over a global one for the same wire
// name. Overrides bypass FieldNaming entirely and are used verbatim.
FieldOverrides map[string]string
```

Derivation applies to schema properties. The existing parameter camelCasing is replaced by
the same shared function, fixing `toCamelCase("userId") -> "userid"` in one place rather
than four.

**Collisions fail the build.** If `user_id` and `userId` both derive to `userId` within one
schema, generation returns an error naming the schema, both wire names, and the
`FieldOverrides` key that resolves it. Silently picking one would produce a client that
drops a field.

This is a **breaking change to generated output**: consumers regenerate and see
`user.userId` where they had `user.user_id`. It lands as the default. `FieldNaming:
preserve` is the documented escape hatch for anyone needing the previous shape.

### Streaming: one typed emitter

The two private `EventEmitter` copies are deleted; `events.ts` becomes the single source,
generic over an event map:

```ts
export class TypedEmitter<E extends Record<string, unknown>> {
  on<K extends keyof E & string>(event: K, handler: (payload: E[K]) => void): this;
  off<K extends keyof E & string>(event: K, handler: (payload: E[K]) => void): this;
  once<K extends keyof E & string>(event: K, handler: (payload: E[K]) => void): this;
  removeAllListeners(event?: keyof E & string): void;
  listenerCount(event: keyof E & string): number;
  eventNames(): (keyof E & string)[];
  protected emit<K extends keyof E & string>(event: K, payload: E[K]): void;
}
```

Each endpoint gets a generated event map, so payload types are inferred from the event name:

```ts
export interface NotificationsEvents {
  'message.created': types.Message;
  'user.joined': types.User;
  error: Error;
  stateChange: ConnectionState;
}
export class NotificationsSSEClient extends TypedEmitter<NotificationsEvents> { /* … */ }

client.on('message.created', m => m.createdAt);  // inferred, camelCased, codec-decoded
client.on('mesage.created', () => {});           // compile error
```

The `onFoo`/`offFoo` convenience methods remain, generated from the same map.

**IR additions.** `SSEEndpoint` gains `Parameters` and `QueryParams`; `WebSocketEndpoint`
gains `QueryParams` (it already has `Parameters`). Path parameters become required
arguments to `connect()`, query parameters an optional trailing object — the same
signature shape for SSE and WS.

Also in this area: frames run through the codec; browser SSE auth is derived from the
spec's security scheme; the `connect()` promise is guarded against double settlement and
reconnect failures route to the `error` event; the Node fallback uses dynamic `import()`
rather than `require()`.

### How fixed runtime code is authored

Roughly 15k lines of Go emit TypeScript through `strings.Builder`, so not even a syntax
error is visible until a consumer runs `tsc`. Code that is identical in every generated
client has no reason to be built that way.

The fixed runtime moves to real `.ts` files pulled in with `go:embed`: the codec runtime,
`TypedEmitter`, the `HTTPClient` core, and the error classes. They become editable,
lintable, and unit-testable as TypeScript. `strings.Builder` remains for genuinely
spec-derived output — endpoint methods, interfaces, event maps, the codec table. This is
scoped to files already being changed, not a rewrite of the package.

## Testing

**Everything in this spec is built test-first.** For each item below: write the test, run
it, confirm it fails for the expected reason, then implement until it passes. No
implementation code is written before a failing test exists for it. The four confirmed
defects each get a test that reproduces the defect before any fix is applied.

**The `tsc` gate.** A Go test writes a generated client to a temp directory and runs
`tsc --noEmit` against it, calling `t.Skip` when `node`/`tsc` is absent from `PATH` so
`go test ./...` still works everywhere; CI installs Node so the gate is real there.
Fixtures are chosen to reproduce what the review found:

- non-default `APIName`
- `IncludeAuth` on and off
- property keys such as `content-type` and `3dtiles`
- streaming-only specs (no REST endpoints)
- recursive and mutually-recursive schemas
- unions with and without a discriminator
- schemas named `Message`, `Room`, and `UserPresence` (collision with streaming types)

**Determinism.** A test generates the same spec twelve times and asserts byte-identical
output across all files.

**Embedded runtime.** The `go:embed`ed TypeScript gets its own unit tests run under the
same Node gate, including codec round-trips that assert unknown-key passthrough and that
`record` keys survive untouched.

**Go-level.** Collision detection returns an error naming the schema and both wire names;
naming derivation is table-driven across the four strategies including the `userId`
regression; `insertIntoTree` covers both insertion orders and duplicate operation IDs.

## Phases

Each phase is test-first throughout and leaves `go test` green and the generator
shippable.

1. **`tsc` harness and correctness fixes.** The gate first, then the confirmed defects and
   the code-reading defects: the `Client`/`APIName` import mismatch, the dangling
   `AuthConfig`, unquoted property keys, sorted iteration everywhere, `encodeURIComponent`
   on path parameters, the `insertIntoTree` branch-clobber and duplicate-operationID
   overwrite, the shared case function replacing `toCamelCase`, deletion of dead
   `generateEndpointMethod`, combined abort signals, real `Error` throws.
2. **Type-generation depth.** `readOnly`/`writeOnly` request and response split,
   `additionalProperties`, discriminated unions, non-string enums, JSDoc and `@deprecated`,
   format mapping, 2xx return unions and non-JSON bodies, schema-name sanitizing and
   deduplication against the streaming types. Emits the codec table.
3. **Naming codec wired into REST.** `FieldNaming` and `FieldOverrides`, codec refs on
   `RequestConfig`, encode and decode at the HTTP boundary, collision detection that fails
   the build.
4. **Streaming rework.** Typed emitter and event maps, IR parameter additions, codec on
   frames, SSE auth from the security scheme, connect-promise and reconnect fixes, dynamic
   `import()`.

## Decisions and their rationale

| Decision | Rationale |
| --- | --- |
| Runtime codec with per-schema maps, not generic case conversion | Generic conversion is lossy: it mangles keys that are legitimately not snake_case, and it cannot distinguish schema fields from user-controlled `Record` keys. |
| Codec is on by default, not opt-in | The half-mapped client is the defect being fixed; an opt-in flag would leave the default broken. `preserve` is the escape hatch. |
| Collisions fail generation | The alternative silently drops a field. |
| `tsc` gate skips when Node is absent | Keeps `go test ./...` runnable without a Node toolchain while making the check real in CI. |
| Fixed runtime via `go:embed` | String-concatenated TypeScript is why syntax errors reached consumers. |
| Path, query, and header parameters bypass the codec | Their wire names already come from the spec at the call site and are handled correctly today. |
