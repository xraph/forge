import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { attach } from '../src/devtools';
import { mountOverlay } from '../src/overlay';
import { mountMini } from '../src/mini';
import { mountPanel } from '../src/panel';
import { counter, harness } from './harness';

function shadow(): ShadowRoot {
  const host = document.body.lastElementChild;

  if (host?.shadowRoot == null) throw new Error('nothing attached a shadow root');

  return host.shadowRoot;
}

beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', (cb: () => void) => {
    queueMicrotask(cb);

    return 0;
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function tabs(): string[] {
  return [...shadow().querySelectorAll('.bar button')]
    .map((node) => node.textContent ?? '')
    .filter((text) => text !== '');
}

/**
 * `/overlay` is the name people reach for, so it is the one that has to carry
 * the full panel. The lean six-table view is still here and still cheap; it
 * just no longer owns the obvious name.
 */
describe('the entry points', () => {
  it('gives the full panel from /overlay', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountOverlay(devtools, { parent: document.body, open: true });

    expect(tabs()).toContain('trace');
    expect(tabs()).toContain('network');
    expect(tabs()).toContain('overlay');
    expect(shadow().querySelector('.vitals')).not.toBeNull();

    unmount();
    devtools.dispose();
  });

  it('gives the same thing from /panel, so nobody on it changes', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    expect(tabs()).toContain('trace');
    expect(shadow().querySelector('.vitals')).not.toBeNull();

    unmount();
    devtools.dispose();
  });

  it('keeps the lean six-table view available from /mini', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountMini(devtools, { parent: document.body, open: true });

    // The six it has always had, and none of the ones the panel added. The
    // close button is in the same bar, so this is a containment check.
    for (const name of ['queries', 'entities', 'tags', 'sockets', 'log', 'explain']) {
      expect(tabs()).toContain(name);
    }

    for (const name of ['trace', 'network', 'overlay']) {
      expect(tabs()).not.toContain(name);
    }
    // Lean means lean: no vitals strip, no rail, no detail pane.
    expect(shadow().querySelector('.vitals')).toBeNull();
    expect(shadow().querySelector('.rail')).toBeNull();

    unmount();
    devtools.dispose();
  });

  it('mounts the same implementation from /overlay and /panel', () => {
    expect(mountOverlay).toBe(mountPanel);
  });
});
