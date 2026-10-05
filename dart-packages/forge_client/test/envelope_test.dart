// Ported from packages/client-core/__tests__/envelope.test.ts.
//
// Both tables are the ones internal/client/generators/typescript/
// e2e_envelope_test.go generates from a real OpenAPI document, spelled as the
// Dart generator (plan 02) emits them.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

const EntitySchema entities = {
  'Carrier': EntityMeta(idField: 'id'),
  'Customer': EntityMeta(idField: 'id', fields: {'orders': 'Order'}),
  'Order': EntityMeta(
    idField: 'id',
    fields: {'customer': 'Customer', 'parent': 'Order', 'shipment': 'Shipment'},
  ),
  'OrderReport': EntityMeta(fields: {'topOrders': 'Order'}),
  'PageOrder': EntityMeta(fields: {'items': 'Order'}),
  'Shipment': EntityMeta(fields: {'carrier': 'Carrier'}),
};

const ordersList = OperationMeta(
  id: 'orders.list',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  rootType: 'PageOrder',
  provides: ['Order:{id}', 'Order[]'],
);

const reportsOrders = OperationMeta(
  id: 'reports.orders',
  method: 'GET',
  path: '/reports/orders',
  rootType: 'OrderReport',
);

const page = {
  'items': [
    {
      'id': 'o-1',
      'customer': {'id': 'c-3', 'name': 'Ada'},
      'shipment': {
        'carrier': {'id': 'dhl', 'name': 'DHL'},
        'weightKg': 2,
      },
    },
    {
      'id': 'o-2',
      'customer': {'id': 'c-3', 'name': 'Ada'},
      'shipment': null,
    },
  ],
  'total': 2,
  'nextCursor': 'abc',
};

const none = TagContext.empty;

Map<String, Object?> asMap(Object? value) => value! as Map<String, Object?>;

String customerNameOfFirstItem(Object? pageValue) =>
    asMap(
          asMap((asMap(pageValue)['items']! as List<Object?>)[0])['customer'],
        )['name']!
        as String;

