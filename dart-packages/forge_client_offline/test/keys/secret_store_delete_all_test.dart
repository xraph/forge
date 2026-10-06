import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

import '../support/memory_secret_store.dart';

const _channel = MethodChannel('plugins.it_nomads.com/flutter_secure_storage');

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late List<MethodCall> calls;
  late Map<String, String> entries;

  setUp(() {
    calls = [];
    entries = {
      'forge_client_offline.salt': 's',
      'forge_client_offline.key.abc': 'k',
      'app_token': 'the app',
      'forge_client_offline_lookalike': 'the app',
    };
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(_channel, (call) async {
          calls.add(call);
          final args = call.arguments as Map<Object?, Object?>;
          switch (call.method) {
            case 'readAll':
              return entries;
            case 'delete':
              entries.remove(args['key']);
              return null;
            case 'deleteAll':
              return null;
          }
          return null;
        });
  });

  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(_channel, null);
  });

  FlutterSecretStore store() => FlutterSecretStore(
    secureStorageFor(
      requireUserPresence: false,
      useDataProtectionKeychain: true,
    ),
  );

  Map<Object?, Object?> optionsOf(MethodCall call) =>
      (call.arguments as Map<Object?, Object?>)['options']!
          as Map<Object?, Object?>;

  test(
    'on Android it clears the namespace and may reset its Keystore key',
    () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;

      await store().deleteAll();

      expect(calls.map((c) => c.method), ['deleteAll']);
      expect(
        optionsOf(calls.single)['storageNamespace'],
        'forge_client_offline',
      );
      expect(optionsOf(calls.single)['resetOnError'], 'true');
    },
  );

  test('on Apple platforms it clears the package account only', () async {
    for (final platform in [TargetPlatform.iOS, TargetPlatform.macOS]) {
      calls.clear();
      debugDefaultTargetPlatformOverride = platform;

      await store().deleteAll();

      expect(calls.map((c) => c.method), ['deleteAll'], reason: '$platform');
      expect(optionsOf(calls.single)['accountName'], 'forge_client_offline');
    }
  });

  test(
    'on Linux and Windows it deletes only the package-prefixed entries',
    () async {
      for (final platform in [TargetPlatform.linux, TargetPlatform.windows]) {
        calls.clear();
        entries
          ..['forge_client_offline.salt'] = 's'
          ..['forge_client_offline.key.abc'] = 'k';
        debugDefaultTargetPlatformOverride = platform;

        await store().deleteAll();

        expect(calls.map((c) => c.method), isNot(contains('deleteAll')));
        expect(entries.keys, {
          'app_token',
          'forge_client_offline_lookalike',
        }, reason: '$platform');
      }
    },
  );

  test('KeystoreKeys.deleteAll removes every key and the install salt, and '
      'labels then come from a new salt', () async {
    final secrets = MemorySecretStore();
    final keys = keystoreKeys(store: secrets);
    await keys.obtain('alice');
    await keys.obtain('bob');
    final before = await keys.principalLabel('alice');

    await keys.deleteAll();

    expect(secrets.values, isEmpty);
    expect(await keys.principalLabel('alice'), isNot(before));
    expect((await keys.obtain('alice')).created, isTrue);
  });
}
