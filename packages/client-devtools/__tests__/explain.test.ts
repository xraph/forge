import { describe, expect, it } from 'vitest';
import { attach } from '../src/devtools';
import type { MissReport, RefetchReport } from '../src/types';
import { counter, harness, ops } from './harness';

/**
 * The two questions, and the near miss that is the whole reason for the
 * package.
 */
describe('why did this query refetch', () => {
  it('names the tag, the operation and the query it hit', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    // `orderUpdate` declares `Order[]`, which the list provides.
    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    const key = h.cache.key(ops.orderList);
    const report = devtools.whyRefetched(key);

    expect(report).toBeDefined();
    expect(report?.reason).toBe('invalidation');
    expect(report?.cause?.label).toBe('mutation PATCH /orders/{id}');
    expect(report?.cause?.tags).toEqual(['Order:1', 'Order[]']);
    expect(report?.matched).toContain('Order[]');
    expect(report?.summary).toContain('PATCH /orders/{id}');

    stop();
    devtools.dispose();
  });

  it('records the invalidation, its cause, and every query it hit', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stopList = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    const stopOne = h.cache.subscribe(ops.orderGet, { path: { id: 1 } }, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    const log = devtools.log();
    const mutation = log.find((entry) => entry.kind === 'mutation');

    expect(mutation).toMatchObject({
      kind: 'mutation',
      operation: 'PATCH /orders/{id}',
      tags: ['Order:1', 'Order[]'],
      unresolved: [],
    });

    const hits = log.filter((entry) => entry.kind === 'invalidated');

    // Both queries: the list through `Order[]`, the detail through `Order:1`.
    expect(hits).toHaveLength(2);
    for (const hit of hits) {
      expect(hit.kind === 'invalidated' && hit.cause).toBe(mutation?.seq);
    }

    const listKey = h.cache.key(ops.orderList);
    const detailKey = h.cache.key(ops.orderGet, { path: { id: 1 } });
    const listHit = hits.find((entry) => entry.kind === 'invalidated' && entry.query === listKey);
    const detailHit = hits.find(
      (entry) => entry.kind === 'invalidated' && entry.query === detailKey,
    );

    // The list is reached twice over, and the report says so: through the
    // `Order[]` it declares, and through the `Order:1` its last response put in
    // its dependency set. That second route is what makes an update to a row
    // already on screen reach the list with no declaration at all -- and its
    // absence for a *create* is the near miss the next test is about.
    expect(listHit?.kind === 'invalidated' && listHit.matched).toEqual(['Order:1', 'Order[]']);
    expect(detailHit?.kind === 'invalidated' && detailHit.matched).toEqual(['Order:1']);

    stopList();
    stopOne();
    devtools.dispose();
  });

  it('calls a first fetch a mount rather than an invalidation', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    expect(devtools.whyRefetched(h.cache.key(ops.orderList))?.reason).toBe('mount');

    stop();
    devtools.dispose();
  });
});

