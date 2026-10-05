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
/// that persistence never sees. A frame callback already scheduled when the
/// app is hidden runs when frames resume.
CommitScheduler frameCommitScheduler({SchedulerBinding? binding}) =>
    _FrameCommitScheduler(binding);

final class _FrameCommitScheduler implements CommitScheduler {
  _FrameCommitScheduler(this._binding);

  final SchedulerBinding? _binding;
  final List<void Function()> _pending = [];
  bool _scheduled = false;

  @override
  void schedule(void Function() commit) {
    _pending.add(commit);
    if (_scheduled) return;
    _scheduled = true;

    final binding = _binding ?? SchedulerBinding.instance;
    if (!binding.framesEnabled) {
      scheduleMicrotask(_flush);
      return;
    }
    binding.scheduleFrameCallback((_) => _flush());
  }

  void _flush() {
    _scheduled = false;
    final batch = List.of(_pending);
    _pending.clear();
    for (final commit in batch) {
      commit();
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

  late final StreamController<bool> _controller = StreamController<bool>.broadcast(
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
/// when it comes back. The first event sets the baseline and is reported only
/// when the device starts offline, so subscribing never triggers a
/// revalidation by itself, and switching from Wi-Fi to mobile data while
/// staying online reports nothing.
final class ConnectivityPlusSignal implements ConnectivitySignal {
  /// Creates a signal over [changes], or `Connectivity().onConnectivityChanged`
  /// when null.
  ConnectivityPlusSignal({this._changes});

  final Stream<List<ConnectivityResult>>? _changes;

  @override
  Stream<bool> get online {
    bool? last;
    return (_changes ?? Connectivity().onConnectivityChanged)
        .map(_isOnline)
        .where((now) {
          final previous = last;
          last = now;
          return previous == null ? !now : previous != now;
        });
  }

  static bool _isOnline(List<ConnectivityResult> results) =>
      results.any((result) => result != ConnectivityResult.none);
}

final Expando<_Installation> _installations =
    Expando<_Installation>('forge_client_flutter seams');

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
    revalidateOnFocus(cache, focus ?? AppLifecycleFocusSignal(), throttle: focusThrottle),
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
