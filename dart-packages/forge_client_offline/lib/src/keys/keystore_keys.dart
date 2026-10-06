import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import 'key_provider.dart';
import 'principal_labels.dart';

/// The namespace this package keeps its entries in, apart from the app's own
/// use of flutter_secure_storage.
const _namespace = 'forge_client_offline';

/// Keys held in the platform keystore through flutter_secure_storage: the
/// Keychain on iOS and macOS, the Keystore on Android. Each principal gets a
/// random 256-bit key on first use.
///
/// Entries are named by [PrincipalLabels] (an HMAC of the principal under a
/// per-install random salt), never by the principal or its plain hash.
///
/// Without [requireUserPresence], Apple platforms store the key as
/// `first_unlock_this_device`: readable in the background after the first
/// unlock, so the outbox can replay, and never restored to another device. A
/// database restored without its key is started fresh rather than read.
///
/// With [requireUserPresence], reading the key needs biometrics or the device
/// passcode: `passcode` accessibility plus the `userPresence` access control
/// flag on Apple platforms, `AndroidOptions.biometric(enforceBiometrics: true)`
/// on Android. Background replay then waits until the app is opened.
///
/// [useDataProtectionKeychain] applies to macOS only. The data protection
/// keychain needs the `keychain-access-groups` entitlement and a signed app;
/// pass false to use the login keychain, as the package's own macOS
/// integration test does.
///
/// [store] replaces the platform keystore, for tests.
KeystoreKeys keystoreKeys({
  bool requireUserPresence = false,
  bool useDataProtectionKeychain = true,
  SecretStore? store,
}) => KeystoreKeys(
  store ??
      FlutterSecretStore(
        secureStorageFor(
          requireUserPresence: requireUserPresence,
          useDataProtectionKeychain: useDataProtectionKeychain,
        ),
      ),
);

/// The flutter_secure_storage configuration [keystoreKeys] uses.
///
/// Android is configured with `resetOnError: false`. The plugin's default is
/// true, and on a Keystore failure (a restored backup, a changed lock screen, a
/// corrupt master key) it then deletes every entry and reads null. That would
/// turn "the key is unavailable" into "there is no key", and a new key would
/// be minted over a database that still holds queued writes. With false the
/// plugin throws instead, [KeystoreKeys] raises [KeyUnavailable], and the
/// database file is left alone.
///
/// Every entry lives in this package's own namespace: `accountName` on Apple
/// platforms and `storageNamespace` on Android, both `forge_client_offline`.
/// The app's own `FlutterSecureStorage()` (whose Android default is
/// `resetOnError: true`), or a logout `deleteAll()`, then cannot wipe the salt
/// and keys, and the Android cipher markers do not collide.
FlutterSecureStorage secureStorageFor({
  required bool requireUserPresence,
  required bool useDataProtectionKeychain,
}) {
  final accessibility = requireUserPresence
      ? KeychainAccessibility.passcode
      : KeychainAccessibility.first_unlock_this_device;
  final flags = requireUserPresence
      ? const [AccessControlFlag.userPresence]
      : const <AccessControlFlag>[];

  return FlutterSecureStorage(
    aOptions: requireUserPresence
        ? const AndroidOptions.biometric(
            resetOnError: false,
            storageNamespace: _namespace,
            enforceBiometrics: true,
            biometricPromptTitle: 'Unlock offline data',
          )
        : const AndroidOptions(
            resetOnError: false,
            storageNamespace: _namespace,
          ),
    iOptions: IOSOptions(
      accountName: _namespace,
      accessibility: accessibility,
      accessControlFlags: flags,
    ),
    mOptions: MacOsOptions(
      accountName: _namespace,
      accessibility: accessibility,
      accessControlFlags: flags,
      usesDataProtectionKeychain: useDataProtectionKeychain,
    ),
  );
}

/// A [SecretStore] over [FlutterSecureStorage].
final class FlutterSecretStore implements SecretStore {
  /// Wraps [storage].
  FlutterSecretStore(this.storage);

  /// The underlying plugin instance.
  final FlutterSecureStorage storage;

  /// Which physical store [storage] reaches: the Android namespace and the
  /// Apple account names. Two wrappers over the same one are interchangeable.
  String get _identity =>
      '${storage.aOptions.storageNamespace}/${storage.iOptions.accountName}/'
      '${storage.mOptions.accountName}';

  @override
  bool operator ==(Object other) =>
      other is FlutterSecretStore && other._identity == _identity;

  @override
  int get hashCode => _identity.hashCode;

  @override
  Future<String?> read(String name) => storage.read(key: name);

  @override
  Future<void> write(String name, String value) =>
      storage.write(key: name, value: value);

  @override
  Future<void> delete(String name) => storage.delete(key: name);
}

/// The [KeyProvider] [keystoreKeys] returns. It is also the install's
/// [PrincipalLabeler], so database file names can share its labels.
final class KeystoreKeys implements KeyProvider, PrincipalLabeler {
  /// Keeps keys in [store], generating them with [random].
  KeystoreKeys(SecretStore store, {Random? random})
    : _store = store,
      _random = random ?? Random.secure(),
      _labels = PrincipalLabels(store, random: random);

  final SecretStore _store;
  final Random _random;
  final PrincipalLabels _labels;

  /// Key attempts by store and principal, one per isolate, so two providers
  /// built over one store cannot each create a key for the same principal.
  static final Map<(SecretStore, String), Future<DatabaseKey>> _inflight = {};

  @override
  Future<String> principalLabel(String principal) => _labels.label(principal);

  @override
  Future<DatabaseKey> obtain(String principal) {
    final id = (_store, principal);
    final running = _inflight[id];
    if (running != null) return running;

    // A block body: an arrow would return remove's result, which is this very
    // future, and the call would wait on itself forever.
    final future = _obtain(principal).whenComplete(() {
      _inflight.remove(id);
    });
    _inflight[id] = future;
    return future;
  }

  Future<DatabaseKey> _obtain(String principal) async {
    final name = await _entryName(principal);

    final String? stored;
    try {
      stored = await _store.read(name);
    } on Object catch (error) {
      throw KeyUnavailable(principal, error);
    }

    if (stored != null) {
      final decoded = _decode(stored);
      if (decoded != null) {
        return DatabaseKey(decoded, created: false, resetOnMismatch: true);
      }
    }

    final bytes = Uint8List.fromList(
      List<int>.generate(32, (_) => _random.nextInt(256)),
    );
    try {
      await _store.write(name, base64Encode(bytes));
    } on Object catch (error) {
      throw KeyUnavailable(principal, error);
    }

    return DatabaseKey(bytes, created: true, resetOnMismatch: true);
  }

  @override
  Future<void> delete(String principal) async {
    await _store.delete(await _entryName(principal));
  }

  Future<String> _entryName(String principal) async =>
      'forge_client_offline.key.${await _labels.label(principal)}';

  static Uint8List? _decode(String stored) {
    try {
      final bytes = base64Decode(stored);
      return bytes.length == 32 ? bytes : null;
    } on FormatException {
      return null;
    }
  }
}
