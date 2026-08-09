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

## Server rendering

```tsx
import { ForgeHydrationBoundary } from '@forge-go/client-react';

<ForgeHydrationBoundary state={state} ops={ops}>
  <OrderTable />
</ForgeHydrationBoundary>
```

`state` is what `dehydrate` produced on the server, and `ops` is the generated
operation table passed straight through. `hydrate` needs it because a cache
record holds an `OperationMeta` and needs one to refetch later.

**It hydrates during render rather than in an effect.** Children read
`getSnapshot` during their own render, which happens after this component's
render returns, so a render-phase hydrate is visible to them on the first pass.
An effect runs after the tree commits: the first paint would be the loading
branch and then flip, which is a visible flash and, on the hydration pass,
exactly the mismatch the component exists to remove. It renders no element of
its own for the same reason, since a wrapper would change the DOM the two sides
are being compared on.

StrictMode double-invokes render, so the boundary remembers which payloads it
has already hydrated into which cache. The mark is set *after* hydration
succeeds, never before: React retries a render that threw, and marking first
would make the retry skip hydration, throw nothing, and render the children as
though it had worked.

`getServerSnapshot` now reads through `QueryCache.peek`, which returns what the
cache holds without opening a record for a query it has never seen. So
`renderToString` emits real markup where a boundary warmed the cache above the
component, and `idle` where none did. Both sides agree either way, which is the
property React actually checks.

A refused payload is handled by reason rather than by message text. A principal
mismatch rethrows, so your error boundary catches it and the subtree does not
mount. A version or operation mismatch is reported through the cache's
`onError` and the tree renders on, because both are repaired by the queries
simply fetching for themselves and blanking a page through every deploy would
be worse than the problem. Anything unrecognised rethrows.

## What it does not do yet

Devtools.

## Peer dependencies

Both React **and** `@forge-go/client-core` are peers, not dependencies. Two
copies of React means hooks dispatched against the wrong renderer. Two copies of
the core means two module-level caches, so the client the application configured
is not the one its generated hooks read from — the same defect, one layer down.

React 18 and 19 are both supported and both tested.
