// New in Dart: the second adversarial review of the embedded-entity
// optimistic fix. Each case is a reviewer's probe turned into a regression
// test. The models are shaped like generated code: `DateTime` fields that
// re-encode in a different spelling (and lose Go's nanoseconds), optional
// fields the encoder omits when null, and a list of embedded line items.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

final class Customer {
  const Customer({required this.id, required this.name, this.since});

  final String id;
  final String name;
  final DateTime? since;

  static Customer fromClient(Object? client) {
    final json = client! as Json;

    return Customer(
      id: json['id']! as String,
      name: json['name']! as String,
      since: json['since'] == null
          ? null
          : DateTime.parse(json['since']! as String),
    );
  }

  Json toClient() => {
    'id': id,
    'name': name,
    if (since case final value?) 'since': value.toIso8601String(),
  };
}

final class Item {
  const Item({required this.sku, required this.qty, this.at});

  final String sku;
  final int qty;
  final DateTime? at;

  static Item fromClient(Object? client) {
    final json = client! as Json;

    return Item(
      sku: json['sku']! as String,
      qty: json['qty']! as int,
      at: json['at'] == null ? null : DateTime.parse(json['at']! as String),
    );
  }

  Json toClient() => {
    'sku': sku,
    'qty': qty,
    if (at case final value?) 'at': value.toIso8601String(),
  };
}

final class Order {
  const Order({
    required this.id,
    required this.status,
    this.customer,
    this.items = const [],
  });

  final int id;
  final String status;
  final Customer? customer;
  final List<Item> items;

  static Order fromClient(Object? client) {
    final json = client! as Json;

    return Order(
      id: json['id']! as int,
      status: json['status']! as String,
      customer: json['customer'] == null
          ? null
          : decodeCached(Customer.fromClient, json['customer']),
      items: [
        for (final item in (json['items'] as List<Object?>?) ?? const [])
          decodeCached(Item.fromClient, item),
      ],
    );
  }

  Json toClient() => {
    'id': id,
    'status': status,
    if (customer case final value?) 'customer': value.toClient(),
    'items': [for (final item in items) item.toClient()],
  };

  Order copyWith({String? status, Customer? customer, List<Item>? items}) =>
      Order(
        id: id,
        status: status ?? this.status,
        customer: customer ?? this.customer,
        items: items ?? this.items,
      );
}

const opGetOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  rootType: 'Order',
);

const opUpdateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order[]'],
  rootType: 'Order',
);

const opCreateCustomer = OperationMeta(
  id: 'op_create_customer',
  method: 'POST',
  path: '/customers',
  entity: 'Customer',
  invalidates: ['Customer[]'],
  rootType: 'Customer',
);

final class OrderArgs implements OperationArgs {
  const OrderArgs(this.id);

  final int id;

  @override
  TagContext toTagContext() => TagContext(path: {'id': id});

  @override
  bool operator ==(Object other) => other is OrderArgs && other.id == id;

  @override
  int get hashCode => id.hashCode;
}

final updateOrder = mutation<Order, OrderArgs, Order>(
  opUpdateOrder,
  Order.fromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (order) => order.toClient(),
);

const order7 = TagContext(path: {'id': 7});

typedef Rig = ({
  QueryCache cache,
  Completer<Object?> gate,
  List<(Object, String)> reported,
});

/// A cache holding [order] as `Order:7`, with every write waiting on `gate`.
Future<Rig> held(Json order) async {
  final reported = <(Object, String)>[];
  final gate = Completer<Object?>();
  final cache = QueryCache(
    transport: FakeTransport(
      (request, _) => request.meta.method == 'GET' ? order : gate.future,
    ),
    entities: schema,
    scheduler: ManualScheduler(),
    onError: (error, context) => reported.add((error, context)),
  );

  await cache.fetch(opGetOrder, order7);

  return (cache: cache, gate: gate, reported: reported);
}

Json readOrder(QueryCache cache) =>
    cache.getState(opGetOrder, order7).dataOrNull! as Json;

Future<void> settled(Future<Object?> future) =>
    future.then<void>((_) {}, onError: (Object _) {});

