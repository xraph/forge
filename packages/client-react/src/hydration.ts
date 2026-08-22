import type { ReactNode } from 'react';
import { hydrateBoundary } from '@forge-go/client-core';
import type { DehydratedState, OperationMeta, QueryCache } from '@forge-go/client-core';
import { useClient } from './context.js';

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
 *
 * The once-per-payload bookkeeping and the decision about which refusals a page
 * survives both live in `hydrateBoundary`, because the Vue and Angular
 * boundaries need exactly the same two things and three copies of a security
 * posture is three postures waiting to drift apart.
 */
export function HydrationBoundary(props: HydrationBoundaryProps): ReactNode {
  const client = useClient(props.client);

  hydrateBoundary(client, props.state, {
    ops: props.ops,
    ...(props.stale === true ? { stale: true } : {}),
  });

  return props.children ?? null;
}
