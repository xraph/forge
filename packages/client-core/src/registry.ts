import { queryKey, resolveTags } from './tags';
import type { TagContext } from './tags';
import type { EntityKey } from './types';

/** What the caller mounts. Everything but `operation` is optional. */
export interface QuerySpec {
  /** The generated manifest's key for this operation, e.g. `orderList`. */
  readonly operation: string;
  /** The arguments this query was called with. Part of its cache key. */
  readonly args?: TagContext;
  /** `ops[operation].provides`, still as templates. */
  readonly provides?: readonly string[];
  /** Override the derived cache key. Two queries sharing a key are one query. */
  readonly key?: string;
}

/**
 * One query in the registry, mounted or merely remembered.
 *
 * Handed to callbacks and returned by `get`. **Treat it as read-only**: the
 * registry mutates these fields in place so that the tag index can hold entry
 * objects directly, and a caller writing to `tags` would desynchronise the
 * index from the registry -- the exact failure this class exists to prevent.
 */
export interface QueryEntry {
  readonly key: string;
  readonly operation: string;
  readonly args: TagContext;
  readonly provides: readonly string[];
  /** Everything this query provides: resolved `provides`, plus its entity deps. */
  tags: Set<string>;
  /** The entity keys its skeleton reached, as `normalize` reported them. */
  deps: Set<EntityKey>;
  /** How many places have this query mounted. Zero means nobody is watching. */
  mounts: number;
  /** Known to be behind the server. Refetches now if mounted, on mount if not. */
  stale: boolean;
  /** The last value this query settled with, as the caller supplied it. */
  value: unknown;
  /** The invalidation clock reading at the last settle. See `QueryRegistry`. */
  settledAt: number;
}

/** What `settle` records. Omitted fields are left as they were. */
export interface SettleResult {
  readonly value?: unknown;
  /** `WriteResult.deps` from the entity store. */
  readonly deps?: Iterable<EntityKey>;
  /** The response, so `provides` templates naming `{res.x}` can resolve. */
  readonly response?: unknown;
}

export interface QueryRegistryOptions {
  /**
   * A mounted query became stale -- either an invalidation hit it while it was
   * mounted, or it mounted having been invalidated while it was not. The
   * `Invalidator` sets this to its own enqueue.
   */
  onStale?: (entry: QueryEntry) => void;
  /** A `provides` template resolved to nothing. See `Invalidator`. */
  onUnresolved?: (template: string, entry: QueryEntry) => void;
}

/** Undo one mount. Idempotent: calling it twice does not decrement twice. */
export type Unmount = () => void;

/**
 * The mounted-query registry and the tag index, as one structure.
 *
 * They are one structure because every mount, unmount and settle has to touch
 * both, and a code path that updates one without the other produces either a
 * leak (a tag bucket holding a query nobody watches, which then refetches on
 * every invalidation forever) or a query that quietly stops updating. Making
 * the index private and giving it no mutator of its own removes the class of
 * bug rather than documenting it.
 *
 * Two facts about lifetime, which pull in opposite directions:
 *
 * - **The tag index holds mounted queries only.** The last unmount removes the
 *   entry from every one of its tag buckets and deletes the buckets it emptied.
 * - **An invalidation arriving while a query is unmounted must still be
 *   observed when it mounts again.** Otherwise a tab switch swallows a write.
 *
 * Reconciled by a clock rather than by keeping unmounted queries indexed. Every
 * invalidation bumps `clock` and stamps that reading onto each tag it touched;
 * every settle stamps the reading onto the query. A query mounting with a tag
 * whose stamp is newer than its own was invalidated in the meantime. The tag
 * stamps are one number per tag ever invalidated -- bounded by the API's tag
 * vocabulary, not by how many queries have ever mounted.
 */
export class QueryRegistry {
  private readonly entries = new Map<string, QueryEntry>();
  private readonly index = new Map<string, Set<QueryEntry>>();
  private readonly stamps = new Map<string, number>();
  private clock = 0;

