# @forge-go/client-core

The runtime a generated Forge client delegates to. Generated output contains
types and one-line facades; everything a hook does lives here, so a runtime
defect is fixed by publishing this package rather than by regenerating every
repository that consumes a client.

Five chunks so far: the **normalized entity store**, the **tag graph** that
turns "this mutation invalidates `Order[]`" into "these three mounted queries
must refetch", the **query engine and REST transport** that a generated
`hooks.ts` binds to, **stream binding** (a ref-counted subscription manager and
a frame applier that routes a socket frame through the same path a mutation
response takes), and **optimistic overlays**, an ordered stack of pending
patches folded over the store on every read. Nothing here reaches the network on
its own: the HTTP client, the socket, the clock and the scheduler are all
injected.

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

- `normalize(value, entities, rootType?)`: pure. Returns `{skeleton, records, deps}`.
- `EntityStore#write(value, entities, rootType?)`: normalize and commit.
- `denormalize(skeleton, store)`: rebuild, with structural sharing.
- `EntityStore#dependencies(skeleton)`: the entity keys a skeleton reaches.

### Caller contract: nothing here is copied defensively

A subtree containing no entity is never copied. The skeleton holds the
response's own object, and `denormalize` hands that same object back, which is
exactly what makes identity stable across reads, and is why the response you
passed to `write`, the skeleton, and the denormalized result can all be the same
object.

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

- `rootType` names the type of the response: `ops.orderGet.rootType`, resolved
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
`Order:7`. That is deliberate: a Go type's identity field has one type, fixed at
generation time, so one typename cannot legitimately produce both. A server that
returns `7` from one endpoint and `"7"` from another is describing the same
record. Keying them apart would split one entity into two entries that never
converge.

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
resolves to a named schema: through a direct `$ref`, an array whose items are a
`$ref` (the *element* name, since a typename passes through an array unchanged),
or a `oneOf`/`anyOf`/`allOf` wrapper naming exactly one type, which is how a
nullable reference is spelled. It is omitted when a type has no such property.

**An edge is only recorded when an entity is REACHABLE through it.** The walk's
only use for one is `schema[type]`, so an edge to a type with nothing worth
descending to buys nothing: an enum, a plain value struct. But a named
non-entity with an entity beneath it does get a row, carrying `fields` and no
`idField`:

- A named **non-entity** hop no longer breaks the chain. `Order → Shipment →
  Carrier`, where `Shipment` has no identity field, keeps the `Order.shipment`
  edge and gives `Shipment` its own row, so the `Carrier` below is lifted out.
- An **envelope**, `{items: [...], total: n}`, gets a row too, so a paginated
  response normalizes. `ops.orderList.rootType` names the wrapper while
  `ops.orderList.entity` names what it carries; the two are separate fields
  precisely because passing the entity name as the root reads `Order`'s edges
  against the envelope's properties and descends into nothing.

A row with **no `idField`** means "walk me, never store me". Types that carry
one are only reachable through this table, never keyed into the store. This
replaced an earlier workaround, an `idField` no payload was expected to carry,
which worked only for as long as no payload carried that property.

Only types some **root** reaches get a row: the entities, and the endpoints'
response root types. A wrapper mentioned solely by a request body is never
walked into, so it stays out of a file CI byte-diffs.

An envelope's **cache contract** is a separate question from routing, and is not
inferred. `PageOrder{items: [Order], total}` and `OrderReport{topOrders:
[Order], generatedAt}` are the same shape, and only one of them is the
collection, so `provides: ['Order[]']` requires an explicit `x-forge-envelope`
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
stays flat and JSON-serializable either way: each record is acyclic, and only
the graph *between* records closes.

## Structural sharing

`useSyncExternalStore` tears when `getSnapshot` returns a fresh object for
unchanged state, so referential stability is a correctness requirement here,
not an optimization.

Four mechanisms, together:

1. **Nothing unchanged is rewritten.** `normalize` returns the input subtree by
   reference when no entity occurs beneath it, so most of a response is already
   its own skeleton and rehydrating it is the identity function.
2. **Rehydration is memoized per subtree**, keyed by entity key for records and
   by node identity for skeleton containers.
3. **Invalidation walks the reverse-dependency graph** from the key that was
   written, so a write touches only the memos that actually reach it, work
   proportional to what changed, not to the size of the store. A one-hop
   version check would miss a change two hops away; this does not.