void main() {
  group('enveloped list responses', () {
    test('normalizes the entities inside a page', () {
      final result = normalize(page, ordersList.rootType, entities);

      expect(result.skeleton, {
        'items': [refTo('Order:o-1'), refTo('Order:o-2')],
        'total': 2,
        'nextCursor': 'abc',
      });
      expect(result.records.containsKey('PageOrder:undefined'), isFalse);

      expect(result.records.keys.toList()..sort(), [
        'Carrier:dhl',
        'Customer:c-3',
        'Order:o-1',
        'Order:o-2',
      ]);
      expect(result.deps, hasLength(4));
    });

    test('reaches an entity through a non-entity hop', () {
      final result = normalize(page, 'PageOrder', entities);

      expect(result.records['Order:o-1']?['shipment'], {
        'carrier': refTo('Carrier:dhl'),
        'weightKg': 2,
      });
      expect(result.records['Carrier:dhl'], {'id': 'dhl', 'name': 'DHL'});
    });

    test(
      'normalizes nothing when handed the entity name instead of the root type',
      () {
        final result = normalize(page, ordersList.entity, entities);

        expect(result.records, isEmpty);
      },
    );

    test('shares records between a page and a single read', () {
      final store = EntityStore();

      store.write(page, entities, ordersList.rootType);
      store.write(
        {
          'id': 'o-1',
          'customer': {'id': 'c-3', 'name': 'Ada Lovelace'},
        },
        entities,
        'Order',
      );

      expect(
        store.getRecord('Customer:c-3')?.data,
        containsPair('name', 'Ada Lovelace'),
      );
      expect(store.has('Order:o-2'), isTrue);
    });

    test('normalizes an undeclared wrapper while providing no tags', () {
      expect(reportsOrders.provides, isEmpty);

      final result = normalize(
        {
          'topOrders': [
            {
              'id': 'o-1',
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
          'generatedAt': 'now',
        },
        reportsOrders.rootType,
        entities,
      );

      expect(result.records.keys.toList()..sort(), [
        'Customer:c-3',
        'Order:o-1',
      ]);
    });
  });

  group('enveloped responses through the query cache', () {
    const orderPageList = ordersList;
    const orderGet = OperationMeta(
      id: 'orderGet',
      method: 'GET',
      path: '/orders/{id}',
      entity: 'Order',
      rootType: 'Order',
      provides: ['Order:{id}'],
    );
    const orderCreate = OperationMeta(
      id: 'orderCreate',
      method: 'POST',
      path: '/orders',
      entity: 'Order',
      rootType: 'Order',
    );

    QueryCache rig(
      FutureOr<Object?> Function(TransportRequest request, int call) handler,
    ) => QueryCache(
      transport: FakeTransport(handler),
      entities: entities,
      scheduler: ManualScheduler(),
    );

    test(
      'shares a record between a paginated list and a single read',
      () async {
        final cache = rig(
          (request, _) => request.meta.path == '/orders'
              ? {
                  'items': [
                    {
                      'id': 'o-1',
                      'customer': {'id': 'c-3', 'name': 'Ada'},
                    },
                  ],
                  'total': 1,
                }
              : {
                  'id': 'o-1',
                  'customer': {'id': 'c-3', 'name': 'Ada Lovelace'},
                },
        );

        final first = await cache.fetch(orderPageList, none);
        expect(customerNameOfFirstItem(first), 'Ada');

        await cache.fetch(orderGet, const TagContext(path: {'id': 'o-1'}));

        expect(
          customerNameOfFirstItem(
            cache.getState(orderPageList, none).dataOrNull,
          ),
          'Ada Lovelace',
        );
      },
    );

    test('keeps the envelope around the records it lifted out', () async {
      final cache = rig(
        (_, _) => {
          'items': [
            {'id': 'o-1'},
            {'id': 'o-2'},
          ],
          'total': 2,
          'nextCursor': 'abc',
        },
      );

      expect(await cache.fetch(orderPageList, none), {
        'items': [
          {'id': 'o-1'},
          {'id': 'o-2'},
        ],
        'total': 2,
        'nextCursor': 'abc',
      });
    });

    test('normalizes a mutation response into the shared store', () async {
      final cache = rig(
        (request, _) => request.meta.method == 'POST'
            ? {
                'id': 'o-9',
                'customer': {'id': 'c-3', 'name': 'Grace'},
              }
            : {
                'items': [
                  {
                    'id': 'o-9',
                    'customer': {'id': 'c-3', 'name': 'Ada'},
                  },
                ],
                'total': 1,
              },
      );

      await cache.fetch(orderPageList, none);
      await cache.mutate(orderCreate, none);

      expect(
        customerNameOfFirstItem(cache.getState(orderPageList, none).dataOrNull),
        'Grace',
      );
    });

    test('records the entities inside a page as deps and tags', () async {
      final cache = rig(
        (_, _) => {
          'items': [
            {
              'id': 'o-1',
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
            {
              'id': 'o-2',
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
          'total': 2,
        },
      );

      await cache.fetch(orderPageList, none);

      final entry = cache.registry.get(cache.key(orderPageList, none));

      expect(entry?.deps, {'Order:o-1', 'Order:o-2', 'Customer:c-3'});
      expect(entry?.tags.contains('Order:o-1'), isTrue);
      expect(entry?.tags.contains('Order:o-2'), isTrue);
    });

    test(
      'falls back to the entity when a manifest carries no root type',
      () async {
        const legacy = OperationMeta(
          id: 'legacy',
          method: 'GET',
          path: '/orders',
          entity: 'Order',
          provides: ['Order[]'],
        );

        final cache = rig(
          (request, _) => request.meta.path == '/orders'
              ? [
                  {
                    'id': 'o-1',
                    'customer': {'id': 'c-3', 'name': 'Ada'},
                  },
                ]
              : {
                  'id': 'o-1',
                  'customer': {'id': 'c-3', 'name': 'Ada Lovelace'},
                },
        );

        await cache.fetch(legacy, none);
        await cache.fetch(orderGet, const TagContext(path: {'id': 'o-1'}));

        final rows = cache.getState(legacy, none).dataOrNull! as List<Object?>;
        expect(asMap(asMap(rows[0])['customer'])['name'], 'Ada Lovelace');
      },
    );
  });
}
