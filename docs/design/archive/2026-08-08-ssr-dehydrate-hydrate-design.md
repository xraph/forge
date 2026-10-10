# SSR `dehydrate` / `hydrate` for the Forge web client runtime

This is the original proposal from 2026-08-08. For current behavior, see [SSR](../../content/docs/web-client/ssr.mdx).

Date: 2026-08-08
Status: archived design from 2026-08-08
Packages: `@forge-go/client-core`, `@forge-go/client-react`

## The problem

There is no store serialization, so there is no server-render story. A skeleton
already _serializes_ (references carry a `__ref` property put there for exactly
this purpose (`client-core/src/types.ts:57-66`)) but a deserialized one is not
recognised as a skeleton, because `isRef` decides reference identity by
membership in a private `WeakSet` rather than by inspecting properties
(`client-core/src/ref.ts:13-34`). Hydration therefore needs a revive pass, and
until it exists the API is withheld rather than shipped half-working.

Today this surfaces in React as: a server render returns `idle` for every query
and issues no request, and a hydrating client necessarily starts empty
(`client-react/src/useQuery.ts:33-81`).

## What ships

- `dehydrate(cache, options)` and `hydrate(cache, state, options)` in a new
  `client-core/src/ssr.ts`.
- `QueryCache.peek` and a small `QueryCache.restore` seam.
- `SettleResult.tags` on `QueryRegistry.settle`.
- `<ForgeHydrationBoundary>` in `client-react`, and a `getServerSnapshot` that
  returns real state.
- Docs, including a correction to the `nextjs-plugin` sentence in
  `not-yet-shipped.mdx`.

## The crux: reviving references without becoming lossy

### `ref.ts` does not change

`makeRef` and `markRewritten` are already exported. The revive pass mints
genuine references and re-marks rewritten containers through those primitives,
so the two `WeakSet`s stay private and the invariant `ref.ts:3-12` defends is
preserved: a _response_ containing `{__ref: …}` still round-trips untouched,
because a response never enters revive. Only data this module serialized itself
does.

### The collision, which is in the records rather than the skeleton

A genuine `Ref` is `Object.freeze({__ref: key})`: exactly one own key, string
value. A response can legitimately contain a byte-identical object:

```ts
// `meta` has no entity beneath it, so `normalize` leaves it inline
{ id: 7, meta: { __ref: 'anything' } }
// stored record data, verbatim:
{ id: 7, meta: { __ref: 'anything' } }
```

After `JSON.parse` the two are indistinguishable. A revive pass that treats
every `{__ref}` as a reference mints one from user data, and `denormalize`
resolves it to `undefined`: reintroducing through the back door precisely the
lossy round-trip `ref.ts:5-11` refuses.

### Escaping on the way out

`dehydrate` escapes; `hydrate` unescapes. The walk needed for the reachability
closure already visits every key of every record, so this costs nothing extra.

| Direction | Rule                                                                |
| --------- | ------------------------------------------------------------------- |
| serialize | own key matching `/^_*__ref$/` → emit with one extra leading `_`    |
| revive    | own key matching `/^_+__ref$/` → strip one leading `_`; it is data  |
| revive    | own key exactly `__ref`, sole key, string value → genuine reference |

`{__ref: 'x'}` as data goes out as `{___ref: 'x'}` and comes back as
`{__ref: 'x'}`. A genuine reference goes out as `{__ref: 'Order:7'}` and comes
back as a real `Ref`. The scheme is idempotent under nesting: `___ref` →
`____ref` → `___ref`.

Escaping applies to **both** halves of a normalized payload (the records and
the skeletons) because a skeleton's inline subtrees are response data for the
same reason a record's fields are.

Denormalized mode needs none of it. A denormalized value contains no genuine
references, so it ships verbatim and `store.write` handles it exactly as a live
response does.

### The revive walk

Per skeleton, bottom-up on one pass:

1. Mint a `Ref` via `makeRef` at each genuine `__ref` position.
2. Unescape data keys.
3. `markRewritten` every container with a reference anywhere beneath it.

Step 3 is what keeps hydration honest. A container _without_ a reference beneath
it is deliberately left unmarked, so `store.ts:368`'s `isRewritten` fast path
still returns it by identity and a hydrated skeleton behaves identically to a
normalized one. Marking everything would be correct but would void structural
sharing for the whole response; marking nothing would leave references
unresolved beneath the fast path.

