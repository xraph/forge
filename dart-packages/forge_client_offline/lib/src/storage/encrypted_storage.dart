import 'dart:async';
import 'dart:typed_data';

import 'package:forge_client/forge_client.dart';
import 'package:sqlite3/common.dart';

import '../keys/key_provider.dart';
import 'database_files.dart';
import 'raw_key.dart';
import 'schema.dart';
import 'sqlite_session.dart';

/// SQLITE_NOTADB: the key does not decrypt the file.
const int _sqliteNotADatabase = 26;

/// How long a connection waits for another connection's lock on the same
/// file (another isolate or process) before failing with SQLITE_BUSY.
const int _busyTimeoutMs = 5000;

/// One encrypted SQLite database per principal, keyed by [keys].
///
/// On native platforms [directory] is required; pass
/// `(await getApplicationSupportDirectory()).path`. The package keeps every
/// file in its own `forge_client_offline` subdirectory of it, and never
/// touches anything else there. On the web [directory]
/// prefixes the OPFS path or IndexedDB name, and [wasmUri] locates
/// `sqlite3mc.wasm` (default: `sqlite3mc.wasm` next to the page).
///
/// Files are named by [labels], an HMAC of the principal under a per-install
/// salt kept in the keystore, never by the principal. It defaults to [keys]
/// when [keys] is a [PrincipalLabeler], as `keystoreKeys()` is. A
/// `passphraseKey` is not, so pass the same `PrincipalLabels` it was given,
/// and give it `platformPassphraseSaltStore(directory: directory)` so its salt
/// sidecar lives and dies with the database.
///
/// [onReset] is told when a database had to be started fresh because its key
/// no longer fits, for example after an OS restore brought the file back but
/// not the keystore entry.
EncryptedSqliteStorage encryptedSqliteStorage({
  required KeyProvider keys,
  String? directory,
  Uri? wasmUri,
  PrincipalLabeler? labels,
  void Function(StorageReset reset)? onReset,
}) {
  final namer =
      labels ??
      switch (keys) {
        final PrincipalLabeler keys => keys,
        _ => null,
      };
  return EncryptedSqliteStorage(
    keys: keys,
    files: platformDatabaseFiles(
      directory: directory,
      wasmUri: wasmUri,
      labels: namer,
    ),
    secrets: [if (namer case final ErasableSecrets eraser) eraser],
    onReset: onReset,
  );
}

/// Why a principal's database was started fresh.
final class StorageReset {
  /// [principal]'s database was replaced for [reason].
  const StorageReset({required this.principal, required this.reason});

  /// Whose database was replaced.
  final String principal;

  /// A sentence for logs.
  final String reason;
}

/// Thrown when a passphrase key does not decrypt the database. The database
/// is kept, so the right passphrase still opens it.
final class WrongKey implements Exception {
  /// Creates the error.
  const WrongKey();

  @override
  String toString() =>
      'WrongKey: this key does not decrypt the stored database';
}

/// Thrown when the linked SQLite has no cipher. Storage refuses to open
/// rather than write anything unencrypted.
final class EncryptionUnavailable implements Exception {
  /// Creates the error.
  const EncryptionUnavailable();

  @override
  String toString() =>
      'EncryptionUnavailable: this SQLite build has no cipher. Add '
      '"hooks: user_defines: sqlite3: source: sqlite3mc" to the app pubspec.yaml, '
      'or load sqlite3mc.wasm on the web.';
}

/// Whether [db] is SQLite3MultipleCiphers. Plain SQLite ignores unknown
/// pragmas, so `PRAGMA cipher` returns no rows there; the sqlite3mc SQL
/// function is the second opinion.
bool cipherAvailable(CommonDatabase db) {
  if (db.select('PRAGMA cipher').isNotEmpty) return true;
  try {
    db.select("SELECT sqlite3mc_config('cipher')");
    return true;
  } on SqliteException {
    return false;
  }
}

/// Keys [db] with the raw 32-byte [key] and proves the key fits by reading
/// the schema.
///
/// Throws [EncryptionUnavailable] on a build without a cipher, before any key
/// is sent, and a [SqliteException] with result code 26 (SQLITE_NOTADB) when
/// the key does not decrypt the file. The key is applied as the cipher key
/// itself (see `applyRawKey`), never as a passphrase.
void unlockDatabase(CommonDatabase db, Uint8List key) {
  if (!cipherAvailable(db)) throw const EncryptionUnavailable();
  applyRawKey(db, key);
  db.select('SELECT count(*) FROM sqlite_master');
}

