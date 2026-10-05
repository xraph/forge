import 'dart:collection';
import 'dart:convert';
import 'dart:math';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

/// Response trees aimed at the shapes hand-written cases miss: arrays of
/// arrays, nullable entity references, entities at varying depth, the same
/// entity twice, objects shaped exactly like a reference, `-0`, and the mutual
/// Order to Customer to Order cycle. Ids come from small pools on purpose, so
/// the collisions that exercise merging actually happen.
final class _Gen {
  _Gen(int seed) : _random = Random(seed);

  final Random _random;

  static const _pieces = [
    'a',
    'Z',
    '_',
    ' ',
    'é',
    '日',
    '"',
    r'\',
    '0',
    ':',
    '/',
    '-',
  ];

  bool chance(int percent) => _random.nextInt(100) < percent;

  T pick<T>(List<T> options) => options[_random.nextInt(options.length)];

  int integer([int span = 2000]) => _random.nextInt(span) - span ~/ 2;

  String string([int max = 6]) {
    final length = _random.nextInt(max + 1);

    return [for (var i = 0; i < length; i++) pick(_pieces)].join();
  }

  /// A property name; the reference marker and its escapes are drawn on
  /// purpose, since an arbitrary string never produces them.
  String name() {
    if (chance(25)) {
      return pick(const ['__ref', '___ref', '____ref', '__refs', '_ref']);
    }

    final text = string(5);

    return text.isEmpty ? 'k' : text;
  }

  Object? scalar() => switch (_random.nextInt(7)) {
    0 => integer(),
    1 => string(),
    2 => _random.nextBool(),
    3 => null,
    4 => (_random.nextDouble() - 0.5) * 1e6,
    5 => -0.0,
    _ => integer(20).toDouble(),
  };

  List<Object?> listOf(int max, Object? Function() make) {
    final count = _random.nextInt(max + 1);

    return [for (var i = 0; i < count; i++) make()];
  }

  Map<String, Object?> mapOf(int max, Object? Function() make) {
    final count = _random.nextInt(max + 1);

    return {for (var i = 0; i < count; i++) name(): make()};
  }

  /// A plain value with no entity in it.
  Object? plain(int depth) {
    if (depth == 0) return scalar();

    return switch (_random.nextInt(5)) {
      0 || 1 => scalar(),
      2 => listOf(3, () => plain(depth - 1)),
      3 => mapOf(3, () => plain(depth - 1)),
      _ => <String, Object?>{'__ref': string()},
    };
  }

  Map<String, Object?> lineItem() => {
    'sku': pick(const ['SKU-1', 'SKU-2']),
    'qty': _random.nextInt(10),
  };

  /// The non-`id` identity; empty and null numbers must stay inline.
  Map<String, Object?> invoice() => {
    if (chance(75))
      'invoiceNumber': pick(const <Object?>['INV-1', 'INV-2', '', null]),
    'amount': integer(),
  };

  Map<String, Object?> order(int depth) => {
    'id': pick(const <Object>[1, 2, 3, 'a', 'b']),
    if (chance(70)) 'total': integer(),
    if (chance(60))
      'customer': depth > 0 && chance(70) ? customer(depth - 1) : null,
    if (chance(50)) 'invoice': chance(30) ? null : invoice(),
    if (chance(60))
      'items': chance(70)
          ? listOf(3, lineItem)
          : listOf(2, () => listOf(2, lineItem)),
    if (chance(50))
      'related': depth > 0 ? listOf(2, () => order(depth - 1)) : <Object?>[],
    if (chance(50)) 'notes': plain(2),
  };

  Map<String, Object?> customer(int depth) => {
    'id': pick(const ['c-1', 'c-2']),
    if (chance(70)) 'name': string(),
    if (chance(70))
      'orders': depth > 0 ? listOf(2, () => order(depth - 1)) : <Object?>[],
    if (chance(50)) 'meta': plain(2),
  };

  /// What an operation can hand the runtime: an entity, a list, an envelope,
  /// or a value with no declared type.
  ({Object? value, String? type}) response() => switch (_random.nextInt(7)) {
    0 => (value: order(3), type: 'Order'),
    1 => (value: listOf(4, () => order(3)), type: 'Order'),
    2 => (value: customer(3), type: 'Customer'),
    3 => (value: invoice(), type: 'Invoice'),
    4 => (value: listOf(3, invoice), type: 'Invoice'),
    5 => (
      value: <String, Object?>{
        'data': order(3),
        'items': listOf(3, () => order(2)),
        'invoice': chance(30) ? null : invoice(),
        'meta': plain(2),
      },
      type: 'Envelope',
    ),
    _ => (value: plain(3), type: null),
  };
}

const _seed = 20261004;

