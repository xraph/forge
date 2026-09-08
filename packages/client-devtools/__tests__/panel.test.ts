import {
  applyFrames,
  manualClock,
  RestTransport,
  StreamBinder,
  SubscriptionManager,
} from '@forge-go/client-core';
import type { StreamConnect, StreamConnection } from '@forge-go/client-core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { attach } from '../src/devtools';
import { mountPanel } from '../src/panel';
import { RequestLog } from '../src/requests';
import { Revalidation, TransportControls } from '../src/control';
import { counter, harness, ops } from './harness';

/** A generated client's HTTP error, faked down to what `statusOf` reads. */
class HttpFail extends Error {
  readonly statusCode: number;

  constructor(statusCode: number) {
    super(`HTTP ${statusCode}`);
    this.name = 'HTTPError';
    this.statusCode = statusCode;
  }
}

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

describe('the launcher', () => {
  it('shows the forge mark rather than the word "forge"', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    const button = shadow().querySelector('button');

    expect(button?.getAttribute('aria-label')).toBe('Open Forge devtools');
    expect(button?.querySelector('svg')).not.toBeNull();
    expect(button?.textContent?.trim()).toBe('');

    unmount();
    devtools.dispose();
  });
});

describe('the inspector', () => {
  it('stays shut until a row is picked, and the list takes the whole panel', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');

    // Not "rendered but empty": there is no detail element at all, which is
    // what gives the list the full width it needs for a query key.
    expect(shadow().querySelector('.detail')).toBeNull();

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    expect(shadow().querySelector('.detail')).not.toBeNull();

    stop();
    unmount();
    devtools.dispose();
  });

  it('shuts again when its close button is pressed', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    shadow()
      .querySelector('.detail [data-act="close-detail"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(shadow().querySelector('.detail')).toBeNull();

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('the chrome', () => {
  it('offers four dock modes as icon buttons that name themselves', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const dock = [...shadow().querySelectorAll('[data-dock]')];

    expect(dock.map((node) => node.getAttribute('data-dock'))).toEqual([
      'bottom',
      'right',
      'full',
      'window',
    ]);

    // Icon only, so the tooltip and the label are the only thing naming them.
    for (const node of dock) {
      expect(node.querySelector('svg')).not.toBeNull();
      expect(node.getAttribute('data-tip')).toBeTruthy();
      expect(node.getAttribute('aria-label')).toBeTruthy();
      expect(node.textContent?.trim()).toBe('');
    }

    unmount();
    devtools.dispose();
  });

  it('switches the panel to fullscreen and back', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const dockTo = (mode: string): void =>
      shadow()
        .querySelector(`[data-dock="${mode}"]`)
        ?.dispatchEvent(new Event('click', { bubbles: true })) as unknown as void;

    expect(shadow().querySelector('.panel')?.getAttribute('data-mode')).toBe('bottom');

    dockTo('full');
    expect(shadow().querySelector('.panel')?.getAttribute('data-mode')).toBe('full');

    dockTo('bottom');
    expect(shadow().querySelector('.panel')?.getAttribute('data-mode')).toBe('bottom');

    unmount();
    devtools.dispose();
  });
});

describe('the trace', () => {
  /**
   * The whole point of replacing the flat log. Every `invalidated`, `fetch`
   * and `settle` entry already carries the `seq` of the cause that produced it;
   * rendering them as one reversed table throws that structure away.
   */
  it('nests what a mutation caused underneath the mutation', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('trace') === true)
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    const causes = [...shadow().querySelectorAll('.cause')];
    const mutation = causes.find((node) => node.textContent?.includes('PATCH /orders/{id}'));

    expect(mutation).toBeDefined();
    // The tags it raised, and the query it went on to reach, both inside the
    // block for the cause rather than three unrelated rows away from it.
    expect(mutation?.textContent).toContain('Order[]');
    expect(
      [...(mutation?.querySelectorAll('.effect') ?? [])].map((node) => node.textContent ?? ''),
    ).toSatisfy((effects: string[]) =>
      effects.some((text) => text.includes(h.cache.key(ops.orderList))),
    );

    stop();
    unmount();
    devtools.dispose();
  });

  it('marks what you did yourself as a cause of its own', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    devtools.actions.invalidateTag('Order[]');
    h.flush();
    await h.settle();

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('trace') === true)
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    const yours = [...shadow().querySelectorAll('.cause[data-kind="action"]')];

    expect(yours.length).toBe(1);
    expect(yours[0]?.textContent).toContain('invalidateTag');
    expect(yours[0]?.textContent).toContain('Order[]');

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('the control rail', () => {
  it('is absent when the application wired no controls', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    expect(shadow().querySelector('.rail')).toBeNull();

    unmount();
    devtools.dispose();
  });

  it('switches the network mode, one of three at a time', () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const modes = [...shadow().querySelectorAll('[data-net]')];

    expect(modes.map((node) => node.getAttribute('data-net'))).toEqual([
      'online',
      'slow',
      'offline',
    ]);
    expect(modes[0]?.getAttribute('aria-pressed')).toBe('true');

    modes[2]?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(controls.mode).toBe('offline');
    expect(
      [...shadow().querySelectorAll('[data-net]')].map((node) => node.getAttribute('aria-pressed')),
    ).toEqual(['false', 'false', 'true']);

    unmount();
    devtools.dispose();
  });

  it('arms a single synthetic failure and shows that it is armed', () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const arm = (): void =>
      shadow()
        .querySelector('[data-act="fail-next"]')
        ?.dispatchEvent(new Event('click', { bubbles: true })) as unknown as void;

    expect(controls.armed).toBe(false);

    arm();

    expect(controls.armed).toBe(true);
    expect(shadow().querySelector('[data-act="fail-next"]')?.getAttribute('aria-pressed')).toBe(
      'true',
    );

    // Pressing it again gives up on the armed failure rather than arming a second.
    arm();

    expect(controls.armed).toBe(false);

    unmount();
    devtools.dispose();
  });

  it('sets the injected latency from the slider', () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const slider = shadow().querySelector('[data-act="latency"]') as HTMLInputElement | null;

    expect(slider).not.toBeNull();

    if (slider !== null) {
      slider.value = '800';
      slider.dispatchEvent(new Event('input', { bubbles: true }));
    }

    expect(controls.latency).toBe(800);

    unmount();
    devtools.dispose();
  });

  it('only offers the revalidation sources the application actually wired', () => {
    let stopped = 0;
    const revalidation = new Revalidation({
      focus: () => (): void => {
        stopped += 1;
      },
    });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), revalidation });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const toggles = [...shadow().querySelectorAll('[data-reval]')];

    expect(toggles.map((node) => node.getAttribute('data-reval'))).toEqual(['focus']);

    toggles[0]?.dispatchEvent(new Event('click', { bubbles: true }));
    expect(revalidation.enabled('focus')).toBe(true);

    shadow()
      .querySelector('[data-reval="focus"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));
    expect(revalidation.enabled('focus')).toBe(false);
    expect(stopped).toBe(1);

    unmount();
    devtools.dispose();
  });

  /**
   * Freezing the view, not the cache. A stream at twelve frames a second
   * repaints the thing you are reading out from under you, and the panel can
   * honestly stop repainting; it cannot stop the cache committing, which would
   * need a seam in the cache itself.
   */
  it('holds the view still while frozen and catches up when released', async () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const freeze = (): void =>
      shadow()
        .querySelector('[data-act="freeze"]')
        ?.dispatchEvent(new Event('click', { bubbles: true })) as unknown as void;

    freeze();

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    expect(shadow().textContent).not.toContain(h.cache.key(ops.orderList));

    freeze();

    expect(shadow().textContent).toContain(h.cache.key(ops.orderList));

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('the network tab', () => {
  it('says so plainly when no request log is wired', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('network') === true)
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    const text = shadow().querySelector('.list')?.textContent ?? '';

    expect(text).toContain('RequestLog');
    expect(text).toContain('observer');

    unmount();
    devtools.dispose();
  });

  it('renders a retried request with its attempts and its backoff', async () => {
    const log = new RequestLog(20, counter());
    const rest = new RestTransport({
      client: {
        request<T>(_config: unknown): Promise<T> {
          calls += 1;

          return calls === 1
            ? Promise.reject(new HttpFail(503))
            : (Promise.resolve({ ok: true }) as Promise<T>);
        },
      },
      sleep: () => Promise.resolve(),
      random: () => 0,
      observer: log.observer,
    });
    let calls = 0;

    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await rest.execute({
      meta: { method: 'GET', path: '/orders', provides: [], invalidates: [] },
      args: {},
    });

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('network') === true)
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    const row = shadow().querySelector('tr.row');

    expect(row?.textContent).toContain('GET /orders');
    // Two attempts against a limit of three, so the ledger is the point.
    expect(row?.textContent).toContain('2/3');

    unmount();
    devtools.dispose();
  });

  /**
   * The distinction a browser network tab cannot draw, and the reason this
   * view is worth its bytes at all.
   */
  it('names the reason a failed POST was never retried', async () => {
    const log = new RequestLog(20, counter());
    const rest = new RestTransport({
      client: {
        request<T>(_config: unknown): Promise<T> {
          return Promise.reject(new HttpFail(500)) as Promise<T>;
        },
      },
      sleep: () => Promise.resolve(),
      observer: log.observer,
    });

    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await expect(
      rest.execute({
        meta: { method: 'POST', path: '/orders', provides: [], invalidates: [] },
        args: { body: {} },
      }),
    ).rejects.toThrow();

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('network') === true)
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    expect(shadow().querySelector('.detail')?.textContent).toContain('not idempotent');

    unmount();
    devtools.dispose();
  });
});

