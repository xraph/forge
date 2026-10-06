import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:meta/meta.dart';
import 'package:sqlite3/common.dart';
import 'package:sqlite3/sqlite3.dart';

import '../keys/key_provider.dart';
import '../keys/passphrase_key.dart';
import '../keys/principal_hash.dart' show hex;
import 'database_files.dart';

/// A label as `PrincipalLabels` makes it: 64 lowercase hex characters.
final _label = RegExp(r'^[0-9a-f]{64}$');

/// The subdirectory of the app's directory that this package owns. Every file
/// it writes lives here and nowhere else, so erasing it never touches a file
/// the app keeps beside it.
const _subdirectory = 'forge_client_offline';

/// The file inside a salt sidecar directory that holds the salt.
const _saltFile = 'salt';

/// Every file name this package writes into its subdirectory: a database and
/// its journal siblings. A second guard: only entries inside the package's
/// own subdirectory are ever considered.
final _ownedFile = RegExp(r'^[0-9a-f]{64}\.(db|db-journal|db-wal|db-shm)$');

/// Every directory name this package writes into its subdirectory: a salt
/// sidecar, and the temporary directory a sidecar is published through.
final _ownedDirectory = RegExp(r'^[0-9a-f]{64}\.salt(\.tmp-[0-9a-f]{16})?$');

/// Suffixes SQLite adds to a database's path for its journal files.
const _journalSuffixes = ['-journal', '-wal', '-shm'];

/// The native [DatabaseFiles]: one file per principal, named by [labels], in
/// the `forge_client_offline` subdirectory of [directory].
///
/// Pass the app's support directory, for example
/// `(await getApplicationSupportDirectory()).path`, and the `KeystoreKeys`
/// (or a `PrincipalLabels` over the keystore) as [labels]. The package keeps
/// to its own subdirectory, so files the app keeps in [directory] are never
/// touched. [wasmUri] is the web's and is ignored here.
DatabaseFiles platformDatabaseFiles({
  String? directory,
  Uri? wasmUri,
  PrincipalLabeler? labels,
}) {
  if (directory == null) {
    throw ArgumentError.value(
      directory,
      'directory',
      'encrypted storage needs a directory on this platform, '
          'for example (await getApplicationSupportDirectory()).path',
    );
  }
  if (labels == null) {
    throw ArgumentError.value(
      labels,
      'labels',
      'database files are named by a PrincipalLabeler; pass the KeystoreKeys, '
          'or a PrincipalLabels over the keystore when the key is a passphrase',
    );
  }
  return NativeDatabaseFiles(directory, labels: labels);
}

/// The native [PassphraseSaltStore]: a [FilePassphraseSaltStore] beside the
/// databases in the `forge_client_offline` subdirectory of [directory]. Give
/// `passphraseKey` this store and give `encryptedSqliteStorage` the same
/// [directory].
PassphraseSaltStore platformPassphraseSaltStore({String? directory}) {
  if (directory == null) {
    throw ArgumentError.value(
      directory,
      'directory',
      'the passphrase salt lives beside the database; pass the same directory',
    );
  }
  return FilePassphraseSaltStore(directory);
}

/// Where [principal]'s database file lives for the app directory
/// [directory]: `<directory>/forge_client_offline/<label>.db`, named by the
/// principal's label from [labels], never by the principal.
Future<String> nativeDatabasePath(
  String directory,
  PrincipalLabeler labels,
  String principal,
) async => _databasePath(directory, await _labelOf(labels, principal));

/// The subdirectory of [directory] that holds every file of the package.
String nativeStoreDirectory(String directory) =>
    '$directory${Platform.pathSeparator}$_subdirectory';

String _databasePath(String directory, String label) =>
    '${nativeStoreDirectory(directory)}${Platform.pathSeparator}$label.db';

/// The sidecar directory of [label]: `<label>.salt/`, holding `salt`.
String _saltPath(String directory, String label) =>
    '${nativeStoreDirectory(directory)}${Platform.pathSeparator}$label.salt';

/// [labels]' label for [principal], refused unless it is the 64 lowercase hex
/// characters `PrincipalLabels` makes. Anything else could escape the
/// directory, and [NativeDatabaseFiles.deleteAll] could not recognise it.
Future<String> _labelOf(PrincipalLabeler labels, String principal) async {
  final label = await labels.principalLabel(principal);
  _checkLabel(label);
  return label;
}

void _checkLabel(String label) {
  if (!_label.hasMatch(label)) {
    throw ArgumentError.value(
      label,
      'label',
      'a principal label is 64 lowercase hex characters',
    );
  }
}

