# @forge-go/client-core

The runtime a generated Forge client delegates to. Generated output contains
types and one-line facades; everything a hook does lives here, so a runtime
defect is fixed by publishing this package rather than by regenerating every
repository that consumes a client.

Five chunks so far: the **normalized entity store**, the **tag graph** that
turns "this mutation invalidates `Order[]`" into "these three mounted queries
must refetch", the **query engine and REST transport** that a generated
`hooks.ts` binds to, **stream binding** — a ref-counted subscription manager
and a frame applier that routes a socket frame through the same path a
mutation response takes — and **optimistic overlays**, an ordered stack of
pending patches folded over the store on every read. Nothing here reaches the
network on its own: the HTTP client, the socket, the clock and the scheduler
are all injected.

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

## Stream binding

```ts
import { StreamBinder, SubscriptionManager } from '@forge-go/client-core';
import { ops, streams } from './ops'; // generated

const manager = new SubscriptionManager({
  connect: ({ endpoint }) => {
    const socket = new OrdersWebSocketClient({ baseURL });
    void socket.connect();

    return {
      onMessage: (handler) => socket.onMessage(handler),
      onClose: (handler) => socket.onClose(() => handler()),
      close: () => socket.disconnect(),
    };
  },
  principal: () => cache.owner,
});

const binder = new StreamBinder({ cache, streams, manager });

const release = binder.subscribe(ops.orderList); // this query is now live
```

The adapter is the whole integration: `websocket.ts` and `sse.ts` keep their
public surface, and nothing in this package imports `WebSocket` or
`EventSource`.

Constructing the binder also **registers it on the cache** as `cache.live`,
which is how `useQuery(op, args, {live: true})` finds it in every framework
adapter: they resolve a cache — from a per-call option, a provider, or the
module default — and call `cache.watchLive(op.meta, args)`, which is
`binder.subscribe` plus one diagnostic. The seam is declared in `cache.ts` as a
structural `LiveBinding` rather than imported from `live.ts`, so an adapter
calling it does not drag the streams layer into a REST-only bundle.

### Which envelope the frames arrive in

`decode` is injectable because the envelope is the server's business rather than
this package's — and Forge sends two of them.

The default `decodeFrame`, used above, reads the shape a plain Forge WebSocket
handler emits, where `type` **is** the message name:

```json
{"type": "order.created", "payload": {"id": 7}}
```

The streaming extension sends a different one. There `type` is the *transport
kind* — one of `message`, `presence`, `typing`, `system`, `join`, `leave`,
`error` — and the domain name lives in `event`:

```json
{"id": "m-1", "type": "message", "event": "order.created", "channel_id": "orders", "data": {"id": 7}}
```

Both readings are correct for their own envelope and neither can be made correct
for both, so a channel served by the streaming extension passes its decoder:

```ts
import { forgeStreamingDecoder } from '@forge-go/client-core';

const binder = new StreamBinder({ cache, streams, manager, decode: forgeStreamingDecoder() });
```

> **The default reads `event` first**, so a streaming channel's domain frames
> bind without any configuration. What `forgeStreamingDecoder` adds is the rest
> of the envelope: it knows `presence`, `typing` and `join` are transport kinds
> rather than message names, and drops them instead of reporting each one
> through `onUnknown` once per channel.

`forgeStreamingDecoder` is a superset of the default rather than an alternative
to it: it reads `event` first and keeps `type` as the fallback, so an
application serving both shapes installs it once instead of routing two decoders
by endpoint. Frames that carry only a reserved transport kind — presence,
typing, join — are dropped silently rather than reported, because no generated
manifest binds them and they are working exactly as designed.

Its one option, `channelOf`, matters only when several channels are multiplexed
over one socket *and* the same message name is bound on more than one of them.
The extension's `channel_id` is a logical subscription id (`orders`); a manifest
channel is the endpoint path (`/ws/orders`). Nothing but the application can map
between the two, so by default the id is left out entirely and a frame keeps the
channel it arrived on — a guess here is a lookup miss, and a lookup miss is the
silent failure above.