describe('the overlay tab', () => {
  it('lists each pending write with its patches and what it will raise', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: { total: 99 } } as const]]),
      undefined,
      ['Order[]'],
    );

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('overlay') === true)
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    const layers = [...shadow().querySelectorAll('.layer')];

    expect(layers).toHaveLength(1);
    expect(layers[0]?.textContent).toContain('Order:1');
    expect(layers[0]?.textContent).toContain('merge');
    expect(layers[0]?.textContent).toContain('Order[]');

    unmount();
    devtools.dispose();
  });

  it('says the stack is empty rather than showing a blank tab', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('overlay') === true)
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(shadow().querySelector('.list')?.textContent).toContain('No optimistic write');

    unmount();
    devtools.dispose();
  });
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

    const labels = [...shadow().querySelectorAll('button')].map(
      (node) => node.querySelector('span')?.textContent ?? node.textContent,
    );

    for (const tab of [
      'trace',
      'queries',
      'entities',
      'tags',
      'sockets',
      'streams',
      'frames',
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

describe('the header buckets', () => {
  /**
   * `fresh 2 · stale 0 · fetching 0 · error 0 · unmounted 1`, parsed out of the
   * top bar.
   *
   * The labels are named rather than matched as `\\w+`, because the tab
   * buttons sit in the same bar with no whitespace between them and the
   * counts, so `explainfresh 1` is what a greedy word match actually sees.
   */
  const bucket = (name: string): number => {
    // Only the buckets, not every number that shares the status bar with them.
    const text = shadow().querySelector('.statusbar .buckets')?.textContent ?? '';
    const found = new RegExp(`${name} (\\d+)`).exec(text);

    return found?.[1] === undefined ? -1 : Number(found[1]);
  };

  /** Repaint now, rather than waiting on the coalesced animation frame. */
  const repaint = (): void => {
    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith('queries') === true)
      ?.dispatchEvent(new Event('click'));
  };

  it('counts fresh, fetching and unmounted across the tracked queries', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const first = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    const second = h.cache.subscribe(ops.orderGet, { path: { id: 1 } }, () => undefined);

    // Both requests are out and neither has come back. `fetching` and `status`
    // live only on the record, which is the half `records()` supplies -- so a
    // broken join shows up right here as `fetching 0`.
    repaint();

    expect(bucket('fetching')).toBe(2);

    await h.settle();
    repaint();

    expect(bucket('fresh')).toBe(2);
    expect(bucket('stale')).toBe(0);
    expect(bucket('fetching')).toBe(0);
    expect(bucket('error')).toBe(0);
    expect(bucket('unmounted')).toBe(0);

    // The registry remembers an unmounted query, and `mounts` is what the
    // unmounted bucket counts.
    first();
    second();
    await h.settle();
    repaint();

    expect(bucket('unmounted')).toBe(2);
    expect(bucket('fresh')).toBe(2);

    unmount();
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

    goTo('queries');

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

    goTo('queries');

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

    // Session-wide actions live in the status bar, beside the session-wide
    // numbers, rather than in the row of tabs.
    [...shadow().querySelectorAll('.statusbar button')]
      .find((node) => node.textContent === 'clear cache')
      ?.dispatchEvent(new Event('click'));

    expect(devtools.store().records).toBe(0);

    unmount();
    devtools.dispose();
  });
});

