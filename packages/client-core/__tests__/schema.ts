import type { EntitySchema } from '../src/types';

/**
 * The table the generator emits, plus the `fields` links it does not emit yet.
 *
 * `Envelope` has an `idField` no payload ever carries, which is how a
 * non-entity wrapper -- `{data: ...}`, `{items: [...], total: n}` -- routes
 * typenames to its children without ever becoming an entity itself.
 */
export const schema: EntitySchema = {
  Order: {
    idField: 'id',
    fields: { customer: 'Customer', items: 'LineItem', related: 'Order' },
  },
  Customer: {
    idField: 'id',
    fields: { orders: 'Order' },
  },
  LineItem: {
    idField: 'sku',
  },
  // Declared with an explicit non-`id` identity, the ForgeEntity() escape.
  Invoice: {
    idField: 'invoiceNumber',
  },
  Envelope: {
    idField: '__never',
    fields: { data: 'Order', items: 'Order' },
  },
};
