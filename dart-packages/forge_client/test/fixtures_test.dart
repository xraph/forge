@TestOn('vm')
library;

import 'dart:convert';
import 'dart:io';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/wire.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/harness.dart';

const _root = '../../packages/client-fixtures';
final _writing = Platform.environment['FORGE_WRITE_FIXTURES'] == '1';

Map<String, Object?> _object(Object? json) => json! as Map<String, Object?>;

List<Object?> _list(Object? json) => json! as List<Object?>;

Map<String, Object?> _read(String name) =>
    _object(jsonDecode(File('$_root/$name').readAsStringSync()));

List<String> _names(String directory) => [
  for (final entity in Directory('$_root/$directory').listSync())
    if (entity is File && entity.path.endsWith('.json'))
      '$directory/${entity.uri.pathSegments.last}',
]..sort();

EntitySchema _schema(Object? json) => {
  for (final MapEntry(:key, :value) in _object(json).entries)
    key: EntityMeta(
      idField: _object(value)['idField'] as String?,
      fields: {
        for (final MapEntry(key: field, value: target)
            in ((_object(value)['fields'] as Map<String, Object?>?) ?? const {})
                .entries)
          field: target! as String,
      },
    ),
};

OperationMeta _meta(int index, Object? json) {
  final op = _object(json);

  return OperationMeta(
    id: 'fixture_$index',
    method: op['method']! as String,
    path: op['path']! as String,
    entity: op['entity'] as String?,
    rootType: op['rootType'] as String?,
    provides: [
      for (final tag in _list(op['provides'] ?? const <Object?>[]))
        tag! as String,
    ],
    invalidates: [
      for (final tag in _list(op['invalidates'] ?? const <Object?>[]))
        tag! as String,
    ],
  );
}

Map<String, Object?> _keyed(Object? json) =>
    json is Map<String, Object?> ? json : const {};

/// The header map a Dart [TagContext] can hold: string values only. A TS
/// header map that carries a `null` loses that entry, because a header left
/// off and a header passed null are the same request (see `operationQueryKey`).
Map<String, String> _headers(Object? json) => {
  for (final MapEntry(:key, :value) in _keyed(json).entries)
    if (value != null) key: '$value',
};

TagContext _args(Object? json) {
  if (json is! Map<String, Object?>) return TagContext.empty;

  return TagContext(
    path: _keyed(json['path']),
    query: _keyed(json['query']),
    headers: _headers(json['headers']),
    body: json['body'],
  );
}

/// The payload with each query's tags sorted: tag order is a registry
/// iteration detail, not part of the format.
Object? _canonical(Object? json) {
  final copy = jsonDecode(jsonEncode(json));

  for (final query in _list(_object(copy)['queries'])) {
    final tags = _object(query)['tags'];

    if (tags is List<Object?>) tags.sort((a, b) => '$a'.compareTo('$b'));
  }

  return copy;
}

/// [state] as Dart writes it when a query's `args.headers` held a null: the
/// entry is gone, so the null header is read as an absent one.
Object? _withoutNullHeaders(Object? state) {
  final copy = _object(jsonDecode(jsonEncode(state)));

  for (final query in _list(copy['queries'])) {
    final headers = _keyed(_object(query)['args'])['headers'];

    if (headers is Map<String, Object?>) {
      headers.removeWhere((_, value) => value == null);
    }
  }

  return copy;
}

/// Rebuild every map with its keys sorted, so the file does not depend on the
/// order the runtime inserted them in. Lists keep their order.
Object? _sorted(Object? json) => switch (json) {
  final Map<String, Object?> map => {
    for (final key in map.keys.toList()..sort()) key: _sorted(map[key]),
  },
  final List<Object?> list => [for (final item in list) _sorted(item)],
  _ => json,
};

/// Fetch every query of a snapshot fixture into a fresh Dart cache.
Future<QueryCache> _fetchAll(Map<String, Object?> fixture) async {
  final specs = _list(fixture['queries']);
  final cache = QueryCache(
    transport: FakeTransport((_, call) => _object(specs[call])['response']),
    entities: _schema(fixture['schema']),
    scheduler: ManualScheduler(),
    clock: const FixedClock(1000),
  );

  cache.setPrincipal(fixture['principal'] as String?);

  for (var i = 0; i < specs.length; i++) {
    final spec = _object(specs[i]);

    await cache.fetch(_meta(i, spec['operation']), _args(spec['args']));
  }

  return cache;
}

