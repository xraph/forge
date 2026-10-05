import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

import 'scope.dart';
import 'state_equality.dart';
import 'subscription.dart';

/// Rebuilds with the [QueryState] of one query.
///
/// ```dart
/// ForgeQueryBuilder(
///   query: getOrder(GetOrderArgs(id: id)),
///   builder: (context, state) => switch (state) {
///     QueryIdle() || QueryLoading() => const CircularProgressIndicator(),
///     QuerySuccess(:final data) => OrderView(data),
///     QueryFailure(:final error, :final previous) => ErrorView(error, previous),
///   },
/// )
/// ```
///
/// The subscription is keyed on [QueryRef.key], the same key the cache uses,
/// so a `query:` written inline in a parent's `build` does not refetch or
/// resubscribe when the parent rebuilds, even with a hand-written args class
/// that has no `==`. The client resolves as [ForgeScope.of] does: [client],
/// then the nearest scope, then the global client.
///
/// A change that reaches the builder while the tree is being built, which
/// happens when another widget mounting during a build starts a fetch of the
/// same query, is applied after that frame rather than during it.
final class ForgeQueryBuilder<T> extends StatefulWidget {
  /// Creates a builder for [query].
  const ForgeQueryBuilder({
    super.key,
    required this.query,
    required this.builder,
    this.client,
    this.live = false,
    this.staleTime,
    this.enabled = true,
    this.select,
  });

  /// The query to watch, usually `binding(args)`.
  final QueryRef<T, OperationArgs> query;

  /// Builds the subtree for the current state.
  final Widget Function(BuildContext context, QueryState<T> state) builder;

  /// Use this cache rather than the scoped or global one.
  final QueryCache? client;

  /// Also apply server frames pushed on the channels this query's entities
  /// ride. Toggling it subscribes or releases the channel and never refetches.
  final bool live;

  /// How long this call site considers the result fresh.
  final Duration? staleTime;

  /// While false the query is `QueryIdle`, fetches nothing and holds no
  /// mount. Use it for a query that depends on another one's result.
  final bool enabled;

  /// When set, the builder rebuilds only when `select(state)` changes,
  /// compared by identity and then deep equality. Return a record to watch
  /// several things at once.
  final Object? Function(QueryState<T> state)? select;

  @override
  State<ForgeQueryBuilder<T>> createState() => _ForgeQueryBuilderState<T>();
}

final class _ForgeQueryBuilderState<T> extends State<ForgeQueryBuilder<T>> {
  late final QuerySubscription<T> _subscription = QuerySubscription<T>(_changed);
  QueryCache? _scoped;
  Object? _selected;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _scoped = ForgeScope.maybeOf(context);
    _bind();
  }

  @override
  void didUpdateWidget(ForgeQueryBuilder<T> oldWidget) {
    super.didUpdateWidget(oldWidget);
    _bind();
  }

  void _bind() {
    _subscription.bind(
      widget.client ?? _scoped ?? getClient(),
      widget.query,
      live: widget.live,
      staleTime: widget.staleTime,
      enabled: widget.enabled,
    );
    _selected = widget.select?.call(_subscription.state);
  }

  // The subscription calls this only for a state that renders differently,
  // never after dispose and never during a build, so it needs neither a
  // `mounted` check nor a second sameQueryState.
  void _changed(QueryState<T> previous, QueryState<T> next) {
    final select = widget.select;
    if (select != null) {
      final selected = select(next);
      if (sameSelection(selected, _selected)) return;
      _selected = selected;
    }
    setState(() {});
  }

  @override
  void dispose() {
    _subscription.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(context, _subscription.state);
}
