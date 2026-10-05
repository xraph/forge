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

        // The minted id is a string, so the typed list cannot decode it. The
        // raw cache shows the minted row; the typed state keeps the last rows
        // it could decode, flagged optimistic, rather than failing.
        final raw =
            cache.getState(opListOrders, ref.context).dataOrNull!
                as List<Object?>;
        expect((raw[0]! as Map<String, Object?>)['id'], '~opt1');

        final typed = ref.getState(cache);
        expect(typed, isA<QuerySuccess<List<Order>>>());
        expect(typed.isOptimistic, isTrue);
        expect(typed.dataOrNull, [const Order(id: 8, total: 12)]);

        gate.complete({'id': 9, 'total': 5});

        expect(await pending, const Order(id: 9, total: 5));
      },
    );

    test(
      'never shows a typed int-id list as failed across an optimistic create',
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
        final seen = <QueryState<List<Order>>>[];

        final subscription = ref.watch(cache).listen(seen.add);
        await settle();

        final before = ref.getState(cache).dataOrNull!;

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

        final during = ref.getState(cache);
        expect(during, isA<QuerySuccess<List<Order>>>());
        expect(during.isOptimistic, isTrue);
        expect(during.dataOrNull, same(before));
        expect(seen.last, same(during));

        gate.complete({'id': 9, 'total': 5});
        await pending;
        await settle();

        final after = ref.getState(cache);
        expect(after, isA<QuerySuccess<List<Order>>>());
        expect(after.isOptimistic, isFalse);
        expect(after.dataOrNull, [
          const Order(id: 9, total: 5),
          const Order(id: 8, total: 12),
        ]);
        expect(after.dataOrNull![1], same(before[0]));
        expect(seen.whereType<QueryFailure<List<Order>>>(), isEmpty);

        await subscription.cancel();
      },
    );

    test('shows loading for an optimistic value it cannot decode and never decoded before', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = rig(
        (request, _) => request.meta.method == 'GET'
            ? [
                {'id': 8, 'total': 12},
              ]
            : gate.future,
      );
      final ref = listOrders(const ListOrdersArgs());

      // Fetched through the raw cache, so the typed ref has decoded nothing.
      await cache.fetch(opListOrders, ref.context);
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

      final during = ref.getState(cache);
      expect(during, isA<QueryLoading<List<Order>>>());
      expect(during.isOptimistic, isTrue);

      gate.complete({'id': 9, 'total': 5});
      await pending;
    });

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

  group('optimistic conversion', () {
    test('applies a Json-typed update through an untyped binding and sends the write', () async {
      final gate = Completer<Object?>();
      final errors = <String>[];
      final transport = FakeTransport(
        (request, _) => request.meta.method == 'GET'
            ? {'id': 7, 'total': 99, 'status': 'open'}
            : gate.future,
      );
      final cache = QueryCache(
        transport: transport,
        entities: schema,
        scheduler: ManualScheduler(),
        onError: (_, context) => errors.add(context),
      );
      final patchRaw = mutation<Object?, UpdateOrderArgs, Object?>(
        opUpdateOrder,
        (client) => client,
      );
      final ref = getOrder(const GetOrderArgs(id: 7));

      await ref.fetch(cache);
      cache.subscribe(opGetOrder, ref.context, () {});

      final pending = patchRaw(
        cache,
        const UpdateOrderArgs(id: 7, status: Assign('shipped')),
        optimistic: OptimisticUpdate<Json>(
          (Json previous) => {...previous, 'status': 'shipped'},
        ),
      );

      expect(ref.getState(cache).dataOrNull?.status, 'shipped');
      expect(ref.getState(cache).isOptimistic, isTrue);
      expect(transport.calls.map((call) => call.meta.method), ['GET', 'PATCH']);

      gate.complete({'id': 7, 'total': 99, 'status': 'shipped'});

      expect(await pending, {'id': 7, 'total': 99, 'status': 'shipped'});
      expect(errors, isEmpty);
    });

    test('sends the write without optimism when the conversion throws, and reports it once', () async {
      final reported = <(Object, String)>[];
      final transport = FakeTransport((_, _) => {'id': 9, 'total': 5});
      final cache = QueryCache(
        transport: transport,
        entities: schema,
        scheduler: ManualScheduler(),
        onError: (error, context) => reported.add((error, context)),
      );
      final broken = mutation<Order, CreateOrderArgs, Order>(
        opCreateOrder,
        Order.fromClient,
        entityFromClient: Order.fromClient,
        entityToClient: (_) => throw StateError('cannot encode'),
      );

      final result = await broken(
        cache,
        const CreateOrderArgs(total: 5),
        optimistic: const OptimisticCreate(Order(id: 0, total: 5)),
      );

      expect(result, const Order(id: 9, total: 5));
      expect(transport.calls, hasLength(1));
      expect(reported, hasLength(1));
      expect(reported.single.$1, isA<StateError>());
      expect(reported.single.$2, 'optimistic');
      expect(cache.overlays.empty, isTrue);
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
