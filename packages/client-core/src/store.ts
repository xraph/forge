import { normalize } from './normalize';
import { isRef, isRewritten, sameValue } from './ref';
import type { EntityKey, EntityRecord, EntitySchema, Ref } from './types';

/**
 * One rehydrated subtree, held so the next read can return the same object.
 *
 * `deps` are the entity keys reached while building `value` -- one hop for
 * this memo, because deeper changes propagate through the dependents index
 * rather than being re-checked on every read. `key` is set when the memo
 * materialises an entity, which is what lets invalidation walk on past it.
 */
interface Memo {
  valid: boolean;
  value: unknown;
  deps: EntityKey[];
  key: EntityKey | undefined;
  /** True while this memo's value is still being filled in. */
  building: boolean;
  /** True when this memo sits on a cycle that closes through a plain object. */
  cyclic: boolean;
}

/** What a write reported: the skeleton to cache, and what it depends on. */
export interface WriteResult {
  readonly skeleton: unknown;
  readonly deps: ReadonlySet<EntityKey>;
}

/**
 * A normalized value that has not been committed yet.
 *
 * The split exists because a caller has to be able to see *which entities* a
 * response carries before deciding whether to commit it. A stream frame that
 * landed while the request was out makes the response older than the store for
 * those entities, and the only way to notice is to normalize first and compare.
 * See `racedSince`.
 */
export interface StagedWrite extends WriteResult {
  readonly records: ReadonlyMap<EntityKey, Record<string, unknown>>;
}

/**
 * How many frame-evicted keys keep a stamp. See `EntityStore.remember`.
 *
 * Bounded rather than unbounded because the map is otherwise a slow leak
 * indexed by every entity a long session ever deleted, and a client left open
 * on a busy channel for a week is the case that finds it.
 */
const TOMBSTONE_LIMIT = 256;

/**
 * Marks a materialized record as carrying an optimistic value.
 *
 * A symbol rather than a property so it is invisible to `Object.keys`,
 * `JSON.stringify` and the deep-equality below -- an application that
 * serializes what it renders must not start shipping a cache internal, and
 * `equal` compares by string keys, so a property would report a change every
 * time an overlay was pushed or dropped. A spread (`{...record}`) does copy
 * it, same as any other own-enumerable symbol-keyed property -- a component
 * that clones a record before mutating it locally carries the marker
 * forward, which is the right outcome for that case.
 */
export const OPTIMISTIC: unique symbol = Symbol('forge.optimistic');

/**
 * The overlay runtime, as the store sees it.
 *
 * Declared structurally here rather than imported from `overlay.ts`, for the
 * same reason `cache.ts` declares `LiveBinding` rather than importing
 * `StreamBinder`: the store is the smallest surface in this package and must
 * not drag the overlay algebra into a bundle that never pushes one. The
 * compiler erases this entirely.
 *
 * `effective` returns the record with every live overlay folded in, or
 * `undefined` when the fold deletes it -- which rehydrates as a hole, exactly
 * as an eviction does. `holds` answers whether any overlay touches the key at
 * all, which is what the OPTIMISTIC stamp is keyed off. `rebase` says the base
 * for this key moved and the fold must be recomputed.
 */
export interface OverlayLayer {
  effective(key: EntityKey): EntityRecord | undefined;
  holds(key: EntityKey): boolean;
  rebase(key: EntityKey): void;
}

/** How a staged write is committed. */
export interface CommitOptions {
  /**
   * Stamp every record written with this frame-clock reading. Non-zero only on
   * the stream-frame path; see `nextFrame`.
   */
  readonly frameAt?: number;
  /** Entity keys to leave alone. What the store already holds is newer. */
  readonly skip?: ReadonlySet<EntityKey>;
}

