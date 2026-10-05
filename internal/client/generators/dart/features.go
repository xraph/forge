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
	file  string
	class string
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
		return strings.NewReplacer(pairs...).Replace(strings.ReplaceAll(template, "{{CONNECT}}", featureConnect))
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
			"{{ON_OPEN}}", "",
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{MAX_ROOMS}}", strconv.Itoa(maxRooms),
		)

		enabled = append(enabled, featureFile{"rooms", "RoomClient"})
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
			"{{ON_OPEN}}", presenceOnOpen,
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{HEARTBEAT_MS}}", strconv.Itoa(heartbeat),
			"{{STATUSES}}", dartStringList(statuses),
		)

		enabled = append(enabled, featureFile{"presence", "PresenceClient"})
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
			"{{ON_OPEN}}", "",
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{TIMEOUT_MS}}", strconv.Itoa(timeout),
			"{{DEBOUNCE_MS}}", strconv.Itoa(debounce),
		)

		enabled = append(enabled, featureFile{"typing", "TypingClient"})
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
			"{{ON_OPEN}}", "",
			"{{PATH}}", literal,
			"{{DOC_PATH}}", doc,
			"{{MAX_CHANNELS}}", strconv.Itoa(maxChannels),
		)

		enabled = append(enabled, featureFile{"channels", "ChannelClient"})
	}

	if config.ShouldGenerateUnifiedStreamingClient() && len(enabled) > 0 {
		files["lib/src/streaming/streaming_client.dart"] = renderStreamingClient(enabled)
	}

	return files
}

// renderStreamingClient composes the enabled feature clients over one base
// URL.
func renderStreamingClient(enabled []featureFile) string {
	var b strings.Builder

	b.WriteString(generatedHeader)
	b.WriteString("\nimport 'package:forge_client/forge_client.dart' show StreamConnect;\n\n")

	for _, f := range enabled {
		fmt.Fprintf(&b, "import '%s.dart';\n", f.file)
	}

	b.WriteString("\n/// Every streaming feature this API offers, over one base URL.\n")
	b.WriteString("final class StreamingClient {\n")
	b.WriteString("  /// Creates the feature clients; none connects until asked.\n")
	b.WriteString("  StreamingClient({required Uri baseUrl, StreamConnect? connect, Map<String, String> headers = const {}})\n")

	for i, f := range enabled {
		lead := "      : "
		if i > 0 {
			lead = "        "
		}

		end := ","
		if i == len(enabled)-1 {
			end = ";"
		}

		fmt.Fprintf(&b, "%s%s = %s(baseUrl: baseUrl, connect: connect, headers: headers)%s\n", lead, f.file, f.class, end)
	}

	names := make([]string, len(enabled))

	for i, f := range enabled {
		names[i] = f.file
		fmt.Fprintf(&b, "\n  /// The %s client.\n", f.file)
		fmt.Fprintf(&b, "  final %s %s;\n", f.class, f.file)
	}

	b.WriteString("\n  /// Connects every feature.\n")
	b.WriteString("  ///\n")
	b.WriteString("  /// A feature that fails to connect fails the call; the others stay\n")
	b.WriteString("  /// connected, so [close] after a failure.\n")
	b.WriteString("  Future<void> connect() async {\n")
	fmt.Fprintf(&b, "    await Future.wait([%s]);\n", joinCalls(names, "connect"))
	b.WriteString("  }\n")
	b.WriteString("\n  /// Closes every feature.\n")
	b.WriteString("  Future<void> close() async {\n")
	fmt.Fprintf(&b, "    await Future.wait([%s]);\n", joinCalls(names, "close"))
	b.WriteString("  }\n}\n")

	return b.String()
}

func joinCalls(names []string, method string) string {
	calls := make([]string, len(names))
	for i, n := range names {
		calls[i] = n + "." + method + "()"
	}

	return strings.Join(calls, ", ")
}

// featureConnect is the connect method every feature client shares. It opens
// one connection however many callers ask at once, forgets the connection when
// the server closes it so connect can open another, and gives a frame that
// cannot be read to the client's _onError instead of leaving it unhandled.
const featureConnect = `  /// Opens the connection. Calling it while open, or while opening, waits for
  /// the same connection; a closed client cannot connect again.
  Future<void> connect() => _opening ??= _open();

  Future<void> _open() async {
    if (_closed) throw StateError('the client is closed');
    try {
      final base = baseUrl.toString().replaceAll(RegExp(r'/+$'), '');
      final url = Uri.parse('$base{{PATH}}');
      final scheme = switch (url.scheme) { 'https' => 'wss', 'http' => 'ws', final s => s };
      final connection = await _connect(
        StreamConnectContext(url: url.replace(scheme: scheme), endpoint: '{{PATH}}', headers: headers),
      );
      if (_closed) {
        await connection.close();
        throw StateError('the client is closed');
      }
      _connection = connection;
      _subscription = connection.messages.listen(_onFrame, onError: _onError);
      unawaited(connection.closed.then((_) => _lost(connection)));
{{ON_OPEN}}    } on Object {
      _opening = null;
      rethrow;
    }
  }
`