## The payload

Discriminated on `mode`, so `hydrate` never guesses:

```ts
type DehydratedState =
  | {
      v: 1;
      mode: "normalized";
      principal?: string | number | null;
      records: Record<EntityKey, unknown>; // escaped, closure-limited
      queries: readonly {
        operation: string;
        args: TagContext;
        skeleton: unknown; // escaped
        tags: readonly string[];
      }[];
    }
  | {
      v: 1;
      mode: "denormalized";
      principal?: string | number | null;
      queries: readonly {
        operation: string;
        args: TagContext;
        value: unknown;
      }[];
    };
```

Selected by `dehydrate(cache, { principal, mode })`, defaulting to
`'normalized'`. Both ship because they trade differently and the trade is the
caller's: normalized is the smallest wire form and dedupes an entity five
queries share; denormalized is the simplest to reason about and needs no revive
pass at all. They share one restoration path, differing only in how a query's
skeleton is obtained.

### Everything derivable is derived, not shipped

The payload carries the minimum that cannot be recomputed on the client:

| Not shipped   | Recomputed from                                                                                       |
| ------------- | ----------------------------------------------------------------------------------------------------- |
| the cache key | `cache.key(meta, args)`: deriving it means a key-scheme change cannot desynchronise server and client |
| `provides`    | `meta.provides`, exactly as `cache.ts:576` builds a `QuerySpec`                                       |
| `rootType`    | `meta`, at hydrate time                                                                               |
| `deps`        | `EntityStore.dependencies(skeleton)` after revive: exact, and does not trust the payload              |

`tags` is the one exception, and only in normalized mode. `registry.settle`
re-resolves `provides` templates _from the response_ (`registry.ts:311`), and
normalized mode has no response; without shipping them, every `{res.x}`-templated
tag silently vanishes and a mutation stops reaching the query that displays what
it changed. `SettleResult` therefore gains an optional `tags` that bypasses
resolution, used by `hydrate` only. Denormalized mode ships nothing extra: `settle({response: value})` recomputes tags exactly as a live response does.

### What `denormalized` mode actually serializes, and its one limitation

The cache does not retain raw responses: `registry.settle` reads `response` to
resolve tags and does not store it, and `Record_` holds a skeleton rather than a
document. So denormalized mode serializes `store.read(record.skeleton)`: the
rehydrated value, which is the response as the store currently holds it, with
merges applied.

That is equivalent for hydration, because `store.write` re-normalizes it into
the same records and an equivalent skeleton. It has one limitation normalized
mode does not:

> **A query whose value contains an entity cycle cannot be dehydrated in
> `denormalized` mode.** `denormalize` rebuilds `Order → Customer → Orders[] →
Order` as a real cycle, and no JSON encoding of it exists. It throws with the
> same cycle error. Normalized mode serializes exactly that graph without
> difficulty, because it closes through references and the records map is flat.

This is stated in the docs as the reason normalized is the default.

### `version` and `frameAt` are not carried

Records dehydrate with data only. The frame clock is per-session and monotonic
for the life of a store (`store.ts:82-90`); a server had no frames, and carrying
its readings would let them be compared against a client clock they have no
relationship to. A hydrated client store starts at version 1 with stamp 0.

### `OperationMeta` cannot be reconstructed, so it is supplied

A cache record holds an `OperationMeta` (`cache.ts:100`) and needs it to
refetch, to `watchLive`, and to drive the transport. It is route metadata living
in the generated `ops.ts`, not in the store, so `hydrate` takes it:

```ts
import { ops } from "./generated/ops";
hydrate(cache, state, { ops }); // operation name -> OperationMeta
```

`ops.ts` already exports exactly that table keyed by operation name, so this is
a lookup rather than an invention. Rejected alternative: serializing each
`OperationMeta` into the payload, which makes `hydrate` argument-free at the
cost of putting the route table (methods and paths) into every HTML response
to duplicate data the client bundle already ships.

### How `hydrate` installs a query: the `restore` seam

`ssr.ts` lives in the same package as `cache.ts`, so it writes through one small
method rather than reaching into private fields:

```ts
// QueryCache
restore(meta: OperationMeta, args: TagContext | undefined, input: {
  skeleton: unknown;
  tags?: Iterable<string>;      // normalized mode; absent in denormalized mode
  response?: unknown;           // denormalized mode; lets settle resolve tags
  stale?: boolean;
}): void;
```

