import 'package:forge_client_offline/forge_client_offline.dart';

/// An in-memory SecretStore that can be told to fail reads or writes.
final class MemorySecretStore implements SecretStore {
  final Map<String, String> values = {};
  int writes = 0;
  Object? failReadsWith;

  /// When set, only reads of names this accepts fail; otherwise every read does.
  bool Function(String name)? failReadsFor;

  Object? failWritesWith;

  /// When set, a write stores what this returns instead of the value given, as
  /// if another writer's value had landed first.
  String Function(String name, String value)? rewriteWrites;

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
    values[name] = rewriteWrites?.call(name, value) ?? value;
  }

  @override
  Future<void> delete(String name) async {
    values.remove(name);
  }

  /// How many times [deleteAll] ran.
  int deleteAlls = 0;

  @override
  Future<void> deleteAll() async {
    deleteAlls++;
    values.clear();
  }
}