/// Runs [body] for [runs] seeds, printing the failing one.
void _forAll(int runs, void Function(_Gen gen) body, {int base = _seed}) {
  for (var i = 0; i < runs; i++) {
    final seed = base + i;

    try {
      body(_Gen(seed));
    } on Object {
      printOnFailure(
        'failing case: seed $seed (case ${i + 1} of $runs); replay with _Gen($seed)',
      );

      rethrow;
    }
  }
}

/// Deep equality that terminates on cyclic values (a pair already being
/// compared is assumed equal), sees through references, and tells `-0.0`
/// from `0.0`.
///
/// With [viaJson], [b] is [a] after a trip through snapshot JSON text, which
/// writes `-0` as `0` exactly as `JSON.stringify` does: a negative zero in [a]
/// must arrive in [b] as a zero that is not negative, so the comparison
/// proves the sign was dropped rather than ignoring it.
bool _deepEquals(
  Object? a,
  Object? b, {
  bool viaJson = false,
  List<(Object, Object)>? assumed,
}) {
  final pairs = assumed ?? <(Object, Object)>[];

  if (a is EntityRef && b is EntityRef) return a.key == b.key;

  if (a is Map<Object?, Object?> && b is Map<Object?, Object?>) {
    if (pairs.any((pair) => identical(pair.$1, a) && identical(pair.$2, b))) {
      return true;
    }
    if (a.length != b.length) return false;

    pairs.add((a, b));

    for (final MapEntry(:key, :value) in a.entries) {
      if (!b.containsKey(key) ||
          !_deepEquals(value, b[key], viaJson: viaJson, assumed: pairs)) {
        return false;
      }
    }

    return true;
  }

  if (a is List<Object?> && b is List<Object?>) {
    if (pairs.any((pair) => identical(pair.$1, a) && identical(pair.$2, b))) {
      return true;
    }
    if (a.length != b.length) return false;

    pairs.add((a, b));

    for (var i = 0; i < a.length; i++) {
      if (!_deepEquals(a[i], b[i], viaJson: viaJson, assumed: pairs)) {
        return false;
      }
    }

    return true;
  }

  if (a is num && b is num) {
    if (viaJson && _isNegativeZero(a)) return b == 0 && !_isNegativeZero(b);

    return a == b && _isNegativeZero(a) == _isNegativeZero(b);
  }

  return a == b;
}

bool _isNegativeZero(Object? number) =>
    number is double && number == 0 && number.isNegative;

/// The expected value of a round trip: not the input, but the input with each
/// entity replaced by the union of every occurrence of it, rebuilt with one
/// object per key (which can be cyclic where the input was not).
Object? _expected(Object? value, String? type) {
  final merged = <String, Map<String, Object?>>{};

  String? keyOf(Map<String, Object?> node, String? hint) {
    final idField = hint == null ? null : schema[hint]?.idField;
    final id = idField == null ? null : node[idField];

    return isIdentity(id) ? entityKey(hint!, id!) : null;
  }

  void collect(Object? node, String? hint) {
    if (node is List<Object?>) {
      for (final item in node) {
        collect(item, hint);
      }

      return;
    }

    if (node is! Map<String, Object?>) return;

    final fields = hint == null ? null : schema[hint]?.fields;

    for (final MapEntry(key: field, :value) in node.entries) {
      collect(value, fields?[field]);
    }

    final key = keyOf(node, hint);

    if (key != null) merged[key] = {...?merged[key], ...node};
  }

  collect(value, type);

  final built = <String, Map<String, Object?>>{};

  Object? rebuild(Object? node, String? hint) {
    if (node is List<Object?>) {
      return [for (final item in node) rebuild(item, hint)];
    }
    if (node is! Map<String, Object?>) return node;

    final key = keyOf(node, hint);

    if (key != null) {
      final done = built[key];

      if (done != null) return done;
    }

    final fields = hint == null ? null : schema[hint]?.fields;
    final source = key == null ? node : merged[key]!;
    final out = <String, Object?>{};

    if (key != null) built[key] = out;

    for (final MapEntry(key: field, :value) in source.entries) {
      out[field] = rebuild(value, fields?[field]);
    }

    return out;
  }

  return rebuild(value, type);
}

/// Every container reachable from a value, by identity, cycle-safe.
List<Object> _subtrees(Object? value) {
  final out = <Object>[];
  final seen = HashSet<Object>.identity();

  void walk(Object? node) {
    if (node is Map<Object?, Object?>) {
      if (seen.add(node)) {
        out.add(node);
        node.values.forEach(walk);
      }
    } else if (node is List<Object?>) {
      if (seen.add(node)) {
        out.add(node);
        node.forEach(walk);
      }
    }
  }

  walk(value);

  return out;
}

bool _hasNegativeZero(Object? node) => switch (node) {
  final double number => _isNegativeZero(number),
  final Map<Object?, Object?> map => map.values.any(_hasNegativeZero),
  final List<Object?> list => list.any(_hasNegativeZero),
  _ => false,
};

