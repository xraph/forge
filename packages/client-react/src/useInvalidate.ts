import { useMemo } from 'react';
import type {
  OperationMeta,
  QueryBinding,
  QueryCache,
  QueryEntry,
  TagContext,
} from '@forge-go/client-core';
import { useClient } from './context.js';

/**
 * Refresh queries this component does not hold.
 *
 * Callable for the common case, with two named methods for the rest. See
 * `useInvalidate`.
 */
export interface Invalidate {
  /**
   * Mark a read stale: every cached variant of it, or one exact variant.
   *
   * `invalidate(useOrderList)` reaches every set of arguments the cache is
   * holding for that operation -- every page, every filter, every sort.
   * `invalidate(useOrderGet, {path: {id: 7}})` reaches exactly the query a
   * component elsewhere opened with those arguments and nothing else.
   *
   * Omitting `args` is *not* the same as passing `{}`. `useQuery(useOrderList)`
   * keys as `GET /orders` and `useQuery(useOrderList, {})` keys as
   * `GET /orders|{}`, which are two records; the no-argument form here means
   * "all of them" and so covers both, which is what a caller asking to refresh
   * a list actually wants.
   */
  <T>(op: QueryBinding<T>, args?: TagContext): void;
  /**
   * The same selection, but eager and awaitable for the queries on screen.
   *
   * Mounted matches start now and this resolves when they have settled;
   * unmounted matches are marked stale exactly as `invalidate` marks them and
   * are not waited for. For a dialog that must not close until the list behind
   * it shows the row it just wrote, and for nothing else -- `invalidate` is the
   * spelling that belongs in an event handler.
   *
   * **Rejects** when a refetch fails, unlike the void-returning call above.
   * That asymmetry is the same one `useMutation` draws between `mutate` and
   * `mutateAsync`: the safe spelling is the one a caller reaches for first, and
   * the one that hands back a failure is the one they asked for by name.
   */
  refetch<T>(op: QueryBinding<T>, args?: TagContext): Promise<void>;
  /**
   * Invalidate already-resolved tags, as a stream frame or a settled mutation
   * would.
   *
   * The runtime's own model, and the right tool when an operation genuinely
   * declares `provides`. It is a poor default only because most generated reads
   * currently declare none, which is why the callable form above addresses
   * queries by operation instead.
   */
  tags(tags: Iterable<string>): void;
}

/**
 * The entries this call selects, **collected before anything is done to them.**
 *
 * An array rather than a generator, because the callers below mutate the
 * registry while they walk this: `QueryCache.refetch` opens a record, and
 * opening one can `reap`, which deletes from the very map being iterated.
 *
 * The operation name comes back out of `client.key(meta, undefined)` rather
 * than being rebuilt from `meta.method` and `meta.path`. They are the same
 * string -- `queryKey(operation, undefined)` is the bare operation -- and
 * asking the cache means the key scheme stays the core's business. A copy of
 * the derivation here would be a second definition of query identity that
 * nothing forces to agree with the first.
 */
function matching<T>(
  client: QueryCache,
  op: QueryBinding<T>,
  args: TagContext | undefined,
): QueryEntry[] {
  const operation = client.key(op.meta, undefined);
  const target = args === undefined ? undefined : client.key(op.meta, args);
  const found: QueryEntry[] = [];

  for (const entry of client.registry.all()) {
    if (target === undefined ? entry.operation === operation : entry.key === target) {
      found.push(entry);
    }
  }

  return found;
}

/**
 * The arguments that **reproduce this entry's key**, which are not always the
 * arguments the entry holds.
 *
 * `QueryCache.open` derives the key from what the caller passed and then stores
 * `args ?? {}`, so a query fetched as `useQuery(useOrderList)` is keyed
 * `GET /orders` while its entry holds `{}` -- which re-derives as
 * `GET /orders|{}`. Refetching with `entry.args` would therefore open a
 * *second*, empty record, fetch that, and leave the query the component is
 * actually watching exactly as stale as it was. `settledQueries` draws the same
 * distinction for the same reason.
 */
function argsFor(client: QueryCache, meta: OperationMeta, entry: QueryEntry): TagContext | undefined {
  return client.key(meta, undefined) === entry.key ? undefined : entry.args;
}

/**
 * Refresh a query some other component is holding.
 *
 * The gap this fills is narrow and constant: a mutation in a dialog has to
 * refresh a list in a parent or a sibling, and `useQuery`'s own `refetch` only
 * reaches the query the calling component opened. What a caller needs is a way
 * to *name* a query without reaching the component that holds it, and the
 * generated binding is already exactly that name -- `useOrderList` is a
 * module-level constant out of `hooks.ts`, so the dialog imports the operation
 * rather than the component.
 *
 * ```tsx
 * const invalidate = useInvalidate();
 *
 * invalidate(useOrderList);                          // every cached variant
 * invalidate(useOrderGet, {path: {id: 7}});          // exactly that one
 * await invalidate.refetch(useOrderList);            // and wait for it
 * invalidate.tags(['Order[]']);                      // the tag graph directly
 * ```
 *
 * **By operation rather than by tag, deliberately.** Tag invalidation is the
 * runtime's own model and `tags` exposes it, but it can only reach a query that
 * declares `provides`, and most generated reads currently declare none. An API
 * whose primary spelling worked for a fifth of the operations in a manifest
 * would be a worse answer than no API at all, because the fifth it did work for
 * is not the fifth a developer hits first.
 *
 * **What a match costs.** `invalidate` marks stale and returns; it does not
 * fetch. A mounted match is reported to the `Invalidator`, which coalesces
 * every invalidation raised in the same turn into one batch, so three writes in
 * a `Promise.all` cost one refetch of each affected query rather than three. An
 * unmounted match keeps the flag and refetches when it is next mounted -- a
 * list on a route the user has navigated away from costs nothing until they go
 * back to it. Both behaviours are the registry's, not this hook's.
 *
 * Resolves its cache exactly as `useClient` does, and takes the same override,
 * so the precedence a component reads from is the precedence it invalidates
 * through. The returned function is stable for as long as that cache is, so it
 * is safe in a dependency array and in a `memo`'d child's props.
 */
export function useInvalidate(client?: QueryCache): Invalidate {
  const cache = useClient(client);

  return useMemo(() => {
    const invalidate = <T>(op: QueryBinding<T>, args?: TagContext): void => {
      for (const entry of matching(cache, op, args)) cache.registry.markStale(entry);
    };

    return Object.assign(invalidate, {
      /**
       * Mounted matches are started **directly** rather than being marked
       * stale and left to the batch, and that is not merely a shortcut.
       * `markStale` enqueues into the `Invalidator`, whose batch runs on the
       * cache's scheduler; awaiting the request here would let it settle first,
       * and the batch would then find no request in flight and issue a second
       * one for an answer already on screen. Marking only what this call is not
       * going to wait for keeps the two paths from overlapping at all.
       */
      refetch: <T>(op: QueryBinding<T>, args?: TagContext): Promise<void> => {
        const running: Promise<unknown>[] = [];

        for (const entry of matching(cache, op, args)) {
          if (entry.mounts === 0) {
            cache.registry.markStale(entry);
            continue;
          }

          running.push(cache.refetch(op.meta, argsFor(cache, op.meta, entry)));
        }

        return Promise.all(running).then(() => undefined);
      },
      tags: (tags: Iterable<string>): void => {
        cache.invalidate(tags);
      },
    });
  }, [cache]);
}
