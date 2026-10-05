package dart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// featureFile is one streaming feature client: rooms, presence, typing or
// channels. Each speaks the same JSON frames the TypeScript feature clients
// send, so one server serves both.
type featureFile struct {
	file   string
	class  string
	option string
}

// renderFeatures renders the feature clients the config enables, and the
// StreamingClient that composes them when the unified client is on. A
// feature's socket path comes from the AsyncAPI document when it declares one.
func renderFeatures(spec *client.APISpec, config client.GeneratorConfig) map[string]string {
	files := map[string]string{}

	// path renders a socket path for a Dart string literal and, for a
	// comment, for one line of documentation.
	path := func(declared, fallback string) (literal, doc string) {
		if declared != "" {
			fallback = declared
		}

		return escapeInString(fallback), docText(fallback)
	}

	expand := func(template string, pairs ...string) string {
		return strings.NewReplacer(pairs...).Replace(template)
	}

	streaming := spec.Streaming

	var enabled []featureFile

	if config.ShouldGenerateRoomClient() {
		declared := ""
		if streaming != nil && streaming.Rooms != nil {
			declared = streaming.Rooms.Path
		}

		history := ""
		if config.Streaming.EnableHistory {
			history = roomsHistory
		}

		maxRooms := config.Streaming.RoomConfig.MaxRoomsPerUser
		if maxRooms <= 0 {
			maxRooms = 50
		}

		literal, doc := path(declared, "/ws")
		files["lib/src/streaming/rooms.dart"] = expand(roomsTemplate,
			"{{HISTORY}}", history,
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{MAX_ROOMS}}", strconv.Itoa(maxRooms),
		)

		enabled = append(enabled, featureFile{"rooms", "RoomClient", "room"})
	}

	if config.ShouldGeneratePresenceClient() {
		declared := ""
		if streaming != nil && streaming.Presence != nil {
			declared = streaming.Presence.Path
		}

		statuses := config.Streaming.PresenceConfig.Statuses
		if streaming != nil && streaming.Presence != nil && len(streaming.Presence.Statuses) > 0 {
			statuses = streaming.Presence.Statuses
		}

		if len(statuses) == 0 {
			statuses = []string{"online", "away", "busy", "offline"}
		}

		heartbeat := config.Streaming.PresenceConfig.HeartbeatIntervalMs
		if heartbeat <= 0 {
			heartbeat = 30000
		}

		literal, doc := path(declared, "/presence")
		files["lib/src/streaming/presence.dart"] = expand(presenceTemplate,
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{HEARTBEAT_MS}}", strconv.Itoa(heartbeat),
			"{{STATUSES}}", dartStringList(statuses),
		)

		enabled = append(enabled, featureFile{"presence", "PresenceClient", "presence"})
	}

	if config.ShouldGenerateTypingClient() {
		declared := ""
		if streaming != nil && streaming.Typing != nil {
			declared = streaming.Typing.Path
		}

		timeout := config.Streaming.TypingConfig.TimeoutMs
		if timeout <= 0 {
			timeout = 3000
		}

		debounce := config.Streaming.TypingConfig.DebounceMs
		if debounce <= 0 {
			debounce = 300
		}

		literal, doc := path(declared, "/typing")
		files["lib/src/streaming/typing.dart"] = expand(typingTemplate,
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{TIMEOUT_MS}}", strconv.Itoa(timeout),
			"{{DEBOUNCE_MS}}", strconv.Itoa(debounce),
		)

		enabled = append(enabled, featureFile{"typing", "TypingClient", "typing"})
	}

	if config.ShouldGenerateChannelClient() {
		declared := ""
		if streaming != nil && streaming.Channels != nil {
			declared = streaming.Channels.Path
		}

		maxChannels := config.Streaming.ChannelConfig.MaxChannelsPerUser
		if maxChannels <= 0 {
			maxChannels = 100
		}

		literal, doc := path(declared, "/channels")
		files["lib/src/streaming/channels.dart"] = expand(channelsTemplate,
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{MAX_CHANNELS}}", strconv.Itoa(maxChannels),
		)

		enabled = append(enabled, featureFile{"channels", "ChannelClient", "channel"})
	}

	if config.ShouldGenerateUnifiedStreamingClient() && len(enabled) > 0 {
		files["lib/src/streaming/streaming_client.dart"] = renderStreamingClient(enabled)
	}

	return files
}

