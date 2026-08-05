# Forge Web Client: Normalized Cache, Invalidation Tags, and Stream Binding

Date: 2026-08-03
Status: Approved, pending implementation plan

## Problem

Forge generates a TypeScript client that covers REST, WebSocket, SSE and WebTransport from
one intermediate representation (`internal/client/ir.go`). The generated package is
correct at the transport level and wrong at the state level.

Four defects, each structural rather than incidental:

1. **The cache is keyed by request, not by data.** `query.ts` derives TanStack Query keys
   from call parameters, so `useOrderGet({id: 7})` and `useOrderList()` hold two unrelated
   copies of order 7. A mutation response or a socket frame cannot update both without
   hand-written invalidation, and nothing detects when that hand-written invalidation stops
   matching the keys it was written against.

2. **Invalidation is authored on the client and stringly-typed.** The server knows that
   `POST /orders` affects order lists. It has no way to say so. Every consuming frontend
   rediscovers the same invalidation graph by hand, and each one drifts independently.

3. **Streams live outside the cache entirely.** The generated WebSocket and SSE clients are
   `EventEmitter` subclasses with no relationship to cached data. Wiring a live message into
   a rendered list is per-component `setQueryData`, reinvented slightly differently by every
   team that tries.

4. **Auth stops at token attach.** `WithRequiredAuth("jwt", "write:users", "admin")` records
   providers and scopes per route. The client emits a config type and discards the rest.

The first defect cannot be fixed inside TanStack Query. Its cache entry *is* the response
object — `getQueryData` returns what the server sent. Introducing an indirection between the
cache entry and the hook's return value changes its central data structure, not its
periphery. Normalization requires a different runtime.

## Goals

- Normalized entity cache with identity inferred from Go types, no annotation required
- Server-declared invalidation tags shared by mutations and stream messages
- Channel-to-entity binding, so a query can be made live at its call site
- Framework-agnostic core; React, Vue and Angular adapters
- Generated output containing types and one-line facades, no logic
- Bundle smaller than TanStack Query core + adapter while doing strictly more
- A drift story that makes a stale client impossible rather than merely unlikely

## Non-goals

- Replacing `rest.ts`, `websocket.ts`, `sse.ts` or `webtransport.ts`. They become the
  transports the runtime drives. Their public surface is unchanged.
- Offline-first sync, conflict resolution, or a mutation queue that survives reload.
- Automatic placement of created entities into filtered or paginated lists. The runtime
  refetches; the application may override per query.
- Capability gating as a security boundary. It is a UX affordance. Authorization remains
  server-side.

## Design

### Architecture and packaging

```
@forge/client-core       entity store, normalizer, tag graph, query engine,
                         transports, auth. No framework dependency.
@forge/client-react      adapter (~300 LOC): useSyncExternalStore bindings
@forge/client-vue        adapter: shallowRef + effectScope
@forge/client-angular    adapter: signals + DestroyRef
@forge/client-devtools   cache inspector, tag graph viewer. Separate entry point.
```

Generation emits types, an operation manifest, and one-line typed facades:

```ts
// generated: hooks.ts
export const useOrderList   = query<ListOrdersReq, ListOrdersRes>(ops.orderList);
export const useOrderGet    = query<GetOrderReq,  Order>(ops.orderGet);
export const useOrderCreate = mutation<CreateOrderReq, Order>(ops.orderCreate);
```

No per-endpoint logic is generated. A runtime bug is fixed by publishing the runtime, not by
regenerating every consuming repository. A new framework is a new adapter, not a second
generator.

Data flow:

```
Go route + options  →  RouteInfo.Metadata  →  IR (entity/tags/bindings)
                                                      ↓
                        manifest.ts  +  types.ts  +  hooks.ts (facades)
                                                      ↓
                                          @forge/client-core
                                     ┌────────┼────────────┐
                              entity store  tag graph   transports
                                     └────────┼────────────┘
                                        framework adapter
```

Bundle budgets, gzipped and tree-shaken, enforced in CI by `size-limit`:

