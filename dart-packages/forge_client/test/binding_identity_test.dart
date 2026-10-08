// New in Dart: the contracts' Identity rule (a typed model is `identical`
// across unchanged reads, through the Expando memo) and `enabled: false`.
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
  group('the identity rule', () {
    test(
      'hands back an identical model across reads that changed nothing',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => {'id': 7, 'total': 99},
        );
        final ref = getOrder(const GetOrderArgs(id: 7));

        await ref.fetch(cache);

        final first = ref.getState(cache);
        final second = ref.getState(cache);

        expect(second, same(first));
        expect(second.dataOrNull, same(first.dataOrNull));
      },
    );

    test('hands back an identical model across a refetch that returned the same bytes', () async {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => {'id': 7, 'total': 99},
      );
      final ref = getOrder(const GetOrderArgs(id: 7));

      final before = await ref.fetch(cache);
      final after = await ref.refetch(cache);

      expect(after, same(before));
      expect(ref.getState(cache).dataOrNull, same(before));
    });

    test('decodes once for a value the store kept, even when the state object moved', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = rig(
        (_, call) => call == 0 ? {'id': 7, 'total': 99} : gate.future,
      );
      final ref = getOrder(const GetOrderArgs(id: 7));

      final model = await ref.fetch(cache);
      final decodes = Order.decodes;

      // A background refetch flips isFetching, so the state object changes
      // while the data under it does not.
      unawaited(ref.refetch(cache));
      final fetching = ref.getState(cache);

      expect(fetching.isFetching, isTrue);
      expect(fetching.dataOrNull, same(model));
      expect(Order.decodes, decodes);

      gate.complete({'id': 7, 'total': 99});
      await settle();
    });

    test('keeps the model of an untouched row identical when a sibling row changes', () async {
      var total = 99;
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {'id': 7, 'total': 1},
          {'id': 8, 'total': total},
        ],
      );
      final ref = listOrders(const ListOrdersArgs());

      final before = await ref.fetch(cache);

      total = 100;
      final after = await ref.refetch(cache);

      expect(after, isNot(same(before)));
      expect(after[1].total, 100);
      // The list model is rebuilt because its container changed, but the
      // row decoder goes through decodeCached, and Order:7's value is
      // identical, so its model is too.
      expect(after[0], same(before[0]));
      expect(after[1], isNot(same(before[1])));
    });

    test(
      'gives a list row and the single-record read the same model',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (request, _) => request.meta.id == opGetOrder.id
              ? {'id': 7, 'total': 99}
              : [
                  {'id': 7, 'total': 99},
                  {'id': 8, 'total': 1},
                ],
        );

        final rows = await listOrders(const ListOrdersArgs()).fetch(cache);
        final single = await getOrder(const GetOrderArgs(id: 7)).fetch(cache);

        // Both decode Order:7 through decodeCached with the same fromClient, so
        // the store's one record becomes one model.
        expect(single, same(rows[0]));
      },
    );

    test(
      'emits an identical typed state for every unchanged raw state on watch',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => {'id': 7, 'total': 99},
        );
        final ref = getOrder(const GetOrderArgs(id: 7));
        final seen = <QueryState<Order>>[];

        final subscription = ref.watch(cache).listen(seen.add);
        await settle();

        expect(seen.last, isA<QuerySuccess<Order>>());
        expect(seen.last, same(ref.getState(cache)));

        await subscription.cancel();
      },
    );

    test(
      'a different binding over the same value gets its own model',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => {'id': 7, 'total': 99},
        );
        final raw = query<Object?, GetOrderArgs>(
          opGetOrder,
          (client) => client,
        );

        final typed = await getOrder(const GetOrderArgs(id: 7)).fetch(cache);
        final untyped = await raw(const GetOrderArgs(id: 7)).fetch(cache);

        expect(typed, isA<Order>());
        expect(untyped, isA<Map<String, Object?>>());
      },
    );

    test(
      'turns a value the model codec cannot read into a failure state',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => {'id': 'not-an-int', 'total': 99},
        );
        final ref = getOrder(const GetOrderArgs(id: 7));

        await cache.fetch(opGetOrder, ref.context);

        expect(ref.getState(cache), isA<QueryFailure<Order>>());
      },
    );
  });

  // Review focus: the structurally-equal refetch through real JSON parsing.
  group('review focus', () {
    test(
      'a refetch that parses the same bytes keeps the typed model identical',
      () async {
        final fake = FakeHttp(
          (_, _) => {'id': 7, 'total': 99, 'status': 'open'},
        );
        final cache = QueryCache(
          transport: RestTransport(baseUrl: base, client: fake.client),
          entities: schema,
        );
        final ref = getOrder(const GetOrderArgs(id: 7));
        final seen = <QueryState<Order>>[];

        final subscription = ref.watch(cache).listen(seen.add);
        await settle();

        final model = ref.getState(cache).dataOrNull;

        await ref.refetch(cache);
        await settle();

        expect(fake.calls, hasLength(2));
        // The state object moved (isFetching went true and back); the model
        // under it did not.
        expect(ref.getState(cache).dataOrNull, same(model));
        // The fetching flip emitted states; none of them carried a new model.
        expect(
          seen.map((state) => state.dataOrNull).whereType<Order>().toSet(),
          hasLength(1),
        );

        await subscription.cancel();
      },
    );
  });

  group('enabled: false', () {
    test('yields idle and neither fetches nor ref-counts', () async {
      final (:cache, :transport, scheduler: _) = rig(
        (_, _) => {'id': 7, 'total': 99},
      );
      final ref = getOrder(const GetOrderArgs(id: 7));
      final seen = <QueryState<Order>>[];

      final subscription = ref.watch(cache, enabled: false).listen(seen.add);
      await settle();

      expect(seen, [isA<QueryIdle<Order>>()]);
      expect(transport.calls, isEmpty);
      expect(cache.size, 0);
      expect(cache.registry.get(ref.key), isNull);

      await subscription.cancel();
    });

    test('getState returns idle without opening a record', () async {
      final (:cache, :transport, scheduler: _) = rig(
        (_, _) => {'id': 7, 'total': 99},
      );
      final ref = getOrder(const GetOrderArgs(id: 7));

      final state = ref.getState(cache, enabled: false);

      expect(state, isA<QueryIdle<Order>>());
      expect(ref.getState(cache, enabled: false), same(state));
      expect(cache.size, 0);
      expect(cache.registry.get(ref.key), isNull);

      await settle();
      expect(transport.calls, isEmpty);
    });

    test('starts fetching once a dependent query flips enabled on', () async {
      final (:cache, :transport, scheduler: _) = rig(
        (_, _) => {'id': 7, 'total': 99},
      );
      final ref = getOrder(const GetOrderArgs(id: 7));

      final off = ref.watch(cache, enabled: false).listen((_) {});
      await settle();
      await off.cancel();

      final seen = <QueryState<Order>>[];
      final on = ref.watch(cache).listen(seen.add);
      await settle();

      expect(transport.calls, hasLength(1));
      expect(seen.last.dataOrNull, const Order(id: 7, total: 99));

      await on.cancel();
    });
  });
}
