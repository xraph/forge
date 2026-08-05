import { describe, expect, it } from 'vitest';

import { normalize } from '../src/normalize';
import { EntityStore } from '../src/store';
import type { EntitySchema } from '../src/types';

/**
 * The other half of an end-to-end proof.
 *
 * Every table in `./schema.ts` is written by hand for the runtime's
 * convenience. This one is COPIED VERBATIM out of the `ops.ts` the Go
 * generator emits for the fixture in
 * `internal/client/generators/typescript/e2e_entity_fields_test.go`, which is
 * itself driven from a real OpenAPI file on disk through `SpecParser.ParseFile`.
 * Keep the two in step: the Go test asserts these exact bytes are produced,
 * and this file asserts they do what they were emitted to do.
 *
 * What the generator decided, visible here:
 *   - `customer` carries the typename of what it contains.
 *   - `items` carries the ELEMENT typename, `LineItem`, not an array marker.
 *   - `parent` is a self edge, so recursion through same-typed nesting works.
 *   - `status` is absent: its type `OrderStatus` is named but is not an
 *     entity, so an edge to it would name a table entry that does not exist.
 *   - `audit` is absent: an inline object has no typename to record.
 */
const entities = {
  Customer: { idField: 'id', fields: { orders: 'Order' } },
  LineItem: { idField: 'id' },
  Order: { idField: 'id', fields: { customer: 'Customer', items: 'LineItem', parent: 'Order' } },
} as const satisfies EntitySchema;

describe('the generated entity table', () => {
  it('normalizes a nested entity of a different type', () => {
    const { skeleton, records, deps } = normalize(
      { id: 'o-1', status: 'open', customer: { id: 'c-3', name: 'Ada' } },
      entities,
      'Order',
    );

    expect(skeleton).toEqual({ __ref: 'Order:o-1' });
    expect(records.get('Customer:c-3')).toEqual({ id: 'c-3', name: 'Ada' });
    expect(records.get('Order:o-1')).toEqual({
      id: 'o-1',
      status: 'open',
      customer: { __ref: 'Customer:c-3' },
    });
    expect([...deps].sort()).toEqual(['Customer:c-3', 'Order:o-1']);
  });

  it('normalizes an array-valued edge through its element typename', () => {
    const { records } = normalize(
      { id: 'o-1', items: [{ id: 'li-1', qty: 1 }, { id: 'li-2', qty: 2 }] },
      entities,
      'Order',
    );

    expect(records.get('LineItem:li-1')).toEqual({ id: 'li-1', qty: 1 });
    expect(records.get('LineItem:li-2')).toEqual({ id: 'li-2', qty: 2 });
    expect(records.get('Order:o-1')).toEqual({
      id: 'o-1',
      items: [{ __ref: 'LineItem:li-1' }, { __ref: 'LineItem:li-2' }],
    });
  });

  it('recurses back through the Customer -> Order edge', () => {
    const { records } = normalize(
      { id: 'o-1', customer: { id: 'c-3', orders: [{ id: 'o-2' }] } },
      entities,
      'Order',
    );

    expect(records.get('Order:o-2')).toEqual({ id: 'o-2' });
    expect(records.get('Customer:c-3')).toEqual({
      id: 'c-3',
      orders: [{ __ref: 'Order:o-2' }],
    });
  });

  it('leaves a property with no edge inline', () => {
    // `status` is a named non-entity and `audit` is anonymous; the generator
    // records neither, so both survive the walk untouched.
    const audit = { by: 'ada' };
    const { records } = normalize({ id: 'o-1', status: 'open', audit }, entities, 'Order');

    const order = records.get('Order:o-1');

    expect(order?.status).toBe('open');
    expect(order?.audit).toBe(audit);
  });

  it('commits the nested entity to the store, which is what the feature buys', () => {
    const store = new EntityStore();

    const { skeleton, deps } = store.write(
      { id: 'o-1', customer: { id: 'c-3', name: 'Ada' } },
      entities,
      'Order',
    );

    // Keyed on its own, and named as a dependency of the query holding this
    // skeleton: a later `PATCH /customers/c-3` reaches this order's view with
    // no refetch, which is the whole point of lifting it out.
    expect(store.has('Customer:c-3')).toBe(true);
    expect(store.getRecord('Customer:c-3')?.data).toEqual({ id: 'c-3', name: 'Ada' });
    expect(deps.has('Customer:c-3')).toBe(true);

    // And a write to the nested record shows through the order's skeleton
    // without the order itself being written again.
    store.put('Customer:c-3', { name: 'Grace' });

    expect(store.read<{ customer: { name: string } }>(skeleton).customer.name).toBe('Grace');
  });
});