It opens the record through the existing `open`, sets
`skeleton`/`settled`/`status: 'success'`, calls `registry.settle` with
`deps: store.dependencies(skeleton)` plus either `tags` or `response`, marks the
entry stale when asked, and notifies. Roughly twenty lines, and it is the only
class-method growth in this change: free functions tree-shake, class methods do
not, so it is kept deliberately small.

**Hydrating into a warm cache merges rather than replaces.** Records go through
`put`, which merges fields and returns `false` for identical data (`store.ts:244`),
so a payload matching what the client already holds bumps no version and moves no
identity. A query the cache already has settled is re-settled against the
hydrated skeleton. Hydrating the same payload twice is therefore a no-op beyond
the walk itself, which is what makes the React boundary's guard an optimisation
rather than a correctness requirement.

## The security property

`dehydrate` never reads `store.keys()`. The record set is _built_ by a
breadth-first walk from the exported queries:

```
seed     = the skeletons of the exported queries
step     = every Ref found -> add its EntityKey -> walk that record's data -> more Refs
fixpoint = the transitive closure; nothing else is emitted
```

**An entity that no exported query transitively references cannot appear in the
payload**: not because a rule forbids it, but because nothing ever put it
there. A module-level cache holding three concurrent requests' data exports only
what the queries you named actually reach. This is the design's answer to
"a dehydrated payload is server state embedded in an HTML response".

One walk does four jobs, since it is already visiting every node: copy, escape,
collect references for the closure, and detect cycles on the route.

### What is exported

Only queries with `status === 'success'`. A pending query has no skeleton, and
an errored one would hydrate a client into a failure the server observed and the
client cannot meaningfully retry. Both are absent, and the client fetches them
normally.

`options.include` narrows further, by cache key. A key the cache does not hold
throws rather than exporting nothing: a typo that silently ships an empty
payload is the defect found in production.

### The principal

Required on `dehydrate`, and constrained to `string | number | null |
undefined`. Anything else throws.

That is not arbitrary. `setPrincipal` (`cache.ts:501`) compares with `===`, so an
object principal already re-clears the cache on every call that mints a fresh
object; the store's working contract is a scalar, and this states it. `undefined`
is encoded as the key's absence, which is what `JSON.stringify` does with it, so
all four values compare correctly with `Object.is` after a round-trip.

`hydrate` refuses a payload whose principal differs from the client cache's
owner. That is the second half of the pair: a payload cached at a CDN and served
to the wrong session refuses to load rather than rendering another user's data.

`hydrate` asserts; it does not call `setPrincipal`. The application sets the
principal first, and a later `setPrincipal` with a different value clears the
hydrated store, which is the correct behaviour rather than a conflict.

## Freshness

```ts
hydrate(cache, state, { ops }); // fresh: no request on mount
hydrate(cache, state, { ops, stale: true }); // render now, refetch on mount
```

Default fresh, because for a dynamically rendered page the server fetched the
data milliseconds earlier. `stale: true` marks the registry entries stale
through the existing `markStale` path, which is what a statically generated or
ISR page wants: instant paint, then a verifying refetch. No new clock and no
`staleTime` concept enters the runtime: the registry's `settledAt` is a reading
of an invalidation clock, not a timestamp, and this change does not alter that.

## Errors

All plain `Error` with a `[forge] ` prefix, matching `client.ts:33` and
`cache.ts:1093`. This package has no custom error classes and does not gain any.

| Where     | When                                                       |
| --------- | ---------------------------------------------------------- |
| dehydrate | `principal` differs from `cache.owner`                     |
| dehydrate | `principal` is not `string \| number \| null \| undefined` |
| dehydrate | `include` names a key the cache does not hold              |
| dehydrate | a cycle that the selected mode cannot encode (see below)   |
| hydrate   | `state.v` is not 1, or `state.mode` is unrecognised        |
| hydrate   | `state.principal` differs from `cache.owner`               |
| hydrate   | an operation name is absent from `ops`                     |

### Cycles

In **normalized** mode, cycles _between_ records (the README's
`Order → Customer → Orders[] → Order` case) serialize untouched and rebuild as
cycles, because they close through references and the records map is flat. In
**denormalized** mode the same graph is a real cycle in the serialized value and
throws; that is the limitation stated above, and the reason normalized is the
default.

