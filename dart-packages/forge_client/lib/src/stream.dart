/// Sockets: one per endpoint and principal, multiplexed by channel,
/// ref-counted, closed one turn after the last release, and reopened on a
/// backoff that reports the gap. Port of `packages/client-core/src/stream.ts`.
library;

import 'dart:async';
import 'dart:math' as math;

import 'freshness.dart' show ConnectivitySignal;
import 'invalidate.dart' show Scheduler, microtaskScheduler;
import 'stream_types.dart';
import 'transport.dart' show Sleep, realSleep;

/// One socket, as the subscription manager drives it.
///
/// A connection is used until it closes and then discarded. Reconnection
/// produces a new connection rather than reviving this one, which is what lets
/// the manager ignore a late frame from a socket it has already replaced.
abstract interface class StreamConnection {
  /// Decoded messages. An error event is a transport failure, reported and
  /// never fatal.
  Stream<Object?> get messages;

  /// Completes when the socket goes away, from either side.
  Future<void> get closed;

  /// Send one message up the socket.
  void send(Object? message);

  /// Close for good.
  Future<void> close();
}

/// A connection that cannot send, such as SSE or WebTransport datagrams.
///
/// Its [send] throws. The manager never calls it, answers no keepalive on it,
/// and refuses a subscriber that wants to send a hello or goodbye frame.
abstract interface class ReceiveOnlyConnection implements StreamConnection {}

/// Everything a connect factory is told about the socket it is asked for.
final class StreamConnectContext {
  /// Describes one connect attempt.
  const StreamConnectContext({
    required this.url,
    required this.endpoint,
    this.headers = const {},
    this.principal,
    this.channels = const [],
    this.attempt = 0,
  });

  /// Where to connect: the manager's `baseUrl` plus [endpoint].
  final Uri url;

  /// Headers to send where the transport can.
  final Map<String, String> headers;

  /// Who the socket belongs to.
  final String? principal;

  /// The endpoint this socket serves.
  final String endpoint;

  /// The channels multiplexed over it at the moment it is opened.
  final List<String> channels;

  /// How many times this socket has been opened. 0 is the first open.
  final int attempt;
}

/// Open one socket. Called again, with a higher `attempt`, on reconnect.
typedef StreamConnect = Future<StreamConnection> Function(
  StreamConnectContext context,
);

/// This platform cannot open [transport] for [route].
///
/// Terminal for the socket that met it: the manager reports it once and does
/// not retry. See [fallbackConnection] for the transport that should be used
/// instead.
final class TransportUnavailable implements Exception {
  /// [route] could not be opened over [transport].
  const TransportUnavailable(this.route, this.transport);

  /// The endpoint that was asked for.
  final String route;

  /// The transport this platform lacks, e.g. `webtransport`.
  final String transport;

  @override
  String toString() =>
      'TransportUnavailable: $transport is not available on this platform '
      'for $route';
}

/// Decide whether an inbound message is a keepalive, and what to answer it
/// with. `null` means "not a keepalive", the answer for nearly every frame.
typedef Keepalive = Object? Function(Object? message);

/// The default [Keepalive]: answer the streaming extension's ping.
///
/// The extension closes a connection that has said nothing for its ping
/// interval plus pong timeout, and its ping is a JSON application message, not
/// a WebSocket control frame. A client that only listens must answer it.
Object? forgeKeepalive(Object? message) {
  if (message is! Map<Object?, Object?>) return null;
  if (message['type'] != 'system' || message['event'] != 'ping') return null;

  return const {'type': 'system', 'event': 'pong'};
}

/// The two control events the server sends on a resumed stream.
const List<String> streamControlEvents = ['forge.resumed', 'forge.gap'];

/// The message names the generated `streams` table binds on one channel.
List<String> channelMessages(List<StreamBinding> bindings, String channel) {
  final names = <String>[];

  for (final binding in bindings) {
    if (binding is EntityStreamBinding &&
        binding.channel == channel &&
        !names.contains(binding.message)) {
      names.add(binding.message);
    }
  }

  return names;
}

