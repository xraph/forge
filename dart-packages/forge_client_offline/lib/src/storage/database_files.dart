import 'package:sqlite3/common.dart';

export 'database_files_stub.dart'
    if (dart.library.io) 'database_files_native.dart'
    show platformDatabaseFiles, platformPassphraseSaltStore;

/// Where one principal's database lives and how it is opened, flushed and
/// deleted. [platformDatabaseFiles] returns the native or web implementation.
///
/// Files are named by the principal's label (see `PrincipalLabeler`), never
/// by the principal or a plain hash of it.
abstract interface class DatabaseFiles {
  /// Opens (creating if needed) [principal]'s database. Not yet unlocked.
  ///
  /// A database already open for [principal] is closed first, so at most one
  /// connection per principal is open through this object.
  Future<CommonDatabase> open(String principal);

  /// Which file system holds [principal]'s open database.
  String kind(String principal);

  /// Makes completed writes durable where the file system needs telling.
  Future<void> afterWrite(String principal);

  /// Closes [principal]'s database if open.
  Future<void> close(String principal);

  /// Closes and deletes every file of [principal]'s database, including its
  /// journal siblings and its passphrase salt sidecar.
  Future<void> delete(String principal);

  /// Closes every open database and deletes every file this package owns in
  /// its location, for every principal, including principals whose label can
  /// no longer be computed. Files that are not this package's are left alone.
  Future<void> deleteAll();
}
