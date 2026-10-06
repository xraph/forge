import 'package:sqlite3/common.dart';

/// The schema version this package writes.
const int schemaVersion = 1;

/// One schema step: its version and the statements that reach it.
final class Migration {
  /// A step to [version] running [statements] in one transaction.
  const Migration(this.version, this.statements);

  /// The version this step produces.
  final int version;

  /// The statements, run in order.
  final List<String> statements;
}

/// Every schema step, oldest first. Append; never edit a shipped step.
///
/// Version 1: `snapshot` holds at most one row (`written_at` is milliseconds
/// since the epoch). `outbox` keeps enqueue order in `seq`, which
/// AUTOINCREMENT never reuses, so a re-queued id goes to the back; `id` is
/// UNIQUE so a second enqueue of one mutation fails; `created_at_us` is
/// microseconds since the epoch, so a record reads back to the microsecond;
/// `state_json` is null until the outbox sets a state. `kv` is keyed by
/// namespace and key.
const List<Migration> migrations = [
  Migration(1, [
    'CREATE TABLE snapshot ('
        'id INTEGER PRIMARY KEY CHECK (id = 1), '
        'json TEXT NOT NULL, '
        'written_at INTEGER NOT NULL)',
    'CREATE TABLE outbox ('
        'seq INTEGER PRIMARY KEY AUTOINCREMENT, '
        'id TEXT NOT NULL UNIQUE, '
        'operation_id TEXT NOT NULL, '
        'args_json TEXT NOT NULL, '
        'optimistic_json TEXT, '
        'idempotency_key TEXT NOT NULL, '
        'created_at_us INTEGER NOT NULL, '
        'state_json TEXT)',
    'CREATE TABLE kv ('
        'namespace TEXT NOT NULL, '
        'key TEXT NOT NULL, '
        'value TEXT NOT NULL, '
        'PRIMARY KEY (namespace, key)) WITHOUT ROWID',
  ]),
];

/// Thrown when a database was written by a newer version of this package.
/// Opening it is refused rather than guessing at a schema it does not know.
final class UnsupportedSchemaVersion implements Exception {
  /// The database is at [found]; this package understands up to [supported].
  const UnsupportedSchemaVersion({
    required this.found,
    required this.supported,
  });

  /// The version recorded in the database.
  final int found;

  /// The newest version this package can read.
  final int supported;

  @override
  String toString() =>
      'UnsupportedSchemaVersion: the database is at schema $found, newer than $supported';
}

/// Brings [db] to the newest version in [steps], one transaction per step.
///
/// Safe to run from several connections to one file at once. Each step
/// re-reads the applied version inside its own `BEGIN IMMEDIATE` transaction
/// and skips itself when another connection got there first. Give every such
/// connection a busy timeout (`PRAGMA busy_timeout`) before calling this, so
/// a connection waits for another's step instead of failing with SQLITE_BUSY.
void migrate(
  CommonDatabase db, {
  List<Migration> steps = migrations,
  int Function()? now,
}) {
  final clock = now ?? () => DateTime.now().millisecondsSinceEpoch;
  final latest = steps.isEmpty ? 0 : steps.last.version;

  db.execute(
    'CREATE TABLE IF NOT EXISTS forge_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)',
  );

  int applied() {
    final found =
        (db.select('SELECT MAX(version) AS v FROM forge_migrations').first['v']
            as int?) ??
        0;
    if (found > latest) {
      throw UnsupportedSchemaVersion(found: found, supported: latest);
    }
    return found;
  }

  // A first read outside any transaction, so a database from a newer schema
  // is refused before this takes a write lock.
  final current = applied();

  for (final step in steps.where((s) => s.version > current)) {
    db.execute('BEGIN IMMEDIATE');
    try {
      // Read again under the write lock: another connection may have applied
      // this step between the first read and here.
      if (applied() >= step.version) {
        db.execute('COMMIT');
        continue;
      }
      for (final statement in step.statements) {
        db.execute(statement);
      }
      db.execute(
        'INSERT INTO forge_migrations (version, applied_at) VALUES (?, ?)',
        [step.version, clock()],
      );
      db.execute('COMMIT');
    } on Object {
      // SQLite rolls some failures back by itself; a second ROLLBACK would
      // throw and hide the original error.
      if (!db.autocommit) db.execute('ROLLBACK');
      rethrow;
    }
  }
}
