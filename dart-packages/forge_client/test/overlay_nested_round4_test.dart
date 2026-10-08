// New in Dart: the third adversarial review of the embedded-entity
// optimistic fix. Each case is a reviewer's probe (P1 to P7) turned into a
// regression test, plus the cases around them. The models are shaped like
// generated code: `DateTime` fields that re-encode in a different spelling
// and lose Go's nanoseconds, optional fields the encoder omits when null, a
// list of embedded line items, a scalar foreign key next to its entity, and
// a list of plain timestamps with no identity.
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

  Customer copyWith({String? name}) =>
      Customer(id: id, name: name ?? this.name, since: since);
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

  Item copyWith({int? qty}) => Item(sku: sku, qty: qty ?? this.qty, at: at);
}

final class Order {
  const Order({
    required this.id,
    required this.status,
    this.customer,
    this.customerId,
    this.items = const [],
    this.stamps = const [],
  });

  final int id;
  final String status;
  final Customer? customer;
  final String? customerId;
  final List<Item> items;

  /// Plain values with no identity: the schema does not declare the field.
  final List<DateTime> stamps;

  static Order fromClient(Object? client) {
    final json = client! as Json;

    return Order(
      id: json['id']! as int,
      status: json['status']! as String,
      customer: json['customer'] == null
          ? null
          : decodeCached(Customer.fromClient, json['customer']),
      customerId: json['customerId'] as String?,
      items: [
        for (final item in (json['items'] as List<Object?>?) ?? const [])
          decodeCached(Item.fromClient, item),
      ],
      stamps: [
        for (final stamp in (json['stamps'] as List<Object?>?) ?? const [])
          DateTime.parse(stamp! as String),
      ],
    );
  }

  Json toClient() => {
    'id': id,
    'status': status,
    if (customer case final value?) 'customer': value.toClient(),
    'customerId': ?customerId,
    'items': [for (final item in items) item.toClient()],
    if (stamps.isNotEmpty)
      'stamps': [for (final stamp in stamps) stamp.toIso8601String()],
  };