  onStale: ((entry: QueryEntry) => void) | undefined;
  onUnresolved: ((template: string, entry: QueryEntry) => void) | undefined;

  constructor(options: QueryRegistryOptions = {}) {
    this.onStale = options.onStale;
    this.onUnresolved = options.onUnresolved;
  }

  /** Every query the registry remembers, mounted or not. */
  get size(): number {
    return this.entries.size;
  }

  /** How many distinct queries have at least one mount. */
  get mounted(): number {
    let count = 0;

    for (const entry of this.entries.values()) {
      if (entry.mounts > 0) count++;
    }

    return count;
  }

  /** How many tags currently have at least one mounted query. */
  get indexedTags(): number {
    return this.index.size;
  }

  get(key: string): QueryEntry | undefined {
    return this.entries.get(key);
  }

  /** The mounted queries carrying `tag`. Unmounted queries are never here. */
  queriesFor(tag: string): QueryEntry[] {
    const bucket = this.index.get(tag);

    return bucket === undefined ? [] : [...bucket];
  }

  /**
   * Mount a query, and return the undo.
   *
   * The same query mounted from three components is one entry with three
   * mounts, not three entries: the tag index must not fan one invalidation out
   * into three refetches of identical data. A second mount of a key that
   * already exists keeps the first spec -- the key is derived from operation
   * and arguments, so two specs sharing a key describe the same request.
   *
   * A query mounting for the first time is *not* marked stale. Fetching on
   * first mount belongs to the transport chunk, which owns whether a cached
   * value is fresh enough to serve. This chunk only reports queries that fell
   * behind something that happened.
   */
  mount(spec: QuerySpec): Unmount {
    const key = spec.key ?? queryKey(spec.operation, spec.args);
    let entry = this.entries.get(key);

    if (entry === undefined) {
      const args = spec.args ?? {};
      const provides = spec.provides ?? [];

      entry = {
        key,
        operation: spec.operation,
        args,
        provides,
        // Templates naming `{res.x}` cannot resolve before there is a
        // response, and reporting them here would fire a warning on every
        // mount of a perfectly good query. They resolve at settle, which is
        // the first moment every source exists.
        tags: new Set(resolveTags(provides, args).tags),
        deps: new Set(),
        mounts: 0,
        stale: false,
        value: undefined,
        settledAt: this.clock,
      };

      this.entries.set(key, entry);
    }

    entry.mounts++;

    if (entry.mounts === 1) {
      this.link(entry);

      if (this.invalidatedSince(entry)) this.markStale(entry);
    }

    let released = false;

    return () => {
      if (released) return;

      released = true;
      this.release(key);
    };
  }

  /**
   * Record what a query settled with, and re-derive its tags.
   *
   * The tag set is `provides` (now resolvable against the response) unioned
   * with the entity keys the response normalized to. Those keys are already
   * spelled `Type:id`, which is exactly the tag `PATCH /orders/7` invalidates,
   * so a list that loaded `Order:7` is reachable from a mutation to it with no
   * extra bookkeeping.
   */
  settle(key: string, result: SettleResult = {}): void {
    const entry = this.entries.get(key);

    if (entry === undefined) return;

    const resolved = resolveTags(entry.provides, { ...entry.args, response: result.response });

    for (const template of resolved.unresolved) this.onUnresolved?.(template, entry);

    if (result.deps !== undefined) entry.deps = new Set(result.deps);
    if ('value' in result) entry.value = result.value;

    this.retag(entry, new Set([...resolved.tags, ...entry.deps]));

    entry.stale = false;
    entry.settledAt = this.clock;
  }