/// The [StorageAdapter] [encryptedSqliteStorage] returns.
///
/// Every [open] returns a distinct session handle. Handles for one principal
/// share one connection, which closes when the last of them closes; closing
/// one handle leaves the others working.
///
/// [open] and [destroy] for one principal run one at a time, in call order.
/// [destroy] revokes every handle the moment it is called, and again when its
/// turn comes, so a handle from an [open] that was still running when
/// [destroy] was called stops working too, and an [open] called after
/// [destroy] gets a new key and a new file.
///
/// Use one instance per storage location in an isolate. Two instances over
/// one directory each serialize only their own calls.
final class EncryptedSqliteStorage implements StorageAdapter {
  /// Stores each principal's database through [files] under a key from
  /// [keys].
  ///
  /// [resetOfflineData] clears [keys] when it is [ErasableSecrets], and every
  /// store in [secrets], for example the `PrincipalLabels` that names the
  /// files of a passphrase-keyed database.
  EncryptedSqliteStorage({
    required this._keys,
    required this._files,
    Iterable<ErasableSecrets> secrets = const [],
    this._onReset,
  }) : _secrets = [...secrets];

  final KeyProvider _keys;
  final DatabaseFiles _files;
  final List<ErasableSecrets> _secrets;
  final void Function(StorageReset reset)? _onReset;

  /// The open connection of each principal that has live handles.
  final Map<String, _Connection> _connections = {};

  /// The last queued operation of each principal; it never fails.
  final Map<String, Future<void>> _tails = {};

  /// Completes when the running [resetOfflineData] finishes.
  Future<void>? _resetting;

  final StreamController<StorageReset> _resets =
      StreamController<StorageReset>.broadcast(sync: true);

  /// Every database started fresh, as it happens: the same events the
  /// `onReset` callback gets. Broadcast and synchronous, so a listener added
  /// before [open] hears a reset before [open] returns. `OfflineClient.open`
  /// listens here, so a reset that drops queued writes is never silent.
  Stream<StorageReset> get resets => _resets.stream;

  @override
  Future<StorageSession> open(String principal) =>
      _serialized(principal, () async {
        final live = _connections[principal];
        if (live != null) return _handle(principal, live);

        final key = await _keys.obtain(principal);
        final db = await _unlocked(principal, key);
        try {
          migrate(db);
        } on Object {
          await _files.close(principal);
          rethrow;
        }

        final connection = _Connection(db);
        _connections[principal] = connection;
        return _handle(principal, connection);
      });

  /// Opens and unlocks [principal]'s file with [key]. A key that does not fit
  /// throws [WrongKey], or, when the key says so, deletes the file unread and
  /// starts a fresh one.
  Future<CommonDatabase> _unlocked(String principal, DatabaseKey key) async {
    final db = await _files.open(principal);
    try {
      _waitForLocks(db);
      unlockDatabase(db, key.bytes);
      return db;
    } on SqliteException catch (error) {
      await _files.close(principal);
      if (error.resultCode != _sqliteNotADatabase) rethrow;
      if (!key.resetOnMismatch) throw const WrongKey();
    } on Object {
      await _files.close(principal);
      rethrow;
    }

    await _files.delete(principal);
    final reset = StorageReset(
      principal: principal,
      reason: key.created
          ? 'no key was stored for this database, so it was deleted unread '
                'and started fresh'
          : 'the stored key does not decrypt this database, so it was '
                'deleted unread and started fresh',
    );
    _resets.add(reset);
    _onReset?.call(reset);

    final fresh = await _files.open(principal);
    try {
      _waitForLocks(fresh);
      unlockDatabase(fresh, key.bytes);
    } on Object {
      await _files.close(principal);
      rethrow;
    }
    return fresh;
  }

  /// Sets the busy timeout before anything reads the file, so the key check
  /// and a hot-journal rollback wait for another connection's lock too.
  /// The pragma reads nothing from the file, so it is safe before the key.
  static void _waitForLocks(CommonDatabase db) =>
      db.execute('PRAGMA busy_timeout = $_busyTimeoutMs');

  StorageSession _handle(String principal, _Connection connection) {
    late final SqliteStorageSession session;
    session = SqliteStorageSession(
      principal: principal,
      database: connection.database,
      vfs: _files.kind(principal),
      afterWrite: () => connection.revoked
          ? Future<void>.value()
          : _files.afterWrite(principal),
      onClose: () => _release(principal, connection, session),
    );
    connection.handles.add(session);
    return session;
  }

  /// Drops [session] from [connection], closing the connection when it was
  /// the last handle. A revoked connection is closed by whoever revoked it.
  Future<void> _release(
    String principal,
    _Connection connection,
    SqliteStorageSession session,
  ) async {
    connection.handles.remove(session);
    if (connection.revoked || connection.handles.isNotEmpty) return;

    connection.revoked = true;
    if (identical(_connections[principal], connection)) {
      _connections.remove(principal);
    }
    await _serialized(principal, () async {
      // An open that ran first replaced the connection, and closed this one
      // as it did; the new one is not this handle's to close.
      if (!_connections.containsKey(principal)) await _files.close(principal);
    });
  }