QueryCache _hydratingCache(Map<String, Object?> fixture, String? principal) {
  final cache = QueryCache(
    transport: FakeTransport(
      (_, _) => throw StateError('a hydrated query must not fetch'),
    ),
    entities: _schema(fixture['schema']),
    scheduler: ManualScheduler(),
  );

  cache.setPrincipal(principal);

  return cache;
}

Map<String, OperationMeta> _operations(Map<String, Object?> fixture) {
  final specs = _list(fixture['queries']);

  return {
    for (var i = 0; i < specs.length; i++)
      'q$i': _meta(i, _object(specs[i])['operation']),
  };
}

void main() {
  group('snapshot fixtures', () {
    for (final name in _names('snapshot')) {
      if (name.endsWith('from-dart.json')) continue;

      group(name, () {
        final numericPrincipal = _read(name)['principal'] is num;

        if (numericPrincipal) {
          // TS accepts a numeric principal. Dart sessions are keyed by a
          // string, so the payload is refused instead of read: a cast error
          // here would be a crash, a HydrationFailure is the contract.
          test('is refused, because Dart principals are strings', () {
            final fixture = _read(name);

            for (final principal in <String?>[
              null,
              '${fixture['principal']}',
            ]) {
              final cache = _hydratingCache(fixture, principal);

              expect(
                () => hydrate(
                  cache,
                  Snapshot(_object(fixture['state'])),
                  principal: principal,
                  operations: _operations(fixture),
                ),
                throwsA(
                  isA<HydrationFailure>().having(
                    (failure) => failure.reason,
                    'reason',
                    'principal',
                  ),
                ),
                reason:
                    'principal ${fixture['principal']} against cache $principal',
              );
              expect(cache.queries, isEmpty);
            }
          });

          return;
        }

        test('hydrates the TS payload and reads the same values', () {
          final fixture = _read(name);
          final principal = fixture['principal'] as String?;
          final operations = _operations(fixture);
          final cache = _hydratingCache(fixture, principal);

          hydrate(
            cache,
            Snapshot(_object(fixture['state'])),
            principal: principal,
            operations: operations,
          );

          final reads = _list(fixture['reads']);

          expect(reads, isNotEmpty);

          for (final entry in reads) {
            final read = _object(entry);
            final meta = operations.values.firstWhere(
              (meta) => '${meta.method} ${meta.path}' == read['operation'],
            );

            expect(
              cache.getState(meta, _args(read['args'])).dataOrNull,
              read['value'],
            );
          }
        });

        test('dehydrates the same responses to the same payload', () async {
          final fixture = _read(name);
          final state = _object(fixture['state']);
          final cache = await _fetchAll(fixture);
          final mode = state['mode'] == 'denormalized'
              ? SnapshotMode.denormalized
              : SnapshotMode.normalized;

          final dart = dehydrate(
            cache,
            principal: fixture['principal'] as String?,
            mode: mode,
          );

          expect(
            _canonical(dart.json),
            _canonical(
              name.endsWith('null-header-value.json')
                  ? _withoutNullHeaders(state)
                  : state,
            ),
          );
        });

        test('writes its keys in the order TS writes them', () async {
          final fixture = _read(name);
          final denormalized =
              _object(fixture['state'])['mode'] == 'denormalized';
          final principal = fixture['principal'] as String?;
          final dart = dehydrate(
            await _fetchAll(fixture),
            principal: principal,
            mode: denormalized
                ? SnapshotMode.denormalized
                : SnapshotMode.normalized,
          ).json;

          expect(dart.keys, [
            'v',
            'mode',
            if (principal != null) 'principal',
            if (!denormalized) 'records',
            'queries',
          ]);

          for (final query in _list(dart['queries'])) {
            final entry = _object(query);

            expect(entry.keys, [
              'operation',
              if (entry.containsKey('args')) 'args',
              if (denormalized) 'value' else ...['skeleton', 'tags'],
              'settledTime',
            ]);
          }
        });

        if (name.endsWith('null-header-value.json')) {
          // TS keeps `"x-trace":null` in the payload and in the cache key.
          // A Dart header map holds strings, so Dart drops the entry on the
          // way in, never turning the null into the text "null".
          test('drops a null header value instead of stringifying it', () {
            final fixture = _read(name);
            final cache = _hydratingCache(fixture, 'u-1');

            hydrate(
              cache,
              Snapshot(_object(fixture['state'])),
              principal: 'u-1',
              operations: _operations(fixture),
            );

            final keys = [for (final query in cache.queries) query.key];

            expect(keys, [
              'GET /orders/{id}|{"headers":{"x-region":"eu"},"path":{"id":7}}',
            ]);
          });
        }
      });
    }
  });

  group('codec fixtures', () {
    for (final name in _names('codec')) {
      group(name, () {
        test('normalizes and encodes the client value to the TS wire form', () {
          final fixture = _read(name);

          if (fixture['kind'] != 'ref-encoding') return;

          final schema = _schema(fixture['schema']);
          final result = normalize(
            fixture['client'],
            fixture['rootType'] as String?,
            schema,
          );
          final wire = _object(fixture['wire']);

          expect(
            encode(
              result.skeleton,
              const EncodeContext(query: 'fixture'),
            ).value,
            wire['skeleton'],
          );
          expect({
            for (final MapEntry(:key, :value) in result.records.entries)
              key: encode(
                value,
                EncodeContext(query: 'fixture', entity: key),
              ).value,
          }, wire['records']);
        });

        test('revives the TS wire form back to the client value', () {
          final fixture = _read(name);

          if (fixture['kind'] != 'ref-encoding') return;

          final wire = _object(fixture['wire']);
          final cache = QueryCache(
            transport: FakeTransport((_, _) => null),
            entities: _schema(fixture['schema']),
          );

          for (final MapEntry(:key, :value) in _object(
            wire['records'],
          ).entries) {
            cache.store.put(key, revive(value)! as Map<String, Object?>);
          }

          expect(cache.store.read(revive(wire['skeleton'])), fixture['client']);
        });
      });
    }
  });

  group('frame fixtures', () {
    for (final name in _names('frames')) {
      test(
        '$name produces the store the TS runtime produced from the same frames',
        () {
          final fixture = _read(name);
          final schema = _schema(fixture['entities']);
          final initial = _object(fixture['initial']);
          final cache = QueryCache(
            transport: FakeTransport((_, _) => null),
            entities: schema,
          );

          cache.store.write(
            initial['value'],
            schema,
            initial['rootType'] as String?,
          );

          for (final batch in _list(fixture['frames'])) {
            applyFrames(cache, [
              for (final entry in _list(batch))
                StreamFrame(
                  binding: EntityStreamBinding(
                    channel:
                        _object(_object(entry)['binding'])['channel']!
                            as String,
                    message:
                        _object(_object(entry)['binding'])['message']!
                            as String,
                    entity:
                        _object(_object(entry)['binding'])['entity']! as String,
                    intent: StreamIntent.values.byName(
                      _object(_object(entry)['binding'])['intent']! as String,
                    ),
                    invalidates: [
                      for (final tag in _list(
                        _object(_object(entry)['binding'])['invalidates'],
                      ))
                        tag! as String,
                    ],
                  ),
                  payload: _object(entry)['payload'],
                ),
            ]);
          }

          final keys = [...cache.store.keys]..sort();

          expect({
            for (final key in keys)
              key: encode(
                cache.store.getRecord(key)!.data,
                EncodeContext(query: 'fixture', entity: key),
              ).value,
          }, _object(fixture['expected'])['records']);
        },
      );
    }
  });

  test('writes or verifies the snapshot the Dart runtime produces', () async {
    final source = _read('snapshot/orders-normalized.json');
    final cache = await _fetchAll(source);
    final principal = source['principal'] as String?;
    final specs = _list(source['queries']);

    final fixture = {
      'description': 'Written by the Dart runtime from orders-normalized.json; TS hydrates it',
      'principal': principal,
      'schema': source['schema'],
      'queries': specs,
      'state': dehydrate(cache, principal: principal).json,
      'reads': [
        for (var i = 0; i < specs.length; i++)
          {
            'operation':
                '${_object(_object(specs[i])['operation'])['method']} '
                '${_object(_object(specs[i])['operation'])['path']}',
            'args': _object(specs[i])['args'],
            'value': cache
                .getState(
                  _meta(i, _object(specs[i])['operation']),
                  _args(_object(specs[i])['args']),
                )
                .dataOrNull,
          },
      ],
    };
    final text =
        '${const JsonEncoder.withIndent('  ').convert(_sorted(fixture))}\n';
    final file = File('$_root/snapshot/from-dart.json');

    if (_writing) {
      file.writeAsStringSync(text);

      return;
    }

    expect(
      file.existsSync(),
      isTrue,
      reason: 'run with FORGE_WRITE_FIXTURES=1',
    );
    expect(file.readAsStringSync(), text);
  });
}
