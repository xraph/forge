# @forge-go/client-core

The runtime a generated Forge client delegates to. Generated output contains
types and one-line facades; everything a hook does lives here, so a runtime
defect is fixed by publishing this package rather than by regenerating every
repository that consumes a client.

This is the first chunk: the **normalized entity store**. The query engine,
transports, tag graph and framework adapters land later.

```ts
import { EntityStore, denormalize } from '@forge-go/client-core';
import { entities, ops } from './ops'; // generated

const store = new EntityStore();
const { skeleton, deps } = store.write(response, entities, ops.orderList.entity);

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

## How the runtime knows a typename

A JSON response carries no typename, and this runtime refuses to derive one
from shape. "It has an `id` property, so it must be an entity" is the guess
the Go side deliberately declines to make, because a type carrying both `ID`
and `TenantID` keys two tenants' records to one cache entry.

Instead the typename arrives from the generated manifest and is propagated
structurally:

- `rootType` names the type of the response — `ops.orderGet.entity`, resolved
  in Go against the real response schema.
- An array does not change the typename: `[]Order` is a list of `Order`.
- Descending into an object's field uses `entities[Type].fields[field]`, which
  maps a JSON property to the typename of what it contains (the element
  typename for arrays).

An object is an entity when, and only when, the table names its type *and* the
object carries that type's id field with a usable value (a non-empty string, a
finite number, or a bigint).

### What the generator still has to emit

`internal/client/generators/typescript/opsmanifest.go` emits

```ts
export const entities = { Order: { idField: 'id' } } as const;
```

It does **not** emit `fields`, and `internal/client.EntityRef` has nowhere to
hold it. Until it does, only entities of the operation's declared root type
are extracted; a nested `Customer` inside an `Order` stays inline in the
skeleton. That is under-normalization: the nested record is not shared, so a
write to it does not update this view and the view refetches. It costs a
round trip. It is the correct failure, because the alternative — guessing the
typename from the shape of the JSON — costs a cross-tenant collision.

The same table also routes non-entity wrappers. A type whose `idField` no
payload ever carries is never an entity but still directs typenames to its
children, which is how `{data: ...}` and `{items: [...], total: n}` envelopes
work with no special case.

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

The one case that does not memoize is a cycle closing through plain objects
rather than through an entity. Those memos would carry incomplete dependency
sets, so they are discarded and the subtree is rebuilt per read. JSON cannot
express that shape; only a hand-built object graph can.

## Scripts

```
npm run build      tsc
npm test           vitest
npm run typecheck  tsc --noEmit over src and test
npm run size       build, then size-limit
```

The size budget is enforced from day one because an unenforced budget is a
sentence in a README. The whole core, REST-only, is budgeted at 9 kB gzipped;
this chunk is capped at 2 kB and currently measures **1.54 kB**.

## Known gaps, deliberately left to later chunks

- SSR revival. A skeleton serializes (references carry a `__ref` property) but
  a deserialized one is not recognised as a skeleton, because references are
  identified by object identity rather than by that property. Hydration needs a
  revive pass.
- A refetch returning identical data produces a *new* skeleton, so the root
  identity moves even though no record did. Holding the previous skeleton when
  no record changed belongs to the query engine, which owns the query key.
- Garbage collection of unreferenced entities and the LRU cap.
- Optimistic overlays, principal partitioning, tag graph, transports.
