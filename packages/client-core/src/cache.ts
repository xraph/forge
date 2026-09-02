import { Invalidator } from './invalidate.js';
import type { Placement, Scheduler } from './invalidate.js';
import type { CacheObserver } from './observe.js';
import { OverlayStack, specToPatches } from './overlay.js';
import type { OptimisticSpec } from './overlay.js';
import { QueryRegistry } from './registry.js';
import type { QueryEntry, QuerySpec, Unmount } from './registry.js';
import { EntityStore } from './store.js';
import type { StagedWrite } from './store.js';
import { queryKey, resolveTags } from './tags.js';
import type { TagContext } from './tags.js';
import { realClock } from './transport.js';
import type { OperationMeta, Transport } from './transport.js';
import type { EntityKey, EntitySchema } from './types.js';

/** Where a query is in its lifecycle. */
export type QueryStatus = 'idle' | 'pending' | 'success' | 'error';

/**
 * What a subscriber reads.
 *
 * **Referentially stable.** The same object is returned on every read until
 * something in it actually changes, because `useSyncExternalStore` tears when
 * `getSnapshot` returns a fresh object for unchanged state -- and `data` itself
 * is stable for the same reason, all the way down to the unchanged subtree
 * (see `EntityStore`).
 *
 * `data` survives an error: a refetch that fails leaves the last good value in
 * place with `status: 'error'` beside it, so an interface can show stale data
 * and a warning rather than an empty screen.
 */
export interface QueryState<T = unknown> {
  readonly status: QueryStatus;
  readonly data: T | undefined;
  readonly error: unknown;
  /** A request is in flight. True during a background refetch of good data. */
  readonly isFetching: boolean;
  /**
   * Some of this value is a local change the server has not confirmed.
   *
   * Computed here rather than in each adapter, so all three frameworks inherit
   * it from one implementation. For row-level treatment, read the `OPTIMISTIC`
   * symbol on the record itself: this flag is true for the whole query as soon
   * as any entity it reaches is overlaid.
   */
  readonly isOptimistic: boolean;
}

export interface QueryCacheOptions {
  readonly transport: Transport;
  /** The generated `entities` table. */
  readonly entities: EntitySchema;
  /** When invalidation batches run. Defaults to one batch per microtask. */
  readonly scheduler?: Scheduler;
  /**
   * How many queries to remember with nobody watching them, before the least
   * recently used are forgotten. Bounds both this cache and the registry.
   */
  readonly limit?: number;
  /** A refetch or a placement callback failed. */
  readonly onError?: (error: unknown, context: string) => void;
  /**
   * How many times one request sequence may be re-run because a stream frame
   * overtook it, before it commits around the frames instead.
   *
   * See `applyFrames`. The bound exists because an unbounded rule -- always
   * re-run -- livelocks against a channel busy enough that every attempt
   * straddles a frame commit, and a query that never settles is a worse defect
   * than the one being avoided. Zero disables re-running entirely.
   */
  readonly frameRestarts?: number;
  /** Reads the wall clock. Defaults to `realClock`. */
  readonly now?: () => number;
  /** Default milliseconds a result stays fresh. Defaults to `Infinity`. */
  readonly staleTime?: number;
}

/** Extra per-call knobs a generated hook may pass through. */
export interface RequestOptions {
  readonly headers?: Readonly<Record<string, string>>;
  readonly signal?: AbortSignal;
}

export interface MutateOptions extends RequestOptions {
  /** Per-tag placement callbacks. See `Placement`. */
  readonly place?: Readonly<Record<string, Placement>>;
  /**
   * What this mutation changes, shown immediately and reconciled on settle.
   *
   * A patch, never a value: it is re-applied on every refold, which is what
   * makes two concurrent mutations against one entity compose. See
   * `OptimisticSpec`. There is no rollback to write -- discarding an overlay
   * is inherently the undo.
   */
  readonly optimistic?: OptimisticSpec;
}

/** One settled query, as `dehydrate` reads it out of the cache. */
export interface CachedQuery {
  readonly key: string;
  readonly meta: OperationMeta;
  /**
   * The arguments that **reproduce `key`**, which is not always the arguments
   * the record stores. See `settledQueries`.
   */
  readonly args: TagContext | undefined;
  readonly skeleton: unknown;
}

/** What `QueryCache.restore` installs. See that method. */
export interface RestoreInput {
  readonly skeleton: unknown;
  /** Resolved tags, for a payload that carries no response. */
  readonly tags?: Iterable<string>;
  /** The response, for a payload that does. Ignored when `tags` is present. */
  readonly response?: unknown;
  /** Settle behind the server, so a mount refetches. */
  readonly stale?: boolean;
}

/**
 * The stream runtime, as the cache and the framework adapters see it.
 *
 * `StreamBinder` satisfies this, and is the only thing that ever does. It is
 * declared here rather than imported because of where the two live in the
 * bundle: `live.ts` pulls in the whole streams layer, and an adapter that
 * imported `StreamBinder` to call `subscribe` on it would put 2.4 kB of frame
 * decoding into every REST-only application that happens to use `useQuery`.
 * A structural interface in this file costs nothing at runtime and is erased
 * entirely by the compiler.
 */
export interface LiveBinding {
  /**
   * Make one query live, and return the release. See `StreamBinder.subscribe`.
   *
   * Ref-counted twice over: per query, so two components on the same live
   * query are one subscription, and per socket underneath, so two *different*
   * live queries whose entities ride the same channel are one connection.
   */
  subscribe(meta: OperationMeta, args?: TagContext): () => void;
  /** Which channels this operation's entities are pushed on. */
  channelsFor(meta: OperationMeta): readonly string[];
}

/**
 * One tracked query, as an inspector sees it.
 *
 * A structural view over `Record_`, declared the way `LiveBinding` is and for
 * the same reason: the compiler erases it, so naming the shape costs a
 * production bundle nothing. It exists because the registry knows what a query
 * *provides* and only the record knows what it is *doing* -- erroring,
 * fetching, restarted by a frame that overtook its response.
 *
 * Handed out live, on the same contract as `QueryRegistry.all()`: treat it as
 * read-only. `@forge-go/client-devtools` copies before it returns anything.
 */
export interface TrackedRecord {
  readonly key: string;
  readonly meta: OperationMeta;
  readonly args: TagContext;
  readonly status: QueryStatus;
  readonly error: unknown;
  readonly fetching: boolean;
  readonly settled: boolean;
  /** A request sequence is in flight. */
  readonly inflight: Promise<unknown> | undefined;
  /** An invalidation landed mid-flight and the answer in progress predates it. */
  readonly restart: boolean;
  /** How many times a stream frame has overtaken this query's request. */
  readonly frameRestarts: number;
}

/** One query the cache is tracking. Private; `QueryState` is what escapes. */
interface Record_ {
  readonly key: string;
  readonly meta: OperationMeta;
  readonly args: TagContext;
  readonly spec: QuerySpec;
  readonly listeners: Map<() => void, number>;
  /** The last response's skeleton. Meaningless until `settled`. */
  skeleton: unknown;
  settled: boolean;
  /** When this record last settled, on the injected clock. Zero until then. */
  settledTime: number;
  status: QueryStatus;
  error: unknown;
  fetching: boolean;
  /** The retry sequence in progress, shared by everyone who asked for it. */
  inflight: Promise<unknown> | undefined;
  /** Identifies that sequence, so an abandoned one can recognise itself. */
  run: number;
  /** An invalidation landed mid-flight; the answer in progress predates it. */
  restart: boolean;
  /** How many times a stream frame has already overtaken this sequence. */
  frameRestarts: number;
  /**
   * Same, but placement already supplied the answer, so re-running would only
   * spend a request confirming what the cache already knows.
   */
  discard: boolean;
  /** The registry mount held while anyone is listening. */
  unmount: Unmount | undefined;
  /** The last state object handed out, kept so identity survives a re-read. */
  state: QueryState | undefined;
}

