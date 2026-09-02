import { resolveTags } from '@forge-go/client-core';
import type {
  BinderSnapshot,
  CacheEvent,
  CacheObserver,
  OperationMeta,
  QueryCache,
  SocketSnapshot,
  StreamBinder,
  SubscriptionManager,
  TagContext,
} from '@forge-go/client-core';
import { createActions } from './actions.js';
import type { DevtoolsActions } from './actions.js';
import { argsKey, causeOf, operationName, whyNotRefetched, whyRefetched, wouldInvalidate } from './explain.js';
import type { MissCause } from './explain.js';
import { capture, FrameRing } from './frames.js';
import * as read from './inspect.js';
import type { EntityFilter } from './inspect.js';
import { EventLog } from './log.js';
import type {
  CacheSnapshot,
  EntitySnapshot,
  FrameCapture,
  InvalidationPreview,
  LogEntry,
  MissReport,
  QueryDetail,
  QuerySnapshot,
  RefetchReport,
  StoreSnapshot,
  TagSnapshot,
} from './types.js';

export interface DevtoolsOptions {
  /**
   * How many events the log holds before the oldest are overwritten.
   *
   * 500 is roughly a minute of a busy application. Raising it raises the
   * memory the tool costs and nothing else; the entries themselves are already
   * bounded in size, so the product is predictable.
   */
  readonly limit?: number;
  /** Where timestamps come from. A test passes a counter; the default is the clock. */
  readonly now?: () => number;
  /**
   * The stream runtime's subscription manager.
   *
   * Only needed when the application built one without a `StreamBinder` --
   * otherwise it is found through `cache.live`.
   */
  readonly manager?: SubscriptionManager;
  /** How much of a query's arguments to keep in a log entry. */
  readonly argsLimit?: number;
  /**
   * The stream binder, when the application holds one the cache does not.
   *
   * `cache.live` is the binder in every wiring this package has seen, so this
   * is the same escape hatch `manager` is, for the same reason.
   */
  readonly binder?: StreamBinder;
  /**
   * Keep the last N decoded stream frames, payloads included.
   *
   * Off by default, and the only thing in this package that retains a payload.
   * See `FrameCapture`.
   *
   * `limit` is optional and a bare `{}` turns capture on at
   * `DEFAULT_FRAME_LIMIT`. Presence is the switch, not the number: reading an
   * empty object back as zero would have made `{ frames: {} }` a way to ask
   * for capture and silently get none, which is the kind of nothing that
   * takes an hour to notice.
   */
  readonly frames?: { readonly limit?: number };
}

/**
 * How many query keys the recorder will track before it prunes.
 *
 * The recorder holds three small maps keyed by query key, and a query key
 * includes its arguments -- so a search box calling `useOrderList({q})` on
 * every keystroke mints a new one per keystroke. The cache itself is bounded by
 * its LRU cap; these maps are not, unless they are made so. Pruning drops the
 * keys the registry no longer remembers, which is exactly the set that can
 * never be asked about again.
 */
const TRACK_LIMIT = 512;

/**
 * How many frames are kept when capture is asked for without a number.
 *
 * The figure the README has always used in its example. Big enough to hold the
 * burst around whatever you are looking at, small enough that leaving it on is
 * not a decision.
 */
const DEFAULT_FRAME_LIMIT = 200;

/**
 * The inspector.
 *
 * Framework-agnostic by construction: it takes a `QueryCache` and returns an
 * object of plain functions. Everything a UI could want to show is a method
 * here, and the overlay in `./overlay` is a view over this and nothing more --
 * which is what lets a Vue or an Angular application build its own panel, or
 * none at all and just call `explain()` from the console.
 *
 * Attaching claims `cache.observer`, chaining to whatever was already there and
 * restoring it on `dispose`. The cache keeps no history of its own; every event
 * it emits is either recorded here or forgotten, which is what makes the
 * "production must not retain a log nobody reads" property true rather than
 * merely intended.
 */
