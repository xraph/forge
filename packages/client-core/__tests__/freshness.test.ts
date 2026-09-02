import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { revalidateOnFocus, revalidateOnReconnect } from '../src/freshness';
import { manualScheduler } from '../src/invalidate';
import type { OperationMeta } from '../src/transport';
import { deferred, fakeTransport, settleMicrotasks } from './harness';
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

describe('revalidate', () => {
  it('refetches mounted expired queries and reports how many it started', async () => {
    const time = clock();
    const { queries, transport } = cache({ staleTime: 1_000, now: time.now });

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    expect(queries.revalidate()).toBe(0);

    time.advance(1_001);
    expect(queries.revalidate()).toBe(1);

    await settleMicrotasks();
    expect(transport.calls).toHaveLength(2);
  });

  // The three tests below each violate exactly one of `revalidate`'s skip
  // conditions while satisfying the other two -- unlike a single record that
  // is unwatched, unsettled and in-flight all at once, where deleting any one
  // check from the implementation would still leave the other two silently
  // covering for it and the test would keep passing.

  it('skips a settled, expired record nobody is watching', async () => {
    const time = clock();
    const { queries, transport } = cache({ staleTime: 1_000, now: time.now });

    // Settled and, after the advance, expired -- but never subscribed, so
    // only the watched-check should be what stops this.
    await queries.fetch(orderList);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    time.advance(1_001);

    expect(queries.revalidate()).toBe(0);
    expect(transport.calls).toHaveLength(1);
  });

  it('skips a subscribed record that has not settled, even once it reads as expired', async () => {
    const time = clock();
    const transport = fakeTransport(() => {
      throw new Error('boom');
    });
    const queries = new QueryCache({
      transport,
      entities: schema,
      staleTime: 1_000,
      now: time.now,
    });

    // The mount's own fetch fails, so the record ends unsettled -- and,
    // critically, no longer in flight either: `inflight` clears on the
    // rejection just as it would on a success. A promise left permanently
    // pending would still be in flight when `revalidate` runs, and the
    // in-flight check would mask the very check this test exists to pin
    // down. Failing it instead is the only way through the public API to
    // reach "watched, not in flight, unsettled" all at once.
    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    // settledTime is still its zero default, so once the clock has moved far
    // enough past it the record reads as expired on the clock comparison
    // alone. Only the settled-check stops `revalidate` here.
    time.advance(5_000);

    expect(queries.revalidate()).toBe(0);
    expect(transport.calls).toHaveLength(1);
  });

  it('skips a settled, expired record with a second request already running', async () => {
    const time = clock();
    const held = deferred<unknown>();
    const transport = fakeTransport((_request, call) =>
      call === 0 ? [{ id: 7, total: 99 }] : held.promise,
    );
    const queries = new QueryCache({
      transport,
      entities: schema,
      staleTime: 1_000,
      now: time.now,
    });

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    time.advance(1_001);

    // `refetch`, not `revalidate`: a second request is already out for this
    // query -- held open on `held.promise` -- before `revalidate` ever runs.
    // Settled stays true (only `start` touches it, and it never resets the
    // flag), and the clock says expired, so only the in-flight check should
    // be what stops this.
    void queries.refetch(orderList);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(2);

    expect(queries.revalidate()).toBe(0);
    expect(transport.calls).toHaveLength(2);
  });
});

/** A stand-in for `document` or `globalThis`, with no DOM anywhere. */
function fakeTarget(visibilityState?: string) {
  const listeners = new Map<string, Set<() => void>>();

  return {
    visibilityState,
    listenerCount: (type: string) => listeners.get(type)?.size ?? 0,
    emit(type: string) {
      for (const listener of listeners.get(type) ?? []) listener();
    },
    addEventListener(type: string, listener: () => void) {
      const set = listeners.get(type) ?? new Set<() => void>();
      set.add(listener);
      listeners.set(type, set);
    },
    removeEventListener(type: string, listener: () => void) {
      listeners.get(type)?.delete(listener);
    },
  };
}

describe('revalidateOnFocus', () => {
  it('revalidates when the document becomes visible, and not while hidden', async () => {
    const time = clock();
    const { queries, transport } = cache({ staleTime: 1_000, now: time.now });
    const target = fakeTarget('visible');

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(1);

    const stop = revalidateOnFocus(queries, { target });
    expect(target.listenerCount('visibilitychange')).toBe(1);

    time.advance(1_001);
    target.emit('visibilitychange');
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(2);

    stop();
    expect(target.listenerCount('visibilitychange')).toBe(0);

    // Idempotent: a second stop is not an error and removes nothing further.
    stop();
    expect(target.listenerCount('visibilitychange')).toBe(0);
  });

  it('does nothing when the document is hidden', async () => {
    const time = clock();
    const { queries, transport } = cache({ staleTime: 1_000, now: time.now });
    const target = fakeTarget('hidden');

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    revalidateOnFocus(queries, { target });
    time.advance(5_000);
    target.emit('visibilitychange');
    await settleMicrotasks();

    expect(transport.calls).toHaveLength(1);
  });

  it('returns a working no-op when there is no target to listen on', () => {
    const { queries } = cache();

    // `target: false` is the explicit off switch, and stands in for a server
    // render where no global carries addEventListener.
    const stop = revalidateOnFocus(queries, { target: false });

    expect(() => stop()).not.toThrow();
  });
});

describe('revalidateOnReconnect', () => {
  it('revalidates when the network comes back', async () => {
    const time = clock();
    const { queries, transport } = cache({ staleTime: 1_000, now: time.now });
    const target = fakeTarget();

    queries.subscribe(orderList, undefined, () => undefined);
    await settleMicrotasks();

    const stop = revalidateOnReconnect(queries, { target });
    expect(target.listenerCount('online')).toBe(1);

    time.advance(1_001);
    target.emit('online');
    await settleMicrotasks();
    expect(transport.calls).toHaveLength(2);

    stop();
    expect(target.listenerCount('online')).toBe(0);
  });
});
