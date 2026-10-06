import 'package:forge_client/forge_client.dart';
import 'package:sqlite3/common.dart';

/// SQLITE_CONSTRAINT_PRIMARYKEY and SQLITE_CONSTRAINT_UNIQUE: the extended
/// result codes a second insert of one outbox id fails with.
const _duplicateKeyCodes = {1555, 2067};

/// A [StorageSession] over one open SQLite database.
///
/// The session reads and writes; it does not open, key or name files. The
/// adapter that builds it opens the file named by the principal's
/// `PrincipalLabels` label (never the principal itself, never a plain hash of
/// it), keys it with `applyRawKey`, runs [migrate], and passes the open
/// connection in. Revoking sessions when a partition is destroyed is the
/// adapter's job too; here, [close] is the only way a session stops.
final class SqliteStorageSession implements StorageSession {
  /// Wraps [database], which must already be unlocked and migrated.
  ///
  /// [afterWrite] runs after every write (the web storage flushes IndexedDB
  /// there). [onClose] runs once, on the first [close].
  SqliteStorageSession({
    required this.principal,
    required this._database,
    required this.vfs,
    required this._afterWrite,
    required this._onClose,
  });

  @override
  final String principal;

  /// Which file system holds the database: `file`, `opfs`, `indexeddb`, or a
  /// name a test chose.
  final String vfs;

  final CommonDatabase _database;
  final Future<void> Function() _afterWrite;
  final Future<void> Function() _onClose;
  bool _closed = false;

  /// Whether [close] has been called.
  bool get isClosed => _closed;

  void _check() {
    if (_closed) {
      throw StateError('[forge] the storage session for $principal is closed');
    }
  }

  CommonDatabase get _open {
    _check();
    return _database;
  }

  @override
  Future<Snapshot?> readSnapshot() async {
    final rows = _open.select('SELECT json FROM snapshot WHERE id = 1');
    if (rows.isEmpty) return null;
    return Snapshot.decode(rows.first['json'] as String);
  }

  @override
  Future<void> writeSnapshot(Snapshot snapshot) async {
    _open.execute(
      'INSERT INTO snapshot (id, json, written_at) VALUES (1, ?, ?) '
      'ON CONFLICT (id) DO UPDATE SET json = excluded.json, '
      'written_at = excluded.written_at',
      [snapshot.encode(), DateTime.now().millisecondsSinceEpoch],
    );
    await _afterWrite();
  }

  @override
  Future<List<PendingMutationRecord>> readOutbox() async {
    final rows = _open.select(
      'SELECT id, operation_id, args_json, optimistic_json, idempotency_key, '
      'created_at_us, state_json FROM outbox ORDER BY seq',
    );

    return List.unmodifiable([
      for (final row in rows)
        PendingMutationRecord(
          id: row['id'] as String,
          operationId: row['operation_id'] as String,
          argsJson: row['args_json'] as String,
          optimisticJson: row['optimistic_json'] as String?,
          idempotencyKey: row['idempotency_key'] as String,
          createdAt: DateTime.fromMicrosecondsSinceEpoch(
            row['created_at_us'] as int,
            isUtc: true,
          ),
          stateJson: row['state_json'] as String?,
        ),
    ]);
  }

  @override
  Future<void> enqueue(PendingMutationRecord record) async {
    // A plain insert: the UNIQUE id makes a second enqueue of one record
    // fail, as memoryStorage() does. State changes go through updateState.
    try {
      _open.execute(
        'INSERT INTO outbox (id, operation_id, args_json, optimistic_json, '
        'idempotency_key, created_at_us, state_json) '
        'VALUES (?, ?, ?, ?, ?, ?, ?)',
        [
          record.id,
          record.operationId,
          record.argsJson,
          record.optimisticJson,
          record.idempotencyKey,
          record.createdAt.microsecondsSinceEpoch,
          record.stateJson,
        ],
      );
    } on SqliteException catch (error) {
      if (!_duplicateKeyCodes.contains(error.extendedResultCode)) rethrow;
      throw StateError('[forge] mutation ${record.id} is already queued');
    }
    await _afterWrite();
  }

