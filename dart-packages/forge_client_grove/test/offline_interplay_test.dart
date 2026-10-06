import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
// The package barrel exports the Flutter key stores; OfflineClient itself is
// plain Dart.
// ignore: implementation_imports
import 'package:forge_client_offline/src/offline_client.dart';
import 'package:test/test.dart';

import 'support/fake_grove_http.dart';
import 'support/harness.dart';
import 'support/kit.dart';

/// A REST list of a dataset's rows, so the snapshot has a query whose
/// skeleton references the grove-owned records.
const _opListRows = OperationMeta(
  id: 'op_list_rows',
  method: 'GET',
  path: '/d/{id}/rows',
  entity: 'Note',
  rootType: 'Note',
  provides: ['Note[]'],
  responseCodec: NoteCodec(),
);

/// Answers the row list the way a server would.
final class _Rows implements Transport {
  @override
  Future<Object?> execute(TransportRequest request) async => [
    {'note_id': compositeId('a', 'r1'), 'title': 'rest title'},
  ];
}

void main() {
  test(
    'through OfflineClient: after leave(erase) the snapshot holds no grove row '
    'and the namespace is empty',
    () async {
      final storage = memoryStorage();
      final server = FakeGroveHttp()
        ..remote(
          'r1',
          'title',
          'alice secret row',
          3,
          dataset: 'a',
          table: 'ds_a',
        );
      final grove = GroveSyncSource(
        declarations: const [rowsSync],
        entities: noteEntities,
        bindings: const {'Note': GroveEntity(codec: NoteCodec())},
        baseUrl: Uri.parse('http://grove.test'),
        httpClient: server.client,
        live: LiveChannel.poll,
        pollInterval: const Duration(hours: 1),
        pushDebounce: Duration.zero,
        nowMs: harnessNow,
        newId: () => 'new-id',
        newNodeId: nextNodeId,
      );
      final client = await OfflineClient.open(
        transport: _Rows(),
        entities: noteEntities,
        operations: const {'op_list_rows': _opListRows},
        storage: storage,
        principal: 'alice',
        syncSources: [grove],
        snapshotDebounce: const Duration(milliseconds: 1),
      );

      addTearDown(client.close);

      final cache = client.cache;

      await cache.idle;
      await grove.join(const GroveDataset('a', table: 'ds_a'));
      await pumpEventQueue(times: 50);
      expect(
        cache.store.getRecord('Note:a:r1')?.data['title'],
        'alice secret row',
      );

      final rows = cache
          .watch(_opListRows, const TagContext(path: {'id': 'a'}))
          .listen((_) {});

      addTearDown(rows.cancel);
      await pumpEventQueue(times: 50);
      await client.flush();

      Future<Map<String, Object?>> snapshot() => _snapshot(cache);

      expect(
        _noteRecords(await snapshot()),
        isEmpty,
        reason: 'normalized snapshots never hold owned rows',
      );

      await grove.leave('a', erase: true);
      expect(cache.store.getRecord('Note:a:r1'), isNull);

      // The next snapshot write.
      await client.flush();

      final after = await snapshot();

      expect(after['queries'], isNotEmpty, reason: 'the snapshot is not empty');
      expect(_noteRecords(after), isEmpty);
      expect(jsonEncode(after), isNot(contains('alice secret row')));
      expect(await cache.session!.namespace('grove/a').scan(''), isEmpty);
    },
  );
}

Future<Map<String, Object?>> _snapshot(QueryCache cache) async =>
    (await cache.session!.readSnapshot())!.json;

Iterable<String> _noteRecords(Map<String, Object?> snapshot) => [
  for (final key in (snapshot['records'] as Map<String, Object?>? ?? {}).keys)
    if (key.startsWith('Note:')) key,
];