  Order copyWith({
    String? status,
    Customer? customer,
    String? customerId,
    List<Item>? items,
    List<DateTime>? stamps,
  }) => Order(
    id: id,
    status: status ?? this.status,
    customer: customer ?? this.customer,
    customerId: customerId ?? this.customerId,
    items: items ?? this.items,
    stamps: stamps ?? this.stamps,
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

const opCreateItem = OperationMeta(
  id: 'op_create_item',
  method: 'POST',
  path: '/items',
  entity: 'LineItem',
  invalidates: ['LineItem[]'],
  rootType: 'LineItem',
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

/// Two timestamps in the same microsecond, as Go spells them. A Dart
/// `DateTime` encodes both as `...36.123456Z`.
const nanosA = '2026-10-07T18:12:36.123456789Z';
const nanosB = '2026-10-07T18:12:36.123456111Z';

typedef Rig = ({
  QueryCache cache,
  Completer<Object?> gate,
  List<(Object, String)> reported,
});

/// A cache holding [order] as `Order:7`, every POST waiting on `create` and
/// every other write on `gate`.
Future<Rig> held(Json order, {Completer<Object?>? create}) async {
  final reported = <(Object, String)>[];
  final gate = Completer<Object?>();
  final cache = QueryCache(
    transport: FakeTransport((request, _) {
      if (request.meta.method == 'GET') return order;
      if (request.meta.method == 'POST' && create != null) {
        return create.future;
      }

      return gate.future;
    }),
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

Json twoItems() => {
  'id': 7,
  'status': 'open',
  'items': [
    {'sku': 'a', 'qty': 1, 'at': nanosA},
    {'sku': 'b', 'qty': 1, 'at': nanosB},
  ],
};

/// [write] with `key: 'Order:7'`, settled with a 204.
Future<void> promoted(Rig rig, Order Function(Order order) write) async {
  final pending = updateOrder(
    rig.cache,
    const OrderArgs(7),
    optimistic: OptimisticUpdate(write, key: 'Order:7'),
  );

  rig.gate.complete(null);
  await settled(pending);
}

EntityRecord record(QueryCache cache, String key) =>
    cache.store.getRecord(key)!;

void main() {
  group('I-R3a: untouched bytes come from the same entity only', () {
    test('P2: appending a line item keeps its siblings exact', () async {
      final rig = await held(twoItems());

      final pending = updateOrder(
        rig.cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            items: [
              ...order.items,
              const Item(sku: 'c', qty: 1),
            ],
          ),
          key: 'Order:7',
        ),
      );

      // While pending the siblings still track their records.
      final items =
          rig.cache.overlays.effective('Order:7')!.data['items']!
              as List<Object?>;
      expect(items[0], refTo('LineItem:a'));
      expect(items[1], refTo('LineItem:b'));
      expect(items[2], {'sku': 'c', 'qty': 1});

      rig.gate.complete(null);
      await settled(pending);

      expect(record(rig.cache, 'LineItem:a').data['at'], nanosA);
      expect(record(rig.cache, 'LineItem:a').version, 1);
      expect(record(rig.cache, 'LineItem:b').data['at'], nanosB);
      expect(record(rig.cache, 'LineItem:b').version, 1);
      expect(record(rig.cache, 'LineItem:c').data, {'sku': 'c', 'qty': 1});
      expect(record(rig.cache, 'Order:7').data['items'], [
        refTo('LineItem:a'),
        refTo('LineItem:b'),
        refTo('LineItem:c'),
      ]);
      expect(rig.reported, isEmpty);
    });

    test('P3: removing a line item keeps the rest exact', () async {
      final rig = await held(twoItems());

      await promoted(rig, (order) => order.copyWith(items: [order.items[1]]));

      expect(record(rig.cache, 'LineItem:b').data['at'], nanosB);
      expect(record(rig.cache, 'LineItem:b').version, 1);
      expect(record(rig.cache, 'Order:7').data['items'], [refTo('LineItem:b')]);
      expect(rig.reported, isEmpty);
    });

    test('P4: reordering never swaps raw fields between entities', () async {
      final rig = await held(twoItems());

      final pending = updateOrder(
        rig.cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(items: [order.items[1], order.items[0]]),
          key: 'Order:7',
        ),
      );

      expect(rig.cache.overlays.effective('Order:7')!.data['items'], [
        refTo('LineItem:b'),
        refTo('LineItem:a'),
      ]);

      rig.gate.complete(null);
      await settled(pending);

      expect(record(rig.cache, 'LineItem:a').data['at'], nanosA);
      expect(record(rig.cache, 'LineItem:b').data['at'], nanosB);
      expect(record(rig.cache, 'LineItem:a').version, 1);
      expect(record(rig.cache, 'LineItem:b').version, 1);
      expect(record(rig.cache, 'Order:7').data['items'], [
        refTo('LineItem:b'),
        refTo('LineItem:a'),
      ]);
    });

    test(
      'an edit plus an append keeps the edited item exact elsewhere',
      () async {
        final rig = await held(twoItems());

        await promoted(
          rig,
          (order) => order.copyWith(
            items: [
              order.items[0].copyWith(qty: 5),
              order.items[1],
              const Item(sku: 'c', qty: 2),
            ],
          ),
        );

        expect(record(rig.cache, 'LineItem:a').data, {
          'sku': 'a',
          'qty': 5,
          'at': nanosA,
        });
        expect(record(rig.cache, 'LineItem:b').version, 1);
        expect(record(rig.cache, 'LineItem:c').data, {'sku': 'c', 'qty': 2});
      },
    );

    test('a reorder with an edit pairs the edit with its own item', () async {
      final rig = await held(twoItems());

      await promoted(
        rig,
        (order) => order.copyWith(
          items: [order.items[1], order.items[0].copyWith(qty: 9)],
        ),
      );

      expect(record(rig.cache, 'LineItem:a').data, {
        'sku': 'a',
        'qty': 9,
        'at': nanosA,
      });
      expect(record(rig.cache, 'LineItem:b').data['at'], nanosB);
      expect(record(rig.cache, 'LineItem:b').version, 1);
      expect(record(rig.cache, 'Order:7').data['items'], [
        refTo('LineItem:b'),
        refTo('LineItem:a'),
      ]);
    });

    test('an entity moved in from elsewhere keeps its own bytes', () async {
      final rig = await held(twoItems());

      rig.cache.store.put('LineItem:z', {'sku': 'z', 'qty': 3, 'at': nanosB});

      final pending = updateOrder(
        rig.cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            items: [
              ...order.items,
              Item(sku: 'z', qty: 3, at: DateTime.parse(nanosB)),
            ],
          ),
          key: 'Order:7',
        ),
      );

      // Unchanged from what base holds, so it tracks the record at once.
      final items =
          rig.cache.overlays.effective('Order:7')!.data['items']!
              as List<Object?>;
      expect(items[2], refTo('LineItem:z'));

      rig.gate.complete(null);
      await settled(pending);

      expect(record(rig.cache, 'LineItem:z').data['at'], nanosB);
      expect(record(rig.cache, 'LineItem:z').version, 1);
    });

    test('P7: a swapped customer never takes the old one\'s bytes', () async {
      final rig = await held({
        'id': 7,
        'status': 'open',
        'customer': {'id': 'c1', 'name': 'Ada', 'since': nanosA},
      });

      rig.cache.store.put('Customer:c3', {
        'id': 'c3',
        'name': 'Hedy',
        'since': nanosB,
      });

      await promoted(
        rig,
        (order) => order.copyWith(
          customer: Customer(
            id: 'c3',
            name: 'Hedy',
            since: DateTime.parse(nanosB),
          ),
        ),
      );

      expect(record(rig.cache, 'Customer:c3').data['since'], nanosB);
      expect(record(rig.cache, 'Customer:c3').version, 1);
      expect(record(rig.cache, 'Customer:c1').data['since'], nanosA);
      expect(
        record(rig.cache, 'Order:7').data['customer'],
        refTo('Customer:c3'),
      );
      expect(rig.reported, isEmpty);
    });

    test('a swapped and edited customer writes only the edit', () async {
      final rig = await held({
        'id': 7,
        'status': 'open',
        'customer': {'id': 'c1', 'name': 'Ada', 'since': nanosA},
      });

      rig.cache.store.put('Customer:c3', {
        'id': 'c3',
        'name': 'Hedy',
        'since': nanosB,
      });

      await promoted(
        rig,
        (order) => order.copyWith(
          customer: Customer(
            id: 'c3',
            name: 'Hedy L.',
            since: DateTime.parse(nanosB),
          ),
        ),
      );

      expect(record(rig.cache, 'Customer:c3').data, {
        'id': 'c3',
        'name': 'Hedy L.',
        'since': nanosB,
      });
      expect(record(rig.cache, 'Customer:c1').data['name'], 'Ada');
    });

    test('a list with no identity pairs only equal values, in place', () async {
      final rig = await held({
        'id': 7,
        'status': 'open',
        'stamps': [nanosA, '2026-01-01T00:00:00Z'],
      });

      await promoted(
        rig,
        (order) =>
            order.copyWith(stamps: [...order.stamps, DateTime.utc(2026, 2)]),
      );

      expect(record(rig.cache, 'Order:7').data['stamps'], [
        nanosA,
        '2026-01-01T00:00:00Z',
        '2026-02-01T00:00:00.000Z',
      ]);
    });
  });