describe('sort and selection are per tab', () => {
  const clickTab = (name: string): void => {
    [...shadow().querySelectorAll('.bar button')]
      .find((node) => node.textContent?.startsWith(name) === true)
      ?.dispatchEvent(new Event('click'));
  };

  const header = (name: string): Element | undefined =>
    [...shadow().querySelectorAll('th')].find((node) => node.textContent === name);

  const column = (): string[] =>
    [...shadow().querySelectorAll('tr.row td:first-child')].map((node) => node.textContent ?? '');

  it('does not carry one tab\'s sort column over to the next', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    // Sort the queries tab by its third column, twice, so it is descending.
    header('state')?.dispatchEvent(new Event('click'));
    header('state')?.dispatchEvent(new Event('click'));

    clickTab('entities');

    // Entities arrives unsorted, in store order, rather than descending by
    // whatever its own third column happens to be.
    const unsorted = column();

    expect(unsorted.length).toBeGreaterThan(1);

    // And the first click on a column here starts ascending rather than
    // reversing, which is what a shared `descending` flag got wrong.
    header('entity')?.dispatchEvent(new Event('click'));

    const ascending = column();

    expect(ascending).toEqual([...ascending].sort());

    stop();
    unmount();
    devtools.dispose();
  });

  it('shows the entity pane for an entity row, not "no longer tracked"', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    clickTab('entities');

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    const detail = shadow().querySelector('.detail')?.textContent ?? '';

    // The bug: `detail()` is a registry lookup, `Order:1` is not a query key,
    // so the pane reported a record that was visibly on screen as gone.
    expect(detail).not.toContain('no longer tracked');
    expect(detail).toContain('version');
    expect(detail).toContain('dependents');
    expect(detail).toContain(h.cache.key(ops.orderList));

    stop();
    unmount();
    devtools.dispose();
  });

  it('evicts the selected record through the action layer', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    clickTab('entities');

    const before = devtools.store().records;

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    const evict = [...shadow().querySelectorAll('.detail button')].find(
      (node) => node.textContent === 'evict',
    );

    expect(evict).toBeDefined();

    evict?.dispatchEvent(new Event('click'));

    expect(devtools.store().records).toBe(before - 1);
    expect(
      devtools.log().some((entry) => entry.kind === 'action' && entry.action === 'evict'),
    ).toBe(true);

    stop();
    unmount();
    devtools.dispose();
  });

  it('keeps a query selection and an entity selection apart', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    clickTab('entities');

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    clickTab('queries');

    const detail = shadow().querySelector('.detail')?.textContent ?? '';

    expect(detail).toContain('operation');
    expect(detail).toContain(h.cache.key(ops.orderList));

    stop();
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
      .find((node) => node.textContent?.startsWith('streams') === true)
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
      .find((node) => node.textContent?.startsWith('frames') === true)
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

describe('the launcher ring', () => {
  it('reads idle when nothing is happening', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    expect(shadow().querySelector('button')?.getAttribute('data-pulse')).toBe('idle');

    unmount();
    devtools.dispose();
  });

  it('reads offline the moment you switch the rail offline', async () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body });

    controls.mode = 'offline';
    // The launcher redraws on cache activity like everything else, coalesced
    // onto an animation frame, so let that frame run before reading it.
    devtools.actions.invalidateTag('Order[]');
    await Promise.resolve();

    expect(shadow().querySelector('button')?.getAttribute('data-pulse')).toBe('offline');

    unmount();
    devtools.dispose();
  });

  /**
   * Offline outranks the failures it causes. A red ring over errors you
   * switched on yourself sends you debugging a request that was never sent.
   */
  it('keeps saying offline even while queries are failing because of it', async () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();

    h.fail('GET /orders', new Error('nope'));

    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    expect(devtools.records().some((record) => record.status === 'error')).toBe(true);
    expect(shadow().querySelector('button')?.getAttribute('data-pulse')).toBe('error');

    controls.mode = 'offline';
    devtools.actions.invalidateTag('Order[]');
    await Promise.resolve();

    expect(shadow().querySelector('button')?.getAttribute('data-pulse')).toBe('offline');
    // The count is still carried; only the explanation on the ring changed.
    expect(shadow().querySelector('.badge')?.textContent).toBe('1');

    stop();
    unmount();
    devtools.dispose();
  });

  it('reads throttled when the rail is set to a slow connection', async () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body });

    controls.mode = 'slow';
    devtools.actions.invalidateTag('Order[]');
    await Promise.resolve();

    expect(shadow().querySelector('button')?.getAttribute('data-pulse')).toBe('slow');

    unmount();
    devtools.dispose();
  });
});

