import 'dart:typed_data';

/// A 256-bit database key for one principal.
final class DatabaseKey {
  /// Wraps [bytes], which must be exactly 32 bytes long.
  DatabaseKey(
    this.bytes, {
    required this.created,
    required this.resetOnMismatch,
  }) {
    if (bytes.length != 32) {
      throw ArgumentError.value(
        bytes.length,
        'bytes',
        'a database key is 32 bytes',
      );
    }
  }

  /// The raw key.
  final Uint8List bytes;

  /// True when this call generated the key because none was stored.
  final bool created;

  /// Whether a database this key cannot decrypt should be deleted and started
  /// fresh. True for keys a store generates (the old key is gone, so the data
  /// is unrecoverable anyway); false for a passphrase, where a mismatch means
  /// the user typed the wrong one and the data must be kept.
  final bool resetOnMismatch;
}

/// Supplies and forgets per-principal database keys.
abstract interface class KeyProvider {
  /// Returns [principal]'s key, creating one when this provider stores keys
  /// and none exists. Throws [KeyUnavailable] when the key store exists but
  /// cannot be read right now.
  Future<DatabaseKey> obtain(String principal);

  /// Forgets [principal]'s key. Once this returns, a database encrypted under
  /// it cannot be read by anyone.
  Future<void> delete(String principal);
}

/// Names a principal's on-disk artefacts (database files, keystore entries)
/// without putting the principal itself on disk.
abstract interface class PrincipalLabeler {
  /// Returns a stable, filename-safe label for [principal], stable for as long
  /// as this install's keystore is. Throws [KeyUnavailable] when the keystore
  /// cannot be read, because a label made without it would name a different
  /// file than the one that already exists.
  Future<String> principalLabel(String principal);
}

/// Thrown when a key store cannot be read right now, for example an iOS
/// keychain item protected until first unlock. Storage fails closed on it and
/// never treats it as a lost key, so the database file is left untouched.
final class KeyUnavailable implements Exception {
  /// Creates the error for [principal], caused by [cause].
  const KeyUnavailable(this.principal, this.cause);

  /// Whose key could not be read.
  final String principal;

  /// The key store's own error.
  final Object cause;

  @override
  String toString() =>
      'KeyUnavailable: the database key could not be read: $cause';
}

/// The minimal secret store [KeyProvider]s are built on.
abstract interface class SecretStore {
  /// Returns the value stored under [name], or null.
  Future<String?> read(String name);

  /// Stores [value] under [name].
  Future<void> write(String name, String value);

  /// Removes [name].
  Future<void> delete(String name);
}
