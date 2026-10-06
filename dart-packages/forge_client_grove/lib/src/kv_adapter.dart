import 'package:forge_client/forge_client.dart';
import 'package:grove_crdt/grove_crdt.dart';

/// Presents a forge_client [KeyValueStore] as grove_crdt's [ReplicaKeyValue].
///
/// Both interfaces share one contract, so every call delegates. In
/// particular [batch] keeps the store's rules: the builder runs synchronously
/// and must not read or await, the batch closes when the builder returns (a
/// later write throws [StateError]), and a builder that throws applies
/// nothing.
final class ForgeKeyValueAdapter implements ReplicaKeyValue {
  /// Wraps [store].
  ForgeKeyValueAdapter(this.store);

  /// The wrapped namespace.
  final KeyValueStore store;

  @override
  Future<String?> get(String key) => store.get(key);

  @override
  Future<void> put(String key, String value) => store.put(key, value);

  @override
  Future<void> delete(String key) => store.delete(key);

  @override
  Future<Map<String, String>> scan(String prefix) => store.scan(prefix);

  @override
  Future<void> batch(void Function(ReplicaKeyValueBatch batch) build) =>
      store.batch((b) => build(_Batch(b)));
}

final class _Batch implements ReplicaKeyValueBatch {
  _Batch(this._b);

  final KeyValueBatch _b;

  @override
  void put(String key, String value) => _b.put(key, value);

  @override
  void delete(String key) => _b.delete(key);
}
