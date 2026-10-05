package dart

import "testing"

// streamingRuntimeTest drives the generated streaming clients over a fake
// StreamConnection, asserting the frames each one sends. The expected frames
// are the ones the TypeScript feature clients send (rooms.ts, presence.ts,
// typing.ts and channels.ts), so one server serves both.
const streamingRuntimeTest = `import 'dart:async';

import 'package:forge_client/forge_client.dart' show StreamConnect, StreamConnectContext, StreamConnection;
import 'package:streaming_forge_client/streaming_forge_client.dart';
import 'package:test/test.dart';

final class FakeConnection implements StreamConnection {
  final _incoming = StreamController<Object?>();
  final _closed = Completer<void>();
  final sent = <Object?>[];

  /// Every send, including the ones refused because the connection closed.
  var attempts = 0;

  void deliver(Object? message) => _incoming.add(message);

  @override
  Stream<Object?> get messages => _incoming.stream;

  @override
  Future<void> get closed => _closed.future;

  @override
  void send(Object? message) {
    attempts++;
    if (_closed.isCompleted) throw StateError('closed');
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

  StreamConnect get connect => (context) async {
    contexts.add(context);
    final connection = FakeConnection();
    connections.add(connection);
    return connection;
  };

  FakeConnection get only => connections.single;
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
    expect(() => hub.rooms.send('r', 1), throwsStateError);
  });
}
`

// TestGeneratedStreamingRunsAtRuntime runs the typed sockets and the feature
// clients over a fake connection and checks the frames they send.
func TestGeneratedStreamingRunsAtRuntime(t *testing.T) {
	runGeneratedTest(t, streamingFixture(), "streaming", streamingRuntimeTest)
}