/**
 * The normalized entity store: entity key to record, plus the machinery that
 * rebuilds a response out of it without changing the identity of anything
 * that did not change.
 *
 * Referential stability is a correctness requirement rather than a
 * performance one. `useSyncExternalStore` tears when `getSnapshot` returns a
 * fresh object for unchanged state, so `read` is memoized per subtree and the
 * memos are invalidated by walking the reverse-dependency graph from the key
 * that was written -- work proportional to what changed, not to the size of
 * the store.
 */
export class EntityStore {
  private readonly records = new Map<EntityKey, EntityRecord>();
  private readonly memoByKey = new Map<EntityKey, Memo>();
  private memoByNode = new WeakMap<object, Memo>();
  private readonly dependents = new Map<EntityKey, Set<Memo>>();
  private writes = 0;

  /**
   * The frame clock: how many stream-frame commits have been applied.
   *
   * Monotonic for the life of the store, and deliberately not reset by
   * `clear` -- a request dispatched before an identity change holds a reading
   * from the previous session, and the reading it holds must never compare as
   * *newer* than a stamp written after it.
   */
  private frames = 0;

  /**
   * The overlay stack, when one is attached. `undefined` in a store used
   * without a `QueryCache`, which is how every normalization test drives it.
   */
  overlays: OverlayLayer | undefined;

  /**
   * Frame stamps for keys the store no longer holds a record for.
   *
   * A frame that evicts `Order:7` leaves nothing to carry the stamp, so a
   * response dispatched before the delete would resurrect the row -- the
   * "deleted item came back" defect, which looks exactly like a caching bug and
   * is one. Bounded by frame-evicted keys, and each entry is dropped the moment
   * a record for that key exists again to carry the stamp itself.
   */
  private readonly graves = new Map<EntityKey, number>();

  /** The memos the read in progress is part-way through building. */
  private readonly buildStack: Memo[] = [];
  /** Memos on a plain-object cycle, awaiting the cycle entry's dependencies. */
  private readonly deferred: Memo[] = [];
  /** Stack depth of the outermost memo currently known to sit on a cycle. */
  private cycleFloor = Infinity;

  /** Total number of record writes committed. Bumps only on real change. */
  get version(): number {
    return this.writes;
  }

  /**
   * The current frame-clock reading.
   *
   * A request records this at dispatch. If any entity in its response carries a
   * stamp newer than the recorded reading, a frame overtook the request and its
   * answer is stale for that entity.
   */
  get frameVersion(): number {
    return this.frames;
  }

  /** Open a new frame: one reading per batch of frames, not one per frame. */
  nextFrame(): number {
    return ++this.frames;
  }

  /**
   * How many frame-evicted keys currently hold a stamp.
   *
   * Exposed because "this map does not grow without bound" is a property worth
   * a test, and one that is otherwise unobservable from outside -- the same
   * reason `QueryRegistry.stampedTags` is exposed.
   */
  get tombstones(): number {
    return this.graves.size;
  }

  /**
   * Drop every tombstone no outstanding request could still read.
   *
   * A tombstone is only ever consulted by a response that was dispatched
   * *before* the delete and has not arrived yet, so its whole useful life is
   * one request round trip. `oldestLiveDispatch` is the frame-clock reading of
   * the earliest such request; a stamp at or below it has no reader left,
   * because every request still out was dispatched after the delete and will
   * therefore lose the comparison in `racedSince` on its own.
   *
   * Pass `Infinity` when nothing is in flight, which drops all of them.
   *
   * This is what turns `TOMBSTONE_LIMIT` from the mechanism into a backstop.
   * The cap alone made resurrection improbable rather than impossible: delete
   * a row, then delete 256 more, and the first stamp is pushed out while a
   * request that straddles it is still in the air. Expiring by dispatch
   * instead removes stamps only once they cannot matter, so the cap is reached
   * only by a session deleting faster than its requests complete.
   *
   * Returns how many were dropped, which is what makes it testable.
   */
  expireTombstones(oldestLiveDispatch: number): number {
    if (this.graves.size === 0) return 0;

    let dropped = 0;

    for (const [key, frameAt] of this.graves) {
      if (frameAt > oldestLiveDispatch) continue;

      this.graves.delete(key);
      dropped += 1;
    }

    return dropped;
  }