/// [DatabaseFiles] on a native file system.
///
/// Each principal's database is `<label>.db` in the `forge_client_offline`
/// subdirectory of [directory], with SQLite's `-journal`, `-wal` and `-shm`
/// siblings and, for a passphrase key, the `<label>.salt` sidecar a
/// [FilePassphraseSaltStore] writes. Nothing is ever written to [directory]
/// itself. Calls for one principal must not overlap;
/// `EncryptedSqliteStorage` serializes them.
final class NativeDatabaseFiles implements DatabaseFiles {
  /// Keeps databases in the `forge_client_offline` subdirectory of
  /// [directory], creating it on first open, and names them with [labels].
  NativeDatabaseFiles(this.directory, {required this._labels});

  /// The app directory whose `forge_client_offline` subdirectory holds the
  /// database files.
  final String directory;

  final PrincipalLabeler _labels;
  final Map<String, Database> _open = {};

  @override
  Future<CommonDatabase> open(String principal) async {
    await close(principal);
    final path = await nativeDatabasePath(directory, _labels, principal);
    await Directory(nativeStoreDirectory(directory)).create(recursive: true);
    final db = sqlite3.open(path);
    _open[principal] = db;
    return db;
  }

  @override
  String kind(String principal) => 'file';

  /// Nothing to do: SQLite's default `synchronous = FULL` makes a native
  /// commit durable before it returns.
  @override
  Future<void> afterWrite(String principal) => Future<void>.value();

  @override
  Future<void> close(String principal) async {
    _open.remove(principal)?.close();
  }

  /// Deletes the database, its journals and the salt sidecar.
  ///
  /// For a passphrase key `EncryptedSqliteStorage.destroy` has usually
  /// removed the sidecar already: it deletes the key first, and
  /// `PassphraseKey.delete` deletes the salt, which is the passphrase's half
  /// of the crypto-shred. A destroy interrupted after that point leaves a
  /// database whose salt is gone; no passphrase can open it, and opening it
  /// fails closed with `KeyUnavailable` until the destroy is retried (or
  /// `resetOfflineData` runs), which finishes deleting it. Deleting the
  /// sidecar here as well covers key providers that leave it behind.
  @override
  Future<void> delete(String principal) async {
    await close(principal);
    final label = await _labelOf(_labels, principal);
    final path = _databasePath(directory, label);

    await _deleteIfPresent(File(path));
    for (final suffix in _journalSuffixes) {
      await _deleteIfPresent(File('$path$suffix'));
    }
    await FilePassphraseSaltStore(directory).delete(label);
  }

  @override
  Future<void> deleteAll() async {
    for (final principal in [..._open.keys]) {
      await close(principal);
    }

    // Only the package's own subdirectory, never the app's directory, and
    // never through a link: a `forge_client_offline` that is a symlink (or
    // anything but a real directory) is not the package's and is left alone.
    final path = nativeStoreDirectory(directory);
    final type = await FileSystemEntity.type(path, followLinks: false);
    if (type != FileSystemEntityType.directory) return;

    final dir = Directory(path);
    await for (final entity in dir.list(followLinks: false)) {
      final name = entity.path.split(Platform.pathSeparator).last;
      if (entity is File && _ownedFile.hasMatch(name)) {
        await _deleteIfPresent(entity);
      } else if (entity is Directory && _ownedDirectory.hasMatch(name)) {
        await _deleteTree(entity);
      }
    }
    try {
      await dir.delete();
    } on FileSystemException {
      // Something that is not the package's is still in there: keep it.
    }
  }
}

/// A [PassphraseSaltStore] kept as a plain-text sidecar beside the
/// `<label>.db` database, in the `forge_client_offline` subdirectory of
/// [directory]. The sidecar is a directory, `<label>.salt/`, holding the
/// 16-byte salt in a file named `salt`.
///
/// The salt is not secret (see [PassphraseSaltStore]). It is born with the
/// database: `passphraseKey` stores it just before the database is first
/// created, and the storage deletes it when the database is destroyed.
///
/// A sidecar is published atomically and exclusively. [put] writes the salt
/// into a fresh temporary directory, `<label>.salt.tmp-<random>/salt`, syncs
/// it, and renames that directory onto `<label>.salt`. Renaming a directory
/// onto a directory that already holds a salt fails on every platform, so of
/// any number of creators, in any process and however long one of them
/// stalls, exactly one publishes and the rest find its salt. A sidecar is
/// therefore either a whole salt or absent. A crash leaves at most a
/// temporary directory, which [get] ignores and [delete] removes.
///
/// Instances over the same directory are equal, so `passphraseKey` shares one
/// in-flight salt attempt between them.
final class FilePassphraseSaltStore implements PassphraseSaltStore {
  /// Keeps sidecars in the `forge_client_offline` subdirectory of
  /// [directory].
  FilePassphraseSaltStore(this.directory, {Random? random})
    : _random = random ?? Random.secure();

