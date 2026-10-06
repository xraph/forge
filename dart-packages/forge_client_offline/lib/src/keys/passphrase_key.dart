import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';

import 'key_provider.dart';

/// Where a [passphraseKey] keeps each principal's Argon2id salt.
///
/// The salt is not secret, so it can sit in plain text next to the database it
/// protects. What matters is that it is random per principal and per database,
/// so a precomputed table for "this principal plus a constant" is useless. It
/// is addressed by the principal's label (see [PrincipalLabeler]), never by the
/// principal.
///
/// The salt must live and die with the database: written when the database is
/// created, deleted when it is destroyed. [hasData] is how the store says the
/// database exists, so a salt that has gone missing is told apart from a
/// database that was never made.
abstract interface class PassphraseSaltStore {
  /// Returns the salt stored for [label], or null when there is none.
  Future<Uint8List?> get(String label);

  /// Whether data encrypted under a key derived with [label]'s salt already
  /// exists, for example because the database file is there.
  Future<bool> hasData(String label);

  /// Stores [salt] for [label].
  Future<void> put(String label, Uint8List salt);

  /// Removes the salt for [label].
  Future<void> delete(String label);
}

/// A [PassphraseSaltStore] kept in memory, for tests.
final class MemoryPassphraseSaltStore implements PassphraseSaltStore {
  /// The stored salts by label.
  final Map<String, Uint8List> salts = {};

  /// Labels whose data exists, as [hasData] reports.
  final Set<String> labelsWithData = {};

  @override
  Future<Uint8List?> get(String label) async => salts[label];

  @override
  Future<bool> hasData(String label) async => labelsWithData.contains(label);

  @override
  Future<void> put(String label, Uint8List salt) async {
    salts[label] = salt;
  }

  @override
  Future<void> delete(String label) async {
    salts.remove(label);
  }
}

/// Derives each principal's key from a user secret with Argon2id. The secret
/// is read through [secret] on every [KeyProvider.obtain] and never stored,
/// which is the stronger option on the web, where there is no keystore.
///
/// Each principal gets a random 16-byte salt, created on first use and kept in
/// [salts] (a plain-text sidecar next to the database, supplied by the storage
/// layer). The salt is not secret. It exists so that no two databases, on any
/// device or in any app, share a derivation, which rules out a table built in
/// advance from a principal and a known constant. [labels] names the salt, so
/// the principal does not appear on disk.
///
/// A missing salt is created only when [PassphraseSaltStore.hasData] says no
/// database exists yet. If the database exists and its salt does not, the key
/// can never be derived again, so [KeyProvider.obtain] throws [KeyUnavailable]
/// and the file is kept rather than replaced. A salt that is present but not 16
/// bytes is treated the same way and never overwritten.
///
/// The salt deliberately does not come from the install salt that
/// [PrincipalLabels] keeps in the keystore. That salt is per install, so a
/// reinstall or a second device would derive a different key from the same
/// passphrase, which is the case a passphrase is chosen to survive. The salt
/// travels with the database instead.
///
/// Defaults follow RFC 9106's second recommended Argon2id option (64 MiB, 3
/// passes, 1 lane). Parameters below the floor, 19 MiB and 2 passes (OWASP's
/// minimum), are refused with an [ArgumentError], unless
/// [allowWeakParametersForTesting] is set. Only tests should set it.
///
/// [KeyProvider.delete] removes the salt. Without it the passphrase can no
/// longer derive the key, but the salt is not secret and a deleted file can
/// survive on disk, so signing out with a passphrase key still relies on
/// deleting the database file as well.
KeyProvider passphraseKey(
  String Function() secret, {
  required PassphraseSaltStore salts,
  required PrincipalLabeler labels,
  int memoryKiB = PassphraseKey.defaultMemoryKiB,
  int iterations = PassphraseKey.defaultIterations,
  int parallelism = PassphraseKey.defaultParallelism,
  bool allowWeakParametersForTesting = false,
  Random? random,
}) => PassphraseKey(
  secret,
  salts: salts,
  labels: labels,
  memoryKiB: memoryKiB,
  iterations: iterations,
  parallelism: parallelism,
  allowWeakParametersForTesting: allowWeakParametersForTesting,
  random: random,
);