/// Try each factory in order, moving on only when one is unavailable here.
///
/// The spec's native rule in one place: a channel served over WebTransport
/// and a WebSocket uses the WebSocket where WebTransport does not exist. Any
/// other failure is the real answer and is not masked.
StreamConnect fallbackConnection(List<StreamConnect> candidates) {
  if (candidates.isEmpty) {
    throw ArgumentError.value(candidates, 'candidates', 'must not be empty');
  }

  final ordered = List<StreamConnect>.unmodifiable(candidates);

  return (context) async {
    TransportUnavailable? last;

    for (final connect in ordered) {
      try {
        return await connect(context);
      } on TransportUnavailable catch (error) {
        last = error;
      }
    }

    throw last!;
  };
}

/// How far apart reconnect attempts are, and how many there are.
final class BackoffPolicy {
  /// [jitter] is the share of each delay that is randomised. TS uses 0.5.
  const BackoffPolicy({
    this.initial = const Duration(milliseconds: 500),
    this.max = const Duration(seconds: 30),
    this.factor = 2.0,
    this.jitter = 0.2,
    this.attempts = 10,
  });

  /// The first delay, before jitter.
  final Duration initial;

  /// The longest delay, before jitter.
  final Duration max;

  /// How much each delay grows over the last.
  final double factor;

  /// The randomised share of each delay, from 0 to 1.
  final double jitter;

  /// Attempts after a drop before giving up. `null` never gives up.
  final int? attempts;

  /// The delay before attempt [attempt], for a [random] sample in `[0, 1)`.
  Duration delay(int attempt, double random) {
    final grown = initial.inMicroseconds * math.pow(factor, attempt);
    final capped = math.min(grown.toDouble(), max.inMicroseconds.toDouble());
    final jittered = capped * (1 - jitter) + random * capped * jitter;

    return Duration(microseconds: jittered.round());
  }
}

/// One subscription: its channel, handler and frames, and the socket that
/// currently holds it.
final class _Subscription {
  _Subscription(this.channel, this.handler, this.options, this.socket);

  final String channel;
  final FrameHandler handler;
  final SubscribeOptions options;

  /// Moved by `repartition`, so a release always reaches the live socket.
  _Socket socket;

  bool get speaks => options.hello != null || options.goodbye != null;
}

/// One socket the manager is holding open.
final class _Socket {
  _Socket(this.endpoint, this.principal);

  final String endpoint;
  String? principal;

  /// The live connection, or null between a drop and a reopen.
  StreamConnection? connection;

  /// Identifies the connect in progress or in use, so a late result can tell
  /// it no longer belongs to anything.
  Object? token;

  /// A connect was called and has not settled.
  bool connecting = false;

  /// True between a connection arriving and it closing.
  bool ready = false;

  /// Subscribers, by channel, in subscription order.
  final Map<String, Set<_Subscription>> channels = {};

  /// Subscribers that send a hello or goodbye, in subscription order.
  final Set<_Subscription> frames = {};

  int refs = 0;
  int attempt = 0;
  int opens = 0;
  bool closing = false;
  bool disposed = false;
  bool reconnecting = false;

  /// Met [TransportUnavailable]; never retried.
  bool unavailable = false;

  StreamSubscription<Object?>? listener;
}

String _cannotSend(String endpoint) =>
    '[forge] $endpoint cannot send; the transport has no send()';

Object? _frame(Object value) => value is Object? Function() ? value() : value;

String _sameChannel(String channel) => channel;

String? _nobody() => null;