  group('I-R3b: a kept previous returned later writes what it held', () {
    test('P1: an untyped undo keeps the references it never read', () async {
      final rig = await held({
        'id': 7,
        'status': 'open',
        'customer': {'id': 'c1', 'name': 'Ada'},
      });

      rig.cache.store.put('Customer:c1', {
        'orders': markRewritten([makeRef('Order:7')]),
      });

      Json? saved;
      final first = rig.cache.mutate(
        opUpdateOrder,
        order7,
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>((value) {
            saved = value! as Json;

            return {...saved!, 'status': 'shipped'};
          }, key: 'Order:7'),
        ),
      );

      rig.gate.complete(null);
      await first;

      final undo = rig.cache.mutate(
        opUpdateOrder,
        order7,
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>((_) => saved, key: 'Order:7'),
        ),
      );

      final folded = rig.cache.overlays.effective('Order:7')!.data;
      expect(folded['status'], 'open');
      expect(folded['customer'], refTo('Customer:c1'));

      final shown = readOrder(rig.cache)['customer']! as Json;
      expect(shown['orders'], isA<List<Object?>>());

      await undo;

      final customer = record(rig.cache, 'Customer:c1').data;
      expect(customer['orders'], [refTo('Order:7')]);
      expect(customer['name'], 'Ada');
      expect(record(rig.cache, 'Order:7').data['status'], 'open');
      expect(
        record(rig.cache, 'Order:7').data['customer'],
        refTo('Customer:c1'),
      );
      expect(rig.reported, isEmpty);

      // The seal still holds: the kept view resolves nothing new.
      expect((saved!['customer']! as Json)['orders'], isNull);
    });

    test('a typed undo writes nothing over the entities it kept', () async {
      final rig = await held({
        ...twoItems(),
        'customer': {'id': 'c1', 'name': 'Ada', 'since': nanosA},
      });

      rig.cache.store.put('Customer:c1', {
        'orders': markRewritten([makeRef('Order:7')]),
      });

      Order? saved;
      final first = updateOrder(
        rig.cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate((order) {
          saved = order;

          return order.copyWith(status: 'shipped');
        }, key: 'Order:7'),
      );

      rig.gate.complete(null);
      await settled(first);

      final undo = updateOrder(
        rig.cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate((_) => saved!, key: 'Order:7'),
      );

      await settled(undo);

      final order = record(rig.cache, 'Order:7').data;
      expect(order['status'], 'open');
      expect(order['customer'], refTo('Customer:c1'));
      expect(order['items'], [refTo('LineItem:a'), refTo('LineItem:b')]);
      expect(record(rig.cache, 'Customer:c1').data['orders'], [
        refTo('Order:7'),
      ]);
      expect(record(rig.cache, 'Customer:c1').data['since'], nanosA);
      expect(record(rig.cache, 'LineItem:a').version, 1);
      expect(rig.reported, isEmpty);
    });

