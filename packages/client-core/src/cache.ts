import { Invalidator } from './invalidate';
import type { Placement, Scheduler } from './invalidate';
import { QueryRegistry } from './registry';
import type { QueryEntry, QuerySpec, Unmount } from './registry';
import { EntityStore } from './store';
import { queryKey } from './tags';
import type { TagContext } from './tags';
import type { OperationMeta, Transport } from './transport';
import type { EntitySchema } from './types';

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
}

/** Extra per-call knobs a generated hook may pass through. */
export interface RequestOptions {
  readonly headers?: Readonly<Record<string, string>>;
  readonly signal?: AbortSignal;
}

export interface MutateOptions extends RequestOptions {
  /** Per-tag placement callbacks. See `Placement`. */
  readonly place?: Readonly<Record<string, Placement>>;
}

/** One query the cache is tracking. Private; `QueryState` is what escapes. */
interface Record_ {
  readonly key: string;
  readonly meta: OperationMeta;
  readonly args: TagContext;
  readonly spec: QuerySpec;
  readonly listeners: Set<() => void>;
  /** The last response's skeleton. Meaningless until `settled`. */
  skeleton: unknown;
  settled: boolean;
  status: QueryStatus;
  error: unknown;
  fetching: boolean;
  /** The retry sequence in progress, shared by everyone who asked for it. */
  inflight: Promise<unknown> | undefined;
  /** Identifies that sequence, so an abandoned one can recognise itself. */
  run: number;
  /** An invalidation landed mid-flight; the answer in progress predates it. */
  restart: boolean;
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

  private readonly records = new Map<string, Record_>();
  private readonly transport: Transport;
  private readonly entities: EntitySchema;
  private readonly limit: number;
  private readonly onError: ((error: unknown, context: string) => void) | undefined;

  /** Stamps each request sequence. See `start`. */
  private runs = 0;

  /** Who the cached data belongs to. See `setPrincipal`. */
  private principal: unknown;

  constructor(options: QueryCacheOptions) {
    this.transport = options.transport;
    this.entities = options.entities;
    this.limit = options.limit ?? 128;
    this.onError = options.onError;

    this.invalidator = new Invalidator(this.registry, {
      execute: (batch) => this.refetchAll(batch),
      ...(options.scheduler === undefined ? {} : { scheduler: options.scheduler }),
      ...(options.onError === undefined ? {} : { onError: options.onError }),
      onPlace: (entry, value) => this.adopt(entry, value),
      onInvalidated: (entry) => this.stale(entry),
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
  subscribe(meta: OperationMeta, args: TagContext | undefined, listener: () => void): () => void {
    const record = this.open(meta, args);

    record.listeners.add(listener);

    if (record.listeners.size === 1) record.unmount = this.registry.mount(record.spec);

    if (!record.settled && record.inflight === undefined) this.detach(this.start(record));

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
      return Promise.resolve(this.read(record) as T);
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
    const response = await this.transport.execute({
      meta,
      args,
      ...(options.headers === undefined ? {} : { headers: options.headers }),
      ...(options.signal === undefined ? {} : { signal: options.signal }),
    });

    const { skeleton } = this.store.write(response, this.entities, rootTypeOf(meta));
    const created = this.store.read(skeleton);

    // Placement is handed each query's `current` value, which lives on the
    // registry entry and is only as fresh as the last read of it. Refreshing
    // every tracked query here costs one memoized store read each and removes
    // the class of bug where a placement callback prepends to a list that a
    // previous write already changed underneath it.
    for (const record of this.records.values()) this.snapshot(record);

    this.invalidator.settled({
      invalidates: meta.invalidates,
      args,
      response,
      created,
      ...(options.place === undefined ? {} : { place: options.place }),
    });

    return created as T;
  }

  /** Invalidate already-resolved tags, as a stream frame or a manual refresh would. */
  invalidate(tags: Iterable<string>): void {
    this.invalidator.invalidate(tags);
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
      record.discard = false;
      record.state = undefined;

      this.records.set(record.key, record);
      record.unmount = this.registry.mount(record.spec);

      this.notify(record);
      this.detach(this.start(record));
    }
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
      listeners: new Set(),
      skeleton: undefined,
      settled: false,
      status: 'idle',
      error: undefined,
      fetching: false,
      inflight: undefined,
      run: 0,
      restart: false,
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
      for (;;) {
        record.restart = false;
        record.discard = false;

        let response: unknown;

        try {
          response = await this.transport.execute({ meta: record.meta, args: record.args });
        } catch (error) {
          if (record.run !== run) throw abandoned();

          if (record.discard) return this.drop(record);

          if (record.restart) continue;

          record.inflight = undefined;
          this.fail(record, error);

          throw error;
        }

        if (record.run !== run) throw abandoned();

        if (record.discard) return this.drop(record);

        if (record.restart) continue;

        record.inflight = undefined;

        return this.settle(record, response);
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
   * Throw away the answer in flight without running another request.
   *
   * The placement path. The application already supplied the value the refetch
   * would have produced, so re-running would spend a request confirming what
   * the cache knows -- which is the cost the escape hatch exists to avoid. The
   * promise resolves with the placed value, so a caller awaiting the refetch
   * that placement pre-empted gets the current answer rather than a rejection.
   */
  private drop(record: Record_): unknown {
    record.discard = false;
    record.restart = false;
    record.inflight = undefined;
    record.fetching = false;

    this.notify(record);

    return this.read(record);
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

  private settle(record: Record_, response: unknown): unknown {
    const { skeleton, deps } = this.store.write(response, this.entities, rootTypeOf(record.meta));

    record.skeleton = skeleton;
    record.settled = true;
    record.status = 'success';
    record.error = undefined;
    record.fetching = false;

    const value = this.read(record);

    this.registry.settle(record.key, { value, deps, response });
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
    const { skeleton } = this.store.write(value, this.entities, record.meta.entity);

    record.skeleton = skeleton;
    record.settled = true;
    record.status = 'success';
    record.error = undefined;

    if (record.inflight !== undefined) {
      record.restart = false;
      record.discard = true;
    }

    this.notify(record);
  }

  /** Rehydrate this query's value, and keep the registry's copy current. */
  private read(record: Record_): unknown {
    const value = record.settled ? this.store.read(record.skeleton) : undefined;
    const entry = this.registry.get(record.key);

    if (entry !== undefined) entry.value = value;

    return value;
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
    const data = this.read(record);
    const previous = record.state;

    if (
      previous !== undefined &&
      previous.data === data &&
      previous.status === record.status &&
      previous.error === record.error &&
      previous.isFetching === record.fetching
    ) {
      return previous;
    }

    const next: QueryState = {
      status: record.status,
      data,
      error: record.error,
      isFetching: record.fetching,
    };

    record.state = next;

    return next;
  }

  private notify(record: Record_): void {
    for (const listener of record.listeners) listener();
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

    for (const [key, record] of this.records) {
      if (this.records.size <= this.limit) return;

      if (record.listeners.size > 0 || record.inflight !== undefined) continue;

      this.records.delete(key);
      this.registry.drop(key);
    }
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
function operationName(meta: OperationMeta): string {
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
