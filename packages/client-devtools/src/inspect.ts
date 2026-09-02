import { binderSnapshot, isRef, socketSnapshot } from '@forge-go/client-core';
import type {
  BinderSnapshot,
  QueryCache,
  QueryEntry,
  SocketSnapshot,
  StreamBinder,
  SubscriptionManager,
  TrackedRecord,
} from '@forge-go/client-core';
import type {
  CacheSnapshot,
  EntitySnapshot,
  QueryDetail,
  QuerySnapshot,
  StoreSnapshot,
  TagSnapshot,
} from './types.js';

/**
 * Reading the cache, and the one rule that governs every line of this file:
 * **inspection must not mutate**.
 *
 * That is harder than it sounds, because the obvious way to read a query is
 * `cache.getState(meta, args)` and that is a *write*. It calls `open`, which
 * moves the record to the back of the LRU order and creates one if it is
 * missing; then `snapshot`, which calls `read`, which rehydrates the skeleton
 * -- building store memos, linking them into the reverse-dependency index, and
 * assigning the result onto the registry entry's `value`. Calling it from a
 * devtools panel would change which query gets evicted next, change what a
 * placement callback is handed as `current`, and populate memos for queries
 * nobody rendered. A previous review of this codebase found exactly that shape
 * of defect -- `getState` opening records as a render-phase side effect -- and
 * an inspector that reintroduced it from the outside would be worse, because it
 * only misbehaves while somebody is watching.
 *
 * So nothing here calls `getState`, `fetch`, `read`, `denormalize`,
 * `dependencies` or `key`-with-open. Everything below is a map lookup, a
 * counter, or a copy of one. The whole surface is:
 *
 * - `registry.all()`, `registry.get`, `registry.queriesFor` -- map reads.
 * - `store.keys`, `store.getRecord`, `store.size`, `store.version`,
 *   `store.frameVersion`, `store.tombstones` -- map reads and counters.
 * - `cache.tracked()` -- a map read over the records, not `getState`.
 * - `socketSnapshot(manager)` -- a copy of the socket table.
 * - `binderSnapshot(binder)` -- a copy of the stream binder's internals.
 *
 * `store.getRecord` returns the stored record itself, so its `data` is copied
 * one level on the way out. A panel that mutated a field in place would move
 * the store's data with no version bump and no memo invalidation, which is the
 * single most confusing failure this cache can have.
 */

/** Copy one registry entry. The entry is live and must not be handed out. */
function toQuery(entry: QueryEntry): QuerySnapshot {
  return {
    key: entry.key,
    operation: entry.operation,
    args: entry.args,
    mounts: entry.mounts,
    stale: entry.stale,
    // The registry has no settled flag; a value is the observable consequence
    // of one. A query that settled with a literal `undefined` reads as unsettled
    // here, which is a cosmetic inaccuracy in a rare case and not worth a field
    // on the hot path to fix.
    settled: entry.value !== undefined,
    provides: [...entry.provides],
    tags: [...entry.tags].sort(),
    deps: [...entry.deps].sort(),
    settledAt: entry.settledAt,
  };
}

/** Every query the registry remembers, mounted or not, keyed order. */
export function queries(cache: QueryCache): QuerySnapshot[] {
  const out: QuerySnapshot[] = [];

  for (const entry of cache.registry.all()) out.push(toQuery(entry));

  return out;
}

/** One query by cache key. */
export function query(cache: QueryCache, key: string): QuerySnapshot | undefined {
  const entry = cache.registry.get(key);

  return entry === undefined ? undefined : toQuery(entry);
}

/** The entity keys a record's fields point at, one hop. */
function refsOf(data: Readonly<Record<string, unknown>>): string[] {
  const found = new Set<string>();

  const walk = (node: unknown, depth: number): void => {
    if (node === null || typeof node !== 'object' || depth > 8) return;

    if (isRef(node)) {
      found.add((node as { __ref: string }).__ref);

      return;
    }

    if (Array.isArray(node)) {
      for (const element of node) walk(element, depth + 1);

      return;
    }

    for (const value of Object.values(node as Record<string, unknown>)) walk(value, depth + 1);
  };

  walk(data, 0);

  return [...found].sort();
}

