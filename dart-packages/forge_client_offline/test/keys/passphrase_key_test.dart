import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

import '../support/memory_secret_store.dart';

/// A salt store that counts writes and can be told to fail or to keep another
/// writer's value.
final class _CountingSaltStore implements PassphraseSaltStore {
  final MemoryPassphraseSaltStore inner = MemoryPassphraseSaltStore();
  int puts = 0;
  Object? failGetsWith;
  Uint8List? keepInstead;

  @override
  Future<Uint8List?> get(String label) async {
    final failure = failGetsWith;
    if (failure != null) throw failure;
    return inner.get(label);
  }

  @override
  Future<bool> hasData(String label) => inner.hasData(label);

  @override
  Future<void> put(String label, Uint8List salt) {
    puts++;
    return inner.put(label, keepInstead ?? salt);
  }

  @override
  Future<void> delete(String label) => inner.delete(label);
}

KeyProvider _fast(
  String secret, {
  required PassphraseSaltStore salts,
  PrincipalLabeler? labels,
}) => passphraseKey(
  () => secret,
  salts: salts,
  labels: labels ?? PrincipalLabels(MemorySecretStore()),
  memoryKiB: 64,
  iterations: 1,
  allowWeakParametersForTesting: true,
);

void main() {
  late MemorySecretStore keystore;
  late PrincipalLabels labels;
  late _CountingSaltStore salts;

  setUp(() {
    keystore = MemorySecretStore();
    labels = PrincipalLabels(keystore);
    salts = _CountingSaltStore();
  });

  KeyProvider keys(String secret) =>
      _fast(secret, salts: salts, labels: labels);

  test('the same secret and principal derive the same key', () async {
    final a = await keys('correct horse').obtain('alice');
    final b = await keys('correct horse').obtain('alice');

    expect(a.bytes, hasLength(32));
    expect(b.bytes, a.bytes);
    expect(a.created, isFalse);
    expect(a.resetOnMismatch, isFalse);
  });

  test('the principal and the secret both change the key', () async {
    final alice = await keys('correct horse').obtain('alice');
    final bob = await keys('correct horse').obtain('bob');
    final other = await keys('battery staple').obtain('alice');

    expect(bob.bytes, isNot(alice.bytes));
    expect(other.bytes, isNot(alice.bytes));
  });

  test('an empty secret is refused', () async {
    await expectLater(keys('').obtain('alice'), throwsArgumentError);
  });

  test('the secret is read on every call and never kept', () async {
    var secret = 'first';
    final provider = passphraseKey(
      () => secret,
      salts: salts,
      labels: labels,
      memoryKiB: 64,
      iterations: 1,
      allowWeakParametersForTesting: true,
    );

    final a = await provider.obtain('alice');
    secret = 'second';
    final b = await provider.obtain('alice');

    expect(b.bytes, isNot(a.bytes));
  });

  group('salt', () {
    test(
      'is 16 random bytes kept under the label, never the principal',
      () async {
        await keys('x').obtain('alice@example.com');

        final label = await labels.label('alice@example.com');
        expect(salts.inner.salts.keys, [label]);
        expect(salts.inner.salts[label], hasLength(16));
      },
    );

    test('is random per database, so another install derives another key', () async {
      final here = await keys('correct horse').obtain('alice');
      final there = await _fast(
        'correct horse',
        salts: MemoryPassphraseSaltStore(),
        labels: PrincipalLabels(MemorySecretStore()),
      ).obtain('alice');

      expect(
        there.bytes,
        isNot(here.bytes),
        reason:
            'a table built from the principal and the passphrase must not fit',
      );
    });

    test('differs between principals', () async {
      await keys('x').obtain('alice');
      await keys('x').obtain('bob');

      final stored = salts.inner.salts.values.toList();
      expect(stored, hasLength(2));
      expect(stored[0], isNot(stored[1]));
    });

    test(
      'travels with the database, so the key survives a reinstall',
      () async {
        // The install salt (the keystore) is gone; the sidecar salt is not.
        final first = await keys('correct horse').obtain('alice');
        PrincipalLabels.forgetCachedSalts();
        final fresh = PrincipalLabels(MemorySecretStore());
        // A new install has a new label salt, so the sidecar is found by the
        // caller under whatever label it now has. Carry the entry over to model it.
        final oldLabel = await labels.label('alice');
        salts.inner.salts[await fresh.label('alice')] =
            salts.inner.salts[oldLabel]!;

        final again = await _fast(
          'correct horse',
          salts: salts,
          labels: fresh,
        ).obtain('alice');

        expect(again.bytes, first.bytes);
      },
    );

    test(
      'concurrent first calls create one salt and agree on one key',
      () async {
        final provider = keys('correct horse');

        final all = await Future.wait([
          for (var i = 0; i < 10; i++) provider.obtain('alice'),
        ]);

        expect(salts.puts, 1);
        expect(all.map((k) => k.bytes.join(',')).toSet(), hasLength(1));
      },
    );

    test(
      'a missing salt beside existing data fails closed and writes nothing',
      () async {
        salts.inner.labelsWithData.add(await labels.label('alice'));

        await expectLater(
          keys('correct horse').obtain('alice'),
          throwsA(isA<KeyUnavailable>()),
        );

        expect(salts.puts, 0);
        expect(salts.inner.salts, isEmpty);
      },
    );

    test('a damaged salt fails closed and is not replaced', () async {
      final label = await labels.label('alice');
      salts.inner.salts[label] = Uint8List.fromList([1, 2, 3]);

      await expectLater(
        keys('correct horse').obtain('alice'),
        throwsA(isA<KeyUnavailable>()),
      );

      expect(salts.inner.salts[label], [1, 2, 3]);
      expect(salts.puts, 0);
    });

    test('a salt store that cannot be read raises KeyUnavailable', () async {
      salts.failGetsWith = StateError('disk unreadable');

      await expectLater(
        keys('correct horse').obtain('alice'),
        throwsA(isA<KeyUnavailable>()),
      );

      expect(salts.puts, 0);
    });

    test('a salt another writer stored first is the one used', () async {
      final theirs = Uint8List.fromList(List<int>.generate(16, (i) => i));
      salts.keepInstead = theirs;

      final first = await keys('correct horse').obtain('alice');
      salts.keepInstead = null;
      final again = await keys('correct horse').obtain('alice');

      expect(again.bytes, first.bytes);
      expect(salts.inner.salts.values.single, theirs);
    });

    test(
      'an unavailable keystore stops the key, since the label needs it',
      () async {
        keystore.failReadsWith = StateError('keychain locked');

        await expectLater(
          keys('correct horse').obtain('alice'),
          throwsA(isA<KeyUnavailable>()),
        );

        expect(salts.inner.salts, isEmpty);
      },
    );
  });

  test(
    'delete removes the salt, so the old key cannot be derived again',
    () async {
      final before = await keys('correct horse').obtain('alice');
      await keys('correct horse').delete('alice');
      expect(salts.inner.salts, isEmpty);

      final after = await keys('correct horse').obtain('alice');

      expect(after.bytes, isNot(before.bytes));
    },
  );

  test('delete leaves other principals alone', () async {
    final bob = await keys('x').obtain('bob');
    await keys('x').obtain('alice');

    await keys('x').delete('alice');

    expect((await keys('x').obtain('bob')).bytes, bob.bytes);
  });

  group('parameters', () {
    PassphraseKey build({
      int memoryKiB = PassphraseKey.defaultMemoryKiB,
      int iterations = PassphraseKey.defaultIterations,
      int parallelism = 1,
      bool weak = false,
    }) => PassphraseKey(
      () => 'x',
      salts: salts,
      labels: labels,
      memoryKiB: memoryKiB,
      iterations: iterations,
      parallelism: parallelism,
      allowWeakParametersForTesting: weak,
    );

    test('default to RFC 9106 (64 MiB, 3 passes, 1 lane)', () {
      expect(PassphraseKey.defaultMemoryKiB, 64 * 1024);
      expect(PassphraseKey.defaultIterations, 3);
      expect(PassphraseKey.defaultParallelism, 1);
    });

    test('passphraseKey uses those defaults when none are given', () {
      // Building with no cost arguments must not trip the floor.
      expect(
        () => passphraseKey(() => 'x', salts: salts, labels: labels),
        returnsNormally,
      );
    });

    test('below the floor are refused', () {
      expect(
        () => build(memoryKiB: PassphraseKey.minMemoryKiB - 1),
        throwsArgumentError,
      );
      expect(() => build(iterations: 1), throwsArgumentError);
    });

    test('at the floor are accepted', () {
      expect(
        () => build(
          memoryKiB: PassphraseKey.minMemoryKiB,
          iterations: PassphraseKey.minIterations,
        ),
        returnsNormally,
      );
    });

    test('below the floor are accepted only with the test override', () {
      expect(
        () => build(memoryKiB: 64, iterations: 1, weak: true),
        returnsNormally,
      );
    });

    test('never accept fewer than one lane', () {
      expect(() => build(parallelism: 0, weak: true), throwsArgumentError);
    });
  });
}