The remaining case is common to both modes. A plain-object cycle _inside a
single record's own subtree_ is a shape JSON cannot express under either
encoding. It is unreachable from a JSON transport by construction,
but `put` is called with hand-built objects on the frame path and on the
optimistic path still to come, where aliasing is ordinary (`store.ts:679`). It
throws, naming the query key, the entity key and the field path:

```
[forge] dehydrate: cannot serialize a cycle within one record
  query   orderList({"status":"open"})
  entity  Order:7
  path    data.meta.self.self
```

Detection is free: the closure walk already carries a route.

Rejected alternatives: a `{__cycle: path}` marker, which adds a second in-band
marker to a codebase whose `ref.ts` argues at length against exactly that; and
silently breaking the back-edge, which leaves the hydrated client holding
different data from the server with nothing reporting it.

## The React surface

### `<ForgeHydrationBoundary state ops client? children />`

In `client-react/src/hydration.ts`, built with `createElement` so the package
still emits no JSX-runtime dependency (`context.ts:36`). The client is resolved
through `useForgeClient`, so the existing explicit → provider → global
precedence holds.

**It hydrates during render, not in an effect.** Children read `getSnapshot`
during their own render, which happens after the parent's render returns, so a
render-phase hydrate is visible to them on the first pass. An effect runs after
the tree commits: the first paint would be the loading branch and then flip,
which is a visible flash and, on the hydration pass, exactly the mismatch this
feature exists to remove.

Two consequences, handled:

- **StrictMode double-invokes render.** A `WeakMap<QueryCache, WeakSet<object>>`
  keyed on the state object makes the second pass a no-op. Per cache rather than
  global, because the same payload legitimately hydrates two caches.
- **The boundary is a client component, so it renders on the server too**,
  hydrating the server cache with the payload the server just produced. Every
  `put` is deep-equal, so `store.ts:262` keeps the previous record object, bumps
  no version and churns no identity. Harmless: and it means `peek` hits even
  for a query the boundary seeded rather than the page prefetched.

### `QueryCache.peek(meta, args): QueryState | undefined`

Resolves the key, returns `snapshot(record)` when a record exists, and creates
nothing when it does not. Stable by construction: `snapshot` returns the previous
state object whenever nothing in it moved.

`peek` is the API `useQuery.ts:170` already names as the clean fix for the
render-side record-creation wrinkle. This change adds it and uses it for the
server path only; routing the _client_ `getSnapshot` through it as well is
deliberately out of scope.

### `getServerSnapshot`

Becomes the peek, falling back to the frozen `IDLE` constant, exposed as
`getServerState` on the query handle in `client.ts` next to the
`subscribe`/`getState`/`refetch` it already carries. The three constraints in
`useQuery.ts:33-71` are then satisfied by machinery rather than by abstaining:
stable because `snapshot` memoizes, matching because the boundary hydrated above
it, and non-mutating because `peek` opens nothing.

A server render emits the order table rather than the spinner.

### Vue and Angular

Get nothing in this change, and the docs say so. `dehydrate`/`hydrate` are
framework-agnostic and those adapters can call them directly from an SSR setup;
what they do not get is a boundary component or a server-snapshot path.

## Package placement

`packages/nextjs-plugin` already exists and is something else: a dashboard
contributor bridge exporting `withForge`, `ForgeBridge` and `useForgeBridge`
over `@forge-go/bridge-client`. It is not a starting point and it is not renamed.

The SSR surface lives in `client-core` (framework-agnostic) and `client-react`
(the boundary and the server snapshot). No new package. The Next.js App Router
integration then needs none:

```tsx
// app/orders/page.tsx  (server component)
const cache = new QueryCache({ transport, entities });
cache.setPrincipal(session.userId);
await cache.fetch(ops.orderList);

return <Orders state={dehydrate(cache, { principal: session.userId })} />;

// orders.tsx  ('use client')
<ForgeHydrationBoundary state={state} ops={ops}>
  <OrderTable />
</ForgeHydrationBoundary>;
```

A `packages/client-next` remains available later if streamed-payload injection
or `cache()`-based request dedup earns one. It does not yet.

## Files

