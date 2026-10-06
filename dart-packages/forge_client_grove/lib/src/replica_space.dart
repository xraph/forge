import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:uuid/uuid.dart';

import 'kv_adapter.dart';

/// The principal's grove replicas: its node id and one namespace per dataset.
///
/// Everything lives in the principal's own [StorageSession], so two principals
/// on one device never share a node id or a replica.
final class ReplicaSpace {
  ReplicaSpace._(this._session, this.nodeId);

  final StorageSession? _session;
  final Map<String, MapReplicaKeyValue> _memory = {};

  /// This device's node id for the principal, `dart-<uuid v4>`, kept across
  /// restarts at key `node` of the session's `grove` namespace.
  final String nodeId;

  /// Whether replicas survive a restart (a session was available).
  bool get persistent => _session != null;

  /// The tail of the opens queued on each session.
  static final Expando<Future<void>> _opening = Expando('ReplicaSpace.open');

  /// Opens the space in [session], or in memory when [session] is null.
  ///
  /// Opens of one session run one after another. The key-value store has no
  /// compare-and-set, so two first-time opens running at once would each find
  /// no node id and mint their own; queued, the second reads the first's.
  ///
  /// [newNodeId] replaces the generator of a first-time node id, for tests.
  static Future<ReplicaSpace> open(
    StorageSession? session, {
    String Function()? newNodeId,
  }) async {
    if (session == null) return _open(null, newNodeId);

    final previous = _opening[session];
    final done = Completer<void>();

    _opening[session] = done.future;

    try {
      // Never fails: every queued open completes it normally.
      if (previous != null) await previous;

      return await _open(session, newNodeId);
    } finally {
      done.complete();
    }
  }

  static Future<ReplicaSpace> _open(
    StorageSession? session,
    String Function()? newNodeId,
  ) async {
    final ReplicaKeyValue meta = session == null
        ? MapReplicaKeyValue()
        : ForgeKeyValueAdapter(session.namespace('grove'));

    var node = await meta.get('node');

    if (node == null) {
      node = newNodeId?.call() ?? 'dart-${const Uuid().v4()}';

      await meta.put('node', node);
    }

    return ReplicaSpace._(session, node);
  }

  ReplicaKeyValue _kv(String replicaKey) {
    final session = _session;

    return session == null
        ? _memory.putIfAbsent(replicaKey, MapReplicaKeyValue.new)
        : ForgeKeyValueAdapter(session.namespace('grove/$replicaKey'));
  }

  /// The replica storage for one dataset, in namespace `grove/<replicaKey>`.
  ///
  /// Synchronous: it throws a [StateError] at the call when the principal's
  /// session is closed or destroyed. Its storage calls fail through their
  /// futures.
  KeyValueReplicaStorage dataset(String replicaKey) =>
      KeyValueReplicaStorage(_kv(replicaKey));

  /// Deletes every key of one dataset's namespace. The storage seam has no
  /// namespace drop, and an empty namespace is the same thing.
  ///
  /// Every failure, including a closed session's [StateError], arrives through
  /// the returned future, never as a throw at the call.
  Future<void> erase(String replicaKey) async => dataset(replicaKey).clearAll();
}
