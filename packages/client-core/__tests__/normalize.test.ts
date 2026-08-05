import { describe, expect, it } from 'vitest';

import { normalize } from '../src/normalize';
import { isRef } from '../src/ref';
import { EntityStore } from '../src/store';
import { schema } from './schema';

describe('normalize', () => {
  it('lifts the root entity out and leaves a reference', () => {
    const { skeleton, records, deps } = normalize({ id: 7, total: 99 }, schema, 'Order');

    expect(isRef(skeleton)).toBe(true);
    expect(skeleton).toEqual({ __ref: 'Order:7' });
    expect(records.get('Order:7')).toEqual({ id: 7, total: 99 });
    expect([...deps]).toEqual(['Order:7']);
  });

  it('carries the typename through arrays', () => {
    const { skeleton, records } = normalize(
      [{ id: 7, total: 99 }, { id: 8, total: 1 }],
      schema,
      'Order',
    );

    expect(skeleton).toEqual([{ __ref: 'Order:7' }, { __ref: 'Order:8' }]);
    expect(records.size).toBe(2);
  });

  it('extracts nested entities of another type through the field links', () => {
    const { skeleton, records, deps } = normalize(
      {
        id: 7,
        total: 99,
        customer: { id: 'c-3', name: 'Ada' },
        items: [{ sku: 'A', qty: 1 }, { sku: 'B', qty: 2 }],
      },
      schema,
      'Order',
    );

    expect(skeleton).toEqual({ __ref: 'Order:7' });
    expect(records.get('Order:7')).toEqual({
      id: 7,
      total: 99,
      customer: { __ref: 'Customer:c-3' },
      items: [{ __ref: 'LineItem:A' }, { __ref: 'LineItem:B' }],
    });
    expect(records.get('Customer:c-3')).toEqual({ id: 'c-3', name: 'Ada' });
    expect([...deps].sort()).toEqual(['Customer:c-3', 'LineItem:A', 'LineItem:B', 'Order:7']);
  });

  it('routes through a wrapper that is not itself an entity', () => {
    const { skeleton, records } = normalize(
      { items: [{ id: 7 }], total: 1 },
      schema,
      'Envelope',
    );

    expect(skeleton).toEqual({ items: [{ __ref: 'Order:7' }], total: 1 });
    expect(records.get('Order:7')).toEqual({ id: 7 });
  });

  // The contract `EntityMeta.idField?` exists to express. A table entry with
  // `fields` and no identity is a signpost: it routes typenames onward and is
  // itself never stored, no matter what the payload happens to carry.
  it('walks a type with no idField for its fields without ever storing it', () => {
    const { skeleton, records, deps } = normalize(
      { items: [{ id: 7 }], total: 1 },
      { Envelope: { fields: { items: 'Order' } }, Order: { idField: 'id' } },
      'Envelope',
    );

    expect(skeleton).toEqual({ items: [{ __ref: 'Order:7' }], total: 1 });
    expect([...records.keys()]).toEqual(['Order:7']);
    expect([...deps]).toEqual(['Order:7']);
  });

  // The guard this needs is real: `node[meta.idField]` with an absent idField
  // reads the literal property "undefined", so a payload carrying that key
  // would be stored under a typename that declared it has no identity.
  it('does not key an idField-less type off a literal "undefined" property', () => {
    const input = { undefined: 'x', items: [{ id: 7 }] };
    const { records } = normalize(
      input,
      { Envelope: { fields: { items: 'Order' } }, Order: { idField: 'id' } },
      'Envelope',
    );

    expect([...records.keys()]).toEqual(['Order:7']);
  });

  // The heuristic the Go side refuses. `{id: 7}` under no declared type, or
  // under a type the table does not name, is data -- not a cache entry that
  // another tenant's record can land on.
  it('does not treat an id property as evidence of an entity', () => {
    const input = { id: 7, total: 99 };
    const { skeleton, records, deps } = normalize(input, schema);

    expect(records.size).toBe(0);
    expect(deps.size).toBe(0);
    expect(skeleton).toBe(input);

    const unnamed = normalize({ id: 7 }, schema, 'NotInTheTable');
    expect(unnamed.records.size).toBe(0);
  });

  it('leaves a declared type inline when it does not carry its id field', () => {
    const { skeleton, records } = normalize({ invoiceNumber: null, amount: 5 }, schema, 'Invoice');

    expect(records.size).toBe(0);
    expect(skeleton).toEqual({ invoiceNumber: null, amount: 5 });

    const identified = normalize({ invoiceNumber: 'INV-1', amount: 5 }, schema, 'Invoice');
    expect(identified.records.get('Invoice:INV-1')).toEqual({ invoiceNumber: 'INV-1', amount: 5 });
  });

  it('rejects ids that cannot key a record', () => {
    for (const id of [null, undefined, '', {}, [], true, NaN]) {
      const { records } = normalize({ id, total: 1 }, schema, 'Order');
      expect(records.size, `id ${String(id)} should not key a record`).toBe(0);
    }
  });

  it('merges an entity that occurs twice with different field sets', () => {
    const { records } = normalize(
      {
        id: 7,
        customer: { id: 'c-3', name: 'Ada' },
        related: [{ id: 9, customer: { id: 'c-3', tier: 'gold' } }],
      },
      schema,
      'Order',
    );

    expect(records.get('Customer:c-3')).toEqual({ id: 'c-3', name: 'Ada', tier: 'gold' });
  });

  it('returns the same reference target for one entity appearing twice', () => {
    const customer = { id: 'c-3', name: 'Ada' };
    const { skeleton } = normalize(
      [
        { id: 7, customer },
        { id: 8, customer },
      ],
      schema,
      'Order',
    );

    expect(skeleton).toEqual([{ __ref: 'Order:7' }, { __ref: 'Order:8' }]);
  });

  it('leaves subtrees containing no entity referentially untouched', () => {
    const meta = { page: { size: 10, cursor: null } };
    const input = { data: [{ id: 7 }], meta };
    const { skeleton } = normalize(input, schema, 'Envelope');

    expect((skeleton as { meta: unknown }).meta).toBe(meta);
  });

  it('does not mutate its input', () => {
    const input = {
      id: 7,
      customer: { id: 'c-3', name: 'Ada' },
      items: [{ sku: 'A' }],
    };
    const before = JSON.parse(JSON.stringify(input));

    normalize(input, schema, 'Order');

    expect(input).toEqual(before);
  });

  // A response that literally contains the reference marker must survive.
  // References are recognised by identity, not by inspecting `__ref`.
  it('round-trips an object shaped like a reference', () => {
    const store = new EntityStore();
    const input = { id: 7, note: { __ref: 'Order:999' } };
    const { skeleton } = store.write(input, schema, 'Order');

    expect(store.read(skeleton)).toEqual(input);
  });

  it('terminates on a cyclic object graph', () => {
    const order: Record<string, unknown> = { id: 7, total: 99 };
    const customer: Record<string, unknown> = { id: 'c-3', name: 'Ada' };
    order.customer = customer;
    customer.orders = [order];

    const { skeleton, records, deps } = normalize(order, schema, 'Order');

    expect(skeleton).toEqual({ __ref: 'Order:7' });
    expect(records.get('Order:7')).toEqual({
      id: 7,
      total: 99,
      customer: { __ref: 'Customer:c-3' },
    });
    expect(records.get('Customer:c-3')).toEqual({
      id: 'c-3',
      name: 'Ada',
      orders: [{ __ref: 'Order:7' }],
    });
    expect([...deps].sort()).toEqual(['Customer:c-3', 'Order:7']);
  });

  it('terminates on a cycle that closes through plain objects', () => {
    const node: Record<string, unknown> = { label: 'a', order: { id: 7 } };
    node.self = node;

    const { skeleton, records } = normalize({ data: node }, schema, 'Envelope');

    expect(records.size).toBe(0);
    const data = (skeleton as { data: Record<string, unknown> }).data;
    expect(data.self).toBe(data);
  });
});
