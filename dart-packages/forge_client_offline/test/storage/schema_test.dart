@TestOn('vm')
library;

import 'dart:async';
import 'dart:io';
import 'dart:isolate';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:sqlite3/sqlite3.dart';

/// Step 1 plus a statement that holds its write lock for half a second, so a
/// second connection starting at the same moment reads the version before the
/// first commits, however busy the machine is.
List<Migration> _slowSteps() => [
  Migration(1, [...migrations.first.statements, 'SELECT hold_lock()']),
  ...migrations.skip(1),
];

/// Opens [args] path, reports a port, migrates when that port is told to, and
/// reports `ok` or the error.
Future<void> _migrateOnSignal(List<Object> args) async {
  final reply = args[0] as SendPort;
  final db = sqlite3.open(args[1] as String);
  db.execute('PRAGMA busy_timeout = 10000');
  db.createFunction(
    functionName: 'hold_lock',
    argumentCount: const AllowedArgumentCount(0),
    function: (_) {
      sleep(const Duration(milliseconds: 500));
      return null;
    },
  );
  final go = ReceivePort();
  reply.send(go.sendPort);
  await go.first;
  try {
    migrate(db, steps: _slowSteps());
    reply.send('ok');
  } on Object catch (error) {
    reply.send('$error');
  } finally {
    db.close();
  }
}

void main() {
  late Database db;

  setUp(() => db = sqlite3.openInMemory());
  tearDown(() => db.close());

  Set<String> tables() => db
      .select("SELECT name FROM sqlite_master WHERE type = 'table'")
      .map((r) => r['name'] as String)
      .toSet();

  test('migrate creates the tables and records the version', () {
    migrate(db, now: () => 42);

    expect(
      tables(),
      containsAll(<String>['forge_migrations', 'snapshot', 'outbox', 'kv']),
    );
    final rows = db.select('SELECT version, applied_at FROM forge_migrations');
    expect(rows.single['version'], schemaVersion);
    expect(rows.single['applied_at'], 42);
  });

  test('migrating twice is a no-op', () {
    migrate(db);
    migrate(db);

    expect(db.select('SELECT version FROM forge_migrations'), hasLength(1));
  });

  test('later migrations run in order on an older database', () {
    migrate(db);
    migrate(
      db,
      steps: [
        ...migrations,
        const Migration(2, ['CREATE TABLE extra (id INTEGER PRIMARY KEY)']),
      ],
    );

    expect(tables(), contains('extra'));
    expect(
      db
          .select('SELECT version FROM forge_migrations ORDER BY version')
          .map((r) => r['version']),
      [1, 2],
    );
  });

  test('a database from a newer schema is refused', () {
    migrate(db);
    db.execute(
      'INSERT INTO forge_migrations (version, applied_at) VALUES (99, 0)',
    );

    expect(() => migrate(db), throwsA(isA<UnsupportedSchemaVersion>()));
  });

  test('a failing migration leaves no trace', () {
    expect(
      () => migrate(
        db,
        steps: [
          const Migration(1, [
            'CREATE TABLE ok (id INTEGER)',
            'THIS IS NOT SQL',
          ]),
        ],
      ),
      throwsA(isA<SqliteException>()),
    );

    expect(tables(), isNot(contains('ok')));
    expect(db.select('SELECT version FROM forge_migrations'), isEmpty);
  });

  test('two connections migrating one file both succeed, once', () async {
    final dir = Directory.systemTemp.createTempSync('forge_migrate_race_');
    addTearDown(() => dir.deleteSync(recursive: true));
    final path = '${dir.path}/race.db';

    // Create the bookkeeping table first, at version 0. Otherwise a racer that
    // finds no table waits on the other's lock in CREATE TABLE IF NOT EXISTS,
    // reads the version only after the other commits, and the race this test
    // is about never happens.
    final setup = sqlite3.open(path);
    migrate(setup, steps: const []);
    setup.close();

    final ports = [ReceivePort(), ReceivePort()];
    final replies = [for (final port in ports) StreamIterator<Object?>(port)];
    for (final port in ports) {
      await Isolate.spawn(_migrateOnSignal, <Object>[port.sendPort, path]);
    }

    final starts = <SendPort>[];
    for (final reply in replies) {
      expect(await reply.moveNext(), isTrue);
      starts.add(reply.current! as SendPort);
    }
    for (final start in starts) {
      start.send(null);
    }

    final outcomes = <Object?>[];
    for (final reply in replies) {
      expect(await reply.moveNext(), isTrue);
      outcomes.add(reply.current);
      await reply.cancel();
    }

    expect(outcomes, ['ok', 'ok']);
    final check = sqlite3.open(path);
    addTearDown(check.close);
    expect(
      check
          .select('SELECT version FROM forge_migrations')
          .map((r) => r['version']),
      [1],
    );
  });
}
