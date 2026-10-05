/// The storage seam: where a principal's snapshot, outbox and sync replica
/// live. Defined in `forge_client` so the sync seam can name it without
/// depending on `forge_client_offline`, which supplies the encrypted adapter.
library;

import 'dart:collection';

import 'snapshot.dart';

/// Opens and destroys per-principal partitions.
abstract interface class StorageAdapter {
  /// Open [principal]'s partition, creating it when absent.
  Future<StorageSession> open(String principal);

  /// Crypto-shred [principal]'s partition, then delete it. Open sessions for
  /// it stop working.
  Future<void> destroy(String principal);
}

/// One principal's partition, open.
abstract interface class StorageSession {
  /// Whose partition this is.
  String get principal;

  /// The last snapshot written, or null.
  Future<Snapshot?> readSnapshot();

  /// Replace the snapshot.
  Future<void> writeSnapshot(Snapshot snapshot);

  /// Every queued mutation, in enqueue order, failed ones included.
  Future<List<PendingMutationRecord>> readOutbox();

  /// Append [record] to the outbox.
  Future<void> enqueue(PendingMutationRecord record);

  /// Drop a mutation from the outbox. Unknown ids are a no-op.
  Future<void> remove(String mutationId);

  /// Replace a mutation's state (`{"kind":"queued"}`,
  /// `{"kind":"sending","at":ms}` or `{"kind":"failed","failure":{...}}`),
  /// keeping its place in the outbox. Unknown ids throw [StateError].
  Future<void> updateState(String mutationId, String stateJson);

  /// A key-value store private to [name] within this partition. Grove keeps
  /// its replica in one.
  KeyValueStore namespace(String name);

  /// Release the session. The partition is kept.
  Future<void> close();
}

/// One queued mutation, as the outbox persists it.
final class PendingMutationRecord {
  /// A record for mutation [id] of operation [operationId].
  const PendingMutationRecord({
    required this.id,
    required this.operationId,
    required this.argsJson,
    this.optimisticJson,
    required this.idempotencyKey,
    required this.createdAt,
    this.stateJson,
  });

  /// The mutation id, a uuid v4.
  final String id;

  /// The generated `operations` key, e.g. `op_update_order`.
  final String operationId;

  /// The arguments, JSON-encoded.
  final String argsJson;

  /// The optimistic patch, JSON-encoded, when there is one.
  final String? optimisticJson;

  /// The `Idempotency-Key`, stable across replays.
  final String idempotencyKey;

  /// When the mutation was first queued.
  ///
  /// An adapter must round-trip it losslessly: the same instant to the
  /// microsecond, read back as a UTC [DateTime]. Equality compares it, and
  /// [DateTime] equality compares `isUtc` too, so a record read back with a
  /// coarser time, or as local time, is a different record.
  final DateTime createdAt;

  /// The outbox state, JSON-encoded: queued, sending or failed. Written and
  /// read by `forge_client_offline`; null until it sets one.
  final String? stateJson;

  @override
  bool operator ==(Object other) =>
      other is PendingMutationRecord &&
      other.id == id &&
      other.operationId == operationId &&
      other.argsJson == argsJson &&
      other.optimisticJson == optimisticJson &&
      other.idempotencyKey == idempotencyKey &&
      other.createdAt == createdAt &&
      other.stateJson == stateJson;

  @override
  int get hashCode => Object.hash(
    id,
    operationId,
    argsJson,
    optimisticJson,
    idempotencyKey,
    createdAt,
    stateJson,
  );
}

/// String keys to string values, scoped to one namespace of one partition.
abstract interface class KeyValueStore {
  /// The value at [key], or null.
  Future<String?> get(String key);

  /// Set [key] to [value].
  Future<void> put(String key, String value);

  /// Remove [key].
  Future<void> delete(String key);

  /// Every entry whose key starts with [prefix], read as a literal string
  /// (no `_`, `%` or other pattern character means anything), in ascending
  /// UTF-16 code unit order, the order [String.compareTo] gives.
  Future<Map<String, String>> scan(String prefix);

  /// Apply every write [build] records, all together, or none if it throws.
  ///
  /// [build] runs synchronously and must not await: the batch closes when it
  /// returns, and a write recorded after that throws [StateError].
  Future<void> batch(void Function(KeyValueBatch batch) build);
}

/// The writes of one [KeyValueStore.batch]. Usable only while its builder
/// runs.
abstract interface class KeyValueBatch {
  /// Set [key] to [value].
  void put(String key, String value);

  /// Remove [key].
  void delete(String key);
}

/// An in-memory [StorageAdapter] for tests and for apps that persist nothing.
StorageAdapter memoryStorage() => _MemoryStorage();