/** Type the filter box the way a person does, then read what survived. */
function typeFilter(text: string): void {
  const input = shadow().querySelector('.bar input') as HTMLInputElement | null;

  if (input === null) throw new Error('no filter input');

  input.value = text;
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

function goTo(name: string): void {
  // A tab button holds its name and its row count, so the name is a prefix.
  [...shadow().querySelectorAll('.bar button')]
    .find((node) => node.textContent?.startsWith(name) === true)
    ?.dispatchEvent(new Event('click', { bubbles: true }));
}

function chip(label: string): Element | undefined {
  return [...shadow().querySelectorAll('.facet')].find((node) =>
    node.textContent?.startsWith(label),
  );
}

describe('the facet chips', () => {
  it('offers the facets of the tab you are on, each carrying its own count', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');

    // A count on the chip says what pressing it would cost, before you press it.
    expect(chip('mounted')?.textContent).toContain('1');
    expect(chip('unmounted')?.textContent).toContain('0');

    stop();
    unmount();
    devtools.dispose();
  });

  it('narrows the list to what the chip selects', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(1);

    chip('unmounted')?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(0);
    expect(chip('unmounted')?.getAttribute('aria-pressed')).toBe('true');

    stop();
    unmount();
    devtools.dispose();
  });

  it('keeps one tab\'s chips out of the next tab\'s', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');
    chip('unmounted')?.dispatchEvent(new Event('click', { bubbles: true }));

    goTo('entities');

    // The entities tab has its own facets and none of them are pressed.
    expect(
      [...shadow().querySelectorAll('.facet')].every(
        (node) => node.getAttribute('aria-pressed') === 'false',
      ),
    ).toBe(true);
    expect(shadow().querySelectorAll('tr.row').length).toBeGreaterThan(0);

    goTo('queries');

    // Coming back, the chip is still where you left it.
    expect(chip('unmounted')?.getAttribute('aria-pressed')).toBe('true');

    stop();
    unmount();
    devtools.dispose();
  });

  it('clears the chips and the text box together', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');
    chip('unmounted')?.dispatchEvent(new Event('click', { bubbles: true }));
    typeFilter('nothing matches this');

    shadow()
      .querySelector('[data-act="clear-filters"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(1);

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('the filter operators', () => {
  it('selects a response class with status:', async () => {
    const log = new RequestLog(20, counter());
    const rest = new RestTransport({
      client: {
        request<T>(config: { url: string }): Promise<T> {
          return config.url.includes('orders')
            ? (Promise.resolve({ ok: true }) as Promise<T>)
            : (Promise.reject(new HttpFail(404)) as Promise<T>);
        },
      },
      sleep: () => Promise.resolve(),
      observer: log.observer,
    });

    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await rest.execute({
      meta: { method: 'GET', path: '/orders', provides: [], invalidates: [] },
      args: {},
    });
    await expect(
      rest.execute({ meta: { method: 'GET', path: '/gone', provides: [], invalidates: [] }, args: {} }),
    ).rejects.toThrow();

    goTo('network');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(2);

    typeFilter('status:4xx');

    const remaining = [...shadow().querySelectorAll('tr.row')];

    expect(remaining).toHaveLength(1);
    expect(remaining[0]?.textContent).toContain('/gone');

    unmount();
    devtools.dispose();
  });

  it('selects the slow ones with a duration threshold', async () => {
    // The injected clock advances one per read, so each request measures a
    // duration of exactly one tick; the threshold is what is under test.
    const log = new RequestLog(20, counter());
    const rest = new RestTransport({
      client: {
        request<T>(): Promise<T> {
          return Promise.resolve({ ok: true }) as Promise<T>;
        },
      },
      observer: log.observer,
    });

    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await rest.execute({
      meta: { method: 'GET', path: '/orders', provides: [], invalidates: [] },
      args: {},
    });

    goTo('network');
    typeFilter('>0ms');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(1);

    typeFilter('>500ms');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(0);

    unmount();
    devtools.dispose();
  });

  it('selects by carried tag with tag:', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');
    typeFilter('tag:Order[]');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(1);

    typeFilter('tag:Customer[]');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(0);

    stop();
    unmount();
    devtools.dispose();
  });

  it('still does a plain substring when no operator is used', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');
    typeFilter('/orders');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(1);

    typeFilter('/invoices');

    expect(shadow().querySelectorAll('tr.row')).toHaveLength(0);

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('the launcher, fully dressed', () => {
  it('goes amber while an optimistic write is pending', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: { total: 99 } } as const]]),
      undefined,
      ['Order[]'],
    );
    devtools.actions.invalidateTag('Order[]');
    await Promise.resolve();

    expect(shadow().querySelector('button')?.getAttribute('data-pulse')).toBe('pending');

    unmount();
    devtools.dispose();
  });

  /**
   * A stuck optimistic write is a bug you want to see; a request in flight is
   * Tuesday. So pending outranks fetching, and a real error outranks both.
   */
  it('lets an error outrank a pending write', async () => {
    const h = harness();

    h.fail('GET /orders', new Error('nope'));

    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: { total: 99 } } as const]]),
      undefined,
      [],
    );

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    expect(shadow().querySelector('button')?.getAttribute('data-pulse')).toBe('error');

    stop();
    unmount();
    devtools.dispose();
  });

  it('carries the vitals beside the mark', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const vitals = shadow().querySelector('.launcher-vitals')?.textContent ?? '';

    // Records held, queries mounted. The counts a glance should answer.
    expect(vitals).toContain('3');
    expect(vitals).toContain('ent');
    expect(vitals).toContain('1');
    expect(vitals).toContain('mnt');

    stop();
    unmount();
    devtools.dispose();
  });

  it('counts pending writes in the vitals only when there are some', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    expect(shadow().querySelector('.launcher-vitals')?.textContent).not.toContain('pend');

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: {} } as const]]),
      undefined,
      [],
    );
    devtools.actions.invalidateTag('Order[]');
    await Promise.resolve();

    expect(shadow().querySelector('.launcher-vitals')?.textContent).toContain('pend');

    unmount();
    devtools.dispose();
  });
});