4. **A refetch reuses the containers it did not change.** The first three
   mechanisms all key off the skeleton, and a refetch builds a *second* one, so
   a poll returning identical bytes used to hand back a new root with every
   record under it unchanged. `read` takes the previous value and compares each
   rebuilt container against it. The comparison is shallow and bottom-up:
   by the time a container is checked its children have already settled their
   own identity, so `===` per child is the whole answer and a read stays
   proportional to the skeleton rather than to the response. The one deep
   comparison is for a subtree with no entity beneath it, which has no rebuilt
   children to compare, and it is the same `equal` a write already runs.

A write whose data is deep-equal to what is already stored is not a write: the
version does not move, the record object is kept, and no identity downstream of
it changes. A poll that returns the same bytes must not re-render anything. The
comparison tracks the *route* it took rather than every object it has seen. An
object reached twice through different fields is a DAG, not a cycle, and
treating the second encounter as already-equal would drop a real change without
ever looking at what it was compared against.

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
searches the response. A bare `{x}` searches path, query, body, response. First
match wins, where a match is the first source that has the property *at all*: a
body holding `customerId: null` has answered, and falling through to the
response would invalidate a different customer's list on a value nobody
supplied.

`resolveTag` returns `undefined` rather than a partially substituted string. A
tag that silently becomes `Customer:` matches no query, fires nothing, and
reports nothing, the failure the design forbids. At runtime the `Invalidator`
**skips that one tag and reports it** through `onUnresolved` (which warns once
per template by default), keeping every other tag in the same list. Throwing was
the alternative and is worse: the write has already committed on the server, and
turning a cache defect into an application-visible error for completed work
trades a stale row for a broken screen. Generation is where an unresolvable
template is supposed to fail; this is the runtime's report that one got past it.

### One structure, not two

The registry and the tag index are one class, because every mount, unmount and
settle touches both. A path that updates one and not the other leaks (a bucket
holding a query nobody watches, refetching forever) or silently stops a query
updating. The index is private and has no mutator of its own.

The index holds **mounted queries only**. The last unmount removes the entry
from every bucket and deletes the buckets it emptied. An invalidation arriving
while a query is unmounted is still observed, through a clock rather than
through a retained index entry: each invalidation stamps a counter onto the tags
it touched, each settle stamps it onto the query, and a query mounting with a
newer tag stamp than its own missed something.

A stamp is written **only for a tag some remembered query already carries**, and
deleted when the last carrier is forgotten. An earlier version stamped every tag
and pruned nothing, on the claim that the stamps were bounded by the API's tag
vocabulary. They were not: `settle` folds a query's entity dependencies into its
tag set, so `Order:7`, `Order:8`, … are all tags, and every distinct entity ever
invalidated left a permanent entry, a bound of "every entity the application has
ever touched".

Restricting the write loses nothing. A query acquires a tag in exactly two
places, `mount` and `settle`, and both set `settledAt` to the current clock. A
stamp written at reading *c* while nothing carried the tag can therefore only
ever be compared against a `settledAt` of *c* or later, and the staleness test
asks for *strictly* newer: the stamp is dead the moment it is written. The same
argument licenses deleting one when its last carrier is dropped.

### Coalescing

Invalidated queries go into a set, and one flush is scheduled per tick. N
queries hit in one tick is one batch; one query hit by two tags is one refetch.
The scheduler is injectable and defaults to a microtask, never a timer, since a
delay that is "long enough" on a laptop is a flake on CI and a visible pause on
a phone. `manualScheduler()` runs nothing until asked, which is how every
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

Returning a list skips that query's refetch; returning `undefined` falls back to
it. The runtime never reasons about whether an entity belongs in a filtered or
paginated window (that is the Relay connection-directive tarpit), and the
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

Bindings run at module scope, before an application has constructed anything, so
they resolve their cache when they are *called*. `configureClient` sets the
default; every entry point also takes an explicit `client` for the cases where a
global is the wrong answer: SSR, tests, two backends.

A query binding takes no per-call `headers` or `signal`, and a mutation binding
takes both. That asymmetry is the point: a query is *shared*. Ten subscribers
with the same arguments are one record and one request, keyed by the arguments
alone, so a per-call header would belong to whichever caller happened to create
the record and be silently dropped for the rest, and one subscriber's abort
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
so it is thrown away and the sequence runs again, still one request at a time
per query, and everyone waiting stays on the same promise.

