/// Browser stream connections: WebSocket through `web_socket_channel`,
/// `EventSource` through `package:web`, WebTransport through `dart:js_interop`.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'package:http/http.dart' as http;
import 'package:web/web.dart' as web;
import 'package:web_socket_channel/web_socket_channel.dart';

import 'stream.dart';
import 'ws_connection.dart';

/// Opens browser WebSockets. [pingInterval] and the context's headers are
/// ignored: a browser socket cannot set either.
StreamConnect webSocketConnection({Duration? pingInterval}) => (context) async {
  final channel = WebSocketChannel.connect(webSocketUri(context.url));

  await channel.ready;

  return WebSocketStreamConnection(channel);
};

/// Opens a browser `EventSource` listening to [events], the control events
/// and `message`, delivering `{'event': name, 'data': json, 'id': lastId}`.
///
/// The frame is the one the native factory produces. A browser keeps the last
/// event id itself, sets it on every dispatched event (so an id-only event
/// updates it, an empty `id:` clears it and nothing has set it yet reads as
/// `''`), and sends `Last-Event-ID` on its own, so `lastEventId` is used as is.
///
/// A browser can only hear named events it has a listener for, so unlike the
/// native factory an empty [events] does not mean every event: only the
/// control events and `message` arrive. [client] and the context's headers are
/// ignored. The source's own retry is not used: an error closes it and reports
/// a drop, so the manager reconnects on its backoff and the binder recovers the
/// gap, as TS does.
StreamConnect eventSourceConnection({
  http.Client? client,
  Iterable<String> events = const [],
}) {
  final names = {...events, ...streamControlEvents, 'message'};

  return (context) async {
    final connection = _EventSourceConnection(
      web.EventSource(context.url.toString()),
      names,
    );

    await connection.opened;

    return connection;
  };
}

/// Opens a browser `WebTransport` session and reads its datagrams as UTF-8
/// JSON. Throws [TransportUnavailable] where the browser has none.
StreamConnect webTransportConnection() => (context) async {
  if (globalContext['WebTransport'].isUndefinedOrNull) {
    throw TransportUnavailable(context.endpoint, 'webtransport');
  }

  final transport = _WebTransport(context.url.toString());

  try {
    await transport.ready.toDart;
  } on Object {
    // A session that never became ready rejects `closed` as well; nothing
    // listens to it, so absorb it rather than leave an unhandled rejection.
    unawaited(
      transport.closed.toDart.then<void>((_) {}, onError: (Object _) {}),
    );
    transport.close();

    rethrow;
  }

  return _WebTransportConnection(transport);
};

final class _EventSourceConnection implements ReceiveOnlyConnection {
  _EventSourceConnection(this._source, Set<String> names) {
    for (final name in names) {
      final listener = ((web.MessageEvent event) => _receive(name, event)).toJS;

      _listeners[name] = listener;
      _source.addEventListener(name, listener);
    }

    _source.onopen = ((web.Event _) {
      if (!_opened.isCompleted) _opened.complete();
    }).toJS;
    _source.onerror = ((web.Event _) => _failed()).toJS;
  }

  final web.EventSource _source;
  final Map<String, JSFunction> _listeners = {};
  final Completer<void> _opened = Completer<void>();
  final StreamController<Object?> _messages = StreamController<Object?>();
  final Completer<void> _closed = Completer<void>();
  bool _over = false;

  Future<void> get opened => _opened.future;

  @override
  Stream<Object?> get messages => _messages.stream;

  @override
  Future<void> get closed => _closed.future;

  @override
  void send(Object? message) =>
      throw UnsupportedError('[forge] an EventSource cannot send');

  @override
  Future<void> close() async {
    _over = true;
    _finish();
  }

  void _receive(String name, web.MessageEvent event) {
    final text = event.data.dartify();

    if (text is! String) return;

    final Object? data;

    try {
      data = jsonDecode(text);
    } on FormatException catch (error) {
      _messages.addError(error);

      return;
    }

    _messages.add({'event': name, 'data': data, 'id': event.lastEventId});
  }

  void _failed() {
    if (_over) return;

    _over = true;

    if (!_opened.isCompleted) {
      _opened.completeError(
        StateError('[forge] EventSource could not connect to ${_source.url}'),
      );
    } else {
      _messages.addError(
        StateError('[forge] EventSource error on ${_source.url}'),
      );
    }

    _finish();
  }

  // Ends the source, drops every listener and handler, and closes the stream.
  void _finish() {
    if (_closed.isCompleted) return;

    _source.close();
    for (final MapEntry(:key, :value) in _listeners.entries) {
      _source.removeEventListener(key, value);
    }
    _listeners.clear();
    _source.onopen = null;
    _source.onerror = null;

    _closed.complete();
    unawaited(_messages.close());
  }
}

@JS('WebTransport')
extension type _WebTransport._(JSObject _) implements JSObject {
  external factory _WebTransport(String url);
  external JSPromise<JSAny?> get ready;
  external JSPromise<JSAny?> get closed;
  external _Datagrams get datagrams;
  external void close();
}

extension type _Datagrams._(JSObject _) implements JSObject {
  external _Readable get readable;
}

extension type _Readable._(JSObject _) implements JSObject {
  external _Reader getReader();
}

extension type _Reader._(JSObject _) implements JSObject {
  external JSPromise<_ReadResult> read();
  external void releaseLock();
}

extension type _ReadResult._(JSObject _) implements JSObject {
  external bool get done;
  external JSUint8Array? get value;
}

final class _WebTransportConnection implements ReceiveOnlyConnection {
  _WebTransportConnection(this._transport) {
    unawaited(
      _transport.closed.toDart.then<void>(
        (_) => _finish(),
        onError: (Object error) {
          if (!_over) _messages.addError(error);
          _finish();
        },
      ),
    );

    // Started now: the controller buffers until the manager listens, so no
    // datagram read before then is lost.
    unawaited(_read());
  }

  final _WebTransport _transport;
  final StreamController<Object?> _messages = StreamController<Object?>();
  final Completer<void> _closed = Completer<void>();
  bool _over = false;

  @override
  Stream<Object?> get messages => _messages.stream;

  @override
  Future<void> get closed => _closed.future;

  @override
  void send(Object? message) => throw UnsupportedError(
    '[forge] WebTransport datagrams are receive-only here',
  );

  @override
  Future<void> close() async {
    _over = true;
    _transport.close();
    _finish();
  }

  Future<void> _read() async {
    final reader = _transport.datagrams.readable.getReader();

    try {
      while (!_closed.isCompleted) {
        final next = await reader.read().toDart;

        // A datagram still queued when the session ended is dropped: the
        // controller is closed and the manager has been told.
        if (next.done || _closed.isCompleted) break;

        final bytes = next.value;

        if (bytes == null) continue;

        // One bad datagram is one bad packet, not the end of the session.
        try {
          _messages.add(jsonDecode(utf8.decode(bytes.toDart)));
        } on FormatException catch (error) {
          _messages.addError(error);
        }
      }
    } on Object catch (error) {
      if (!_over && !_messages.isClosed) _messages.addError(error);
    } finally {
      reader.releaseLock();
      _finish();
    }
  }

  void _finish() {
    if (_closed.isCompleted) return;

    _closed.complete();
    unawaited(_messages.close());
  }
}