describe('the launcher, out of the way', () => {
  /**
   * It sits where you put it. Guessing from an element name got this wrong:
   * a framework whose badge exists but is not in this corner still matched,
   * and the launcher lifted itself away from the edge for no reason.
   */
  it('stays in the corner even with a framework dev badge on the page', () => {
    const badge = document.createElement('nextjs-portal');

    document.body.append(badge);

    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    expect(shadow().querySelector('.root')?.getAttribute('data-offset')).toBe('none');

    unmount();
    badge.remove();
    devtools.dispose();
  });

  it('sits in the corner when nothing else is there', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    expect(shadow().querySelector('.root')?.getAttribute('data-offset')).toBe('none');

    unmount();
    devtools.dispose();
  });

  it('takes an explicit offset over anything it guessed', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, offset: 96 });

    const root = shadow().querySelector('.root') as HTMLElement | null;

    expect(root?.style.bottom).toBe('96px');

    unmount();
    devtools.dispose();
  });
});

describe('the near-miss banner', () => {
  /**
   * The threshold, stated: flag per raised tag, not per cause. A tag that
   * reached no mounted query and has a near miss gets a banner even when the
   * same cause reached something through a different tag.
   *
   * `orderCreate` invalidates `Order:{res.id}` and nothing else. The list
   * carries `Order[]`, so the tag resolves fine, reaches nothing, and the
   * screen stays stale with no error anywhere. That is the defect this whole
   * package exists to explain, and the trace should say so where it happened.
   */
  it('flags a raised tag that reached nothing but nearly matched', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderCreate, { body: { total: 30 } });
    h.flush();
    await h.settle();

    goTo('trace');

    const banner = shadow().querySelector('.nearmiss');

    expect(banner).not.toBeNull();
    expect(banner?.textContent).toContain('Order:9');
    expect(banner?.textContent).toContain('Order[]');
    // The relation, and the fix, both already computed by tag.ts.
    expect(banner?.textContent).toContain('instance-vs-collection');

    stop();
    unmount();
    devtools.dispose();
  });

  it('says nothing when every raised tag reached something', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    // `orderUpdate` raises `Order[]`, which the list carries and is reached by.
    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    goTo('trace');

    const banners = [...shadow().querySelectorAll('.nearmiss')].filter((node) =>
      node.textContent?.includes('Order[]'),
    );

    expect(banners).toHaveLength(0);

    stop();
    unmount();
    devtools.dispose();
  });

  it('stays quiet for a tag that reached nothing and resembles nothing', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    // Nothing carries a Customer tag, and it is not a near miss for Order[].
    devtools.actions.invalidateTag('Customer[]');
    h.flush();
    await h.settle();

    goTo('trace');

    expect(shadow().querySelector('.nearmiss')).toBeNull();

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('the vitals strip', () => {
  it('reports the counters that say whether anything is leaking', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const vitals = shadow().querySelector('.vitals');
    const read = (key: string): string | undefined =>
      vitals?.querySelector(`[data-vital="${key}"]`)?.textContent ?? undefined;

    expect(vitals).not.toBeNull();
    expect(read('entities')).toContain('3');
    expect(read('mounted')).toContain('1');
    expect(read('store')).toContain('v');
    expect(read('tags')).toBeDefined();
    // Tombstones and stamped tags are both bounded caches; a number that keeps
    // climbing is the shape of a leak, and neither was visible anywhere.
    expect(read('tombstones')).toBeDefined();

    stop();
    unmount();
    devtools.dispose();
  });

  it('flags pending optimistic writes in the strip only when there are some', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    expect(shadow().querySelector('[data-vital="pending"]')).toBeNull();

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: {} } as const]]),
      undefined,
      [],
    );
    devtools.actions.invalidateTag('Order[]');
    await Promise.resolve();

    expect(shadow().querySelector('[data-vital="pending"]')?.textContent).toContain('1');

    unmount();
    devtools.dispose();
  });
});

