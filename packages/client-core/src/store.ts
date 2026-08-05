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
}

/** What a write reported: the skeleton to cache, and what it depends on. */
export interface WriteResult {
  readonly skeleton: unknown;
  readonly deps: ReadonlySet<EntityKey>;
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

  /** Node memos created by the read in progress, and whether one closed a cycle. */
  private pendingNodes: Array<[object, Memo]> = [];
  private sawNodeCycle = false;

  /** Total number of record writes committed. Bumps only on real change. */
  get version(): number {
    return this.writes;
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
   * `rootType` is the operation's declared entity typename from the generated
   * manifest.
   */
  write(value: unknown, schema: EntitySchema, rootType?: string): WriteResult {
    const { skeleton, records, deps } = normalize(value, schema, rootType);

    for (const [key, data] of records) this.put(key, data);

    return { skeleton, deps };
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
   */
  put(key: EntityKey, data: Readonly<Record<string, unknown>>): boolean {
    const prev = this.records.get(key);

    if (prev === undefined) {
      this.records.set(key, { data, version: 1 });
      this.writes++;
      this.invalidate(key);

      return true;
    }

    const merged = { ...prev.data, ...data };

    if (equal(prev.data, merged)) return false;

    this.records.set(key, { data: merged, version: prev.version + 1 });
    this.writes++;
    this.invalidate(key);

    return true;
  }

  /** Drop one record. Skeletons still referencing it rehydrate to undefined. */
  evict(key: EntityKey): boolean {
    if (!this.records.delete(key)) return false;

    this.writes++;
    this.invalidate(key);

    return true;
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
    // Replaced rather than cleared: a WeakMap has no clear, and a surviving
    // node memo would keep serving the previous principal's data to a
    // skeleton the caller still holds. That is the defect partitioning exists
    // to prevent, so it must not be reintroduced by a cache.
    this.memoByNode = new WeakMap<object, Memo>();
    this.writes++;
  }

  /** Rebuild a value from its skeleton. See `denormalize`. */
  read<T = unknown>(skeleton: unknown): T {
    return this.materialize(skeleton, new Set<EntityKey>()) as T;
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
   * One read.
   *
   * A cycle that closes through plain objects rather than through an entity
   * leaves the memos inside it with incomplete dependency sets -- the
   * back-edge returned before its own dependencies were known -- so those
   * memos would go stale without ever being invalidated. Rather than keep a
   * subset that is hard to reason about, every node memo this read created is
   * discarded when one is seen. Entity memos are unaffected: a reference
   * always records its key before anything else, so a cycle closing through
   * one is fully tracked. JSON cannot express a plain-object cycle; only a
   * hand-built object graph can, and it pays a rebuild per read.
   */
  private materialize(skeleton: unknown, collect: Set<EntityKey>): unknown {
    this.pendingNodes = [];
    this.sawNodeCycle = false;

    const value = this.materializeNode(skeleton, collect);

    if (this.sawNodeCycle) {
      for (const [node, memo] of this.pendingNodes) {
        if (this.memoByNode.get(node) === memo) this.memoByNode.delete(node);

        memo.valid = false;
        this.dropMemo(memo);
      }
    }

    this.pendingNodes = [];

    return value;
  }

  private materializeNode(node: unknown, collect: Set<EntityKey>): unknown {
    if (node === null || typeof node !== 'object') return node;

    if (isRef(node)) return this.materializeKey((node as Ref).__ref, collect);

    // Not rewritten means no reference anywhere beneath it, so the skeleton
    // node already is the answer. This is most of a real response.
    if (!isRewritten(node)) return node;

    const cached = this.memoByNode.get(node);

    if (cached !== undefined && cached.valid) {
      if (cached.building) this.sawNodeCycle = true;
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
    };

    this.memoByNode.set(node, memo);
    this.pendingNodes.push([node, memo]);

    const deps = new Set<EntityKey>();

    if (Array.isArray(source)) {
      const target = out as unknown[];
      for (let i = 0; i < source.length; i++) target.push(this.materializeNode(source[i], deps));
    } else {
      const target = out as Record<string, unknown>;
      for (const field of Object.keys(source)) {
        target[field] = this.materializeNode(source[field], deps);
      }
    }

    memo.building = false;
    this.commitMemo(memo, deps, collect);

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

    const record = this.records.get(key);

    if (record === undefined) return undefined;

    const out: Record<string, unknown> = {};
    const memo: Memo = {
      valid: true,
      value: out,
      deps: [],
      key,
      building: true,
    };

    this.memoByKey.set(key, memo);

    const deps = new Set<EntityKey>();

    for (const field of Object.keys(record.data)) {
      out[field] = this.materializeNode(record.data[field], deps);
    }

    memo.building = false;
    this.commitMemo(memo, deps, collect);

    return out;
  }

  private commitMemo(memo: Memo, deps: Set<EntityKey>, collect: Set<EntityKey>): void {
    memo.deps = [...deps];

    for (const dep of memo.deps) {
      let set = this.dependents.get(dep);

      if (set === undefined) {
        set = new Set<Memo>();
        this.dependents.set(dep, set);
      }

      set.add(memo);
      collect.add(dep);
    }
  }

  /**
   * Invalidate every memo that reaches `root`, transitively.
   *
   * The walk is over the reverse-dependency index rather than over the store,
   * so a write to one of 50,000 entities touches only the subtrees that
   * actually contain it.
   */
  private invalidate(root: EntityKey): void {
    const queue: EntityKey[] = [root];
    const done = new Set<EntityKey>();

    while (queue.length > 0) {
      const key = queue.pop() as EntityKey;

      if (done.has(key)) continue;
      done.add(key);

      const own = this.memoByKey.get(key);

      if (own !== undefined) {
        this.memoByKey.delete(key);
        own.valid = false;
        this.dropMemo(own);
      }

      const set = this.dependents.get(key);

      if (set === undefined) continue;
      this.dependents.delete(key);

      for (const memo of set) {
        if (!memo.valid) continue;

        memo.valid = false;
        this.dropMemo(memo);

        if (memo.key !== undefined) queue.push(memo.key);
      }
    }
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
 * `seen` guards the pathological input: a plain-object cycle inside a record,
 * which JSON cannot produce but a hand-built graph can.
 */
function equal(a: unknown, b: unknown, seen?: Set<unknown>): boolean {
  if (sameValue(a, b)) return true;

  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') return false;

  // A reference and an object that merely looks like one are different data.
  if (isRef(a) !== isRef(b)) return false;

  const visited = seen ?? new Set<unknown>();

  if (visited.has(a)) return true;
  visited.add(a);

  if (Array.isArray(a)) {
    if (!Array.isArray(b) || a.length !== b.length) return false;

    for (let i = 0; i < a.length; i++) {
      if (!equal(a[i], b[i], visited)) return false;
    }

    return true;
  }

  if (Array.isArray(b)) return false;

  const left = a as Record<string, unknown>;
  const right = b as Record<string, unknown>;
  const keys = Object.keys(left);

  if (keys.length !== Object.keys(right).length) return false;

  for (const key of keys) {
    if (!equal(left[key], right[key], visited)) return false;
  }

  return true;
}
