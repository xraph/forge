@TestOn('browser')
library;

import 'dart:async';
import 'dart:convert';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

// A VM-side server the browser connects to. Spawned with spawnHybridCode so
// its imports (dart:io, stream_channel) are never analyzed as part of this
// package.
const _serverSource = r'''
import 'dart:convert';
import 'dart:io';

import 'package:stream_channel/stream_channel.dart';

Future<void> hybridMain(StreamChannel<Object?> channel) async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);

  server.listen((request) async {
    if (request.uri.path.startsWith('/ws/')) {
      final socket = await WebSocketTransformer.upgrade(request);
      socket.add(jsonEncode({'event': 'order.created', 'data': {'id': 9}}));
      socket.listen((data) => socket.add(
        jsonEncode({'event': 'echo', 'data': jsonDecode(data as String)}),
      ));

      return;
    }

    final response = request.response;
    final ids = request.uri.path == '/sse/ids';
    response.headers
      ..set('content-type', 'text/event-stream')
      ..set('cache-control', 'no-cache')
      ..set('access-control-allow-origin', '*');
    response.bufferOutput = false;
    response.write(': open\n\n');
    await response.flush();

    if (ids) {
      // No id yet, then an id-only event, then a data event, then a cleared id.
      response.write('event: a\ndata: 1\n\n');
      response.write('id: k-1\n\n');
      response.write('event: a\ndata: 2\n\n');
      response.write('id:\nevent: a\ndata: 3\n\n');
      await response.flush();

      return;
    }

    response.write('id: e-1\nevent: order.created\ndata: {"id":9}\n\n');
    response.write('event: order.unbound\ndata: 2\n\n');
    response.write('event: forge.gap\ndata: {"reason":"unresumable"}\n\n');
    await response.flush();
  });

  channel.sink.add(server.port);
}
''';

// A stand-in for the browser's WebTransport with the three members the
// adapter reads, installed over the real one for the WebTransport cases.
const _fakeWebTransport = r'''
(() => {
  class FakeWebTransport {
    constructor(url) {
      let push, end, shut;
      this.url = url;
      this.closeCount = 0;
      this.ready = Promise.resolve();
      this.closed = new Promise((resolve) => { shut = resolve; });
      this._shut = shut;
      this.datagrams = {
        readable: new ReadableStream({
          start(controller) {
            push = (bytes) => controller.enqueue(bytes);
            end = () => controller.close();
          },
        }),
      };
      const self = this;
      globalThis.__forgeFakeWebTransport = {
        send: (text) => push(new TextEncoder().encode(text)),
        drop: () => { end(); shut(); },
        closes: () => self.closeCount,
      };
    }

    close() {
      this.closeCount += 1;
      this._shut('closed by us');
    }
  }

  if (!('__forgeRealWebTransport' in globalThis)) {
    globalThis.__forgeRealWebTransport = globalThis.WebTransport;
  }
  globalThis.WebTransport = FakeWebTransport;
})();
''';

const _restoreWebTransport =
    'globalThis.WebTransport = globalThis.__forgeRealWebTransport;';

void _eval(String source) =>
    globalContext.callMethod<JSAny?>('eval'.toJS, source.toJS);

JSObject get _fake =>
    globalContext.getProperty<JSObject>('__forgeFakeWebTransport'.toJS);

void _send(String text) => _fake.callMethod<JSAny?>('send'.toJS, text.toJS);

