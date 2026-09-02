import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import { counter, harness, ops } from './harness';

describe('actions', () => {
  it('refetches one query and logs itself as the cause', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);

    await h.settle();
    const before = h.calls.length;

    await devtools.actions.refetch(h.cache.key(ops.orderList));
    await h.settle();

    expect(h.calls.length).toBe(before + 1);
    expect(devtools.log().find((item) => item.kind === 'action')).toMatchObject({
      kind: 'action',
      action: 'refetch',
    });

    stop();
    devtools.dispose();
  });

  it('rejects a refetch on a key nothing tracks, and logs nothing', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    await expect(devtools.actions.refetch('GET /nothing')).rejects.toThrow();
    expect(devtools.log().filter((item) => item.kind === 'action')).toHaveLength(0);

    devtools.dispose();
  });

  it('invalidates the tags a query carries, and reaches it', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);

    await h.settle();
    const before = h.calls.length;

    expect(devtools.actions.invalidate(h.cache.key(ops.orderList))).toBe(true);
    h.flush();
    await h.settle();

    expect(h.calls.length).toBe(before + 1);

    stop();
    devtools.dispose();
  });

  it('evicts one entity and leaves the rest of the store alone', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);

    await h.settle();

    expect(devtools.entity('Order:1')).toBeDefined();
    expect(devtools.actions.evict('Order:1')).toBe(true);
    expect(devtools.entity('Order:1')).toBeUndefined();
    expect(devtools.entity('Order:2')).toBeDefined();

    stop();
    devtools.dispose();
  });

  it('drops a watched query, which resets it and refetches', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);

    await h.settle();
    const before = h.calls.length;

    expect(devtools.actions.drop(h.cache.key(ops.orderList))).toBe(true);
    await h.settle();

    expect(h.calls.length).toBe(before + 1);

    stop();
    devtools.dispose();
  });

  it('answers false for a key it is not tracking, and logs nothing', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    expect(devtools.actions.drop('GET /nothing')).toBe(false);
    expect(devtools.log().filter((item) => item.kind === 'action')).toHaveLength(0);

    devtools.dispose();
  });

  it('clears the whole cache', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    await h.cache.fetch(ops.orderList);

    expect(devtools.store().records).toBeGreaterThan(0);

    devtools.actions.clear();

    expect(devtools.store().records).toBe(0);

    devtools.dispose();
  });
});
