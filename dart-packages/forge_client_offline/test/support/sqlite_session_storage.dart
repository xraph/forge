import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:forge_client_offline/src/storage/raw_key.dart';
import 'package:sqlite3/sqlite3.dart';

import 'memory_secret_store.dart';

/// The thinnest [StorageAdapter] over [SqliteStorageSession] that the
/// conformance harness can run against: one encrypted file per principal in
/// [directory], named by its [PrincipalLabels] label, and one connection per
/// [open], so sibling sessions are separate connections to one file.
///
/// It is not the package's adapter. It has no session registry, so it cannot
/// revoke sessions, and [destroy] refuses. Task 8's EncryptedSqliteStorage owns
/// both, and runs the harness's destroy cases.
final class SqliteSessionStorage implements StorageAdapter {
  SqliteSessionStorage(this.directory);

  final Directory directory;

  /// Writes seen by every session's afterWrite hook, across principals.
  int writes = 0;

  final PrincipalLabels _labels = PrincipalLabels(MemorySecretStore());
  final Map<String, Uint8List> _keys = {};
  final List<SqliteStorageSession> _sessions = [];
  final Random _random = Random.secure();

  /// Where [principal]'s database lives.
  Future<String> pathOf(String principal) async =>
      '${directory.path}/${await _labels.label(principal)}.db';

  /// The raw key [principal]'s database is encrypted under.
  Uint8List keyOf(String principal) => _keys.putIfAbsent(
    principal,
    () => Uint8List.fromList(List.generate(32, (_) => _random.nextInt(256))),
  );

  @override
  Future<StorageSession> open(String principal) async {
    final db = sqlite3.open(await pathOf(principal));
    try {
      applyRawKey(db, keyOf(principal));
      // Sibling sessions are separate connections to one file: wait for a
      // sibling's write lock instead of failing with SQLITE_BUSY.
      db.execute('PRAGMA busy_timeout = 5000');
      migrate(db);
    } on Object {
      db.close();
      rethrow;
    }

    final session = SqliteStorageSession(
      principal: principal,
      database: db,
      vfs: 'file',
      afterWrite: () async => writes++,
      onClose: () async => db.close(),
    );
    _sessions.add(session);
    return session;
  }

  @override
  Future<void> destroy(String principal) async => throw StateError(
    'SqliteSessionStorage has no session registry to revoke; destroy belongs '
    'to EncryptedSqliteStorage (Task 8)',
  );

  /// Closes every session still open, so the directory can be deleted.
  Future<void> closeAll() async {
    for (final session in _sessions) {
      await session.close();
    }
  }
}
