// Ported from packages/client-core/__tests__/ordering.test.ts.
//
// Every cache here runs on the default microtask scheduler, so the
// interleaving is driven by future ordering rather than an explicit flush.
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

({FakeTransport transport, QueryCache cache}) rig(
  FutureOr<Object?> Function(TransportRequest request, int call) handler,
) {
  final transport = FakeTransport(handler);

  return (
    transport: transport,
    cache: QueryCache(transport: transport, entities: schema),
  );
}

/// A transport answering from a fixed queue of futures, so the number of hops
/// between dispatch and arrival is fixed and visible.
final class QueuedTransport implements Transport {
  QueuedTransport(this.responses);

  final List<Future<Object?>> responses;
  final List<TransportRequest> calls = [];

  @override
  Future<Object?> execute(TransportRequest request) {
    calls.add(request);

    return responses.removeAt(0);
  }
}

/// A transport that throws synchronously, as a non-async implementation can.
final class ThrowingTransport implements Transport {
  int calls = 0;

  @override
  Future<Object?> execute(TransportRequest request) {
    calls++;

    throw StateError('sync boom');
  }
}

void main() {
  group('a response that predates a write never commits', () {
    test('does not let a pre-create refetch overwrite a placed list', () async {
      final pending = Completer<Object?>();
      final (:cache, :transport) = rig((request, call) {
        if (identical(request.meta, orderCreate)) return {'id': 9, 'total': 5};
        if (call == 1) return pending.future;

        return [
          {'id': 7, 'total': 99},
        ];
      });

      cache.subscribe(orderList, none, () {});
      await settle();
      expect(cache.getState(orderList, none).dataOrNull, [
        {'id': 7, 'total': 99},
      ]);

      final refetching = cache.refetch(orderList, none);
      await settle();
      expect(transport.calls, hasLength(2));

      await cache.mutate(
        orderCreate,
        const TagContext(body: {'total': 5}),
        options: MutateOptions(
          place: {
            'Order[]': (created, current, _) => [
              created,
              ...current! as List<Object?>,
            ],
          },
        ),
      );

      expect(cache.getState(orderList, none).dataOrNull, [
        {'id': 9, 'total': 5},
        {'id': 7, 'total': 99},
      ]);

      pending.complete([
        {'id': 7, 'total': 99},
      ]);
      await settle();

      expect(cache.getState(orderList, none).dataOrNull, [
        {'id': 9, 'total': 5},
        {'id': 7, 'total': 99},
      ]);

      // Discarded, not restarted.
      expect(transport.calls, hasLength(3));

      expect(await refetching, [
        {'id': 9, 'total': 5},
        {'id': 7, 'total': 99},
      ]);
    });

    test(
      'does not let a pre-write refetch clobber the mutation’s own write',
      () async {
        final pending = Completer<Object?>();
        final transport = QueuedTransport([
          Future<Object?>.value([
            {'id': 7, 'total': 1},
          ]),
          pending.future,
          Future<Object?>.value({'id': 7, 'total': 100}),
          Future<Object?>.value([
            {'id': 7, 'total': 100},
          ]),
          Future<Object?>.value([
            {'id': 7, 'total': 100},
          ]),
        ]);
        final cache = QueryCache(transport: transport, entities: schema);
        final totals = <Object?>[];

        cache.subscribe(orderList, none, () {
          final record = cache.store.getRecord('Order:7');

          if (record != null) totals.add(record.data['total']);
        });

        await settle();

        final refetching = cache.refetch(orderList, none);
        await settle();
        expect(transport.calls, hasLength(2));

        final mutating = cache.mutate(
          orderPatch,
          const TagContext(path: {'id': 7}),
        );
        pending.complete([
          {'id': 7, 'total': 1},
        ]);

        await mutating;
        expect(cache.store.getRecord('Order:7')?.data['total'], 100);

        final seenBeforeTheRace = totals.length;

        await settle();

        expect(cache.store.getRecord('Order:7')?.data['total'], 100);
        expect(totals.skip(seenBeforeTheRace), isNot(contains(1)));
        expect(cache.getState(orderList, none).dataOrNull, [
          {'id': 7, 'total': 100},
        ]);

        expect(transport.calls, hasLength(4));

        expect(await refetching, [
          {'id': 7, 'total': 100},
        ]);
      },
    );

    test('still refetches normally when nothing was in flight', () async {
      final (:cache, :transport) = rig(
        (request, _) => identical(request.meta, orderPatch)
            ? {'id': 7, 'total': 100}
            : [
                {'id': 7, 'total': 100},
              ],
      );

      cache.subscribe(orderList, none, () {});
      await settle();
      expect(transport.calls, hasLength(1));

      await cache.mutate(orderPatch, const TagContext(path: {'id': 7}));
      await settle();

      expect(transport.calls.map((call) => call.meta).toList(), [
        orderList,
        orderPatch,
        orderList,
      ]);
    });
  });

  // Review focus: a query dispatched after a mutation that lands before it.
  group('a mutation response racing a query response', () {
    test('a query answer that lands before a slower mutation is corrected by the mutation', () async {
      final write = Completer<Object?>();
      final (:cache, :transport) = rig((request, call) {
        if (identical(request.meta, orderPatch)) return write.future;

        // The first read is the initial load, the second races the write and
        // carries the pre-write value, the third is the refetch afterwards.
        return [
          {'id': 7, 'total': call < 3 ? 1 : 100},
        ];
      });

      cache.subscribe(orderList, none, () {});
      await settle();

      final mutating = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}),
      );
      await cache.refetch(orderList, none);

      expect(cache.store.getRecord('Order:7')?.data['total'], 1);

      write.complete({'id': 7, 'total': 100});
      await mutating;
      await settle();

      expect(cache.store.getRecord('Order:7')?.data['total'], 100);
      expect(cache.getState(orderList, none).dataOrNull, [
        {'id': 7, 'total': 100},
      ]);
      expect(transport.calls.map((call) => call.meta).toList(), [
        orderList,
        orderPatch,
        orderList,
        orderList,
      ]);
    });
  });

  group('a transport that throws synchronously', () {
    test(
      'does not install its rejection as the in-flight request forever',
      () async {
        final transport = ThrowingTransport();
        final cache = QueryCache(transport: transport, entities: schema);

        for (var attempt = 0; attempt < 6; attempt++) {
          await expectLater(cache.fetch(orderList, none), throwsStateError);
        }

        await expectLater(cache.refetch(orderList, none), throwsStateError);

        expect(transport.calls, 7);
        expect(cache.getState(orderList, none), isA<QueryFailure<Object?>>());
      },
    );
  });

  group('abandoning a request', () {
    test(
      'rejects rather than resolving undefined when the cache is cleared',
      () async {
        final pending = Completer<Object?>();
        final (:cache, transport: _) = rig((_, _) => pending.future);

        cache.setPrincipal('user-a');

        final fetching = cache.fetch(orderList, none);
        await settle();

        cache.setPrincipal('user-b');
        pending.complete([
          {'id': 7, 'total': 99},
        ]);

        await expectLater(fetching, throwsA(isA<RequestAbandoned>()));
        expect(cache.store.size, 0);
      },
    );
  });
}