**Staleness is marked synchronously, at the invalidation, not at the batch.**
This is not a detail. The batch runs on the scheduler, which by default is a
microtask, and a request dispatched before the write can arrive inside that gap
and commit its pre-write answer. Worse, a query answered by a *placement*
callback never reaches a batch at all, and that path has no recovery, because
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
per the endpoint's declared scheme rather than blanketing every request with the
same header. On a 401 the transport takes a **single-flight** refresh: two
requests failing together produce one refresh and two retries, not two
refreshes. A request whose credentials predate a refresh that has already landed
retries against the new one without asking for another. Exactly one retry (a 401
against a freshly refreshed credential is an authorization answer, not a
transient failure), and a refresh that fails surfaces the original 401 rather
than the token endpoint's complaint.

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
a change, abandons every request in flight (a response for the identity that
went away is never committed), and re-mounts and refetches whatever is still
being watched. This is a correctness property, not a feature; it is the class of
defect document caches do not have.

### Bounded memory

A search box calling `useOrderList({q})` on every keystroke mints a distinct
query per keystroke. The cache caps *unwatched* records (`limit`, 128 by
default) and evicts least-recently-used, dropping the registry entry with them.
A watched query is never evicted, however old.

Reaping a query is also the moment its entities may have become unreachable,
so the sweep hangs off it. `collect()` walks the skeletons of every query still
cached plus every key a pending overlay holds, and evicts the records nothing
in that set reaches. Reachability is recomputed rather than refcounted: a count
would have to be maintained at every write, eviction, frame and rollback, and
the one place it went wrong would free a record something still points at. The
walk is proportional to what survives and runs only when a query was actually
reaped, so an application that is not churning queries never pays for it. Call
it yourself when you know a screen is finished with.

Tombstones are bounded the same way, by when they stop mattering rather than by
a cap. A tombstone is only ever read by a response that was dispatched before
the delete and has not arrived yet, so the cache tells the store the frame
reading of the oldest request still outstanding and every stamp at or below it
is dropped. `TOMBSTONE_LIMIT` is still there as a backstop, but reaching it now
takes a session deleting faster than its requests complete. The registration
survives until a response has *committed*, not until it arrives: `racedSince`
is the reader the tombstone exists for, and retiring the dispatch any earlier
would let a response expire the very stamp it is about to consult, undoing the
delete it straddles by its own arrival.

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

WebTransport is the one adapter you do not have to write, because it is not a
four-line literal. Datagrams arrive as a `ReadableStream` rather than a
callback, so the adapter is a pull loop with a reader to release and a decode
that can fail one packet at a time. `webTransportConnection` is that loop:

```ts
import { webTransportConnection } from '@forge-go/client-core';

const manager = new SubscriptionManager({
  connect: ({ endpoint }) => webTransportConnection(new WebTransport(baseURL + endpoint)),
  principal: () => cache.owner,
});
```

You pass a session that is already constructed, so this package still opens
nothing. There is no need to await `ready` first: datagrams do not arrive until
the handshake finishes, and a handshake that fails rejects `closed`, which the
adapter reports and then treats as a drop like any other. Datagrams are UTF-8
JSON by default. Pass `parse` for a binary envelope, and a datagram it throws
on is reported through `onError` and skipped rather than ending the
subscription.

Constructing the binder also **registers it on the cache** as `cache.live`,
which is how `useQuery(op, args, {live: true})` finds it in every framework
adapter: they resolve a cache (from a per-call option, a provider, or the module
default) and call `cache.watchLive(op.meta, args)`, which is `binder.subscribe`
plus one diagnostic. The seam is declared in `cache.ts` as a structural
`LiveBinding` rather than imported from `live.ts`, so an adapter calling it does
not drag the streams layer into a REST-only bundle.

### Which envelope the frames arrive in

`decode` is injectable because the envelope is the server's business rather than
this package's, and Forge sends two of them.

The default `decodeFrame`, used above, reads the shape a plain Forge WebSocket
handler emits, where `type` **is** the message name:

```json
{"type": "order.created", "payload": {"id": 7}}
```

The streaming extension sends a different one. There `type` is the *transport
kind* (one of `message`, `presence`, `typing`, `system`, `join`, `leave`,
`error`), and the domain name lives in `event`:

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
by endpoint. Frames that carry only a reserved transport kind (presence, typing,
join) are dropped silently rather than reported, because no generated manifest
binds them and they are working exactly as designed.