    test(
      'an undo of a previous that read nothing keeps the reference',
      () async {
        final rig = await held({
          'id': 7,
          'status': 'open',
          'customer': {'id': 'c1', 'name': 'Ada'},
        });

        Json? saved;
        final first = rig.cache.mutate(
          opUpdateOrder,
          order7,
          options: MutateOptions(
            optimistic: OptimisticUpdate<Object?>((value) {
              saved = value! as Json;

              return {'status': 'shipped'};
            }, key: 'Order:7'),
          ),
        );

        rig.gate.complete(null);
        await first;

        final undo = rig.cache.mutate(
          opUpdateOrder,
          order7,
          options: MutateOptions(
            optimistic: OptimisticUpdate<Object?>((_) => saved, key: 'Order:7'),
          ),
        );

        expect(
          rig.cache.overlays.effective('Order:7')!.data['customer'],
          refTo('Customer:c1'),
        );
        expect((readOrder(rig.cache)['customer']! as Json)['name'], 'Ada');

        await undo;

        final order = record(rig.cache, 'Order:7').data;
        expect(order['status'], 'open');
        expect(order['customer'], refTo('Customer:c1'));
        expect(saved!['customer'], isNull);
      },
    );
  });

  group('M-R3c and M-R3d: a minted key never reaches base', () {
    test('P5: a scalar foreign key to a minted id is left alone', () async {
      final create = Completer<Object?>();
      final rig = await held({
        'id': 7,
        'status': 'open',
        'customerId': 'c1',
        'customer': {'id': 'c1', 'name': 'Ada'},
      }, create: create);

      final creating = rig.cache.mutate(
        opCreateCustomer,
        TagContext.empty,
        options: const MutateOptions(
          optimistic: OptimisticCreate<Object?>({'name': 'New'}),
        ),
      );
      final mintedId = rig.cache.overlays
          .list()
          .single
          .created!
          .split(':')
          .last;

      await promoted(
        rig,
        (order) => order.copyWith(
          status: 'assigned',
          customer: Customer(id: mintedId, name: 'New'),
          customerId: mintedId,
        ),
      );

      create.completeError(StateError('create failed'));
      await settled(creating);

      final base = record(rig.cache, 'Order:7').data;
      expect(base['status'], 'assigned');
      expect(base['customerId'], 'c1');
      expect(base['customer'], refTo('Customer:c1'));
      expect(
        rig.cache.store.keys.where((key) => key.contains('~opt')),
        isEmpty,
      );
    });

    test('a minted element in a list is dropped, the rest promoted', () async {
      final create = Completer<Object?>();
      final rig = await held(twoItems(), create: create);

      final creating = rig.cache.mutate(
        opCreateItem,
        TagContext.empty,
        options: const MutateOptions(
          optimistic: OptimisticCreate<Object?>({'qty': 1}),
        ),
      );
      final mintedSku = rig.cache.overlays
          .list()
          .single
          .created!
          .split(':')
          .last;

      final pending = updateOrder(
        rig.cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            items: [
              ...order.items,
              const Item(sku: 'c', qty: 1),
              Item(sku: mintedSku, qty: 1),
            ],
          ),
          key: 'Order:7',
        ),
      );

      // Shown in full while pending.
      expect(readOrder(rig.cache)['items'], hasLength(4));

      rig.gate.complete(null);
      await settled(pending);
      create.completeError(StateError('create failed'));
      await settled(creating);

      expect(record(rig.cache, 'Order:7').data['items'], [
        refTo('LineItem:a'),
        refTo('LineItem:b'),
        refTo('LineItem:c'),
      ]);
      expect(
        rig.cache.store.keys.where((key) => key.contains('~opt')),
        isEmpty,
      );
    });

    test('a minted id in a list of plain values is dropped too', () async {
      final create = Completer<Object?>();
      final rig = await held({
        'id': 7,
        'status': 'open',
        'tagIds': ['t0'],
      }, create: create);

      final creating = rig.cache.mutate(
        opCreateCustomer,
        TagContext.empty,
        options: const MutateOptions(
          optimistic: OptimisticCreate<Object?>({'name': 'New'}),
        ),
      );
      final mintedId = rig.cache.overlays
          .list()
          .single
          .created!
          .split(':')
          .last;

      final pending = rig.cache.mutate(
        opUpdateOrder,
        order7,
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>(
            (value) => {
              ...value! as Json,
              'tagIds': ['t0', 't1', mintedId],
            },
            key: 'Order:7',
          ),
        ),
      );

      rig.gate.complete(null);
      await settled(pending);
      create.completeError(StateError('create failed'));
      await settled(creating);

      expect(record(rig.cache, 'Order:7').data['tagIds'], ['t0', 't1']);
    });
  });
}
