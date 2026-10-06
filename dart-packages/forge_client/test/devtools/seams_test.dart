import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/release.dart';
import 'package:forge_client/src/devtools/seams.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:test/test.dart';

import 'harness.dart';

void main() {
  test('the devtools are compiled into test builds', () {
    expect(kForgeDevtools, isTrue);
  });

  group('DevCache reads', () {
    test('sees the registry, the store and the tracked records after a list settles', () async {
      final h = Harness();
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final key = h.key(Ops.orderList);
      final query = h.dev.query(key)!;

      expect(query.mounts, 1);
      expect(query.settled, isTrue);
      expect(query.operation, 'GET /orders');
      expect(query.provides, ['Order[]']);
      expect(
        query.tags,
        containsAll(<String>['Order[]', 'Order:1', 'Order:2']),
      );
      expect(query.deps, containsAll(<String>['Order:1', 'Customer:c1']));
      expect(h.dev.queries().map((q) => q.key), contains(key));
      expect(h.dev.mountedKeysFor('Order[]'), [key]);
      expect(h.dev.remembered, 1);
      expect(h.dev.mounted, 1);
      expect(
        h.dev.entityKeys(),
        containsAll(<String>['Order:1', 'Order:2', 'Customer:c1']),
      );
      expect(h.dev.record('Order:1')!.version, 1);
      expect(h.dev.record('Order:1')!.data['total'], 10);
      expect(h.dev.record('Order:404'), isNull);
      expect(h.dev.records, 3);
      expect(h.dev.tracked, 1);

      final tracked = h.dev.trackedRecords().single;

      expect(tracked.key, key);
      expect(tracked.status, 'success');
      expect(tracked.fetching, isFalse);
      expect(tracked.inflight, isFalse);
      expect(h.dev.lruOrder(), [key]);

      await sub.cancel();
    });

    test('marks stale, evicts and notifies through the seam', () async {
      final h = Harness();
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final key = h.key(Ops.orderList);

      h.dev.markStale(h.dev.query(key)!);
      expect(h.dev.query(key)!.stale, isTrue);

      expect(h.dev.hasEntity('Order:2'), isTrue);
      expect(h.dev.evictEntity('Order:2'), isTrue);
      expect(h.dev.hasEntity('Order:2'), isFalse);
      h.dev.notifyChanged();

      await sub.cancel();
    });

    test('pushes, folds, lists, takes and promotes overlay layers', () async {
      final h = Harness();
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final id = h.dev.pushMerge('Order:1', {'total': 99});

      expect(h.dev.overlays().single.patches, [
        (key: 'Order:1', kind: 'merge'),
      ]);
      expect(h.dev.folded('Order:1')!['total'], 99);
      expect(h.dev.record('Order:1')!.data['total'], 10);
      expect(h.dev.takeOverlay(id), isTrue);
      expect(h.dev.takeOverlay(id), isFalse);
      expect(h.dev.overlays(), isEmpty);

      final second = h.dev.pushMerge('Order:1', {'total': 98});

      expect(h.dev.promoteOverlay(second), isTrue);
      expect(h.dev.record('Order:1')!.data['total'], 98);

      h.dev.pushDelete('Order:2', tags: ['Order:2'], created: 'Order:~opt1');

      final layer = h.dev.overlays().single;

      expect(layer.patches, [(key: 'Order:2', kind: 'delete')]);
      expect(layer.created, 'Order:~opt1');
      expect(layer.tags, ['Order:2']);
      expect(layer.places, isFalse);

      await sub.cancel();
    });

    test('reports the sync sources and the storage session the cache was built with', () {
      expect(Harness().dev.syncSources, isEmpty);
      expect(Harness().dev.session, isNull);
    });
  });

  group('DevEvent', () {
    test('normalises query, mutation, invalidation and frame events', () async {
      final h = Harness();
      final seen = <DevEvent>[];
      h.cache.observer = (event) => seen.add(devEvent(event));

      final sub = h.mount(Ops.orderList);
      await h.settle();
      await h.cache.mutate(Ops.orderUpdate, const TagContext(path: {'id': 1}));
      h.flush();
      await h.settle();
      debugApplyFrames(h.cache, orderBinding, {'id': 1, 'total': 5});

      expect(seen.whereType<DevQueryEvent>(), isNotEmpty);
      expect(
        seen.whereType<DevMutationEvent>().single.meta.id,
        Ops.orderUpdate.id,
      );
      expect(
        seen.whereType<DevInvalidatedEvent>().map((e) => e.key),
        contains(h.key(Ops.orderList)),
      );

      final frames = seen.whereType<DevFramesEvent>().single;

      expect(frames.count, 1);
      expect(frames.tags, contains('Order[]'));
      expect(frames.frames.single.channel, '/ws/orders');
      expect(frames.frames.single.message, 'order.updated');
      expect(frames.frames.single.intent, 'upsert');
      expect(frames.frames.single.entity, 'Order');

      await sub.cancel();
    });

    test('normalises outbox and sync events', () {
      expect(
        devEvent(debugOutboxEnqueued('m1', 'op_order_create')),
        isA<DevOutboxEnqueued>()
            .having((e) => e.mutationId, 'mutationId', 'm1')
            .having((e) => e.operation, 'operation', 'op_order_create'),
      );
      expect(
        devEvent(debugOutboxReplayed('m1', 'op_order_create')),
        isA<DevOutboxReplayed>().having(
          (e) => e.mutationId,
          'mutationId',
          'm1',
        ),
      );
      expect(
        devEvent(
          debugOutboxFailed(
            'm1',
            'op_order_create',
            StateError('conflict on Order:9'),
          ),
        ),
        isA<DevOutboxFailed>()
            .having((e) => e.operation, 'operation', 'op_order_create')
            .having(
              (e) => e.failure,
              'failure',
              contains('conflict on Order:9'),
            ),
      );
      expect(
        devEvent(debugSyncStatusChanged('Doc', const Pending(3))),
        isA<DevSyncStatus>()
            .having((e) => e.entity, 'entity', 'Doc')
            .having((e) => e.status, 'status', 'pending')
            .having((e) => e.pending, 'pending', 3),
      );
      expect(syncStatusName(const Synced()), 'synced');
      expect(syncStatusName(const Offline()), 'offline');
      expect(syncStatusName(SyncFailed(StateError('x'))), 'failed');
    });

    test('truncates a long failure message rather than retaining it whole', () {
      final event = devEvent(
        debugOutboxFailed('m1', 'op_order_create', StateError('y' * 5000)),
      ) as DevOutboxFailed;

      expect(event.failure.length, lessThan(220));
      expect(event.failure, endsWith('...'));
    });
  });

  group('DevRequestEvent', () {
    test(
      'maps the transport events and fills the debug observer slot',
      () async {
        final fromConstructor = <RequestEvent>[];
        final fromSlot = <DevRequestEvent>[];
        final rest = RestTransport(
          baseUrl: Uri.parse('http://forge.test'),
          client: MockClient(
            (request) async => http.Response(
              jsonEncode({'ok': true}),
              200,
              headers: {'content-type': 'application/json'},
            ),
          ),
          observer: fromConstructor.add,
        );

        rest.debugObserver = (event) {
          final mapped = devRequestEvent(event);
          if (mapped != null) fromSlot.add(mapped);
        };

        await rest.execute(
          const TransportRequest(meta: Ops.orderList, args: TagContext.empty),
        );

        // Both slots hear the same request: the debug slot never replaces the
        // application's own observer.
        expect(fromConstructor, isNotEmpty);
        expect(
          fromSlot.first,
          isA<DevRequestStarted>()
              .having((e) => e.method, 'method', 'GET')
              .having((e) => e.path, 'path', '/orders'),
        );
        expect(
          fromSlot.last,
          isA<DevRequestSettled>().having((e) => e.ok, 'ok', isTrue),
        );
        expect(fromSlot.map((e) => e.id).toSet(), hasLength(1));
      },
    );

    // The 01a ruling: an observer never changes a request's outcome. Each slot
    // has its own guard, so one that throws fails neither the request nor the
    // other slot.
    test('a throwing observer in either slot neither fails the request nor silences the other', () async {
      for (final debugThrows in [true, false]) {
        final heard = <RequestEvent>[];
        void thrower(RequestEvent event) => throw StateError('observer bug');

        final rest = RestTransport(
          baseUrl: Uri.parse('http://forge.test'),
          client: MockClient(
            (request) async => http.Response(
              jsonEncode({'ok': true}),
              200,
              headers: {'content-type': 'application/json'},
            ),
          ),
          observer: debugThrows ? heard.add : thrower,
        );

        rest.debugObserver = debugThrows ? thrower : heard.add;

        final value = await rest.execute(
          const TransportRequest(meta: Ops.orderList, args: TagContext.empty),
        );

        expect(value, {'ok': true});
        expect(heard.last, isA<RequestSettled>());
      }
    });
  });
}
