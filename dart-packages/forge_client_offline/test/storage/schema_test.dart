@TestOn('vm')
library;

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:sqlite3/sqlite3.dart';

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
}