export interface Devtools {
  /** How many events the log holds. */
  readonly capacity: number;
  /** How many have been overwritten and are gone for good. */
  readonly dropped: number;
  /** How many identity changes have happened since attaching. */
  readonly session: number;

  /** Counters, queries and the tag graph, in one read. */
  snapshot(): CacheSnapshot;
  /** The counters alone. */
  store(): StoreSnapshot;
  /** Every query the registry remembers, mounted or not. */
  queries(): QuerySnapshot[];
  query(key: string): QuerySnapshot | undefined;
  /** One query, joined across the registry and the record it is running in. */
  detail(key: string): QueryDetail | undefined;
  /** The stream binder: bindings, live queries, queue depth, gap recovery. */
  streams(): BinderSnapshot | undefined;
  /** What the cache holds for one entity, and which queries depend on it. */
  entity(key: string): EntitySnapshot | undefined;
  entities(filter?: EntityFilter): EntitySnapshot[];
  /** Which queries reached this entity. */
  dependents(key: string): QuerySnapshot[];
  /** Every tag, and who carries it. */
  tags(): TagSnapshot[];
  /** Which sockets are open, for which channels, with what ref count. */
  sockets(): readonly SocketSnapshot[];

  /** The log, oldest first. */
  log(): LogEntry[];
  /** Drop every recorded event. The cache is untouched. */
  clear(): void;
  /** Be told about each event as it is recorded. */
  subscribe(listener: (entry: LogEntry) => void): () => void;

  /** Why did this query refetch? */
  whyRefetched(key: string): RefetchReport | undefined;
  /**
   * Why did this query NOT refetch?
   *
   * With no cause, the most recent recorded mutation or frame batch is used.
   */
  whyNotRefetched(key: string, cause?: MissCause): MissReport;
  /**
   * The one to call when you do not know which question you have.
   *
   * Returns the refetch explanation if the query refetched after the cause, and
   * the miss explanation otherwise.
   */
  explain(key: string): RefetchReport | MissReport;
  /** What would this operation invalidate, and who would it reach? */
  wouldInvalidate(meta: OperationMeta, args?: TagContext, response?: unknown): InvalidationPreview;

  /**
   * The mutating half. See `actions.ts`.
   *
   * Separate from everything above it on purpose: every other method on this
   * interface is a read that cannot move the cache, and this one property is
   * the whole of what can.
   */
  readonly actions: DevtoolsActions;

  /** The most recent thing that raised tags, if the log still holds one. */
  lastCause(): LogEntry | undefined;

  /** Whether frame capture is on. See `DevtoolsOptions.frames`. */
  readonly capturing: boolean;
  /** The captured frames, oldest first. Empty unless capture was asked for. */
  frames(): FrameCapture[];

  /** Stop observing and restore whatever observer was there before. */
  dispose(): void;
}

/**
 * Start observing a cache.
 *
 * ```ts
 * if (import.meta.env.DEV) {
 *   const devtools = (await import('@forge-go/client-devtools')).attach(client);
 *   (globalThis as Record<string, unknown>).forge = devtools;
 * }
 * ```
 *
 * The dynamic import is the point: written that way, a production build
 * contains neither this package nor the branch that would have loaded it.
 */
