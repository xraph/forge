import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/kit.dart';

void main() {
  test('projects the persisted replica before any network', () async {
    final storage = memoryStorage();
    final pre = await storage.open('alice');
    final space = await ReplicaSpace.open(pre, newNodeId: nextNodeId);

    await space
        .dataset(replicaKey(datasetId: '', pullPath: '/sync/pull'))
        .saveDocument(
          'notes',
          'n1',
          DocumentState(
            table: 'notes',
            pk: 'n1',
            fields: {
              'title': FieldState(
                type: CrdtType.lww,
                hlc: HLC(BigInt.one, 0, 'x'),
                nodeId: 'x',
                value: const JsonValue('cached'),
              ),
            },
          ),
        );
    await pre.close();

    final h = Harness(storage: storage)..server.gone = true;

    await h.signIn();
    expect(h.record('n1'), {'noteId': 'n1', 'title': 'cached'});
  });

  test(
    'a mutation becomes a local change, is visible at once and is pushed',
    () async {
      final h = Harness();

      await h.signIn();

      final result = await h.mutate(
        opUpdateNote,
        const TagContext(path: {'noteId': 'n1'}, body: {'title': 'Hi'}),
      );

      expect(result, {'noteId': 'n1', 'title': 'Hi'});
      expect(h.record('n1')!['title'], 'Hi');
      await pumpEventQueue(times: 50);
      expect(h.server.log.single.field, 'title');
      expect(await h.source.status('Note').first, const Synced());
    },
  );

  test(
    'cache.mutate reaches the source, and a Rejected error is thrown',
    () async {
      final h = Harness(declarations: const [rowsSync]);

      await h.signIn();
      await h.source.join(const GroveDataset('a', table: 'ds_a'));

      final result = await h.cache.mutate(
        opUpdateNote,
        TagContext(
          path: {'noteId': compositeId('a', 'r1')},
          body: const {'title': 'through the cache'},
        ),
      );

      expect(result, {'noteId': 'a:r1', 'title': 'through the cache'});
      expect(h.record('a:r1')!['title'], 'through the cache');
      await expectLater(
        h.cache.mutate(
          opUpdateNote,
          TagContext(
            path: {'noteId': compositeId('zzz', 'r1')},
            body: const {'title': 'x'},
          ),
        ),
        throwsA(isA<StateError>()),
      );
    },
    // The cache mints mutation ids from the secure random source, which the
    // Node test runner lacks.
    testOn: 'vm',
  );

  test('a create without an id gets one and lands in the store', () async {
    final h = Harness();

    await h.signIn();
    await h.mutate(opCreateNote, const TagContext(body: {'title': 'New'}));
    expect(h.record('new-id')!['title'], 'New');
  });

  test('a delete evicts the record and pushes a tombstone', () async {
    final h = Harness();

    await h.signIn();
    await h.mutate(
      opUpdateNote,
      const TagContext(path: {'noteId': 'n1'}, body: {'title': 'x'}),
    );
    await h.mutate(opDeleteNote, const TagContext(path: {'noteId': 'n1'}));
    expect(h.record('n1'), isNull);
    await pumpEventQueue(times: 50);
    expect(h.server.log.last.tombstone, isTrue);
  });

  test('remote changes merge and re-project on sync', () async {
    final h = Harness();

    await h.signIn();
    h.server.remote('n2', 'title', 'from another device', 5);
    await h.source.syncNow();
    await pumpEventQueue();
    expect(h.record('n2')!['title'], 'from another device');
  });

  test(
    'a server rejection surfaces as SyncFailed and keeps the change',
    () async {
      final h = Harness();

      await h.signIn();
      h.server.rejectField = 'title';
      await h.mutate(
        opUpdateNote,
        const TagContext(path: {'noteId': 'n1'}, body: {'title': 'mine'}),
      );
      await pumpEventQueue(times: 50);

      final status = await h.source.status('Note').first;

      expect(
        status,
        isA<SyncFailed>().having(
          (f) => (f.error as GroveChangeRejected).reason,
          'reason',
          'title is locked',
        ),
      );
      expect(h.record('n1')!['title'], 'mine');
      expect(h.events.last.status, isA<SyncFailed>());
      expect(h.source.rejected('Note').single.field, 'title');

      h.server.rejectField = null;
      h.server.remote('n1', 'title', 'server value', 1);
      await h.source.discardRejected(h.source.rejected('Note').single.key);
      await pumpEventQueue();
      expect(h.record('n1')!['title'], 'server value');
      expect(await h.source.status('Note').first, const Synced());
    },
  );

  test(
    'a 404 dataset fails typed, keeps pending, and re-probes at most once per '
    'goneRecheck',
    () {
      fakeAsync((async) {
        final h = Harness(goneRecheck: const Duration(minutes: 5));

        h.cache.setPrincipal('alice');
        async.flushMicrotasks();
        h.server.gone = true;
        unawaited(
          h.mutate(
            opUpdateNote,
            const TagContext(
              path: {'noteId': 'n1'},
              body: {'title': 'pending'},
            ),
          ),
        );
        // Fires the zero-delay push, which meets the 404.
        async.elapse(const Duration(milliseconds: 1));

        SyncStatus? last;

        h.source.status('Note').listen((s) => last = s);
        async.flushMicrotasks();
        expect(
          last,
          isA<SyncFailed>().having(
            (f) => f.error,
            'error',
            isA<GroveDatasetGone>(),
          ),
        );

        final before = h.server.paths.length;

        unawaited(
          h.mutate(
            opUpdateNote,
            const TagContext(
              path: {'noteId': 'n1'},
              body: {'title': 'still pending'},
            ),
          ),
        );
        async.elapse(const Duration(minutes: 4));
        expect(
          h.server.paths.length,
          before,
          reason: 'no request before goneRecheck, local writes included',
        );
        async.elapse(const Duration(minutes: 2));
        expect(h.server.paths.length, before + 1, reason: 'one probe');
        h.server.gone = false;
        async.elapse(const Duration(minutes: 5));
        async.flushMicrotasks();
        expect(h.server.log.last.value, const JsonValue('still pending'));
        expect(last, const Synced());
      });
    },
  );

  test('works without storage, with a memory-only replica', () async {
    final h = Harness(withStorage: false);

    await h.signIn();
    await h.mutate(
      opUpdateNote,
      const TagContext(path: {'noteId': 'n1'}, body: {'title': 'mem'}),
    );
    expect(h.record('n1')!['title'], 'mem');
  });

  test('a grove entity without a binding is refused at construction', () {
    expect(
      () => GroveSyncSource(
        declarations: const [noteSync],
        entities: noteEntities,
        bindings: const {},
        baseUrl: Uri.parse('http://x'),
      ),
      throwsArgumentError,
    );
  });

  test('declarations with a dataset must share one set of endpoints', () {
    expect(
      () => GroveSyncSource(
        declarations: const [
          rowsSync,
          SyncDeclaration(
            protocol: 'grove-crdt',
            entity: 'Other',
            pull: '/e/{id}/pull',
            push: '/e/{id}/push',
            dataset: '{id}',
          ),
        ],
        entities: const {
          ...noteEntities,
          'Other': EntityMeta(idField: 'noteId'),
        },
        bindings: const {
          'Note': GroveEntity(codec: NoteCodec()),
          'Other': GroveEntity(codec: NoteCodec()),
        },
        baseUrl: Uri.parse('http://x'),
      ),
      throwsArgumentError,
    );
  });

  test('a codec that throws turns into a rejected write', () async {
    final h = Harness(
      bindings: const {'Note': GroveEntity(codec: _BrittleCodec())},
    );

    await h.signIn();
    await expectLater(
      h.mutate(
        opUpdateNote,
        const TagContext(path: {'noteId': 'n1'}, body: {'title': 'boom'}),
      ),
      throwsA(isA<TypeError>()),
    );
    expect(h.record('n1'), isNull);
    expect(h.source.replica('Note')!.pendingCount, 0);
  });
}

/// The kit codec, except that it fails with a TypeError (an Error, not an
/// Exception) on a body titled `boom`.
final class _BrittleCodec implements WireCodec {
  const _BrittleCodec();

  @override
  Object? decode(Object? wire) => const NoteCodec().decode(wire);

  @override
  Object? encode(Object? client) {
    final title = (client! as Map<String, Object?>)['title'];

    if (title == 'boom') return (title as Object) as int;

    return const NoteCodec().encode(client);
  }
}
