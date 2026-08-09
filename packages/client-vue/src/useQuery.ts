import { computed, onScopeDispose, shallowRef, toValue, watch } from 'vue';
import type { ComputedRef, MaybeRefOrGetter, ShallowRef } from 'vue';
import type {
  QueryBinding,
  QueryCache,
  QueryHandle,
  QueryState,
  QueryStatus,
  TagContext,
} from '@forge-go/client-core';
import { useForgeClient } from './context';

export interface UseQueryOptions {
  /** Use this cache rather than the provided or configured one. */
  readonly client?: QueryCache;
  /**
   * Subscribe to the channels this query's entities are pushed on, so it
   * updates from server frames as well as from requests.
   *
   * **Opt-in, per call site, deliberately.** Making it automatic would be
   * fewer characters and two worse properties: a developer reading a component
   * could no longer tell whether it holds a socket, and the application's
   * connection count would become an emergent property of the render tree. So
   * it is one word at the place that pays for it.
   *
   * Reactive, like `args`: a `ref` or a getter is followed, and a plain `true`
   * is read once. Omitting it entirely means this call site is *not* live and
   * cannot become live -- which is the point of the opt-in, and why there is
   * no watcher at all in that case.
   *
   * Cheaper than it looks when several components want it. Two components on
   * the same live query are one subscription, and two *different* live queries
   * whose entities ride the same channel are one connection.
   */
  readonly live?: MaybeRefOrGetter<boolean | undefined>;
}

/** What `useQuery` returns: the query's state as refs, plus the controls. */
export interface UseQueryResult<T> {
  /**
   * The whole snapshot, as the cache produced it.
   *
   * A `shallowRef`, so `state.value` **is** the object the cache holds --
   * never a reactive proxy of it. Exposed rather than hidden because a caller
   * who wants to pass one stable value into a `computed`, a `watch` or a
   * child's prop should not have to reassemble it out of the four fields
   * below, which would mint a new object and undo the whole point.
   */
  readonly state: ShallowRef<QueryState<T>>;
  readonly data: ComputedRef<T | undefined>;
  readonly error: ComputedRef<unknown>;
  readonly status: ComputedRef<QueryStatus>;
  readonly isFetching: ComputedRef<boolean>;
  readonly isOptimistic: ComputedRef<boolean>;
  /** Run the query again whatever the cache holds. */
  refetch(): Promise<T>;
  /**
   * Release the subscription now, rather than when the scope ends.
   *
   * Needed only by a caller running outside an `effectScope` -- and Vue warns
   * in development when that happens, because a subscription with no owner is
   * a leak unless somebody calls this.
   */
  dispose(): void;
}

/**
 * Subscribe a component -- or any effect scope -- to one query.
 *
 * `op` is a binding out of the generated `hooks.ts` -- `query(ops.orderList)`
 * -- a module-level constant. `args` may be a plain object, a `ref`, or a
 * getter: `useQuery(useOrderGet, () => ({ path: { id: id.value } }))` follows
 * `id` and re-subscribes when it moves. A plain object is read once, which is
 * correct for the common case where the arguments genuinely do not change, and
 * is why the simple spelling stays simple.
 *
 * Everything that decides *what* the value is happens in the cache: identity,
 * staleness, deduplication, invalidation. This function's job is to move that
 * value into Vue's reactivity without Vue rewriting it on the way.
 */
