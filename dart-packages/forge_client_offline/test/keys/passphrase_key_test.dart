import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

KeyProvider _fast(String secret) =>
    passphraseKey(() => secret, memoryKiB: 64, iterations: 1);

void main() {
  test('the same secret and principal derive the same key', () async {
    final a = await _fast('correct horse').obtain('alice');
    final b = await _fast('correct horse').obtain('alice');

    expect(a.bytes, hasLength(32));
    expect(b.bytes, a.bytes);
    expect(a.created, isFalse);
    expect(a.resetOnMismatch, isFalse);
  });

  test('the principal and the secret both change the key', () async {
    final alice = await _fast('correct horse').obtain('alice');
    final bob = await _fast('correct horse').obtain('bob');
    final other = await _fast('battery staple').obtain('alice');

    expect(bob.bytes, isNot(alice.bytes));
    expect(other.bytes, isNot(alice.bytes));
  });

  test('an empty secret is refused', () async {
    await expectLater(_fast('').obtain('alice'), throwsArgumentError);
  });

  test('the secret is read on every call and never kept', () async {
    var secret = 'first';
    final keys = passphraseKey(() => secret, memoryKiB: 64, iterations: 1);

    final a = await keys.obtain('alice');
    secret = 'second';
    final b = await keys.obtain('alice');

    expect(b.bytes, isNot(a.bytes));
  });

  test('the key does not depend on any install, so it opens the same database after a reinstall', () async {
    // A per-install salt would make the key unrecoverable once the keystore is
    // wiped, which is the one case a passphrase is chosen to survive.
    final first = await _fast('correct horse').obtain('alice');
    final reinstalled = await _fast('correct horse').obtain('alice');

    expect(reinstalled.bytes, first.bytes);
  });

  test('delete stores nothing and completes', () async {
    await _fast('x').delete('alice');
  });
}