### Which channels a query subscribes to

Every channel with a binding whose `entity` is reachable from the query's
declared result type — its `ops[x].entity`, plus everything `entities[T].fields`
descends into, transitively. That is exactly the set of typenames `normalize`
can lift out of the response, which is to say exactly the entities that can
appear in its skeleton.

Root-only would be the smaller rule and is wrong in a way that is hard to see:
an order list rendering `order.customer.name` holds `Customer` records in its
skeleton, and a `customer.updated` frame changes what is on screen.

The rule reads the **manifest**, never the query's settled `deps`. Deps do not
exist until the first response lands, so a deps-based rule would be deaf during
exactly the window whose frames matter most — and deps follow the data, so a
second page of orders containing no `Invoice` would drop that channel and
re-acquire it on the page after.

### One code path, not two

A socket frame **is** a mutation the client did not initiate, so it commits
through the same normalizer, refreshes the same registry values and raises its
tags through the same `Invalidator` as a mutation response. `applyFrames` is
`mutate` without the request. Two apply paths is how one of them ends up
missing the fix the other got.

It is a **free function over the cache's public surface** rather than a method
on `QueryCache` — `applyFrames(cache, frames)`, exported from the streams
surface. That keeps it out of a REST-only bundle (`QueryCache` is pulled in
whole by anyone who imports it) and makes the streams surface explicit rather
than bolting a second top-level write method on beside `mutate`. The seams it
goes through — `cache.store`, `cache.entities`, `cache.notifyChanged()`,
`cache.invalidate()` — are the same ones `mutate` uses.

The manifest supplies the rest. `intent` is `upsert` (merge, plus whatever
membership the binding declares), `patch` (merge, and nothing else — an
`order.updated` costs zero requests and re-renders only the queries whose value
actually moved), or `evict`.

`evict` drops the record, leaves a tombstone, **and raises `${entity}[]`
whatever the binding declared.** The generator passes `Invalidates` through
verbatim rather than synthesizing one, so a delete binding can reach the client
declaring nothing at all — and a settled skeleton is never rewritten, so
without the synthesized tag nothing repairs the lists that still reference the
record. An eviction is an entity-level event, and an entity-level event
necessarily changes the membership of every collection that held it; that is
knowable here without the server saying so, and it is the same reasoning
`recover` applies on a reconnect. It is deliberately *not* done for `upsert` or
`patch` — synthesizing `Order[]` for `order.updated` would refetch every
mounted list on every update and destroy the property that makes a live query
worth having.

The rehydration closes the other half. A reference whose record is gone is a
**hole, not a value**: it is dropped from an array rather than pushed as
`undefined`, so `data.map(o => o.id)` cannot throw. That matters because the
subscriber is notified with the post-eviction value *synchronously*, before any
refetch the eviction triggered can land — a repair that arrives with the
refetch arrives too late. Only a reference is dropped; a literal `null` the
server actually sent is passed through untouched.

A message no binding claims is **ignored with a development warning**, never
thrown. The server deploys ahead of the client, always, and a client that falls
over on the first unrecognised message type turns every additive backend
release into an outage.

### The ordering guarantee

> **A committed write never overwrites an entity with a value older than a
> stream frame the client has already applied.**

It covers **both** kinds of write — a query response and a mutation response.
An earlier draft of this section said "a committed response", which read as
covering both while only the query path was actually stamped.

A frame arriving while a request for the same entity is out is a real race, and
whichever lands last on the wire is not necessarily whichever is newer. So:
each request records the store's frame clock at dispatch; each batch of frames
takes a fresh reading and stamps every record it writes; a response is
normalized *before* it is committed, and if any entity it carries has been
stamped by a frame since the request went out, that data does not commit.

