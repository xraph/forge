// New in Dart: optimistic updates over a record that embeds another entity.
//
// The store holds an `EntityRef` where an embedded entity was, so the merge a
// typed `OptimisticUpdate` runs has to see the record the way a read does,
// with every reference resolved, and has to leave the references in place for
// whatever it did not change. The flat `Order` in support/models.dart cannot
// show any of this, which is why these cases live apart from binding_test.dart.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

/// An embedded entity, shaped like a generated model.
final class Customer {
  const Customer({required this.id, required this.name});

  final String id;
  final String name;

  static Customer fromClient(Object? client) {
    final json = client! as Json;

    return Customer(id: json['id']! as String, name: json['name']! as String);
  }

  Json toClient() => {'id': id, 'name': name};

  @override
  bool operator ==(Object other) =>
      other is Customer && other.id == id && other.name == name;

  @override
  int get hashCode => Object.hash(id, name);
}

/// An `Order` that embeds its `Customer`, the way the generated orders client
/// does. The schema routes `Order.customer` to `Customer`.
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

  @override
  bool operator ==(Object other) =>
      other is Order &&
      other.id == id &&
      other.status == status &&
      other.customer == customer;

  @override
  int get hashCode => Object.hash(id, status, customer);
}

final class GetOrderArgs implements OperationArgs {
  const GetOrderArgs(this.id);

  final int id;

  @override
  TagContext toTagContext() => TagContext(path: {'id': id});

  @override
  bool operator ==(Object other) => other is GetOrderArgs && other.id == id;

  @override
  int get hashCode => id.hashCode;
}

final class UpdateOrderArgs implements OperationArgs {
  const UpdateOrderArgs(this.id, this.status);

  final int id;
  final String status;

  @override
  TagContext toTagContext() =>
      TagContext(path: {'id': id}, body: {'status': status});

  @override
  bool operator ==(Object other) =>
      other is UpdateOrderArgs && other.id == id && other.status == status;

  @override
  int get hashCode => Object.hash(id, status);
}

const opGetOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
  rootType: 'Order',
);

// What the generator emits for a PATCH: the collection, not the item. The
// target comes from `key:`, as it does with the generated orders client.
const opUpdateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order[]'],
  rootType: 'Order',
);

const opGetCustomer = OperationMeta(
  id: 'op_get_customer',
  method: 'GET',
  path: '/customers/{id}',
  entity: 'Customer',
  provides: ['Customer:{id}'],
  rootType: 'Customer',
);

final getOrder = query<Order, GetOrderArgs>(opGetOrder, Order.fromClient);
final updateOrder = mutation<Order, UpdateOrderArgs, Order>(
  opUpdateOrder,
  Order.fromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (order) => order.toClient(),
);

const ada = {'id': 'c1', 'name': 'Ada'};
const grace = {'id': 'c2', 'name': 'Grace'};

Json order(String status, [Json customer = ada]) => {
  'id': 7,
  'status': status,
  'customer': customer,
};

typedef Rig = ({
  FakeTransport transport,
  QueryCache cache,
  List<(Object, String)> reported,
  QueryRef<Order, GetOrderArgs> ref,
});

/// A cache holding `Order:7` (embedding `Customer:c1`), with a subscriber, and
/// a PATCH that waits on [patch].
Future<Rig> held(FutureOr<Object?> Function() patch) async {
  final reported = <(Object, String)>[];
  final transport = FakeTransport(
    (request, _) => switch (request.meta.method) {
      'PATCH' => patch(),
      _ when request.meta.entity == 'Customer' => grace,
      _ => order('open'),
    },
  );
  final cache = QueryCache(
    transport: transport,
    entities: schema,
    scheduler: ManualScheduler(),
    onError: (error, context) => reported.add((error, context)),
  );
  final ref = getOrder(const GetOrderArgs(7));

  await ref.fetch(cache);
  cache.subscribe(opGetOrder, ref.context, () {});

  return (transport: transport, cache: cache, reported: reported, ref: ref);
}