/**
 * The query cache: the entity store, the tag graph and a transport, wired.
 *
 * Running a query means: issue the request, normalize the response into the
 * entity store, hand the registry the skeleton's dependencies so invalidation
 * can find this query again, and return the rehydrated value. Nothing here
 * caches a document; the cached thing is a skeleton of references, and reads
 * of it are memoized by the store, so the value a subscriber gets keeps its
 * object identity for as long as the entities under it do not move.
 *
 * Two properties that are easy to lose and expensive to debug:
 *
 * - **One request per query, not one per subscriber.** Ten components mounting
 *   `useOrderList()` in the same tick share one retry sequence and settle
 *   together. The shared unit is the whole sequence, not an attempt: nobody is
 *   resolved from a failed attempt while somebody else waits for the retry.
 * - **A response that predates a write never settles the query it was for.**
 *   An invalidation arriving mid-flight sets `restart`, and the sequence runs
 *   again rather than committing an answer the server produced before the
 *   write it is supposed to reflect.
 */
export class QueryCache {
  readonly store = new EntityStore();
  readonly registry = new QueryRegistry();
  readonly invalidator: Invalidator;
  /**
   * Pending optimistic changes, layered over the store rather than written
   * into it. Public so an application can ask whether anything is unconfirmed.
   */
  readonly overlays: OverlayStack;

  private readonly records = new Map<string, Record_>();
  /**
   * The generated `entities` table this cache normalizes against.
   *
   * Public so the stream layer can normalize a frame payload against the same
   * schema without a second copy of it being threaded through. Treat it as
   * frozen.
   */
  readonly entities: EntitySchema;

  /**
   * The stream runtime, when the application wired one up.
   *
   * Assigned by `StreamBinder`'s constructor rather than passed in, because
   * the binder is built *from* a cache and the two would otherwise have to be
   * constructed in a cycle. `undefined` means a REST-only application, which
   * is the majority of them and not an error.
   *
   * A second binder over the same cache replaces the first, exactly as it
   * replaces the manager's `onReconnect` -- see the note on `StreamBinder`.
   */
  live: LiveBinding | undefined;

  /**
   * Where the cache reports what it did. Undefined in every production wiring.
   *
   * The whole of the devtools seam; see `CacheObserver` for what it costs when
   * unset (one property load and one nullish check per emit site, no
   * allocation) and why the cache keeps no history of its own. Declared without
   * an initializer on purpose: under this package's `target`, that emits no
   * code at all.
   */
  observer: CacheObserver | undefined;

  private readonly transport: Transport;
  private readonly limit: number;
  private readonly now: () => number;
  private readonly staleTime: number;
  private readonly frameRestartLimit: number;
  private readonly onError: ((error: unknown, context: string) => void) | undefined;

  /** Stamps each request sequence. See `start`. */
  private runs = 0;

  /**
   * The frame-clock reading of every request currently in flight, by token.
   *
   * A map rather than a running minimum, because requests do not finish in the
   * order they started: the earliest dispatch has to be recomputable when the
   * request holding it lands, and a single number cannot be un-set.
   *
   * What it is for is `EntityStore#expireTombstones`. A tombstone is only read
   * by a response that straddles the delete, so knowing the oldest dispatch
   * still outstanding is knowing exactly which stamps can go.
   */
  private readonly dispatches = new Map<number, number>();

  private dispatchToken = 0;

  /** Who the cached data belongs to. See `setPrincipal`. */
  private principal: unknown;

  /** Told when that changes. See `watchPrincipal`. */
  private readonly principals = new Set<(principal: unknown) => void>();

  constructor(options: QueryCacheOptions) {
    this.transport = options.transport;
    this.entities = options.entities;
    this.limit = options.limit ?? 128;
    this.now = options.now ?? realClock;
    this.staleTime = options.staleTime ?? Infinity;
    this.frameRestartLimit = options.frameRestarts ?? 3;
    this.onError = options.onError;

    this.overlays = new OverlayStack(this.store, (error, context) => this.report(error, context));
    this.store.overlays = this.overlays;

    this.invalidator = new Invalidator(this.registry, {
      execute: (batch) => this.refetchAll(batch),
      ...(options.scheduler === undefined ? {} : { scheduler: options.scheduler }),
      ...(options.onError === undefined ? {} : { onError: options.onError }),
      onPlace: (entry, value) => {
        this.observer?.({ type: 'placed', key: entry.key });
        this.adopt(entry, value);
      },
      onInvalidated: (entry, matched) => {
        this.observer?.({ type: 'invalidated', key: entry.key, matched });
        this.stale(entry);
      },
    });
  }

  /** How many queries are being tracked, watched or merely remembered. */
  get size(): number {
    return this.records.size;
  }

  /** The cache key this operation and these arguments resolve to. */
  key(meta: OperationMeta, args?: TagContext): string {
    return queryKey(operationName(meta), args);
  }

  /**
   * Watch a query, fetching it if there is nothing to show yet.
   *
   * The returned function is the unsubscribe. The registry sees one mount per
   * *query*, not one per listener: the tag index exists to stop one
   * invalidation fanning out into N refetches of identical data, and counting
   * listeners there would put it back.
   */
  subscribe(
    meta: OperationMeta,
    args: TagContext | undefined,
    listener: () => void,
    options?: { readonly staleTime?: number },
  ): () => void {
    const record = this.open(meta, args);

    record.listeners.set(listener, options?.staleTime ?? meta.staleTime ?? this.staleTime);

    if (record.listeners.size === 1) record.unmount = this.registry.mount(record.spec);

    if ((!record.settled || this.expired(record)) && record.inflight === undefined) {
      this.detach(this.start(record));
    }

    let released = false;

    return () => {
      if (released) return;

      released = true;
      record.listeners.delete(listener);

      if (record.listeners.size > 0) return;

      record.unmount?.();
      record.unmount = undefined;
      this.reap();
    };
  }

  /** This query's current state. Stable across reads while nothing changes. */
  getState<T = unknown>(meta: OperationMeta, args?: TagContext): QueryState<T> {
    return this.snapshot(this.open(meta, args)) as QueryState<T>;
  }

  /**
   * This query's state **without opening a record for it.**
   *
   * `getState` routes through `open`, which creates the record if it is new --
   * correct for a subscriber, wrong for two callers that must have no side
   * effects. A server render asks about every query on the page, including ones
   * this request never fetched, and on a server the cache may be shared between
   * concurrent requests; and `dehydrate` reads a cache rather than using it.
   * `undefined` means nothing is cached, which is a different answer from
   * `idle`.
   *
   * Deliberately does not move the record's LRU position either. A peek is not
   * a use, and letting a server render reorder the eviction queue would make
   * which query gets evicted depend on the order components happened to render.
   */
  peek<T = unknown>(meta: OperationMeta, args?: TagContext): QueryState<T> | undefined {
    const record = this.records.get(this.key(meta, args));

    if (record === undefined) return undefined;

    return this.snapshot(record) as QueryState<T>;
  }

