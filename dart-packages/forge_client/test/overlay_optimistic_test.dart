// Ported from packages/client-core/__tests__/overlay.test.ts, part two: the
// optimistic cases that run through the query cache.
//
// TS shorthands map onto the sealed Optimistic family: a literal
// `{status: 'shipped'}` is `OptimisticUpdate((_) => {'status': 'shipped'})`
// (a merge), `'delete'` is `OptimisticDelete()`, a literal on a create is
// `OptimisticCreate({...})`, and the `[{key, patch}]` array is
// `OptimisticMany([...])`. The TS OPTIMISTIC symbol is `isOptimistic(record)`;
// Dart maps carry no hidden keys, so the plain `equals` checks below need no
// `objectContaining`.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

const none = TagContext.empty;

OptimisticUpdate<Object?> fields(Json patch) =>
    OptimisticUpdate<Object?>((_) => patch);

/// What plan 01b's `applyFrames` does for a `patch` frame with no
/// invalidates: normalize under a fresh frame stamp, then notify. Inlined here
/// because the frame applier is 01b's; 01b's frame-ordering port covers the
/// applier itself.
void applyPatchFrame(QueryCache cache, Object? payload) {
  final stamp = cache.store.nextFrame();

  cache.store.write(
    payload,
    cache.entities,
    'Order',
    CommitOptions(frameAt: stamp),
  );
  cache.notifyChanged();
}

const patchMeta = OperationMeta(
  id: 'orderPatch',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}', 'Order[]'],
);

const orderList = OperationMeta(
  id: 'orderList',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);
const orderPatch = patchMeta;
const orderGet = OperationMeta(
  id: 'orderGet',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
);
const orderDelete = OperationMeta(
  id: 'orderDelete',
  method: 'DELETE',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}', 'Order[]'],
);
const orderCreate = OperationMeta(
  id: 'orderCreate',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  invalidates: ['Order[]'],
);

typedef Rig = ({
  ManualScheduler scheduler,
  FakeTransport transport,
  QueryCache cache,
});

Rig optimisticCache(
  FutureOr<Object?> Function(TransportRequest request, int call) handler, {
  void Function(Object, String)? onError,
}) {
  final scheduler = ManualScheduler();
  final transport = FakeTransport(handler);

  return (
    scheduler: scheduler,
    transport: transport,
    cache: QueryCache(
      transport: transport,
      entities: schema,
      scheduler: scheduler,
      onError: onError,
    ),
  );
}

Object? dataOf(
  QueryCache cache,
  OperationMeta meta, [
  TagContext args = none,
]) => cache.getState(meta, args).dataOrNull;

List<Object?> prepend(Object? made, Object? current, TagContext _) => [
  made,
  ...current! as List<Object?>,
];

Future<Object?> swallow(Future<Object?> future) =>
    future.then<Object?>((value) => value, onError: (Object _) => null);

