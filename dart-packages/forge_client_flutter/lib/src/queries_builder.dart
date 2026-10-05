import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

import 'scope.dart';
import 'subscription.dart';

/// The combined status of several queries.
enum ForgeCombinedStatus {
  /// Some query is disabled, and none is loading or failed.
  idle,

  /// Some query has no data yet, and none failed.
  loading,

  /// Every query has data.
  success,

  /// Some query failed.
  failure,
}

/// What a [ForgeQueriesBuilder] hands its builder.
final class ForgeQueriesState {
  /// Combines [states], in the order the queries were given.
  ForgeQueriesState(List<QueryState<Object?>> states)
    : states = List<QueryState<Object?>>.unmodifiable(states);

  /// Each query's own state, in order.
  final List<QueryState<Object?>> states;

  /// Failure if any failed, then loading if any is loading, then idle if any
  /// is idle, otherwise success. An empty list is success.
  ForgeCombinedStatus get status {
    if (states.any((s) => s is QueryFailure<Object?>)) return .failure;
    if (states.any((s) => s is QueryLoading<Object?>)) return .loading;
    if (states.any((s) => s is QueryIdle<Object?>)) return .idle;
    return .success;
  }

  /// The first failure's error, or null.
  Object? get error => states.whereType<QueryFailure<Object?>>().firstOrNull?.error;

  /// Whether any query is fetching.
  bool get isFetching => states.any((s) => s.isFetching);

  /// Whether any query shows an optimistic value.
  bool get isOptimistic => states.any((s) => s.isOptimistic);

  /// The sync statuses folded with `foldSyncStatus`.
  SyncStatus get syncStatus => foldSyncStatus(states.map((s) => s.syncStatus));

  /// Every query's data, in order, when [status] is success; otherwise null.
  List<Object?>? get data => status == .success ? [for (final s in states) s.dataOrNull] : null;

  /// Query [index]'s data, cast to [T]. Use a nullable [T] when the query may
  /// not have data yet.
  T dataAt<T>(int index) => states[index].dataOrNull as T;
}

/// Rebuilds with the combined state of several queries.
///
/// Each query keeps its own subscription, keyed on its [QueryRef.key], so
/// changing one entry's args resubscribes only that entry. The options apply
/// to every query. A change that reaches the builder while the tree is being
/// built is applied after that frame rather than during it, as
/// [ForgeQueryBuilder] does.
final class ForgeQueriesBuilder extends StatefulWidget {
  /// Creates a builder over [queries].
  const ForgeQueriesBuilder({
    super.key,
    required this.queries,
    required this.builder,
    this.client,
    this.live = false,
    this.staleTime,
    this.enabled = true,
  });

  /// The queries to watch, in order.
  final List<QueryRef<Object?, OperationArgs>> queries;

  /// Builds the subtree for the combined state.
  final Widget Function(BuildContext context, ForgeQueriesState state) builder;

  /// Use this cache rather than the scoped or global one.
  final QueryCache? client;

  /// Also apply server frames to every query.
  final bool live;

  /// How long this call site considers each result fresh.
  final Duration? staleTime;

  /// While false every query is idle and nothing is fetched.
  final bool enabled;

  @override
  State<ForgeQueriesBuilder> createState() => _ForgeQueriesBuilderState();
}

final class _ForgeQueriesBuilderState extends State<ForgeQueriesBuilder> {
  final List<QuerySubscription<Object?>> _subscriptions = [];
  QueryCache? _scoped;

  /// The combination for the current subscription states, built on demand
  /// and dropped whenever any of them changes.
  ForgeQueriesState? _combined;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _scoped = ForgeScope.maybeOf(context);
    _bind();
  }

  @override
  void didUpdateWidget(ForgeQueriesBuilder oldWidget) {
    super.didUpdateWidget(oldWidget);
    _bind();
  }

  void _bind() {
    final client = widget.client ?? _scoped ?? getClient();
    while (_subscriptions.length > widget.queries.length) {
      _subscriptions.removeLast().dispose();
    }
    while (_subscriptions.length < widget.queries.length) {
      _subscriptions.add(QuerySubscription<Object?>(_changed));
    }
    for (var i = 0; i < widget.queries.length; i++) {
      _subscriptions[i].bind(
        client,
        widget.queries[i],
        live: widget.live,
        staleTime: widget.staleTime,
        enabled: widget.enabled,
      );
    }
    _combined = null;
  }

  // Each subscription calls this only for a state that renders differently,
  // never after dispose and never during a build.
  void _changed(QueryState<Object?> previous, QueryState<Object?> next) {
    setState(() => _combined = null);
  }

  @override
  void dispose() {
    for (final subscription in _subscriptions) {
      subscription.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(
    context,
    _combined ??= ForgeQueriesState([for (final s in _subscriptions) s.state]),
  );
}
