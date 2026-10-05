import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/fake_sockets.dart';

// Nothing in this file touches a socket, a timer or the wall clock. The
// transport is FakeSockets, the delay runs on fakeAsync, and the deferred
// close is a ManualScheduler, so every assertion is about an ordering the test
// chose. Dart streams deliver on a microtask, so each delivery is followed by
// flushMicrotasks where TS asserts synchronously.

typedef _Kit = ({
  SubscriptionManager subscriptions,
  FakeSockets sockets,
  ManualScheduler release,
  List<(Object, String)> errors,
  List<(String, String)> reconnects,
});

_Kit _build({
  String? Function()? principal,
  String Function(String channel)? endpointOf,
  int? attempts = 10,
  bool autoOpen = true,
}) {
  final sockets = FakeSockets(autoOpen: autoOpen);
  final release = ManualScheduler();
  final errors = <(Object, String)>[];
  final reconnects = <(String, String)>[];

  final subscriptions = SubscriptionManager(
    connect: sockets.connect,
    // No jitter: the delay a test asserts on should be the delay the policy
    // computes, not a sample from it.
    random: () => 0,
    backoff: BackoffPolicy(
      initial: const Duration(seconds: 1),
      max: const Duration(seconds: 8),
      jitter: 0.5,
      attempts: attempts,
    ),
    release: release,
    onError: (error, context) => errors.add((error, context)),
    principal: principal,
    endpointOf: endpointOf,
  );

  subscriptions.onReconnect = (endpoint, channels) =>
      reconnects.add((endpoint, channels.join(',')));

  return (
    subscriptions: subscriptions,
    sockets: sockets,
    release: release,
    errors: errors,
    reconnects: reconnects,
  );
}