// renderStreamingClient composes the enabled feature clients over one base
// URL, with each one's state, errors and settings gathered in one place.
func renderStreamingClient(enabled []featureFile) string {
	var b strings.Builder

	b.WriteString(generatedHeader)
	b.WriteString("\nimport 'dart:async';\n\n")
	b.WriteString("import 'package:forge_client/forge_client.dart' show StreamConnect;\n\n")

	for _, f := range enabled {
		fmt.Fprintf(&b, "import '%s.dart';\n", f.file)
	}

	b.WriteString("import 'live_socket.dart' show LiveConnectionState, LiveOptions;\n")

	b.WriteString("\n/// Every streaming feature this API offers, over one base URL.\n")
	b.WriteString("///\n")
	b.WriteString("/// [options] apply to every feature. A feature's own options replace them for\n")
	b.WriteString("/// that feature alone.\n")
	b.WriteString("final class StreamingClient {\n")
	b.WriteString("  /// Creates the feature clients; none connects until asked.\n")
	b.WriteString("  StreamingClient({\n")
	b.WriteString("    required Uri baseUrl,\n")
	b.WriteString("    StreamConnect? connect,\n")
	b.WriteString("    Map<String, String> headers = const {},\n")
	b.WriteString("    LiveOptions options = const LiveOptions(),\n")

	for _, f := range enabled {
		fmt.Fprintf(&b, "    LiveOptions? %sOptions,\n", f.option)
	}

	b.WriteString("  })")

	for i, f := range enabled {
		lead := "\n      : "
		if i > 0 {
			lead = "\n        "
		}

		end := ","
		if i == len(enabled)-1 {
			end = " {"
		}

		fmt.Fprintf(&b, "%s%s = %s(baseUrl: baseUrl, connect: connect, headers: headers, options: %sOptions ?? options)%s", lead, f.file, f.class, f.option, end)
	}

	b.WriteString("\n")

	for _, f := range enabled {
		fmt.Fprintf(&b, "    _watch('%s', %s.states, %s.errors);\n", f.file, f.file, f.file)
	}

	b.WriteString("  }\n")

	for _, f := range enabled {
		fmt.Fprintf(&b, "\n  /// The %s client.\n", f.file)
		fmt.Fprintf(&b, "  final %s %s;\n", f.class, f.file)
	}

	b.WriteString("\n  final _states = StreamController<LiveConnectionState>.broadcast();\n")
	b.WriteString("  final _changes = StreamController<({String client, LiveConnectionState state})>.broadcast();\n")
	b.WriteString("  final _errors = StreamController<String>.broadcast();\n")
	b.WriteString("  final _watching = <StreamSubscription<Object?>>[];\n")
	b.WriteString("  var _state = LiveConnectionState.disconnected;\n")

	b.WriteString("\n  /// The state of the features together: connected when all are, connecting or\n")
	b.WriteString("  /// reconnecting or failed when any is, closed when all are.\n")
	b.WriteString("  LiveConnectionState get state => _state;\n")
	b.WriteString("\n  /// Changes of [state].\n")
	b.WriteString("  Stream<LiveConnectionState> get states => _states.stream;\n")
	b.WriteString("\n  /// Each feature's state changes, named by feature.\n")
	b.WriteString("  Stream<({String client, LiveConnectionState state})> get clientStateChanges => _changes.stream;\n")
	b.WriteString("\n  /// Errors from every feature.\n")
	b.WriteString("  Stream<String> get errors => _errors.stream;\n")

	var fields, values, all []string

	for _, f := range enabled {
		fields = append(fields, "LiveConnectionState "+f.file)
		values = append(values, f.file+": "+f.file+".state")
		all = append(all, f.file+".state")
	}

	b.WriteString("\n  /// The state of each feature.\n")
	fmt.Fprintf(&b, "  ({%s}) get clientStates => (%s);\n", strings.Join(fields, ", "), strings.Join(values, ", "))

	var params, calls []string

	for _, f := range enabled {
		params = append(params, "bool "+f.file+" = true")
		calls = append(calls, fmt.Sprintf("if (%s) this.%s.connect()", f.file, f.file))
	}

	b.WriteString("\n  /// Connects the features chosen, all by default.\n")
	b.WriteString("  ///\n")
	b.WriteString("  /// A feature that fails to connect fails the call; the others stay\n")
	b.WriteString("  /// connected, so [close] after a failure.\n")
	fmt.Fprintf(&b, "  Future<void> connect({%s}) async {\n", strings.Join(params, ", "))
	b.WriteString("    _set(LiveConnectionState.connecting);\n")
	b.WriteString("    try {\n")
	fmt.Fprintf(&b, "      await Future.wait([%s]);\n", strings.Join(calls, ", "))
	b.WriteString("    } on Object {\n")
	b.WriteString("      _set(LiveConnectionState.error);\n")
	b.WriteString("      rethrow;\n")
	b.WriteString("    }\n")
	b.WriteString("    _update();\n")
	b.WriteString("  }\n")

	disconnects := make([]string, len(enabled))
	closes := make([]string, len(enabled))

	for i, f := range enabled {
		disconnects[i] = f.file + ".disconnect(rejectQueued: rejectQueued)"
		closes[i] = f.file + ".close()"
	}

	b.WriteString("\n  /// Disconnects every feature. What is waiting to be sent fails unless\n")
	b.WriteString("  /// [rejectQueued] is false.\n")
	b.WriteString("  Future<void> disconnect({bool rejectQueued = true}) async {\n")
	fmt.Fprintf(&b, "    await Future.wait([%s]);\n", strings.Join(disconnects, ", "))
	b.WriteString("    _set(LiveConnectionState.closed);\n")
	b.WriteString("  }\n")

	b.WriteString("\n  /// Closes every feature for good.\n")
	b.WriteString("  Future<void> close() async {\n")
	b.WriteString("    for (final watch in _watching) {\n")
	b.WriteString("      await watch.cancel();\n")
	b.WriteString("    }\n")
	b.WriteString("    _watching.clear();\n")
	fmt.Fprintf(&b, "    await Future.wait([%s]);\n", strings.Join(closes, ", "))
	b.WriteString("    _set(LiveConnectionState.closed);\n")
	b.WriteString("    await _states.close();\n")
	b.WriteString("    await _changes.close();\n")
	b.WriteString("    await _errors.close();\n")
	b.WriteString("  }\n")

	b.WriteString("\n  void _watch(String client, Stream<LiveConnectionState> states, Stream<String> errors) {\n")
	b.WriteString("    _watching\n")
	b.WriteString("      ..add(states.listen((state) {\n")
	b.WriteString("        if (!_changes.isClosed) _changes.add((client: client, state: state));\n")
	b.WriteString("        _update();\n")
	b.WriteString("      }))\n")
	b.WriteString("      ..add(errors.listen((error) {\n")
	b.WriteString("        if (!_errors.isClosed) _errors.add(error);\n")
	b.WriteString("      }));\n")
	b.WriteString("  }\n")

	b.WriteString("\n  void _update() {\n")
	fmt.Fprintf(&b, "    final all = [%s];\n", strings.Join(all, ", "))
	b.WriteString("    _set(\n")
	b.WriteString("      all.every((s) => s == LiveConnectionState.connected)\n")
	b.WriteString("          ? LiveConnectionState.connected\n")
	b.WriteString("          : all.contains(LiveConnectionState.connecting)\n")
	b.WriteString("          ? LiveConnectionState.connecting\n")
	b.WriteString("          : all.contains(LiveConnectionState.reconnecting)\n")
	b.WriteString("          ? LiveConnectionState.reconnecting\n")
	b.WriteString("          : all.contains(LiveConnectionState.error)\n")
	b.WriteString("          ? LiveConnectionState.error\n")
	b.WriteString("          : all.every((s) => s == LiveConnectionState.closed)\n")
	b.WriteString("          ? LiveConnectionState.closed\n")
	b.WriteString("          : LiveConnectionState.disconnected,\n")
	b.WriteString("    );\n")
	b.WriteString("  }\n")

	b.WriteString("\n  void _set(LiveConnectionState next) {\n")
	b.WriteString("    if (_state == next) return;\n")
	b.WriteString("    _state = next;\n")
	b.WriteString("    if (!_states.isClosed) _states.add(next);\n")
	b.WriteString("  }\n}\n")

	return b.String()
}

