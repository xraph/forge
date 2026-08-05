import { isRef, socketSnapshot } from '@forge-go/client-core';
import type {
  QueryCache,
  QueryEntry,
  SocketSnapshot,
  SubscriptionManager,
} from '@forge-go/client-core';
import type {
  CacheSnapshot,
  EntitySnapshot,
  QuerySnapshot,
  StoreSnapshot,
  TagSnapshot,
} from './types';

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
 * - `socketSnapshot(manager)` -- a copy of the socket table.
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
