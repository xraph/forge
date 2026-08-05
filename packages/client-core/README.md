# @forge-go/client-core

The runtime a generated Forge client delegates to. Generated output contains
types and one-line facades; everything a hook does lives here, so a runtime
defect is fixed by publishing this package rather than by regenerating every
repository that consumes a client.

Three chunks so far: the **normalized entity store**, the **tag graph** that
turns "this mutation invalidates `Order[]`" into "these three mounted queries
must refetch", and the **query engine and REST transport** that a generated
`hooks.ts` binds to. Streaming transports, optimistic overlays and framework
adapters land later. Nothing here reaches the network on its own: the HTTP
client, the clock and the scheduler are injected.

```ts
import { EntityStore, denormalize } from '@forge-go/client-core';
import { entities, ops } from './ops'; // generated

const store = new EntityStore();
const { skeleton, deps } = store.write(response, entities, ops.orderList.rootType);

denormalize(skeleton, store); // the response, rebuilt from the store
```

## What it does

A response is split in two on arrival. Entities go into a flat store keyed
`Type:id`; what is left is a *skeleton* holding references and inline scalars.
Reading a query rehydrates the skeleton against the store, which is why a
`PATCH /orders/7` updates the list, the detail page and a sidebar badge with
no refetch: all three reference `Order:7`.

- `normalize(value, entities, rootType?)` — pure. Returns `{skeleton, records, deps}`.
- `EntityStore#write(value, entities, rootType?)` — normalize and commit.
- `denormalize(skeleton, store)` — rebuild, with structural sharing.
- `EntityStore#dependencies(skeleton)` — the entity keys a skeleton reaches.

### Caller contract: nothing here is copied defensively

A subtree containing no entity is never copied. The skeleton holds the
response's own object, and `denormalize` hands that same object back — which is
exactly what makes identity stable across reads, and is why the response you
passed to `write`, the skeleton, and the denormalized result can all be the
same object.

**Treat all three as immutable, and stop using the response object once you
have written it.** Mutating any of them changes the others with no version bump
and no invalidation, so a component keeps rendering from a memo built before
the edit. Copy before mutating.

## How the runtime knows a typename

A JSON response carries no typename, and this runtime refuses to derive one
from shape. "It has an `id` property, so it must be an entity" is the guess
the Go side deliberately declines to make, because a type carrying both `ID`
and `TenantID` keys two tenants' records to one cache entry.

Instead the typename arrives from the generated manifest and is propagated
structurally:

- `rootType` names the type of the response — `ops.orderGet.rootType`, resolved
  in Go against the real response schema. It is **not** `ops.orderGet.entity`,
  which names what the response is *about*; the two agree for a bare record or
  array and diverge for an envelope.
- An array does not change the typename: `[]Order` is a list of `Order`.
- Descending into an object's field uses `entities[Type].fields[field]`, which
  maps a JSON property to the typename of what it contains (the element
  typename for arrays).

An object is an entity when, and only when, the table names its type *and* the
object carries that type's id field with a usable value (a non-empty string, a
finite number, or a bigint).

The id is stringified into the key, so numeric `7` and string `'7'` are one
`Order:7`. That is deliberate: a Go type's identity field has one type, fixed
at generation time, so one typename cannot legitimately produce both — and a
server that returns `7` from one endpoint and `"7"` from another is describing
the same record. Keying them apart would split one entity into two entries that
never converge.

### What the generator emits

`internal/client/generators/typescript/opsmanifest.go` emits

```ts
export const entities = {
  Customer: { idField: 'id', fields: { orders: 'Order' } },
  LineItem: { idField: 'id' },
  Order: { idField: 'id', fields: { customer: 'Customer', items: 'LineItem' } },
  PageOrder: { fields: { items: 'Order' } },
} as const;
```

`fields` is resolved in Go by walking each entity's component schema:
`internal/client/entity_fields.go` runs once both intermediate-representation
builders have discovered every entity, and records a property whose type
resolves to a named schema — through a direct `$ref`, an array whose items are
a `$ref` (the *element* name, since a typename passes through an array
unchanged), or a `oneOf`/`anyOf`/`allOf` wrapper naming exactly one type, which
is how a nullable reference is spelled. It is omitted when a type has no such
property.

