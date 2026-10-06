import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

import '../support/memory_secret_store.dart';

const _saltEntry = 'forge_client_offline.salt';

/// A new store holding the same entries, standing in for a later run of the app
/// that reads the keystore for the first time.
MemorySecretStore _reopened(MemorySecretStore store) =>
    MemorySecretStore()..values.addAll(store.values);

void main() {
  late MemorySecretStore secrets;

  setUp(() => secrets = MemorySecretStore());

  test('two instances on one store make one salt and one label', () async {
    final a = PrincipalLabels(secrets);
    final b = PrincipalLabels(secrets);

    final labels = await Future.wait([
      a.label('alice'),
      b.label('alice'),
      a.label('bob'),
      b.label('bob'),
    ]);

    expect(labels[0], labels[1]);
    expect(labels[2], labels[3]);
    expect(labels[0], isNot(labels[2]));
    expect(
      secrets.writes,
      1,
      reason: 'the salt is written once, by whichever instance got there first',
    );
  });

  test('two providers on one store agree on one key per principal', () async {
    final a = keystoreKeys(store: secrets);
    final b = keystoreKeys(store: secrets);

    final both = await Future.wait([a.obtain('alice'), b.obtain('alice')]);

    expect(both[0].bytes, both[1].bytes);
    expect(
      secrets.values.keys.where(
        (n) => n.startsWith('forge_client_offline.key.'),
      ),
      hasLength(1),
      reason: 'a second key entry means a second salt orphaned a database',
    );
    expect(
      (await keystoreKeys(store: _reopened(secrets)).obtain('alice')).created,
      isFalse,
    );
  });

  test('a salt another writer stored first is the one used', () async {
    final theirs = List<int>.generate(32, (i) => 200 - i);
    secrets.rewriteWrites = (name, value) =>
        name == _saltEntry ? base64Encode(theirs) : value;

    final first = await PrincipalLabels(secrets).label('alice');

    expect(base64Decode(secrets.values[_saltEntry]!), theirs);
    expect(
      await PrincipalLabels(_reopened(secrets)).label('alice'),
      first,
      reason: 'the label must come from the stored salt, not the generated one',
    );
  });

  test('a salt the store cannot give back fails closed', () async {
    secrets.rewriteWrites = (name, value) => 'not base64 of 32 bytes';

    await expectLater(
      PrincipalLabels(secrets).label('alice'),
      throwsA(isA<KeyUnavailable>()),
    );
  });
}