export function attach(cache: QueryCache, options: DevtoolsOptions = {}): Devtools {
  const clock = options.now ?? Date.now;
  const log = new EventLog(options.limit ?? 500, clock);
  const limit = options.argsLimit ?? 200;
  // Presence, then the number. `options.frames?.limit ?? 0` read `{}` as zero
  // and quietly left capture off for a caller who had just asked for it.
  const frameLimit =
    options.frames === undefined ? 0 : (options.frames.limit ?? DEFAULT_FRAME_LIMIT);
  const ring = frameLimit > 0 ? new FrameRing(frameLimit) : undefined;

  /** Last known `fetching` per query, so a transition can be told from a repeat. */
  const fetching = new Map<string, boolean>();
  /** Keys that have been fetched at least once, to tell a mount from a refetch. */
  const seen = new Set<string>();
  /** Per query, the cause that made it stale, awaiting the request it produces. */
  const pending = new Map<string, number>();

  let session = 0;
  let owner = cache.owner;
  let disposed = false;
  /**
   * The cause the next invalidation belongs to.
   *
   * Set by a mutation or a frame batch, consumed by the invalidations that
   * follow it synchronously, and cleared by any other event. That rule is sound
   * because of the order the core emits in: `mutate` reports itself and then
   * calls `settled`, which reaches every hit query before control returns. An
   * invalidation raised by a bare `cache.invalidate(tags)` -- which has no cause
   * of its own -- is therefore attributed to nothing, which is the honest
   * answer rather than the previous mutation's.
   */
  let cause: number | undefined;

  const previous = cache.observer;

  const prune = (): void => {
    if (fetching.size <= TRACK_LIMIT) return;

    for (const key of [...fetching.keys()]) {
      if (cache.registry.get(key) === undefined) {
        fetching.delete(key);
        seen.delete(key);
        pending.delete(key);
      }
    }

    // Still over, which means the cache itself is holding more than the tracker
    // will. Start again rather than grow: the only consequence is that the next
    // request for a forgotten key is logged as a `mount` rather than a refetch.
    if (fetching.size > TRACK_LIMIT) {
      fetching.clear();
      seen.clear();
      pending.clear();
    }
  };

  /**
   * Close the session if the identity has changed, before recording anything
   * under the wrong one.
   *
   * Checked here rather than only from `watchPrincipal`, and the ordering is
   * the reason: `setPrincipal` assigns the new principal and *then* calls
   * `clear`, which re-mounts and re-fetches every watched query -- so a whole
   * burst of events arrives before the principal watchers ever run. Attributing
   * those to the previous session would put the boundary in the wrong place and
   * label the re-fetches as refetches of queries whose data no longer exists.
   * `cache.owner` is already the new value by then, so one comparison per event
   * puts the divider where it belongs.
   */
  const checkPrincipal = (): void => {
    if (cache.owner === owner) return;

    owner = cache.owner;
    session++;
    fetching.clear();
    seen.clear();
    pending.clear();
    cause = undefined;
    log.push({ kind: 'principal', session });
  };

  const observer: CacheObserver = (event: CacheEvent) => {
    if (disposed) {
      previous?.(event);

      return;
    }

    checkPrincipal();

    switch (event.type) {
      case 'mutation': {
        const resolved = resolveTags(event.meta.invalidates, {
          ...event.args,
          response: event.response,
        });

        cause = log.push({
          kind: 'mutation',
          session,
          operation: operationName(event.meta),
          args: argsKey(event.args, limit),
          tags: resolved.tags,
          unresolved: resolved.unresolved,
        }).seq;

        break;
      }

      case 'frames': {
        if (ring !== undefined) {
          for (const frame of event.frames) {
            ring.push({
              seq: log.sequence,
              at: clock(),
              channel: frame.binding.channel,
              message: frame.binding.message,
              intent: frame.binding.intent,
              entity: frame.binding.entity,
              payload: capture(frame.payload),
            });
          }
        }

        cause = log.push({
          kind: 'frames',
          session,
          frames: event.count,
          tags: [...event.tags],
        }).seq;

        break;
      }

      case 'invalidated': {
        log.push({
          kind: 'invalidated',
          session,
          query: event.key,
          matched: [...event.matched],
          cause,
        });

        if (cause !== undefined) pending.set(event.key, cause);

        break;
      }

      case 'placed': {
        log.push({ kind: 'placed', session, query: event.key, cause });
        // Placement answers *instead of* a refetch, so the pending attribution
        // is spent here rather than waiting for a request that will not come.
        pending.delete(event.key);

        break;
      }

      case 'query': {
        const before = fetching.get(event.key) ?? false;

        fetching.set(event.key, event.fetching);

        // A request going out or coming back closes the cause: whatever raised
        // those tags has finished raising them. A notification with `fetching`
        // unchanged does *not*, and that distinction is load-bearing --
        // `applyFrames` reports itself, then calls `notifyChanged`, which
        // notifies every query a patch moved before a single tag is applied.
        // Clearing on those would leave every stream invalidation attributed to
        // nothing.
        if (before !== event.fetching) cause = undefined;

        if (!before && event.fetching) {
          const attributed = pending.get(event.key);

          pending.delete(event.key);

          log.push({
            kind: 'fetch',
            session,
            query: event.key,
            reason:
              attributed !== undefined ? 'invalidation' : seen.has(event.key) ? 'manual' : 'mount',
            cause: attributed,
          });

          seen.add(event.key);
          prune();

          break;
        }

        // A request that was in flight has arrived. Only this transition is
        // logged: a notification with `fetching` unchanged is a placement or a
        // clear, and both have their own entry.
        if (before && !event.fetching) {
          if (event.status === 'error') {
            log.push({ kind: 'error', session, query: event.key, message: 'request failed' });
          } else {
            log.push({ kind: 'settle', session, query: event.key, version: cache.store.version });
          }
        }

        break;
      }
    }

    previous?.(event);
  };

  cache.observer = observer;

  // The backstop, for an identity change that produces no events at all -- a
  // logout with nothing mounted. `checkPrincipal` is idempotent, so whichever
  // of the two paths notices first is the one that records the boundary.
  const unwatch = cache.watchPrincipal(checkPrincipal);

  const lastCause = (): LogEntry | undefined =>
    log.last((entry) => entry.kind === 'mutation' || entry.kind === 'frames');

  const missCause = (given?: MissCause): MissCause => {
    if (given !== undefined) return given;

    const entry = lastCause();
    const summary = entry === undefined ? undefined : causeOf(entry);

    if (summary === undefined) {
      return { tags: [], label: 'nothing (no mutation or frame batch is in the log)' };
    }

    return {
      tags: summary.tags,
      unresolved: summary.unresolved,
      label: summary.label,
      seq: summary.seq,
    };
  };

  const actions = createActions(cache, log, () => session);

  return {
    get capacity() {
      return log.capacity;
    },
    get dropped() {
      return log.dropped;
    },
    get session() {
      return session;
    },

    snapshot: () => read.snapshot(cache),
    store: () => read.store(cache),
    queries: () => read.queries(cache),
    query: (key) => read.query(cache, key),
    detail: (key) => read.detail(cache, key),
    streams: () => read.binderView(cache, options.binder),
    entity: (key) => read.entity(cache, key),
    entities: (filter) => read.entities(cache, filter),
    dependents: (key) => read.dependents(cache, key),
    tags: () => read.tags(cache),
    sockets: () => read.sockets(cache, options.manager),

    log: () => log.entries(),
    clear: () => {
      log.clear();
      ring?.clear();
    },
    subscribe: (listener) => log.subscribe(listener),

    whyRefetched: (key) => whyRefetched(log, key),
    whyNotRefetched: (key, given) => whyNotRefetched(cache, key, missCause(given), log),

    explain(key) {
      const report = whyNotRefetched(cache, key, missCause(), log);

      // It did refetch, so "why not" is the wrong question and the causal
      // history is the right answer. Falls back to the miss report when the log
      // has no dispatch for it -- which is itself informative.
      if (report.outcome === 'refetched') return whyRefetched(log, key) ?? report;

      return report;
    },

    wouldInvalidate: (meta, args, response) => wouldInvalidate(cache, meta, args, response),
    actions,
    lastCause,

    get capturing() {
      return ring !== undefined;
    },
    frames: () => ring?.entries() ?? [],

    dispose() {
      unwatch();
      // Inert either way. When a second inspector has attached over this one,
      // the slot cannot be given back without unhooking it -- so this observer
      // stays in the chain as a pass-through and stops recording. Going quiet
      // rather than staying live matters: a disposed inspector that kept
      // filling its ring would be a leak with no reader.
      disposed = true;

      // Only if it is still ours. See above.
      if (cache.observer === observer) cache.observer = previous;
    },
  };
}
