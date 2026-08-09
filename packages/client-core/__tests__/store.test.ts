import { describe, expect, it } from 'vitest';

import { makeRef } from '../src/ref';
import { EntityStore, OPTIMISTIC, denormalize } from '../src/store';
import type { OverlayLayer } from '../src/store';
import type { EntityKey, EntityRecord } from '../src/types';
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

  // The change-detection trap: one object reached through two fields is a
  // DAG, not a cycle. A comparison that remembers every object it has seen
  // rather than the route it took answers "already equal" for the second
  // branch without ever looking at what it is being compared against, drops
  // a real change, and skips both the version bump and the invalidation.
  // No response parsed from JSON aliases, so no property test can reach this
  // -- but `put` with hand-built objects is what the optimistic-overlay and
  // live-frame chunks do.
  it('sees a change under two fields that aliased one object', () => {
    const store = new EntityStore();
    const shared = { n: 1 };

    store.put('Order:1', { x: shared, y: shared });

    expect(store.put('Order:1', { x: { n: 1 }, y: { n: 2 } })).toBe(true);
    expect(store.getRecord('Order:1')?.version).toBe(2);
    expect(store.getRecord('Order:1')?.data).toEqual({ x: { n: 1 }, y: { n: 2 } });
  });

  it('still reports no change when an aliased object is rewritten identically', () => {
    const store = new EntityStore();
    const shared = { n: 1 };

    store.put('Order:1', { x: shared, y: shared });

    expect(store.put('Order:1', { x: { n: 1 }, y: { n: 1 } })).toBe(false);
    expect(store.getRecord('Order:1')?.version).toBe(1);
  });

  it('sees a change deep under an aliased branch', () => {
    const store = new EntityStore();
    const shared = { deep: { n: 1 } };

    store.put('Order:1', { x: shared, y: shared, z: shared });

    expect(
      store.put('Order:1', {
        x: { deep: { n: 1 } },
        y: { deep: { n: 1 } },
        z: { deep: { n: 9 } },
      }),
    ).toBe(true);
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
    // Empty rather than `[undefined, undefined]`: a reference with no record
    // behind it is a hole, and a hole in a list is one fewer element. See
    // below.
    expect(denormalize(skeleton, store)).toEqual([]);
  });

  it('drops a reference to a missing record rather than rendering a hole', () => {
    // A settled skeleton is never rewritten when an entity is evicted, so this
    // is the only place the gap can be closed. Pushing `undefined` hands the
    // application a list whose first `.map(o => o.id)` throws -- and it is
    // handed over *synchronously*, before any refetch the eviction triggered
    // can land, so repairing it on the refetch repairs it too late.
    const { store, skeleton } = seeded();

    store.evict('Order:8');

    expect(denormalize(skeleton, store)).toEqual([list[0]]);
  });

  it('leaves a literal null or undefined the server sent alone', () => {
    // Only a *reference* is a hole. A response that genuinely contained a null
    // element is data, and shortening that array would be the runtime lying
    // about what the server said.
    const store = new EntityStore();
    const { skeleton } = store.write(
      { items: [{ id: 7, total: 1 }, null, undefined] },
      { Envelope: { idField: '__never', fields: { items: 'Order' } }, ...schema },
      'Envelope',
    );

    expect(store.read(skeleton)).toEqual({ items: [{ id: 7, total: 1 }, null, undefined] });

    store.evict('Order:7');

    expect(store.read(skeleton)).toEqual({ items: [null, undefined] });
  });

  it('restores a dropped element when the record comes back', () => {
    const store = new EntityStore();
    const { skeleton } = store.write([{ id: 7, total: 1 }], schema, 'Order');

    store.evict('Order:7');
    expect(store.read(skeleton)).toEqual([]);

    // The memo recorded the key as a dependency *before* looking the record
    // up, so the array recomputes when it arrives rather than staying short.
    store.put('Order:7', { id: 7, total: 5 });
    expect(store.read(skeleton)).toEqual([{ id: 7, total: 5 }]);
  });

  it('leaves an object field pointing at an evicted record as undefined', () => {
    // The array case shortens; a named field has nowhere to go and becomes
    // `undefined`, which is what `order.customer` reading undefined after the
    // customer was deleted should look like.
    const { store, skeleton } = seeded();

    store.evict('Customer:c-3');

    expect(store.read<Record<string, unknown>[]>(skeleton)[0]).toEqual({
      id: 7,
      total: 99,
      customer: undefined,
    });
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

  // A plain-object cycle has no entity key to hang invalidation on, so its
  // memos are indexed under the cycle entry's dependencies instead of being
  // thrown away. Discarding them voided sharing for the whole read, which is
  // the useSyncExternalStore tear this chunk exists to prevent -- and the
  // cyclic object need not have anything to do with the subtree that lost
  // its identity.
  it('keeps structural sharing intact around a plain-object cycle', () => {
    const meta: Record<string, unknown> = { page: 1 };
    meta.self = meta;

    const store = new EntityStore();
    const { skeleton } = store.write(
      { items: [{ id: 7, total: 1 }, { id: 8, total: 2 }], meta },
      schema,
      'Envelope',
    );

    const a = denormalize<Record<string, any>>(skeleton, store);
    const b = denormalize<Record<string, any>>(skeleton, store);

    expect(b).toBe(a);
    expect(b.items).toBe(a.items);
    expect(b.items[0]).toBe(a.items[0]);
    expect(b.meta).toBe(a.meta);
  });

  it('leaves a cycle that depends on nothing alone when an entity changes', () => {
    const meta: Record<string, unknown> = { page: 1 };
    meta.self = meta;

    const store = new EntityStore();
    const { skeleton } = store.write({ items: [{ id: 7, total: 1 }], meta }, schema, 'Envelope');

    const a = denormalize<Record<string, any>>(skeleton, store);

    store.put('Order:7', { total: 2 });

    const b = denormalize<Record<string, any>>(skeleton, store);

    expect(b).not.toBe(a);
    expect(b.items[0].total).toBe(2);
    // The cycle reaches no entity, so nothing about it went stale.
    expect(b.meta).toBe(a.meta);
    expect(b.meta.self).toBe(b.meta);
  });

  it('invalidates a cycle that does reach an entity', () => {
    const store = new EntityStore();
    const wrapper: Record<string, unknown> = { data: { id: 7, total: 1 } };
    wrapper.wrapper = wrapper;

    const { skeleton } = store.write({ wrapper }, schema, 'Envelope');

    const a = denormalize<Record<string, any>>(skeleton, store);
    expect(a.wrapper.wrapper).toBe(a.wrapper);
    expect(a.wrapper.data).toEqual({ id: 7, total: 1 });
    expect(denormalize(skeleton, store)).toBe(a);

    store.put('Order:7', { total: 2 });

    const b = denormalize<Record<string, any>>(skeleton, store);

    expect(b.wrapper).not.toBe(a.wrapper);
    expect(b.wrapper.data.total).toBe(2);
    expect(b.wrapper.wrapper).toBe(b.wrapper);
    expect(denormalize(skeleton, store)).toBe(b);
  });

  // The cycle sits at the root of the skeleton, so there is no ancestor memo
  // to short-circuit the read. Sharing has to come from the cycle's own memo.
  it('keeps sharing when the cycle is the root of the skeleton', () => {
    const store = new EntityStore();
    const root: Record<string, unknown> = { data: { id: 7, total: 1 } };
    root.self = root;

    const { skeleton } = store.write(root, schema, 'Envelope');

    const a = denormalize<Record<string, any>>(skeleton, store);
    expect(a.self).toBe(a);
    expect(a.data).toEqual({ id: 7, total: 1 });
    expect(denormalize(skeleton, store)).toBe(a);

    store.put('Order:7', { total: 2 });

    const b = denormalize<Record<string, any>>(skeleton, store);
    expect(b).not.toBe(a);
    expect(b.data.total).toBe(2);
    expect(b.self).toBe(b);
    expect(denormalize(skeleton, store)).toBe(b);
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

/** A layer under the test's control. The real one arrives in Task 2. */
function stubLayer(overrides: Map<EntityKey, Record<string, unknown> | null>): OverlayLayer {
  return {
    effective(key) {
      const patch = overrides.get(key);

      if (patch === undefined) return undefined;
      if (patch === null) return undefined;

      return { data: patch, version: 1 } satisfies EntityRecord;
    },
    holds: (key) => overrides.has(key),
    rebase: () => undefined,
  };
}

describe('the overlay seam', () => {
  it('reads a record through the layer rather than from base', () => {
    const store = new EntityStore();
    store.put('Order:7', { id: 7, status: 'open' });

    store.overlays = stubLayer(new Map([['Order:7', { id: 7, status: 'shipped' }]]));

    // objectContaining rather than a bare equality: the read is also stamped
    // with the OPTIMISTIC symbol, which Vitest's toEqual walks even though
    // Object.keys and JSON.stringify do not -- see the stamping test below for
    // that string-key invisibility guarantee.
    expect(store.read(makeRef('Order:7'))).toEqual(
      expect.objectContaining({ id: 7, status: 'shipped' }),
    );
  });

  it('rehydrates a layer-deleted record as a hole, exactly as an eviction does', () => {
    const store = new EntityStore();
    const { skeleton } = store.write([{ id: 7 }, { id: 8 }], { Order: { idField: 'id' } }, 'Order');

    store.overlays = stubLayer(new Map([['Order:7', null]]));

    expect(store.read(skeleton)).toEqual([{ id: 8 }]);
  });

  it('stamps OPTIMISTIC on a held record and on nothing else', () => {
    const store = new EntityStore();
    store.put('Order:7', { id: 7 });
    store.put('Order:8', { id: 8 });

    store.overlays = stubLayer(new Map([['Order:7', { id: 7, status: 'shipped' }]]));

    const seven = store.read<Record<string, unknown>>(makeRef('Order:7'));
    const eight = store.read<Record<string, unknown>>(makeRef('Order:8'));

    expect((seven as never)[OPTIMISTIC]).toBe(true);
    expect((eight as never)[OPTIMISTIC]).toBeUndefined();
    // Invisible to everything that walks an object by its string keys -- the
    // overlaid data (id, status) still shows up, only the stamp does not.
    expect(Object.keys(seven)).toEqual(['id', 'status']);
    expect(JSON.stringify(seven)).toBe('{"id":7,"status":"shipped"}');
  });

  it('touch drops memos for the named keys and leaves the rest sharing', () => {
    const store = new EntityStore();
    const { skeleton } = store.write([{ id: 7 }, { id: 8 }], { Order: { idField: 'id' } }, 'Order');
    const before = store.read<unknown[]>(skeleton);

    store.touch(['Order:7']);
    const after = store.read<unknown[]>(skeleton);

    expect(after[1]).toBe(before[1]);
    expect(after).not.toBe(before);
  });

  it('touch on a key nothing ever read bumps no version', () => {
    const store = new EntityStore();

    expect(store.version).toBe(0);

    store.touch(['Never:Seen']);

    expect(store.version).toBe(0);
  });
});