// roomsTemplate is the generated rooms client, with its configurable values as
// placeholders.
const roomsTemplate = `// Generated by forge. Do not edit.

import 'dart:async';

import 'package:forge_client/forge_client.dart' show StreamConnect, webSocketConnection;

import 'live_socket.dart';

String? _string(Object? value) => value is String ? value : null;

Map<String, Object?>? _object(Object? value) =>
    value is Map<Object?, Object?> ? value.cast<String, Object?>() : null;

/// A message posted to a room.
final class RoomMessage {
  /// Creates a message.
  const RoomMessage({
    required this.roomId,
    this.id,
    this.userId,
    this.type,
    this.data,
    this.timestamp,
    this.metadata,
  });

  /// Reads a frame.
  factory RoomMessage.fromFrame(Map<String, Object?> frame) => RoomMessage(
    roomId: _string(frame['room_id']) ?? '',
    id: _string(frame['id']),
    userId: _string(frame['user_id']),
    type: _string(frame['type']),
    data: frame['data'],
    timestamp: _string(frame['timestamp']),
    metadata: _object(frame['metadata']),
  );

  /// The room.
  final String roomId;

  /// The server's id for the message, when it says.
  final String? id;

  /// The sender, when the server says.
  final String? userId;

  /// The message type.
  final String? type;

  /// The payload.
  final Object? data;

  /// When it was sent, as ISO-8601.
  final String? timestamp;

  /// Metadata the server attached.
  final Map<String, Object?>? metadata;
}

/// Something that happened in a joined room.
sealed class RoomEvent {
  /// Const base constructor.
  const RoomEvent(this.roomId);

  /// The room.
  final String roomId;
}

/// A message arrived.
final class RoomMessageReceived extends RoomEvent {
  /// Creates the event.
  RoomMessageReceived(this.message) : super(message.roomId);

  /// The message.
  final RoomMessage message;
}

/// A member joined.
final class RoomMemberJoined extends RoomEvent {
  /// Creates the event.
  const RoomMemberJoined(super.roomId, this.member);

  /// The member as the server described them.
  final Map<String, Object?> member;
}

/// A member left.
final class RoomMemberLeft extends RoomEvent {
  /// Creates the event.
  const RoomMemberLeft(super.roomId, this.member);

  /// The member as the server described them.
  final Map<String, Object?> member;
}

/// The server reported an error, or a frame could not be read.
final class RoomFailure extends RoomEvent {
  /// Creates the event.
  const RoomFailure(super.roomId, this.message);

  /// The server's message.
  final String message;
}

final class _Room {
  _Room({this.metadata, this.role});

  final Map<String, Object?>? metadata;
  final String? role;
  String? name;
  var members = <Map<String, Object?>>[];
  var connected = false;
}

/// Joins rooms, sends to them and listens to them over the {{DOC_PATH}} socket.
///
/// After a reconnect the client joins each room again with the metadata and role
/// it joined with, and only then sends what was waiting.
final class RoomClient {
  /// Creates a client that connects relative to [baseUrl].
  RoomClient({
    required this.baseUrl,
    StreamConnect? connect,
    this.headers = const {},
    this.options = const LiveOptions(),
    this.requestTimeout = const Duration(seconds: 10),
    this.maxRooms = {{MAX_ROOMS}},
  }) : _connect = connect ?? webSocketConnection();

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

  /// How the connection opens, reopens and queues what it cannot send.
  final LiveOptions options;

  /// How long a join or history request may wait for its reply.
  final Duration requestTimeout;

  /// The most rooms one connection may join.
  final int maxRooms;

  final StreamConnect _connect;
  final _events = StreamController<RoomEvent>.broadcast();
  final _errors = StreamController<String>.broadcast();
  final _pending = <String, Completer<Map<String, Object?>>>{};
  final _rooms = <String, _Room>{};
  final _listening = <StreamSubscription<Object?>>[];
  late final LiveSocket _socket = _makeSocket();
  var _closed = false;
  var _nextRequest = 0;

  LiveSocket _makeSocket() {
    final socket = LiveSocket(
      open: liveOpen(
        connect: _connect,
        baseUrl: baseUrl,
        path: '{{PATH}}',
        endpoint: '{{PATH}}',
        headers: headers,
        options: options,
      ),
      options: options,
      onOpen: _onOpen,
      onLost: _onLost,
    );
    _listening
      ..add(socket.frames.listen(_onFrame, onError: _onError))
      ..add(socket.errors.listen(_onError));
    return socket;
  }

  /// The connection state.
  LiveConnectionState get state => _socket.state;

  /// Changes of [state].
  Stream<LiveConnectionState> get states => _socket.states;

  /// Events from every joined room.
  Stream<RoomEvent> get events => _events.stream;

  /// Errors the server reports, and failures with no caller to take them.
  Stream<String> get errors => _errors.stream;

  /// The messages in [roomId].
  Stream<RoomMessage> messagesIn(String roomId) =>
      events.expand((e) => e is RoomMessageReceived && e.roomId == roomId ? [e.message] : const <RoomMessage>[]);

  /// The members who join [roomId].
  Stream<Map<String, Object?>> memberJoins(String roomId) =>
      events.expand((e) => e is RoomMemberJoined && e.roomId == roomId ? [e.member] : const <Map<String, Object?>>[]);

  /// The members who leave [roomId].
  Stream<Map<String, Object?>> memberLeaves(String roomId) =>
      events.expand((e) => e is RoomMemberLeft && e.roomId == roomId ? [e.member] : const <Map<String, Object?>>[]);

  /// The rooms joined, including any waiting to be joined again.
  Set<String> get joined => Set.unmodifiable(_rooms.keys);

  /// Whether the server currently has this client in [roomId].
  bool roomConnected(String roomId) => _rooms[roomId]?.connected ?? false;

  /// The name the server gave [roomId], if it said.
  String? roomName(String roomId) => _rooms[roomId]?.name;

  /// The members the server last reported in [roomId].
  List<Map<String, Object?>> membersOf(String roomId) =>
      List.unmodifiable(_rooms[roomId]?.members ?? const <Map<String, Object?>>[]);

  /// How many sends are waiting for a connection.
  int get queueSize => _socket.queueSize;

  /// Drops what is waiting to be sent, failing it when [rejectPending] is true.
  void clearQueue({bool rejectPending = true}) => _socket.clearQueue(rejectPending: rejectPending);

  /// Opens the connection. Calling it while open, or while opening, waits for
  /// the same connection.
  Future<void> connect() => _socket.connect();

  /// Closes the connection, forgets the rooms and stops reconnecting. What is
  /// waiting to be sent fails unless [rejectQueued] is false. [connect] opens
  /// it again.
  Future<void> disconnect({bool rejectQueued = true}) async {
    await _socket.disconnect(rejectQueued: rejectQueued);
    _rooms.clear();
  }

  /// Joins [roomId], carrying [metadata] and [role] to the server.
  Future<void> join(String roomId, {Map<String, Object?>? metadata, String? role}) async {
    if (_rooms.length >= maxRooms) {
      throw StateError('Maximum rooms per user limit reached');
    }
    final room = _Room(metadata: metadata, role: role);
    await _join(roomId, room);
    _rooms[roomId] = room;
  }

  Future<void> _join(String roomId, _Room room) async {
    final reply = await _request('join', {
      'room_id': roomId,
      'metadata': ?room.metadata,
      'role': ?room.role,
    });
    if (reply['error'] case final String error) throw StateError(error);
    room.name = _string(reply['room_name']) ?? room.name;
    final members = reply['members'];
    room.members = [
      if (members is List<Object?>)
        for (final m in members) ?_object(m),
    ];
    room.connected = true;
  }

  /// Leaves [roomId].
  void leave(String roomId) {
    _send({'type': 'leave', 'room_id': roomId});
    _rooms.remove(roomId);
  }

  /// Sends [data] to [roomId], which must be joined.
  ///
  /// While not connected the message waits in the queue, and the future
  /// completes when it is sent.
  Future<void> send(String roomId, Object? data) {
    if (!_rooms.containsKey(roomId)) return Future.error(StateError('Not joined to room: $roomId'));
    return _socket.deliver(
      () => {
        'type': 'message',
        'room_id': roomId,
        'data': data,
        'timestamp': DateTime.now().toUtc().toIso8601String(),
      },
      check: () => _rooms.containsKey(roomId) ? null : 'No longer joined to room',
    );
  }

  /// Sends [data] to every member of [roomId]. The same as [send].
  Future<void> broadcast(String roomId, Object? data) => send(roomId, data);

{{HISTORY}}  /// Closes the client for good: disconnects and closes every stream.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await disconnect();
    for (final listening in _listening) {
      await listening.cancel();
    }
    await _socket.dispose();
    await _events.close();
    await _errors.close();
  }

  void _send(Object frame) {
    try {
      _socket.connection?.send(frame);
    } on Object catch (error) {
      _onError(error);
    }
  }

  Future<void> _onOpen(bool reconnected) async {
    if (!reconnected) return;
    for (final entry in [..._rooms.entries]) {
      try {
        await _join(entry.key, entry.value);
      } on Object catch (error) {
        entry.value.connected = false;
        _onError('could not join ${entry.key} again: $error');
      }
    }
  }

  void _onLost() {
    _failPending(StateError('Connection closed'));
    for (final room in _rooms.values) {
      room.connected = false;
    }
  }

  void _failPending(Object error) {
    final waiting = [..._pending.values];
    _pending.clear();
    for (final pending in waiting) {
      pending.completeError(error);
    }
  }

  Future<Map<String, Object?>> _request(String type, Map<String, Object?> payload) {
    final connection = _socket.connection ?? (throw StateError('WebSocket is not connected, call connect() first'));
    final id = 'req_${++_nextRequest}_${DateTime.now().millisecondsSinceEpoch}';
    final completer = Completer<Map<String, Object?>>();
    _pending[id] = completer;
    try {
      connection.send({'type': type, 'request_id': id, ...payload});
    } on Object {
      _pending.remove(id);
      rethrow;
    }
    return completer.future.timeout(requestTimeout, onTimeout: () {
      _pending.remove(id);
      throw TimeoutException('$type timed out', requestTimeout);
    });
  }

  void _onError(Object error) {
    if (_events.isClosed) return;
    _events.add(RoomFailure('', '$error'));
    _errors.add('$error');
  }

  void _onFrame(Object? raw) {
    if (raw is! Map<Object?, Object?>) return;
    final frame = raw.cast<String, Object?>();
    if (frame['request_id'] case final String id) {
      final pending = _pending.remove(id);
      if (pending != null) {
        pending.complete(frame);
        return;
      }
    }
    final roomId = _string(frame['room_id']) ?? '';
    final member = _object(frame['member']);
    if (_string(frame['room_name']) case final name?) _rooms[roomId]?.name = name;
    switch (frame['type']) {
      case 'message':
        _events.add(RoomMessageReceived(RoomMessage.fromFrame(frame)));
      case 'member_join':
        if (member != null) _rooms[roomId]?.members.add(member);
        _events.add(RoomMemberJoined(roomId, member ?? const <String, Object?>{}));
      case 'member_leave':
        if (member != null) {
          _rooms[roomId]?.members.removeWhere((m) => m['user_id'] == member['user_id']);
        }
        _events.add(RoomMemberLeft(roomId, member ?? const <String, Object?>{}));
      case 'error':
        final message = '${frame['message'] ?? frame['error']}';
        _events.add(RoomFailure(roomId, message));
        _errors.add(message);
    }
  }
}
`