  /**
   * When this query last settled, on the injected clock, or `undefined` if it
   * never has.
   *
   * Narrow on purpose. `Record_` is private and stays private; this exposes
   * the one field an inspector and a test need, without widening `tracked()`
   * into a second way to reach the record.
   */
  settledTimeOf(meta: OperationMeta, args?: TagContext): number | undefined {
    const record = this.records.get(this.key(meta, args));

    return record?.settled === true ? record.settledTime : undefined;
  }

  /**
   * The staleTime that governs this record right now.
   *
   * The minimum across live subscribers, because two components may mount one
   * query with different values and the stricter of them is the one that has
   * to be honoured. With nothing watching there is no call layer to read, so
   * it falls through to the manifest and then the cache default.
   */
  private staleTimeOf(record: Record_): number {
    if (record.listeners.size === 0) return record.meta.staleTime ?? this.staleTime;

    let min = Infinity;

    for (const value of record.listeners.values()) if (value < min) min = value;

    return min;
  }

  /** `staleTimeOf` for a query named from outside. Exposed for tests and devtools. */
  effectiveStaleTime(meta: OperationMeta, args?: TagContext): number | undefined {
    const record = this.records.get(this.key(meta, args));

    return record === undefined ? undefined : this.staleTimeOf(record);
  }

  /**
   * Whether time has made this record stale.
   *
   * The clock is read only when a finite staleTime is in play. That ordering
   * is deliberate and load-bearing: at the default of `Infinity` this costs one
   * comparison and never touches `now`, which is what lets a test pin the
   * default with a clock that throws.
   */
  private expired(record: Record_): boolean {
    const staleTime = this.staleTimeOf(record);

    return staleTime !== Infinity && this.now() - record.settledTime > staleTime;
  }

  /**
   * Every query this cache is tracking, for an inspector.
   *
   * One map read, no copy, and no `open`: reading this does not create a
   * record and does not touch the LRU order. `QueryRegistry.all()` is the same
   * shape for the same reason, and costs the same nine bytes.
   */
  tracked(): IterableIterator<TrackedRecord> {
    return this.records.values();
  }

  /**
   * Every query that settled successfully, as `dehydrate` reads them.
   *
   * Pending and failed queries are absent by construction. A pending query has
   * no skeleton to serialize, and a failed one would hydrate a client into a
   * failure the server observed and the client cannot meaningfully retry --
   * both are better left for the client to fetch normally.
   *
   * `args` is reported as whatever **reproduces the key**, which is not always
   * `record.args`. `open` derives the key from the caller's arguments and then
   * stores `args ?? {}`, and `queryKey` tells those two apart: a query fetched
   * as `fetch(orderList)` is keyed `GET /orders` while its record holds `{}`,
   * which re-derives as `GET /orders|{}`. A consumer that round-trips the
   * arguments and re-derives the key -- which is exactly what `hydrate` does,
   * so that a key-scheme change cannot desynchronise a server from a client --
   * would otherwise land on a second, empty record and never find this one.
   */
  settledQueries(): CachedQuery[] {
    const out: CachedQuery[] = [];

    for (const record of this.records.values()) {
      if (!record.settled || record.status !== 'success') continue;

      out.push({
        key: record.key,
        meta: record.meta,
        args: this.key(record.meta, undefined) === record.key ? undefined : record.args,
        skeleton: record.skeleton,
      });
    }

    return out;
  }

  /**
   * Settle a query from a skeleton, with no request behind it.
   *
   * The seam `hydrate` writes through. Everything a settle normally does apart
   * from the request: install the skeleton, mark the record successful, record
   * the dependencies, retag the registry entry and notify.
   *
   * `deps` are recomputed from the skeleton against the live store rather than
   * carried in a payload -- `dependencies` is exact, costs one memoized walk,
   * and does not have to trust what arrived over the wire.
   *
   * Merges rather than replaces. A query the cache already holds is re-settled
   * against the hydrated skeleton, and the records behind it went through `put`,
   * which keeps the previous object for identical data. Hydrating the same
   * payload twice therefore moves no version and changes no identity.
   */
  restore(meta: OperationMeta, args: TagContext | undefined, input: RestoreInput): void {
    const record = this.open(meta, args);

    record.skeleton = input.skeleton;
    record.settled = true;
    record.settledTime = this.now();
    record.status = 'success';
    record.error = undefined;
    record.fetching = false;

    // `base`, not `value`, for the same reason `settle` uses it: `entry.value`
    // is what a placement callback is handed as `current`, and it must be
    // entity-plane only rather than membership-projected.
    this.registry.settle(record.key, {
      value: this.base(record),
      deps: this.store.dependencies(input.skeleton),
      ...(input.tags === undefined ? {} : { tags: input.tags }),
      ...(input.response === undefined ? {} : { response: input.response }),
    });

    const entry = this.registry.get(record.key);

    if (input.stale === true && entry !== undefined) this.registry.markStale(entry);

    this.notify(record);
  }

  /**
   * Run this query, or join the request already running for it.
   *
   * Serves the cached value without a request when the query has settled and
   * nothing has invalidated it since. Prefetching and SSR use this; so does a
   * subscriber that mounts onto a warm cache.
   */
  fetch<T = unknown>(meta: OperationMeta, args?: TagContext): Promise<T> {
    const record = this.open(meta, args);
    const entry = this.registry.get(record.key);

    if (record.settled && record.inflight === undefined && entry?.stale !== true) {
      return Promise.resolve(this.value(record) as T);
    }

    return this.start(record) as Promise<T>;
  }

  /** Run this query again whatever the cache holds. */
  refetch<T = unknown>(meta: OperationMeta, args?: TagContext): Promise<T> {
    return this.restart(this.open(meta, args)) as Promise<T>;
  }

