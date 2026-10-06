import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/kit.dart';

void main() {
  late Harness h;

  setUp(() async {
    h = Harness(declarations: const [rowsSync]);
    await h.signIn();
  });

  test('a dataset joined at runtime syncs into the shared store', () async {
    expect(h.source.joined, isEmpty);
    await h.source.join(const GroveDataset('a', table: 'ds_a'));
    await h.mutate(
      opUpdateNote,
      TagContext(
        path: {'noteId': compositeId('a', 'r1')},
        body: const {'title': 'in a'},
      ),
    );
    await pumpEventQueue(times: 50);
    expect(h.record('a:r1'), {'noteId': 'a:r1', 'title': 'in a'});

    final pushed = h.server.logOf('a').single;

    expect(pushed.table, 'ds_a');
    expect(pushed.pk, 'r1');
  });

  test('two datasets sync at once and the same pk does not collide', () async {
    h.server
      ..remote('r1', 'title', 'row one of a', 3, dataset: 'a', table: 'ds_a')
      ..remote('r1', 'title', 'row one of b', 4, dataset: 'b', table: 'ds_b');
    await h.source.join(const GroveDataset('a'));
    await h.source.join(const GroveDataset('b'));
    await h.source.syncNow();
    await pumpEventQueue();
    expect(h.record('a:r1')!['title'], 'row one of a');
    expect(h.record('b:r1')!['title'], 'row one of b');
    await h.mutate(
      opUpdateNote,
      TagContext(
        path: {'noteId': compositeId('b', 'r1')},
        body: const {'title': 'edited in b'},
      ),
    );
    await pumpEventQueue(times: 50);
    expect(h.server.logOf('b').last.value, const JsonValue('edited in b'));
    expect(h.server.logOf('a'), hasLength(1));
    expect(h.record('a:r1')!['title'], 'row one of a');
  });

  test('a create in a dataset takes the dataset from the path and gets a '
      'composite id', () async {
    await h.source.join(const GroveDataset('a', table: 'ds_a'));
    await h.source.join(const GroveDataset('b', table: 'ds_b'));

    final created = await h.mutate(
      const OperationMeta(
        id: 'op_create_row',
        method: 'POST',
        path: '/d/{id}/rows',
        entity: 'Note',
        rootType: 'Note',
        bodyCodec: NoteCodec(),
        responseCodec: NoteCodec(),
      ),
      const TagContext(path: {'id': 'b'}, body: {'title': 'new row'}),
    );

    expect(created, {'noteId': 'b:new-id', 'title': 'new row'});
    await pumpEventQueue(times: 50);
    expect(h.server.logOf('b').single.pk, 'new-id');
  });

  test(
    'with the dataset in the path, a raw pk holding a colon is not cut at it',
    () async {
      await h.source.join(const GroveDataset('a', table: 'ds_a'));
      await h.source.join(const GroveDataset('b', table: 'ds_b'));

      const opPutRow = OperationMeta(
        id: 'op_put_row',
        method: 'PUT',
        path: '/d/{id}/rows/{rowId}',
        entity: 'Note',
        rootType: 'Note',
        bodyCodec: NoteCodec(),
        responseCodec: NoteCodec(),
      );

      // `b:raw` is a raw pk of dataset a, not row `raw` of dataset b.
      final written = await h.mutate(
        opPutRow,
        const TagContext(
          path: {'id': 'a', 'rowId': 'b:raw'},
          body: {'title': 'colon pk'},
        ),
      );

      expect(written, {'noteId': 'a:b:raw', 'title': 'colon pk'});

      // The dataset's own prefix is still dropped.
      await h.mutate(
        opPutRow,
        const TagContext(
          path: {'id': 'a', 'rowId': 'a:r2'},
          body: {'title': 'prefixed'},
        ),
      );
      await pumpEventQueue(times: 50);
      expect([for (final c in h.server.logOf('a')) c.pk], ['b:raw', 'r2']);
      expect(h.server.logOf('b'), isEmpty);
    },
  );

  test('statusOf reports one dataset; status folds across datasets', () async {
    await h.source.join(const GroveDataset('a', table: 'ds_a'));
    await h.source.join(const GroveDataset('b', table: 'ds_b'));
    h.server.goneDatasets.add('a');
    await h.source.syncNow();
    expect(
      await h.source.statusOf('a').first,
      isA<SyncFailed>().having(
        (f) => f.error,
        'error',
        isA<GroveDatasetGone>(),
      ),
    );
    expect(await h.source.statusOf('b').first, const Synced());
    expect(await h.source.status('Note').first, isA<SyncFailed>());
  });

  test(
    'leave evicts rows and keeps the replica; rejoin restores it offline',
    () async {
      h.server.remote('r1', 'title', 'kept', 3, dataset: 'a', table: 'ds_a');
      await h.source.join(const GroveDataset('a'));
      await h.source.syncNow();
      await pumpEventQueue();
      expect(h.record('a:r1')!['title'], 'kept');
      await h.source.leave('a');
      expect(h.record('a:r1'), isNull);
      expect(h.source.joined, isEmpty);
      h.server.gone = true;
      await h.source.join(const GroveDataset('a'));
      expect(h.record('a:r1')!['title'], 'kept');
    },
  );

  test('leave with erase deletes the dataset namespace', () async {
    await h.source.join(const GroveDataset('a', table: 'ds_a'));
    await h.mutate(
      opUpdateNote,
      TagContext(
        path: {'noteId': compositeId('a', 'r1')},
        body: const {'title': 'x'},
      ),
    );
    await h.source.leave('a', erase: true);
    expect(await h.cache.session!.namespace('grove/a').scan(''), isEmpty);
    expect(h.record('a:r1'), isNull);
  });

  test('a write to a dataset that is not joined is rejected', () async {
    await h.source.join(const GroveDataset('a', table: 'ds_a'));
    await expectLater(
      h.mutate(
        opUpdateNote,
        TagContext(
          path: {'noteId': compositeId('zzz', 'r1')},
          body: const {'title': 'x'},
        ),
      ),
      throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          contains('not joined'),
        ),
      ),
    );
  });

  test(
    'a write before the dataset table is known is rejected with guidance',
    () async {
      await h.source.join(const GroveDataset('a'));
      await expectLater(
        h.mutate(
          opUpdateNote,
          TagContext(
            path: {'noteId': compositeId('a', 'r1')},
            body: const {'title': 'x'},
          ),
        ),
        throwsA(
          isA<StateError>().having(
            (e) => e.message,
            'message',
            contains('GroveDataset.table'),
          ),
        ),
      );
    },
  );

  test(
    'a write with two joined datasets and no dataset hint is ambiguous',
    () async {
      await h.source.join(const GroveDataset('a', table: 'ds_a'));
      await h.source.join(const GroveDataset('b', table: 'ds_b'));
      await expectLater(
        h.mutate(opCreateNote, const TagContext(body: {'title': 'x'})),
        throwsArgumentError,
      );
    },
  );

  test('a second join adopts a table it did not know', () async {
    await h.source.join(const GroveDataset('a'));
    await h.source.join(const GroveDataset('a', table: 'ds_a'));
    expect(h.source.joined, {'a'});
    await h.mutate(
      opUpdateNote,
      TagContext(
        path: {'noteId': compositeId('a', 'r1')},
        body: const {'title': 'now known'},
      ),
    );
    await pumpEventQueue(times: 50);
    expect(h.server.logOf('a').single.table, 'ds_a');
  });

  test('stop forgets joined datasets', () async {
    await h.source.join(const GroveDataset('a', table: 'ds_a'));
    h.cache.setPrincipal('bob');
    await pumpEventQueue(times: 50);
    expect(h.source.joined, isEmpty);
  });

  test('a join while signed out throws and is not queued', () async {
    final fresh = Harness(declarations: const [rowsSync]);
    final signedOut = throwsA(
      isA<StateError>().having(
        (e) => e.message,
        'message',
        'join requires a signed-in principal',
      ),
    );

    // Before any principal.
    await expectLater(fresh.source.join(const GroveDataset('a')), signedOut);

    // In the sign-out window, and after it.
    h.cache.setPrincipal(null);
    await expectLater(h.source.join(const GroveDataset('a')), signedOut);
    await h.cache.idle;
    await expectLater(h.source.join(const GroveDataset('a')), signedOut);
    expect(h.source.joined, isEmpty);
  });

  test(
    'sign out, a join attempt, then bob signs in: bob joins nothing',
    () async {
      await h.source.join(const GroveDataset('a', table: 'ds_a'));
      h.cache.setPrincipal(null);
      await expectLater(
        h.source.join(const GroveDataset('b', table: 'ds_b')),
        throwsStateError,
      );
      await h.cache.idle;
      await expectLater(
        h.source.join(const GroveDataset('c', table: 'ds_c')),
        throwsStateError,
      );

      final sent = h.server.paths.length;

      await h.signIn('bob');
      expect(h.source.joined, isEmpty);
      expect(
        h.server.paths.skip(sent),
        isEmpty,
        reason: 'bob syncs no dataset',
      );
    },
  );

  test(
    'the datasets callback runs after start returned and is joined',
    () async {
      final asked = <SyncDeclaration>[];
      final fresh = Harness(
        declarations: const [rowsSync],
        datasets: (d) async {
          asked.add(d);
          await pumpEventQueue();

          return const [GroveDataset('a', table: 'ds_a')];
        },
      );

      await fresh.signIn();
      expect(asked.single.entity, 'Note');
      expect(fresh.source.joined, {'a'});
      expect(fresh.server.paths, contains('/d/a/pull'));
    },
  );

  test('leave with erase while no principal runs is refused', () async {
    final fresh = Harness(declarations: const [rowsSync]);

    await expectLater(
      fresh.source.leave('a', erase: true),
      throwsA(isA<StateError>()),
    );
    // Without erase there is nothing to do.
    await fresh.source.leave('a');
    expect(fresh.source.joined, isEmpty);
  });
}
