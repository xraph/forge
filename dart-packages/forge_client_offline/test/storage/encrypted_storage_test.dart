@TestOn('vm')
library;

import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:forge_client_offline/src/keys/principal_hash.dart' show hex;
import 'package:forge_client_offline/src/storage/database_files_native.dart'
    show NativeDatabaseFiles, nativeDatabasePath, nativeStoreDirectory;
import 'package:forge_client_offline/src/storage/raw_key.dart';
import 'package:sqlite3/common.dart' show CommonDatabase;
import 'package:sqlite3/sqlite3.dart';

import '../support/fixed_keys.dart';
import '../support/interrupted_delete_files.dart';
import '../support/memory_secret_store.dart';

PendingMutationRecord record(String id) => PendingMutationRecord(
  id: id,
  operationId: 'op_update_order',
  argsJson: '{"v":1,"seq":1}',
  idempotencyKey: 'key-$id',
  createdAt: DateTime.utc(2026, 10, 4),
  stateJson: '{"kind":"queued"}',
);

final Matcher throwsNotADatabase = throwsA(
  isA<SqliteException>().having((e) => e.resultCode, 'resultCode', 26),
);

final Matcher throwsClosed = throwsA(isA<StateError>());

/// Stands in for a SQLite build without SQLite3MultipleCiphers. Records every
/// statement, and fails the test if anything would be written.
final class _PlainSqlite implements CommonDatabase {
  _PlainSqlite({this.configAnswers = false});

  /// Whether `sqlite3mc_config` exists although `PRAGMA cipher` is empty.
  final bool configAnswers;
  final List<String> statements = [];

  @override
  ResultSet select(String sql, [List<Object?> parameters = const []]) {
    statements.add(sql);
    if (sql == 'PRAGMA cipher') {
      return ResultSet(const ['cipher'], const [null], const []);
    }
    if (configAnswers && sql.contains('sqlite3mc_config')) {
      return ResultSet(const ['c'], const [null], const [
        ['chacha20'],
      ]);
    }
    throw SqliteException(
      extendedResultCode: 1,
      message: 'no such function: sqlite3mc_config',
    );
  }

  @override
  void execute(String sql, [List<Object?> parameters = const []]) =>
      fail('a database without a cipher must never be keyed or written: $sql');

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

/// Counts opens and closes and keeps the connection [open] returned last.
final class _CountingFiles implements DatabaseFiles {
  _CountingFiles(this.inner);

  final DatabaseFiles inner;
  int opens = 0;
  int closes = 0;
  CommonDatabase? last;

  @override
  Future<CommonDatabase> open(String principal) async {
    opens++;
    return last = await inner.open(principal);
  }

  @override
  String kind(String principal) => inner.kind(principal);

  @override
  Future<void> afterWrite(String principal) => inner.afterWrite(principal);

  @override
  Future<void> close(String principal) {
    closes++;
    return inner.close(principal);
  }

  @override
  Future<void> delete(String principal) => inner.delete(principal);

  @override
  Future<void> deleteAll() => inner.deleteAll();
}

/// Records every statement run on the connections it opens, in order.
final class _RecordingFiles implements DatabaseFiles {
  _RecordingFiles(this.inner);

  final DatabaseFiles inner;
  final List<String> statements = [];

  @override
  Future<CommonDatabase> open(String principal) async =>
      _RecordingDatabase(await inner.open(principal), statements);

  @override
  String kind(String principal) => inner.kind(principal);

  @override
  Future<void> afterWrite(String principal) => inner.afterWrite(principal);

  @override
  Future<void> close(String principal) => inner.close(principal);

  @override
  Future<void> delete(String principal) => inner.delete(principal);

  @override
  Future<void> deleteAll() => inner.deleteAll();
}

/// The members of a connection that open, unlock, migrate and the session
/// use, recorded and passed on.
final class _RecordingDatabase implements CommonDatabase {
  _RecordingDatabase(this.inner, this.statements);

  final CommonDatabase inner;
  final List<String> statements;