  /**
   * Run a mutation, commit what it returned, and invalidate what it declared.
   *
   * The order matters. The response is normalized *first*, so an entity the
   * mutation changed is already current when placement callbacks run and when
   * refetched queries rehydrate. Placement then gets the freshly rehydrated
   * entity as `created` -- the same object identity the store holds -- so
   * `[created, ...current]` produces a list the store recognises as unchanged
   * data rather than as a new record.
   */
  async mutate<T = unknown>(
    meta: OperationMeta,
    args: TagContext = {},
    options: MutateOptions = {},
  ): Promise<T> {
    const overlay = this.push(meta, args, options);
    // Stamped exactly as a query is, for exactly the same reason: a frame that
    // lands while this write is in flight is newer than the answer coming back,
    // and committing the response wholesale would silently undo it.
    const dispatchedAt = this.store.frameVersion;

    // Retired below, after the response has committed rather than when it
    // arrives. The `racedSince` calls further down read the tombstones this
    // registration holds open, so releasing it early would let `landed` expire
    // the stamp this very response is about to consult.
    const token = this.dispatched(dispatchedAt);

    let response: unknown;

    try {
      response = await this.transport.execute({
        meta,
        args,
        ...(options.headers === undefined ? {} : { headers: options.headers }),
        ...(options.signal === undefined ? {} : { signal: options.signal }),
      });
    } catch (error) {
      this.landed(token);

      // Base was never touched, so nothing is owed: no tags are raised and no
      // refetch is scheduled. Dropping before the rethrow means `mutateAsync`'s
      // rejection and `mutate`'s deliberate swallow both observe a clean cache.
      if (overlay !== undefined) {
        this.overlays.take(overlay);
        this.refresh(true);
      }

      throw error;
    }

    const staged = this.store.stage(response, this.entities, rootTypeOf(meta));
    const skip = new Set(this.store.racedSince(staged.records.keys(), dispatchedAt));

    // Taken BEFORE it is promoted, so a computed merge is evaluated against
    // base alone. Promoting while the overlay is still folded would apply it
    // twice -- once by the write and once by the fold on top of it.
    if (overlay !== undefined) {
      const entry = this.overlays.take(overlay);

      // Without this, a 204 delete flashes the row back between settle and
      // refetch: the overlay is gone and the response carries nothing to
      // replace it. Promotion makes the confirmed change permanent, and the
      // response commits over it, so the server still has the last word.
      //
      // A promoted overlay is a committed write, so the same ordering
      // guarantee that withholds the response's answer for a raced key
      // withholds the local guess for it too. The frame clock is read a second
      // time here rather than reusing `skip`: that set covers the keys the
      // RESPONSE carries, and an overlay is free to patch an entity the
      // response says nothing about, which a frame can overtake just as
      // easily.
      if (entry !== undefined) {
        const overtaken = new Set(this.store.racedSince(entry.patches.keys(), dispatchedAt));

        for (const key of this.overlays.promote(entry, overtaken)) skip.add(key);
      }
    }

    // **Never re-run.** This is where the mutation path and the query path
    // deliberately diverge. A query that lost the race is re-issued, because
    // re-reading is free and idempotent. Re-issuing a *write* is the
    // duplicate-orders hazard the transport's retry policy is careful about:
    // the client cannot distinguish a request the server never saw from one it
    // processed, and a POST sent twice is two orders.
    //
    // So the response commits around the frames instead. Every entity the
    // frame did not touch lands normally; the ones it did keep the frame's
    // value, and `created` below is read back out of the store, so the caller
    // is handed the current truth rather than its own superseded write.
    //
    // "Keep the frame's value" covers an optimistic mutation as well, and only
    // because promotion above honours the same clock. Withholding the server's
    // answer for a raced key while promoting the client's guess for it would
    // hand the frame's row a value the server never sent and the user merely
    // predicted -- a worse outcome than the stale commit `skip` exists to
    // prevent, because nothing later contradicts it.
    this.store.commit(staged, skip.size === 0 ? {} : { skip });

    // ...unless the frame that won was a *delete*, in which case there is no
    // current truth to read. The record is gone, so rehydrating the skeleton
    // resolves the mutation's own entity to nothing: a scalar root returns
    // `undefined` typed as `T`, and an array root silently loses an element.
    //
    // Reading a corpse is bad enough as a return value; handing it to placement
    // is worse. `[created, ...current]` becomes `[undefined, {...}]`, `adopt`
    // re-normalizes that, and a literal `undefined` is deliberately *not* a
    // hole -- only a dangling reference is -- so it survives into the rendered
    // value and the next `data.map(o => o.id)` throws. That is precisely the
    // exception the eviction fix removed, reintroduced through a narrower door.
    //
    // So a raced key the store no longer holds is treated as what it is, a
    // delete: hand back what the server actually said, and decline placement.
    //
    // Declining is load-bearing rather than cautious, and for a second reason
    // beyond the hole. `adopt` re-normalizes whatever a placement callback
    // returns straight back into the store, with no frame stamp and no skip --
    // so placing the response would **resurrect the deleted entity**, which is
    // measurably worse than the `undefined` this branch started out fixing.
    // Declining costs nothing: `undefined` from placement already means
    // "refetch instead", and the eviction frame synthesized `${entity}[]` on
    // its way through, so those lists are already being refetched.
    //
    // Checked against the FINAL `skip`, not the frame-race snapshot taken
    // above it -- `skip` by this point also carries every key an optimistic
    // `promote` evicted. An optimistic delete is exactly the same shape of
    // problem: the mutation's own root is gone, so `staged.skeleton` resolves
    // to a hole for it too, and a `place` callback handed `[undefined,
    // ...current]` reintroduces the identical defect through a second door.
    const buried = [...skip].some((key) => !this.store.has(key));
    const created = buried ? response : this.store.read(staged.skeleton);

    // The last read of `skip`, and therefore of the tombstones behind it, is
    // above. Safe to retire the dispatch now: everything below is placement
    // and notification, which consult the store rather than the frame clock.
    this.landed(token);

    // Placement is handed each query's `current` value, which lives on the
    // registry entry and is only as fresh as the last read of it. Refreshing
    // every tracked query here costs one memoized store read each and removes
    // the class of bug where a placement callback prepends to a list that a
    // previous write already changed underneath it.
    //
    // **Notifying** is not an extra: it is the only chance these subscribers
    // get. A response commits entities that no invalidated tag reaches -- a
    // create declaring `Order[]` returns the new `Order:9`, and a query already
    // displaying `Order:9` is in none of the tags being refetched. This line
    // is the only place that read happens, so refreshing silently *consumes*
    // the change: `record.state` advances, and every later `notifyChanged()`
    // compares the new state against the state it already advanced to, finds
    // them equal, and reports nothing. The query is then permanently stale
    // against a cache that holds the right answer.
    //
    // It was silent for as long as it was because a `useSyncExternalStore`
    // consumer re-reads `getSnapshot` during *every* render, so React's own
    // re-render for the mutation's `pending -> success` usually papered over
    // it. A consumer holding the last snapshot it was handed -- Vue's
    // `shallowRef`, an Angular signal -- has nothing to paper over it with.
    //
    // Notification is by state-object identity, so this costs a render only in
    // the queries whose rendered value actually moved. The ones that are also
    // about to be refetched by `settled` below notify again when they settle,
    // which is a second render of a value that did change, not a spurious one.
    this.refresh(true);

    // Before the tags are applied, so an observer sees the cause ahead of every
    // `invalidated` it explains. The response is handed over for the duration of
    // this synchronous call and nothing in the core retains it.
    this.observer?.({ type: 'mutation', meta, args, response });

    this.invalidator.settled({
      invalidates: meta.invalidates,
      args,
      response,
      created,
      ...(options.place === undefined || buried ? {} : { place: options.place }),
    });

    return created as T;
  }

  /**
   * Push this mutation's declared change, if it declared one.
   *
   * Returns the overlay id, or `undefined` when nothing was pushed -- either
   * because the caller asked for nothing or because the target could not be
   * derived, which `specToPatches` has already reported.
   *
   * Everything application code can reach from here -- `onError` through the
   * report callback, a subscriber's listener through `refresh` -> `notify` --
   * is guarded against throwing. This runs BEFORE `transport.execute`, unlike
   * every other `refresh(true)` in this file, and every framework adapter
   * swallows `mutate`'s rejection by design: an unguarded throw here would
   * reject the returned promise before the request went out, and the write
   * would silently not happen. Not being optimistic is a small failure;
   * silently not writing is not. The overlay itself is added first and its id
   * captured before any of the guarded calls run, and is always returned --
   * stranding it unreturned would leave it on the stack with nothing able to
   * `take` it off, a permanent phantom value, which is worse than the throw
   * this guard exists to contain.
   */
  private push(
    meta: OperationMeta,
    args: TagContext,
    options: MutateOptions,
  ): number | undefined {
    if (options.optimistic === undefined) return undefined;

    const resolved = specToPatches(
      options.optimistic,
      meta,
      args,
      this.entities,
      () => this.overlays.mint(),
      (error, context) => this.safeReport(error, context),
    );

    if (resolved === undefined) return undefined;

    const { tags } = resolveTags(meta.invalidates, { ...args });
    const id = this.overlays.add(resolved.patches, options.place, tags, resolved.created);

    try {
      this.refresh(true);
    } catch (error) {
      this.safeReport(error, 'optimistic');
    }

    return id;
  }