describe('density', () => {
  it('switches the panel to compact rows and back', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const press = (): void =>
      shadow()
        .querySelector('[data-act="density"]')
        ?.dispatchEvent(new Event('click', { bubbles: true })) as unknown as void;

    expect(shadow().querySelector('.panel')?.getAttribute('data-density')).toBe('comfortable');

    press();
    expect(shadow().querySelector('.panel')?.getAttribute('data-density')).toBe('compact');

    press();
    expect(shadow().querySelector('.panel')?.getAttribute('data-density')).toBe('comfortable');

    unmount();
    devtools.dispose();
  });
});

describe('the keyboard', () => {
  const key = (init: KeyboardEventInit): void => {
    document.dispatchEvent(new KeyboardEvent('keydown', { ...init, bubbles: true }));
  };

  it('closes the inspector on escape', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');

    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );
    expect(shadow().querySelector('.detail')).not.toBeNull();

    key({ key: 'Escape' });

    expect(shadow().querySelector('.detail')).toBeNull();

    stop();
    unmount();
    devtools.dispose();
  });

  it('switches tabs by number', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const current = (): string | null | undefined =>
      [...shadow().querySelectorAll('.bar button')]
        .find((node) => node.getAttribute('aria-selected') === 'true')
        ?.querySelector('span')?.textContent;

    expect(current()).toBe('trace');

    key({ key: '3' });

    expect(current()).toBe('queries');

    unmount();
    devtools.dispose();
  });

  it('toggles the panel with the mount shortcut', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body });

    expect(shadow().querySelector('.panel')).toBeNull();

    key({ key: 'F', metaKey: true, shiftKey: true });

    expect(shadow().querySelector('.panel')).not.toBeNull();

    key({ key: 'F', metaKey: true, shiftKey: true });

    expect(shadow().querySelector('.panel')).toBeNull();

    unmount();
    devtools.dispose();
  });

  it('freezes and releases the view on f', () => {
    const controls = new TransportControls({ sleep: () => Promise.resolve() });
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), controls });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    key({ key: 'f' });

    expect(shadow().querySelector('[data-act="freeze"]')?.getAttribute('aria-pressed')).toBe(
      'true',
    );

    key({ key: 'f' });

    expect(shadow().querySelector('[data-act="freeze"]')?.getAttribute('aria-pressed')).toBe(
      'false',
    );

    unmount();
    devtools.dispose();
  });

  /**
   * Typing "3" into the filter box must filter, not jump to the third tab.
   * A devtools panel that eats keystrokes out of its own input is worse than
   * one with no shortcuts at all.
   */
  it('keeps its hands off keys typed into an input', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const input = shadow().querySelector('.bar input');

    input?.dispatchEvent(new KeyboardEvent('keydown', { key: '3', bubbles: true }));

    expect(
      [...shadow().querySelectorAll('.bar button')]
        .find((node) => node.getAttribute('aria-selected') === 'true')
        ?.querySelector('span')?.textContent,
    ).toBe('trace');

    unmount();
    devtools.dispose();
  });

  it('does nothing at all once unmounted', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    unmount();
    devtools.dispose();

    // No shadow root to read any more; the assertion is that this does not throw.
    expect(() => {
      key({ key: '3' });
    }).not.toThrow();
  });
});

