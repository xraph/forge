import 'dart:convert';

import 'package:cryptography/cryptography.dart';

/// Returns the lowercase hex SHA-256 of [principal].
///
/// This is unsalted, so it can be dictionary-attacked when principals are
/// guessable (an e-mail address, say). Do not use it to name anything that is
/// stored on the device. [PrincipalLabeler.principalLabel] is the salted
/// replacement for database file names and keystore entries.
Future<String> principalHash(String principal) async {
  final hash = await Sha256().hash(utf8.encode(principal));
  return hex(hash.bytes);
}

/// Lowercase hex encoding of [bytes].
String hex(List<int> bytes) {
  final out = StringBuffer();
  for (final b in bytes) {
    out.write(b.toRadixString(16).padLeft(2, '0'));
  }
  return out.toString();
}
