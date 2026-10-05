@TestOn('vm')
library;

import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/ws_connection.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:test/test.dart';

import 'support/harness.dart';

final class _Sink implements StreamSink<Object?> {
  final List<Object?> sent = [];
  int closes = 0;

  @override
  void add(Object? event) => sent.add(event);

  @override
  void addError(Object error, [StackTrace? stackTrace]) {}

  @override
  Future<void> addStream(Stream<Object?> stream) => stream.forEach(add);

  @override
  Future<void> close() async => closes++;

  @override
  Future<void> get done => Future<void>.value();
}

/// An SSE response whose body the test writes, and the request that asked
/// for it.
({
  StreamController<List<int>> body,
  List<http.BaseRequest> requests,
  http.Client client,
})
_sse() {
  final body = StreamController<List<int>>();
  final requests = <http.BaseRequest>[];
  final client = MockClient.streaming((request, _) async {
    requests.add(request);

    return http.StreamedResponse(body.stream, 200);
  });

  return (body: body, requests: requests, client: client);
}

final _context = StreamConnectContext(
  url: Uri.parse('http://api.test/sse/orders'),
  endpoint: '/sse/orders',
);

void main() {
  group('webSocketConnection', () {
    test('parses JSON text frames and passes anything else through', () async {
      final incoming = StreamController<Object?>();
      final connection = WebSocketStreamConnection.over(
        incoming.stream,
        _Sink(),
      );
      final seen = <Object?>[];

      connection.messages.listen(seen.add);
      incoming
        ..add('{"type":"order.created","payload":{"id":7}}')
        ..add({'already': 'parsed'});
      await settle();

      expect(seen, [
        {
          'type': 'order.created',
          'payload': {'id': 7},
        },
        {'already': 'parsed'},
      ]);
    });

    test('reports a frame that does not parse and keeps the socket', () async {
      final incoming = StreamController<Object?>();
      final sink = _Sink();
      final connection = WebSocketStreamConnection.over(incoming.stream, sink);
      final errors = <Object>[];
      var closed = false;

      connection.messages.listen((_) {}, onError: errors.add);
      unawaited(connection.closed.then((_) => closed = true));
      incoming.add('{not json');
      await settle();

      expect(errors, hasLength(1));
      expect(sink.closes, 0);
      expect(closed, isFalse);
    });

    test('encodes what it sends, and reports a peer close once', () async {
      final incoming = StreamController<Object?>();
      final sink = _Sink();
      final connection = WebSocketStreamConnection.over(incoming.stream, sink);
      var closes = 0;

      connection.messages.listen((_) {});
      unawaited(connection.closed.then((_) => closes++));
      connection
        ..send({'type': 'system', 'event': 'pong'})
        ..send('raw');
      await incoming.close();
      await settle();

      expect(sink.sent, ['{"type":"system","event":"pong"}', 'raw']);
      expect(closes, 1);
    });

    // Dart differs: `closed` is a future, so it completes on a close the
    // manager asked for too. What TS guards here, a reconnect after a
    // deliberate close, the manager prevents by ignoring a disposed socket's
    // close (stream_test.dart). The socket is still closed exactly once.
    test('does not report a close it asked for', () async {
      final incoming = StreamController<Object?>();
      final sink = _Sink();
      final connection = WebSocketStreamConnection.over(incoming.stream, sink);

      connection.messages.listen((_) {});
      await connection.close();
      await incoming.close();
      await settle();

      expect(sink.closes, 1);
    });
  });

  group('eventSourceConnection', () {
    test('listens for the named events and the control events, delivering decodable frames', () async {
      final sse = _sse();
      final connection = await eventSourceConnection(
        client: sse.client,
        events: ['order.created'],
      )(_context);
      final seen = <Object?>[];

      connection.messages.listen(seen.add);
      sse.body.add(
        utf8.encode(
          'id: 41\nevent: order.created\ndata: {"id":7}\n\n'
          'event: order.unbound\ndata: 1\n\n'
          'event: forge.gap\ndata: {"reason":"x"}\n\n',
        ),
      );
      await settle();

      expect(sse.requests.single.headers['accept'], 'text/event-stream');
      expect(seen, [
        {
          'event': 'order.created',
          'data': {'id': 7},
          'id': '41',
        },
        {
          'event': 'forge.gap',
          'data': {'reason': 'x'},
          'id': '41',
        },
      ]);
    });

    test('treats an error as a drop: reports it, closes the source, and says so once', () async {
      final sse = _sse();
      final connection = await eventSourceConnection(client: sse.client)(
        _context,
      );
      final errors = <Object>[];
      var closes = 0;

      connection.messages.listen((_) {}, onError: errors.add);
      unawaited(connection.closed.then((_) => closes++));
      sse.body
        ..addError(StateError('network'))
        ..addError(StateError('network'));
      await settle();

      expect(errors, hasLength(1));
      expect(closes, 1);
    });
  });

  group('channelMessages', () {
    test(
      'reads one channel\'s message names out of the streams table, once each',
      () {
        EntityStreamBinding row(String channel, String message) =>
            EntityStreamBinding(
              channel: channel,
              message: message,
              entity: 'Order',
              intent: StreamIntent.patch,
            );

        final streams = <StreamBinding>[
          row('/sse/orders', 'order.created'),
          row('/sse/orders', 'order.deleted'),
          row('/sse/orders', 'order.created'),
          const DuplexStreamBinding(
            channel: '/sse/orders',
            send: 'command',
            receive: 'event',
          ),
          row('/sse/other', 'x'),
        ];

        expect(channelMessages(streams, '/sse/orders'), [
          'order.created',
          'order.deleted',
        ]);
      },
    );
  });
}
