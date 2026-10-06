import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/devtools.dart';
import 'package:forge_client/src/devtools/inspect.dart' as read;
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

import 'harness.dart';

const _one = TagContext(path: {'id': 1});

/// Everything about a registry entry an accidental read would move, except
/// the value, which [_values] compares by identity.
List<String> _registryState(Harness h) => [
  for (final e in h.dev.queries())
    '${e.key}|${e.mounts}|${e.stale}|${e.settledAt}|${([...e.tags]..sort()).join(',')}|${([...e.deps]..sort()).join(',')}',
];

List<Object?> _values(Harness h) => [for (final e in h.dev.queries()) e.value];

List<String> _versions(Harness h) => [
  for (final key in h.dev.entityKeys()) '$key@${h.dev.record(key)?.version}',
];

void main() {
  // The rule the whole package is built on: reading must not change anything.
  // These assertions read the cache's private LRU order on purpose, because
  // that is the thing a stray `getState` would move.
  group('inspection does not mutate the cache', () {
    test('leaves record count, versions, LRU order and registry state exactly as they were', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final list = h.mount(Ops.orderList);
      final one = h.mount(Ops.orderGet, _one);
      await h.settle();
      await h.cache.fetch(Ops.orderGet, const TagContext(path: {'id': 2}));
      await h.settle();

      final before = (
        records: h.dev.records,
        version: h.dev.version,
        frameVersion: h.dev.frameVersion,
        tombstones: h.dev.tombstones,
        tracked: h.dev.tracked,
        remembered: h.dev.remembered,
        mounted: h.dev.mounted,
        indexedTags: h.dev.indexedTags,
        stampedTags: h.dev.stampedTags,
      );
      final lru = h.dev.lruOrder();
      final state = _registryState(h);
      final values = _values(h);
      final versions = _versions(h);

      // A full inspection pass: every read the API offers, over everything.
      // TS also calls `sockets()` and `streams()`; the Dart devtools do not
      // port the stream panels (see the plan's coverage notes).
      devtools
        ..snapshot()
        ..store()
        ..queries()
        ..tags()
        ..log()
        ..entities()
        ..entities(const read.EntityFilter(type: 'Order'))
        ..records();

      for (final record in devtools.entities()) {
        devtools
          ..entity(record.key)
          ..dependents(record.key);
      }

      for (final query in devtools.queries()) {
        devtools
          ..query(query.key)
          ..detail(query.key)
          ..whyNotRefetched(query.key)
          ..whyRefetched(query.key)
          ..explain(query.key);
      }

      devtools
        ..wouldInvalidate(Ops.orderUpdate, _one)
        ..wouldInvalidate(Ops.orderCreate, const TagContext(body: {}), {
          'id': 9,
        });

      // The free functions, which is the surface the service handlers use.
      read.snapshot(h.dev);
      read.entities(h.dev);
      read.tags(h.dev);
      read.records(h.dev);

      expect((
        records: h.dev.records,
        version: h.dev.version,
        frameVersion: h.dev.frameVersion,
        tombstones: h.dev.tombstones,
        tracked: h.dev.tracked,
        remembered: h.dev.remembered,
        mounted: h.dev.mounted,
        indexedTags: h.dev.indexedTags,
        stampedTags: h.dev.stampedTags,
      ), before);
      // The LRU order, in order: a single stray `getState` shows up here.
      expect(h.dev.lruOrder(), lru);
      expect(_versions(h), versions);
      expect(_registryState(h), state);

      // Including the value by identity: rehydrating would replace it.
      final after = _values(h);
      for (var i = 0; i < values.length; i++) {
        expect(
          identical(after[i], values[i]),
          isTrue,
          reason: 'registry value $i was replaced',
        );
      }

      await list.cancel();
      await one.cancel();
      devtools.dispose();
    });

    test(
      'discriminates: the same assertions fail when getState is used instead',
      () async {
        final h = Harness();
        final list = h.mount(Ops.orderList);
        await h.settle();
        await h.cache.fetch(Ops.orderGet, const TagContext(path: {'id': 2}));
        await h.settle();

        final order = h.dev.lruOrder();

        // The read an inspector must not perform, on the query at the front of
        // the LRU order. Proves the test above can fail.
        h.cache.getState(Ops.orderList, TagContext.empty);

        expect(h.dev.lruOrder(), isNot(order));

        await list.cancel();
      },
    );

    // The brief's version of this wrote into `fields` and checked the store.
    // Every snapshot is now a bounded, read-only copy, so the write itself is
    // refused, which is stronger: it cannot reach the store because it cannot
    // happen. The store assertion stays.
    test('hands out copies, so a panel writing to a snapshot cannot move the store', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final snapshot = devtools.entity('Order:1');

      expect(snapshot, isNotNull);
      expect(() => snapshot!.fields['total'] = 99999, throwsUnsupportedError);
      expect(h.dev.record('Order:1')!.data['total'], 10);

      await sub.cancel();
      devtools.dispose();
    });

    // The counters above are cache state. These are the effects: nothing was
    // written, nothing was announced, nothing went out on the wire.
    test('writes nothing, announces nothing and fetches nothing, with an overlay on the stack', () async {
      final h = Harness();
      var events = 0;
      h.cache.observer = (_) => events++;
      final devtools = attach(h.cache, clock: CounterClock());
      var watched = 0;
      final watch = h.cache
          .watch(Ops.orderList, TagContext.empty)
          .listen((_) => watched++);
      await h.settle();

      final layer = h.dev.pushMerge(
        'Order:1',
        {'total': 99},
        tags: ['Order[]'],
      );

      // An unmounted but remembered query, and a key nothing holds.
      await h.cache.fetch(Ops.orderGet, const TagContext(path: {'id': 2}));
      await h.settle();

      Object counters() => (
        events: events,
        watched: watched,
        wire: h.calls.length,
        storeWrites: h.dev.version,
        frames: h.dev.frameVersion,
        overlayStack: h.cache.overlays.version,
        logged: devtools.log().length,
        records: h.dev.records,
        lru: h.dev.lruOrder().join(','),
      );
      final before = counters();

      devtools
        ..snapshot()
        ..store()
        ..queries()
        ..tags()
        ..entities()
        ..records()
        ..overlays()
        ..entity('Order:1')
        ..entity('Order:404')
        ..dependents('Order:1')
        ..baseRecord('Order:1')
        ..foldedRecord('Order:1')
        ..foldedRecord('Order:404')
        ..countEntities()
        ..detail(h.key(Ops.orderList))
        ..detail('GET /nothing')
        ..query(h.key(Ops.orderList));
      await h.settle();

      expect(counters(), before);
      // The fold is a read: the layer is still pending and still unpromoted.
      expect(devtools.foldedRecord('Order:1')!['total'], 99);
      expect(devtools.baseRecord('Order:1')!['total'], 10);
      expect(devtools.overlays().map((l) => l.id), [layer]);

      await watch.cancel();
      devtools.dispose();
    });
  });

  group('what leaves is bounded and read-only', () {
    test(
      'caps a record sized by a response, and refuses a write to any copy',
      () async {
        final h = Harness()
          ..reply('GET /orders/{id}', {
            'id': 1,
            'total': 10,
            'essay': 'x' * 5000,
            for (var i = 0; i < 400; i++) 'field$i': i,
          });
        final devtools = attach(h.cache, clock: CounterClock());
        final one = h.mount(Ops.orderGet, _one);
        final list = h.mount(Ops.orderList);
        await h.settle();

        final fields = devtools.entity('Order:1')!.fields;

        // Fifty keys and the marker for the rest; no string past a thousand.
        expect(fields.length, lessThanOrEqualTo(51));
        expect(fields.keys, contains('[more]'));
        expect(jsonEncode(fields).length, lessThan(5000));
        expect(() => fields['total'] = 0, throwsUnsupportedError);
        expect(() => fields.remove('id'), throwsUnsupportedError);

        final base = devtools.baseRecord('Order:1')!;
        final folded = devtools.foldedRecord('Order:1')!;

        expect(base.length, lessThanOrEqualTo(51));
        expect(folded.length, lessThanOrEqualTo(51));
        expect(() => base['total'] = 0, throwsUnsupportedError);
        expect(() => folded['total'] = 0, throwsUnsupportedError);

        final detail = devtools.detail(h.key(Ops.orderList))!;
        final value = detail.value! as List<Object?>;

        expect(() => value.add(1), throwsUnsupportedError);
        expect(
          () => (value.first! as Map<String, Object?>)['total'] = 0,
          throwsUnsupportedError,
        );
        expect(() => detail.tags.add('x'), throwsUnsupportedError);
        expect(() => detail.deps.add('x'), throwsUnsupportedError);
        expect(
          () => (detail.query.args! as Map<String, Object?>)['x'] = 1,
          throwsUnsupportedError,
        );
        expect(
          () => devtools.queries().add(devtools.queries().first),
          throwsUnsupportedError,
        );
        expect(() => devtools.entities().clear(), throwsUnsupportedError);
        expect(
          () => devtools.tags().first.carriers.add('x'),
          throwsUnsupportedError,
        );
        expect(h.dev.record('Order:1')!.data.length, greaterThan(400));

        await one.cancel();
        await list.cancel();
        devtools.dispose();
      },
    );
  });

  group('a read never shows the previous principal', () {
    // Everything an inspector can be asked, as one string, so a planted secret
    // anywhere in any answer shows.
    String everything(Devtools d, Harness h) => jsonEncode([
      d.snapshot().toJson(),
      d.store().toJson(),
      [for (final q in d.queries()) q.toJson()],
      [for (final e in d.entities()) e.toJson()],
      [for (final t in d.tags()) t.toJson()],
      [for (final r in d.records()) r.toJson()],
      [for (final o in d.overlays()) o.toJson()],
      d.query(h.key(Ops.orderList))?.toJson(),
      d.detail(h.key(Ops.orderList))?.toJson(),
      d.entity('Order:1')?.toJson(),
      d.entity('Customer:c1')?.toJson(),
      d.baseRecord('Order:1'),
      d.foldedRecord('Order:1'),
      [for (final q in d.dependents('Order:1')) q.toJson()],
      [for (final q in d.dependents('Customer:c1')) q.toJson()],
      d.countEntities(),
      [for (final entry in d.log()) entry.toJson()],
    ]);

    // The window is between the cache's changing notification and its clear:
    // `principal` already says bob, the store is still alice's.
    for (final (name, readerFirst) in [('after', false), ('before', true)]) {
      test(
        'answers as an empty cache while the change is pending, for a reader that runs $name the recorder',
        () async {
          const secret = 'alice-ssn';
          final h = Harness()
            ..reply('GET /orders', [
              {'id': 1, 'total': 10, 'secret': secret},
            ]);
          h.cache.setPrincipal('alice');

          Devtools? devtools;
          final seen = <String, Object?>{};

          void reader(String? _) {
            final d = devtools;

            if (d == null) return;

            seen['stillHolds'] = h.dev.records;
            seen['everything'] = everything(d, h);
            seen['store'] = d.store().toJson();
            seen['count'] = d.countEntities();
            seen['entity'] = d.entity('Order:1');
            seen['detail'] = d.detail(h.key(Ops.orderList));
            seen['query'] = d.query(h.key(Ops.orderList));
            seen['base'] = d.baseRecord('Order:1');
            seen['folded'] = d.foldedRecord('Order:1');
            seen['entities'] = d.entities();
            seen['queries'] = d.queries();
            seen['records'] = d.records();
            seen['overlays'] = d.overlays();
            seen['tags'] = d.tags();
            seen['dependents'] = d.dependents('Order:1');
          }

          if (readerFirst) h.cache.watchPrincipalChanging(reader);
          devtools = attach(h.cache, clock: CounterClock());
          if (!readerFirst) h.cache.watchPrincipalChanging(reader);

          final sub = h.mount(Ops.orderList);
          await h.settle();
          await sub.cancel();
          h.dev.pushMerge('Order:1', {'total': 99});

          expect(everything(devtools, h), contains(secret));

          h.cache.setPrincipal('bob');

          // The cache really did still hold alice's data while the reader ran.
          expect(seen['stillHolds'], greaterThan(0));
          expect(seen['everything'], isNot(contains(secret)));
          expect(seen['store'], {
            for (final key in const [
              'records',
              'version',
              'frameVersion',
              'tombstones',
              'tracked',
              'remembered',
              'mounted',
              'indexedTags',
              'stampedTags',
            ])
              key: 0,
          });
          expect(seen['count'], 0);
          expect(seen['entity'], isNull);
          expect(seen['detail'], isNull);
          expect(seen['query'], isNull);
          expect(seen['base'], isNull);
          expect(seen['folded'], isNull);
          expect(seen['entities'], isEmpty);
          expect(seen['queries'], isEmpty);
          expect(seen['records'], isEmpty);
          expect(seen['overlays'], isEmpty);
          expect(seen['tags'], isEmpty);
          expect(seen['dependents'], isEmpty);

          devtools.dispose();
        },
      );
    }

    test('cannot reach a planted secret once the switch has settled', () async {
      const secret = 'alice-ssn';
      final h = Harness()
        ..reply('GET /orders', [
          {
            'id': 1,
            'total': 10,
            'secret': secret,
            'customer': {'id': 'c1', 'name': secret},
          },
        ]);
      h.cache.setPrincipal('alice');
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();
      // Nothing watching, so nothing refetches for the next principal.
      await sub.cancel();
      devtools
        ..patchEntity('Order:1', {'note': secret})
        ..actions.hold(h.key(Ops.orderList), 'error');

      expect(everything(devtools, h), contains(secret));

      h.cache.setPrincipal('bob');
      await h.settle();

      final after = everything(devtools, h);

      expect(after, isNot(contains(secret)));
      expect(devtools.entities(), isEmpty);
      expect(devtools.queries(), isEmpty);
      expect(devtools.overlays(), isEmpty);
      expect(devtools.entity('Order:1'), isNull);
      expect(devtools.baseRecord('Order:1'), isNull);
      expect(devtools.foldedRecord('Order:1'), isNull);
      expect(devtools.detail(h.key(Ops.orderList)), isNull);
      // The log holds the principal marker and nothing else of alice's.
      expect(devtools.log().single, isA<PrincipalLog>());

      devtools.dispose();
    });

    test('answers empty once the inspector is disposed', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      devtools.dispose();

      expect(devtools.entities(), isEmpty);
      expect(devtools.queries(), isEmpty);
      expect(devtools.entity('Order:1'), isNull);
      expect(devtools.snapshot().queries, isEmpty);
      expect(devtools.store().records, 0);
      // The cache itself is untouched.
      expect(h.dev.records, greaterThan(0));

      await sub.cancel();
    });
  });

  group('an entity a sync source owns', () {
    test('can be read, and reading it moves nothing', () async {
      final (h, _) = await ownedHarness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.cache.watch(noteList, TagContext.empty).listen((_) {});
      await h.settle();

      final version = h.dev.record('Note:1')!.version;
      final data = h.dev.record('Note:1')!.data;
      final snapshot = devtools.entity('Note:1');

      expect(snapshot, isNotNull);
      expect(snapshot!.fields['body'], ownedBody);
      expect(devtools.baseRecord('Note:1')!['body'], ownedBody);
      expect(
        devtools.entities(const read.EntityFilter(type: 'Note')),
        hasLength(1),
      );
      expect(devtools.countEntities(const read.EntityFilter(type: 'Note')), 1);
      expect(h.dev.record('Note:1')!.version, version);
      expect(identical(h.dev.record('Note:1')!.data, data), isTrue);

      await sub.cancel();
      devtools.dispose();
    });
  });

  group('the bulk record read', () {
    test(
      'reports every tracked record, and nothing that is sized by a response',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        final listKey = h.key(Ops.orderList);
        final found = devtools.records();

        expect(found.map((r) => r.key), contains(listKey));

        final list = found.firstWhere((r) => r.key == listKey);

        expect(
          (list.status, list.fetching, list.settled),
          ('success', false, true),
        );
        expect(list.toJson().keys.toList()..sort(), [
          'fetching',
          'frameRestarts',
          'inflight',
          'key',
          'restart',
          'settled',
          'status',
        ]);

        await sub.cancel();
        devtools.dispose();
      },
    );

    test('agrees with detail() on the fields they share', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final list = h.mount(Ops.orderList);
      final one = h.mount(Ops.orderGet, _one);
      await h.settle();

      for (final record in devtools.records()) {
        final detail = devtools.detail(record.key);

        expect(detail?.status, record.status);
        expect(detail?.fetching, record.fetching);
        expect(detail?.inflight, record.inflight);
        expect(detail?.frameRestarts, record.frameRestarts);
      }

      await list.cancel();
      await one.cancel();
      devtools.dispose();
    });
  });

  group('what is in the cache for one entity', () {
    test('reports its version, its fields, its references and its dependents', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final list = h.mount(Ops.orderList);
      final one = h.mount(Ops.orderGet, _one);
      await h.settle();

      final record = devtools.entity('Order:1')!;

      expect(
        (record.key, record.type, record.id, record.version),
        ('Order:1', 'Order', '1', 1),
      );
      expect(record.fields['total'], 10);
      // The customer was lifted into its own record, so the order holds a
      // reference to it rather than a copy.
      expect(record.refs, ['Customer:c1']);
      expect(
        record.dependents,
        [h.key(Ops.orderList), h.key(Ops.orderGet, _one)]..sort(),
      );

      // The version moves only when the data does. This response is identical.
      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      expect(devtools.entity('Order:1')!.version, 1);

      // And now one that changes something.
      h
        ..reply('PATCH /orders/{id}', {'id': 1, 'total': 11})
        ..reply('GET /orders', [
          {
            'id': 1,
            'total': 11,
            'customer': {'id': 'c1', 'name': 'Ada'},
          },
        ])
        ..reply('GET /orders/{id}', {'id': 1, 'total': 11});
      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      expect(devtools.entity('Order:1')!.version, 2);
      expect(devtools.entity('Order:1')!.fields['total'], 11);

      await list.cancel();
      await one.cancel();
      devtools.dispose();
    });

    test('answers which queries depend on an entity, including through a nested reference', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      // `Customer:c1` is nowhere in the list's `provides`; it is there because
      // the response's orders pointed at it.
      expect(devtools.dependents('Customer:c1').map((q) => q.key), [
        h.key(Ops.orderList),
      ]);

      await sub.cancel();
      devtools.dispose();
    });

    test('returns nothing for an entity the store does not hold', () {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());

      expect(devtools.entity('Order:404'), isNull);

      devtools.dispose();
    });
  });

  group('paging the entity table', () {
    test(
      'filters by type and substring, and pages with an offset and a limit',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        expect(devtools.countEntities(), 3);
        expect(
          devtools.countEntities(const read.EntityFilter(type: 'Order')),
          2,
        );
        expect(
          devtools.countEntities(const read.EntityFilter(contains: 'c1')),
          1,
        );
        expect(
          devtools
              .entities(
                const read.EntityFilter(type: 'Order', offset: 1, limit: 5),
              )
              .map((e) => e.key),
          ['Order:2'],
        );
        expect(
          devtools.entities(const read.EntityFilter(limit: 1)),
          hasLength(1),
        );
        expect(devtools.entities().every((e) => e.dependents.isEmpty), isTrue);

        await sub.cancel();
        devtools.dispose();
      },
    );
  });

  group('the tag graph', () {
    test('separates the queries that carry a tag from the ones an invalidation reaches', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final listKey = h.key(Ops.orderList);
      final mounted = devtools.tags().firstWhere((row) => row.tag == 'Order[]');

      expect(mounted.carriers, [listKey]);
      expect(mounted.mounted, [listKey]);

      await sub.cancel();

      // Unmounted: still a carrier, no longer in the index.
      final after = devtools.tags().firstWhere((row) => row.tag == 'Order[]');

      expect(after.carriers, [listKey]);
      expect(after.mounted, isEmpty);

      devtools.dispose();
    });
  });

  group('detail', () {
    test(
      'joins the registry entry to the record, so status and error are visible',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        final detail = devtools.detail(h.key(Ops.orderList))!;

        expect(detail.status, 'success');
        expect(detail.fetching, isFalse);
        expect(detail.mounts, 1);
        expect(detail.tags, contains('Order[]'));
        expect(detail.provides, ['Order[]']);
        expect(detail.error, isNull);
        expect(detail.value, isA<List<Object?>>());

        await sub.cancel();
        devtools.dispose();
      },
    );

    test('answers undefined for a key nothing is tracking', () {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());

      expect(devtools.detail('GET /nothing'), isNull);

      devtools.dispose();
    });

    test(
      'returns a copy of the value, so a panel cannot move the store',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        final first = devtools.detail(h.key(Ops.orderList))!;
        final second = devtools.detail(h.key(Ops.orderList))!;

        expect(identical(first.value, second.value), isFalse);

        await sub.cancel();
        devtools.dispose();
      },
    );
  });

  group('the overlay stack', () {
    test(
      'lists the pending writes in push order, with their patches and tags',
      () {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());

        h.dev.pushMerge('Order:1', {'total': 99}, tags: ['Order[]']);
        h.dev.pushDelete('Order:2', tags: ['Order:2'], created: 'Order:~opt1');

        final layers = read.overlays(h.dev);

        expect(layers, hasLength(2));
        expect(layers[0].patches, [(key: 'Order:1', kind: 'merge')]);
        expect(layers[0].tags, ['Order[]']);
        expect(layers[0].created, isNull);
        expect(layers[1].patches, [(key: 'Order:2', kind: 'delete')]);
        expect(layers[1].created, 'Order:~opt1');

        devtools.dispose();
      },
    );

    // A merge source is application data. Carrying it into a snapshot would
    // put it into a structure the panel holds across repaints.
    test(
      'carries the shape of each patch and never the value it would write',
      () {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());

        h.dev.pushMerge('Order:1', {'total': 99, 'note': 'do not retain me'});

        expect(
          jsonEncode([
            for (final layer in read.overlays(h.dev)) layer.toJson(),
          ]),
          isNot(contains('do not retain me')),
        );

        devtools.dispose();
      },
    );
  });
}