// presenceOnOpen starts the presence heartbeat once the socket is open.
const presenceOnOpen = `      _timer = Timer.periodic(heartbeat, (_) {
        connection.send({'type': 'heartbeat', 'timestamp': DateTime.now().toUtc().toIso8601String()});
      });
`

// roomsTemplate is the generated rooms client, with its configurable values as
// placeholders.
const roomsTemplate = `// Generated by forge. Do not edit.

import 'dart:async';

import 'package:forge_client/forge_client.dart'
    show StreamConnect, StreamConnectContext, StreamConnection, webSocketConnection;

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

/// Joins rooms, sends to them and listens to them over the {{DOC_PATH}} socket.
final class RoomClient {
  /// Creates a client that connects relative to [baseUrl].
  RoomClient({
    required this.baseUrl,
    StreamConnect? connect,
    this.headers = const {},
    this.requestTimeout = const Duration(seconds: 10),
    this.maxRooms = {{MAX_ROOMS}},
  }) : _connect = connect ?? webSocketConnection();

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

  /// How long a join or history request may wait for its reply.
  final Duration requestTimeout;

  /// The most rooms one connection may join.
  final int maxRooms;

  final StreamConnect _connect;
  final _events = StreamController<RoomEvent>.broadcast();
  final _pending = <String, Completer<Map<String, Object?>>>{};
  final _joined = <String, List<Map<String, Object?>>>{};
  StreamConnection? _connection;
  StreamSubscription<Object?>? _subscription;
  Future<void>? _opening;
  var _closed = false;
  var _nextRequest = 0;

  /// Events from every joined room.
  Stream<RoomEvent> get events => _events.stream;

  /// The rooms currently joined.
  Set<String> get joined => Set.unmodifiable(_joined.keys);

  /// The members the server last reported in [roomId].
  List<Map<String, Object?>> membersOf(String roomId) =>
      List.unmodifiable(_joined[roomId] ?? const <Map<String, Object?>>[]);

{{CONNECT}}
  /// Joins [roomId].
  Future<void> join(String roomId, {Map<String, Object?>? metadata, String? role}) async {
    if (_joined.length >= maxRooms) {
      throw StateError('maximum of $maxRooms rooms reached');
    }
    final reply = await _request('join', {
      'room_id': roomId,
      'metadata': ?metadata,
      'role': ?role,
    });
    if (reply['error'] case final String error) throw StateError(error);
    final members = reply['members'];
    _joined[roomId] = [
      if (members is List<Object?>)
        for (final m in members) ?_object(m),
    ];
  }

  /// Leaves [roomId].
  void leave(String roomId) {
    _connection?.send({'type': 'leave', 'room_id': roomId});
    _joined.remove(roomId);
  }

  /// Sends [data] to [roomId], which must be joined.
  void send(String roomId, Object? data) {
    if (!_joined.containsKey(roomId)) throw StateError('not joined to $roomId');
    _require().send({
      'type': 'message',
      'room_id': roomId,
      'data': data,
      'timestamp': DateTime.now().toUtc().toIso8601String(),
    });
  }

{{HISTORY}}  /// Closes the connection and fails every pending request. A closed client
  /// cannot connect again.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    final connection = _connection;
    final subscription = _subscription;
    _connection = null;
    _subscription = null;
    _opening = null;
    _failPending(StateError('connection closed'));
    _joined.clear();
    await subscription?.cancel();
    await connection?.close();
    await _events.close();
  }

  StreamConnection _require() =>
      _connection ?? (throw StateError('call connect() first'));

  void _lost(StreamConnection lost) {
    if (!identical(_connection, lost)) return;
    _connection = null;
    _opening = null;
    unawaited(_subscription?.cancel());
    _subscription = null;
    _failPending(StateError('connection closed'));
    _joined.clear();
  }

  void _failPending(Object error) {
    for (final pending in _pending.values) {
      pending.completeError(error);
    }
    _pending.clear();
  }

  Future<Map<String, Object?>> _request(String type, Map<String, Object?> payload) {
    final connection = _require();
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
    if (!_events.isClosed) _events.add(RoomFailure('', '$error'));
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
    switch (frame['type']) {
      case 'message':
        _events.add(RoomMessageReceived(RoomMessage.fromFrame(frame)));
      case 'member_join':
        if (member != null) _joined[roomId]?.add(member);
        _events.add(RoomMemberJoined(roomId, member ?? const <String, Object?>{}));
      case 'member_leave':
        if (member != null) {
          _joined[roomId]?.removeWhere((m) => m['user_id'] == member['user_id']);
        }
        _events.add(RoomMemberLeft(roomId, member ?? const <String, Object?>{}));
      case 'error':
        _events.add(RoomFailure(roomId, '${frame['message'] ?? frame['error']}'));
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

import 'package:forge_client/forge_client.dart'
    show StreamConnect, StreamConnectContext, StreamConnection, webSocketConnection;

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
final class PresenceClient {
  /// Creates a client that connects relative to [baseUrl].
  PresenceClient({
    required this.baseUrl,
    StreamConnect? connect,
    this.headers = const {},
    this.heartbeat = const Duration(milliseconds: {{HEARTBEAT_MS}}),
  }) : _connect = connect ?? webSocketConnection();

  /// The statuses the server declares.
  static const List<String> statuses = {{STATUSES}};

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

  /// How often to tell the server this user is still here.
  final Duration heartbeat;

  final StreamConnect _connect;
  final _updates = StreamController<UserPresence>.broadcast();
  final _errors = StreamController<String>.broadcast();
  final _known = <String, UserPresence>{};
  StreamConnection? _connection;
  StreamSubscription<Object?>? _subscription;
  Future<void>? _opening;
  Timer? _timer;
  var _closed = false;
  var _status = 'offline';
  var _customMessage = '';

  /// Every presence change the server reports.
  Stream<UserPresence> get updates => _updates.stream;

  /// Errors the server reports and frames that could not be read.
  Stream<String> get errors => _errors.stream;

  /// The last known presence of [userId].
  UserPresence? presenceOf(String userId) => _known[userId];

  /// The users last known not to be offline, in [roomId] when it is given.
  List<UserPresence> onlineUsers({String? roomId}) => [
    for (final p in _known.values)
      if (p.status != 'offline' && (roomId == null || p.roomId == roomId)) p,
  ];

  /// The status this client last set.
  ({String status, String customMessage}) get current => (status: _status, customMessage: _customMessage);

{{CONNECT}}
  /// Sets this user's status.
  void setStatus(String status, {String? customMessage}) {
    final connection = _require();
    _status = status;
    _customMessage = customMessage ?? '';
    connection.send({
      'type': 'presence',
      'status': status,
      'custom_status': ?customMessage,
      'timestamp': DateTime.now().toUtc().toIso8601String(),
    });
  }

  /// Follows [userIds].
  void subscribe(List<String> userIds) =>
      _require().send({'type': 'subscribe_presence', 'user_ids': userIds});

  /// Stops following [userIds].
  void unsubscribe(List<String> userIds) {
    final connection = _require();
    userIds.forEach(_known.remove);
    connection.send({'type': 'unsubscribe_presence', 'user_ids': userIds});
  }

  /// Stops the heartbeat and closes the connection. A closed client cannot
  /// connect again.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    _timer?.cancel();
    _timer = null;
    final connection = _connection;
    final subscription = _subscription;
    _connection = null;
    _subscription = null;
    _opening = null;
    _known.clear();
    await subscription?.cancel();
    await connection?.close();
    await _updates.close();
    await _errors.close();
  }

  StreamConnection _require() =>
      _connection ?? (throw StateError('call connect() first'));

  void _lost(StreamConnection lost) {
    if (!identical(_connection, lost)) return;
    _timer?.cancel();
    _timer = null;
    _connection = null;
    _opening = null;
    unawaited(_subscription?.cancel());
    _subscription = null;
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
        _errors.add('${frame['message'] ?? frame['error']}');
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

import 'package:forge_client/forge_client.dart'
    show StreamConnect, StreamConnectContext, StreamConnection, webSocketConnection;

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
    this.timeout = const Duration(milliseconds: {{TIMEOUT_MS}}),
    this.debounce = const Duration(milliseconds: {{DEBOUNCE_MS}}),
  }) : _connect = connect ?? webSocketConnection();

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

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
  StreamConnection? _connection;
  StreamSubscription<Object?>? _subscription;
  Future<void>? _opening;
  var _closed = false;

  /// Typing changes from other users.
  Stream<TypingEvent> get events => _events.stream;

  /// Errors the server reports and frames that could not be read.
  Stream<String> get errors => _errors.stream;

  /// The users last seen typing in [roomId].
  List<String> typingIn(String roomId) => [...?_typing[roomId]];

{{CONNECT}}
  /// Marks this user as typing in [roomId]. Does nothing while not connected.
  void start(String roomId) {
    if (_connection == null) return;
    _debounce.remove(roomId)?.cancel();
    _debounce[roomId] = Timer(debounce, () {
      _debounce.remove(roomId);
      _frame(roomId, typing: true);
    });
    _autoStop.remove(roomId)?.cancel();
    _autoStop[roomId] = Timer(timeout, () => stop(roomId));
  }

  /// Marks this user as no longer typing in [roomId].
  void stop(String roomId) {
    _debounce.remove(roomId)?.cancel();
    _autoStop.remove(roomId)?.cancel();
    _frame(roomId, typing: false);
  }

  /// Cancels every timer and closes the connection. A closed client cannot
  /// connect again.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    _cancelTimers();
    final connection = _connection;
    final subscription = _subscription;
    _connection = null;
    _subscription = null;
    _opening = null;
    _typing.clear();
    await subscription?.cancel();
    await connection?.close();
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

  void _lost(StreamConnection lost) {
    if (!identical(_connection, lost)) return;
    _cancelTimers();
    _connection = null;
    _opening = null;
    unawaited(_subscription?.cancel());
    _subscription = null;
    _typing.clear();
  }

  void _frame(String roomId, {required bool typing}) => _connection?.send({
    'type': 'typing',
    'room_id': roomId,
    'data': typing,
    'timestamp': DateTime.now().toUtc().toIso8601String(),
  });

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
        _errors.add('${frame['message'] ?? frame['error']}');
    }
  }
}
`

