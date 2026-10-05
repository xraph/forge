import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/schema.dart';

// The guarantee this file demonstrates: a committed response never contains an
// entity older than a stream frame the client has already applied. Every cache
// runs on the default microtask scheduler, because a manual scheduler plus a
// flush in the right place closes the very gap these tests open. Frames go
// through applyFrames directly: the interleaving under test is between a frame
// commit and a response arrival.

const _created = EntityStreamBinding(
  channel: '/ws/orders',
  message: 'order.created',
  entity: 'Order',
  intent: StreamIntent.upsert,
  invalidates: ['Order[]'],
);

const _updated = EntityStreamBinding(
  channel: '/ws/orders',
  message: 'order.updated',
  entity: 'Order',
  intent: StreamIntent.patch,
);

const _deleted = EntityStreamBinding(
  channel: '/ws/orders',
  message: 'order.deleted',
  entity: 'Order',
  intent: StreamIntent.evict,
  invalidates: ['Order[]'],
);

const _patch = OperationMeta(
  id: 'op_order_patch',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
);

const _seven = TagContext(path: {'id': 7});

StreamFrame _frame(EntityStreamBinding binding, Object? payload) =>
    StreamFrame(binding: binding, payload: payload);

QueryCache _cache(Transport transport, {int frameRestarts = 3}) => QueryCache(
  transport: transport,
  entities: schema,
  frameRestarts: frameRestarts,
);

Object? _total(QueryCache cache, String key) =>
    cache.store.getRecord(key)?.data['total'];

Object? _data(QueryCache cache) =>
    cache.getState(orderList, TagContext.empty).dataOrNull;