Its one option, `channelOf`, matters only when several channels are multiplexed
over one socket *and* the same message name is bound on more than one of them.
The extension's `channel_id` is a logical subscription id (`orders`); a manifest
channel is the endpoint path (`/ws/orders`). Nothing but the application can map
between the two, so by default the id is left out entirely and a frame keeps the
channel it arrived on. A guess here is a lookup miss, and a lookup miss is the
silent failure above. A literal `channel` field is a different matter: it is
already an endpoint path, so it is passed through as the frame's channel with or
without `channelOf`, exactly as the default decoder does, and it never goes
through the mapping. That is what makes the superset claim true of the channel
and not only of the name.

### Which channels a query subscribes to

Every channel with a binding whose `entity` is reachable from the query's
declared result type: its `ops[x].entity`, plus everything `entities[T].fields`
descends into, transitively. That is exactly the set of typenames `normalize`
can lift out of the response, which is to say exactly the entities that can
appear in its skeleton.

Root-only would be the smaller rule and is wrong in a way that is hard to see:
an order list rendering `order.customer.name` holds `Customer` records in its
skeleton, and a `customer.updated` frame changes what is on screen.

The rule reads the **manifest**, never the query's settled `deps`. Deps do not
exist until the first response lands, so a deps-based rule would be deaf during
exactly the window whose frames matter most, and deps follow the data, so a
second page of orders containing no `Invoice` would drop that channel and
re-acquire it on the page after.

### One code path, not two

A socket frame **is** a mutation the client did not initiate, so it commits
through the same normalizer, refreshes the same registry values and raises its
tags through the same `Invalidator` as a mutation response. `applyFrames` is
`mutate` without the request. Two apply paths is how one of them ends up
missing the fix the other got.

It is a **free function over the cache's public surface** rather than a method
on `QueryCache`: `applyFrames(cache, frames)`, exported from the streams
surface. That keeps it out of a REST-only bundle (`QueryCache` is pulled in
whole by anyone who imports it) and makes the streams surface explicit rather
than bolting a second top-level write method on beside `mutate`. The seams it
goes through (`cache.store`, `cache.entities`, `cache.notifyChanged()`,
`cache.invalidate()`) are the same ones `mutate` uses.

The manifest supplies the rest. `intent` is `upsert` (merge, plus whatever
membership the binding declares), `patch` (merge, and nothing else: an
`order.updated` costs zero requests and re-renders only the queries whose value
actually moved), or `evict`.

`evict` drops the record, leaves a tombstone, **and raises `${entity}[]`
whatever the binding declared.** The generator passes `Invalidates` through
verbatim rather than synthesizing one, so a delete binding can reach the client
declaring nothing at all, and a settled skeleton is never rewritten, so without
the synthesized tag nothing repairs the lists that still reference the record.
An eviction is an entity-level event, and an entity-level event necessarily
changes the membership of every collection that held it; that is knowable here
without the server saying so, and it is the same reasoning `recover` applies on
a reconnect. It is deliberately *not* done for `upsert` or `patch`: synthesizing
`Order[]` for `order.updated` would refetch every mounted list on every update
and destroy the property that makes a live query worth having.

The rehydration closes the other half. A reference whose record is gone is a
**hole, not a value**: it is dropped from an array rather than pushed as
`undefined`, so `data.map(o => o.id)` cannot throw. That matters because the
subscriber is notified with the post-eviction value *synchronously*, before any
refetch the eviction triggered can land. A repair that arrives with the refetch
arrives too late. Only a reference is dropped; a literal `null` the server
actually sent is passed through untouched.

A message no binding claims is **ignored with a development warning**, never
thrown. The server deploys ahead of the client, always, and a client that falls
over on the first unrecognised message type turns every additive backend
release into an outage.

### The ordering guarantee

> **A committed write never overwrites an entity with a value older than a
> stream frame the client has already applied.**

It covers **both** kinds of write: a query response and a mutation response. An
earlier draft of this section said "a committed response", which read as
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
  and idempotent. One re-run per frame, not a retry loop: the re-run is
  dispatched *after* the frame, so it carries a stamp the frame cannot be newer
  than, and it commits whatever it returns. Every entity in the rejected
  response that the frame did *not* touch is committed before the re-run, so a
  re-run that then fails does not lose data the client already received.