/// Ref-counted sockets: one per `(endpoint, principal)`, multiplexed by
/// channel, closed one turn after the last release.
///
/// Owns sharing (ten subscribers to `/ws/orders` are one socket), surviving a
/// phantom unmount (the deferred close), and reconnecting on a schedule while
/// reporting the gap through [onReconnect], only after a reopen.
final class SubscriptionManager {
  /// [baseUrl] plus an endpoint is the connect url; without it the endpoint
  /// is parsed as is. [keepalive] `null` answers nothing. [revive] retries
  /// abandoned sockets when it reports the network is back.
  SubscriptionManager({
    required this._connect,
    this._baseUrl,
    this._headers,
    String Function(String channel)? endpointOf,
    String? Function()? principal,
    this.onReconnect,
    this._sleep = realSleep,
    double Function()? random,
    this._backoff = const BackoffPolicy(),
    this._keepalive = forgeKeepalive,
    ConnectivitySignal? revive,
    Scheduler? release,
    this._onError,
  }) : _endpointOf = endpointOf ?? _sameChannel,
       _principal = principal ?? _nobody,
       _random = random ?? math.Random().nextDouble,
       _release = release ?? microtaskScheduler() {
    _revive = revive?.online.listen((online) {
      if (online) retry();
    });
  }

  /// A socket that had dropped is open and greeted again, and frames were
  /// missed while it was down. Assigned by `StreamBinder`.
  void Function(String endpoint, List<String> channels)? onReconnect;

  final StreamConnect _connect;
  final Uri? _baseUrl;
  final Map<String, String> Function()? _headers;
  final String Function(String channel) _endpointOf;
  final String? Function() _principal;
  final Sleep _sleep;
  final double Function() _random;
  final BackoffPolicy _backoff;
  final Keepalive? _keepalive;
  final Scheduler _release;
  final void Function(Object error, String context)? _onError;
  StreamSubscription<bool>? _revive;

  final Map<String, _Socket> _sockets = {};

  /// Sockets awaiting the deferred close. One set and one scheduled callback,
  /// because a one-slot scheduler would lose all but the last of N callbacks.
  final Set<_Socket> _releasing = {};
  bool _releaseScheduled = false;

  /// How many sockets are held, open or reconnecting.
  int get size => _sockets.length;

  /// Whether this endpoint has a connection, open or still connecting.
  bool connected(String endpoint) {
    final socket = _sockets[endpoint];

    return socket != null && (socket.connection != null || socket.connecting);
  }

  /// Which endpoint a channel resolves to under this manager's `endpointOf`.
  String endpointFor(String channel) => _endpointOf(channel);

  /// Subscribe to a channel. The returned function is the release, and
  /// releasing twice decrements once.
  void Function() subscribe(
    String channel,
    FrameHandler handler, [
    SubscribeOptions options = const SubscribeOptions(),
  ]) {
    final endpoint = _endpointOf(channel);
    final socket = _socketFor(endpoint);
    final subscription = _Subscription(channel, handler, options, socket);

    if (subscription.speaks && socket.connection is ReceiveOnlyConnection) {
      throw StateError(_cannotSend(endpoint));
    }

    // Cancels a deferred close: the socket the phantom unmount was about to
    // close is the one this mount wants.
    socket.closing = false;
    _releasing.remove(socket);

    (socket.channels[channel] ??= <_Subscription>{}).add(subscription);
    socket.refs++;

    if (subscription.speaks) socket.frames.add(subscription);

    if (socket.connection == null &&
        !socket.connecting &&
        !socket.reconnecting &&
        !socket.unavailable) {
      _open(socket);
    }

    final hello = options.hello;

    if (hello != null && socket.ready) _say(socket, _frame(hello));

    var released = false;

    return () {
      if (released) return;

      released = true;

      final held = subscription.socket;
      final speaking = held.frames.remove(subscription);
      final goodbye = options.goodbye;

      if (speaking && goodbye != null && held.ready) {
        _say(held, _frame(goodbye));
      }

      held.refs--;

      final current = held.channels[channel];

      if (current != null) {
        current.remove(subscription);

        if (current.isEmpty) held.channels.remove(channel);
      }

      if (held.refs > 0 || held.disposed) return;

      held.closing = true;
      _releasing.add(held);
      _scheduleRelease();
    };
  }

