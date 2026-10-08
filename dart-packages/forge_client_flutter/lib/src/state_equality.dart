import 'package:collection/collection.dart';
import 'package:forge_client/forge_client.dart';

/// The state to render on the first frame for [query], before its stream
/// has delivered anything.
///
/// `QueryRef.watch` delivers its first event on a microtask, so a widget or
/// provider reads [QueryRef.getState] right after listening; the listen has
/// already mounted the query and started any fetch, so the seed shows
/// loading rather than idle. A disabled query gets the binding's shared
/// [QueryIdle] from `getState` itself, which opens no record, as a disabled
/// `watch` opens none.
QueryState<T> firstState<T>(
  QueryCache client,
  QueryRef<T, OperationArgs> query, {
  required bool enabled,
}) => query.getState(client, enabled: enabled);

/// Whether two query states would render the same.
///
/// Data is compared with [identical], which the core's structural sharing
/// makes exact: an unchanged record is the same object, so an unchanged
/// result is too.
bool sameQueryState(QueryState<Object?> a, QueryState<Object?> b) {
  if (identical(a, b)) return true;
  if (a.isFetching != b.isFetching ||
      a.isOptimistic != b.isOptimistic ||
      a.syncStatus != b.syncStatus) {
    return false;
  }
  return switch ((a, b)) {
    (QueryIdle(), QueryIdle()) || (QueryLoading(), QueryLoading()) => true,
    (QuerySuccess(data: final x), QuerySuccess(data: final y)) => identical(
      x,
      y,
    ),
    (
      QueryFailure(error: final e1, previous: final p1),
      QueryFailure(error: final e2, previous: final p2),
    ) =>
      identical(e1, e2) && identical(p1, p2),
    _ => false,
  };
}

/// Whether two `select` results are the same slice: identical, or deeply
/// equal, so a selector that builds a new list each time does not rebuild
/// while the list's contents are unchanged.
bool sameSelection(Object? a, Object? b) =>
    identical(a, b) || const DeepCollectionEquality().equals(a, b);
