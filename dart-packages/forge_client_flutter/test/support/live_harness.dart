import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';

import 'harness.dart';

/// The generated `streams` table for the one channel these tests push on.
/// Bound to `Order` only: which channels a query resolves to is the core's
/// rule, tested there.
const List<StreamBinding> orderStreams = [
  EntityStreamBinding(
    channel: '/ws/orders',
    message: 'order.created',
    entity: 'Order',
    intent: StreamIntent.upsert,
    invalidates: ['Order[]'],
  ),
  EntityStreamBinding(
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: StreamIntent.patch,
  ),
];

/// A socket the test drives by hand: no WebSocket, no server, no timers.
final class FakeConnection implements StreamConnection {
  FakeConnection(this.context);

  final StreamConnectContext context;
  final StreamController<Object?> _messages = StreamController<Object?>.broadcast(sync: true);
  final Completer<void> _closed = Completer<void>();

  bool get isClosed => _closed.isCompleted;

  @override
  Stream<Object?> get messages => _messages.stream;

  @override
  Future<void> get closed => _closed.future;

  @override
  void send(Object? message) {}

  @override
  Future<void> close() async {
    if (!_closed.isCompleted) _closed.complete();
  }

  /// The server hangs up: [closed] completes and the manager reconnects.
  void dropFromServer() => unawaited(close());

  /// Pushes one message to whoever is listening, if the socket is open.
  void deliver(Object? message) {
    if (!isClosed) _messages.add(message);
  }
}

final class LiveHarness {
  LiveHarness({
    required this.cache,
    required this.transport,
    required this.scheduler,
    required this.closes,
    required this.manager,
    required this.binder,
    required this.opened,
    required this.errors,
  });

  final QueryCache cache;
  final FakeTransport transport;
  final ManualScheduler scheduler;

  /// When a socket nobody is subscribed to is actually closed. Manual, so a
  /// test that asserts a release decides when the deferral elapsed.
  final ManualScheduler closes;

  final SubscriptionManager manager;
  final StreamBinder binder;

  /// Every socket ever opened, in order.
  final List<FakeConnection> opened;

  /// Every error the cache, the manager or the binder reported, as
  /// `context: error`. A test tears down red if this is not empty, so a
  /// swallowed error cannot pass.
  final List<String> errors;

  /// Asserts the errors the runtime reported are exactly [expected], then
  /// forgets them so the teardown check does not fail on them. The only way
  /// to consume an error: a test that does not call this fails on any.
  void expectErrors(Matcher expected) {
    expect(errors, expected);
    errors.clear();
  }

  /// The same cache as a [Harness], for `scope()`.
  Harness get harness => Harness(cache, transport, scheduler);

  /// How many sockets are still open.
  int live() => opened.where((connection) => !connection.isClosed).length;

  /// Pushes one message onto every open socket. The commit lands on the next
  /// Flutter frame, because the cache's commit scheduler is
  /// [frameCommitScheduler].
  void emit(Object? message) {
    for (final connection in opened) {
      connection.deliver(message);
    }
  }
}

LiveHarness liveHarness(
  FutureOr<Object?> Function(TransportRequest request, int call) handler, {
  List<StreamBinding> bindings = orderStreams,

  /// Where the manager says its sockets belong. Defaults to the cache's own
  /// principal, which is what a correctly wired app passes. A test that
  /// passes anything else is building the miswired case on purpose, and must
  /// consume the error the binder reports with [LiveHarness.expectErrors].
  String? Function()? managerPrincipal,
}) {
  final transport = FakeTransport(handler);
  final scheduler = ManualScheduler();
  final closes = ManualScheduler();
  final opened = <FakeConnection>[];
  final errors = <String>[];
  void onError(Object error, String context) => errors.add('$context: $error');
  // Recorded and asserted at teardown rather than thrown: an error thrown
  // inside a runtime callback is exactly what the runtime swallows.
  addTearDown(() => expect(errors, isEmpty, reason: 'the runtime reported an error'));
  final cache = QueryCache(
    transport: transport,
    entities: schema,
    scheduler: scheduler,
    commitScheduler: frameCommitScheduler(),
    onError: onError,
  );
  final manager = SubscriptionManager(
    connect: (context) async {
      final connection = FakeConnection(context);
      opened.add(connection);
      return connection;
    },
    release: closes,
    principal: managerPrincipal ?? () => cache.principal,
    // The first backoff is then exactly 400ms: 500ms less the whole 20% jitter.
    random: () => 0,
    onError: onError,
  );
  // The binder attaches itself to the cache, which is how `live: true`
  // finds it. Nothing hands it to a widget.
  final binder = StreamBinder(
    cache: cache,
    streams: bindings,
    manager: manager,
    onError: onError,
  );
  return LiveHarness(
    cache: cache,
    transport: transport,
    scheduler: scheduler,
    closes: closes,
    manager: manager,
    binder: binder,
    opened: opened,
    errors: errors,
  );
}