void main() {
  group('optimistic mutations', () {
    test(
      'shows the patch before the server answers, and keeps it after',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 7, 'status': 'open'},
                ]
              : gate.future,
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        final pending = cache.mutate(
          orderPatch,
          const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
          options: MutateOptions(optimistic: fields({'status': 'shipped'})),
        );

        expect(dataOf(cache, orderList), [
          {'id': 7, 'status': 'shipped'},
        ]);

        gate.complete({'id': 7, 'status': 'shipped'});
        await pending;

        expect(dataOf(cache, orderList), [
          {'id': 7, 'status': 'shipped'},
        ]);
        expect(
          isOptimistic((dataOf(cache, orderList)! as List<Object?>)[0]),
          isFalse,
        );
      },
    );

    test(
      'reverts on failure, raises no tags, and schedules no refetch',
      () async {
        final gate = Completer<Object?>();
        final (:cache, :scheduler, :transport) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 7, 'status': 'open'},
                ]
              : gate.future,
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});
        final calls = transport.calls.length;

        final pending = cache.mutate(
          orderPatch,
          const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
          options: MutateOptions(optimistic: fields({'status': 'shipped'})),
        );

        expect(dataOf(cache, orderList), [
          {'id': 7, 'status': 'shipped'},
        ]);

        gate.completeError(StateError('nope'));
        await expectLater(pending, throwsStateError);
        await settle();

        expect(dataOf(cache, orderList), [
          {'id': 7, 'status': 'open'},
        ]);
        expect(scheduler.pending, isFalse);
        expect(transport.calls, hasLength(calls + 1));
      },
    );

    test('keeps a later mutation when an EARLIER one fails', () async {
      final first = Completer<Object?>();
      final second = Completer<Object?>();
      var writes = 0;
      final (:cache, transport: _, scheduler: _) = optimisticCache((
        request,
        _,
      ) {
        if (request.meta.method == 'GET') {
          return [
            {'id': 7, 'status': 'open', 'note': ''},
          ];
        }

        writes++;

        return writes == 1 ? first.future : second.future;
      });

      await cache.fetch(orderList, none);
      cache.subscribe(orderList, none, () {});

      final a = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
        options: MutateOptions(optimistic: fields({'status': 'shipped'})),
      );
      final b = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}, body: {'note': 'gift'}),
        options: MutateOptions(optimistic: fields({'note': 'gift'})),
      );

      expect(dataOf(cache, orderList), [
        {'id': 7, 'status': 'shipped', 'note': 'gift'},
      ]);

      first.completeError(StateError('nope'));
      await expectLater(a, throwsStateError);
      await settle();

      expect(dataOf(cache, orderList), [
        {'id': 7, 'status': 'open', 'note': 'gift'},
      ]);

      second.complete({'id': 7, 'status': 'open', 'note': 'gift'});
      await b;
    });

    test(
      'does not flash a deleted row back when the server returns no content',
      () async {
        final gate = Completer<Object?>();
        final seen = <Object?>[];
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 7},
                  {'id': 8},
                ]
              : gate.future,
        );

        await cache.fetch(orderList, none);
        cache.subscribe(
          orderList,
          none,
          () => seen.add(dataOf(cache, orderList)),
        );

        final pending = cache.mutate(
          orderDelete,
          const TagContext(path: {'id': 7}),
          options: const MutateOptions(optimistic: OptimisticDelete()),
        );

        expect(dataOf(cache, orderList), [
          {'id': 8},
        ]);

        gate.complete(null);
        await pending;
        await settle();

        expect(dataOf(cache, orderList), [
          {'id': 8},
        ]);
        for (final value in seen) {
          expect(value, isNot(contains({'id': 7})));
        }
      },
    );

    test(
      'does not resurrect a deleted row from a body-returning DELETE',
      () async {
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 7},
                  {'id': 8},
                ]
              : {'id': 7},
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        await cache.mutate(
          orderDelete,
          const TagContext(path: {'id': 7}),
          options: const MutateOptions(optimistic: OptimisticDelete()),
        );

        expect(dataOf(cache, orderList), [
          {'id': 8},
        ]);
      },
    );

    test(
      'does not resurrect a confirmed delete from a read dispatched before it',
      () async {
        final gate = Completer<Object?>();
        var reads = 0;
        const listOnlyDelete = OperationMeta(
          id: 'orderDelete',
          method: 'DELETE',
          path: '/orders/{id}',
          entity: 'Order',
          invalidates: ['Order[]'],
        );
        final (:cache, :scheduler, transport: _) = optimisticCache((
          request,
          _,
        ) {
          if (request.meta.method == 'DELETE') return null;

          final read = reads++;

          if (read == 0) return {'id': 7, 'total': 1};
          if (read == 1) return gate.future;

          return null;
        });
        const args = TagContext(path: {'id': 7});

        cache.subscribe(orderGet, args, () {});
        await settle();

        unawaited(swallow(cache.refetch(orderGet, args)));

        await cache.mutate(
          listOnlyDelete,
          args,
          options: const MutateOptions(
            optimistic: OptimisticMany([OptimisticDelete(key: 'Order:7')]),
          ),
        );
        scheduler.flush();
        await settle();

        expect(cache.store.getRecord('Order:7'), isNull);

        gate.complete({'id': 7, 'total': 1});
        await settle();

        expect(cache.store.getRecord('Order:7'), isNull);
      },
    );

    test(
      'reports an ambiguous target and runs the mutation without an overlay',
      () async {
        final errors = <String>[];
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (_, _) => {'id': 7},
          onError: (_, context) => errors.add(context),
        );
        const transfer = OperationMeta(
          id: 't',
          method: 'POST',
          path: '/orders/{id}/transfer',
          entity: 'Order',
          invalidates: ['Order:{id}', 'Customer:{req.customerId}'],
        );

        await cache.mutate(
          transfer,
          const TagContext(path: {'id': 7}, body: {'customerId': 3}),
          options: MutateOptions(optimistic: fields({'status': 'moved'})),
        );

        expect(errors, contains('optimistic'));
        expect(cache.overlays.empty, isTrue);
      },
    );

    test('drops every overlay when the principal changes', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = optimisticCache(
        (request, _) => request.meta.method == 'GET'
            ? [
                {'id': 7, 'status': 'open'},
              ]
            : gate.future,
      );

      await cache.fetch(orderList, none);
      final pending = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
        options: MutateOptions(optimistic: fields({'status': 'shipped'})),
      );

      expect(cache.overlays.empty, isFalse);

      cache.setPrincipal('someone-else');

      expect(cache.overlays.empty, isTrue);

      gate.complete({'id': 7, 'status': 'shipped'});
      await swallow(pending);
    });

    test('keeps an in-flight overlay out of base when another mutation places over it', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = optimisticCache((
        request,
        _,
      ) {
        if (request.meta.method == 'GET') {
          return [
            {'id': 7, 'status': 'open'},
          ];
        }
        if (request.meta.method == 'PATCH') return gate.future;

        return {'id': 9, 'status': 'new'};
      });

      await cache.fetch(orderList, none);
      cache.subscribe(orderList, none, () {});

      final pending = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
        options: MutateOptions(optimistic: fields({'status': 'shipped'})),
      );

      await cache.mutate(
        orderCreate,
        none,
        options: const MutateOptions(place: {'Order[]': prepend}),
      );

      expect(cache.store.getRecord('Order:7')?.data, {
        'id': 7,
        'status': 'open',
      });

      gate.completeError(StateError('nope'));
      await expectLater(pending, throwsStateError);
      await settle();

      expect(cache.store.getRecord('Order:7')?.data, {
        'id': 7,
        'status': 'open',
      });
    });

    test(
      'declines placement for an optimistic delete instead of placing a hole',
      () async {
        var placed = 0;
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 7},
                  {'id': 8},
                ]
              : {'id': 7},
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        final result = await cache.mutate(
          orderDelete,
          const TagContext(path: {'id': 7}),
          options: MutateOptions(
            optimistic: const OptimisticDelete(),
            place: {
              'Order[]': (_, _, _) {
                placed++;

                return null;
              },
            },
          ),
        );

        expect(placed, 0);
        expect(result, {'id': 7});
        expect(dataOf(cache, orderList), [
          {'id': 8},
        ]);
      },
    );

    test(
      'discards a patch a stream frame overtook rather than promoting it',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 7, 'status': 'open'},
                ]
              : gate.future,
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        final pending = cache.mutate(
          orderPatch,
          const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
          options: MutateOptions(optimistic: fields({'status': 'shipped'})),
        );
        await settle();

        applyPatchFrame(cache, {
          'id': 7,
          'status': 'cancelled',
          'cancelledBy': 'ops',
        });

        gate.complete({'id': 7, 'status': 'shipped'});
        await pending;
        await settle();

        expect(cache.store.getRecord('Order:7')?.data, {
          'id': 7,
          'status': 'cancelled',
          'cancelledBy': 'ops',
        });
        expect(dataOf(cache, orderList), [
          {'id': 7, 'status': 'cancelled', 'cancelledBy': 'ops'},
        ]);
      },
    );

    test(
      'leaves a row a frame revived alive, rather than burying it on promotion',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 7, 'status': 'open'},
                  {'id': 8},
                ]
              : gate.future,
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        final pending = cache.mutate(
          orderDelete,
          const TagContext(path: {'id': 7}),
          options: const MutateOptions(optimistic: OptimisticDelete()),
        );

        expect(dataOf(cache, orderList), [
          {'id': 8},
        ]);

        await settle();
        applyPatchFrame(cache, {'id': 7, 'status': 'cancelled'});

        gate.complete({'id': 7});
        final result = await pending;
        await settle();

        expect(dataOf(cache, orderList), [
          {'id': 7, 'status': 'cancelled'},
          {'id': 8},
        ]);
        expect(result, {'id': 7, 'status': 'cancelled'});
      },
    );

    test('still dispatches and settles when a subscriber throws during the pre-dispatch push', () async {
      final (:cache, :transport, scheduler: _) = optimisticCache(
        (request, _) => request.meta.method == 'GET'
            ? [
                {'id': 7, 'status': 'open'},
              ]
            : {'id': 7, 'status': 'shipped'},
      );

      await cache.fetch(orderList, none);

      var notifications = 0;
      cache.subscribe(orderList, none, () {
        notifications++;

        if (notifications == 1) throw StateError('subscriber boom');
      });

      final result = await cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
        options: MutateOptions(optimistic: fields({'status': 'shipped'})),
      );

      expect(result, {'id': 7, 'status': 'shipped'});
      expect(
        transport.calls.any((call) => call.meta.method == 'PATCH'),
        isTrue,
      );
      expect(notifications, greaterThan(1));
    });
  });

  group('optimistic create', () {
    test(
      'places a temp row immediately and replaces it with the real one',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 8, 'total': 12},
                ]
              : gate.future,
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        final pending = cache.mutate(
          orderCreate,
          const TagContext(body: {'total': 99}),
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'total': 99}),
            place: {'Order[]': prepend},
          ),
        );

        expect(dataOf(cache, orderList), [
          {'id': '~opt1', 'total': 99},
          {'id': 8, 'total': 12},
        ]);

        gate.complete({'id': 9, 'total': 99});
        await pending;
        await settle();

        expect(dataOf(cache, orderList), [
          {'id': 9, 'total': 99},
          {'id': 8, 'total': 12},
        ]);
        expect(cache.store.has('Order:~opt1'), isFalse);
      },
    );

    test(
      'places concurrent creates in push order, and drops one cleanly',
      () async {
        final first = Completer<Object?>();
        final second = Completer<Object?>();
        var writes = 0;
        final (:cache, transport: _, scheduler: _) = optimisticCache((
          request,
          _,
        ) {
          if (request.meta.method == 'GET') {
            return [
              {'id': 8},
            ];
          }

          writes++;

          return writes == 1 ? first.future : second.future;
        });

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        final a = cache.mutate(
          orderCreate,
          const TagContext(body: {'total': 1}),
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'total': 1}),
            place: {'Order[]': prepend},
          ),
        );
        final b = cache.mutate(
          orderCreate,
          const TagContext(body: {'total': 2}),
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'total': 2}),
            place: {'Order[]': prepend},
          ),
        );

        expect(dataOf(cache, orderList), [
          {'id': '~opt2', 'total': 2},
          {'id': '~opt1', 'total': 1},
          {'id': 8},
        ]);

        first.completeError(StateError('nope'));
        await expectLater(a, throwsStateError);
        await settle();

        expect(dataOf(cache, orderList), [
          {'id': '~opt2', 'total': 2},
          {'id': 8},
        ]);

        second.complete({'id': 9, 'total': 2});
        await b;
      },
    );

    test('leaves an enveloped query alone and reports that it did', () async {
      final errors = <String>[];
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = optimisticCache(
        (request, _) => request.meta.method == 'GET'
            ? {
                'items': [
                  {'id': 8},
                ],
                'total': 1,
              }
            : gate.future,
        onError: (_, context) => errors.add(context),
      );
      const paged = OperationMeta(
        id: 'paged',
        method: 'GET',
        path: '/paged-orders',
        entity: 'Order',
        rootType: 'Envelope',
        provides: ['Order[]'],
      );

      await cache.fetch(paged, none);
      cache.subscribe(paged, none, () {});

      final pending = cache.mutate(
        orderCreate,
        const TagContext(body: {'total': 99}),
        options: const MutateOptions(
          optimistic: OptimisticCreate<Object?>({'total': 99}),
          place: {'Order[]': prepend},
        ),
      );

      expect(dataOf(cache, paged), {
        'items': [
          {'id': 8},
        ],
        'total': 1,
      });
      expect(errors, contains('optimistic'));

      gate.complete({'id': 9, 'total': 99});
      await pending;
    });

    test(
      'keeps the projected value referentially stable while nothing moves',
      () async {
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) => request.meta.method == 'GET'
              ? [
                  {'id': 8},
                ]
              : gate.future,
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        final pending = cache.mutate(
          orderCreate,
          const TagContext(body: {'total': 99}),
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'total': 99}),
            place: {'Order[]': prepend},
          ),
        );

        final once = dataOf(cache, orderList);
        final twice = dataOf(cache, orderList);

        expect(twice, same(once));

        gate.complete({'id': 9, 'total': 99});
        await pending;
      },
    );

    test(
      'reports nothing for a still-loading list, and places once it settles',
      () async {
        final errors = <String>[];
        final listGate = Completer<Object?>();
        final gate = Completer<Object?>();
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (request, _) =>
              request.meta.method == 'GET' ? listGate.future : gate.future,
          onError: (_, context) => errors.add(context),
        );

        cache.subscribe(orderList, none, () {});

        final pending = cache.mutate(
          orderCreate,
          const TagContext(body: {'total': 99}),
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'total': 99}),
            place: {'Order[]': prepend},
          ),
        );

        expect(dataOf(cache, orderList), isNull);
        expect(errors, isNot(contains('optimistic')));

        listGate.complete([
          {'id': 8, 'total': 12},
        ]);
        await settle();

        expect(dataOf(cache, orderList), [
          {'id': '~opt1', 'total': 99},
          {'id': 8, 'total': 12},
        ]);
        expect(errors, isNot(contains('optimistic')));

        gate.complete({'id': 9, 'total': 99});
        await pending;
      },
    );

    test("keeps a settling query's registry value entity-plane only while a create overlay is live", () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = optimisticCache(
        (request, _) => request.meta.method == 'GET'
            ? [
                {'id': 8, 'total': 12},
              ]
            : gate.future,
      );

      await cache.fetch(orderList, none);
      cache.subscribe(orderList, none, () {});

      final pending = cache.mutate(
        orderCreate,
        const TagContext(body: {'total': 99}),
        options: const MutateOptions(
          optimistic: OptimisticCreate<Object?>({'total': 99}),
          place: {'Order[]': prepend},
        ),
      );

      await cache.refetch(orderList, none);

      final entry = cache.registry.get(cache.key(orderList, none));

      expect(entry?.value, [
        {'id': 8, 'total': 12},
      ]);

      gate.complete({'id': 9, 'total': 99});
      await pending;
    });
  });

  group('surfacing pending state', () {
    test('flags only the queries a live overlay reaches', () async {
      final gate = Completer<Object?>();
      const customerList = OperationMeta(
        id: 'customerList',
        method: 'GET',
        path: '/customers',
        entity: 'Customer',
        provides: ['Customer[]'],
      );
      final (:cache, transport: _, scheduler: _) = optimisticCache((
        request,
        _,
      ) {
        if (request.meta.path == '/orders') {
          return [
            {'id': 7, 'status': 'open'},
          ];
        }
        if (request.meta.path == '/customers') {
          return [
            {'id': 'c-3', 'name': 'Ada'},
          ];
        }

        return gate.future;
      });

      await cache.fetch(orderList, none);
      await cache.fetch(customerList, none);
      cache.subscribe(orderList, none, () {});
      cache.subscribe(customerList, none, () {});

      expect(cache.getState(orderList, none).isOptimistic, isFalse);

      final pending = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
        options: MutateOptions(optimistic: fields({'status': 'shipped'})),
      );

      expect(cache.getState(orderList, none).isOptimistic, isTrue);
      expect(cache.getState(customerList, none).isOptimistic, isFalse);

      gate.complete({'id': 7, 'status': 'shipped'});
      await pending;
      await settle();

      expect(cache.getState(orderList, none).isOptimistic, isFalse);
    });

    test('marks the pending row and not its neighbours', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = optimisticCache(
        (request, _) => request.meta.method == 'GET'
            ? [
                {'id': 7},
                {'id': 8},
              ]
            : gate.future,
      );

      await cache.fetch(orderList, none);
      cache.subscribe(orderList, none, () {});

      final pending = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}, body: {'status': 'shipped'}),
        options: MutateOptions(optimistic: fields({'status': 'shipped'})),
      );

      final rows = dataOf(cache, orderList)! as List<Object?>;

      expect(isOptimistic(rows[0]), isTrue);
      expect(isOptimistic(rows[1]), isFalse);

      gate.complete({'id': 7, 'status': 'shipped'});
      await pending;
    });

    test(
      'keeps the state object stable when isOptimistic does not move',
      () async {
        final (:cache, transport: _, scheduler: _) = optimisticCache(
          (_, _) => [
            {'id': 7},
          ],
        );

        await cache.fetch(orderList, none);
        cache.subscribe(orderList, none, () {});

        expect(
          cache.getState(orderList, none),
          same(cache.getState(orderList, none)),
        );
      },
    );

    test('builds a NEW state object when only isOptimistic moves', () async {
      final listGate = Completer<Object?>();
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = optimisticCache(
        (request, _) =>
            request.meta.method == 'GET' ? listGate.future : gate.future,
      );

      cache.subscribe(orderList, none, () {});

      final before = cache.getState(orderList, none);

      expect(before.isOptimistic, isFalse);

      final pending = cache.mutate(
        orderCreate,
        const TagContext(body: {'total': 99}),
        options: const MutateOptions(
          optimistic: OptimisticCreate<Object?>({'total': 99}),
          place: {'Order[]': prepend},
        ),
      );

      final after = cache.getState(orderList, none);

      expect(after, isNot(same(before)));
      expect(after.isOptimistic, isTrue);
      expect(after.dataOrNull, isNull);
      expect(after.runtimeType, before.runtimeType);
      expect(after.isFetching, before.isFetching);

      listGate.complete([
        {'id': 8},
      ]);
      gate.complete({'id': 9, 'total': 99});
      await pending;
    });
  });

  // Dart-only: the typed spec family's own rules.
  group('optimistic specs', () {
    test('refuses an OptimisticMany patch that names no key', () async {
      final errors = <String>[];
      final (:cache, transport: _, scheduler: _) = optimisticCache(
        (_, _) => {'id': 7},
        onError: (_, context) => errors.add(context),
      );

      await cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}),
        options: const MutateOptions(
          optimistic: OptimisticMany([OptimisticDelete()]),
        ),
      );

      expect(errors, ['optimistic']);
      expect(cache.overlays.empty, isTrue);
    });

    test('reports a typed spec handed straight to the cache instead of failing the write', () async {
      final errors = <String>[];
      final (:cache, :transport, scheduler: _) = optimisticCache(
        (_, _) => {'id': 7},
        onError: (_, context) => errors.add(context),
      );

      await cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}),
        options: MutateOptions(
          optimistic: OptimisticUpdate<String>((previous) => previous),
        ),
      );

      expect(errors, ['optimistic']);
      expect(transport.calls, hasLength(1));
    });

    // Review focus: the brief's cases read only the reported context, which a
    // different failure would also produce. These read what was reported.
    test(
      'reports the ambiguous target itself, and still dispatches the write',
      () async {
        final reported = <(Object, String)>[];
        final (:cache, :transport, scheduler: _) = optimisticCache(
          (_, _) => {'id': 7},
          onError: (error, context) => reported.add((error, context)),
        );
        const transfer = OperationMeta(
          id: 't',
          method: 'POST',
          path: '/orders/{id}/transfer',
          entity: 'Order',
          invalidates: ['Order:{id}', 'Customer:{req.customerId}'],
        );

        final result = await cache.mutate(
          transfer,
          const TagContext(path: {'id': 7}, body: {'customerId': 3}),
          options: MutateOptions(optimistic: fields({'status': 'moved'})),
        );

        expect(result, {'id': 7});
        expect(transport.calls, hasLength(1));
        expect(reported, hasLength(1));
        expect(reported.single.$2, 'optimistic');
        expect(
          reported.single.$1,
          isA<AmbiguousTargetError>().having((error) => error.keys, 'keys', [
            'Order:7',
            'Customer:3',
          ]),
        );
      },
    );

    test('reports the keyless OptimisticMany element as the reason', () async {
      final reported = <Object>[];
      final (:cache, :transport, scheduler: _) = optimisticCache(
        (_, _) => {'id': 7},
        onError: (error, _) => reported.add(error),
      );

      await cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}),
        options: MutateOptions(
          optimistic: OptimisticMany([
            OptimisticUpdate<Object?>(
              (_) => {'status': 'shipped'},
              key: 'Order:7',
            ),
            const OptimisticDelete(),
          ]),
        ),
      );

      expect(transport.calls, hasLength(1));
      expect(reported.single, isA<StateError>());
      expect('${reported.single}', contains('names no key'));
      expect(cache.overlays.empty, isTrue);
    });

    test('shows every keyed OptimisticMany patch at once, and reverts them together', () async {
      final gate = Completer<Object?>();
      final (:cache, transport: _, scheduler: _) = optimisticCache(
        (request, _) => request.meta.method == 'GET'
            ? [
                {'id': 7, 'status': 'open'},
                {'id': 8, 'status': 'open'},
              ]
            : gate.future,
      );

      await cache.fetch(orderList, none);
      cache.subscribe(orderList, none, () {});

      final pending = cache.mutate(
        orderPatch,
        const TagContext(path: {'id': 7}),
        options: MutateOptions(
          optimistic: OptimisticMany([
            OptimisticUpdate<Object?>(
              (_) => {'status': 'shipped'},
              key: 'Order:7',
            ),
            const OptimisticDelete(key: 'Order:8'),
          ]),
        ),
      );

      expect(dataOf(cache, orderList), [
        {'id': 7, 'status': 'shipped'},
      ]);

      gate.completeError(StateError('nope'));
      await expectLater(pending, throwsStateError);
      await settle();

      expect(dataOf(cache, orderList), [
        {'id': 7, 'status': 'open'},
        {'id': 8, 'status': 'open'},
      ]);
      expect(cache.overlays.empty, isTrue);
    });

    test(
      'refuses an optimistic create for an operation that names no entity',
      () async {
        final reported = <Object>[];
        final (:cache, :transport, scheduler: _) = optimisticCache(
          (_, _) => {'id': 9},
          onError: (error, _) => reported.add(error),
        );
        const bare = OperationMeta(id: 'bare', method: 'POST', path: '/things');

        await cache.mutate(
          bare,
          none,
          options: const MutateOptions(
            optimistic: OptimisticCreate<Object?>({'total': 1}),
          ),
        );

        expect(transport.calls, hasLength(1));
        expect('${reported.single}', contains('names no entity to create'));
        expect(cache.overlays.empty, isTrue);
      },
    );
  });
}