  /**
   * Report a failure, tolerating a throwing `onError`.
   *
   * The one caller that needs this is `push`, which runs before dispatch --
   * see its docblock. `report` itself stays unguarded everywhere else in this
   * file: every other call site runs after the write, where a throw is a
   * defect in application code the caller is meant to see, not one this
   * runtime has to survive on the write's behalf.
   */
  private safeReport(error: unknown, context: string): void {
    try {
      this.report(error, context);
    } catch {
      // Nowhere further to send it that would not risk the exact same
      // failure again, so it is dropped rather than compounded.
    }
  }

  /**
   * Subscribe this query to the channels its entities are pushed on, and
   * return the release. The whole of what `{live: true}` does.
   *
   * Every framework adapter routes through here rather than through
   * `this.live` directly, for the one reason worth a method: a call site that
   * asked for `live` against a client with no stream runtime attached must not
   * *silently* be given a query that never updates. That is the failure this
   * layer exists to make impossible, and it is indistinguishable from a quiet
   * backend by inspection. Reported through the cache's own error channel and
   * then survived, because the query itself is perfectly good -- it simply
   * fetches rather than streams.
   */
  watchLive(meta: OperationMeta, args?: TagContext): () => void {
    const live = this.live;

    if (live === undefined) {
      this.report(
        new Error(`[forge] live: no stream runtime attached for ${meta.method} ${meta.path}`),
        'live',
      );

      return () => undefined;
    }

    return live.subscribe(meta, args);
  }

  /** Invalidate already-resolved tags, as a stream frame or a manual refresh would. */
  invalidate(tags: Iterable<string>): void {
    this.invalidator.invalidate(tags);
  }

  /**
   * Something wrote to the store behind this cache's back: re-read every
   * tracked query and notify the ones whose value actually moved.
   *
   * The seam the stream layer commits through, and the reason `applyFrames`
   * can live outside this class without becoming a second apply path. It is
   * needed because a `patch` frame invalidates nothing and refetches nothing:
   * the store has the new value and the memos are rebuilt around it, but no
   * request is owed, so nothing would ever settle and nothing would ever tell
   * the subscribers. Also the honest entry point for an application that
   * writes to `cache.store` directly.
   *
   * Notification is by state-object identity, which `snapshot` makes exact --
   * thirty-nine mounted queries that did not move cost one memoized read each
   * and no render.
   */
  notifyChanged(): void {
    this.refresh(true);
  }

  /** Report a failure through the cache's own error channel. */
  /**
   * Register a request as outstanding, at the frame reading it dispatched on.
   *
   * Returns the token `landed` takes back. A token rather than the stamp
   * itself, because two requests can dispatch on the same frame reading and
   * removing one must not remove the other.
   */
  private dispatched(at: number): number {
    const token = ++this.dispatchToken;

    this.dispatches.set(token, at);

    return token;
  }

  /**
   * That request is done, whether it succeeded or not.
   *
   * A failure retires a dispatch exactly as a success does: what a tombstone
   * is guarding against is a response arriving late, and a request that threw
   * is not going to arrive at all.
   */
  private landed(token: number): void {
    this.dispatches.delete(token);

    let oldest = Infinity;

    for (const at of this.dispatches.values()) oldest = Math.min(oldest, at);

    this.store.expireTombstones(oldest);
  }

  report(error: unknown, context: string): void {
    this.onError?.(error, context);
  }

  /**
   * Who the cached data belongs to.
   *
   * Read by the stream layer, which has to be able to drop a frame that was
   * decoded for the previous principal.
   */
  get owner(): unknown {
    return this.principal;
  }

  /** Be told when the identity changes, after the cache has been dropped. */
  watchPrincipal(listener: (principal: unknown) => void): () => void {
    this.principals.add(listener);

    return () => {
      this.principals.delete(listener);
    };
  }

  /**
   * Declare who the cached data belongs to, dropping everything on a change.
   *
   * This is a correctness property, not a convenience. A normalized store keys
   * `Order:7` globally, with no memory of who fetched it; without partitioning,
   * the next principal's `useOrder(7)` renders the previous principal's record
   * before -- or instead of -- the server ever answers. Document caches do not
   * have this defect, which is exactly why it has to be handled here.
   *
   * Queries with listeners are re-mounted and refetched, so an interface that
   * survives a login change repopulates rather than freezing on whatever it
   * had. Their in-flight requests are abandoned: a response for the previous
   * principal that lands after the switch is dropped rather than committed.
   */
  setPrincipal(principal: unknown): void {
    if (principal === this.principal) return;

    this.principal = principal;
    this.clear();

    // After the clear, not before: a watcher that tears sockets down and puts
    // them back must not do so against a store that is still holding the
    // previous principal's entities.
    for (const listener of [...this.principals]) {
      try {
        listener(principal);
      } catch (error) {
        this.onError?.(error, 'principal');
      }
    }
  }

  /** Drop every entity, every skeleton and every registry entry. */
  clear(): void {
    const tracked = [...this.records.values()];

    // Every request out, watched or not, is abandoned before anything else --
    // `start` drops a response whose record no longer holds its promise. An
    // unwatched query left running would otherwise normalize the previous
    // principal's response into the store that was just emptied, which is
    // precisely the leak this method exists to prevent.
    for (const record of tracked) {
      record.inflight = undefined;
      // 0 is never a live run: `start` pre-increments, so the first is 1.
      record.run = 0;
    }

    const watched = tracked.filter((record) => record.listeners.size > 0);

    // A pending edit belongs to the identity that made it.
    this.overlays.clear();
    this.store.clear();
    this.registry.clear();
    this.records.clear();

    for (const record of watched) {
      record.skeleton = undefined;
      record.settled = false;
      record.status = 'pending';
      record.error = undefined;
      record.fetching = false;
      record.restart = false;
      record.frameRestarts = 0;
      record.discard = false;
      record.state = undefined;

      this.records.set(record.key, record);
      record.unmount = this.registry.mount(record.spec);

      this.notify(record);
      this.detach(this.start(record));
    }
  }

  /**
   * Forget one query, or reset it if somebody is watching.
   *
   * `clear()` narrowed to a single key, deliberately with the same two
   * behaviours it already has. An unwatched query is deleted outright and its
   * entities collected, which is what `reap` does. A watched one cannot be
   * deleted without orphaning the mount its subscribers hold, so it is reset
   * in place to the state a fresh mount would find and re-run -- exactly what
   * `clear()` does to the watched records it keeps.
   *
   * The abandonment comes first in both paths: `start` drops a response whose
   * record no longer holds its promise, so a request already in flight cannot
   * land in a record that has been reset underneath it.
   *
   * Returns false when nothing was tracking that key.
   */
  drop(key: string): boolean {
    const record = this.records.get(key);

    if (record === undefined) return false;

    record.inflight = undefined;
    // 0 is never a live run: `start` pre-increments, so the first is 1.
    record.run = 0;

    if (record.listeners.size === 0) {
      this.records.delete(key);
      this.registry.drop(key);
      this.collect();

      return true;
    }

    record.skeleton = undefined;
    record.settled = false;
    record.status = 'pending';
    record.error = undefined;
    record.fetching = false;
    record.restart = false;
    record.frameRestarts = 0;
    record.state = undefined;

    this.detach(this.start(record));

    return true;
  }

