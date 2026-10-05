/// A [StreamConnection] over a `WebSocketChannel`, shared by both platforms.
library;

import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

import 'stream.dart';

/// Adapts an open [WebSocketChannel]. Text frames are JSON-decoded; any other
/// frame is passed through. A text frame that is not JSON is reported on
/// [messages] as an error and dropped.
final class WebSocketStreamConnection implements StreamConnection {
  /// Wraps [channel], which must already be ready.
  WebSocketStreamConnection(WebSocketChannel channel)
    : this.over(channel.stream, channel.sink);

  /// Wraps a socket's incoming [stream] and outgoing [sink]. What the adapter
  /// tests drive, with no `WebSocketChannel` behind it.
  WebSocketStreamConnection.over(Stream<Object?> stream, this._sink) {
    _subscription = stream.listen(
      (data) {
        final Object? message;

        try {
          message = data is String ? jsonDecode(data) : data;
        } on FormatException catch (error) {
          _messages.addError(error);

          return;
        }

        _messages.add(message);
      },
      onError: (Object error) => _messages.addError(error),
      onDone: _finish,
    );
  }

  final StreamSink<Object?> _sink;
  late final StreamSubscription<Object?> _subscription;
  final StreamController<Object?> _messages = StreamController<Object?>();
  final Completer<void> _closed = Completer<void>();

  @override
  Stream<Object?> get messages => _messages.stream;

  /// Completes once, when the peer closes the socket or [close] is called.
  @override
  Future<void> get closed => _closed.future;

  /// A string is sent as is; anything else is JSON-encoded.
  @override
  void send(Object? message) =>
      _sink.add(message is String ? message : jsonEncode(message));

  /// Closes the sink, waiting at most five seconds. [closed] completes even
  /// when the sink's close throws; the error is rethrown.
  @override
  Future<void> close() async {
    try {
      await _sink.close().timeout(
        const Duration(seconds: 5),
        onTimeout: () => null,
      );
    } finally {
      _finish();
    }
  }

  void _finish() {
    if (_closed.isCompleted) return;

    _closed.complete();

    // A frame that arrives after this must not reach the closed controller.
    unawaited(_subscription.cancel());
    unawaited(_messages.close());
  }
}

/// The WebSocket form of an http(s) url: `ws` for `http`, `wss` for `https`.
Uri webSocketUri(Uri url) => switch (url.scheme) {
  'http' => url.replace(scheme: 'ws'),
  'https' => url.replace(scheme: 'wss'),
  _ => url,
};
