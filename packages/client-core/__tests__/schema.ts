import type { EntitySchema } from '../src/types';

/**
 * A hand-written table exercising the runtime's whole vocabulary.
 *
 * The generator now emits `fields` for entity-to-entity edges -- see
 * `./generated-fields.test.ts`, which drives a table copied verbatim out of a
 * generated `ops.ts`. What it does NOT emit is the `Envelope` entry below.
 *
 * `Envelope` has an `idField` no payload ever carries, which is how a
 * non-entity wrapper -- `{data: ...}`, `{items: [...], total: n}` -- routes
 * typenames to its children without ever becoming an entity itself.
 */
export const schema: EntitySchema = {
  Order: {
    idField: 'id',
    fields: { customer: 'Customer', items: 'LineItem', related: 'Order', invoice: 'Invoice' },
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
    fields: { data: 'Order', items: 'Order', wrapper: 'Envelope', invoice: 'Invoice' },
  },
};