**An edge is only recorded when an entity is REACHABLE through it.** The walk's
only use for one is `schema[type]`, so an edge to a type with nothing worth
descending to buys nothing — an enum, a plain value struct. But a named
non-entity with an entity beneath it does get a row, carrying `fields` and no
`idField`:

- A named **non-entity** hop no longer breaks the chain. `Order → Shipment →
  Carrier`, where `Shipment` has no identity field, keeps the `Order.shipment`
  edge and gives `Shipment` its own row, so the `Carrier` below is lifted out.
- An **envelope** — `{items: [...], total: n}` — gets a row too, so a paginated
  response normalizes. `ops.orderList.rootType` names the wrapper while
  `ops.orderList.entity` names what it carries; the two are separate fields
  precisely because passing the entity name as the root reads `Order`'s edges
  against the envelope's properties and descends into nothing.

A row with **no `idField`** means "walk me, never store me". Types that carry
one are only reachable through this table, never keyed into the store. This
replaced an earlier workaround — an `idField` no payload was expected to carry —
which worked only for as long as no payload carried that property.

Only types some **root** reaches get a row: the entities, and the endpoints'
response root types. A wrapper mentioned solely by a request body is never
walked into, so it stays out of a file CI byte-diffs.

An envelope's **cache contract** is a separate question from routing, and is not
inferred. `PageOrder{items: [Order], total}` and `OrderReport{topOrders:
[Order], generatedAt}` are the same shape, and only one of them is the
collection — so `provides: ['Order[]']` requires an explicit `x-forge-envelope`
on the schema (`forge.ForgeEnvelope` on the Go type). Both responses normalize
either way; the declaration only adds tags.

## Cycles

An ORM with eager loading produces `Order → Customer → Orders[] → Order`, and
merging alone produces cycles the response never had: `{id: 1, related: [{id: 1}]}`
is a finite tree, but there is one `Order:1`, so its record references itself.

Normalization terminates on both, by registering a node's skeleton before
walking its children. **Denormalization rebuilds the cycle as a cycle** rather
than stopping at the back-edge and handing back the raw reference. An
application walking an association must never have to recognise a cache
internal; that indirection is the thing the skeleton exists to hide. The store
stays flat and JSON-serializable either way — each record is acyclic, and only
the graph *between* records closes.

## Structural sharing

`useSyncExternalStore` tears when `getSnapshot` returns a fresh object for
unchanged state, so referential stability is a correctness requirement here,
not an optimization.

Three mechanisms, together:

1. **Nothing unchanged is rewritten.** `normalize` returns the input subtree by
   reference when no entity occurs beneath it, so most of a response is already
   its own skeleton and rehydrating it is the identity function.
2. **Rehydration is memoized per subtree**, keyed by entity key for records and
   by node identity for skeleton containers.
3. **Invalidation walks the reverse-dependency graph** from the key that was
   written, so a write touches only the memos that actually reach it — work
   proportional to what changed, not to the size of the store. A one-hop
   version check would miss a change two hops away; this does not.

A write whose data is deep-equal to what is already stored is not a write: the
version does not move, the record object is kept, and no identity downstream
of it changes. A poll that returns the same bytes must not re-render anything.
The comparison tracks the *route* it took rather than every object it has
seen — an object reached twice through different fields is a DAG, not a cycle,
and treating the second encounter as already-equal would drop a real change
without ever looking at what it was compared against.

A cycle closing through plain objects rather than through an entity has no
entity key for invalidation to travel through, so those memos are indexed
under the dependencies of the frame the cycle closes back onto. Every memo on
the cycle is reachable from that frame, so its dependency set is a superset of
each one's: over-invalidation, never under. Sharing is preserved everywhere,
including for the cyclic subtree itself and for everything unrelated to it in
the same response.

## The tag graph