/// The entity keys each record references, at any depth.
Map<String, Set<String>> _referenceGraph(EntityStore store) {
  final edges = <String, Set<String>>{};

  for (final key in [...store.keys]) {
    final out = <String>{};

    void walk(Object? node) {
      if (node is EntityRef) {
        out.add(node.key);
      } else if (node is Map<Object?, Object?>) {
        node.values.forEach(walk);
      } else if (node is List<Object?>) {
        node.forEach(walk);
      }
    }

    walk(store.getRecord(key)?.data);
    edges[key] = out;
  }

  return edges;
}

/// One operation per root typename, so `rootType` matches the sample.
OperationMeta _metaFor(String? type) => OperationMeta(
  id: 'op_${type ?? 'untyped'}',
  method: 'GET',
  path: '/${(type ?? 'untyped').toLowerCase()}',
  entity: type,
  rootType: type,
);

QueryCache _snapshotCache() => QueryCache(
  transport: FakeTransport(
    (_, _) => throw StateError('the snapshot properties issue no requests'),
  ),
  entities: schema,
  scheduler: ManualScheduler(),
);

/// A cache holding one settled query for this sample.
({QueryCache cache, OperationMeta meta}) _rendered(
  Object? value,
  String? type,
) {
  final cache = _snapshotCache();
  final meta = _metaFor(type);
  final written = cache.store.write(value, schema, type);

  cache.restore(
    RestoreInput(meta: meta, skeleton: written.skeleton, tags: const []),
  );

  return (cache: cache, meta: meta);
}

/// The client cache [server] hydrates into, through JSON text.
QueryCache _hydrated(({QueryCache cache, OperationMeta meta}) server) {
  final wire = Snapshot.decode(dehydrate(server.cache).encode());
  final client = _snapshotCache();

  hydrate(client, wire, operations: {'op': server.meta});

  return client;
}

