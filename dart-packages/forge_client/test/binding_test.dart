// New in Dart: the typed binding layer's mutation half. The identity rule and
// `enabled: false` have their own suite, binding_identity_test.dart.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/models.dart';
import 'support/schema.dart';

const opListOrders = OperationMeta(
  id: 'op_list_orders',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);
const opGetOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
);
const opUpdateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}'],
);
const opCreateOrder = OperationMeta(
  id: 'op_create_order',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  invalidates: ['Order[]'],
);

final listOrders = query<List<Order>, ListOrdersArgs>(
  opListOrders,
  ordersFromClient,
);
final getOrder = query<Order, GetOrderArgs>(opGetOrder, Order.fromClient);
final updateOrder = mutation<Order, UpdateOrderArgs, Order>(
  opUpdateOrder,
  Order.fromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (order) => order.toClient(),
);
final createOrder = mutation<Order, CreateOrderArgs, Order>(
  opCreateOrder,
  Order.fromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (order) => order.toClient(),
);

typedef Rig = ({
  FakeTransport transport,
  ManualScheduler scheduler,
  QueryCache cache,
});

Rig rig(
  FutureOr<Object?> Function(TransportRequest request, int call) handler,
) {
  final transport = FakeTransport(handler);
  final scheduler = ManualScheduler();

  return (
    transport: transport,
    scheduler: scheduler,
    cache: QueryCache(
      transport: transport,
      entities: schema,
      scheduler: scheduler,
    ),
  );
}

void main() {
  group('typed mutations', () {
    test(
      'converts a typed update to client shape and returns the typed response',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = rig(
          (request, _) => request.meta.method == 'GET'
              ? {'id': 7, 'total': 99, 'status': 'open'}
              : gate.future,
        );
        final ref = getOrder(const GetOrderArgs(id: 7));

        await ref.fetch(cache);
        cache.subscribe(opGetOrder, ref.context, () {});

        final pending = updateOrder(
          cache,
          const UpdateOrderArgs(id: 7, status: Assign('shipped')),
          optimistic: OptimisticUpdate(
            (order) => order.copyWith(status: 'shipped'),
          ),
        );

        expect(ref.getState(cache).dataOrNull?.status, 'shipped');
        expect(ref.getState(cache).isOptimistic, isTrue);

        gate.complete({'id': 7, 'total': 99, 'status': 'shipped'});

        expect(await pending, const Order(id: 7, total: 99, status: 'shipped'));
        expect(ref.getState(cache).isOptimistic, isFalse);
      },
    );

    test(
      'shows a typed create under a minted key until the server answers',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = rig(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 8, 'total': 12},
                ]
              : gate.future,
        );
        final ref = listOrders(const ListOrdersArgs());

        await ref.fetch(cache);
        cache.subscribe(opListOrders, ref.context, () {});

        final pending = createOrder(
          cache,
          const CreateOrderArgs(total: 5),
          optimistic: const OptimisticCreate(Order(id: 0, total: 5)),
          place: {
            'Order[]': (created, current, _) => [
              created,
              ...current! as List<Object?>,
            ],
          },
        );

        // The minted id is a string, so the typed list cannot decode it: the
        // raw cache shows the row, which is what an adapter renders through a
        // tolerant model.
        final raw =
            cache.getState(opListOrders, ref.context).dataOrNull!
                as List<Object?>;
        expect((raw[0]! as Map<String, Object?>)['id'], '~opt1');

        gate.complete({'id': 9, 'total': 5});

        expect(await pending, const Order(id: 9, total: 5));
      },
    );

    test('omits an unchanged PATCH field and sends an assigned null', () {
      expect(
        const UpdateOrderArgs(id: 7).toTagContext().body,
        <String, Object?>{},
      );
      expect(
        const UpdateOrderArgs(id: 7, status: Assign(null)).toTagContext().body,
        {'status': null},
      );
    });
  });

  group('args', () {
    // Built at run time, as a widget rebuilding every frame builds them.
    final seven = int.parse('7');
    final shipped = 'SHIPPED'.toLowerCase();

    test('rebuilt args compare equal and key the same query', () {
      const first = UpdateOrderArgs(id: 7, status: Assign('shipped'));
      final rebuilt = UpdateOrderArgs(id: seven, status: Assign(shipped));

      expect(rebuilt, first);
      expect(rebuilt.hashCode, first.hashCode);
      expect(
        getOrder(GetOrderArgs(id: seven)).key,
        getOrder(const GetOrderArgs(id: 7)).key,
      );
      expect(getOrder(GetOrderArgs(id: seven)).args, const GetOrderArgs(id: 7));
    });
  });
}
