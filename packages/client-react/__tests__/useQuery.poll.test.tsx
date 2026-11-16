import { useState } from 'react';
import { act, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ClientProvider, useQuery } from '../src';
import { harness, useOrderList } from './harness';
import type { Harness, Order } from './harness';

/** Move the fake clock, letting every request and render it releases settle. */
async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

/** A transport whose every response takes `latency` ms of fake time. */
function slowHarness(latency: number): Harness {
  let total = 0;

  return harness(async () => {
    await new Promise((resolve) => setTimeout(resolve, latency));
    total += 1;

    return [{ id: 1, total }];
  });
}

function setVisibility(state: 'visible' | 'hidden'): void {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
}

describe('useQuery refetchInterval', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
  });

  it('polls on the interval, and stops on unmount', async () => {
    const h = slowHarness(0);

    function List() {
      useQuery<Order[]>(useOrderList, undefined, { refetchInterval: 1_000 });

      return null;
    }

    const view = render(
      <ClientProvider client={h.cache}>
        <List />
      </ClientProvider>,
    );

    await advance(0);
    expect(h.transport.calls).toHaveLength(1);

    await advance(3_000);
    expect(h.transport.calls).toHaveLength(4);

    view.unmount();

    await advance(3_000);
    expect(h.transport.calls).toHaveLength(4);
  });

  // The bug a hand-written hook keeps shipping: a timer keyed on the result
  // object restarts on every response, so a three second poll on a slow
  // endpoint becomes interval + latency (xraph/forge#100).
  it('holds its cadence while each response re-renders the component', async () => {
    const h = slowHarness(400);
    let renders = 0;

    function List() {
      renders++;
      useQuery<Order[]>(useOrderList, undefined, { refetchInterval: 1_000 });

      return null;
    }

    render(
      <ClientProvider client={h.cache}>
        <List />
      </ClientProvider>,
    );

    // Mount fetch at 0, polls at 1000, 2000 and 3000. A timer restarted by
    // each response would fire at 1400 and 2800 instead.
    await advance(3_000);
    expect(h.transport.calls).toHaveLength(4);
    expect(renders).toBeGreaterThan(4);
  });

  it('reads false as off, and starts and stops as the value changes', async () => {
    const h = slowHarness(0);
    let setInterval!: (value: number | false) => void;

    function List() {
      const [interval, set] = useState<number | false>(false);
      setInterval = set;
      useQuery<Order[]>(useOrderList, undefined, { refetchInterval: interval });

      return null;
    }

    render(
      <ClientProvider client={h.cache}>
        <List />
      </ClientProvider>,
    );

    await advance(5_000);
    expect(h.transport.calls).toHaveLength(1);

    await act(async () => {
      setInterval(1_000);
    });
    await advance(2_000);
    expect(h.transport.calls).toHaveLength(3);

    await act(async () => {
      setInterval(false);
    });
    await advance(5_000);
    expect(h.transport.calls).toHaveLength(3);
  });

  it('pauses while the tab is hidden unless asked to keep going', async () => {
    setVisibility('hidden');

    const paused = slowHarness(0);
    const background = slowHarness(0);

    function List({ inBackground }: { inBackground: boolean }) {
      useQuery<Order[]>(useOrderList, undefined, {
        refetchInterval: 1_000,
        refetchIntervalInBackground: inBackground,
      });

      return null;
    }

    render(
      <>
        <ClientProvider client={paused.cache}>
          <List inBackground={false} />
        </ClientProvider>
        <ClientProvider client={background.cache}>
          <List inBackground />
        </ClientProvider>
      </>,
    );

    await advance(3_000);
    expect(paused.transport.calls).toHaveLength(1);
    expect(background.transport.calls).toHaveLength(4);
  });
});