```ts
import { Invalidator, QueryRegistry } from '@forge-go/client-core';
import { ops } from './ops'; // generated

const registry = new QueryRegistry();
const invalidator = new Invalidator(registry, {
  execute: (batch) => batch.forEach(refetch), // the transport chunk supplies this
});

// Mounting is explicit, and returns the undo.
const unmount = registry.mount({
  operation: 'orderList',
  args: { query: { status: 'open' } },
  provides: ops.orderList.provides,
});

// When the fetch lands, the store's dep set becomes the query's tags.
const { skeleton, deps } = store.write(response, entities, ops.orderList.rootType);
registry.settle(queryKey('orderList', args), { deps, value: skeleton });

// When a mutation settles, matching mounted queries refetch, in one batch.
invalidator.settled({
  invalidates: ops.orderCreate.invalidates,
  args: { body },
  response: created,
});
```

A mounted query provides two kinds of tag, and they live in one set:
`provides` from the manifest, resolved against its arguments and response
(`Order[]`, `Order:{id}`), and the entity keys `normalize` reported for its
skeleton. Those keys are already spelled `Type:id`, which is exactly what a
mutation to `Order:7` invalidates, so a list that loaded `Order:7` is reachable
from a write to it with no second index.

### Templates that resolve to nothing are skipped, and reported

`{req.x}` searches the request (path, then query, then body). `{res.a.b}`
searches the response. A bare `{x}` searches path, query, body, response —
first match wins, where a match is the first source that has the property *at
all*: a body holding `customerId: null` has answered, and falling through to
the response would invalidate a different customer's list on a value nobody
supplied.

`resolveTag` returns `undefined` rather than a partially substituted string. A
tag that silently becomes `Customer:` matches no query, fires nothing, and
reports nothing — the failure the design forbids. At runtime the `Invalidator`
**skips that one tag and reports it** through `onUnresolved` (which warns once
per template by default), keeping every other tag in the same list. Throwing
was the alternative and is worse: the write has already committed on the
server, and turning a cache defect into an application-visible error for
completed work trades a stale row for a broken screen. Generation is where an
unresolvable template is supposed to fail; this is the runtime's report that
one got past it.

### One structure, not two

The registry and the tag index are one class, because every mount, unmount and
settle touches both. A path that updates one and not the other leaks (a bucket
holding a query nobody watches, refetching forever) or silently stops a query
updating. The index is private and has no mutator of its own.

The index holds **mounted queries only** — the last unmount removes the entry
from every bucket and deletes the buckets it emptied. An invalidation arriving
while a query is unmounted is still observed, through a clock rather than
through a retained index entry: each invalidation stamps a counter onto the
tags it touched, each settle stamps it onto the query, and a query mounting
with a newer tag stamp than its own missed something.

A stamp is written **only for a tag some remembered query already carries**,
and deleted when the last carrier is forgotten. An earlier version stamped
every tag and pruned nothing, on the claim that the stamps were bounded by the
API's tag vocabulary. They were not: `settle` folds a query's entity
dependencies into its tag set, so `Order:7`, `Order:8`, … are all tags, and
every distinct entity ever invalidated left a permanent entry — a bound of
"every entity the application has ever touched".

Restricting the write loses nothing. A query acquires a tag in exactly two
places, `mount` and `settle`, and both set `settledAt` to the current clock. A
stamp written at reading *c* while nothing carried the tag can therefore only
ever be compared against a `settledAt` of *c* or later, and the staleness test
asks for *strictly* newer: the stamp is dead the moment it is written. The same
argument licenses deleting one when its last carrier is dropped.

### Coalescing

Invalidated queries go into a set, and one flush is scheduled per tick. N
queries hit in one tick is one batch; one query hit by two tags is one refetch.
The scheduler is injectable and defaults to a microtask — never a timer, since
a delay that is "long enough" on a laptop is a flake on CI and a visible pause
on a phone. `manualScheduler()` runs nothing until asked, which is how every
coalescing test here asserts on what the code did rather than on whether a
machine got round to it.

A query that unmounted between the invalidation and the flush is dropped from
the batch: refetching data nobody is looking at is how a smart cache becomes a
bandwidth complaint. It stays stale and refetches if it mounts again.

### The placement escape hatch

```ts
invalidator.settled({
  invalidates: ops.orderCreate.invalidates,
  response: created,
  place: {
    'Order[]': (order, current, args) =>
      args.query?.status && args.query.status !== order.status
        ? undefined // filtered list this order does not belong to: refetch
        : [order, ...current],
  },
});
```

