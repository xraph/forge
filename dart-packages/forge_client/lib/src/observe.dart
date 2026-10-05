import 'operation.dart';
import 'state.dart';
import 'stream_types.dart';
import 'sync.dart';

/// Everything the cache reports to its one observer slot.
///
/// Each event either says what a query did or what caused it; the pairing of
/// the two is what answers "why did this refetch". Payloads are live: read
/// them inside the synchronous call and copy anything you keep. Plans 04 and
/// 05 emit the outbox and sync events declared here.
sealed class CacheEvent {
  /// Const base constructor.
  const CacheEvent();
}

/// A tracked query changed state. TypeScript `{type: 'query'}`.
final class QueryTransition extends CacheEvent {
  /// Creates the event.
  const QueryTransition({
    required this.key,
    required this.status,
    required this.fetching,
  });

  /// The query's cache key.
  final String key;

  /// Its status after the transition.
  final QueryStatus status;

  /// Whether a request is in flight after the transition.
  final bool fetching;
}

/// A mutation settled, immediately before its tags are applied. TypeScript
/// `{type: 'mutation'}`.
final class MutationCommitted extends CacheEvent {
  /// Creates the event.
  const MutationCommitted({
    required this.meta,
    required this.args,
    required this.response,
  });

  /// The operation.
  final OperationMeta meta;

  /// Its arguments.
  final TagContext args;

  /// What the server returned.
  final Object? response;
}

/// A batch of stream frames committed, with the tags it raised. TypeScript
/// `{type: 'frames'}`. Emitted by plan 01b's frame applier.
final class FramesCommitted extends CacheEvent {
  /// Creates the event.
  const FramesCommitted({
    required this.count,
    required this.tags,
    required this.frames,
  });

  /// How many frames the batch held.
  final int count;

  /// The tags the batch raised.
  final Set<String> tags;

  /// The frames themselves.
  final List<StreamFrame> frames;
}

/// One mounted query was hit, with the tags that reached it. TypeScript
/// `{type: 'invalidated'}`.
final class QueryInvalidated extends CacheEvent {
  /// Creates the event.
  const QueryInvalidated({required this.key, required this.matched});

  /// The query's cache key.
  final String key;

  /// The tags that matched.
  final Set<String> matched;
}

/// A placement callback answered for this query, so no refetch is owed.
/// TypeScript `{type: 'placed'}`.
final class QueryPlaced extends CacheEvent {
  /// Creates the event.
  const QueryPlaced({required this.key});

  /// The query's cache key.
  final String key;
}

/// A write went into the outbox. Emitted by plan 04.
final class OutboxEnqueued extends CacheEvent {
  /// Creates the event.
  const OutboxEnqueued({required this.mutationId, required this.operationId});

  /// The pending mutation's id.
  final String mutationId;

  /// The operation's table key.
  final String operationId;
}

/// An outbox write replayed successfully. Emitted by plan 04.
final class OutboxReplayed extends CacheEvent {
  /// Creates the event.
  const OutboxReplayed({required this.mutationId, required this.operationId});

  /// The pending mutation's id.
  final String mutationId;

  /// The operation's table key.
  final String operationId;
}

/// An outbox replay failed. [failure] is the offline package's
/// `OutboxFailure`. Emitted by plan 04.
final class OutboxFailed extends CacheEvent {
  /// Creates the event.
  const OutboxFailed({
    required this.mutationId,
    required this.operationId,
    required this.failure,
  });

  /// The pending mutation's id.
  final String mutationId;

  /// The operation's table key.
  final String operationId;

  /// Why it failed.
  final Object failure;
}

/// A sync source's status for one entity changed. Emitted by plan 05.
final class SyncStatusChanged extends CacheEvent {
  /// Creates the event.
  const SyncStatusChanged({required this.entity, required this.status});

  /// The entity typename.
  final String entity;

  /// The new status.
  final SyncStatus status;
}

/// The observer slot's type. One observer, not a list: assigning replaces.
typedef CacheObserver = void Function(CacheEvent event);

/// What the devtools Outbox panel (plan 06) drives. The offline package's
/// `OfflineClient` (plan 04) implements it; declared here so it can before
/// the devtools package exists.
abstract interface class OutboxInspector {
  /// Replays the pending write [mutationId] now, out of its normal turn.
  Future<void> replay(String mutationId);

  /// Drops the pending write [mutationId] and rolls back its optimistic
  /// overlay.
  Future<void> discard(String mutationId);
}

/// Anything that reports outbox failures. `forge_client_offline`'s
/// `OfflineClient` implements it; each event is that package's
/// `OutboxFailure`, typed `Object` here because the class lives downstream.
abstract interface class OutboxFailureSource {
  /// Every outbox failure, as it happens.
  Stream<Object> get failures;
}

/// Something the devtools extension (plan 06) can describe in a panel. The
/// Grove sync source (plan 05) implements it with its replica clock, peers and
/// pending counts per entity.
abstract interface class DevtoolsInspectable {
  /// A JSON-safe description of the current state. Called only while a
  /// devtools panel is open, so it may be expensive.
  Future<Map<String, Object?>> describeForDevtools();
}