void main() {
  group('optimistic update over an embedded entity', () {
    test('shows the patched value and rolls back on failure', () async {
      final gate = Completer<Object?>();
      final (:cache, :reported, :ref, transport: _) = await held(
        () => gate.future,
      );

      expect(
        cache.store.getRecord('Order:7')!.data['customer'],
        refTo('Customer:c1'),
      );

      final pending = updateOrder(
        cache,
        const UpdateOrderArgs(7, 'shipped'),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(status: 'shipped'),
          key: 'Order:7',
        ),
      );

      final shown = ref.getState(cache);
      expect(shown.dataOrNull?.status, 'shipped');
      expect(shown.dataOrNull?.customer, const Customer(id: 'c1', name: 'Ada'));
      expect(shown.isOptimistic, isTrue);
      expect(reported, isEmpty);

      gate.completeError(StateError('nope'));
      await expectLater(pending, throwsStateError);

      final after = ref.getState(cache);
      expect(after.dataOrNull?.status, 'open');
      expect(after.dataOrNull?.customer, const Customer(id: 'c1', name: 'Ada'));
      expect(after.isOptimistic, isFalse);
      expect(reported, isEmpty);
    });

    test('keeps the reference for a field the update did not change', () async {
      final gate = Completer<Object?>();
      final (:cache, :reported, :ref, transport: _) = await held(
        () => gate.future,
      );

      unawaited(
        updateOrder(
          cache,
          const UpdateOrderArgs(7, 'shipped'),
          optimistic: OptimisticUpdate(
            (order) => order.copyWith(status: 'shipped'),
            key: 'Order:7',
          ),
        ).then<void>((_) {}, onError: (Object _) {}),
      );

      final folded = cache.overlays.effective('Order:7')!.data;
      expect(folded['status'], 'shipped');
      expect(folded['customer'], refTo('Customer:c1'));

      // Still a reference, so a write to the customer shows through the
      // pending order.
      cache.store.put('Customer:c1', {'id': 'c1', 'name': 'Ada L.'});
      cache.notifyChanged();

      expect(ref.getState(cache).dataOrNull?.customer.name, 'Ada L.');
      expect(ref.getState(cache).dataOrNull?.status, 'shipped');
      expect(reported, isEmpty);

      gate.complete(order('shipped', {'id': 'c1', 'name': 'Ada L.'}));
    });

    test('promotes on success without inlining the embedded entity', () async {
      final gate = Completer<Object?>();
      final (:cache, :reported, :ref, transport: _) = await held(
        () => gate.future,
      );

      final pending = updateOrder(
        cache,
        const UpdateOrderArgs(7, 'shipped'),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(status: 'shipped'),
          key: 'Order:7',
        ),
      );

      // A 204-style answer: nothing to commit, so what base ends up holding
      // is exactly what the promotion wrote.
      gate.complete(null);
      await pending.then<void>((_) {}, onError: (Object _) {});

      final base = cache.store.getRecord('Order:7')!.data;
      expect(base['status'], 'shipped');
      expect(base['customer'], refTo('Customer:c1'));
      expect(ref.getState(cache).isOptimistic, isFalse);
      expect(ref.getState(cache).dataOrNull?.status, 'shipped');
      expect(reported, isEmpty);
    });

    test(
      'shows a reassigned embedded entity, and promotes it as a reference',
      () async {
        final gate = Completer<Object?>();
        final (:cache, :reported, :ref, transport: _) = await held(
          () => gate.future,
        );

        // The store holds the customer the order is being moved to.
        await cache.fetch(opGetCustomer, const TagContext(path: {'id': 'c2'}));

        final pending = updateOrder(
          cache,
          const UpdateOrderArgs(7, 'open'),
          optimistic: OptimisticUpdate(
            (order) => order.copyWith(
              customer: const Customer(id: 'c2', name: 'Grace'),
            ),
            key: 'Order:7',
          ),
        );

        expect(
          ref.getState(cache).dataOrNull?.customer,
          const Customer(id: 'c2', name: 'Grace'),
        );

        gate.complete(null);
        await pending.then<void>((_) {}, onError: (Object _) {});

        expect(
          cache.store.getRecord('Order:7')!.data['customer'],
          refTo('Customer:c2'),
        );
        expect(
          ref.getState(cache).dataOrNull?.customer,
          const Customer(id: 'c2', name: 'Grace'),
        );
        expect(reported, isEmpty);
      },
    );

    test('promotes a reassignment to an entity base does not hold as a reference to its new record', () async {
      final gate = Completer<Object?>();
      final (:cache, :reported, :ref, transport: _) = await held(
        () => gate.future,
      );

      final pending = updateOrder(
        cache,
        const UpdateOrderArgs(7, 'open'),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(
            customer: const Customer(id: 'c3', name: 'Hedy'),
          ),
          key: 'Order:7',
        ),
      );

      expect(ref.getState(cache).dataOrNull?.customer.name, 'Hedy');

      gate.complete(null);
      await pending.then<void>((_) {}, onError: (Object _) {});

      // The record is written too, so the reference is never a hole.
      expect(
        cache.store.getRecord('Order:7')!.data['customer'],
        refTo('Customer:c3'),
      );
      expect(cache.store.getRecord('Customer:c3')!.data, {
        'id': 'c3',
        'name': 'Hedy',
      });
      expect(ref.getState(cache).dataOrNull?.customer.name, 'Hedy');
      expect(reported, isEmpty);
    });

    test('terminates on a cycle back to the record being patched', () async {
      final gate = Completer<Object?>();
      final (:cache, :reported, :ref, transport: _) = await held(
        () => gate.future,
      );

      // The customer lists the order, as `Customer.orders` allows.
      cache.store.put('Customer:c1', {
        'orders': markRewritten([makeRef('Order:7')]),
      });
      cache.notifyChanged();

      bool? closes;
      final raw = mutation<Object?, UpdateOrderArgs, Object?>(
        opUpdateOrder,
        (client) => client,
      );

      unawaited(
        raw(
          cache,
          const UpdateOrderArgs(7, 'shipped'),
          optimistic: OptimisticUpdate<Json>((Json previous) {
            // Read while the compute runs: afterwards the view is sealed.
            final customer = previous['customer']! as Json;
            closes ??= identical(
              (customer['orders']! as List<Object?>).single,
              previous,
            );

            return {...previous, 'status': 'shipped'};
          }, key: 'Order:7'),
        ).then<void>((_) {}, onError: (Object _) {}),
      );

      expect(ref.getState(cache).dataOrNull?.status, 'shipped');

      // The cycle closes on the view itself, as a read's does.
      expect(closes, isTrue);
      expect(
        cache.overlays.effective('Order:7')!.data['customer'],
        refTo('Customer:c1'),
      );
      expect(reported, isEmpty);

      gate.complete(order('shipped'));
    });

    test(
      'hands an untyped update the record with references resolved',
      () async {
        final gate = Completer<Object?>();
        final (:cache, :reported, :ref, transport: _) = await held(
          () => gate.future,
        );
        final raw = mutation<Object?, UpdateOrderArgs, Object?>(
          opUpdateOrder,
          (client) => client,
        );
        Object? seen;

        unawaited(
          raw(
            cache,
            const UpdateOrderArgs(7, 'shipped'),
            optimistic: OptimisticUpdate<Json>((Json previous) {
              seen = previous['customer'];

              return {...previous, 'status': 'shipped'};
            }, key: 'Order:7'),
          ).then<void>((_) {}, onError: (Object _) {}),
        );

        expect(ref.getState(cache).dataOrNull?.status, 'shipped');
        expect(seen, ada);
        expect(
          cache.overlays.effective('Order:7')!.data['customer'],
          refTo('Customer:c1'),
        );
        expect(reported, isEmpty);

        gate.complete(order('shipped'));
      },
    );

    test('reports an update that throws, under optimistic, and still sends the write', () async {
      final (:cache, :reported, :ref, :transport) = await held(
        () => order('shipped'),
      );

      final result = await updateOrder(
        cache,
        const UpdateOrderArgs(7, 'shipped'),
        optimistic: OptimisticUpdate(
          (_) => throw StateError('patch boom'),
          key: 'Order:7',
        ),
      );

      expect(result.status, 'shipped');
      expect(transport.calls.map((call) => call.meta.method), ['GET', 'PATCH']);
      expect(reported, isNotEmpty);
      expect(reported.map((entry) => entry.$2).toSet(), {'optimistic'});
      expect(reported.map((entry) => '${entry.$1}').toSet(), {
        'Bad state: patch boom',
      });
      expect(ref.getState(cache).dataOrNull?.status, 'shipped');
      expect(cache.overlays.empty, isTrue);
    });

    test(
      'keeps reading when onError itself throws on a failed update',
      () async {
        final gate = Completer<Object?>();
        final transport = FakeTransport(
          (request, _) =>
              request.meta.method == 'PATCH' ? gate.future : order('open'),
        );
        final cache = QueryCache(
          transport: transport,
          entities: schema,
          scheduler: ManualScheduler(),
          onError: (_, _) => throw StateError('handler boom'),
        );
        final ref = getOrder(const GetOrderArgs(7));

        await ref.fetch(cache);
        cache.subscribe(opGetOrder, ref.context, () {});

        final pending = updateOrder(
          cache,
          const UpdateOrderArgs(7, 'shipped'),
          optimistic: OptimisticUpdate(
            (_) => throw StateError('patch boom'),
            key: 'Order:7',
          ),
        );

        expect(ref.getState(cache).dataOrNull?.status, 'open');
        expect(ref.getState(cache).isOptimistic, isTrue);

        gate.complete(order('shipped'));

        expect((await pending).status, 'shipped');
        expect(ref.getState(cache).dataOrNull?.status, 'shipped');
      },
    );
  });

  // The advice in these messages is what a reader acts on. On a typed binding
  // `OptimisticMany` does not typecheck, so the route that works is `key:`.
  group('what an untargeted optimistic update says', () {
    test('names key: when the operation invalidates no entity key', () async {
      final (:cache, :reported, :ref, :transport) = await held(
        () => order('shipped'),
      );

      final result = await updateOrder(
        cache,
        const UpdateOrderArgs(7, 'shipped'),
        optimistic: OptimisticUpdate(
          (order) => order.copyWith(status: 'shipped'),
        ),
      );

      expect(result.status, 'shipped');
      expect(transport.calls.map((call) => call.meta.method), ['GET', 'PATCH']);
      expect(reported, hasLength(1));
      expect(reported.single.$2, 'optimistic');
      expect(reported.single.$1, isA<StateError>());
      expect(
        (reported.single.$1 as StateError).message,
        "[forge] optimistic: PATCH /orders/{id} names no entity to patch: its "
        'invalidates holds no single entity key. Pass key: to name the '
        "record, for example key: entityKey('Order', id), or, on an untyped "
        'binding, OptimisticMany with explicit keys.',
      );
      expect(ref.getState(cache).dataOrNull?.status, 'shipped');
    });

    test('says the same for a delete', () async {
      final (:cache, :reported, ref: _, transport: _) = await held(() => null);
      const remove = OperationMeta(
        id: 'op_delete_order',
        method: 'DELETE',
        path: '/orders/{id}',
        entity: 'Order',
        invalidates: ['Order[]'],
      );

      await cache.mutate(
        remove,
        const TagContext(path: {'id': 7}),
        options: const MutateOptions(optimistic: OptimisticDelete<Object?>()),
      );

      expect(
        '${reported.single.$1}',
        contains(
          "Pass key: to name the record, for example key: entityKey('Order', id),",
        ),
      );
    });

    test('names key: and OptimisticMany when two entity keys make it ambiguous', () async {
      final (:cache, :reported, ref: _, transport: _) = await held(
        () => order('moved'),
      );
      const transfer = OperationMeta(
        id: 'op_transfer_order',
        method: 'PATCH',
        path: '/orders/{id}/transfer',
        entity: 'Order',
        invalidates: ['Order:{id}', 'Customer:{req.customerId}'],
      );

      await cache.mutate(
        transfer,
        const TagContext(path: {'id': 7}, body: {'customerId': 'c2'}),
        options: MutateOptions(
          optimistic: OptimisticUpdate<Object?>((_) => {'status': 'moved'}),
        ),
      );

      expect(reported.single.$2, 'optimistic');
      expect(reported.single.$1, isA<AmbiguousTargetError>());
      expect(
        '${reported.single.$1}',
        '[forge] optimistic: PATCH /orders/{id}/transfer invalidates more '
            'than one entity (Order:7, Customer:c2), so its target cannot be '
            'derived. Pass key: to name the record to patch, or, on an untyped '
            'binding, OptimisticMany with a key on each patch to change several.',
      );
    });

    test(
      'does not suggest OptimisticMany for a create it cannot key',
      () async {
        final reported = <Object>[];
        final cache = QueryCache(
          transport: FakeTransport((_, _) => {'id': 9}),
          entities: schema,
          scheduler: ManualScheduler(),
          onError: (error, _) => reported.add(error),
        );
        const bare = OperationMeta(id: 'bare', method: 'POST', path: '/things');

        await cache.mutate(
          bare,
          TagContext.empty,
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'total': 1}),
          ),
        );

        expect(
          (reported.single as StateError).message,
          '[forge] optimistic: POST /things names no entity to create: an '
          'optimistic create needs the operation to name an entity with an '
          'identity field.',
        );
      },
    );
  });
}
