/// Local servers for the end-to-end stream tests: SSE over shelf, WebSocket
/// over `dart:io`.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:shelf/shelf.dart';
import 'package:shelf/shelf_io.dart' as shelf_io;

/// Poll [condition] until it holds, failing after [timeout].
Future<void> until(
  bool Function() condition, {
  Duration timeout = const Duration(seconds: 5),
}) async {
  final deadline = DateTime.now().add(timeout);

  while (!condition()) {
    if (DateTime.now().isAfter(deadline)) {
      throw TimeoutException('condition not met', timeout);
    }

    await Future<void>.delayed(const Duration(milliseconds: 5));
  }
}

/// An SSE endpoint the test writes by hand, one body per connection.
final class SseTestServer {
  late HttpServer _server;

  /// Each request's headers, lower-cased, in arrival order.
  final List<Map<String, String>> requests = [];

  /// Each connection's body, in arrival order.
  final List<StreamController<List<int>>> bodies = [];

  /// The status to answer with. Anything but 200 sends no stream.
  int status = 200;

  /// The CORS origin to allow, for a browser test. Null sends no header.
  String? allowOrigin;

  /// Bind to a free loopback port.
  Future<void> start() async {
    _server = await shelf_io.serve(_handle, InternetAddress.loopbackIPv4, 0);
  }

  Response _handle(Request request) {
    requests.add(request.headers);

    final cors = {'access-control-allow-origin': ?allowOrigin};

    if (status != 200) return Response(status, body: 'refused', headers: cors);

    final body = StreamController<List<int>>();
    bodies.add(body);

    // A comment first, so the response headers go out before any event.
    body.add(utf8.encode(': open\n\n'));

    return Response.ok(
      body.stream,
      headers: {
        'content-type': 'text/event-stream',
        'cache-control': 'no-cache',
        ...cors,
      },
      context: {'shelf.io.buffer_output': false},
    );
  }

  /// Where the server listens.
  Uri get url => Uri.parse('http://127.0.0.1:${_server.port}');

  /// Wait until [count] connections have arrived.
  Future<void> connections(int count) => until(() => bodies.length >= count);

  /// Write raw SSE text to connection [index].
  void send(int index, String text) => bodies[index].add(utf8.encode(text));

  /// End connection [index] from the server side.
  Future<void> end(int index) => bodies[index].close();

  /// Close everything.
  Future<void> stop() async {
    for (final body in bodies) {
      if (!body.isClosed) unawaited(body.close());
    }

    await _server.close(force: true);
  }
}

/// A WebSocket endpoint over `dart:io`, recording what clients send.
final class WsTestServer {
  late HttpServer _server;

  /// Every accepted socket, in order.
  final List<WebSocket> sockets = [];

  /// Every handshake's request headers, in order.
  final List<HttpHeaders> handshakes = [];

  /// Every JSON message received from any client, decoded, in order.
  final List<Object?> received = [];

  /// Bind to a free loopback port.
  Future<void> start() async {
    _server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    _server.listen((request) async {
      handshakes.add(request.headers);

      final socket = await WebSocketTransformer.upgrade(request);
      sockets.add(socket);
      socket.listen(
        (Object? data) => received.add(jsonDecode(data! as String)),
      );
    });
  }

  /// Where the server listens, as http; the client converts the scheme.
  Uri get url => Uri.parse('http://127.0.0.1:${_server.port}');

  /// Send one JSON message on socket [index].
  void send(int index, Object? message) =>
      sockets[index].add(jsonEncode(message));

  /// Close everything.
  Future<void> stop() async {
    for (final socket in sockets) {
      await socket.close();
    }

    await _server.close(force: true);
  }
}
