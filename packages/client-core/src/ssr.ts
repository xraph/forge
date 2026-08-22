import { operationName } from './cache.js';
import type { CachedQuery, QueryCache } from './cache.js';
import type { TagContext } from './tags.js';
import type { EntityKey } from './types.js';
import type { OperationMeta } from './transport.js';
import { assertAcyclic, encode, revive } from './wire.js';

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

/**
 * Why `hydrate` refused a payload.
 *
 * A value rather than a message to match on. A hydration boundary has to decide
 * whether to rethrow or to degrade to a client-side fetch, and that decision
 * settles how an identity mismatch behaves -- far too load-bearing to hang on
 * the exact wording of an error string, which anyone may reasonably reword.
 *
 * - `principal` -- the payload belongs to someone else. The security backstop.
 * - `version` -- the payload was written by code this client does not know,
 *   which a deploy produces routinely while old JS is still cached.
 * - `operation` -- the payload names an operation absent from `ops`. A bug.
 */
export type HydrationFailure = 'principal' | 'version' | 'operation';

/** The own property a refusal carries its reason on. */
const REASON = 'forgeHydration';

function refuse(reason: HydrationFailure, message: string): Error {
  // A plain `Error` with a property, not a subclass: this package has no error
  // classes, and `instanceof` across two copies of it would answer `false`
  // anyway -- which is exactly the case a security check must not get wrong.
  return Object.assign(new Error(`[forge] hydrate: ${message}`), { [REASON]: reason });
}

/**
 * Why `hydrate` threw, or `undefined` for anything it did not raise itself.
 *
 * `undefined` covers both a failure from deeper down and a future reason this
 * client does not recognise, so a caller branching on it treats an unknown
 * refusal as unknown rather than as safe.
 */
