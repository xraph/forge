import type { QueryCache } from '@forge-go/client-core';
import type { EventLog } from './log.js';

/** The kinds an `ActionLog` can carry. Kept beside the calls that record them. */
type ActionKind = 'refetch' | 'invalidate' | 'invalidateTag' | 'evict' | 'drop' | 'clear';

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
  };
}