export function useQuery<T>(
  op: QueryBinding<T>,
  args?: MaybeRefOrGetter<TagContext | undefined>,
  options?: UseQueryOptions,
): UseQueryResult<T> {
  const client = useForgeClient(options?.client);

  /**
   * The cache key -- a string -- is what the re-subscription watches, **not
   * the arguments object**.
   *
   * A getter spelt `() => ({ path: { id: id.value } })` mints a fresh literal
   * on every evaluation, so a `watch` over the object itself would fire on
   * every unrelated tick of anything it reads. Against a ref-counted cache
   * that is not merely wasteful: each cycle drops the query's mount count to
   * zero, unlinking it from the tag index and making it a candidate for LRU
   * eviction, so a query can lose its own cache entry while it is on screen.
   * Two argument objects that produce the same key are the same query, and
   * `key` says exactly that in a value `watch` can compare with `===`.
   */
  const key = computed(() => client.key(op.meta, toValue(args)));

  let handle: QueryHandle<T> = op(toValue(args), { client });

  /**
   * A `shallowRef`, and the entire adapter turns on that one word.
   *
   * `ref(state)` would hand back `reactive(state)` -- a proxy, recursively --
   * so `data.value` would be a proxy of the array, `data.value[0]` a proxy of
   * the entity, and none of them the objects the entity store holds. Every
   * identity guarantee the three chunks below this one were built to provide
   * would survive inside the cache and be destroyed on the way out of it: a
   * child component comparing `props.order` against the value it was given
   * last render would be comparing two proxies minted by different reads, and
   * the memoised row that was the whole point of normalizing would re-render
   * anyway. It would also make the cache's objects writable through the proxy,
   * turning a stray `order.total = 0` in a template into a silent mutation of
   * shared cache state that no subscriber is notified about.
   *
   * `shallowRef` reacts to *assignment of a new snapshot* and nothing else,
   * which is precisely the granularity the cache already publishes at.
   */
  const state = shallowRef<QueryState<T>>(handle.getState());

  let release: (() => void) | undefined;

  function attach(): void {
    const bound = handle;

    /**
     * The listener assigns; it does not merge, spread or map. Assigning the
     * same object into a `shallowRef` is a no-op -- Vue compares with
     * `Object.is` -- so a notification that changed nothing produces no
     * re-render at all, which is how a write to `Order:1` stays invisible to a
     * component displaying `Order:2`.
     */
    release = bound.subscribe(() => {
      state.value = bound.getState();
    });

    /**
     * Read again *after* subscribing. `QueryCache.subscribe` starts the
     * request synchronously for a cold query, so the state moved from `idle`
     * to `pending` between the read above and this line -- and that transition
     * happened before there was a listener to hear about it.
     */
    state.value = bound.getState();
  }

  function detach(): void {
    release?.();
    release = undefined;
  }

  attach();

  /**
   * The live subscription: a second, independent lifetime beside the query's.
   *
   * Independent is the whole of the toggling semantics. `live` moving from
   * `false` to `true` subscribes a socket and does nothing to the query -- no
   * remount, no refetch, no loading state -- and moving back releases it. The
   * two watchers below both react to `key`, so an argument change re-subscribes
   * the live half against the new arguments as well; they are separate
   * watchers rather than one because `live` must be able to move without the
   * key moving, and the key without `live`.
   *
   * Turning `live` on does *not* refetch to cover the window it was off for.
   * Freshness is the cache's business, decided by invalidation and staleness;
   * making `live` a hidden refetch trigger would mean a `ref` that flips costs
   * a request per flip. The gap that genuinely is the runtime's fault -- a
   * dropped socket -- is recovered by the binder.
   */
  const wants = options?.live;
  let liveRelease: (() => void) | undefined;

  function attachLive(): void {
    if (toValue(wants) === true) liveRelease = client.watchLive(op.meta, toValue(args));
  }

  function detachLive(): void {
    liveRelease?.();
    liveRelease = undefined;
  }

  attachLive();

  // Only when the call site asked about `live` at all. A query that never
  // mentions it cannot become live, so a watcher for it would be a scheduled
  // no-op on every tick for the majority of queries in an application.
  const stopLive =
    wants === undefined
      ? undefined
      : watch([key, computed(() => toValue(wants) === true)], () => {
          detachLive();
          attachLive();
        });

  /**
   * Default `flush: 'pre'`, deliberately. A pre-watcher created in `setup`
   * runs before its own component re-renders, so a render triggered by the
   * same argument change never paints the previous query's data. `'sync'`
   * would re-subscribe in the middle of whatever assignment moved the ref,
   * which is a fetch started from inside a reactive setter.
   */
  const stop = watch(key, () => {
    detach();
    handle = op(toValue(args), { client });
    state.value = handle.getState();
    attach();
  });

  function dispose(): void {
    stop();
    stopLive?.();
    detach();
    detachLive();
  }

  /**
   * Scope, not component. `onScopeDispose` fires for an `effectScope().stop()`
   * as well as for an unmount, so a query created inside a store, a route
   * guard or a detached scope is released when *that* ends -- which is the
   * lifetime the caller actually meant. A component's own scope is disposed on
   * unmount, so the common case is unchanged.
   *
   * Called unconditionally: with no scope at all Vue warns, in development
   * only, and that warning is correct. `dispose` is the answer for a caller
   * who really does mean to own the lifetime by hand.
   */
  onScopeDispose(dispose);

  return {
    state,
    // Each field is a `computed` over the snapshot, so it notifies only when
    // that field's identity changes: a refetch returning an equal `data` moves
    // `isFetching` twice and `data` not at all.
    data: computed(() => state.value.data),
    error: computed(() => state.value.error),
    status: computed(() => state.value.status),
    isFetching: computed(() => state.value.isFetching),
    isOptimistic: computed(() => state.value.isOptimistic),
    refetch: () => handle.refetch(),
    dispose,
  };
}