  /** When a stream frame last wrote this key. 0 if none ever did. */
  frameStamp(key: EntityKey): number {
    const record = this.records.get(key);

    if (record !== undefined) return record.frameAt ?? 0;

    return this.graves.get(key) ?? 0;
  }

  /**
   * Which of these keys a stream frame wrote after `since`.
   *
   * The question a response has to ask before it commits. Returns the keys
   * rather than a boolean so the caller can commit the rest of the response
   * around them if it decides not to re-run the request.
   */
  racedSince(keys: Iterable<EntityKey>, since: number): EntityKey[] {
    const raced: EntityKey[] = [];

    for (const key of keys) {
      if (this.frameStamp(key) > since) raced.push(key);
    }

    return raced;
  }

  get size(): number {
    return this.records.size;
  }

  has(key: EntityKey): boolean {
    return this.records.has(key);
  }

  /** The stored record, or undefined. Never a copy: treat it as frozen. */
  getRecord(key: EntityKey): EntityRecord | undefined {
    return this.records.get(key);
  }

  keys(): IterableIterator<EntityKey> {
    return this.records.keys();
  }

  /**
   * Normalize a response and commit every entity it contained.
   *
   * `rootType` is the operation's declared response typename from the generated
   * manifest.
   */
  write(
    value: unknown,
    schema: EntitySchema,
    rootType?: string,
    options?: CommitOptions,
  ): WriteResult {
    const staged = this.stage(value, schema, rootType);

    this.commit(staged, options);

    return staged;
  }

  /**
   * Normalize a value without writing anything.
   *
   * Pure, and the half of `write` a caller needs when the decision to commit
   * depends on what the response turned out to contain.
   */
  stage(value: unknown, schema: EntitySchema, rootType?: string): StagedWrite {
    return normalize(value, schema, rootType);
  }

  /** Write a staged normalization into the store. */
  commit(staged: StagedWrite, options: CommitOptions = {}): void {
    const frameAt = options.frameAt ?? 0;
    const skip = options.skip;

    for (const [key, data] of staged.records) {
      if (skip?.has(key) === true) continue;

      this.put(key, data, frameAt);
    }
  }

  /**
   * Merge one entity's fields into the store.
   *
   * Merge rather than replace: a newer server sends fields this client's
   * types do not name, and a view that does read them must not lose them
   * because some other endpoint returned a narrower projection of the same
   * record.
   *
   * Returns whether anything actually changed. A write of identical data
   * keeps the previous data object, the previous version, and therefore every
   * object identity downstream of it -- a refetch that returns the same bytes
   * must not re-render anything.
   *
   * `frameAt` is the frame-clock reading when this write came from a stream
   * frame, and 0 otherwise. It is carried forward by `Math.max`, so a later
   * response merging extra fields into a frame-written record does not erase
   * the record's memory of having been overtaken.
   */
  put(key: EntityKey, data: Readonly<Record<string, unknown>>, frameAt = 0): boolean {
    const prev = this.records.get(key);
    // A tombstone hands its stamp to the record that replaces it, which is what
    // keeps that map bounded by frame-evicted-and-not-yet-restored keys.
    const carried = Math.max(prev?.frameAt ?? this.graves.get(key) ?? 0, frameAt);

    this.graves.delete(key);

    if (prev === undefined) {
      this.records.set(key, { data, version: 1, frameAt: carried });
      this.writes++;
      this.invalidate(key);
      this.overlays?.rebase(key);

      return true;
    }

    const merged = { ...prev.data, ...data };

    if (equal(prev.data, merged)) {
      // No data moved, but a frame did touch the record, and the stamp is what
      // stops an older response from moving it. Recording it costs one object
      // and bumps no version, so nothing downstream re-renders.
      if (carried !== (prev.frameAt ?? 0)) {
        this.records.set(key, { data: prev.data, version: prev.version, frameAt: carried });
      }

      return false;
    }

    this.records.set(key, { data: merged, version: prev.version + 1, frameAt: carried });
    this.writes++;
    this.invalidate(key);
    this.overlays?.rebase(key);

    return true;
  }