/**
 * What the cache holds for one entity, and which queries depend on it.
 *
 * The two halves of "what is in the cache for `Order:7`, and who is watching
 * it". `dependents` is computed by scanning the registry rather than by an
 * index, because the registry is bounded by the cache's LRU cap and an index
 * maintained for a panel nobody has open is a cost the runtime should not pay.
 */
export function entity(cache: QueryCache, key: string): EntitySnapshot | undefined {
  const record = cache.store.getRecord(key);

  if (record === undefined) return undefined;

  const colon = key.indexOf(':');
  const dependents: string[] = [];

  for (const entry of cache.registry.all()) {
    if (entry.deps.has(key) || entry.tags.has(key)) dependents.push(entry.key);
  }

  return {
    key,
    type: colon > 0 ? key.slice(0, colon) : key,
    id: colon > 0 ? key.slice(colon + 1) : '',
    version: record.version,
    frameAt: record.frameAt ?? 0,
    // Copied one level: the record's own object must not escape into a panel
    // that could write to it. See the note at the top of this file.
    fields: { ...record.data },
    refs: refsOf(record.data),
    dependents: dependents.sort(),
  };
}

/** Which queries reached this entity. The cheap half of `entity`. */
export function dependents(cache: QueryCache, key: string): QuerySnapshot[] {
  const out: QuerySnapshot[] = [];

  for (const entry of cache.registry.all()) {
    if (entry.deps.has(key) || entry.tags.has(key)) out.push(toQuery(entry));
  }

  return out;
}

/** How `entities` is narrowed. Both are optional; the default is everything. */
export interface EntityFilter {
  /** Only records whose key starts with `${type}:`. */
  readonly type?: string;
  /** Stop after this many. */
  readonly limit?: number;
}

/**
 * Every entity in the store, or the ones matching a filter.
 *
 * `dependents` is left empty here on purpose: filling it would be one registry
 * scan per record, and a store of fifty thousand records against a registry of
 * a hundred queries is five million set lookups for a list view. Ask `entity`
 * for the one you care about.
 */
export function entities(cache: QueryCache, filter: EntityFilter = {}): EntitySnapshot[] {
  const prefix = filter.type === undefined ? undefined : `${filter.type}:`;
  const limit = filter.limit ?? Infinity;
  const out: EntitySnapshot[] = [];

  for (const key of cache.store.keys()) {
    if (out.length >= limit) break;
    if (prefix !== undefined && !key.startsWith(prefix)) continue;

    const record = cache.store.getRecord(key);

    if (record === undefined) continue;

    const colon = key.indexOf(':');

    out.push({
      key,
      type: colon > 0 ? key.slice(0, colon) : key,
      id: colon > 0 ? key.slice(colon + 1) : '',
      version: record.version,
      frameAt: record.frameAt ?? 0,
      fields: { ...record.data },
      refs: refsOf(record.data),
      dependents: [],
    });
  }

  return out;
}

/**
 * The tag graph: every tag any remembered query carries, and who carries it.
 *
 * `carriers` includes unmounted queries and `mounted` does not, and the gap
 * between the two columns is itself an answer -- a tag with carriers and no
 * mounted queries is one where an invalidation marks queries stale and issues
 * no request at all, which is the correct behaviour and a common surprise.
 */
export function tags(cache: QueryCache): TagSnapshot[] {
  const carriers = new Map<string, string[]>();

  for (const entry of cache.registry.all()) {
    for (const tag of entry.tags) {
      const held = carriers.get(tag);

      if (held === undefined) carriers.set(tag, [entry.key]);
      else held.push(entry.key);
    }
  }

  const out: TagSnapshot[] = [];

  for (const [tag, keys] of carriers) {
    out.push({
      tag,
      carriers: keys.sort(),
      mounted: cache.registry
        .queriesFor(tag)
        .map((entry) => entry.key)
        .sort(),
    });
  }

  return out.sort((a, b) => (a.tag < b.tag ? -1 : a.tag > b.tag ? 1 : 0));
}

