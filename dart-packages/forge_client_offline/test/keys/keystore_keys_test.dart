import 'dart:convert';

import 'package:flutter/services.dart' show PlatformException;
import 'package:flutter_secure_storage/flutter_secure_storage.dart'
    show AndroidOptions, FlutterSecureStorage, IOSOptions, MacOsOptions;
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

import '../support/memory_secret_store.dart';

const _saltEntry = 'forge_client_offline.salt';
const _keyPrefix = 'forge_client_offline.key.';

Iterable<String> _keyEntries(MemorySecretStore secrets) =>
    secrets.values.keys.where((name) => name.startsWith(_keyPrefix));

void main() {
  late MemorySecretStore secrets;
  late KeystoreKeys keys;

  setUp(() {
    secrets = MemorySecretStore();
    keys = keystoreKeys(store: secrets);
  });

  test(
    'creates a 256-bit key on first use and returns the same key after',
    () async {
      final first = await keys.obtain('alice');
      final second = await keys.obtain('alice');

      expect(first.bytes, hasLength(32));
      expect(first.created, isTrue);
      expect(second.created, isFalse);
      expect(second.bytes, first.bytes);
      expect(first.resetOnMismatch, isTrue);
    },
  );

  test('keys are per principal', () async {
    final alice = await keys.obtain('alice');
    final bob = await keys.obtain('bob');

    expect(bob.bytes, isNot(alice.bytes));
  });

  test('concurrent first calls agree on one key', () async {
    final both = await Future.wait([
      keys.obtain('alice'),
      keys.obtain('alice'),
    ]);

    expect(both[0].bytes, both[1].bytes);
    expect(_keyEntries(secrets), hasLength(1));
    expect(secrets.writes, 2, reason: 'one salt and one key');
  });

  test('many concurrent first calls create at most one entry', () async {
    final all = await Future.wait([
      for (var i = 0; i < 20; i++) keys.obtain('alice'),
    ]);

    expect(all.map((key) => base64Encode(key.bytes)).toSet(), hasLength(1));
    expect(secrets.writes, 2, reason: 'one salt and one key');
    expect(_keyEntries(secrets), hasLength(1));
  });

  test(
    'concurrent first calls for different principals share one salt',
    () async {
      final both = await Future.wait([
        keys.obtain('alice'),
        keys.obtain('bob'),
      ]);

      expect(
        secrets.values.keys.where((name) => name == _saltEntry),
        hasLength(1),
      );
      expect(_keyEntries(secrets), hasLength(2));

      // A new provider over the same store must find both keys under the one salt.
      final again = keystoreKeys(store: secrets);
      final alice = await again.obtain('alice');
      final bob = await again.obtain('bob');
      expect(alice.created, isFalse);
      expect(bob.created, isFalse);
      expect(alice.bytes, both[0].bytes);
      expect(bob.bytes, both[1].bytes);
    },
  );

  test('a failed call does not poison the next one', () async {
    secrets.failReadsWith = StateError('keychain locked');
    await expectLater(keys.obtain('alice'), throwsA(isA<KeyUnavailable>()));

    secrets.failReadsWith = null;
    final key = await keys.obtain('alice');

    expect(key.created, isTrue);
  });

  test(
    'a call that waits on itself would hang, so a call must finish',
    () async {
      // Regression: whenComplete(() => map.remove(k)) returns the map's own
      // future, so obtain awaited itself forever.
      final key = await keys
          .obtain('alice')
          .timeout(const Duration(seconds: 5));

      expect(key.bytes, hasLength(32));
    },
    timeout: const Timeout(Duration(seconds: 10)),
  );

  test('delete forgets the key, so the next call creates a new one', () async {
    final before = await keys.obtain('alice');
    await keys.delete('alice');
    final after = await keys.obtain('alice');

    expect(after.created, isTrue);
    expect(after.bytes, isNot(before.bytes));
  });

  test('delete leaves other principals and the install salt alone', () async {
    await keys.obtain('alice');
    final bob = await keys.obtain('bob');
    final salt = secrets.values[_saltEntry];

    await keys.delete('alice');

    expect(secrets.values[_saltEntry], salt);
    expect(_keyEntries(secrets), hasLength(1));
    expect((await keys.obtain('bob')).bytes, bob.bytes);
  });

  test(
    'a key store that cannot be read raises KeyUnavailable and creates nothing',
    () async {
      secrets.failReadsWith = StateError('keychain locked before first unlock');

      await expectLater(keys.obtain('alice'), throwsA(isA<KeyUnavailable>()));
      expect(secrets.values, isEmpty);
      expect(secrets.writes, 0);
    },
  );

  test('an unreadable key never becomes a new key over the existing one', () async {
    // Android with resetOnError true wipes the store and reads null here. The
    // provider must see a thrown error, fail closed and leave the entry alone.
    final original = await keys.obtain('alice');
    final entries = Map<String, String>.of(secrets.values);
    final writesBefore = secrets.writes;
    secrets.failReadsWith = PlatformException(
      code: 'KeyStore',
      message: 'operation failed',
    );
    secrets.failReadsFor = (name) => name.startsWith(_keyPrefix);

    await expectLater(keys.obtain('alice'), throwsA(isA<KeyUnavailable>()));

    expect(secrets.values, entries);
    expect(secrets.writes, writesBefore);

    secrets.failReadsWith = null;
    final recovered = await keystoreKeys(store: secrets).obtain('alice');
    expect(recovered.created, isFalse);
    expect(recovered.bytes, original.bytes);
  });

  test('an unreadable install salt fails closed like any other key', () async {
    await keys.obtain('alice');
    final entries = Map<String, String>.of(secrets.values);
    secrets.failReadsWith = StateError('keychain locked');
    secrets.failReadsFor = (name) => name == _saltEntry;
    PrincipalLabels.forgetCachedSalts(); // a new process, with nothing cached

    await expectLater(
      keystoreKeys(store: secrets).obtain('alice'),
      throwsA(isA<KeyUnavailable>()),
    );

    expect(secrets.values, entries);
  });

  test(
    'a malformed install salt fails closed instead of being replaced',
    () async {
      await keys.obtain('alice');
      secrets.values[_saltEntry] = base64Encode([1, 2, 3]);
      final entries = Map<String, String>.of(secrets.values);
      PrincipalLabels.forgetCachedSalts(); // a new process, with nothing cached

      await expectLater(
        keystoreKeys(store: secrets).obtain('alice'),
        throwsA(isA<KeyUnavailable>()),
      );

      expect(secrets.values, entries);
    },
  );

  test('a store that cannot be written raises KeyUnavailable', () async {
    secrets.failWritesWith = StateError('keychain full');

    await expectLater(keys.obtain('alice'), throwsA(isA<KeyUnavailable>()));
  });

  test('a malformed stored key is replaced with a fresh one', () async {
    await keys.obtain('alice');
    final name = _keyEntries(secrets).single;
    secrets.values[name] = base64Encode([1, 2, 3]);

    final replaced = await keys.obtain('alice');

    expect(replaced.created, isTrue);
    expect(replaced.bytes, hasLength(32));
  });

  group('entry names', () {
    test('never contain the principal and are not its plain SHA-256', () async {
      await keys.obtain('alice@example.com');

      for (final name in secrets.values.keys) {
        expect(name, isNot(contains('alice')));
        expect(name, isNot(contains('example')));
      }
      expect(
        _keyEntries(secrets).single,
        isNot(contains(await principalHash('alice@example.com'))),
      );
    });

    test('are stable within an install', () async {
      await keys.obtain('alice');
      final names = secrets.values.keys.toSet();

      final again = keystoreKeys(store: secrets);
      await again.obtain('alice');

      expect(secrets.values.keys.toSet(), names);
    });

    test(
      'differ across installs, so one dictionary does not fit every device',
      () async {
        final other = MemorySecretStore();
        await keys.obtain('alice@example.com');
        await keystoreKeys(store: other).obtain('alice@example.com');

        expect(_keyEntries(other).single, isNot(_keyEntries(secrets).single));
      },
    );
  });

  group('principal labels', () {
    test('are 64 lowercase hex characters, safe as a file name', () async {
      final label = await keys.principalLabel('alice@example.com/../x');

      expect(label, matches(RegExp(r'^[0-9a-f]{64}$')));
    });

    test('are stable within an install and distinct per principal', () async {
      final alice = await keys.principalLabel('alice');

      expect(await keys.principalLabel('alice'), alice);
      expect(await keystoreKeys(store: secrets).principalLabel('alice'), alice);
      expect(await keys.principalLabel('bob'), isNot(alice));
    });

    test('differ across installs for the same principal', () async {
      final a = await keys.principalLabel('alice');
      final b = await keystoreKeys(store: MemorySecretStore())
          .principalLabel('alice');

      expect(b, isNot(a));
    });

    test('are not the plain SHA-256 of the principal', () async {
      expect(
        await keys.principalLabel('alice'),
        isNot(await principalHash('alice')),
      );
    });

    test('use a 32-byte random salt kept in the store', () async {
      await keys.principalLabel('alice');

      expect(base64Decode(secrets.values[_saltEntry]!), hasLength(32));
    });

    test(
      'match the key entry name, so files and entries share one label',
      () async {
        await keys.obtain('alice');

        expect(
          _keyEntries(secrets).single,
          '$_keyPrefix${await keys.principalLabel('alice')}',
        );
      },
    );

    test('cannot be made when the salt is unreadable', () async {
      secrets.failReadsWith = StateError('keychain locked');

      await expectLater(
        keys.principalLabel('alice'),
        throwsA(isA<KeyUnavailable>()),
      );
      expect(secrets.values, isEmpty);
    });
  });

  group('secureStorageFor', () {
    FlutterSecureStorage storage({
      bool presence = false,
      bool dataProtection = true,
    }) => secureStorageFor(
      requireUserPresence: presence,
      useDataProtectionKeychain: dataProtection,
    );

    test('never lets Android wipe the store on a Keystore error', () {
      for (final presence in [false, true]) {
        expect(
          storage(presence: presence).aOptions.toMap()['resetOnError'],
          'false',
          reason: 'requireUserPresence: $presence',
        );
      }
    });

    test('keeps every entry in a namespace of its own', () {
      for (final presence in [false, true]) {
        final s = storage(presence: presence);
        const reason = 'the app must not be able to wipe or collide with these';

        expect(
          s.aOptions.toMap()['storageNamespace'],
          'forge_client_offline',
          reason: reason,
        );
        expect(
          s.iOptions.toMap()['accountName'],
          'forge_client_offline',
          reason: reason,
        );
        expect(
          s.mOptions.toMap()['accountName'],
          'forge_client_offline',
          reason: reason,
        );
      }
    });

    test('never syncs the key to iCloud', () {
      expect(storage().iOptions.toMap()['synchronizable'], 'false');
      expect(storage().mOptions.toMap()['synchronizable'], 'false');
    });

    test(
      'keeps the key off other devices and behind a passcode when asked',
      () {
        final open = storage();
        final guarded = storage(presence: true, dataProtection: false);

        expect(
          open.iOptions.toMap()['accessibility'],
          'first_unlock_this_device',
        );
        expect(
          open.mOptions.toMap()['accessibility'],
          'first_unlock_this_device',
        );
        expect(guarded.iOptions.toMap()['accessibility'], 'passcode');
        expect(guarded.mOptions.toMap()['accessibility'], 'passcode');
        expect(
          guarded.iOptions.toMap()['accessControlFlags'],
          contains('userPresence'),
        );
        expect(
          guarded.mOptions.toMap()['accessControlFlags'],
          contains('userPresence'),
        );
        expect(guarded.aOptions.toMap()['enforceBiometrics'], 'true');
      },
    );

    test('macOS uses the data protection keychain unless told not to', () {
      expect(storage().mOptions.toMap()['usesDataProtectionKeychain'], 'true');
      expect(
        storage(dataProtection: false).mOptions
            .toMap()['usesDataProtectionKeychain'],
        'false',
      );
    });

    test(
      'two wrappers over the keystore are one store, so they share a salt',
      () {
        expect(
          FlutterSecretStore(storage()),
          FlutterSecretStore(storage(presence: true)),
        );
      },
    );

    test('wrappers over different native stores are not equal', () {
      FlutterSecretStore wrap({
        AndroidOptions a = const AndroidOptions(),
        IOSOptions i = const IOSOptions(),
        MacOsOptions m = const MacOsOptions(),
      }) => FlutterSecretStore(
        FlutterSecureStorage(aOptions: a, iOptions: i, mOptions: m),
      );
      final base = wrap();

      expect(base, wrap());
      expect(base.hashCode, wrap().hashCode);
      expect(
        base,
        isNot(wrap(m: const MacOsOptions(usesDataProtectionKeychain: false))),
        reason: 'the login keychain and the data protection keychain differ',
      );
      expect(
        FlutterSecretStore(storage(dataProtection: true)),
        isNot(FlutterSecretStore(storage(dataProtection: false))),
      );
      expect(
        base,
        isNot(wrap(a: const AndroidOptions(storageNamespace: 'other'))),
      );
      expect(base, isNot(wrap(i: const IOSOptions(accountName: 'other'))));
      expect(base, isNot(wrap(m: const MacOsOptions(accountName: 'other'))));
      expect(base, isNot(wrap(i: const IOSOptions(groupId: 'group'))));
      expect(base, isNot(wrap(m: const MacOsOptions(synchronizable: true))));
      expect(
        base,
        isNot(wrap(a: const AndroidOptions(preferencesKeyPrefix: 'p'))),
      );
    });
  });
}
