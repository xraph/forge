import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { normalize } from '../src/normalize';
import { EntityStore } from '../src/store';
import type { OperationMeta } from '../src/transport';
import type { EntitySchema } from '../src/types';
import { fakeTransport } from './harness';

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

/**
 * The last mile: the same table, driven through the query cache rather than
 * `normalize` directly.
 *
 * `QueryCache` reaches `store.write` on three paths, and two of them normalize
 * a RESPONSE. Those used to pass `meta.entity`, which is the right string only
 * while the response document happens to be the entity -- true for
 * `GET /orders/{id}` and for a bare `[]Order`, false for every enveloped read.
 * A manifest can carry a perfectly correct `rootType` and still cache nothing
 * if the runtime does not read it, so these assert the wiring rather than the
 * manifest.
 */
describe('enveloped responses through the query cache', () => {
  const orderPageList: OperationMeta = {
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    rootType: 'PageOrder',
    provides: ['Order:{id}', 'Order[]'],
    invalidates: [],
  };

  const orderGet: OperationMeta = {
    method: 'GET',
    path: '/orders/{id}',
    entity: 'Order',
    rootType: 'Order',
    provides: ['Order:{id}'],
    invalidates: [],
  };

  const orderCreate: OperationMeta = {
    method: 'POST',
    path: '/orders',
    entity: 'Order',
    rootType: 'Order',
    provides: [],
    invalidates: [],
  };

  function cache(handler: Parameters<typeof fakeTransport>[0]) {
    const scheduler = manualScheduler();
    const transport = fakeTransport(handler);

    return new QueryCache({ transport, entities, scheduler: scheduler.schedule });
  }

  // The whole point, stated as behaviour a user would notice: a page and a
  // detail view are the same record, so reading the detail updates the page
  // with no refetch. Without the root type the page normalizes to nothing,
  // shares nothing, and keeps showing the name it first loaded.
  it('shares a record between a paginated list and a single read', async () => {
    const queries = cache((request) =>
      request.meta.path === '/orders'
        ? { items: [{ id: 'o-1', customer: { id: 'c-3', name: 'Ada' } }], total: 1 }
        : { id: 'o-1', customer: { id: 'c-3', name: 'Ada Lovelace' } },
    );

    const page = await queries.fetch<{ items: { customer: { name: string } }[] }>(orderPageList);
    expect(page.items[0]?.customer.name).toBe('Ada');

    await queries.fetch(orderGet, { path: { id: 'o-1' } });

    const after = queries.getState<{ items: { customer: { name: string } }[] }>(orderPageList);
    expect(after.data?.items[0]?.customer.name).toBe('Ada Lovelace');
  });

  // The envelope's own scalars are not entities and must survive the round
  // trip untouched -- a page that normalized its items but lost `total` would
  // be a different bug wearing this one's clothes.
  it('keeps the envelope around the records it lifted out', async () => {
    const queries = cache(() => ({
      items: [{ id: 'o-1' }, { id: 'o-2' }],
      total: 2,
      nextCursor: 'abc',
    }));

    const page = await queries.fetch(orderPageList);

    expect(page).toEqual({ items: [{ id: 'o-1' }, { id: 'o-2' }], total: 2, nextCursor: 'abc' });
  });

  // The mutation path writes its response through the same helper. A create
  // returning a bare Order has rootType === entity, so this pins that the
  // fallback did not change behaviour where the two agree.
  it('normalizes a mutation response into the shared store', async () => {
    const queries = cache((request) =>
      request.meta.method === 'POST'
        ? { id: 'o-9', customer: { id: 'c-3', name: 'Grace' } }
        : { items: [{ id: 'o-9', customer: { id: 'c-3', name: 'Ada' } }], total: 1 },
    );

    await queries.fetch(orderPageList);
    await queries.mutate(orderCreate);

    const after = queries.getState<{ items: { customer: { name: string } }[] }>(orderPageList);
    expect(after.data?.items[0]?.customer.name).toBe('Grace');
  });

  // Tags, which is the half of the fix that "it normalizes now" does not by
  // itself prove. `provides` alone would give this query `Order[]` whatever
  // the response did, because that tag is static. The per-id tags and the dep
  // set are derived from the entities the walk actually lifted out, so an
  // envelope that normalized to nothing used to settle with an empty dep set
  // and no `Order:o-1` -- present in the cache, reachable by no invalidation,
  // and therefore serving a stale page forever.
  //
  // This settles through `stage` and `commit`, not `write`: the assertion is
  // that the root type survives the staged path rather than only the direct
  // one, which is the combination neither branch tested on its own.
  it('records the entities inside a page as deps and tags', async () => {
    const queries = cache(() => ({
      items: [
        { id: 'o-1', customer: { id: 'c-3', name: 'Ada' } },
        { id: 'o-2', customer: { id: 'c-3', name: 'Ada' } },
      ],
      total: 2,
    }));

    await queries.fetch(orderPageList);

    const entry = queries.registry.get(queries.key(orderPageList));

    expect(entry?.deps).toEqual(new Set(['Order:o-1', 'Order:o-2', 'Customer:c-3']));
    expect(entry?.tags.has('Order:o-1')).toBe(true);
    expect(entry?.tags.has('Order:o-2')).toBe(true);
  });

  // A manifest generated before rootType existed carries only `entity`. The
  // fallback has to keep those working exactly as they did.
  it('falls back to the entity when a manifest carries no root type', async () => {
    const legacy: OperationMeta = {
      method: 'GET',
      path: '/orders',
      entity: 'Order',
      provides: ['Order[]'],
      invalidates: [],
    };

    const queries = cache((request) =>
      request.meta.path === '/orders'
        ? [{ id: 'o-1', customer: { id: 'c-3', name: 'Ada' } }]
        : { id: 'o-1', customer: { id: 'c-3', name: 'Ada Lovelace' } },
    );

    await queries.fetch(legacy);
    await queries.fetch(orderGet, { path: { id: 'o-1' } });

    const after = queries.getState<{ customer: { name: string } }[]>(legacy);
    expect(after.data?.[0]?.customer.name).toBe('Ada Lovelace');
  });
});