  /// Revokes every handle of [principal]'s connection: each one's next call
  /// throws [StateError]. The connection itself is closed later, in turn.
  void _revoke(String principal) {
    final connection = _connections.remove(principal);
    if (connection == null) return;

    connection.revoked = true;
    final handles = [...connection.handles];
    connection.handles.clear();
    for (final handle in handles) {
      // Marks the handle closed before its first await; its onClose sees the
      // connection revoked and returns, so nothing here waits on a turn.
      unawaited(handle.close());
    }
  }

  /// Revokes [principal]'s handles, then closes the connection, deletes the
  /// key (the crypto-shred: once it is gone no copy of the file can be read)
  /// and deletes the database, its journal siblings and its salt sidecar.
  ///
  /// Every step runs even when an earlier one fails, and the first failure is
  /// rethrown at the end. Deleting the key comes before deleting the files,
  /// so a delete interrupted half way leaves a file nothing can decrypt.
  /// If destroy throws, call it again: every step is safe to repeat, and a
  /// retry finishes the deletion.
  ///
  /// Safe to call with handles still open, for example right after
  /// `cache.setPrincipal(null)` and `await cache.idle` on sign-out, and for a
  /// principal that was never opened.
  @override
  Future<void> destroy(String principal) {
    _revoke(principal);
    return _serialized(principal, () async {
      // Handles an open made while this waited for its turn.
      _revoke(principal);
      await _allSteps([
        () => _files.close(principal),
        () => _keys.delete(principal),
        () => _files.delete(principal),
      ]);
    });
  }

  /// Erases everything this package keeps on the device, for every principal:
  /// every handle is revoked, every key and the install salt are removed from
  /// the keystore namespace, then every database, journal and salt sidecar
  /// the package owns is deleted, including those of principals this storage
  /// never opened or can no longer name. On native platforms that is only the
  /// package's `forge_client_offline` subdirectory: files the app keeps in
  /// the directory it passed are never touched, whatever they are called.
  ///
  /// Never called automatically. Call it after the user confirms, or when
  /// [open] keeps throwing [KeyUnavailable] while the device is unlocked. One
  /// example: an Android Auto Backup restore brought back the secure-storage
  /// preferences without the Keystore key that decrypts them, so neither the
  /// install salt nor any key can be read, and nothing short of erasing them
  /// lets the app store data again. Queued writes that never reached the
  /// server are lost.
  ///
  /// Calls to [open] and [destroy] made while this runs wait for it, and
  /// calls already running finish first. Every step runs even when an earlier
  /// one fails, and the first failure is rethrown at the end.
  Future<void> resetOfflineData() async {
    final done = Completer<void>();
    final earlier = _resetting;
    _resetting = done.future;
    final running = [..._tails.values];
    [..._connections.keys].forEach(_revoke);

    try {
      if (earlier != null) await earlier;
      await Future.wait(running);
      [..._connections.keys].forEach(_revoke);

      final erasers = <ErasableSecrets>[
        if (_keys case final ErasableSecrets keys) keys,
        for (final secrets in _secrets)
          if (!identical(secrets, _keys)) secrets,
      ];
      await _allSteps([
        for (final eraser in erasers) eraser.deleteAll,
        _files.deleteAll,
      ]);
    } finally {
      if (identical(_resetting, done.future)) _resetting = null;
      done.complete();
    }
  }

  /// Runs [body] after every earlier operation on [principal] and after any
  /// [resetOfflineData] that started before it.
  Future<T> _serialized<T>(String principal, Future<T> Function() body) {
    final previous = _tails[principal];
    final reset = _resetting;

    Future<T> run() async {
      if (previous != null) await previous;
      if (reset != null) await reset;
      return body();
    }

    final result = run();
    final tail = result.then<void>((_) {}, onError: (Object _) {});
    _tails[principal] = tail;
    unawaited(
      tail.then((_) {
        if (identical(_tails[principal], tail)) _tails.remove(principal);
      }),
    );
    return result;
  }
}

/// Runs every step in order even when one fails, then rethrows the first
/// failure.
Future<void> _allSteps(List<Future<void> Function()> steps) async {
  Object? failure;
  StackTrace? trace;
  for (final step in steps) {
    try {
      await step();
    } on Object catch (error, stack) {
      if (failure == null) {
        failure = error;
        trace = stack;
      }
    }
  }
  if (failure != null) Error.throwWithStackTrace(failure, trace!);
}

/// One principal's open database and the handles sharing it.
final class _Connection {
  _Connection(this.database);

  final CommonDatabase database;
  final Set<SqliteStorageSession> handles = {};

  /// Set once the connection is retired: revoked by a destroy or a reset, or
  /// released by its last handle. Its handles must not close it again.
  bool revoked = false;
}
