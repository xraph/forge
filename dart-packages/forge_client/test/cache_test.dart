// Ported from packages/client-core/__tests__/cache.test.ts.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

const orderList = OperationMeta(
  id: 'orderList',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);
const orderGet = OperationMeta(
  id: 'orderGet',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
);
const customerList = OperationMeta(
  id: 'customerList',
  method: 'GET',
  path: '/customers',
  entity: 'Customer',
  provides: ['Customer[]'],
);
const orderCreate = OperationMeta(
  id: 'orderCreate',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  invalidates: ['Order[]'],
);
const orderPatch = OperationMeta(
  id: 'orderPatch',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}'],
);

const none = TagContext.empty;

typedef Rig = ({
  FakeTransport transport,
  ManualScheduler scheduler,
  QueryCache cache,
});

Rig rig(
  FutureOr<Object?> Function(TransportRequest request, int call) handler,
) {
  final scheduler = ManualScheduler();
  final transport = FakeTransport(handler);

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

Object? dataOf(
  QueryCache cache,
  OperationMeta meta, [
  TagContext args = none,
]) => cache.getState(meta, args).dataOrNull;

/// A one-element list skeleton, built the way revive (plan 01b) builds one.
/// The mark is load-bearing: an unmarked container reads as itself.
Object skeletonOf(String key) => markRewritten(<Object?>[makeRef(key)]);

/// A [LiveBinding] that counts how often a query was made live and released.
final class CountingLive implements LiveBinding {
  int subscribes = 0;
  int releases = 0;

  @override
  void Function() subscribe(OperationMeta meta, TagContext args) {
    subscribes++;

    return () => releases++;
  }

  @override
  List<String> channelsFor(OperationMeta meta) => const [];

  @override
  void Function() raw(
    String channel,
    FrameHandler handler, [
    SubscribeOptions options = const SubscribeOptions(),
  ]) => () {};
}

void main() {
  group('running a query', () {
    test(
      'normalizes the response, records its dependencies, and reads it back',
      () async {
        final response = [
          {
            'id': 7,
            'total': 99,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
          {
            'id': 8,
            'total': 12,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ];
        final (:cache, transport: _, scheduler: _) = rig((_, _) => response);

        final value = await cache.fetch(orderList, none);

        expect(value, response);
        expect(cache.store.getRecord('Order:7')?.data['total'], 99);
        expect(cache.store.getRecord('Customer:c-3')?.data['name'], 'Ada');

        // The two orders share one customer object rather than two copies.
        final list = value! as List<Object?>;
        expect(
          (list[0]! as Map<String, Object?>)['customer'],
          same((list[1]! as Map<String, Object?>)['customer']),
        );

        final entry = cache.registry.get(cache.key(orderList, none));
        expect(entry?.deps, {'Order:7', 'Order:8', 'Customer:c-3'});
        expect(entry?.tags.contains('Order[]'), isTrue);
        expect(entry?.tags.contains('Order:7'), isTrue);
      },
    );

    test(
      'serves a second read from cache, with the same object identity',
      () async {
        final (:cache, :transport, scheduler: _) = rig(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        final first = await cache.fetch(orderList, none);
        final second = await cache.fetch(orderList, none);

        expect(second, same(first));
        expect(dataOf(cache, orderList), same(first));
        expect(
          cache.getState(orderList, none),
          same(cache.getState(orderList, none)),
        );
        expect(transport.calls, hasLength(1));
      },
    );

    test(
      'keeps the identity of an unchanged subtree across a refetch',
      () async {
        var total = 99;
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => [
            {
              'id': 7,
              'total': total,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );

        final first = await cache.fetch(orderList, none) as List<Object?>;
        final customer = (first[0]! as Map<String, Object?>)['customer'];

        total = 120;

        final second = await cache.refetch(orderList, none) as List<Object?>;

        expect(second, isNot(same(first)));
        expect(
          (second[0]! as Map<String, Object?>)['customer'],
          same(customer),
        );
      },
    );

    test(
      'keeps the container identity when a refetch returns identical data',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
            {
              'id': 8,
              'total': 1,
              'customer': {'id': 'c-4', 'name': 'Grace'},
            },
          ],
        );

        cache.subscribe(orderList, none, () {});
        await settle();

        final first = dataOf(cache, orderList);

        await cache.refetch(orderList, none);

        expect(dataOf(cache, orderList), same(first));
      },
    );

    test('reports pending, then success, to its subscribers', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = rig((_, _) => gate.future);
      final seen = <String>[];

      cache.subscribe(
        orderList,
        none,
        () => seen.add(cache.getState(orderList, none).runtimeType.toString()),
      );

      await settle();
      expect(cache.getState(orderList, none).isFetching, isTrue);

      gate.complete([
        {'id': 7},
      ]);
      await settle();

      expect(seen, ['QueryLoading<Object?>', 'QuerySuccess<Object?>']);
      expect(dataOf(cache, orderList), [
        {'id': 7},
      ]);
    });

    test('keeps the last good value when a refetch fails', () async {
      var fail = false;
      final (:cache, transport: _, scheduler: _) = rig((_, _) {
        if (fail) throw const HttpStatusError(500, null);

        return [
          {'id': 7, 'total': 99},
        ];
      });

      final good = await cache.fetch(orderList, none);

      fail = true;
      await expectLater(
        cache.refetch(orderList, none),
        throwsA(httpError(500)),
      );

      final state = cache.getState(orderList, none);
      expect(state, isA<QueryFailure<Object?>>());
      expect(state.dataOrNull, same(good));
    });
  });

  group('deduplication', () {
    test(
      'makes one request for N subscribers mounting the same query',
      () async {
        final gate = Completer<Object?>();
        final (:cache, :transport, scheduler: _) = rig((_, _) => gate.future);
        final calls = List<int>.filled(4, 0);

        for (var i = 0; i < calls.length; i++) {
          cache.subscribe(orderList, none, () => calls[i]++);
        }

        await settle();
        expect(transport.calls, hasLength(1));

        gate.complete([
          {'id': 7},
        ]);
        await settle();

        for (final count in calls) {
          expect(count, greaterThan(0));
        }
        expect(transport.calls, hasLength(1));
      },
    );

    test('treats differently-ordered arguments as one query', () async {
      final (:cache, :transport, scheduler: _) = rig((_, _) => <Object?>[]);

      await cache.fetch(
        orderList,
        const TagContext(query: {'status': 'open', 'page': 1}),
      );
      await cache.fetch(
        orderList,
        const TagContext(query: {'page': 1, 'status': 'open'}),
      );

      expect(transport.calls, hasLength(1));
      expect(cache.size, 1);
    });

    test('shares the whole retry sequence, never resolving anyone from a failed attempt', () async {
      final (:cache, :transport, scheduler: _) = rig((_, call) {
        if (call == 0) throw const HttpStatusError(500, null);

        return [
          {'id': 7},
        ];
      });

      final first = cache.fetch(orderList, none);
      final second = cache.fetch(orderList, none);

      await expectLater(first, throwsA(httpError(500)));
      await expectLater(second, throwsA(httpError(500)));
      expect(transport.calls, hasLength(1));

      expect(await cache.fetch(orderList, none), [
        {'id': 7},
      ]);
      expect(transport.calls, hasLength(2));
    });

    test('discards a response that predates an invalidation it was meant to reflect', () async {
      final gates = [Completer<Object?>(), Completer<Object?>()];
      final (:cache, transport: _, :scheduler) = rig(
        (_, call) => gates[call].future,
      );

      cache.subscribe(orderList, none, () {});
      await settle();

      cache.invalidate(['Order[]']);
      scheduler.flush();

      gates[0].complete([
        {'id': 7, 'total': 99},
      ]);
      await settle();

      expect(dataOf(cache, orderList), isNull);

      gates[1].complete([
        {'id': 7, 'total': 99},
        {'id': 8, 'total': 12},
      ]);
      await settle();

      expect(dataOf(cache, orderList), hasLength(2));
    });
  });

  group('mutations', () {
    test('settles, invalidates the queries that match, and leaves the others alone', () async {
      final (:cache, :transport, :scheduler) = rig(
        (request, _) => identical(request.meta, orderCreate)
            ? {'id': 9, 'total': 5}
            : <Object?>[],
      );

      cache.subscribe(orderList, none, () {});
      cache.subscribe(customerList, none, () {});
      await settle();

      final before = transport.calls.length;

      await cache.mutate(orderCreate, const TagContext(body: {'total': 5}));
      scheduler.flush();
      await settle();

      final refetched = transport.calls
          .skip(before + 1)
          .map((call) => call.meta)
          .toList();

      expect(refetched, [orderList]);
      expect(cache.store.getRecord('Order:9')?.data['total'], 5);
    });

    test(
      'reaches a list through the entity it displays, with no declared tag',
      () async {
        final (:cache, :transport, :scheduler) = rig(
          (request, _) => identical(request.meta, orderPatch)
              ? {'id': 7, 'total': 120}
              : [
                  {'id': 7, 'total': 99},
                ],
        );

        cache.subscribe(orderList, none, () {});
        await settle();

        final before = transport.calls.length;

        await cache.mutate(orderPatch, const TagContext(path: {'id': 7}));

        expect(cache.store.getRecord('Order:7')?.data['total'], 120);
        expect(dataOf(cache, orderList), [
          {'id': 7, 'total': 120},
        ]);

        scheduler.flush();
        await settle();

        expect(
          transport.calls.skip(before + 1).map((call) => call.meta).toList(),
          [orderList],
        );
      },
    );

    test(
      'keeps depending on a nested entity a narrower refetch left out',
      () async {
        var call = 0;
        final (:cache, :transport, :scheduler) = rig(
          (_, _) => call++ == 0
              ? {
                  'id': 7,
                  'customer': {'id': 3, 'name': 'a'},
                }
              : {'id': 7},
        );
        const args = TagContext(path: {'id': 7});

        cache.subscribe(orderGet, args, () {});
        await settle();
        await cache.refetch(orderGet, args);
        await settle();

        expect(dataOf(cache, orderGet, args), {
          'id': 7,
          'customer': {'id': 3, 'name': 'a'},
        });

        final before = transport.calls.length;

        cache.invalidate(['Customer:3']);
        scheduler.flush();
        await settle();

        expect(transport.calls.skip(before).map((c) => c.meta).toList(), [
          orderGet,
        ]);
      },
    );

    List<Object?> prepend(Object? created, Object? current, TagContext _) => [
      created,
      ...current! as List<Object?>,
    ];

    test(
      'lets a placement callback answer for a query instead of refetching it',
      () async {
        final (:cache, :transport, :scheduler) = rig(
          (request, _) => identical(request.meta, orderCreate)
              ? {'id': 9, 'total': 5}
              : [
                  {'id': 7, 'total': 99},
                ],
        );

        cache.subscribe(orderList, none, () {});
        await settle();

        final before = transport.calls.length;

        await cache.mutate(
          orderCreate,
          const TagContext(body: {'total': 5}),
          options: MutateOptions(place: {'Order[]': prepend}),
        );
        scheduler.flush();
        await settle();

        expect(transport.calls, hasLength(before + 1));
        expect(dataOf(cache, orderList), [
          {'id': 9, 'total': 5},
          {'id': 7, 'total': 99},
        ]);

        cache.store.put('Order:9', {'total': 6});
        expect(
          ((dataOf(cache, orderList)! as List<Object?>)[0]!
              as Map<String, Object?>)['total'],
          6,
        );
      },
    );

    test('indexes a placed query under the entities it placed', () async {
      final (:cache, :transport, :scheduler) = rig(
        (request, _) => identical(request.meta, orderCreate)
            ? {'id': 9, 'total': 5}
            : [
                {'id': 7, 'total': 99},
              ],
      );

      cache.subscribe(orderList, none, () {});
      await settle();

      await cache.mutate(
        orderCreate,
        const TagContext(body: {'total': 5}),
        options: MutateOptions(place: {'Order[]': prepend}),
      );
      scheduler.flush();
      await settle();

      final before = transport.calls.length;

      cache.invalidate(['Order:9']);
      scheduler.flush();
      await settle();

      expect(transport.calls.skip(before).map((call) => call.meta).toList(), [
        orderList,
      ]);
    });

    test('clears isFetching when placement answers a query with a request in flight', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, :scheduler) = rig(
        (request, _) => identical(request.meta, orderCreate)
            ? {'id': 9, 'total': 5}
            : gate.future,
      );

      cache.subscribe(orderList, none, () {});
      await settle();

      expect(cache.getState(orderList, none).isFetching, isTrue);

      await cache.mutate(
        orderCreate,
        const TagContext(body: {'total': 5}),
        options: MutateOptions(
          place: {
            'Order[]': (created, current, _) => [
              created,
              ...?current as List<Object?>?,
            ],
          },
        ),
      );
      scheduler.flush();
      await settle();

      expect(cache.getState(orderList, none).isFetching, isFalse);
      expect(dataOf(cache, orderList), [
        {'id': 9, 'total': 5},
      ]);

      gate.complete([
        {'id': 7, 'total': 1},
      ]);
      await settle();

      expect(dataOf(cache, orderList), [
        {'id': 9, 'total': 5},
      ]);
    });

    test('falls back to a refetch when placement declines', () async {
      final (:cache, :transport, :scheduler) = rig(
        (request, _) => identical(request.meta, orderCreate)
            ? {'id': 9, 'total': 5, 'status': 'draft'}
            : <Object?>[],
      );

      cache.subscribe(
        orderList,
        const TagContext(query: {'status': 'open'}),
        () {},
      );
      await settle();

      final before = transport.calls.length;

      await cache.mutate(
        orderCreate,
        const TagContext(body: {'total': 5}),
        options: MutateOptions(
          place: {
            'Order[]': (created, current, args) =>
                args.query['status'] ==
                    (created! as Map<String, Object?>)['status']
                ? [created, ...current! as List<Object?>]
                : null,
          },
        ),
      );
      scheduler.flush();
      await settle();

      expect(transport.calls, hasLength(before + 2));
    });

    test('notifies a query whose entity the write changed but whose tags it never named', () async {
      var written = 776;
      final (:cache, transport: _, :scheduler) = rig((request, _) {
        if (identical(request.meta, orderCreate)) {
          return {'id': 9, 'total': ++written};
        }

        return identical(request.meta, orderGet)
            ? {'id': 9, 'total': 1}
            : [
                {'id': 'c-3', 'name': 'Ada'},
              ];
      });

      var detail = 0;
      var unrelated = 0;
      const nine = TagContext(path: {'id': 9});

      cache.subscribe(orderGet, nine, () => detail++);
      cache.subscribe(customerList, none, () => unrelated++);
      await settle();

      expect(dataOf(cache, orderGet, nine), {'id': 9, 'total': 1});

      final settled = detail;

      await cache.mutate(
        orderCreate,
        const TagContext(body: <String, Object?>{}),
      );

      expect(detail, greaterThan(settled));
      expect(dataOf(cache, orderGet, nine), {'id': 9, 'total': 777});

      final reported = detail;

      cache.notifyChanged();
      scheduler.flush();
      await settle();

      expect(detail, reported);

      final before = unrelated;
      final beforeDetail = detail;

      await cache.mutate(
        orderCreate,
        const TagContext(body: <String, Object?>{}),
      );

      expect(detail, greaterThan(beforeDetail));
      expect(unrelated, before);
      expect(dataOf(cache, orderGet, nine), {'id': 9, 'total': 778});
    });
  });

  group('identity partitioning', () {
    test('drops the store on an identity change, and refetches what is being watched', () async {
      Object? body = [
        {
          'id': 7,
          'total': 99,
          'customer': {'id': 'c-3', 'name': 'Ada'},
        },
      ];
      final (:cache, :transport, scheduler: _) = rig((_, _) => body);

      cache.setPrincipal('user-a');
      cache.subscribe(orderList, none, () {});
      await settle();

      expect(cache.store.has('Order:7'), isTrue);
      expect(dataOf(cache, orderList), hasLength(1));

      body = [
        {'id': 42, 'total': 1},
      ];
      cache.setPrincipal('user-b');

      expect(cache.store.has('Order:7'), isFalse);
      expect(cache.store.has('Customer:c-3'), isFalse);
      expect(cache.store.size, 0);
      expect(cache.registry.size, 1);
      expect(dataOf(cache, orderList), isNull);

      await settle();

      expect(transport.calls, hasLength(2));
      expect(dataOf(cache, orderList), [
        {'id': 42, 'total': 1},
      ]);
      expect(cache.store.has('Order:7'), isFalse);
    });

    test(
      'drops an in-flight response for the principal that went away',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = rig(
          (_, call) => call == 0
              ? gate.future
              : [
                  {'id': 42},
                ],
        );

        cache.setPrincipal('user-a');
        unawaited(
          cache
              .fetch(orderList, none)
              .then<Object?>((value) => value, onError: (Object _) => null),
        );
        await settle();

        cache.setPrincipal('user-b');

        gate.complete([
          {'id': 7, 'total': 99},
        ]);
        await settle();

        expect(cache.store.has('Order:7'), isFalse);
        expect(cache.store.size, 0);
      },
    );

    test('does nothing when the principal has not actually changed', () async {
      final (:cache, :transport, scheduler: _) = rig(
        (_, _) => [
          {'id': 7},
        ],
      );

      cache.setPrincipal('user-a');
      await cache.fetch(orderList, none);
      cache.setPrincipal('user-a');

      expect(cache.store.has('Order:7'), isTrue);
      expect(transport.calls, hasLength(1));
    });

    // Dart-only: the stream face of watchPrincipal.
    test('announces the new principal after the cache was emptied', () async {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {'id': 7},
        ],
      );
      final seen = <(String?, int)>[];

      await cache.fetch(orderList, none);
      cache.principalChanges.listen(
        (principal) => seen.add((principal, cache.store.size)),
      );

      cache.setPrincipal('user-b');

      expect(seen, [('user-b', 0)]);
    });
  });

  group('prefetching', () {
    ({FakeTransport transport, QueryCache cache}) live(
      FutureOr<Object?> Function(TransportRequest request, int call) handler,
    ) {
      final transport = FakeTransport(handler);

      return (
        transport: transport,
        cache: QueryCache(transport: transport, entities: schema),
      );
    }

    test(
      'refetches a prefetch whose response predates a mutation, once it mounts',
      () async {
        final gate = Completer<Object?>();
        final (:cache, :transport) = live(
          (request, call) => identical(request.meta, orderCreate)
              ? {'id': 9, 'total': 5}
              : call == 0
              ? gate.future
              : [
                  {'id': 7, 'total': 99},
                  {'id': 9, 'total': 5},
                ],
        );

        final prefetch = cache.fetch(orderList, none);
        await settle();

        expect(transport.calls, hasLength(1));

        await cache.mutate(orderCreate, const TagContext(body: {'total': 5}));

        gate.complete([
          {'id': 7, 'total': 99},
        ]);
        await prefetch;
        await settle();

        cache.subscribe(orderList, none, () {});
        await settle();

        expect(dataOf(cache, orderList), [
          {'id': 7, 'total': 99},
          {'id': 9, 'total': 5},
        ]);
      },
    );

    test('does not serve a cached prefetch that predates a mutation', () async {
      final gate = Completer<Object?>();
      final (:cache, :transport) = live(
        (request, call) => identical(request.meta, orderCreate)
            ? {'id': 9, 'total': 5}
            : call == 0
            ? gate.future
            : [
                {'id': 7, 'total': 99},
                {'id': 9, 'total': 5},
              ],
      );

      final prefetch = cache.fetch(orderList, none);
      await settle();

      await cache.mutate(orderCreate, const TagContext(body: {'total': 5}));

      gate.complete([
        {'id': 7, 'total': 99},
      ]);
      await prefetch;
      await settle();

      expect(await cache.fetch(orderList, none), [
        {'id': 7, 'total': 99},
        {'id': 9, 'total': 5},
      ]);
      expect(transport.calls, hasLength(3));
    });
  });

  group('bounded memory', () {
    test('forgets the least recently used unwatched queries, and never a watched one', () async {
      final transport = FakeTransport((_, _) => {'id': 1});
      final cache = QueryCache(
        transport: transport,
        entities: schema,
        scheduler: ManualScheduler(),
        limit: 3,
      );

      final keep = cache.subscribe(
        orderGet,
        const TagContext(path: {'id': 0}),
        () {},
      );
      await settle();

      for (var id = 1; id <= 10; id++) {
        final release = cache.subscribe(
          orderGet,
          TagContext(path: {'id': id}),
          () {},
        );
        await settle();
        release();
      }

      expect(cache.size, lessThanOrEqualTo(3));
      expect(cache.registry.size, lessThanOrEqualTo(3));
      expect(
        cache.getState(orderGet, const TagContext(path: {'id': 0})),
        isA<QuerySuccess<Object?>>(),
      );

      keep();
    });
  });

  group('entity collection', () {
    test('drops a record no cached query reaches', () async {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {
            'id': 7,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ],
      );

      await cache.fetch(orderList, none);

      cache.store.put('Order:999', {'id': 999, 'total': 1});

      expect(cache.collect(), 1);
      expect(cache.store.getRecord('Order:999'), isNull);
      expect(cache.store.getRecord('Order:7'), isNotNull);
      expect(cache.store.getRecord('Customer:c-3'), isNotNull);
    });

    test(
      'keeps everything a cached query still reaches, watched or not',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => [
            {
              'id': 7,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
            {
              'id': 8,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );

        await cache.fetch(orderList, none);

        expect(cache.collect(), 0);
        expect(cache.store.getRecord('Customer:c-3'), isNotNull);
      },
    );

    test('keeps a record only an optimistic overlay holds', () async {
      final (:cache, transport: _, scheduler: _) = rig((_, _) => {'id': 1});

      cache.store.put('Order:1', {'id': 1, 'total': 1});
      unawaited(
        cache
            .mutate(
              orderPatch,
              const TagContext(path: {'id': 1}),
              options: MutateOptions(
                optimistic: OptimisticUpdate(
                  (_) => {
                    'patch': {'total': 5},
                  },
                ),
              ),
            )
            .then<Object?>((value) => value, onError: (Object _) => null),
      );

      expect(cache.collect(), 0);
      expect(cache.store.getRecord('Order:1'), isNotNull);
    });

    test('collects when the query cap reaps an unwatched query', () async {
      final transport = FakeTransport(
        (request, _) => {
          'id': request.args.path['id'],
          'customer': {'id': 'c-${request.args.path['id']}', 'name': 'Ada'},
        },
      );
      final cache = QueryCache(
        transport: transport,
        entities: schema,
        scheduler: ManualScheduler(),
        limit: 3,
      );

      for (var id = 1; id <= 10; id++) {
        final release = cache.subscribe(
          orderGet,
          TagContext(path: {'id': id}),
          () {},
        );
        await settle();
        release();
      }

      expect(cache.size, lessThanOrEqualTo(3));
      expect(cache.store.size, lessThanOrEqualTo(6));
    });
  });

  group('tombstone expiry', () {
    test(
      'drops a tombstone once nothing that predates it is still in flight',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (request, _) => identical(request.meta, orderList)
              ? [
                  {'id': 55},
                ]
              : {'id': 1},
        );

        await cache.fetch(orderGet, const TagContext(path: {'id': 1}));

        cache.store.nextFrame();
        cache.store.evict('Order:1', cache.store.frameVersion);

        expect(cache.store.tombstones, 1);

        await cache.fetch(orderList, none);

        expect(cache.store.tombstones, 0);
      },
    );

    test(
      'keeps a tombstone while a request that predates the delete is out',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = rig((request, _) {
          if (identical(request.meta, orderList)) return gate.future;
          if (identical(request.meta, orderPatch)) return {'id': 55};

          return {'id': 1};
        });

        await cache.fetch(orderGet, const TagContext(path: {'id': 1}));

        final outstanding = cache.fetch(orderList, none);
        await settle();

        cache.store.nextFrame();
        cache.store.evict('Order:1', cache.store.frameVersion);

        expect(cache.store.tombstones, 1);

        await cache.mutate(orderPatch, const TagContext(path: {'id': 1}));

        expect(cache.store.tombstones, 1);

        gate.complete(<Object?>[]);
        await outstanding;

        expect(cache.store.tombstones, 0);
      },
    );
  });

  group('peek', () {
    test('returns undefined for a query the cache has never opened, and opens nothing', () {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      expect(cache.peek(orderList, none), isNull);
      expect(cache.size, 0);
    });

    test(
      'returns the same state object as getState once a record exists',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await cache.fetch(orderList, none);

        expect(
          cache.peek(orderList, none),
          same(cache.getState(orderList, none)),
        );
      },
    );

    test(
      'is referentially stable across calls while nothing changes',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await cache.fetch(orderList, none);

        expect(cache.peek(orderList, none), same(cache.peek(orderList, none)));
      },
    );
  });

  group('settledQueries', () {
    test('lists only the queries that settled successfully', () async {
      final (:cache, transport: _, scheduler: _) = rig((request, _) {
        if (identical(request.meta, customerList)) {
          throw const HttpStatusError(500, null);
        }

        return [
          {'id': 7, 'total': 99},
        ];
      });

      await cache.fetch(orderList, none);
      await cache
          .fetch(customerList, none)
          .then<Object?>((value) => value, onError: (Object _) => null);
      cache.getState(orderGet, const TagContext(path: {'id': 1}));

      expect(cache.queries.map((query) => query.key), [
        cache.key(orderList, none),
      ]);
    });

    test('reports the skeleton the store holds, not the response', () async {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await cache.fetch(orderList, none);

      expect(cache.queries.first.skeleton, [refTo('Order:7')]);
    });
  });

  group('restore', () {
    test('settles a query from a skeleton with no request', () {
      final (:cache, :transport, scheduler: _) = rig(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      cache.store.put('Order:7', {'id': 7, 'total': 99});
      cache.restore(
        RestoreInput(
          meta: orderList,
          skeleton: skeletonOf('Order:7'),
          tags: const ['Order[]'],
        ),
      );

      expect(cache.getState(orderList, none), isA<QuerySuccess<Object?>>());
      expect(dataOf(cache, orderList), [
        {'id': 7, 'total': 99},
      ]);
      expect(transport.calls, isEmpty);
    });

    test(
      'records the skeleton dependencies, so a write to the entity is seen',
      () {
        final (:cache, transport: _, scheduler: _) = rig((_, _) => <Object?>[]);

        cache.store.put('Order:7', {'id': 7, 'total': 99});
        cache.restore(
          RestoreInput(
            meta: orderList,
            skeleton: skeletonOf('Order:7'),
            tags: const ['Order[]'],
          ),
        );

        expect(cache.registry.get(cache.key(orderList, none))?.deps.toList(), [
          'Order:7',
        ]);
      },
    );

    test('leaves the entry fresh by default and stale when asked', () {
      final (:cache, transport: _, scheduler: _) = rig((_, _) => <Object?>[]);

      cache.store.put('Order:7', {'id': 7, 'total': 99});
      cache.restore(
        RestoreInput(
          meta: orderList,
          skeleton: skeletonOf('Order:7'),
          tags: const ['Order[]'],
        ),
      );
      expect(cache.registry.get(cache.key(orderList, none))?.stale, isFalse);

      cache.restore(
        RestoreInput(
          meta: orderGet,
          args: const TagContext(path: {'id': 7}),
          skeleton: makeRef('Order:7'),
          tags: const ['Order:7'],
          stale: true,
        ),
      );
      expect(
        cache.registry
            .get(cache.key(orderGet, const TagContext(path: {'id': 7})))
            ?.stale,
        isTrue,
      );
    });

    test('notifies the subscribers of a query it settles', () {
      final (:cache, transport: _, scheduler: _) = rig((_, _) => <Object?>[]);
      var notified = 0;

      cache.store.put('Order:7', {'id': 7, 'total': 99});
      cache.subscribe(orderList, none, () => notified++);

      final before = notified;
      cache.restore(
        RestoreInput(
          meta: orderList,
          skeleton: skeletonOf('Order:7'),
          tags: const ['Order[]'],
        ),
      );

      expect(notified, greaterThan(before));
    });
  });

  group('restore, staleness', () {
    test('refetches on the first mount when hydrated stale', () async {
      final (:cache, :transport, :scheduler) = rig(
        (_, _) => [
          {'id': 7, 'total': 120},
        ],
      );

      cache.store.put('Order:7', {'id': 7, 'total': 99});
      cache.restore(
        RestoreInput(
          meta: orderList,
          skeleton: skeletonOf('Order:7'),
          tags: const ['Order[]'],
          stale: true,
        ),
      );

      cache.subscribe(orderList, none, () {});
      scheduler.flush();
      await settle();

      expect(transport.calls, hasLength(1));
    });

    test('does not refetch on the first mount when hydrated fresh', () async {
      final (:cache, :transport, :scheduler) = rig(
        (_, _) => [
          {'id': 7, 'total': 120},
        ],
      );

      cache.store.put('Order:7', {'id': 7, 'total': 99});
      cache.restore(
        RestoreInput(
          meta: orderList,
          skeleton: skeletonOf('Order:7'),
          tags: const ['Order[]'],
        ),
      );

      cache.subscribe(orderList, none, () {});
      scheduler.flush();
      await settle();

      expect(transport.calls, isEmpty);
    });
  });

  group('tracked', () {
    test(
      'maps a cache key back to its operation and its runtime state',
      () async {
        final transport = FakeTransport(
          (_, _) => [
            {'id': 1, 'total': 10},
          ],
        );
        final cache = QueryCache(transport: transport, entities: schema);

        await cache.fetch(orderList, none);

        final found = cache
            .tracked()
            .where((record) => record.key == cache.key(orderList, none))
            .firstOrNull;

        expect(found, isNotNull);
        expect(found?.meta.method, 'GET');
        expect(found?.meta.path, '/orders');
        expect(found?.status, QueryStatus.success);
        expect(found?.fetching, isFalse);
        expect(found?.settled, isTrue);
        expect(found?.frameRestarts, 0);
      },
    );

    test('reports an error without throwing it again', () async {
      final transport = FakeTransport((_, _) => throw StateError('nope'));
      final cache = QueryCache(transport: transport, entities: schema);

      await cache
          .fetch(orderList, none)
          .then<Object?>((value) => value, onError: (Object _) => null);

      final found = cache
          .tracked()
          .where((record) => record.key == cache.key(orderList, none))
          .firstOrNull;

      expect(found?.status, QueryStatus.error);
      expect('${found?.error}', contains('nope'));
    });
  });

  group('drop', () {
    test('forgets an unwatched query so the next fetch is cold', () async {
      final transport = FakeTransport(
        (_, _) => [
          {'id': 1, 'total': 10},
        ],
      );
      final cache = QueryCache(transport: transport, entities: schema);

      await cache.fetch(orderList, none);
      expect(transport.calls, hasLength(1));

      expect(cache.dropKey(cache.key(orderList, none)), isTrue);
      expect(cache.tracked(), isEmpty);

      await cache.fetch(orderList, none);
      expect(transport.calls, hasLength(2));
    });

    test('resets a watched query in place and refetches it', () async {
      final transport = FakeTransport(
        (_, _) => [
          {'id': 1, 'total': 10},
        ],
      );
      final cache = QueryCache(transport: transport, entities: schema);
      final stop = cache.subscribe(orderList, none, () {});

      await settle();
      expect(transport.calls, hasLength(1));

      expect(cache.drop(orderList, none), isTrue);
      await settle();

      expect(cache.tracked(), hasLength(1));
      expect(transport.calls, hasLength(2));
      expect(cache.getState(orderList, none), isA<QuerySuccess<Object?>>());

      stop();
    });

    test('answers false for a key it has never heard of', () {
      final cache = QueryCache(
        transport: FakeTransport((_, _) => <Object?>[]),
        entities: schema,
      );

      expect(cache.dropKey('GET /nothing'), isFalse);
    });

    test('abandons an in-flight fetch so the response never lands', () async {
      final gate = Completer<Object?>();
      final transport = FakeTransport((_, _) => gate.future);
      final cache = QueryCache(transport: transport, entities: schema);

      final fetching = cache.fetch(orderList, none);

      await settle();
      expect(transport.calls, hasLength(1));

      expect(cache.dropKey(cache.key(orderList, none)), isTrue);
      expect(cache.tracked(), isEmpty);

      gate.complete([
        {'id': 1, 'total': 10},
      ]);

      await expectLater(fetching, throwsA(isA<RequestAbandoned>()));
    });
  });

  // Review focus: inputs the spec implies and the TS suites do not exercise.
  group('review focus', () {
    test('two queries that 401 together share one credential refresh and both settle', () async {
      var token = 't0';
      var refreshes = 0;
      final gate = Completer<void>();
      final fake = FakeHttp((request, _) {
        if (request.headers['Authorization'] != 'Bearer t1') {
          throw HttpFailure(401);
        }

        return request.url.path == '/orders'
            ? [
                {'id': 7},
              ]
            : [
                {'id': 'c-3'},
              ];
      });
      final cache = QueryCache(
        transport: RestTransport(
          baseUrl: base,
          client: fake.client,
          auth: AuthProvider.callbacks(
            credentials: (_) => {'Authorization': 'Bearer $token'},
            refresh: () async {
              refreshes++;
              await gate.future;
              token = 't1';
            },
          ),
        ),
        entities: schema,
      );

      cache.subscribe(orderList, none, () {});
      cache.subscribe(customerList, none, () {});
      await settle();

      expect(refreshes, 1);

      gate.complete();
      await settle();

      expect(refreshes, 1);
      expect(cache.getState(orderList, none), isA<QuerySuccess<Object?>>());
      expect(cache.getState(customerList, none), isA<QuerySuccess<Object?>>());
      expect(fake.calls, hasLength(4));
    });

    test(
      'a watched query survives eviction pressure and keeps receiving writes',
      () async {
        final transport = FakeTransport(
          (request, _) => identical(request.meta, orderList)
              ? [
                  {'id': 7, 'total': 1},
                ]
              : {'id': request.args.path['id'], 'total': 0},
        );
        final cache = QueryCache(
          transport: transport,
          entities: schema,
          scheduler: ManualScheduler(),
          limit: 1,
        );
        final seen = <QueryState<Object?>>[];

        final subscription = cache.watch(orderList, none).listen(seen.add);
        await settle();

        for (var id = 100; id < 120; id++) {
          await cache.fetch(orderGet, TagContext(path: {'id': id}));
        }

        expect(cache.size, lessThanOrEqualTo(2));
        expect(cache.store.has('Order:7'), isTrue);

        cache.store.put('Order:7', {'total': 2});
        cache.notifyChanged();

        expect(seen.last.dataOrNull, [
          {'id': 7, 'total': 2},
        ]);

        await subscription.cancel();
      },
    );
  });

  // Review focus: TS compares `previous.data === data`, which is by value for a
  // string. `identical` would call two equal strings from two responses
  // different and rebuild the state.
  group('scalar data', () {
    const label = OperationMeta(id: 'label', method: 'GET', path: '/label');

    test(
      'keeps the state object when a refetch returns an equal string',
      () async {
        final (:cache, transport: _, scheduler: _) = rig(
          // A fresh String each call, so the two answers are equal but never
          // the same object.
          (_, _) => String.fromCharCodes('ready'.codeUnits),
        );

        final first = await cache.fetch(label, none);
        final before = cache.getState(label, none);

        final second = await cache.refetch(label, none);

        expect(second, first);
        expect(cache.getState(label, none), same(before));
      },
    );

    test(
      'keeps the state object when a refetch returns an equal number',
      () async {
        var call = 0;
        final (:cache, transport: _, scheduler: _) = rig(
          (_, _) => call++ == 0 ? 1 : 1.0,
        );

        await cache.fetch(label, none);
        final before = cache.getState(label, none);

        await cache.refetch(label, none);

        expect(cache.getState(label, none), same(before));
      },
    );

    test('still builds a new state when the scalar changed', () async {
      var call = 0;
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => call++ == 0 ? 'a' : 'b',
      );

      await cache.fetch(label, none);
      final before = cache.getState(label, none);

      await cache.refetch(label, none);

      final after = cache.getState(label, none);

      expect(after, isNot(same(before)));
      expect(after.dataOrNull, 'b');
    });
  });

  // Review focus: a query that failed before it ever settled keeps its error
  // while the retry is in flight, as TS does. The state must stay one object.
  group('a failed, never-settled query retrying', () {
    test('keeps one state object while the retry is in flight', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = rig((_, call) {
        if (call == 0) throw const HttpStatusError(500, null);

        return gate.future;
      });
      var notified = 0;
      final seen = <QueryState<Object?>>[];

      await expectLater(cache.fetch(orderList, none), throwsA(httpError(500)));
      expect(cache.getState(orderList, none), isA<QueryFailure<Object?>>());

      cache.subscribe(orderList, none, () => notified++);

      final subscription = cache.watch(orderList, none).listen(seen.add);
      await settle();

      final loading = cache.getState(orderList, none);

      expect(loading, isA<QueryLoading<Object?>>());
      expect(loading.isFetching, isTrue);
      expect(cache.getState(orderList, none), same(loading));
      expect(seen.last, same(loading));

      final listenerCalls = notified;
      final events = seen.length;

      cache.notifyChanged();
      cache.notifyChanged();
      await settle();

      expect(notified, listenerCalls);
      expect(seen, hasLength(events));
      expect(cache.getState(orderList, none), same(loading));

      gate.complete([
        {'id': 7},
      ]);
      await settle();

      expect(cache.getState(orderList, none), isA<QuerySuccess<Object?>>());

      await subscription.cancel();
    });
  });

  // Review focus: a live watch is made live once and released once, whichever
  // of dispose and cancel reaches it first.
  group('releasing a live watch', () {
    ({QueryCache cache, CountingLive live}) liveRig() {
      final live = CountingLive();
      final cache = QueryCache(
        transport: FakeTransport(
          (_, _) => [
            {'id': 7},
          ],
        ),
        entities: schema,
      )..live = live;

      return (cache: cache, live: live);
    }

    test('releases once when dispose closes the stream', () async {
      final (:cache, :live) = liveRig();

      cache.watch(orderList, none, live: true).listen((_) {});
      await settle();

      expect(live.subscribes, 1);

      await cache.dispose();
      await settle();

      expect(live.subscribes, 1);
      expect(live.releases, 1);
    });

    test('releases once when the listener cancels before dispose', () async {
      final (:cache, :live) = liveRig();

      final subscription = cache
          .watch(orderList, none, live: true)
          .listen((_) {});
      await settle();
      await subscription.cancel();

      expect(live.releases, 1);

      await cache.dispose();
      await settle();

      expect(live.subscribes, 1);
      expect(live.releases, 1);
    });
  });

  // Dart-only: the stream face of subscribe, and the contract's additions.
  group('watch', () {
    test('emits the current state first, then every change, and releases on cancel', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = rig((_, _) => gate.future);
      final seen = <QueryState<Object?>>[];

      final subscription = cache.watch(orderList, none).listen(seen.add);
      await settle();

      expect(seen.single, isA<QueryLoading<Object?>>());
      expect(cache.registry.get(cache.key(orderList, none))?.mounts, 1);

      gate.complete([
        {'id': 7},
      ]);
      await settle();

      expect(seen.last, isA<QuerySuccess<Object?>>());
      expect(seen.last, same(cache.getState(orderList, none)));

      await subscription.cancel();

      expect(cache.registry.get(cache.key(orderList, none))?.mounts, 0);
    });

    test('never re-emits a state that did not change', () async {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {'id': 7},
        ],
      );
      final seen = <QueryState<Object?>>[];

      final subscription = cache.watch(orderList, none).listen(seen.add);
      await settle();
      final count = seen.length;

      cache.notifyChanged();
      cache.notifyChanged();
      await settle();

      expect(seen, hasLength(count));
      await subscription.cancel();
    });

    test(
      'reports live with no stream runtime attached, and still fetches',
      () async {
        final errors = <String>[];
        final transport = FakeTransport(
          (_, _) => [
            {'id': 7},
          ],
        );
        final cache = QueryCache(
          transport: transport,
          entities: schema,
          onError: (_, context) => errors.add(context),
        );

        final subscription = cache
            .watch(orderList, none, live: true)
            .listen((_) {});
        await settle();

        expect(errors, ['live']);
        expect(transport.calls, hasLength(1));
        await subscription.cancel();
      },
    );
  });

  group('invalidateQuery', () {
    test('refetches the one query it names when watched', () async {
      final (:cache, :transport, :scheduler) = rig((_, _) => <Object?>[]);

      cache.subscribe(orderList, none, () {});
      cache.subscribe(customerList, none, () {});
      await settle();

      final before = transport.calls.length;

      cache.invalidateQuery(orderList, none);
      scheduler.flush();
      await settle();

      expect(transport.calls.skip(before).map((call) => call.meta).toList(), [
        orderList,
      ]);
    });
  });

  group('commits', () {
    test('fires after a store commit', () async {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {'id': 7},
        ],
      );
      var commits = 0;

      cache.commits.listen((_) => commits++);
      await cache.fetch(orderList, none);
      await settle();

      expect(commits, greaterThan(0));
    });
  });

  group('dispose', () {
    test('closes every watch stream and refuses new work', () async {
      final (:cache, transport: _, scheduler: _) = rig(
        (_, _) => [
          {'id': 7},
        ],
      );
      var done = false;

      cache.watch(orderList, none).listen((_) {}, onDone: () => done = true);
      await settle();

      await cache.dispose();
      await settle();

      expect(done, isTrue);
      expect(() => cache.fetch(orderList, none), throwsStateError);
    });
  });
}