  @override
  Future<void> remove(String mutationId) async {
    _open.execute('DELETE FROM outbox WHERE id = ?', [mutationId]);
    await _afterWrite();
  }

  @override
  Future<void> updateState(String mutationId, String stateJson) async {
    final db = _open;
    db.execute('UPDATE outbox SET state_json = ? WHERE id = ?', [
      stateJson,
      mutationId,
    ]);
    if (db.updatedRows == 0) {
      throw StateError('[forge] mutation $mutationId is not queued');
    }
    await _afterWrite();
  }

  @override
  KeyValueStore namespace(String name) {
    _check();
    return SqliteKeyValueStore._(this, name);
  }

  @override
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await _onClose();
  }

  /// Runs [body] in one transaction: all of it commits, or none of it.
  void _transaction(void Function(CommonDatabase db) body) {
    final db = _open;
    db.execute('BEGIN IMMEDIATE');
    try {
      body(db);
      db.execute('COMMIT');
    } on Object {
      // SQLite rolls some failures back by itself; a second ROLLBACK would
      // throw and hide the original error.
      if (!db.autocommit) db.execute('ROLLBACK');
      rethrow;
    }
  }
}

/// One namespace of the `kv` table. Plan 05 keeps the Grove replica here.
final class SqliteKeyValueStore implements KeyValueStore {
  SqliteKeyValueStore._(this._session, this.name);

  final SqliteStorageSession _session;

  /// The namespace this store reads and writes.
  final String name;

  static const _upsert =
      'INSERT INTO kv (namespace, key, value) VALUES (?, ?, ?) '
      'ON CONFLICT (namespace, key) DO UPDATE SET value = excluded.value';

  static const _delete = 'DELETE FROM kv WHERE namespace = ? AND key = ?';

  @override
  Future<String?> get(String key) async {
    final rows = _session._open.select(
      'SELECT value FROM kv WHERE namespace = ? AND key = ?',
      [name, key],
    );
    return rows.isEmpty ? null : rows.first['value'] as String;
  }

  @override
  Future<void> put(String key, String value) async {
    _session._open.execute(_upsert, [name, key, value]);
    await _session._afterWrite();
  }

  @override
  Future<void> delete(String key) async {
    _session._open.execute(_delete, [name, key]);
    await _session._afterWrite();
  }

  @override
  Future<Map<String, String>> scan(String prefix) async {
    // instr() matches the prefix literally, where LIKE would read `_` and `%`
    // as patterns (and fold ASCII case). SQLite's BINARY collation orders by
    // UTF-8 bytes, which is code point order, not the UTF-16 code unit order
    // the contract asks for: the two differ once a key holds a character
    // above U+FFFF. So the rows are sorted here, by String.compareTo.
    final rows = _session._open.select(
      'SELECT key, value FROM kv WHERE namespace = ? AND instr(key, ?) = 1',
      [name, prefix],
    );

    final entries = [
      for (final row in rows) (row['key'] as String, row['value'] as String),
    ]..sort((a, b) => a.$1.compareTo(b.$1));

    return {for (final (key, value) in entries) key: value};
  }

  @override
  Future<void> batch(void Function(KeyValueBatch batch) build) async {
    _session._check();

    final staged = _SqliteBatch();
    // A throw here leaves the store untouched: nothing has been applied yet.
    try {
      build(staged);
    } finally {
      staged.closed = true;
    }
    if (staged.writes.isEmpty) return;

    _session._transaction((db) {
      for (final (key, value) in staged.writes) {
        if (value == null) {
          db.execute(_delete, [name, key]);
        } else {
          db.execute(_upsert, [name, key, value]);
        }
      }
    });
    await _session._afterWrite();
  }
}

final class _SqliteBatch implements KeyValueBatch {
  final List<(String, String?)> writes = [];

  /// Set once the builder returned or threw.
  bool closed = false;

  void _check() {
    if (closed) {
      throw StateError(
        '[forge] this batch was already applied; record every write before '
        'the builder returns, without awaiting',
      );
    }
  }

  @override
  void put(String key, String value) {
    _check();
    writes.add((key, value));
  }

  @override
  void delete(String key) {
    _check();
    writes.add((key, null));
  }
}