| Artifact | Budget |
|---|---|
| core, REST only | 9 kB |
| core + streams | 14 kB |
| React adapter | 2 kB |
| generated, per 50 endpoints | 6 kB |

TanStack Query core plus its React adapter is approximately 13 kB and provides no
normalization. The like-for-like comparison is the REST-only configuration, since TanStack
has no streaming at all: 9 kB + 2 kB against 13 kB, while additionally normalizing. Streams
cost a further 5 kB and buy a capability with no counterpart to compare against. Those are
the two numbers this design is willing to be held to; an unenforced budget is a sentence in
a README.

### Entity identity: inferred from the Go type

The three concepts sit at three different levels. Identity is intrinsic to a type; effects
belong to an operation; bindings belong to a channel. Declaring identity per endpoint would
mean repeating it on every route that returns an `Order`.

| Concept | Declared on |
|---|---|
| Entity identity | the type |
| Invalidation tags | the operation |
| Stream binding | the channel |

A response type registers as an entity when all of the following hold:

1. It is a named struct type — not anonymous, not a bare map
2. It has **exactly one** identity-shaped field: tagged `forge:"id"`, or named `ID`/`Id`, or
   carrying the json name `id`
3. That field is a string, an integer, or implements `encoding.TextMarshaler`

Typename is the Go type name. On collision across packages, **both** sides are
package-qualified — `billing.Invoice` and `shipping.Invoice` — never one silent winner, and
every qualification is reported at generation time. A typename that changes because someone
added an unrelated type in another package is a cache key that silently changes shape.

The "exactly one" clause is load-bearing. A struct carrying both `ID` and `TenantID` is
ambiguous, and guessing wrong collides two tenants' records under one cache key. That is a
data-leak defect wearing a caching defect's clothes. Inference refuses and requires an
explicit declaration:

```go
func (Order) ForgeEntity() forge.EntityDef {
    return forge.EntityDef{Type: "Order", IDField: "OrderNumber"}
}
```

A method rather than a struct tag: compile-checked, discoverable from an editor, and immune
to tag-string typos.

**Normalization recurses.** If `Order` embeds a `Customer` and a `[]LineItem`, fetching one
order populates `Customer:c-3` and every `LineItem:*`. This needs no configuration, and it
also means envelope shapes — `{data: ...}`, `{items: [...], total: n}` — need no special
handling. Entities are extracted wherever they occur in the response tree.

Opting out is per-endpoint, because the exception lives there: a projection or snapshot that
must not merge with the canonical record.

```go
router.GET("/orders/{id}/audit-snapshot", h, forge.WithoutEntity())
```

Inference runs in the Go generator. By the time the manifest ships, `Order` is a resolved
fact with a resolved ID field. The runtime never guesses.

### Invalidation tags: infer same-entity effects, declare cross-entity ones

Derived with no annotation:

| Operation | Provides | Invalidates |
|---|---|---|
| `GET /orders/{id}` | `Order:7` | — |
| `GET /orders` | `Order:7`, `Order:8`, `Order[]` | — |
| `POST /orders` | `Order:9` | `Order[]` |
| `PATCH /orders/{id}` | `Order:7` | `Order[]` |
| `DELETE /orders/{id}` | — | `Order[]`, evicts `Order:7` |

The rule: **any non-GET operation touching entity `E` invalidates `E[]`.** An operation
touches `E` when `E` occurs at any depth in its request body or response body, or when `E`
is the entity its path addresses. `PATCH` is included, which
looks over-eager — a patch only changes membership when it touches a filtered field, and the
server cannot know which lists are mounted. Over-refetching is a performance defect that can
be measured and then suppressed; under-refetching is a correctness defect that surfaces as a
stale row reported three weeks later. The default is correct and the escape is explicit:

```go
router.PATCH("/orders/{id}/notes", h, forge.WithoutInvalidation("Order[]"))
```

Hand-written declarations are therefore limited to **cross-entity edges**, which are the
genuinely non-obvious cases and few in number:

```go
router.POST("/orders", createOrder,
    forge.WithInvalidates("Inventory[]", "Customer:{req.customerId}"),
)
```

Template resolution is explicit-first — `{req.customerId}`, `{res.customer.id}` — falling
back for a bare `{customerId}` to path, then query, then request body, then response body,
first match wins. A template resolving to nothing **fails generation**. A tag that silently
resolves to the empty string is an invalidation that never fires and never reports.

### Stream binding

```go
router.WebSocket("/ws/orders", handler,
    forge.WithStreamBinding(
        forge.Emits[Order]("order.created"),   // upsert + invalidate Order[]
        forge.Emits[Order]("order.updated"),   // patch entity, no refetch
        forge.Emits[Order]("order.deleted"),   // evict + invalidate Order[]
    ),
)
```

Intent is inferred from the message-name suffix — `*.created`, `*.updated`/`*.changed`,
`*.deleted`/`*.removed` — with an explicit override for names outside the convention:

```go
forge.Emits[Order]("order.fulfilled").As(forge.StreamPatch).Invalidates("Shipment[]")
```

`order.updated` costs nothing: the payload is an `Order`, the normalizer patches `Order:7`,
every mounted view depending on it re-renders. Only membership changes reach the network.
Mutations and stream frames share one tag vocabulary because a socket frame *is* a mutation
the client did not initiate; there is one code path, not two.

### Plumbing into the IR

All three land in `RouteInfo.Metadata` under `forge.client.*` keys, surface as
`x-forge-entity`, `x-forge-tags` and `x-forge-stream` extensions in the OpenAPI and AsyncAPI
documents, and are read back by the introspector into new IR fields.

The spec documents are the contract. `forge client generate` therefore works against a
checked-in `openapi.json` with no running server, which is what makes it usable from CI and
from a frontend repository that cannot import the Go module.

New route options: `WithEntity`, `WithoutEntity`, `WithInvalidates`, `WithoutInvalidation`,
`WithStreamBinding`, and the `Emits[T]` builder.

### Runtime: skeletons, not documents

A response is split in two on arrival:

```
GET /orders  →  [{id:7, total:99, customer:{id:"c-3", name:"Ada"}}, {id:8, ...}]

           normalize
               ↓
entity store                          query skeleton  (key: orders.list|{})
┌──────────────────────────────┐      ┌────────────────────────────┐
│ Order:7      {total:99, …} v4│      │ [ →Order:7, →Order:8 ]     │
│ Order:8      {…}           v1│      └────────────────────────────┘
│ Customer:c-3 {name:"Ada"}  v2│       deps: {Order:7, Order:8, Customer:c-3}
└──────────────────────────────┘
```

The skeleton holds references and inline scalars, no entity data. Reading a query rehydrates
the skeleton against the entity store. That indirection is why `PATCH /orders/7` updates the
list, the detail page and a sidebar badge with no refetch: all three reference `Order:7`.

Recomputation is dependency-tracked. Each mounted query records the entity keys its skeleton
touched; writes bump per-entity version counters; only queries whose dependency set
intersects the changed keys rehydrate. Rehydration uses structural sharing, so an unchanged
`Customer` subtree keeps its object identity and React skips it. A store holding 50,000
entities with 40 mounted queries does work proportional to what changed.

Referential stability is a correctness requirement, not an optimization: `useSyncExternalStore`
tears if `getSnapshot` returns a new object when nothing changed. Memoizing rehydration on
`(queryKey, depVersions)` supplies it.

### Runtime: invalidation and the placement escape hatch

On mutation settle, the runtime intersects the operation's `invalidates` set against the tag
index (`Map<tag, Set<queryId>>`), marks matches stale, and refetches **mounted queries only**,
coalesced into one batch per tick. Unmounted queries are marked stale and refetch on next
mount. Refetching data nobody is looking at converts a smart cache into a bandwidth
complaint.

Placement is declarable at the mutation site and may decline per query:

```ts
const create = useOrderCreate({
  place: {
    'Order[]': (created, current, args) =>
      // filtered list, new order doesn't match — decline, fall back to refetch
      args.status && args.status !== created.status
        ? undefined
        : [created, ...current],
  },
});
```

Returning a list skips that query's refetch; returning `undefined` falls back to refetching.
This is what keeps the design out of the Relay connection-directive tarpit. The runtime never
reasons about whether an entity belongs in a filtered or paginated window, and the
application is permitted to answer *I don't know* for the cases it cannot decide.

### Runtime: live queries

`useOrderList({ live: true })` resolves the entity types in its result skeleton, looks up
bound channels in the manifest, and subscribes through a ref-counted manager: one socket per
`(endpoint, principal)`, multiplexed by channel, closed on the last unmount. An incoming
frame decodes, matches its binding, and applies the declared intent plus any tag
invalidation, through the same path as a mutation response.

Two properties that are cheap now and expensive later:

- **Gap recovery.** A dropped socket means missed frames, and a reconnected client looks
  correct while being wrong. On reconnect the runtime invalidates every tag bound to that
  channel and refetches mounted live queries. Without it, staleness after a closed laptop lid
  presents as "it just stops updating" and is unfalsifiable from the outside.
- **Write batching.** Frames coalesce into one store commit per animation frame. A channel at
  200 msg/s must not mean 200 renders.

### Optimistic writes

The store is a base plus an ordered stack of overlays. An optimistic mutation pushes an
overlay; reads merge base and overlays in order; success rebases onto the server response and
drops the overlay; failure drops it alone.

Layering is what makes *concurrent* optimistic mutations correct. With two in-flight edits to
`Order:7` where the first fails, mutation-in-place would require reconstructing what the
second edit meant against a base it never observed. Dropping one layer and re-merging is
well-defined.

### Auth and capability gating

Credential attach follows the endpoint's declared scheme. A 401 triggers a **single-flight**
refresh with one retry; concurrent requests queue behind the in-flight refresh rather than
stampeding.

The entity store is **partitioned by principal and dropped on identity change**. A normalized
store keys `Order:7` globally with no memory of who fetched it; without partitioning, entities
from one session remain addressable in the next. This is a correctness property, not a
feature, and it is the class of defect that document caches do not have.

Scopes declared through `WithRequiredAuth` are emitted as typed capability constants plus a
`can()` helper, so interfaces can hide unavailable actions and the client can fail a
would-be-403 locally instead of round-tripping:

```ts
if (can('orders.write')) { /* render the button */ }
```

This is a UX affordance and is documented as such. It is never a security boundary;
authorization remains server-side and unconditional.

### Errors, retries, SSR

Typed per-endpoint errors derive from the IR's `Responses` map and `DefaultError`; the
discriminated union already exists in `types.ts`.

Retries apply to **idempotent methods only** by default (`GET`, `HEAD`, `PUT`, `DELETE`),
with exponential backoff and jitter, and no retry on 4xx except 408 and 429. Retrying a
`POST` on a timeout produces duplicate orders.

SSR serializes the entity store and skeletons; hydration adopts them without refetching.
`packages/nextjs-plugin` gets a supported integration rather than a documentation section.

Entities unreferenced by any mounted skeleton are garbage-collected, with an LRU cap on the
store.

### Staying in sync with the backend

Drift is the failure mode that kills generated clients. Five layers, each individually cheap:

**1. Dev loop.** `forge client watch` watches the specification source and regenerates on
every change, using exactly the configuration `forge client generate` resolves. A changed
Go handler that re-emits the spec regenerates the manifest and types, and the frontend's
TypeScript server surfaces the error in the editor. No restart, no manual regenerate. This
loop is the substance of tRPC's reputation, and it is available there only if the backend is
TypeScript.

