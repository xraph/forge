import 'package:forge_client_offline/forge_client_offline.dart';

/// An in-memory SecretStore that can be told to fail reads or writes.
final class MemorySecretStore implements SecretStore {
  final Map<String, String> values = {};
  int writes = 0;
  Object? failReadsWith;

  /// When set, only reads of names this accepts fail; otherwise every read does.
  bool Function(String name)? failReadsFor;

  Object? failWritesWith;

  @override
  Future<String?> read(String name) async {
    final failure = failReadsWith;
    if (failure != null && (failReadsFor?.call(name) ?? true)) throw failure;
    return values[name];
  }

  @override
  Future<void> write(String name, String value) async {
    final failure = failWritesWith;
    if (failure != null) throw failure;
    writes++;
    values[name] = value;
  }

  @override
  Future<void> delete(String name) async {
    values.remove(name);
  }
}
