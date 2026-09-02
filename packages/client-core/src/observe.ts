import type { QueryStatus } from './cache.js';
import type { StreamFrame } from './live.js';
import type { TagContext } from './tags.js';
import type { OperationMeta } from './transport.js';

/**
 * The one observation seam the cache offers, and the whole of what
 * `@forge-go/client-devtools` is built on.
 *
 * **This module contains types and nothing else.** It compiles to an empty
 * file, so importing it costs nothing, and the field it types --
 * `QueryCache.observer` -- is a bare declaration with no initializer, which
 * under this package's `target` emits no code either. What a production bundle
 * pays for the seam is the handful of `this.observer?.(...)` expressions at the
 * emit sites: one property load and one nullish check each, and **no allocation
 * at all** when nothing is attached, because an optional call does not evaluate
 * its arguments when the callee is nullish.
 *
 * The cache keeps no history. Every event is handed to the observer and
 * forgotten; the ring buffer, the causal attribution and the analysis all live
 * in the devtools package, which a production build never imports. A core that
 * retained a log nobody reads would be the defect this design exists to avoid.
 *
 * One observer, not a list. Two consumers of a debug seam is not a real
 * scenario, a list costs an array in every bundle, and a single slot makes the
 * "who has it" question answerable by looking at one field. Assigning over an
 * existing observer replaces it; `Devtools.dispose` restores whatever it found.
 */
export type CacheObserver = (event: CacheEvent) => void;

/**
 * Everything the cache reports.
 *
 * Deliberately small and deliberately *causal*. Each event either says what a
 * query did or says what caused it, and the pairing of the two is what answers
 * "why did this refetch". Nothing here carries a response body: an observer
 * that wants one is holding the object it was handed for exactly as long as the
 * synchronous call lasts, and the devtools reduce it to resolved tags before
 * returning.
 */
export type CacheEvent =
  /**
   * A tracked query changed state: a request went out, one settled, or one
   * failed. `fetching` going false to true is a dispatch; going true to false
   * is an arrival, and `status` says which kind.
   *
   * One event for all of it because `notify` is the single choke point every
   * transition already passes through, and a seam that costs one expression is
   * a seam that can be justified in a package with a byte budget.
   */
  | {
      readonly type: 'query';
      readonly key: string;
      readonly status: QueryStatus;
      readonly fetching: boolean;
    }
  /** A mutation settled, immediately before its tags are applied. The cause. */
  | {
      readonly type: 'mutation';
      readonly meta: OperationMeta;
      readonly args: TagContext;
      readonly response: unknown;
    }
  /**
   * A batch of stream frames committed, with the tags it raised. Also a cause.
   *
   * `frames` is the batch itself, which the emit site already holds, so it
   * costs the event object one property and costs an unattached cache nothing
   * at all -- an optional call does not evaluate its arguments. Each frame
   * carries its binding, so an observer can name the channel, the message and
   * the intent behind an invalidation without a second seam for each.
   *
   * The payloads on it are live, on the same terms as `mutation.response`:
   * read them inside the synchronous call, copy anything you keep.
   */
  | {
      readonly type: 'frames';
      readonly count: number;
      readonly tags: ReadonlySet<string>;
      readonly frames: readonly StreamFrame[];
    }
  /** One mounted query was hit, with the tags of the cause that reached it. */
  | {
      readonly type: 'invalidated';
      readonly key: string;
      readonly matched: ReadonlySet<string>;
    }
  /** A placement callback answered for this query, so no refetch is owed. */
  | { readonly type: 'placed'; readonly key: string };

/**
 * Deliberately *not* an event: the identity change.
 *
 * `QueryCache.watchPrincipal` already reports it, to anyone, with the cache
 * already emptied -- so a sixth emit site would be a second way to learn the
 * same fact, paid for by every production bundle. An observer that needs the
 * boundary subscribes there.
 */

