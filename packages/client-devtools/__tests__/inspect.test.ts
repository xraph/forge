import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import * as read from '../src/inspect';
import { counter, harness, ops } from './harness';

/**
 * The rule the whole package is built on: **reading must not change anything.**
 *
 * The failure this guards against is not theoretical. `cache.getState` -- the
 * obvious way to read a query -- calls `open`, which moves the record to the
 * back of the LRU order and creates one if it is missing, and then `snapshot`,
 * which rehydrates the skeleton, builds store memos, links them into the
 * reverse-dependency index and writes the result onto the registry entry's
 * `value`. An inspector that used it would change which query gets evicted
 * next, change what a placement callback is handed as `current`, and populate
 * memos for queries nobody rendered -- all only while somebody has the panel
 * open, which is the worst possible failure mode for a debugging tool.
 *
 * So the assertions below are deliberately over-specified. They read the
 * cache's private LRU order, because that is the thing that would move and
 * there is no public way to see it.
 */

/** The LRU order the cache reaps from the front of. Private by design. */
function lru(cache: unknown): string[] {
  return [...(cache as { records: Map<string, unknown> }).records.keys()];
}

/** Everything about a registry entry that an accidental read would move. */
function registryState(cache: unknown): unknown[] {
  const registry = (cache as { registry: { all(): IterableIterator<Record<string, unknown>> } })
    .registry;

  return [...registry.all()].map((entry) => ({
    key: entry['key'],
    mounts: entry['mounts'],
    stale: entry['stale'],
    settledAt: entry['settledAt'],
    // Identity, not contents: a rehydration through the inspector would mint a
    // new value object here even when the data had not moved.
    value: entry['value'],
    tags: [...(entry['tags'] as Set<string>)].sort(),
    deps: [...(entry['deps'] as Set<string>)].sort(),
  }));
}

describe('inspection does not mutate the cache', () => {
  it('leaves record count, versions, LRU order and registry state exactly as they were', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    // Three queries, so the LRU order has something to get wrong, and one of
    // them settled with nested entities so the store holds memos.
    const stopList = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    const stopOne = h.cache.subscribe(ops.orderGet, { path: { id: 1 } }, () => undefined);
    await h.settle();
    await h.cache.fetch(ops.orderGet, { path: { id: 2 } });
    await h.settle();

    const before = {
      records: h.cache.store.size,
      version: h.cache.store.version,
      frameVersion: h.cache.store.frameVersion,
      tombstones: h.cache.store.tombstones,
      tracked: h.cache.size,
      remembered: h.cache.registry.size,
      mounted: h.cache.registry.mounted,
      indexedTags: h.cache.registry.indexedTags,
      stampedTags: h.cache.registry.stampedTags,
      lru: lru(h.cache),
      entries: registryState(h.cache),
      versions: [...h.cache.store.keys()].map((key) => [
        key,
        h.cache.store.getRecord(key)?.version,
      ]),
    };

    // A full inspection pass: every read the API offers, over everything.
    devtools.snapshot();
    devtools.store();
    devtools.queries();
    devtools.tags();
    devtools.sockets();
    devtools.log();
    devtools.entities();
    devtools.entities({ type: 'Order' });
    devtools.streams();

    for (const record of devtools.entities()) {
      devtools.entity(record.key);
      devtools.dependents(record.key);
    }

    for (const query of devtools.queries()) {
      devtools.query(query.key);
      devtools.detail(query.key);
      devtools.whyNotRefetched(query.key);
      devtools.whyRefetched(query.key);
      devtools.explain(query.key);
    }

    devtools.wouldInvalidate(ops.orderUpdate, { path: { id: 1 } });
    devtools.wouldInvalidate(ops.orderCreate, { body: {} }, { id: 9 });

    // ...and the free functions, which is the surface a custom panel would use.
    read.snapshot(h.cache);
    read.entities(h.cache);
    read.tags(h.cache);

    expect(h.cache.store.size).toBe(before.records);
    expect(h.cache.store.version).toBe(before.version);
    expect(h.cache.store.frameVersion).toBe(before.frameVersion);
    expect(h.cache.store.tombstones).toBe(before.tombstones);
    expect(h.cache.size).toBe(before.tracked);
    expect(h.cache.registry.size).toBe(before.remembered);
    expect(h.cache.registry.mounted).toBe(before.mounted);
    expect(h.cache.registry.indexedTags).toBe(before.indexedTags);
    expect(h.cache.registry.stampedTags).toBe(before.stampedTags);

    // The LRU order, in order. `open` would have moved whatever it touched to
    // the back, so a single stray `getState` shows up here.
    expect(lru(h.cache)).toEqual(before.lru);

    expect(
      [...h.cache.store.keys()].map((key) => [key, h.cache.store.getRecord(key)?.version]),
    ).toEqual(before.versions);

    // Including `value` by identity: rehydrating would replace it.
    expect(registryState(h.cache)).toEqual(before.entries);

    stopList();
    stopOne();
    devtools.dispose();
  });

  it('discriminates: the same assertions fail when getState is used instead', async () => {
    const h = harness();

    const stopList = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();
    await h.cache.fetch(ops.orderGet, { path: { id: 2 } });
    await h.settle();

    const order = lru(h.cache);

    // The read an inspector must not perform, on the query at the *front* of
    // the LRU order. This is the probe that proves the test above is capable of
    // failing -- a passing assertion over a check that cannot fail proves
    // nothing at all.
    h.cache.getState(ops.orderList);

    expect(lru(h.cache)).not.toEqual(order);

    stopList();
  });

  it('hands out copies, so a panel writing to a snapshot cannot move the store', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const snapshot = devtools.entity('Order:1');

    expect(snapshot).toBeDefined();

    (snapshot?.fields as Record<string, unknown>)['total'] = 99999;

    expect(h.cache.store.getRecord('Order:1')?.data['total']).toBe(10);

    stop();
    devtools.dispose();
  });
});

