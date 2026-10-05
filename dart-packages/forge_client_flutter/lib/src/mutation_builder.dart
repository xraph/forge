import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

import 'scope.dart';

/// Binds one write operation to a subtree.
///
/// ```dart
/// ForgeMutationBuilder(
///   mutation: updateOrder,
///   optimistic: (args) => OptimisticUpdate((order) => order.copyWith(note: args.note)),
///   builder: (context, m) => FilledButton(
///     onPressed: m.isPending ? null : () => m.mutate(UpdateOrderArgs(id: id, note: note)),
///     child: const Text('Save'),
///   ),
/// )
/// ```
///
/// A mutation is local to the widget that fires it: two builders on one
/// binding are two independent statuses. Everything the write causes (the
/// entity commit, invalidation, placement and the optimistic overlay) happens
/// in the cache and reaches every query through its own subscription.
///
/// [ForgeMutation.mutate] never throws: a failure is recorded in the state and
/// the future resolves with null, so the spelling an `onPressed` uses cannot
/// raise an unhandled error. [ForgeMutation.mutateAsync] records the same
/// state and rethrows, for a caller that sequences work after the write.
///
/// A call that finishes after the widget left the tree still runs to its end
/// and still resolves for its caller, but records nothing and builds nothing.
/// A state change that happens while the tree is being built, such as a
/// `mutate` called from a `build` method, is applied after that frame rather
/// than during it.
final class ForgeMutationBuilder<R, A extends OperationArgs, E> extends StatefulWidget {
  /// Creates a builder for [mutation].
  const ForgeMutationBuilder({
    super.key,
    required this.mutation,
    required this.builder,
    this.client,
    this.optimistic,
    this.place = const {},
  });

  /// The binding to call.
  final MutationBinding<R, A, E> mutation;

  /// Builds the subtree for the current mutation handle.
  final Widget Function(BuildContext context, ForgeMutation<R, A, E> mutation) builder;

  /// Use this cache rather than the scoped or global one.
  final QueryCache? client;

  /// The optimistic patch for a call, from its args. Read at call time from
  /// the latest widget. A per-call `optimistic:` wins.
  final Optimistic<E>? Function(A args)? optimistic;

  /// Placement callbacks by tag, applied instead of refetching those queries.
  /// Read at call time from the latest widget. A per-call `place:` wins.
  final Map<String, Placement> place;

  @override
  State<ForgeMutationBuilder<R, A, E>> createState() => _ForgeMutationBuilderState<R, A, E>();
}

/// The handle a [ForgeMutationBuilder] passes to its builder: the current
/// state plus the operations.
///
/// A handle is a snapshot. Its [state] is the state at the build that made
/// it, while its operations always act on the widget's latest configuration.
final class ForgeMutation<R, A extends OperationArgs, E> {
  const ForgeMutation._(this.state, this._owner);

  /// Where this widget's mutation is in its lifecycle.
  final MutationState<R> state;

  final _ForgeMutationBuilderState<R, A, E> _owner;

  /// Whether a call is in flight.
  bool get isPending => state is MutationPending<R>;

  /// The last successful result, or null.
  R? get dataOrNull => switch (state) {
    MutationSuccess(:final data) => data,
    _ => null,
  };

  /// The last failure, or null.
  Object? get errorOrNull => switch (state) {
    MutationFailure(:final error) => error,
    _ => null,
  };

  /// Runs the mutation. Never throws: on failure it records the error and
  /// resolves with null.
  ///
  /// [options] carries per-call headers and a cancel future to the transport.
  Future<R?> mutate(
    A args, {
    Optimistic<E>? optimistic,
    Map<String, Placement>? place,
    QueryCache? client,
    RequestOptions options = const RequestOptions(),
  }) async {
    try {
      return await _owner._run(
        args,
        optimistic: optimistic,
        place: place,
        client: client,
        options: options,
      );
    } on Object {
      // Recorded in the state by _run; this variant leaves it there.
      return null;
    }
  }

  /// Runs the mutation and rethrows on failure, after recording it.
  Future<R> mutateAsync(
    A args, {
    Optimistic<E>? optimistic,
    Map<String, Placement>? place,
    QueryCache? client,
    RequestOptions options = const RequestOptions(),
  }) => _owner._run(
    args,
    optimistic: optimistic,
    place: place,
    client: client,
    options: options,
  );

  /// Back to idle. Supersedes any call in flight, so its result is not
  /// recorded when it lands.
  void reset() => _owner._reset();
}

final class _ForgeMutationBuilderState<R, A extends OperationArgs, E>
    extends State<ForgeMutationBuilder<R, A, E>> {
  MutationState<R> _state = MutationIdle<R>();
  QueryCache? _scoped;

  // Distinguishes overlapping calls, so the first response landing after the
  // second does not overwrite it.
  int _seq = 0;

  // Whether a post-frame rebuild is already scheduled.
  bool _rebuildScheduled = false;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _scoped = ForgeScope.maybeOf(context);
  }

  Future<R> _run(
    A args, {
    Optimistic<E>? optimistic,
    Map<String, Placement>? place,
    QueryCache? client,
    required RequestOptions options,
  }) async {
    final call = ++_seq;
    _record(call, MutationPending<R>());
    try {
      // Everything that can throw is inside the try, so a missing client or a
      // throwing `optimistic` callback is recorded as the call's failure
      // rather than escaping `mutate`, which promises not to throw.
      final target = client ?? widget.client ?? _scoped ?? getClient();
      final data = await widget.mutation(
        target,
        args,
        optimistic: optimistic ?? widget.optimistic?.call(args),
        place: place ?? widget.place,
        options: options,
      );
      _record(call, MutationSuccess<R>(data));
      return data;
    } on Object catch (error) {
      _record(call, MutationFailure<R>(error));
      rethrow;
    }
  }

  void _record(int call, MutationState<R> next) {
    if (!mounted || call != _seq) return;
    _apply(next);
  }

  void _reset() {
    _seq++;
    if (!mounted) return;
    _apply(MutationIdle<R>());
  }

  /// Takes [next] as the state and asks for a rebuild. The state is current
  /// at once; only the rebuild waits when the tree is mid-build.
  void _apply(MutationState<R> next) {
    _state = next;

    final scheduler = SchedulerBinding.instance;
    if (scheduler.schedulerPhase != SchedulerPhase.persistentCallbacks) {
      setState(() {});
      return;
    }

    // build, layout, paint and finalizeTree: setState throws here when the
    // widget is not a descendant of the one being built. That frame is
    // already running, so the callback is sure to run, and the setState in it
    // schedules the next frame. One callback per frame covers any number of
    // changes, because the rebuild reads the latest state.
    if (_rebuildScheduled) return;
    _rebuildScheduled = true;
    scheduler.addPostFrameCallback((_) {
      _rebuildScheduled = false;
      if (mounted) setState(() {});
    }, debugLabel: 'ForgeMutationBuilder.rebuild');
  }

  @override
  Widget build(BuildContext context) =>
      widget.builder(context, ForgeMutation<R, A, E>._(_state, this));
}
