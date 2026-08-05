# @forge-go/client-vue

The Vue 3 binding over [`@forge-go/client-core`](../client-core). Two composables
and an optional provider, 825 B gzipped.

Everything that decides *what* a value is — identity, staleness, deduplication,
invalidation — was decided in the core, where it is testable without a renderer.
What is left here is the narrow job of moving those values into Vue's reactivity
without Vue rewriting them on the way through.

```vue
<script setup lang="ts">
import { useQuery, useMutation } from '@forge-go/client-vue';
import { useOrderList, useOrderCreate } from './generated/hooks';

const filter = ref('open');

const { data, status, error, isFetching, refetch } = useQuery(useOrderList, () => ({
  query: { status: filter.value },
}));
const create = useMutation(useOrderCreate);
</script>

<template>
  <Spinner v-if="status === 'pending'" />
  <template v-else>
    <button :disabled="create.isPending.value" @click="create.mutate({ body: { total: 0 } })">
      New order
    </button>
    <Warning v-if="create.status.value === 'error'" :error="create.error.value" />
    <ul><Row v-for="order in data" :key="order.id" :order="order" /></ul>
  </template>
</template>
```

## shallowRef, and why it is the whole package

The core returns the **same object** when nothing it can prove changed. That is
what lets `<Row>` above skip its update when a write touched a different order.

`ref(state)` would destroy it. A deep `ref` hands back `reactive(state)`, so
`data` would be a proxy of the array and `data[0]` a proxy of the entity — never
the objects the store holds. Vue memoises one proxy per target, so the wrapping
is subtle rather than obviously broken: identity appears to survive, right up
until a value crosses a boundary the store also feeds. It would also make the
cache's objects writable through the proxy, turning a stray `order.total = 0` in
a template into a silent mutation of shared state nobody is notified about.

So every snapshot lives in a `shallowRef`, and the identity tests assert against
the cache's own objects — `toBe(cache.getState().data)`, `isReactive(v) === false`,
`toRaw(v) === v` — rather than against themselves. Four of the five fail
immediately if that `shallowRef` becomes a `ref`; that was measured, not assumed.

## Reactive arguments

`args` is a `MaybeRefOrGetter`, so both spellings work and mean different things:

```ts
useQuery(useOrderGet, { path: { id: 1 } })                 // read once
useQuery(useOrderGet, () => ({ path: { id: id.value } })) // follows `id`
```

The getter is re-evaluated by a `computed` that derives the *cache key*, and the
re-subscription watches that key rather than the object. A getter mints a fresh
literal every evaluation; watching it would tear the subscription down and put it
back on every unrelated tick — and against a ref-counted cache each cycle drops
the query's mount count to zero, unlinking it from the tag index and making a
query on screen a candidate for LRU eviction.

## Lifetime is the scope, not the component

Teardown hangs off `onScopeDispose`, so a query created inside an
`effectScope()` — a store, a route guard, a plugin — is released when *that*
scope stops, not only when some component unmounts. A component's own scope is
disposed on unmount, so the common case is unchanged.

Called with no scope at all, Vue logs its own development warning, which is
correct: a subscription with no owner is a leak. `useQuery` returns `dispose()`
for a caller who really does mean to own that lifetime by hand.

## Mutations

`mutate` **never rejects**: a failure lands in `status` and `error`, and the
promise resolves with `undefined`. That is why the `@click` above needs no
`.catch`. A mutation that recorded the error *and* rejected would ask every
caller to remember one — and each forgotten `.catch` is an `unhandledrejection`
per failed write, which in production means an alert firing about an error the
user is already looking at. Vue offers no convention that argues the other way:
`app.config.errorHandler` never sees a rejected promise returned from a template
event handler, because it is not a render or lifecycle error.

When you need to sequence work after a write and must not continue if it did not
happen, ask for the rejection by name:

```ts
await create.mutateAsync({ body: { total: 0 } }); // throws on failure
router.push('/orders');
```

Both record identical state. The only difference is who owns the failure.

`useMutation`'s options may be a getter, so a `place` callback or a header that
depends on reactive state is current when the write actually runs rather than
frozen as it was at setup. Only `client` is read once: which cache a write goes
to cannot change under an in-flight request.

## Configuring a client

`configureClient()` from the core is enough on its own:

```ts
import { configureClient, RestTransport } from '@forge-go/client-core';
import { entities } from './generated/ops';
import { client } from './generated/rest';

configureClient({ transport: new RestTransport({ client }), entities });
```

The provider is **optional**, and deliberately so. A generated `hooks.ts` binds
at module scope, long before an application exists to hand anything to;
requiring a provider would mean a file regenerated from a Go route table had
decided how the consuming application does dependency injection. Use one when a
global is the wrong answer — an SSR request that must not share a cache with the
request beside it, a test that must not leak into the next one, an application
talking to two backends:

```ts
app.use(forgeClient(cache));      // whole application
provideForgeClient(cache);        // one subtree, from a parent's setup
```

Resolution is explicit, then provided, then global: `useQuery(op, args, {client})`
beats a provider, which beats `configureClient`. With none of the three,
`getClient()` throws by name rather than minting a scratch cache that nothing
else can see.

## What it guarantees

- **One request per query, not per component.** Two components calling
  `useQuery(useOrderList)` are one cache entry, one registry mount and one
  request. They settle together.
- **A write to `Order:7` updates only what references `Order:7`.** No
  invalidation is authored in the component; the server declared it.
- **An unchanged entity keeps its object identity across a refetch**, so a child
  component holding it as a prop skips its update. The container is not
  guaranteed: a fresh response is a fresh skeleton, and the store does not claim
  to know that the shape of a list is unchanged. Identity is guaranteed where
  identity is known.
- **One subscription, released once**, whether the scope stops, the component
  unmounts, or `dispose()` is called twice by hand.

## What it does not do yet

Streaming (`live: true`), devtools, and SSR hydration — all three land across
every adapter at once rather than one framework at a time.

One core behaviour worth knowing while that is true: a mutation's own response
commits its entities but notifies no subscriber directly. Queries learn about a
write through the tags the server declared on the operation, which is the normal
path and covers the normal case. A query displaying an entity a write touched
*without* declaring a tag for it will not update until something else moves it.

## Peer dependencies

Vue **and** `@forge-go/client-core` are peers, not dependencies. Two copies of
Vue means two reactivity systems whose effects cannot see each other's
dependencies. Two copies of the core means two module-level caches, so the
client the application configured is not the one its generated hooks read from —
the same defect, one layer down.

Vue 3.3 and above, for `toValue`.
