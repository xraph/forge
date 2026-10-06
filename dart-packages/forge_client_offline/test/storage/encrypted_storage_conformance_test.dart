@TestOn('vm')
library;

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:forge_client_offline/src/storage/database_files_native.dart'
    show NativeDatabaseFiles;

import '../support/memory_secret_store.dart';
import 'storage_conformance.dart';

void main() {
  // The full adapter, destroy cases included: nothing is skipped.
  storageConformance(() {
    final dir = Directory.systemTemp.createTempSync('forge_encrypted_');
    final keys = keystoreKeys(store: MemorySecretStore());
    final storage = EncryptedSqliteStorage(
      keys: keys,
      files: NativeDatabaseFiles(dir.path, labels: keys),
    );
    addTearDown(() async {
      // Revokes every handle a case left open and closes its connection.
      await storage.resetOfflineData();
      if (dir.existsSync()) dir.deleteSync(recursive: true);
    });
    return storage;
  });
}
