import 'dart:async';

import 'package:forge_client/forge_client.dart';

/// Counts what an OfflineClient asks of storage, and logs the order of the
/// session closes and destroys it causes.
final class CountingStorage implements StorageAdapter {
  CountingStorage(this.inner, {List<String>? log}) : log = log ?? [];

  final StorageAdapter inner;
  int snapshotWrites = 0;
  int snapshotReads = 0;
  int closes = 0;

  /// `close <principal>` and `destroy <principal>`, in the order they ran.
  final List<String> log;

  /// Runs before each `readSnapshot` reaches [inner].
  FutureOr<void> Function()? beforeReadSnapshot;

  @override
  Future<StorageSession> open(String principal) async =>
      _CountingSession(this, await inner.open(principal));

  @override
  Future<void> destroy(String principal) async {
    log.add('destroy $principal');
    await inner.destroy(principal);
  }
}

final class _CountingSession implements StorageSession {
  _CountingSession(this._counts, this._inner);

  final CountingStorage _counts;
  final StorageSession _inner;

  @override
  String get principal => _inner.principal;

  @override
  Future<Snapshot?> readSnapshot() async {
    _counts.snapshotReads++;
    await _counts.beforeReadSnapshot?.call();
    return _inner.readSnapshot();
  }

  @override
  Future<void> writeSnapshot(Snapshot snapshot) {
    _counts.snapshotWrites++;
    return _inner.writeSnapshot(snapshot);
  }

  @override
  Future<List<PendingMutationRecord>> readOutbox() => _inner.readOutbox();

  @override
  Future<void> enqueue(PendingMutationRecord record) => _inner.enqueue(record);

  @override
  Future<void> remove(String mutationId) => _inner.remove(mutationId);

  @override
  Future<void> updateState(String mutationId, String stateJson) =>
      _inner.updateState(mutationId, stateJson);

  @override
  KeyValueStore namespace(String name) => _inner.namespace(name);

  @override
  Future<void> close() async {
    _counts.closes++;
    await _inner.close();
    _counts.log.add('close $principal');
  }
}
