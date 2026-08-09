import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react';
import type { QueryBinding, QueryCache, QueryState, TagContext } from '@forge-go/client-core';
import { useClient } from './context';

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
   * Cheaper than it looks when several components want it. Two components on
   * the same live query are one subscription, and two *different* live queries
   * whose entities ride the same channel are one connection -- the ref
   * counting is the core's, and this hook is careful not to defeat it by
   * subscribing per component instead of per query.
   */
  readonly live?: boolean;
}

/** What `useQuery` returns: the query's state, plus the manual refetch. */
export interface UseQueryResult<T> extends QueryState<T> {
  /** Run the query again whatever the cache holds. */
  refetch(): Promise<T>;
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
  const client = useClient(options?.client);

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
   * Computing the key costs one sorted `JSON.stringify` of the arguments.
   *
   * Measured, it happens **three times per render per query**: once here, and
   * twice inside the cache, because both `subscribe` and `getState` route
   * through `QueryCache.open`, which re-derives the key from the same
   * arguments. Accepted rather than optimised: the two extra calls are the
   * core's, deduplicating them would mean either caching a key on the handle
   * (which is what `handle.key` already is, but the cache does not take it) or
   * threading a precomputed key through `open` -- a change to a chunk-3 API
   * for a sorted stringify of a small object literal. If a profile ever puts
   * this on a critical path, `open` taking an optional key is the fix, and it
   * belongs in the core rather than here.
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
   *
   * One known wrinkle, recorded here because it is the caller's problem to
   * understand and the core's to fix. React calls `getSnapshot` **during
   * render**, and `QueryCache.getState` routes through `open`, which creates
   * the record if it is new. So a render that is started and then discarded
   * still leaves a cache record behind -- measured at one record and zero
   * requests, since nothing fetches until `subscribe` runs in an effect -- and
   * that insert can evict a different unwatched query through the 128-entry
   * LRU. Bounded and cheap today: an empty record, and eviction only ever
   * costs a refetch of something nobody is watching. The clean fix is a
   * read-only `peek` on the cache that does not open. `QueryCache.peek` now
   * exists and the server snapshot below uses it; routing this line through it
   * as well would change the client read path for every query in every
   * application, which belongs in its own change rather than in an SSR one.
   *
   * `handle.getServerState` is the third argument, and it is not optional in
   * practice: React calls it during `renderToString` and again on the client's
   * hydration pass, and a tree containing a hook that omits it throws
   * `Missing getServerSnapshot`. It reads through `peek`, so it is stable, it
   * opens nothing, and it returns real data whenever a hydration boundary has
   * warmed the cache above this component -- which is what makes a server
   * render emit markup rather than a spinner. Where no boundary ran, both sides
   * see an empty cache and both read `idle`; the two agree either way, which is
   * the property React actually checks.
   */
  const state = useSyncExternalStore(handle.subscribe, handle.getState, handle.getServerState);

  /**
   * The live subscription, and the four things about this effect that matter.
   *
   * **It is separate from the query subscription.** Toggling `live` therefore
   * subscribes or releases a socket and does nothing whatever to the query:
   * no remount, no refetch, no loading state. `false -> true` starts applying
   * frames from that moment; `true -> false` stops. Turning it on deliberately
   * does *not* refetch to close the window it was deaf for -- freshness is the
   * cache's business, decided by invalidation and staleness, and making `live`
   * a hidden refetch trigger would mean a prop that flips per render costs a
   * request per render. The gap that genuinely is the runtime's fault -- a
   * dropped socket -- is recovered by the binder's `recover`.
   *
   * **It is keyed on `key`, not `args`.** Same reason the memo above is: a
   * fresh object literal every render would tear the subscription down and put
   * it back on each one, which against a ref-counted manager means closing and
   * reopening a socket. `args` is read from the closure, which is sound
   * precisely because two argument objects with the same key are the same
   * query.
   *
   * **The cleanup is what makes StrictMode free.** Development double-invokes
   * this effect -- subscribe, release, subscribe -- and the release drops the
   * ref count to zero. The manager defers the actual close by one turn for
   * exactly this reason, so the second subscribe finds the socket still open
   * and cancels the pending close. Nothing here needs to know that; it just
   * must not hold the subscription outside the effect, which is what a ref
   * would do.
   *
   * **Returning `undefined` when not live is not a subscription React has to
   * clean up.** The effect still runs on every `live` change, which is how the
   * toggle is observed at all.
   */
  useEffect(() => {
    if (options?.live !== true) return;

    return client.watchLive(op.meta, args);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `key` is `args`'s identity
  }, [client, op, key, options?.live]);

  const refetch = useCallback(() => handle.refetch(), [handle]);

  // Memoised on the snapshot, so the returned object changes identity exactly
  // when the state does. A caller passing this into a `memo`'d child, or into
  // a dependency array, gets the stability the store went to such lengths to
  // provide rather than a new object per render.
  return useMemo(() => ({ ...state, refetch }), [state, refetch]);
}