This is checked against the response's own contents rather than against the tag
index, which is what makes it hold for the case tags cannot reach: an
**unmounted** query with a request in flight has no entry in the index, and no
invalidation-driven fix would ever see it.

**How the loser converges differs by path, and the difference is deliberate.**

- A **query** response is discarded and the request re-run. Re-reading is free
  and idempotent. One re-run per frame, not a retry loop — the re-run is
  dispatched *after* the frame, so it carries a stamp the frame cannot be newer
  than, and it commits whatever it returns. Every entity in the rejected
  response that the frame did *not* touch is committed before the re-run, so a
  re-run that then fails does not lose data the client already received.
- A **mutation** response is **never re-issued**. Retrying a write is the
  duplicate-orders hazard the retry policy is careful about: the client cannot
  distinguish a request the server never saw from one it processed. It commits
  around the raced keys instead — the entities the frame touched keep the
  frame's value, everything else lands — and the value handed back to the
  caller is read out of the store, so it is the current truth rather than the
  caller's own superseded write.

  **Unless the frame that won was a delete**, in which case there is no current
  truth to read: the record is gone, so rehydrating would hand the caller
  `undefined` typed as `T`. A raced key the store no longer holds is treated as
  the delete it is — the caller gets what the server actually said, and
  placement is **declined** for that mutation. Declining is not caution: `adopt`
  re-normalizes a placement result straight back into the store with no stamp
  and no skip, so placing it would resurrect the deleted entity. `undefined`
  from placement already means "refetch instead", and the eviction frame
  synthesized `${entity}[]` on its way through, so the lists are already being
  refetched.

A `frameRestarts` bound (3 by default) caps the query re-runs, for a channel
busy enough that a frame lands inside every request window; past it the query
falls back to the mutation rule.

What is deliberately **not** claimed: the guarantee is **per entity, not per
field** — a raced record is skipped whole, and merging a stale response's other
fields into a frame-written record would need field-level stamps. Two frames on
one channel apply in arrival order, because the transport is ordered and there
is no server clock to do better with; frames on two different channels have no
defined order relative to each other, for the same reason.

### Gap recovery

A dropped socket means missed frames, and a reconnected client looks correct
while being wrong. Without recovery, staleness after a closed laptop lid
presents as "it just stops updating" and is unfalsifiable from the outside.

On reconnect — and on an identity change, which is the same thing from the
store's point of view — the binder does both halves, because neither subsumes
the other:

- **Invalidates every tag bound to the channel**, which catches everything
  whose membership may have moved, including a filtered list on another screen
  that holds no subscription of its own.
- **Refetches the registered live queries.** A channel that only emits
  `order.updated` declares no invalidations at all, by design, so there is no
  tag to raise and the missed patches would stay missed for the session.

The invalidation runs first, so a query refetched below has already been marked
stale and the batch skips it rather than spending a second request.

### Ref counting, and the phantom unmount

One socket per `(endpoint, principal)`, multiplexed by channel, closed on the
last release — but **not synchronously**. React development double-invokes
effects, so every subscriber is torn down and put back before anything else
runs; closing on the count reaching zero opens a second socket on every mount
and, worse, can leave a remount racing the close so the live query silently
stops. It reproduces only in development, so it gets reported as "works in
prod". The close is deferred by one turn through an injected scheduler, which
makes the phantom unmount free and the behaviour testable.

Reconnect backoff is exponential with jitter, taken through an injected
`Sleep`; any traffic at all resets the ladder. A socket opened for a principal
that has since changed is never adopted, and `repartition` swaps it for one
opened under the current identity while keeping its subscribers.

### Write batching

Frames coalesce into **one store commit per animation frame** — a channel at 200
msg/s is not 200 renders. The scheduler is injected (`animationFrameScheduler`
by default, microtask where there is no `requestAnimationFrame`), so the
coalescing window is driven by the test rather than waited on.

## Optimistic overlays

