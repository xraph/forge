import { describe, expect, it } from 'vitest';

import { normalize } from '../src/normalize';
import { EntityStore } from '../src/store';
import type { EntitySchema } from '../src/types';

/**
 * The other half of the envelope proof.
 *
 * Both tables below are COPIED VERBATIM out of the `src/ops.ts` that
 * internal/client/generators/typescript/e2e_envelope_test.go generates from a
 * real OpenAPI document. Neither is hand-designed for this file, and the two
 * halves have to be checked in together: Go can emit a table this runtime does
 * not read, and this runtime can read a table Go does not emit. The Go half
 * asserts the bytes; this half asserts what the bytes do.
 *
 * If a Go change makes these disagree, the Go test fails on the bytes and this
 * one keeps passing against a table nothing produces any more -- which is why
 * the Go test names this file and this file names it back.
 */
const entities = {
  Carrier: { idField: 'id' },
  Customer: { idField: 'id', fields: { orders: 'Order' } },
  Order: { idField: 'id', fields: { customer: 'Customer', parent: 'Order', shipment: 'Shipment' } },
  OrderReport: { fields: { topOrders: 'Order' } },
  PageOrder: { fields: { items: 'Order' } },
  Shipment: { fields: { carrier: 'Carrier' } },
} as const satisfies EntitySchema;

const ops = {
  'orders.list': {
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    rootType: 'PageOrder',
    provides: ['Order:{id}', 'Order[]'],
    invalidates: [],
  },
  'reports.orders': {
    method: 'GET',
    path: '/reports/orders',
    rootType: 'OrderReport',
    provides: [],
    invalidates: [],
  },
} as const;

/** A page of orders, as the API would return it. */
const page = {
  items: [
    {
      id: 'o-1',
      customer: { id: 'c-3', name: 'Ada' },
      shipment: { carrier: { id: 'dhl', name: 'DHL' }, weightKg: 2 },
    },
    { id: 'o-2', customer: { id: 'c-3', name: 'Ada' }, shipment: null },
  ],
  total: 2,
  nextCursor: 'abc',
};

describe('enveloped list responses', () => {
  // The gap this closed. An operation returning `PageOrder{items, total}` used
  // to reach the runtime with no rootType and no row to look up, so the whole
  // response stayed inline and nothing in it was ever shared or invalidated.
  it('normalizes the entities inside a page', () => {
    const { skeleton, records, deps } = normalize(page, entities, ops['orders.list'].rootType);

    // The envelope itself survives as a plain object: it has no idField, so it
    // is walked and never stored.
    expect(skeleton).toEqual({
      items: [{ __ref: 'Order:o-1' }, { __ref: 'Order:o-2' }],
      total: 2,
      nextCursor: 'abc',
    });
    expect(records.has('PageOrder:undefined')).toBe(false);

    expect([...records.keys()].sort()).toEqual([
      'Carrier:dhl',
      'Customer:c-3',
      'Order:o-1',
      'Order:o-2',
    ]);
    expect(deps.size).toBe(4);
  });

  // The transitive hop, which is the reason `Shipment` has a row at all.
  // `Shipment` carries no identity, so it stays inline inside its Order -- but
  // the walk continues through it and the Carrier underneath is lifted out.
  it('reaches an entity through a non-entity hop', () => {
    const { records } = normalize(page, entities, 'PageOrder');

    expect(records.get('Order:o-1')?.shipment).toEqual({
      carrier: { __ref: 'Carrier:dhl' },
      weightKg: 2,
    });
    expect(records.get('Carrier:dhl')).toEqual({ id: 'dhl', name: 'DHL' });
  });

  // Why `rootType` is its own field rather than a reuse of `entity`. Passing
  // the entity name where the root type belongs reads Order's field edges --
  // customer, parent, shipment -- against an envelope whose properties are
  // items, total and nextCursor. Nothing matches, and the response silently
  // does not normalize. That is the failure this manifest field exists to make
  // impossible, so it is pinned here rather than left as a comment.
  it('normalizes nothing when handed the entity name instead of the root type', () => {
    const { records } = normalize(page, entities, ops['orders.list'].entity);

    expect(records.size).toBe(0);
  });

  // The store is the point: two views of the same order share one record, so a
  // later write through either is seen by both.
  it('shares records between a page and a single read', () => {
    const store = new EntityStore();

    store.write(page, entities, ops['orders.list'].rootType);
    store.write({ id: 'o-1', customer: { id: 'c-3', name: 'Ada Lovelace' } }, entities, 'Order');

    expect(store.getRecord('Customer:c-3')?.data).toMatchObject({ name: 'Ada Lovelace' });
    expect(store.has('Order:o-2')).toBe(true);
  });

  // The response the Go side deliberately refuses to tag. `OrderReport` looks
  // exactly like `PageOrder` and carries no cache contract -- and it still
  // normalizes, because routing was never the part that needed a declaration.
  // The orders in a report land on the same records the list put there.
  it('normalizes an undeclared wrapper while providing no tags', () => {
    expect(ops['reports.orders'].provides).toEqual([]);

    const { records } = normalize(
      { topOrders: [{ id: 'o-1', customer: { id: 'c-3', name: 'Ada' } }], generatedAt: 'now' },
      entities,
      ops['reports.orders'].rootType,
    );

    expect([...records.keys()].sort()).toEqual(['Customer:c-3', 'Order:o-1']);
  });
});
