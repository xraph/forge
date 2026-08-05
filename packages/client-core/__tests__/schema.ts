import type { EntitySchema } from '../src/types';

/**
 * A hand-written table exercising the runtime's whole vocabulary.
 *
 * The generator emits `fields` for both entity-to-entity edges and the
 * non-entity hops between them -- see `./generated-fields.test.ts` and
 * `./envelope.test.ts`, which drive tables copied verbatim out of a generated
 * `ops.ts`.
 *
 * `Envelope` carries no `idField` at all, which is how a wrapper --
 * `{data: ...}`, `{items: [...], total: n}` -- routes typenames to its
 * children without ever becoming an entity itself. It used to spell that as an
 * `idField` no payload carries; the omission says the same thing without
 * depending on no future payload ever using that name.
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
    fields: { data: 'Order', items: 'Order', wrapper: 'Envelope', invoice: 'Invoice' },
  },
};