describe('why did this query NOT refetch', () => {
  /**
   * The one that matters. A list carrying `Order[]`, a create invalidating
   * `Order:9`, and nothing anywhere reporting that they never met.
   */
  it('answers the Order[] / Order:9 near miss with the declaration to change', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const reads = (): number =>
      h.calls.filter((call) => call.meta.path === '/orders' && call.meta.method === 'GET').length;
    const before = reads();

    await h.cache.mutate(ops.orderCreate, { body: { total: 30 } });
    h.flush();
    await h.settle();

    // The premise: it really did not refetch.
    expect(reads()).toBe(before);

    const key = h.cache.key(ops.orderList);
    const report = devtools.whyNotRefetched(key);

    expect(report.outcome).toBe('missed');
    expect(report.cause.label).toBe('mutation POST /orders');
    expect(report.invalidated).toEqual(['Order:9']);
    expect(report.carried).toContain('Order[]');
    expect(report.matched).toEqual([]);

    expect(report.nearest[0]).toMatchObject({
      invalidated: 'Order:9',
      carried: 'Order[]',
      relation: 'instance-vs-collection',
    });
    expect(report.suggestions.join(' ')).toContain("Add `Order[]` to the operation's Invalidates");
    expect(report.reason).toContain('disjoint');

    stop();
    devtools.dispose();
  });

  it('distinguishes a real miss from a query nobody has mounted', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();
    // Unmount, but the registry still remembers it.
    stop();

    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    const report = devtools.whyNotRefetched(h.cache.key(ops.orderList));

    expect(report.outcome).toBe('stale-while-unmounted');
    expect(report.matched).toContain('Order[]');
    expect(report.mounts).toBe(0);
    expect(report.reason).toContain('refetches the moment it mounts again');
    // The distinction is the point: this is not a declaration bug, so it does
    // not offer declarations to change.
    expect(report.suggestions).toEqual([]);

    devtools.dispose();
  });

  it('blames a placement callback rather than the tag graph when one answered', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } }, {
      place: {
        'Order[]': (created, current) => [created, ...(current as unknown[])],
        'Order:1': (created, current) => current as unknown[],
      },
    });
    h.flush();
    await h.settle();

    const report = devtools.whyNotRefetched(h.cache.key(ops.orderList));

    expect(report.outcome).toBe('placed');
    expect(report.reason).toContain('placement callback answered');

    stop();
    devtools.dispose();
  });

  it('names an unresolved template, which is the silent case', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    // The response carries no `ref`, so `Order[]:{res.ref}` resolves to nothing
    // and is skipped. Nothing is invalidated and nothing says so.
    await h.cache.mutate(ops.orderArchive, { path: { id: 1 } });
    h.flush();
    await h.settle();

    const report = devtools.whyNotRefetched(h.cache.key(ops.orderList));

    expect(report.outcome).toBe('missed');
    expect(report.invalidated).toEqual([]);
    expect(report.cause.unresolved).toEqual(['Order[]:{res.ref}']);
    expect(report.suggestions[0]).toContain('resolved to nothing and were skipped');

    stop();
    devtools.dispose();
  });

  it('reports a key it has never heard of as such', () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const report = devtools.whyNotRefetched('GET /nope');

    expect(report.outcome).toBe('not-tracked');
    expect(report.reason).toContain('never heard of');

    devtools.dispose();
  });

  it('says so when there is no cause in the log at all', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const report = devtools.whyNotRefetched(h.cache.key(ops.orderList));

    expect(report.outcome).toBe('missed');
    expect(report.cause.label).toContain('no mutation or frame batch');

    stop();
    devtools.dispose();
  });
});

describe('explain picks the question', () => {
  it('returns the refetch story when it refetched and the miss story when it did not', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    await h.cache.mutate(ops.orderUpdate, { path: { id: 1 } });
    h.flush();
    await h.settle();

    const hit = devtools.explain(h.cache.key(ops.orderList)) as RefetchReport;

    expect(hit.reason).toBe('invalidation');

    await h.cache.mutate(ops.orderCreate, { body: {} });
    h.flush();
    await h.settle();

    const miss = devtools.explain(h.cache.key(ops.orderList)) as MissReport;

    expect(miss.outcome).toBe('missed');

    stop();
    devtools.dispose();
  });
});

describe('asking before running it', () => {
  it('reports what an operation would invalidate and who it would reach', async () => {
    const h = harness();
    const devtools = attach(h.cache, { now: counter() });

    const stop = h.cache.subscribe(ops.orderList, undefined, () => undefined);
    await h.settle();

    const before = h.calls.length;
    const preview = devtools.wouldInvalidate(ops.orderCreate, { body: {} }, { id: 9 });

    // Asking must not be answered by doing.
    expect(h.calls.length).toBe(before);
    expect(preview.tags).toEqual(['Order:9']);
    expect(preview.missed).toEqual(['Order:9']);
    expect(preview.hits[0]?.queries).toEqual([]);

    const covered = devtools.wouldInvalidate(ops.orderUpdate, { path: { id: 1 } });

    expect(covered.tags).toEqual(['Order:1', 'Order[]']);
    expect(covered.hits.find((hit) => hit.tag === 'Order[]')?.queries).toEqual([
      h.cache.key(ops.orderList),
    ]);

    stop();
    devtools.dispose();
  });
});
