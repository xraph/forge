import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import { EventLog } from '../src/log';
import { counter, harness, ops } from './harness';

/**
 * An event log is a memory leak by default. These are the tests that say this
 * one is not.
 */
describe('the log is bounded', () => {
  it('holds exactly its capacity and counts what it dropped', () => {
    const log = new EventLog(4, counter());

    for (let i = 0; i < 10; i++) {
      log.push({ kind: 'settle', session: 0, query: `q${String(i)}`, version: i });
    }

    const held = log.entries();

    expect(log.capacity).toBe(4);
    expect(held).toHaveLength(4);
    expect(log.dropped).toBe(6);

    // Oldest first, and the oldest is the fifth thing pushed: the ring is a
    // window on the recent past, not a recording.
    expect(held.map((entry) => entry.kind === 'settle' && entry.query)).toEqual([
      'q6',
      'q7',
      'q8',
      'q9',
    ]);

    // The sequence keeps counting across the wrap, so a `cause` pointing at a
    // dropped entry is recognisable rather than pointing at its replacement.
    expect(held[0]?.seq).toBe(7);
    expect(log.find(1)).toBeUndefined();
  });

  it('survives a capacity of zero rather than writing outside the ring', () => {
    const log = new EventLog(0, counter());

    log.push({ kind: 'principal', session: 1 });
    log.push({ kind: 'principal', session: 2 });

    expect(log.capacity).toBe(1);
    expect(log.entries()).toHaveLength(1);
    expect(log.dropped).toBe(1);
  });

  it('stops growing under a load that would otherwise grow it without bound', async () => {
    const h = harness();
    const devtools = attach(h.cache, { limit: 32, now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    for (let i = 0; i < 200; i++) {
      await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
      h.flush();
      await h.settle();
    }

    expect(devtools.log()).toHaveLength(32);
    expect(devtools.dropped).toBeGreaterThan(500);

    stop();
    devtools.dispose();
  });

  it('keeps no response body, no error object and no rehydrated value', async () => {
    const h = harness();

    h.reply('POST /orders', { id: 9, secret: 'x'.repeat(5000) });

    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderCreate, { body: { note: 'y'.repeat(5000) } });
    h.flush();
    await h.settle();

    const serialised = JSON.stringify(devtools.log());

    // The response never reaches the log, and the arguments are truncated to a
    // cache key. A ring of five hundred entries each holding a page of orders
    // is the same leak arriving more slowly.
    expect(serialised).not.toContain('x'.repeat(50));
    expect(serialised.length).toBeLessThan(3000);

    const mutation = devtools.log().find((entry) => entry.kind === 'mutation');

    expect(mutation?.kind === 'mutation' && mutation.args.endsWith('...')).toBe(true);
    expect(mutation?.kind === 'mutation' && mutation.args.length).toBeLessThan(220);

    stop();
    devtools.dispose();
  });

  it('prunes its per-query bookkeeping instead of growing one entry per query key', async () => {
    const h = harness();
    const devtools = attach(h.cache, { limit: 16, now: counter() });

    // A search box: one distinct query key per keystroke, each with a registry
    // entry behind it, most of them evicted by the cache's own LRU cap.
    for (let i = 0; i < 700; i++) {
      await h.cache.fetch(ops.orderGet, { path: { id: i } });
    }

    await h.settle();

    const tracked = (devtools as unknown as { readonly capacity: number }).capacity;

    expect(tracked).toBe(16);
    expect(devtools.log()).toHaveLength(16);
    // The cache's own cap held, which is what the recorder prunes against.
    // `reap` runs before the insert, so the ceiling is the limit plus the one
    // record that was just asked for.
    expect(h.cache.size).toBeLessThanOrEqual(129);

    devtools.dispose();
  });

  it('clear forgets the events and leaves the cache alone', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const records = h.cache.store.size;

    expect(devtools.log().length).toBeGreaterThan(0);

    devtools.clear();

    expect(devtools.log()).toEqual([]);
    expect(devtools.dropped).toBe(0);
    expect(h.cache.store.size).toBe(records);

    stop();
    devtools.dispose();
  });
});

describe('the identity boundary', () => {
  it('divides the log by session rather than showing two principals as one timeline', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    expect(h.cache.store.size).toBeGreaterThan(0);

    h.cache.setPrincipal('user-2');
    await h.settle();

    const log = devtools.log();
    const boundary = log.findIndex((entry) => entry.kind === 'principal');

    expect(boundary).toBeGreaterThan(0);
    expect(devtools.session).toBe(1);
    expect(log.slice(0, boundary).every((entry) => entry.session === 0)).toBe(true);
    expect(log.slice(boundary).every((entry) => entry.session === 1)).toBe(true);

    // The store the earlier half describes is gone; the query re-mounted and
    // re-fetched, and it is logged as a mount rather than as a refetch of a
    // query whose data no longer exists.
    const after = log.slice(boundary).find((entry) => entry.kind === 'fetch');

    expect(after?.kind === 'fetch' && after.reason).toBe('mount');

    stop();
    devtools.dispose();
  });
});

describe('attaching and detaching', () => {
  /**
   * The claim that "the core retains no log when nobody reads one", tested from
   * the outside: run a session's worth of traffic with nothing attached, attach
   * afterwards, and find the log empty. If the core had been buffering, this is
   * where it would show.
   */
  it('finds nothing waiting, because the core buffered nothing', async () => {
    const h = harness();

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    for (let i = 0; i < 50; i++) {
      await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
      h.flush();
      await h.settle();
    }

    expect(h.cache.observer).toBeUndefined();

    const devtools = attach(h.cache, { now: counter() });

    expect(devtools.log()).toEqual([]);
    expect(devtools.dropped).toBe(0);

    stop();
    devtools.dispose();
  });

  it('restores the previous observer and stops recording', async () => {
    const h = harness();
    const seen: string[] = [];

    h.cache.observer = (event) => seen.push(event.type);

    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    // Chained, not replaced: an existing observer keeps receiving events.
    expect(seen.length).toBeGreaterThan(0);
    expect(devtools.log().length).toBeGreaterThan(0);

    const recorded = devtools.log().length;

    devtools.dispose();

    expect(typeof h.cache.observer).toBe('function');

    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    expect(devtools.log()).toHaveLength(recorded);

    stop();
    h.cache.observer = undefined;
  });

  it('does not unhook a second inspector that took the slot', async () => {
    const h = harness();
    const first = attach(h.cache, { now: counter() });
    const second = attach(h.cache, { now: counter() });

    first.dispose();

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    expect(second.log().length).toBeGreaterThan(0);

    const held = second.log().length;

    second.dispose();

    // The first inspector's observer is still in the chain -- it cannot be
    // spliced out without unhooking whatever attached over it -- but it is
    // inert, so neither ring grows.
    expect(typeof h.cache.observer).toBe('function');

    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    expect(first.log()).toEqual([]);
    expect(second.log()).toHaveLength(held);

    stop();
  });
});