Future<void> _settle() async {
  for (var i = 0; i < 8; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

Future<Uri> _startServer() async {
  final channel = spawnHybridCode(_serverSource);
  final port = await channel.stream.first as int;

  return Uri.parse('http://127.0.0.1:$port');
}

typedef _Kit = ({
  SubscriptionManager subscriptions,
  ManualScheduler release,
  List<(Object, String)> errors,
});

_Kit _harness() {
  final release = ManualScheduler();
  final errors = <(Object, String)>[];
  final subscriptions = SubscriptionManager(
    connect: webTransportConnection(),
    random: () => 0,
    backoff: const BackoffPolicy(
      initial: Duration(seconds: 1),
      jitter: 0.5,
      attempts: 1,
    ),
    release: release,
    onError: (error, context) => errors.add((error, context)),
  );

  return (subscriptions: subscriptions, release: release, errors: errors);
}

void main() {
  group('the browser WebSocket', () {
    test('opens a WebSocket and round-trips JSON frames', () async {
      final base = await _startServer();
      final connection = await webSocketConnection()(
        StreamConnectContext(
          url: base.replace(path: '/ws/orders'),
          endpoint: '/ws/orders',
        ),
      );
      addTearDown(connection.close);

      final seen = <Object?>[];
      final done = Completer<void>();
      connection.messages.listen((message) {
        seen.add(message);

        if (seen.length == 2) done.complete();
      });

      connection.send({'hello': 1});
      await done.future.timeout(const Duration(seconds: 5));

      expect(seen, [
        {
          'event': 'order.created',
          'data': {'id': 9},
        },
        {
          'event': 'echo',
          'data': {'hello': 1},
        },
      ]);
    });
  });

  group('the browser EventSource', () {
    test('delivers the listed events and the control events as event, data and id frames', () async {
      final base = await _startServer();
      final connection = await eventSourceConnection(events: ['order.created'])(
        StreamConnectContext(
          url: base.replace(path: '/sse/orders'),
          endpoint: '/sse/orders',
        ),
      );
      addTearDown(connection.close);

      expect(connection, isA<ReceiveOnlyConnection>());

      final seen = <Map<Object?, Object?>>[];
      final done = Completer<void>();
      connection.messages.listen((message) {
        seen.add(message! as Map<Object?, Object?>);

        if (seen.length == 2) done.complete();
      });

      await done.future.timeout(const Duration(seconds: 5));

      expect(seen.map((frame) => frame['event']), [
        'order.created',
        'forge.gap',
      ]);
      expect(seen.first['data'], {'id': 9});
      expect(seen.first['id'], 'e-1');
    });
  });

  group('the browser EventSource frames match the native ones', () {
    test('an id-only event updates the id, an empty id clears it and none reads as empty', () async {
      final base = await _startServer();
      final connection = await eventSourceConnection(events: ['a'])(
        StreamConnectContext(
          url: base.replace(path: '/sse/ids'),
          endpoint: '/sse/ids',
        ),
      );
      addTearDown(connection.close);

      final seen = <Map<Object?, Object?>>[];
      final done = Completer<void>();
      connection.messages.listen((message) {
        seen.add(message! as Map<Object?, Object?>);

        if (seen.length == 3) done.complete();
      });

      await done.future.timeout(const Duration(seconds: 5));

      expect(seen, [
        {'event': 'a', 'data': 1, 'id': ''},
        {'event': 'a', 'data': 2, 'id': 'k-1'},
        {'event': 'a', 'data': 3, 'id': ''},
      ]);
    });

    test('closes the source and ends the stream on close', () async {
      final base = await _startServer();
      final connection = await eventSourceConnection(events: ['a'])(
        StreamConnectContext(
          url: base.replace(path: '/sse/orders'),
          endpoint: '/sse/orders',
        ),
      );

      var ended = false;
      connection.messages.listen((_) {}, onDone: () => ended = true);

      await connection.close();
      await connection.closed;
      await _settle();

      expect(ended, isTrue);
    });
  });

  group('the WebTransport adapter', () {
    setUp(() => _eval(_fakeWebTransport));
    tearDown(() => _eval(_restoreWebTransport));

    test('delivers a datagram to a channel subscriber', () async {
      final kit = _harness();
      addTearDown(kit.subscriptions.closeAll);
      final seen = <Object?>[];

      kit.subscriptions.subscribe(
        '/wt/orders',
        (message, _) => seen.add(message),
      );
      await _settle();

      _send(jsonEncode({'type': 'order.created', 'id': 7}));
      await _settle();

      expect(seen, [
        {'type': 'order.created', 'id': 7},
      ]);
    });

    test('delivers datagrams in the order they arrive', () async {
      final kit = _harness();
      addTearDown(kit.subscriptions.closeAll);
      final seen = <Object?>[];

      kit.subscriptions.subscribe(
        '/wt/orders',
        (message, _) => seen.add(message),
      );
      await _settle();

      _send(jsonEncode({'n': 1}));
      _send(jsonEncode({'n': 2}));
      _send(jsonEncode({'n': 3}));
      await _settle();

      expect(seen, [
        {'n': 1},
        {'n': 2},
        {'n': 3},
      ]);
    });

    test('reports a malformed datagram and keeps reading', () async {
      final kit = _harness();
      addTearDown(kit.subscriptions.closeAll);
      final seen = <Object?>[];

      kit.subscriptions.subscribe(
        '/wt/orders',
        (message, _) => seen.add(message),
      );
      await _settle();

      _send('{not json');
      _send(jsonEncode({'n': 2}));
      await _settle();

      // The bad datagram is reported, not thrown, and the loop survives it.
      expect(kit.errors, hasLength(1));
      expect(seen, [
        {'n': 2},
      ]);
    });

    test(
      'tells the manager the socket closed when the datagram source ends',
      () async {
        final kit = _harness();
        addTearDown(kit.subscriptions.closeAll);

        kit.subscriptions.subscribe('/wt/orders', (_, _) {});
        await _settle();
        expect(kit.subscriptions.size, 1);

        _fake.callMethod<JSAny?>('drop'.toJS);
        await _settle();

        // The manager owns what happens next; a reconnect is scheduled off the
        // back of the notification and the socket is still held.
        expect(kit.subscriptions.size, 1);
        expect(kit.subscriptions.connected('/wt/orders'), isFalse);
      },
    );

    test(
      'closes the transport when the manager releases the last subscriber',
      () async {
        final kit = _harness();

        final stop = kit.subscriptions.subscribe('/wt/orders', (_, _) {});
        await _settle();

        stop();
        kit.release.flush();

        expect(_fake.callMethod<JSNumber>('closes'.toJS).toDartInt, 1);
      },
    );

    test(
      'throws TransportUnavailable where the browser has no WebTransport',
      () async {
        _eval('globalThis.WebTransport = undefined;');

        await expectLater(
          webTransportConnection()(
            StreamConnectContext(
              url: Uri.parse('https://127.0.0.1:1/wt/orders'),
              endpoint: '/wt/orders',
            ),
          ),
          throwsA(
            isA<TransportUnavailable>().having(
              (e) => e.route,
              'route',
              '/wt/orders',
            ),
          ),
        );
      },
    );
  });
}
