import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

/// A hand-written table exercising the runtime's whole vocabulary. Ported from
/// `packages/client-core/__tests__/schema.ts`.
///
/// `Envelope` carries no idField, which is how a wrapper routes typenames to
/// its children without becoming an entity itself.
const EntitySchema schema = {
  'Order': EntityMeta(
    idField: 'id',
    fields: {
      'customer': 'Customer',
      'items': 'LineItem',
      'related': 'Order',
      'invoice': 'Invoice',
    },
  ),
  'Customer': EntityMeta(idField: 'id', fields: {'orders': 'Order'}),
  'LineItem': EntityMeta(idField: 'sku'),
  // Declared with an explicit non-`id` identity, the ForgeEntity() escape.
  'Invoice': EntityMeta(idField: 'invoiceNumber'),
  'Envelope': EntityMeta(
    fields: {
      'data': 'Order',
      'items': 'Order',
      'wrapper': 'Envelope',
      'invoice': 'Invoice',
    },
  ),
};

/// Matches an [EntityRef] to [key]. The Dart spelling of TS `toEqual({__ref: key})`.
Matcher refTo(String key) =>
    isA<EntityRef>().having((ref) => ref.key, 'key', key);
