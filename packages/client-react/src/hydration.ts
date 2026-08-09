import type { ReactNode } from 'react';
import { hydrate, hydrationFailure } from '@forge-go/client-core';
import type { DehydratedState, OperationMeta, QueryCache } from '@forge-go/client-core';
import { useForgeClient } from './context';

/**
 * Which payloads have already been hydrated into which cache.
 *
 * Keyed on the cache first because the same payload legitimately hydrates two
 * of them -- this component renders on the server as well, against the cache
 * that produced the payload, and again on the client against a fresh one.
 *
 * An optimisation rather than a correctness requirement: `hydrate` merges, and
 * a record written with identical data keeps its previous object and bumps no
 * version. What this buys is that StrictMode's double-invoked render does not
 * walk the payload twice.
 */
const hydrated = new WeakMap<QueryCache, WeakSet<object>>();

export interface HydrationBoundaryProps {
  /** The payload from `dehydrate`, after whatever transport carried it. */
  readonly state: DehydratedState | undefined;
  /** The generated `ops.ts` table, passed verbatim. */
  readonly ops: Readonly<Record<string, OperationMeta>>;
  /** Use this cache rather than the provided or configured one. */
  readonly client?: QueryCache;
  /** Settle the hydrated queries behind the server, so mounting refetches. */
  readonly stale?: boolean;
  readonly children?: ReactNode;
}

/**
 * Hydrate a payload into the cache this subtree reads from.
 *
 * **It hydrates during render, not in an effect.** Children read `getSnapshot`
 * during their own render, which happens after this one returns, so a
 * render-phase hydrate is visible to them on the first pass. An effect runs
 * after the tree commits: the first paint would be the loading branch and then
 * flip, which is a visible flash and, on the hydration pass, exactly the
 * mismatch this component exists to remove.
 *
 * Rendering no element of its own is deliberate. A wrapper would change the DOM
 * the server and the client compare, in a component whose entire job is to make
 * those two agree.
 */
export function HydrationBoundary(props: HydrationBoundaryProps): ReactNode {
  const client = useForgeClient(props.client);
  const { state } = props;

  if (state !== undefined) {
    let seen = hydrated.get(client);

    if (seen === undefined) {
      seen = new WeakSet<object>();
      hydrated.set(client, seen);
    }

    if (!seen.has(state)) {
      try {
        hydrate(client, state, {
          ops: props.ops,
          ...(props.stale === true ? { stale: true } : {}),
        });
      } catch (error) {
        onHydrateError(error, client);
      }

      // **After**, and unreachable when `onHydrateError` rethrows.
      //
      // React retries a render that threw. Marking the payload first would make
      // the retry find it already done, skip hydration, throw nothing, and
      // render the children as though it had succeeded -- turning the one
      // failure this component is required to surface into a silent degrade,
      // with no error boundary ever seeing it. A rethrow has to throw again on
      // every attempt, which is exactly what leaving the mark until here buys.
      //
      // A failure that was reported rather than rethrown does reach this line,
      // so it is recorded once rather than on every subsequent render.
      seen.add(state);
    }
  }

  return props.children ?? null;
}

/**
 * What to do when `hydrate` refuses a payload during render.
 *
 * One rule: **continue only for a failure this code recognises AND that a
 * client-side fetch fully repairs. Rethrow everything else.** Degrading is a
 * claim that the page will still be correct, and that claim can only be made
 * about a failure whose consequences are understood.
 *
 * - `version` continues. A client running older code than the server that
 *   rendered the page is what every deploy produces while old JS is still
 *   cached, `hydrate` rejects such a payload before writing anything, and the
 *   queries simply fetch. Blanking a page for the duration of each rollout
 *   would be a far worse failure than the one being handled.
 * - `operation` continues. It is always a bug, but a recoverable one: the
 *   component fetches its own query, and the report reaches wherever the
 *   application sends `onError`. Taking the page down in production for a
 *   wiring mistake that degrades cleanly is the wrong trade.
 * - `principal` **rethrows**. It is the one refusal that says something is
 *   wrong with *whose data this is*, and it is not repaired by fetching --
 *   whatever routed this payload here is still misrouted. It is also the case
 *   this feature's security rests on, so it fails loudly by construction rather
 *   than by remembering to.
 * - Anything else rethrows, including a failure raised below `hydrate`.
 *   `hydrationFailure` answers `undefined` for a reason a future version adds,
 *   so an unrecognised refusal is treated as unknown rather than as safe.
 *
 * Rethrowing propagates out of render, so the nearest error boundary catches it
 * and the subtree does not mount.
 */
function onHydrateError(error: unknown, client: QueryCache): void {
  const reason = hydrationFailure(error);

  if (reason !== 'version' && reason !== 'operation') throw error;

  client.report(error, 'hydrate');
}
