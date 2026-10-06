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
/// starts changing (and when it is disposed). The next principal's first request
/// never inherits the armed failure, and a request that was sleeping is aborted
/// with an `http.RequestAbortedException` instead of being sent under the new
/// principal's credentials. The outbox reads that as a cancellation: it rethrows
/// to the caller and stores nothing. The mode and the latency are the
/// developer's network, not any principal's data, so they persist across a
/// switch. Revalidation toggles persist for the same reason. A transport used
/// without `Devtools` is not told about principal changes.
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
  final StreamController<bool> _online = StreamController<bool>.broadcast();
  NetworkMode _mode = NetworkMode.online;
  int? _next;
  int _epoch = 0;

  /// Extra delay before every request. Not cleared by a principal change.
  Duration latency = Duration.zero;

  /// The current mode.
  NetworkMode get mode => _mode;

  /// Sets the mode. Entering or leaving offline is reported on [online].
  set mode(NetworkMode value) {
    final wasOnline = isOnline;
    _mode = value;
    if (wasOnline != isOnline && !_online.isClosed) _online.add(isOnline);
  }

  /// Whether the simulated network is up. Slow counts as up.
  bool get isOnline => _mode != NetworkMode.offline;

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

  /// Tells the transport the principal is changing (or the inspector is gone).
  /// Disarms the armed failure and aborts every request still waiting out
  /// simulated latency, so none is sent afterwards. The mode and the latency
  /// stay. `Devtools` calls this synchronously from the principal-changing
  /// notification.
  void principalChanged() {
    _epoch++;
    disarm();
  }

  @override
  Future<Object?> execute(TransportRequest request) async {
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
      await _sleep(delay);

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

    controller = StreamController<bool>(
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