/** The counters. All of them are getters over a `Map.size` or an integer. */
export function store(cache: QueryCache): StoreSnapshot {
  return {
    records: cache.store.size,
    version: cache.store.version,
    frameVersion: cache.store.frameVersion,
    tombstones: cache.store.tombstones,
    tracked: cache.size,
    remembered: cache.registry.size,
    mounted: cache.registry.mounted,
    indexedTags: cache.registry.indexedTags,
    stampedTags: cache.registry.stampedTags,
  };
}

/** Everything except the entity table, which is usually far too large to dump. */
export function snapshot(cache: QueryCache): CacheSnapshot {
  return { store: store(cache), queries: queries(cache), tags: tags(cache) };
}

/**
 * The stream runtime's sockets, if there is one.
 *
 * The manager is found the way an application would: `cache.live` is the
 * `StreamBinder` a `configureStreams` wired up, and a binder holds its manager.
 * The property is read structurally because `LiveBinding` -- the interface the
 * cache declares -- deliberately does not mention it, for the bundle-size
 * reason documented on that type. An application that built a manager without a
 * binder passes it in explicitly.
 */
export function sockets(
  cache: QueryCache,
  manager?: SubscriptionManager,
): readonly SocketSnapshot[] {
  const found = manager ?? (cache.live as { manager?: SubscriptionManager } | undefined)?.manager;

  return found === undefined ? [] : socketSnapshot(found);
}

/**
 * A bounded copy of an arbitrary value.
 *
 * The detail pane renders the last settled response, and that response is the
 * one thing in this file that is not already small. Capping it keeps a panel
 * from serialising a ten-thousand-row list into the DOM, and keeps the
 * snapshot from aliasing anything the store still holds.
 */
function capped(value: unknown, depth = 0): unknown {
  if (depth > 6) return '[deeper]';
  if (value === null || typeof value !== 'object') return value;

  if (Array.isArray(value)) {
    const out: unknown[] = value.slice(0, 100).map((element) => capped(element, depth + 1));

    if (value.length > 100) out.push(`[${String(value.length - 100)} more]`);

    return out;
  }

  const out: Record<string, unknown> = {};

  for (const [key, member] of Object.entries(value as Record<string, unknown>)) {
    out[key] = capped(member, depth + 1);
  }

  return out;
}

/**
 * One query, joined across both halves of what the cache knows about it.
 *
 * `registry.get` and `cache.tracked()`, both map reads. Nothing here opens a
 * record: a query the registry remembers but the cache has reaped has an entry
 * and no record, and is reported with `status: 'idle'`, which is the honest
 * answer rather than an invented one.
 */
export function detail(cache: QueryCache, key: string): QueryDetail | undefined {
  const entry = cache.registry.get(key);

  if (entry === undefined) return undefined;

  let record: TrackedRecord | undefined;

  for (const candidate of cache.tracked()) {
    if (candidate.key === key) {
      record = candidate;
      break;
    }
  }

  return {
    ...toQuery(entry),
    status: record?.status ?? 'idle',
    fetching: record?.fetching ?? false,
    error: record?.error === undefined ? undefined : String(record.error),
    inflight: record?.inflight !== undefined,
    restart: record?.restart ?? false,
    frameRestarts: record?.frameRestarts ?? 0,
    value: capped(entry.value),
  };
}

/** The stream binder, copied out. `undefined` when no stream runtime is wired. */
export function binderView(
  cache: QueryCache,
  override?: StreamBinder,
): BinderSnapshot | undefined {
  const binder = override ?? (cache.live as StreamBinder | undefined);

  if (binder === undefined) return undefined;

  // A `LiveBinding` that is not a `StreamBinder` has none of the internals the
  // snapshot reads. Duck-check rather than assume: `manager` is the binder's
  // one public field.
  if (!('manager' in binder)) return undefined;

  return binderSnapshot(binder);
}
