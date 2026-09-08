import type { QueryCache } from '@forge-go/client-core';
import type { EventLog } from './log.js';

/** The kinds an `ActionLog` can carry. Kept beside the calls that record them. */
type ActionKind =
  | 'refetch'
  | 'invalidate'
  | 'invalidateTag'
  | 'evict'
  | 'drop'
  | 'clear'
  | 'rollback'
  | 'hold'
  | 'release'
  | 'stale';

/**
 * The half that writes.
 *
 * `inspect.ts` opens with the rule that inspection must not mutate, and every
 * line of that file keeps it. This file is the deliberate exception, kept
 * separate so the rule over there stays literally true rather than mostly
 * true, and so a reader can see the whole mutating surface of this package on
 * one screen.
 *
 * Each call does one thing the runtime already does. Nothing here fabricates a
 * state the server could not produce: there is no "pretend this query is
 * loading" and no "pretend it failed", because a cache holding a state no
 * response produced is a new class of confusing bug to have on screen while
 * you debug a real one.
 */
export interface DevtoolsActions {
  /**
   * Run this query again whatever the cache holds.
   *
   * Rejects, rather than resolving or answering false, when nothing is
   * tracking `key`: there is no boolean here for a caller to branch on, only
   * a request that goes nowhere. A button wired to this must catch it.
   */
  refetch(key: string): Promise<unknown>;
  /** Raise the tags this query carries, reaching it and everything sharing them. */
  invalidate(key: string): boolean;
  /** Raise one tag by hand. */
  invalidateTag(tag: string): void;
  /** Drop one entity record. False when the store does not hold it. */
  evict(entityKey: string): boolean;
  /** Forget this query, or reset it if watched. See `QueryCache.drop`. */
  drop(key: string): boolean;
  /** Drop every entity, every skeleton and every registry entry. */
  clear(): void;
  /**
   * Take one pending optimistic write off the stack.
   *
   * Removal, not an inverse. That is the whole of rollback in `OverlayStack`
   * and it is what keeps it correct when an earlier layer fails after a later
   * one has already landed: an inverse recorded for the second was computed
   * against a base that already included the first.
   */
  rollback(id: number): boolean;
  /**
   * Commit one pending optimistic write to the base store by hand.
   *
   * What the runtime does when a mutation settles, minus the response. A
   * `create` is never promoted; its temp key exists only to be rendered, so
   * promoting a layer that minted one writes its merges and drops the create.
   */
  promote(id: number): boolean;
  /**
   * Mark one query behind the server without asking for it again.
   *
   * `invalidate` raises the query's tags, which reaches every query sharing
   * them and refetches the mounted ones. This marks exactly one, and leaves
   * the request for whenever something next needs it, which is the state you
   * want when you are trying to see what a stale read looks like.
   */
  forceStale(key: string): boolean;
  /**
   * Push one hand-written field change onto the overlay stack.
   *
   * Deliberately an overlay and not a store write. See `editBar` in the panel:
   * this rides the same machinery a pending optimistic mutation does, which is
   * what makes it reversible by removal and correctly beaten by an evicting
   * stream frame. Returns the overlay id, for the undo.
   */
  patchEntity(key: string, fields: Readonly<Record<string, unknown>>): number;
  /**
   * Record that the panel is holding a query in a state it did not reach.
   *
   * Nothing in the cache moves. This exists so the trace says a held query was
   * held by you, on the same terms as every other action here.
   */
  hold(key: string, state: string): void;
  release(key: string): void;
}

/**
 * Build the action layer over one cache.
 *
 * `session` is a function rather than a captured number, because an identity
 * change increments it and an action stamped with the previous session would
 * sit in the log describing a cache that no longer exists.
 */
export function createActions(
  cache: QueryCache,
  log: EventLog,
  session: () => number,
): DevtoolsActions {
  const find = (key: string) => {
    for (const record of cache.tracked()) {
      if (record.key === key) return record;
    }

    return undefined;
  };

  const record = (action: ActionKind, target: string): void => {
    log.push({ kind: 'action', session: session(), action, target });
  };

  return {
    refetch(key) {
      const found = find(key);

      if (found === undefined) {
        return Promise.reject(new Error(`[forge] nothing is tracking ${key}`));
      }

      record('refetch', key);

      return cache.refetch(found.meta, found.args);
    },

    invalidate(key) {
      const entry = cache.registry.get(key);

      if (entry === undefined) return false;

      record('invalidate', key);
      cache.invalidate(entry.tags);

      return true;
    },

    invalidateTag(tag) {
      record('invalidateTag', tag);
      cache.invalidate([tag]);
    },

    evict(entityKey) {
      if (!cache.store.has(entityKey)) return false;

      record('evict', entityKey);

      const dropped = cache.store.evict(entityKey);

      cache.notifyChanged();

      return dropped;
    },

    drop(key) {
      if (find(key) === undefined) return false;

      record('drop', key);

      return cache.drop(key);
    },

    clear() {
      record('clear', '*');
      cache.clear();
    },

    rollback(id) {
      const entry = cache.overlays.take(id);

      if (entry === undefined) return false;

      record('rollback', `overlay #${String(id)}`);
      // The stack changed under everything reading through it, so say so.
      cache.notifyChanged();

      return true;
    },

    promote(id) {
      const entry = cache.overlays.take(id);

      if (entry === undefined) return false;

      record('rollback', `promote overlay #${String(id)}`);
      cache.overlays.promote(entry);
      cache.notifyChanged();

      return true;
    },

    forceStale(key) {
      const entry = cache.registry.get(key);

      if (entry === undefined) return false;

      record('stale', key);
      cache.registry.markStale(entry);
      cache.notifyChanged();

      return true;
    },

    patchEntity(key, fields) {
      record('rollback', `patch ${key}`);

      const id = cache.overlays.add(new Map([[key, { kind: 'merge', source: { ...fields } }]]), undefined, []);

      cache.notifyChanged();

      return id;
    },

    hold(key, state) {
      record('hold', `${key} in ${state}`);
    },

    release(key) {
      record('release', key);
    },
  };
}
