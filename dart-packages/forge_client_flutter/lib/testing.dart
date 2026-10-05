/// Test doubles for apps and packages that test against forge_client_flutter.
///
/// Nothing here touches a platform channel, a timer or the network, so a
/// widget test that uses these fakes runs entirely on the microtask queue and
/// the frames the test pumps.
library;

import 'dart:async';

import 'package:forge_client/forge_client.dart';

/// A [Transport] that answers from a handler on the microtask queue and
/// records every request it was given.
final class FakeTransport implements Transport {
  /// Creates a transport that answers every request with [handler].
  FakeTransport(this.handler);

  /// Produces the client-shaped response for the request numbered `call`
  /// (zero based), or throws to fail it.
  final FutureOr<Object?> Function(TransportRequest request, int call) handler;

  /// Every request executed, in order.
  final List<TransportRequest> calls = [];

  /// How many requests named [meta]'s operation.
  int countOf(OperationMeta meta) =>
      calls.where((request) => request.meta.id == meta.id).length;

  @override
  Future<Object?> execute(TransportRequest request) {
    final call = calls.length;
    calls.add(request);
    return Future<void>.value().then((_) => handler(request, call));
  }
}

/// A [FocusSignal] the test drives by hand.
final class FakeFocusSignal implements FocusSignal {
  final StreamController<bool> _controller =
      StreamController<bool>.broadcast(sync: true);

  @override
  Stream<bool> get focused => _controller.stream;

  /// Whether anything is currently listening, which is how a test proves an
  /// installer was installed and removed.
  bool get hasListener => _controller.hasListener;

  /// Reports that the app regained focus.
  void focus() => _controller.add(true);

  /// Reports that the app lost focus.
  void blur() => _controller.add(false);
}

/// A [ConnectivitySignal] the test drives by hand.
final class FakeConnectivitySignal implements ConnectivitySignal {
  final StreamController<bool> _controller =
      StreamController<bool>.broadcast(sync: true);

  @override
  Stream<bool> get online => _controller.stream;

  /// Whether anything is currently listening.
  bool get hasListener => _controller.hasListener;

  /// Reports that the device came back online.
  void goOnline() => _controller.add(true);

  /// Reports that the device went offline.
  void goOffline() => _controller.add(false);
}