// roomsHistory is the generated history client, which the rooms client has
// only when history is enabled.
const roomsHistory = `  /// Fetches recent messages in [roomId].
  Future<List<RoomMessage>> history(
    String roomId, {
    int? limit,
    String? before,
    String? after,
    String? beforeId,
    String? afterId,
  }) async {
    final reply = await _request('history', {
      'room_id': roomId,
      'limit': ?limit,
      'before': ?before,
      'after': ?after,
      'before_id': ?beforeId,
      'after_id': ?afterId,
    });
    if (reply['error'] case final String error) throw StateError(error);
    final messages = reply['messages'];
    if (messages is! List<Object?>) return const [];
    return [
      for (final m in messages)
        if (_object(m) case final frame?) RoomMessage.fromFrame(frame),
    ];
  }

`

// presenceTemplate is the generated presence client, with its configurable values as
// placeholders.
const presenceTemplate = `// Generated by forge. Do not edit.

import 'dart:async';

import 'package:forge_client/forge_client.dart' show StreamConnect, webSocketConnection;

import 'live_socket.dart';

String? _string(Object? value) => value is String ? value : null;

/// A user's presence as the server last reported it.
final class UserPresence {
  /// Creates a presence record.
  const UserPresence({required this.userId, required this.status, this.customMessage, this.lastSeen, this.roomId});

  /// Reads a frame.
  factory UserPresence.fromFrame(Map<String, Object?> frame) => UserPresence(
    userId: _string(frame['user_id']) ?? '',
    status: _string(frame['status']) ?? 'offline',
    customMessage: _string(frame['custom_status']),
    lastSeen: _string(frame['timestamp']),
    roomId: _string(frame['room_id']),
  );

  /// The user.
  final String userId;

  /// One of [PresenceClient.statuses], or a status this client does not know.
  final String status;

  /// A free-text status.
  final String? customMessage;

  /// When the server last saw the user, as ISO-8601.
  final String? lastSeen;

  /// The room the user is in, when the server says.
  final String? roomId;
}

/// Publishes this user's presence and follows other users' over the {{DOC_PATH}} socket.
///
/// After a reconnect the client sets the status again, if one was ever set, and
/// follows the same users again.
final class PresenceClient {
  /// Creates a client that connects relative to [baseUrl].
  PresenceClient({
    required this.baseUrl,
    StreamConnect? connect,
    this.headers = const {},
    this.options = const LiveOptions(),
    this.heartbeat = const Duration(milliseconds: {{HEARTBEAT_MS}}),
  }) : _connect = connect ?? webSocketConnection();

  /// The statuses the server declares.
  static const List<String> statuses = {{STATUSES}};

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

  /// How the connection opens, reopens and queues what it cannot send.
  final LiveOptions options;

  /// How often to tell the server this user is still here.
  final Duration heartbeat;

  final StreamConnect _connect;
  final _updates = StreamController<UserPresence>.broadcast();
  final _errors = StreamController<String>.broadcast();
  final _known = <String, UserPresence>{};
  final _followed = <String>{};
  final _listening = <StreamSubscription<Object?>>[];
  late final LiveSocket _socket = _makeSocket();
  var _closed = false;
  String? _status;
  var _customMessage = '';

  LiveSocket _makeSocket() {
    final socket = LiveSocket(
      open: liveOpen(
        connect: _connect,
        baseUrl: baseUrl,
        path: '{{PATH}}',
        endpoint: '{{PATH}}',
        headers: headers,
        options: options,
      ),
      options: options,
      heartbeat: heartbeat,
      heartbeatFrame: () => {'type': 'heartbeat', 'timestamp': DateTime.now().toUtc().toIso8601String()},
      onOpen: _onOpen,
    );
    _listening
      ..add(socket.frames.listen(_onFrame, onError: _onError))
      ..add(socket.errors.listen(_onError));
    return socket;
  }

  /// The connection state.
  LiveConnectionState get state => _socket.state;

  /// Changes of [state].
  Stream<LiveConnectionState> get states => _socket.states;

  /// Every presence change the server reports.
  Stream<UserPresence> get updates => _updates.stream;

  /// Errors the server reports, and failures with no caller to take them.
  Stream<String> get errors => _errors.stream;

  /// The last known presence of [userId].
  UserPresence? presenceOf(String userId) => _known[userId];

  /// The users last known not to be offline, in [roomId] when it is given.
  List<UserPresence> onlineUsers({String? roomId}) => [
    for (final p in _known.values)
      if (p.status != 'offline' && (roomId == null || p.roomId == roomId)) p,
  ];

  /// The status this client last set, or offline if it has set none.
  ({String status, String customMessage}) get current =>
      (status: _status ?? 'offline', customMessage: _customMessage);

  /// Opens the connection and starts the heartbeat. Calling it while open, or
  /// while opening, waits for the same connection.
  Future<void> connect() => _socket.connect();

  /// Sets this user's status.
  void setStatus(String status, {String? customMessage}) {
    final connection = _socket.connection ?? (throw StateError('WebSocket is not connected, call connect() first'));
    _status = status;
    _customMessage = customMessage ?? '';
    connection.send(_statusFrame());
  }

  Map<String, Object?> _statusFrame() => {
    'type': 'presence',
    'status': _status,
    'custom_status': ?(_customMessage.isEmpty ? null : _customMessage),
    'timestamp': DateTime.now().toUtc().toIso8601String(),
  };

  /// Follows [userIds].
  void subscribe(List<String> userIds) {
    final connection = _socket.connection ?? (throw StateError('WebSocket is not connected, call connect() first'));
    _followed.addAll(userIds);
    connection.send({'type': 'subscribe_presence', 'user_ids': userIds});
  }

  /// Stops following [userIds].
  void unsubscribe(List<String> userIds) {
    final connection = _socket.connection ?? (throw StateError('WebSocket is not connected, call connect() first'));
    userIds.forEach(_followed.remove);
    userIds.forEach(_known.remove);
    connection.send({'type': 'unsubscribe_presence', 'user_ids': userIds});
  }

  /// Stops the heartbeat, closes the connection, stops reconnecting and
  /// forgets who was followed. [connect] opens it again.
  Future<void> disconnect({bool rejectQueued = true}) async {
    await _socket.disconnect(rejectQueued: rejectQueued);
    _followed.clear();
    _known.clear();
  }

  /// Closes the client for good: disconnects and closes every stream.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await disconnect();
    for (final listening in _listening) {
      await listening.cancel();
    }
    await _socket.dispose();
    await _updates.close();
    await _errors.close();
  }

  Future<void> _onOpen(bool reconnected) async {
    if (!reconnected) return;
    try {
      if (_status != null) _socket.connection?.send(_statusFrame());
      if (_followed.isNotEmpty) {
        _socket.connection?.send({'type': 'subscribe_presence', 'user_ids': [..._followed]});
      }
    } on Object catch (error) {
      _onError(error);
    }
  }

  void _onError(Object error) {
    if (!_errors.isClosed) _errors.add('$error');
  }

  void _onFrame(Object? raw) {
    if (raw is! Map<Object?, Object?>) return;
    final frame = raw.cast<String, Object?>();
    switch (frame['type']) {
      case 'presence':
        _record(UserPresence.fromFrame(frame));
      case 'presence_sync':
        if (frame['presences'] case final List<Object?> all) {
          for (final p in all) {
            if (p is Map<Object?, Object?>) _record(UserPresence.fromFrame(p.cast<String, Object?>()));
          }
        }
      case 'error':
        _onError('${frame['message'] ?? frame['error']}');
    }
  }

  void _record(UserPresence presence) {
    _known[presence.userId] = presence;
    _updates.add(presence);
  }
}
`

