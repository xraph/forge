@TestOn('vm')
library;

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

void main() {
  test('off the web the default is the platform keystore', () {
    expect(defaultKeys(), isA<KeystoreKeys>());
  });

  test('webCryptoKeys is refused off the web', () {
    expect(webCryptoKeys, throwsUnsupportedError);
  });
}
