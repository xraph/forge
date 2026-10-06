@TestOn('vm')
library;

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:sqlite3/sqlite3.dart';

PendingMutationRecord record(
  String id, {
  String args = '{}',
  String? state = '{"kind":"queued"}',
  DateTime? createdAt,
}) => PendingMutationRecord(
  id: id,
  operationId: 'op_update_order',
  argsJson: args,
  optimisticJson: '{"kind":"delete","key":"Order:7"}',
  idempotencyKey: 'key-$id',
  createdAt: createdAt ?? DateTime.utc(2026, 10, 4, 12),
  stateJson: state,
);

void main() {
  late Database db;
  late SqliteStorageSession session;
  late int writes;

  setUp(() {
    db = sqlite3.openInMemory();
    migrate(db);
    writes = 0;
    session = SqliteStorageSession(
      principal: 'alice',
      database: db,
      vfs: 'memory',
      afterWrite: () async => writes++,
      onClose: () async => db.close(),
    );
  });

  tearDown(() => session.close());

  group('snapshot', () {
    test('is null until written', () async {
      expect(await session.readSnapshot(), isNull);
    });

    test('round-trips and the last write wins', () async {
      await session.writeSnapshot(const Snapshot({'v': 1, 'n': 1}));
      await session.writeSnapshot(const Snapshot({'v': 1, 'n': 2}));

      expect((await session.readSnapshot())!.json, {'v': 1, 'n': 2});
      expect(db.select('SELECT COUNT(*) AS c FROM snapshot').first['c'], 1);
    });
  });

  group('outbox', () {
    test('reads in the order records were enqueued', () async {
      for (final id in ['c', 'a', 'b']) {
        await session.enqueue(record(id));
      }

      expect((await session.readOutbox()).map((r) => r.id), ['c', 'a', 'b']);
    });

    test('round-trips every field', () async {
      await session.enqueue(
        record('a', args: '{"v":1}', state: '{"kind":"sending","at":7}'),
      );
      final got = (await session.readOutbox()).single;

      expect(got.operationId, 'op_update_order');
      expect(got.argsJson, '{"v":1}');
      expect(got.optimisticJson, '{"kind":"delete","key":"Order:7"}');
      expect(got.idempotencyKey, 'key-a');
      expect(got.createdAt, DateTime.utc(2026, 10, 4, 12));
      expect(got.stateJson, '{"kind":"sending","at":7}');
    });

    test(
      'a second enqueue of the same id is refused and changes nothing',
      () async {
        await session.enqueue(record('a'));
        writes = 0;
        await expectLater(
          session.enqueue(record('a', args: '{"edited":true}')),
          throwsStateError,
        );
        expect(writes, 0);

        final got = await session.readOutbox();
        expect(got.map((r) => r.id), ['a']);
        expect(got.single.argsJson, '{}');
      },
    );

    test('a record with no state round-trips its null state', () async {
      await session.enqueue(record('a', state: null));
      expect((await session.readOutbox()).single.stateJson, isNull);

      await session.updateState('a', '{"kind":"queued"}');
      expect(
        (await session.readOutbox()).single.stateJson,
        '{"kind":"queued"}',
      );
    });

    test(
      'createdAt round-trips to the microsecond and reads back as UTC',
      () async {
        final at = DateTime.utc(2026, 10, 4, 12, 0, 0, 999, 1);
        await session.enqueue(record('a', createdAt: at));
        await session.enqueue(record('b', createdAt: at.toLocal()));

        final got = await session.readOutbox();
        expect(got.first.createdAt, at);
        expect(got.last.createdAt, at);
        expect(got.map((r) => r.createdAt.isUtc), everyElement(isTrue));
        expect(
          db.select('SELECT created_at_us FROM outbox').first['created_at_us'],
          at.microsecondsSinceEpoch,
        );
      },
    );

    test('a re-queued id goes to the back of the outbox', () async {
      await session.enqueue(record('a'));
      await session.enqueue(record('b'));
      await session.remove('a');
      await session.enqueue(record('a'));

      expect((await session.readOutbox()).map((r) => r.id), ['b', 'a']);
    });

    test('readOutbox hands out a list that cannot be changed', () async {
      await session.enqueue(record('a'));
      final got = await session.readOutbox();

      expect(() => got.add(record('b')), throwsUnsupportedError);
      expect(() => got.removeLast(), throwsUnsupportedError);
    });

    test('updateState on an unknown id throws and writes nothing', () async {
      await session.enqueue(record('a'));
      writes = 0;

      await expectLater(
        session.updateState('missing', '{"kind":"queued"}'),
        throwsStateError,
      );
      expect(writes, 0);
      expect(
        (await session.readOutbox()).single.stateJson,
        '{"kind":"queued"}',
      );
    });

    test('updateState overwrites the state each time', () async {
      await session.enqueue(record('a'));
      await session.updateState('a', '{"kind":"sending","at":1}');
      await session.updateState('a', '{"kind":"queued"}');

      expect(
        (await session.readOutbox()).single.stateJson,
        '{"kind":"queued"}',
      );
    });

    test(
      'updateState records a failure and remove deletes the record',
      () async {
        await session.enqueue(record('a'));
        await session.enqueue(record('b'));
        await session.updateState(
          'a',
          '{"kind":"failed","failure":{"kind":"conflict","status":409}}',
        );
        await session.remove('b');

        final got = await session.readOutbox();
        expect(got.map((r) => r.id), ['a']);
        expect(
          got.single.stateJson,
          '{"kind":"failed","failure":{"kind":"conflict","status":409}}',
        );
      },
    );
  });

  group('key-value namespaces', () {
    group('unpaired surrogates (a departure from memoryStorage)', () {
      // SQLite stores UTF-8, which turns every unpaired surrogate into
      // U+FFFD, so these two keys would share a row.
      const high = 'a\uD800';
      const low = 'a\uDC00';
      final badKeys = [high, low, '\uDC00\uD800', 'x\uD800y', '\uD83D'];

      test('a key holding one is refused by every key-value call', () async {
        final kv = session.namespace('grove');

        for (final key in badKeys) {
          await expectLater(kv.get(key), throwsArgumentError, reason: key);
          await expectLater(kv.put(key, 'v'), throwsArgumentError);
          await expectLater(kv.delete(key), throwsArgumentError);
          await expectLater(kv.scan(key), throwsArgumentError);
          await expectLater(
            kv.batch((b) => b.put(key, 'v')),
            throwsArgumentError,
          );
          await expectLater(
            kv.batch((b) => b.delete(key)),
            throwsArgumentError,
          );
        }
      });

      test('a refused batch write applies none of the batch', () async {
        final kv = session.namespace('grove');
        writes = 0;

        await expectLater(
          kv.batch((b) {
            b.put('fine', '1');
            b.put(high, '2');
          }),
          throwsArgumentError,
        );

        expect(await kv.scan(''), isEmpty);
        expect(writes, 0);
      });

      test('nothing was written under either key', () async {
        final kv = session.namespace('grove');
        await expectLater(kv.put(high, 'h'), throwsArgumentError);
        await expectLater(kv.put(low, 'l'), throwsArgumentError);

        expect(db.select('SELECT COUNT(*) AS c FROM kv').first['c'], 0);
      });

      test('paired surrogates are still accepted', () async {
        final kv = session.namespace('grove');
        await kv.put('a\u{1F600}', 'v');

        expect(await kv.get('a\u{1F600}'), 'v');
        expect(await kv.scan('a\u{1F600}'), hasLength(1));
      });

      test(
        'a JSON value keeps a lone surrogate; a raw value does not',
        () async {
          final kv = session.namespace('grove');
          final json = jsonEncode({'s': 'x\uD800'});
          await kv.put('json', json);
          await kv.put('raw', 'x\uD800');

          expect(await kv.get('json'), json);
          expect(
            (jsonDecode((await kv.get('json'))!) as Map<String, Object?>)['s'],
            'x\uD800',
          );
          expect(await kv.get('raw'), 'x\uFFFD');
        },
      );
    });

    test('namespaces do not see each other', () async {
      await session.namespace('grove').put('k', 'grove-value');
      await session.namespace('other').put('k', 'other-value');

      expect(await session.namespace('grove').get('k'), 'grove-value');
      expect(await session.namespace('other').get('k'), 'other-value');
      expect(await session.namespace('missing').get('k'), isNull);
    });

    test('put overwrites, delete removes', () async {
      final kv = session.namespace('grove');
      await kv.put('k', '1');
      await kv.put('k', '2');
      expect(await kv.get('k'), '2');

      await kv.delete('k');
      expect(await kv.get('k'), isNull);
    });

    test('scan returns every key with the prefix, in key order', () async {
      final kv = session.namespace('grove');
      for (final k in ['doc/2', 'doc/1', 'docs', 'other', 'doc/\u{1F600}']) {
        await kv.put(k, 'v-$k');
      }

      final got = await kv.scan('doc/');
      expect(got.keys, ['doc/1', 'doc/2', 'doc/\u{1F600}']);
      expect(got['doc/1'], 'v-doc/1');
      expect(await kv.scan(''), hasLength(5));
    });

    test(
      'scan orders surrogate pairs by UTF-16 code unit, not code point',
      () async {
        final kv = session.namespace('grove');
        for (final k in [
          'p\uFFFF',
          'p\u{10FFFF}',
          'p\u{1F600}',
          'p\uE000',
          'q',
        ]) {
          await kv.put(k, k);
        }

        // SQLite alone would give U+E000, U+FFFF, U+1F600, U+10FFFF.
        expect((await kv.scan('p')).keys, [
          'p\u{1F600}',
          'p\u{10FFFF}',
          'p\uE000',
          'p\uFFFF',
        ]);
      },
    );

    test('scan reads the prefix literally and keeps its case', () async {
      final kv = session.namespace('grove');
      for (final k in ['a_1', 'ab1', 'A_2', 'a%', 'a_']) {
        await kv.put(k, k);
      }

      expect((await kv.scan('a_')).keys, ['a_', 'a_1']);
      expect((await kv.scan('a%')).keys, ['a%']);
    });

    test('batch applies all its writes', () async {
      final kv = session.namespace('grove');
      await kv.put('gone', 'x');

      await kv.batch((b) {
        b.put('a', '1');
        b.put('b', '2');
        b.delete('gone');
      });

      expect(await kv.scan(''), {'a': '1', 'b': '2'});
    });

    test(
      'a batch that fails in SQLite part way rolls back its earlier writes',
      () async {
        final kv = session.namespace('grove');
        await kv.put('kept', 'x');
        db.execute(
          'CREATE TRIGGER refuse BEFORE INSERT ON kv WHEN NEW.key = \'boom\' '
          "BEGIN SELECT RAISE(ABORT, 'refused'); END",
        );
        writes = 0;

        await expectLater(
          kv.batch((b) {
            b.put('a', '1');
            b.delete('kept');
            b.put('boom', '2');
          }),
          throwsA(isA<SqliteException>()),
        );

        expect(await kv.scan(''), {'kept': 'x'});
        expect(writes, 0);
        expect(db.autocommit, isTrue, reason: 'the transaction was closed');

        await kv.put('after', 'y');
        expect(await kv.get('after'), 'y');
      },
    );

    test('the batch handle is dead once the builder returns', () async {
      final kv = session.namespace('grove');
      late KeyValueBatch kept;

      await kv.batch((b) => kept = b);

      expect(() => kept.put('late', 'v'), throwsStateError);
      expect(() => kept.delete('late'), throwsStateError);
      expect(await kv.get('late'), isNull);
    });

    test('an empty batch writes nothing', () async {
      writes = 0;
      await session.namespace('grove').batch((_) {});

      expect(writes, 0);
    });

    test('a batch whose builder throws writes nothing', () async {
      final kv = session.namespace('grove');

      await expectLater(
        kv.batch((b) {
          b.put('a', '1');
          throw StateError('builder failed');
        }),
        throwsStateError,
      );

      expect(await kv.scan(''), isEmpty);
    });
  });

  test('every write calls afterWrite', () async {
    await session.writeSnapshot(const Snapshot({'v': 1}));
    await session.enqueue(record('a'));
    await session.updateState('a', '{"kind":"queued"}');
    await session.remove('a');
    await session.namespace('n').put('k', 'v');
    await session.namespace('n').batch((b) => b.put('j', 'v'));
    await session.namespace('n').delete('k');

    expect(writes, 7);
  });

  test(
    'a namespace handle taken before close stops working with the session',
    () async {
      final kv = session.namespace('n');
      await kv.put('k', 'v');
      await session.close();

      await expectLater(kv.get('k'), throwsStateError);
      await expectLater(kv.put('k', 'w'), throwsStateError);
      await expectLater(kv.batch((b) => b.put('k', 'w')), throwsStateError);
    },
  );

  test('onClose runs once', () async {
    var closes = 0;
    final other = SqliteStorageSession(
      principal: 'bob',
      database: db,
      vfs: 'memory',
      afterWrite: () async {},
      onClose: () async => closes++,
    );

    await other.close();
    await other.close();

    expect(closes, 1);
  });

  test(
    'a closed session refuses to be used and closing twice is harmless',
    () async {
      await session.close();
      await session.close();

      expect(session.isClosed, isTrue);
      await expectLater(session.readOutbox(), throwsStateError);
      expect(() => session.namespace('n'), throwsStateError);
    },
  );

  test('vfs and principal are reported', () {
    expect(session.vfs, 'memory');
    expect(session.principal, 'alice');
  });
}
