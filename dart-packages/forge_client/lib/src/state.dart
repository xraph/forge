import 'sync.dart';

/// Where a tracked query is in its lifecycle, as the cache records it.
/// [QueryState] is what subscribers read; this is what the devtools seam and
/// `TrackedRecord` report.
enum QueryStatus {
  /// Opened, never fetched.
  idle,

  /// The first request is in flight and nothing has settled.
  pending,

  /// Settled with a value.
  success,

  /// The last request failed.
  error,
}

/// What a subscriber reads. Referentially stable: the same object is handed
/// out until something in it actually changes.
sealed class QueryState<T> {
  /// Const base constructor.
  const QueryState({
    this.isFetching = false,
    this.isOptimistic = false,
    this.syncStatus = const Synced(),
  });

  /// A request is in flight. True during a background refetch of good data.
  final bool isFetching;

  /// Some of this value is a local change the server has not confirmed.
  final bool isOptimistic;

  /// The sync sources' status, folded across the owned entities the query
  /// touches: its operation's entity and every entity its value reaches.
  /// [Synced] when no sync source owns any of them.
  final SyncStatus syncStatus;

  /// The data this state carries: the value on success, the last good value on
  /// failure, null otherwise.
  T? get dataOrNull;
}

/// Opened, never fetched.
final class QueryIdle<T> extends QueryState<T> {
  /// Creates the state.
  const QueryIdle({super.isFetching, super.isOptimistic, super.syncStatus});

  @override
  T? get dataOrNull => null;
}

/// The first request is in flight. TypeScript's `status: 'pending'`.
final class QueryLoading<T> extends QueryState<T> {
  /// Creates the state.
  const QueryLoading({super.isFetching, super.isOptimistic, super.syncStatus});

  @override
  T? get dataOrNull => null;
}

/// Settled with [data].
final class QuerySuccess<T> extends QueryState<T> {
  /// Creates the state.
  const QuerySuccess(
    this.data, {
    super.isFetching,
    super.isOptimistic,
    super.syncStatus,
  });

  /// The value.
  final T data;

  @override
  T? get dataOrNull => data;
}

/// The last request failed with [error]. A failure that follows a success
/// keeps the last good value in [previous], as TypeScript keeps `data`.
final class QueryFailure<T> extends QueryState<T> {
  /// Creates the state.
  const QueryFailure(
    this.error, {
    this.previous,
    super.isFetching,
    super.isOptimistic,
    super.syncStatus,
  });

  /// What went wrong.
  final Object error;

  /// The last good value, if there was one.
  final T? previous;

  @override
  T? get dataOrNull => previous;
}

/// Where a mutation is.
sealed class MutationState<R> {
  /// Const base constructor.
  const MutationState();
}

/// Not called yet.
final class MutationIdle<R> extends MutationState<R> {
  /// Creates the state.
  const MutationIdle();
}

/// In flight.
final class MutationPending<R> extends MutationState<R> {
  /// Creates the state.
  const MutationPending();
}

/// Settled with [data].
final class MutationSuccess<R> extends MutationState<R> {
  /// Creates the state.
  const MutationSuccess(this.data);

  /// The response.
  final R data;
}

/// Failed with [error].
final class MutationFailure<R> extends MutationState<R> {
  /// Creates the state.
  const MutationFailure(this.error);

  /// What went wrong.
  final Object error;
}
