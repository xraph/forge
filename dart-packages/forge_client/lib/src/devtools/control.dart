/// The conditions half of the panel. Port of `client-devtools/src/control.ts`.
///
/// A decorator rather than an observer: offline has to fail before the inner
/// transport, or the request still reaches the server. It wraps outside the
/// retry loop, so latency is a delay on the operation, not on each attempt,
/// and offline fails the operation once rather than once per retry.
///
/// Placement: wrap the `RestTransport` itself, and put an `OutboxTransport`
/// (when there is one) outside it, never the other way round. A drain then goes
/// through the simulator, so it feels the simulated network. Wrapped outside
/// the outbox, offline would throw before the outbox saw the write.
///
/// An outbox that queues offline writes must also be given
/// [withSimulatedConnectivity] as its connectivity signal. [SimulatedOffline]
/// is an `http.ClientException`, which the outbox reads as an uncertain
/// outcome ("the request may have reached the server") because forge_client
/// has no failure kind that says "never sent" for it to honour. With the
/// signal wired the outbox knows the device is offline and holds the write; with
/// no signal, a non-idempotent write (a PATCH or POST) fails as uncertain
/// instead of queuing, though the simulator never let it out.
///
/// Principal changes: an armed failure belongs to the principal who armed it,
/// and so does a request waiting out simulated latency. `Devtools` calls
/// [ControlledTransport.principalChanged] synchronously when the principal
/// starts changing. The next principal's first request
/// never inherits the armed failure, and a request that was sleeping is aborted
/// with an `http.RequestAbortedException` instead of being sent under the new
/// principal's credentials. The outbox reads that as a cancellation: it rethrows
/// to the caller and stores nothing. The mode and the latency are the
/// developer's network, not any principal's data, so they persist across a
/// switch. Revalidation toggles persist for the same reason. A transport used
/// without `Devtools` is not told about principal changes.
///
/// Disposing the inspector is not a principal change: `Devtools.dispose` calls
/// [ControlledTransport.release]. Nothing is aborted, because the write that
/// was waiting belongs to a user who is still there, and aborting it would lose
/// it. Requests waiting out latency go straight to the inner transport, and the
/// transport passes through from then on.
library;

import 'dart:async';

import 'package:http/http.dart' as http;

import '../freshness.dart';
import '../transport.dart';
import '../types.dart';

/// The network the panel imposes.
enum NetworkMode {
  /// Pass through.
  online,

  /// Add the slow delay before every request.
  slow,

  /// Fail every request before it leaves.
  offline,
}

/// Thrown by [ControlledTransport] while offline. The request was never sent.
final class SimulatedOffline extends http.ClientException {
  /// Creates the failure for [uri].
  SimulatedOffline(Uri? uri)
    : super('[forge] offline, switched on from the devtools panel', uri);
}

/// Wraps a transport with switchable offline, slow and fail-next conditions.
final class ControlledTransport implements Transport, ConnectivitySignal {
  /// Wraps [inner]. [sleep] is injected so tests do not wait.
  ControlledTransport(
    this.inner, {
    this._sleep = realSleep,
    this._slow = const Duration(milliseconds: 400),
  });

  /// The transport requests go to when allowed.
  final Transport inner;

  final Sleep _sleep;
  final Duration _slow;
  final StreamController<bool> _online = StreamController<bool>.broadcast(
    sync: true,
  );
  final List<void Function()> _changes = [];
  bool _dispatching = false;
  NetworkMode _mode = NetworkMode.online;
  int? _next;
  int _epoch = 0;
  bool _released = false;
  final Completer<void> _release = Completer<void>();

  /// Extra delay before every request. Not cleared by a principal change.
  Duration latency = Duration.zero;

  /// The current mode.
  NetworkMode get mode => _mode;

  /// Sets the mode. Entering or leaving offline is reported on [online]
  /// synchronously, before this returns: a write sent on the next line must
  /// find the outbox already knowing the network is gone, or it fails as an
  /// uncertain outcome instead of queuing.
  ///
  /// A change requested from inside a listener is not lost and does not
  /// interrupt the dispatch in progress: it is applied, and reported, once
  /// that dispatch has finished. A listener that throws is reported to its
  /// zone like any stream listener's error and does not stop the others.
  set mode(NetworkMode value) {
    _change(() {
      final wasOnline = isOnline;
      _mode = value;
      _announce(wasOnline);
    });
  }

  /// Applies [change] now, or after the dispatch under way when called from a
  /// listener, so connectivity reports never nest and arrive in order.
  void _change(void Function() change) {
    _changes.add(change);
    if (_dispatching) return;

    _dispatching = true;
    try {
      while (_changes.isNotEmpty) {
        _changes.removeAt(0)();
      }
    } finally {
      _dispatching = false;
    }
  }

  /// Reports [isOnline] if it differs from [wasOnline].
  void _announce(bool wasOnline) {
    if (wasOnline != isOnline && !_online.isClosed) _online.add(isOnline);
  }

  /// Whether the simulated network is up. Slow counts as up.
  bool get isOnline => _released || _mode != NetworkMode.offline;

  @override
  Stream<bool> get online => _online.stream;

  /// Whether a synthetic failure is waiting to be spent.
  bool get armed => _next != null;

  /// The status the next request will fail with, when armed.
  int? get armedStatus => _next;

  /// Fails the next request, once, with [status].
  void failNext([int status = 500]) => _next = status;