| File                            | Change                                                     |
| ------------------------------- | ---------------------------------------------------------- |
| `client-core/src/ssr.ts`        | new: both walks, both modes, the escape scheme, the errors |
| `client-core/src/cache.ts`      | add `peek`, add the `restore` seam                         |
| `client-core/src/registry.ts`   | `SettleResult.tags`                                        |
| `client-core/src/client.ts`     | `getServerState` on the query handle                       |
| `client-core/src/index.ts`      | exports                                                    |
| `client-core/package.json`      | size-limit entry; raise `core with streams`                |
| `client-react/src/hydration.ts` | new: `ForgeHydrationBoundary`                              |
| `client-react/src/useQuery.ts`  | `getServerSnapshot` via the handle                         |
| `client-react/src/index.ts`     | exports                                                    |

## Size budgets

`dehydrate` and `hydrate` are free functions and tree-shake out of an
application that never imports them, so the two budgets that gate real
applications (`core, REST only` at 9 kB and `core with streams` at 14 kB) are
unaffected in substance. But the `core with streams` entry has no `import` field,
so it weighs the whole bundle and `ssr.ts` will push it past 14 kB. That entry
is raised, with a note in the README's budget section stating why the figure
moved and that no application-facing budget did. A new `ssr` entry measures
`{ dehydrate, hydrate }` on its own.

## Testing

`client-core` uses Vitest with tests in `__tests__/`, importing from `../src/…`
and sharing `./schema` and `./harness`. This follows that.

### `client-core/__tests__/ssr.test.ts`

- **The collision case.** A record holding `{__ref: 'Order:7'}` as _data_
  round-trips as data and does not resolve to `undefined`. This is the test that
  fails under the naive design, and the one that justifies the escaping.
- Nested escaping: `{___ref: 'x'}` and `{____ref: 'x'}` round-trip.
- `isRewritten` is true on revived containers with a reference beneath and
  **false** on those without, so the identity fast path survives hydration.
- Reachability: two queries in one cache, `include` one, assert the other's
  entities are absent from the payload.
- Cycles: `Order ↔ Customer` through references round-trips and rebuilds _as a
  cycle_; a plain-object cycle inside one record throws with the path.
- A `{res.x}`-templated `provides` tag still invalidates the query after a
  normalized hydrate: the test that catches dropping `tags`.
- Freshness both ways: default hydrate issues no request on subscribe;
  `{stale: true}` refetches.
- Principal: mismatch throws on both sides; a non-scalar throws.
- Idempotence: hydrating the same payload twice keeps unchanged identities.
- Denormalized mode: the same round-trip property; `tags` resolve from the
  serialized value rather than from the payload; and a query holding an entity
  cycle throws rather than producing an unserializable value.
- Both modes: `deps` after hydration equal `deps` before dehydration, since they
  are recomputed from the skeleton rather than trusted from the wire.

### `client-core/__tests__/roundtrip.property.test.ts`

Add the property `normalize → dehydrate → JSON → hydrate → denormalize` is
equivalent to `normalize → denormalize`, over the existing generators.

**Seed `__ref` and `___ref` into `propertyName`'s pool.** It currently draws
arbitrary strings, so it will never hit the collision by chance: which is why
the collision survived earlier review as a comment rather than as a test.

### `client-react/__tests__/ssr.test.tsx`

`renderToString` emits the order table rather than the loading branch;
`hydrateRoot` under the boundary logs no hydration mismatch and the fake
transport records **zero** calls.

## Out of scope, named rather than implied

- Streaming-SSR payload injection. Multiple boundaries work; no flush helper.
- Vue and Angular boundary components.
- `packages/client-next`.
- Routing the client-side `getSnapshot` through `peek`.
- Entity garbage collection, optimistic overlays, and the other runtime gaps
  already listed in the README: untouched.

## Documentation

- `docs/content/docs/web-client/ssr.mdx`: new, under the "Browser runtime"
  group in `meta.json`, after `adapters`.
- `docs/content/docs/web-client/not-yet-shipped.mdx`: the SSR section is
  removed and the feature moves to "What is shipped". Its claim that the design
  "names a supported `packages/nextjs-plugin` integration" is corrected: that
  package is a dashboard contributor bridge, and the SSR surface deliberately
  lives in `client-core` and `client-react`. Vue/Angular boundaries and streamed
  injection are added as named gaps.
- `packages/client-core/README.md`: an SSR section; the first "Known gaps"
  bullet is removed; the budget note.
- `packages/client-react/README.md`: the boundary and the server snapshot.
- Mirror with `pnpm docs:import <forge-repo-path> forge v1` from
  `/Users/rexraphael/Work/xraph/website`. That repo's `content/docs/forge/v1/**`
  is generated and must never be hand-edited.