void main() {
  // A generator that never produces the shape it was written for is a test
  // that passes for the wrong reason.
  group('generator coverage', () {
    test('reaches the shapes it claims to', () {
      var invoices = 0;
      var refused = 0;
      var mutualCycles = 0;
      var referenceShaped = 0;
      var negativeZeros = 0;

      _forAll(600, base: 20260803, (gen) {
        final (:value, :type) = gen.response();
        final store = EntityStore();
        final written = store.write(value, schema, type);
        final text = jsonEncode(value);

        if (written.deps.any((key) => key.startsWith('Invoice:'))) invoices++;
        if (text.contains('"invoiceNumber":""')) refused++;
        if (text.contains('{"__ref":')) referenceShaped++;
        if (_hasNegativeZero(value)) negativeZeros++;

        final edges = _referenceGraph(store);

        for (final MapEntry(key: from, value: to) in edges.entries) {
          if (!from.startsWith('Order:')) continue;

          for (final other in to) {
            if (other.startsWith('Customer:') &&
                (edges[other]?.contains(from) ?? false)) {
              mutualCycles++;
            }
          }
        }
      });

      expect(invoices, greaterThan(0));
      expect(refused, greaterThan(0));
      expect(mutualCycles, greaterThan(0));
      expect(referenceShaped, greaterThan(0));
      expect(negativeZeros, greaterThan(0));
    });
  });

  group('round trip', () {
    test('denormalize(normalize(x)) equals x, modulo entity merging', () {
      _forAll(400, (gen) {
        final (:value, :type) = gen.response();
        final store = EntityStore();
        final written = store.write(value, schema, type);

        expect(
          _deepEquals(
            denormalize(written.skeleton, store),
            _expected(value, type),
          ),
          isTrue,
        );
      });
    });

    test('leaves no entity data inline in the skeleton', () {
      _forAll(400, (gen) {
        final (:value, :type) = gen.response();
        final result = normalize(value, type, schema);

        for (final key in result.deps) {
          expect(result.records.containsKey(key), isTrue, reason: key);
        }

        final skeleton = result.skeleton;

        if (skeleton is EntityRef) {
          expect(result.records.containsKey(skeleton.key), isTrue);
        }

        for (final node in _subtrees(skeleton)) {
          final children = node is Map<Object?, Object?>
              ? node.values
              : node as List<Object?>;

          for (final child in children) {
            if (child is EntityRef) {
              expect(result.records.containsKey(child.key), isTrue);
            }
          }
        }
      });
    });

    test('does not mutate the response it was given', () {
      _forAll(200, (gen) {
        final (:value, :type) = gen.response();
        final before = jsonDecode(jsonEncode(value));

        normalize(value, type, schema);

        expect(_deepEquals(value, before), isTrue);
      });
    });

    test('writing the same response twice bumps no version', () {
      _forAll(400, (gen) {
        final (:value, :type) = gen.response();
        final store = EntityStore();

        store.write(value, schema, type);
        final writes = store.version;
        store.write(value, schema, type);

        expect(store.version, writes);
      });
    });

    test(
      'reads with no write between are referentially identical, everywhere',
      () {
        _forAll(400, (gen) {
          final (:value, :type) = gen.response();
          final store = EntityStore();
          final written = store.write(value, schema, type);

          final first = denormalize(written.skeleton, store);
          final second = denormalize(written.skeleton, store);

          expect(second, same(first));

          final a = _subtrees(first);
          final b = _subtrees(second);

          expect(b, hasLength(a.length));

          for (var i = 0; i < a.length; i++) {
            expect(b[i], same(a[i]));
          }
        });
      },
    );

    test(
      'a write to one entity leaves every subtree that excludes it identical',
      () {
        _forAll(300, (gen) {
          final (:value, :type) = gen.response();
          final bump = gen.integer(1000000);
          final store = EntityStore();
          final written = store.write(value, schema, type);
          final keys = written.deps.toList();

          if (keys.isEmpty) return;

          final target = keys[bump.abs() % keys.length];
          final before = denormalize(written.skeleton, store);
          final beforeIds = HashSet<Object>.identity()
            ..addAll(_subtrees(before));

          store.put(target, {'__probe': bump});

          final after = denormalize(written.skeleton, store);

          for (final node in _subtrees(after)) {
            if (!beforeIds.contains(node)) continue;

            expect(
              node is Map<Object?, Object?> && node['__probe'] == bump,
              isFalse,
            );
          }

          expect(store.getRecord(target)?.data.containsKey('__probe'), isTrue);
        });
      },
    );

    test('a write to an unrelated entity recomputes nothing', () {
      _forAll(400, (gen) {
        final (:value, :type) = gen.response();
        final store = EntityStore();
        final written = store.write(value, schema, type);
        const unrelated = 'Order:not-in-this-response';

        if (written.deps.contains(unrelated)) return;

        final before = denormalize(written.skeleton, store);

        store.put(unrelated, {'id': 'not-in-this-response'});

        expect(denormalize(written.skeleton, store), same(before));
      });
    });
  });

  // TS filters responses holding `-0` out of these properties (`jsonSafe`),
  // because `JSON.stringify` writes it as `0`. The Dart snapshot writes the
  // same text, so `-0` stays in the samples here and `_deepEquals(viaJson:
  // true)` asserts that it arrives as `0`: TS parity, stated rather than
  // filtered away.
  group('the SSR round trip', () {
    test('hydrates to the value the server rendered, through JSON', () {
      var negativeZeros = 0;

      _forAll(500, (gen) {
        final (:value, :type) = gen.response();

        if (_hasNegativeZero(value)) negativeZeros++;

        final server = _rendered(value, type);
        final expected = server.cache
            .getState(server.meta, TagContext.empty)
            .dataOrNull;
        final client = _hydrated(server);

        expect(
          _deepEquals(
            expected,
            client.getState(server.meta, TagContext.empty).dataOrNull,
            viaJson: true,
          ),
          isTrue,
        );
      });

      // Otherwise the -0 assertion above never ran.
      expect(negativeZeros, greaterThan(0));
    });

    test('recomputes the dependency set it started with', () {
      _forAll(500, (gen) {
        final (:value, :type) = gen.response();
        final server = _rendered(value, type);
        final key = server.cache.key(server.meta, TagContext.empty);
        final before = [...?server.cache.registry.get(key)?.deps]..sort();
        final client = _hydrated(server);

        expect([...?client.registry.get(key)?.deps]..sort(), before);
      });
    });

    // Dart only: the store itself comes back record for record, not merely
    // the value one query reads from it, with every `-0` arriving as `0`.
    // Only the records the query reaches travel: a merge can leave a record
    // nothing references any more (an entity's later occurrence replacing
    // the list that held it), and the reachability closure leaves it behind.
    test('reproduces every reachable record of the store it dehydrated', () {
      var orphans = 0;

      _forAll(500, (gen) {
        final (:value, :type) = gen.response();
        final server = _rendered(value, type);
        final reachable = server.cache.store.dependencies(
          server.cache.queries.single.skeleton,
        );
        final client = _hydrated(server);

        if (reachable.length < server.cache.store.size) orphans++;

        expect({...client.store.keys}, reachable);

        for (final key in reachable) {
          expect(
            _deepEquals(
              server.cache.store.getRecord(key)?.data,
              client.store.getRecord(key)?.data,
              viaJson: true,
            ),
            isTrue,
            reason: key,
          );
        }
      });

      // Otherwise the closure was never asked to leave anything behind.
      expect(orphans, greaterThan(0));
    });
  });
}
