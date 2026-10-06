import 'package:flutter/foundation.dart' show kIsWeb;

import 'key_provider.dart';
import 'keystore_keys.dart';
import 'web_crypto_keys.dart';

/// The platform's default [KeyProvider]: [webCryptoKeys] on the web,
/// [keystoreKeys] everywhere else.
KeyProvider defaultKeys({bool requireUserPresence = false}) => kIsWeb
    ? webCryptoKeys()
    : keystoreKeys(requireUserPresence: requireUserPresence);
