import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../keys/keystore_keys.dart';
import '../keys/web_crypto_keys.dart';
import 'encrypted_storage.dart';

/// The default encrypted storage: [encryptedSqliteStorage] keyed by the
/// platform keystore on native platforms. Web: not yet supported (the web
/// branch reaches the `webCryptoKeys` placeholder, which has no key store).
/// [keystore] replaces the default flutter_secure_storage configuration
/// (`secureStorageFor`), and with it whatever [requireUserPresence] would
/// have set.
///
/// [directory] is required on native platforms; pass
/// `(await getApplicationSupportDirectory()).path`. Build one adapter per
/// directory per isolate and keep it for the life of the app: an adapter
/// serializes its own opens and destroys, and two adapters over one directory
/// do not see each other's.
///
/// Files are named by the keystore's per-install labels, so no
/// `PrincipalLabels` is needed here. To key the database from a user secret
/// instead, build the storage yourself: `encryptedSqliteStorage(keys:
/// passphraseKey(secret, salts: platformPassphraseSaltStore(directory: dir),
/// labels: labels), labels: labels, directory: dir)`, with the same
/// `PrincipalLabels` given to both.
///
/// [onReset] is told when a database is started fresh because its key no
/// longer fits; the adapter's `resets` stream carries the same events, and
/// `OfflineClient.open` listens there.
EncryptedSqliteStorage encryptedStorage({
  String? directory,
  FlutterSecureStorage? keystore,
  bool requireUserPresence = false,
  Uri? wasmUri,
  void Function(StorageReset reset)? onReset,
}) => encryptedSqliteStorage(
  keys: kIsWeb
      ? webCryptoKeys()
      : keystoreKeys(
          requireUserPresence: requireUserPresence,
          store: keystore == null ? null : FlutterSecretStore(keystore),
        ),
  directory: directory,
  wasmUri: wasmUri,
  onReset: onReset,
);
