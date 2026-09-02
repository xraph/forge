import { manualScheduler, QueryCache } from '@forge-go/client-core';
import { ClientProvider } from '@forge-go/client-react';
import { act, createElement, StrictMode, useEffect } from 'react';
import type { ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';
import type { Devtools } from '@forge-go/client-devtools';
import { ForgeDevtools, useForgeDevtools } from '../src/dev';

function cache(): QueryCache {
  const scheduler = manualScheduler();

  return new QueryCache({
    transport: { execute: () => Promise.resolve([]) },
    entities: { Order: { idField: 'id' } },
    scheduler: scheduler.schedule,
  });
}

function mount(node: ReactNode): { unmount: () => void } {
  const host = document.createElement('div');

  document.body.append(host);

  const root = createRoot(host);

  act(() => {
    root.render(node);
  });

  return {
    unmount: () => {
      act(() => {
        root.unmount();
      });
      host.remove();
    },
  };
}

/** Panels are hosts with a shadow root; the React roots are not. */
const panels = (): number =>
  [...document.body.children].filter((node) => node.shadowRoot !== null).length;

/** The dynamic imports inside the effect settle on the microtask queue. */
async function settle(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

describe('ForgeDevtools', () => {
  it('mounts one panel and exposes the inspector on globalThis', async () => {
    const client = cache();
    const app = mount(
      createElement(ClientProvider, { client }, createElement(ForgeDevtools, null)),
    );

    await settle();

    expect(panels()).toBe(1);
    expect((globalThis as Record<string, unknown>)['forge']).toBeDefined();

    app.unmount();
    await settle();

    expect(panels()).toBe(0);
  });

  it('mounts exactly one panel under StrictMode, which runs effects twice', async () => {
    const client = cache();
    const app = mount(
      createElement(
        StrictMode,
        null,
        createElement(ClientProvider, { client }, createElement(ForgeDevtools, null)),
      ),
    );

    await settle();

    expect(panels()).toBe(1);

    app.unmount();
    await settle();

    expect(panels()).toBe(0);
    // The observer slot is given back, so a later attach is not chained onto a
    // disposed inspector.
    expect(client.observer).toBeUndefined();
  });

  it('two components on one cache share a single inspector', async () => {
    const client = cache();
    const app = mount(
      createElement(
        ClientProvider,
        { client },
        createElement(ForgeDevtools, { key: 'a' }),
        createElement(ForgeDevtools, { key: 'b' }),
      ),
    );

    await settle();

    expect(panels()).toBe(2);

    app.unmount();
    await settle();

    expect(client.observer).toBeUndefined();
  });

  it('does not dispose a live inspector when a second component unmounts mid-attach', async () => {
    const client = cache();
    const a = mount(
      createElement(ClientProvider, { client }, createElement(ForgeDevtools, null)),
    );

    await settle();

    expect(panels()).toBe(1);
    expect(client.observer).toBeDefined();

    // Mount a second component on the same cache and unmount it immediately,
    // synchronously, before a single microtask has run. `acquire()` takes its
    // ref for this join synchronously (`existing.refs++`), but the `await`
    // that returns it to the caller still needs a tick -- so this reproduces
    // the exact window where the component's cleanup fires before its own
    // effect body has observed the ref it already holds. A release that
    // fires unconditionally from both the cleanup and the async body's early
    // return double-counts here and disposes `a`'s still-live inspector out
    // from under it.
    const b = mount(
      createElement(ClientProvider, { client }, createElement(ForgeDevtools, null)),
    );

    b.unmount();

    await settle();

    expect(client.observer).toBeDefined();
    expect(panels()).toBe(1);

    a.unmount();
    await settle();

    expect(client.observer).toBeUndefined();
  });

  it('useForgeDevtools returns the inspector once it has attached, and again once it is gone', async () => {
    const client = cache();
    const box: { value: Devtools | undefined } = { value: undefined };

    function Probe(): null {
      const devtools = useForgeDevtools();

      useEffect(() => {
        box.value = devtools;
      });

      return null;
    }

    // Two separate roots on the same cache, so the probe survives the
    // devtools component's unmount and can observe what happens after --
    // a probe torn down in the same `unmount()` as the thing it is watching
    // never gets a render to report a changed value with.
    const devtools = mount(
      createElement(ClientProvider, { client }, createElement(ForgeDevtools, null)),
    );
    const probe = mount(createElement(ClientProvider, { client }, createElement(Probe, null)));

    await settle();

    expect(box.value).toBeDefined();

    devtools.unmount();
    await settle();

    expect(box.value).toBeUndefined();

    probe.unmount();
  });
});
