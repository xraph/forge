import { describe, expect, it } from 'vitest';

import { EntityStore, denormalize } from '../src/store';
import { schema } from './schema';

const list = [
  { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
  { id: 8, total: 1, customer: { id: 'c-4', name: 'Grace' } },
];

function seeded(): { store: EntityStore; skeleton: unknown } {
  const store = new EntityStore();
  const { skeleton } = store.write(list, schema, 'Order');

  return { store, skeleton };
}

describe('EntityStore', () => {
  it('rebuilds the response it was given', () => {
    const { store, skeleton } = seeded();

    expect(denormalize(skeleton, store)).toEqual(list);
  });

  it('versions each entity independently and bumps on write', () => {
    const { store } = seeded();

    expect(store.getRecord('Order:7')?.version).toBe(1);

    store.put('Order:7', { total: 120 });

    expect(store.getRecord('Order:7')?.version).toBe(2);
    expect(store.getRecord('Order:8')?.version).toBe(1);
    expect(store.getRecord('Order:7')?.data).toEqual({
      id: 7,
      total: 120,
      customer: { __ref: 'Customer:c-3' },
    });
  });

  it('does not bump a version when the write changes nothing', () => {
    const { store } = seeded();
    const before = store.getRecord('Order:7');
    const writes = store.version;

    expect(store.put('Order:7', { total: 99 })).toBe(false);
    expect(store.getRecord('Order:7')).toBe(before);
    expect(store.version).toBe(writes);
  });

  it('does not bump a version when the same response is written again', () => {
    const { store } = seeded();
    const writes = store.version;

    store.write(list, schema, 'Order');

    expect(store.version).toBe(writes);
  });

  it('preserves fields a later, narrower write does not mention', () => {
    const { store } = seeded();

    store.put('Order:7', { total: 120 });

    expect(store.getRecord('Order:7')?.data.customer).toEqual({ __ref: 'Customer:c-3' });
  });

  it('serves nothing from a cleared store, including out of a memo', () => {
    const { store, skeleton } = seeded();

    denormalize(skeleton, store);
    store.clear();

    expect(store.size).toBe(0);
    expect(denormalize(skeleton, store)).toEqual([undefined, undefined]);
  });

  it('rehydrates a reference to a missing record as undefined', () => {
    const { store, skeleton } = seeded();

    store.evict('Order:8');

    expect(denormalize(skeleton, store)).toEqual([list[0], undefined]);
  });

  it('recomputes when a missing record arrives', () => {
    const store = new EntityStore();
    const { skeleton } = store.write([{ id: 7, total: 1 }], schema, 'Order');

    store.evict('Order:7');
    expect(store.read(skeleton)).toEqual([undefined]);

    store.put('Order:7', { id: 7, total: 5 });
    expect(store.read(skeleton)).toEqual([{ id: 7, total: 5 }]);
  });
});

describe('structural sharing', () => {
  it('returns identical objects when nothing was written', () => {
    const { store, skeleton } = seeded();

    const a = denormalize<Array<Record<string, unknown>>>(skeleton, store);
    const b = denormalize<Array<Record<string, unknown>>>(skeleton, store);

    expect(b).toBe(a);
    expect(b[0]).toBe(a[0]);
    expect(b[0].customer).toBe(a[0].customer);
    expect(b[1]).toBe(a[1]);
  });

  it('changes only the affected subtree when one entity is written', () => {
    const { store, skeleton } = seeded();

    const before = denormalize<Array<Record<string, unknown>>>(skeleton, store);

    store.put('Order:8', { total: 2 });

    const after = denormalize<Array<Record<string, unknown>>>(skeleton, store);

    // The root changed, because one of its elements did.
    expect(after).not.toBe(before);
    // Order:7 and everything under it did not.
    expect(after[0]).toBe(before[0]);
    expect(after[0].customer).toBe(before[0].customer);
    // Order:8 did.
    expect(after[1]).not.toBe(before[1]);
    // Its untouched customer did not.
    expect(after[1].customer).toBe(before[1].customer);
    expect(after[1].total).toBe(2);
  });

  it('propagates a change through a nested entity to its holders', () => {
    const { store, skeleton } = seeded();

    const before = denormalize<Array<Record<string, unknown>>>(skeleton, store);

    store.put('Customer:c-3', { name: 'Ada Lovelace' });

    const after = denormalize<Array<Record<string, unknown>>>(skeleton, store);

    expect(after[0]).not.toBe(before[0]);
    expect(after[0].customer).not.toBe(before[0].customer);
    // Order:8 has a different customer and must not have been recomputed.
    expect(after[1]).toBe(before[1]);
  });

  // The transitive case a one-hop version check gets wrong: the order's own
  // dependency, Customer:c-3, did not change -- something two hops away did.
  it('propagates a change two hops away', () => {
    const store = new EntityStore();
    const { skeleton } = store.write(
      {
        id: 7,
        customer: { id: 'c-3', name: 'Ada', orders: [{ id: 9, total: 1 }] },
      },
      schema,
      'Order',
    );

    const before = denormalize<Record<string, unknown>>(skeleton, store);

    store.put('Order:9', { total: 2 });

    const after = denormalize<Record<string, unknown>>(skeleton, store);

    expect(after).not.toBe(before);
    expect(after).toEqual({
      id: 7,
      customer: { id: 'c-3', name: 'Ada', orders: [{ id: 9, total: 2 }] },
    });
  });

  it('keeps identity for an unrelated skeleton over the same store', () => {
    const { store, skeleton } = seeded();
    const detail = store.write({ id: 7, total: 99 }, schema, 'Order').skeleton;

    const listBefore = denormalize<Array<Record<string, unknown>>>(skeleton, store);
    const detailBefore = denormalize(detail, store);

    store.put('Order:8', { total: 3 });

    expect(denormalize(detail, store)).toBe(detailBefore);
    expect(denormalize<Array<Record<string, unknown>>>(skeleton, store)[0]).toBe(listBefore[0]);
  });
});

describe('cycles', () => {
  function cyclic(): { store: EntityStore; skeleton: unknown } {
    const order: Record<string, unknown> = { id: 7, total: 99 };
    const customer: Record<string, unknown> = { id: 'c-3', name: 'Ada' };
    order.customer = customer;
    customer.orders = [order];

    const store = new EntityStore();

    return { store, ...store.write(order, schema, 'Order') };
  }

  // The decision: rebuild the cyclic graph rather than stop at the back-edge
  // and hand back a raw reference. An application walking an association must
  // never have to recognise a cache internal.
  it('rebuilds the cycle as a cycle', () => {
    const { store, skeleton } = cyclic();
    const order = denormalize<Record<string, any>>(skeleton, store);

    expect(order.id).toBe(7);
    expect(order.customer.name).toBe('Ada');
    expect(order.customer.orders[0]).toBe(order);
  });

  it('keeps the cyclic result stable across reads', () => {
    const { store, skeleton } = cyclic();

    const a = denormalize<Record<string, any>>(skeleton, store);
    const b = denormalize<Record<string, any>>(skeleton, store);

    expect(b).toBe(a);
    expect(b.customer).toBe(a.customer);
  });

  it('recomputes a whole cycle when one of its members changes', () => {
    const { store, skeleton } = cyclic();

    const before = denormalize<Record<string, any>>(skeleton, store);

    store.put('Customer:c-3', { name: 'Grace' });

    const after = denormalize<Record<string, any>>(skeleton, store);

    expect(after).not.toBe(before);
    expect(after.customer.name).toBe('Grace');
    expect(after.customer.orders[0]).toBe(after);
  });

  // Found by the round-trip property, and worth pinning: the response is a
  // finite tree, but there is only one Order:1, so merging its two
  // occurrences produces a record that references itself. Normalization
  // creates the cycle; the input never had one.
  it('handles a cycle that merging introduces', () => {
    const store = new EntityStore();
    const { skeleton } = store.write({ id: 1, related: [{ id: 1 }] }, schema, 'Order');

    expect(store.size).toBe(1);
    expect(store.getRecord('Order:1')?.data).toEqual({ id: 1, related: [{ __ref: 'Order:1' }] });

    const order = denormalize<Record<string, any>>(skeleton, store);

    expect(order.id).toBe(1);
    expect(order.related[0]).toBe(order);
  });

  it('stores each record acyclically even when the graph cycles', () => {
    const { store } = cyclic();

    for (const key of [...store.keys()]) {
      expect(() => JSON.stringify(store.getRecord(key)?.data)).not.toThrow();
    }
  });

  it('survives a cycle that closes through plain objects', () => {
    const meta: Record<string, unknown> = { page: 1 };
    meta.self = meta;

    const store = new EntityStore();
    const { skeleton } = store.write({ items: [{ id: 7, total: 1 }], meta }, schema, 'Envelope');

    const a = denormalize<Record<string, any>>(skeleton, store);
    expect(a.meta.self).toBe(a.meta);
    expect(a.items[0]).toEqual({ id: 7, total: 1 });

    store.put('Order:7', { total: 2 });

    const b = denormalize<Record<string, any>>(skeleton, store);
    expect(b.items[0].total).toBe(2);
    expect(b.meta.self).toBe(b.meta);
  });
});

describe('dependencies', () => {
  it('reports every key a skeleton reaches, transitively', () => {
    const { store, skeleton } = seeded();

    expect([...store.dependencies(skeleton)].sort()).toEqual([
      'Customer:c-3',
      'Customer:c-4',
      'Order:7',
      'Order:8',
    ]);
  });

  it('reports a key the store does not hold yet', () => {
    const store = new EntityStore();
    const { skeleton } = store.write({ id: 7 }, schema, 'Order');

    store.evict('Order:7');

    expect([...store.dependencies(skeleton)]).toEqual(['Order:7']);
  });
});
