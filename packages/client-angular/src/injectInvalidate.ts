import { runInInjectionContext } from '@angular/core';
import type { Injector } from '@angular/core';
import type {
  OperationMeta,
  QueryBinding,
  QueryCache,
  QueryEntry,
  TagContext,
} from '@forge-go/client-core';
import { injectClient } from './context.js';
import type { QueryArgs } from './injectQuery.js';

export interface InjectInvalidateOptions {
  /** Use this cache rather than the injected or configured one. */
  readonly client?: QueryCache;
  /**
   * Run in this injector rather than the ambient injection context.
   *
   * The same escape hatch `injectQuery` and `injectMutation` take, for calling
   * from `ngOnInit` or a callback where Angular has no context of its own.
   * Unlike theirs it buys only the cache lookup: this binding registers no
   * `DestroyRef` callback, because it holds nothing that could outlive the
   * caller.
   */
  readonly injector?: Injector;
}

/**
 * Refresh queries this component does not hold.
 *
 * Callable for the common case, with two named methods for the rest. See
 * `injectInvalidate`.
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
   * `args` takes a getter as well as a plain object -- and a `Signal` is a
   * getter -- so the same `() => ({path: {id: this.id()}})` a component handed
   * `injectQuery` names the same query here. It is read once, at the moment of
   * the call: this is an imperative act, not a subscription, so there is
   * nothing for a signal to stay reactive *to*, and nothing here registers as
   * a tracked read.
   *
   * Omitting `args` is *not* the same as passing `{}`.
   * `injectQuery(useOrderList)` keys as `GET /orders` and
   * `injectQuery(useOrderList, {})` keys as `GET /orders|{}`, which are two
   * records; the no-argument form here means "all of them" and so covers both,
   * which is what a caller asking to refresh a list actually wants.
   */
  <T>(op: QueryBinding<T>, args?: QueryArgs): void;
  /**
   * The same selection, but eager and awaitable for the queries on screen.
   *
   * Mounted matches start now and this resolves when they have settled;
   * unmounted matches are marked stale exactly as `invalidate` marks them and
   * are not waited for. For a dialog that must not close until the list behind
   * it shows the row it just wrote, and for nothing else -- `invalidate` is the
   * spelling that belongs in a `(click)`.
   *
   * **Rejects** when a refetch fails, unlike the void-returning call above.
   * That asymmetry is the same one `injectMutation` draws between `mutate` and
   * `mutateAsync`: the safe spelling is the one a caller reaches for first, and
   * the one that hands back a failure is the one they asked for by name.
   */
  refetch<T>(op: QueryBinding<T>, args?: QueryArgs): Promise<void>;
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

function resolve(args: QueryArgs): TagContext | undefined {
  return typeof args === 'function' ? args() : args;
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
 * `args ?? {}`, so a query fetched as `injectQuery(useOrderList)` is keyed
 * `GET /orders` while its entry holds `{}` -- which re-derives as
 * `GET /orders|{}`. Refetching with `entry.args` would therefore open a
 * *second*, empty record, fetch that, and leave the query the component is
 * actually watching exactly as stale as it was. `settledQueries` draws the same
 * distinction for the same reason.
 */
function argsFor(
  client: QueryCache,
  meta: OperationMeta,
  entry: QueryEntry,
): TagContext | undefined {
  return client.key(meta, undefined) === entry.key ? undefined : entry.args;
}

/**
 * Refresh a query some other component is holding.
 *
 * The gap this fills is narrow and constant: a mutation in a dialog has to
 * refresh a list in a parent or a sibling, and `injectQuery`'s own `refetch`
 * only reaches the query the calling component opened. What a caller needs is
 * a way to *name* a query without reaching the component that holds it, and
 * the generated binding is already exactly that name -- `useOrderList` is a
 * module-level constant out of `hooks.ts`, so the dialog imports the operation
 * rather than the component.
 *
 * ```ts
 * export class ArchiveDialog {
 *   private readonly invalidate = injectInvalidate();
 *
 *   archive(): void {
 *     this.invalidate(useOrderList);                     // every cached variant
 *     this.invalidate(useOrderGet, { path: { id: 7 } }); // exactly that one
 *     this.invalidate.tags(['Order[]']);                 // the tag graph directly
 *   }
 * }
 * ```
 *
 * Named `inject*` for the same reason its siblings are: it resolves the cache
 * from the injector, so it must run in an injection context unless it is given
 * one in `{injector}` or handed a cache in `{client}`.
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
 * back to it. Both behaviours are the registry's, not this binding's.
 *
 * Resolves its cache exactly as `injectClient` does, and takes the same
 * override, so the precedence a component reads from is the precedence it
 * invalidates through. There is no `destroy()` and no `DestroyRef` hook: this
 * holds no subscription, no effect and no socket, so there is nothing a
 * teardown could release. The returned function is stable for the life of the
 * context that built it, which is what makes it a field on a component class.
 */
export function injectInvalidate(options?: InjectInvalidateOptions): Invalidate {
  const injector = options?.injector;

  return injector === undefined
    ? bind(options)
    : runInInjectionContext(injector, () => bind(options));
}

function bind(options: InjectInvalidateOptions | undefined): Invalidate {
  const client = injectClient(options?.client);

  const invalidate = <T>(op: QueryBinding<T>, args?: QueryArgs): void => {
    for (const entry of matching(client, op, resolve(args))) client.registry.markStale(entry);
  };

  return Object.assign(invalidate, {
    /**
     * Mounted matches are started **directly** rather than being marked stale
     * and left to the batch, and that is not merely a shortcut. `markStale`
     * enqueues into the `Invalidator`, whose batch runs on the cache's
     * scheduler; awaiting the request here would let it settle first, and the
     * batch would then find no request in flight and issue a second one for an
     * answer already on screen. Marking only what this call is not going to
     * wait for keeps the two paths from overlapping at all.
     */
    refetch: <T>(op: QueryBinding<T>, args?: QueryArgs): Promise<void> => {
      const running: Promise<unknown>[] = [];

      for (const entry of matching(client, op, resolve(args))) {
        if (entry.mounts === 0) {
          client.registry.markStale(entry);
          continue;
        }

        running.push(client.refetch(op.meta, argsFor(client, op.meta, entry)));
      }

      return Promise.all(running).then(() => undefined);
    },
    tags: (tags: Iterable<string>): void => {
      client.invalidate(tags);
    },
  });
}
