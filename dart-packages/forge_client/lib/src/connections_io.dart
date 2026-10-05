/// Native stream connections: WebSocket through `web_socket_channel`, SSE over
/// a streamed `package:http` response parsed in Dart, and no WebTransport.
library;

import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:web_socket_channel/io.dart';

import 'sse.dart';
import 'stream.dart';
import 'transport.dart' show HttpStatusError;
import 'ws_connection.dart';

/// Opens WebSockets with `IOWebSocketChannel`, sending the context's headers.
///
/// [pingInterval] adds WebSocket control pings, which some proxies need. The
/// Forge keepalive is answered by the manager either way.
StreamConnect webSocketConnection({Duration? pingInterval}) => (context) async {
  final channel = IOWebSocketChannel.connect(
    webSocketUri(context.url),
    headers: context.headers,
    pingInterval: pingInterval,
  );

  await channel.ready;

  return WebSocketStreamConnection(channel);
};

/// Opens SSE streams over [client], or a client of its own per connection.
///
/// Each event is delivered as `{'event': name, 'data': json, 'id': lastId}`,
/// the shape TS `eventSourceConnection` produces. With [events] non-empty only
/// those names, the control events and `message` are delivered, matching what
/// a browser `EventSource` with those listeners would see. The last dispatched
/// id is remembered per url and principal, and sent as `Last-Event-ID` on the
/// next connect for the same pair. As in a browser, an id-only event still
/// updates it, an empty `id:` clears it, and a frame sent before any id carries
/// `''`.
StreamConnect eventSourceConnection({
  http.Client? client,
  Iterable<String> events = const [],
}) {
  final wanted = {...events};
  final lastIds = <(String, String?), String>{};

  return (context) async {
    final owned = client == null;
    final http.Client sender = client ?? http.Client();
    final key = (context.url.toString(), context.principal);
    final request = http.Request('GET', context.url)
      ..headers.addAll(context.headers)
      ..headers['accept'] = 'text/event-stream'
      ..headers['cache-control'] = 'no-cache';

    if (lastIds[key] case final id? when id.isNotEmpty) {
      request.headers['last-event-id'] = id;
    }

    final http.StreamedResponse response;

    try {
      response = await sender.send(request);
    } on Object {
      if (owned) sender.close();

      rethrow;
    }

    if (response.statusCode < 200 || response.statusCode > 299) {
      final body = await response.stream.bytesToString().catchError(
        (Object _) => '',
      );

      if (owned) sender.close();

      throw HttpStatusError(
        response.statusCode,
        body,
        headers: response.headers,
      );
    }

    return _SseConnection(
      response.stream,
      owned ? sender.close : null,
      wanted: wanted,
      onId: (id) {
        if (id.isEmpty) {
          lastIds.remove(key);
        } else {
          lastIds[key] = id;
        }
      },
    );
  };
}

/// Always fails with [TransportUnavailable]: Dart has no native WebTransport.
/// Put a WebSocket or SSE factory after it in [fallbackConnection].
StreamConnect webTransportConnection() =>
    (context) async =>
        throw TransportUnavailable(context.endpoint, 'webtransport');

final class _SseConnection implements ReceiveOnlyConnection {
  _SseConnection(
    Stream<List<int>> body,
    this._release, {
    required Set<String> wanted,
    required void Function(String id) onId,
  }) {
    _subscription = body
        .transform(utf8.decoder)
        .transform(SseParser(onLastEventId: onId))
        .listen(
          (event) {
            if (wanted.isNotEmpty &&
                !wanted.contains(event.event) &&
                !streamControlEvents.contains(event.event) &&
                event.event != 'message') {
              return;
            }

            final Object? data;

            try {
              data = jsonDecode(event.data);
            } on FormatException catch (error) {
              _messages.addError(error);

              return;
            }

            _messages.add({
              'event': event.event,
              'data': data,
              'id': event.lastEventId ?? '',
            });
          },
          onError: (Object error) {
            // One report per drop: the first error ends the stream.
            if (_closing || _closed.isCompleted) return;

            _messages.addError(error);
            _finish();
            unawaited(_subscription.cancel());
          },
          onDone: _finish,
        );
  }

  final void Function()? _release;
  late final StreamSubscription<SseEvent> _subscription;
  final StreamController<Object?> _messages = StreamController<Object?>();
  final Completer<void> _closed = Completer<void>();
  bool _closing = false;

  @override
  Stream<Object?> get messages => _messages.stream;

  @override
  Future<void> get closed => _closed.future;

  @override
  void send(Object? message) =>
      throw UnsupportedError('[forge] an SSE stream cannot send');

  @override
  Future<void> close() async {
    _closing = true;
    await _subscription.cancel();
    _finish();
  }

  void _finish() {
    if (_closed.isCompleted) return;

    _closed.complete();
    unawaited(_messages.close());
    _release?.call();
  }
}