// typingTemplate is the generated typing client, with its configurable values as
// placeholders.
const typingTemplate = `// Generated by forge. Do not edit.

import 'dart:async';

import 'package:forge_client/forge_client.dart' show StreamConnect, webSocketConnection;

import 'live_socket.dart';

String? _string(Object? value) => value is String ? value : null;

/// A user started or stopped typing in a room.
final class TypingEvent {
  /// Creates the event.
  const TypingEvent({required this.roomId, required this.userId, required this.isTyping, this.timestamp});

  /// The room.
  final String roomId;

  /// The user.
  final String userId;

  /// True when the user started typing.
  final bool isTyping;

  /// When the server saw the change, as ISO-8601, when it says.
  final String? timestamp;
}

/// Sends and receives typing indicators over the {{DOC_PATH}} socket.
final class TypingClient {
  /// Creates a client that connects relative to [baseUrl].
  TypingClient({
    required this.baseUrl,
    StreamConnect? connect,
    this.headers = const {},
    this.options = const LiveOptions(),
    this.timeout = const Duration(milliseconds: {{TIMEOUT_MS}}),
    this.debounce = const Duration(milliseconds: {{DEBOUNCE_MS}}),
  }) : _connect = connect ?? webSocketConnection();

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

  /// How the connection opens, reopens and queues what it cannot send.
  final LiveOptions options;

  /// Typing stops by itself after this long without [start].
  final Duration timeout;

  /// Repeated [start] calls within this window send one frame.
  final Duration debounce;

  final StreamConnect _connect;
  final _events = StreamController<TypingEvent>.broadcast();
  final _errors = StreamController<String>.broadcast();
  final _debounce = <String, Timer>{};
  final _autoStop = <String, Timer>{};
  final _typing = <String, Set<String>>{};
  final _listening = <StreamSubscription<Object?>>[];
  late final LiveSocket _socket = _makeSocket();
  var _closed = false;

  LiveSocket _makeSocket() {
    final socket = LiveSocket(
      open: liveOpen(
        connect: _connect,
        baseUrl: baseUrl,
        path: '{{PATH}}',
        endpoint: '{{PATH}}',
        headers: headers,
        options: options,
      ),
      options: options,
      onLost: _onLost,
    );
    _listening
      ..add(socket.frames.listen(_onFrame, onError: _onError))
      ..add(socket.errors.listen(_onError));
    return socket;
  }

  /// The connection state.
  LiveConnectionState get state => _socket.state;

  /// Changes of [state].
  Stream<LiveConnectionState> get states => _socket.states;

  /// Typing changes from other users.
  Stream<TypingEvent> get events => _events.stream;

  /// Errors the server reports, and failures with no caller to take them.
  Stream<String> get errors => _errors.stream;

  /// The users last seen typing in [roomId].
  List<String> typingIn(String roomId) => [...?_typing[roomId]];

  /// Opens the connection. Calling it while open, or while opening, waits for
  /// the same connection.
  Future<void> connect() => _socket.connect();

  /// Marks this user as typing in [roomId]. Does nothing while not connected.
  void start(String roomId) {
    if (_socket.connection == null) return;
    _debounce.remove(roomId)?.cancel();
    _debounce[roomId] = Timer(debounce, () {
      _debounce.remove(roomId);
      _frame(roomId, typing: true);
    });
    _autoStop.remove(roomId)?.cancel();
    _autoStop[roomId] = Timer(timeout, () => stop(roomId));
  }

  /// Marks this user as no longer typing in [roomId]. Does nothing while not
  /// connected, apart from cancelling the timers.
  void stop(String roomId) {
    _debounce.remove(roomId)?.cancel();
    _autoStop.remove(roomId)?.cancel();
    _frame(roomId, typing: false);
  }

  /// Cancels every timer, closes the connection and stops reconnecting.
  /// [connect] opens it again.
  Future<void> disconnect({bool rejectQueued = true}) async {
    _cancelTimers();
    await _socket.disconnect(rejectQueued: rejectQueued);
    _typing.clear();
  }

  /// Closes the client for good: disconnects and closes every stream.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await disconnect();
    for (final listening in _listening) {
      await listening.cancel();
    }
    await _socket.dispose();
    await _events.close();
    await _errors.close();
  }

  void _cancelTimers() {
    for (final timer in [..._debounce.values, ..._autoStop.values]) {
      timer.cancel();
    }
    _debounce.clear();
    _autoStop.clear();
  }

  void _onLost() {
    _cancelTimers();
    _typing.clear();
  }

  void _frame(String roomId, {required bool typing}) {
    final connection = _socket.connection;
    if (connection == null) return;
    try {
      connection.send({
        'type': 'typing',
        'room_id': roomId,
        'data': typing,
        'timestamp': DateTime.now().toUtc().toIso8601String(),
      });
    } on Object catch (error) {
      _onError(error);
    }
  }

  void _onError(Object error) {
    if (!_errors.isClosed) _errors.add('$error');
  }

  void _onFrame(Object? raw) {
    if (raw is! Map<Object?, Object?>) return;
    final frame = raw.cast<String, Object?>();
    switch (frame['type']) {
      case 'typing':
        final roomId = _string(frame['room_id']) ?? '';
        final userId = _string(frame['user_id']) ?? '';
        final isTyping = frame['data'] == true;
        final users = _typing.putIfAbsent(roomId, () => {});
        if (isTyping) {
          users.add(userId);
        } else {
          users.remove(userId);
        }
        _events.add(TypingEvent(
          roomId: roomId,
          userId: userId,
          isTyping: isTyping,
          timestamp: _string(frame['timestamp']),
        ));
      case 'error':
        _onError('${frame['message'] ?? frame['error']}');
    }
  }
}
`