Returning a list skips that query's refetch; returning `undefined` falls back
to it. The runtime never reasons about whether an entity belongs in a filtered
or paginated window — that is the Relay connection-directive tarpit — and the
application is allowed to answer *I don't know*.

All or nothing per query: a query matched by `Order[]` and `Customer:3` where
only the first has a callback still refetches, because placing one while the
other is unhandled leaves it looking updated while being wrong. A callback that
throws is reported through `onError` and treated as `undefined`; it does not
take the rest of the batch with it.

## The query engine

```ts
import { configureClient, RestTransport } from '@forge-go/client-core';
import { RESTClient } from './client';        // generated
import { entities } from './ops';             // generated
import { useOrderList, useOrderCreate } from './hooks'; // generated

configureClient({
  transport: new RestTransport({ client: new RESTClient('https://api.example.com'), auth }),
  entities,
});

const orders = useOrderList({ query: { status: 'open' } });
const release = orders.subscribe(rerender);
orders.getState(); // { status, data, error, isFetching } -- stable across reads

await useOrderCreate({ body: { total: 99 } });  // settles, invalidates, refetches
```

`query(op)` and `mutation(op)` are what a generated `hooks.ts` imports, one
binding per endpoint. They are framework-agnostic on purpose: a `QueryHandle`
is `subscribe` plus a referentially stable `getState`, which is exactly the
`useSyncExternalStore` contract and nothing more. Every decision about
identity, staleness and deduplication is made here, where it is testable
without a renderer.

Bindings run at module scope, before an application has constructed anything,
so they resolve their cache when they are *called*. `configureClient` sets the
default; every entry point also takes an explicit `client` for the cases where
a global is the wrong answer — SSR, tests, two backends.

A query binding takes no per-call `headers` or `signal`, and a mutation binding
takes both. That asymmetry is the point: a query is *shared*. Ten subscribers
with the same arguments are one record and one request, keyed by the arguments
alone, so a per-call header would belong to whichever caller happened to create
the record and be silently dropped for the rest — and one subscriber's abort
would cancel a request nine others were waiting on. A header that varies per
request belongs in the `AuthProvider`; one that varies per query belongs in the
arguments, where it keys the cache.

### Retries: idempotent methods only

`GET`, `HEAD`, `PUT`, `DELETE`, with exponential backoff and jitter, and no
retry on a 4xx except 408 and 429. Retrying a `POST` on a timeout is how
duplicate orders happen: the client cannot tell a request the server never saw
from one it processed and failed to acknowledge, and only the idempotent
methods make that distinction not matter.

The generated client has a retry loop of its own that retries any method. Every
request the transport builds sets `retry: { maxAttempts: 1 }` to switch it off,
so exactly one policy governs. Jitter is not decoration: fifty clients that
lost the same connection retry on the same schedule without it and arrive
together.

The backoff clock is injected (`sleep`), as is the jitter source (`random`).
`manualClock()` moves only when a test says so. Nothing here waits on
wall-clock time.

### Deduplication, and what it shares

Ten components mounting `useOrderList()` in the same tick make **one** request.
The shared unit is the whole retry sequence, not an attempt: nobody is resolved
from a failed attempt while a neighbour waits for the retry, and a sequence
that terminally fails releases its slot so the next caller starts a fresh one
rather than adopting a dead promise.

An invalidation that lands *while* a request is out does not resolve from it.
The answer in progress was produced before the write it is supposed to reflect,
so it is thrown away and the sequence runs again — still one request at a time
per query, and everyone waiting stays on the same promise.

**Staleness is marked synchronously, at the invalidation, not at the batch.**
This is not a detail. The batch runs on the scheduler, which by default is a
microtask, and a request dispatched before the write can arrive inside that
gap and commit its pre-write answer. Worse, a query answered by a *placement*
callback never reaches a batch at all — and that path has no recovery, because
placement means no refetch is owed: a pre-write response overwriting the placed
skeleton deletes the created entity from the list permanently. So the
`Invalidator` reports every hit query through `onInvalidated` the moment it is
known to be behind, before placement is even attempted, and the cache marks its
in-flight request there.