  /**
   * Throw away the answer in flight without running another request.
   *
   * The placement path. The application already supplied the value the refetch
   * would have produced, so re-running would spend a request confirming what
   * the cache knows -- which is the cost the escape hatch exists to avoid. The
   * promise resolves with the placed value, so a caller awaiting the refetch
   * that placement pre-empted gets the current answer rather than a rejection.
   */
  private discardInflight(record: Record_): unknown {
    record.discard = false;
    record.restart = false;
    record.inflight = undefined;
    record.fetching = false;

    this.notify(record);

    return this.value(record);
  }

  /** The record for this query, created if it is new. */
  private open(meta: OperationMeta, args: TagContext | undefined): Record_ {
    const key = this.key(meta, args);
    const existing = this.records.get(key);

    if (existing !== undefined) {
      // Move to the end: `reap` evicts from the front, so this is the LRU
      // ordering rather than a first-created-first-dropped one.
      this.records.delete(key);
      this.records.set(key, existing);

      return existing;
    }

    const resolved = args ?? {};
    const spec: QuerySpec = {
      operation: operationName(meta),
      args: resolved,
      provides: meta.provides,
      key,
    };

    const record: Record_ = {
      key,
      meta,
      args: resolved,
      spec,
      listeners: new Map(),
      skeleton: undefined,
      settled: false,
      settledTime: 0,
      status: 'idle',
      error: undefined,
      fetching: false,
      inflight: undefined,
      run: 0,
      restart: false,
      frameRestarts: 0,
      discard: false,
      unmount: undefined,
      state: undefined,
    };

    // Before the insert, not after: `reap` evicts from the front of the map
    // and this record would be at the back, but a cap of N with every other
    // record watched would otherwise evict the one just asked for.
    this.reap();
    this.records.set(key, record);

    // Create the registry entry now and release it immediately, so a query
    // that is fetched before it is ever watched -- a prefetch, an SSR pass --
    // still has somewhere for `settle` to record its dependencies. Without it
    // the first mount would find an entry with no deps and no tags beyond
    // `provides`, and a mutation to an entity that query displays would not
    // reach it.
    this.registry.mount(spec)();

    return record;
  }

  /**
   * Start a request, or hand back the one already running.
   *
   * The loop is the restart path: an invalidation that lands while this is in
   * flight makes the answer in progress stale before it arrives, so it is
   * thrown away and the sequence runs again. Everyone waiting stays on the
   * same promise, so a query is never resolved from a response that predates a
   * write it was supposed to observe.
   *
   * The sequence is deferred by a microtask rather than started inline. A
   * `Transport` is free to throw *synchronously* -- `RestTransport` cannot,
   * being `async`, but the interface is the declared seam for the stream
   * transports still to come -- and an inline body would run its own `catch`
   * before `record.inflight` had been assigned, leaving the rejected promise
   * installed as the in-flight request forever. Every later `fetch` would then
   * be served that first failure with no request made.
   */
  private start(record: Record_): Promise<unknown> {
    const running = record.inflight;

    if (running !== undefined) return running;

    record.fetching = true;

    if (!record.settled) record.status = 'pending';

    // Identifies this sequence, so a response that arrives after the cache was
    // cleared can tell that it no longer belongs to anything. Comparing
    // against `record.inflight` would say the same thing, but the promise does
    // not exist yet at the point the closure below needs to name it.
    const run = ++this.runs;
    record.run = run;

    const sequence = async (): Promise<unknown> => {
      record.frameRestarts = 0;

      for (;;) {
        record.restart = false;
        record.discard = false;

        // TWO readings, of two different clocks, for two different races. They
        // look alike deliberately -- read before dispatch, compare on arrival --
        // and neither subsumes the other.
        //
        // `dispatchedAt` is the entity store's frame version, which only a
        // stream frame advances. It answers "did a frame overtake this response
        // for some entity it carries", per key, and the remedy is to commit
        // around those keys. See the ordering guarantee on `applyFrames`.
        //
        // `startedAt` is the registry's invalidation clock, which only a
        // mutation's tags advance. It answers "was this query invalidated while
        // this request was out", per query, and the remedy is to settle as
        // stale so the next mount refetches.
        //
        // A stream frame raises no tags and a mutation bumps no frame version,
        // so a response can lose either race independently -- or both.
        const dispatchedAt = this.store.frameVersion;

        // Read per attempt, not once per sequence: a restart issues a genuinely
        // new request, and settling it against the abandoned attempt's reading
        // would report the fresh answer as behind the very invalidation it was
        // sent to satisfy -- one wasted refetch, every time.
        const startedAt = this.registry.stamp;

        // Retired in the `finally` at the bottom of this attempt, not the
        // moment the response arrives. `racedSince` below reads the tombstones
        // this registration is holding open: releasing it first would let
        // `landed` expire the very stamp this response is about to consult,
        // and the delete it straddles would be undone by its own arrival.
        const token = this.dispatched(dispatchedAt);

        try {
          let response: unknown;

          try {
            response = await this.transport.execute({ meta: record.meta, args: record.args });
          } catch (error) {
            if (record.run !== run) throw abandoned();

            if (record.discard) return this.discardInflight(record);

            if (record.restart) continue;

            record.inflight = undefined;
            this.fail(record, error);

            throw error;
          }

          if (record.run !== run) throw abandoned();

          if (record.discard) return this.discardInflight(record);

          if (record.restart) continue;

          // Normalized but not committed: which entities the response carries is
          // the question, and it is not answerable before the walk.
          const staged = this.store.stage(response, this.entities, rootTypeOf(record.meta));
          const raced = this.store.racedSince(staged.records.keys(), dispatchedAt);

          if (raced.length > 0 && record.frameRestarts < this.frameRestartLimit) {
            record.frameRestarts++;

            // Keep the siblings. Only the raced keys are stale; every other
            // entity in this response is at least as new as the store's, and
            // throwing them away would lose them for good if the re-run then
            // fails -- the query lands on `status: 'error'` holding data older
            // than an answer it actually received. Committing them costs a merge
            // that says nothing new when the re-run succeeds.
            this.store.commit(staged, { skip: new Set(raced) });
            this.refresh(true);

            continue;
          }

          record.inflight = undefined;

          // Past the bound. The response commits, but the entities a frame
          // overtook keep the frame's value: the alternative is a query that
          // never settles against a busy channel, and the alternative to *that*
          // is a row that visibly reverts.
          return this.settle(
            record,
            response,
            staged,
            startedAt,
            raced.length > 0 ? new Set(raced) : undefined,
          );
        } finally {
          // The whole attempt, not just the request. `racedSince` above is the
          // reader this registration exists for, and a restart re-registers on
          // the next pass around the loop, so a query that keeps losing the
          // race keeps holding the tombstones it keeps consulting.
          this.landed(token);
        }
      }
    };

    const promise = Promise.resolve().then(sequence);

    record.inflight = promise;
    this.notify(record);

    return promise;
  }

  /**
   * Run this query again, even if a request is already out for it.
   *
   * Rather than issuing a second request, the one in flight is marked as
   * answering a question that has since changed: it will be thrown away on
   * arrival and the sequence re-run. That keeps one request per query while
   * still guaranteeing the value a subscriber ends up with was fetched after
   * the write that invalidated it.
   */
  private restart(record: Record_): Promise<unknown> {
    if (record.inflight === undefined) return this.start(record);

    record.discard = false;
    record.restart = true;

    return record.inflight;
  }

  /**
   * A query went stale, reported synchronously by the invalidator.
   *
   * This is the *only* place a request in flight learns it is answering a
   * question that has changed. Doing it from the batch instead was wrong twice
   * over: a query answered by a placement callback never reaches a batch, and
   * the default scheduler is a microtask, which is ample time for a request
   * dispatched before the write to land and commit its pre-write answer. The
   * batch still decides what to *fetch*; staleness is known here.
   */
  private stale(entry: QueryEntry): void {
    const record = this.records.get(entry.key);

    if (record === undefined || record.inflight === undefined) return;

    record.discard = false;
    record.restart = true;
  }

