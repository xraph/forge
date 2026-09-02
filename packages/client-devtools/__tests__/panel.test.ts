import {
  applyFrames,
  manualClock,
  StreamBinder,
  SubscriptionManager,
} from '@forge-go/client-core';
import type { StreamConnect, StreamConnection } from '@forge-go/client-core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { attach } from '../src/devtools';
import { mountPanel } from '../src/panel';
import { counter, harness, ops } from './harness';

function shadow(): ShadowRoot {
  const host = document.body.lastElementChild;

  if (host?.shadowRoot == null) throw new Error('the panel did not attach a shadow root');

  return host.shadowRoot;
}

/**
 * jsdom's `requestAnimationFrame` is timer-backed, not microtask-backed, so it
 * never fires within `harness.settle()`'s microtask flushing. The panel's own
 * `schedule()` genuinely coalesces onto an animation frame -- that is correct
 * production behaviour and stays that way -- so it is the test environment
 * that is stubbed to cooperate, not the panel that is weakened to suit jsdom.
 */
beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (cb: () => void) => {
    queueMicrotask(cb);

    return 0;
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the panel shell', () => {
  it('renders the status buckets and the query list', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const text = shadow().textContent ?? '';

    expect(text).toContain('fresh');
    expect(text).toContain('stale');
    // One query, mounted and settled: the header bucket for it must read
    // exactly `fresh 1`, not merely contain the word `fresh` -- which a
    // buckets() that always reports zero, or a match against a row's own
    // `fresh` state cell, would also satisfy.
    expect(text).toContain('fresh 1');
    expect(text).toContain(h.cache.key(ops.orderList));

    stop();
    unmount();
    devtools.dispose();
  });

  it('has the eight tabs', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const labels = [...shadow().querySelectorAll('button')].map((node) => node.textContent);

    for (const tab of [
      'queries',
      'entities',
      'tags',
      'sockets',
      'streams',
      'frames',
      'log',
      'explain',
    ]) {
      expect(labels).toContain(tab);
    }

    unmount();
    devtools.dispose();
  });

  it('keeps its styles to itself and removes itself cleanly', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const before = document.body.childElementCount;
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    expect(shadow().querySelector('style')).not.toBeNull();
    expect(document.querySelector('body > style')).toBeNull();

    unmount();

    expect(document.body.childElementCount).toBe(before);

    devtools.dispose();
  });
});

describe('sorting', () => {
  it('sorts the query list by a column, and reverses on a second click', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const first = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    const second = h.cache.subscribe(ops.orderGet, { path: { id: 1 } }, () => undefined);

    await h.settle();

    const keyHeader = [...shadow().querySelectorAll('th')].find(
      (node) => node.textContent === 'key',
    );

    keyHeader?.dispatchEvent(new Event('click'));

    const ascending = [...shadow().querySelectorAll('tr.row td:first-child')].map(
      (node) => node.textContent ?? '',
    );

    expect([...ascending].sort()).toEqual(ascending);

    keyHeader?.dispatchEvent(new Event('click'));

    const descending = [...shadow().querySelectorAll('tr.row td:first-child')].map(
      (node) => node.textContent ?? '',
    );

    expect(descending).toEqual([...ascending].reverse());

    first();
    second();
    unmount();
    devtools.dispose();
  });
});

describe('the detail pane', () => {
  it('fills in when a row is clicked', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    const detail = shadow().querySelector('.detail')?.textContent ?? '';

    expect(detail).toContain('status');
    expect(detail).toContain('success');
    expect(detail).toContain('Order[]');

    stop();
    unmount();
    devtools.dispose();
  });

  it('refetches through the action layer when the button is pressed', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    const before = h.calls.length;
    const refetch = [...shadow().querySelectorAll('.detail button')].find(
      (node) => node.textContent === 'refetch',
    );

    refetch?.dispatchEvent(new Event('click'));
    await h.settle();

    expect(h.calls.length).toBe(before + 1);
    expect(devtools.log().some((entry) => entry.kind === 'action')).toBe(true);

    stop();
    unmount();
    devtools.dispose();
  });

  it('offers clear cache in the global bar', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await h.cache.fetch(ops.orderList);

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent === 'clear cache')
      ?.dispatchEvent(new Event('click'));

    expect(devtools.store().records).toBe(0);

    unmount();
    devtools.dispose();
  });
});