export function hydrationFailure(error: unknown): HydrationFailure | undefined {
  if (typeof error !== 'object' || error === null) return undefined;

  const reason = (error as Record<string, unknown>)[REASON];

  return reason === 'principal' || reason === 'version' || reason === 'operation'
    ? reason
    : undefined;
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
  // Both refusals happen before anything is written, so a rejected payload
  // leaves the cache exactly as it found it. That is what lets a caller treat
  // one of these as "nothing happened" rather than as a partial hydration.
  if (state.v !== 1) {
    throw refuse('version', `unsupported payload version ${String(state.v)}`);
  }

  if (!Object.is(state.principal, cache.owner)) {
    throw refuse(
      'principal',
      'this payload belongs to a different principal -- set the principal before ' +
        'hydrating, and never hydrate a payload built for someone else',
    );
  }

  const index = new Map<string, OperationMeta>();

  for (const meta of Object.values(options.ops)) index.set(operationName(meta), meta);

  const metaFor = (operation: string): OperationMeta => {
    const meta = index.get(operation);

    if (meta === undefined) throw refuse('operation', `no operation named ${operation}`);

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

  // `version` rather than a mode of its own: a payload naming a mode this
  // client has never heard of was written by code this client does not know,
  // which is the same situation and wants the same handling.
  throw refuse(
    'version',
    `unrecognised payload mode ${String((state as { mode: unknown }).mode)}`,
  );
}

function scalar(value: unknown): value is string | number | null | undefined {
  return (
    value === undefined || value === null || typeof value === 'string' || typeof value === 'number'
  );
}

/**
 * Which payloads have already been hydrated into which cache.
 *
 * Keyed on the cache first because the same payload legitimately hydrates two
 * of them: a boundary renders on the server against the cache that produced
 * the payload, and again on the client against a fresh one.
 *
 * An optimisation rather than a correctness requirement. `hydrate` merges, and
 * a record written with identical data keeps its previous object and bumps no
 * version, so hydrating twice is harmless. What this buys is that a framework
 * which renders a component more than once for one mount, as React's
 * StrictMode does, does not walk the payload each time.
 */
const boundaries = new WeakMap<QueryCache, WeakSet<object>>();

/**
 * Hydrate a payload into a cache once, applying the policy a framework's
 * hydration boundary needs.
 *
 * All three adapters need the same two things around `hydrate`: do it at most
 * once per cache and payload, and decide which refusals a page can survive.
 * Neither is framework-specific, and having each adapter carry its own copy is
 * how three components drift into three different security postures.
 *
 * The rule for a refusal is one sentence: **continue only for a failure this
 * code recognises AND that a client-side fetch fully repairs. Rethrow
 * everything else.** Degrading is a claim that the page will still be correct,
 * and that claim can only be made about a failure whose consequences are
 * understood.
 *
 * - `version` continues. A client running older code than the server that
 *   rendered the page is what every deploy produces while old JS is still
 *   cached. `hydrate` rejects the payload before writing anything and the
 *   queries simply fetch. Blanking a page for the length of each rollout would
 *   be a far worse failure than the one being handled.
 * - `operation` continues. It is always a bug, but a recoverable one: the
 *   component fetches its own query and the report reaches wherever the
 *   application sends `onError`.
 * - `principal` rethrows. It is the one refusal that says something is wrong
 *   with *whose data this is*, and fetching does not repair it, because
 *   whatever routed this payload here is still misrouted. It is also the case
 *   this feature's security rests on, so it fails loudly by construction
 *   rather than by anyone remembering to make it.
 * - Anything else rethrows, including a failure raised below `hydrate`.
 *   `hydrationFailure` answers `undefined` for a reason a future version adds,
 *   so an unrecognised refusal is treated as unknown rather than as safe.
 *
 * A rethrow leaves the payload unmarked, so it throws again on the next
 * attempt. React re-renders a component that threw: marking first would let
 * the retry find the payload already done, skip hydration, throw nothing and
 * render as though it had worked, turning the one refusal that must be loud
 * into a silent degrade with no error boundary ever seeing it.
 */
export function hydrateBoundary(
  cache: QueryCache,
  state: DehydratedState | undefined,
  options: HydrateOptions,
): void {
  if (state === undefined) return;

  let seen = boundaries.get(cache);

  if (seen === undefined) {
    seen = new WeakSet<object>();
    boundaries.set(cache, seen);
  }

  if (seen.has(state)) return;

  try {
    hydrate(cache, state, options);
  } catch (error) {
    const reason = hydrationFailure(error);

    if (reason !== 'version' && reason !== 'operation') throw error;

    cache.report(error, 'hydrate');
  }

  seen.add(state);
}

/** One flush of a streamed payload. See `streamingDehydrator`. */
export interface StreamingDehydrator {
  /**
   * The payload for everything that settled since the last flush, or
   * `undefined` when nothing did.
   *
   * `undefined` rather than an empty payload so a caller can write the result
   * straight into a stream without first checking whether it says anything.
   */
  flush(): DehydratedState | undefined;
}

/**
 * Dehydrate a cache repeatedly during a streamed render, one chunk per flush.
 *
 * A non-streamed render has one moment to serialize: every query has settled
 * and `dehydrate` runs once. A streamed one does not. Queries settle while
 * the response is already going out, and the point of streaming is to send
 * each result as it lands rather than to hold the document open until the
 * slowest one finishes. Calling `dehydrate` per chunk would work and would
 * also re-send the whole cache every time, so a page with ten boundaries ships
 * its first query's records ten times over.
 *
 * So this remembers what it has already emitted and emits the difference:
 *
 * ```ts
 * const stream = streamingDehydrator(cache, { principal: session.userId });
 *
 * // Each time a boundary resolves, on the server:
 * const chunk = stream.flush();
 * if (chunk !== undefined) write(`<script>__FORGE__.push(${JSON.stringify(chunk)})</script>`);
 * ```
 *
 * On the client, hand each chunk to `hydrate` (or a boundary) in arrival
 * order. `hydrate` merges rather than replaces, so a chunk whose skeleton
 * references a record an earlier chunk carried resolves against what is
 * already in the store, and applying every chunk in order lands on exactly the
 * cache one non-streamed payload would have produced.
 *
 * A record is re-emitted when its version moves, so a value that changed
 * between two flushes is corrected rather than left stale. A query is
 * re-emitted when its skeleton or its tags change. Everything else is sent
 * once.
 *
 * The principal is asserted on **every** flush, not once at construction. A
 * render whose identity changes half way through is a bug, and it is one worth
 * finding at the flush that would have leaked rather than at the end.
 */
export function streamingDehydrator(
  cache: QueryCache,
  options: DehydrateOptions,
): StreamingDehydrator {
  const records = new Map<EntityKey, unknown>();
  const queries = new Map<string, string>();

  /**
   * What identifies a query across flushes.
   *
   * The operation alone is not enough: `GET /orders/{id}` settles once per id,
   * and two of them in one render are two queries.
   */
  const identify = (query: { operation: string; args: TagContext | undefined }): string =>
    `${query.operation}(${query.args === undefined ? '' : JSON.stringify(query.args)})`;

  return {
    flush(): DehydratedState | undefined {
      const full = dehydrate(cache, options);

      if (full.mode === 'denormalized') {
        const fresh = full.queries.filter((query) => {
          const signature = JSON.stringify(query.value);

          if (queries.get(identify(query)) === signature) return false;

          queries.set(identify(query), signature);

          return true;
        });

        if (fresh.length === 0) return undefined;

        return { ...full, queries: fresh };
      }

      const freshRecords: Record<EntityKey, unknown> = {};
      let count = 0;

      for (const key of Object.keys(full.records)) {
        // Compared by version rather than by value. The version is the store's
        // own answer to "did this record change", and it is the same answer
        // `put` already computed with a deep comparison the write paid for --
        // recomputing it here would pay for it twice per flush.
        const version = cache.store.getRecord(key)?.version;

        if (records.get(key) === version) continue;

        records.set(key, version);
        freshRecords[key] = full.records[key];
        count += 1;
      }

      const freshQueries = full.queries.filter((query) => {
        const signature = JSON.stringify({ skeleton: query.skeleton, tags: query.tags });

        if (queries.get(identify(query)) === signature) return false;

        queries.set(identify(query), signature);

        return true;
      });

      if (count === 0 && freshQueries.length === 0) return undefined;

      return { ...full, records: freshRecords, queries: freshQueries };
    },
  };
}