  @override
  ResultSet select(String sql, [List<Object?> parameters = const []]) {
    statements.add(sql);
    return inner.select(sql, parameters);
  }

  @override
  void execute(String sql, [List<Object?> parameters = const []]) {
    statements.add(sql);
    inner.execute(sql, parameters);
  }

  @override
  bool get autocommit => inner.autocommit;

  @override
  int get updatedRows => inner.updatedRows;

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

/// Wraps a KeyProvider so a test can hold an obtain or a delete half way and
/// see the order they ran in.
final class _GatedKeys implements KeyProvider {
  _GatedKeys(this.inner);

  final KeyProvider inner;
  final List<String> events = [];

  /// When set, obtain reads the key, then waits for this before returning it.
  Completer<void>? holdObtain;

  /// When set, delete waits for this before deleting.
  Completer<void>? holdDelete;

  /// When set, delete throws this after deleting nothing.
  Object? failDelete;

  @override
  Future<DatabaseKey> obtain(String principal) async {
    final key = await inner.obtain(principal);
    events.add('obtain');
    await holdObtain?.future;
    events.add('obtained');
    return key;
  }

  @override
  Future<void> delete(String principal) async {
    events.add('delete');
    await holdDelete?.future;
    final failure = failDelete;
    if (failure != null) throw failure;
    await inner.delete(principal);
    events.add('deleted');
  }
}

void main() {
  late Directory dir;
  late MemorySecretStore secrets;
  late KeystoreKeys keystore;
  late List<StorageReset> resets;

  setUp(() async {
    dir = await Directory.systemTemp.createTemp('forge_offline_');
    secrets = MemorySecretStore();
    keystore = keystoreKeys(store: secrets);
    resets = [];
  });

  tearDown(() async {
    if (await dir.exists()) await dir.delete(recursive: true);
  });

  EncryptedSqliteStorage storageWith({
    KeyProvider? keys,
    DatabaseFiles? files,
  }) {
    final storage = EncryptedSqliteStorage(
      keys: keys ?? keystore,
      files: files ?? NativeDatabaseFiles(dir.path, labels: keystore),
      secrets: [keystore],
      onReset: resets.add,
    );
    // Closes whatever a test left open, so the directory can go.
    addTearDown(() => storage.resetOfflineData());
    return storage;
  }

  Future<String> pathOf(String principal) =>
      nativeDatabasePath(dir.path, keystore, principal);

  /// The package's own subdirectory of the test directory.
  String store() => nativeStoreDirectory(dir.path);

  /// The names in the package's subdirectory, or none when it is gone.
  Set<String> storeNames() => Directory(store()).existsSync()
      ? {
          for (final e in Directory(store()).listSync())
            e.path.split(Platform.pathSeparator).last,
        }
      : {};

  Future<void> writeRecord(
    EncryptedSqliteStorage storage,
    String principal,
    String id,
  ) async {
    final session = await storage.open(principal);
    await session.enqueue(record(id));
    await session.close();
  }

  /// The key the keystore holds for [principal], creating one if none.
  Future<Uint8List> keyOf(String principal) async =>
      Uint8List.fromList((await keystore.obtain(principal)).bytes);

  /// The names of the keystore's per-principal key entries.
  Iterable<String> keyEntries() =>
      secrets.values.keys.where((name) => name.contains('.key.'));

  /// A raw connection to [path], keyed with [key] as the raw cipher key.
  Database rawOpen(String path, [Uint8List? key]) {
    final raw = sqlite3.open(path);
    addTearDown(raw.close);
    if (key != null) applyRawKey(raw, key);
    return raw;
  }

  Uint8List randomKey(int fill) => Uint8List(32)..fillRange(0, 32, fill);

  PassphraseKey pass(String secret) => PassphraseKey.weakForTesting(
    () => secret,
    salts: platformPassphraseSaltStore(directory: dir.path),
    labels: PrincipalLabels(secrets),
    memoryKiB: 64,
    iterations: 1,
    parallelism: 1,
  );

  group('encryption at rest', () {
    test('the file on disk is not a plaintext SQLite database', () async {
      await writeRecord(storageWith(), 'alice', 'a');

      final bytes = await File(await pathOf('alice')).readAsBytes();
      expect(String.fromCharCodes(bytes.take(15)), isNot('SQLite format 3'));
      expect(String.fromCharCodes(bytes), isNot(contains('op_update_order')));
    });

    test('opening the file without a key fails', () async {
      await writeRecord(storageWith(), 'alice', 'a');

      final raw = rawOpen(await pathOf('alice'));

      expect(
        () => raw.select('SELECT count(*) FROM sqlite_master'),
        throwsNotADatabase,
      );
    });

    test('opening the file with the wrong key fails', () async {
      await writeRecord(storageWith(), 'alice', 'a');

      final raw = rawOpen(await pathOf('alice'), randomKey(0xab));

      expect(
        () => raw.select('SELECT count(*) FROM sqlite_master'),
        throwsNotADatabase,
      );
    });

    test('the right key reads the records back', () async {
      await writeRecord(storageWith(), 'alice', 'a');

      final raw = rawOpen(await pathOf('alice'), await keyOf('alice'));
      expect(raw.select('SELECT id FROM outbox').single['id'], 'a');

      final session = await storageWith().open('alice');
      expect((await session.readOutbox()).map((r) => r.id), ['a']);
      expect((session as SqliteStorageSession).vfs, 'file');
      await session.close();
    });

    test('the key is the raw cipher key, never a passphrase', () async {
      await writeRecord(storageWith(), 'alice', 'a');
      final key = await keyOf('alice');

      final asPassphrase = rawOpen(await pathOf('alice'));
      asPassphrase.execute("PRAGMA hexkey = '${hex(key)}'");

      expect(
        () => asPassphrase.select('SELECT count(*) FROM sqlite_master'),
        throwsNotADatabase,
      );
      expect(
        rawOpen(await pathOf('alice'), key).select('SELECT id FROM outbox'),
        hasLength(1),
      );
    });

    test('a SQLite build without a cipher is refused and never keyed', () {
      final plain = _PlainSqlite();

      expect(
        () => unlockDatabase(plain, Uint8List(32)),
        throwsA(isA<EncryptionUnavailable>()),
      );
      expect(
        plain.statements.where((sql) => sql.contains('key')),
        isEmpty,
        reason: 'no key may be sent to a build that would ignore it',
      );
    });

    test('every connection waits 5 s for a lock before failing', () async {
      final files = _CountingFiles(
        NativeDatabaseFiles(dir.path, labels: keystore),
      );
      final session = await storageWith(files: files).open('alice');

      expect(
        files.last!.select('PRAGMA busy_timeout').single.values.single,
        5000,
      );
      await session.close();
    });
  });

  test('the busy timeout is set before anything reads the file', () async {
    final files = _RecordingFiles(
      NativeDatabaseFiles(dir.path, labels: keystore),
    );
    final session = await storageWith(files: files).open('alice');

    final timeout = files.statements.indexWhere(
      (sql) => sql.startsWith('PRAGMA busy_timeout'),
    );
    expect(timeout, 0, reason: 'before the cipher check, key and first read');
    await session.close();
  });

  test('a reset reaches the resets stream as well as onReset', () async {
    final storage = storageWith();
    final heard = <StorageReset>[];
    storage.resets.listen(heard.add);
    await writeRecord(storage, 'alice', 'a');
    secrets.values.removeWhere((name, _) => name.contains('.key.'));

    final session = await storage.open('alice');

    expect(heard.single.principal, 'alice', reason: 'delivered synchronously');
    expect(resets.single.principal, 'alice');
    await session.close();
  });

  test('a database started fresh also waits 5 s for a lock', () async {
    final files = _CountingFiles(
      NativeDatabaseFiles(dir.path, labels: keystore),
    );
    final storage = storageWith(files: files);
    await writeRecord(storage, 'alice', 'a');
    secrets.values.removeWhere((name, _) => name.contains('.key.'));

    final session = await storage.open('alice');

    expect(resets, hasLength(1));
    expect(
      files.last!.select('PRAGMA busy_timeout').single.values.single,
      5000,
    );
    await session.close();
  });

  group('cipherAvailable', () {
    test('is true for the native build', () {
      final db = sqlite3.openInMemory();
      addTearDown(db.close);

      expect(cipherAvailable(db), isTrue);
    });

    test(
      'is false when neither PRAGMA cipher nor sqlite3mc_config answers',
      () {
        expect(cipherAvailable(_PlainSqlite()), isFalse);
      },
    );

    test('trusts sqlite3mc_config when PRAGMA cipher is silent', () {
      expect(cipherAvailable(_PlainSqlite(configAnswers: true)), isTrue);
    });
  });

  group('keys', () {
    test('a wrong passphrase is refused and the database is kept', () async {
      await writeRecord(storageWith(keys: pass('right')), 'alice', 'a');
      await expectLater(
        storageWith(keys: pass('wrong')).open('alice'),
        throwsA(isA<WrongKey>()),
      );
      expect(resets, isEmpty);

      final session = await storageWith(keys: pass('right')).open('alice');
      expect((await session.readOutbox()).map((r) => r.id), ['a']);
      await session.close();
    });

    test(
      'a passphrase salt sidecar is born with the database and dies with it',
      () async {
        final label = await keystore.principalLabel('alice');
        final sidecar = File('${store()}/$label.salt/salt');
        final storage = storageWith(keys: pass('right'));

        await writeRecord(storage, 'alice', 'a');
        expect(await sidecar.exists(), isTrue);
        expect(await sidecar.length(), 16);

        await storage.destroy('alice');
        expect(await sidecar.exists(), isFalse);
        expect(await sidecar.parent.exists(), isFalse);
        expect(await File(await pathOf('alice')).exists(), isFalse);
      },
    );

    test(
      'a key lost from the keystore starts a fresh database and never throws',
      () async {
        final storage = storageWith();
        await writeRecord(storage, 'alice', 'a');

        // What restoring a backup onto a new device does to a ThisDeviceOnly
        // key that the install salt survived.
        secrets.values.removeWhere((name, _) => name.contains('.key.'));

        final session = await storage.open('alice');
        expect(await session.readOutbox(), isEmpty);
        expect(resets.single.principal, 'alice');

        await session.enqueue(record('b'));
        expect((await session.readOutbox()).map((r) => r.id), ['b']);
        await session.close();
      },
    );

    test(
      'a keystore lost whole names a new file and never reads the old one',
      () async {
        final storage = storageWith();
        await writeRecord(storage, 'alice', 'a');
        final oldPath = await pathOf('alice');

        secrets.values.clear();
        PrincipalLabels.forgetCachedSalts();

        final session = await storage.open('alice');
        expect(await session.readOutbox(), isEmpty);
        expect(await pathOf('alice'), isNot(oldPath));
        expect(
          await File(oldPath).exists(),
          isTrue,
          reason: 'orphaned until resetOfflineData',
        );
        await session.close();
      },
    );

    test(
      'a keystore that cannot be read fails closed and leaves the file alone',
      () async {
        final storage = storageWith();
        await writeRecord(storage, 'alice', 'a');
        final before = await File(await pathOf('alice')).readAsBytes();

        secrets.failReadsWith = StateError(
          'keychain locked before first unlock',
        );
        await expectLater(
          storage.open('alice'),
          throwsA(isA<KeyUnavailable>()),
        );

        expect(await File(await pathOf('alice')).readAsBytes(), before);
        expect(resets, isEmpty);

        secrets.failReadsWith = null;
        final session = await storage.open('alice');
        expect((await session.readOutbox()).map((r) => r.id), ['a']);
        await session.close();
      },
    );
  });

  group('sign-out', () {
    test('destroy deletes the key, the file and its siblings', () async {
      final storage = storageWith();
      await writeRecord(storage, 'alice', 'a');
      final path = await pathOf('alice');
      for (final suffix in ['-journal', '-wal', '-shm']) {
        await File('$path$suffix').writeAsString('left over');
      }

      await storage.destroy('alice');

      expect(keyEntries(), isEmpty);
      for (final suffix in ['', '-journal', '-wal', '-shm']) {
        expect(await File('$path$suffix').exists(), isFalse, reason: suffix);
      }
    });

    test(
      'destroy deletes the salt sidecar even when the keys leave it',
      () async {
        // FixedKeys.delete forgets nothing on disk, so only the storage's own
        // file delete can remove the sidecar.
        final keys = FixedKeys(3);
        final storage = storageWith(
          keys: keys,
          files: NativeDatabaseFiles(dir.path, labels: keys),
        );
        await writeRecord(storage, 'alice', 'a');
        final label = await keys.principalLabel('alice');
        final sidecar = File('${store()}/$label.salt/salt')
          ..createSync(recursive: true)
          ..writeAsStringSync('s');
        // And the temporary directory of a creator that crashed.
        File('${store()}/$label.salt.tmp-0123456789abcdef/salt')
          ..createSync(recursive: true)
          ..writeAsStringSync('t');

        await storage.destroy('alice');

        expect(keys.deleted, ['alice']);
        expect(await sidecar.exists(), isFalse);
        expect(storeNames(), isEmpty);
      },
    );

    test('an interrupted delete still leaves the data unreadable', () async {
      final files = InterruptedDeleteFiles(
        NativeDatabaseFiles(dir.path, labels: keystore),
      );
      final storage = storageWith(files: files);
      await writeRecord(storage, 'alice', 'a');
      final oldKey = await keyOf('alice');
      // A crash stops everything after it, so the key must already be gone
      // when deleting the files begins.
      bool? keyLeftAtDelete;
      files.beforeDelete = () => keyLeftAtDelete = keyEntries().isNotEmpty;

      await expectLater(storage.destroy('alice'), throwsStateError);
      expect(keyLeftAtDelete, isFalse);

      expect(
        await File(await pathOf('alice')).exists(),
        isTrue,
        reason: 'the simulated crash left the file',
      );
      expect(
        keyEntries(),
        isEmpty,
        reason: 'the key went first, so the leftover file has no key',
      );

      files.interrupt = false;
      final session = await storage.open('alice');
      expect(await session.readOutbox(), isEmpty);
      expect(resets, hasLength(1));
      expect(await keyOf('alice'), isNot(oldKey));
      await session.close();
    });

    test('a copy of the file taken before destroy can never be read', () async {
      final storage = storageWith();
      await writeRecord(storage, 'alice', 'a');
      final path = await pathOf('alice');
      final copy = await File(path).copy('${dir.path}/stolen-copy');
      final oldKey = await keyOf('alice');

      await storage.destroy('alice');

      expect(keyEntries(), isEmpty, reason: 'the key is gone');
      final keyless = rawOpen(copy.path);
      expect(
        () => keyless.select('SELECT count(*) FROM sqlite_master'),
        throwsNotADatabase,
      );
      final wrong = rawOpen(copy.path, randomKey(0x5a));
      expect(
        () => wrong.select('SELECT count(*) FROM sqlite_master'),
        throwsNotADatabase,
      );

      // Put the copy back where it was: the storage still cannot read it, so
      // it deletes it unread and starts fresh.
      await copy.copy(path);
      final session = await storage.open('alice');
      expect(await session.readOutbox(), isEmpty);
      expect(resets.single.principal, 'alice');
      expect(await keyOf('alice'), isNot(oldKey));
      await session.close();
    });

    test('destroy revokes every open handle, and their namespaces', () async {
      final storage = storageWith();
      final first = await storage.open('alice');
      final second = await storage.open('alice');
      final store = second.namespace('replica');

      final destroying = storage.destroy('alice');
      // Revoked at the call, before destroy yields to anything else.
      expect((first as SqliteStorageSession).isClosed, isTrue);
      expect((second as SqliteStorageSession).isClosed, isTrue);
      await destroying;

      expect(first.readOutbox(), throwsClosed);
      expect(second.enqueue(record('late')), throwsClosed);
      expect(store.put('k', 'v'), throwsClosed);
      // Closing a revoked handle is still fine.
      await first.close();
    });

    test(
      'destroy is safe right after sign-out with handles still open',
      () async {
        final storage = storageWith();
        final session = await storage.open('alice');
        await session.enqueue(record('a'));

        // cache.setPrincipal(null); await cache.idle; then:
        await storage.destroy('alice');

        expect(session.readOutbox(), throwsClosed);
        final fresh = await storage.open('alice');
        expect(await fresh.readOutbox(), isEmpty);
        expect(resets, isEmpty, reason: 'the file went with the key');
        await fresh.close();
      },
    );

    test('destroy runs every step and rethrows the first failure', () async {
      final keys = _GatedKeys(keystore)
        ..failDelete = StateError('keystore delete failed');
      final files = _CountingFiles(
        NativeDatabaseFiles(dir.path, labels: keystore),
      );
      final storage = storageWith(keys: keys, files: files);
      await writeRecord(storage, 'alice', 'a');
      final session = await storage.open('alice');
      final closesBefore = files.closes;

      await expectLater(
        storage.destroy('alice'),
        throwsA(
          isA<StateError>().having(
            (e) => e.message,
            'message',
            'keystore delete failed',
          ),
        ),
      );

      expect(
        files.closes,
        greaterThan(closesBefore),
        reason: 'the connection was closed',
      );
      expect(
        await File(await pathOf('alice')).exists(),
        isFalse,
        reason: 'the files were still deleted',
      );
      expect(session.readOutbox(), throwsClosed);
    });
  });

  group('serialized per principal', () {
    test(
      'an open still running when destroy is called hands out a revoked handle',
      () async {
        final keys = _GatedKeys(keystore);
        final storage = storageWith(keys: keys);
        await writeRecord(storage, 'alice', 'a');
        keys
          ..events.clear()
          ..holdObtain = Completer<void>();
        final oldKey = await keyOf('alice');

        final opening = storage.open('alice');
        await pumpEventQueue();
        expect(keys.events, [
          'obtain',
        ], reason: 'the old key was read before destroy');
        final destroying = storage.destroy('alice');
        await pumpEventQueue();
        expect(keys.events, [
          'obtain',
        ], reason: 'destroy waits for the running open');

        keys.holdObtain!.complete();
        final stale = await opening;
        await destroying;

        expect(keys.events, ['obtain', 'obtained', 'delete', 'deleted']);
        expect(stale.readOutbox(), throwsClosed);

        keys.holdObtain = null;
        final fresh = await storage.open('alice');
        expect(
          await fresh.readOutbox(),
          isEmpty,
          reason: 'the old file is gone, not reopened',
        );
        expect(resets, isEmpty);
        expect(await keyOf('alice'), isNot(oldKey));
        await fresh.close();
      },
    );

    test(
      'an open called while destroy runs waits and gets a new key and file',
      () async {
        final keys = _GatedKeys(keystore);
        final storage = storageWith(keys: keys);
        await writeRecord(storage, 'alice', 'a');
        final oldKey = await keyOf('alice');
        keys
          ..events.clear()
          ..holdDelete = Completer<void>();

        final destroying = storage.destroy('alice');
        await pumpEventQueue();
        var opened = false;
        final opening = storage.open('alice').then((session) {
          opened = true;
          return session;
        });
        await pumpEventQueue();
        expect(opened, isFalse);
        expect(keys.events, ['delete']);

        keys.holdDelete!.complete();
        await destroying;
        final session = await opening;

        expect(keys.events, ['delete', 'deleted', 'obtain', 'obtained']);
        expect(await session.readOutbox(), isEmpty);
        expect(await keyOf('alice'), isNot(oldKey));
        expect(resets, isEmpty);
        await session.close();
      },
    );

    test('other principals are not held up by a running destroy', () async {
      final keys = _GatedKeys(keystore);
      final storage = storageWith(keys: keys);
      await writeRecord(storage, 'alice', 'a');
      keys.holdDelete = Completer<void>();

      final destroying = storage.destroy('alice');
      await pumpEventQueue();
      final bob = await storage.open('bob');
      expect(await bob.readOutbox(), isEmpty);

      keys.holdDelete!.complete();
      await destroying;
      await bob.close();
    });
  });

  group('principals', () {
    test("principal B never reads principal A's rows", () async {
      final storage = storageWith();
      await writeRecord(storage, 'alice', 'a');

      final bob = await storage.open('bob');
      expect(await bob.readOutbox(), isEmpty);
      await bob.close();

      expect(await pathOf('alice'), isNot(await pathOf('bob')));

      final raw = rawOpen(await pathOf('alice'), await keyOf('bob'));
      expect(
        () => raw.select('SELECT count(*) FROM sqlite_master'),
        throwsNotADatabase,
      );
    });

    test(
      'file names are the label, never the principal or its plain hash',
      () async {
        const principal = 'alice@example.com';
        final path = await pathOf(principal);
        final label = await keystore.principalLabel(principal);

        expect(path, '${store()}${Platform.pathSeparator}$label.db');
        expect(
          store(),
          '${dir.path}${Platform.pathSeparator}forge_client_offline',
        );
        expect(path, isNot(contains('alice')));
        expect(path, isNot(contains(await principalHash(principal))));
      },
    );

    test(
      'a label that is not 64 lowercase hex characters is refused',
      () async {
        final files = NativeDatabaseFiles(dir.path, labels: _BadLabels());

        await expectLater(files.open('alice'), throwsArgumentError);
        expect(storeNames(), isEmpty);
      },
    );
  });

  group('lifecycle', () {
    test('every open is a distinct handle over one connection', () async {
      final files = _CountingFiles(
        NativeDatabaseFiles(dir.path, labels: keystore),
      );
      final storage = storageWith(files: files);

      final first = await storage.open('alice');
      final second = await storage.open('alice');
      expect(identical(first, second), isFalse);
      expect(files.opens, 1);

      await first.close();
      await second.enqueue(record('a'));
      expect(files.closes, 0, reason: 'a sibling still uses the connection');

      await second.close();
      expect(files.closes, 1, reason: 'the last handle closed it');

      final third = await storage.open('alice');
      expect(files.opens, 2);
      expect((await third.readOutbox()).map((r) => r.id), ['a']);
      await third.close();
    });

    test('a database from a newer schema is refused, not wiped', () async {
      final storage = storageWith();
      await writeRecord(storage, 'alice', 'a');

      final raw = sqlite3.open(await pathOf('alice'));
      applyRawKey(raw, await keyOf('alice'));
      raw.execute(
        'INSERT INTO forge_migrations (version, applied_at) VALUES (99, 0)',
      );
      raw.close();

      await expectLater(
        storage.open('alice'),
        throwsA(isA<UnsupportedSchemaVersion>()),
      );
      expect(resets, isEmpty);
      expect(await File(await pathOf('alice')).exists(), isTrue);
    });

    test('a directory is required on native platforms', () {
      expect(
        () => encryptedSqliteStorage(keys: FixedKeys(1)),
        throwsArgumentError,
      );
    });

    test('a labeler is required when the keys are not one', () {
      expect(
        () => encryptedSqliteStorage(keys: pass('right'), directory: dir.path),
        throwsA(isA<ArgumentError>().having((e) => e.name, 'name', 'labels')),
      );
      expect(
        encryptedSqliteStorage(
          keys: pass('right'),
          directory: dir.path,
          labels: PrincipalLabels(secrets),
        ),
        isA<EncryptedSqliteStorage>(),
      );
    });
  });

  group('resetOfflineData', () {
    test('leaves no file and no keystore entry of the package', () async {
      final storage = storageWith();
      await writeRecord(storage, 'alice', 'a');
      final open = await storage.open('bob');
      await writeRecord(storageWith(keys: pass('carol secret')), 'carol', 'c');
      final label = await keystore.principalLabel('dave');
      File('${store()}/$label.salt.tmp-0123456789abcdef/salt')
        ..createSync(recursive: true)
        ..writeAsStringSync('x');
      await File('${store()}/$label.db-journal').writeAsString('x');
      // Not the package's: left alone, even when named like one of its own.
      final appLabel = 'c0' * 32;
      final appFiles = {
        'app.db',
        'notes.txt',
        '$appLabel.db',
        '$appLabel.db-wal',
        '$appLabel.db-shm',
        '$appLabel.salt',
      };
      for (final name in appFiles) {
        await File('${dir.path}/$name').writeAsString('the app');
      }
      expect(secrets.values, isNotEmpty);

      await storage.resetOfflineData();

      expect(secrets.values, isEmpty);
      expect(
        dir
            .listSync()
            .map((e) => e.path.split(Platform.pathSeparator).last)
            .toSet(),
        appFiles,
      );
      for (final name in appFiles) {
        expect(File('${dir.path}/$name').readAsStringSync(), 'the app');
      }
      expect(open.readOutbox(), throwsClosed);

      final fresh = await storage.open('alice');
      expect(await fresh.readOutbox(), isEmpty);
      expect(resets, isEmpty);
      await fresh.close();
    });

    test('never follows a package subdirectory that is a symlink', () async {
      final elsewhere = Directory.systemTemp.createTempSync('forge_elsewhere_');
      addTearDown(() => elsewhere.deleteSync(recursive: true));
      final label = 'd0' * 32;
      final victims = [
        File('${elsewhere.path}/$label.db'),
        File('${elsewhere.path}/$label.db-wal'),
        File('${elsewhere.path}/$label.salt/salt'),
      ];
      for (final file in victims) {
        file
          ..createSync(recursive: true)
          ..writeAsStringSync('not the package');
      }
      final link = Link(store())..createSync(elsewhere.path);

      await storageWith().resetOfflineData();

      expect(link.existsSync(), isTrue);
      for (final file in victims) {
        expect(file.readAsStringSync(), 'not the package', reason: file.path);
      }
    });

    test('also erases the labeler of passphrase-keyed storage', () async {
      final labels = PrincipalLabels(secrets);
      final storage = encryptedSqliteStorage(
        keys: pass('right'),
        directory: dir.path,
        labels: labels,
      );
      await writeRecord(storage, 'alice', 'a');
      expect(secrets.values, isNotEmpty);

      await storage.resetOfflineData();

      expect(secrets.values, isEmpty);
      expect(storeNames(), isEmpty);
    });

    test('waits for a running open and revokes the handle it made', () async {
      final keys = _GatedKeys(keystore)..holdObtain = Completer<void>();
      final storage = storageWith(keys: keys);

      final opening = storage.open('alice');
      await pumpEventQueue();
      final resetting = storage.resetOfflineData();
      keys.holdObtain!.complete();
      final stale = await opening;
      await resetting;

      expect(stale.readOutbox(), throwsClosed);
      expect(storeNames(), isEmpty);
      expect(secrets.values, isEmpty);
    });

    test('holds back an open called while it runs', () async {
      final eraser = _GatedEraser();
      final storage = EncryptedSqliteStorage(
        keys: keystore,
        files: NativeDatabaseFiles(dir.path, labels: keystore),
        secrets: [eraser],
      );
      await writeRecord(storage, 'alice', 'a');

      final resetting = storage.resetOfflineData();
      await pumpEventQueue();
      expect(eraser.started, isTrue);
      var opened = false;
      final opening = storage.open('alice').then((session) {
        opened = true;
        return session;
      });
      await pumpEventQueue();
      expect(opened, isFalse, reason: 'the open waits for the reset');

      eraser.gate.complete();
      await resetting;
      final session = await opening;

      expect(await session.readOutbox(), isEmpty);
      await session.close();
    });
  });
}

final class _BadLabels implements PrincipalLabeler {
  @override
  Future<String> principalLabel(String principal) async => '../$principal';
}

/// An ErasableSecrets whose deleteAll waits for [gate], to hold a reset open.
final class _GatedEraser implements ErasableSecrets {
  final Completer<void> gate = Completer<void>();
  bool started = false;

  @override
  Future<void> deleteAll() async {
    started = true;
    await gate.future;
  }
}