```ts
update.mutate({ path: { id: 7 }, body: { status: 'shipped' } },
              { optimistic: { status: 'shipped' } });
```

**An overlay is never applied to the base store.** What a subscriber sees is
`fold(base, patches in push order)`, recomputed on demand — which is what
makes rollback the removal of an entry rather than the application of an
inverse. An inverse is the thing that goes wrong under concurrency: the one
recorded for a second pending mutation would have been computed against a
base that already included the first, so when the first fails it would
restore a state that never existed. There is nothing recorded here to get
wrong, because there is nothing recorded — dropping the first of two pending
mutations re-applies the second to the reverted base, correctly, rather than
undoing anything.

The target a patch is checked against is derived, not declared, from what the
mutation already invalidates. `PATCH /orders/{id}` carries `Order:{id}`
through derived same-entity invalidation, and resolving that template against
the call's own arguments produces `Order:7` — which *is* the entity key. So
update and delete need nothing from the caller. A create, which invalidates
only `Order[]` and names no key, mints a temp key from a stack counter
(`Order:~opt1`). A mutation whose tags name two entities is ambiguous, and
that is reported through `onError` and skipped, never thrown — throwing would
reject the mutation before it was dispatched, and `mutate` swallows rejections
by design, so the write would silently not happen, which is a worse failure
than not being optimistic.

What a caller writes is a patch, never a value:

```ts
optimistic: (o) => ({ likes: o.likes + 1 })   // re-run on every refold -- composes
optimistic: { likes: order.likes + 1 }        // captured at call time -- does not
```

A literal captured at call time is a value computed against whatever the base
happened to be at that instant, so replaying it on a different base replays
the wrong number. A function is re-run on every refold against the base the
refold is actually standing on, which is what lets two concurrent `+1`s show
2, and dropping the first show 1 rather than 0.

On success, `mutate` **takes** the overlay off the stack before promoting it
into base, and only then commits the response over that. Taking first is what
stops a computed merge being applied twice — once by the promoting write and
once by a fold that still contains the overlay. Promotion is what stops a
`204 No Content` delete flashing the row back between settle and refetch:
without it the overlay is gone and the response carries nothing to replace
it, so the row reappears for one frame before the refetch removes it again.
Promoted delete targets go into the same `skip` set a raced stream frame
uses, so a body-returning `DELETE` that echoes the deleted entity cannot
resurrect it either. On failure the overlay is simply dropped — base was
never touched, so nothing is owed: no tag is raised and no refetch is
scheduled.

A `merge` over a base record that does not exist is a no-op, not a
resurrection. That single rule is what lets an evicting stream frame beat a
pending local edit with no special case: the row is gone, and a patch to
something that is gone patches nothing.

`QueryState.isOptimistic` is computed once, in `QueryCache.snapshot`, as
whether any live overlay reaches the query's tags or dependencies — with an
early-out to `false` on an empty stack, so an application that never opts in
pays nothing to check it. The exported `OPTIMISTIC` symbol is the row-level
answer: stamped on a materialized record any overlay touches, so one row in a
list of fifty can dim itself while the other forty-nine render normally.
Symbol-keyed, so it is invisible to `Object.keys`, `JSON.stringify`, and the
deep-equality `equal()` uses — rendering it never serializes a cache internal,
and its presence or absence never registers as a change to a comparison that
walks string keys. A spread (`{...order}`) is the one place it survives: JS
copies own-enumerable symbols along with everything else, so a component that
clones a record before editing it locally carries the marker forward onto the
copy too. That is the right outcome, not a leak — the clone really did come
from an overlaid record, and losing the marker on it would be the surprising
behaviour.

## Scripts

```
npm run build      tsc
npm test           vitest
npm run typecheck  tsc --noEmit over src and test
npm run size       build, then size-limit
```

The size budget is enforced from day one because an unenforced budget is a
sentence in a README. The design budgets 9 kB gzipped REST-only and 14 kB with
streams. Measured, minified, gzipped, tree-shaken to what an importer actually
pulls:

| | limit | actual |
|---|---|---|
| entity store | 2.17 kB | **2.07 kB** |
| tag graph | 2.2 kB | **2.12 kB** |
| query engine and REST transport | 8.25 kB | **8.15 kB** |
| stream binding | 3.4 kB | **3.39 kB** |
| optimistic overlays | 1.23 kB | **1.13 kB** |
| core, REST only | 9 kB | **8.19 kB** |
| core with streams | 14 kB | **11.05 kB** |

Streams cost 2.86 kB on top of REST-only.

REST-only itself went from 5.95 kB to **6.39 kB** when stream binding landed.
Making `applyFrames` a free function over the cache's public surface recovered
0.19 kB of what an earlier draft spent by hanging it off `QueryCache`. The
rest — the frame clock, the per-record stamps, the staged-commit split and
`racedSince` — is genuinely on the query path and cannot be moved, because
`mutate` is stamped too. That is the honest residue, and it buys the ordering
guarantee for the writes an application makes as well as for the ones it
reads.

The `stream binding` sub-budget moved from 3 kB to 3.4 kB in the same change,
because the apply logic moved *into* that surface from the query engine.

Optimistic overlays paid for themselves the same way, in a different sub-bucket.
`mutate` references the overlay stack statically, so `overlay.ts` lands in the
REST-only budget whether or not an application ever passes an `optimistic`
option — that is the trade described above: installable would be DX tax paid
for bytes. Two sub-budgets moved to make room for it: `entity store` from
2 kB to 2.17 kB, for the `OPTIMISTIC` stamp and the `touch` seam `store.ts`
opens for the overlay layer, and `query engine and REST transport` from
6.5 kB to 8.25 kB, for the stack itself, target derivation and the fold.
`optimistic overlays` gets its own size-limit line — `OverlayStack` measured
on its own — for the same reason `stream binding` did: so the cost is visible
in the line that caused it rather than absorbed into a neighbour.

**The two budgets the design actually sets — 9 kB and 14 kB — are unchanged
and are still what the total is held to**, both after streams and after
overlays: 8.19 kB and 11.05 kB measured, against those same two limits, with
0.81 kB and 2.95 kB of headroom respectively. Two internal lines moved so the
two numbers an application actually depends on did not have to.

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
- **WebTransport binding.** `SubscriptionManager` takes any `StreamConnection`,
  so a WebTransport adapter is the same four-line object literal as the
  WebSocket one, but none is written or tested here.
- **A frame's ordering is per entity, not per field.** A response rejected for
  carrying a frame-stamped entity is rejected whole; past the restart bound it
  commits with that entity skipped whole. Merging a stale response's *other*
  fields into a frame-written record would need field-level stamps, which is a
  larger claim than the wire supports.
- **An evicted tombstone can resurrect a dead entity.** Tombstones are capped at
  256 (see above). Delete `Order:7`, then delete 256 further *cached* entities,
  and `Order:7`'s stamp is pushed out — at which point a response dispatched
  before the delete and still in flight puts the row back. The cap makes this
  **improbable, not impossible**: a tombstone is only read by a request that
  straddles the delete, so 256 is three orders of magnitude more than that
  window needs, and that is what makes it survivable rather than prevented.
  Only reachable where the tag path cannot help — an unmounted query, a
  prefetch, an SSR pass with a request outstanding. With the query mounted, the
  synthesized `Order[]` restarts the request and nothing resurrects. Raising the
  cap moves the boundary without removing it; the fix is a real GC policy that
  drops a tombstone once no request predating it is still in flight, which needs
  the cache to tell the store its oldest live dispatch stamp.
- **Multiplexed channels are matched by message name.** Where several channels
  share one socket and the envelope carries no `channel` field, a message name
  bound on two of them applies for both. A decoder that reads a channel out of
  the envelope narrows it; the default one does when the field is present.
