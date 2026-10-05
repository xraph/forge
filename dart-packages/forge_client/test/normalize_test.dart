// Ported from packages/client-core/__tests__/normalize.test.ts.
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/schema.dart';

void main() {
  group('normalize', () {
    test('lifts the root entity out and leaves a reference', () {
      final result = normalize({'id': 7, 'total': 99}, 'Order', schema);

      expect(isRef(result.skeleton), isTrue);
      expect(result.skeleton, refTo('Order:7'));
      expect(result.records['Order:7'], {'id': 7, 'total': 99});
      expect(result.deps.toList(), ['Order:7']);
    });

    test('carries the typename through arrays', () {
      final result = normalize(
        [
          {'id': 7, 'total': 99},
          {'id': 8, 'total': 1},
        ],
        'Order',
        schema,
      );

      expect(result.skeleton, [refTo('Order:7'), refTo('Order:8')]);
      expect(result.records, hasLength(2));
    });

    test(
      'extracts nested entities of another type through the field links',
      () {
        final result = normalize(
          {
            'id': 7,
            'total': 99,
            'customer': {'id': 'c-3', 'name': 'Ada'},
            'items': [
              {'sku': 'A', 'qty': 1},
              {'sku': 'B', 'qty': 2},
            ],
          },
          'Order',
          schema,
        );

        expect(result.skeleton, refTo('Order:7'));
        expect(result.records['Order:7'], {
          'id': 7,
          'total': 99,
          'customer': refTo('Customer:c-3'),
          'items': [refTo('LineItem:A'), refTo('LineItem:B')],
        });
        expect(result.records['Customer:c-3'], {'id': 'c-3', 'name': 'Ada'});
        expect(result.deps.toList()..sort(), [
          'Customer:c-3',
          'LineItem:A',
          'LineItem:B',
          'Order:7',
        ]);
      },
    );

    test('routes through a wrapper that is not itself an entity', () {
      final result = normalize(
        {
          'items': [
            {'id': 7},
          ],
          'total': 1,
        },
        'Envelope',
        schema,
      );

      expect(result.skeleton, {
        'items': [refTo('Order:7')],
        'total': 1,
      });
      expect(result.records['Order:7'], {'id': 7});
    });

    // A table entry with fields and no identity is a signpost: it routes
    // typenames onward and is itself never stored.
    test(
      'walks a type with no idField for its fields without ever storing it',
      () {
        final result = normalize(
          {
            'items': [
              {'id': 7},
            ],
            'total': 1,
          },
          'Envelope',
          const {
            'Envelope': EntityMeta(fields: {'items': 'Order'}),
            'Order': EntityMeta(idField: 'id'),
          },
        );

        expect(result.skeleton, {
          'items': [refTo('Order:7')],
          'total': 1,
        });
        expect(result.records.keys.toList(), ['Order:7']);
        expect(result.deps.toList(), ['Order:7']);
      },
    );

    // TS reads `node[undefined]` when idField is absent; Dart has no such
    // property lookup, so this pins that a key literally named "undefined"
    // still cannot make a signpost type an entity.
    test(
      'does not key an idField-less type off a literal "undefined" property',
      () {
        final result = normalize(
          {
            'undefined': 'x',
            'items': [
              {'id': 7},
            ],
          },
          'Envelope',
          const {
            'Envelope': EntityMeta(fields: {'items': 'Order'}),
            'Order': EntityMeta(idField: 'id'),
          },
        );

        expect(result.records.keys.toList(), ['Order:7']);
      },
    );

    // `{id: 7}` under no declared type, or under a type the table does not
    // name, is data, not a cache entry.
    test('does not treat an id property as evidence of an entity', () {
      final input = {'id': 7, 'total': 99};
      final result = normalize(input, null, schema);

      expect(result.records, isEmpty);
      expect(result.deps, isEmpty);
      expect(result.skeleton, same(input));

      final unnamed = normalize({'id': 7}, 'NotInTheTable', schema);
      expect(unnamed.records, isEmpty);
    });

    test(
      'leaves a declared type inline when it does not carry its id field',
      () {
        final result = normalize(
          {'invoiceNumber': null, 'amount': 5},
          'Invoice',
          schema,
        );

        expect(result.records, isEmpty);
        expect(result.skeleton, {'invoiceNumber': null, 'amount': 5});

        final identified = normalize(
          {'invoiceNumber': 'INV-1', 'amount': 5},
          'Invoice',
          schema,
        );
        expect(identified.records['Invoice:INV-1'], {
          'invoiceNumber': 'INV-1',
          'amount': 5,
        });
      },
    );

    test('rejects ids that cannot key a record', () {
      // TS also lists `undefined`; Dart has only null.
      for (final id in <Object?>[
        null,
        '',
        <String, Object?>{},
        <Object?>[],
        true,
        double.nan,
      ]) {
        final result = normalize({'id': id, 'total': 1}, 'Order', schema);
        expect(
          result.records,
          isEmpty,
          reason: 'id $id should not key a record',
        );
      }
    });

    test('merges an entity that occurs twice with different field sets', () {
      final result = normalize(
        {
          'id': 7,
          'customer': {'id': 'c-3', 'name': 'Ada'},
          'related': [
            {
              'id': 9,
              'customer': {'id': 'c-3', 'tier': 'gold'},
            },
          ],
        },
        'Order',
        schema,
      );

      expect(result.records['Customer:c-3'], {
        'id': 'c-3',
        'name': 'Ada',
        'tier': 'gold',
      });
    });

    test(
      'returns the same reference target for one entity appearing twice',
      () {
        final customer = {'id': 'c-3', 'name': 'Ada'};
        final result = normalize(
          [
            {'id': 7, 'customer': customer},
            {'id': 8, 'customer': customer},
          ],
          'Order',
          schema,
        );

        expect(result.skeleton, [refTo('Order:7'), refTo('Order:8')]);
      },
    );

    test('leaves subtrees containing no entity referentially untouched', () {
      final meta = {
        'page': {'size': 10, 'cursor': null},
      };
      final input = {
        'data': [
          {'id': 7},
        ],
        'meta': meta,
      };
      final result = normalize(input, 'Envelope', schema);

      expect((result.skeleton! as Map<String, Object?>)['meta'], same(meta));
    });

    test('does not mutate its input', () {
      final input = {
        'id': 7,
        'customer': {'id': 'c-3', 'name': 'Ada'},
        'items': [
          {'sku': 'A'},
        ],
      };
      final before = jsonDecode(jsonEncode(input));

      normalize(input, 'Order', schema);

      expect(input, before);
    });

    // References are recognised by type, not by inspecting `__ref`.
    test('round-trips an object shaped like a reference', () {
      final store = EntityStore();
      final input = {
        'id': 7,
        'note': {'__ref': 'Order:999'},
      };
      final staged = store.write(input, schema, 'Order');

      expect(store.read(staged.skeleton), input);
    });

    test('terminates on a cyclic object graph', () {
      final order = <String, Object?>{'id': 7, 'total': 99};
      final customer = <String, Object?>{'id': 'c-3', 'name': 'Ada'};
      order['customer'] = customer;
      customer['orders'] = [order];

      final result = normalize(order, 'Order', schema);

      expect(result.skeleton, refTo('Order:7'));
      expect(result.records['Order:7'], {
        'id': 7,
        'total': 99,
        'customer': refTo('Customer:c-3'),
      });
      expect(result.records['Customer:c-3'], {
        'id': 'c-3',
        'name': 'Ada',
        'orders': [refTo('Order:7')],
      });
      expect(result.deps.toList()..sort(), ['Customer:c-3', 'Order:7']);
    });

    test('terminates on a cycle that closes through plain objects', () {
      final node = <String, Object?>{
        'label': 'a',
        'order': {'id': 7},
      };
      node['self'] = node;

      final result = normalize({'data': node}, 'Envelope', schema);

      expect(result.records, isEmpty);
      final data =
          (result.skeleton! as Map<String, Object?>)['data']!
              as Map<String, Object?>;
      expect(data['self'], same(data));
    });
  });
}