final class _Partition {
  String? snapshot;
  final List<PendingMutationRecord> outbox = [];
  final Map<String, SplayTreeMap<String, String>> namespaces = {};
}

final class _MemoryStorage implements StorageAdapter {
  final Map<String, _Partition> _partitions = {};
  final Map<String, Set<_MemorySession>> _sessions = {};

  @override
  Future<StorageSession> open(String principal) async {
    final session = _MemorySession(
      principal,
      _partitions.putIfAbsent(principal, _Partition.new),
      _forget,
    );

    (_sessions[principal] ??= {}).add(session);

    return session;
  }

  @override
  Future<void> destroy(String principal) async {
    for (final session in [...?_sessions.remove(principal)]) {
      session._revoke();
    }

    _partitions.remove(principal);
  }

  void _forget(_MemorySession session) =>
      _sessions[session.principal]?.remove(session);
}

final class _MemorySession implements StorageSession {
  _MemorySession(this.principal, this._partition, this._forget);

  @override
  final String principal;

  final _Partition _partition;
  final void Function(_MemorySession session) _forget;
  bool _closed = false;

  void _revoke() => _closed = true;

  void _check() {
    if (_closed) {
      throw StateError('[forge] the storage session for $principal is closed');
    }
  }

  @override
  Future<Snapshot?> readSnapshot() async {
    _check();

    final text = _partition.snapshot;

    return text == null ? null : Snapshot.decode(text);
  }

  @override
  Future<void> writeSnapshot(Snapshot snapshot) async {
    _check();
    _partition.snapshot = snapshot.encode();
  }

  @override
  Future<List<PendingMutationRecord>> readOutbox() async {
    _check();

    return List.unmodifiable(_partition.outbox);
  }

  @override
  Future<void> enqueue(PendingMutationRecord record) async {
    _check();

    if (_partition.outbox.any((queued) => queued.id == record.id)) {
      throw StateError('[forge] mutation ${record.id} is already queued');
    }

    _partition.outbox.add(record);
  }

  @override
  Future<void> remove(String mutationId) async {
    _check();
    _partition.outbox.removeWhere((queued) => queued.id == mutationId);
  }

  @override
  Future<void> updateState(String mutationId, String stateJson) async {
    _check();

    final index = _partition.outbox.indexWhere(
      (queued) => queued.id == mutationId,
    );

    if (index == -1) {
      throw StateError('[forge] mutation $mutationId is not queued');
    }

    final queued = _partition.outbox[index];

    _partition.outbox[index] = PendingMutationRecord(
      id: queued.id,
      operationId: queued.operationId,
      argsJson: queued.argsJson,
      optimisticJson: queued.optimisticJson,
      idempotencyKey: queued.idempotencyKey,
      createdAt: queued.createdAt,
      stateJson: stateJson,
    );
  }

  @override
  KeyValueStore namespace(String name) {
    _check();

    return _MemoryKeyValueStore(
      this,
      _partition.namespaces.putIfAbsent(name, SplayTreeMap<String, String>.new),
    );
  }

  @override
  Future<void> close() async {
    if (_closed) return;

    _closed = true;
    _forget(this);
  }
}

final class _MemoryKeyValueStore implements KeyValueStore {
  _MemoryKeyValueStore(this._session, this._entries);

  final _MemorySession _session;
  final SplayTreeMap<String, String> _entries;

  @override
  Future<String?> get(String key) async {
    _session._check();

    return _entries[key];
  }

  @override
  Future<void> put(String key, String value) async {
    _session._check();
    _entries[key] = value;
  }

  @override
  Future<void> delete(String key) async {
    _session._check();
    _entries.remove(key);
  }

  @override
  Future<Map<String, String>> scan(String prefix) async {
    _session._check();

    return {
      for (final MapEntry(:key, :value) in _entries.entries)
        if (key.startsWith(prefix)) key: value,
    };
  }

  @override
  Future<void> batch(void Function(KeyValueBatch batch) build) async {
    _session._check();

    final staged = _Batch();

    // A throw here leaves the store untouched: nothing has been applied yet.
    try {
      build(staged);
    } finally {
      staged.closed = true;
    }

    for (final (key, value) in staged.writes) {
      if (value == null) {
        _entries.remove(key);
      } else {
        _entries[key] = value;
      }
    }
  }
}

final class _Batch implements KeyValueBatch {
  final List<(String, String?)> writes = [];

  /// Set once the builder returned or threw.
  bool closed = false;

  void _check() {
    if (closed) {
      throw StateError(
        '[forge] this batch was already applied; record every write before '
        'the builder returns, without awaiting',
      );
    }
  }

  @override
  void put(String key, String value) {
    _check();
    writes.add((key, value));
  }

  @override
  void delete(String key) {
    _check();
    writes.add((key, null));
  }
}
