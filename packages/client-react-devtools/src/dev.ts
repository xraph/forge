import { useEffect, useSyncExternalStore } from 'react';
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

/**
 * `useForgeDevtools` reads `attached` through `useSyncExternalStore`, which
 * needs a way to be told the store changed. A plain module-level `Map` has
 * none on its own -- these two are it, notified only where the value
 * `attached.get(cache)?.devtools` can actually change identity: the first
 * attach and the final dispose. A ref-count join or release never changes
 * which `Devtools` a cache is holding, so it doesn't notify.
 */
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);

  return () => listeners.delete(listener);
}

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
  notify();

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

  notify();
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
    // Whether *this* effect run currently owns one reference on `client`'s
    // entry in `attached`. `acquire()` can take that reference either
    // synchronously (joining an already-attached cache bumps `refs` before
    // any `await`) or only after its dynamic import resolves (the first
    // attacher). Either way, `held` flips to `true` the instant `acquire()`
    // returns to us, which is the one point both paths pass through.
    //
    // Cleanup and every early return below call the same `releaseOnce`,
    // which only acts while `held` is true and flips it straight back to
    // `false`. Without that guard, a component that unmounts in the window
    // between "acquire took the ref synchronously" and "our own `await
    // acquire(...)` observed it" gets released once by cleanup and a second
    // time when the async body resumes and sees `live` is now false -- two
    // decrements for one acquire, which can dispose another component's
    // still-live inspector out from under it.
    let held = false;

    const releaseOnce = (): void => {
      if (!held) return;

      held = false;
      release(client);
    };

    // The `development` export condition is what a production build is meant
    // to route around, but a bundler that ignores export conditions entirely
    // would still resolve to this file. This guard is what drops the dynamic
    // imports below out of that build too: written bare so it folds, matching
    // the guard this component replaces at the call site.
    if (process.env.NODE_ENV !== 'production') {
      void (async () => {
        await acquire(client, { limit, frames, manager, binder });
        held = true;

        if (!live) {
          releaseOnce();

          return;
        }

        const entry = attached.get(client);

        if (entry === undefined) return;

        if (panel) {
          const { mountPanel } = await import('@forge-go/client-devtools/panel');

          if (!live) {
            releaseOnce();

            return;
          }

          unmount = mountPanel(entry.devtools, { open });
        } else {
          const { mountOverlay } = await import('@forge-go/client-devtools/overlay');

          if (!live) {
            releaseOnce();

            return;
          }

          unmount = mountOverlay(entry.devtools, { open });
        }
      })();
    }

    return () => {
      live = false;
      unmount?.();
      unmount = undefined;
      releaseOnce();
    };
  }, [client, panel, open, frames, limit, manager, binder]);

  return null;
}

/**
 * The inspector for this cache, once it has attached.
 *
 * Attaching happens inside a dynamic import, so it is never done by the time
 * this first renders: seeding from `attached.get(cache)` and only
 * re-checking it in an effect keyed on `cache` would seed `undefined` and
 * then never look again, since `cache` itself doesn't change when the attach
 * lands. `useSyncExternalStore` subscribed to `acquire`/`release`'s own
 * `notify()` is what makes this actually update once the inspector exists,
 * and again once it's gone.
 */
export function useForgeDevtools(client?: QueryCache): Devtools | undefined {
  const cache = useClient(client);

  return useSyncExternalStore(subscribe, () => attached.get(cache)?.devtools);
}
