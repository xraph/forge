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
