# @forge-go/client-vue

The Vue 3 binding over [`@forge-go/client-core`](../client-core). Three composables
and an optional provider, 1.32 kB gzipped.

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
app.use(clientPlugin(cache));      // whole application
provideClient(cache);        // one subtree, from a parent's setup
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
  invalidation is authored in the component; the server declared it. That holds
  for the entities a mutation's own response commits, even when no tag it
  declared reaches them — a query displaying `Order:9` is updated by a create
  that returns `Order:9` and invalidates only `Order[]`.
- **An unchanged entity keeps its object identity across a refetch**, so a child
  component holding it as a prop skips its update. The container is not
  guaranteed: a fresh response is a fresh skeleton, and the store does not claim
  to know that the shape of a list is unchanged. Identity is guaranteed where
  identity is known.
- **One subscription, released once**, whether the scope stops, the component
  unmounts, or `dispose()` is called twice by hand. `live` releases on all three
  as well.
- **`live` is opt-in per call site, and shared underneath.** Two components on
  the same live query are one subscription; two *different* live queries whose
  entities ride the same channel are one connection.

## Refreshing a query you don't hold

`useQuery` hands back a `refetch` for the query that scope opened. When the
write lives somewhere else, use `useInvalidate`:

```vue
<script setup lang="ts">
import { useInvalidate, useMutation } from '@forge-go/client-vue';
import { useOrderArchive, useOrderList } from './generated/hooks';

const props = defineProps<{ id: number }>();
const emit = defineEmits<{ done: [] }>();

const archive = useMutation(useOrderArchive);
const invalidate = useInvalidate();

async function submit() {
  await archive.mutateAsync({ path: { id: props.id } });
  invalidate(useOrderList);
  emit('done');
}
</script>

<template>
  <button @click="submit">Archive</button>
</template>
```

You name the operation, never the component. `useOrderList` is a module-level
constant out of the generated `hooks.ts`, so the dialog imports the read it wants
refreshed and stays ignorant of whatever list happens to be displaying it.

Three ways to say which:

```ts
invalidate(useOrderList);                     // every cached variant
invalidate(useOrderGet, { path: { id: 7 } }); // that one exactly
invalidate.tags(['Order[]', 'Order:7']);      // the tag graph directly
```

Pass no arguments and you cover every page, filter and sort the cache is holding
for that operation, which is usually what you mean once a write has landed. Pass
arguments and you get the one query they key, the same key `useQuery` computed
when some other component opened it. A ref or a getter works there too, so the
`() => ({ path: { id: id.value } })` you already handed `useQuery` names the same
query here. It is read once, at the call, and not watched afterwards. One wrinkle. `invalidate(op)`
and `invalidate(op, {})` do not name the same query, because a read called with
no arguments keys differently from one called with an empty object, and the
no-argument form covers both.

`invalidate` marks queries stale and returns. Mounted ones refetch on the next
batch, and several invalidations raised in the same turn coalesce into one round
of requests. Unmounted ones keep the flag and refetch when they next mount, so a
list on a route you have navigated away from costs you nothing until you go back
to it.

When you have to wait, ask for it by name:

```ts
await archive.mutateAsync({ path: { id: props.id } });
await invalidate.refetch(useOrderList);
emit('done');
```

That starts the mounted matches now and resolves once they have settled, which
is what you want before a dialog closes over a list the user is about to read.
It rejects on failure, like `mutateAsync` and unlike `mutate`. Unmounted matches
stay lazy. Refetching twenty cached filter combinations nobody is looking at
would spend twenty requests to no purpose.

Reach for `tags` wherever your operations declare `provides`. That is the
runtime's own model, and it keeps the decision on the server where the schema
lives. Most generated reads declare nothing today, though, so the callable form
addresses queries by operation and works whether or not the tag graph has an edge
to the thing you want refreshed.

It holds no subscription and no watcher, so there is nothing to release and no
`dispose()`. That also makes it safe outside a component: a store or a router
guard can call it inside a bare `effectScope`, and it falls through to the
module-level client exactly as `useClient` does.

## Live queries

```ts
const live = ref(false);
const { data } = useQuery(useOrderList, () => ({ query: { status: filter.value } }), {
  live,
});
```

That subscribes to every channel the manifest binds to an entity this query's
result type can contain, and releases it when the scope ends. A frame updates
the store directly, so `order.updated` costs no request at all.

**Opt-in, per call site, deliberately.** Making it automatic would be fewer
characters and two worse properties: a developer reading a component could no
longer tell whether it holds a socket, and the application's connection count
would become an emergent property of the render tree.

`live` is reactive, like `args`: a `ref` or a getter is followed. Toggling it
subscribes or releases and does **nothing** to the query — no remount, no
refetch, no loading state. Turning it on does not refetch to cover the window it
was off for: freshness is the cache's business, and `live` is not a hidden
refetch trigger. The gap that genuinely is the runtime's fault, a dropped
socket, is recovered by the core. Omitting the option entirely means this call
site is not live and cannot become live, which is why no watcher is created.

It needs a stream runtime — a `StreamBinder` constructed over the same cache.
Without one, `{live: true}` reports through the cache's `onError` rather than
silently handing back a query that never updates.

## What it does not do yet

Devtools and SSR hydration — both land across every adapter at once rather than
one framework at a time.

## Peer dependencies

Vue **and** `@forge-go/client-core` are peers, not dependencies. Two copies of
Vue means two reactivity systems whose effects cannot see each other's
dependencies. Two copies of the core means two module-level caches, so the
client the application configured is not the one its generated hooks read from —
the same defect, one layer down.

Vue 3.3 and above, for `toValue`.
