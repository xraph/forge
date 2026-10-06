import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';

import 'key_provider.dart';

/// Derives each principal's key from a user secret with Argon2id. The secret
/// is read through [secret] on every [KeyProvider.obtain] and never stored,
/// which is the stronger option on the web, where there is no keystore.
///
/// The Argon2id salt is derived from the principal alone, so the same secret
/// gives different principals different keys. It deliberately does not use the
/// per-install salt that [PrincipalLabels] keeps for file and entry names. An
/// Argon2 salt only has to be unique, not secret, and a per-install salt would
/// make the key depend on the install: after a reinstall or on a second device
/// the same passphrase would derive a different key and the database would be
/// unreadable, which is the case a passphrase is chosen to survive. The salt is
/// never written anywhere, so it names nothing on disk. Defaults follow OWASP's
/// Argon2id baseline (19 MiB, 2 iterations, parallelism 1).
///
/// Nothing is stored, so [KeyProvider.delete] has nothing to forget: signing
/// out with a passphrase key relies on deleting the database file, and an
/// interrupted delete leaves data readable to anyone with the passphrase.
///
/// This provider has no store, so it cannot name files either. Pair it with a
/// [PrincipalLabels] over a keystore for database file names.
KeyProvider passphraseKey(
  String Function() secret, {
  int memoryKiB = 19456,
  int iterations = 2,
  int parallelism = 1,
}) => PassphraseKey(
  secret,
  memoryKiB: memoryKiB,
  iterations: iterations,
  parallelism: parallelism,
);

/// The [KeyProvider] [passphraseKey] returns.
final class PassphraseKey implements KeyProvider {
  /// Derives keys from [secret] with the given Argon2id cost.
  PassphraseKey(
    this._secret, {
    required int memoryKiB,
    required int iterations,
    required int parallelism,
  }) : _algorithm = Argon2id(
         parallelism: parallelism,
         memory: memoryKiB,
         iterations: iterations,
         hashLength: 32,
       );

  final String Function() _secret;
  final Argon2id _algorithm;

  @override
  Future<DatabaseKey> obtain(String principal) async {
    final value = _secret();
    if (value.isEmpty) {
      throw ArgumentError.value('', 'secret', 'a passphrase must not be empty');
    }

    final saltSource = utf8.encode(
      'forge_client_offline/argon2id/v1\u0000$principal',
    );
    final salt = (await Sha256().hash(saltSource)).bytes.sublist(0, 16);
    final key = await _algorithm.deriveKey(
      secretKey: SecretKey(utf8.encode(value)),
      nonce: salt,
    );

    return DatabaseKey(
      Uint8List.fromList(await key.extractBytes()),
      created: false,
      resetOnMismatch: false,
    );
  }

  /// Completes at once: a passphrase key is never stored.
  @override
  Future<void> delete(String principal) => Future<void>.value();
}
