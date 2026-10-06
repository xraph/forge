/// The sync seam: a source that owns some entity types, holds their records,
/// takes their mutations and reports their status, and the status family
/// `QueryState.syncStatus` carries. `forge_client_grove` implements the seam
/// over the Grove CRDT protocol.
library;

import 'cache.dart' show QueryCache;
import 'operation.dart' show OperationMeta, TagContext;
import 'overlay.dart' show Optimistic;
import 'storage.dart';
import 'store.dart' show EntityStore;
import 'transport.dart' show Transport;

/// Where a sync source stands for one entity. Surfaced on every `QueryState`
/// as `syncStatus`, folded across the entities the query touches.
sealed class SyncStatus {
  /// Const base constructor.
  const SyncStatus();
}

/// Everything local has reached the server.
final class Synced extends SyncStatus {
  /// Creates the status.
  const Synced();

  @override
  bool operator ==(Object other) => other is Synced;

  @override
  int get hashCode => (Synced).hashCode;

  @override
  String toString() => 'Synced()';
}

/// [count] local changes have not reached the server yet.
final class Pending extends SyncStatus {
  /// Creates the status.
  const Pending(this.count);

  /// How many changes are waiting.
  final int count;

  @override
  bool operator ==(Object other) => other is Pending && other.count == count;

  @override
  int get hashCode => Object.hash(Pending, count);

  @override
  String toString() => 'Pending($count)';
}

/// The source cannot reach the server.
final class Offline extends SyncStatus {
  /// Creates the status.
  const Offline();

  @override
  bool operator ==(Object other) => other is Offline;

  @override
  int get hashCode => (Offline).hashCode;

  @override
  String toString() => 'Offline()';
}

/// The source failed with [error], for example a server sync hook rejection.
final class SyncFailed extends SyncStatus {
  /// Creates the status.
  const SyncFailed(this.error);

  /// What went wrong.
  final Object error;

  @override
  bool operator ==(Object other) => other is SyncFailed && other.error == error;

  @override
  int get hashCode => Object.hash(SyncFailed, error);

  @override
  String toString() => 'SyncFailed($error)';
}

/// Folds several statuses into one: [SyncFailed] beats [Offline], which beats
/// [Pending] (counts summed), which beats [Synced]. The first failure wins.
SyncStatus foldSyncStatus(Iterable<SyncStatus> statuses) {
  SyncFailed? failed;
  var offline = false;
  var pending = 0;

  for (final status in statuses) {
    switch (status) {
      case SyncFailed():
        failed ??= status;
      case Offline():
        offline = true;
      case Pending(:final count):
        pending += count;
      case Synced():
        break;
    }
  }

  if (failed != null) return failed;
  if (offline) return const Offline();
  if (pending > 0) return Pending(pending);

  return const Synced();
}

/// Owns [entities]: their records come from the source, never from a REST
/// response, a stream frame or a snapshot, and their mutations go to the
/// source.
///
/// The cache runs one principal's sources at a time. Between principals it
/// stops them, closes the old session and drops every owned record before the
/// next principal's sources start.
///
/// A source writes records only through [SyncContext.write], and only while
/// its context is [SyncContext.active]. The cache deactivates the context
/// synchronously, inside the `setPrincipal` call that moves to the next
/// principal and before that call empties the store, and on `dispose`. From
/// then on a write is dropped, even while [stop] is still running: the store
/// already belongs to the next principal. A source must never write through
/// `context.cache.store` directly. The cache also drops every owned record
/// once the old sources stopped, but that only catches what a source wrote
/// outside the seam; by then the next principal may have read it.
abstract interface class SyncSource {
  /// The typenames this source owns.
  Set<String> get entities;

  /// Begin syncing for `context.principal`.
  ///
  /// Return promptly: project what is local, then do network work (pull,
  /// connect) in the background rather than awaiting it here. The cache's
  /// next principal change, and every owned mutation, waits for this to
  /// return. When it throws, the cache reports the error, deactivates
  /// [context], calls [stop] and drops whatever records of [entities] the
  /// store holds: a partial projection must not survive a failed start.
  Future<void> start(SyncContext context);

  /// Apply one mutation locally; it may complete offline.
  Future<MutationOutcome> apply(PendingMutation mutation);

  /// The status of one owned entity type. Emits the current status to each
  /// new listener first, then every change.
  Stream<SyncStatus> status(String entity);

  /// Stop syncing. Idempotent: the cache may call it on a source whose
  /// [start] threw. The cache closes the session after every source stopped.
  Future<void> stop();
}

/// What a source is started with, and the fence it writes through.
///
/// One context per source per principal. It is active from the moment the
/// cache builds it until the cache moves to another principal or is disposed;
/// it never becomes active again.
final class SyncContext {
  /// The cache, its principal, its transport and the principal's session.
  SyncContext({
    required this.cache,
    required this.principal,
    required this.transport,
    required this.storage,
  });

  /// The cache whose store the source projects records into, through [write].
  final QueryCache cache;

  /// Whose data this is. Never null: nothing starts without a principal.
  final String principal;

  /// The cache's transport, for pull and push.
  final Transport transport;