void main() {
  group('review R5: a kept previous resolves nothing after its compute', () {
    // Alice's pending update keeps `previous` without reading `customer`.
    // Whatever happens next, reading it later must not show anything that
    // was not already resolved while the compute ran.
    Future<(QueryCache, Json)> kept() async {
      final (:cache, gate: _, reported: _) = await held({
        'id': 7,
        'status': 'open',
        'customer': {'id': 'c1', 'name': 'Ada (alice)'},
      });

      cache.setPrincipal('alice');
      await cache.fetch(opGetOrder, order7);

      Json? captured;

      unawaited(
        settled(
          cache.mutate(
            opUpdateOrder,
            order7,
            options: MutateOptions(
              optimistic: OptimisticUpdate<Object?>((value) {
                captured = value! as Json;

                return {'status': 'x'};
              }, key: 'Order:7'),
            ),
          ),
        ),
      );

      cache.overlays.effective('Order:7');

      return (cache, captured!);
    }

    test('across setPrincipal', () async {
      final (cache, captured) = await kept();

      cache.setPrincipal('bob');
      cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Ada (bob)'});

      expect(captured['customer'], isNull);
    });

    test('across clear()', () async {
      final (cache, captured) = await kept();

      cache.clear();
      cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Ada (bob)'});

      expect(captured['customer'], isNull);
    });

    test('across a store write', () async {
      final (cache, captured) = await kept();

      cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Renamed later'});

      expect(captured['customer'], isNull);
    });

    test('keeps what the compute did read', () async {
      final (:cache, gate: _, reported: _) = await held({
        'id': 7,
        'status': 'open',
        'customer': {'id': 'c1', 'name': 'Ada'},
      });
      Json? captured;

      unawaited(
        settled(
          cache.mutate(
            opUpdateOrder,
            order7,
            options: MutateOptions(
              optimistic: OptimisticUpdate<Object?>((value) {
                captured = value! as Json;

                return {
                  'status': 'for ${(captured!['customer']! as Json)['name']}',
                };
              }, key: 'Order:7'),
            ),
          ),
        ),
      );

      expect(cache.overlays.effective('Order:7')!.data['status'], 'for Ada');

      cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Grace'});

      final customer = captured!['customer']! as Json;
      expect(customer['name'], 'Ada');
      // Its own fields were read; anything it reaches beyond them was not.
      expect(customer['id'], 'c1');
    });
  });

  group('review R2: nothing minted reaches base', () {
    test(
      'a failed create leaves no ~opt record and no reference to one',
      () async {
        final reported = <(Object, String)>[];
        final orderGate = Completer<Object?>();
        final createGate = Completer<Object?>();
        final cache = QueryCache(
          transport: FakeTransport((request, _) {
            if (request.meta.method == 'GET') {
              return {
                'id': 7,
                'status': 'open',
                'customer': {'id': 'c1', 'name': 'Ada'},
              };
            }

            if (request.meta.method == 'POST') return createGate.future;

            return orderGate.future;
          }),
          entities: schema,
          scheduler: ManualScheduler(),
          onError: (error, context) => reported.add((error, context)),
        );

        await cache.fetch(opGetOrder, order7);

        final create = cache.mutate(
          opCreateCustomer,
          TagContext.empty,
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'name': 'New'}),
          ),
        );
        final minted = cache.overlays.list().single.created!;
        final mintedId = minted.split(':').last;

        final write = updateOrder(
          cache,
          const OrderArgs(7),
          optimistic: OptimisticUpdate(
            (order) => order.copyWith(
              status: 'assigned',
              customer: Customer(id: mintedId, name: 'New'),
            ),
            key: 'Order:7',
          ),
        );

        // While pending, the order shows the customer being created.
        expect((readOrder(cache)['customer']! as Json)['name'], 'New');

        orderGate.complete(null);
        await settled(write);

        createGate.completeError(StateError('create failed'));
        await settled(create);

        expect(cache.overlays.empty, isTrue);
        expect(cache.store.has(minted), isFalse);
        expect(cache.store.keys.where((key) => key.contains('~opt')), isEmpty);

        final base = cache.store.getRecord('Order:7')!.data;
        // The rest of the change is promoted; the field naming a minted
        // record is left for the response to answer.
        expect(base['status'], 'assigned');
        expect(base['customer'], refTo('Customer:c1'));
        expect(reported, isEmpty);
      },
    );
  });

  group('review R3 and R4: only what the caller changed is written', () {
    test('a Go nanosecond timestamp on an untouched field reaches base byte for byte', () async {
      final (:cache, :gate, :reported) = await held({
        'id': 7,
        'status': 'open',
        'customer': {
          'id': 'c1',
          'name': 'Ada',
          'since': '2026-10-07T18:12:36.123456789Z',
        },
      });

      final write = updateOrder(
        cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            customer: Customer(
              id: 'c1',
              name: 'Ada L.',
              since: order.customer!.since,
            ),
          ),
          key: 'Order:7',
        ),
      );

      gate.complete(null);
      await settled(write);

      final customer = cache.store.getRecord('Customer:c1')!.data;
      expect(customer['name'], 'Ada L.');
      expect(customer['since'], '2026-10-07T18:12:36.123456789Z');
      expect(reported, isEmpty);
    });

    test('an edit to one line item keeps its siblings as references', () async {
      final (:cache, :gate, :reported) = await held({
        'id': 7,
        'status': 'open',
        'items': [
          {'sku': 'a', 'qty': 1, 'at': '2026-01-01T00:00:00Z'},
          {'sku': 'b', 'qty': 1, 'at': '2026-01-01T00:00:00Z'},
        ],
      });

      final write = updateOrder(
        cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            items: [
              Item(sku: 'a', qty: 5, at: order.items[0].at),
              order.items[1],
            ],
          ),
          key: 'Order:7',
        ),
      );

      final items =
          cache.overlays.effective('Order:7')!.data['items']! as List<Object?>;

      expect(items[1], refTo('LineItem:b'));
      expect(items[0], {'sku': 'a', 'qty': 5, 'at': '2026-01-01T00:00:00Z'});

      final shown = readOrder(cache)['items']! as List<Object?>;
      expect([for (final item in shown) (item! as Json)['qty']], [5, 1]);

      gate.complete(null);
      await settled(write);

      expect(cache.store.getRecord('Order:7')!.data['items'], [
        refTo('LineItem:a'),
        refTo('LineItem:b'),
      ]);
      expect(cache.store.getRecord('LineItem:a')!.data, {
        'sku': 'a',
        'qty': 5,
        'at': '2026-01-01T00:00:00Z',
      });
      // The untouched sibling is not rewritten at all.
      expect(cache.store.getRecord('LineItem:b')!.version, 1);
      expect(
        cache.store.getRecord('LineItem:b')!.data['at'],
        '2026-01-01T00:00:00Z',
      );
      expect(reported, isEmpty);
    });

    test('clearing an optional field the encoder omits writes null', () async {
      final (:cache, :gate, :reported) = await held({
        'id': 7,
        'status': 'open',
        'customer': {
          'id': 'c1',
          'name': 'Ada',
          'since': '2026-01-01T00:00:00Z',
        },
      });

      final write = updateOrder(
        cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            customer: const Customer(id: 'c1', name: 'Ada'),
          ),
          key: 'Order:7',
        ),
      );

      expect((readOrder(cache)['customer']! as Json)['since'], isNull);

      gate.complete(null);
      await settled(write);

      expect(cache.store.getRecord('Customer:c1')!.data['since'], isNull);
      expect(reported, isEmpty);
    });
  });

  group('review round 2: checked and fine, pinned', () {
    test(
      'R1: refolds when a customer the compute read but replaced changes',
      () async {
        final (:cache, gate: _, reported: _) = await held({
          'id': 7,
          'status': 'open',
          'customer': {'id': 'c1', 'name': 'Ada'},
        });

        cache.store.put('Customer:c2', {'id': 'c2', 'name': 'Bob'});
        cache.subscribe(opGetOrder, order7, () {});

        unawaited(
          settled(
            updateOrder(
              cache,
              const OrderArgs(7),
              optimistic: OptimisticUpdate(
                (order) => order.copyWith(
                  status: 'was ${order.customer!.name}',
                  customer: const Customer(id: 'c2', name: 'Bob'),
                ),
                key: 'Order:7',
              ),
            ),
          ),
        );

        expect(readOrder(cache)['status'], 'was Ada');

        cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Grace'});
        cache.notifyChanged();

        expect(readOrder(cache)['status'], 'was Grace');
      },
    );

    test(
      'R1c: refolds when a record the compute found missing arrives',
      () async {
        final (:cache, gate: _, reported: _) = await held({
          'id': 7,
          'status': 'open',
          'customer': {'id': 'c1', 'name': 'Ada'},
        });

        cache.store.evict('Customer:c1');

        unawaited(
          settled(
            cache.mutate(
              opUpdateOrder,
              order7,
              options: MutateOptions(
                optimistic: OptimisticUpdate<Object?>((value) {
                  final previous = value! as Json;

                  return {
                    ...previous,
                    'status': previous['customer'] == null
                        ? 'nobody'
                        : 'someone',
                  };
                }, key: 'Order:7'),
              ),
            ),
          ),
        );

        expect(cache.overlays.effective('Order:7')!.data['status'], 'nobody');

        cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Ada'});

        expect(cache.overlays.effective('Order:7')!.data['status'], 'someone');
      },
    );

    test('R1e: refolds on a read made through a nested fold', () async {
      final (:cache, gate: _, reported: _) = await held({
        'id': 7,
        'status': 'open',
        'customer': {'id': 'c1', 'name': 'Ada'},
      });

      cache.store.put('Customer:c1', {
        'orders': markRewritten([makeRef('Order:8')]),
      });
      cache.store.put('Order:8', {'id': 8, 'status': 'eight'});
      cache.overlays.add({
        'Customer:c1': MergePatch.computed((previous) {
          final orders = previous['orders']! as List<Object?>;

          return {
            ...previous,
            'name': 'sees ${(orders.single! as Json)['status']}',
          };
        }),
      });
      cache.overlays.add({
        'Order:7': MergePatch.computed((previous) {
          final customer = previous['customer']! as Json;

          return {...previous, 'status': 'for ${customer['name']}'};
        }),
      });

      expect(
        cache.overlays.effective('Order:7')!.data['status'],
        'for sees eight',
      );

      cache.store.put('Order:8', {'status': 'EIGHT'});

      expect(
        cache.overlays.effective('Order:7')!.data['status'],
        'for sees EIGHT',
      );
    });

    test(
      'R7: a denormalized snapshot of a cyclic graph throws on its own',
      () async {
        final (:cache, gate: _, reported: _) = await held({
          'id': 7,
          'status': 'open',
          'customer': {'id': 'c1', 'name': 'Ada'},
        });

        cache.store.put('Customer:c1', {
          'orders': markRewritten([makeRef('Order:7')]),
        });

        expect(
          () => dehydrate(
            cache,
            principal: cache.principal,
            mode: SnapshotMode.denormalized,
          ).encode(),
          throwsStateError,
        );
      },
    );
  });
}
