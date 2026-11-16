import { effectScope, nextTick, ref } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useQuery } from '../src';
import { harness, useOrderList } from './harness';
import type { Harness, Order } from './harness';

/** A transport whose every response takes `latency` ms of fake time. */
function slowHarness(latency: number): Harness {
  let total = 0;

  return harness(async () => {
    await new Promise((resolve) => setTimeout(resolve, latency));
    total += 1;

    return [{ id: 1, total }];
  });
}

describe('useQuery refetchInterval', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('polls on the interval, and stops when its scope is disposed', async () => {
    const fx = slowHarness(0);
    const scope = effectScope();

    scope.run(() => useQuery<Order[]>(useOrderList, undefined, { client: fx.cache, refetchInterval: 1_000 }));

    await vi.advanceTimersByTimeAsync(0);
    expect(fx.transport.calls).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(3_000);
    expect(fx.transport.calls).toHaveLength(4);

    scope.stop();

    await vi.advanceTimersByTimeAsync(3_000);
    expect(fx.transport.calls).toHaveLength(4);
  });

  // A timer restarted by each response fires at interval + latency instead
  // (xraph/forge#100).
  it('holds its cadence on a slow endpoint', async () => {
    const fx = slowHarness(400);
    const scope = effectScope();

    scope.run(() => useQuery<Order[]>(useOrderList, undefined, { client: fx.cache, refetchInterval: 1_000 }));

    await vi.advanceTimersByTimeAsync(3_000);
    expect(fx.transport.calls).toHaveLength(4);

    scope.stop();
  });

  it('reads false as off, and follows a reactive value', async () => {
    const fx = slowHarness(0);
    const interval = ref<number | false>(false);
    const scope = effectScope();

    scope.run(() =>
      useQuery<Order[]>(useOrderList, undefined, { client: fx.cache, refetchInterval: interval }),
    );

    await vi.advanceTimersByTimeAsync(5_000);
    expect(fx.transport.calls).toHaveLength(1);

    interval.value = 1_000;
    await nextTick();
    await vi.advanceTimersByTimeAsync(2_000);
    expect(fx.transport.calls).toHaveLength(3);

    interval.value = false;
    await nextTick();
    await vi.advanceTimersByTimeAsync(5_000);
    expect(fx.transport.calls).toHaveLength(3);

    scope.stop();
  });
});
