import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:sqlite3/common.dart';
import 'package:sqlite3/sqlite3.dart';

import '../keys/key_provider.dart';
import '../keys/passphrase_key.dart';
import '../keys/principal_hash.dart' show hex;
import 'database_files.dart';

/// A label as `PrincipalLabels` makes it: 64 lowercase hex characters.
final _label = RegExp(r'^[0-9a-f]{64}$');

/// Every file name this package writes into a storage directory: a database,
/// its journal siblings, a passphrase salt sidecar, and the temporary file a
/// sidecar is written through.
final _ownedFile = RegExp(
  r'^[0-9a-f]{64}\.(db|db-journal|db-wal|db-shm|salt|salt\.[0-9a-f]{16}\.tmp)$',
);

/// Suffixes SQLite adds to a database's path for its journal files.
const _journalSuffixes = ['-journal', '-wal', '-shm'];

/// The native [DatabaseFiles]: one file per principal in [directory], named
/// by [labels].
///
/// Pass the app's support directory, for example
/// `(await getApplicationSupportDirectory()).path`, and the `KeystoreKeys`
/// (or a `PrincipalLabels` over the keystore) as [labels]. [wasmUri] is the
/// web's and is ignored here.
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
/// databases in [directory]. Give `passphraseKey` this store and give
/// `encryptedSqliteStorage` the same [directory].
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

/// Where [principal]'s database file lives inside [directory]: `<label>.db`,
/// named by the principal's label from [labels], never by the principal.
Future<String> nativeDatabasePath(
  String directory,
  PrincipalLabeler labels,
  String principal,
) async => _databasePath(directory, await _labelOf(labels, principal));

String _databasePath(String directory, String label) =>
    '$directory${Platform.pathSeparator}$label.db';

String _saltPath(String directory, String label) =>
    '$directory${Platform.pathSeparator}$label.salt';

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
/// Each principal's database is `<label>.db` in [directory], with SQLite's
/// `-journal`, `-wal` and `-shm` siblings and, for a passphrase key, the
/// `<label>.salt` sidecar a [FilePassphraseSaltStore] writes. Calls for one
/// principal must not overlap; `EncryptedSqliteStorage` serializes them.
final class NativeDatabaseFiles implements DatabaseFiles {
  /// Keeps databases in [directory], creating it on first open, and names
  /// them with [labels].
  NativeDatabaseFiles(this.directory, {required this._labels});

  /// The directory holding the database files.
  final String directory;

  final PrincipalLabeler _labels;
  final Map<String, Database> _open = {};

  @override
  Future<CommonDatabase> open(String principal) async {
    await close(principal);
    final path = await nativeDatabasePath(directory, _labels, principal);
    await Directory(directory).create(recursive: true);
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

  /// Deletes the database and its journals first and the salt sidecar last.
  /// Interrupted here, the leftover is a sidecar, which is not secret and
  /// which the next database for the label adopts, rather than a database
  /// whose salt is gone.
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

    final dir = Directory(directory);
    if (!await dir.exists()) return;
    await for (final entity in dir.list(followLinks: false)) {
      if (entity is! File) continue;
      final name = entity.path.split(Platform.pathSeparator).last;
      if (_ownedFile.hasMatch(name)) await _deleteIfPresent(entity);
    }
  }
}

/// A [PassphraseSaltStore] kept as a plain-text sidecar `<label>.salt` beside
/// the `<label>.db` database in [directory].
///
/// The salt is not secret (see [PassphraseSaltStore]). It is born with the
/// database: `passphraseKey` stores it just before the database is first
/// created, and the storage deletes it when the database is destroyed.
///
/// [put] is create-exclusive: it never replaces a sidecar that exists, so two
/// creators racing on a new database cannot overwrite each other's salt, and
/// the loser fails instead. The salt is written to a temporary file and
/// renamed over an empty placeholder created exclusively first, so a reader
/// sees no salt, an empty sidecar, or the whole salt, never part of one. If
/// the process dies between the placeholder and the rename, the empty sidecar
/// is left behind; `passphraseKey` then refuses it as malformed rather than
/// guess, and destroying the principal or `resetOfflineData` clears it.
final class FilePassphraseSaltStore implements PassphraseSaltStore {
  /// Keeps sidecars in [directory].
  FilePassphraseSaltStore(this.directory, {Random? random})
    : _random = random ?? Random.secure();

  /// The directory holding the databases and their sidecars.
  final String directory;

  final Random _random;

  @override
  Future<Uint8List?> get(String label) async {
    _checkLabel(label);
    final file = File(_saltPath(directory, label));
    if (!await file.exists()) return null;
    return file.readAsBytes();
  }

  /// Whether `<label>.db` exists beside the sidecar.
  @override
  Future<bool> hasData(String label) async {
    _checkLabel(label);
    return File(_databasePath(directory, label)).exists();
  }

  /// Stores [salt] for [label] unless a sidecar exists already, in which case
  /// it throws [StateError] and changes nothing.
  @override
  Future<void> put(String label, Uint8List salt) async {
    _checkLabel(label);
    await Directory(directory).create(recursive: true);

    final target = File(_saltPath(directory, label));
    final suffix = hex(List<int>.generate(8, (_) => _random.nextInt(256)));
    final temp = File('${target.path}.$suffix.tmp');
    await temp.writeAsBytes(salt, flush: true);

    try {
      await target.create(exclusive: true);
    } on FileSystemException {
      await _deleteIfPresent(temp);
      throw StateError(
        'a passphrase salt already exists for this database and is never '
        'replaced',
      );
    }

    try {
      await temp.rename(target.path);
    } on Object {
      await _deleteIfPresent(temp);
      await _deleteIfPresent(target);
      rethrow;
    }
  }

  @override
  Future<void> delete(String label) async {
    _checkLabel(label);
    await _deleteIfPresent(File(_saltPath(directory, label)));
  }
}

Future<void> _deleteIfPresent(File file) async {
  try {
    await file.delete();
  } on PathNotFoundException {
    // Already gone, which is what was asked for.
  }
}