  /**
   * Drop one record.
   *
   * A reference to it in a settled skeleton rehydrates to nothing: an array
   * element is dropped, an object field becomes `undefined`. See
   * `materializeNode`.
   *
   * A non-zero `frameAt` leaves a tombstone, so a response that was already in
   * flight when the delete arrived cannot put the row back.
   */
  evict(key: EntityKey, frameAt = 0): boolean {
    const held = this.records.delete(key);

    // Only for a key the store actually held. A delete for something never
    // cached has nothing to protect -- no request can be carrying a record this
    // client never asked for and no skeleton references it -- and writing one
    // anyway made the map grow with every delete the server ever announced,
    // whether or not this client had any interest in it.
    if (frameAt > 0 && held) this.remember(key, frameAt);

    if (!held) return false;

    this.writes++;
    this.invalidate(key);
    this.overlays?.rebase(key);

    return true;
  }

  /**
   * Drop the memos for these keys without writing anything.
   *
   * The overlay stack's seam back into the store: pushing or dropping an
   * overlay changes what `read` must return for a key while leaving the base
   * record untouched, so there is a memo to invalidate and no record to write.
   * One version bump for the whole set, because a fold is one event -- and
   * only when `invalidate` actually dropped something, per `version`'s
   * contract above: a key nothing ever read has no memo to drop, and touching
   * it must not look like a write.
   */
  touch(keys: Iterable<EntityKey>): void {
    let moved = false;

    for (const key of keys) {
      if (this.invalidate(key)) moved = true;
    }

    if (moved) this.writes++;
  }

  /**
   * Drop everything.
   *
   * The identity-change path in a later chunk needs this: a normalized store
   * keys `Order:7` globally with no memory of who fetched it.
   */
  clear(): void {
    this.records.clear();
    this.memoByKey.clear();
    this.dependents.clear();
    // The frame clock itself keeps running: a request dispatched before the
    // identity change holds an older reading, and resetting to 0 would let a
    // stamp written afterwards fail to compare as newer than it.
    this.graves.clear();
    // Replaced rather than cleared: a WeakMap has no clear, and a surviving
    // node memo would keep serving the previous principal's data to a
    // skeleton the caller still holds. That is the defect partitioning exists
    // to prevent, so it must not be reintroduced by a cache.
    this.memoByNode = new WeakMap<object, Memo>();
    this.writes++;
  }

  /**
   * Rebuild a value from its skeleton. See `denormalize`.
   *
   * `previous` is the value the last read of this query returned, and passing
   * it is what keeps a container's identity across a *refetch*. Records
   * survive one on their own, because `put` treats a deep-equal write as no
   * write and every entity memo stays valid, but the skeleton does not: a
   * refetch normalizes freshly parsed JSON into a second skeleton whose nodes
   * are new objects, and the container memos are keyed by node identity. With
   * nothing to compare against, a poll returning identical bytes hands back a
   * new root and re-renders whatever is mounted on it.
   *
   * Omit it and the old behaviour is what happens, which is what every caller
   * that has no previous read wants.
   */
  read<T = unknown>(skeleton: unknown, previous?: unknown): T {
    return this.materialize(skeleton, new Set<EntityKey>(), previous) as T;
  }

  /**
   * Which entity keys a skeleton currently reaches, transitively.
   *
   * Recomputed against the live store rather than reused from normalization,
   * because a reference that resolved to nothing at write time resolves to a
   * whole subtree once that record arrives.
   */
  dependencies(skeleton: unknown): Set<EntityKey> {
    const deps = new Set<EntityKey>();
    this.materialize(skeleton, deps);

    return deps;
  }