describe('the request waterfall', () => {
  function wired(): { log: RequestLog; rest: RestTransport } {
    const log = new RequestLog(20, counter());
    let calls = 0;
    const rest = new RestTransport({
      client: {
        request<T>(): Promise<T> {
          calls += 1;

          return calls === 1
            ? (Promise.reject(new HttpFail(503)) as Promise<T>)
            : (Promise.resolve({ ok: true }) as Promise<T>);
        },
      },
      sleep: () => Promise.resolve(),
      random: () => 0,
      observer: log.observer,
    });

    return { log, rest };
  }

  it('draws a bar per request, proportional to the slowest one', async () => {
    const { log, rest } = wired();
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await rest.execute({
      meta: { method: 'GET', path: '/orders', provides: [], invalidates: [] },
      args: {},
    });

    goTo('network');

    const bar = shadow().querySelector('tr.row .wf');

    expect(bar).not.toBeNull();
    // Segments are widths, so the sum is the whole bar.
    expect(bar?.querySelectorAll('i').length).toBeGreaterThan(0);

    unmount();
    devtools.dispose();
  });

  it('shows the backoff as its own segment, distinct from the wire', async () => {
    const { log, rest } = wired();
    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await rest.execute({
      meta: { method: 'GET', path: '/orders', provides: [], invalidates: [] },
      args: {},
    });

    goTo('network');

    expect(shadow().querySelector('tr.row .wf .backoff')).not.toBeNull();

    unmount();
    devtools.dispose();
  });
});

describe('the overlay stack, acted on', () => {
  it('rolls a pending write back off the stack', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: { total: 99 } } as const]]),
      undefined,
      ['Order[]'],
    );

    goTo('overlay');

    expect(shadow().querySelectorAll('.layer')).toHaveLength(1);

    shadow()
      .querySelector('[data-act="rollback"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(devtools.overlays()).toHaveLength(0);
    expect(shadow().querySelectorAll('.layer')).toHaveLength(0);

    unmount();
    devtools.dispose();
  });

  it('shows the base record beside what the patch makes of it', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: { total: 999 } } as const]]),
      undefined,
      [],
    );

    goTo('overlay');

    const diff = shadow().querySelector('.layer .diff')?.textContent ?? '';

    expect(diff).toContain('10');
    expect(diff).toContain('999');

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('holding a query in a state', () => {
  it('holds it in loading, marks it, and gives the real record back', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');

    const key = h.cache.key(ops.orderList);
    const row = [...shadow().querySelectorAll('tr.row')].find((node) =>
      node.textContent?.includes(key),
    );

    row?.dispatchEvent(new Event('click', { bubbles: true }));

    shadow()
      .querySelector('[data-act="hold-loading"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(shadow().querySelector('.held')).not.toBeNull();
    expect(shadow().querySelector('.detail')?.textContent).toContain('Held in loading');
    // The cache itself is untouched: the record still says what it said.
    expect(devtools.detail(key)?.status).toBe('success');

    shadow()
      .querySelector('[data-act="release"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(shadow().querySelector('.held')).toBeNull();

    stop();
    unmount();
    devtools.dispose();
  });

  it('writes the hold to the trace, so it is not mistaken for the app', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');
    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );
    shadow()
      .querySelector('[data-act="hold-error"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    goTo('trace');

    const yours = [...shadow().querySelectorAll('.cause[data-kind="action"]')];

    expect(yours.some((node) => node.textContent?.includes('hold'))).toBe(true);

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('reaching the explanation', () => {
  it('jumps from a query row to its explanation', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderCreate, { body: { total: 30 } });
    h.flush();
    await h.settle();

    goTo('queries');
    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    shadow()
      .querySelector('[data-act="why"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    const text = shadow().textContent ?? '';

    // Explain, already pointed at the pair you were looking at, rather than
    // asking you to type an exact query key from memory.
    expect(text).toContain('outcome: missed');
    expect(text).toContain('instance-vs-collection');

    stop();
    unmount();
    devtools.dispose();
  });

  it('refetches and invalidates the selected row from the keyboard', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');
    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    const before = h.calls.length;

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'r', bubbles: true }));
    await h.settle();

    expect(h.calls.length).toBeGreaterThan(before);

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'i', bubbles: true }));
    h.flush();
    await h.settle();

    goTo('trace');

    expect(
      [...shadow().querySelectorAll('.cause[data-kind="action"]')].some((node) =>
        node.textContent?.includes('invalidate'),
      ),
    ).toBe(true);

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('promoting a pending write', () => {
  it('commits the overlay to the base store by hand', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: { total: 777 } } as const]]),
      undefined,
      [],
    );

    goTo('overlay');

    shadow()
      .querySelector('[data-act="promote"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(devtools.overlays()).toHaveLength(0);
    // Promoted means written, so the base record now carries it.
    expect(devtools.baseRecord('Order:1')?.['total']).toBe(777);

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('closing the last gaps', () => {
  it('draws the auth wait as its own segment', async () => {
    const log = new RequestLog(20, counter());
    let token = 't0';
    const rest = new RestTransport({
      client: {
        request<T>(config: { headers?: Record<string, string> }): Promise<T> {
          return config.headers?.['Authorization'] === 'Bearer t0'
            ? (Promise.reject(new HttpFail(401)) as Promise<T>)
            : (Promise.resolve({ ok: true }) as Promise<T>);
        },
      },
      auth: {
        credentials: () => ({ Authorization: `Bearer ${token}` }),
        refresh: () => {
          token = 't1';

          return Promise.resolve();
        },
      },
      sleep: () => Promise.resolve(),
      observer: log.observer,
    });

    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await rest.execute({
      meta: { method: 'GET', path: '/orders', provides: [], invalidates: [] },
      args: {},
    });

    goTo('network');

    expect(shadow().querySelector('tr.row .wf .auth')).not.toBeNull();

    unmount();
    devtools.dispose();
  });

  it('writes a curl line for the selected request', async () => {
    const log = new RequestLog(20, counter());
    const rest = new RestTransport({
      client: {
        request<T>(): Promise<T> {
          return Promise.resolve({ ok: true }) as Promise<T>;
        },
      },
      observer: log.observer,
    });

    const h = harness();
    const devtools = attach(h.cache, { now: counter(), requests: log });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    await rest.execute({
      meta: { method: 'GET', path: '/orders', provides: [], invalidates: [] },
      args: {},
    });

    goTo('network');
    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    const curl = shadow().querySelector('[data-curl]')?.textContent ?? '';

    expect(curl).toContain("curl -X GET");
    expect(curl).toContain('/orders');
    // Never the real credential, which the log does not keep in the first place.
    expect(curl).toContain('$TOKEN');

    unmount();
    devtools.dispose();
  });

  /**
   * The mock's argument, finally built: a devtools edit is an overlay entry.
   * It never writes the base store, so undo is removing it rather than
   * applying an inverse, and a stream frame that evicts the row takes it with
   * it exactly as it would a real optimistic write.
   */
  it('edits an entity field by pushing a patch onto the overlay stack', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('entities');

    const row = [...shadow().querySelectorAll('tr.row')].find((node) =>
      node.textContent?.includes('Order:1'),
    );

    row?.dispatchEvent(new Event('click', { bubbles: true }));

    const field = shadow().querySelector('[data-act="edit-field"]') as HTMLInputElement | null;
    const value = shadow().querySelector('[data-act="edit-value"]') as HTMLInputElement | null;

    expect(field).not.toBeNull();

    if (field !== null && value !== null) {
      field.value = 'total';
      value.value = '4242';
    }

    shadow()
      .querySelector('[data-act="edit-apply"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));

    expect(devtools.overlays()).toHaveLength(1);
    expect(devtools.foldedRecord('Order:1')?.['total']).toBe(4242);
    // The base is untouched, which is the whole point.
    expect(devtools.baseRecord('Order:1')?.['total']).toBe(10);

    stop();
    unmount();
    devtools.dispose();
  });

  it('marks a query stale without refetching it', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    goTo('queries');
    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    const before = h.calls.length;

    shadow()
      .querySelector('[data-act="force-stale"]')
      ?.dispatchEvent(new Event('click', { bubbles: true }));
    await h.settle();

    expect(devtools.queries()[0]?.stale).toBe(true);
    // Stale, not refetched: no request went out.
    expect(h.calls.length).toBe(before);

    stop();
    unmount();
    devtools.dispose();
  });
});

