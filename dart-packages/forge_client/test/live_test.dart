import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/harness.dart';
import 'support/schema.dart';
import 'support/fake_sockets.dart';

const _created = EntityStreamBinding(
  channel: '/ws/orders',
  message: 'order.created',
  entity: 'Order',
  intent: StreamIntent.upsert,
  invalidates: ['Order[]'],
);

const _updated = EntityStreamBinding(
  channel: '/ws/orders',
  message: 'order.updated',
  entity: 'Order',
  intent: StreamIntent.patch,
);

const _deleted = EntityStreamBinding(
  channel: '/ws/orders',
  message: 'order.deleted',
  entity: 'Order',
  intent: StreamIntent.evict,
  invalidates: ['Order[]'],
);

/// What the generated manifest's `streams` table looks like for one channel.
const _streams = <StreamBinding>[_created, _updated, _deleted];

typedef _Harness = ({
  QueryCache cache,
  SubscriptionManager manager,
  StreamBinder binder,
  FakeTransport transport,
  FakeSockets sockets,
  ManualScheduler batches,
  ManualCommitScheduler frames,
  ManualScheduler release,
  List<(String, String)> unknown,
});

_Harness _harness(
  FutureOr<Object?> Function(TransportRequest request, int call) handler, {
  List<StreamBinding> bindings = _streams,
  Sleep? sleep,
  Duration resumeGrace = const Duration(seconds: 1),
  String Function(String channel)? endpointOf,
}) {
  final transport = FakeTransport(handler);
  final batches = ManualScheduler();
  final frames = ManualCommitScheduler();
  final release = ManualScheduler();
  final sockets = FakeSockets();
  final unknown = <(String, String)>[];

  final cache = QueryCache(
    transport: transport,
    entities: schema,
    scheduler: batches,
  );
  final manager = SubscriptionManager(
    connect: sockets.connect,
    random: () => 0,
    backoff: const BackoffPolicy(initial: Duration(seconds: 1), jitter: 0.5),
    release: release,
    principal: () => cache.principal,
    endpointOf: endpointOf,
  );
  final binder = StreamBinder(
    cache: cache,
    streams: bindings,
    manager: manager,
    scheduler: frames,
    onUnknown: (message, channel) => unknown.add((message, channel)),
    sleep: sleep ?? realSleep,
    resumeGrace: resumeGrace,
  );

  return (
    cache: cache,
    manager: manager,
    binder: binder,
    transport: transport,
    sockets: sockets,
    batches: batches,
    frames: frames,
    release: release,
    unknown: unknown,
  );
}

void _watch(
  _Harness h, [
  OperationMeta meta = orderList,
  TagContext args = TagContext.empty,
  void Function()? onChange,
]) {
  h.cache.watch(meta, args).listen((_) => onChange?.call());
}

void _deliver(
  _Harness h,
  FakeAsync async,
  Object? message, [
  String? endpoint,
]) {
  h.sockets.last(endpoint).deliver(message);
  async.flushMicrotasks();
}

Object? _data(
  _Harness h, [
  OperationMeta meta = orderList,
  TagContext args = TagContext.empty,
]) => h.cache.getState(meta, args).dataOrNull;

Object? _field(_Harness h, String key, String field) =>
    h.cache.store.getRecord(key)?.data[field];

/// Drop the orders socket and let it come back, past nothing else.
void _reconnect(_Harness h, FakeAsync async, [String? endpoint]) {
  h.sockets.last(endpoint).drop();
  async.elapse(const Duration(milliseconds: 1000));
  h.sockets.last(endpoint).open();
  async.flushMicrotasks();
}

final class _RenameCodec implements WireCodec {
  const _RenameCodec();

  @override
  Object? decode(Object? wire) {
    final rest = Map<String, Object?>.of(wire! as Map<String, Object?>);
    final customerId = rest.remove('customer_id');

    return {...rest, 'customerId': customerId};
  }

  @override
  Object? encode(Object? client) => client;
}

final class _ThrowingCodec implements WireCodec {
  const _ThrowingCodec();

  @override
  Object? decode(Object? wire) => throw StateError('not for identities');

  @override
  Object? encode(Object? client) => client;
}

