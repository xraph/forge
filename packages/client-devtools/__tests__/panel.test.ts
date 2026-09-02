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
