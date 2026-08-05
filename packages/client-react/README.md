# @forge-go/client-react

The React binding over [`@forge-go/client-core`](../client-core). Two hooks and a
provider, 786 B gzipped.

Everything that decides *what* a value is — identity, staleness, deduplication,
invalidation — was decided in the core, where it is testable without a renderer.
What is left here is the narrow job of satisfying `useSyncExternalStore`'s
contract without undoing any of it.

```tsx
import { useQuery, useMutation } from '@forge-go/client-react';
import { useOrderList, useOrderCreate } from './generated/hooks';

function Orders() {
  const { data, status, error, isFetching, refetch } = useQuery(useOrderList, {
    query: { status: 'open' },
  });
  const create = useMutation(useOrderCreate);

  if (status === 'pending') return <Spinner />;

  return (
    <>
      <button disabled={create.isPending} onClick={() => create.mutate({ body: { total: 0 } })}>
        New order
      </button>
      {create.status === 'error' && <Warning error={create.error} />}
      <ul>{data?.map((order) => <Row key={order.id} order={order} />)}</ul>
    </>
  );
}
```

`mutate` **never rejects**: a failure lands in `status` and `error`, and the
promise resolves with `undefined`. That is deliberate, and it is why the handler
above needs no `.catch`. A mutation that recorded the error *and* rejected would
ask every caller to remember one — and each forgotten `.catch` is an
`unhandledrejection` per failed write, which in production means an alert firing
about an error the user is already looking at.

When you need to sequence work after a write and must not continue if it did not
happen, ask for the rejection by name:

```ts
await create.mutateAsync({ body: { total: 0 } }); // throws on failure
router.push('/orders');
```

Both record identical state. The only difference is who owns the failure.

The first argument is a binding out of the generated `hooks.ts`
(`export const useOrderList = query(ops.orderList)`), which is a module-level
constant. There is no per-endpoint hook to generate and nothing to regenerate
when this package changes.

## Configuring a client

`configureClient()` from the core is enough on its own:

```ts
import { configureClient, RestTransport } from '@forge-go/client-core';
import { entities } from './generated/ops';
import { client } from './generated/rest';

configureClient({ transport: new RestTransport({ client }), entities });
```

`ForgeProvider` is **optional**, and deliberately so. A generated `hooks.ts`
binds at module scope, long before an application exists to hand anything to;
requiring a provider would mean a file regenerated from a Go route table had
decided how the consuming application does dependency injection. Render one when
a global is the wrong answer — a server handling two requests concurrently, a
test that must not leak into the next one, an application talking to two
backends:

```tsx
<ForgeProvider client={cache}>
  <App />
</ForgeProvider>
```

Resolution is explicit, then provided, then global: `useQuery(op, args, {client})`
beats a provider, which beats `configureClient`. With none of the three,
`getClient()` throws by name rather than minting a scratch cache that nothing
else can see.

## What it guarantees

- **One request per query, not per component.** Two components calling
  `useQuery(useOrderList)` are one cache entry, one registry mount and one
  request. They settle together.
- **A write to `Order:7` re-renders only what references `Order:7`.** No
  invalidation is authored in the component; the server declared it.
- **An unchanged entity keeps its object identity across a refetch**, so a
  `memo`'d row rendering it skips. The container is not guaranteed: a fresh
  response is a fresh skeleton, and the store does not claim to know that the
  shape of a list is unchanged. Identity is guaranteed where identity is known.
- **StrictMode's mount / unmount / mount leaves exactly one live subscription**
  and provokes no second request — for the socket as well as for the query.
- **`live` is opt-in per call site, and shared underneath.** Two components on
  the same live query are one subscription; two *different* live queries whose
  entities ride the same channel are one connection.

## Live queries

```tsx
const { data } = useQuery(useOrderList, { query: { status: 'open' } }, { live: true });
```

That subscribes to every channel the manifest binds to an entity this query's
result type can contain, and releases it when the last consumer unmounts. A
frame updates the store directly, so `order.updated` costs no request at all.

**Opt-in, per call site, deliberately.** Making it automatic would be fewer
characters and two worse properties: a developer reading a component could no
longer tell whether it holds a socket, and the application's connection count
would become an emergent property of the render tree.

`live` is an ordinary prop, so it may change. Toggling it subscribes or releases
and does **nothing** to the query — no remount, no refetch, no loading state.
Turning it on does not refetch to cover the window it was off for: freshness is
the cache's business, and `live` is not a hidden refetch trigger. The gap that
genuinely is the runtime's fault, a dropped socket, is recovered by the core.

It needs a stream runtime — a `StreamBinder` constructed over the same cache.
Without one, `{live: true}` reports through the cache's `onError` rather than
silently handing back a query that never updates.

## What it does not do yet

Devtools and SSR hydration. On a server render
`useQuery` returns `idle` and issues no request: this chunk ships no store
serialisation, so a hydrating client necessarily starts empty, and returning
server-fetched data from `getServerSnapshot` would be a guaranteed hydration
mismatch rather than an optimisation.

## Peer dependencies

Both React **and** `@forge-go/client-core` are peers, not dependencies. Two
copies of React means hooks dispatched against the wrong renderer. Two copies of
the core means two module-level caches, so the client the application configured
is not the one its generated hooks read from — the same defect, one layer down.

React 18 and 19 are both supported and both tested.
