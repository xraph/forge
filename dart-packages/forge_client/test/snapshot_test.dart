import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/harness.dart';
import 'support/schema.dart';

const _ops = <String, OperationMeta>{
  'orderList': orderList,
  'customerList': customerList,
};

/// A cache owned by [principal]. Null is a meaningful principal here: an
/// application that never signs in.
QueryCache _cacheOwnedBy(
  String? principal,
  FutureOr<Object?> Function(TransportRequest request, int call) handler, {
  void Function(Object error, String context)? onError,
  Clock clock = realClock,
}) {
  final client = QueryCache(
    transport: FakeTransport(handler),
    entities: schema,
    scheduler: ManualScheduler(),
    onError: onError,
    clock: clock,
  );

  client.setPrincipal(principal);

  return client;
}

QueryCache _cache(
  FutureOr<Object?> Function(TransportRequest request, int call) handler,
) => _cacheOwnedBy('u-1', handler);

/// A cache whose transport must never be reached.
QueryCache _offline() => _cacheOwnedBy(
  null,
  (_, _) => throw StateError('a hydrated query must not fetch'),
);

/// Serialize and read back, as persistence or an HTML round trip would.
Snapshot _transfer(Snapshot state) => Snapshot.decode(state.encode());

List<Object?> _queries(Snapshot state) =>
    state.json['queries']! as List<Object?>;

Map<String, Object?> _query(Snapshot state, int index) =>
    _queries(state)[index]! as Map<String, Object?>;

Map<String, Object?> _records(Snapshot state) =>
    state.json['records']! as Map<String, Object?>;

Object? _data(
  QueryCache cache,
  OperationMeta meta, [
  TagContext args = TagContext.empty,
]) => cache.getState(meta, args).dataOrNull;

Matcher _refused(String reason, String text) => throwsA(
  isA<HydrationFailure>()
      .having((failure) => failure.reason, 'reason', reason)
      .having((failure) => '$failure', 'text', contains(text)),
);

String? _reasonOf(void Function() run) {
  try {
    run();
  } on HydrationFailure catch (failure) {
    return failure.reason;
  }

  return null;
}

const _ssrOnly =
    'SSR-only: Dart snapshots back persistence, so there is no streamed render '
    'and no hydration boundary to port';