void main() {
  group('a frame that lands while a request is in flight', () {
    test('is not overwritten by the response that predates it', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([
          () => [
            {'id': 7, 'total': 1},
          ],
          // The refetch. Dispatched before the frame, answers after it, and
          // carries the pre-frame value.
          () => gate.future,
          () => [
            {'id': 7, 'total': 100},
          ],
        ]);
        final cache = _cache(transport);
        final rendered = <Object?>[];

        cache.watch(orderList, TagContext.empty).listen((_) {
          final record = cache.store.getRecord('Order:7');

          if (record != null) rendered.add(record.data['total']);
        });

        async.flushMicrotasks();
        expect(_total(cache, 'Order:7'), 1);

        Object? refetched;
        unawaited(
          cache
              .refetch(orderList, TagContext.empty)
              .then((value) => refetched = value),
        );
        async.flushMicrotasks();
        expect(transport.calls, hasLength(2));

        // The server pushes. This is newer than the answer on its way.
        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);
        expect(_total(cache, 'Order:7'), 100);

        final seenBeforeTheRace = rendered.length;

        gate.complete([
          {'id': 7, 'total': 1},
        ]);
        async.flushMicrotasks();

        expect(_total(cache, 'Order:7'), 100);
        expect(rendered.sublist(seenBeforeTheRace), isNot(contains(1)));

        // Discarded and re-run rather than committed.
        expect(transport.calls, hasLength(3));
        expect(refetched, [
          {'id': 7, 'total': 100},
        ]);
        expect(_data(cache), [
          {'id': 7, 'total': 100},
        ]);
      });
    });

    test(
      'converges in exactly one re-run, because the re-run postdates the frame',
      () {
        fakeAsync((async) {
          final gate = Completer<Object?>();
          final transport = ScriptedTransport([
            () => [
              {'id': 7, 'total': 1},
            ],
            () => gate.future,
            // The server is lagging: the re-run gets the old value too.
            () => [
              {'id': 7, 'total': 1},
            ],
          ]);
          final cache = _cache(transport);

          cache.watch(orderList, TagContext.empty).listen((_) {});
          async.flushMicrotasks();

          unawaited(
            cache
                .refetch(orderList, TagContext.empty)
                .catchError((Object _) => null),
          );
          async.flushMicrotasks();

          applyFrames(cache, [
            _frame(_updated, {'id': 7, 'total': 100}),
          ]);

          gate.complete([
            {'id': 7, 'total': 1},
          ]);
          async.flushMicrotasks();

          expect(transport.calls, hasLength(3));
          expect(_total(cache, 'Order:7'), 1);
        });
      },
    );

    test('holds for an unmounted query, which no tag index can reach', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([
          () => gate.future,
          () => [
            {'id': 7, 'total': 100},
          ],
        ]);
        final cache = _cache(transport);

        Object? fetched;
        unawaited(
          cache
              .fetch(orderList, TagContext.empty)
              .then((value) => fetched = value),
        );
        async.flushMicrotasks();

        expect(cache.registry.mounted, 0);

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);
        gate.complete([
          {'id': 7, 'total': 1},
        ]);
        async.flushMicrotasks();

        expect(transport.calls, hasLength(2));
        expect(_total(cache, 'Order:7'), 100);
        expect(fetched, [
          {'id': 7, 'total': 100},
        ]);
      });
    });

    test('does not let a pre-delete response resurrect the row', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([
          () => [
            {'id': 7, 'total': 1},
            {'id': 8, 'total': 2},
          ],
          () => gate.future,
          () => [
            {'id': 8, 'total': 2},
          ],
        ]);
        final cache = _cache(transport);

        cache.watch(orderList, TagContext.empty).listen((_) {});
        async.flushMicrotasks();
        expect(cache.store.has('Order:7'), isTrue);

        unawaited(
          cache
              .refetch(orderList, TagContext.empty)
              .catchError((Object _) => null),
        );
        async.flushMicrotasks();

        applyFrames(cache, [
          _frame(_deleted, {'id': 7}),
        ]);
        expect(cache.store.has('Order:7'), isFalse);

        gate.complete([
          {'id': 7, 'total': 1},
          {'id': 8, 'total': 2},
        ]);
        async.flushMicrotasks();

        expect(cache.store.has('Order:7'), isFalse);
        expect(transport.calls, hasLength(3));
        expect(_data(cache), [
          {'id': 8, 'total': 2},
        ]);
      });
    });

    test(
      'does not resurrect it through a query the delete’s tags cannot reach',
      () {
        fakeAsync((async) {
          final gate = Completer<Object?>();
          final transport = ScriptedTransport([
            () => gate.future,
            () => <Object?>[],
          ]);
          final cache = _cache(transport);

          cache.store.put('Order:7', {'id': 7, 'total': 1});

          Object? fetched;
          unawaited(
            cache
                .fetch(orderList, TagContext.empty)
                .then((value) => fetched = value),
          );
          async.flushMicrotasks();
          expect(cache.registry.mounted, 0);

          applyFrames(cache, [
            _frame(_deleted, {'id': 7}),
          ]);
          gate.complete([
            {'id': 7, 'total': 1},
          ]);
          async.flushMicrotasks();

          expect(cache.store.has('Order:7'), isFalse);
          expect(transport.calls, hasLength(2));
          expect(fetched, isEmpty);
        });
      },
    );
  });

  group('a mutation that lost the race', () {
    test('does not clobber the frame, and is never re-issued', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([() => gate.future]);
        final cache = _cache(transport);

        cache.store.put('Order:7', {'id': 7, 'total': 1});

        Object? created;
        unawaited(
          cache.mutate(_patch, _seven).then((value) => created = value),
        );
        async.flushMicrotasks();
        expect(transport.calls, hasLength(1));

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);

        gate.complete({'id': 7, 'total': 50});
        async.flushMicrotasks();

        expect(_total(cache, 'Order:7'), 100);
        expect(transport.calls, hasLength(1));
        expect(created, {'id': 7, 'total': 100});
      });
    });

    test('still commits every entity the frame did not touch', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([() => gate.future]);
        final cache = _cache(transport);

        const bulk = OperationMeta(
          id: 'op_order_bulk',
          method: 'POST',
          path: '/orders/bulk',
          entity: 'Order',
        );

        unawaited(cache.mutate(bulk, TagContext.empty));
        async.flushMicrotasks();

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);

        gate.complete([
          {'id': 7, 'total': 1},
          {'id': 8, 'total': 2},
        ]);
        async.flushMicrotasks();

        expect(_total(cache, 'Order:7'), 100);
        expect(_total(cache, 'Order:8'), 2);
        expect(transport.calls, hasLength(1));
      });
    });

    test('hands no undefined to the caller when its own entity was deleted mid-flight', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([() => gate.future]);
        final cache = _cache(transport);

        cache.store.put('Order:7', {'id': 7, 'total': 1});

        Object? created;
        unawaited(
          cache.mutate(_patch, _seven).then((value) => created = value),
        );
        async.flushMicrotasks();

        applyFrames(cache, [
          _frame(_deleted, {'id': 7}),
        ]);
        expect(cache.store.has('Order:7'), isFalse);

        gate.complete({'id': 7, 'total': 50});
        async.flushMicrotasks();

        // What the server said, not a corpse read back out of the store.
        expect(created, {'id': 7, 'total': 50});
        expect(cache.store.has('Order:7'), isFalse);
        expect(transport.calls, hasLength(1));
      });
    });

    test(
      'declines placement rather than splicing a deleted entity into a list',
      () {
        fakeAsync((async) {
          final gate = Completer<Object?>();
          final transport = ScriptedTransport([
            () => [
              {'id': 7, 'total': 1},
              {'id': 8, 'total': 2},
            ],
            () => gate.future,
            () => [
              {'id': 8, 'total': 2},
            ],
          ]);
          final cache = _cache(transport);
          final placedWith = <Object?>[];
          final rendered = <List<Object?>>[];

          cache.watch(orderList, TagContext.empty).listen((_) {
            rendered.add((_data(cache) as List<Object?>?) ?? const []);
          });
          async.flushMicrotasks();
          expect(cache.store.has('Order:7'), isTrue);

          const replace = OperationMeta(
            id: 'op_order_replace',
            method: 'PUT',
            path: '/orders/{id}',
            entity: 'Order',
            invalidates: ['Order[]'],
          );

          unawaited(
            cache.mutate(
              replace,
              _seven,
              options: MutateOptions(
                place: {
                  'Order[]': (made, current, _) {
                    placedWith.add(made);

                    return [made, ...?(current as List<Object?>?)];
                  },
                },
              ),
            ),
          );
          async.flushMicrotasks();

          applyFrames(cache, [
            _frame(_deleted, {'id': 7}),
          ]);
          expect(cache.store.has('Order:7'), isFalse);

          gate.complete({'id': 7, 'total': 50});
          async.flushMicrotasks();

          expect(placedWith, isEmpty);

          for (final value in rendered) {
            expect(value, isNot(contains(null)));
            expect(
              () => value
                  .map((order) => (order! as Map<String, Object?>)['id'])
                  .toList(),
              returnsNormally,
            );
          }

          expect(cache.store.has('Order:7'), isFalse);
          expect(_data(cache), [
            {'id': 8, 'total': 2},
          ]);
        });
      },
    );

    test('commits normally when no frame overtook it', () {
      fakeAsync((async) {
        final transport = ScriptedTransport([
          () => {'id': 7, 'total': 50},
        ]);
        final cache = _cache(transport);

        cache.store.put('Order:7', {'id': 7, 'total': 1});

        Object? created;
        unawaited(
          cache.mutate(_patch, _seven).then((value) => created = value),
        );
        async.flushMicrotasks();

        expect(created, {'id': 7, 'total': 50});
        expect(_total(cache, 'Order:7'), 50);
        expect(transport.calls, hasLength(1));
      });
    });
  });

  group('what the guarantee deliberately does not do', () {
    test(
      'commits a response dispatched after the frame, without re-running it',
      () {
        fakeAsync((async) {
          final transport = ScriptedTransport([
            () => [
              {'id': 7, 'total': 1},
            ],
            () => [
              {'id': 7, 'total': 250},
            ],
          ]);
          final cache = _cache(transport);

          cache.watch(orderList, TagContext.empty).listen((_) {});
          async.flushMicrotasks();

          applyFrames(cache, [
            _frame(_updated, {'id': 7, 'total': 100}),
          ]);
          async.flushMicrotasks();

          unawaited(cache.refetch(orderList, TagContext.empty));
          async.flushMicrotasks();

          expect(transport.calls, hasLength(2));
          expect(_total(cache, 'Order:7'), 250);
        });
      },
    );

    test('does not restart a response over a frame that touched a different entity', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([
          () => [
            {'id': 7, 'total': 1},
          ],
          () => gate.future,
        ]);
        final cache = _cache(transport);

        cache.watch(orderList, TagContext.empty).listen((_) {});
        async.flushMicrotasks();

        unawaited(
          cache
              .refetch(orderList, TagContext.empty)
              .catchError((Object _) => null),
        );
        async.flushMicrotasks();

        applyFrames(cache, [
          _frame(_updated, {'id': 9, 'total': 5}),
        ]);

        gate.complete([
          {'id': 7, 'total': 3},
        ]);
        async.flushMicrotasks();

        expect(transport.calls, hasLength(2));
        expect(_total(cache, 'Order:7'), 3);
        expect(_total(cache, 'Order:9'), 5);
      });
    });

    test(
      'still restarts it when the frame’s own tags say membership moved',
      () {
        fakeAsync((async) {
          final gate = Completer<Object?>();
          final transport = ScriptedTransport([
            () => [
              {'id': 7, 'total': 1},
            ],
            () => gate.future,
            () => [
              {'id': 9, 'total': 5},
              {'id': 7, 'total': 1},
            ],
          ]);
          final cache = _cache(transport);

          cache.watch(orderList, TagContext.empty).listen((_) {});
          async.flushMicrotasks();

          unawaited(
            cache
                .refetch(orderList, TagContext.empty)
                .catchError((Object _) => null),
          );
          async.flushMicrotasks();

          applyFrames(cache, [
            _frame(_created, {'id': 9, 'total': 5}),
          ]);

          gate.complete([
            {'id': 7, 'total': 1},
          ]);
          async.flushMicrotasks();

          expect(transport.calls, hasLength(3));
          expect(_data(cache), [
            {'id': 9, 'total': 5},
            {'id': 7, 'total': 1},
          ]);
        });
      },
    );

    test('commits around the frames rather than looping, once the restart bound is spent', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([
          () => [
            {'id': 7, 'total': 1},
            {'id': 8, 'total': 2},
          ],
          () => gate.future,
        ]);
        final cache = _cache(transport, frameRestarts: 0);

        cache.watch(orderList, TagContext.empty).listen((_) {});
        async.flushMicrotasks();

        unawaited(
          cache
              .refetch(orderList, TagContext.empty)
              .catchError((Object _) => null),
        );
        async.flushMicrotasks();

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);

        gate.complete([
          {'id': 7, 'total': 1},
          {'id': 8, 'total': 22},
        ]);
        async.flushMicrotasks();

        expect(transport.calls, hasLength(2));
        expect(_total(cache, 'Order:7'), 100);
        expect(_total(cache, 'Order:8'), 22);
      });
    });
  });

  group('the frame stamp itself', () {
    test('survives a later response merging fields into the record', () {
      fakeAsync((async) {
        final transport = ScriptedTransport([
          () => [
            {'id': 7, 'total': 1},
          ],
        ]);
        final cache = _cache(transport);

        cache.watch(orderList, TagContext.empty).listen((_) {});
        async.flushMicrotasks();

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);
        final stamped = cache.store.frameVersion;

        cache.store.put('Order:7', {'note': 'from a later response'});

        expect(cache.store.getRecord('Order:7')?.frameAt, stamped);
        expect(cache.store.racedSince(['Order:7'], stamped - 1), ['Order:7']);
      });
    });

    test(
      'is recorded even when the frame changed nothing, and bumps no version',
      () {
        final cache = _cache(ScriptedTransport([]));

        cache.store.put('Order:7', {'id': 7, 'total': 5});
        final before = cache.store.getRecord('Order:7');

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 5}),
        ]);

        final after = cache.store.getRecord('Order:7');

        expect(after?.data, before?.data);
        expect(after?.version, before?.version);
        expect(after?.frameAt, cache.store.frameVersion);
      },
    );

    test('keeps the siblings of a raced entity rather than losing them to a failed re-run', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([
          () => [
            {'id': 7, 'total': 1},
            {'id': 8, 'total': 2},
          ],
          () => gate.future,
          () => throw StateError('gateway timeout'),
        ]);
        final cache = _cache(transport);

        cache.watch(orderList, TagContext.empty).listen((_) {});
        async.flushMicrotasks();

        unawaited(
          cache
              .refetch(orderList, TagContext.empty)
              .catchError((Object _) => null),
        );
        async.flushMicrotasks();

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);

        gate.complete([
          {'id': 7, 'total': 1},
          {'id': 8, 'total': 222},
        ]);
        async.flushMicrotasks();

        expect(transport.calls, hasLength(3));
        expect(
          cache.getState(orderList, TagContext.empty),
          isA<QueryFailure<Object?>>(),
        );
        expect(_total(cache, 'Order:7'), 100);
        expect(_total(cache, 'Order:8'), 222);
      });
    });

    test(
      'bounds the tombstones rather than growing one per delete forever',
      () {
        final cache = _cache(ScriptedTransport([]));

        for (var i = 0; i < 5000; i++) {
          applyFrames(cache, [
            _frame(_deleted, {'id': 'ghost-$i'}),
          ]);
        }

        expect(cache.store.size, 0);
        expect(cache.store.tombstones, 0);

        for (var i = 0; i < 5000; i++) {
          cache.store.put('Order:$i', {'id': i, 'total': 1});
          applyFrames(cache, [
            _frame(_deleted, {'id': i}),
          ]);
        }

        expect(cache.store.size, 0);
        expect(cache.store.tombstones, lessThanOrEqualTo(256));
        expect(cache.store.frameStamp('Order:4999'), greaterThan(0));
      },
    );

    test('hands a tombstone’s stamp to the record that replaces it', () {
      final cache = _cache(ScriptedTransport([]));

      cache.store.put('Order:7', {'id': 7, 'total': 5});
      applyFrames(cache, [
        _frame(_deleted, {'id': 7}),
      ]);

      final stamp = cache.store.frameVersion;
      expect(cache.store.frameStamp('Order:7'), stamp);

      cache.store.put('Order:7', {'id': 7, 'total': 9});

      expect(cache.store.getRecord('Order:7')?.frameAt, stamp);
      expect(cache.store.frameStamp('Order:7'), stamp);
    });
  });

  group('Dart port', () {
    // Review Focus: a frame overtaking an optimistic write in flight. The
    // frame stands, the overlay is gone, and nothing is left looking
    // unconfirmed once the response lands.
    test('keeps the frame and drops the overlay when an optimistic write loses the race', () {
      fakeAsync((async) {
        final gate = Completer<Object?>();
        final transport = ScriptedTransport([
          () => {'id': 7, 'total': 1},
          () => gate.future,
        ]);
        final cache = _cache(transport);

        cache.watch(orderGet, _seven).listen((_) {});
        async.flushMicrotasks();

        unawaited(
          cache.mutate(
            _patch,
            _seven,
            options: MutateOptions(
              optimistic: OptimisticUpdate<Object?>(
                (previous) => {
                  ...previous! as Map<String, Object?>,
                  'total': 50,
                },
                // Dart port: `_patch` declares no `invalidates`, so the
                // target is not derivable and is named outright.
                key: 'Order:7',
              ),
            ),
          ),
        );
        async.flushMicrotasks();

        final during = cache.getState(orderGet, _seven);
        expect((during.dataOrNull! as Map<String, Object?>)['total'], 50);
        expect(during.isOptimistic, isTrue);

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ]);

        gate.complete({'id': 7, 'total': 50});
        async.flushMicrotasks();

        final after = cache.getState(orderGet, _seven);
        expect(_total(cache, 'Order:7'), 100);
        expect((after.dataOrNull! as Map<String, Object?>)['total'], 100);
        expect(after.isOptimistic, isFalse);
        expect(transport.calls, hasLength(2));
      });
    });

    // The synthesized `<Entity>[]` of an eviction: the generator passes
    // `invalidates` through verbatim, so a binding may declare nothing.
    test(
      'raises the entity list on an evict whose binding declares no tags',
      () {
        fakeAsync((async) {
          const bare = EntityStreamBinding(
            channel: '/ws/orders',
            message: 'order.deleted',
            entity: 'Order',
            intent: StreamIntent.evict,
          );
          final transport = ScriptedTransport([
            () => [
              {'id': 7, 'total': 1},
              {'id': 8, 'total': 2},
            ],
            () => [
              {'id': 8, 'total': 2},
            ],
          ]);
          final cache = _cache(transport);

          cache.watch(orderList, TagContext.empty).listen((_) {});
          async.flushMicrotasks();

          applyFrames(cache, [_frame(bare, 7)]);
          async.flushMicrotasks();

          expect(cache.store.has('Order:7'), isFalse);
          expect(transport.calls, hasLength(2));
          expect(_data(cache), [
            {'id': 8, 'total': 2},
          ]);
        });
      },
    );

    // Dart port addition (spec: one user's data must never reach the next).
    // Frames coalesced for a delay can outlive a clear or a principal change.
    group('a batch captured under a previous cache generation', () {
      test('commits when the cache has not been emptied since', () {
        final cache = _cache(ScriptedTransport([]));
        final events = <CacheEvent>[];

        cache.observer = events.add;

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
        ], generation: cache.generation);

        expect(_total(cache, 'Order:7'), 100);
        expect(events.whereType<FramesCommitted>(), hasLength(1));
      });

      test('writes nothing after clear()', () {
        final cache = _cache(ScriptedTransport([]));
        final events = <CacheEvent>[];
        final captured = cache.generation;

        cache.observer = events.add;
        cache.clear();

        applyFrames(cache, [
          _frame(_created, {'id': 7, 'total': 100}),
        ], generation: captured);

        expect(cache.store.has('Order:7'), isFalse);
        expect(cache.store.size, 0);
        expect(cache.store.frameVersion, 0);
        expect(events, isEmpty);
      });

      test('writes nothing after the principal changes', () {
        final cache = _cache(ScriptedTransport([]));
        final events = <CacheEvent>[];

        cache.setPrincipal('alice');
        cache.store.put('Order:7', {'id': 7, 'total': 1});

        final captured = cache.generation;

        cache.observer = events.add;
        cache.setPrincipal('bob');

        applyFrames(cache, [
          _frame(_updated, {'id': 7, 'total': 100}),
          _frame(_created, {'id': 8, 'total': 5}),
        ], generation: captured);

        expect(cache.store.size, 0);
        expect(cache.store.has('Order:7'), isFalse);
        expect(cache.store.has('Order:8'), isFalse);
        expect(events, isEmpty);
      });

      test('does not evict or tombstone a record of the new generation', () {
        final cache = _cache(ScriptedTransport([]));
        final captured = cache.generation;

        cache.clear();
        cache.store.put('Order:7', {'id': 7, 'total': 9});

        applyFrames(cache, [
          _frame(_deleted, {'id': 7}),
        ], generation: captured);

        expect(_total(cache, 'Order:7'), 9);
        expect(cache.store.tombstones, 0);
      });

      test('does not invalidate a query the new generation mounted', () {
        fakeAsync((async) {
          final transport = ScriptedTransport([
            () => [
              {'id': 7, 'total': 1},
            ],
            () => [
              {'id': 7, 'total': 100},
            ],
          ]);
          final cache = _cache(transport);
          final captured = cache.generation;

          cache.setPrincipal('bob');
          cache.watch(orderList, TagContext.empty).listen((_) {});
          async.flushMicrotasks();
          expect(transport.calls, hasLength(1));

          applyFrames(cache, [
            _frame(_created, {'id': 9, 'total': 5}),
          ], generation: captured);
          async.flushMicrotasks();

          expect(transport.calls, hasLength(1));
          expect(cache.store.has('Order:9'), isFalse);
        });
      });
    });
  });
}
