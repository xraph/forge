import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

PendingMutationRecord _record(String id, {String? state}) =>
    PendingMutationRecord(
      id: id,
      operationId: 'op_update_order',
      argsJson: '{"path":{"id":"7"}}',
      optimisticJson: '{"total":5}',
      idempotencyKey: 'key-$id',
      createdAt: DateTime.utc(2026, 10, 4, 12),
      stateJson: state,
    );

Snapshot _snapshot(String marker) => Snapshot({
  'v': 1,
  'mode': 'normalized',
  'records': {
    'Order:7': {'id': 7, 'marker': marker},
  },
  'queries': <Object?>[],
});

final _closed = throwsA(isA<StateError>());

void main() {
  group('a pending mutation record', () {
    test('compares by value, field by field', () {
      final base = _record('m-1', state: '{"kind":"queued"}');

      expect(_record('m-1', state: '{"kind":"queued"}'), base);
      expect(
        _record('m-1', state: '{"kind":"queued"}').hashCode,
        base.hashCode,
      );

      final others = [
        _record('m-2', state: '{"kind":"queued"}'),
        _record('m-1'),
        _record('m-1', state: '{"kind":"sending","at":1}'),
        PendingMutationRecord(
          id: 'm-1',
          operationId: 'op_other',
          argsJson: base.argsJson,
          optimisticJson: base.optimisticJson,
          idempotencyKey: base.idempotencyKey,
          createdAt: base.createdAt,
          stateJson: base.stateJson,
        ),
        PendingMutationRecord(
          id: 'm-1',
          operationId: base.operationId,
          argsJson: '{}',
          optimisticJson: base.optimisticJson,
          idempotencyKey: base.idempotencyKey,
          createdAt: base.createdAt,
          stateJson: base.stateJson,
        ),
        PendingMutationRecord(
          id: 'm-1',
          operationId: base.operationId,
          argsJson: base.argsJson,
          idempotencyKey: base.idempotencyKey,
          createdAt: base.createdAt,
          stateJson: base.stateJson,
        ),
        PendingMutationRecord(
          id: 'm-1',
          operationId: base.operationId,
          argsJson: base.argsJson,
          optimisticJson: base.optimisticJson,
          idempotencyKey: 'other-key',
          createdAt: base.createdAt,
          stateJson: base.stateJson,
        ),
        PendingMutationRecord(
          id: 'm-1',
          operationId: base.operationId,
          argsJson: base.argsJson,
          optimisticJson: base.optimisticJson,
          idempotencyKey: base.idempotencyKey,
          createdAt: DateTime.utc(2026, 10, 5),
          stateJson: base.stateJson,
        ),
      ];

      for (final other in others) {
        expect(other, isNot(base));
      }
    });
  });

  group('a session', () {
    test('starts a partition empty', () async {
      final session = await memoryStorage().open('u-1');

      expect(session.principal, 'u-1');
      expect(await session.readSnapshot(), isNull);
      expect(await session.readOutbox(), isEmpty);
    });

    test('round-trips a snapshot as a copy, not the same object', () async {
      final session = await memoryStorage().open('u-1');
      const written = Snapshot({
        'v': 1,
        'mode': 'normalized',
        'records': {
          'Order:7': {'id': 7},
        },
        'queries': <Object?>[],
      });

      await session.writeSnapshot(written);
      final read = await session.readSnapshot();

      expect(read?.json, written.json);
      expect(identical(read?.json, written.json), isFalse);
    });

    test(
      'keeps what was written when the caller mutates the snapshot after',
      () async {
        final session = await memoryStorage().open('u-1');
        final written = _snapshot('first');

        await session.writeSnapshot(written);
        (written.json['records']! as Map<String, Object?>)['Order:8'] = {
          'id': 8,
        };
        final read = await session.readSnapshot();

        expect(read?.json, _snapshot('first').json);
      },
    );

    test('replaces the snapshot on a second write', () async {
      final session = await memoryStorage().open('u-1');

      await session.writeSnapshot(_snapshot('first'));
      await session.writeSnapshot(_snapshot('second'));

      expect((await session.readSnapshot())?.json, _snapshot('second').json);
    });

    test('keeps the outbox in enqueue order', () async {
      final session = await memoryStorage().open('u-1');

      await session.enqueue(_record('m-2'));
      await session.enqueue(_record('m-1'));
      await session.enqueue(_record('m-3'));

      expect((await session.readOutbox()).map((record) => record.id), [
        'm-2',
        'm-1',
        'm-3',
      ]);
    });

    test('hands out an outbox list that cannot change the queue', () async {
      final session = await memoryStorage().open('u-1');

      await session.enqueue(_record('m-1'));
      final read = await session.readOutbox();

      expect(() => read.add(_record('m-2')), throwsUnsupportedError);
      expect(() => read.clear(), throwsUnsupportedError);
      expect(await session.readOutbox(), [_record('m-1')]);
    });

    test('refuses a mutation id that is already queued', () async {
      final session = await memoryStorage().open('u-1');

      await session.enqueue(_record('m-1'));

      await expectLater(
        session.enqueue(_record('m-1', state: '{"kind":"queued"}')),
        throwsA(isA<StateError>()),
      );
      expect(await session.readOutbox(), [_record('m-1')]);
    });

    test(
      'removes a record and treats removing an unknown id as done',
      () async {
        final session = await memoryStorage().open('u-1');

        await session.enqueue(_record('m-1'));
        await session.enqueue(_record('m-2'));
        await session.remove('m-1');
        await session.remove('m-404');

        expect(await session.readOutbox(), [_record('m-2')]);
      },
    );

    test('lets a removed id be queued again', () async {
      final session = await memoryStorage().open('u-1');

      await session.enqueue(_record('m-1'));
      await session.remove('m-1');
      await session.enqueue(_record('m-1'));

      expect(await session.readOutbox(), [_record('m-1')]);
    });

    test("updates a record's state and keeps it in place", () async {
      final session = await memoryStorage().open('u-1');

      await session.enqueue(_record('m-1', state: '{"kind":"queued"}'));
      await session.enqueue(_record('m-2', state: '{"kind":"queued"}'));
      await session.updateState(
        'm-1',
        '{"kind":"failed","failure":{"kind":"conflict"}}',
      );

      expect(await session.readOutbox(), [
        _record(
          'm-1',
          state: '{"kind":"failed","failure":{"kind":"conflict"}}',
        ),
        _record('m-2', state: '{"kind":"queued"}'),
      ]);
    });

    test(
      'refuses to update the state of a record that is not queued',
      () async {
        final session = await memoryStorage().open('u-1');

        await session.enqueue(_record('m-1'));

        await expectLater(
          session.updateState('m-404', '{}'),
          throwsA(isA<StateError>()),
        );
        expect(await session.readOutbox(), [_record('m-1')]);
      },
    );

    test('stops working once closed, and changes nothing', () async {
      final storage = memoryStorage();
      final session = await storage.open('u-1');
      final store = session.namespace('replica');

      await session.enqueue(_record('m-1'));
      await session.close();

      expect(session.readSnapshot(), _closed);
      expect(session.writeSnapshot(_snapshot('x')), _closed);
      expect(session.readOutbox(), _closed);
      expect(session.enqueue(_record('m-2')), _closed);
      expect(session.remove('m-1'), _closed);
      expect(session.updateState('m-1', '{"kind":"failed"}'), _closed);
      expect(() => session.namespace('replica'), _closed);
      expect(store.get('k'), _closed);
      expect(store.put('k', 'v'), _closed);
      expect(store.delete('k'), _closed);
      expect(store.scan(''), _closed);
      expect(store.batch((batch) => batch.put('k', 'v')), _closed);

      final reopened = await storage.open('u-1');

      expect(await reopened.readSnapshot(), isNull);
      expect(await reopened.readOutbox(), [_record('m-1')]);
      expect(await reopened.namespace('replica').scan(''), isEmpty);
    });

    test('closes twice without complaint', () async {
      final session = await memoryStorage().open('u-1');

      await session.close();

      await session.close();
    });

    test('closing one session leaves its siblings working', () async {
      final storage = memoryStorage();
      final first = await storage.open('u-1');
      final second = await storage.open('u-1');

      await first.close();
      await second.enqueue(_record('m-1'));

      expect(await second.readOutbox(), [_record('m-1')]);
    });
  });

  group('principals', () {
    test(
      'shows the same partition to every session of one principal',
      () async {
        final storage = memoryStorage();
        final first = await storage.open('u-1');

        await first.enqueue(_record('m-1'));
        await first.close();

        final second = await storage.open('u-1');

        expect(await second.readOutbox(), [_record('m-1')]);
      },
    );

    test(
      'shows one open session what another open session of the principal wrote',
      () async {
        final storage = memoryStorage();
        final first = await storage.open('u-1');
        final second = await storage.open('u-1');

        await first.writeSnapshot(_snapshot('shared'));
        await first.namespace('replica').put('doc:1', 'shared');

        expect((await second.readSnapshot())?.json, _snapshot('shared').json);
        expect(await second.namespace('replica').get('doc:1'), 'shared');
      },
    );

    test('never shows one principal’s rows to another', () async {
      final storage = memoryStorage();
      final alice = await storage.open('alice');
      final bob = await storage.open('bob');

      await alice.writeSnapshot(_snapshot('alice'));
      await alice.enqueue(_record('m-1'));
      await alice.namespace('replica').put('doc:1', 'secret');

      expect(await bob.readSnapshot(), isNull);
      expect(await bob.readOutbox(), isEmpty);
      expect(await bob.namespace('replica').get('doc:1'), isNull);
      expect(await bob.namespace('replica').scan(''), isEmpty);
    });

    test('keeps two principals’ writes to the same names apart', () async {
      final storage = memoryStorage();
      final alice = await storage.open('alice');
      final bob = await storage.open('bob');

      await alice.writeSnapshot(_snapshot('alice'));
      await bob.writeSnapshot(_snapshot('bob'));
      await alice.enqueue(_record('m-1', state: '{"kind":"queued"}'));
      await bob.enqueue(_record('m-1', state: '{"kind":"sending","at":1}'));
      await alice.updateState('m-1', '{"kind":"failed"}');
      await alice.namespace('replica').put('doc:1', 'alice');
      await bob.namespace('replica').put('doc:1', 'bob');
      await alice.namespace('replica').delete('doc:1');

      expect((await alice.readSnapshot())?.json, _snapshot('alice').json);
      expect((await bob.readSnapshot())?.json, _snapshot('bob').json);
      expect(await alice.readOutbox(), [
        _record('m-1', state: '{"kind":"failed"}'),
      ]);
      expect(await bob.readOutbox(), [
        _record('m-1', state: '{"kind":"sending","at":1}'),
      ]);
      expect(await alice.namespace('replica').get('doc:1'), isNull);
      expect(await bob.namespace('replica').get('doc:1'), 'bob');
    });

    test('destroys a partition and revokes its open sessions', () async {
      final storage = memoryStorage();
      final session = await storage.open('u-1');

      await session.enqueue(_record('m-1'));
      await storage.destroy('u-1');

      expect(session.readOutbox(), _closed);

      final reopened = await storage.open('u-1');

      expect(await reopened.readOutbox(), isEmpty);
    });

    test('destroys the snapshot, the outbox and every namespace', () async {
      final storage = memoryStorage();
      final session = await storage.open('u-1');

      await session.writeSnapshot(_snapshot('gone'));
      await session.enqueue(_record('m-1'));
      await session.namespace('replica').put('doc:1', 'gone');
      await session.namespace('other').put('doc:2', 'gone');
      await session.close();
      await storage.destroy('u-1');

      final reopened = await storage.open('u-1');

      expect(await reopened.readSnapshot(), isNull);
      expect(await reopened.readOutbox(), isEmpty);
      expect(await reopened.namespace('replica').scan(''), isEmpty);
      expect(await reopened.namespace('other').scan(''), isEmpty);
    });

    test('revokes the namespace handles of a destroyed partition', () async {
      final storage = memoryStorage();
      final session = await storage.open('u-1');
      final store = session.namespace('replica');

      await storage.destroy('u-1');

      expect(store.get('k'), _closed);
      expect(store.put('k', 'v'), _closed);

      final reopened = await storage.open('u-1');

      expect(await reopened.namespace('replica').scan(''), isEmpty);
    });

    test('destroys one principal and leaves the others alone', () async {
      final storage = memoryStorage();
      final alice = await storage.open('alice');
      final bob = await storage.open('bob');

      await alice.enqueue(_record('m-1'));
      await bob.writeSnapshot(_snapshot('bob'));
      await bob.enqueue(_record('m-2'));
      await bob.namespace('replica').put('doc:1', 'bob');
      await storage.destroy('alice');

      expect(await bob.readOutbox(), [_record('m-2')]);
      expect((await bob.readSnapshot())?.json, _snapshot('bob').json);
      expect(await bob.namespace('replica').get('doc:1'), 'bob');

      await bob.close();
      final reopened = await storage.open('bob');

      expect(await reopened.readOutbox(), [_record('m-2')]);
      expect((await reopened.readSnapshot())?.json, _snapshot('bob').json);
      expect(await reopened.namespace('replica').get('doc:1'), 'bob');
    });

    test('destroys a principal that was never opened', () async {
      await memoryStorage().destroy('nobody');
    });
  });

  group('a key-value namespace', () {
    test('isolates namespaces from each other', () async {
      final session = await memoryStorage().open('u-1');

      await session.namespace('a').put('k', '1');

      expect(await session.namespace('b').get('k'), isNull);
      expect(await session.namespace('a').get('k'), '1');
    });

    test(
      'sees the same entries through every handle to one namespace',
      () async {
        final session = await memoryStorage().open('u-1');

        await session.namespace('a').put('k', '1');

        expect(await session.namespace('a').get('k'), '1');
      },
    );

    test('deletes a key', () async {
      final store = (await memoryStorage().open('u-1')).namespace('replica');

      await store.put('k', 'v');
      await store.delete('k');

      expect(await store.get('k'), isNull);
    });

    test('scans a prefix in key order', () async {
      final store = (await memoryStorage().open('u-1')).namespace('replica');

      await store.put('doc:2', 'b');
      await store.put('meta:1', 'x');
      await store.put('old-doc:3', 'c');
      await store.put('doc:1', 'a');

      final found = await store.scan('doc:');

      expect(found, {'doc:1': 'a', 'doc:2': 'b'});
      expect(found.keys, ['doc:1', 'doc:2']);
    });

    test('scans everything for an empty prefix and nothing for a prefix no key has', () async {
      final store = (await memoryStorage().open('u-1')).namespace('replica');

      await store.put('b', '2');
      await store.put('a', '1');

      expect((await store.scan('')).keys, ['a', 'b']);
      expect(await store.scan('c'), isEmpty);
    });

    test('scans a literal prefix, not a pattern', () async {
      final store = (await memoryStorage().open('u-1')).namespace('replica');

      await store.put('docX1', 'x');
      await store.put('doc_1', 'a');

      expect(await store.scan('doc_'), {'doc_1': 'a'});
    });

    test('scans in ascending UTF-16 code unit order', () async {
      final store = (await memoryStorage().open('u-1')).namespace('replica');

      // 'Z' (0x5A) before 'a' (0x61) before 'é' (0xE9) before an astral
      // character's high surrogate (0xD83D) before U+FFFD.
      for (final key in ['k\uFFFD', 'ka', 'k\u{1F600}', 'kZ', 'k\u00E9']) {
        await store.put(key, key);
      }

      expect((await store.scan('k')).keys, [
        'kZ',
        'ka',
        'k\u00E9',
        'k\u{1F600}',
        'k\uFFFD',
      ]);
    });

    test('refuses a write to a batch after its builder returned', () async {
      final store = (await memoryStorage().open('u-1')).namespace('replica');
      late KeyValueBatch kept;

      await store.batch((batch) {
        kept = batch;
        batch.put('a', '1');
      });

      expect(() => kept.put('b', '2'), throwsStateError);
      expect(() => kept.delete('a'), throwsStateError);
      expect(await store.scan(''), {'a': '1'});
    });

    test('refuses a write to a batch whose builder threw', () async {
      final store = (await memoryStorage().open('u-1')).namespace('replica');
      late KeyValueBatch kept;

      await expectLater(
        store.batch((batch) {
          kept = batch;
          throw StateError('half way');
        }),
        throwsStateError,
      );

      expect(() => kept.put('b', '2'), throwsStateError);
    });

    test(
      'applies a batch whole, or not at all when the builder throws',
      () async {
        final store = (await memoryStorage().open('u-1')).namespace('replica');

        await store.put('keep', '1');
        await store.batch((batch) {
          batch.put('a', '1');
          batch.delete('keep');
        });

        expect(await store.scan(''), {'a': '1'});

        await expectLater(
          store.batch((batch) {
            batch.put('b', '2');
            batch.delete('a');
            throw StateError('half way');
          }),
          throwsA(isA<StateError>()),
        );

        expect(await store.scan(''), {'a': '1'});
      },
    );

    test(
      'applies the writes of a batch in the order they were recorded',
      () async {
        final store = (await memoryStorage().open('u-1')).namespace('replica');

        await store.batch((batch) {
          batch.put('k', 'first');
          batch.delete('k');
          batch.put('j', 'first');
          batch.put('j', 'second');
          batch.delete('m');
          batch.put('m', 'again');
        });

        expect(await store.scan(''), {'j': 'second', 'm': 'again'});
      },
    );
  });
}
