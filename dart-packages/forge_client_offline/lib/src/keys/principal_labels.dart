import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:meta/meta.dart';

import 'key_provider.dart';
import 'principal_hash.dart';

/// The keystore entry that holds this install's label salt.
const _saltEntry = 'forge_client_offline.salt';

/// Bytes of salt. The salt is a HMAC key, so 32 bytes matches the hash size.
const _saltLength = 32;

/// Domain separation, so a label is never the MAC of some other message.
const _labelContext = 'forge_client_offline/principal-label/v1\u0000';

/// Names a principal's database file and keystore entry without exposing the
/// principal, even when principals are guessable such as e-mail addresses.
///
/// A label is `HMAC-SHA256(salt, principal)` in lowercase hex (64 characters,
/// safe as a file name on every platform, including case-insensitive file
/// systems). The salt is 32 random bytes from a CSPRNG, created on first use
/// and kept in the same [SecretStore] as the database keys. Each install has its
/// own salt, so one dictionary of principals does not fit every device, and
/// the salt never leaves the keystore, so someone holding only the files
/// cannot test a guess.
///
/// The salt is part of the same trust boundary as the keys. If it cannot be
/// read, [label] throws [KeyUnavailable] and writes nothing: a label made from
/// a new salt would name a different file than the existing database, and
/// storage would mistake an unreadable keystore for a new install. A salt that
/// is present but damaged is treated the same way and never replaced.
///
/// Every instance in one isolate that talks to the same store shares one salt
/// future (a static map keyed by the store), so two providers built over one
/// keystore cannot create two salts. After writing a new salt the value is read
/// back and the stored value is used, so if the store holds a different value
/// than the one just written, that value is used.
///
/// That sharing stops at the isolate. The first open of a database, which is
/// when the salt is created, must run on the main isolate. Two isolates racing
/// on an empty keystore each write a salt and the last writer wins. Read-back
/// adopts whatever the store holds at the moment it reads, so the isolate that
/// wrote first can read the other's salt, or its own if it read before the
/// second write landed. The loser can then have named a database file under a
/// label that is no longer current, and that file is orphaned.
final class PrincipalLabels implements PrincipalLabeler, ErasableSecrets {
  /// Keeps the salt in [store], generating it with [random].
  ///
  /// Instances share a salt when their stores are equal (`==`). A [SecretStore]
  /// that is rebuilt on every call but reaches one physical store, like
  /// [FlutterSecretStore], must override `==` and `hashCode` to say so.
  PrincipalLabels(this._store, {Random? random})
    : _random = random ?? Random.secure();

  final SecretStore _store;
  final Random _random;

  /// Salt attempts by store identity, one per isolate.
  static final Map<Object, Future<Uint8List>> _salts = {};

  /// Forgets every cached salt, as a new process would.
  @visibleForTesting
  static void forgetCachedSalts() => _salts.clear();

  @override
  Future<String> principalLabel(String principal) => label(principal);

  /// Removes everything this package keeps in the store, the install salt
  /// included, and forgets the cached salt, so the next label is made from a
  /// new one. Every existing label then names nothing: delete the files they
  /// named as well, as `EncryptedSqliteStorage.resetOfflineData` does.
  @override
  Future<void> deleteAll() async {
    try {
      await _store.deleteAll();
    } finally {
      _salts.remove(_store)?.ignore();
    }
  }

  /// Returns [principal]'s label. See the class comment.
  Future<String> label(String principal) async {
    final Uint8List salt;
    try {
      salt = await _loadSalt();
    } on _SaltFailure catch (failure) {
      throw KeyUnavailable(principal, failure.cause);
    }

    final mac = await Hmac.sha256().calculateMac(
      utf8.encode('$_labelContext$principal'),
      secretKey: SecretKey(salt),
    );
    return hex(mac.bytes);
  }

  /// One shared attempt per store, so concurrent and repeated first calls agree
  /// on one salt. A failure is not remembered: the next call tries again.
  Future<Uint8List> _loadSalt() {
    final running = _salts[_store];
    if (running != null) return running;

    final attempt = _readOrCreateSalt();
    _salts[_store] = attempt;
    attempt.then<void>(
      (_) {},
      onError: (Object _) {
        if (identical(_salts[_store], attempt)) _salts.remove(_store);
      },
    );
    return attempt;
  }

  Future<Uint8List> _readOrCreateSalt() async {
    final existing = await _readSalt();
    if (existing != null) return existing;

    final salt = Uint8List.fromList(
      List<int>.generate(_saltLength, (_) => _random.nextInt(256)),
    );
    try {
      await _store.write(_saltEntry, base64Encode(salt));
    } on Object catch (error) {
      throw _SaltFailure(error);
    }

    // Use what the store now holds, not what was just generated: if another
    // writer's salt landed first, theirs is the one the files are named by.
    final stored = await _readSalt();
    if (stored == null) {
      throw const _SaltFailure(
        FormatException('the install salt was written but cannot be read back'),
      );
    }
    return stored;
  }

  Future<Uint8List?> _readSalt() async {
    final String? stored;
    try {
      stored = await _store.read(_saltEntry);
    } on Object catch (error) {
      throw _SaltFailure(error);
    }
    if (stored == null) return null;

    final decoded = _decode(stored);
    if (decoded == null) {
      throw const _SaltFailure(
        FormatException('the stored install salt is not 32 bytes of base64'),
      );
    }
    return decoded;
  }

  static Uint8List? _decode(String stored) {
    try {
      final bytes = base64Decode(stored);
      return bytes.length == _saltLength ? bytes : null;
    } on FormatException {
      return null;
    }
  }
}

/// A salt read or write that failed, before it knows whose label it was for.
final class _SaltFailure implements Exception {
  const _SaltFailure(this.cause);

  final Object cause;
}