  /// The app directory whose `forge_client_offline` subdirectory holds the
  /// databases and their sidecars.
  final String directory;

  final Random _random;

  /// Runs after the salt is written to its temporary directory and before
  /// that directory is published, so a test can stall a creator there.
  @visibleForTesting
  Future<void> Function()? beforePublish;

  /// [directory] made absolute and normalized: what equality compares.
  String get _identity {
    final path = Directory(directory).absolute.uri.normalizePath().toFilePath();
    return path.length > 1 && path.endsWith(Platform.pathSeparator)
        ? path.substring(0, path.length - 1)
        : path;
  }

  @override
  bool operator ==(Object other) =>
      other is FilePassphraseSaltStore && other._identity == _identity;

  @override
  int get hashCode => _identity.hashCode;

  @override
  Future<Uint8List?> get(String label) async {
    _checkLabel(label);
    final file = File(_saltFileOf(label));
    try {
      return await file.readAsBytes();
    } on PathNotFoundException {
      return null;
    }
  }

  /// Whether `<label>.db` exists beside the sidecar.
  @override
  Future<bool> hasData(String label) async {
    _checkLabel(label);
    return File(_databasePath(directory, label)).exists();
  }

  /// Publishes [salt] for [label] unless a salt is published already. When
  /// one is, whether it was there before or another creator got there first,
  /// it is kept and this returns without changing it: read the stored salt
  /// back, as `passphraseKey` does, rather than assume [salt] was stored.
  @override
  Future<void> put(String label, Uint8List salt) async {
    _checkLabel(label);
    await Directory(nativeStoreDirectory(directory)).create(recursive: true);

    final suffix = hex(List<int>.generate(8, (_) => _random.nextInt(256)));
    final temp = Directory('${_saltPath(directory, label)}.tmp-$suffix');
    try {
      await temp.create();
      await File('${temp.path}${Platform.pathSeparator}$_saltFile')
          .writeAsBytes(salt, flush: true);
      await beforePublish?.call();
      await _publish(label, temp);
    } finally {
      await _deleteTree(temp);
    }
  }

  /// Renames [temp] onto the sidecar. A sidecar holding a salt makes the
  /// rename fail, and that salt stands. An empty sidecar directory (left by a
  /// delete that stopped between the salt file and its directory) holds no
  /// salt: it is removed, which fails if a salt lands in it meanwhile, and
  /// the rename is tried once more.
  Future<void> _publish(String label, Directory temp) async {
    final target = Directory(_saltPath(directory, label));
    for (var attempt = 0; ; attempt++) {
      try {
        await temp.rename(target.path);
        return;
      } on FileSystemException {
        if (await File(_saltFileOf(label)).exists()) return;
        if (attempt > 0) rethrow;
        try {
          await target.delete();
        } on FileSystemException {
          // Not empty after all, or already gone: the retry decides.
        }
      }
    }
  }

  /// Removes the sidecar and any temporary directory a creator left for
  /// [label].
  @override
  Future<void> delete(String label) async {
    _checkLabel(label);
    await _deleteTree(Directory(_saltPath(directory, label)));

    final store = nativeStoreDirectory(directory);
    final type = await FileSystemEntity.type(store, followLinks: false);
    if (type != FileSystemEntityType.directory) return;
    await for (final entity in Directory(store).list(followLinks: false)) {
      final name = entity.path.split(Platform.pathSeparator).last;
      if (entity is Directory &&
          name.startsWith('$label.salt.tmp-') &&
          _ownedDirectory.hasMatch(name)) {
        await _deleteTree(entity);
      }
    }
  }

  String _saltFileOf(String label) =>
      '${_saltPath(directory, label)}${Platform.pathSeparator}$_saltFile';
}

Future<void> _deleteIfPresent(File file) async {
  try {
    await file.delete();
  } on PathNotFoundException {
    // Already gone, which is what was asked for.
  }
}

/// Deletes [dir] and everything in it, without following links.
Future<void> _deleteTree(Directory dir) async {
  try {
    await dir.delete(recursive: true);
  } on PathNotFoundException {
    // Already gone, which is what was asked for.
  }
}