void main() {
  group('intents', () {
    test(
      'upserts a created entity and invalidates what the binding declares',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, call) => call == 0
                ? [
                    {'id': 7, 'total': 99},
                  ]
                : [
                    {'id': 9, 'total': 5},
                    {'id': 7, 'total': 99},
                  ],
          );

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          _deliver(h, async, {
            'type': 'order.created',
            'payload': {'id': 9, 'total': 5},
          });
          h.frames.flush();

          // The entity is in the store before anything reaches the network.
          expect(h.cache.store.getRecord('Order:9')?.data, {
            'id': 9,
            'total': 5,
          });

          h.batches.flush();
          async.flushMicrotasks();

          expect(h.transport.calls, hasLength(2));
          expect(_data(h), [
            {'id': 9, 'total': 5},
            {'id': 7, 'total': 99},
          ]);
        });
      },
    );

    test('patches an entity with no request at all', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );
        var renders = 0;

        _watch(h, orderList, TagContext.empty, () => renders++);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        final before = renders;
        final customerBefore =
            ((_data(h)! as List<Object?>).first!
                as Map<String, Object?>)['customer'];

        _deliver(h, async, {
          'type': 'order.updated',
          'payload': {'id': 7, 'total': 100},
        });
        h.frames.flush();
        h.batches.flush();
        async.flushMicrotasks();

        expect(renders, greaterThan(before));
        expect(h.transport.calls, hasLength(1));
        expect(_data(h), [
          {
            'id': 7,
            'total': 100,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ]);

        // The untouched subtree kept its identity.
        expect(
          ((_data(h)! as List<Object?>).first!
              as Map<String, Object?>)['customer'],
          same(customerBefore),
        );
      });
    });

    test('evicts a deleted entity, from a record or from a bare id', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
            {'id': 8, 'total': 1},
          ],
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        expect(h.cache.store.has('Order:7'), isTrue);

        _deliver(h, async, {
          'type': 'order.deleted',
          'payload': {'id': 7},
        });
        h.frames.flush();
        expect(h.cache.store.has('Order:7'), isFalse);

        _deliver(h, async, {'type': 'order.deleted', 'payload': 8});
        h.frames.flush();
        expect(h.cache.store.has('Order:8'), isFalse);

        // An empty list, never a list of holes.
        expect(_data(h), isEmpty);
      });
    });

    test('hands a subscriber a list with no holes in it, synchronously', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
            {'id': 8, 'total': 1},
          ],
        );
        final rendered = <List<Object?>>[];

        _watch(
          h,
          orderList,
          TagContext.empty,
          () => rendered.add((_data(h) as List<Object?>?) ?? const []),
        );
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _deliver(h, async, {
          'type': 'order.deleted',
          'payload': {'id': 7},
        });
        h.frames.flush();
        async.flushMicrotasks();

        for (final value in rendered) {
          expect(value, isNot(contains(null)));
          expect(
            () => value
                .map((order) => (order! as Map<String, Object?>)['id'])
                .toList(),
            returnsNormally,
          );
        }

        expect(rendered.last, [
          {'id': 8, 'total': 1},
        ]);
      });
    });

    test(
      'refetches a list after a delete whose binding declares no invalidation',
      () {
        fakeAsync((async) {
          const silent = <StreamBinding>[
            EntityStreamBinding(
              channel: '/ws/orders',
              message: 'order.deleted',
              entity: 'Order',
              intent: StreamIntent.evict,
            ),
          ];
          final h = _harness(
            (_, call) => call == 0
                ? [
                    {'id': 7, 'total': 99},
                    {'id': 8, 'total': 1},
                  ]
                : [
                    {'id': 8, 'total': 1},
                  ],
            bindings: silent,
          );

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();
          expect(h.transport.calls, hasLength(1));

          _deliver(h, async, {
            'type': 'order.deleted',
            'payload': {'id': 7},
          });
          h.frames.flush();
          h.batches.flush();
          async.flushMicrotasks();

          expect(h.transport.calls, hasLength(2));
          expect(_data(h), [
            {'id': 8, 'total': 1},
          ]);
        });
      },
    );

    test('does not synthesize a list tag for a patch', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _deliver(h, async, {
          'type': 'order.updated',
          'payload': {'id': 7, 'total': 100},
        });
        h.frames.flush();
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(1));
      });
    });

    test('skips an evict whose payload identifies nothing', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _deliver(h, async, {
          'type': 'order.deleted',
          'payload': {'total': 99},
        });
        h.frames.flush();

        expect(h.cache.store.has('Order:7'), isTrue);
      });
    });
  });

  group('forward compatibility', () {
    test(
      'ignores a message type no binding claims, with a development warning',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, _) => [
              {'id': 7, 'total': 99},
            ],
          );

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          expect(
            () => _deliver(h, async, {
              'type': 'order.fulfilled',
              'payload': {'id': 7, 'total': 0},
            }),
            returnsNormally,
          );

          h.frames.flush();

          expect(h.unknown, [('order.fulfilled', '/ws/orders')]);
          expect(h.cache.store.getRecord('Order:7')?.data, {
            'id': 7,
            'total': 99,
          });
          expect(h.binder.pending, 0);
        });
      },
    );

    test(
      'warns rather than throwing when a manifest carries an intent it cannot act on',
      () {},
      skip:
          'StreamIntent is a closed enum in Dart, so a manifest cannot carry an '
          'intent this runtime cannot act on',
    );

    test(
      'drops a message the decoder does not recognise without warning about it',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, _) => [
              {'id': 7, 'total': 99},
            ],
          );

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          _deliver(h, async, {'ping': 1});
          h.frames.flush();

          expect(h.unknown, isEmpty);
          expect(h.binder.pending, 0);
        });
      },
    );
  });

  group('write batching', () {
    test(
      'coalesces a burst of frames into one store commit and one render',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, _) => [
              {'id': 7, 'total': 0},
            ],
          );
          var renders = 0;

          _watch(h, orderList, TagContext.empty, () => renders++);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          final before = renders;
          final commits = h.cache.store.frameVersion;

          for (var i = 1; i <= 200; i++) {
            h.sockets.last().deliver({
              'type': 'order.updated',
              'payload': {'id': 7, 'total': i},
            });
          }
          async.flushMicrotasks();

          // 200 messages, zero commits.
          expect(h.binder.pending, 200);
          expect(_field(h, 'Order:7', 'total'), 0);
          expect(renders, before);

          h.frames.flush();
          h.batches.flush();
          async.flushMicrotasks();

          expect(h.cache.store.frameVersion, commits + 1);
          expect(_field(h, 'Order:7', 'total'), 200);
          expect(renders, before + 1);
        });
      },
    );

    test('schedules exactly one flush however many frames arrive', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 0},
          ],
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        for (var i = 0; i < 50; i++) {
          h.sockets.last().deliver({
            'type': 'order.updated',
            'payload': {'id': 7, 'total': i},
          });
        }
        async.flushMicrotasks();

        expect(h.frames.scheduled, 1);

        h.frames.flush();

        _deliver(h, async, {
          'type': 'order.updated',
          'payload': {'id': 7, 'total': 999},
        });
        expect(h.frames.scheduled, 2);

        h.frames.flush();
        expect(_field(h, 'Order:7', 'total'), 999);
      });
    });
  });

  group('gap recovery', () {
    // A second channel on an entity with no edge to or from Order, so the two
    // sockets under test stay independent.
    const crossChannel = <StreamBinding>[
      ..._streams,
      EntityStreamBinding(
        channel: '/ws/widgets',
        message: 'widget.created',
        entity: 'Widget',
        intent: StreamIntent.upsert,
        invalidates: ['Widget[]'],
      ),
    ];

    List<Object?> twoRunHandler(TransportRequest _, int call) => [
      {'id': 7, 'total': call == 0 ? 99 : 4242},
    ];

    test('invalidates the channel’s tags and refetches mounted live queries on reconnect', () {
      fakeAsync((async) {
        final h = _harness(twoRunHandler);

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(1));

        h.sockets.last().drop();
        async.elapse(const Duration(milliseconds: 1000));

        expect(h.sockets.opened, hasLength(2));
        h.sockets.last().open();

        // Past the grace window: no forge.resumed arrived, so recovery runs.
        async.elapse(const Duration(milliseconds: 1000));

        h.batches.flush();
        async.flushMicrotasks();

        // One refetch: the live-query refetch and the Order[] batch converge.
        expect(h.transport.calls, hasLength(2));
        expect(_data(h), [
          {'id': 7, 'total': 4242},
        ]);
      });
    });

    test('marks a query the channel’s tags reach, even though it is not itself live', () {
      fakeAsync((async) {
        const filtered = TagContext(query: {'status': 'open'});
        final h = _harness(
          (request, _) => request.args.query.isEmpty
              ? [
                  {'id': 7, 'total': 99},
                ]
              : [
                  {'id': 8, 'total': 1},
                ],
        );

        _watch(h);
        _watch(h, orderList, filtered);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));

        _reconnect(h, async);
        async.elapse(const Duration(milliseconds: 1000));

        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(4));
        expect(
          h.transport.calls.where((call) => call.args.query.isNotEmpty),
          hasLength(2),
        );
      });
    });

    test('refetches a live query no channel tag reaches', () {
      fakeAsync((async) {
        const patchOnly = <StreamBinding>[_updated];
        const seven = TagContext(path: {'id': 7});
        final h = _harness(
          (_, call) => {'id': 7, 'total': call == 0 ? 99 : 4242},
          bindings: patchOnly,
        );

        _watch(h, orderGet, seven);
        h.binder.subscribe(orderGet, seven);
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(1));

        _reconnect(h, async);
        async.elapse(const Duration(milliseconds: 1000));

        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));
        expect(_field(h, 'Order:7', 'total'), 4242);
      });
    });

    test('recovers once for a query two components hold live', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        _watch(h);
        final a = h.binder.subscribe(orderList);
        final b = h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _reconnect(h, async);
        async.elapse(const Duration(milliseconds: 1000));

        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));

        a();
        b();
      });
    });

    test('stops recovering a query whose live subscription was released', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        _watch(h);
        final stop = h.binder.subscribe(orderList);
        async.flushMicrotasks();

        final socket = h.sockets.last();
        stop();
        h.release.flush();
        socket.drop();

        async.elapse(const Duration(seconds: 60));
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(1));
      });
    });

    test('declines to make a query live when no channel binds its entity', () {
      final h = _harness((_, _) => <Object?>[]);

      final release = h.binder.subscribe(
        const OperationMeta(
          id: 'op_invoice_list',
          method: 'GET',
          path: '/invoices',
          entity: 'Invoice',
          provides: ['Invoice[]'],
        ),
      );

      expect(h.unknown, hasLength(1));
      expect(release, returnsNormally);
    });

    test('does not refetch when the server reports a completed replay', () {
      fakeAsync((async) {
        final h = _harness(twoRunHandler);

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(1));

        _reconnect(h, async);

        _deliver(h, async, {
          'type': 'forge.resumed',
          'payload': {'from': 'e-1', 'count': 2},
        });

        // Well past the grace window: the recovery was cancelled, not delayed.
        async.elapse(const Duration(milliseconds: 5000));
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(1));
      });
    });

    // The Go server sends the SSE envelope, event/data. The payload field
    // names are the ones internal/router/streaming_sse_replay_test.go asserts.
    test(
      'does not refetch when a completed replay arrives in the SSE envelope',
      () {
        fakeAsync((async) {
          final h = _harness(twoRunHandler);

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          expect(h.transport.calls, hasLength(1));

          _reconnect(h, async);

          _deliver(h, async, {
            'event': 'forge.resumed',
            'data': {'from': 'e-1', 'count': 2},
          });

          async.elapse(const Duration(milliseconds: 5000));
          h.batches.flush();
          async.flushMicrotasks();

          expect(h.transport.calls, hasLength(1));
        });
      },
    );

    test('recovers when the grace timer rejects', () {
      fakeAsync((async) {
        final h = _harness(
          twoRunHandler,
          sleep: (_) => Future<void>.error(StateError('timer unavailable')),
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(1));

        _reconnect(h, async);

        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));
      });
    });

    test('refetches immediately when the server reports an unfillable gap', () {
      fakeAsync((async) {
        final h = _harness(twoRunHandler);

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _reconnect(h, async);

        _deliver(h, async, {
          'type': 'forge.gap',
          'payload': {'reason': 'unresumable'},
        });
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));
      });
    });

    // The fail-safe: a server that knows nothing about replay says nothing.
    test('refetches when no control event arrives', () {
      fakeAsync((async) {
        final h = _harness(twoRunHandler);

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _reconnect(h, async);

        // Nothing yet: the grace window is still open.
        expect(h.transport.calls, hasLength(1));

        async.elapse(const Duration(milliseconds: 1000));
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));
      });
    });

    test('refetches without deferral when resumeGrace is 0', () {
      fakeAsync((async) {
        final h = _harness(twoRunHandler, resumeGrace: Duration.zero);

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _reconnect(h, async);

        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));
      });
    });

    test('recovers when the resumed payload is missing or malformed', () {
      fakeAsync((async) {
        final h = _harness(twoRunHandler);

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _reconnect(h, async);

        // No payload: the bare envelope becomes its own payload, which has
        // neither from nor count.
        _deliver(h, async, {'type': 'forge.resumed'});

        async.elapse(const Duration(milliseconds: 1000));
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));
      });
    });

    test('does not let an ordinary data frame cancel a pending recovery', () {
      fakeAsync((async) {
        final h = _harness(twoRunHandler);

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _reconnect(h, async);

        _deliver(h, async, {
          'type': 'order.updated',
          'payload': {'id': 7, 'total': 500},
        });

        async.elapse(const Duration(milliseconds: 1000));
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));
      });
    });

    test('recovers every endpoint independently when more than one socket reconnects', () {
      fakeAsync((async) {
        final h = _harness(
          (request, _) => request.meta.path == '/orders'
              ? [
                  {'id': 7, 'total': 99},
                ]
              : [
                  {'id': 1, 'name': 'Gadget'},
                ],
          bindings: crossChannel,
        );

        _watch(h);
        _watch(h, widgetList);
        h.binder.subscribe(orderList);
        h.binder.subscribe(widgetList);
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(2));

        h.sockets.last('/ws/orders').drop();
        h.sockets.last('/ws/widgets').drop();
        async.elapse(const Duration(milliseconds: 1000));
        h.sockets.last('/ws/orders').open();
        h.sockets.last('/ws/widgets').open();

        async.elapse(const Duration(milliseconds: 1000));
        h.batches.flush();
        async.flushMicrotasks();

        expect(h.transport.calls, hasLength(4));
      });
    });

    test(
      'does not let a resume on one endpoint cancel recovery owed to another',
      () {
        fakeAsync((async) {
          final h = _harness(
            (request, _) => request.meta.path == '/orders'
                ? [
                    {'id': 7, 'total': 99},
                  ]
                : [
                    {'id': 1, 'name': 'Gadget'},
                  ],
            bindings: crossChannel,
          );

          _watch(h);
          _watch(h, widgetList);
          h.binder.subscribe(orderList);
          h.binder.subscribe(widgetList);
          async.flushMicrotasks();

          expect(h.transport.calls, hasLength(2));

          h.sockets.last('/ws/orders').drop();
          h.sockets.last('/ws/widgets').drop();
          async.elapse(const Duration(milliseconds: 1000));
          h.sockets.last('/ws/orders').open();
          h.sockets.last('/ws/widgets').open();

          _deliver(h, async, {
            'type': 'forge.resumed',
            'payload': {'from': 'e-1', 'count': 1},
          }, '/ws/widgets');

          async.elapse(const Duration(milliseconds: 1000));
          h.batches.flush();
          async.flushMicrotasks();

          // Orders recovered; widgets did not, its gap was filled.
          expect(h.transport.calls, hasLength(3));
        });
      },
    );
  });

  group('principal partitioning', () {
    test('never writes a frame decoded for the previous identity into the new store', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        h.cache.setPrincipal('user-a');
        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        final stale = h.sockets.last();

        // A frame is decoded and queued, and the identity changes before the
        // frame window closes.
        _deliver(h, async, {
          'type': 'order.created',
          'payload': {'id': 9, 'total': 5},
        });
        expect(h.binder.pending, 1);

        h.cache.setPrincipal('user-b');
        h.frames.flush();

        expect(h.cache.store.has('Order:9'), isFalse);
        expect(h.binder.pending, 0);

        // principalChanges is synchronous (01a decision 22), so the
        // repartition already ran; the flush lets the replacement connect.
        async.flushMicrotasks();
        expect(stale.isClosed, isTrue);
        expect(h.sockets.last().context.principal, 'user-b');

        stale.deliver({
          'type': 'order.created',
          'payload': {'id': 11, 'total': 1},
        });
        async.flushMicrotasks();
        h.frames.flush();
        expect(h.cache.store.has('Order:11'), isFalse);

        _deliver(h, async, {
          'type': 'order.created',
          'payload': {'id': 12, 'total': 2},
        });
        h.frames.flush();
        expect(h.cache.store.has('Order:12'), isTrue);
        expect(h.transport.calls, isNotEmpty);
      });
    });
  });

  group('channel resolution', () {
    const nested = <StreamBinding>[
      ..._streams,
      EntityStreamBinding(
        channel: '/ws/customers',
        message: 'customer.updated',
        entity: 'Customer',
        intent: StreamIntent.patch,
      ),
    ];

    test('resolves every channel reachable from the query result type, not just the root', () {
      final h = _harness((_, _) => <Object?>[], bindings: nested);

      expect([...h.binder.channelsFor(orderList)]..sort(), [
        '/ws/customers',
        '/ws/orders',
      ]);

      // And it terminates on a schema that is a graph.
      const customerRooted = OperationMeta(
        id: 'op_customer_rooted',
        method: 'GET',
        path: '/orders',
        entity: 'Customer',
        provides: ['Order[]'],
      );
      expect([...h.binder.channelsFor(customerRooted)]..sort(), [
        '/ws/customers',
        '/ws/orders',
      ]);
    });

    test('applies a frame on a nested entity to a query rooted elsewhere', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
          bindings: nested,
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        // Two channels, so two sockets.
        expect(h.sockets.opened, hasLength(2));

        _deliver(h, async, {
          'type': 'customer.updated',
          'payload': {'id': 'c-3', 'name': 'Grace'},
        }, '/ws/customers');
        h.frames.flush();

        expect(_field(h, 'Customer:c-3', 'name'), 'Grace');
        expect(h.transport.calls, hasLength(1));
      });
    });

    test('subscribes from the declared result type, before the query has ever settled', () {
      final h = _harness((_, _) => <Object?>[], bindings: nested);

      h.binder.subscribe(orderList);

      expect(h.sockets.opened, hasLength(2));
    });
  });

  group('the cache seam', () {
    test('registers itself on the cache, so `{live: true}` can find it', () {
      final h = _harness((_, _) => <Object?>[]);

      expect(h.cache.live, same(h.binder));

      final release = h.cache.watchLive(orderList, TagContext.empty);

      expect(h.sockets.opened, hasLength(1));

      release();
    });

    test(
      'reports rather than silently going deaf when no runtime is attached',
      () {
        final reported = <(Object, String)>[];
        final cache = QueryCache(
          transport: FakeTransport((_, _) => <Object?>[]),
          entities: schema,
          onError: (error, context) => reported.add((error, context)),
        );

        final release = cache.watchLive(orderList, TagContext.empty);

        expect(cache.live, isNull);
        expect(reported, hasLength(1));
        expect(reported.single.$2, 'live');
        expect('${reported.single.$1}', contains('no stream runtime'));
        expect(release, returnsNormally);
      },
    );

    test(
      'gives the slot up on dispose, but never one another binder has taken',
      () {
        final h = _harness((_, _) => <Object?>[]);
        final second = StreamBinder(
          cache: h.cache,
          streams: _streams,
          manager: h.manager,
        );

        expect(h.cache.live, same(second));

        h.binder.dispose();
        expect(h.cache.live, same(second));

        second.dispose();
        expect(h.cache.live, isNull);
      },
    );
  });

  group('observer event payload', () {
    test('hands the observer the batch, so a frame can be attributed to its channel', () {
      final cache = QueryCache(
        transport: FakeTransport((_, _) => <Object?>[]),
        entities: schema,
      );
      final seen = <(String, String, StreamIntent)>[];

      cache.observer = (event) {
        if (event is! FramesCommitted) return;

        for (final frame in event.frames) {
          seen.add((
            frame.binding.channel,
            frame.binding.message,
            frame.binding.intent,
          ));
        }
      };

      applyFrames(cache, [
        const StreamFrame(
          binding: EntityStreamBinding(
            channel: '/ws/orders',
            message: 'order.updated',
            entity: 'Order',
            intent: StreamIntent.upsert,
            invalidates: ['Order[]'],
          ),
          payload: {'id': 1, 'total': 99},
        ),
      ]);

      expect(seen, [('/ws/orders', 'order.updated', StreamIntent.upsert)]);

      cache.observer = null;
    });
  });

  group('binderSnapshot', () {
    test(
      'reports the manifest bindings and the mounted live queries per channel',
      () {
        fakeAsync((async) {
          final h = _harness((_, _) => <Object?>[]);
          final release = h.binder.subscribe(orderList);

          async.flushMicrotasks();

          final snap = binderSnapshot(h.binder);
          final channel = snap.channels.firstWhere(
            (entry) => entry.channel == '/ws/orders',
          );

          expect(
            channel.bindings.whereType<EntityStreamBinding>().map(
              (binding) => binding.message,
            ),
            contains('order.updated'),
          );
          expect(
            snap.live.map((entry) => entry.channel),
            contains('/ws/orders'),
          );
          expect(snap.live.first.key, queryKey(orderList, TagContext.empty));
          expect(snap.queued, 0);
          expect(snap.recovering, isEmpty);

          release();
        });
      },
    );

    test('is a copy, so a panel cannot reach the binder through it', () {
      final h = _harness((_, _) => <Object?>[]);
      final first = binderSnapshot(h.binder);
      final second = binderSnapshot(h.binder);

      expect(identical(first, second), isFalse);
      expect(identical(first.channels, second.channels), isFalse);
    });
  });

  group('frame decoding', () {
    test('decodes a frame through the binding before writing it', () {
      final cache = QueryCache(
        transport: FakeTransport((_, _) => null),
        entities: schema,
      );
      const binding = EntityStreamBinding(
        channel: '/ws/orders',
        message: 'order.created',
        entity: 'Order',
        intent: StreamIntent.upsert,
        decode: _RenameCodec(),
      );

      applyFrames(cache, [
        const StreamFrame(
          binding: binding,
          payload: {'id': 7, 'customer_id': 'c-1'},
        ),
      ]);

      expect(cache.store.getRecord('Order:7')?.data, {
        'id': 7,
        'customerId': 'c-1',
      });
    });

    test('passes a bare identity through untouched and reports a codec that throws', () {
      final errors = <String>[];
      final cache = QueryCache(
        transport: FakeTransport((_, _) => null),
        entities: schema,
        onError: (_, context) => errors.add(context),
      );

      cache.store.put('Order:7', {'id': 7});

      const evict = EntityStreamBinding(
        channel: '/ws/orders',
        message: 'order.deleted',
        entity: 'Order',
        intent: StreamIntent.evict,
        decode: _ThrowingCodec(),
      );

      applyFrames(cache, [const StreamFrame(binding: evict, payload: 7)]);
      expect(cache.store.getRecord('Order:7'), isNull);

      const upsert = EntityStreamBinding(
        channel: '/ws/orders',
        message: 'order.created',
        entity: 'Order',
        intent: StreamIntent.upsert,
        decode: _ThrowingCodec(),
      );

      applyFrames(cache, [
        const StreamFrame(binding: upsert, payload: {'id': 8}),
      ]);
      expect(cache.store.getRecord('Order:8'), isNull);
      expect(errors, ['decode']);
    });
  });

  group('Dart port', () {
    // Review Focus: channels multiplexed onto one socket are matched by
    // message name, the documented edge ported as is. A frame with no channel
    // field reaches the binding on every channel that declares the name; a
    // frame naming its channel reaches that one binding from every handler.
    // Either way the store ends right and a tag is raised once per batch.
    test('applies a frame once per channel binding when two channels on one socket share a message name', () {
      fakeAsync((async) {
        const onA = EntityStreamBinding(
          channel: '/ws/a',
          message: 'order.updated',
          entity: 'Order',
          intent: StreamIntent.patch,
        );
        const onB = EntityStreamBinding(
          channel: '/ws/b',
          message: 'order.updated',
          entity: 'Order',
          intent: StreamIntent.patch,
          invalidates: ['Order[]'],
        );
        final h = _harness(
          (_, call) => [
            {'id': 7, 'total': call == 0 ? 1 : 2},
          ],
          bindings: const [onA, onB],
          endpointOf: (_) => '/ws',
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();
        expect(h.sockets.opened, hasLength(1));

        _deliver(h, async, {
          'type': 'order.updated',
          'payload': {'id': 7, 'total': 5},
        });
        expect(h.binder.pending, 2);

        h.frames.flush();
        expect(_field(h, 'Order:7', 'total'), 5);

        h.batches.flush();
        async.flushMicrotasks();
        expect(h.transport.calls, hasLength(2));

        _deliver(h, async, {
          'type': 'order.updated',
          'channel': '/ws/a',
          'payload': {'id': 7, 'total': 9},
        });
        expect(h.binder.pending, 2);

        h.frames.flush();
        h.batches.flush();
        async.flushMicrotasks();

        expect(_field(h, 'Order:7', 'total'), 9);
        expect(h.transport.calls, hasLength(2));
      });
    });

    // One user's data must never reach the next. clear() raises no principal
    // change, so the queue survives it and only the generation captured when
    // the frame was batched stands between the frame and the emptied store.
    test('writes nothing from a batch queued before the cache was cleared', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _deliver(h, async, {
          'type': 'order.created',
          'payload': {'id': 9, 'total': 5},
        });
        expect(h.binder.pending, 1);

        h.cache.clear();
        h.frames.flush();

        expect(h.cache.store.has('Order:9'), isFalse);
        expect(h.binder.pending, 0);
      });
    });

    test(
      'commits only the frames that arrived after a clear in the same window',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, _) => [
              {'id': 7, 'total': 99},
            ],
          );

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          _deliver(h, async, {
            'type': 'order.created',
            'payload': {'id': 9, 'total': 5},
          });
          h.cache.clear();
          _deliver(h, async, {
            'type': 'order.created',
            'payload': {'id': 12, 'total': 2},
          });
          expect(h.binder.pending, 1);

          h.frames.flush();

          expect(h.cache.store.has('Order:9'), isFalse);
          expect(h.cache.store.getRecord('Order:12')?.data, {
            'id': 12,
            'total': 2,
          });
        });
      },
    );

    test('keeps the generation across a plain invalidate, so a queued batch still commits', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();

        _deliver(h, async, {
          'type': 'order.created',
          'payload': {'id': 9, 'total': 5},
        });

        final generation = h.cache.generation;
        h.cache.invalidate(['Order[]']);

        expect(h.cache.generation, generation);

        h.frames.flush();

        expect(h.cache.store.getRecord('Order:9')?.data, {'id': 9, 'total': 5});
      });
    });

    test(
      'raises no tag for a batch whose commit a listener answered by clearing',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, _) => [
              {'id': 7, 'total': 99},
            ],
          );
          final invalidated = <String>[];
          var cleared = false;

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          // Synchronous, so it runs inside applyFrames' notification, between
          // the store write and the invalidation.
          h.cache.subscribe(orderList, TagContext.empty, () {
            if (cleared || _field(h, 'Order:7', 'total') != 5) return;

            cleared = true;
            h.cache.clear();
          });
          h.cache.observer = (event) {
            if (cleared && event is QueryInvalidated) {
              invalidated.add(event.key);
            }
          };

          // order.created invalidates Order[], and changing Order:7 moves the
          // list's value, so the listener above is notified.
          _deliver(h, async, {
            'type': 'order.created',
            'payload': {'id': 7, 'total': 5},
          });
          h.frames.flush();
          expect(cleared, isTrue);

          h.batches.flush();
          async.flushMicrotasks();

          expect(invalidated, isEmpty);
        });
      },
    );

    // 01a decision 22: principalChanges is synchronous, and so is the
    // repartition the binder runs inside it. Nothing waits for a microtask.
    test(
      'repartitions inside the principal change, before any microtask runs',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, _) => [
              {'id': 7, 'total': 99},
            ],
          );

          h.cache.setPrincipal('user-a');
          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          final stale = h.sockets.last();

          _deliver(h, async, {
            'type': 'order.created',
            'payload': {'id': 9, 'total': 5},
          });

          h.cache.setPrincipal('user-b');

          expect(h.binder.pending, 0);
          expect(stale.isClosed, isTrue);
          expect(h.sockets.opened, hasLength(2));
          expect(h.sockets.last().context.principal, 'user-b');
        });
      },
    );

    test('does not let a response the frame overtook commit over it', () {
      fakeAsync((async) {
        final first = Completer<Object?>();
        final h = _harness(
          (_, call) => call == 0
              ? first.future
              : [
                  {'id': 7, 'total': 500},
                ],
        );

        _watch(h);
        h.binder.subscribe(orderList);
        async.flushMicrotasks();
        expect(h.transport.calls, hasLength(1));

        // The request is out; the frame lands first.
        _deliver(h, async, {
          'type': 'order.updated',
          'payload': {'id': 7, 'total': 500},
        });
        h.frames.flush();
        expect(_field(h, 'Order:7', 'total'), 500);

        first.complete([
          {'id': 7, 'total': 99},
        ]);
        async.flushMicrotasks();

        expect(_field(h, 'Order:7', 'total'), 500);

        h.batches.flush();
        async.flushMicrotasks();

        expect(_field(h, 'Order:7', 'total'), 500);
        expect(_data(h), [
          {'id': 7, 'total': 500},
        ]);
      });
    });

    test('holds one live entry per query and lets the channel go with its last holder', () {
      fakeAsync((async) {
        final h = _harness((_, _) => <Object?>[]);

        final a = h.binder.subscribe(orderList);
        final b = h.binder.subscribe(orderList);
        async.flushMicrotasks();

        expect(h.sockets.opened, hasLength(1));
        expect(binderSnapshot(h.binder).live.single.refs, 2);

        a();
        a();
        h.release.flush();
        async.flushMicrotasks();

        expect(binderSnapshot(h.binder).live.single.refs, 1);
        expect(h.sockets.last().isClosed, isFalse);

        b();
        expect(binderSnapshot(h.binder).live, isEmpty);

        h.release.flush();
        async.flushMicrotasks();

        expect(h.sockets.last().isClosed, isTrue);
        expect(h.manager.size, 0);
      });
    });

    // Cancelling a recovery is the one irreversible move, allowed only on a
    // well-formed forge.resumed. A release inside the grace window settles it
    // as unfilled instead, so a query the channel's tags reach still learns
    // of the gap, and no timer is left behind.
    test('settles a recovery as unfilled, without its timer, when the last live query on the endpoint is released', () {
      fakeAsync((async) {
        const filtered = TagContext(query: {'status': 'open'});
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );
        int filteredCalls() => h.transport.calls
            .where((call) => call.args.query.isNotEmpty)
            .length;

        _watch(h);
        _watch(h, orderList, filtered);
        final stop = h.binder.subscribe(orderList);
        async.flushMicrotasks();
        expect(filteredCalls(), 1);

        _reconnect(h, async);

        expect(binderSnapshot(h.binder).recovering, ['/ws/orders']);
        expect(async.pendingTimers, isNotEmpty);

        stop();

        expect(binderSnapshot(h.binder).recovering, isEmpty);
        expect(async.pendingTimers, isEmpty);

        h.batches.flush();
        async.flushMicrotasks();

        expect(filteredCalls(), 2);
      });
    });

    test(
      'cancels the grace timer when the server reports a completed replay',
      () {
        fakeAsync((async) {
          final h = _harness(
            (_, _) => [
              {'id': 7, 'total': 99},
            ],
          );

          _watch(h);
          h.binder.subscribe(orderList);
          async.flushMicrotasks();

          _reconnect(h, async);
          expect(async.pendingTimers, isNotEmpty);

          _deliver(h, async, {
            'type': 'forge.resumed',
            'payload': {'from': 'e-1', 'count': 2},
          });

          expect(async.pendingTimers, isEmpty);
        });
      },
    );

    test('releases every subscription and timer it holds on dispose', () {
      fakeAsync((async) {
        final h = _harness(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        _watch(h);
        final stop = h.binder.subscribe(orderList);
        h.binder.channel('/ws/orders');
        async.flushMicrotasks();

        _reconnect(h, async);
        _deliver(h, async, {
          'type': 'order.created',
          'payload': {'id': 9, 'total': 5},
        });

        // Two of the binder's handlers ride the channel, so two frames.
        expect(h.binder.pending, 2);
        expect(async.pendingTimers, isNotEmpty);

        h.binder.dispose();

        expect(async.pendingTimers, isEmpty);
        expect(h.binder.pending, 0);
        expect(h.cache.live, isNull);
        expect(h.manager.onReconnect, isNull);

        final snap = binderSnapshot(h.binder);
        expect(snap.live, isEmpty);
        expect(snap.recovering, isEmpty);

        h.release.flush();
        async.flushMicrotasks();

        expect(h.sockets.last().isClosed, isTrue);
        expect(h.manager.size, 0);

        // A holder releasing late is harmless, and a commit the scheduler
        // still owes writes nothing.
        expect(stop, returnsNormally);
        h.frames.flush();
        expect(h.cache.store.has('Order:9'), isFalse);

        // The identity watch is gone too: a principal change no longer
        // repartitions the manager on the binder's behalf.
        h.manager.subscribe('/ws/orders', (_, _) {});
        async.flushMicrotasks();

        final after = h.sockets.last();
        h.cache.setPrincipal('user-z');

        expect(after.isClosed, isFalse);
      });
    });
  });

  group('principal wiring', () {
    // The binder repartitions on the cache's identity, and the manager opens
    // sockets for its own principal source. When the two disagree, every
    // socket belongs to the wrong identity, so it is reported, not thrown.
    ({QueryCache cache, List<(Object, String)> reports}) wire(
      String? Function(QueryCache cache) source,
    ) {
      final reports = <(Object, String)>[];
      final cache = QueryCache(
        transport: FakeTransport((_, _) => <Object?>[]),
        entities: schema,
      );
      final manager = SubscriptionManager(
        connect: FakeSockets().connect,
        release: ManualScheduler(),
        principal: () => source(cache),
      );

      StreamBinder(
        cache: cache,
        streams: _streams,
        manager: manager,
        onError: (error, context) => reports.add((error, context)),
      );

      return (cache: cache, reports: reports);
    }

    test('reports a manager whose principal source is not the cache, on construction and on each flip', () {
      final kit = wire((_) => 'someone-else');

      expect(kit.reports.map((report) => report.$2), ['principal']);

      kit.cache.setPrincipal('user-a');
      expect(kit.reports, hasLength(2));

      kit.cache.setPrincipal('user-b');
      expect(kit.reports, hasLength(3));
      expect(kit.reports.every((report) => report.$2 == 'principal'), isTrue);
      expect('${kit.reports.last.$1}', contains('user-b'));
    });

    test('reports nothing when the manager reads the cache principal', () {
      final kit = wire((cache) => cache.principal);

      kit.cache.setPrincipal('user-a');
      kit.cache.setPrincipal('user-b');
      kit.cache.setPrincipal(null);

      expect(kit.reports, isEmpty);
    });
  });

  group('decodeFrame', () {
    void expectFrame(
      Object? message, {
      required String name,
      Object? payload,
      String? channel,
    }) {
      final decoded = decodeFrame(message);

      expect(decoded, isNotNull);
      expect(decoded!.message, name);
      expect(decoded.payload, payload);
      expect(decoded.channel, channel);
    }

    test('reads the SSE envelope, event and data', () {
      expectFrame(
        {
          'event': 'order.created',
          'data': {'id': 9},
        },
        name: 'order.created',
        payload: {'id': 9},
      );
    });

    test('reads a plain WebSocket envelope, type and payload', () {
      expectFrame(
        {
          'type': 'order.created',
          'payload': {'id': 9},
        },
        name: 'order.created',
        payload: {'id': 9},
      );
    });

    test('reads the AsyncAPI name', () {
      expectFrame(
        {'name': 'order.created', 'payload': 9},
        name: 'order.created',
        payload: 9,
      );
    });

    test('prefers payload over data when an envelope carries both', () {
      expectFrame(
        {'type': 'order.created', 'payload': 1, 'data': 2},
        name: 'order.created',
        payload: 1,
      );
    });

    test(
      'makes a message with a name and no payload field its own payload',
      () {
        const message = {'type': 'forge.resumed', 'from': 'e-1'};

        expectFrame(message, name: 'forge.resumed', payload: message);
      },
    );

    test('keeps a payload that is present and null', () {
      expectFrame({
        'type': 'order.deleted',
        'payload': null,
      }, name: 'order.deleted');
    });

    test('takes the channel the envelope names, and only a string one', () {
      expectFrame(
        {'type': 'order.created', 'channel': '/ws/orders', 'payload': 9},
        name: 'order.created',
        payload: 9,
        channel: '/ws/orders',
      );
      expectFrame(
        {'type': 'order.created', 'channel': 7, 'payload': 9},
        name: 'order.created',
        payload: 9,
      );
    });

    // The two cases of streaming.test.ts' "the default decoder's name
    // resolution", which Task 7 ports under their TS strings.
    test('falls through an unusable event or type to the next name', () {
      expectFrame(
        {
          'type': 'order.created',
          'event': '',
          'payload': {'id': 9},
        },
        name: 'order.created',
        payload: {'id': 9},
      );
      expectFrame(
        {
          'type': 'order.created',
          'event': 7,
          'payload': {'id': 9},
        },
        name: 'order.created',
        payload: {'id': 9},
      );
      expectFrame(
        {
          'type': '',
          'name': 'order.created',
          'payload': {'id': 9},
        },
        name: 'order.created',
        payload: {'id': 9},
      );
    });

    test('has nothing to decode when no name is usable or the message is not a map', () {
      expect(
        decodeFrame({
          'event': '',
          'type': '',
          'name': 42,
          'payload': <String, Object?>{},
        }),
        isNull,
      );
      expect(decodeFrame({'ping': 1}), isNull);
      expect(decodeFrame('order.created'), isNull);
      expect(decodeFrame(null), isNull);
      expect(decodeFrame(['order.created']), isNull);
    });
  });
}
