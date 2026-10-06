import 'package:flutter/foundation.dart' show kIsWeb;

import 'key_provider.dart';
import 'keystore_keys.dart';
import 'web_crypto_keys.dart';

/// The platform's default [KeyProvider]: [keystoreKeys] on native platforms.
/// Web: not yet supported ([webCryptoKeys] is a placeholder there). On Linux and Windows the keystore has no
/// namespace to isolate this package's entries; see [keystoreKeys].
KeyProvider defaultKeys({bool requireUserPresence = false}) => kIsWeb
    ? webCryptoKeys()
    : keystoreKeys(requireUserPresence: requireUserPresence);
