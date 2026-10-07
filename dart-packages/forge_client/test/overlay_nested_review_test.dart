// New in Dart: the adversarial review of the embedded-entity optimistic fix.
//
// Each case here is a reviewer's probe turned into a regression test. The
// models are shaped like generated code on purpose: a `DateTime` that
// re-encodes in a different spelling, and an optional field the encoder omits
// when it is null. overlay_nested_test.dart covers the lossless happy path.
import 'dart:async';
import 'dart:convert';

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

  // As the generator emits an optional field: omitted when null.
  Json toClient() => {
    'id': id,
    'name': name,
    if (since case final value?) 'since': value.toIso8601String(),
  };
}

final class Order {
  const Order({required this.id, required this.status, required this.customer});

  final int id;
  final String status;
  final Customer customer;

  static Order fromClient(Object? client) {
    final json = client! as Json;

    return Order(
      id: json['id']! as int,
      status: json['status']! as String,
      customer: decodeCached(Customer.fromClient, json['customer']),
    );
  }

  Json toClient() => {
    'id': id,
    'status': status,
    'customer': customer.toClient(),
  };

  Order copyWith({String? status, Customer? customer}) => Order(
    id: id,
    status: status ?? this.status,
    customer: customer ?? this.customer,
  );
}

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

const opPatchItem = OperationMeta(
  id: 'op_patch_item',
  method: 'PATCH',
  path: '/items/{sku}',
  entity: 'LineItem',
  invalidates: ['LineItem[]'],
  rootType: 'LineItem',
);

final updateOrder = mutation<Order, OrderArgs, Order>(
  opUpdateOrder,
  Order.fromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (order) => order.toClient(),
);

const order7 = TagContext(path: {'id': 7});

QueryCache newCache(
  FutureOr<Object?> Function(TransportRequest request) handler,
  List<(Object, String)> reported,
) => QueryCache(
  transport: FakeTransport((request, _) => handler(request)),
  entities: schema,
  scheduler: ManualScheduler(),
  onError: (error, context) => reported.add((error, context)),
);

Json readOrder(QueryCache cache) =>
    cache.getState(opGetOrder, order7).dataOrNull! as Json;

Future<void> settled(Future<Object?> future) =>
    future.then<void>((_) {}, onError: (Object _) {});

