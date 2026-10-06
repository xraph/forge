import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';

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
/// Use one instance per store in a process. The first call creates the salt,
/// and two instances racing on an empty store could each create one.
final class PrincipalLabels implements PrincipalLabeler {
  /// Keeps the salt in [store], generating it with [random].
  PrincipalLabels(this._store, {Random? random})
    : _random = random ?? Random.secure();

  final SecretStore _store;
  final Random _random;
  Future<Uint8List>? _salt;

  @override
  Future<String> principalLabel(String principal) => label(principal);

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

  /// One shared attempt, so concurrent first calls agree on one salt. A failure
  /// is not remembered: the next call tries the store again.
  Future<Uint8List> _loadSalt() {
    final running = _salt;
    if (running != null) return running;

    final attempt = _readOrCreateSalt();
    _salt = attempt;
    attempt.then<void>(
      (_) {},
      onError: (Object _) {
        if (identical(_salt, attempt)) _salt = null;
      },
    );
    return attempt;
  }

  Future<Uint8List> _readOrCreateSalt() async {
    final String? stored;
    try {
      stored = await _store.read(_saltEntry);
    } on Object catch (error) {
      throw _SaltFailure(error);
    }

    if (stored != null) {
      final decoded = _decode(stored);
      if (decoded == null) {
        throw const _SaltFailure(
          FormatException('the stored install salt is not 32 bytes of base64'),
        );
      }
      return decoded;
    }

    final salt = Uint8List.fromList(
      List<int>.generate(_saltLength, (_) => _random.nextInt(256)),
    );
    try {
      await _store.write(_saltEntry, base64Encode(salt));
    } on Object catch (error) {
      throw _SaltFailure(error);
    }
    return salt;
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
