import 'dart:typed_data';

import 'package:sqlite3/common.dart';

import '../keys/principal_hash.dart';

/// The SQLite3MultipleCiphers cipher every database is written with. Its key
/// is 32 bytes, the size of a [DatabaseKey].
const String rawKeyCipher = 'chacha20';

/// Keys [db] with [key] as the cipher key itself, before anything is read.
///
/// sqlite3mc has two ways in. `PRAGMA key` and `PRAGMA hexkey` take a
/// passphrase and derive the cipher key from it with PBKDF2-HMAC-SHA256
/// (64007 rounds for ChaCha20) over a salt kept at the start of the file. A
/// value written `raw:` followed by the key in hex skips that derivation and
/// uses the bytes as they are. Every key this package holds is already 32
/// uniformly random bytes (or an Argon2id output), so a second derivation
/// adds nothing but start-up time, and this uses the raw form.
///
/// sqlite3mc only treats a `raw:` value as raw when its length matches the
/// cipher's key. Anything else silently falls back to being a passphrase, so
/// [key] must be exactly 32 bytes and the cipher is selected explicitly rather
/// than left to the build's default.
///
/// The caller checks first that the SQLite build has a cipher at all: on plain
/// SQLite both pragmas are ignored, and this throws [StateError] rather than
/// leave the database in plaintext. A wrong key is not detected here; the
/// first read fails with `SQLITE_NOTADB`. Every failure is a [StateError] or
/// [ArgumentError] whose text holds no key material: the key pragma's own
/// [SqliteException] is never rethrown, because its text includes the
/// statement and so the key.
void applyRawKey(CommonDatabase db, Uint8List key) {
  if (key.length != 32) {
    throw ArgumentError.value(
      key.length,
      'key',
      'a raw ChaCha20 key is 32 bytes',
    );
  }

  final cipher = db.select("PRAGMA cipher = '$rawKeyCipher'");
  if (cipher.isEmpty || cipher.first.values.first != rawKeyCipher) {
    throw StateError(
      'this SQLite build cannot select the $rawKeyCipher cipher; '
      'refusing to open the database unencrypted',
    );
  }

  // The statement carries the key, and SqliteException.toString() prints the
  // statement, so neither the exception nor its text may escape from here.
  final keyHex = hex(key);
  final ResultSet result;
  try {
    result = db.select("PRAGMA key = 'raw:$keyHex'");
  } on SqliteException catch (error) {
    throw StateError(
      'SQLite3MultipleCiphers refused the key (SQLite code '
      '${error.extendedResultCode}): ${_withoutKey(error.message, keyHex)}',
    );
  }

  final answer = result.isEmpty ? 'no result' : '${result.first.values.first}';
  if (answer != 'ok') {
    throw StateError(
      'SQLite3MultipleCiphers did not accept the key: '
      '${_withoutKey(answer, keyHex)}',
    );
  }
}

/// [text], unless it holds [keyHex] in any case, in which case a placeholder.
/// sqlite3mc's messages do not echo the key today; this keeps it that way.
String _withoutKey(String text, String keyHex) =>
    text.toLowerCase().contains(keyHex.toLowerCase())
    ? '(message withheld: it contained key material)'
    : text;
