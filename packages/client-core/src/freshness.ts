import type { QueryCache } from './cache.js';
import type { EventTargetLike } from './stream.js';
import type { TagContext } from './tags.js';
import { realSleep } from './transport.js';
import type { OperationMeta, Sleep } from './transport.js';

/**
 * Ambient revalidation: the browser telling the cache that time has passed in
 * ways a tag graph cannot see.
 *
 * A separate module, and this is the whole reason the design works. `size-limit`
 * bills by what `QueryCache` references, so anything reachable from the cache is
 * paid for by every consumer. Nothing in `cache.ts` imports this file, which was
 * measured: with all three installers exported from the package index, the
 * REST-only budgets read exactly what they read with this file deleted.
 *
 * Every installer takes an explicit target, sniffs for one when given none, and
 * returns a disposer. Sniffing once at install rather than at each event is what
 * keeps a server render from registering anything, and it is the same shape
 * `SubscriptionManager` already uses to hear about the network coming back.
 *
 * `resolveTarget` below is deliberately a second implementation of that shape
 * rather than a shared one. `stream.ts`'s `resolveReviveTarget` sniffs
 * `globalThis` and only that; focus revalidation needs `globalThis.document`,
 * so sharing would mean generalising a helper inside the working reconnect
 * path. Ten lines, parameterised on the lookup. Promote it if a third caller
 * ever turns up.
 */

/** What `revalidateOnFocus` reads. Structural, so a test passes a literal. */
export interface VisibilityLike extends EventTargetLike {
  readonly visibilityState?: string;
}

/**
 * Resolve a listen target: `false` is off, an explicit one is used as given,
 * and anything else is sniffed.
 */
function resolveTarget<T extends EventTargetLike>(
  explicit: T | false | undefined,
  sniff: () => unknown,
): T | undefined {
  if (explicit === false) return undefined;
  if (explicit !== undefined) return explicit;

  const candidate = sniff() as Partial<EventTargetLike> | undefined;

  return candidate !== undefined &&
    typeof candidate.addEventListener === 'function' &&
    typeof candidate.removeEventListener === 'function'
    ? (candidate as T)
    : undefined;
}

export interface FocusOptions {
  /** Listen on this instead of sniffing. `false` registers nothing. */
  readonly target?: VisibilityLike | false;
}

/**
 * Revalidate stale queries when the tab becomes visible again.
 *
 * `visibilitychange` rather than `focus`, deliberately. `focus` also fires when
 * you return to a tab that never stopped being visible, refetching for a reader
 * who was looking at the data the entire time.
 *
 * There is no throttle here and none is needed: a query that just refetched is
 * not expired, so `staleTime` is already the rate limit.
 */
export function revalidateOnFocus(cache: QueryCache, options: FocusOptions = {}): () => void {
  const target = resolveTarget<VisibilityLike>(
    options.target,
    () => (globalThis as { document?: unknown }).document,
  );

  if (target === undefined) return () => undefined;

  const handler = (): void => {
    // An absent `visibilityState` means the target does not report visibility,
    // which is a target that only ever fires when it wants attention.
    if (target.visibilityState === undefined || target.visibilityState === 'visible') {
      cache.revalidate();
    }
  };

  target.addEventListener('visibilitychange', handler);

  let stopped = false;

  return () => {
    if (stopped) return;

    stopped = true;
    target.removeEventListener('visibilitychange', handler);
  };
}

export interface ReconnectOptions {
  /** Listen on this instead of sniffing. `false` registers nothing. */
  readonly target?: EventTargetLike | false;
}

/** Revalidate stale queries when the network comes back. */
export function revalidateOnReconnect(
  cache: QueryCache,
  options: ReconnectOptions = {},
): () => void {
  const target = resolveTarget<EventTargetLike>(options.target, () => globalThis);

  if (target === undefined) return () => undefined;

  const handler = (): void => {
    cache.revalidate();
  };

  target.addEventListener('online', handler);

  let stopped = false;

  return () => {
    if (stopped) return;

    stopped = true;
    target.removeEventListener('online', handler);
  };
}

export interface PollOptions {
  /** How a delay is taken. Defaults to `realSleep`. Tests pass a manual clock. */
  readonly sleep?: Sleep;
  /** Keep polling while the document is hidden. Defaults to false. */
  readonly whileHidden?: boolean;
}

/**
 * Refetch one query on an interval. Returns the stop.
 *
 * A loop over an injected `Sleep` rather than `setInterval`, which is the shape
 * `RestTransport` already uses for retry backoff and the reason a polling test
 * can run on a manual clock instead of on wall time. It also cannot stack:
 * the next delay begins after the previous request settled, so a slow endpoint
 * spreads its polls out rather than queueing them.
 *
 * Paused while the document is hidden, because polling a background tab spends
 * requests on a screen nobody is reading.
 */
export function poll(
  cache: QueryCache,
  meta: OperationMeta,
  args: TagContext | undefined,
  intervalMs: number,
  options: PollOptions = {},
): () => void {
  const sleep = options.sleep ?? realSleep;
  let stopped = false;

  void (async () => {
    while (!stopped) {
      await sleep(intervalMs);

      if (stopped) break;

      if (options.whileHidden !== true) {
        const doc = (globalThis as { document?: VisibilityLike }).document;

        if (doc?.visibilityState === 'hidden') continue;
      }

      try {
        await cache.refetch(meta, args);
      } catch {
        // Swallowed on purpose. The cache has already reported this through
        // its own `onError`, and a poll that dies on one failed request is a
        // poll that silently stops refreshing the screen.
      }
    }
  })();

  return () => {
    stopped = true;
  };
}
