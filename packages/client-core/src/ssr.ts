import { operationName } from './cache';
import type { CachedQuery, QueryCache } from './cache';
import type { TagContext } from './tags';
import type { EntityKey } from './types';
import type { OperationMeta } from './transport';
import { assertAcyclic, encode, revive } from './wire';

/**
 * Serializing a cache for a server render, and reading it back.
 *
 * The payload is data embedded in an HTML response, so what it may contain is a
 * property of this module rather than a caution in the documentation.
 * `dehydrate` never reads the store wholesale: the record set is *built* by a
 * reachability walk from the exported queries, so an entity no exported query
 * references cannot appear in the payload -- not because a rule forbids it, but
 * because nothing ever put it there. That is what makes a module-level cache
 * shared across concurrent server requests survivable rather than a leak.
 *
 * Both sides also assert the principal. `dehydrate` refuses to serialize for
 * anyone but the cache's current owner, and `hydrate` refuses a payload that
 * belongs to someone else -- which is what a payload cached at a CDN and served
 * to the wrong session runs into.
 *
 * Optimistic overlays are not serialized, and there is nothing to decide there:
 * an overlay is a local write the server has not confirmed, both reads below go
 * through the entity plane rather than the projection, and a server render has
 * no overlays to begin with.
 */

/** One query in a normalized payload. */
export interface NormalizedQuery {
  readonly operation: string;
  /** Absent when the query takes none. See `CachedQuery.args`. */
  readonly args: TagContext | undefined;
  readonly skeleton: unknown;
  /**
   * The resolved tag set.
   *
   * Carried because this mode holds no response, and `provides` templates
   * naming `{res.x}` cannot be resolved without one. See `SettleResult.tags`.
   */
  readonly tags: readonly string[];
}

/** One query in a denormalized payload. */
export interface DenormalizedQuery {
  readonly operation: string;
  /** Absent when the query takes none. See `CachedQuery.args`. */
  readonly args: TagContext | undefined;
  readonly value: unknown;
}

export interface NormalizedState {
  readonly v: 1;
  readonly mode: 'normalized';
  readonly principal?: string | number | null;
  readonly records: Readonly<Record<EntityKey, unknown>>;
  readonly queries: readonly NormalizedQuery[];
}

export interface DenormalizedState {
  readonly v: 1;
  readonly mode: 'denormalized';
  readonly principal?: string | number | null;
  readonly queries: readonly DenormalizedQuery[];
}

export type DehydratedState = NormalizedState | DenormalizedState;

export interface DehydrateOptions {
  /**
   * Who this payload's data belongs to. **Required**, and asserted against the
   * cache's owner.
   *
   * Constrained to a scalar. That is not arbitrary: `setPrincipal` compares
   * with `===`, so an object principal already re-clears the cache on every
   * call that mints a fresh one -- the store's working contract is a scalar,
   * and this states it. `undefined` is encoded as the key's absence, which is
   * what `JSON.stringify` does with it anyway.
   */
  readonly principal: string | number | null | undefined;
  /**
   * `normalized` (the default) dedupes an entity several queries share and is
   * the smallest wire form. `denormalized` ships each query's rehydrated value
   * and needs no revive pass, at the cost of duplicating shared entities -- and
   * it cannot express a query whose value contains an entity cycle, because
   * `denormalize` rebuilds such a graph as a real cycle and no JSON encoding of
   * one exists.
   */
  readonly mode?: 'normalized' | 'denormalized';
  /** Cache keys to export. Every settled query, when absent. */
  readonly include?: readonly string[];
}

export function dehydrate(cache: QueryCache, options: DehydrateOptions): DehydratedState {
  const { principal } = options;

  if (!scalar(principal)) {
    throw new Error(
      '[forge] dehydrate: principal must be a string, number, null or undefined, ' +
        'so that it survives JSON and compares by value',
    );
  }

  if (!Object.is(principal, cache.owner)) {
    throw new Error(
      '[forge] dehydrate: principal does not match the cache owner -- ' +
        'this cache holds another identity’s data',
    );
  }

  const exported = select(cache, options.include);

  return options.mode === 'denormalized'
    ? denormalized(cache, exported, principal)
    : normalized(cache, exported, principal);
}

/**
 * The queries to export: those named, or every settled one.
 *
 * A named key the cache does not hold throws rather than exporting nothing. A
 * typo that silently ships an empty payload is the defect found in production,
 * where it presents as server rendering having quietly stopped working.
 */
function select(cache: QueryCache, include: readonly string[] | undefined): CachedQuery[] {
  const settled = cache.settledQueries();

  if (include === undefined) return settled;

  const byKey = new Map(settled.map((query) => [query.key, query]));

  return include.map((key) => {
    const query = byKey.get(key);

    if (query === undefined) throw new Error(`[forge] dehydrate: no settled query for ${key}`);

    return query;
  });
}

