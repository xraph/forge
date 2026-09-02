import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import { mountPanel } from '../src/panel';
import { counter, harness, ops } from './harness';

function shadow(): ShadowRoot {
  const host = document.body.lastElementChild;

  if (host?.shadowRoot == null) throw new Error('the panel did not attach a shadow root');

  return host.shadowRoot;
}

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