  /// Close every socket that no longer belongs to the current principal, and
  /// reopen the ones that still have subscribers, through the reconnect path.
  void repartition() {
    final principal = _principal();

    for (final socket in [..._sockets.values]) {
      if (socket.principal == principal) continue;

      final channels = [...socket.channels.keys];
      final held = {
        for (final entry in socket.channels.entries)
          entry.key: {...entry.value},
      };
      final frames = {...socket.frames};
      final refs = socket.refs;

      _dispose(socket);

      if (refs == 0) continue;

      final replacement = _socketFor(socket.endpoint);
      replacement.refs = refs;

      held.forEach((channel, subscriptions) {
        replacement.channels[channel] = subscriptions;

        for (final subscription in subscriptions) {
          subscription.socket = replacement;
        }
      });

      replacement.frames.addAll(frames);

      _open(replacement, report: channels);
    }
  }

  /// Close everything, now. Subscriptions are not restored by a later call.
  void closeAll() {
    for (final socket in [..._sockets.values]) {
      _dispose(socket);
    }

    _releasing.clear();
    unawaited(_revive?.cancel());
    _revive = null;
  }

  /// Run the deferred closes now, whatever the scheduler had planned.
  void flushReleases() {
    _releaseScheduled = false;

    for (final socket in [..._releasing]) {
      if (socket.closing && socket.refs == 0) _dispose(socket);
    }

    _releasing.clear();
  }

  /// Reopen anything that gave up reconnecting, with a full budget.
  ///
  /// A socket that is connected, connecting, counting down, unavailable or
  /// unwatched is left alone, so calling this on every network event is free.
  void retry() {
    for (final socket in [..._sockets.values]) {
      if (socket.disposed || socket.reconnecting || socket.unavailable) {
        continue;
      }
      if (socket.connection != null || socket.connecting) continue;
      if (socket.refs == 0) continue;

      socket.attempt = 0;
      unawaited(_reconnect(socket));
    }
  }

  _Socket _socketFor(String endpoint) {
    final principal = _principal();
    final existing = _sockets[endpoint];

    if (existing != null) {
      // A socket opened for somebody else is not this subscriber's socket.
      if (existing.principal == principal) return existing;

      _dispose(existing);
    }

    return _sockets[endpoint] = _Socket(endpoint, principal);
  }

  /// Start one connect. [report] is handed to [onReconnect] once the socket
  /// is greeted. Returns false when the factory threw synchronously.
  bool _open(_Socket socket, {List<String>? report, bool retry = true}) {
    if (socket.disposed) return false;

    socket.principal = _principal();
    socket.reconnecting = false;
    socket.ready = false;

    final token = Object();
    final context = StreamConnectContext(
      url: _urlFor(socket.endpoint),
      endpoint: socket.endpoint,
      headers: _headers?.call() ?? const {},
      principal: socket.principal,
      channels: [...socket.channels.keys],
      attempt: socket.opens,
    );

    final Future<StreamConnection> pending;

    try {
      pending = _connect(context);
    } on Object catch (error) {
      _failed(socket, error, retry: retry);

      return false;
    }

    socket.token = token;
    socket.connecting = true;
    socket.opens++;

    unawaited(
      pending.then<void>(
        (connection) => _adopt(socket, token, connection, report),
        onError: (Object error) {
          if (socket.disposed || socket.token != token) return;

          socket.token = null;
          socket.connecting = false;
          _failed(socket, error, retry: true);
        },
      ),
    );

    return true;
  }

  void _failed(_Socket socket, Object error, {required bool retry}) {
    _report(error, 'stream connect ${socket.endpoint}');

    if (error is TransportUnavailable) {
      socket.unavailable = true;

      return;
    }

    if (retry && socket.refs > 0) unawaited(_reconnect(socket));
  }