describe('what is in the cache for one entity', () => {
  it('reports its version, its fields, its references and its dependents', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stopList = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    const stopOne = h.cache.subscribe(ops.orderGet, { path: { id: 1 } }, () => undefined);
    await h.settle();

    const record = devtools.entity('Order:1');

    expect(record).toMatchObject({ key: 'Order:1', type: 'Order', id: '1', version: 1 });
    expect(record?.fields['total']).toBe(10);
    // The customer was lifted out into its own record, so the order holds a
    // reference to it rather than a copy.
    expect(record?.refs).toEqual(['Customer:c1']);
    expect(record?.dependents).toEqual(
      [h.cache.key(ops.orderList), h.cache.key(ops.orderGet, { path: { id: 1 } })].sort(),
    );


    // The version moves only when the data does. This mutation's response is
    // byte-identical to what the store holds, so nothing moves.
    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    expect(devtools.entity('Order:1')?.version).toBe(1);

    // And now one that changes something.
    h.reply('PATCH /orders/{id}', { id: 1, total: 11 });
    h.reply('GET /orders', [{ id: 1, total: 11, customer: { id: 'c1', name: 'Ada' } }]);
    h.reply('GET /orders/{id}', { id: 1, total: 11 });
    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    expect(devtools.entity('Order:1')?.version).toBe(2);
    expect(devtools.entity('Order:1')?.fields['total']).toBe(11);

    stopList();
    stopOne();
    devtools.dispose();
  });

  it('answers which queries depend on an entity, including through a nested reference', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    // `Customer:c1` is nowhere in the list's `provides`; it is there because the
    // response's orders pointed at it.
    expect(devtools.dependents('Customer:c1').map((entry) => entry.key)).toEqual([
      h.cache.key(ops.orderList),
    ]);

    stop();
    devtools.dispose();
  });

  it('returns nothing for an entity the store does not hold', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    expect(devtools.entity('Order:404')).toBeUndefined();

    devtools.dispose();
  });
});

describe('the tag graph', () => {
  it('separates the queries that carry a tag from the ones an invalidation reaches', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const listKey = h.cache.key(ops.orderList);
    const mounted = devtools.tags().find((row) => row.tag === 'Order[]');

    expect(mounted).toMatchObject({ carriers: [listKey], mounted: [listKey] });

    stop();

    // Unmounted: still a carrier, no longer in the index. The gap between the
    // two columns is exactly the "why did nothing happen" answer.
    const after = devtools.tags().find((row) => row.tag === 'Order[]');

    expect(after).toMatchObject({ carriers: [listKey], mounted: [] });

    devtools.dispose();
  });
});

describe('detail', () => {
  it('joins the registry entry to the record, so status and error are visible', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);

    await h.settle();

    const detail = devtools.detail(h.cache.key(ops.orderList));

    expect(detail?.status).toBe('success');
    expect(detail?.fetching).toBe(false);
    expect(detail?.mounts).toBe(1);
    expect(detail?.tags).toContain('Order[]');
    expect(detail?.provides).toEqual(['Order[]']);
    expect(detail?.error).toBeUndefined();
    expect(Array.isArray(detail?.value)).toBe(true);

    stop();
    devtools.dispose();
  });

  it('answers undefined for a key nothing is tracking', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    expect(devtools.detail('GET /nothing')).toBeUndefined();

    devtools.dispose();
  });

  it('returns a copy of the value, so a panel cannot move the store', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });
    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);

    await h.settle();

    const first = devtools.detail(h.cache.key(ops.orderList));
    const second = devtools.detail(h.cache.key(ops.orderList));

    expect(first?.value).not.toBe(second?.value);

    stop();
    devtools.dispose();
  });
});