  /**
   * One read. Resets the per-read bookkeeping so a throw part-way through a
   * previous read cannot leak a half-built stack into this one.
   */
  private materialize(skeleton: unknown, collect: Set<EntityKey>, previous?: unknown): unknown {
    this.buildStack.length = 0;
    this.deferred.length = 0;
    this.cycleFloor = Infinity;

    return this.materializeNode(skeleton, collect, previous);
  }

  private materializeNode(node: unknown, collect: Set<EntityKey>, previous?: unknown): unknown {
    if (node === null || typeof node !== 'object') return node;

    if (isRef(node)) return this.materializeKey((node as Ref).__ref, collect);

    // Not rewritten means no reference anywhere beneath it, so the skeleton
    // node already is the answer. This is most of a real response.
    //
    // It is also the one place reuse cannot be settled by comparing children,
    // because there are no rebuilt children to compare: the node is raw
    // response data and the walk stops here. So this is the only deep
    // comparison in the read, and it is the same `equal` a write already runs
    // per record. Skipping it would cost more than it saves: `{items: [refs],
    // meta: {page: 1}}` is an ordinary page, and a `meta` that counts as
    // changed on every refetch means the root above it can never be reused.
    if (!isRewritten(node)) {
      return previous !== undefined && equal(previous, node) ? previous : node;
    }

    const cached = this.memoByNode.get(node);

    if (cached !== undefined && cached.valid) {
      // Re-entering a memo that is still building closes a cycle through a
      // plain object, which has no entity key to hang invalidation on. Every
      // frame from that memo up to this one is on the cycle.
      if (cached.building) this.markCycle(cached);
      for (const dep of cached.deps) collect.add(dep);

      return cached.value;
    }

    const source = node as Record<string, unknown>;
    const out: Record<string, unknown> | unknown[] = Array.isArray(source) ? [] : {};
    const memo: Memo = {
      valid: true,
      value: out,
      deps: [],
      key: undefined,
      building: true,
      cyclic: false,
    };

    this.memoByNode.set(node, memo);
    this.buildStack.push(memo);

    const deps = new Set<EntityKey>();

    // Threaded down so each child can try to reuse its own counterpart before
    // this container asks whether all of them did. Positional, so a list whose
    // elements moved simply fails to match and rebuilds, which is a miss
    // rather than a wrong answer.
    const before = previous !== null && typeof previous === 'object' ? previous : undefined;
    const beforeArray = Array.isArray(before) ? before : undefined;
    const beforeObject =
      before !== undefined && beforeArray === undefined
        ? (before as Record<string, unknown>)
        : undefined;

    if (Array.isArray(source)) {
      const target = out as unknown[];

      for (let i = 0; i < source.length; i++) {
        const element = source[i];
        const value = this.materializeNode(element, deps, beforeArray?.[i]);

        // A reference whose record is gone -- evicted by a delete frame -- is a
        // *hole*, not a value, and it is dropped rather than pushed as
        // `undefined`. Nothing rewrites a settled skeleton when an entity is
        // evicted, so without this a list renders as `[undefined, {...}]` and
        // the first `data.map(o => o.id)` in application code throws. The
        // subscriber is handed that list synchronously, before any refetch the
        // eviction triggered can land, so a repair that arrives with the
        // refetch is a repair that arrives too late.
        //
        // Only a reference is dropped. A literal `undefined` or `null` in the
        // response is data the server sent and is passed through untouched.
        if (value === undefined && isRef(element)) continue;

        target.push(value);
      }
    } else {
      const target = out as Record<string, unknown>;
      for (const field of Object.keys(source)) {
        target[field] = this.materializeNode(source[field], deps, beforeObject?.[field]);
      }
    }

    this.commitMemo(memo, deps, collect);

    // Bottom-up, and shallow on purpose. By the time this runs every child has
    // already settled its own identity: a record came from `memoByKey`, a
    // nested container just reused or rebuilt itself, and anything else is a
    // primitive. So `===` per child is a complete answer here, and the whole
    // read stays proportional to the skeleton rather than to the data.
    //
    // Not for a cyclic memo. A cycle re-entering this node was handed `out`
    // while it was building, so `out` is what the cycle points at; returning
    // `previous` instead would hand the caller a graph whose interior still
    // refers to the object it replaced.
    if (previous !== undefined && !memo.cyclic && sameChildren(out, previous)) {
      memo.value = previous;

      return previous;
    }

    return out;
  }