  void _adopt(
    _Socket socket,
    Object token,
    StreamConnection connection,
    List<String>? report,
  ) {
    if (socket.disposed || socket.token != token) {
      // A connection nobody wants any more: its socket was released or
      // replaced while it was connecting.
      _close(socket.endpoint, connection);

      return;
    }

    socket.connecting = false;
    socket.connection = connection;
    socket.ready = true;

    var over = false;

    void dropped() {
      if (over) return;

      over = true;

      if (socket.disposed || socket.connection != connection) return;

      socket.ready = false;
      socket.connection = null;
      socket.token = null;
      unawaited(socket.listener?.cancel());
      socket.listener = null;

      if (socket.refs == 0) return;

      unawaited(_reconnect(socket));
    }

    socket.listener = connection.messages.listen(
      (message) {
        if (socket.disposed || socket.connection != connection) return;

        // Nothing from a previous principal may reach the next, even when the
        // caller has not repartitioned yet.
        if (socket.principal != _principal()) return;

        // Any traffic proves the endpoint healthy, so the next drop starts
        // its backoff from the first rung.
        socket.attempt = 0;

        _answer(connection, message);
        _deliver(socket, message);
      },
      onError: (Object error) {
        if (socket.disposed || socket.connection != connection) return;

        _report(error, 'stream ${socket.endpoint}');
      },
      onDone: dropped,
    );

    unawaited(
      connection.closed.then<void>(
        (_) => dropped(),
        onError: (Object error) {
          _report(error, 'stream ${socket.endpoint}');
          dropped();
        },
      ),
    );

    _greet(socket);

    // After the greeting, so a consumer that refetches on a reconnect never
    // asks about a channel the server does not yet know this client wants.
    if (report != null) onReconnect?.call(socket.endpoint, report);
  }

  /// Wait, reopen, and let the reopen report the gap.
  Future<void> _reconnect(_Socket socket) async {
    if (socket.disposed || socket.reconnecting || socket.unavailable) return;

    socket.reconnecting = true;

    try {
      while (!socket.disposed && socket.refs > 0) {
        final budget = _backoff.attempts;

        if (budget != null && socket.attempt >= budget) {
          _report(
            StateError(
              '[forge] gave up reconnecting to ${socket.endpoint} after '
              '$budget attempts',
            ),
            'stream ${socket.endpoint}',
          );

          return;
        }

        final delay = _backoff.delay(socket.attempt, _random());
        socket.attempt++;

        try {
          await _sleep(delay);
        } on Object catch (error) {
          _report(error, 'stream sleep ${socket.endpoint}');
        }

        if (socket.disposed || socket.refs == 0) return;

        if (_open(socket, report: [...socket.channels.keys], retry: false)) {
          return;
        }

        if (socket.unavailable) return;
      }
    } finally {
      socket.reconnecting = false;
    }
  }

  /// Answer a keepalive, before delivery and never instead of it.
  void _answer(StreamConnection connection, Object? message) {
    final keepalive = _keepalive;

    if (keepalive == null || connection is ReceiveOnlyConnection) return;

    try {
      final reply = keepalive(message);

      if (reply != null) connection.send(reply);
    } on Object catch (error) {
      _report(error, 'stream keepalive');
    }
  }

  void _say(_Socket socket, Object? message) {
    final connection = socket.connection;

    if (connection == null || connection is ReceiveOnlyConnection) return;

    try {
      connection.send(message);
    } on Object catch (error) {
      _report(error, 'stream send ${socket.endpoint}');
    }
  }

  /// Every hello, in subscription order. Called on each open, first or not.
  void _greet(_Socket socket) {
    final connection = socket.connection;

    if (connection == null || socket.frames.isEmpty) return;

    if (connection is ReceiveOnlyConnection) {
      _report(
        StateError(_cannotSend(socket.endpoint)),
        'stream send ${socket.endpoint}',
      );

      return;
    }

    for (final held in [...socket.frames]) {
      final hello = held.options.hello;

      if (hello != null) _say(socket, _frame(hello));
    }
  }