void main() {
  group('dehydrate, normalized', () {
    test(
      'emits the skeleton, the reachable records and the resolved tags',
      () async {
        final client = _cache(
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );

        await client.fetch(orderList, TagContext.empty);

        final state = dehydrate(client, principal: 'u-1');

        expect(state.json['v'], 1);
        expect(state.json['mode'], 'normalized');
        expect(state.json['principal'], 'u-1');
        expect(_queries(state), hasLength(1));
        expect(_query(state, 0)['operation'], 'GET /orders');
        expect(_query(state, 0).containsKey('args'), isFalse);
        expect(_query(state, 0)['skeleton'], [
          {'__ref': 'Order:7'},
        ]);
        expect(
          _query(state, 0)['tags'],
          containsAll(['Order[]', 'Order:7', 'Customer:c-3']),
        );
        expect(_records(state)['Order:7'], {
          'id': 7,
          'total': 99,
          'customer': {'__ref': 'Customer:c-3'},
        });
        expect(_records(state)['Customer:c-3'], {'id': 'c-3', 'name': 'Ada'});
      },
    );

    test('survives JSON', () async {
      final client = _cache(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      expect(
        () => dehydrate(client, principal: 'u-1').encode(),
        returnsNormally,
      );
    });

    test('emits an entity cycle between records without difficulty', () async {
      final client = _cache(
        (_, _) => [
          {
            'id': 7,
            'total': 99,
            'customer': {
              'id': 'c-3',
              'orders': [
                {'id': 7},
              ],
            },
          },
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      final state = dehydrate(client, principal: 'u-1');

      expect(_records(state)['Customer:c-3'], {
        'id': 'c-3',
        'orders': [
          {'__ref': 'Order:7'},
        ],
      });
    });

    test('escapes a record field shaped like a reference', () async {
      final client = _cache(
        (_, _) => [
          {
            'id': 7,
            'meta': {'__ref': 'not a reference'},
          },
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      final state = dehydrate(client, principal: 'u-1');

      expect(_records(state)['Order:7'], {
        'id': 7,
        'meta': {'___ref': 'not a reference'},
      });
    });
  });

  group('dehydrate, the reachability closure', () {
    test('omits an entity no exported query references', () async {
      final client = _cache(
        (request, _) => request.meta.path == '/orders'
            ? [
                {'id': 7, 'total': 99},
              ]
            : [
                {'id': 'c-9', 'name': 'Grace'},
              ],
      );

      await client.fetch(orderList, TagContext.empty);
      await client.fetch(customerList, TagContext.empty);

      final state = dehydrate(
        client,
        principal: 'u-1',
        queries: [queryKey(orderList, TagContext.empty)],
      );

      expect(_records(state).keys, ['Order:7']);
      expect(_queries(state), hasLength(1));
    });

    test(
      'never reads the store wholesale: an orphaned record is not emitted',
      () async {
        final client = _cache(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await client.fetch(orderList, TagContext.empty);
        client.store.put('Order:999', {'id': 999, 'secret': 'another request'});

        final state = dehydrate(client, principal: 'u-1');

        expect(_records(state).keys, ['Order:7']);
      },
    );

    test(
      'throws for an include naming a key the cache does not hold',
      () async {
        final client = _cache(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await client.fetch(orderList, TagContext.empty);

        expect(
          () => dehydrate(client, principal: 'u-1', queries: ['GET /nope()']),
          throwsA(
            isA<StateError>().having(
              (error) => error.message,
              'message',
              contains('[forge] dehydrate: no settled query for GET /nope()'),
            ),
          ),
        );
      },
    );

    test('omits a query that failed', () async {
      final client = _cache((request, _) {
        if (request.meta.path == '/customers') throw StateError('boom');

        return [
          {'id': 7, 'total': 99},
        ];
      });

      await client.fetch(orderList, TagContext.empty);
      await client
          .fetch(customerList, TagContext.empty)
          .catchError((Object _) => null);

      expect(_queries(dehydrate(client, principal: 'u-1')), hasLength(1));
    });
  });

  group('dehydrate, the principal', () {
    test('throws when it does not match the cache owner', () async {
      final client = _cache(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      expect(
        () => dehydrate(client, principal: 'u-2'),
        throwsA(
          isA<StateError>().having(
            (error) => error.message,
            'message',
            contains(
              '[forge] dehydrate: principal does not match the cache owner',
            ),
          ),
        ),
      );
    });

    test('accepts an unset principal on both sides', () async {
      final client = _cacheOwnedBy(
        null,
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      expect(dehydrate(client).json.containsKey('principal'), isFalse);
    });

    test(
      'refuses a principal that cannot survive JSON',
      () {},
      skip: 'Dart principals are typed String?, so a non-scalar principal cannot be passed',
    );
  });

  group('dehydrate, denormalized', () {
    test('emits the rehydrated value and no records', () async {
      final client = _cache(
        (_, _) => [
          {
            'id': 7,
            'total': 99,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      final state = dehydrate(
        client,
        principal: 'u-1',
        mode: SnapshotMode.denormalized,
      );

      expect(state.json['mode'], 'denormalized');
      expect(_queries(state), [
        {
          'operation': 'GET /orders',
          'value': [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
          'settledTime': isA<int>(),
        },
      ]);
      expect(state.json.containsKey('records'), isFalse);
    });

    test('passes reference-shaped response data through untouched', () async {
      final client = _cache(
        (_, _) => [
          {
            'id': 7,
            'meta': {'__ref': 'not a reference'},
          },
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      final state = dehydrate(
        client,
        principal: 'u-1',
        mode: SnapshotMode.denormalized,
      );

      expect(_query(state, 0)['value'], [
        {
          'id': 7,
          'meta': {'__ref': 'not a reference'},
        },
      ]);
    });

    test(
      'throws on an entity cycle, which normalized mode serializes fine',
      () async {
        final client = _cache(
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {
                'id': 'c-3',
                'orders': [
                  {'id': 7},
                ],
              },
            },
          ],
        );

        await client.fetch(orderList, TagContext.empty);

        expect(
          () => dehydrate(
            client,
            principal: 'u-1',
            mode: SnapshotMode.denormalized,
          ),
          throwsA(
            isA<StateError>().having(
              (error) => error.message,
              'message',
              contains('cannot serialize a cyclic value'),
            ),
          ),
        );
        expect(() => dehydrate(client, principal: 'u-1'), returnsNormally);
      },
    );
  });

  group('hydrate', () {
    test(
      'serves the hydrated value with no request, in normalized mode',
      () async {
        final server = _cacheOwnedBy(
          null,
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final client = _offline();

        hydrate(client, _transfer(dehydrate(server)), operations: _ops);

        expect(
          client.getState(orderList, TagContext.empty),
          isA<QuerySuccess<Object?>>(),
        );
        expect(_data(client, orderList), [
          {
            'id': 7,
            'total': 99,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ]);
      },
    );

    test(
      'serves the hydrated value with no request, in denormalized mode',
      () async {
        final server = _cacheOwnedBy(
          null,
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final client = _offline();

        hydrate(
          client,
          _transfer(dehydrate(server, mode: SnapshotMode.denormalized)),
          operations: _ops,
        );

        expect(_data(client, orderList), [
          {
            'id': 7,
            'total': 99,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ]);
      },
    );

    test(
      'produces a store the entity graph is genuinely normalized into',
      () async {
        final server = _cacheOwnedBy(
          null,
          (_, _) => [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final client = _offline();

        hydrate(client, _transfer(dehydrate(server)), operations: _ops);

        expect(client.store.has('Order:7'), isTrue);
        expect(client.store.has('Customer:c-3'), isTrue);
        expect(
          isRef(client.store.getRecord('Order:7')?.data['customer']),
          isTrue,
        );
      },
    );

    test('keeps reference-shaped response data as data', () async {
      final server = _cacheOwnedBy(
        null,
        (_, _) => [
          {
            'id': 7,
            'meta': {'__ref': 'not a reference'},
          },
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      final client = _offline();

      hydrate(client, _transfer(dehydrate(server)), operations: _ops);

      expect(_data(client, orderList), [
        {
          'id': 7,
          'meta': {'__ref': 'not a reference'},
        },
      ]);
    });

    test('rebuilds an entity cycle as a cycle', () async {
      final server = _cacheOwnedBy(
        null,
        (_, _) => [
          {
            'id': 7,
            'total': 99,
            'customer': {
              'id': 'c-3',
              'orders': [
                {'id': 7},
              ],
            },
          },
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      final client = _offline();

      hydrate(client, _transfer(dehydrate(server)), operations: _ops);

      final rows = _data(client, orderList)! as List<Object?>;
      final first = rows.first! as Map<String, Object?>;
      final customer = first['customer']! as Map<String, Object?>;

      expect((customer['orders']! as List<Object?>).first, same(first));
    });

    test('carries a response-templated provides tag across, so a mutation still reaches it', () async {
      const tagged = OperationMeta(
        id: 'op_order_list_tagged',
        method: 'GET',
        path: '/orders',
        entity: 'Order',
        provides: ['Order[]', 'Batch:{res.0.id}'],
      );
      final server = _cacheOwnedBy(
        null,
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await server.fetch(tagged, TagContext.empty);

      final client = _offline();

      hydrate(
        client,
        _transfer(dehydrate(server)),
        operations: const {'orderList': tagged},
      );
      final watching = client.watch(tagged, TagContext.empty).listen((_) {});
      addTearDown(watching.cancel);

      expect(client.registry.queriesFor('Batch:7').map((entry) => entry.key), [
        queryKey(tagged, TagContext.empty),
      ]);
    });

    test('settles fresh by default and stale when asked', () async {
      final server = _cacheOwnedBy(
        null,
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      final state = _transfer(dehydrate(server));
      final key = queryKey(orderList, TagContext.empty);

      final fresh = _offline();
      hydrate(fresh, state, operations: _ops);
      expect(fresh.registry.get(key)?.stale, isFalse);

      final verifying = _offline();
      hydrate(verifying, state, operations: _ops, stale: true);
      expect(verifying.registry.get(key)?.stale, isTrue);
    });

    test(
      'is idempotent: hydrating twice moves no version and keeps identity',
      () async {
        final server = _cacheOwnedBy(
          null,
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final client = _offline();

        hydrate(client, _transfer(dehydrate(server)), operations: _ops);
        final first = (_data(client, orderList)! as List<Object?>).first;

        hydrate(client, _transfer(dehydrate(server)), operations: _ops);

        expect(client.store.getRecord('Order:7')?.version, 1);
        expect((_data(client, orderList)! as List<Object?>).first, same(first));
      },
    );

    test('refuses a payload belonging to another principal', () async {
      final server = _cache(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      final state = _transfer(dehydrate(server, principal: 'u-1'));
      final client = _cacheOwnedBy('u-2', (_, _) => <Object?>[]);

      expect(
        () => hydrate(client, state, principal: 'u-2', operations: _ops),
        _refused(
          'principal',
          '[forge] hydrate: this payload belongs to a different principal',
        ),
      );
    });

    test('refuses an unrecognised payload version', () {
      final client = _offline();

      expect(
        () => hydrate(
          client,
          const Snapshot({
            'v': 2,
            'mode': 'normalized',
            'records': <String, Object?>{},
            'queries': <Object?>[],
          }),
          operations: _ops,
        ),
        _refused('version', '[forge] hydrate: unsupported payload version 2'),
      );
    });

    test('refuses an operation the ops table does not name', () async {
      final server = _cacheOwnedBy(
        null,
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      expect(
        () => hydrate(
          _offline(),
          _transfer(dehydrate(server)),
          operations: const {'customerList': customerList},
        ),
        _refused(
          'operation',
          '[forge] hydrate: no operation named GET /orders',
        ),
      );
    });
  });

  group('hydrate, keying', () {
    const orderGetByPath = OperationMeta(
      id: 'op_order_get_by_path',
      method: 'GET',
      path: '/orders/{id}',
      entity: 'Order',
      provides: ['Order:{path.id}'],
    );
    const seven = TagContext(path: {'id': 7});

    test(
      'lands on the record a component asks for, for a query with arguments',
      () async {
        final server = _cacheOwnedBy(null, (_, _) => {'id': 7, 'total': 99});

        await server.fetch(orderGetByPath, seven);

        final client = _offline();

        hydrate(
          client,
          _transfer(dehydrate(server)),
          operations: const {'orderGet': orderGetByPath},
        );

        expect(_data(client, orderGetByPath, seven), {'id': 7, 'total': 99});
      },
    );

    test(
      'lands on the record a component asks for, for a query with none',
      () async {
        final server = _cacheOwnedBy(
          null,
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final client = _offline();

        hydrate(client, _transfer(dehydrate(server)), operations: _ops);

        // The record the payload restored and the record getState opens must be
        // one record, not two.
        expect(client.size, 1);
        expect(
          client.getState(orderList, TagContext.empty),
          isA<QuerySuccess<Object?>>(),
        );
      },
    );
  });

  group('hydrateBoundary', () {
    test(
      'hydrates a payload the first time and skips it thereafter',
      () {},
      skip: _ssrOnly,
    );
    test(
      'hydrates the same payload again into a different cache',
      () {},
      skip: _ssrOnly,
    );
    test('reports a version refusal and continues', () {}, skip: _ssrOnly);
    test('reports an unknown operation and continues', () {}, skip: _ssrOnly);
    test('rethrows a principal refusal', () {}, skip: _ssrOnly);
    test(
      'rethrows a principal refusal on every attempt, not just the first',
      () {},
      skip: _ssrOnly,
    );
    test('does nothing at all without a payload', () {}, skip: _ssrOnly);
  });

  group('streamingDehydrator', () {
    test(
      'carries everything on the first flush and nothing on a second',
      () {},
      skip: _ssrOnly,
    );
    test(
      'carries only the query that settled since the last flush',
      () {},
      skip: _ssrOnly,
    );
    test(
      're-emits a record whose data changed between flushes',
      () {},
      skip: _ssrOnly,
    );
    test(
      'hydrates chunk by chunk to the same place one payload would',
      () {},
      skip: _ssrOnly,
    );
    test(
      'emits a record an earlier chunk already carried only once',
      () {},
      skip: _ssrOnly,
    );
    test(
      'streams a denormalized payload query by query',
      () {},
      skip: _ssrOnly,
    );
    test(
      'refuses a principal that does not own the cache, on every flush',
      () {},
      skip: _ssrOnly,
    );
  });

  group('hydrationFailure', () {
    test(
      'names the reason a refusal carries, so nothing has to match a message',
      () async {
        final server = _cache(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final state = _transfer(dehydrate(server, principal: 'u-1'));

        expect(
          _reasonOf(
            () => hydrate(
              _cacheOwnedBy('u-2', (_, _) => <Object?>[]),
              state,
              principal: 'u-2',
              operations: _ops,
            ),
          ),
          'principal',
        );
        expect(
          _reasonOf(
            () =>
                hydrate(_offline(), const Snapshot({'v': 9}), operations: _ops),
          ),
          'version',
        );
        expect(
          _reasonOf(
            () => hydrate(
              _offline(),
              const Snapshot({
                'v': 1,
                'mode': 'martian',
                'queries': <Object?>[],
              }),
              operations: _ops,
            ),
          ),
          'version',
        );

        final anonymous = _cacheOwnedBy(
          null,
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );
        await anonymous.fetch(orderList, TagContext.empty);

        expect(
          _reasonOf(
            () => hydrate(
              _offline(),
              _transfer(dehydrate(anonymous)),
              operations: const {'customerList': customerList},
            ),
          ),
          'operation',
        );
      },
    );

    test(
      'answers undefined for anything it did not raise',
      () {},
      skip: 'HydrationFailure is a typed exception in Dart; there is no untyped error to misread',
    );

    test('leaves the cache untouched when it refuses before writing', () async {
      final server = _cache(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      final state = _transfer(dehydrate(server, principal: 'u-1'));
      final client = _cacheOwnedBy('u-2', (_, _) => <Object?>[]);

      expect(
        () => hydrate(client, state, principal: 'u-2', operations: _ops),
        throwsA(isA<HydrationFailure>()),
      );
      expect(client.store.size, 0);
      expect(client.size, 0);
    });
  });

  group('carrying the settle time across hydration', () {
    QueryCache timed(int now) {
      final client = QueryCache(
        transport: FakeTransport(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        ),
        entities: schema,
        scheduler: ManualScheduler(),
        clock: FixedClock(now),
      );

      client.setPrincipal('u-1');

      return client;
    }

    test(
      "hydrates with the server's settle time, not the client's clock",
      () async {
        final server = timed(1000);

        await server.fetch(orderList, TagContext.empty);

        final state = dehydrate(server, principal: 'u-1');

        expect(_query(state, 0)['settledTime'], 1000);

        final client = timed(101000);

        hydrate(
          client,
          state,
          principal: 'u-1',
          operations: const {'GET /orders': orderList},
        );

        expect(client.settledTimeOf(orderList, TagContext.empty), 1000);
      },
    );

    test('carries it through a denormalized payload too', () async {
      final server = timed(2000);

      await server.fetch(orderList, TagContext.empty);

      final state = dehydrate(
        server,
        principal: 'u-1',
        mode: SnapshotMode.denormalized,
      );

      expect(_query(state, 0)['settledTime'], 2000);

      final client = timed(50000);

      hydrate(
        client,
        state,
        principal: 'u-1',
        operations: const {'GET /orders': orderList},
      );

      expect(client.settledTimeOf(orderList, TagContext.empty), 2000);
    });

    test(
      'falls back to the local clock for a payload that carries no settle time',
      () async {
        final server = timed(1000);

        await server.fetch(orderList, TagContext.empty);

        final state = dehydrate(server, principal: 'u-1');
        final older = Snapshot({
          ...state.json,
          'queries': [
            for (final query in _queries(state))
              {...query! as Map<String, Object?>}..remove('settledTime'),
          ],
        });

        final client = timed(77000);

        hydrate(
          client,
          older,
          principal: 'u-1',
          operations: const {'GET /orders': orderList},
        );

        expect(client.settledTimeOf(orderList, TagContext.empty), 77000);
      },
    );
  });

  group('Dart port', () {
    test('encodes to JSON text and decodes back to the same payload', () async {
      final client = _cache(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await client.fetch(orderList, TagContext.empty);

      final state = dehydrate(client, principal: 'u-1');

      expect(Snapshot.decode(state.encode()).json, state.json);
      expect(
        state.encode(),
        startsWith('{"v":1,"mode":"normalized","principal":"u-1"'),
      );
    });

    test('refuses snapshot text that is not a JSON object', () {
      expect(() => Snapshot.decode('[1,2]'), throwsFormatException);
    });

    // Review Focus: a snapshot from a different principal, in the two shapes
    // the TS tests do not cover.
    test(
      'refuses an anonymous payload hydrated into a signed-in cache',
      () async {
        final server = _cacheOwnedBy(
          null,
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final client = _cacheOwnedBy('u-1', (_, _) => <Object?>[]);

        expect(
          () => hydrate(
            client,
            _transfer(dehydrate(server)),
            principal: 'u-1',
            operations: _ops,
          ),
          _refused('principal', 'different principal'),
        );
        expect(client.store.size, 0);
      },
    );

    test('refuses a TS payload that carries a numeric principal', () {
      final client = _offline();

      expect(
        () => hydrate(
          client,
          const Snapshot({
            'v': 1,
            'mode': 'normalized',
            'principal': 7,
            'records': <String, Object?>{},
            'queries': <Object?>[],
          }),
          operations: _ops,
        ),
        _refused('principal', 'different principal'),
      );
      expect(client.store.size, 0);
    });

    // Review Focus: the principal is the asserted identity in both
    // directions, so every mismatch between it, the cache owner and the
    // payload is refused, null against non-null included.
    test(
      'refuses a signed-in payload hydrated into an anonymous cache',
      () async {
        final server = _cache(
          (_, _) => [
            {'id': 7, 'total': 99},
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final client = _offline();

        expect(
          () => hydrate(
            client,
            _transfer(dehydrate(server, principal: 'u-1')),
            operations: _ops,
          ),
          _refused('principal', 'different principal'),
        );
        expect(client.store.size, 0);
        expect(client.size, 0);
      },
    );

    test('refuses an asserted principal that is not the cache owner, payload matching', () async {
      final server = _cache(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      final state = _transfer(dehydrate(server, principal: 'u-1'));
      final client = _cacheOwnedBy('u-1', (_, _) => <Object?>[]);

      expect(
        () => hydrate(client, state, principal: 'u-2', operations: _ops),
        _refused('principal', 'different principal'),
      );
      expect(
        () => hydrate(client, state, operations: _ops),
        _refused('principal', 'different principal'),
      );
      expect(client.store.size, 0);

      hydrate(client, state, principal: 'u-1', operations: _ops);

      expect(client.store.size, 1);
    });

    test('refuses to dehydrate for null when signed in, and for a user when anonymous', () async {
      final signedIn = _cache(
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );
      final anonymous = _cacheOwnedBy(
        null,
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await signedIn.fetch(orderList, TagContext.empty);
      await anonymous.fetch(orderList, TagContext.empty);

      final mismatch = throwsA(
        isA<StateError>().having(
          (error) => error.message,
          'message',
          contains('principal does not match the cache owner'),
        ),
      );

      expect(() => dehydrate(signedIn), mismatch);
      expect(() => dehydrate(anonymous, principal: 'u-1'), mismatch);
    });

    test(
      'stops writing when the principal changes while it hydrates',
      () async {
        final server = _cache(
          (request, _) => request.meta.path == '/orders'
              ? [
                  {'id': 7, 'total': 99},
                ]
              : [
                  {'id': 'c-9', 'name': 'Grace'},
                ],
        );

        await server.fetch(orderList, TagContext.empty);
        await server.fetch(customerList, TagContext.empty);

        final state = _transfer(dehydrate(server, principal: 'u-1'));
        final client = _cacheOwnedBy('u-1', (_, _) => <Object?>[]);
        var flipped = false;

        // The first restored query notifies, and a listener signs someone else
        // in. The second query belongs to u-1 and must not land in u-2's cache.
        client.observer = (event) {
          if (!flipped && event is QueryTransition) {
            flipped = true;
            client.setPrincipal('u-2');
          }
        };

        expect(
          () => hydrate(client, state, principal: 'u-1', operations: _ops),
          _refused('principal', 'the principal changed while hydrating'),
        );
        expect(flipped, isTrue);
        expect(client.principal, 'u-2');
        expect(client.store.size, 0);
        expect(client.queries, isEmpty);
      },
    );

    test('refuses an unknown operation before writing anything', () async {
      final server = _cacheOwnedBy(
        null,
        (_, _) => [
          {'id': 7, 'total': 99},
        ],
      );

      await server.fetch(orderList, TagContext.empty);

      final client = _offline();

      expect(
        () => hydrate(
          client,
          _transfer(dehydrate(server)),
          operations: const {'customerList': customerList},
        ),
        _refused('operation', 'no operation named GET /orders'),
      );
      // TS has already written the records by the time it looks the
      // operation up; Dart resolves every operation first.
      expect(client.store.size, 0);
      expect(client.size, 0);
    });

    for (final mode in SnapshotMode.values) {
      test(
        'does not resurrect an entity a frame deleted, ${mode.name}',
        () async {
          final server = _cacheOwnedBy(
            null,
            (_, _) => [
              {'id': 7, 'total': 99},
              {'id': 8, 'total': 5},
            ],
          );

          await server.fetch(orderList, TagContext.empty);

          final state = _transfer(dehydrate(server, mode: mode));
          final client = _offline();

          // A stream frame deleted Order:7 here, leaving a tombstone.
          client.store.put('Order:7', {'id': 7, 'total': 99});
          client.store.evict('Order:7', client.store.nextFrame());

          expect(client.store.tombstones, 1);

          hydrate(client, state, operations: _ops);

          expect(client.store.has('Order:7'), isFalse);
          expect(client.store.has('Order:8'), isTrue);
          expect(_data(client, orderList), [
            {'id': 8, 'total': 5},
          ]);
        },
      );

      test('writes -0 as 0 and hydrates it as 0, ${mode.name}', () async {
        final server = _cacheOwnedBy(
          null,
          (_, _) => [
            {'id': 7, 'delta': -0.0},
          ],
        );

        await server.fetch(orderList, TagContext.empty);

        final state = dehydrate(server, mode: mode);

        expect(state.encode(), contains('"delta":0'));
        expect(state.encode(), isNot(contains('-0')));

        final client = _offline();

        hydrate(client, _transfer(state), operations: _ops);

        final row =
            (_data(client, orderList)! as List<Object?>).single!
                as Map<String, Object?>;
        final delta = row['delta']! as num;

        expect(delta, 0);
        expect(delta is double && delta.isNegative, isFalse);
      });
    }

    test('writes the denormalized document the TS encoder writes', () async {
      final client = _cacheOwnedBy(
        'u-1',
        (_, _) => [
          {
            'id': 7,
            'total': 7.0,
            'delta': -0.0,
            'ratio': double.nan,
            'grid': {'zeta': 1, '10': 2, '2': 3},
          },
        ],
        clock: const FixedClock(1000),
      );

      await client.fetch(orderList, TagContext.empty);

      // JSON.stringify of the same value in node.
      expect(
        dehydrate(
          client,
          principal: 'u-1',
          mode: SnapshotMode.denormalized,
        ).encode(),
        '{"v":1,"mode":"denormalized","principal":"u-1","queries":[{"operation":"GET /orders",'
        '"value":[{"id":7,"total":7,"delta":0,"ratio":null,"grid":{"2":3,"10":2,"zeta":1}}],'
        '"settledTime":1000}]}',
      );
    });

    for (final mode in SnapshotMode.values) {
      test(
        'writes arguments as TS text and hydrates them to the same key, ${mode.name}',
        () async {
          const orderAt = OperationMeta(
            id: 'op_order_at',
            method: 'GET',
            path: '/orders/{id}',
            entity: 'Order',
            provides: ['Order:{path.id}'],
          );
          const args = TagContext(
            path: {'id': 7.0},
            query: {'z': 1, '10': 2.0},
          );
          final server = _cacheOwnedBy(null, (_, _) => {'id': 7, 'total': 99});

          await server.fetch(orderAt, args);

          final state = dehydrate(server, mode: mode);

          expect(
            state.encode(),
            contains('"args":{"path":{"id":7},"query":{"10":2,"z":1}}'),
          );

          final client = _offline();

          hydrate(
            client,
            _transfer(state),
            operations: const {'orderAt': orderAt},
          );

          expect(client.peek(orderAt, args), isA<QuerySuccess<Object?>>());
          expect(client.size, 1);
        },
      );
    }
  });
}
