import type { OverlayLayer } from './store';
import type { EntityKey, EntityRecord } from './types';

/**
 * How a merge patch produces its fields.
 *
 * A function rather than only a literal, and this is the whole reason
 * concurrent optimistic mutations compose. A literal captured at call time --
 * `{likes: order.likes + 1}` -- is a value computed against whatever the base
 * happened to be then, so replaying it on a different base replays the wrong
 * number. A function is re-run on every refold, against the base the refold is
 * actually standing on, so two pending increments show 2 and dropping the first
 * shows 1 rather than 0.
 */
export type MergeSource =
  | Record<string, unknown>
  | ((prev: Record<string, unknown>) => Record<string, unknown>);

/**
 * One entity's change, as a patch rather than as a value.
 *
 * `merge` over a base record that does not exist is a NO-OP rather than a
 * create. That single rule is what lets an evicting stream frame beat a pending
 * local edit without a special case anywhere: the row is gone, and a patch to
 * something that is gone patches nothing.
 */
export type EntityPatch =
  | { readonly kind: 'merge'; readonly source: MergeSource }
  | { readonly kind: 'create'; readonly fields: Record<string, unknown> }
  | { readonly kind: 'delete' };

/** One pending mutation's whole contribution. */
export interface OverlayEntry {
  readonly id: number;
  readonly patches: ReadonlyMap<EntityKey, EntityPatch>;
  /** The mutation's placement callbacks, for the membership plane. */
  readonly place: Readonly<Record<string, unknown>> | undefined;
  /** Its `invalidates`, already resolved against its arguments. */
  readonly tags: readonly string[];
  /** The minted key, for a create. */
  readonly created: EntityKey | undefined;
}

/** What the stack needs of the store. Narrowed so tests can drive it directly. */
export interface OverlayHost {
  getRecord(key: EntityKey): EntityRecord | undefined;
  touch(keys: Iterable<EntityKey>): void;
  read<T = unknown>(skeleton: unknown): T;
  put(key: EntityKey, data: Readonly<Record<string, unknown>>, frameAt?: number): boolean;
  evict(key: EntityKey, frameAt?: number): boolean;
}

/**
 * The ordered stack of pending optimistic changes.
 *
 * Nothing here ever writes the base store outside `promote`. What a subscriber
 * sees is `fold(base, patches in push order)`, recomputed on demand, which is
 * what makes rollback the removal of an entry rather than the application of an
 * inverse. An inverse is the thing that goes wrong under concurrency: the one
 * recorded for the second mutation was computed against a base that already
 * included the first, so when the FIRST fails it restores a state that never
 * existed. There is no inverse in this file.
 */
export class OverlayStack implements OverlayLayer {
  private readonly entries: OverlayEntry[] = [];
  /** Memoized folds, dropped per key when the stack or that key's base moves. */
  private readonly folded = new Map<EntityKey, EntityRecord | undefined>();
  private ids = 0;
  private stamp = 0;

  constructor(
    private readonly host: OverlayHost,
    private readonly report?: (error: unknown, context: string) => void,
  ) {}

  /** Bumps on every push and every drop. Keys the projection memo in Task 5. */
  get version(): number {
    return this.stamp;
  }

  /** The early-out every hot path checks first. True for most applications. */
  get empty(): boolean {
    return this.entries.length === 0;
  }

  /** Every key any live overlay touches. */
  keys(): Set<EntityKey> {
    const keys = new Set<EntityKey>();

    for (const entry of this.entries) {
      for (const key of entry.patches.keys()) keys.add(key);
    }

    return keys;
  }

  holds(key: EntityKey): boolean {
    for (const entry of this.entries) {
      if (entry.patches.has(key)) return true;
    }

    return false;
  }

  effective(key: EntityKey): EntityRecord | undefined {
    if (this.folded.has(key)) return this.folded.get(key);

    const record = this.fold(key);
    this.folded.set(key, record);

    return record;
  }