  void _deliver(_Socket socket, Object? message) {
    // Copied before iterating: a handler releasing its own subscription would
    // otherwise mutate the collection being walked.
    for (final entry in [...socket.channels.entries]) {
      final channel = entry.key;

      for (final subscription in [...entry.value]) {
        try {
          subscription.handler(message, channel);
        } on Object catch (error) {
          // One subscriber must not cost the others their frame.
          _report(error, 'stream handler $channel');
        }
      }
    }
  }

  void _dispose(_Socket socket) {
    socket.disposed = true;
    socket.closing = false;
    socket.reconnecting = false;
    socket.connecting = false;
    socket.token = null;
    _releasing.remove(socket);

    final connection = socket.connection;
    socket.connection = null;
    socket.ready = false;
    unawaited(socket.listener?.cancel());
    socket.listener = null;

    if (identical(_sockets[socket.endpoint], socket)) {
      _sockets.remove(socket.endpoint);
    }

    if (connection != null) _close(socket.endpoint, connection);
  }

  void _close(String endpoint, StreamConnection connection) {
    try {
      unawaited(
        connection.close().catchError((Object error) {
          _report(error, 'stream close $endpoint');
        }),
      );
    } on Object catch (error) {
      _report(error, 'stream close $endpoint');
    }
  }

  void _scheduleRelease() {
    if (_releaseScheduled) return;

    _releaseScheduled = true;
    _release.schedule(() {
      if (_releaseScheduled) flushReleases();
    });
  }

  Uri _urlFor(String endpoint) {
    final base = _baseUrl;

    if (base == null || endpoint.contains('://')) return Uri.parse(endpoint);

    final root = base.toString();
    final trimmed = root.endsWith('/')
        ? root.substring(0, root.length - 1)
        : root;

    return Uri.parse('$trimmed$endpoint');
  }

  void _report(Object error, String context) => _onError?.call(error, context);
}

/// One multiplexed channel on a socket. `handlers` is this channel's ref
/// count; `hello` says whether any subscriber on it sends a hello frame.
typedef ChannelSnapshot = ({String channel, int handlers, bool hello});

/// One socket, copied out for an inspector. A copy, so reading it cannot
/// reach the connection.
final class SocketSnapshot {
  /// Copies one socket's state.
  const SocketSnapshot({
    required this.endpoint,
    required this.principal,
    required this.connected,
    required this.refs,
    required this.opens,
    required this.attempt,
    required this.reconnecting,
    required this.closing,
    required this.channels,
  });

  /// The endpoint this socket serves.
  final String endpoint;

  /// The identity it was opened for.
  final String? principal;

  /// Whether it has a connection, open or still connecting.
  final bool connected;

  /// Outstanding subscriptions across every channel.
  final int refs;

  /// How many times it has been opened. Above 1 means it dropped.
  final int opens;

  /// Consecutive failed reconnects.
  final int attempt;

  /// A reconnect is waiting on the clock.
  final bool reconnecting;

  /// The deferred close is pending.
  final bool closing;

  /// Its channels, in subscription order.
  final List<ChannelSnapshot> channels;
}

/// Every socket a manager holds, copied out. Opens nothing, closes nothing.
List<SocketSnapshot> socketSnapshot(SubscriptionManager manager) => [
  for (final socket in manager._sockets.values)
    SocketSnapshot(
      endpoint: socket.endpoint,
      principal: socket.principal,
      connected: socket.connection != null || socket.connecting,
      refs: socket.refs,
      opens: socket.opens,
      attempt: socket.attempt,
      reconnecting: socket.reconnecting,
      closing: socket.closing,
      channels: [
        for (final MapEntry(key: channel, value: subscriptions)
            in socket.channels.entries)
          (
            channel: channel,
            handlers: subscriptions.length,
            hello: socket.frames.any(
              (held) => held.channel == channel && held.options.hello != null,
            ),
          ),
      ],
    ),
];
