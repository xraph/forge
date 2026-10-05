import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/fake_sockets.dart';

const _streams = <StreamBinding>[
  DuplexStreamBinding(
    channel: '/api/v1/query/live/ws',
    send: 'SendMessage',
    receive: 'ReceiveMessage',
  ),
  EntityStreamBinding(
    channel: '/ws/orders',
    message: 'orderUpdated',
    entity: 'Order',
    intent: StreamIntent.upsert,
  ),
];

final class _Immediately implements CommitScheduler {
  const _Immediately();

  @override
  void schedule(void Function() commit) => commit();
}

({StreamBinder binder, QueryCache cache, FakeSockets sockets}) _build() {
  final sockets = FakeSockets(autoOpen: false);
  final cache = QueryCache(
    transport: FakeTransport((_, _) => <Object?>[]),
    entities: const {'Order': EntityMeta(idField: 'id')},
  );
  final manager = SubscriptionManager(
    connect: sockets.connect,
    random: () => 0,
    release: ManualScheduler(),
  );
  final binder = StreamBinder(
    cache: cache,
    streams: _streams,
    manager: manager,
    scheduler: const _Immediately(),
  );

  return (binder: binder, cache: cache, sockets: sockets);
}

void main() {
  group('a duplex channel', () {
    test('subscribes raw with a hello and hands frames over undecoded', () {
      fakeAsync((async) {
        final kit = _build();
        final seen = <Object?>[];

        final release = kit.binder.raw(
          '/api/v1/query/live/ws',
          (message, _) => seen.add(message),
          const SubscribeOptions(
            hello: {
              'action': 'subscribe',
              'data': {'id': 'q1'},
            },
            goodbye: {
              'action': 'unsubscribe',
              'data': {'id': 'q1'},
            },
          ),
        );
        kit.sockets.last().open();
        async.flushMicrotasks();
        kit.sockets.last().deliver({
          'type': 'snapshot',
          'subscriptionId': 'q1',
          'payload': {'rows': <Object?>[]},
        });
        async.flushMicrotasks();

        expect(kit.sockets.last().sent, [
          {
            'action': 'subscribe',
            'data': {'id': 'q1'},
          },
        ]);
        expect(seen, [
          {
            'type': 'snapshot',
            'subscriptionId': 'q1',
            'payload': {'rows': <Object?>[]},
          },
        ]);

        release();
        expect(kit.sockets.last().sent, [
          {
            'action': 'subscribe',
            'data': {'id': 'q1'},
          },
          {
            'action': 'unsubscribe',
            'data': {'id': 'q1'},
          },
        ]);
      });
    });

    test('is reachable through the LiveBinding the cache exposes', () {
      fakeAsync((async) {
        final kit = _build();
        final live = kit.cache.live;

        if (live == null) fail('the binder did not attach itself to the cache');

        final seen = <(Object?, String)>[];
        final release = live.raw(
          '/api/v1/query/live/ws',
          (message, channel) => seen.add((message, channel)),
          const SubscribeOptions(hello: {'action': 'subscribe'}),
        );
        kit.sockets.last().open();
        async.flushMicrotasks();

        expect(kit.sockets.last().sent, [
          {'action': 'subscribe'},
        ]);

        kit.sockets.last().deliver({'type': 'update', 'id': 'q1'});
        async.flushMicrotasks();

        expect(seen, hasLength(1));
        expect(seen.single.$1, {'type': 'update', 'id': 'q1'});
        expect(seen.single.$2, '/api/v1/query/live/ws');
        release();
      });
    });

    test('refuses a channel that is not a duplex binding', () {
      final kit = _build();
      final refusal = throwsA(
        isA<StateError>().having(
          (e) => e.message,
          'message',
          contains('not a duplex channel'),
        ),
      );

      expect(() => kit.binder.raw('/ws/orders', (_, _) {}), refusal);
      expect(() => kit.binder.raw('/ws/nowhere', (_, _) {}), refusal);
    });

    test('keeps duplex frames out of the entity store', () {
      fakeAsync((async) {
        final kit = _build();

        kit.binder.raw(
          '/api/v1/query/live/ws',
          (_, _) {},
          const SubscribeOptions(hello: {'id': 'q1'}),
        );
        kit.sockets.last().open();
        async.flushMicrotasks();
        kit.sockets.last().deliver({
          'type': 'orderUpdated',
          'payload': {'id': 'o1', 'total': 3},
        });
        async.flushMicrotasks();

        expect(binderSnapshot(kit.binder).queued, 0);
        expect(kit.cache.store.has('Order:o1'), isFalse);
      });
    });

    test('lists the duplex binding in the snapshot', () {
      final kit = _build();

      final channels = binderSnapshot(
        kit.binder,
      ).channels.map((c) => c.channel).toList()..sort();
      expect(channels, ['/api/v1/query/live/ws', '/ws/orders']);
    });
  });
}