// channelsTemplate is the generated channels client, with its configurable values as
// placeholders.
const channelsTemplate = `// Generated by forge. Do not edit.

import 'dart:async';

import 'package:forge_client/forge_client.dart' show StreamConnect, webSocketConnection;

import 'live_socket.dart';

String? _string(Object? value) => value is String ? value : null;

/// A message published to a channel.
final class ChannelMessage {
  /// Creates a message.
  const ChannelMessage({required this.channelId, this.data, this.userId, this.timestamp, this.messageId});

  /// The channel.
  final String channelId;

  /// The payload.
  final Object? data;

  /// The publisher, when the server says.
  final String? userId;

  /// When it was published, as ISO-8601.
  final String? timestamp;

  /// The server's id for the message.
  final String? messageId;
}

/// The server's answer to a subscribe or an unsubscribe.
final class ChannelAck {
  /// Creates the acknowledgement.
  const ChannelAck({required this.channelId, required this.subscribed});

  /// The channel.
  final String channelId;

  /// True for a subscribe, false for an unsubscribe.
  final bool subscribed;
}

final class _Subscription {
  const _Subscription({this.filter, this.fromMessageId, this.fromTimestamp});

  final Map<String, Object?>? filter;
  final String? fromMessageId;
  final String? fromTimestamp;
}

/// Subscribes and publishes to pub/sub channels over the {{DOC_PATH}} socket.
///
/// After a reconnect the client subscribes to each channel again with the
/// options it subscribed with, and only then sends what was waiting.
final class ChannelClient {
  /// Creates a client that connects relative to [baseUrl].
  ChannelClient({
    required this.baseUrl,
    StreamConnect? connect,
    this.headers = const {},
    this.options = const LiveOptions(),
    this.maxChannels = {{MAX_CHANNELS}},
  }) : _connect = connect ?? webSocketConnection();

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

  /// How the connection opens, reopens and queues what it cannot send.
  final LiveOptions options;

  /// The most channels one connection may subscribe to.
  final int maxChannels;

  final StreamConnect _connect;
  final _messages = StreamController<ChannelMessage>.broadcast();
  final _acks = StreamController<ChannelAck>.broadcast();
  final _errors = StreamController<String>.broadcast();
  final _subscribed = <String, _Subscription>{};
  final _listening = <StreamSubscription<Object?>>[];
  late final LiveSocket _socket = _makeSocket();
  var _closed = false;

  LiveSocket _makeSocket() {
    final socket = LiveSocket(
      open: liveOpen(
        connect: _connect,
        baseUrl: baseUrl,
        path: '{{PATH}}',
        endpoint: '{{PATH}}',
        headers: headers,
        options: options,
      ),
      options: options,
      onOpen: _onOpen,
    );
    _listening
      ..add(socket.frames.listen(_onFrame, onError: _onError))
      ..add(socket.errors.listen(_onError));
    return socket;
  }

  /// The connection state.
  LiveConnectionState get state => _socket.state;

  /// Changes of [state].
  Stream<LiveConnectionState> get states => _socket.states;

  /// Messages on every subscribed channel.
  Stream<ChannelMessage> get messages => _messages.stream;

  /// The server's answers to subscribes and unsubscribes.
  Stream<ChannelAck> get acks => _acks.stream;

  /// Errors the server reports, and failures with no caller to take them.
  Stream<String> get errors => _errors.stream;

  /// The channels currently subscribed.
  Set<String> get subscribed => Set.unmodifiable(_subscribed.keys);

  /// How many sends are waiting for a connection.
  int get queueSize => _socket.queueSize;

  /// Drops what is waiting to be sent, failing it when [rejectPending] is true.
  void clearQueue({bool rejectPending = true}) => _socket.clearQueue(rejectPending: rejectPending);

  /// Opens the connection. Calling it while open, or while opening, waits for
  /// the same connection.
  Future<void> connect() => _socket.connect();

  /// Subscribes to [channelId], optionally filtering what the server sends or
  /// starting from a message id or timestamp.
  void subscribe(
    String channelId, {
    Map<String, Object?>? filter,
    String? fromMessageId,
    String? fromTimestamp,
  }) {
    final connection = _socket.connection ?? (throw StateError('WebSocket is not connected, call connect() first'));
    if (_subscribed.length >= maxChannels) {
      throw StateError('Maximum channels per user limit reached');
    }
    final subscription = _Subscription(filter: filter, fromMessageId: fromMessageId, fromTimestamp: fromTimestamp);
    _subscribed[channelId] = subscription;
    connection.send(_subscribeFrame(channelId, subscription));
  }

  Map<String, Object?> _subscribeFrame(String channelId, _Subscription subscription) => {
    'action': 'subscribe',
    'channel_id': channelId,
    'filter': ?subscription.filter,
    'fromMessageId': ?subscription.fromMessageId,
    'fromTimestamp': ?subscription.fromTimestamp,
  };

  /// Unsubscribes from [channelId].
  void unsubscribe(String channelId) {
    try {
      _socket.connection?.send({'action': 'unsubscribe', 'channel_id': channelId});
    } on Object catch (error) {
      _onError(error);
    }
    _subscribed.remove(channelId);
  }

  /// Publishes [data] to [channelId].
  ///
  /// While not connected the message waits in the queue, and the future
  /// completes when it is sent.
  Future<void> publish(String channelId, Object? data) => _socket.deliver(
    () => {
      'action': 'publish',
      'channel_id': channelId,
      'data': data,
      'timestamp': DateTime.now().toUtc().toIso8601String(),
    },
  );

  /// Closes the connection, forgets the channels and stops reconnecting. What
  /// is waiting to be sent fails unless [rejectQueued] is false. [connect]
  /// opens it again.
  Future<void> disconnect({bool rejectQueued = true}) async {
    await _socket.disconnect(rejectQueued: rejectQueued);
    _subscribed.clear();
  }

  /// Closes the client for good: disconnects and closes every stream.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await disconnect();
    for (final listening in _listening) {
      await listening.cancel();
    }
    await _socket.dispose();
    await _messages.close();
    await _acks.close();
    await _errors.close();
  }

  Future<void> _onOpen(bool reconnected) async {
    if (!reconnected) return;
    for (final entry in [..._subscribed.entries]) {
      try {
        _socket.connection?.send(_subscribeFrame(entry.key, entry.value));
      } on Object catch (error) {
        _onError(error);
      }
    }
  }

  void _onError(Object error) {
    if (!_errors.isClosed) _errors.add('$error');
  }

  void _onFrame(Object? raw) {
    if (raw is! Map<Object?, Object?>) return;
    final frame = raw.cast<String, Object?>();
    switch (frame['type']) {
      case 'error':
        _onError('${frame['message'] ?? frame['error']}');
        return;
      case 'subscribed' || 'unsubscribed':
        _acks.add(ChannelAck(channelId: _string(frame['channel_id']) ?? '', subscribed: frame['type'] == 'subscribed'));
        return;
    }
    if (frame['type'] != 'message' && frame['action'] != 'message') return;
    _messages.add(ChannelMessage(
      channelId: _string(frame['channel_id']) ?? '',
      data: frame['data'],
      userId: _string(frame['user_id']),
      timestamp: _string(frame['timestamp']),
      messageId: _string(frame['message_id']) ?? _string(frame['id']),
    ));
  }
}
`
