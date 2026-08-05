# @forge-go/client-react

The React binding over [`@forge-go/client-core`](../client-core). Two hooks and a
provider, 722 B gzipped.

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
  and provokes no second request.

## What it does not do yet

Streaming (`live: true`), devtools, and SSR hydration. On a server render
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