void main() {
  group('review I1: a lossy round trip is not a change', () {
    for (final (name, customer) in [
      (
        'a DateTime the encoder respells',
        <String, Object?>{
          'id': 'c1',
          'name': 'Ada',
          'since': '2024-01-01T00:00:00Z',
        },
      ),
      (
        'an explicit null the encoder omits',
        <String, Object?>{'id': 'c1', 'name': 'Ada', 'since': null},
      ),
      (
        'a field the model does not declare',
        <String, Object?>{'id': 'c1', 'name': 'Ada', 'tier': 'gold'},
      ),
    ]) {
      test('keeps the reference over $name', () async {
        final reported = <(Object, String)>[];
        final gate = Completer<Object?>();
        final cache = newCache(
          (request) => request.meta.method == 'PATCH'
              ? gate.future
              : {'id': 7, 'status': 'open', 'customer': customer},
          reported,
        );

        await cache.fetch(opGetOrder, order7);

        unawaited(
          settled(
            updateOrder(
              cache,
              const OrderArgs(7),
              optimistic: OptimisticUpdate(
                (order) => order.copyWith(status: 'shipped'),
                key: 'Order:7',
              ),
            ),
          ),
        );

        final folded = cache.overlays.effective('Order:7')!.data;
        expect(folded['status'], 'shipped');
        expect(folded['customer'], refTo('Customer:c1'));
        expect(reported, isEmpty);

        gate.complete(null);
      });
    }
  });

  group('review I2: promotion keeps what was shown', () {
    test(
      'commits an edit made to the embedded entity through its parent',
      () async {
        final reported = <(Object, String)>[];
        final gate = Completer<Object?>();
        final cache = newCache(
          (request) => request.meta.method == 'PATCH'
              ? gate.future
              : {
                  'id': 7,
                  'status': 'open',
                  'customer': {'id': 'c1', 'name': 'Ada'},
                },
          reported,
        );

        await cache.fetch(opGetOrder, order7);

        final pending = updateOrder(
          cache,
          const OrderArgs(7),
          optimistic: OptimisticUpdate(
            (order) => order.copyWith(
              customer: const Customer(id: 'c1', name: 'Ada L.'),
            ),
            key: 'Order:7',
          ),
        );

        expect((readOrder(cache)['customer']! as Json)['name'], 'Ada L.');

        gate.complete(null);
        await settled(pending);

        expect((readOrder(cache)['customer']! as Json)['name'], 'Ada L.');
        // Kept as a reference, with the edit written to the entity itself.
        expect(
          cache.store.getRecord('Order:7')!.data['customer'],
          refTo('Customer:c1'),
        );
        expect(cache.store.getRecord('Customer:c1')!.data['name'], 'Ada L.');
        expect(reported, isEmpty);
      },
    );
    test('leaves an embedded entity a frame wrote in flight alone', () async {
      final reported = <(Object, String)>[];
      final gate = Completer<Object?>();
      final cache = newCache(
        (request) => request.meta.method == 'PATCH'
            ? gate.future
            : {
                'id': 7,
                'status': 'open',
                'customer': {'id': 'c1', 'name': 'Ada'},
              },
        reported,
      );

      await cache.fetch(opGetOrder, order7);

      final pending = updateOrder(
        cache,
        const OrderArgs(7),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            status: 'shipped',
            customer: const Customer(id: 'c1', name: 'Ada L.'),
          ),
          key: 'Order:7',
        ),
      );

      // A stream frame renames the customer while the write is in flight.
      cache.store.write(
        {'id': 'c1', 'name': 'From a frame'},
        cache.entities,
        'Customer',
        CommitOptions(frameAt: cache.store.nextFrame()),
      );

      gate.complete(null);
      await settled(pending);

      expect(cache.store.getRecord('Order:7')!.data['status'], 'shipped');
      expect(
        cache.store.getRecord('Customer:c1')!.data['name'],
        'From a frame',
      );
      expect(reported, isEmpty);
    });
  });

  group('review I3: nothing pending, and no cycle, reaches base', () {
    test("does not bake another mutation's pending patch into base", () async {
      final reported = <(Object, String)>[];
      final orderGate = Completer<Object?>();
      final itemGate = Completer<Object?>();
      final cache = newCache((request) {
        if (request.meta.id == 'op_update_order') return orderGate.future;
        if (request.meta.id == 'op_patch_item') return itemGate.future;

        return {
          'id': 7,
          'status': 'open',
          'items': [
            {'sku': 'a', 'qty': 1},
          ],
        };
      }, reported);

      await cache.fetch(opGetOrder, order7);

      // B: a pending qty change on LineItem:a.
      final itemWrite = cache.mutate(
        opPatchItem,
        const TagContext(path: {'sku': 'a'}),
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>(
            (previous) => {...(previous! as Json), 'qty': 99},
            key: 'LineItem:a',
          ),
        ),
      );

      // A: add a line item the store does not hold yet.
      final orderWrite = cache.mutate(
        opUpdateOrder,
        order7,
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>((value) {
            final previous = value! as Json;

            return {
              ...previous,
              'items': [
                ...(previous['items']! as List<Object?>),
                {'sku': 'z', 'qty': 1},
              ],
            };
          }, key: 'Order:7'),
        ),
      );

      final shown = readOrder(cache)['items']! as List<Object?>;
      expect([for (final item in shown) (item! as Json)['qty']], [99, 1]);

      orderGate.complete(null);
      await orderWrite;

      itemGate.completeError(StateError('item write failed'));
      await expectLater(itemWrite, throwsStateError);

      expect(cache.overlays.empty, isTrue);
      expect(cache.store.getRecord('LineItem:a')!.data['qty'], 1);

      final items = readOrder(cache)['items']! as List<Object?>;
      expect([for (final item in items) (item! as Json)['sku']], ['a', 'z']);
      expect([for (final item in items) (item! as Json)['qty']], [1, 1]);
      expect(cache.store.getRecord('Order:7')!.data['items'], [
        refTo('LineItem:a'),
        refTo('LineItem:z'),
      ]);
      expect(reported, isEmpty);
    });

    test('never writes a cyclic record into base', () async {
      final reported = <(Object, String)>[];
      final gate = Completer<Object?>();
      final cache = newCache(
        (request) => request.meta.method == 'PATCH'
            ? gate.future
            : {
                'id': 7,
                'status': 'open',
                'customer': {'id': 'c1', 'name': 'Ada'},
              },
        reported,
      );

      await cache.fetch(opGetOrder, order7);
      cache.store.put('Customer:c1', {
        'orders': markRewritten([makeRef('Order:7')]),
      });

      final write = cache.mutate(
        opUpdateOrder,
        order7,
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>((value) {
            final previous = value! as Json;
            final customer = previous['customer']! as Json;

            return {
              ...previous,
              'customer': {
                ...customer,
                'orders': [
                  ...(customer['orders']! as List<Object?>),
                  {'id': 99, 'status': 'new'},
                ],
              },
            };
          }, key: 'Order:7'),
        ),
      );

      gate.complete(null);
      await write;

      final base = cache.store.getRecord('Order:7')!.data;

      expect(base['customer'], refTo('Customer:c1'));
      expect(cache.store.getRecord('Customer:c1')!.data['orders'], [
        refTo('Order:7'),
        refTo('Order:99'),
      ]);
      // A stored record holds references, which plain JSON cannot spell, so
      // they are written the way the snapshot format writes them. What this
      // proves is that the record is a tree: a cycle would throw.
      expect(
        () => jsonEncode(
          base,
          toEncodable: (Object? value) =>
              value is EntityRef ? {'__ref': value.key} : value,
        ),
        returnsNormally,
      );
      expect(
        () => dehydrate(cache, principal: cache.principal).encode(),
        returnsNormally,
      );
      // A denormalized snapshot is not checked: this graph is cyclic by
      // construction (the customer lists the order), and a denormalized value
      // of it cannot be written with or without any optimistic update.
      expect(reported, isEmpty);
    });
  });

  group('review C1: a fold stays linear', () {
    // A thread holding n messages, each pointing back at the thread, and a
    // pending computed patch on every message: "mark all read". [deep] makes
    // each compute read into its thread's messages, which folds them.
    ({QueryCache cache, int Function() computes, void Function() reset}) thread(
      int n, {
      bool deep = false,
    }) {
      const threads = <String, EntityMeta>{
        'Thread': EntityMeta(idField: 'id', fields: {'messages': 'Message'}),
        'Message': EntityMeta(idField: 'id', fields: {'thread': 'Thread'}),
      };
      final cache = QueryCache(
        transport: FakeTransport((_, _) => Completer<Object?>().future),
        entities: threads,
        scheduler: ManualScheduler(),
      );

      cache.store.put('Thread:t', {
        'id': 't',
        'messages': markRewritten([
          for (var i = 1; i <= n; i++) makeRef('Message:$i'),
        ]),
      });

      for (var i = 1; i <= n; i++) {
        cache.store.put('Message:$i', {
          'id': i,
          'read': false,
          'thread': makeRef('Thread:t'),
        });
      }

      var computes = 0;

      cache.overlays.add({
        for (var i = 1; i <= n; i++)
          'Message:$i': MergePatch.computed((previous) {
            computes++;

            if (deep) {
              final thread = previous['thread']! as Json;
              expect(thread['messages'], hasLength(n));
            }

            return {...previous, 'read': true};
          }),
      });

      return (
        cache: cache,
        computes: () => computes,
        reset: () => computes = 0,
      );
    }

    List<Object?> readAll(QueryCache cache) =>
        (cache.store.read(makeRef('Thread:t'))! as Json)['messages']!
            as List<Object?>;

    test('runs each of 10 message patches once across a whole read', () {
      const n = 10;
      final (:cache, :computes, :reset) = thread(n);

      // Before the fix one fold ran 986410 computes.
      cache.overlays.effective('Message:1');
      expect(computes(), lessThanOrEqualTo(n));

      reset();
      final messages = readAll(cache);

      expect(computes(), lessThanOrEqualTo(n));
      expect(messages, hasLength(n));
      expect(
        messages.every((message) => (message! as Json)['read'] == true),
        isTrue,
      );
    });

    test('folds each message once per fold when computes read the thread', () {
      const n = 10;
      final (:cache, :computes, :reset) = thread(n, deep: true);

      cache.overlays.effective('Message:1');
      expect(computes(), lessThanOrEqualTo(n));

      // A read folds every message at the top: n folds, n computes each.
      reset();
      final messages = readAll(cache);

      expect(computes(), lessThanOrEqualTo(n * n));
      expect(
        messages.every((message) => (message! as Json)['read'] == true),
        isTrue,
      );
    });

    test('folds an all-to-all graph of 8 pending records linearly', () {
      const n = 8;
      final cache = newCache((_) => Completer<Object?>().future, []);

      for (var i = 1; i <= n; i++) {
        cache.store.put('Order:$i', {
          'id': i,
          'related': markRewritten([
            for (var j = 1; j <= n; j++)
              if (j != i) makeRef('Order:$j'),
          ]),
        });
      }

      var computes = 0;

      cache.overlays.add({
        for (var i = 1; i <= n; i++)
          'Order:$i': MergePatch.computed((previous) {
            computes++;

            // Reads every related order, which folds it.
            expect(previous['related'], hasLength(n - 1));

            return {...previous, 'status': 'x'};
          }),
      });

      cache.overlays.effective('Order:1');
      expect(computes, lessThanOrEqualTo(n));
    });
  });

  group('review M1: a compute that reads an embedded entity', () {
    test('runs again when that entity changes', () async {
      final reported = <(Object, String)>[];
      final gate = Completer<Object?>();
      final renameGate = Completer<Object?>();
      final cache = newCache(
        (request) => request.meta.method == 'PATCH'
            ? (request.meta.id == 'op_patch_item'
                  ? renameGate.future
                  : gate.future)
            : {
                'id': 7,
                'status': 'open',
                'customer': {'id': 'c1', 'name': 'Ada'},
              },
        reported,
      );

      await cache.fetch(opGetOrder, order7);
      cache.subscribe(opGetOrder, order7, () {});

      unawaited(
        settled(
          updateOrder(
            cache,
            const OrderArgs(7),
            optimistic: OptimisticUpdate(
              (order) => order.copyWith(status: 'for ${order.customer.name}'),
              key: 'Order:7',
            ),
          ),
        ),
      );

      expect(readOrder(cache)['status'], 'for Ada');

      cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Grace'});
      cache.notifyChanged();

      expect((readOrder(cache)['customer']! as Json)['name'], 'Grace');
      expect(readOrder(cache)['status'], 'for Grace');

      // And when a pending patch on that entity settles.
      final rename = cache.mutate(
        opPatchItem,
        TagContext.empty,
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>(
            (previous) => {...(previous! as Json), 'name': 'Hopper'},
            key: 'Customer:c1',
          ),
        ),
      );

      expect(readOrder(cache)['status'], 'for Hopper');

      renameGate.completeError(StateError('nope'));
      await settled(rename);

      expect(readOrder(cache)['status'], 'for Grace');
      expect(reported, isEmpty);

      gate.complete(null);
    });
  });

  group('review M2: a throwing compute', () {
    test('is reported once per refresh, not once per path to it', () async {
      final reported = <(Object, String)>[];
      final cache = newCache(
        (_) => {
          'id': 7,
          'status': 'open',
          'customer': {'id': 'c1', 'name': 'Ada'},
        },
        reported,
      );

      await cache.fetch(opGetOrder, order7);
      cache.subscribe(opGetOrder, order7, () {});
      cache.overlays.add({
        'Customer:c1': MergePatch.computed((_) => throw StateError('c1 boom')),
      });
      cache.overlays.add({
        'Order:7': MergePatch.computed(
          (previous) => {...previous, 'status': 'x'},
        ),
      });
      reported.clear();

      cache.notifyChanged();
      cache.getState(opGetOrder, order7);

      expect(reported, hasLength(1));
      expect('${reported.single.$1}', 'Bad state: c1 boom');
      expect(reported.single.$2, 'optimistic');
    });
  });

  group('review M3: an embedded entity base did not hold', () {
    test(
      'is written as a record, so a later fetch of it shows through',
      () async {
        final reported = <(Object, String)>[];
        final gate = Completer<Object?>();
        final cache = newCache(
          (request) => request.meta.method == 'PATCH'
              ? gate.future
              : {
                  'id': 7,
                  'status': 'open',
                  'customer': {'id': 'c1', 'name': 'Ada'},
                },
          reported,
        );

        await cache.fetch(opGetOrder, order7);
        cache.subscribe(opGetOrder, order7, () {});

        final pending = updateOrder(
          cache,
          const OrderArgs(7),
          optimistic: OptimisticUpdate(
            (order) => order.copyWith(
              customer: const Customer(id: 'c3', name: 'Hedy'),
            ),
            key: 'Order:7',
          ),
        );

        gate.complete(null);
        await settled(pending);

        expect(
          cache.store.getRecord('Order:7')!.data['customer'],
          refTo('Customer:c3'),
        );
        expect((readOrder(cache)['customer']! as Json)['name'], 'Hedy');

        // The customer arrives later, as a fetch or a frame would write it.
        cache.store.put('Customer:c3', {'id': 'c3', 'name': 'Hedy Lamarr'});
        cache.notifyChanged();

        expect((readOrder(cache)['customer']! as Json)['name'], 'Hedy Lamarr');
        expect(reported, isEmpty);
      },
    );
  });

  group('review: checked and fine, pinned', () {
    test(
      'promotes stacked patches on the order and its customer apart',
      () async {
        final reported = <(Object, String)>[];
        final gates = [Completer<Object?>(), Completer<Object?>()];
        var patches = 0;
        final cache = newCache((request) {
          if (request.meta.method == 'PATCH') return gates[patches++].future;

          return {
            'id': 7,
            'status': 'open',
            'customer': {'id': 'c1', 'name': 'Ada'},
          };
        }, reported);

        await cache.fetch(opGetOrder, order7);

        final a = updateOrder(
          cache,
          const OrderArgs(7),
          optimistic: OptimisticUpdate(
            (order) => order.copyWith(status: 'shipped'),
            key: 'Order:7',
          ),
        );
        final b = cache.mutate(
          opUpdateOrder,
          order7,
          options: MutateOptions(
            optimistic: OptimisticUpdate<Object?>(
              (previous) => {...(previous! as Json), 'name': 'Ada L.'},
              key: 'Customer:c1',
            ),
          ),
        );

        expect(readOrder(cache)['status'], 'shipped');
        expect((readOrder(cache)['customer']! as Json)['name'], 'Ada L.');

        gates[0].complete(null);
        await settled(a);

        expect(
          cache.store.getRecord('Order:7')!.data['customer'],
          refTo('Customer:c1'),
        );
        expect(cache.store.getRecord('Customer:c1')!.data['name'], 'Ada');

        gates[1].completeError(StateError('x'));
        await expectLater(b, throwsStateError);

        expect(readOrder(cache)['status'], 'shipped');
        expect((readOrder(cache)['customer']! as Json)['name'], 'Ada');
        expect(reported, isEmpty);
      },
    );

    test('reports a typed update over a deleted embedded entity', () async {
      final reported = <(Object, String)>[];
      final gate = Completer<Object?>();
      final cache = newCache(
        (request) => request.meta.method == 'PATCH'
            ? gate.future
            : {
                'id': 7,
                'status': 'open',
                'customer': {'id': 'c1', 'name': 'Ada'},
              },
        reported,
      );

      await cache.fetch(opGetOrder, order7);
      cache.overlays.add({'Customer:c1': const DeletePatch()});

      unawaited(
        settled(
          updateOrder(
            cache,
            const OrderArgs(7),
            optimistic: OptimisticUpdate(
              (order) => order.copyWith(status: 'shipped'),
              key: 'Order:7',
            ),
          ),
        ),
      );

      // The typed decode meets a null customer: reported, and no change.
      expect(cache.overlays.effective('Order:7')!.data['status'], 'open');
      expect(reported, isNotEmpty);
      expect(reported.map((entry) => entry.$2).toSet(), {'optimistic'});

      gate.complete(null);
    });
  });
}