  /**
   * Stamp `tags` as invalidated, and return the mounted queries each one hit.
   *
   * The value is a map rather than a flat list because placement callbacks are
   * declared per tag, so the caller has to know *which* of a query's tags
   * matched before it can ask whether every one of them was handled.
   */
  invalidated(tags: Iterable<string>): Map<QueryEntry, Set<string>> {
    this.clock++;

    const hits = new Map<QueryEntry, Set<string>>();

    for (const tag of tags) {
      this.stamps.set(tag, this.clock);

      const bucket = this.index.get(tag);

      if (bucket === undefined) continue;

      for (const entry of bucket) {
        let matched = hits.get(entry);

        if (matched === undefined) {
          matched = new Set<string>();
          hits.set(entry, matched);
        }

        matched.add(tag);
      }
    }

    return hits;
  }

  /**
   * Mark stale and, when someone is watching, report it.
   *
   * A query already dispatched but not yet settled is reported again rather
   * than suppressed. Whether two invalidations one tick apart can share one
   * request is a question about in-flight requests, which this chunk does not
   * own; suppressing here would answer it wrongly, by serving a response
   * fetched before the second write.
   */
  markStale(entry: QueryEntry): void {
    entry.stale = true;

    if (entry.mounts > 0) this.onStale?.(entry);
  }

  /**
   * A placement callback answered for this query, so no refetch is owed.
   *
   * The clock reading is stamped forward as if the query had settled, because
   * from the cache's point of view it has: the application supplied the value
   * the refetch would have produced. Without that stamp, the very invalidation
   * placement just handled would refetch the query on its next mount and undo
   * the point of the escape hatch.
   */
  place(entry: QueryEntry, value: unknown): void {
    entry.value = value;
    entry.stale = false;
    entry.settledAt = this.clock;
  }

  /**
   * Forget a query entirely, mounted or not.
   *
   * The garbage collection policy that decides *when* -- unreferenced for N
   * seconds, an LRU cap -- belongs to the query cache in a later chunk. This
   * is the operation it will drive.
   */
  drop(key: string): boolean {
    const entry = this.entries.get(key);

    if (entry === undefined) return false;

    if (entry.mounts > 0) this.unlink(entry);

    return this.entries.delete(key);
  }

  /** Drop everything, including the tag stamps. The identity-change path. */
  clear(): void {
    this.entries.clear();
    this.index.clear();
    this.stamps.clear();
  }

  private release(key: string): void {
    const entry = this.entries.get(key);

    if (entry === undefined || entry.mounts === 0) return;

    entry.mounts--;

    if (entry.mounts === 0) this.unlink(entry);
  }

  /** Whether any tag this query carries was invalidated after it last settled. */
  private invalidatedSince(entry: QueryEntry): boolean {
    for (const tag of entry.tags) {
      if ((this.stamps.get(tag) ?? 0) > entry.settledAt) return true;
    }

    return false;
  }

  private retag(entry: QueryEntry, tags: Set<string>): void {
    if (entry.mounts > 0) {
      for (const tag of entry.tags) {
        if (!tags.has(tag)) this.unlinkTag(entry, tag);
      }

      for (const tag of tags) {
        if (!entry.tags.has(tag)) this.linkTag(entry, tag);
      }
    }

    entry.tags = tags;
  }

  private link(entry: QueryEntry): void {
    for (const tag of entry.tags) this.linkTag(entry, tag);
  }

  private unlink(entry: QueryEntry): void {
    for (const tag of entry.tags) this.unlinkTag(entry, tag);
  }

  private linkTag(entry: QueryEntry, tag: string): void {
    let bucket = this.index.get(tag);

    if (bucket === undefined) {
      bucket = new Set<QueryEntry>();
      this.index.set(tag, bucket);
    }

    bucket.add(entry);
  }

  private unlinkTag(entry: QueryEntry, tag: string): void {
    const bucket = this.index.get(tag);

    if (bucket === undefined) return;

    bucket.delete(entry);

    // An emptied bucket is deleted rather than left behind. A `Map` holding
    // ten thousand empty `Set`s is the same leak as a bucket holding a
    // query nobody watches, arriving more slowly.
    if (bucket.size === 0) this.index.delete(tag);
  }
}