  /// The cache's open session for [principal], where the source keeps its
  /// replica (in a [StorageSession.namespace]). Null when the cache has no
  /// storage. The cache opened it and closes it; a source never does either.
  final StorageSession? storage;

  bool _active = true;

  /// Whether [cache] still serves [principal] through this context. False
  /// from the moment the cache began moving to someone else, synchronously,
  /// before it emptied the store; [write] then does nothing.
  bool get active => _active;

  /// Apply [edit] to the cache's store and notify the cache's watchers, or do
  /// nothing at all when the context is no longer [active].
  ///
  /// The only way a source writes records. The edit must be synchronous (an
  /// async edit throws [ArgumentError]): the fence is checked once, before it
  /// runs, so an edit that awaited would write after the fence closed. An
  /// `async` closure is rejected before it runs. A closure typed as returning
  /// void that returns a [Future] at run time (an expression body such as
  /// `(store) => later()`) is rejected after its synchronous part has run,
  /// and the watchers are not notified. The watchers are notified even when
  /// [edit] throws part way, and the error is rethrown.
  void write(void Function(EntityStore store) edit) {
    if (edit is Future<void> Function(EntityStore)) {
      throw ArgumentError.value(
        edit,
        'edit',
        'must be synchronous: the fence is checked once, before it runs',
      );
    }

    if (!_active) return;

    final Object? Function(EntityStore store) run = edit;
    var notify = true;

    try {
      final result = run(cache.store);

      if (result is Future<Object?>) {
        notify = false;
        result.ignore();

        throw ArgumentError.value(
          edit,
          'edit',
          'must be synchronous: it returned a Future, which the fence '
              'cannot cover',
        );
      }
    } finally {
      if (notify) cache.notifyChanged();
    }
  }
}

/// Fences [context] for good: every later [SyncContext.write] is dropped. The
/// cache calls this; the package barrel does not export it.
void deactivateSyncContext(SyncContext context) => context._active = false;

/// One mutation handed to a source.
final class PendingMutation {
  /// A mutation [id] of [meta] with [args].
  const PendingMutation({
    required this.id,
    required this.meta,
    required this.args,
    required this.optimistic,
    required this.idempotencyKey,
    required this.createdAt,
  });

  /// A uuid v4.
  final String id;

  /// The operation.
  final OperationMeta meta;

  /// Its arguments, client-shaped.
  final TagContext args;

  /// The caller's optimistic patch, client-shaped, when there is one.
  final Optimistic<Object?>? optimistic;

  /// A uuid v4, stable across replays.
  final String idempotencyKey;

  /// When the mutation was made.
  final DateTime createdAt;
}

/// What [SyncSource.apply] did.
sealed class MutationOutcome {
  /// Base constructor.
  const MutationOutcome();
}

/// Applied now; [response] is what the caller receives.
///
/// Two outcomes are equal when their responses are `==`. That is shallow: a
/// Map or List response compares by identity, so two equal-looking decoded
/// responses are different outcomes.
final class Applied extends MutationOutcome {
  /// An applied mutation answering [response].
  const Applied(this.response);

  /// The client-shaped result.
  final Object? response;

  @override
  bool operator ==(Object other) =>
      other is Applied && other.response == response;

  @override
  int get hashCode => Object.hash(Applied, response);

  @override
  String toString() => 'Applied($response)';
}

/// Accepted and queued; it will be sent later.
final class Queued extends MutationOutcome {
  /// Queued as [mutationId].
  const Queued(this.mutationId);

  /// The queued mutation's id.
  final String mutationId;

  @override
  bool operator ==(Object other) =>
      other is Queued && other.mutationId == mutationId;

  @override
  int get hashCode => Object.hash(Queued, mutationId);

  @override
  String toString() => 'Queued($mutationId)';
}

/// Refused; [error] is thrown to the caller.
///
/// Two outcomes are equal when their errors are `==`, which for most error
/// types, and for a Map or List, means the same object.
final class Rejected extends MutationOutcome {
  /// Refused because of [error].
  const Rejected(this.error);

  /// Why.
  final Object error;

  @override
  bool operator ==(Object other) => other is Rejected && other.error == error;

  @override
  int get hashCode => Object.hash(Rejected, error);

  @override
  String toString() => 'Rejected($error)';
}

/// One row of the generated `sync` table: where an entity syncs.
final class SyncDeclaration {
  /// A declaration for [entity] over [protocol].
  const SyncDeclaration({
    required this.protocol,
    required this.entity,
    this.table,
    required this.pull,
    required this.push,
    this.stream,
    this.socket,
    this.dataset,
  });

  /// e.g. `grove-crdt`.
  final String protocol;

  /// The typename.
  final String entity;

  /// The server table. Null means one table per dataset, supplied at runtime
  /// (contracts, "Decisions from plan 05").
  final String? table;

  /// The pull path.
  final String pull;

  /// The push path.
  final String push;

  /// The SSE change-stream path, when there is one.
  final String? stream;

  /// The WebSocket path, when there is one.
  final String? socket;

  /// The dataset path parameter, when the routes are per dataset.
  final String? dataset;
}
