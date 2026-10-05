// Ported from packages/client-core/__tests__/generated-fields.test.ts.
//
// The table below is the one the Go generator emits for the fixture in
// internal/client/generators/typescript/e2e_entity_fields_test.go, spelled as
// the Dart generator (plan 02) will emit it.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/schema.dart';

const EntitySchema entities = {
  'Customer': EntityMeta(idField: 'id', fields: {'orders': 'Order'}),
  'LineItem': EntityMeta(idField: 'id'),
  'Order': EntityMeta(
    idField: 'id',
    fields: {'customer': 'Customer', 'items': 'LineItem', 'parent': 'Order'},
  ),
};

void main() {
  group('the generated entity table', () {
    test('normalizes a nested entity of a different type', () {
      final result = normalize(
        {
          'id': 'o-1',
          'status': 'open',
          'customer': {'id': 'c-3', 'name': 'Ada'},
        },
        'Order',
        entities,
      );

      expect(result.skeleton, refTo('Order:o-1'));
      expect(result.records['Customer:c-3'], {'id': 'c-3', 'name': 'Ada'});
      expect(result.records['Order:o-1'], {
        'id': 'o-1',
        'status': 'open',
        'customer': refTo('Customer:c-3'),
      });
      expect(result.deps.toList()..sort(), ['Customer:c-3', 'Order:o-1']);
    });

    test('normalizes an array-valued edge through its element typename', () {
      final result = normalize(
        {
          'id': 'o-1',
          'items': [
            {'id': 'li-1', 'qty': 1},
            {'id': 'li-2', 'qty': 2},
          ],
        },
        'Order',
        entities,
      );

      expect(result.records['LineItem:li-1'], {'id': 'li-1', 'qty': 1});
      expect(result.records['LineItem:li-2'], {'id': 'li-2', 'qty': 2});
      expect(result.records['Order:o-1'], {
        'id': 'o-1',
        'items': [refTo('LineItem:li-1'), refTo('LineItem:li-2')],
      });
    });

    test('recurses back through the Customer -> Order edge', () {
      final result = normalize(
        {
          'id': 'o-1',
          'customer': {
            'id': 'c-3',
            'orders': [
              {'id': 'o-2'},
            ],
          },
        },
        'Order',
        entities,
      );

      expect(result.records['Order:o-2'], {'id': 'o-2'});
      expect(result.records['Customer:c-3'], {
        'id': 'c-3',
        'orders': [refTo('Order:o-2')],
      });
    });

    test('leaves a property with no edge inline', () {
      final audit = {'by': 'ada'};
      final result = normalize(
        {'id': 'o-1', 'status': 'open', 'audit': audit},
        'Order',
        entities,
      );

      final order = result.records['Order:o-1'];

      expect(order?['status'], 'open');
      expect(order?['audit'], same(audit));
    });

    test(
      'commits the nested entity to the store, which is what the feature buys',
      () {
        final store = EntityStore();

        final staged = store.write(
          {
            'id': 'o-1',
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
          entities,
          'Order',
        );

        expect(store.has('Customer:c-3'), isTrue);
        expect(store.getRecord('Customer:c-3')?.data, {
          'id': 'c-3',
          'name': 'Ada',
        });
        expect(staged.deps.contains('Customer:c-3'), isTrue);

        store.put('Customer:c-3', {'name': 'Grace'});

        final read = store.read(staged.skeleton)! as Map<String, Object?>;
        expect((read['customer']! as Map<String, Object?>)['name'], 'Grace');
      },
    );
  });
}