// channelsTemplate is the generated channels client, with its configurable values as
// placeholders.
const channelsTemplate = `// Generated by forge. Do not edit.

import 'dart:async';

import 'package:forge_client/forge_client.dart'
    show StreamConnect, StreamConnectContext, StreamConnection, webSocketConnection;

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

/// Subscribes and publishes to pub/sub channels over the {{DOC_PATH}} socket.
final class ChannelClient {
  /// Creates a client that connects relative to [baseUrl].
  ChannelClient({
    required this.baseUrl,
    StreamConnect? connect,
    this.headers = const {},
    this.maxChannels = {{MAX_CHANNELS}},
  }) : _connect = connect ?? webSocketConnection();

  /// The server root.
  final Uri baseUrl;

  /// Headers sent with the upgrade request, where the platform can send them.
  final Map<String, String> headers;

  /// The most channels one connection may subscribe to.
  final int maxChannels;

  final StreamConnect _connect;
  final _messages = StreamController<ChannelMessage>.broadcast();
  final _errors = StreamController<String>.broadcast();
  final _subscribed = <String>{};
  StreamConnection? _connection;
  StreamSubscription<Object?>? _subscription;
  Future<void>? _opening;
  var _closed = false;

  /// Messages on every subscribed channel.
  Stream<ChannelMessage> get messages => _messages.stream;

  /// Errors the server reports and frames that could not be read.
  Stream<String> get errors => _errors.stream;

  /// The channels currently subscribed.
  Set<String> get subscribed => Set.unmodifiable(_subscribed);

{{CONNECT}}
  /// Subscribes to [channelId], optionally filtering what the server sends or
  /// starting from a message id or timestamp.
  void subscribe(
    String channelId, {
    Map<String, Object?>? filter,
    String? fromMessageId,
    String? fromTimestamp,
  }) {
    final connection = _require();
    if (_subscribed.length >= maxChannels) {
      throw StateError('maximum of $maxChannels channels reached');
    }
    _subscribed.add(channelId);
    connection.send({
      'action': 'subscribe',
      'channel_id': channelId,
      'filter': ?filter,
      'fromMessageId': ?fromMessageId,
      'fromTimestamp': ?fromTimestamp,
    });
  }

  /// Unsubscribes from [channelId].
  void unsubscribe(String channelId) {
    _connection?.send({'action': 'unsubscribe', 'channel_id': channelId});
    _subscribed.remove(channelId);
  }

  /// Publishes [data] to [channelId].
  void publish(String channelId, Object? data) => _require().send({
    'action': 'publish',
    'channel_id': channelId,
    'data': data,
    'timestamp': DateTime.now().toUtc().toIso8601String(),
  });

  /// Closes the connection. A closed client cannot connect again.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    final connection = _connection;
    final subscription = _subscription;
    _connection = null;
    _subscription = null;
    _opening = null;
    _subscribed.clear();
    await subscription?.cancel();
    await connection?.close();
    await _messages.close();
    await _errors.close();
  }

  StreamConnection _require() =>
      _connection ?? (throw StateError('call connect() first'));

  void _lost(StreamConnection lost) {
    if (!identical(_connection, lost)) return;
    _connection = null;
    _opening = null;
    unawaited(_subscription?.cancel());
    _subscription = null;
    _subscribed.clear();
  }

  void _onError(Object error) {
    if (!_errors.isClosed) _errors.add('$error');
  }

  void _onFrame(Object? raw) {
    if (raw is! Map<Object?, Object?>) return;
    final frame = raw.cast<String, Object?>();
    if (frame['type'] == 'error') {
      _errors.add('${frame['message'] ?? frame['error']}');
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
