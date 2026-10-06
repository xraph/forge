import '../keys/key_provider.dart';
import '../keys/passphrase_key.dart';
import 'database_files.dart';

/// The platform's [DatabaseFiles]. This build has neither dart:io nor a
/// browser, so there is none.
DatabaseFiles platformDatabaseFiles({
  String? directory,
  Uri? wasmUri,
  PrincipalLabeler? labels,
}) => throw UnsupportedError('encrypted storage needs dart:io or a browser');

/// The platform's [PassphraseSaltStore]. This build has neither dart:io nor a
/// browser, so there is none.
PassphraseSaltStore platformPassphraseSaltStore({String? directory}) =>
    throw UnsupportedError('encrypted storage needs dart:io or a browser');
