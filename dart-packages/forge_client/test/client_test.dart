// Ported from packages/client-core/__tests__/client.test.ts.
//
// TS bindings are callables tagged with a kind; Dart bindings are typed
// classes, so `kind` is the binding's type and the default client is passed
// explicitly where TS resolved it at call time.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/models.dart';
import 'support/schema.dart';

const opsOrderList = OperationMeta(
  id: 'orderList',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);
const opsOrderCreate = OperationMeta(
  id: 'orderCreate',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  invalidates: ['Order[]'],
);

// Bound at library scope, exactly as generation emits them.
final listOrders = query<List<Object?>, ListOrdersArgs>(
  opsOrderList,
  (client) => client! as List<Object?>,
);
final createOrder = mutation<Object?, CreateOrderArgs, Object?>(
  opsOrderCreate,
  (client) => client,
);

({FakeHttp http, ManualScheduler scheduler, QueryCache cache}) wire(
  HttpHandler handler,
) {
  final http = FakeHttp(handler);
  final scheduler = ManualScheduler();
  final cache = configureClient(
    transport: RestTransport(
      baseUrl: base,
      client: http.client,
      sleep: (_) => Future<void>.value(),
      random: () => 0,
    ),
    entities: schema,
    scheduler: scheduler,
  );

  return (http: http, scheduler: scheduler, cache: cache);
}

void main() {
  tearDown(() => setClient(null));

  group('generated bindings', () {
    test('tags each binding with its kind and operation', () {
      expect(listOrders, isA<QueryBinding<List<Object?>, ListOrdersArgs>>());
      expect(listOrders.meta, same(opsOrderList));
      expect(
        createOrder,
        isA<MutationBinding<Object?, CreateOrderArgs, Object?>>(),
      );
      expect(createOrder.meta, same(opsOrderCreate));
    });

    test(
      'refuses to invent a cache rather than caching into a scratch one',
      () {
        expect(
          () => listOrders(const ListOrdersArgs()).getState(getClient()),
          throwsStateError,
        );
        expect(
          getClient,
          throwsA(
            isA<StateError>().having(
              (e) => e.message,
              'message',
              contains('no client configured'),
            ),
          ),
        );
      },
    );

    test(
      'runs the whole stack: subscribe, request, normalize, read back',
      () async {
        final (:http, scheduler: _, :cache) = wire(
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );
        final ref = listOrders(const ListOrdersArgs(status: 'open'));
        final seen = <QueryState<List<Object?>>>[];

        final subscription = ref.watch(cache).listen(seen.add);
        await settle();

        expect(
          http.calls[0].url.toString(),
          'https://api.test/orders?status=open',
        );
        expect(ref.getState(cache).dataOrNull, [
          {
            'id': 7,
            'total': 99,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ]);
        expect(seen, isNotEmpty);

        expect(listOrders(const ListOrdersArgs(status: 'open')).key, ref.key);
        expect(
          listOrders(const ListOrdersArgs(status: 'closed')).key,
          isNot(ref.key),
        );

        await subscription.cancel();
      },
    );

    test('runs a mutation, then refetches the query it invalidated', () async {
      final (:http, :scheduler, :cache) = wire(
        (request, _) => request.method == 'POST'
            ? {'id': 9, 'total': 5}
            : [
                {'id': 7, 'total': 99},
              ],
      );
      final subscription = listOrders(const ListOrdersArgs())
          .watch(cache)
          .listen((_) {});

      await settle();
      final before = http.calls.length;

      expect(await createOrder(cache, const CreateOrderArgs(total: 5)), {
        'id': 9,
        'total': 5,
      });

      scheduler.flush();
      await settle();

      expect(http.calls, hasLength(before + 2));
      expect(http.calls[before].method, 'POST');
      expect(http.calls[before + 1].method, 'GET');

      await subscription.cancel();
    });

    test('retries the query on a 500 and not the mutation', () async {
      var failGet = true;
      final (:http, scheduler: _, :cache) = wire((request, _) {
        if (request.method == 'GET' && failGet) {
          failGet = false;

          throw HttpFailure(500);
        }

        if (request.method == 'POST') throw HttpFailure(500);

        return [
          {'id': 7, 'total': 99},
        ];
      });

      expect(await listOrders(const ListOrdersArgs()).fetch(cache), [
        {'id': 7, 'total': 99},
      ]);
      expect(http.calls.where((call) => call.method == 'GET'), hasLength(2));

      await expectLater(
        createOrder(cache, const CreateOrderArgs(total: 0)),
        throwsA(httpError(500)),
      );
      expect(http.calls.where((call) => call.method == 'POST'), hasLength(1));
    });

    test(
      'takes an explicit cache in preference to the configured default',
      () async {
        final (http: _, scheduler: _, cache: fallback) = wire(
          (_, _) => <Object?>[],
        );
        final isolated = configureClient(
          transport: FakeTransport(
            (_, _) => [
              {'id': 1, 'total': 1},
            ],
          ),
          entities: schema,
        );

        setClient(fallback);

        await listOrders(const ListOrdersArgs()).fetch(isolated);

        expect(isolated.store.has('Order:1'), isTrue);
        expect(fallback.store.size, 0);
      },
    );
  });

  // getServerState is the snapshot React's useSyncExternalStore needs for
  // server rendering. Dart has no server render; adapters read getState.
  group('the server snapshot', () {
    const reason =
        'getServerState is a React SSR seam; Dart adapters read getState';

    test(
      'is idle for a query the cache has nothing for, and opens no record',
      () {},
      skip: reason,
    );
    test(
      'is the same object every call, so useSyncExternalStore does not tear',
      () {},
      skip: reason,
    );
    test(
      'returns what the cache holds once it holds something',
      () {},
      skip: reason,
    );
  });

  group('per-call staleTime', () {
    test(
      'passes a per-call staleTime from the binding through to the cache',
      () async {
        final transport = FakeTransport(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );
        final queries = QueryCache(
          transport: transport,
          entities: schema,
          staleTime: const Duration(minutes: 1),
        );

        final subscription = listOrders(const ListOrdersArgs())
            .watch(queries, staleTime: const Duration(milliseconds: 250))
            .listen((_) {});

        expect(
          queries.effectiveStaleTime(opsOrderList, TagContext.empty),
          const Duration(milliseconds: 250),
        );

        await subscription.cancel();
      },
    );
  });
}
