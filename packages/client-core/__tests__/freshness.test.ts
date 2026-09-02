import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import type { OperationMeta } from '../src/transport';
import { fakeTransport, settleMicrotasks } from './harness';
import { schema } from './schema';

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

/** A clock the test moves by hand. Nothing here reads wall time. */
function clock(start = 1_000) {
  let now = start;

  return {
    now: () => now,
    advance(ms: number) {
      now += ms;
    },
  };
}

function cache(options: { staleTime?: number; now?: () => number } = {}) {
  const transport = fakeTransport(() => [{ id: 7, total: 99 }]);
  const scheduler = manualScheduler();

  return {
    transport,
    scheduler,
    queries: new QueryCache({
      transport,
      entities: schema,
      scheduler: scheduler.schedule,
      ...options,
    }),
  };
}

describe('the settle timestamp', () => {
  it('stamps a record with the injected clock when it settles', async () => {
    const time = clock();
    const { queries } = cache({ now: time.now });

    time.advance(500);
    await queries.fetch(orderList);
    await settleMicrotasks();

    expect(queries.settledTimeOf(orderList)).toBe(1_500);
  });

  it('reads the clock once per settle and no more', async () => {
    let reads = 0;
    const { queries } = cache({
      now: () => {
        reads++;

        return 1_000;
      },
    });

    await queries.fetch(orderList);
    await settleMicrotasks();

    // Stamping a settle is the one read the default pays for. Task 3 pins the
    // other half of this: that `expired` adds no further read at Infinity.
    expect(reads).toBe(1);
  });
});

describe('resolving staleTime', () => {
  it('takes the call value over the manifest value over the cache default', async () => {
    const declared: OperationMeta = { ...orderList, staleTime: 5_000 };
    const { queries } = cache({ staleTime: 60_000, now: clock().now });

    // Cache default only.
    const a = queries.subscribe(orderList, undefined, () => undefined);
    expect(queries.effectiveStaleTime(orderList)).toBe(60_000);
    a();

    // Manifest beats the cache default.
    const b = queries.subscribe(declared, undefined, () => undefined);
    expect(queries.effectiveStaleTime(declared)).toBe(5_000);
    b();

    // The call beats both.
    const c = queries.subscribe(declared, undefined, () => undefined, { staleTime: 100 });
    expect(queries.effectiveStaleTime(declared)).toBe(100);
    c();
  });

  it('uses the strictest live subscriber, and relaxes when it leaves', () => {
    const { queries } = cache({ staleTime: 60_000, now: clock().now });

    const loose = queries.subscribe(orderList, undefined, () => undefined, { staleTime: 30_000 });
    const strict = queries.subscribe(orderList, undefined, () => undefined, { staleTime: 1_000 });

    expect(queries.effectiveStaleTime(orderList)).toBe(1_000);

    strict();
    expect(queries.effectiveStaleTime(orderList)).toBe(30_000);

    loose();
    // Nothing watching: falls back to manifest then cache default.
    expect(queries.effectiveStaleTime(orderList)).toBe(60_000);
  });
});

describe('refetch on mount', () => {
  it('does not refetch a settled query at the default staleTime', async () => {
    const { queries, transport } = cache();

    await queries.fetch(orderList);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    // The whole safety story for this feature, in one assertion.
    expect(transport.calls).toHaveLength(1);
  });

  it('refetches on mount once the result has aged past staleTime', async () => {
    const time = clock();
    const { queries, transport } = cache({ staleTime: 1_000, now: time.now });

    await queries.fetch(orderList);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    time.advance(999);
    queries.subscribe(orderList, undefined, () => undefined)();
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    time.advance(2);
    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(2);
  });

  it('adds no clock read on mount while every layer resolves to Infinity', async () => {
    let reads = 0;
    const { queries, transport } = cache({
      now: () => {
        reads++;

        return 1_000;
      },
    });

    await queries.fetch(orderList);
    await settleMicrotasks();

    // One read, for the settle stamp.
    expect(reads).toBe(1);

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    // Mounting at the default must not consult the clock and must not fetch.
    expect(reads).toBe(1);
    expect(transport.calls).toHaveLength(1);
  });
});