  private materializeKey(key: EntityKey, collect: Set<EntityKey>): unknown {
    // Recorded before the record is looked up, so a skeleton pointing at an
    // entity that has not arrived yet still depends on it and recomputes when
    // it does.
    collect.add(key);

    const cached = this.memoByKey.get(key);

    if (cached !== undefined && cached.valid) {
      for (const dep of cached.deps) collect.add(dep);

      // A cycle re-entering here gets the half-filled object. It is the same
      // object the outer frame is completing, so by the time the caller can
      // observe it, it is whole -- the contract structuredClone gives for
      // cyclic input.
      return cached.value;
    }

    // The one resolution point. Everything else in this class -- `frameStamp`,
    // `racedSince`, `has`, the tombstones -- reads base directly and must
    // continue to: those answer questions about what the *server* has said,
    // and an overlay is not a write.
    const overlaid = this.overlays?.holds(key) === true;
    const record = overlaid ? this.overlays?.effective(key) : this.records.get(key);

    if (record === undefined) return undefined;

    // `PropertyKey` rather than `string`: this is the one record that carries
    // the OPTIMISTIC symbol, and a `Record<string, unknown>` has no index
    // signature a symbol satisfies.
    const out: Record<PropertyKey, unknown> = {};
    if (overlaid) out[OPTIMISTIC] = true;
    const memo: Memo = {
      valid: true,
      value: out,
      deps: [],
      key,
      building: true,
      cyclic: false,
    };

    this.memoByKey.set(key, memo);
    this.buildStack.push(memo);

    const deps = new Set<EntityKey>();

    for (const field of Object.keys(record.data)) {
      out[field] = this.materializeNode(record.data[field], deps);
    }

    this.commitMemo(memo, deps, collect);

    return out;
  }

  /**
   * Mark every frame from the cycle's entry up to the current one.
   *
   * `target` is the memo the back-edge landed on, so it is the outermost frame
   * of this cycle and the one whose dependency set will be complete.
   */
  private markCycle(target: Memo): void {
    for (let i = this.buildStack.length - 1; i >= 0; i--) {
      (this.buildStack[i] as Memo).cyclic = true;

      if (this.buildStack[i] === target) {
        if (i < this.cycleFloor) this.cycleFloor = i;

        return;
      }
    }
  }

  /**
   * Finish one frame: record what it reached, hand that up to its parent, and
   * hook it into the reverse-dependency index.
   *
   * A memo on a plain-object cycle cannot be indexed here, because its own
   * dependency set is incomplete -- the back-edge returned before its
   * dependencies were known, so it would go stale without ever being
   * invalidated. Indexing is deferred to the cycle's entry frame instead, and
   * every memo on the cycle is indexed under the entry's dependencies. Since
   * all of them are reachable from the entry, that set is a superset of each
   * one's true dependencies: over-invalidation, never under. An entity cycle
   * needs none of this -- `materializeKey` records its key before anything
   * else, so the key itself is the hub invalidation travels through.
   *
   * The earlier version of this discarded every node memo in the read as soon
   * as one cycle appeared, which meant an unrelated cyclic object voided
   * structural sharing for the whole response and handed
   * `useSyncExternalStore` a fresh snapshot with nothing written.
   */
  private commitMemo(memo: Memo, deps: Set<EntityKey>, collect: Set<EntityKey>): void {
    memo.deps = [...deps];
    memo.building = false;

    for (const dep of memo.deps) collect.add(dep);

    const depth = this.buildStack.length - 1;
    this.buildStack.pop();

    if (memo.cyclic) {
      this.deferred.push(memo);
    } else {
      this.link(memo);
    }

    // This frame is the outermost one on the cycle, so its dependencies cover
    // everything the cycle reaches.
    if (depth === this.cycleFloor) {
      for (const pending of this.deferred) {
        pending.deps = memo.deps;
        this.link(pending);
      }

      this.deferred.length = 0;
      this.cycleFloor = Infinity;
    }
  }

