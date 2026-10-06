@TestOn('vm')
library;

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import '../support/sqlite_session_storage.dart';
import 'storage_conformance.dart';

void main() {
  storageConformance(
    () {
      final dir = Directory.systemTemp.createTempSync('forge_conformance_');
      final storage = SqliteSessionStorage(dir);
      addTearDown(() async {
        await storage.closeAll();
        if (dir.existsSync()) dir.deleteSync(recursive: true);
      });
      return storage;
    },
    skipDestroy:
        'destroy revokes open sessions before shredding, which the adapter '
        'owns: Task 8 runs these cases against EncryptedSqliteStorage',
  );
}
