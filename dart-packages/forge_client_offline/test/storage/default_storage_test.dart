@TestOn('vm')
library;

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

void main() {
  test('encryptedStorage is encrypted SQLite over the platform keystore', () {
    expect(
      encryptedStorage(directory: '/tmp/forge_offline_default'),
      isA<EncryptedSqliteStorage>(),
    );
  });

  test('encryptedStorage needs a directory on native platforms', () {
    expect(encryptedStorage, throwsArgumentError);
  });
}