  /**
   * Record a tombstone, evicting the oldest once the cap is reached.
   *
   * A tombstone is only ever *read* by a response that was dispatched before
   * the delete and has not arrived yet, so its useful life is one request
   * round trip. The cap is three orders of magnitude more than that window
   * needs, and bounding it turns "grows with every entity the session ever
   * deleted" into a fixed cost.
   *
   * Deleted before it is set, so re-tombstoning a key moves it to the back of
   * the insertion order. A `Map` keeps the original position on a plain
   * overwrite, which would leave the most recently deleted key first in line
   * for eviction -- exactly backwards.
   */
  private remember(key: EntityKey, frameAt: number): void {
    this.graves.delete(key);
    this.graves.set(key, frameAt);

    if (this.graves.size <= TOMBSTONE_LIMIT) return;

    const oldest = this.graves.keys().next();

    if (!oldest.done) this.graves.delete(oldest.value);
  }

  private link(memo: Memo): void {
    for (const dep of memo.deps) {
      let set = this.dependents.get(dep);

      if (set === undefined) {
        set = new Set<Memo>();
        this.dependents.set(dep, set);
      }

      set.add(memo);
    }
  }

  /**
   * Invalidate every memo that reaches `root`, transitively.
   *
   * The walk is over the reverse-dependency index rather than over the store,
   * so a write to one of 50,000 entities touches only the subtrees that
   * actually contain it.
   *
   * Returns whether any memo was actually dropped. `touch` needs this to
   * honour `version`'s "bumps only on real change" contract -- a key nothing
   * ever read has nothing in `memoByKey` or `dependents` to find here.
   */
  private invalidate(root: EntityKey): boolean {
    const queue: EntityKey[] = [root];
    const done = new Set<EntityKey>();
    let dropped = false;

    while (queue.length > 0) {
      const key = queue.pop() as EntityKey;

      if (done.has(key)) continue;
      done.add(key);

      const own = this.memoByKey.get(key);

      if (own !== undefined) {
        this.memoByKey.delete(key);
        own.valid = false;
        this.dropMemo(own);
        dropped = true;
      }

      const set = this.dependents.get(key);

      if (set === undefined) continue;
      this.dependents.delete(key);

      for (const memo of set) {
        if (!memo.valid) continue;

        memo.valid = false;
        this.dropMemo(memo);
        dropped = true;

        if (memo.key !== undefined) queue.push(memo.key);
      }
    }

    return dropped;
  }

  /** Unhook a memo from the reverse-dependency index. */
  private dropMemo(memo: Memo): void {
    for (const dep of memo.deps) this.dependents.get(dep)?.delete(memo);
  }
}

/**
 * Rebuild the original value from a skeleton.
 *
 * Unchanged subtrees keep their object identity across calls, which is what
 * `useSyncExternalStore` requires of `getSnapshot`. A reference to an entity
 * the store does not hold rehydrates to `undefined` rather than to the
 * reference itself, so no application ever sees the store's internals.
 *
 * **Caller contract: treat the result as immutable, and stop using the
 * response object you handed to `write`.** A subtree containing no entity is
 * never copied -- the skeleton holds the response's own object, and this
 * returns that same object -- which is exactly what makes identity stable
 * across reads. It also means the result, the skeleton and the original
 * response can be the same object. Mutating any of them changes the others
 * with no version bump and no invalidation, so a component would keep
 * rendering from a memo built before the edit. Copy before mutating.
 *
 * A cyclic graph is rebuilt as a cyclic graph. The alternative -- stopping at
 * the back-edge and handing back the raw reference -- would make every
 * component that walks an association responsible for recognising a cache
 * internal, which is precisely the indirection the skeleton exists to hide.
 * The store itself stays flat and JSON-serializable either way: each record
 * is acyclic, and only the graph between records closes.
 */
