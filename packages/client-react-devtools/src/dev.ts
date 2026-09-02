import { useEffect, useState } from 'react';
import type { QueryCache, StreamBinder, SubscriptionManager } from '@forge-go/client-core';
import type { Devtools } from '@forge-go/client-devtools';
import { useClient } from '@forge-go/client-react';

export interface ForgeDevtoolsProps {
  /** Beats the provider and the module default, as everywhere else. */
  readonly client?: QueryCache;
  /** The full panel. `false` mounts the lean overlay instead. Defaults to true. */
  readonly panel?: boolean;
  /** Start open rather than as a button in the corner. */
  readonly open?: boolean;
  /** Keep the last N stream frames, payloads included. Off by default. */
  readonly frames?: number;
  /** How many events the causal log holds. */
  readonly limit?: number;
  readonly manager?: SubscriptionManager;
  readonly binder?: StreamBinder;
}

/**
 * One inspector per cache, however many components ask for it.
 *
 * `attach` claims `cache.observer`, a single slot, and gives it back on
 * `dispose`. React 18 StrictMode double-invokes effects in development, which
 * is exactly where this component runs, so mount-mount-unmount-unmount against
 * that slot would either leave a panel behind or restore a stale observer over
 * a live one. Refcounting per cache makes both impossible: a second mount
 * joins the first, and the slot comes back when the last holder leaves.
 */
const attached = new Map<QueryCache, { devtools: Devtools; refs: number }>();

interface AcquireOptions {
  readonly limit?: number;
  readonly frames?: number;
  readonly manager?: SubscriptionManager;
  readonly binder?: StreamBinder;
}

async function acquire(cache: QueryCache, options: AcquireOptions): Promise<Devtools> {
  const existing = attached.get(cache);

  if (existing !== undefined) {
    existing.refs++;

    return existing.devtools;
  }

  const { attach } = await import('@forge-go/client-devtools');
  // Two components mounting in the same tick both await this import. Whichever
  // resolves second finds the first one's entry and joins it, rather than
  // attaching a second inspector over the top of it.
  const again = attached.get(cache);

  if (again !== undefined) {
    again.refs++;

    return again.devtools;
  }

  const devtools = attach(cache, {
    limit: options.limit,
    manager: options.manager,
    binder: options.binder,
    frames: options.frames === undefined ? undefined : { limit: options.frames },
  });

  attached.set(cache, { devtools, refs: 1 });
  (globalThis as Record<string, unknown>)['forge'] = devtools;

  return devtools;
}

function release(cache: QueryCache): void {
  const held = attached.get(cache);

  if (held === undefined) return;

  held.refs--;

  if (held.refs > 0) return;

  attached.delete(cache);
  held.devtools.dispose();

  if ((globalThis as Record<string, unknown>)['forge'] === held.devtools) {
    delete (globalThis as Record<string, unknown>)['forge'];
  }
}

/**
 * Mount the devtools from React, and nothing else.
 *
 * Renders `null`. The panel goes into its own shadow root on `document.body`,
 * so React never owns it: your re-renders cannot disturb it and your CSS
 * cannot reach it. That is the argument the panel itself makes about
 * frameworks, drawn one level out.
 */
export function ForgeDevtools(props: ForgeDevtoolsProps = {}): null {
  const client = useClient(props.client);
  const { panel = true, open, frames, limit, manager, binder } = props;

  useEffect(() => {
    let live = true;
    let unmount: (() => void) | undefined;

    // The `development` export condition is what a production build is meant
    // to route around, but a bundler that ignores export conditions entirely
    // would still resolve to this file. This guard is what drops the dynamic
    // imports below out of that build too: written bare so it folds, matching
    // the guard this component replaces at the call site.
    if (process.env.NODE_ENV !== 'production') {
      void (async () => {
        await acquire(client, { limit, frames, manager, binder });

        if (!live) {
          release(client);

          return;
        }

        const held = attached.get(client);

        if (held === undefined) return;

        if (panel) {
          const { mountPanel } = await import('@forge-go/client-devtools/panel');

          if (!live) {
            release(client);

            return;
          }

          unmount = mountPanel(held.devtools, { open });
        } else {
          const { mountOverlay } = await import('@forge-go/client-devtools/overlay');

          if (!live) {
            release(client);

            return;
          }

          unmount = mountOverlay(held.devtools, { open });
        }
      })();
    }

    return () => {
      live = false;
      unmount?.();
      unmount = undefined;
      release(client);
    };
  }, [client, panel, open, frames, limit, manager, binder]);

  return null;
}

/** The inspector for this cache, once it has attached. */
export function useForgeDevtools(client?: QueryCache): Devtools | undefined {
  const cache = useClient(client);
  const [devtools, setDevtools] = useState<Devtools | undefined>(
    () => attached.get(cache)?.devtools,
  );

  useEffect(() => {
    setDevtools(attached.get(cache)?.devtools);
  }, [cache]);

  return devtools;
}
