import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/harness.dart';
import 'support/schema.dart';
import 'support/fake_sockets.dart';

// The streaming extension's envelope, against the manifest a real channel
// generates. The frames are the JSON shape of internal.Message in
// extensions/streaming/internal/streaming.go; the bindings are what
// writeStreams in the TS generator emits.

const _streams = <StreamBinding>[
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

/// One internal.Message, marshalled. Nothing is trimmed for the test.
Map<String, Object?> _frame(
  String event,
  Object? data, [
  Map<String, Object?> overrides = const {},
]) => {
  'id': 'msg-1',
  'type': 'message',
  'event': event,
  'channel_id': 'orders',
  'user_id': 'u-1',
  'data': data,
  'timestamp': '2026-08-08T10:00:00Z',
  ...overrides,
};

Matcher _decoded(String message, Object? payload, [String? channel]) =>
    isA<DecodedFrame>()
        .having((frame) => frame.message, 'message', message)
        .having((frame) => frame.payload, 'payload', payload)
        .having((frame) => frame.channel, 'channel', channel);

typedef _Kit = ({
  QueryCache cache,
  FakeSockets sockets,
  ManualCommitScheduler frames,
  List<(String, String)> unknown,
});

/// Mount a live query so the orders socket is open and frames are accepted.
_Kit _connect(FakeAsync async, FrameDecoder decode) {
  final sockets = FakeSockets();
  final frames = ManualCommitScheduler();
  final unknown = <(String, String)>[];
  final cache = QueryCache(
    transport: FakeTransport(
      (_, _) => [
        {'id': 7, 'total': 99},
      ],
    ),
    entities: schema,
    scheduler: ManualScheduler(),
  );
  final manager = SubscriptionManager(
    connect: sockets.connect,
    random: () => 0,
    release: ManualScheduler(),
    principal: () => cache.principal,
  );
  final binder = StreamBinder(
    cache: cache,
    streams: _streams,
    manager: manager,
    decode: decode,
    scheduler: frames,
    onUnknown: (message, channel) => unknown.add((message, channel)),
  );

  cache.watch(orderList, TagContext.empty).listen((_) {});
  binder.subscribe(orderList);
  async.flushMicrotasks();

  return (cache: cache, sockets: sockets, frames: frames, unknown: unknown);
}

void _deliver(_Kit kit, FakeAsync async, Object? message) {
  kit.sockets.last().deliver(message);
  async.flushMicrotasks();
}

void main() {
  group('the Forge streaming envelope', () {
    test('is now readable by the default decoder', () {
      fakeAsync((async) {
        final kit = _connect(async, decodeFrame);

        _deliver(kit, async, _frame('order.created', {'id': 9, 'total': 5}));
        kit.frames.flush();

        expect(kit.unknown, isEmpty);
        expect(kit.cache.store.getRecord('Order:9')?.data, {
          'id': 9,
          'total': 5,
        });
      });
    });

    test('still reports the extension’s transport frames, which the streaming decoder does not', () {
      fakeAsync((async) {
        final kit = _connect(async, decodeFrame);

        _deliver(kit, async, {
          'id': 'm',
          'type': 'presence',
          'user_id': 'u-1',
          'data': null,
        });
        kit.frames.flush();

        expect(kit.unknown, [('presence', '/ws/orders')]);
      });
    });

    test('decodes to the binding the manifest declares, and applies it', () {
      fakeAsync((async) {
        final kit = _connect(async, forgeStreamingDecoder());

        _deliver(kit, async, _frame('order.created', {'id': 9, 'total': 5}));
        kit.frames.flush();

        expect(kit.unknown, isEmpty);
        expect(kit.cache.store.getRecord('Order:9')?.data, {
          'id': 9,
          'total': 5,
        });
      });
    });

    test('takes the payload from `data`', () {
      fakeAsync((async) {
        final kit = _connect(async, forgeStreamingDecoder());

        _deliver(kit, async, _frame('order.updated', {'id': 7, 'total': 42}));
        kit.frames.flush();

        final record = kit.cache.store.getRecord('Order:7')?.data;

        expect(record, {'id': 7, 'total': 42});
        expect(record?['user_id'], isNull);
      });
    });

    test('drops transport frames without reporting them as unknown', () {
      fakeAsync((async) {
        final kit = _connect(async, forgeStreamingDecoder());

        for (final kind in [
          'presence',
          'typing',
          'join',
          'leave',
          'system',
          'error',
          'message',
        ]) {
          kit.sockets.last().deliver({
            'id': 'm',
            'type': kind,
            'user_id': 'u-1',
            'data': null,
            'timestamp': 'now',
          });
        }
        async.flushMicrotasks();

        kit.frames.flush();

        expect(kit.unknown, isEmpty);
      });
    });

    test('honours an event name even when the transport kind is reserved', () {
      fakeAsync((async) {
        final kit = _connect(async, forgeStreamingDecoder());

        _deliver(
          kit,
          async,
          _frame('order.created', {'id': 11}, {'type': 'system'}),
        );
        kit.frames.flush();

        expect(kit.cache.store.getRecord('Order:11')?.data, {'id': 11});
      });
    });

    test('still reads the plain `type`/`payload` shape', () {
      fakeAsync((async) {
        final kit = _connect(async, forgeStreamingDecoder());

        _deliver(kit, async, {
          'type': 'order.created',
          'payload': {'id': 13},
        });
        kit.frames.flush();

        expect(kit.unknown, isEmpty);
        expect(kit.cache.store.getRecord('Order:13')?.data, {'id': 13});
      });
    });

    test(
      'ignores an envelope that is not an object, and one with no name at all',
      () {
        final decode = forgeStreamingDecoder();

        expect(decode(null), isNull);
        expect(decode('order.created'), isNull);
        expect(
          decode({
            'data': {'id': 1},
          }),
          isNull,
        );
        expect(
          decode({'type': '', 'event': '', 'data': <String, Object?>{}}),
          isNull,
        );
      },
    );
  });

  group('channel resolution', () {
    test('leaves `channel_id` out of the decoded frame by default', () {
      final decoded = forgeStreamingDecoder()(
        _frame('order.created', {'id': 9}),
      );

      expect(decoded, _decoded('order.created', {'id': 9}));
    });

    test('applies a mapping when one is supplied', () {
      final decode = forgeStreamingDecoder(
        channelOf: (id) => id == 'orders' ? '/ws/orders' : null,
      );

      expect(decode(_frame('order.created', {'id': 9}))?.channel, '/ws/orders');
    });

    test('falls through to the arrival channel for an unmapped id', () {
      fakeAsync((async) {
        final kit = _connect(
          async,
          forgeStreamingDecoder(channelOf: (_) => null),
        );

        _deliver(
          kit,
          async,
          _frame(
            'order.created',
            {'id': 9, 'total': 5},
            {'channel_id': 'unmapped'},
          ),
        );
        kit.frames.flush();

        expect(kit.unknown, isEmpty);
        expect(kit.cache.store.getRecord('Order:9')?.data, {
          'id': 9,
          'total': 5,
        });
      });
    });

    test('reports a mapped channel that binds nothing', () {
      fakeAsync((async) {
        final kit = _connect(
          async,
          forgeStreamingDecoder(channelOf: (_) => '/ws/customers'),
        );

        _deliver(kit, async, _frame('order.created', {'id': 9}));
        kit.frames.flush();

        expect(kit.unknown, [('order.created', '/ws/customers')]);
      });
    });

    test('never asks the mapping about an empty channel_id', () {
      final asked = <String>[];
      final decode = forgeStreamingDecoder(
        channelOf: (id) {
          asked.add(id);

          return '/ws/wrong';
        },
      );

      final decoded = decode({
        'type': 'message',
        'event': 'order.created',
        'channel_id': '',
        'channel': '/ws/orders',
        'data': {'id': 9},
      });

      expect(asked, isEmpty);
      expect(decoded?.channel, '/ws/orders');
    });

    test('surfaces a literal channel with no mapping configured', () {
      final decoded = forgeStreamingDecoder()({
        'type': 'order.created',
        'channel': '/ws/orders',
        'payload': {'id': 9},
      });

      expect(decoded, _decoded('order.created', {'id': 9}, '/ws/orders'));
    });

    test(
      'surfaces channel but not channel_id when both are present and unmapped',
      () {
        final decoded = forgeStreamingDecoder()(
          _frame('order.created', {'id': 9}, {'channel': '/ws/orders'}),
        );

        expect(decoded?.channel, '/ws/orders');
      },
    );

    test('does not route a literal channel through the mapping', () {
      final asked = <String>[];
      final decode = forgeStreamingDecoder(
        channelOf: (id) {
          asked.add(id);

          return null;
        },
      );

      final decoded = decode({
        'type': 'message',
        'event': 'order.created',
        'channel': '/ws/orders',
        'data': {'id': 9},
      });

      expect(decoded?.channel, '/ws/orders');
      expect(asked, isEmpty);
    });

    test('prefers a mapped channel_id over a literal channel', () {
      final decode = forgeStreamingDecoder(
        channelOf: (id) => id == 'orders' ? '/ws/mapped' : null,
      );

      final decoded = decode(
        _frame('order.created', {'id': 9}, {'channel': '/ws/orders'}),
      );

      expect(decoded?.channel, '/ws/mapped');
    });
  });

  group('the default decoder’s name resolution', () {
    // These two ported cases are the only ones live_test.dart's decodeFrame
    // group no longer asserts; it keeps the cases streaming.test.ts lacks.
    test('falls through an unusable event to type', () {
      expect(
        decodeFrame({
          'type': 'order.created',
          'event': '',
          'payload': {'id': 9},
        }),
        _decoded('order.created', {'id': 9}),
      );

      expect(
        decodeFrame({
          'type': 'order.created',
          'event': 7,
          'payload': {'id': 9},
        }),
        _decoded('order.created', {'id': 9}),
      );

      expect(
        decodeFrame({
          'type': '',
          'name': 'order.created',
          'payload': {'id': 9},
        }),
        _decoded('order.created', {'id': 9}),
      );
    });

    test('still has nothing to decode when no candidate is usable', () {
      expect(
        decodeFrame({
          'event': '',
          'type': '',
          'name': 42,
          'payload': <String, Object?>{},
        }),
        isNull,
      );
    });
  });

  // Dart-only. The TS suite has no case for the connections stamping `''` on a
  // frame before the server assigns an id, or for a reserved kind reached with
  // an event that is present but unusable.
  group('envelope shapes the connections produce', () {
    test('decodes a frame whose id is empty, null or absent', () {
      fakeAsync((async) {
        final kit = _connect(async, forgeStreamingDecoder());

        _deliver(kit, async, _frame('order.created', {'id': 21}, {'id': ''}));
        _deliver(kit, async, _frame('order.created', {'id': 22}, {'id': null}));
        _deliver(
          kit,
          async,
          Map<String, Object?>.of(_frame('order.created', {'id': 23}))
            ..remove('id'),
        );
        kit.frames.flush();

        expect(kit.unknown, isEmpty);

        for (final id in [21, 22, 23]) {
          expect(kit.cache.store.getRecord('Order:$id')?.data, {'id': id});
        }
      });
    });

    test(
      'names the frame by event when type is also a usable, unreserved name',
      () {
        final decoded = forgeStreamingDecoder()({
          'type': 'order.created',
          'event': 'order.updated',
          'data': {'id': 9},
        });

        expect(decoded, _decoded('order.updated', {'id': 9}));
      },
    );

    test('prefers payload over data, and makes the envelope its own payload when it has neither', () {
      final decode = forgeStreamingDecoder();

      expect(
        decode({'type': 'order.created', 'payload': 1, 'data': 2}),
        _decoded('order.created', 1),
      );

      const flat = {'type': 'forge.resumed', 'from': 'e-1'};

      expect(decode(flat), _decoded('forge.resumed', flat));
    });

    test('keeps a payload that is present and null', () {
      expect(
        forgeStreamingDecoder()({'event': 'order.deleted', 'data': null}),
        _decoded('order.deleted', null),
      );
    });

    test('drops a reserved kind whose event is empty or not a string', () {
      final decode = forgeStreamingDecoder();

      expect(decode({'type': 'presence', 'event': '', 'data': null}), isNull);
      expect(decode({'type': 'system', 'event': 7, 'data': null}), isNull);
      expect(decode({'type': 'typing', 'event': null, 'data': null}), isNull);
    });

    test(
      'does not ask the mapping about a channel_id that is not a string',
      () {
        final asked = <String>[];
        final decode = forgeStreamingDecoder(
          channelOf: (id) {
            asked.add(id);

            return '/ws/wrong';
          },
        );

        final decoded = decode({
          'type': 'order.created',
          'channel_id': 7,
          'channel': '/ws/orders',
          'payload': {'id': 9},
        });

        expect(asked, isEmpty);
        expect(decoded?.channel, '/ws/orders');
      },
    );

    test('treats an empty mapped channel as no mapping', () {
      final decode = forgeStreamingDecoder(channelOf: (_) => '');

      final decoded = decode(
        _frame('order.created', {'id': 9}, {'channel': '/ws/orders'}),
      );

      expect(decoded?.channel, '/ws/orders');
    });
  });
}
