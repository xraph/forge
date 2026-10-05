import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/fake_sockets.dart';

// Keeping a socket alive, and getting it back when it was not. The streaming
// extension judges liveness by inbound traffic only, and its ping is an
// application message, so a client that only listens must answer it.

typedef _Kit = ({
  SubscriptionManager subscriptions,
  FakeSockets sockets,
  List<(Object, String)> errors,
});

_Kit _build({
  int? attempts = 10,
  Keepalive? keepalive = forgeKeepalive,
  ConnectivitySignal? revive,
  bool receiveOnly = false,
}) {
  final sockets = FakeSockets(receiveOnly: receiveOnly);
  final errors = <(Object, String)>[];
  final subscriptions = SubscriptionManager(
    connect: sockets.connect,
    random: () => 0,
    backoff: BackoffPolicy(
      initial: const Duration(seconds: 1),
      max: const Duration(seconds: 8),
      jitter: 0.5,
      attempts: attempts,
    ),
    release: ManualScheduler(),
    onError: (error, context) => errors.add((error, context)),
    keepalive: keepalive,
    revive: revive,
  );

  return (subscriptions: subscriptions, sockets: sockets, errors: errors);
}

void main() {
  group('answering the server keepalive', () {
    test('sends a pong when the server pings', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        kit.sockets.last().deliver({
          'type': 'system',
          'event': 'ping',
          'id': 'p1',
        });
        async.flushMicrotasks();

        expect(kit.sockets.last().sent, [
          {'type': 'system', 'event': 'pong'},
        ]);
      });
    });

    test('leaves an ordinary frame alone', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        kit.sockets.last().deliver({'type': 'order.created', 'id': 'o1'});
        async.flushMicrotasks();

        expect(kit.sockets.last().sent, isEmpty);
      });
    });

    test('still delivers the keepalive to subscribers', () {
      fakeAsync((async) {
        final kit = _build();
        final seen = <Object?>[];

        kit.subscriptions.subscribe(
          '/ws/orders',
          (message, _) => seen.add(message),
        );
        async.flushMicrotasks();
        kit.sockets.last().deliver({'type': 'system', 'event': 'ping'});
        async.flushMicrotasks();

        // Answering is not swallowing.
        expect(seen, [
          {'type': 'system', 'event': 'ping'},
        ]);
      });
    });

    test('does not throw when the transport cannot send', () {
      fakeAsync((async) {
        final kit = _build(receiveOnly: true);

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();

        expect(() {
          kit.sockets.last().deliver({'type': 'system', 'event': 'ping'});
          async.flushMicrotasks();
        }, returnsNormally);
        expect(kit.errors, isEmpty);
      });
    });

    test('can be replaced with an application policy', () {
      fakeAsync((async) {
        final kit = _build(
          keepalive: (message) =>
              message is Map<Object?, Object?> && message['kind'] == 'hb'
              ? {'kind': 'hb-ack'}
              : null,
        );

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        kit.sockets.last().deliver({'kind': 'hb'});
        kit.sockets.last().deliver({'type': 'system', 'event': 'ping'});
        async.flushMicrotasks();

        // The default is replaced, not added to.
        expect(kit.sockets.last().sent, [
          {'kind': 'hb-ack'},
        ]);
      });
    });

    test('can be turned off entirely', () {
      fakeAsync((async) {
        final kit = _build(keepalive: null);

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        kit.sockets.last().deliver({'type': 'system', 'event': 'ping'});
        async.flushMicrotasks();

        expect(kit.sockets.last().sent, isEmpty);
      });
    });

    test('exports the default policy for an application composing its own', () {
      expect(forgeKeepalive({'type': 'system', 'event': 'ping'}), {
        'type': 'system',
        'event': 'pong',
      });
      expect(forgeKeepalive({'type': 'order.created'}), isNull);
      expect(forgeKeepalive(null), isNull);
      expect(forgeKeepalive('ping'), isNull);
    });
  });

  group('recovering a socket that gave up', () {
    void exhaust(_Kit kit, FakeAsync async) {
      for (var i = 0; i < 3; i++) {
        kit.sockets.last().drop();
        async.elapse(const Duration(seconds: 10));
      }
    }

    test('reopens on retry after the reconnect budget is exhausted', () {
      fakeAsync((async) {
        final kit = _build(attempts: 2);

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        expect(kit.sockets.opened, hasLength(1));

        // Two failed attempts, which is the whole budget.
        exhaust(kit, async);

        expect(
          kit.errors.any((entry) => '${entry.$1}'.contains('gave up')),
          isTrue,
        );

        final abandoned = kit.sockets.opened.length;

        kit.subscriptions.retry();
        async.elapse(const Duration(seconds: 10));

        expect(kit.sockets.opened.length, greaterThan(abandoned));
        expect(kit.sockets.last().isClosed, isFalse);
      });
    });

    test('resets the attempt budget so a later outage gets a full run', () {
      fakeAsync((async) {
        final kit = _build(attempts: 2);

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        exhaust(kit, async);

        final first = kit.errors
            .where((entry) => '${entry.$1}'.contains('gave up'))
            .length;

        kit.subscriptions.retry();
        async.elapse(const Duration(seconds: 10));

        // A retry that inherited the exhausted counter would give up on the
        // very first drop rather than after another full budget.
        kit.sockets.last().drop();
        async.elapse(const Duration(seconds: 10));

        expect(
          kit.errors.where((entry) => '${entry.$1}'.contains('gave up')),
          hasLength(first),
        );
      });
    });

    test('does nothing to a healthy socket', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        final before = kit.sockets.opened.length;

        kit.subscriptions.retry();
        async.elapse(const Duration(seconds: 10));

        expect(kit.sockets.opened, hasLength(before));
        expect(kit.sockets.last().isClosed, isFalse);
      });
    });

    test('retries when the revive target says the network is back', () {
      fakeAsync((async) {
        final target = FakeConnectivity();
        final kit = _build(attempts: 1, revive: target);

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();

        kit.sockets.last().drop();
        async.elapse(const Duration(seconds: 10));
        kit.sockets.last().drop();
        async.elapse(const Duration(seconds: 10));

        final abandoned = kit.sockets.opened.length;

        target.emit(true);
        async.elapse(const Duration(seconds: 10));

        expect(kit.sockets.opened.length, greaterThan(abandoned));
      });
    });

    test('unhooks its listeners on closeAll', () {
      fakeAsync((async) {
        final target = FakeConnectivity();
        final kit = _build(revive: target);

        expect(target.hooked, isTrue);

        kit.subscriptions.closeAll();
        async.flushMicrotasks();

        expect(target.hooked, isFalse);
      });
    });

    test('registers nothing when revive is off', () {
      final target = FakeConnectivity();

      _build();

      expect(target.hooked, isFalse);
    });
  });
}