describe('the remaining shortcuts', () => {
  const key = (init: KeyboardEventInit): void => {
    document.dispatchEvent(new KeyboardEvent('keydown', { ...init, bubbles: true }));
  };

  it('focuses the filter box on slash and on the jump shortcut', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    key({ key: '/' });
    expect(shadow().activeElement?.tagName).toBe('INPUT');

    (shadow().activeElement as HTMLElement | null)?.blur();

    key({ key: 'k', metaKey: true });
    expect(shadow().activeElement?.tagName).toBe('INPUT');

    unmount();
    devtools.dispose();
  });

  it('pops the top overlay on undo', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    h.cache.overlays.add(
      new Map([['Order:1', { kind: 'merge', source: {} } as const]]),
      undefined,
      [],
    );
    h.cache.overlays.add(
      new Map([['Order:2', { kind: 'merge', source: {} } as const]]),
      undefined,
      [],
    );

    key({ key: 'z', metaKey: true });

    expect(devtools.overlays()).toHaveLength(1);
    // The top of the stack went, not the bottom.
    expect(devtools.overlays()[0]?.patches[0]?.key).toBe('Order:1');

    unmount();
    devtools.dispose();
  });

  it('goes fullscreen and back on the fullscreen key', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    key({ key: 'F11' });
    expect(shadow().querySelector('.panel')?.getAttribute('data-mode')).toBe('full');

    key({ key: 'F11' });
    expect(shadow().querySelector('.panel')?.getAttribute('data-mode')).toBe('bottom');

    unmount();
    devtools.dispose();
  });

  it('explains the selected row on enter', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const unmount = mountPanel(devtools, { parent: document.body, open: true });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderCreate, { body: { total: 30 } });
    h.flush();
    await h.settle();

    goTo('queries');
    [...shadow().querySelectorAll('tr.row')][0]?.dispatchEvent(
      new Event('click', { bubbles: true }),
    );

    key({ key: 'Enter' });

    expect(shadow().textContent).toContain('outcome: missed');

    stop();
    unmount();
    devtools.dispose();
  });
});