/// The [KeyProvider] [passphraseKey] returns.
final class PassphraseKey implements KeyProvider {
  /// Derives keys from [secret] with the given Argon2id cost. See
  /// [passphraseKey].
  PassphraseKey(
    this._secret, {
    required this._salts,
    required this._labels,
    required int memoryKiB,
    required int iterations,
    required int parallelism,
    bool allowWeakParametersForTesting = false,
    Random? random,
  }) : _random = random ?? Random.secure(),
       _algorithm = _argon2id(
         memoryKiB: memoryKiB,
         iterations: iterations,
         parallelism: parallelism,
         allowWeak: allowWeakParametersForTesting,
       );

  /// RFC 9106 second recommended option: 64 MiB.
  static const defaultMemoryKiB = 65536;

  /// RFC 9106 second recommended option: 3 passes.
  static const defaultIterations = 3;

  /// RFC 9106 second recommended option: 1 lane.
  static const defaultParallelism = 1;

  /// The least memory accepted outside tests: OWASP's Argon2id minimum.
  static const minMemoryKiB = 19456;

  /// The fewest passes accepted outside tests: OWASP's Argon2id minimum.
  static const minIterations = 2;

  static const _saltLength = 16;

  final String Function() _secret;
  final PassphraseSaltStore _salts;
  final PrincipalLabeler _labels;
  final Random _random;
  final Argon2id _algorithm;
  final Map<String, Future<Uint8List>> _inflight = {};

  static Argon2id _argon2id({
    required int memoryKiB,
    required int iterations,
    required int parallelism,
    required bool allowWeak,
  }) {
    if (parallelism < 1) {
      throw ArgumentError.value(
        parallelism,
        'parallelism',
        'must be at least 1',
      );
    }
    if (!allowWeak && memoryKiB < minMemoryKiB) {
      throw ArgumentError.value(
        memoryKiB,
        'memoryKiB',
        'below the floor of $minMemoryKiB KiB',
      );
    }
    if (!allowWeak && iterations < minIterations) {
      throw ArgumentError.value(
        iterations,
        'iterations',
        'below the floor of $minIterations passes',
      );
    }
    return Argon2id(
      parallelism: parallelism,
      memory: memoryKiB,
      iterations: iterations,
      hashLength: 32,
    );
  }

  @override
  Future<DatabaseKey> obtain(String principal) async {
    final value = _secret();
    if (value.isEmpty) {
      throw ArgumentError.value('', 'secret', 'a passphrase must not be empty');
    }

    final label = await _labels.principalLabel(principal);
    final salt = await _saltFor(principal, label);
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

  /// Removes [principal]'s salt. See [passphraseKey].
  @override
  Future<void> delete(String principal) async {
    await _salts.delete(await _labels.principalLabel(principal));
  }

  /// One shared attempt per label, so concurrent first calls agree on one salt.
  Future<Uint8List> _saltFor(String principal, String label) {
    final running = _inflight[label];
    if (running != null) return running;

    // A block body, for the reason KeystoreKeys.obtain has one.
    final future = _readOrCreateSalt(principal, label).whenComplete(() {
      _inflight.remove(label);
    });
    _inflight[label] = future;
    return future;
  }

  Future<Uint8List> _readOrCreateSalt(String principal, String label) async {
    final existing = await _readSalt(principal, label);
    if (existing != null) return existing;

    final bool dataExists;
    try {
      dataExists = await _salts.hasData(label);
    } on Object catch (error) {
      throw KeyUnavailable(principal, error);
    }
    if (dataExists) {
      throw KeyUnavailable(
        principal,
        StateError('the passphrase salt is missing but its database exists'),
      );
    }

    final salt = Uint8List.fromList(
      List<int>.generate(_saltLength, (_) => _random.nextInt(256)),
    );
    try {
      await _salts.put(label, salt);
    } on Object catch (error) {
      throw KeyUnavailable(principal, error);
    }

    // Use what the store holds, as PrincipalLabels does.
    final stored = await _readSalt(principal, label);
    if (stored == null) {
      throw KeyUnavailable(
        principal,
        StateError('the passphrase salt was written but cannot be read back'),
      );
    }
    return stored;
  }

  Future<Uint8List?> _readSalt(String principal, String label) async {
    final Uint8List? stored;
    try {
      stored = await _salts.get(label);
    } on Object catch (error) {
      throw KeyUnavailable(principal, error);
    }
    if (stored != null && stored.length != _saltLength) {
      throw KeyUnavailable(
        principal,
        const FormatException(
          'the stored passphrase salt is not $_saltLength bytes',
        ),
      );
    }
    return stored;
  }
}
