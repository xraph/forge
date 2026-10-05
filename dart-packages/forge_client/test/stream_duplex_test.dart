import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/fake_sockets.dart';

typedef _Kit = ({
  SubscriptionManager subscriptions,
  FakeSockets sockets,
  ManualScheduler release,
  List<(Object, String)> errors,
});

_Kit _build({
  int? attempts = 10,
  bool autoOpen = false,
  bool receiveOnly = false,
}) {
  final sockets = FakeSockets(autoOpen: autoOpen, receiveOnly: receiveOnly);
  final release = ManualScheduler();
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
    release: release,
    onError: (error, context) => errors.add((error, context)),
  );

  return (
    subscriptions: subscriptions,
    sockets: sockets,
    release: release,
    errors: errors,
  );
}

void main() {
  group('a subscription that speaks first', () {
    // Guards the contract live query depends on: the server learns what the
    // client wants from a frame, and it must arrive once the socket is open.
    test('sends hello after the transport opens, not before', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(
            hello: {
              'action': 'subscribe',
              'data': {'id': 'q1'},
            },
          ),
        );

        expect(kit.sockets.last().sent, isEmpty);
        kit.sockets.last().open();
        async.flushMicrotasks();
        expect(kit.sockets.last().sent, [
          {
            'action': 'subscribe',
            'data': {'id': 'q1'},
          },
        ]);
      });
    });

    test('sends hello immediately when the socket is already open', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe('/ws/live', (_, _) {});
        kit.sockets.last().open();
        async.flushMicrotasks();
        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          SubscribeOptions(
            hello: () => {
              'action': 'subscribe',
              'data': {'id': 'q2'},
            },
          ),
        );

        expect(kit.sockets.last().sent, [
          {
            'action': 'subscribe',
            'data': {'id': 'q2'},
          },
        ]);
      });
    });

    // Guards recovery: a reconnected socket is a fresh server-side session, so
    // every open subscription must reintroduce itself, in the order it was made.
    test('resends every hello after a reconnect, in subscription order', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'first'}),
        );
        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'second'}),
        );
        kit.sockets.last().open();
        async.flushMicrotasks();
        kit.sockets.last().drop('gone');
        async.flushMicrotasks();
        async.elapse(const Duration(milliseconds: 1000));
        kit.sockets.last().open();
        async.flushMicrotasks();

        expect(kit.sockets.opened, hasLength(2));
        expect(kit.sockets.last().sent, [
          {'id': 'first'},
          {'id': 'second'},
        ]);
      });
    });

    test('sends goodbye on release only while connected', () {
      fakeAsync((async) {
        final kit = _build();

        final first = kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'a'}, goodbye: {'bye': 'a'}),
        );
        final second = kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'b'}, goodbye: {'bye': 'b'}),
        );
        kit.sockets.last().open();
        async.flushMicrotasks();
        first();
        expect(kit.sockets.last().sent, [
          {'id': 'a'},
          {'id': 'b'},
          {'bye': 'a'},
        ]);

        kit.sockets.last().drop('gone');
        async.flushMicrotasks();
        second();
        expect(kit.sockets.last().sent, [
          {'id': 'a'},
          {'id': 'b'},
          {'bye': 'a'},
        ]);
      });
    });

    // Guards the failure mode being fixed: a frame nobody can send must not
    // vanish quietly. Dart opens asynchronously, so the first subscriber is
    // refused through onError when the connection arrives, and any later
    // subscriber that speaks is refused outright.
    test('refuses hello on a transport that cannot send', () {
      fakeAsync((async) {
        final kit = _build(autoOpen: true, receiveOnly: true);

        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'x'}),
        );
        async.flushMicrotasks();

        expect(kit.errors.map((entry) => '${entry.$1}'), [
          contains('cannot send'),
        ]);
        expect(kit.errors.single.$2, 'stream send /ws/live');
        expect(
          () => kit.subscriptions.subscribe(
            '/ws/live',
            (_, _) {},
            const SubscribeOptions(hello: {'id': 'y'}),
          ),
          throwsA(
            isA<StateError>().having(
              (e) => e.message,
              'message',
              contains('cannot send'),
            ),
          ),
        );
      });
    });

    test('never gives up when attempts is Infinity', () {
      fakeAsync((async) {
        final kit = _build(attempts: null);

        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'q'}),
        );
        for (var i = 0; i < 40; i++) {
          kit.sockets.last().drop('gone');
          async.flushMicrotasks();
          async.elapse(const Duration(milliseconds: 8000));
        }

        expect(kit.sockets.opened, hasLength(41));
        expect(
          kit.errors.where((entry) => '${entry.$1}'.contains('gave up')),
          isEmpty,
        );
      });
    });

    test('reports which channels carry a hello in the snapshot', () {
      fakeAsync((async) {
        final kit = _build();

        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'q'}),
        );
        kit.subscriptions.subscribe('/ws/orders', (_, _) {});

        final socket = socketSnapshot(kit.subscriptions)
            .singleWhere((s) => s.endpoint == '/ws/live');
        expect(socket.channels, [
          (channel: '/ws/live', handlers: 1, hello: true),
        ]);
      });
    });

    // Guards against the greet-then-say double send: a transport that is open
    // the moment connect completes is greeted once, by the open itself.
    test('sends hello exactly once on a transport with no onOpen', () {
      fakeAsync((async) {
        final kit = _build(autoOpen: true);

        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'once'}),
        );
        async.flushMicrotasks();

        expect(kit.sockets.last().sent, [
          {'id': 'once'},
        ]);
      });
    });

    // Guards the ordering StreamBinder's recovery depends on: a consumer that
    // reacts to onReconnect by refetching must see the reintroduction go out
    // first.
    test(
      'reports onReconnect only after the reconnected socket has been greeted',
      () {
        fakeAsync((async) {
          final kit = _build();
          List<Object?>? sentAtReconnect;
          var reconnectCalls = 0;

          kit.subscriptions.onReconnect = (_, _) {
            reconnectCalls++;
            sentAtReconnect = [...kit.sockets.last().sent];
          };

          kit.subscriptions.subscribe(
            '/ws/live',
            (_, _) {},
            const SubscribeOptions(hello: {'id': 'again'}),
          );
          kit.sockets.last().open();
          async.flushMicrotasks();
          kit.sockets.last().drop('gone');
          async.flushMicrotasks();
          async.elapse(const Duration(milliseconds: 1000));

          // The new connection exists but has not reported open yet.
          expect(reconnectCalls, 0);

          kit.sockets.last().open();
          async.flushMicrotasks();

          expect(reconnectCalls, 1);
          expect(sentAtReconnect, [
            {'id': 'again'},
          ]);
        });
      },
    );

    test('refuses goodbye on a transport that cannot send', () {
      fakeAsync((async) {
        final kit = _build(autoOpen: true, receiveOnly: true);

        kit.subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(goodbye: {'id': 'x'}),
        );
        async.flushMicrotasks();

        expect(kit.errors.map((entry) => '${entry.$1}'), [
          contains('cannot send'),
        ]);
        expect(
          () => kit.subscriptions.subscribe(
            '/ws/live',
            (_, _) {},
            const SubscribeOptions(goodbye: {'id': 'y'}),
          ),
          throwsA(isA<StateError>()),
        );
      });
    });

    // An identity change is a reconnect too, and the replacement socket must
    // carry the hello forward and greet before onReconnect is reported on it.
    test('resends hello and reports onReconnect only after a repartition reopens, in that order', () {
      fakeAsync((async) {
        var principal = 'user-a';
        final sockets = FakeSockets(autoOpen: false);
        final subscriptions = SubscriptionManager(
          connect: sockets.connect,
          principal: () => principal,
        );
        var reconnectCalls = 0;
        List<Object?>? sentAtReconnect;

        subscriptions.onReconnect = (_, _) {
          reconnectCalls++;
          sentAtReconnect = [...sockets.last().sent];
        };

        subscriptions.subscribe(
          '/ws/live',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'again'}),
        );
        sockets.last().open();
        async.flushMicrotasks();

        principal = 'user-b';
        subscriptions.repartition();

        expect(sockets.last().sent, isEmpty);
        expect(reconnectCalls, 0);

        sockets.last().open();
        async.flushMicrotasks();

        expect(sockets.last().sent, [
          {'id': 'again'},
        ]);
        expect(reconnectCalls, 1);
        expect(sentAtReconnect, [
          {'id': 'again'},
        ]);
      });
    });
  });
}