The two outcomes differ. An ordinary invalidation **restarts**: throw the
answer away and run again. Placement **discards**: throw the answer away and
stop, because the application already supplied the value the refetch would have
produced, and spending a request to rediscover it is the cost the escape hatch
exists to avoid. The batch then skips any query that already has a request out,
since that request has either taken responsibility for the staleness or was
started after it.

### Auth: attach per scheme, refresh once per stampede

`AuthProvider.credentials(meta)` receives the operation, so a provider attaches
per the endpoint's declared scheme rather than blanketing every request with
the same header. On a 401 the transport takes a **single-flight** refresh: two
requests failing together produce one refresh and two retries, not two
refreshes. A request whose credentials predate a refresh that has already
landed retries against the new one without asking for another. Exactly one
retry — a 401 against a freshly refreshed credential is an authorization
answer, not a transient failure — and a refresh that fails surfaces the
original 401 rather than the token endpoint's complaint.

The refresh retry applies to every method, `POST` included. A 401 says the
server rejected the request before acting on it, which is what makes re-sending
safe in a way a timeout is not.

### The store is partitioned by principal

```ts
cache.setPrincipal(session?.userId);
```

A normalized store keys `Order:7` globally with no memory of who fetched it, so
without partitioning the next principal's `useOrder(7)` renders the previous
one's record. `setPrincipal` drops every entity, skeleton and registry entry on
a change, abandons every request in flight — a response for the identity that
went away is never committed — and re-mounts and refetches whatever is still
being watched. This is a correctness property, not a feature; it is the class
of defect document caches do not have.

### Bounded memory

A search box calling `useOrderList({q})` on every keystroke mints a distinct
query per keystroke. The cache caps *unwatched* records (`limit`, 128 by
default) and evicts least-recently-used, dropping the registry entry with them.
A watched query is never evicted, however old.

## Scripts

```
npm run build      tsc
npm test           vitest
npm run typecheck  tsc --noEmit over src and test
npm run size       build, then size-limit
```

The size budget is enforced from day one because an unenforced budget is a
sentence in a README. The whole core, REST-only, is budgeted at 9 kB gzipped.
Measured, minified, gzipped, tree-shaken to what an importer actually pulls:

| | limit | actual |
|---|---|---|
| entity store | 2 kB | **1.65 kB** |
| tag graph | 2.2 kB | **2.07 kB** |
| query engine and REST transport | 6 kB | **5.77 kB** |
| the whole entry point | 7 kB | **5.95 kB** |

## Known gaps, deliberately left to later chunks

- SSR revival. A skeleton serializes (references carry a `__ref` property) but
  a deserialized one is not recognised as a skeleton, because references are
  identified by object identity rather than by that property. Hydration needs a
  revive pass, so `dehydrate`/`hydrate` are not offered rather than offered
  half-working.
- A refetch returning identical data produces a *new* skeleton, so the root
  identity moves even though no record did. Every entity subtree beneath it
  keeps its identity, so a React tree re-renders the container and nothing
  under it, but the container is avoidable and is not yet avoided.
- Entity garbage collection. The query cache caps *queries* (see above), and
  dropping a query releases its tags, but an entity no live skeleton references
  is still held. `EntityStore#evict` is the operation that policy will drive.
- **Field renaming does not reach the hook path.** The transport drives
  `HTTPClient#request` directly, below the generated per-endpoint methods that
  set `bodyCodec`/`responseCodec`. Under `NamingPreserve` with no
  `FieldOverrides` — where no codec table is emitted at all — this is exactly
  equivalent. Otherwise a hook returns wire-cased fields while the direct
  client returns renamed ones from the same package, contradicting the
  generated types. Closing it needs the two codec ids on `OperationMeta`
  **and** the `entities` table renamed in the same change: `opsmanifest.go`
  emits `idField` and `fields` as verbatim wire names, so decoding a response
  to camelCase without renaming that table silently stops the normalizer
  finding ids — and a type whose id field is absent simply is not an entity, so
  nothing reports it.
- Optimistic overlays, WebSocket and SSE transports, stream binding, and the
  React adapter.
