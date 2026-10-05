import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

import 'scope.dart';
import 'subscription.dart';

/// Runs a side effect on each state transition of a query, without
/// rebuilding [child].
///
/// It watches the query, so it fetches and mounts it like a builder does.
/// The query's starting state is not a transition; neither is the starting
/// state of a new query after its args change.
///
/// The listener never runs while the tree is being built. A transition that
/// reaches it then, which happens when another widget mounting during a
/// build starts a fetch of the same query, is delivered after that frame.
final class ForgeListener<T> extends StatefulWidget {
  /// Creates a listener on [query].
  const ForgeListener({
    super.key,
    required this.query,
    required this.listener,
    required this.child,
    this.client,
    this.listenWhen,
    this.live = false,
    this.staleTime,
    this.enabled = true,
  });

  /// The query to watch.
  final QueryRef<T, OperationArgs> query;

  /// Called with each transition the [listenWhen] filter lets through. The
  /// latest widget's listener is the one called.
  final void Function(BuildContext context, QueryState<T> previous, QueryState<T> next) listener;

  /// The subtree, never rebuilt by this widget.
  final Widget child;

  /// Use this cache rather than the scoped or global one.
  final QueryCache? client;

  /// Decides which transitions reach [listener]. Defaults to every
  /// transition.
  final bool Function(QueryState<T> previous, QueryState<T> next)? listenWhen;

  /// Also apply server frames.
  final bool live;

  /// How long this call site considers the result fresh.
  final Duration? staleTime;

  /// While false the query is idle and nothing is fetched.
  final bool enabled;

  @override
  State<ForgeListener<T>> createState() => _ForgeListenerState<T>();
}

final class _ForgeListenerState<T> extends State<ForgeListener<T>> {
  late final QuerySubscription<T> _subscription = QuerySubscription<T>(_changed);
  QueryCache? _scoped;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _scoped = ForgeScope.maybeOf(context);
    _bind();
  }

  @override
  void didUpdateWidget(ForgeListener<T> oldWidget) {
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
  }

  // The subscription calls this for every state that renders differently,
  // never after dispose and never during a build.
  void _changed(QueryState<T> previous, QueryState<T> next) {
    final when = widget.listenWhen;
    if (when == null || when(previous, next)) widget.listener(context, previous, next);
  }

  @override
  void dispose() {
    _subscription.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
