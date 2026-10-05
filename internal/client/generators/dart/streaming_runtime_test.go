package dart

import "testing"

// streamingRuntimeTest drives the generated streaming clients over a fake
// StreamConnection, asserting the frames each one sends. The expected frames
// are the ones the TypeScript feature clients send (rooms.ts, presence.ts,
// typing.ts and channels.ts), so one server serves both.
const streamingRuntimeTest = `import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart' show StreamConnect, StreamConnectContext, StreamConnection, TransportUnavailable;
import 'package:streaming_forge_client/streaming_forge_client.dart';
import 'package:test/test.dart';

final class FakeConnection implements StreamConnection {
  FakeConnection({this.sendLimit});

  final _incoming = StreamController<Object?>();
  final _closed = Completer<void>();
  final sent = <Object?>[];

  /// Sends allowed before every send throws, without the connection closing.
  final int? sendLimit;

  /// Makes every send throw while the connection still looks open.
  var broken = false;

  /// Every send, including the ones refused because the connection closed.
  var attempts = 0;

  void deliver(Object? message) => _incoming.add(message);

  void failClosed(Object error) => _closed.completeError(error);

  @override
  Stream<Object?> get messages => _incoming.stream;

  @override
  Future<void> get closed => _closed.future;

  @override
  void send(Object? message) {
    attempts++;
    if (_closed.isCompleted) throw StateError('closed');
    if (broken || (sendLimit != null && sent.length >= sendLimit!)) throw StateError('send failed');
    // The transport encodes what it sends, so a value that is not JSON fails here.
    jsonEncode(message);
    sent.add(message);
  }

  @override
  Future<void> close() async {
    if (!_closed.isCompleted) _closed.complete();
    unawaited(_incoming.close());
  }

  Map<String, Object?> frame(int index) => (sent[index]! as Map<Object?, Object?>).cast<String, Object?>();

  Map<String, Object?> get last => frame(sent.length - 1);
}

final class Harness {
  final contexts = <StreamConnectContext>[];
  final connections = <FakeConnection>[];

  /// How long the first connect takes.
  Duration? firstDelay;

  /// Makes every connect fail.
  var refuse = false;

  /// Makes a connect fail with the error a platform without WebTransport gives.
  var unavailable = false;

  /// The sends each new connection allows.
  int? sendLimit;

  StreamConnect get connect => (context) async {
    contexts.add(context);
    if (firstDelay != null && contexts.length == 1) await Future<void>.delayed(firstDelay!);
    if (unavailable) throw TransportUnavailable(context.endpoint, 'webtransport');
    if (refuse) throw StateError('refused');
    final connection = FakeConnection(sendLimit: sendLimit);
    connections.add(connection);
    return connection;
  };

  FakeConnection get only => connections.single;
}

/// Options that reconnect after a few milliseconds, for tests that wait for it.
const quick = LiveOptions(reconnectDelay: Duration(milliseconds: 10), maxReconnectDelay: Duration(milliseconds: 20));

Future<void> until(bool Function() done, {int milliseconds = 1500}) async {
  final end = DateTime.now().add(Duration(milliseconds: milliseconds));
  while (!done()) {
    if (DateTime.now().isAfter(end)) fail('timed out waiting');
    await Future<void>.delayed(const Duration(milliseconds: 5));
  }
}

Future<void> settle() async {
  for (var i = 0; i < 5; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

Future<void> wait(int milliseconds) => Future<void>.delayed(Duration(milliseconds: milliseconds));

void expectIso(Object? value) {
  expect(value, isA<String>());
  expect(DateTime.parse(value! as String).isUtc, isTrue);
}

void main() {
  group('typed sockets', () {
    test('connect per endpoint, with the path parameter encoded and no principal', () async {
      final h = Harness();
      final socket = ChatSocket(
        baseUrl: Uri.parse('https://api.test/'),
        connect: h.connect,
        headers: {'authorization': 'Bearer t'},
        heartbeat: null,
      );
      final session = await socket.connect(roomId: 'a b/c');
      final context = h.contexts.single;
      expect(context.url.toString(), 'wss://api.test/ws/chat/a%20b%2Fc');
      expect(context.endpoint, '/ws/chat/{roomId}');
      expect(context.headers, {'authorization': 'Bearer t'});
      expect(context.principal, isNull);
      expect(context.channels, isEmpty);
      await session.close();
    });

    test('send encodes through the codec and messages decodes through it', () async {
      final h = Harness();
      final session = await ChatSocket(baseUrl: Uri.parse('http://api.test'), connect: h.connect, heartbeat: null)
          .connect(roomId: 'r');
      expect(h.contexts.single.url.scheme, 'ws');
      session.send(const LineItem(sku: 'x', qty: 2));
      expect(h.only.sent, [
        {'qty': 2, 'sku': 'x'},
      ]);
      final next = session.messages.first;
      h.only.deliver({'sku': 'y', 'qty': 3});
      expect(await next, const LineItem(sku: 'y', qty: 3));
    });

    test('the server ping is answered and kept out of the typed messages', () async {
      final h = Harness();
      final session = await ChatSocket(baseUrl: Uri.parse('http://api.test'), connect: h.connect, heartbeat: null)
          .connect(roomId: 'r');
      final seen = <LineItem>[];
      final errors = <Object>[];
      session.messages.listen(seen.add, onError: errors.add);
      h.only.deliver({'type': 'system', 'event': 'ping'});
      h.only.deliver({'type': 'system', 'event': 'pong'});
      h.only.deliver({'sku': 'z', 'qty': 1});
      await settle();
      expect(h.only.sent, [
        {'type': 'system', 'event': 'pong'},
      ]);
      expect(seen, [const LineItem(sku: 'z', qty: 1)]);
      expect(errors, isEmpty);
    });

    test('a socket pings on the heartbeat until it is closed', () async {
      final h = Harness();
      final session = await ChatSocket(
        baseUrl: Uri.parse('http://api.test'),
        connect: h.connect,
        heartbeat: const Duration(milliseconds: 20),
      ).connect(roomId: 'r');
      await wait(110);
      expect(h.only.sent, isNotEmpty);
      for (final frame in h.only.sent) {
        expect(frame, {'type': 'system', 'event': 'ping'});
      }
      await session.close();
      final count = h.only.attempts;
      await wait(80);
      expect(h.only.attempts, count);
    });

    test('a socket the server closes stops pinging', () async {
      final h = Harness();
      await ChatSocket(
        baseUrl: Uri.parse('http://api.test'),
        connect: h.connect,
        heartbeat: const Duration(milliseconds: 20),
      ).connect(roomId: 'r');
      await wait(50);
      await h.only.close();
      await settle();
      final count = h.only.attempts;
      await wait(80);
      expect(h.only.attempts, count);
    });

    test('an event stream delivers declared events only, as their data', () async {
      final h = Harness();
      final session = await NotificationsEvents(baseUrl: Uri.parse('https://api.test'), connect: h.connect).connect();
      expect(h.contexts.single.url.toString(), 'https://api.test/sse/notifications');
      expect(h.contexts.single.endpoint, '/sse/notifications');
      final seen = <Customer>[];
      session.messages.listen(seen.add);
      h.only.deliver({'event': 'forge.gap', 'data': <String, Object?>{}, 'id': ''});
      h.only.deliver({'event': 'forge.resumed', 'data': <String, Object?>{}, 'id': ''});
      h.only.deliver({'event': 'message', 'data': {'id': 'nope'}, 'id': ''});
      h.only.deliver({'event': 'created', 'data': {'id': 'c1', 'name': 'Ada'}, 'id': '7'});
      await settle();
      expect(seen, [const Customer(id: 'c1', name: 'Ada')]);
    });

    test('a multiplexed direction hands over the raw frames', () async {
      final h = Harness();
      final session = await MixedEvents(baseUrl: Uri.parse('https://api.test'), connect: h.connect).connect(topic: 't');
      expect(h.contexts.single.url.path, '/sse/mixed/t');
      final next = session.messages.first;
      h.only.deliver({'event': 'who', 'data': {'id': 'c'}, 'id': '1'});
      expect(await next, {'event': 'who', 'data': {'id': 'c'}, 'id': '1'});
    });

    test('a transport with no wrapper decodes its datagrams', () async {
      final h = Harness();
      final session = await TelemetryTransport(baseUrl: Uri.parse('https://api.test'), connect: h.connect).connect();
      final next = session.messages.first;
      h.only.deliver({'sku': 'd', 'qty': 9});
      expect(await next, const LineItem(sku: 'd', qty: 9));
    });
  });

  group('rooms', () {
    test('join, send, leave and history send the TypeScript frames', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await Future.wait([rooms.connect(), rooms.connect()]);
      expect(h.contexts, hasLength(1));
      expect(h.contexts.single.url.toString(), 'ws://x.test/realtime/rooms');
      expect(h.contexts.single.endpoint, '/realtime/rooms');

      final join = rooms.join('r1', metadata: {'k': 'v'}, role: 'admin');
      await settle();
      final joinFrame = h.only.frame(0);
      expect(joinFrame.keys.toList(), ['type', 'request_id', 'room_id', 'metadata', 'role']);
      expect(joinFrame['type'], 'join');
      expect(joinFrame['room_id'], 'r1');
      expect(joinFrame['metadata'], {'k': 'v'});
      expect(joinFrame['role'], 'admin');
      expect(joinFrame['request_id'], startsWith('req_'));
      h.only.deliver({
        'type': 'join',
        'request_id': joinFrame['request_id'],
        'members': [
          {'user_id': 'u1'},
        ],
      });
      await join;
      expect(rooms.joined, {'r1'});
      expect(rooms.membersOf('r1'), [
        {'user_id': 'u1'},
      ]);

      rooms.send('r1', {'text': 'hi'});
      final message = h.only.last;
      expect(message.keys.toList(), ['type', 'room_id', 'data', 'timestamp']);
      expect(message['type'], 'message');
      expect(message['room_id'], 'r1');
      expect(message['data'], {'text': 'hi'});
      expectIso(message['timestamp']);

      final history = rooms.history('r1', limit: 5, beforeId: 'm9');
      await settle();
      final historyFrame = h.only.last;
      expect(historyFrame.keys.toList(), ['type', 'request_id', 'room_id', 'limit', 'before_id']);
      expect(historyFrame['type'], 'history');
      h.only.deliver({
        'request_id': historyFrame['request_id'],
        'messages': [
          {'id': 'm1', 'room_id': 'r1', 'user_id': 'u1', 'type': 'message', 'data': 'a', 'timestamp': 't'},
        ],
      });
      final rows = await history;
      expect(rows.single.id, 'm1');
      expect(rows.single.userId, 'u1');
      expect(rows.single.data, 'a');

      rooms.leave('r1');
      expect(h.only.last, {'type': 'leave', 'room_id': 'r1'});
      expect(rooms.joined, isEmpty);
      await rooms.close();
    });

    test('events follow the frames, and sending needs a join', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await rooms.connect();
      expect(() => rooms.send('nope', 1), throwsStateError);

      final events = <RoomEvent>[];
      rooms.events.listen(events.add);
      h.only.deliver({'type': 'message', 'room_id': 'r1', 'user_id': 'u2', 'data': 'yo'});
      h.only.deliver({'type': 'member_join', 'room_id': 'r1', 'member': {'user_id': 'u3'}});
      h.only.deliver({'type': 'member_leave', 'room_id': 'r1', 'member': {'user_id': 'u3'}});
      h.only.deliver({'type': 'error', 'room_id': 'r1', 'message': 'boom'});
      await settle();
      expect(events[0], isA<RoomMessageReceived>().having((e) => e.message.data, 'data', 'yo'));
      expect(events[1], isA<RoomMemberJoined>().having((e) => e.member['user_id'], 'user', 'u3'));
      expect(events[2], isA<RoomMemberLeft>());
      expect(events[3], isA<RoomFailure>().having((e) => e.message, 'message', 'boom'));
    });

    test('the room limit and a refused join are errors', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, maxRooms: 1);
      await rooms.connect();
      final first = rooms.join('a');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id']});
      await first;
      await expectLater(rooms.join('b'), throwsStateError);

      final other = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await other.connect();
      final refused = other.join('a');
      await settle();
      h.connections.last.deliver({'request_id': h.connections.last.last['request_id'], 'error': 'full'});
      await expectLater(refused, throwsA(isA<StateError>().having((e) => e.message, 'message', 'full')));
    });

    test('a reply that never comes times out, and a lost connection fails the request and allows another', () async {
      final h = Harness();
      final rooms = RoomClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        requestTimeout: const Duration(milliseconds: 30),
      );
      await rooms.connect();
      await expectLater(rooms.join('slow'), throwsA(isA<TimeoutException>()));

      final doomed = rooms.join('gone');
      await settle();
      await h.only.close();
      await expectLater(doomed, throwsStateError);
      expect(() => rooms.send('gone', 1), throwsStateError);
      await rooms.connect();
      expect(h.connections, hasLength(2));
    });

    test('a call before connect says so, and a closed client stays closed', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await expectLater(rooms.join('r'), throwsA(isA<StateError>().having((e) => e.message, 'message', contains('connect()'))));
      await rooms.close();
      await expectLater(rooms.connect(), throwsStateError);
    });
  });

  group('presence', () {
    test('heartbeat, status, subscribe and unsubscribe send the TypeScript frames', () async {
      final h = Harness();
      final presence = PresenceClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        heartbeat: const Duration(milliseconds: 20),
      );
      await presence.connect();
      expect(h.contexts.single.url.toString(), 'ws://x.test/realtime/presence');
      expect(PresenceClient.statuses, ['here', 'gone']);
      await wait(70);
      final beat = h.only.frame(0);
      expect(beat.keys.toList(), ['type', 'timestamp']);
      expect(beat['type'], 'heartbeat');
      expectIso(beat['timestamp']);

      h.only.sent.clear();
      presence.setStatus('away', customMessage: 'brb');
      var frame = h.only.frame(0);
      expect(frame.keys.toList(), ['type', 'status', 'custom_status', 'timestamp']);
      expect(frame['type'], 'presence');
      expect(frame['status'], 'away');
      expect(frame['custom_status'], 'brb');
      expectIso(frame['timestamp']);
      expect(presence.current, (status: 'away', customMessage: 'brb'));

      presence.setStatus('online');
      frame = h.only.last;
      expect(frame.keys.toList(), ['type', 'status', 'timestamp']);

      presence.subscribe(['u1', 'u2']);
      expect(h.only.last, {'type': 'subscribe_presence', 'user_ids': ['u1', 'u2']});
      presence.unsubscribe(['u1']);
      expect(h.only.last, {'type': 'unsubscribe_presence', 'user_ids': ['u1']});

      await presence.close();
      final count = h.only.attempts;
      await wait(60);
      expect(h.only.attempts, count);
    });

    test('updates and syncs fill the known presences', () async {
      final h = Harness();
      final presence = PresenceClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await presence.connect();
      final seen = <UserPresence>[];
      presence.updates.listen(seen.add);
      final errors = <String>[];
      presence.errors.listen(errors.add);
      h.only.deliver({'type': 'presence', 'user_id': 'u1', 'status': 'online', 'custom_status': 'hi', 'timestamp': 't', 'room_id': 'r'});
      h.only.deliver({
        'type': 'presence_sync',
        'presences': [
          {'user_id': 'u2', 'status': 'offline'},
          {'user_id': 'u3', 'status': 'away', 'room_id': 'r'},
        ],
      });
      h.only.deliver({'type': 'error', 'message': 'nope'});
      await settle();
      expect(seen.map((p) => p.userId), ['u1', 'u2', 'u3']);
      expect(presence.presenceOf('u1')?.customMessage, 'hi');
      expect(presence.onlineUsers().map((p) => p.userId), ['u1', 'u3']);
      expect(presence.onlineUsers(roomId: 'r').map((p) => p.userId), ['u1', 'u3']);
      expect(presence.onlineUsers(roomId: 'z'), isEmpty);
      expect(errors, ['nope']);
      await presence.close();
    });
  });

  group('typing', () {
    test('start is debounced into one frame and stop sends the other', () async {
      final h = Harness();
      final typing = TypingClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        debounce: const Duration(milliseconds: 20),
        timeout: const Duration(milliseconds: 500),
      );
      typing.start('r1');
      expect(h.connections, isEmpty);
      await typing.connect();
      expect(h.contexts.single.url.toString(), 'ws://x.test/realtime/typing');
      typing
        ..start('r1')
        ..start('r1')
        ..start('r1');
      await wait(70);
      expect(h.only.sent, hasLength(1));
      final started = h.only.frame(0);
      expect(started.keys.toList(), ['type', 'room_id', 'data', 'timestamp']);
      expect(started['type'], 'typing');
      expect(started['room_id'], 'r1');
      expect(started['data'], true);
      expectIso(started['timestamp']);

      typing.stop('r1');
      final stopped = h.only.last;
      expect(stopped.keys.toList(), ['type', 'room_id', 'data', 'timestamp']);
      expect(stopped['data'], false);
      await typing.close();
    });

    test('typing stops by itself after the timeout', () async {
      final h = Harness();
      final typing = TypingClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        debounce: const Duration(milliseconds: 5),
        timeout: const Duration(milliseconds: 100),
      );
      await typing.connect();
      typing.start('r1');
      await wait(260);
      expect(h.only.sent.map((f) => (f! as Map<Object?, Object?>)['data']), [true, false]);
      await typing.close();
    });

    test('events and the typing set follow the server', () async {
      final h = Harness();
      final typing = TypingClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await typing.connect();
      final seen = <TypingEvent>[];
      typing.events.listen(seen.add);
      h.only.deliver({'type': 'typing', 'room_id': 'r1', 'user_id': 'u1', 'data': true, 'timestamp': 't'});
      h.only.deliver({'type': 'typing', 'room_id': 'r1', 'user_id': 'u2', 'data': true});
      await settle();
      expect(typing.typingIn('r1'), ['u1', 'u2']);
      h.only.deliver({'type': 'typing', 'room_id': 'r1', 'user_id': 'u1', 'data': false});
      await settle();
      expect(typing.typingIn('r1'), ['u2']);
      expect(seen.map((e) => e.isTyping), [true, true, false]);
      expect(seen.first.timestamp, 't');
      await typing.close();
    });
  });

  group('channels', () {
    test('subscribe, unsubscribe and publish send the TypeScript frames', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await channels.connect();
      expect(h.contexts.single.url.toString(), 'ws://x.test/realtime/channels');

      channels.subscribe('news');
      expect(h.only.last, {'action': 'subscribe', 'channel_id': 'news'});
      channels.subscribe('sports', filter: {'team': 'x'}, fromMessageId: 'm1', fromTimestamp: 't0');
      expect(h.only.last.keys.toList(), ['action', 'channel_id', 'filter', 'fromMessageId', 'fromTimestamp']);
      expect(h.only.last['filter'], {'team': 'x'});
      expect(channels.subscribed, {'news', 'sports'});

      channels.publish('news', {'headline': 'h'});
      final publish = h.only.last;
      expect(publish.keys.toList(), ['action', 'channel_id', 'data', 'timestamp']);
      expect(publish['action'], 'publish');
      expect(publish['data'], {'headline': 'h'});
      expectIso(publish['timestamp']);

      channels.unsubscribe('news');
      expect(h.only.last, {'action': 'unsubscribe', 'channel_id': 'news'});
      expect(channels.subscribed, {'sports'});
      await channels.close();
    });

    test('messages arrive on type or action, and the limit is enforced', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, maxChannels: 1);
      await channels.connect();
      final seen = <ChannelMessage>[];
      channels.messages.listen(seen.add);
      h.only.deliver({'type': 'message', 'channel_id': 'a', 'data': 1, 'user_id': 'u', 'timestamp': 't', 'message_id': 'm'});
      h.only.deliver({'action': 'message', 'channel_id': 'b', 'data': 2, 'id': 'i'});
      h.only.deliver({'type': 'subscribed', 'channel_id': 'a'});
      await settle();
      expect(seen.map((m) => m.channelId), ['a', 'b']);
      expect(seen[0].messageId, 'm');
      expect(seen[1].messageId, 'i');
      channels.subscribe('a');
      expect(() => channels.subscribe('b'), throwsStateError);
      await channels.close();
    });
  });

  group('typed session', () {
    test('any number of listeners hear the messages, and a ping is answered with none listening', () async {
      final h = Harness();
      final session = await ChatSocket(baseUrl: Uri.parse('http://api.test'), connect: h.connect, heartbeat: null)
          .connect(roomId: 'r');
      h.only.deliver({'type': 'system', 'event': 'ping'});
      await settle();
      expect(h.only.sent, [
        {'type': 'system', 'event': 'pong'},
      ]);
      final first = <LineItem>[];
      final second = <LineItem>[];
      session.messages.listen(first.add);
      session.messages.listen(second.add);
      h.only.deliver({'sku': 'a', 'qty': 1});
      await settle();
      expect(first, [const LineItem(sku: 'a', qty: 1)]);
      expect(second, first);
    });

    test('a user frame whose type is system is delivered, and only the keepalive shapes are held back', () async {
      final h = Harness();
      final session = await ChatSocket(baseUrl: Uri.parse('http://api.test'), connect: h.connect, heartbeat: null)
          .connect(roomId: 'r');
      final raw = <Object?>[];
      final decoded = <LineItem>[];
      final errors = <Object>[];
      final tap = session.messages.listen(decoded.add, onError: errors.add);
      h.only.deliver({'type': 'system', 'sku': 's', 'qty': 4});
      h.only.deliver({'type': 'system', 'event': 'welcome', 'sku': 'w', 'qty': 5});
      await settle();
      expect(decoded.map((m) => m.sku), ['s', 'w']);
      expect(errors, isEmpty);
      await tap.cancel();
      expect(raw, isEmpty);
    });

    test('a message that cannot be decoded is an error on the stream, and the next one still arrives', () async {
      final h = Harness();
      final session = await ChatSocket(baseUrl: Uri.parse('http://api.test'), connect: h.connect, heartbeat: null)
          .connect(roomId: 'r');
      final seen = <LineItem>[];
      final errors = <Object>[];
      session.messages.listen(seen.add, onError: errors.add);
      h.only.deliver({'sku': 'a', 'qty': 'many'});
      h.only.deliver({'sku': 'b', 'qty': 2});
      await settle();
      expect(errors, hasLength(1));
      expect(seen, [const LineItem(sku: 'b', qty: 2)]);
    });

    test('a base url keeps its path and its query', () async {
      final h = Harness();
      await ChatSocket(baseUrl: Uri.parse('https://api.test/v1/?tenant=a#top'), connect: h.connect, heartbeat: null)
          .connect(roomId: 'r');
      expect(h.contexts.single.url.toString(), 'wss://api.test/v1/ws/chat/r?tenant=a');
    });

    test('a first connect that fails leaves nothing trying again', () async {
      final h = Harness()..refuse = true;
      await expectLater(
        ChatSocket(baseUrl: Uri.parse('http://api.test'), connect: h.connect, options: quick, heartbeat: null)
            .connect(roomId: 'r'),
        throwsStateError,
      );
      await wait(120);
      expect(h.contexts, hasLength(1));
    });

    test('a platform with no WebTransport is not retried', () async {
      final h = Harness()..unavailable = true;
      await expectLater(
        TelemetryTransport(baseUrl: Uri.parse('https://api.test'), connect: h.connect, options: quick).connect(),
        throwsA(isA<TransportUnavailable>()),
      );
      await wait(100);
      expect(h.contexts, hasLength(1));
    });

    test('a typed socket survives a drop, and its sends wait for the new connection', () async {
      final h = Harness();
      final session = await ChatSocket(
        baseUrl: Uri.parse('http://api.test'),
        connect: h.connect,
        options: quick,
        heartbeat: null,
      ).connect(roomId: 'r');
      final seen = <LineItem>[];
      session.messages.listen(seen.add);
      await h.only.close();
      await settle();
      expect(session.state, LiveConnectionState.reconnecting);
      final waiting = session.send(const LineItem(sku: 'w', qty: 1));
      expect(session.queueSize, 1);
      await until(() => h.connections.length == 2);
      await waiting;
      expect(h.connections.last.sent, [
        {'qty': 1, 'sku': 'w'},
      ]);
      h.connections.last.deliver({'sku': 'after', 'qty': 2});
      await settle();
      expect(seen.single.sku, 'after');
      expect(session.state, LiveConnectionState.connected);
    });

    test('closing a typed socket stops its heartbeat and reports closed', () async {
      final h = Harness();
      final session = await ChatSocket(
        baseUrl: Uri.parse('http://api.test'),
        connect: h.connect,
        heartbeat: const Duration(milliseconds: 15),
      ).connect(roomId: 'r');
      await wait(50);
      await session.disconnect();
      final count = h.only.attempts;
      await wait(60);
      expect(h.only.attempts, count);
      expect(session.state, LiveConnectionState.closed);
      await session.closed;
    });
  });

  group('connection', () {
    test('the url builder merges the token into the base query and keeps the path', () {
      expect(
        streamUri(Uri.parse('https://h.test/base/?a=1&token=old'), '/ws/x%20y', token: 't+1').toString(),
        'wss://h.test/base/ws/x%20y?a=1&token=t%2B1',
      );
      expect(streamUri(Uri.parse('http://h.test:8080'), '/ws').toString(), 'ws://h.test:8080/ws');
      expect(streamUri(Uri.parse('https://h.test/v1'), '/sse', socket: false).toString(), 'https://h.test/v1/sse');
      expect(bearerToken({'authorization': 'bearer abc '}), 'abc');
      expect(bearerToken({'X-API-Key': 'k'}), isNull);
    });

    test('native sends the credentials as headers and web sends the bearer token as a query parameter', () async {
      final h = Harness();
      final options = LiveOptions(
        credentials: () => {'Authorization': 'Bearer secret', 'X-API-Key': 'key'},
      );
      await liveOpen(
        connect: h.connect,
        baseUrl: Uri.parse('https://api.test/v1?tenant=a'),
        path: '/ws/x',
        endpoint: '/ws/x',
        headers: {'x-extra': 'e'},
        options: options,
        web: false,
      )(0);
      expect(h.contexts.last.headers, {'x-extra': 'e', 'Authorization': 'Bearer secret', 'X-API-Key': 'key'});
      expect(h.contexts.last.url.toString(), 'wss://api.test/v1/ws/x?tenant=a');

      await liveOpen(
        connect: h.connect,
        baseUrl: Uri.parse('https://api.test/v1?tenant=a'),
        path: '/ws/x',
        endpoint: '/ws/x',
        options: options,
        web: true,
      )(1);
      expect(h.contexts.last.url.toString(), 'wss://api.test/v1/ws/x?tenant=a&token=secret');
      expect(h.contexts.last.attempt, 1);
    });

    test('credentials given to a client reach its connect', () async {
      final h = Harness();
      final rooms = RoomClient(
        baseUrl: Uri.parse('https://api.test'),
        connect: h.connect,
        options: LiveOptions(credentials: () async => {'Authorization': 'Bearer t'}),
      );
      await rooms.connect();
      expect(h.contexts.single.headers['Authorization'], 'Bearer t');
      await rooms.close();
    });

    test('a connect that takes too long is an error, then a reconnect, and the late connection is closed', () async {
      final h = Harness()..firstDelay = const Duration(milliseconds: 150);
      final rooms = RoomClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(
          connectionTimeout: Duration(milliseconds: 40),
          reconnectDelay: Duration(milliseconds: 10),
          maxReconnectDelay: Duration(milliseconds: 20),
        ),
      );
      final states = <LiveConnectionState>[];
      rooms.states.listen(states.add);
      await expectLater(
        rooms.connect(),
        throwsA(isA<TimeoutException>().having((e) => e.message, 'message', 'Connection timeout')),
      );
      await until(() => rooms.state == LiveConnectionState.connected);
      expect(states.take(4), [
        LiveConnectionState.connecting,
        LiveConnectionState.error,
        LiveConnectionState.reconnecting,
        LiveConnectionState.connecting,
      ]);
      await until(() => h.connections.length == 2);
      // The attempt that timed out connects late, after the retry did, and is closed unused.
      await h.connections[1].closed.timeout(const Duration(seconds: 2));
      expect(rooms.state, LiveConnectionState.connected);
      await rooms.close();
    });

    test('the state follows a drop and a close, and an errored closed future counts as a drop', () async {
      final h = Harness();
      final rooms = RoomClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(reconnect: false),
      );
      final states = <LiveConnectionState>[];
      rooms.states.listen(states.add);
      await rooms.connect();
      h.only.failClosed(StateError('socket died'));
      await settle();
      expect(rooms.state, LiveConnectionState.disconnected);
      await rooms.connect();
      expect(rooms.state, LiveConnectionState.connected);
      await rooms.disconnect();
      expect(states, [
        LiveConnectionState.connecting,
        LiveConnectionState.connected,
        LiveConnectionState.disconnected,
        LiveConnectionState.connecting,
        LiveConnectionState.connected,
        LiveConnectionState.closed,
      ]);
    });
  });

  group('reconnect', () {
    test('rooms join again with their metadata and role, and only then send what waited', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      await rooms.connect();
      final join = rooms.join('r1', metadata: {'k': 'v'}, role: 'admin');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id'], 'room_name': 'Lobby'});
      await join;

      await h.only.close();
      await settle();
      expect(rooms.state, LiveConnectionState.reconnecting);
      expect(rooms.joined, {'r1'});
      expect(rooms.roomConnected('r1'), isFalse);
      final waiting = rooms.send('r1', 'while down');
      await until(() => h.connections.length == 2);
      await settle();
      final second = h.connections.last;
      expect(second.sent, hasLength(1));
      final rejoin = second.frame(0);
      expect(rejoin.keys.toList(), ['type', 'request_id', 'room_id', 'metadata', 'role']);
      expect(rejoin['type'], 'join');
      expect(rejoin['metadata'], {'k': 'v'});
      expect(rejoin['role'], 'admin');
      second.deliver({'request_id': rejoin['request_id'], 'room_name': 'Lobby'});
      await waiting;
      expect(second.sent, hasLength(2));
      expect(second.frame(1)['type'], 'message');
      expect(second.frame(1)['data'], 'while down');
      expect(rooms.roomConnected('r1'), isTrue);
      expect(rooms.roomName('r1'), 'Lobby');
      await rooms.close();
    });

    test('a room that cannot be joined again is marked not connected and held', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick, maxRooms: 1);
      await rooms.connect();
      final join = rooms.join('r1');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id']});
      await join;
      final errors = <String>[];
      rooms.errors.listen(errors.add);
      await h.only.close();
      await until(() => h.connections.length == 2);
      await settle();
      h.connections.last.deliver({'request_id': h.connections.last.frame(0)['request_id'], 'error': 'gone'});
      await settle();
      expect(rooms.roomConnected('r1'), isFalse);
      expect(rooms.joined, {'r1'});
      expect(errors.single, contains('r1'));
      await rooms.close();
    });

    test('a queued message for a room left meanwhile fails', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      await rooms.connect();
      final join = rooms.join('r1');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id']});
      await join;
      await h.only.close();
      await settle();
      final waiting = rooms.send('r1', 'x');
      final failure = expectLater(
        waiting,
        throwsA(isA<StateError>().having((e) => e.message, 'message', 'No longer joined to room')),
      );
      rooms.leave('r1');
      await until(() => h.connections.length == 2);
      await failure;
      await rooms.close();
    });

    test('presence follows the same users again, and sends no status when none was ever set', () async {
      final h = Harness();
      final presence = PresenceClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      await presence.connect();
      presence.subscribe(['u1', 'u2']);
      await h.only.close();
      await until(() => h.connections.length == 2);
      await settle();
      expect(h.connections.last.sent, [
        {'type': 'subscribe_presence', 'user_ids': ['u1', 'u2']},
      ]);
      await presence.close();
    });

    test('presence sets the status again first, when one was set', () async {
      final h = Harness();
      final presence = PresenceClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      await presence.connect();
      presence.setStatus('busy', customMessage: 'deep work');
      presence.subscribe(['u1']);
      await h.only.close();
      await until(() => h.connections.length == 2);
      await settle();
      final second = h.connections.last;
      expect(second.sent, hasLength(2));
      expect(second.frame(0)['type'], 'presence');
      expect(second.frame(0)['status'], 'busy');
      expect(second.frame(0)['custom_status'], 'deep work');
      expect(second.frame(1)['type'], 'subscribe_presence');
      await presence.close();
    });

    test('channels subscribe again with the options they subscribed with', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick, maxChannels: 2);
      await channels.connect();
      channels
        ..subscribe('news', filter: {'t': 1}, fromMessageId: 'm1')
        ..subscribe('plain');
      await h.only.close();
      await until(() => h.connections.length == 2);
      await settle();
      final second = h.connections.last;
      expect(second.sent, hasLength(2));
      expect(second.frame(0), {
        'action': 'subscribe',
        'channel_id': 'news',
        'filter': {'t': 1},
        'fromMessageId': 'm1',
      });
      expect(second.frame(1), {'action': 'subscribe', 'channel_id': 'plain'});
      expect(channels.subscribed, {'news', 'plain'});
      await channels.close();
    });

    test('giving up closes the socket and fails every queued send', () async {
      final h = Harness();
      final channels = ChannelClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(
          maxReconnectAttempts: 2,
          reconnectDelay: Duration(milliseconds: 5),
          maxReconnectDelay: Duration(milliseconds: 5),
        ),
      );
      await channels.connect();
      h.refuse = true;
      await h.only.close();
      await settle();
      final waiting = channels.publish('a', 1);
      await expectLater(
        waiting,
        throwsA(isA<StateError>().having((e) => e.message, 'message', 'Max reconnection attempts reached')),
      );
      expect(channels.state, LiveConnectionState.closed);
      expect(h.contexts, hasLength(3));
    });

    test('with reconnecting off a drop is just a drop', () async {
      final h = Harness();
      final typing = TypingClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(reconnect: false),
      );
      await typing.connect();
      await h.only.close();
      await wait(60);
      expect(typing.state, LiveConnectionState.disconnected);
      expect(h.contexts, hasLength(1));
    });
  });

  group('offline queue', () {
    test('a send while not connected waits, then goes out in order with its timestamp taken then', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      final a = channels.publish('c', 'a');
      final b = channels.publish('c', 'b');
      final c = channels.publish('c', 'c');
      expect(channels.queueSize, 3);
      await wait(30);
      await channels.connect();
      await Future.wait([a, b, c]);
      expect(h.only.sent.map((f) => (f! as Map<Object?, Object?>)['data']), ['a', 'b', 'c']);
      expect(channels.queueSize, 0);
      final stamped = DateTime.parse(h.only.frame(0)['timestamp']! as String);
      expect(DateTime.now().toUtc().difference(stamped).inMilliseconds, lessThan(25));
      await channels.close();
    });

    test('a full queue refuses the next send, and a switched-off queue refuses all of them', () async {
      final h = Harness();
      final channels = ChannelClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(maxQueueSize: 2),
      );
      final first = channels.publish('c', 1);
      final second = channels.publish('c', 2);
      await expectLater(
        channels.publish('c', 3),
        throwsA(isA<StateError>().having((e) => e.message, 'message', 'Message queue full')),
      );
      expect(channels.queueSize, 2);
      final cleared = Future.wait([
        expectLater(first, throwsA(isA<StateError>().having((e) => e.message, 'message', 'Queue cleared'))),
        expectLater(second, throwsA(isA<StateError>().having((e) => e.message, 'message', 'Queue cleared'))),
      ]);
      channels.clearQueue();
      await cleared;
      expect(channels.queueSize, 0);

      final off = ChannelClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(enableOfflineQueue: false),
      );
      await expectLater(
        off.publish('c', 1),
        throwsA(isA<StateError>().having((e) => e.message, 'message', 'WebSocket is not connected')),
      );
    });

    test('a send that waited past its time fails when the connection opens', () async {
      final h = Harness();
      final channels = ChannelClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(queueMessageTtl: Duration(milliseconds: 30)),
      );
      final stale = channels.publish('c', 'old');
      final failure = expectLater(
        stale,
        throwsA(isA<StateError>().having((e) => e.message, 'message', 'Message expired in queue')),
      );
      await wait(80);
      final fresh = channels.publish('c', 'new');
      await channels.connect();
      await failure;
      await fresh;
      expect(h.only.sent.map((f) => (f! as Map<Object?, Object?>)['data']), ['new']);
      await channels.close();
    });

    test('a send the transport refuses closes the connection, and the kept entries go out in order after the reopen', () async {
      final h = Harness()..sendLimit = 1;
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      final a = channels.publish('c', 'a');
      final b = channels.publish('c', 'b');
      final c = channels.publish('c', 'c');
      await channels.connect();
      h.sendLimit = null;
      await a;
      expect(h.connections.first.sent, hasLength(1));
      await until(() => h.connections.length == 2);
      await Future.wait([b, c]);
      expect(h.connections.first.sent.map((f) => (f! as Map<Object?, Object?>)['data']), ['a']);
      expect(h.connections.last.sent.map((f) => (f! as Map<Object?, Object?>)['data']), ['b', 'c']);
      await channels.close();
    });

    test('a message that is not JSON fails alone: the rest of the queue goes out and live sends go straight out', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      final a = channels.publish('c', 'a');
      final poison = channels.publish('c', Object());
      final poisonFailure = expectLater(poison, throwsA(isA<JsonUnsupportedObjectError>()));
      final b = channels.publish('c', 'b');
      await channels.connect();
      await Future.wait([a, b, poisonFailure]);
      expect(h.only.sent.map((f) => (f! as Map<Object?, Object?>)['data']), ['a', 'b']);
      expect(channels.queueSize, 0);
      final live = channels.publish('c', 'live');
      expect(h.only.sent, hasLength(3), reason: 'a live send goes out at once');
      await live;
      expect(channels.state, LiveConnectionState.connected);
      await channels.close();
    });

    test('a live send the transport cannot encode fails alone, and one it cannot send closes the connection', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      await channels.connect();
      await expectLater(channels.publish('c', Object()), throwsA(isA<JsonUnsupportedObjectError>()));
      expect(channels.state, LiveConnectionState.connected);
      await channels.publish('c', 'fine');
      expect(h.only.sent, hasLength(1));

      h.only.broken = true;
      await expectLater(channels.publish('c', 'lost'), throwsStateError);
      await h.only.closed.timeout(const Duration(seconds: 2));
      await until(() => h.connections.length == 2);
      await channels.close();
    });

    test('closing rejects what waits unless told to keep it', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      final rejected = expectLater(
        channels.publish('c', 1),
        throwsA(isA<StateError>().having((e) => e.message, 'message', 'Connection closed')),
      );
      await channels.disconnect();
      await rejected;

      final kept = channels.publish('c', 2);
      await channels.disconnect(rejectQueued: false);
      expect(channels.queueSize, 1);
      await channels.connect();
      await kept;
      expect(h.only.sent.map((f) => (f! as Map<Object?, Object?>)['data']), [2]);
      await channels.close();
    });
  });

  group('lifecycle', () {
    test('with reconnecting off a drop completes closed and ends the message stream', () async {
      final h = Harness();
      final session = await ChatSocket(
        baseUrl: Uri.parse('http://api.test'),
        connect: h.connect,
        options: const LiveOptions(reconnect: false),
        heartbeat: null,
      ).connect(roomId: 'r');
      final all = session.messages.toList();
      h.only.deliver({'sku': 'a', 'qty': 1});
      await settle();
      await h.only.close();
      await session.closed.timeout(const Duration(seconds: 2));
      expect((await all.timeout(const Duration(seconds: 2))).map((m) => m.sku), ['a']);

      var done = false;
      await session.connect();
      unawaited(session.closed.then((_) => done = true));
      await settle();
      expect(done, isFalse, reason: 'a new connect starts a new closed future');
      await session.close();
      await settle();
      expect(done, isTrue);
    });

    test('giving up completes closed', () async {
      final socket = LiveSocket(
        open: (_) async => throw StateError('refused'),
        options: const LiveOptions(
          maxReconnectAttempts: 1,
          reconnectDelay: Duration(milliseconds: 5),
          maxReconnectDelay: Duration(milliseconds: 5),
        ),
      );
      await expectLater(socket.connect(), throwsStateError);
      await socket.closed.timeout(const Duration(seconds: 2));
      expect(socket.state, LiveConnectionState.closed);
      await socket.dispose();
    });

    test('a disconnect while an open is pending lets the next connect open, and the late connection is closed', () async {
      final gate = Completer<StreamConnection>();
      final late = FakeConnection();
      final fresh = FakeConnection();
      var opens = 0;
      final socket = LiveSocket(
        open: (_) {
          opens++;
          return opens == 1 ? gate.future : Future.value(fresh);
        },
        options: const LiveOptions(reconnect: false),
      );
      final first = socket.connect();
      await settle();
      await socket.disconnect();
      await socket.connect();
      expect(socket.state, LiveConnectionState.connected);
      gate.complete(late);
      await first;
      await late.closed.timeout(const Duration(seconds: 2));
      expect(socket.state, LiveConnectionState.connected);
      expect(socket.connection, same(fresh));
      await socket.dispose();
    });

    test('a disconnect during a slow re-join lets the next connect end with an open socket', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      await rooms.connect();
      final join = rooms.join('r1');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id']});
      await join;
      await h.only.close();
      await until(() => h.connections.length == 2);
      await settle();
      expect(h.connections.last.sent, hasLength(1));
      await rooms.disconnect();
      await rooms.connect();
      expect(h.connections, hasLength(3));
      expect(rooms.state, LiveConnectionState.connected);
      await rooms.close();
    });

    test('a stale attempt that finishes late does not mark the new connection ready or flush its queue', () async {
      final gates = [Completer<void>(), Completer<void>()];
      var opened = 0;
      final connections = <FakeConnection>[];
      final socket = LiveSocket(
        open: (_) async {
          final connection = FakeConnection();
          connections.add(connection);
          return connection;
        },
        options: const LiveOptions(reconnect: false),
        onOpen: (_) => gates[opened++].future,
      );
      final first = socket.connect();
      await until(() => opened == 1);
      await socket.disconnect();
      final second = socket.connect();
      await until(() => opened == 2);
      final waiting = socket.deliver(() => 'queued');
      gates[0].complete();
      await first;
      await settle();
      expect(connections.last.sent, isEmpty, reason: 'the new attempt has not finished its own setup');
      gates[1].complete();
      await second;
      await waiting;
      expect(connections.last.sent, ['queued']);
      await socket.dispose();
    });

    test('a drop while a re-open is still setting up is retried and ends connected', () async {
      final gate = Completer<void>();
      final h = Harness();
      var setups = 0;
      final socket = LiveSocket(
        open: liveOpen(connect: h.connect, baseUrl: Uri.parse('http://x.test'), path: '/p', endpoint: '/p', options: quick),
        options: quick,
        onOpen: (_) => setups++ == 1 ? gate.future : Future.value(),
      );
      await socket.connect();
      await h.only.close();
      await until(() => h.connections.length == 2);
      await settle();
      await h.connections.last.close();
      await until(() => h.connections.length == 3);
      gate.complete();
      await until(() => socket.state == LiveConnectionState.connected && socket.connection == h.connections.last);
      await settle();
      expect(h.connections, hasLength(3));
      expect(socket.state, LiveConnectionState.connected);
      await socket.deliver(() => 'live');
      expect(h.connections.last.sent, ['live']);
      await socket.dispose();
    });

    test('a re-open that was dropped does not flush the queue of the connection that replaced it', () async {
      final gates = [Completer<void>(), Completer<void>()];
      final h = Harness();
      var setups = 0;
      final socket = LiveSocket(
        open: liveOpen(connect: h.connect, baseUrl: Uri.parse('http://x.test'), path: '/p', endpoint: '/p', options: quick),
        options: quick,
        onOpen: (_) => setups == 0 ? Future.value(setups++) : gates[setups++ - 1].future,
      );
      await socket.connect();
      await h.only.close();
      await until(() => h.connections.length == 2);
      await h.connections.last.close();
      await until(() => h.connections.length == 3);
      final waiting = socket.deliver(() => 'queued');
      gates[0].complete();
      await settle();
      expect(h.connections.last.sent, isEmpty, reason: 'the new connection has not finished its own setup');
      gates[1].complete();
      await waiting;
      expect(h.connections.last.sent, ['queued']);
      await socket.dispose();
    });

    test('a timeout that comes after close leaves the state closed', () async {
      final socket = LiveSocket(
        open: (_) => Completer<StreamConnection>().future,
        options: const LiveOptions(connectionTimeout: Duration(milliseconds: 40), reconnect: false),
      );
      final pending = socket.connect();
      await settle();
      await socket.dispose();
      await pending;
      await wait(120);
      expect(socket.state, LiveConnectionState.closed);
    });

    test('a send made during a slow re-join goes out after the re-join and after what already waited', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect, options: quick);
      await rooms.connect();
      final join = rooms.join('r1');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id']});
      await join;
      await h.only.close();
      await settle();
      final first = rooms.send('r1', 'while down');
      await until(() => h.connections.length == 2);
      await settle();
      final second = h.connections.last;
      expect(second.sent, hasLength(1));
      final during = rooms.send('r1', 'during rejoin');
      expect(second.sent, hasLength(1), reason: 'it must not overtake the queue');
      second.deliver({'request_id': second.frame(0)['request_id']});
      await Future.wait([first, during]);
      expect(second.sent.map((f) => (f! as Map<Object?, Object?>)['type']), ['join', 'message', 'message']);
      expect(second.frame(1)['data'], 'while down');
      expect(second.frame(2)['data'], 'during rejoin');
      await rooms.close();
    });

    test('an open that throws at once fails the connect and leaves the socket usable', () async {
      var broken = true;
      final good = FakeConnection();
      final socket = LiveSocket(
        open: (_) {
          if (broken) throw StateError('sync failure');
          return Future.value(good);
        },
        options: const LiveOptions(reconnect: false),
      );
      await expectLater(socket.connect(), throwsStateError);
      expect(socket.state, LiveConnectionState.error);
      broken = false;
      await socket.connect();
      expect(socket.state, LiveConnectionState.connected);
      await socket.dispose();
    });

    test('closing with a send nobody awaits raises no uncaught error, and an awaiting caller still sees it', () async {
      final errors = <Object>[];
      Object? seen;
      await runZonedGuarded(() async {
        final h = Harness();
        final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
        channels.publish('c', 'ignored');
        final awaited = channels.publish('c', 'awaited');
        unawaited(awaited.then((_) {}, onError: (Object e) => seen = e));
        await channels.close();
        await settle();
      }, (error, stack) => errors.add(error));
      expect(errors, isEmpty);
      expect(seen, isA<StateError>());
    });
  });

  group('timers', () {
    test('a heartbeat that cannot be sent is reported and does not throw', () async {
      final h = Harness();
      final presence = PresenceClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(reconnect: false),
        heartbeat: const Duration(milliseconds: 15),
      );
      final errors = <String>[];
      presence.errors.listen(errors.add);
      await presence.connect();
      h.only.broken = true;
      await wait(80);
      expect(errors, isNotEmpty);
      await presence.close();
    });

    test('a typing frame that cannot be sent is reported and does not throw', () async {
      final h = Harness();
      final typing = TypingClient(
        baseUrl: Uri.parse('http://x.test'),
        connect: h.connect,
        options: const LiveOptions(reconnect: false),
        debounce: const Duration(milliseconds: 10),
        timeout: const Duration(milliseconds: 40),
      );
      final errors = <String>[];
      typing.errors.listen(errors.add);
      await typing.connect();
      typing.start('r1');
      h.only.broken = true;
      await wait(120);
      expect(errors, isNotEmpty);
      await typing.close();
    });
  });

  group('rooms extras', () {
    test('broadcast sends, per-room streams filter, and the room name is read from frames', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await rooms.connect();
      final join = rooms.join('a');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id']});
      await join;
      final inA = <RoomMessage>[];
      rooms.messagesIn('a').listen(inA.add);
      final joins = <Map<String, Object?>>[];
      rooms.memberJoins('a').listen(joins.add);
      h.only.deliver({'type': 'message', 'room_id': 'a', 'data': 1, 'room_name': 'Alpha'});
      h.only.deliver({'type': 'message', 'room_id': 'b', 'data': 2});
      h.only.deliver({'type': 'member_join', 'room_id': 'a', 'member': {'user_id': 'u'}});
      h.only.deliver({'type': 'member_join', 'room_id': 'b', 'member': {'user_id': 'v'}});
      await settle();
      expect(inA.map((m) => m.data), [1]);
      expect(joins, [
        {'user_id': 'u'},
      ]);
      expect(rooms.roomName('a'), 'Alpha');
      await rooms.broadcast('a', 'hi');
      expect(h.only.last['type'], 'message');
      expect(h.only.last['data'], 'hi');
      await rooms.close();
    });

    test('a disconnect forgets the rooms, and a join again starts fresh', () async {
      final h = Harness();
      final rooms = RoomClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await rooms.connect();
      final join = rooms.join('a');
      await settle();
      h.only.deliver({'request_id': h.only.last['request_id']});
      await join;
      await rooms.disconnect();
      expect(rooms.joined, isEmpty);
      expect(rooms.state, LiveConnectionState.closed);
    });
  });

  group('channels extras', () {
    test('subscribed and unsubscribed answers arrive as acknowledgements', () async {
      final h = Harness();
      final channels = ChannelClient(baseUrl: Uri.parse('http://x.test'), connect: h.connect);
      await channels.connect();
      final acks = <ChannelAck>[];
      channels.acks.listen(acks.add);
      h.only.deliver({'type': 'subscribed', 'channel_id': 'a'});
      h.only.deliver({'type': 'unsubscribed', 'channel_id': 'a'});
      await settle();
      expect(acks.map((a) => (a.channelId, a.subscribed)), [('a', true), ('a', false)]);
      await channels.close();
    });
  });

  group('unified client', () {
    test('it passes its options down, lets a feature override them and connects the features chosen', () async {
      final h = Harness();
      final hub = StreamingClient(
        baseUrl: Uri.parse('https://api.test'),
        connect: h.connect,
        options: LiveOptions(credentials: () => {'Authorization': 'Bearer all'}),
        typingOptions: LiveOptions(credentials: () => {'Authorization': 'Bearer typing'}),
      );
      await hub.connect(presence: false, channels: false);
      expect(h.contexts.map((c) => c.url.path), ['/realtime/rooms', '/realtime/typing']);
      expect(h.contexts[0].headers['Authorization'], 'Bearer all');
      expect(h.contexts[1].headers['Authorization'], 'Bearer typing');
      expect(hub.clientStates.rooms, LiveConnectionState.connected);
      expect(hub.clientStates.presence, LiveConnectionState.disconnected);
      // Presence and channels were not chosen, so they are not part of the whole.
      expect(hub.state, LiveConnectionState.connected);
      await hub.close();
    });

    test('its state gathers the features, and it forwards their changes and errors', () async {
      final h = Harness();
      final hub = StreamingClient(baseUrl: Uri.parse('https://api.test'), connect: h.connect, options: const LiveOptions(reconnect: false));
      final overall = <LiveConnectionState>[];
      hub.states.listen(overall.add);
      final changes = <String>[];
      hub.clientStateChanges.listen((c) => changes.add('${c.client}:${c.state.name}'));
      final errors = <String>[];
      hub.errors.listen(errors.add);

      await hub.connect();
      await settle();
      expect(hub.state, LiveConnectionState.connected);
      expect(overall, contains(LiveConnectionState.connecting));
      expect(overall.last, LiveConnectionState.connected);
      expect(changes, containsAll(['rooms:connected', 'presence:connected', 'typing:connected', 'channels:connected']));

      await h.connections[1].close();
      await settle();
      expect(hub.clientStates.presence, LiveConnectionState.disconnected);
      expect(hub.state, LiveConnectionState.disconnected);

      h.connections[0].deliver({'type': 'error', 'message': 'boom'});
      h.connections[3].deliver({'type': 'error', 'message': 'bang'});
      await settle();
      expect(errors, containsAll(['boom', 'bang']));

      await hub.disconnect();
      expect(hub.state, LiveConnectionState.closed);
      await hub.close();
    });

    test('a feature that fails to connect fails the call and puts the state in error', () async {
      final h = Harness()..refuse = true;
      final hub = StreamingClient(baseUrl: Uri.parse('https://api.test'), connect: h.connect, options: const LiveOptions(reconnect: false));
      await expectLater(hub.connect(), throwsStateError);
      expect(hub.state, LiveConnectionState.error);
      await hub.close();
    });
  });

  test('the unified client opens every feature on its own path and closes them', () async {
    final h = Harness();
    final hub = StreamingClient(baseUrl: Uri.parse('https://api.test'), connect: h.connect, headers: {'x': 'y'});
    await hub.connect();
    expect(h.contexts.map((c) => c.url.toString()), [
      'wss://api.test/realtime/rooms',
      'wss://api.test/realtime/presence',
      'wss://api.test/realtime/typing',
      'wss://api.test/realtime/channels',
    ]);
    expect(h.contexts.every((c) => c.headers['x'] == 'y'), isTrue);
    await hub.close();
    expect(await Future.wait(h.connections.map((c) => c.closed.then((_) => true))), everyElement(isTrue));
    await expectLater(hub.rooms.send('r', 1), throwsStateError);
  });
}
`

// TestGeneratedStreamingRunsAtRuntime runs the typed sockets and the feature
// clients over a fake connection and checks the frames they send.
func TestGeneratedStreamingRunsAtRuntime(t *testing.T) {
	runGeneratedTest(t, streamingFixture(), "streaming", streamingRuntimeTest)
}
