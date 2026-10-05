@TestOn('vm')
library;

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/test_servers.dart';

StreamConnectContext _context(Uri base, String endpoint) =>
    StreamConnectContext(
      url: base.replace(path: endpoint),
      endpoint: endpoint,
    );

void main() {
  group('eventSourceConnection on native', () {
    late SseTestServer server;

    setUp(() async {
      server = SseTestServer();
      await server.start();
    });

    tearDown(() => server.stop());

    test('delivers named events as event, data and id frames', () async {
      final connection = await eventSourceConnection(
        events: ['order.created', 'order.updated'],
      )(_context(server.url, '/sse/orders'));
      addTearDown(connection.close);

      expect(connection, isA<ReceiveOnlyConnection>());

      final seen = <Object?>[];
      connection.messages.listen(seen.add);

      server.send(0, 'id: e-1\nevent: order.created\ndata: {"id":9}\n\n');
      server.send(
        0,
        ': note\n\nevent: order.updated\ndata: {"id":9,\ndata: "total":5}\n\n',
      );
      await until(() => seen.length == 2);

      expect(seen, [
        {
          'event': 'order.created',
          'data': {'id': 9},
          'id': 'e-1',
        },
        {
          'event': 'order.updated',
          'data': {'id': 9, 'total': 5},
          'id': 'e-1',
        },
      ]);
      expect(server.requests.single['accept'], 'text/event-stream');
    });

    test(
      'listens to the listed events, the control events and message only',
      () async {
        final connection = await eventSourceConnection(
          events: ['order.created'],
        )(_context(server.url, '/sse/orders'));
        addTearDown(connection.close);

        final seen = <Object?>[];
        connection.messages.listen(seen.add);

        server.send(0, 'event: order.created\ndata: 1\n\n');
        server.send(0, 'event: order.unbound\ndata: 2\n\n');
        server.send(
          0,
          'event: forge.resumed\ndata: {"from":"e-1","count":0}\n\n',
        );
        server.send(0, 'data: 4\n\n');
        await until(() => seen.length == 3);

        expect(
          seen.map((frame) => (frame! as Map<Object?, Object?>)['event']),
          ['order.created', 'forge.resumed', 'message'],
        );
      },
    );

    // As in a browser, which hears only the names it has listeners for.
    test(
      'delivers only the control events and message when no events are listed',
      () async {
        final connection = await eventSourceConnection()(
          _context(server.url, '/sse/orders'),
        );
        addTearDown(connection.close);

        final seen = <Object?>[];
        connection.messages.listen(seen.add);

        server.send(0, 'event: order.created\ndata: 1\n\n');
        server.send(0, 'event: forge.gap\ndata: {}\n\n');
        server.send(0, 'data: 3\n\n');
        await until(() => seen.length == 2);

        expect(
          seen.map((frame) => (frame! as Map<Object?, Object?>)['event']),
          ['forge.gap', 'message'],
        );
      },
    );

    test('reports a payload that is not JSON and keeps reading', () async {
      final connection = await eventSourceConnection(events: ['order.created'])(
        _context(server.url, '/sse/orders'),
      );
      addTearDown(connection.close);

      final seen = <Object?>[];
      final errors = <Object>[];
      connection.messages.listen(seen.add, onError: errors.add);

      server.send(0, 'event: order.created\ndata: {not json\n\n');
      server.send(0, 'event: order.created\ndata: {"id":2}\n\n');
      await until(() => seen.length == 1);

      expect(errors, [isA<FormatException>()]);
      expect((seen.single! as Map<Object?, Object?>)['data'], {'id': 2});
    });

    // The connection is open and quiet: nothing more will ever arrive, so a
    // close that waited for the next event would hang the manager's teardown.
    test('closes a quiet stream promptly and frees the connection', () async {
      final connection = await eventSourceConnection()(
        _context(server.url, '/sse/orders'),
      );
      connection.messages.listen((_) {});
      await server.connections(1);

      await connection.close().timeout(const Duration(seconds: 2));

      await connection.closed.timeout(const Duration(seconds: 2));
      // The server only learns the socket is gone by writing to it, so keep
      // writing until it drops its body listener.
      await until(() {
        server.send(0, ': probe\n\n');

        return !server.bodies.single.hasListener;
      });
    });

    test('carries an empty id on a frame sent before any id', () async {
      final connection = await eventSourceConnection()(
        _context(server.url, '/sse/orders'),
      );
      addTearDown(connection.close);

      final seen = <Object?>[];
      connection.messages.listen(seen.add);

      server.send(0, 'data: 1\n\n');
      await until(() => seen.length == 1);

      expect(seen.single, {'event': 'message', 'data': 1, 'id': ''});
    });

    group('Last-Event-ID on the next connect', () {
      /// Connect, let [script] write to the server, end the stream and wait
      /// for the connection to report closed, so every frame was parsed.
      Future<void> session(
        StreamConnect connect,
        StreamConnectContext context,
        String script,
      ) async {
        final connection = await connect(context);
        connection.messages.listen((_) {});

        final index = server.bodies.length - 1;
        server.send(index, script);
        await server.end(index);
        await connection.closed;
      }

      test('an id-only event updates it', () async {
        final connect = eventSourceConnection();
        final context = _context(server.url, '/sse/orders');

        await session(connect, context, 'id: 5\n\n');
        await (await connect(context)).close();

        expect(server.requests[1]['last-event-id'], '5');
      });

      test('an empty id clears it, so no header is sent', () async {
        final connect = eventSourceConnection();
        final context = _context(server.url, '/sse/orders');

        await session(connect, context, 'id: e-1\ndata: 1\n\nid\ndata: 2\n\n');
        await (await connect(context)).close();

        expect(server.requests[1].containsKey('last-event-id'), isFalse);
      });

      test(
        'a connect with the id from a different principal does not send it',
        () async {
          final connect = eventSourceConnection();
          final url = server.url.replace(path: '/sse/orders');
          StreamConnectContext as(String principal) => StreamConnectContext(
            url: url,
            endpoint: '/sse/orders',
            principal: principal,
          );

          await session(connect, as('alice'), 'id: a-1\ndata: 1\n\n');
          await (await connect(as('bob'))).close();
          await (await connect(as('alice'))).close();

          expect(server.requests[1].containsKey('last-event-id'), isFalse);
          expect(server.requests[2]['last-event-id'], 'a-1');
        },
      );
    });

    test('fails the connect on a non-2xx response', () async {
      server.status = 503;

      await expectLater(
        eventSourceConnection()(_context(server.url, '/sse/orders')),
        throwsA(isA<HttpStatusError>().having((e) => e.status, 'status', 503)),
      );
    });

    // Review Focus: a stream that drops mid-event delivers nothing for that
    // event, reports the connection closed, and the reconnect carries the
    // last dispatched id so a replaying server can resume.
    test('reconnects after a drop mid-event and resumes from the last dispatched id', () async {
      final manager = SubscriptionManager(
        connect: eventSourceConnection(events: ['order.updated']),
        baseUrl: server.url,
        backoff: const BackoffPolicy(
          initial: Duration(milliseconds: 20),
          max: Duration(milliseconds: 40),
        ),
      );
      addTearDown(manager.closeAll);

      final seen = <Object?>[];
      final reconnects = <String>[];
      manager.onReconnect = (endpoint, _) => reconnects.add(endpoint);
      manager.subscribe('/sse/orders', (message, _) => seen.add(message));

      await server.connections(1);
      server.send(
        0,
        'id: e-1\nevent: order.updated\ndata: {"id":7,"total":1}\n\n',
      );
      await until(() => seen.length == 1);

      server.send(0, 'id: e-2\nevent: order.updated\ndata: {"id":7,');
      await server.end(0);

      await server.connections(2);
      await until(() => reconnects.length == 1);

      expect(server.requests[1]['last-event-id'], 'e-1');
      expect(seen, hasLength(1));
      expect(reconnects, ['/sse/orders']);
    });
  });

  group('webSocketConnection on native', () {
    late WsTestServer server;

    setUp(() async {
      server = WsTestServer();
      await server.start();
    });

    tearDown(() => server.stop());

    test(
      'round-trips JSON frames, sends headers and answers the keepalive',
      () async {
        final manager = SubscriptionManager(
          connect: webSocketConnection(),
          baseUrl: server.url,
          headers: () => {'x-probe': 'yes'},
        );
        addTearDown(manager.closeAll);

        final seen = <Object?>[];
        manager.subscribe(
          '/ws/orders',
          (message, _) => seen.add(message),
          const SubscribeOptions(hello: {'action': 'subscribe'}),
        );

        await until(
          () => server.sockets.length == 1 && server.received.isNotEmpty,
        );
        expect(server.received.first, {'action': 'subscribe'});
        expect(server.handshakes.single.value('x-probe'), 'yes');

        server.send(0, {'type': 'system', 'event': 'ping'});
        server.send(0, {
          'event': 'order.created',
          'data': {'id': 9},
        });
        await until(() => seen.length == 2 && server.received.length == 2);

        expect(server.received[1], {'type': 'system', 'event': 'pong'});
        expect(seen[1], {
          'event': 'order.created',
          'data': {'id': 9},
        });
      },
    );

    test(
      'reconnects and reports the gap when the server closes the socket',
      () async {
        final manager = SubscriptionManager(
          connect: webSocketConnection(),
          baseUrl: server.url,
          backoff: const BackoffPolicy(initial: Duration(milliseconds: 20)),
        );
        addTearDown(manager.closeAll);

        final reconnects = <String>[];
        manager.onReconnect = (endpoint, _) => reconnects.add(endpoint);
        manager.subscribe('/ws/orders', (_, _) {});

        await until(() => server.sockets.length == 1);
        await server.sockets.first.close();

        await until(() => reconnects.length == 1);
        expect(server.sockets, hasLength(2));
      },
    );
  });

  group('webTransportConnection on native', () {
    test(
      'refuses WebTransport with TransportUnavailable naming the route',
      () async {
        await expectLater(
          webTransportConnection()(
            _context(Uri.parse('https://127.0.0.1:1'), '/wt/orders'),
          ),
          throwsA(
            isA<TransportUnavailable>()
                .having((e) => e.route, 'route', '/wt/orders')
                .having((e) => e.transport, 'transport', 'webtransport'),
          ),
        );
      },
    );

    test(
      'falls back to the WebSocket when WebTransport is unavailable',
      () async {
        final server = WsTestServer();
        await server.start();
        addTearDown(server.stop);

        final errors = <Object>[];
        final manager = SubscriptionManager(
          connect: fallbackConnection([
            webTransportConnection(),
            webSocketConnection(),
          ]),
          baseUrl: server.url,
          onError: (error, _) => errors.add(error),
        );
        addTearDown(manager.closeAll);

        manager.subscribe('/live/orders', (_, _) {});

        await until(() => server.sockets.length == 1);
        expect(errors, isEmpty);
      },
    );
  });
}