  /// Gives up on the armed failure without spending it. `Devtools` calls this
  /// when the principal starts changing.
  void disarm() => _next = null;

  /// Tells the transport the principal is changing. Disarms the armed failure
  /// and aborts every request still waiting out simulated latency, so none is
  /// sent afterwards. The mode and the latency stay. `Devtools` calls this
  /// synchronously from the principal-changing notification.
  void principalChanged() {
    _epoch++;
    disarm();
  }

  /// Stops simulating for good: disarms, releases every request waiting out
  /// latency straight to [inner] (the principal is unchanged, so it is still
  /// the user's request), and passes through from now on, with no delay and no
  /// simulated offline. If the mode was offline, [online] reports the network
  /// back, synchronously, so a held write can drain. `Devtools` calls this on
  /// dispose.
  void release() {
    if (_released) return;

    final wasOnline = isOnline;
    _released = true;
    disarm();
    _release.complete();
    _change(() => _announce(wasOnline));
  }

  @override
  Future<Object?> execute(TransportRequest request) async {
    if (_released) return inner.execute(request);

    final uri = Uri.tryParse(request.meta.path);

    if (_mode == NetworkMode.offline) throw SimulatedOffline(uri);

    final armedStatus = _next;
    if (armedStatus != null) {
      _next = null;
      throw HttpStatusError(armedStatus, {
        'message':
            '[forge] synthetic failure $armedStatus, armed from the devtools panel',
      });
    }

    final delay = latency + (_mode == NetworkMode.slow ? _slow : Duration.zero);
    if (delay > Duration.zero) {
      final epoch = _epoch;
      await Future.any([_sleep(delay), _release.future]);

      // Released while it waited: the simulator is gone, the request is not.
      if (_released) return inner.execute(request);

      // A request that began under one principal is never sent under the next.
      if (epoch != _epoch) throw http.RequestAbortedException(uri);
      // The network went away while it waited.
      if (_mode == NetworkMode.offline) throw SimulatedOffline(uri);
    }

    return inner.execute(request);
  }

  /// The state the panel shows.
  Json toJson() => {
    'mode': _mode.name,
    'latencyMs': latency.inMilliseconds,
    'slowMs': _slow.inMilliseconds,
    'armed': armed,
    'armedStatus': _next,
  };

  /// Closes the connectivity stream.
  Future<void> dispose() => _online.close();
}

/// The three sources of a refetch nobody asked for.
enum RevalidationSource {
  /// `revalidateOnFocus`.
  focus,

  /// `revalidateOnReconnect`.
  reconnect,

  /// `poll`.
  poll,
}

/// Starts one revalidation source and returns how to stop it: the shape
/// `revalidateOnFocus`, `revalidateOnReconnect` and `poll` already have.
typedef StartRevalidation = void Function() Function();

/// The revalidation toggles. The application registers how to start each
/// source it uses; the panel toggles them and never installs one itself.
final class Revalidation {
  /// Registers [sources]. Nothing starts until toggled.
  Revalidation(Map<RevalidationSource, StartRevalidation> sources)
    : _sources = Map.of(sources);

  final Map<RevalidationSource, StartRevalidation> _sources;
  final Map<RevalidationSource, void Function()> _running = {};

  /// Whether the application wired [source].
  bool registered(RevalidationSource source) => _sources.containsKey(source);

  /// Whether [source] is running.
  bool enabled(RevalidationSource source) => _running.containsKey(source);

  /// Starts [source] if off, stops it if on. A no-op if unregistered.
  void toggle(RevalidationSource source) {
    final stop = _running.remove(source);
    if (stop != null) {
      stop();
      return;
    }
    final start = _sources[source];
    if (start != null) _running[source] = start();
  }

  /// Stops everything this started.
  void dispose() {
    for (final stop in _running.values) {
      stop();
    }
    _running.clear();
  }

  /// Per source, whether it is registered and running.
  Json toJson() => {
    for (final source in RevalidationSource.values)
      source.name: {
        'registered': registered(source),
        'enabled': enabled(source),
      },
  };
}

/// A connectivity signal that is offline when either [real] or [controls] is.
/// Feed this to anything that takes a `ConnectivitySignal` (an outbox,
/// `revalidateOnReconnect`) so the panel's offline switch behaves like a lost
/// network.
ConnectivitySignal withSimulatedConnectivity(
  ConnectivitySignal real,
  ControlledTransport controls,
) => _MergedConnectivity(real, controls);

final class _MergedConnectivity implements ConnectivitySignal {
  _MergedConnectivity(this._real, this._controls);

  final ConnectivitySignal _real;
  final ControlledTransport _controls;

  @override
  Stream<bool> get online {
    late final StreamController<bool> controller;
    StreamSubscription<bool>? real;
    StreamSubscription<bool>? simulated;
    var realOnline = true;
    bool? last;

    void emit() {
      final value = realOnline && _controls.isOnline;
      if (value != last) {
        last = value;
        controller.add(value);
      }
    }

    // Synchronous, so the offline switch reaches the outbox before the setter
    // that flipped it returns. An async controller would hand the report on
    // by a microtask and reopen the window the simulator closes.
    controller = StreamController<bool>(
      sync: true,
      onListen: () {
        real = _real.online.listen((value) {
          realOnline = value;
          emit();
        });
        simulated = _controls.online.listen((_) => emit());
        if (!_controls.isOnline) emit();
      },
      onCancel: () async {
        await real?.cancel();
        await simulated?.cancel();
      },
    );

    return controller.stream;
  }
}