export function denormalize<T = unknown>(skeleton: unknown, store: EntityStore): T {
  return store.read<T>(skeleton);
}

/**
 * Whether a record's value changed.
 *
 * Deep rather than shallow, because a refetch that returns byte-identical
 * JSON returns entirely fresh objects: every nested array and object has a
 * new identity, and a shallow check would report a change on every poll,
 * bump the version, invalidate every memo that reaches the record and
 * re-render an application whose data did not move. References compare by
 * key for the same reason -- two normalization passes mint two `Ref` objects
 * for one entity.
 *
 * `path` holds the objects on the route from the root of this comparison to
 * the pair being compared, and each is removed again on the way back out. It
 * has to be a path rather than a running set of everything seen: an object
 * reachable twice through different branches is a DAG, not a cycle, and
 * treating the second encounter as "already equal" would answer `true` for a
 * `b` that was never looked at. `put('Order:1', {x: s, y: s})` followed by
 * `put('Order:1', {x: {n: 1}, y: {n: 2}})` would report no change, skip the
 * version bump, skip invalidation, and leave the record holding `y: {n: 1}`.
 * A response parsed from JSON never aliases, so nothing here would ever have
 * caught it -- but the optimistic-overlay and live-frame paths call `put`
 * with hand-built objects, where aliasing is ordinary.
 */
/**
 * Whether a freshly built container has the same children, by identity, as the
 * one the previous read returned.
 *
 * Deliberately not `equal`. This runs on a container whose children have
 * already been rebuilt and have already settled their own identity, so `===`
 * per child decides it outright and a deep walk would only re-derive an answer
 * the children already gave. The cost of a read stays proportional to the
 * number of skeleton nodes rather than to the size of the response.
 *
 * Key *order* is not compared, only membership and count. Two normalizations
 * of the same JSON produce the same order in practice, and a container that
 * differs only in insertion order is the same value to every consumer here.
 */
function sameChildren(built: object, previous: unknown): boolean {
  if (previous === null || typeof previous !== 'object') return false;
  if (Array.isArray(built) !== Array.isArray(previous)) return false;

  if (Array.isArray(built)) {
    const before = previous as unknown[];

    if (before.length !== built.length) return false;

    for (let i = 0; i < built.length; i++) {
      if (built[i] !== before[i]) return false;
    }

    return true;
  }

  const left = built as Record<string, unknown>;
  const right = previous as Record<string, unknown>;
  const keys = Object.keys(left);

  if (keys.length !== Object.keys(right).length) return false;

  for (const key of keys) {
    if (!Object.prototype.hasOwnProperty.call(right, key)) return false;
    if (left[key] !== right[key]) return false;
  }

  return true;
}

function equal(a: unknown, b: unknown, path?: Set<unknown>): boolean {
  if (sameValue(a, b)) return true;

  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') return false;

  // A reference and an object that merely looks like one are different data.
  if (isRef(a) !== isRef(b)) return false;

  const route = path ?? new Set<unknown>();

  // Already on this route: a genuine cycle, which the enclosing comparison is
  // in the middle of deciding.
  if (route.has(a)) return true;

  route.add(a);
  const same = equalChildren(a, b, route);
  route.delete(a);

  return same;
}

function equalChildren(a: object, b: object, route: Set<unknown>): boolean {
  if (Array.isArray(a)) {
    if (!Array.isArray(b) || a.length !== b.length) return false;

    for (let i = 0; i < a.length; i++) {
      if (!equal(a[i], b[i], route)) return false;
    }

    return true;
  }

  if (Array.isArray(b)) return false;

  const left = a as Record<string, unknown>;
  const right = b as Record<string, unknown>;
  const keys = Object.keys(left);

  if (keys.length !== Object.keys(right).length) return false;

  for (const key of keys) {
    if (!equal(left[key], right[key], route)) return false;
  }

  return true;
}
