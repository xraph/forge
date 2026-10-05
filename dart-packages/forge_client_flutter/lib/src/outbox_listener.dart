import 'dart:async';

import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart' show OutboxFailureSource;

/// Hands each outbox failure to [onFailure] for the app's own UI, a snack
/// bar or a review screen, without rebuilding [child].
///
/// [source] is `forge_client`'s [OutboxFailureSource]. `forge_client_offline`'s
/// `OfflineClient` implements it, so pass the client itself. Its stream
/// carries the sealed `OutboxFailure` values (`OutboxConflict`,
/// `OutboxValidation`, `OutboxUnauthorized`, `OutboxGone`,
/// `OutboxUncertain`), typed `Object` in the interface so neither this
/// package nor the core depends on the offline package; switch over the
/// sealed type in the app, where both are imported.
///
/// [onFailure] never runs while the tree is being built. A source can emit
/// from inside a build, because the cache notifies synchronously, and a
/// callback that shows a snack bar would mark widgets dirty in the middle of
/// it. A failure that arrives then is held and delivered after that frame,
/// in order, and every failure is delivered: unlike a state, one failure does
/// not replace another. A failure still held when the listener is removed is
/// dropped, since there is no context left to hand it. If [onFailure] throws,
/// held or not, the error is reported through `FlutterError.reportError` and
/// later failures are still delivered. A failure emitted from inside
/// [onFailure] queues behind the ones still held.
final class ForgeOutboxListener extends StatefulWidget {
  /// Creates a listener on [source].
  const ForgeOutboxListener({
    super.key,
    required this.source,
    required this.onFailure,
    required this.child,
  });

  /// Where failures come from, usually the `OfflineClient` itself.
  final OutboxFailureSource source;

  /// Called with each failure. The latest widget's callback is the one
  /// called.
  final void Function(BuildContext context, Object failure) onFailure;

  /// The subtree, never rebuilt by this widget.
  final Widget child;

  @override
  State<ForgeOutboxListener> createState() => _ForgeOutboxListenerState();
}

final class _ForgeOutboxListenerState extends State<ForgeOutboxListener> {
  StreamSubscription<Object>? _subscription;

  /// Failures that arrived during a build, awaiting the end of the frame.
  /// While any are held, later ones queue behind them to keep the order.
  final List<Object> _held = [];

  @override
  void initState() {
    super.initState();
    _listen();
  }

  @override
  void didUpdateWidget(ForgeOutboxListener oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.source, widget.source)) {
      _cancel();
      _listen();
    }
  }

  void _listen() {
    _subscription = widget.source.failures.listen(_receive);
  }

  void _receive(Object failure) {
    if (!mounted) return;
    final building =
        SchedulerBinding.instance.schedulerPhase == SchedulerPhase.persistentCallbacks;
    if (_held.isEmpty && !building) {
      _deliver(failure);
      return;
    }

    _held.add(failure);
    // A frame is already running, so this callback is sure to run.
    if (_held.length == 1) {
      SchedulerBinding.instance.addPostFrameCallback(
        (_) => _deliverHeld(),
        debugLabel: 'ForgeOutboxListener.deliver',
      );
    }
  }

  void _deliverHeld() {
    // Drained from the front rather than copied and cleared, so a failure
    // that onFailure itself emits lands behind the ones still waiting.
    while (_held.isNotEmpty) {
      final failure = _held.removeAt(0);
      if (!mounted) {
        _held.clear();
        return;
      }
      _deliver(failure);
    }
  }

  void _deliver(Object failure) {
    try {
      widget.onFailure(context, failure);
    } catch (error, stackTrace) {
      FlutterError.reportError(FlutterErrorDetails(
        exception: error,
        stack: stackTrace,
        library: 'forge_client_flutter',
        context: ErrorDescription('while handling an outbox failure'),
      ));
    }
  }

  void _cancel() {
    final subscription = _subscription;
    _subscription = null;
    if (subscription != null) unawaited(subscription.cancel());
  }

  @override
  void dispose() {
    _cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
