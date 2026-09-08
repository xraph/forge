import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { attach } from '../src/devtools';
import { mountMini } from '../src/mini';
import { TransportControls } from '../src/control';
import { counter, harness, ops } from './harness';

/**
 * The panel is secondary and is tested as such: that it renders the answers,
 * that it does not leak into the page it is inspecting, and that a throwing
 * subscriber cannot take the application down with it.
 */
function shadow(): ShadowRoot {
  const host = document.body.lastElementChild;

  if (host?.shadowRoot == null) throw new Error('the lean view did not attach a shadow root');

  return host.shadowRoot;
}

/** Same reason as the panel's: jsdom's rAF is timer-backed, not microtask-backed. */
beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (cb: () => void) => {
    queueMicrotask(cb);

    return 0;
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the launcher', () => {
  it('shows the forge mark rather than the word "forge"', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountMini(devtools, { parent: document.body });

    const button = shadow().querySelector('button');

    expect(button?.getAttribute('aria-label')).toBe('Open Forge devtools');
    expect(button?.querySelector('svg')).not.toBeNull();
    expect(button?.textContent?.trim()).toBe('');

    unmount();
    devtools.dispose();
  });
});

describe('the lean view', () => {
  it('starts closed, opens, and shows the queries', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountMini(devtools, { parent: document.body });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const button = shadow().querySelector('button');

    // Closed means the panel is not built, not merely hidden: the launcher is
    // the whole of the DOM until it is pressed.
    expect(shadow().querySelector('.panel')).toBeNull();

    button?.dispatchEvent(new Event('click'));

    expect(shadow().textContent).toContain(h.cache.key(ops.orderList));
    expect(shadow().textContent).toContain('Order[]');

    stop();
    unmount();
    devtools.dispose();
  });

  it('renders the near-miss explanation for a query key', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountMini(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderCreate, { body: {} });
    h.flush();
    await h.settle();

    const tabs = [...shadow().querySelectorAll('button')];
    const explain = tabs.find((node) => node.textContent === 'explain');

    explain?.dispatchEvent(new Event('click'));

    const input = shadow().querySelector('input');

    if (input === null) throw new Error('no filter input');

    input.value = h.cache.key(ops.orderList);
    input.dispatchEvent(new Event('change'));

    const text = shadow().textContent ?? '';

    expect(text).toContain('outcome: missed');
    expect(text).toContain('instance-vs-collection');
    expect(text).toContain("to the operation's Invalidates");

    stop();
    unmount();
    devtools.dispose();
  });

  it('keeps its styles to itself and removes itself cleanly', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const before = document.body.childElementCount;
    const unmount = mountMini(devtools, { parent: document.body, open: true });

    expect(document.body.childElementCount).toBe(before + 1);
    // Scoped: the stylesheet is inside the shadow root, not in the document.
    expect(shadow().querySelector('style')).not.toBeNull();
    expect(document.querySelector('body > style')).toBeNull();

    unmount();

    expect(document.body.childElementCount).toBe(before);

    devtools.dispose();
  });

  it('survives a listener that throws, rather than taking the application with it', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    devtools.subscribe(() => {
      throw new Error('a panel that crashes');
    });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);

    await expect(h.cache.fetch(ops.orderList)).resolves.toBeDefined();

    stop();
    devtools.dispose();
  });
});

describe('the launcher ring', () => {
  it('reads offline rather than error when the rail put it there', async () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();

    h.fail('GET /orders', new Error('nope'));

    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountMini(devtools, { parent: document.body });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const ring = (): string | null | undefined =>
      shadow().querySelector('button')?.getAttribute('data-pulse');

    expect(ring()).toBe('error');

    controls.mode = 'offline';
    devtools.actions.invalidateTag('Order[]');
    await Promise.resolve();

    expect(ring()).toBe('offline');

    stop();
    unmount();
    devtools.dispose();
  });
});
