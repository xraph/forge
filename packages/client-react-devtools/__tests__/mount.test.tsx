import { manualScheduler, QueryCache } from '@forge-go/client-core';
import { ClientProvider } from '@forge-go/client-react';
import { act, createElement, StrictMode } from 'react';
import type { ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it } from 'vitest';
import { ForgeDevtools } from '../src/dev';

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
});