describe('the streams and frames tabs', () => {
  it('says so plainly when no stream runtime is wired', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    [...shadow().querySelectorAll('button')]
      .find((node) => node.textContent === 'streams')
      ?.dispatchEvent(new Event('click'));

    expect(shadow().textContent).toContain('no stream runtime');

    unmount();
    devtools.dispose();
  });

  it('says frame capture is off, and how to turn it on', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    [...shadow().querySelectorAll('button')]
      .find((node) => node.textContent === 'frames')
      ?.dispatchEvent(new Event('click'));

    const text = shadow().textContent ?? '';

    expect(text).toContain('frame capture is off');
    expect(text).toContain('frames: { limit');

    unmount();
    devtools.dispose();
  });
});

/**
 * The empty-state tests above prove the panel says the honest thing when
 * there is nothing to show. They prove nothing about the populated path: the
 * bindings table, the live-queries table, the frames table, and above all the
 * `recovering` badge, which is the one thing in this tab that cannot be seen
 * any other way -- it names the endpoints inside the post-reconnect gap
 * window, when the client has silently missed frames and nothing about it
 * looks wrong. Deleting the badge's rendering must fail a test; this file is
 * where that has to happen.
 */
describe('the streams and frames tabs, populated', () => {
  const binding = {
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: 'upsert' as const,
    invalidates: ['Order[]'],
  };

  const clickTab = (name: string): void => {
    [...shadow().querySelectorAll('button')]
      .find((node) => node.textContent === name)
      ?.dispatchEvent(new Event('click'));
  };

  it('renders a captured frame end to end: channel, message and the payload', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), frames: { limit: 10 } });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    applyFrames(h.cache, [{ binding, payload: { id: 1, total: 5 } }]);

    clickTab('frames');

    const text = shadow().textContent ?? '';

    expect(text).toContain('/ws/orders');
    expect(text).toContain('order.updated');
    expect(text).toContain('upsert');
    // The table row proves the frame was captured; the payload line below it
    // proves `explorer()` actually walked the frame's own payload rather than
    // rendering the table and stopping there.
    expect(text).toContain('total: 5');

    unmount();
    devtools.dispose();
  });

  it('renders the bindings and the mounted live query for a real stream binder', async () => {
    // Copied from `streams.test.ts`'s `connect()`: the manager only needs to
    // hold the connection, never deliver anything.
    const connect: StreamConnect = (): StreamConnection => ({
      onMessage: () => undefined,
      onClose: () => undefined,
      close: () => undefined,
    });

    const h = harness();
    const manager = new SubscriptionManager({ connect });
    const binder = new StreamBinder({ cache: h.cache, streams: [binding], manager });
    const devtools = attach(h.cache, { now: counter(), binder });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const release = binder.subscribe(ops.orderList, undefined);
    await h.settle();

    clickTab('streams');

    const text = shadow().textContent ?? '';

    expect(text).toContain('/ws/orders');
    expect(text).toContain('order.updated');
    expect(text).toContain(h.cache.key(ops.orderList));

    release();
    unmount();
    devtools.dispose();
  });

  it('surfaces the recovering badge, and the reason, after a real drop and reconnect', async () => {
    // A connection the test can drop by hand: the same shape as the idle
    // `connect()` above, extended only with a way to invoke the close handler
    // it captures. A real drop-and-reconnect is the only way `recovering`
    // genuinely fills -- `pendingRecovery` is private binder state with no
    // public setter -- so this drives the actual manager/binder state machine
    // rather than constructing a snapshot by hand.
    const drops: (() => void)[] = [];
    const connect: StreamConnect = (): StreamConnection => {
      let onClose: (() => void) | undefined;

      drops.push(() => onClose?.());

      return {
        onMessage: () => undefined,
        onClose: (handler) => {
          onClose = handler;
        },
        close: () => undefined,
      };
    };

    const h = harness();
    const clock = manualClock();
    const manager = new SubscriptionManager({
      connect,
      sleep: clock.sleep,
      random: () => 0,
      backoff: { baseDelay: 1000 },
    });
    const binder = new StreamBinder({
      cache: h.cache,
      streams: [binding],
      manager,
      sleep: clock.sleep,
    });
    const devtools = attach(h.cache, { now: counter(), binder });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const release = binder.subscribe(ops.orderList, undefined);
    await h.settle();

    // The lid closes. Frames are missed, and nothing about the client says so
    // -- until the reconnect lands and the binder starts its resume-grace
    // window.
    const last = drops[drops.length - 1];

    if (last === undefined) throw new Error('no connection opened');

    last();
    await clock.advance(1000);

    // Inside the resume-grace window: the reconnect has happened (a new
    // connection was opened) but no `forge.resumed` arrived, so the endpoint
    // is genuinely awaiting a resume verdict right now. This is the one
    // instant the badge exists to report.
    clickTab('streams');

    const text = shadow().textContent ?? '';

    expect(text).toContain('/ws/orders');
    expect(text).toContain('missed');

    release();
    unmount();
    devtools.dispose();
  });
});