void main() {
  group('ref counting', () {
    test(
      'shares one socket across subscribers and closes on the last release',
      () {
        fakeAsync((async) {
          final kit = _build();
          final seen = <Object?>[];

          final first = kit.subscriptions.subscribe(
            '/ws/orders',
            (message, _) => seen.add(['a', message]),
          );
          final second = kit.subscriptions.subscribe(
            '/ws/orders',
            (message, _) => seen.add(['b', message]),
          );

          expect(kit.sockets.opened, hasLength(1));
          expect(kit.subscriptions.size, 1);

          async.flushMicrotasks();
          kit.sockets.last().deliver({'type': 'order.created'});
          async.flushMicrotasks();
          expect(seen, hasLength(2));

          first();
          kit.release.flush();

          // One subscriber left: the socket is still open and still delivering.
          expect(kit.sockets.last().isClosed, isFalse);
          kit.sockets.last().deliver({'type': 'order.updated'});
          async.flushMicrotasks();
          expect(seen, hasLength(3));

          second();
          kit.release.flush();

          expect(kit.sockets.last().isClosed, isTrue);
          expect(kit.subscriptions.size, 0);
          expect(kit.sockets.opened, hasLength(1));
        });
      },
    );

    test('releases once however many times the release is called', () {
      fakeAsync((async) {
        final kit = _build();

        final one = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        final two = kit.subscriptions.subscribe('/ws/orders', (_, _) {});

        // Let the connection arrive: a socket still connecting is not open, so
        // "not closed" would hold whatever the release did.
        async.flushMicrotasks();

        one();
        one();
        one();
        kit.release.flush();
        async.flushMicrotasks();

        // Three calls to one release is one decrement. A count that went down
        // each time would have closed the socket under the second subscriber.
        expect(kit.sockets.last().isClosed, isFalse);

        two();
        kit.release.flush();
        async.flushMicrotasks();
        expect(kit.sockets.last().isClosed, isTrue);
      });
    });

    test(
      'multiplexes channels that share an endpoint, and counts them together',
      () {
        fakeAsync((async) {
          final kit = _build(endpointOf: (_) => '/ws');
          final orders = <String>[];
          final shipments = <String>[];

          final a = kit.subscriptions.subscribe(
            '/ws/orders',
            (_, channel) => orders.add(channel),
          );
          final b = kit.subscriptions.subscribe(
            '/ws/shipments',
            (_, channel) => shipments.add(channel),
          );

          expect(kit.sockets.opened, hasLength(1));
          expect(kit.sockets.last().context.channels, ['/ws/orders']);

          // One frame off the shared socket reaches both channels' subscribers,
          // each told which channel it was listening on.
          async.flushMicrotasks();
          kit.sockets.last().deliver({'type': 'order.created'});
          async.flushMicrotasks();
          expect(orders, ['/ws/orders']);
          expect(shipments, ['/ws/shipments']);

          a();
          kit.release.flush();
          expect(kit.sockets.last().isClosed, isFalse);

          b();
          kit.release.flush();
          expect(kit.sockets.last().isClosed, isTrue);
        });
      },
    );
  });

  group('StrictMode', () {
    test('leaves a live subscription after mount, unmount, mount', () {
      fakeAsync((async) {
        final kit = _build();
        final seen = <Object?>[];

        final first = kit.subscriptions.subscribe(
          '/ws/orders',
          (message, _) => seen.add(message),
        );
        first();
        final second = kit.subscriptions.subscribe(
          '/ws/orders',
          (message, _) => seen.add(message),
        );

        // The deferred close now runs, and must find the socket claimed again.
        kit.release.flush();

        expect(kit.sockets.opened, hasLength(1));
        expect(kit.sockets.last().isClosed, isFalse);

        async.flushMicrotasks();
        kit.sockets.last().deliver({'type': 'order.created'});
        async.flushMicrotasks();
        expect(seen, hasLength(1));

        second();
        kit.release.flush();
        expect(kit.sockets.last().isClosed, isTrue);
      });
    });

    test('does not open a second socket for the phantom remount', () {
      fakeAsync((async) {
        final kit = _build();

        for (var cycle = 0; cycle < 5; cycle++) {
          final release1 = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
          release1();
          final release2 = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
          kit.release.flush();
          release2();
          kit.release.flush();
        }

        // Five mounts, five unmounts, and five sockets, not ten.
        expect(kit.sockets.opened, hasLength(5));
      });
    });

    test(
      'defers the close of several sockets through one scheduled callback',
      () {
        fakeAsync((async) {
          // ManualScheduler holds exactly one queued flush, so a manager that
          // scheduled per socket would lose all but the last and leak the rest.
          final kit = _build();

          final a = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
          final b = kit.subscriptions.subscribe('/ws/shipments', (_, _) {});
          async.flushMicrotasks();

          expect(kit.sockets.live(), 2);

          a();
          b();
          kit.release.flush();
          async.flushMicrotasks();

          expect(kit.sockets.live(), 0);
          expect(kit.subscriptions.size, 0);
        });
      },
    );
  });

  group('reconnect', () {
    test('backs off exponentially on the injected clock, and reports the gap once back', () {
      fakeAsync((async) {
        final kit = _build(autoOpen: false);

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        expect(kit.sockets.opened, hasLength(1));

        // Dart opens asynchronously, so the first socket is opened before it
        // is dropped. TS can drop a socket that never reported open.
        kit.sockets.last().open();
        async.flushMicrotasks();
        kit.sockets.last().drop();
        async.flushMicrotasks();
        expect(kit.subscriptions.connected('/ws/orders'), isFalse);

        // Nothing yet: the first attempt is 1000ms out, half fixed and half
        // jitter, and the jitter source is pinned at zero.
        async.elapse(const Duration(milliseconds: 499));
        expect(kit.sockets.opened, hasLength(1));

        async.elapse(const Duration(milliseconds: 1));
        expect(kit.sockets.opened, hasLength(2));
        expect(kit.subscriptions.connected('/ws/orders'), isTrue);

        // The reopened socket is not ready until the transport says so, and
        // the gap is not reported until then either.
        expect(kit.reconnects, isEmpty);
        kit.sockets.last().open();
        async.flushMicrotasks();
        expect(kit.reconnects, [('/ws/orders', '/ws/orders')]);

        // The new socket delivers to a subscriber: reconnecting is not a
        // resubscribe.
        var seen = 0;
        kit.subscriptions.subscribe('/ws/orders', (_, _) => seen++);
        kit.sockets.last().deliver({'type': 'order.created'});
        async.flushMicrotasks();
        expect(seen, 1);
      });
    });

    test('escalates the delay across consecutive failures and caps it', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();

        // Each reopened socket drops before delivering anything, so nothing
        // proves the endpoint healthy and the backoff keeps climbing.
        const delays = [500, 1000, 2000, 4000, 4000];

        for (var index = 0; index < delays.length; index++) {
          kit.sockets.last().drop();
          async.flushMicrotasks();

          async.elapse(Duration(milliseconds: delays[index] - 1));
          expect(kit.sockets.opened, hasLength(index + 1));

          async.elapse(const Duration(milliseconds: 1));
          expect(kit.sockets.opened, hasLength(index + 2));
        }
      });
    });

    test('restarts the backoff once the endpoint proves itself', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();

        kit.sockets.last().drop();
        async.elapse(const Duration(milliseconds: 500));
        expect(kit.sockets.opened, hasLength(2));

        kit.sockets.last().drop();
        async.elapse(const Duration(milliseconds: 1000));
        expect(kit.sockets.opened, hasLength(3));

        // A frame is proof of life, so the next outage starts from the first
        // rung rather than from the third.
        kit.sockets.last().deliver({'type': 'order.created'});
        async.flushMicrotasks();
        kit.sockets.last().drop();

        async.elapse(const Duration(milliseconds: 499));
        expect(kit.sockets.opened, hasLength(3));
        async.elapse(const Duration(milliseconds: 1));
        expect(kit.sockets.opened, hasLength(4));
      });
    });

    test(
      'gives up after the configured attempts rather than retrying forever',
      () {
        fakeAsync((async) {
          final kit = _build(attempts: 3);

          kit.subscriptions.subscribe('/ws/orders', (_, _) {});
          async.flushMicrotasks();

          for (var attempt = 0; attempt < 3; attempt++) {
            kit.sockets.last().drop();
            async.elapse(const Duration(seconds: 60));
          }

          expect(kit.sockets.opened, hasLength(4));

          kit.sockets.last().drop();
          async.elapse(const Duration(seconds: 60));

          expect(kit.sockets.opened, hasLength(4));
          expect(
            kit.errors.map((entry) => '${entry.$1}'),
            contains(contains('gave up reconnecting')),
          );
        });
      },
    );

    test('does not reconnect a socket nobody is subscribed to any more', () {
      fakeAsync((async) {
        final kit = _build();

        final stop = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();

        kit.sockets.last().drop();
        stop();
        kit.release.flush();

        async.elapse(const Duration(seconds: 60));

        expect(kit.sockets.opened, hasLength(1));
      });
    });

    test('does not report a gap on the first connect', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();

        // There is nothing to recover: the query is loading right now.
        expect(kit.reconnects, isEmpty);
      });
    });

    test('ignores a frame from a connection it has already replaced', () {
      fakeAsync((async) {
        final kit = _build();
        final seen = <Object?>[];

        kit.subscriptions.subscribe(
          '/ws/orders',
          (message, _) => seen.add(message),
        );
        async.flushMicrotasks();

        final stale = kit.sockets.last();
        stale.drop();
        async.elapse(const Duration(milliseconds: 1000));

        expect(kit.sockets.opened, hasLength(2));

        stale.deliver({
          'type': 'order.created',
          'payload': {'id': 1},
        });
        async.flushMicrotasks();
        expect(seen, isEmpty);

        kit.sockets.last().deliver({
          'type': 'order.created',
          'payload': {'id': 2},
        });
        async.flushMicrotasks();
        expect(seen, hasLength(1));
      });
    });
  });

  group('principal', () {
    test('never adopts a socket opened for a different identity', () {
      fakeAsync((async) {
        var principal = 'user-a';
        final kit = _build(principal: () => principal);

        final first = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        expect(kit.sockets.opened, hasLength(1));
        expect(kit.sockets.last().context.principal, 'user-a');
        async.flushMicrotasks();

        principal = 'user-b';
        kit.subscriptions.subscribe('/ws/orders', (_, _) {});

        expect(kit.sockets.opened, hasLength(2));
        expect(kit.sockets.opened.first.isClosed, isTrue);
        expect(kit.sockets.last().context.principal, 'user-b');

        first();
      });
    });

    test(
      'repartitions open sockets onto the new identity, keeping subscribers',
      () {
        fakeAsync((async) {
          String? principal = 'user-a';
          final kit = _build(principal: () => principal, autoOpen: false);
          final seen = <Object?>[];

          kit.subscriptions.subscribe(
            '/ws/orders',
            (message, _) => seen.add(message),
          );
          kit.sockets.last().open();
          async.flushMicrotasks();

          final before = kit.sockets.last();

          principal = 'user-b';
          kit.subscriptions.repartition();

          expect(before.isClosed, isTrue);
          expect(kit.sockets.opened, hasLength(2));
          expect(kit.sockets.last().context.principal, 'user-b');

          // Not yet: the replacement has not reported open.
          expect(kit.reconnects, isEmpty);
          kit.sockets.last().open();
          async.flushMicrotasks();

          // The gap is reported, because the new session missed everything the
          // old socket would have carried and its store was just emptied.
          expect(kit.reconnects, [('/ws/orders', '/ws/orders')]);

          // The old socket is inert.
          before.deliver({'type': 'order.created'});
          async.flushMicrotasks();
          expect(seen, isEmpty);

          // The subscriber survived the swap.
          kit.sockets.last().deliver({'type': 'order.created'});
          async.flushMicrotasks();
          expect(seen, hasLength(1));
        });
      },
    );

    test('leaves sockets alone when the identity did not move', () {
      fakeAsync((async) {
        final kit = _build(principal: () => 'user-a');

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        kit.subscriptions.repartition();
        async.flushMicrotasks();

        expect(kit.sockets.opened, hasLength(1));
        expect(kit.sockets.last().isClosed, isFalse);
        expect(kit.reconnects, isEmpty);
      });
    });

    test('closes the replacement socket when a subscriber from before a repartition releases', () {
      fakeAsync((async) {
        var principal = 'user-a';
        final kit = _build(principal: () => principal, autoOpen: false);

        final stop = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        kit.sockets.last().open();
        async.flushMicrotasks();

        principal = 'user-b';
        kit.subscriptions.repartition();

        final replacement = kit.sockets.last();
        replacement.open();
        async.flushMicrotasks();

        // The release was handed out for the socket repartition disposed. It
        // has to land on the replacement, or the replacement keeps a ref
        // nobody holds.
        stop();
        kit.release.flush();
        async.flushMicrotasks();

        expect(replacement.isClosed, isTrue);
        expect(kit.subscriptions.size, 0);
      });
    });

    test('drops a frame from a socket opened for a previous principal', () {
      fakeAsync((async) {
        // Defence in depth: the rule is that nothing from the previous
        // principal reaches the next, even when the caller repartitions late.
        var principal = 'user-a';
        final kit = _build(principal: () => principal);
        final seen = <Object?>[];

        kit.subscriptions.subscribe(
          '/ws/orders',
          (message, _) => seen.add(message),
        );
        async.flushMicrotasks();

        final before = kit.sockets.last();

        // The identity moved and nobody has repartitioned yet.
        principal = 'user-b';
        before.deliver({'type': 'order.created', 'payload': 'a'});
        async.flushMicrotasks();
        expect(seen, isEmpty);

        kit.subscriptions.repartition();
        async.flushMicrotasks();

        expect(kit.sockets.opened, hasLength(2));
        kit.sockets.last().deliver({'type': 'order.created', 'payload': 'b'});
        async.flushMicrotasks();
        expect(seen, [
          {'type': 'order.created', 'payload': 'b'},
        ]);
      });
    });
  });

  group('failures', () {
    test('reports a transport error without closing anything', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();
        kit.sockets.last().fail(StateError('frame too large'));
        async.flushMicrotasks();

        expect(kit.sockets.last().isClosed, isFalse);
        expect(kit.errors.first.$2, 'stream /ws/orders');
      });
    });

    test('does not let one subscriber’s throw cost the others their frame', () {
      fakeAsync((async) {
        final kit = _build();
        final seen = <Object?>[];

        kit.subscriptions.subscribe(
          '/ws/orders',
          (_, _) => throw StateError('render exploded'),
        );
        kit.subscriptions.subscribe(
          '/ws/orders',
          (message, _) => seen.add(message),
        );
        async.flushMicrotasks();

        kit.sockets.last().deliver({'type': 'order.created'});
        async.flushMicrotasks();

        expect(seen, hasLength(1));
        expect(kit.errors.first.$2, 'stream handler /ws/orders');
      });
    });

    test('retries a connect factory that throws rather than abandoning the channel', () {
      fakeAsync((async) {
        final sockets = FakeSockets();
        final errors = <Object>[];
        var fail = true;

        final subscriptions = SubscriptionManager(
          connect: (context) {
            if (fail) throw StateError('no token yet');

            return sockets.connect(context);
          },
          random: () => 0,
          backoff: const BackoffPolicy(
            initial: Duration(seconds: 1),
            jitter: 0.5,
          ),
          onError: (error, _) => errors.add(error),
        );

        subscriptions.subscribe('/ws/orders', (_, _) {});

        expect(sockets.opened, isEmpty);
        expect(errors, hasLength(1));

        fail = false;
        async.elapse(const Duration(milliseconds: 1000));

        expect(sockets.opened, hasLength(1));
      });
    });
  });

  group('Dart port', () {
    test('closes a connection that arrives after its last subscriber left', () {
      fakeAsync((async) {
        final kit = _build(autoOpen: false);

        final stop = kit.subscriptions.subscribe('/ws/orders', (_, _) {});
        stop();
        kit.release.flush();

        kit.sockets.last().open();
        async.flushMicrotasks();

        expect(kit.sockets.last().isClosed, isTrue);
        expect(kit.subscriptions.size, 0);
      });
    });

    test('reports TransportUnavailable once and never retries it', () {
      fakeAsync((async) {
        final errors = <(Object, String)>[];
        var connects = 0;

        final subscriptions = SubscriptionManager(
          connect: (context) async {
            connects++;

            throw TransportUnavailable(context.endpoint, 'webtransport');
          },
          random: () => 0,
          onError: (error, context) => errors.add((error, context)),
        );

        subscriptions.subscribe('/wt/orders', (_, _) {});
        async.flushMicrotasks();
        async.elapse(const Duration(minutes: 5));

        expect(connects, 1);
        expect(errors, hasLength(1));
        expect(errors.single.$1, isA<TransportUnavailable>());
        expect(errors.single.$2, 'stream connect /wt/orders');
      });
    });

    test(
      'uses the next factory when the preferred transport is unavailable',
      () {
        fakeAsync((async) {
          final sockets = FakeSockets();
          final seen = <Object?>[];

          final subscriptions = SubscriptionManager(
            connect: fallbackConnection([
              (context) async =>
                  throw TransportUnavailable(context.endpoint, 'webtransport'),
              sockets.connect,
            ]),
          );

          subscriptions.subscribe(
            '/live/orders',
            (message, _) => seen.add(message),
          );
          async.flushMicrotasks();

          expect(sockets.opened, hasLength(1));
          sockets.last().deliver({'type': 'order.created'});
          async.flushMicrotasks();
          expect(seen, hasLength(1));
        });
      },
    );

    test('lists the message names bound on one channel, once each', () {
      const bindings = <StreamBinding>[
        EntityStreamBinding(
          channel: '/ws/orders',
          message: 'order.created',
          entity: 'Order',
          intent: StreamIntent.upsert,
        ),
        EntityStreamBinding(
          channel: '/ws/orders',
          message: 'order.created',
          entity: 'Order',
          intent: StreamIntent.upsert,
        ),
        EntityStreamBinding(
          channel: '/ws/orders',
          message: 'order.deleted',
          entity: 'Order',
          intent: StreamIntent.evict,
        ),
        EntityStreamBinding(
          channel: '/ws/customers',
          message: 'customer.updated',
          entity: 'Customer',
          intent: StreamIntent.patch,
        ),
        DuplexStreamBinding(
          channel: '/ws/orders',
          send: 'Ask',
          receive: 'Answer',
        ),
      ];

      expect(channelMessages(bindings, '/ws/orders'), [
        'order.created',
        'order.deleted',
      ]);
    });

    test(
      'computes the backoff the TS schedule computes at jitter one half',
      () {
        const policy = BackoffPolicy(
          initial: Duration(seconds: 1),
          max: Duration(seconds: 8),
          jitter: 0.5,
        );

        expect(policy.delay(0, 0), const Duration(milliseconds: 500));
        expect(policy.delay(3, 0), const Duration(milliseconds: 4000));
        expect(policy.delay(9, 0), const Duration(milliseconds: 4000));
        expect(policy.delay(0, 1), const Duration(milliseconds: 1000));
        expect(policy.delay(64, 0.5), const Duration(milliseconds: 6000));
      },
    );

    test('resolves the connect url against a base url', () {
      fakeAsync((async) {
        final sockets = FakeSockets();
        final subscriptions = SubscriptionManager(
          connect: sockets.connect,
          baseUrl: Uri.parse('https://api.example.test/v1/'),
          headers: () => {'authorization': 'Bearer t'},
          principal: () => 'u-1',
        );

        subscriptions.subscribe('/ws/orders', (_, _) {});

        final context = sockets.last().context;
        expect(context.url, Uri.parse('https://api.example.test/v1/ws/orders'));
        expect(context.endpoint, '/ws/orders');
        expect(context.headers, {'authorization': 'Bearer t'});
        expect(context.principal, 'u-1');
        expect(context.attempt, 0);
      });
    });
  });
}