  /**
   * Dispatch the batch.
   *
   * A query with a request already out is skipped: either that request was
   * marked stale synchronously by `stale` and will re-run when it arrives, or
   * it was started *after* the invalidation and is already the answer. Calling
   * `restart` here would, in the second case, throw away a perfectly current
   * response and spend another request.
   */
  private refetchAll(batch: readonly QueryEntry[]): void {
    for (const entry of batch) {
      const record = this.records.get(entry.key);

      if (record !== undefined && record.inflight === undefined) {
        this.detach(this.start(record));
      }
    }
  }

  /**
   * Commit a response.
   *
   * `staged` is the walk already performed by the caller -- which entities the
   * response carries has to be known before the frame race can be judged, so
   * the normalize happens up there and only the commit happens here. `skip` is
   * the set of keys a frame overtook, which commit around rather than over.
   *
   * `startedAt` is the *registry* clock reading from when this attempt was
   * dispatched, and it travels with the response rather than being read here:
   * by the time a response lands, invalidations raised during its flight have
   * already moved the clock, and the registry has to compare against the
   * earlier reading to see them. See `QueryRegistry#settle`.
   *
   * It is a different clock from the frame version `skip` was computed against
   * -- see the two readings taken in `start`. Both races are judged against the
   * same response, and settling it is where the two remedies meet: the entities
   * commit around the frames, and the query settles stale if a tag it carries
   * was invalidated in the meantime.
   */
  private settle(
    record: Record_,
    response: unknown,
    staged: StagedWrite,
    startedAt: number,
    skip?: ReadonlySet<EntityKey>,
  ): unknown {
    const { skeleton, deps } = staged;

    this.store.commit(staged, skip === undefined ? {} : { skip });

    record.skeleton = skeleton;
    record.settled = true;
    record.settledTime = this.now();
    record.status = 'success';
    record.error = undefined;
    record.fetching = false;

    const value = this.value(record);

    // NOT `value`: `entry.value` is what a placement callback is handed as
    // `current`, and that callback's return reaches `adopt` -> `store.commit`
    // unchanged. `value` here is membership-projected -- it can hold a pending
    // create's temp row -- while `base` is entity-plane only, which is the
    // invariant `base`'s own docblock states and the one a placement callback's
    // output must be held to. Passing the projection through here would let a
    // temp entity ride a callback's result straight into a query skeleton, with
    // nothing to remove it if the create it came from later fails.
    this.registry.settle(record.key, { value: this.base(record), deps, response, startedAt });
    this.notify(record);

    return value;
  }

  private fail(record: Record_, error: unknown): void {
    record.status = 'error';
    record.error = error;
    record.fetching = false;

    this.notify(record);
    this.onError?.(error, 'fetch');
  }

  /**
   * A placement callback answered for this query, so nothing will be refetched.
   *
   * The value it produced is normalized back into the store rather than held
   * as a document, so the placed list behaves exactly like a fetched one: its
   * entities are the store's, a later write to any of them updates it, and a
   * read of it keeps the identity of every element that did not move.
   *
   * A request already in flight is switched from restart to **discard**. It
   * was dispatched before the mutation, so its answer is pre-write and must
   * not commit -- and this is the one path with no recovery: placement means
   * no refetch is owed, so a pre-write response that overwrote the placed
   * skeleton would delete the created entity from the list permanently, with
   * nothing scheduled to put it back. Discard rather than restart because the
   * application has already supplied the answer; spending a request to
   * rediscover it is the cost the escape hatch exists to avoid.
   */
  private adopt(entry: QueryEntry, value: unknown[]): void {
    const record = this.records.get(entry.key);

    if (record === undefined) return;

    // `entity`, NOT the root type, and this is the one write in this file
    // where that is right. A Placement returns `unknown[]` -- a list of the
    // records themselves, built by the application -- rather than a response
    // document, so the typename of its elements is the entity. Handing it
    // `rootType` would walk each element as though it were the envelope and
    // normalize nothing. The two coincide for the bare-array query this path
    // was written for; they diverge exactly when it matters.
    const staged = this.store.stage(value, this.entities, record.meta.entity);

    // `current` -- what this callback built its answer from -- is an
    // ENTITY-PLANE-PROJECTED read: see `base`. So a `value` derived from it,
    // unchanged, can contain another mutation's still-pending optimistic
    // fields spread across a plain object with no `OPTIMISTIC` marker of its
    // own -- `normalize` walks `Object.keys`, which does not see that symbol
    // even where the record does carry it. Writing that straight into base
    // would make an unconfirmed value permanent, with nothing to remove it if
    // that other mutation later fails.
    //
    // Skipping every key any overlay currently touches closes it: base keeps
    // whatever it already had for those keys, and only the placed callback's
    // MEMBERSHIP (which entities this query's skeleton now references) lands.
    // Reusing `commit`'s `skip` is the same mechanism `mutate` already applies
    // for a frame race; this is not a new path.
    //
    // The cost is real rather than nil. For a callback that passes an overlaid
    // record through untouched -- the ordinary case -- the skipped write says
    // nothing base does not already hold. But a callback that SYNTHESIZES a
    // field on an overlaid record loses that field permanently: it is withheld
    // here, and nothing revisits it once the overlay settles. That is the side
    // to err on. The alternative writes another mutation's unconfirmed value
    // into base with nothing able to remove it if that mutation fails, and a
    // field the application can recompute is cheaper than a value that is
    // wrong for good.
    this.store.commit(staged, this.overlays.empty ? {} : { skip: this.overlays.keys() });

    const { skeleton } = staged;

    record.skeleton = skeleton;
    record.settled = true;
    record.settledTime = this.now();
    record.status = 'success';
    record.error = undefined;

    if (record.inflight !== undefined) {
      record.restart = false;
      record.discard = true;
    }

    this.notify(record);
  }

  /**
   * This query's value, and the registry's copy refreshed to match.
   *
   * NOT un-projected, whatever the name suggests: `store.read` resolves every
   * entity key through `EntityStore.overlays` when one is attached, which is
   * the Task 1 hook that lets a pending optimistic patch show up at all -- so
   * this already reflects the entity plane's live overlays, same as `value`
   * below reflects it today.
   *
   * That matters because `entry.value` is what a placement callback is handed
   * as `current`, and it is the RIGHT contract for that callback to have: the
   * application is choosing where to place `created` relative to what is
   * actually on screen right now, overlay and all, not some server-only view
   * it never rendered.
   *
   * The hazard that contract creates -- a callback's returned list carrying
   * another mutation's still-pending field, on its way to being written
   * straight into base -- is closed where the write happens, in `adopt`, by
   * skipping every currently overlaid key. It is not closed here: a read
   * cannot un-know what the store already resolved it to.
   */
  private base(record: Record_): unknown {
    const entry = this.registry.get(record.key);

    // `entry.value` is still the previous read at this point, which is what
    // lets the store reuse a container a refetch did not change. Records
    // survive a refetch on their own; the skeleton does not, because a refetch
    // builds a second one and the container memos are keyed by node identity.
    // See `EntityStore#read`.
    const value = record.settled ? this.store.read(record.skeleton, entry?.value) : undefined;

    if (entry !== undefined) entry.value = value;

    return value;
  }