This was originally specified as a subscription to a spec-changed event over the debug hub
(`debug_hub.go`, `debug_server.go`). That does not work, and the design is filesystem-based
instead: `debug_server.go` sits behind `//go:build forge_debug`, which narrows but does not
rule out its availability — `forge dev` already builds the app with `-tags forge_debug`
(`dev.go`'s `runApp` and asset-watcher build), so the server is present under `forge dev`.
What actually kills the approach is that the hub broadcasts only metrics and health — there
is no spec-changed event on it to subscribe to, under any build tag. A file spec is
watched through fsnotify (registered on the spec's parent directory, because editors save by
renaming a temp file over the original and a watch on the file itself would go deaf after
the first save); a `--from-url` spec is polled, regenerating only when the fetched bytes
differ. Generation failures are reported and never stop the watch — a spec is invalid
halfway through being edited more often than not.

**2. CI gate.** `forge client check` regenerates into a temporary directory, diffs against
committed output, and exits non-zero on difference — the shape of a `gofmt -l` gate. Watch
mode is what developers enjoy; this is what makes staleness impossible.

**3. Change classification.** `forge client diff old.json new.json`:

| Compatible | Breaking (API) | Breaking (cache) |
|---|---|---|
| added endpoint | removed endpoint | entity typename changed |
| added optional request field | added required request field | ID field changed |
| added response field | removed response field | entity became non-entity |
| widened type | narrowed type | tag removed or renamed |

The third column is specific to this design and is not producible by existing OpenAPI diff
tools. Renaming `Order` to `PurchaseOrder` breaks no HTTP contract — every request and
response is byte-identical — but it repartitions the cache. A persisted store still holding
`Order:` keys becomes unreachable, and a client mid-session normalizes one record under two
identities. It presents as a rendering defect several screens from the rename.

Classification drives the generated package's semver, which is what makes it safe to depend
on.

**4. Runtime guards.** The manifest carries the spec hash it was built from. In development
the client fetches the server's hash once at startup and logs a loud console error on
mismatch — zero production cost. A persisted store is keyed by spec hash, so a deploy that
changes the spec drops it rather than rehydrating entities into a shape that no longer
exists. Optionally the client sends its hash as a request header, letting the server observe
version skew, which is the only way to answer how many users are on a stale bundle before
removing a field.

**5. Forward compatibility.** Unknown response fields are **preserved, not stripped** — a
normalized store merging a newer server's richer `Order` must not discard fields another view
reads. Unknown stream message types are ignored with a development-only warning. Together
these let the server deploy ahead of the client, which it always will.

## Testing

Go-side work extends the existing suite (`determinism_test.go`, `tscheck_test.go`, fixtures):

- **Inference table tests**, driven hardest at the refusal cases: two ID-shaped fields,
  unnamed types, `any`-typed fields, generic instantiations, cross-package collisions. The
  rule is only as good as its failures.
- **Tag derivation tests**: route shape to expected provides/invalidates, including template
  resolution and the generation-time failure when a template resolves nowhere.
- **Determinism** extended to the manifest. Go randomizes map iteration order, and a manifest
  whose key order drifts produces a spurious diff on every CI run, which trains everyone to
  ignore `forge client check`.

Runtime:

- **Property-based normalizer tests** (`fast-check`): for any response tree,
  `denormalize(normalize(x)) === x`. Hand-written cases lose here — the defects live in arrays
  of arrays, nullable refs, an entity appearing twice at different depths, and **cycles**
  (`Order → Customer → Orders[] → Order`), which any ORM with eager loading produces.
- **Invariants**, stated as properties: a patch to `Order:7` updates every dependent mounted
  query and causes no other recomputation; rollback of a failed optimistic mutation restores
  pre-mutation state exactly, including under two concurrent mutations on one entity; no query
  reads an entity written under a different principal; a client reconnecting after a dropped
  socket converges to the same state as one that never disconnected.
- **React StrictMode.** Development double-invokes effects, producing mount → unmount → mount.
  Naive ref counting closes the socket on the phantom unmount and the live query silently
  stops. It reproduces only in development, so it gets reported as "works in prod, broken
  locally" and dismissed. It gets a dedicated test.
- **Contract tests against a real server**: start an example Forge app, generate a client from
  it, run the client against it. Unit tests on either side of the IR can both pass while
  disagreeing about the IR.
- **Bundle budget** enforced by `size-limit`, failing the build on breach.

## Phases

1. **Go side** — route options, IR fields, manifest emitter, facade emitter,
   `check` / `diff` / `watch`
2. **Core** — entity store, normalizer, skeletons, tag graph, REST transport
3. **React adapter** — then dogfooded on `extensions/dashboard`, a real Forge frontend with
   real data
4. **Streams** — WebSocket, SSE and WebTransport binding, live queries, on a core proven by
   phase 3
5. **Vue and Angular adapters** — written against a core that has shipped
6. **Devtools**

Phases 3 and 4 are the gate. Writing a second adapter before the first has run against the
dashboard bakes a bad core abstraction into three frameworks simultaneously. Full scope
ships; the order is sequential rather than parallel.

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Identity ambiguity causing cross-tenant collision | Critical | Refuse to infer on more than one ID-shaped field; require explicit `ForgeEntity()` |
| Normalized-cache tarpit (the Relay/Apollo failure mode) | High | Refetch by default, per-query-declinable placement, devtools showing why a query refetched |
| Maintaining a client state library | High | Small framework-agnostic core, adapters under 300 LOC, enforced budget. Accepted, not mitigable. |
| Three adapters before the core has miles | High | Phase gate at 3 and 4 |
| Socket count growing with the render tree | Medium | Ref-counted multiplexing, dev warning above a threshold, devtools connection panel |
| Cache memory growth | Medium | GC of unreferenced entities, LRU cap |
| Bundle erosion | Medium | CI gate |
| Go types that cannot be named (`any`, unnamed generics) | Low | Degrade to document caching, report at generation time |
| Adoption friction from the existing client | Low | Additive; see below |

The first row is the one to hold hardest. Every other risk produces a visible defect. That
one produces data that looks entirely plausible and belongs to someone else.

**Migration.** `rest.ts`, `websocket.ts`, `sse.ts` and `webtransport.ts` keep their public
surface and keep working, because the runtime drives them rather than replacing them.
`query.ts` (365 lines) is deleted, and its TanStack hooks are the only thing an existing
adopter must move off. Adoption is per-component: one screen may use `useOrderList()` from
the new runtime while the next uses the raw REST client, from the same generated package.

## Decisions and their rationale

**A new runtime rather than a TanStack Query plugin.** TanStack's cache entry is the response
object. Normalization requires an indirection between the cache entry and the hook's return
value, which is a change to its central data structure. This answers the reasonable question
of why a TanStack adapter was not sufficient.

**Identity inferred by default rather than opt-in.** An opt-in design leaves the payoff
invisible until a developer does work whose benefit they have not yet seen. Forge can infer
reliably because the server has real types — Apollo's `dataIdFromObject` guesses from JSON
shape at runtime, while Forge knows at generation time that an endpoint returns
`*orders.Order`. The historically painful part of normalization is cheap here specifically.

**Identity declared on the type, effects on the operation, bindings on the channel.**
Identity is intrinsic to a type; declaring it per endpoint would repeat it on every route
returning an `Order`.

**Over-invalidate by default.** Under-refetching is a correctness defect discovered late by a
user; over-refetching is a performance defect discovered early by a profiler.

**Placement declinable per query.** The runtime cannot know whether a created entity belongs
in a filtered window. The application can, sometimes. Letting it answer *I don't know* is
what distinguishes this from Relay's connection directives.

**`live: true` at the call site rather than automatic.** A developer reading a component
should be able to tell that it holds a socket. Automatic subscription makes connection count
an emergent property of the render tree.

**Capability gating included, scoped to UX.** Forge already records scopes per route and
discards them. Hiding unavailable actions is most of what dashboard applications hand-write.
Documented explicitly as never a security boundary.

**Generated output contains no logic.** A runtime defect is fixed by publishing the runtime.
A new framework is a new adapter rather than a second generator. This is also the whole of
the "not bloated" claim.
