/// Sockets the test drives by hand. Port of the socket half of
/// `packages/client-core/__tests__/harness.ts`.
///
/// No `WebSocket`, no `EventSource`, no server. Every message and every drop
/// is a method call, so a reconnect test is never a sleep with an assertion
/// after it.
library;

import 'dart:async';

import 'package:forge_client/forge_client.dart';

/// One connection the test controls.
class FakeConnection implements StreamConnection {
  /// Opened for [context]. With an [opener], the handshake waits for [open].
  FakeConnection(this.context, {this._opener});

  /// What the manager asked for.
  final StreamConnectContext context;

  final Completer<StreamConnection>? _opener;
  final StreamController<Object?> _messages = StreamController<Object?>();
  final Completer<void> _closed = Completer<void>();

  /// Everything the manager sent back up this connection, in order.
  final List<Object?> sent = [];

  /// Whether the connection is gone, asked for or not.
  bool isClosed = false;

  @override
  Stream<Object?> get messages => _messages.stream;

  @override
  Future<void> get closed => _closed.future;

  @override
  void send(Object? message) => sent.add(message);

  @override
  Future<void> close() async {
    isClosed = true;
    _finish();
  }

  /// The transport finished its handshake. A no-op when auto-opened.
  void open() {
    final opener = _opener;

    if (opener != null && !opener.isCompleted) opener.complete(this);
  }

  /// Push one message to whoever subscribed.
  void deliver(Object? message) => _messages.add(message);

  /// A transport-level error, which is not a close.
  void fail(Object error) => _messages.addError(error);

  /// The socket went away without being asked to.
  ///
  /// Before the handshake that is a failed connect, which is what a real
  /// WebSocket reports when it drops while still connecting.
  void drop([Object? reason]) {
    isClosed = true;

    final opener = _opener;

    if (opener != null && !opener.isCompleted) {
      opener.completeError(StateError('dropped before open: $reason'));

      return;
    }

    _finish();
  }

  void _finish() {
    if (!_closed.isCompleted) _closed.complete();
  }
}

/// A connection that can only listen, like SSE.
class FakeReceiveOnlyConnection extends FakeConnection
    implements ReceiveOnlyConnection {
  /// Opened for [context].
  FakeReceiveOnlyConnection(super.context, {super.opener});

  @override
  void send(Object? message) =>
      throw UnsupportedError('this transport cannot send');
}

/// A `StreamConnect` that records every connection it hands out.
final class FakeSockets {
  /// With [autoOpen], each connection is open as soon as the connect future
  /// completes; without it, the test calls [FakeConnection.open].
  FakeSockets({this.autoOpen = true, this.receiveOnly = false, this.onConnect});

  /// Whether connections open without the test saying so.
  final bool autoOpen;

  /// Whether connections are [ReceiveOnlyConnection]s.
  final bool receiveOnly;

  /// Told about every connect, before the connection exists.
  final void Function(StreamConnectContext context)? onConnect;

  /// Every connection ever opened, in order.
  final List<FakeConnection> opened = [];

  /// The factory to hand a `SubscriptionManager`.
  Future<StreamConnection> connect(StreamConnectContext context) {
    onConnect?.call(context);

    final opener = autoOpen ? null : Completer<StreamConnection>();
    final connection = receiveOnly
        ? FakeReceiveOnlyConnection(context, opener: opener)
        : FakeConnection(context, opener: opener);

    opened.add(connection);

    return opener?.future ?? Future<StreamConnection>.value(connection);
  }

  /// The most recently opened connection, optionally for one endpoint.
  FakeConnection last([String? endpoint]) {
    final matching = endpoint == null
        ? opened
        : [
            for (final connection in opened)
              if (connection.context.endpoint == endpoint) connection,
          ];

    if (matching.isEmpty) {
      throw StateError('no connection opened for ${endpoint ?? 'any'}');
    }

    return matching.last;
  }

  /// How many are still open.
  int live() => opened.where((connection) => !connection.isClosed).length;
}

/// A connectivity signal the test emits on.
final class FakeConnectivity implements ConnectivitySignal {
  final StreamController<bool> _online = StreamController<bool>.broadcast(
    sync: true,
  );

  @override
  Stream<bool> get online => _online.stream;

  /// Whether anything is listening.
  bool get hooked => _online.hasListener;

  /// Report the network state.
  void emit(bool online) => _online.add(online);
}