- A **mutation** response is **never re-issued**. Retrying a write is the
  duplicate-orders hazard the retry policy is careful about: the client cannot
  distinguish a request the server never saw from one it processed. It commits
  around the raced keys instead (the entities the frame touched keep the
  frame's value, everything else lands), and the value handed back to the
  caller is read out of the store, so it is the current truth rather than the
  caller's own superseded write.

  **Unless the frame that won was a delete**, in which case there is no current
  truth to read: the record is gone, so rehydrating would hand the caller
  `undefined` typed as `T`. A raced key the store no longer holds is treated as
  the delete it is: the caller gets what the server actually said, and placement
  is **declined** for that mutation. Declining is not caution: `adopt`
  re-normalizes a placement result straight back into the store with no stamp
  and no skip, so placing it would resurrect the deleted entity. `undefined`
  from placement already means "refetch instead", and the eviction frame
  synthesized `${entity}[]` on its way through, so the lists are already being
  refetched.

A `frameRestarts` bound (3 by default) caps the query re-runs, for a channel
busy enough that a frame lands inside every request window; past it the query
falls back to the mutation rule.

What is deliberately **not** claimed: the guarantee is **per entity, not per
field**. A raced record is skipped whole, and merging a stale response's other
fields into a frame-written record would need field-level stamps. Two frames on
one channel apply in arrival order, because the transport is ordered and there
is no server clock to do better with; frames on two different channels have no
defined order relative to each other, for the same reason.

### Gap recovery

A dropped socket means missed frames, and a reconnected client looks correct
while being wrong. Without recovery, staleness after a closed laptop lid
presents as "it just stops updating" and is unfalsifiable from the outside.

On reconnect (and on an identity change, which is the same thing from the
store's point of view) the binder does both halves, because neither subsumes the
other:

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
last release, but **not synchronously**. React development double-invokes
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

Frames coalesce into **one store commit per animation frame**. A channel at 200
msg/s is not 200 renders. The scheduler is injected (`animationFrameScheduler`
by default, microtask where there is no `requestAnimationFrame`), so the
coalescing window is driven by the test rather than waited on.

## Optimistic overlays

```ts
update.mutate({ path: { id: 7 }, body: { status: 'shipped' } },
              { optimistic: { status: 'shipped' } });
```

**An overlay is never applied to the base store.** What a subscriber sees is
`fold(base, patches in push order)`, recomputed on demand, which is what makes
rollback the removal of an entry rather than the application of an inverse. An
inverse is the thing that goes wrong under concurrency: the one recorded for a
second pending mutation would have been computed against a base that already
included the first, so when the first fails it would restore a state that never
existed. There is nothing recorded here to get wrong, because there is nothing
recorded. Dropping the first of two pending mutations re-applies the second to
the reverted base, correctly, rather than undoing anything.

The target a patch is checked against is derived, not declared, from what the
mutation already invalidates. `PATCH /orders/{id}` carries `Order:{id}` through
derived same-entity invalidation, and resolving that template against the call's
own arguments produces `Order:7`, which *is* the entity key. So update and
delete need nothing from the caller. A create, which invalidates only `Order[]`
and names no key, mints a temp key from a stack counter (`Order:~opt1`). A
mutation whose tags name two entities is ambiguous, and that is reported through
`onError` and skipped, never thrown. Throwing would reject the mutation before
it was dispatched, and `mutate` swallows rejections by design, so the write
would silently not happen, which is a worse failure than not being optimistic.

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
stops a computed merge being applied twice: once by the promoting write and once
by a fold that still contains the overlay. Promotion is what stops a
`204 No Content` delete flashing the row back between settle and refetch:
without it the overlay is gone and the response carries nothing to replace it,
so the row reappears for one frame before the refetch removes it again. Promoted
delete targets go into the same `skip` set a raced stream frame uses, so a
body-returning `DELETE` that echoes the deleted entity cannot resurrect it
either. On failure the overlay is simply dropped. Base was never touched, so
nothing is owed: no tag is raised and no refetch is scheduled.

A `merge` over a base record that does not exist is a no-op, not a
resurrection. That single rule is what lets an evicting stream frame beat a
pending local edit with no special case: the row is gone, and a patch to
something that is gone patches nothing.

`QueryState.isOptimistic` is computed once, in `QueryCache.snapshot`, as whether
any live overlay reaches the query's tags or dependencies, with an early-out to
`false` on an empty stack, so an application that never opts in pays nothing to
check it. The exported `OPTIMISTIC` symbol is the row-level answer: stamped on a
materialized record any overlay touches, so one row in a list of fifty can dim
itself while the other forty-nine render normally. Symbol-keyed, so it is
invisible to `Object.keys`, `JSON.stringify`, and the deep-equality `equal()`
uses: rendering it never serializes a cache internal, and its presence or
absence never registers as a change to a comparison that walks string keys. A
spread (`{...order}`) is the one place it survives: JS copies own-enumerable
symbols along with everything else, so a component that clones a record before
editing it locally carries the marker forward onto the copy too. That is the
right outcome, not a leak: the clone really did come from an overlaid record,
and losing the marker on it would be the surprising behaviour.

## Server rendering

```ts
import { dehydrate, hydrate } from '@forge-go/client-core';

// server, one cache per request
cache.setPrincipal(session.userId);
await cache.fetch(ops.orderList);
const state = dehydrate(cache, { principal: session.userId });

// client
hydrate(cache, state, { ops });
```

**What may be in the payload is a property of how it is built, not a rule
anybody has to remember.** `dehydrate` never reads `store.keys()`. It walks the
skeletons of the queries being exported, collects the references it finds,
walks those records, and repeats to a fixpoint. Whatever the walk reaches is
emitted and nothing else is, so an entity no exported query references cannot
appear in an HTML response even when the cache is shared between concurrent
server requests. `include` narrows the set further, and a key the cache does
not hold throws rather than silently exporting nothing.

Both ends assert the principal. `dehydrate` refuses to serialize for anyone but
the cache's current owner, and `hydrate` refuses a payload built for anybody
else, which is what a payload cached at a CDN and served to the wrong session
runs into. The principal has to be a scalar: `setPrincipal` compares with
`===`, so an object identity already re-clears the cache on every call that
mints a fresh one.

### Boundaries, in all three adapters

`hydrateBoundary` is the policy every framework needs around `hydrate`: walk a
payload at most once per cache, and decide which refusals a page survives. A
`version` or `operation` refusal is reported and the queries just fetch. A
`principal` refusal, or anything unrecognised, is rethrown, because that one
says something is wrong with *whose* data this is and fetching does not repair
it. It lives here rather than in each adapter, since three copies of a security
posture are three postures waiting to drift apart.

React and Vue wrap it in a component that hydrates during render, before its
children read anything, and renders no element of its own:

```tsx
<HydrationBoundary state={state} ops={ops}>
  <Orders />
</HydrationBoundary>
```

Angular gets a provider instead, and its content model is why. A component
wrapping `<ng-content>` does not own its children: projected content is
instantiated by the parent template, so `injectQuery` in a child has already
run by the time the wrapper's constructor does. An environment initializer runs
when the injector is created, before any component in it exists, which is the
same guarantee stated in Angular's own vocabulary:

```ts
bootstrapApplication(App, {
  providers: [provideClient(cache), provideHydration(state, ops)],
});
```

Pass `stale` in any of the three to settle the hydrated queries behind the
server, so a mount paints instantly and then refetches. That is what a
statically generated or ISR page wants; a dynamically rendered one does not,
because the server fetched the data milliseconds ago.

### Streaming a payload in chunks

A non-streamed render serializes once, at the end. A streamed one cannot: the
point of streaming is to send each result as it lands, and calling `dehydrate`
per boundary re-sends the whole cache every time, so a page with ten boundaries
ships its first query's records ten times over.

`streamingDehydrator` remembers what it has already emitted and gives you the
difference:

```ts
const stream = streamingDehydrator(cache, { principal: session.userId });

// each time a boundary resolves
const chunk = stream.flush();
if (chunk !== undefined) write(`<script>__FORGE__.push(${JSON.stringify(chunk)})</script>`);
```

Hand each chunk to `hydrate` in arrival order on the client. `hydrate` merges
rather than replaces, so a chunk whose skeleton references a record an earlier
chunk carried resolves against what is already in the store, and applying every
chunk in order lands on exactly the cache one non-streamed payload would have
produced. A record is re-sent when its version moves, so a value that changed
between two flushes is corrected rather than left stale. `flush` returns
`undefined` when nothing settled, and asserts the principal every time rather
than once, because a render whose identity changes half way through is worth
catching at the flush that would have leaked.

### The `__ref` collision, which is the whole difficulty

A `Ref` is `Object.freeze({__ref: key})` (one own key, a string value), and a
response can legitimately contain an object of exactly that shape. `normalize`
leaves such an object inline, so it reaches the store as ordinary record data.
After `JSON.parse` the two are indistinguishable, and a revive pass that treated
every `{__ref: string}` as a reference would mint one from your data and
rehydrate it to `undefined`. That is precisely the lossy round trip `ref.ts`
refuses, arrived at from the other direction.

So `wire.ts` escapes on the way out and unescapes on the way in. A key matching
`/^_*__ref$/` gains one leading underscore when serialized; a key matching
`/^_+__ref$/` loses one when revived. `{__ref: 'x'}` as data goes out as
`{___ref: 'x'}` and comes back as `{__ref: 'x'}`, and the scheme nests. The
walk was needed anyway for the reachability closure, so this costs nothing
extra.

Reviving also has to restore the *second* WeakSet, not just the first.
`markRewritten` is applied to a container only where a reference occurs beneath
it, exactly as `normalize` applies it, because a container with no mark is the
identity function on read. Mark everything and structural sharing is voided for
the whole response; mark nothing and every reference under the fast path stays
unresolved.

### Two modes

`normalized` (the default) ships records plus skeletons, so an entity five
queries share appears once, and an entity cycle serializes without difficulty
because it closes through references and the record map is flat. `denormalized`
ships each query's rebuilt value, needs no revive pass, and **cannot express a
query whose value contains an entity cycle**: `denormalize` rebuilds that as a
real cycle and no JSON encoding of one exists, so it throws and names the query.

### What is not carried

`deps` are recomputed from the revived skeleton rather than trusted from the
wire, and the cache key is re-derived from the operation and arguments rather
than shipped, so a change to the key scheme cannot desynchronise a server from
a client. Records carry data only: `version` starts again at 1, and `frameAt`
at 0, because the frame clock is per session and a server had no frames to
compare against.

`hydrate` needs the generated `ops` table because a cache record holds an
`OperationMeta` and needs it to refetch, route metadata that lives in the
generated manifest rather than in the store. It refuses with a reason
(`hydrationFailure(error)` answers `'principal'`, `'version'`, `'operation'`, or
`undefined`) so a caller branching on the failure never has to match on message
text.

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
| entity store | 2.4 kB | **2.27 kB** |
| tag graph | 2.25 kB | **2.12 kB** |
| query engine and REST transport | 9.6 kB | **9.3 kB** |
| stream binding | 3.75 kB | **3.65 kB** |
| optimistic overlays | 1.23 kB | **1.14 kB** |
| ssr | 2 kB | **1.82 kB** |
| freshness | 0.5 kB | **0.38 kB** |
| core, REST only | 9.7 kB | **9.34 kB** |
| core with streams | 14.25 kB | **12.08 kB** |

Streams cost 2.74 kB on top of REST-only.

REST-only itself went from 5.95 kB to **6.39 kB** when stream binding landed.
Making `applyFrames` a free function over the cache's public surface recovered
0.19 kB of what an earlier draft spent by hanging it off `QueryCache`. The rest
(the frame clock, the per-record stamps, the staged-commit split and
`racedSince`) is genuinely on the query path and cannot be moved, because
`mutate` is stamped too. That is the honest residue, and it buys the ordering
guarantee for the writes an application makes as well as for the ones it reads.

The `stream binding` sub-budget moved from 3 kB to 3.4 kB in the same change,
because the apply logic moved *into* that surface from the query engine.

Optimistic overlays paid for themselves the same way, in a different sub-bucket.
`mutate` references the overlay stack statically, so `overlay.ts` lands in the
REST-only budget whether or not an application ever passes an `optimistic`
option. That is the trade described above: installable would be DX tax paid for
bytes. Two sub-budgets moved to make room for it: `entity store` from 2 kB to
2.17 kB, for the `OPTIMISTIC` stamp and the `touch` seam `store.ts` opens for
the overlay layer, and `query engine and REST transport` from 6.5 kB to 8.25 kB,
for the stack itself, target derivation and the fold. `optimistic overlays` gets
its own size-limit line, `OverlayStack` measured on its own, for the same reason
`stream binding` did: so the cost is visible in the line that caused it rather
than absorbed into a neighbour.

Server rendering split the same way, and mostly onto its own line. `dehydrate`
and `hydrate` are free functions, so `ssr.ts` and `wire.ts` tree-shake out of
an application that never imports them, which is why `core, REST only` absorbed
only 0.27 kB of the 1.9 kB the feature weighs. What could not tree-shake are
the three methods it needed on `QueryCache` (`peek`, `settledQueries`,
`restore`) and `getServerState` on the query handle, because a class method
lands in every import set that pulls the class in. That is the 0.27 kB, and it
moved `query engine and REST transport` from 8.25 kB to 8.5 kB and `stream
binding` from 3.4 kB to 3.5 kB, both of which had been sitting within a
hundred bytes of their limits already. `tag graph` went from 2.2 kB to 2.25 kB
for the `tags` branch in `settle`. `core with streams` did **not** move, and an
earlier draft of this section wrongly predicted it would: measured, it is
12.8 kB against 14 kB.

Keeping the socket alive moved `stream binding` again, from 3.5 kB to 3.75 kB.
The bytes are `SubscriptionManager` answering the streaming extension's
keepalive, `retry` restarting a socket whose reconnect budget ran out, and the
`online` listener that calls it. None of it is optional in the way a feature is
optional. The extension's heartbeat judges liveness by inbound traffic only, and
the ping it sends is an application message rather than a WebSocket control
frame, so a browser that subscribes and then only listens is closed every
`PingInterval + PongTimeout`. Reconnect and recovery hide that: the data stays
right and the socket comes back, which is exactly why it went unnoticed. What
you get for the 0.22 kB is a live query that streams instead of one that polls
on a forty second cycle.

Freshness landed on its own line. `revalidateOnFocus`, `revalidateOnReconnect`
and `poll` filter to 378 B against 500 B, so an application that never imports
them does not carry them. The parts that could not stay out of the shared set
moved two budgets while the feature was landing: `query engine and REST
transport` from 8.9 kB to 9.32 kB in 428da9ed, and `core, REST only` from
9.2 kB to 9417 B in feb56ada.

Both of those were then raised again, for a different reason than feature
growth. `query engine and REST transport` was passing with about 20 bytes to
spare and `core, REST only` with 77, which is close enough that adding a doc
comment fails the build, and a budget that fails for a reason nobody can act on
teaches people to raise limits without reading them. They are now 9.6 kB and
9700 B, near 3 and 4 percent above what they measure, which is deliberately
tighter than the 6 percent a client-devtools line got: that argument rested on
never reaching a production bundle, and these do. The two are coupled, so they
moved together. REST-only imports the query engine's set plus `EntityStore` and
`manualScheduler`, worth 40 bytes on top today, so a REST-only line sitting
below what the query engine's line permits would fail for growth its own budget
had already approved.

The two budgets the design actually sets are 9 kB REST-only and 14 kB with
streams. With streams has been raised once, to 14.25 kB, for the inspector seam
and the runtime gap-closing work that came with it. REST-only was raised in the
same change and twice more since, so it is worth reading the moves in order:
9 kB to 9.2 kB there, to 9417 B in feb56ada for the freshness work, and to
9.7 kB in 44409faa for the reason given above.

`core with streams` was also the only line with no `import`
filter, so it measured the whole of `dist/index.js`: every export, including
the ones a streaming application never pulls in. That is not the number the
budget is about, and it cannot tell "the streaming runtime got fatter" from "a
tree-shakeable export now exists". It now filters to what a streaming
application actually imports, the `core, REST only` set plus `StreamBinder`,
`SubscriptionManager` and `applyFrames`. It measured 11.86 kB against 14.25 kB
when it was refiltered and **12.08 kB** today. `core, REST only` measures
**9.34 kB** against 9.7 kB.

Only that one line changed meaning. The other eight in the table above were
always filtered and are still comparable. It is the old `core with streams`
figure, 13.73 kB, that was measuring something else, and it should not be read
against the 11.86 kB.

## Known gaps, deliberately left to later chunks

- A frame's ordering is per entity, not per field. A response rejected for
  carrying a frame-stamped entity is rejected whole; past the restart bound it
  commits with that entity skipped whole. Merging a stale response's *other*
  fields into a frame-written record would need field-level stamps, which is a
  larger claim than the wire supports.
- Multiplexed channels need a mapping to disambiguate. Where several channels
  share one socket and the envelope carries no `channel` field, a message name
  bound on two of them applies for both. Pass `channelOf` to
  `forgeStreamingDecoder` and a `channel_id` resolves to exactly one channel;
  see the stream binding section above. With no mapping and no literal
  `channel` either, the frame falls back to the channel it arrived on and the
  ambiguity stands.
- A denormalized payload cannot carry an entity cycle. The default `normalized`
  mode serializes `Order -> Customer -> Orders[] -> Order` without difficulty,
  because that graph closes through references. `mode: 'denormalized'` ships
  the rebuilt value, which is the cycle itself, so it throws and names the
  query rather than emitting something that will not parse.
- A dehydrated `-0` arrives as `0`. The payload is JSON, and `JSON.stringify`
  has never preserved negative zero. This is a property of JSON rather than of
  the encoding here, so it is recorded rather than worked around. Nothing else
  in a response is lost, including an object shaped exactly like an internal
  reference.
