import { QueryCache } from './cache.js';
import type { MutateOptions, QueryCacheOptions, QueryState } from './cache.js';
import type { OptimisticSpec } from './overlay.js';
import type { TagContext } from './tags.js';
import type { OperationMeta } from './transport.js';

/**
 * The cache generated hooks use when they are not handed one.
 *
 * A module-level default exists because `hooks.ts` binds at module scope --
 * `export const useOrderList = query(ops.orderList)` runs at import time, long
 * before an application has constructed anything. The binding therefore has to
 * resolve its cache when it is *called*, not when it is created, and every
 * entry point below takes an explicit `client` for the cases where a global is
 * the wrong answer: SSR, tests, and an application talking to two backends.
 */
let active: QueryCache | undefined;

/** Build a cache and make it the default. Returns it, for the explicit path. */
export function configureClient(options: QueryCacheOptions): QueryCache {
  active = new QueryCache(options);

  return active;
}

/** Install an already-built cache as the default. */
export function setClient(client: QueryCache | undefined): void {
  active = client;
}

/** The default cache. Throws rather than silently caching into a scratch one. */
export function getClient(): QueryCache {
  if (active === undefined) {
    throw new Error('[forge] no client configured: call configureClient() before using a hook');
  }

  return active;
}

/**
 * Per-call options a query binding accepts.
 *
 * Deliberately *not* `RequestOptions`. A query is shared: ten subscribers with
 * the same arguments are one record and one request, and the cache key is the
 * arguments alone. Per-call `headers` or a per-call `signal` would therefore
 * belong to whichever caller happened to create the record, and be silently
 * dropped for the rest -- or, worse, one subscriber's abort would cancel a
 * request nine others are waiting on. Declaring them and discarding them was
 * the previous shape of this type and was simply a lie. A header that varies
 * per request belongs in the `AuthProvider` or an interceptor on the generated
 * client; one that varies per *query* belongs in the arguments, where it keys
 * the cache. Mutations are not shared, so `MutationOptions` does carry both.
 */
export interface QueryOptions {
  /** Use this cache rather than the configured default. */
  readonly client?: QueryCache;
}

/** Per-call options a mutation binding accepts. */
export interface MutationOptions<E = unknown> extends Omit<MutateOptions, 'optimistic'> {
  readonly client?: QueryCache;
  readonly optimistic?: OptimisticSpec<E>;
}

/**
 * The server snapshot for a query the cache holds nothing for.
 *
 * A module-level frozen constant because `getServerSnapshot` has to be
 * referentially stable under a harder condition than `getSnapshot` does: it is
 * asked about queries no record exists for, so there is no per-record memo to
 * lean on. A constant is stable by construction.
 */
const IDLE: QueryState<never> = Object.freeze({
  status: 'idle' as const,
  data: undefined,
  error: undefined,
  isFetching: false,
  isOptimistic: false,
});

/**
 * A mounted query, as a framework binding consumes it.
 *
 * Deliberately the `useSyncExternalStore` shape: `subscribe` plus a
 * `getState` whose result is referentially stable while nothing changes. The
 * React binding in the next chunk is those two functions and nothing else,
 * which is the point -- every decision about identity, staleness and
 * deduplication is made here, where it can be tested without a renderer.
 */
export interface QueryHandle<T> {
  /** This query's cache key. Two handles sharing it are the same query. */
  readonly key: string;
  subscribe(listener: () => void): () => void;
  getState(): QueryState<T>;
  /**
   * The snapshot a server render sees, and the one a hydrating client's first
   * pass must match.
   *
   * `peek` rather than `getState`: this is called for queries the cache has
   * never opened, and opening a record as a side effect of a *render* is wrong
   * twice over -- on a server the cache may be shared between concurrent
   * requests, and a render that is started and discarded would leave an entry
   * behind. `undefined` from `peek` means nothing is cached, which is `idle`.
   *
   * Returning real data here is only correct because hydration exists. React
   * compares this against the client's first pass and treats a difference as a
   * mismatch, so a hydration boundary has to have run above the component. With
   * one, both sides read the same warm cache and the server emits real markup;
   * without one, the cache is empty on both sides and this is `idle` on both.
   * Either way the two agree, which is the property that matters.
   */
  getServerState(): QueryState<T>;
  /** Resolve with the value, fetching only if the cache has nothing fresh. */
  fetch(): Promise<T>;
  /** Fetch regardless of what the cache holds. */
  refetch(): Promise<T>;
}

/**
 * What `query(ops.x)` produces: a callable binding, tagged with its operation.
 *
 * The tag is not decoration -- a framework binding receives these as opaque
 * values from a generated module and needs to tell a query from a mutation
 * without guessing from the shape of what they return.
 */
export interface QueryBinding<T> {
  (args?: TagContext, options?: QueryOptions): QueryHandle<T>;
  readonly kind: 'query';
  readonly meta: OperationMeta;
}

/**
 * What `mutation(ops.x)` produces.
 *
 * `TEntity` is what the operation's optimistic patch is checked against, and
 * it is a second parameter rather than being derived from `TResponse` because
 * the two diverge exactly where it matters: an enveloped create returns a
 * wrapper, and a patch is against the record inside it. The generator emits
 * both from `Endpoint.RootType` and `Endpoint.Entity.Type`.
 */
export interface MutationBinding<TResponse, TEntity = unknown> {
  (args?: TagContext, options?: MutationOptions<TEntity>): Promise<TResponse>;
  readonly kind: 'mutation';
  readonly meta: OperationMeta;
}

/**
 * Bind one read operation. The `query` in the generated `hooks.ts`.
 *
 * Returns a handle rather than a value because a query is a subscription: the
 * value changes when a mutation elsewhere touches an entity it displays, with
 * no refetch and no involvement from the caller.
 */
export function query<T = unknown>(meta: OperationMeta): QueryBinding<T> {
  const bind = (args?: TagContext, options?: QueryOptions): QueryHandle<T> => {
    const cache = options?.client ?? getClient();

    return {
      key: cache.key(meta, args),
      subscribe: (listener) => cache.subscribe(meta, args, listener),
      getState: () => cache.getState<T>(meta, args),
      getServerState: () => cache.peek<T>(meta, args) ?? (IDLE as QueryState<T>),
      fetch: () => cache.fetch<T>(meta, args),
      refetch: () => cache.refetch<T>(meta, args),
    };
  };

  return Object.assign(bind, { kind: 'query', meta } as const);
}

/**
 * Bind one write operation. The `mutation` in the generated `hooks.ts`.
 *
 * Calling it runs the request, commits the response to the entity store,
 * invalidates the tags the operation declared, and gives any placement
 * callbacks the chance to answer for a query instead of refetching it.
 */
export function mutation<TResponse = unknown, TEntity = unknown>(
  meta: OperationMeta,
): MutationBinding<TResponse, TEntity> {
  const bind = (args?: TagContext, options?: MutationOptions<TEntity>): Promise<TResponse> =>
    (options?.client ?? getClient()).mutate<TResponse>(meta, args, options as MutateOptions);

  return Object.assign(bind, { kind: 'mutation', meta } as const);
}
