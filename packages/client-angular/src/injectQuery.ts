import { DestroyRef, computed, effect, inject, runInInjectionContext, signal, untracked } from '@angular/core';
import type { Injector, Signal } from '@angular/core';
import type {
  QueryBinding,
  QueryCache,
  QueryHandle,
  QueryState,
  QueryStatus,
  TagContext,
} from '@forge-go/client-core';
import { injectForgeClient } from './context';

/**
 * Arguments, static or reactive.
 *
 * A `Signal` is a function, so one type covers `() => ({path: {id: id()}})`
 * and a signal passed straight through, and a plain object means "these do not
 * change" rather than "I forgot to make them reactive".
 */
export type QueryArgs = TagContext | undefined | (() => TagContext | undefined);

export interface InjectQueryOptions {
  /** Use this cache rather than the injected or configured one. */
  readonly client?: QueryCache;
  /**
   * Run in this injector rather than the ambient injection context.
   *
   * The escape hatch for calling from `ngOnInit` or a callback, where Angular
   * has no context of its own. The binding's lifetime becomes that injector's.
   */
  readonly injector?: Injector;
}

/** What `injectQuery` returns: the query's state as signals, plus the controls. */
export interface InjectQueryResult<T> {
  /**
   * The whole snapshot, as the cache produced it.
   *
   * Exposed rather than hidden because a caller who wants to pass one stable
   * value into a `computed` or a child's input should not have to reassemble
   * it out of the four signals below, which would mint a new object on every
   * read and undo the point of the exercise.
   */
  readonly state: Signal<QueryState<T>>;
  readonly data: Signal<T | undefined>;
  readonly error: Signal<unknown>;
  readonly status: Signal<QueryStatus>;
  readonly isFetching: Signal<boolean>;
  /** Run the query again whatever the cache holds. */
  refetch(): Promise<T>;
  /**
   * Release the subscription now, rather than when the injection context is
   * destroyed. Rarely needed; the `DestroyRef` covers the normal case.
   */
  destroy(): void;
}

function resolve(args: QueryArgs): TagContext | undefined {
  return typeof args === 'function' ? args() : args;
}

/**
 * Subscribe a component -- or any injection context -- to one query.
 *
 * `op` is a binding out of the generated `hooks.ts` -- `query(ops.orderList)`
 * -- a module-level constant. Named for the `inject*` convention rather than
 * `useX`, because that is what Angular calls a function which must run in an
 * injection context, and because this one genuinely does: it resolves the
 * cache and the `DestroyRef` from the injector.
 *
 * Angular 19's `resource()` was the other candidate shape, and was not taken.
 * A `resource` owns its request and re-runs a loader when its params change;
 * this owns nothing -- the request, the cache entry and the invalidation all
 * belong to the core, and the query is shared with every other caller asking
 * the same question. Wearing a `resource`'s clothes would advertise a
 * lifecycle this does not have.
 *
 * Everything that decides *what* the value is happens in the cache: identity,
 * staleness, deduplication, invalidation. This function's job is to move that
 * value into the signal graph without copying it on the way.
 */
export function injectQuery<T>(
  op: QueryBinding<T>,
  args?: QueryArgs,
  options?: InjectQueryOptions,
): InjectQueryResult<T> {
  const injector = options?.injector;

  return injector === undefined
    ? bind(op, args, options)
    : runInInjectionContext(injector, () => bind(op, args, options));
}

function bind<T>(
  op: QueryBinding<T>,
  args: QueryArgs,
  options: InjectQueryOptions | undefined,
): InjectQueryResult<T> {
  const client = injectForgeClient(options?.client);
  const destroyRef = inject(DestroyRef);

  let handle: QueryHandle<T> = op(resolve(args), { client });

  /**
   * A signal holding the snapshot, and nothing on the way out of it copies.
   *
   * Angular's default equality is `Object.is`, so writing the same object back
   * is not a change and notifies nobody -- which is exactly the granularity
   * the cache publishes at, and the reason a write to `Order:1` costs a
   * component displaying `Order:2` precisely one memoised read and no change
   * detection. The hazard here is not Angular rewriting the value the way
   * Vue's deep `ref` would; it is a binding that spreads or clones a snapshot
   * on its way to a template. Nothing below does, and the identity tests
   * assert it against the cache's own objects.
   */
  const state = signal<QueryState<T>>(handle.getState());

  let release: (() => void) | undefined;

  function attach(): void {
    const bound = handle;

    release = bound.subscribe(() => {
      state.set(bound.getState());
    });

    /**
     * Read again *after* subscribing. `QueryCache.subscribe` starts the
     * request synchronously for a cold query, so the state moved from `idle`
     * to `pending` between the read above and this line -- and that transition
     * happened before there was a listener to hear about it.
     */
    state.set(bound.getState());
  }

  function detach(): void {
    release?.();
    release = undefined;
  }

  attach();

  if (typeof args === 'function') {
    /**
     * The cache key -- a string -- is what the re-subscription watches, **not
     * the arguments object**. A getter spelt `() => ({path: {id: id()}})`
     * mints a fresh literal on every evaluation, so an effect over the object
     * itself would re-subscribe whenever anything it reads ticks. Against a
     * ref-counted cache that is not merely wasteful: each cycle drops the
     * query's mount count to zero, unlinking it from the tag index and making
     * a query that is on screen a candidate for LRU eviction.
     *
     * Created only for reactive arguments. A plain object cannot change, so an
     * effect over it would be a scheduled no-op on every change detection for
     * the majority of queries in an application.
     */
    const key = computed(() => client.key(op.meta, resolve(args)));

    let current = key();

    effect(() => {
      const next = key();

      if (next === current) return;

      current = next;

      /**
       * `untracked`, so the re-subscription is not itself a tracked read --
       * and so the `state.set` inside `attach` is a write from outside a
       * reactive consumer. Angular before 19 rejects a signal write during an
       * effect's tracked execution outright (`NG0600`); `untracked` clears the
       * active consumer, which makes this one binding correct from 17 through
       * 22 without a version-conditional option that has since been removed
       * from the effect signature.
       */
      untracked(() => {
        detach();
        handle = op(resolve(args), { client });
        attach();
      });
    });
  }

  /**
   * `DestroyRef`, so the lifetime is the injection context's: a component, a
   * lazily-loaded route's injector, a service, or one created by hand for a
   * scope Angular has no other name for. `ngOnDestroy` would only ever cover
   * the first of those.
   */
  destroyRef.onDestroy(detach);

  return {
    state: state.asReadonly(),
    // Each field is a `computed` over the snapshot, so it notifies only when
    // that field's identity changes: a refetch returning an equal `data` moves
    // `isFetching` twice and `data` not at all, and a template reading only
    // `data()` is not marked dirty by either move.
    data: computed(() => state().data),
    error: computed(() => state().error),
    status: computed(() => state().status),
    isFetching: computed(() => state().isFetching),
    refetch: () => handle.refetch(),
    destroy: detach,
  };
}