function normalized(
  cache: QueryCache,
  exported: readonly CachedQuery[],
  principal: string | number | null | undefined,
): NormalizedState {
  const queries: NormalizedQuery[] = [];
  const records: Record<EntityKey, unknown> = {};
  const seen = new Set<EntityKey>();
  // Each pending key remembers the query that reached it, so a cycle inside a
  // record can name the query whose payload would have carried it.
  const pending: { key: EntityKey; from: string }[] = [];

  const enqueue = (keys: readonly EntityKey[], from: string): void => {
    for (const key of keys) {
      if (seen.has(key)) continue;

      seen.add(key);
      pending.push({ key, from });
    }
  };

  for (const query of exported) {
    const encoded = encode(query.skeleton, { query: query.key });

    enqueue(encoded.refs, query.key);

    queries.push({
      operation: operationName(query.meta),
      args: query.args,
      skeleton: encoded.value,
      tags: [...(cache.registry.get(query.key)?.tags ?? [])],
    });
  }

  while (pending.length > 0) {
    const { key, from } = pending.pop() as { key: EntityKey; from: string };
    const record = cache.store.getRecord(key);

    // A reference the store no longer holds -- evicted between the fetch and
    // this call. It rehydrates to nothing on the client exactly as it does
    // here, which is the behaviour `denormalize` already specifies for a hole.
    if (record === undefined) continue;

    const encoded = encode(record.data, { query: from, entity: key });

    records[key] = encoded.value;
    enqueue(encoded.refs, from);
  }

  return {
    v: 1,
    mode: 'normalized',
    ...(principal === undefined ? {} : { principal }),
    records,
    queries,
  };
}

function denormalized(
  cache: QueryCache,
  exported: readonly CachedQuery[],
  principal: string | number | null | undefined,
): DenormalizedState {
  const queries = exported.map((query) => {
    // The cache retains no raw responses -- `settle` reads one to resolve tags
    // and does not keep it -- so this is the response as the store now holds
    // it, merges included. `store.write` re-normalizes it into the same records.
    const value = cache.store.read(query.skeleton);

    assertAcyclic(value, { query: query.key });

    return { operation: operationName(query.meta), args: query.args, value };
  });

  return {
    v: 1,
    mode: 'denormalized',
    ...(principal === undefined ? {} : { principal }),
    queries,
  };
}

export interface HydrateOptions {
  /**
   * The generated `ops.ts` table, passed verbatim.
   *
   * A cache record holds an `OperationMeta` and needs it to refetch, to
   * `watchLive` and to drive the transport -- and that is route metadata living
   * in the generated manifest, not in the store, so it cannot be reconstructed
   * from a payload. Serializing it instead would make this argument unnecessary
   * at the cost of putting the route table into every HTML response, to
   * duplicate what the client bundle already ships.
   *
   * Keyed however the generator keys it. The values are what matter, and they
   * are re-indexed below by the same `method path` the cache keys operations by.
   */
  readonly ops: Readonly<Record<string, OperationMeta>>;
  /**
   * Settle every hydrated query behind the server, so a mount refetches.
   *
   * Off by default, which is right for a dynamically rendered page: the server
   * fetched the data milliseconds earlier. A statically generated or ISR page
   * wants it on -- instant paint, then a verifying refetch.
   */
  readonly stale?: boolean;
}

export function hydrate(cache: QueryCache, state: DehydratedState, options: HydrateOptions): void {
  if (state.v !== 1) {
    throw new Error(`[forge] hydrate: unsupported payload version ${String(state.v)}`);
  }

  if (!Object.is(state.principal, cache.owner)) {
    throw new Error(
      '[forge] hydrate: this payload belongs to a different principal -- ' +
        'set the principal before hydrating, and never hydrate a payload built for someone else',
    );
  }

  const index = new Map<string, OperationMeta>();

  for (const meta of Object.values(options.ops)) index.set(operationName(meta), meta);

  const metaFor = (operation: string): OperationMeta => {
    const meta = index.get(operation);

    if (meta === undefined) throw new Error(`[forge] hydrate: no operation named ${operation}`);

    return meta;
  };

  const stale = options.stale === true ? { stale: true } : {};

  if (state.mode === 'normalized') {
    // Records before skeletons. `restore` reads its query's value as it settles,
    // and a skeleton restored before the entity it references would read as a
    // hole and settle the query with one.
    for (const [key, data] of Object.entries(state.records)) {
      cache.store.put(key, revive(data) as Record<string, unknown>);
    }

    for (const query of state.queries) {
      cache.restore(metaFor(query.operation), query.args, {
        skeleton: revive(query.skeleton),
        tags: query.tags,
        ...stale,
      });
    }

    return;
  }

  if (state.mode === 'denormalized') {
    for (const query of state.queries) {
      const meta = metaFor(query.operation);
      const { skeleton } = cache.store.write(
        query.value,
        cache.entities,
        meta.rootType ?? meta.entity,
      );

      cache.restore(meta, query.args, { skeleton, response: query.value, ...stale });
    }

    return;
  }

  throw new Error(
    `[forge] hydrate: unrecognised payload mode ${String((state as { mode: unknown }).mode)}`,
  );
}

function scalar(value: unknown): value is string | number | null | undefined {
  return (
    value === undefined || value === null || typeof value === 'string' || typeof value === 'number'
  );
}
