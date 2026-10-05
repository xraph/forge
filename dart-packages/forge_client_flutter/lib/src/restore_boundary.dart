import 'dart:async';

import 'package:flutter/widgets.dart';

/// Shows [placeholder] until [restore] completes, then [child].
///
/// [restore] runs once per mount, after the frame that mounts the boundary
/// and never inside it. A restore usually hydrates the cache, and the cache
/// tells its watchers synchronously; started during a build it would reach
/// them in the middle of the frame. A rebuild that passes a new closure does
/// not run it again. To restore again, for example for another principal,
/// give the boundary a new `Key`: `ValueKey(principal)` shows the placeholder
/// and runs the new mount's callback.
///
/// A failed restore still renders [child], on whatever the cache holds: the
/// app works on a cold cache, it just fetches. The failure goes to [onError]
/// when given, otherwise to `FlutterError.reportError`. A callback that
/// throws before it returns a future counts as a failed restore too.
///
/// `forge_client_offline` supplies the callback, so this package does not
/// depend on it. It reads the snapshot from the `QueryCache.session` the
/// cache opened for the current principal, then calls `hydrate` with the
/// generated operations table. `session` is null while a principal switch is
/// in progress, so the callback awaits `QueryCache.idle` before reading it:
///
/// ```dart
/// restore: () async {
///   await cache.idle;
///   final stored = await readSnapshot(cache.session);
///   if (stored != null) {
///     hydrate(cache, stored, principal: cache.principal, operations: operations, stale: true);
///   }
/// }
/// ```
final class ForgeRestoreBoundary extends StatefulWidget {
  /// Creates a boundary that runs [restore] before showing [child].
  const ForgeRestoreBoundary({
    super.key,
    required this.restore,
    required this.child,
    this.placeholder = const SizedBox.shrink(),
    this.onError,
  });

  /// Seeds the cache, typically by hydrating a persisted snapshot.
  final Future<void> Function() restore;

  /// The subtree shown once [restore] has finished.
  final Widget child;

  /// Shown while [restore] runs.
  final Widget placeholder;

  /// Receives a failed restore. When null the failure is reported through
  /// `FlutterError.reportError`.
  final void Function(Object error, StackTrace stackTrace)? onError;

  @override
  State<ForgeRestoreBoundary> createState() => _ForgeRestoreBoundaryState();
}

final class _ForgeRestoreBoundaryState extends State<ForgeRestoreBoundary> {
  bool _ready = false;

  @override
  void initState() {
    super.initState();
    // The first closure is the one this mount runs. A microtask starts it
    // after the current build, and turns a synchronous throw into a failed
    // future, so both reach the same handling.
    unawaited(_run(widget.restore));
  }

  Future<void> _run(Future<void> Function() restore) async {
    try {
      await Future<void>.microtask(restore);
    } catch (error, stackTrace) {
      _report(error, stackTrace);
    }
    if (mounted) setState(() => _ready = true);
  }

  void _report(Object error, StackTrace stackTrace) {
    final onError = widget.onError;
    if (onError != null) {
      onError(error, stackTrace);
      return;
    }
    FlutterError.reportError(FlutterErrorDetails(
      exception: error,
      stack: stackTrace,
      library: 'forge_client_flutter',
      context: ErrorDescription('while restoring the cache'),
    ));
  }

  @override
  Widget build(BuildContext context) => _ready ? widget.child : widget.placeholder;
}
