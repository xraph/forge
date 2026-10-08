import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

/// A [CommitScheduler] that applies store commits at the start of the next
/// frame, so a burst of stream frames costs one rebuild rather than one per
/// frame.
///
/// `QueryCache` takes its commit scheduler at construction, so pass this when
/// the cache is built:
///
/// ```dart
/// configureClient(transport: t, entities: entities, commitScheduler: frameCommitScheduler());
/// ```
///
/// While frames are disabled (the app is hidden or paused) commits run on a
/// microtask instead, so a backgrounded app does not hold a queue of writes
/// that persistence never sees. That includes commits queued behind a frame
/// callback that was registered just before the app was hidden: the pending
/// batch is flushed on a microtask and the frame callback, when frames resume,
/// finds nothing left to do.
///
/// Every commit of a batch runs even when one throws. The first error is
/// rethrown once the whole batch has run.
CommitScheduler frameCommitScheduler({SchedulerBinding? binding}) =>
    _FrameCommitScheduler(binding);

final class _FrameCommitScheduler implements CommitScheduler {
  _FrameCommitScheduler(this._binding);

  final SchedulerBinding? _binding;
  final List<void Function()> _pending = [];
  bool _frameQueued = false;
  bool _microtaskQueued = false;

  @override
  void schedule(void Function() commit) {
    _pending.add(commit);

    final binding = _binding ?? SchedulerBinding.instance;
    if (!binding.framesEnabled) {
      if (_microtaskQueued) return;
      _microtaskQueued = true;
      scheduleMicrotask(() {
        _microtaskQueued = false;
        _flush();
      });
      return;
    }

    if (_frameQueued) return;
    _frameQueued = true;
    binding.scheduleFrameCallback((_) {
      _frameQueued = false;
      _flush();
    });
  }

  void _flush() {
    final batch = List.of(_pending);
    _pending.clear();
    Object? firstError;
    StackTrace? firstStack;
    for (final commit in batch) {
      try {
        commit();
      } catch (error, stack) {
        if (firstError == null) {
          firstError = error;
          firstStack = stack;
        }
      }
    }
    if (firstError != null) {
      Error.throwWithStackTrace(firstError, firstStack!);
    }
  }
}

/// A [FocusSignal] driven by [AppLifecycleListener]: `false` when the app is
/// hidden, `true` when it is resumed.
///
/// The listener is created when the first subscriber arrives and disposed
/// when the last one leaves, so an uninstalled signal holds nothing on the
/// binding.
final class AppLifecycleFocusSignal implements FocusSignal {
  /// Creates a signal over [binding], or [WidgetsBinding.instance] when null.
  AppLifecycleFocusSignal({this._binding});

  final WidgetsBinding? _binding;
  AppLifecycleListener? _listener;

  late final StreamController<bool> _controller =
      StreamController<bool>.broadcast(
        onListen: _start,
        onCancel: _stop,
        sync: true,
      );

  @override
  Stream<bool> get focused => _controller.stream;

  void _start() {
    _listener = AppLifecycleListener(
      binding: _binding,
      onResume: () => _controller.add(true),
      onHide: () => _controller.add(false),
    );
  }

  void _stop() {
    _listener?.dispose();
    _listener = null;
  }
}

/// A [ConnectivitySignal] over `connectivity_plus`.
///
/// It reports transitions only: `false` when the device goes offline, `true`
/// when it comes back. Subscribing never triggers a revalidation by itself,
/// and switching from Wi-Fi to mobile data while staying online reports
/// nothing.
///
/// `onConnectivityChanged` has no initial emission, so with the default
/// stream the baseline is read from `Connectivity().checkConnectivity()`.
/// An app that launches offline therefore reports `true` on its first
/// connection. Events that arrive before that read completes are held and
/// replayed against it. If the read fails, the first event is the baseline
/// and is reported only when it is offline.
///
/// With an injected [changes] stream there is no seed unless [initial] is
/// given, and the first event is the baseline.
final class ConnectivityPlusSignal implements ConnectivitySignal {
  /// Creates a signal over [changes], or `Connectivity().onConnectivityChanged`
  /// when null. [initial] reads the starting state; it defaults to
  /// `Connectivity().checkConnectivity()` when [changes] is null and to no
  /// seed when [changes] is given.
  ConnectivityPlusSignal({this._changes, this._initial});

  final Stream<List<ConnectivityResult>>? _changes;
  final Future<List<ConnectivityResult>> Function()? _initial;

  @override
  Stream<bool> get online {
    final changes = _changes;
    final source = changes ?? Connectivity().onConnectivityChanged;
    final seed =
        _initial ?? (changes == null ? Connectivity().checkConnectivity : null);

    return Stream<bool>.multi((controller) {
      bool? last;
      var seeding = seed != null;
      var cancelled = false;
      final held = <bool>[];

      void handle(bool now) {
        final previous = last;
        last = now;
        if (previous == null ? !now : previous != now) controller.addSync(now);
      }

      final subscription = source
          .map(_isOnline)
          .listen(
            (now) => seeding ? held.add(now) : handle(now),
            onError: controller.addErrorSync,
            onDone: controller.closeSync,
          );
      controller.onCancel = () {
        cancelled = true;
        return subscription.cancel();
      };

      if (seed != null) {
        Future<List<ConnectivityResult>>.sync(seed)
            .then<bool?>(_isOnline)
            .catchError((Object _) => null)
            .then((now) {
              if (cancelled) return;
              seeding = false;
              if (now != null) handle(now);
              held.forEach(handle);
              held.clear();
            });
      }
    });
  }

  static bool _isOnline(List<ConnectivityResult> results) =>
      results.any((result) => result != ConnectivityResult.none);
}

final Expando<_Installation> _installations = Expando<_Installation>(
  'forge_client_flutter seams',
);

final class _Installation {
  _Installation(this._uninstallers);

  final List<void Function()> _uninstallers;
  int count = 0;

  void remove() {
    for (final uninstall in _uninstallers) {
      uninstall();
    }
  }
}

/// Installs focus and reconnect revalidation on [cache] and returns the
/// uninstaller.
///
/// Ref-counted per cache: a second call for the same cache shares the first
/// installation (and its signals) and the seams are removed when the last
/// caller releases. Calling an uninstaller twice is a no-op. [ForgeScope] and
/// `forge_client_riverpod`'s installed client both call this, which is why
/// the two adapters revalidate identically.
///
/// [focus] defaults to [AppLifecycleFocusSignal] and [connectivity] to
/// [ConnectivityPlusSignal]. Frame commits are not installed here: pass
/// [frameCommitScheduler] when the cache is constructed.
void Function() installFlutterSeams(
  QueryCache cache, {
  FocusSignal? focus,
  ConnectivitySignal? connectivity,
  Duration focusThrottle = const Duration(seconds: 5),
}) {
  final installation = _installations[cache] ??= _Installation([
    revalidateOnFocus(
      cache,
      focus ?? AppLifecycleFocusSignal(),
      throttle: focusThrottle,
    ),
    revalidateOnReconnect(cache, connectivity ?? ConnectivityPlusSignal()),
  ]);
  installation.count++;

  var released = false;
  return () {
    if (released) return;
    released = true;
    installation.count--;
    if (installation.count > 0) return;
    _installations[cache] = null;
    installation.remove();
  };
}

/// Whether [installFlutterSeams] currently has seams installed on [cache].
bool flutterSeamsInstalled(QueryCache cache) => _installations[cache] != null;
