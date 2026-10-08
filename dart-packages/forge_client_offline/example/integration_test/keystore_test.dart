import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:integration_test/integration_test.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  final principal = 'integration-${DateTime.now().microsecondsSinceEpoch}';
  KeystoreKeys keys() => keystoreKeys(useDataProtectionKeychain: false);

  // The install salt is created on first use and outlives every principal's
  // key, so removing the principal's key is not enough to leave the login
  // keychain as it was found. This clears the package's own namespace and
  // nothing else.
  tearDownAll(() => keys().deleteAll());

  testWidgets(
    'the Keychain keeps one key per principal and forgets it on delete',
    (tester) async {
      addTearDown(() => keys().delete(principal));

      final first = await keys().obtain(principal);
      final second = await keys().obtain(principal);

      expect(first.created, isTrue);
      expect(second.created, isFalse);
      expect(second.bytes, first.bytes);

      await keys().delete(principal);
      final third = await keys().obtain(principal);

      expect(third.created, isTrue);
      expect(third.bytes, isNot(first.bytes));
    },
  );

  testWidgets(
    'encrypted storage round-trips through the Keychain and crypto-shreds',
    (tester) async {
      final dir = await Directory.systemTemp.createTemp('forge_offline_it_');
      final storage = encryptedSqliteStorage(keys: keys(), directory: dir.path);
      addTearDown(() => dir.delete(recursive: true));
      addTearDown(() => storage.destroy(principal));

      final session = await storage.open(principal);
      await session.enqueue(
        PendingMutationRecord(
          id: 'm1',
          operationId: 'op_update_order',
          argsJson: '{"v":1,"seq":1,"requestHeaders":{},"args":{}}',
          idempotencyKey: 'key-m1',
          createdAt: DateTime.utc(2026, 10, 4),
          stateJson: '{"kind":"queued"}',
        ),
      );
      await session.close();

      final again = await storage.open(principal);
      expect((await again.readOutbox()).map((r) => r.id), ['m1']);

      await storage.destroy(principal);

      expect(dir.listSync().where((e) => e.path.endsWith('.db')), isEmpty);
      expect(
        (await keys().obtain(principal)).created,
        isTrue,
        reason: 'destroy removed the Keychain item',
      );
    },
  );
}
