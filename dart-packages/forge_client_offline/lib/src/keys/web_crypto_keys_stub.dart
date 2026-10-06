import 'key_provider.dart';

/// Keys wrapped by a non-extractable WebCrypto key kept in IndexedDB. Only
/// available on the web; elsewhere this throws [UnsupportedError].
KeyProvider webCryptoKeys({
  String databaseName = 'forge_client_offline_keys',
}) => throw UnsupportedError(
  'webCryptoKeys is only available on the web; use keystoreKeys on this platform',
);