  /**
   * The base for this key moved, so the memoized fold is void.
   *
   * Only the memo is dropped: `put` and `evict` have already invalidated the
   * store's own memos for this key, and invalidating them twice would cost a
   * second walk of the dependency graph for one change.
   */
  rebase(key: EntityKey): void {
    this.folded.delete(key);
  }

  /** Push one overlay and return its id. */
  add(
    patches: ReadonlyMap<EntityKey, EntityPatch>,
    place?: Readonly<Record<string, unknown>>,
    tags: readonly string[] = [],
    created?: EntityKey,
  ): number {
    const entry: OverlayEntry = { id: ++this.ids, patches, place, tags, created };

    this.entries.push(entry);
    this.settle(entry.patches.keys());

    return entry.id;
  }

  /** Remove one overlay and return it. The whole of rollback. */
  take(id: number): OverlayEntry | undefined {
    const at = this.entries.findIndex((entry) => entry.id === id);

    if (at < 0) return undefined;

    const [entry] = this.entries.splice(at, 1) as [OverlayEntry];
    this.settle(entry.patches.keys());

    return entry;
  }

  /**
   * Make a taken overlay's effects permanent in base, and report the keys it
   * deleted so the response commit can skip them.
   *
   * Called with an entry that is ALREADY off the stack, so a computed merge is
   * evaluated against base alone and cannot be applied twice -- once by this
   * write and once by a fold that still contains it.
   *
   * A `create` is never promoted. Its temp key exists only to be rendered; the
   * real entity arrives in the response, and writing `Order:~opt1` into base
   * would leave a record nothing ever removes.
   */
  promote(entry: OverlayEntry): EntityKey[] {
    const buried: EntityKey[] = [];

    for (const [key, patch] of entry.patches) {
      if (patch.kind === 'create') continue;

      if (patch.kind === 'delete') {
        this.host.evict(key);
        buried.push(key);
        continue;
      }

      const base = this.host.getRecord(key);

      if (base === undefined) continue;

      this.host.put(key, this.apply(patch.source, base.data));
    }

    return buried;
  }

  /** Drop everything. The identity-change path: a pending edit is not portable. */
  clear(): void {
    if (this.entries.length === 0) return;

    const keys = this.keys();
    this.entries.length = 0;
    this.settle(keys);
  }

  /** Void the memos for these keys and tell the store to re-read them. */
  private settle(keys: Iterable<EntityKey>): void {
    const touched = [...keys];

    for (const key of touched) this.folded.delete(key);

    this.stamp++;
    this.host.touch(touched);
  }

  private fold(key: EntityKey): EntityRecord | undefined {
    const base = this.host.getRecord(key);
    let data = base?.data as Record<string, unknown> | undefined;
    let touched = false;

    for (const entry of this.entries) {
      const patch = entry.patches.get(key);

      if (patch === undefined) continue;

      touched = true;

      if (patch.kind === 'delete') {
        data = undefined;
        continue;
      }

      if (patch.kind === 'create') {
        data = { ...(data ?? {}), ...patch.fields };
        continue;
      }

      // A merge over a hole patches nothing. See `EntityPatch`.
      if (data === undefined) continue;

      data = { ...data, ...this.apply(patch.source, data) };
    }

    if (!touched) return base;
    if (data === undefined) return undefined;

    return { data, version: base?.version ?? 1, frameAt: base?.frameAt ?? 0 };
  }

  /**
   * Evaluate one merge source.
   *
   * A throwing callback must not take a render down: the value it would have
   * produced is unknown, so the honest answer is "no change from this patch",
   * and the throw is reported rather than swallowed.
   */
  private apply(
    source: MergeSource,
    prev: Readonly<Record<string, unknown>>,
  ): Record<string, unknown> {
    if (typeof source !== 'function') return source;

    try {
      return source(prev as Record<string, unknown>);
    } catch (error) {
      this.report?.(error, 'optimistic');

      return {};
    }
  }
}