  /** What a subscriber sees: the base value with pending overlays projected. */
  private value(record: Record_): unknown {
    const base = this.base(record);

    if (this.overlays.empty) return base;

    return this.overlays.project(record.key, base, this.registry.get(record.key));
  }

  /**
   * The state object for this record, reusing the previous one when nothing in
   * it changed.
   *
   * `data` compares by identity, which is sound precisely because the store
   * guarantees identity for an unchanged subtree. A deep comparison here would
   * be both slower and wrong: two structurally equal values built from
   * different records are genuinely different data.
   */
  private snapshot(record: Record_): QueryState {
    const data = this.value(record);
    const optimistic = this.overlays.affects(this.registry.get(record.key));
    const previous = record.state;

    if (
      previous !== undefined &&
      previous.data === data &&
      previous.status === record.status &&
      previous.error === record.error &&
      previous.isFetching === record.fetching &&
      previous.isOptimistic === optimistic
    ) {
      return previous;
    }

    const next: QueryState = {
      status: record.status,
      data,
      error: record.error,
      isFetching: record.fetching,
      isOptimistic: optimistic,
    };

    record.state = next;

    return next;
  }

  /**
   * Tell this query's subscribers, and the observer if one is attached.
   *
   * The single choke point every state transition already passes through --
   * `start`, `settle`, `fail`, `adopt`, `drop`, `clear` -- which is why the
   * whole of the query half of the devtools seam is one expression rather than
   * six.
   */
  private notify(record: Record_): void {
    this.observer?.({
      type: 'query',
      key: record.key,
      status: record.status,
      fetching: record.fetching,
    });

    for (const listener of record.listeners.keys()) listener();
  }

  /**
   * Re-read every tracked query against the store it was just written to.
   *
   * Two callers, wanting slightly different things from it. A mutation needs
   * the registry's `value` current before placement callbacks are handed it as
   * `current`, and nothing more -- the queries it affects are about to refetch
   * and will notify then. A stream frame needs the same *plus* the
   * notification, because a `patch` invalidates nothing and refetches nothing:
   * the store has the new value, the memos have been rebuilt around it, and if
   * this does not tell the subscribers, nothing ever will.
   *
   * `snapshot` reuses the previous state object when nothing in it moved, so
   * the identity comparison here is exact: only the queries whose rendered
   * value actually changed are notified, and the other thirty-nine mounted
   * queries cost one memoized read each and no render.
   */
  private refresh(notify: boolean): void {
    for (const record of this.records.values()) {
      const before = record.state;
      const after = this.snapshot(record);

      if (notify && before !== after) this.notify(record);
    }
  }

  /**
   * Forget the least recently used queries nobody is watching.
   *
   * Unbounded growth here is not hypothetical: a search box calling
   * `useOrderList({q})` on every keystroke mints a distinct query per
   * keystroke, each with a registry entry and a tag set behind it. The cap is
   * on *unwatched* records only -- a watched query is never evicted, however
   * old.
   */
  private reap(): void {
    if (this.records.size <= this.limit) return;

    let reaped = false;

    for (const [key, record] of this.records) {
      if (this.records.size <= this.limit) break;

      if (record.listeners.size > 0 || record.inflight !== undefined) continue;

      this.records.delete(key);
      this.registry.drop(key);
      reaped = true;
    }

    // Dropping a query is the moment its entities may have become
    // unreachable, and the only such moment: while a query is still cached,
    // `getState` can be called on it and would read the store. So the sweep
    // hangs off this rather than off a second cap the caller would have to
    // size, and it does not run at all on an application that is not churning
    // queries.
    if (reaped) this.collect();
  }

  /**
   * Drop every entity no cached query and no pending overlay can reach.
   *
   * The query cache caps *queries*, and dropping one releases its tags, but
   * until now the records behind it stayed. A search box minting a query per
   * keystroke therefore bounded one map and not the other.
   *
   * Reachability is recomputed rather than counted. A refcount would have to
   * be maintained at every write, eviction, frame and rollback, and the one
   * place it went wrong would free a record something still points at. Walking
   * the surviving skeletons is O(what is left) and runs only when a query was
   * actually reaped.
   *
   * Two roots, and the second is the one that is easy to forget: an optimistic
   * overlay holds keys that no settled query need reach, and collecting one
   * would leave a pending write patching a row that no longer exists.
   *
   * Public because an application that knows it has just finished with a
   * screen can say so, and because "this store does not grow without bound" is
   * worth a test. Returns how many records were dropped.
   */
  collect(): number {
    const reachable = new Set<EntityKey>(this.overlays.keys());

    for (const record of this.records.values()) {
      if (!record.settled) continue;

      for (const key of this.store.dependencies(record.skeleton)) reachable.add(key);
    }

    const garbage: EntityKey[] = [];

    for (const key of this.store.keys()) {
      if (!reachable.has(key)) garbage.push(key);
    }

    // Collected after the walk, not during it: `evict` mutates the map
    // `keys()` is iterating.
    //
    // No frame stamp, so no tombstone. A tombstone exists to stop an
    // in-flight response resurrecting a row the *server* deleted; this record
    // is merely unreferenced, and a later response that carries it again is
    // welcome to put it back.
    for (const key of garbage) this.store.evict(key);

    return garbage.length;
  }

  /**
   * Consume a promise the caller did not ask for.
   *
   * A refetch nobody awaited must not become an unhandled rejection -- in a
   * browser that is a console error for a failure the subscriber has already
   * been told about through `QueryState.error`, and in Node it can take the
   * process down.
   */
  private detach(promise: Promise<unknown>): void {
    void promise.catch(() => undefined);
  }
}

/**
 * The name a query is keyed under.
 *
 * `OperationMeta` carries no identifier of its own -- the generated `ops.ts`
 * holds each operation under a key the value does not repeat -- and method
 * plus path is unique per endpoint by construction, which is all a cache key
 * needs. Deriving it here rather than taking it as an argument keeps the
 * generated `hooks.ts` at one binding per line, with nothing to get out of
 * step.
 */
export function operationName(meta: OperationMeta): string {
  return `${meta.method} ${meta.path}`;
}

/**
 * The typename to normalize an operation's RESPONSE against.
 *
 * `rootType` names the response document; `entity` names what that document is
 * about. They are the same string for `GET /orders/{id}` and for a bare
 * `[]Order`, and they differ for every enveloped read -- `PageOrder{items:
 * [Order], total}` is `entity: 'Order'`, `rootType: 'PageOrder'`. Normalizing
 * that response against 'Order' reads Order's field edges against an
 * envelope's properties, matches nothing, and stores nothing, which presents
 * as a paginated list that simply never shares a record with anything.
 *
 * Falling back to `entity` covers a manifest generated before `rootType`
 * existed: identical behaviour where the two agree, and no worse than before
 * where they do not.
 *
 * This is deliberately NOT applied to a value the application supplied -- see
 * `adopt`, which receives a list of records rather than a document.
 */
function rootTypeOf(meta: OperationMeta): string | undefined {
  return meta.rootType ?? meta.entity;
}

/**
 * What an abandoned request sequence rejects with.
 *
 * Rejecting rather than resolving `undefined`: a caller that awaited `fetch()`
 * across an identity change asked a question that no longer has an answer, and
 * handing back `undefined` as though the server had returned nothing is the
 * kind of quiet lie that surfaces three layers away. Every sequence the cache
 * starts on its own behalf is already detached, so this never becomes an
 * unhandled rejection.
 */
function abandoned(): Error {
  return new Error('[forge] request abandoned: the cache was cleared');
}
