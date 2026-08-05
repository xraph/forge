import { useCallback, useMemo, useSyncExternalStore } from 'react';
import type { QueryBinding, QueryCache, QueryState, TagContext } from '@forge-go/client-core';
import { useForgeClient } from './context';

export interface UseQueryOptions {
  /** Use this cache rather than the provided or configured one. */
  readonly client?: QueryCache;
}

/** What `useQuery` returns: the query's state, plus the manual refetch. */
export interface UseQueryResult<T> extends QueryState<T> {
  /** Run the query again whatever the cache holds. */
  refetch(): Promise<T>;
}

/**
 * The snapshot a server render sees, for every query, always.
 *
 * `getServerSnapshot` is not optional in practice: React calls it during
 * `renderToString` and again on the client's hydration pass, and a tree
 * containing a hook that omits it throws `Missing getServerSnapshot`. So one
 * has to exist even though SSR hydration proper is a later chunk. What it
 * returns is the interesting question, and there are three constraints:
 *
 * 1. **It must be referentially stable**, for the same reason `getSnapshot`
 *    is, and under a harder condition: it is called for queries the cache has
 *    never opened, so there is no per-record memo to lean on. A frozen
 *    module-level constant is stable by construction.
 *
 * 2. **It must match what the client renders on its first pass.** React
 *    compares the two and treats a difference as a hydration mismatch. This
 *    chunk ships no store serialisation, so a hydrating client necessarily
 *    starts with an empty cache -- which is `idle`. Returning server-fetched
 *    data would therefore not be an optimisation; it would be a guaranteed
 *    mismatch on every query on the page.
 *
 * 3. **It must not touch a cache.** On a server the module-level client is
 *    shared by every concurrent request, and `getState` opens a record as a
 *    side effect: one request's server render would create entries under
 *    another's cache. Worse, `getClient()` throws when nothing is configured,
 *    which would turn "SSR is not supported yet" into "your server render
 *    crashes".
 *
 * The consequence is deliberate and worth stating plainly: **a server render
 * emits the loading branch**, and the data arrives after hydration. That is
 * the honest rendering of what this chunk can actually deliver. Chunk 5
 * replaces this with a snapshot read from the serialised store, at which point
 * points 2 and 3 are satisfied by real machinery rather than by abstaining.
 */
const IDLE: QueryState<never> = Object.freeze({
  status: 'idle' as const,
  data: undefined,
  error: undefined,
  isFetching: false,
});

function serverSnapshot(): QueryState<never> {
  return IDLE;
}

/**
 * Subscribe a component to one query.
 *
 * `op` is a binding out of the generated `hooks.ts` -- `query(ops.orderList)`
 * -- which is a module-level constant and therefore stable by construction.
 *
 * Everything that decides *what* the value is happens in the cache: identity,
 * staleness, deduplication, invalidation. This function's entire job is to
 * hand React two functions that satisfy `useSyncExternalStore`'s contract, and
 * every line below exists because one of them is easy to get wrong.
 */
export function useQuery<T>(
  op: QueryBinding<T>,
  args?: TagContext,
  options?: UseQueryOptions,
): UseQueryResult<T> {
  const client = useForgeClient(options?.client);

  /**
   * The cache key -- a string -- is what the memo below keys on, **not the
   * arguments object**.
   *
   * `useQuery(useOrderGet, {path: {id: 7}})` mints a fresh object literal on
   * every render. Memoising on it would make every dependency comparison fail,
   * rebuild the handle, hand `useSyncExternalStore` a new `subscribe`, and
   * make React tear the subscription down and put it back on every single
   * render. Against a ref-counted cache that is not merely wasteful: each
   * cycle drops the query's mount count to zero, which unlinks it from the tag
   * index and makes it a candidate for LRU eviction, so a component that
   * re-renders often can lose its own cache entry.
   *
   * Computing the key costs one sorted `JSON.stringify` of the arguments and
   * is the cheapest thing in this function.
   */
  const key = client.key(op.meta, args);

  /**
   * The handle, and with it the `subscribe` and `getSnapshot` React holds.
   *
   * `args` is deliberately absent from the dependency list: `key` *is* its
   * identity, and two argument objects that produce the same key describe the
   * same query. Including it would reintroduce exactly the churn the line
   * above exists to prevent.
   *
   * `useMemo` is a cache React is permitted to discard, so this is a strong
   * hint rather than a guarantee. That is survivable in the direction it can
   * fail -- a discarded memo costs one resubscribe of the same query, which
   * the cache ref-counts correctly -- whereas the direction that would be
   * fatal, a `subscribe` that stays stale after `key` changes, is impossible
   * here because `key` is a dependency. Pinning the handle in a ref instead
   * would invert exactly that trade, which is why it is not done.
   */
  const handle = useMemo(() => op(args, { client }), [op, client, key]);

  /**
   * `handle.getState` returns the *same* `QueryState` object on every call
   * until something in it actually changes -- `QueryCache` memoises it, over a
   * `data` whose identity the entity store preserves for every subtree that
   * did not move. That is what the three chunks below this one were built for,
   * and it is consumed here by passing it through untouched.
   *
   * Nothing wraps, spreads or maps the snapshot on its way to React. A single
   * `{...state}` here would return a fresh object on every call and produce
   * either "The result of getSnapshot should be cached to avoid an infinite
   * loop" or a render loop that runs until React's update-depth limit trips.
   * The convenience shape callers actually want is built *after* this line,
   * out of the snapshot, memoised on it.
   */
  const state = useSyncExternalStore(handle.subscribe, handle.getState, serverSnapshot);

  const refetch = useCallback(() => handle.refetch(), [handle]);

  // Memoised on the snapshot, so the returned object changes identity exactly
  // when the state does. A caller passing this into a `memo`'d child, or into
  // a dependency array, gets the stability the store went to such lengths to
  // provide rather than a new object per render.
  return useMemo(() => ({ ...state, refetch }), [state, refetch]);
}
