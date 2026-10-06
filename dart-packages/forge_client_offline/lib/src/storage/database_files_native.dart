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

/// The subdirectory of the app's directory that this package owns. Every file
/// it writes lives here and nowhere else, so erasing it never touches a file
/// the app keeps beside it.
const _subdirectory = 'forge_client_offline';

/// How old an empty salt sidecar must be before it is taken for the leftover
/// of a crashed [FilePassphraseSaltStore.put] rather than a creator still at
/// work. A live creator holds the empty placeholder for one rename.
const _stalePlaceholderAge = Duration(seconds: 2);

/// Every file name this package writes into its subdirectory: a database, its
/// journal siblings, a passphrase salt sidecar, and the temporary file a
/// sidecar is written through. A second guard: only files inside the
/// package's own subdirectory are ever considered.
final _ownedFile = RegExp(
  r'^[0-9a-f]{64}\.(db|db-journal|db-wal|db-shm|salt|salt\.[0-9a-f]{16}\.tmp)$',
);

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

    // Only the package's own subdirectory, never the app's directory.
    final dir = Directory(nativeStoreDirectory(directory));
    if (!await dir.exists()) return;
    await for (final entity in dir.list(followLinks: false)) {
      if (entity is! File) continue;
      final name = entity.path.split(Platform.pathSeparator).last;
      if (_ownedFile.hasMatch(name)) await _deleteIfPresent(entity);
    }
    try {
      await dir.delete();
    } on FileSystemException {
      // Something that is not the package's is still in there: keep it.
    }
  }
}

/// A [PassphraseSaltStore] kept as a plain-text sidecar `<label>.salt` beside
/// the `<label>.db` database, in the `forge_client_offline` subdirectory of
/// [directory].
///
/// The salt is not secret (see [PassphraseSaltStore]). It is born with the
/// database: `passphraseKey` stores it just before the database is first
/// created, and the storage deletes it when the database is destroyed.
///
/// [put] is create-exclusive: it never replaces a sidecar that exists, so two
/// creators racing on a new database cannot overwrite each other's salt, and
/// the loser fails instead. The salt is written to a temporary file and
/// renamed over an empty placeholder created exclusively first, so a reader
/// sees no salt, an empty sidecar, or the whole salt, never part of one.
///
/// If the process dies between the placeholder and the rename, an empty
/// sidecar is left with no database. While no database exists, an empty
/// sidecar older than a couple of seconds is that leftover: [get] reports it
/// as absent and [put] replaces it. The replacement stays exclusive: the
/// leftover is first renamed away (only one claimant can), and the new salt
/// still goes through an exclusive create. A younger empty sidecar belongs to
/// a creator still at work, and [put] refuses as for any existing salt. Next
/// to an existing database an empty sidecar is never replaced; the salt is
/// lost and `passphraseKey` fails closed.
final class FilePassphraseSaltStore implements PassphraseSaltStore {
  /// Keeps sidecars in the `forge_client_offline` subdirectory of
  /// [directory].
  FilePassphraseSaltStore(this.directory, {Random? random})
    : _random = random ?? Random.secure();

  /// The app directory whose `forge_client_offline` subdirectory holds the
  /// databases and their sidecars.
  final String directory;

  final Random _random;

  @override
  Future<Uint8List?> get(String label) async {
    _checkLabel(label);
    final file = File(_saltPath(directory, label));
    if (!await file.exists()) return null;
    final bytes = await file.readAsBytes();
    if (bytes.isEmpty && await _isStalePlaceholder(label, file)) return null;
    return bytes;
  }

  /// Whether `<label>.db` exists beside the sidecar.
  @override
  Future<bool> hasData(String label) async {
    _checkLabel(label);
    return File(_databasePath(directory, label)).exists();
  }

  /// Stores [salt] for [label] unless a sidecar exists already, in which case
  /// it throws [StateError] and changes nothing. The one exception is the
  /// empty leftover of a crashed [put], described on the class.
  @override
  Future<void> put(String label, Uint8List salt) async {
    _checkLabel(label);
    await Directory(nativeStoreDirectory(directory)).create(recursive: true);

    final target = File(_saltPath(directory, label));
    final temp = File(_tempPath(target));
    await temp.writeAsBytes(salt, flush: true);

    if (!await _createExclusive(target) &&
        !(await _claimStalePlaceholder(label, target) &&
            await _createExclusive(target))) {
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

  String _tempPath(File target) {
    final suffix = hex(List<int>.generate(8, (_) => _random.nextInt(256)));
    return '${target.path}.$suffix.tmp';
  }

  /// Creates [file] empty, or returns false when it exists.
  static Future<bool> _createExclusive(File file) async {
    try {
      await file.create(exclusive: true);
      return true;
    } on FileSystemException {
      return false;
    }
  }

  /// Whether [file] is the empty, old leftover of a crashed [put] with no
  /// database beside it.
  Future<bool> _isStalePlaceholder(String label, File file) async {
    if (await hasData(label)) return false;
    final stat = await file.stat();
    return stat.type == FileSystemEntityType.file &&
        stat.size == 0 &&
        DateTime.now().difference(stat.modified) >= _stalePlaceholderAge;
  }

  /// Moves a stale empty sidecar at [target] out of the way, so an exclusive
  /// create can replace it. Returns false, with [target] as it was, when what
  /// is there is a real salt or a creator's fresh placeholder.
  Future<bool> _claimStalePlaceholder(String label, File target) async {
    if (!await _isStalePlaceholder(label, target)) return false;

    // The rename is the claim: of several claimants only one moves the file,
    // and the rest go on to the exclusive create, which picks one winner.
    final File claimed;
    try {
      claimed = await target.rename(_tempPath(target));
    } on FileSystemException {
      return true;
    }

    // Another claimant may have replaced the leftover between the check and
    // the rename; what was moved is then theirs, and goes back.
    if (await _isStaleFile(claimed)) {
      await _deleteIfPresent(claimed);
      return true;
    }
    try {
      await claimed.rename(target.path);
    } on FileSystemException {
      await _deleteIfPresent(claimed);
    }
    return false;
  }

  static Future<bool> _isStaleFile(File file) async {
    final stat = await file.stat();
    return stat.size == 0 &&
        DateTime.now().difference(stat.modified) >= _stalePlaceholderAge;
  }
}

Future<void> _deleteIfPresent(File file) async {
  try {
    await file.delete();
  } on PathNotFoundException {
    // Already gone, which is what was asked for.
  }
}
