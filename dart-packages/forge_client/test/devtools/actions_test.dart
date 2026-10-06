import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/devtools.dart';
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

import 'harness.dart';

void main() {
  group('actions', () {
    test('refetches one query and logs itself as the cause', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();
      final before = h.calls.length;

      await devtools.actions.refetch(h.key(Ops.orderList));
      await h.settle();

      expect(h.calls.length, before + 1);
      expect(
        devtools.log().whereType<ActionLog>().first.action,
        ActionKind.refetch,
      );

      await sub.cancel();
      devtools.dispose();
    });

    test(
      'rejects a refetch on a key nothing tracks, and logs nothing',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());

        await expectLater(
          devtools.actions.refetch('GET /nothing'),
          throwsA(isA<StateError>()),
        );
        expect(devtools.log().whereType<ActionLog>(), isEmpty);

        devtools.dispose();
      },
    );

    test('invalidates the tags a query carries, and reaches it', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();
      final before = h.calls.length;

      expect(devtools.actions.invalidate(h.key(Ops.orderList)), isTrue);
      h.flush();
      await h.settle();

      expect(h.calls.length, before + 1);

      await sub.cancel();
      devtools.dispose();
    });

    test('evicts one entity and leaves the rest of the store alone', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      expect(devtools.entity('Order:1'), isNotNull);
      expect(devtools.actions.evict('Order:1'), isTrue);
      expect(devtools.entity('Order:1'), isNull);
      expect(devtools.entity('Order:2'), isNotNull);

      await sub.cancel();
      devtools.dispose();
    });

    test('drops a watched query, which resets it and refetches', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();
      final before = h.calls.length;

      expect(devtools.actions.drop(h.key(Ops.orderList)), isTrue);
      await h.settle();

      expect(h.calls.length, before + 1);

      await sub.cancel();
      devtools.dispose();
    });

    test('answers false for a key it is not tracking, and logs nothing', () {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());

      expect(devtools.actions.drop('GET /nothing'), isFalse);
      expect(devtools.log().whereType<ActionLog>(), isEmpty);

      devtools.dispose();
    });

    test('clears the whole cache', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());

      await h.cache.fetch(Ops.orderList, TagContext.empty);

      expect(devtools.store().records, greaterThan(0));

      devtools.actions.clear();

      expect(devtools.store().records, 0);

      devtools.dispose();
    });

    test('patches, rolls back, promotes and marks stale through the overlay stack, logging each', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final patched = devtools.patchEntity('Order:1', {'total': 42});

      expect(devtools.foldedRecord('Order:1')!['total'], 42);
      expect(devtools.baseRecord('Order:1')!['total'], 10);
      expect(devtools.actions.rollback(patched), isTrue);
      expect(devtools.actions.rollback(patched), isFalse);
      expect(devtools.foldedRecord('Order:1')!['total'], 10);

      final kept = devtools.patchEntity('Order:1', {'total': 43});

      expect(devtools.actions.promote(kept), isTrue);
      expect(devtools.baseRecord('Order:1')!['total'], 43);
      expect(devtools.actions.forceStale(h.key(Ops.orderList)), isTrue);
      expect(devtools.actions.forceStale('GET /nothing'), isFalse);

      devtools.actions
        ..hold('GET /orders', 'error')
        ..release('GET /orders');

      expect(
        [
          for (final a in devtools.log().whereType<ActionLog>())
            '${a.action.name} ${a.target}',
        ],
        [
          'rollback patch Order:1',
          'rollback overlay #$patched',
          'rollback patch Order:1',
          'rollback promote overlay #$kept',
          'stale ${h.key(Ops.orderList)}',
          'hold GET /orders in error',
          'release GET /orders',
        ],
      );

      await sub.cancel();
      devtools.dispose();
    });
  });

  group('through the cache\'s own write paths', () {
    test(
      'an invalidation is heard by the cache observer, as for an app write',
      () async {
        final h = Harness();
        final heard = <String>[];
        h.cache.observer = (event) {
          if (event is QueryInvalidated) heard.add(event.key);
        };
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        devtools.actions.invalidateTag('Order[]');
        h.flush();
        await h.settle();

        expect(heard, [h.key(Ops.orderList)]);

        await sub.cancel();
        devtools.dispose();
      },
    );

    test('a patch and an eviction reach a mounted watcher, and a rollback takes the patch back out', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final seen = <Object?>[];
      final watch = h.cache
          .watch(Ops.orderList, TagContext.empty)
          .listen((state) => seen.add(state.dataOrNull));
      await h.settle();

      List<Object?> totals() => [
        for (final order in seen.last! as List<Object?>)
          (order! as Map<Object?, Object?>)['total'],
      ];

      expect(totals(), [10, 20]);

      final layer = devtools.patchEntity('Order:1', {'total': 42});

      expect(totals(), [42, 20]);
      expect(devtools.actions.rollback(layer), isTrue);
      expect(totals(), [10, 20]);

      await watch.cancel();
      devtools.dispose();
    });

    // `DevCache.version` also counts the memo drops a layer causes, which is
    // not a record write, so this reads the record itself.
    test(
      'a patch is an overlay, so the record underneath is not written',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();
        final data = h.dev.record('Order:1')!.data;
        final version = h.dev.record('Order:1')!.version;

        devtools.patchEntity('Order:1', {'total': 42});

        expect(identical(h.dev.record('Order:1')!.data, data), isTrue);
        expect(h.dev.record('Order:1')!.version, version);
        expect(devtools.baseRecord('Order:1')!['total'], 10);
        expect(devtools.foldedRecord('Order:1')!['total'], 42);

        await sub.cancel();
        devtools.dispose();
      },
    );
  });

  // A pending identity change is the window in which the cache still holds the
  // previous principal's records. An action aimed there would land on data
  // that is about to be dropped, or on the next principal's.
  group('while the cache is changing principal', () {
    for (final (name, readerFirst) in [('after', false), ('before', true)]) {
      test(
        'every action throws a StateError and changes nothing, for a caller that runs $name the recorder',
        () async {
          final h = Harness();
          h.cache.setPrincipal('alice');
          Devtools? devtools;
          final outcomes = <String, Object?>{};
          final listKey = h.key(Ops.orderList);
          late int layer;

          void caller(String? _) {
            final d = devtools;

            if (d == null) return;

            final calls = <String, Object? Function()>{
              // Settled at once: a failed Future nobody listens to is an
              // unhandled error.
              'refetch': () => d.actions
                  .refetch(listKey)
                  .then<Object?>((value) => value, onError: (Object e) => e),
              'invalidate': () => d.actions.invalidate(listKey),
              'invalidateTag': () => d.actions.invalidateTag('Order[]'),
              'evict': () => d.actions.evict('Order:1'),
              'drop': () => d.actions.drop(listKey),
              'clear': d.actions.clear,
              'rollback': () => d.actions.rollback(layer),
              'promote': () => d.actions.promote(layer),
              'forceStale': () => d.actions.forceStale(listKey),
              'patchEntity': () => d.patchEntity('Order:1', {'total': 1}),
              'hold': () => d.actions.hold(listKey, 'error'),
              'release': () => d.actions.release(listKey),
            };

            for (final MapEntry(:key, value: call) in calls.entries) {
              try {
                outcomes[key] = call();
              } on StateError catch (error) {
                outcomes[key] = error;
              }
            }
          }

          if (readerFirst) h.cache.watchPrincipalChanging(caller);
          devtools = attach(h.cache, clock: CounterClock());
          if (!readerFirst) h.cache.watchPrincipalChanging(caller);

          final sub = h.mount(Ops.orderList);
          await h.settle();
          await sub.cancel();
          layer = h.dev.pushMerge('Order:1', {'total': 99});
          final calls = h.calls.length;

          h.cache.setPrincipal('bob');
          await h.settle();

          expect(outcomes.keys, hasLength(12));

          for (final MapEntry(:key, value: outcome) in outcomes.entries) {
            // `refetch` is async, so its refusal arrives as a Future.
            final error = outcome is Future<Object?> ? await outcome : outcome;

            expect(error, isA<StateError>(), reason: key);
            expect(
              (error! as StateError).message,
              contains('changing principal'),
              reason: key,
            );
          }

          // Nothing was requested, recorded or promoted on anyone's behalf.
          expect(h.calls.length, calls);
          expect(devtools.log().whereType<ActionLog>(), isEmpty);

          devtools.dispose();
        },
      );
    }

    test(
      'acts on the current principal only once the change has settled',
      () async {
        final h = Harness();
        h.cache.setPrincipal('alice');
        final devtools = attach(h.cache, clock: CounterClock());
        final aliceList = h.key(Ops.orderList);
        final sub = h.mount(Ops.orderList);
        await h.settle();
        final aliceLayer = devtools.patchEntity('Order:1', {'total': 99});
        await sub.cancel();

        h.cache.setPrincipal('bob');
        await h.settle();

        // Alice's keys, layers and entities are not in bob's cache, so there is
        // nothing for an action to land on.
        expect(devtools.actions.evict('Order:1'), isFalse);
        expect(devtools.actions.invalidate(aliceList), isFalse);
        expect(devtools.actions.drop(aliceList), isFalse);
        expect(devtools.actions.forceStale(aliceList), isFalse);
        expect(devtools.actions.rollback(aliceLayer), isFalse);
        expect(devtools.actions.promote(aliceLayer), isFalse);
        await expectLater(
          devtools.actions.refetch(aliceList),
          throwsA(isA<StateError>()),
        );
        expect(devtools.log().whereType<ActionLog>(), isEmpty);

        // And they work on what bob has.
        final bobs = h.mount(Ops.orderList);
        await h.settle();

        expect(devtools.actions.evict('Order:1'), isTrue);
        expect(
          devtools.log().whereType<ActionLog>().single.session,
          devtools.session,
        );

        await bobs.cancel();
        devtools.dispose();
      },
    );

    test('throws once the inspector is disposed', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      devtools.dispose();

      expect(() => devtools.actions.evict('Order:1'), throwsStateError);
      expect(() => devtools.actions.clear(), throwsStateError);
      expect(
        () => devtools.patchEntity('Order:1', {'total': 1}),
        throwsStateError,
      );
      await expectLater(
        devtools.actions.refetch(h.key(Ops.orderList)),
        throwsA(isA<StateError>()),
      );
      expect(h.dev.hasEntity('Order:1'), isTrue);

      await sub.cancel();
    });
  });

  // A sync source owns its entity types: only it writes their records. A
  // devtools edit would leave the replica and the store disagreeing, so each
  // action that would write one is refused, by name, before anything moves.
  group('an entity a sync source owns', () {
    // Everything a refused action must leave alone.
    Object untouched(Harness h, Devtools d) => (
      note: h.dev.record('Note:1')!.data,
      version: h.dev.record('Note:1')!.version,
      storeWrites: h.dev.version,
      overlays: h.cache.overlays.version,
      layers: d.overlays().length,
      wire: h.calls.length,
      logged: d.log().length,
    );

    Matcher refusal(String named) => isA<StateError>().having(
      (e) => e.message,
      'message',
      allOf(contains(named), contains('sync source')),
    );

    test(
      'is refused by evict, which names the entity and leaves the record',
      () async {
        final (h, _) = await ownedHarness();
        final devtools = attach(h.cache, clock: CounterClock());
        final before = untouched(h, devtools);

        expect(
          () => devtools.actions.evict('Note:1'),
          throwsA(refusal('Note:1')),
        );
        // Refused by type, so a record that is not held is refused too rather
        // than answering "nothing to evict" for something it may never touch.
        expect(
          () => devtools.actions.evict('Note:404'),
          throwsA(refusal('Note:404')),
        );

        expect(h.dev.hasEntity('Note:1'), isTrue);
        expect(untouched(h, devtools), before);
        expect(devtools.log().whereType<ActionLog>(), isEmpty);

        devtools.dispose();
      },
    );

    test('is refused by patchEntity, and no layer is pushed', () async {
      final (h, _) = await ownedHarness();
      final devtools = attach(h.cache, clock: CounterClock());
      final before = untouched(h, devtools);

      expect(
        () => devtools.patchEntity('Note:1', {'body': 'edited'}),
        throwsA(refusal('Note:1')),
      );
      expect(
        () => devtools.actions.patchEntity('Note:1', {'body': 'edited'}),
        throwsA(refusal('Note:1')),
      );

      expect(devtools.overlays(), isEmpty);
      expect(devtools.foldedRecord('Note:1')!['body'], ownedBody);
      expect(untouched(h, devtools), before);
      expect(devtools.log().whereType<ActionLog>(), isEmpty);

      devtools.dispose();
    });

    test('is refused by promote, and the layer stays on the stack', () async {
      final (h, _) = await ownedHarness();
      final devtools = attach(h.cache, clock: CounterClock());
      // A layer an app (or a test) put there; the devtools themselves cannot.
      final layer = h.dev.pushMerge('Note:1', {'body': 'edited'});
      final before = untouched(h, devtools);

      expect(() => devtools.actions.promote(layer), throwsA(refusal('Note:1')));

      expect(devtools.overlays().map((l) => l.id), [layer]);
      expect(devtools.baseRecord('Note:1')!['body'], ownedBody);
      expect(untouched(h, devtools), before);
      expect(devtools.log().whereType<ActionLog>(), isEmpty);

      devtools.dispose();
    });

    test(
      'is refused by invalidate and invalidateTag, which would raise its tags',
      () async {
        final (h, _) = await ownedHarness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.cache.watch(noteList, TagContext.empty).listen((_) {});
        await h.settle();
        final before = untouched(h, devtools);

        expect(
          () => devtools.actions.invalidate(h.key(noteList)),
          throwsA(refusal('Note[]')),
        );
        expect(
          () => devtools.actions.invalidateTag('Note[]'),
          throwsA(refusal('Note[]')),
        );
        expect(
          () => devtools.actions.invalidateTag('Note:1'),
          throwsA(refusal('Note:1')),
        );
        expect(
          () => devtools.actions.invalidateTag('Note[]:open'),
          throwsA(refusal('Note[]:open')),
        );

        h.flush();
        await h.settle();

        expect(untouched(h, devtools), before);
        expect(devtools.log().whereType<ActionLog>(), isEmpty);

        // A tag of a type nobody owns is not caught by the rule.
        devtools.actions.invalidateTag('Order[]');

        expect(
          devtools.log().whereType<ActionLog>().single.action,
          ActionKind.invalidateTag,
        );

        await sub.cancel();
        devtools.dispose();
      },
    );

    test(
      'is left alone by the actions that never write an entity row',
      () async {
        final (h, _) = await ownedHarness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.cache.watch(noteList, TagContext.empty).listen((_) {});
        await h.settle();
        final key = h.key(noteList);
        final body = h.dev.record('Note:1')!.data['body'];

        // refetch: REST answers with different text, and the cache refuses to
        // let a response write an owned record.
        await devtools.actions.refetch(key);
        await h.settle();

        expect(h.dev.record('Note:1')!.data['body'], body);

        // stale, rollback and the two log-only actions touch no record.
        expect(devtools.actions.forceStale(key), isTrue);
        expect(
          devtools.actions.rollback(h.dev.pushMerge('Order:1', {'total': 1})),
          isTrue,
        );
        devtools.actions
          ..hold(key, 'error')
          ..release(key);

        expect(h.dev.record('Note:1')!.data['body'], body);

        // drop and clear: the cache keeps a source's records through both.
        expect(devtools.actions.drop(key), isTrue);

        devtools.actions.clear();

        expect(h.dev.record('Note:1')!.data['body'], body);
        expect(devtools.entity('Note:1')!.fields['body'], body);

        await sub.cancel();
        devtools.dispose();
      },
    );
  });

  group('the ownership seam', () {
    test('reads a type from a key and from every tag form', () async {
      final (h, _) = await ownedHarness();

      expect(h.dev.ownsKey('Note:1'), isTrue);
      expect(h.dev.ownsKey('Note'), isTrue);
      expect(h.dev.ownsKey('Order:1'), isFalse);
      expect(h.dev.ownsKey('Notebook:1'), isFalse);
      expect([
        for (final tag in ['Note:1', 'Note[]', 'Note[]:open'])
          h.dev.ownsTag(tag),
      ], everyElement(isTrue));
      expect([
        for (final tag in ['Order:1', 'Order[]', 'Notebook[]'])
          h.dev.ownsTag(tag),
      ], everyElement(isFalse));
    });
  });
}
