import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/seams.dart';
import 'package:test/test.dart';

/// Port of `client-devtools/__tests__/harness.ts`: a cache with no network,
/// no timers and no framework. The invalidation batch runs when `flush()`
/// says so and nothing waits on wall-clock time.
const schema = <String, EntityMeta>{
  'Order': EntityMeta(
    idField: 'id',
    fields: {'customer': 'Customer', 'items': 'LineItem'},
  ),
  'Customer': EntityMeta(idField: 'id'),
  'LineItem': EntityMeta(idField: 'sku'),
};

/// The TS harness's `ops`. `orderCreate` invalidates only `Order:{res.id}`,
/// which is the near miss this whole package exists to explain.
abstract final class Ops {
  static const orderList = OperationMeta(
    id: 'op_order_list',
    method: 'GET',
    path: '/orders',
    entity: 'Order',
    provides: ['Order[]'],
  );
  static const orderGet = OperationMeta(
    id: 'op_order_get',
    method: 'GET',
    path: '/orders/{id}',
    entity: 'Order',
    provides: ['Order:{id}'],
  );
  static const orderCreate = OperationMeta(
    id: 'op_order_create',
    method: 'POST',
    path: '/orders',
    entity: 'Order',
    invalidates: ['Order:{res.id}'],
  );
  static const orderUpdate = OperationMeta(
    id: 'op_order_update',
    method: 'PATCH',
    path: '/orders/{id}',
    entity: 'Order',
    invalidates: ['Order:{id}', 'Order[]'],
  );

  /// Declares a template that cannot resolve unless the response carries `ref`.
  static const orderArchive = OperationMeta(
    id: 'op_order_archive',
    method: 'POST',
    path: '/orders/{id}/archive',
    entity: 'Order',
    invalidates: ['Order[]:{res.ref}'],
  );

  static const all = <OperationMeta>[
    orderList,
    orderGet,
    orderCreate,
    orderUpdate,
    orderArchive,
  ];
}

/// The stream binding the frame tests apply.
const orderBinding = EntityStreamBinding(
  channel: '/ws/orders',
  message: 'order.updated',
  entity: 'Order',
  intent: StreamIntent.upsert,
  invalidates: ['Order[]'],
);

/// A clock that only moves when read: every `now()` is one more than the last.
final class CounterClock implements Clock {
  int _at = 0;

  @override
  int now() => ++_at;
}

final class _ReplyTransport implements Transport {
  _ReplyTransport(this.calls, this.replies);

  final List<TransportRequest> calls;
  final Map<String, Object?> replies;

  @override
  Future<Object?> execute(TransportRequest request) async {
    calls.add(request);
    await Future<void>.value();

    final value = replies['${request.meta.method} ${request.meta.path}'];

    // Stored errors are thrown rather than returned, which is the only way a
    // test reaches a query's error state through the real code path.
    if (value is Exception) throw value;

    // A fresh object graph per request, so identity checks in the store never
    // compare a value against itself.
    return value == null ? null : jsonDecode(jsonEncode(value));
  }
}

/// One cache, its manual scheduler, and the requests that reached the wire.
final class Harness {
  factory Harness() {
    final calls = <TransportRequest>[];
    final replies = <String, Object?>{
      'GET /orders': [
        {
          'id': 1,
          'total': 10,
          'customer': {'id': 'c1', 'name': 'Ada'},
        },
        {
          'id': 2,
          'total': 20,
          'customer': {'id': 'c1', 'name': 'Ada'},
        },
      ],
      'GET /orders/{id}': {'id': 1, 'total': 10},
      'POST /orders': {'id': 9, 'total': 30},
      // Identical to what the list holds, so a write of unchanged data bumps
      // no version.
      'PATCH /orders/{id}': {'id': 1, 'total': 10},
      'POST /orders/{id}/archive': {'id': 1, 'archived': true},
    };
    final scheduler = ManualScheduler();
    final cache = QueryCache(
      transport: _ReplyTransport(calls, replies),
      entities: schema,
      scheduler: scheduler,
    );
    return Harness._(cache, scheduler, calls, replies);
  }

  Harness._(this.cache, this.scheduler, this.calls, this._replies);

  final QueryCache cache;
  final ManualScheduler scheduler;
  final List<TransportRequest> calls;
  final Map<String, Object?> _replies;

  /// The devtools' view of [cache].
  DevCache get dev => DevCache(cache);

  /// Run the pending invalidation batch.
  void flush() => scheduler.flush();

  /// Let queued microtasks and zero-delay timers run. Not a sleep.
  Future<void> settle() => pumpEventQueue();

  /// What the transport answers with, by `METHOD path`.
  void reply(String operation, Object? value) => _replies[operation] = value;

  /// Make an operation throw.
  void fail(String operation, Exception error) => _replies[operation] = error;

  /// The TS `cache.subscribe(meta, args, () => undefined)`: mount and ignore.
  StreamSubscription<QueryState<Object?>> mount(
    OperationMeta meta, [
    TagContext args = TagContext.empty,
  ]) => cache.watch(meta, args).listen((_) {});

  /// The TS `cache.key(meta, args)`.
  String key(OperationMeta meta, [TagContext args = TagContext.empty]) =>
      queryKey(meta, args);
}
