import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:forge_client_offline/src/keys/key_provider.dart';
import 'package:forge_client_offline/src/keys/principal_hash.dart' show hex;

/// A KeyProvider that always returns the same key, derived from [seed], and
/// names files with labels that are the same in every isolate and process.
///
/// It imports no Flutter library, so a plain `dart` process can use it.
final class FixedKeys implements KeyProvider, PrincipalLabeler {
  FixedKeys(int seed, {this.resetOnMismatch = false})
    : bytes = Uint8List.fromList(
        List<int>.generate(32, (i) => (seed * 31 + i) & 0xff),
      );

  final Uint8List bytes;
  final bool resetOnMismatch;
  final List<String> deleted = [];

  @override
  Future<DatabaseKey> obtain(String principal) async =>
      DatabaseKey(bytes, created: false, resetOnMismatch: resetOnMismatch);

  @override
  Future<void> delete(String principal) async => deleted.add(principal);

  /// An HMAC under a fixed test salt: 64 lowercase hex characters, like a
  /// PrincipalLabels label, but deterministic.
  @override
  Future<String> principalLabel(String principal) async {
    final mac = await Hmac.sha256().calculateMac(
      utf8.encode(principal),
      secretKey: SecretKey(utf8.encode('forge_client_offline fixed keys')),
    );
    return hex(mac.bytes);
  }
}
