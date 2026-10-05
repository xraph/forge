// Ported from packages/client-core/__tests__/store.test.ts.
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/schema.dart';

const list = [
  {
    'id': 7,
    'total': 99,
    'customer': {'id': 'c-3', 'name': 'Ada'},
  },
  {
    'id': 8,
    'total': 1,
    'customer': {'id': 'c-4', 'name': 'Grace'},
  },
];

typedef Seeded = ({EntityStore store, Object? skeleton});

Seeded seeded() {
  final store = EntityStore();
  final staged = store.write(list, schema, 'Order');

  return (store: store, skeleton: staged.skeleton);
}

List<Object?> readList(
  EntityStore store,
  Object? skeleton, [
  Object? previous,
]) => store.read(skeleton, previous)! as List<Object?>;

Map<String, Object?> readMap(
  EntityStore store,
  Object? skeleton, [
  Object? previous,
]) => store.read(skeleton, previous)! as Map<String, Object?>;

Map<String, Object?> at(List<Object?> rows, int index) =>
    rows[index]! as Map<String, Object?>;

Map<String, Object?> field(Map<String, Object?> map, String name) =>
    map[name]! as Map<String, Object?>;

/// A layer under the test's control. The real one arrives with the overlay
/// task. A null override means "the fold deletes it".
final class StubLayer implements OverlayLayer {
  StubLayer(this.overrides);

  final Map<EntityKey, Json?> overrides;

  @override
  EntityRecord? effective(EntityKey key) {
    final patch = overrides[key];

    if (patch == null) return null;

    return EntityRecord(data: patch, version: 1);
  }

  @override
  bool holds(EntityKey key) => overrides.containsKey(key);

  @override
  void rebase(EntityKey key) {}
}

void main() {
  group('EntityStore', () {
    test('rebuilds the response it was given', () {
      final (:store, :skeleton) = seeded();

      expect(denormalize(skeleton, store), list);
    });

    test('versions each entity independently and bumps on write', () {
      final (:store, skeleton: _) = seeded();

      expect(store.getRecord('Order:7')?.version, 1);

      store.put('Order:7', {'total': 120});

      expect(store.getRecord('Order:7')?.version, 2);
      expect(store.getRecord('Order:8')?.version, 1);
      expect(store.getRecord('Order:7')?.data, {
        'id': 7,
        'total': 120,
        'customer': refTo('Customer:c-3'),
      });
    });

    test('does not bump a version when the write changes nothing', () {
      final (:store, skeleton: _) = seeded();
      final before = store.getRecord('Order:7');
      final writes = store.version;

      expect(store.put('Order:7', {'total': 99}), isFalse);
      expect(store.getRecord('Order:7'), same(before));
      expect(store.version, writes);
    });

    test('does not bump a version when the same response is written again', () {
      final (:store, skeleton: _) = seeded();
      final writes = store.version;

      store.write(list, schema, 'Order');

      expect(store.version, writes);
    });

    // One object reached through two fields is a DAG, not a cycle.
    test('sees a change under two fields that aliased one object', () {
      final store = EntityStore();
      final shared = {'n': 1};

      store.put('Order:1', {'x': shared, 'y': shared});

      expect(
        store.put('Order:1', {
          'x': {'n': 1},
          'y': {'n': 2},
        }),
        isTrue,
      );
      expect(store.getRecord('Order:1')?.version, 2);
      expect(store.getRecord('Order:1')?.data, {
        'x': {'n': 1},
        'y': {'n': 2},
      });
    });

    test(
      'still reports no change when an aliased object is rewritten identically',
      () {
        final store = EntityStore();
        final shared = {'n': 1};

        store.put('Order:1', {'x': shared, 'y': shared});

        expect(
          store.put('Order:1', {
            'x': {'n': 1},
            'y': {'n': 1},
          }),
          isFalse,
        );
        expect(store.getRecord('Order:1')?.version, 1);
      },
    );

    test('sees a change deep under an aliased branch', () {
      final store = EntityStore();
      final shared = {
        'deep': {'n': 1},
      };

      store.put('Order:1', {'x': shared, 'y': shared, 'z': shared});

      expect(
        store.put('Order:1', {
          'x': {
            'deep': {'n': 1},
          },
          'y': {
            'deep': {'n': 1},
          },
          'z': {
            'deep': {'n': 9},
          },
        }),
        isTrue,
      );
    });

    test('preserves fields a later, narrower write does not mention', () {
      final (:store, skeleton: _) = seeded();

      store.put('Order:7', {'total': 120});

      expect(
        store.getRecord('Order:7')?.data['customer'],
        refTo('Customer:c-3'),
      );
    });

    test('serves nothing from a cleared store, including out of a memo', () {
      final (:store, :skeleton) = seeded();

      denormalize(skeleton, store);
      store.clear();

      expect(store.size, 0);
      // Empty rather than `[null, null]`: a reference with no record behind it
      // is a hole, and a hole in a list is one fewer element.
      expect(denormalize(skeleton, store), isEmpty);
    });

    test(
      'drops a reference to a missing record rather than rendering a hole',
      () {
        final (:store, :skeleton) = seeded();

        store.evict('Order:8');

        expect(denormalize(skeleton, store), [list[0]]);
      },
    );

    // TS also keeps a literal `undefined`; Dart has only null, so the list
    // carries two nulls where TS carries null and undefined.
    test('leaves a literal null or undefined the server sent alone', () {
      final store = EntityStore();
      final staged = store.write(
        {
          'items': [
            {'id': 7, 'total': 1},
            null,
            null,
          ],
        },
        {
          ...schema,
          'Envelope': const EntityMeta(
            idField: '__never',
            fields: {'items': 'Order'},
          ),
        },
        'Envelope',
      );

      expect(store.read(staged.skeleton), {
        'items': [
          {'id': 7, 'total': 1},
          null,
          null,
        ],
      });

      store.evict('Order:7');

      expect(store.read(staged.skeleton), {
        'items': [null, null],
      });
    });

    test('restores a dropped element when the record comes back', () {
      final store = EntityStore();
      final staged = store.write(
        [
          {'id': 7, 'total': 1},
        ],
        schema,
        'Order',
      );

      store.evict('Order:7');
      expect(store.read(staged.skeleton), isEmpty);

      store.put('Order:7', {'id': 7, 'total': 5});
      expect(store.read(staged.skeleton), [
        {'id': 7, 'total': 5},
      ]);
    });

    test(
      'leaves an object field pointing at an evicted record as undefined',
      () {
        final (:store, :skeleton) = seeded();

        store.evict('Customer:c-3');

        expect(at(readList(store, skeleton), 0), {
          'id': 7,
          'total': 99,
          'customer': null,
        });
      },
    );
  });

  group('structural sharing', () {
    test('returns identical objects when nothing was written', () {
      final (:store, :skeleton) = seeded();

      final a = readList(store, skeleton);
      final b = readList(store, skeleton);

      expect(b, same(a));
      expect(b[0], same(a[0]));
      expect(at(b, 0)['customer'], same(at(a, 0)['customer']));
      expect(b[1], same(a[1]));
    });

    test(
      'keeps the container identity when a refetch returns the same data',
      () {
        final store = EntityStore();
        final first = store.write(list, schema, 'Order').skeleton;

        final before = readList(store, first);

        final second = store.write(list, schema, 'Order').skeleton;

        expect(second, isNot(same(first)));

        final after = readList(store, second, before);

        expect(after, same(before));
        expect(after[0], same(before[0]));
        expect(at(after, 0)['customer'], same(at(before, 0)['customer']));
      },
    );

    test('reuses every container a refetch did not change', () {
      final store = EntityStore();
      final first = store.write(list, schema, 'Order').skeleton;
      final before = readList(store, first);

      final changed = [
        list[0],
        {...list[1], 'total': 2},
      ];
      final second = store.write(changed, schema, 'Order').skeleton;
      final after = readList(store, second, before);

      expect(after, isNot(same(before)));
      expect(after[0], same(before[0]));
      expect(after[1], isNot(same(before[1])));
      expect(at(after, 1)['customer'], same(at(before, 1)['customer']));
      expect(at(after, 1)['total'], 2);
    });

    test('keeps the container identity for a wrapper carrying plain data', () {
      final store = EntityStore();

      // Rebuilt per call, because a refetch parses fresh JSON.
      Object? page() => jsonDecode(
        jsonEncode({
          'items': [
            {
              'id': 7,
              'total': 99,
              'customer': {'id': 'c-3', 'name': 'Ada'},
            },
            {
              'id': 8,
              'total': 1,
              'customer': {'id': 'c-4', 'name': 'Grace'},
            },
          ],
          'meta': {'page': 1, 'size': 20},
        }),
      );

      final first = store.write(page(), schema, 'Envelope').skeleton;
      final before = readMap(store, first);

      final second = store.write(page(), schema, 'Envelope').skeleton;
      final after = readMap(store, second, before);

      expect(after, same(before));
      expect(after['meta'], same(before['meta']));
    });

    test('returns a fresh container when no previous read is offered', () {
      final store = EntityStore();
      final first = store.write(list, schema, 'Order').skeleton;
      final before = store.read(first);

      final second = store.write(list, schema, 'Order').skeleton;

      expect(store.read(second), isNot(same(before)));
    });

    test('changes only the affected subtree when one entity is written', () {
      final (:store, :skeleton) = seeded();

      final before = readList(store, skeleton);

      store.put('Order:8', {'total': 2});

      final after = readList(store, skeleton);

      expect(after, isNot(same(before)));
      expect(after[0], same(before[0]));
      expect(at(after, 0)['customer'], same(at(before, 0)['customer']));
      expect(after[1], isNot(same(before[1])));
      expect(at(after, 1)['customer'], same(at(before, 1)['customer']));
      expect(at(after, 1)['total'], 2);
    });

    test('propagates a change through a nested entity to its holders', () {
      final (:store, :skeleton) = seeded();

      final before = readList(store, skeleton);

      store.put('Customer:c-3', {'name': 'Ada Lovelace'});

      final after = readList(store, skeleton);

      expect(after[0], isNot(same(before[0])));
      expect(at(after, 0)['customer'], isNot(same(at(before, 0)['customer'])));
      expect(after[1], same(before[1]));
    });

    test('propagates a change two hops away', () {
      final store = EntityStore();
      final staged = store.write(
        {
          'id': 7,
          'customer': {
            'id': 'c-3',
            'name': 'Ada',
            'orders': [
              {'id': 9, 'total': 1},
            ],
          },
        },
        schema,
        'Order',
      );

      final before = denormalize(staged.skeleton, store);

      store.put('Order:9', {'total': 2});

      final after = denormalize(staged.skeleton, store);

      expect(after, isNot(same(before)));
      expect(after, {
        'id': 7,
        'customer': {
          'id': 'c-3',
          'name': 'Ada',
          'orders': [
            {'id': 9, 'total': 2},
          ],
        },
      });
    });

    test('keeps identity for an unrelated skeleton over the same store', () {
      final (:store, :skeleton) = seeded();
      final detail = store
          .write({'id': 7, 'total': 99}, schema, 'Order')
          .skeleton;

      final listBefore = readList(store, skeleton);
      final detailBefore = denormalize(detail, store);

      store.put('Order:8', {'total': 3});

      expect(denormalize(detail, store), same(detailBefore));
      expect(readList(store, skeleton)[0], same(listBefore[0]));
    });
  });

  group('cycles', () {
    Seeded cyclic() {
      final order = <String, Object?>{'id': 7, 'total': 99};
      final customer = <String, Object?>{'id': 'c-3', 'name': 'Ada'};
      order['customer'] = customer;
      customer['orders'] = [order];

      final store = EntityStore();

      return (
        store: store,
        skeleton: store.write(order, schema, 'Order').skeleton,
      );
    }

    test('rebuilds the cycle as a cycle', () {
      final (:store, :skeleton) = cyclic();
      final order = readMap(store, skeleton);

      expect(order['id'], 7);
      expect(field(order, 'customer')['name'], 'Ada');
      expect(
        (field(order, 'customer')['orders']! as List<Object?>)[0],
        same(order),
      );
    });

    test('keeps the cyclic result stable across reads', () {
      final (:store, :skeleton) = cyclic();

      final a = readMap(store, skeleton);
      final b = readMap(store, skeleton);

      expect(b, same(a));
      expect(b['customer'], same(a['customer']));
    });

    test('recomputes a whole cycle when one of its members changes', () {
      final (:store, :skeleton) = cyclic();

      final before = readMap(store, skeleton);

      store.put('Customer:c-3', {'name': 'Grace'});

      final after = readMap(store, skeleton);

      expect(after, isNot(same(before)));
      expect(field(after, 'customer')['name'], 'Grace');
      expect(
        (field(after, 'customer')['orders']! as List<Object?>)[0],
        same(after),
      );
    });

    // Merging the two occurrences of Order:1 produces a record that references
    // itself. Normalization creates the cycle; the input never had one.
    test('handles a cycle that merging introduces', () {
      final store = EntityStore();
      final staged = store.write(
        {
          'id': 1,
          'related': [
            {'id': 1},
          ],
        },
        schema,
        'Order',
      );

      expect(store.size, 1);
      expect(store.getRecord('Order:1')?.data, {
        'id': 1,
        'related': [refTo('Order:1')],
      });

      final order = readMap(store, staged.skeleton);

      expect(order['id'], 1);
      expect((order['related']! as List<Object?>)[0], same(order));
    });

    // TS proves this with JSON.stringify; Dart's jsonEncode cannot encode a
    // EntityRef, so the walk below asserts the same property directly: no record's
    // data reaches itself through plain containers.
    test('stores each record acyclically even when the graph cycles', () {
      final (:store, skeleton: _) = cyclic();

      bool acyclic(Object? node, Set<Object> path) {
        if (node is! Map && node is! List) return true;
        if (!path.add(node!)) return false;
        final children = node is Map ? node.values : node as List<Object?>;
        final ok = children.every((child) => acyclic(child, path));
        path.remove(node);
        return ok;
      }

      for (final key in store.keys.toList()) {
        expect(
          acyclic(store.getRecord(key)?.data, Set<Object>.identity()),
          isTrue,
        );
      }
    });

    test('survives a cycle that closes through plain objects', () {
      final meta = <String, Object?>{'page': 1};
      meta['self'] = meta;

      final store = EntityStore();
      final staged = store.write(
        {
          'items': [
            {'id': 7, 'total': 1},
          ],
          'meta': meta,
        },
        schema,
        'Envelope',
      );

      final a = readMap(store, staged.skeleton);
      expect(field(a, 'meta')['self'], same(a['meta']));
      expect((a['items']! as List<Object?>)[0], {'id': 7, 'total': 1});

      store.put('Order:7', {'total': 2});

      final b = readMap(store, staged.skeleton);
      expect(at(b['items']! as List<Object?>, 0)['total'], 2);
      expect(field(b, 'meta')['self'], same(b['meta']));
    });

    test('keeps structural sharing intact around a plain-object cycle', () {
      final meta = <String, Object?>{'page': 1};
      meta['self'] = meta;

      final store = EntityStore();
      final staged = store.write(
        {
          'items': [
            {'id': 7, 'total': 1},
            {'id': 8, 'total': 2},
          ],
          'meta': meta,
        },
        schema,
        'Envelope',
      );

      final a = readMap(store, staged.skeleton);
      final b = readMap(store, staged.skeleton);

      expect(b, same(a));
      expect(b['items'], same(a['items']));
      expect(
        (b['items']! as List<Object?>)[0],
        same((a['items']! as List<Object?>)[0]),
      );
      expect(b['meta'], same(a['meta']));
    });

    test(
      'leaves a cycle that depends on nothing alone when an entity changes',
      () {
        final meta = <String, Object?>{'page': 1};
        meta['self'] = meta;

        final store = EntityStore();
        final staged = store.write(
          {
            'items': [
              {'id': 7, 'total': 1},
            ],
            'meta': meta,
          },
          schema,
          'Envelope',
        );

        final a = readMap(store, staged.skeleton);

        store.put('Order:7', {'total': 2});

        final b = readMap(store, staged.skeleton);

        expect(b, isNot(same(a)));
        expect(at(b['items']! as List<Object?>, 0)['total'], 2);
        expect(b['meta'], same(a['meta']));
        expect(field(b, 'meta')['self'], same(b['meta']));
      },
    );

    test('invalidates a cycle that does reach an entity', () {
      final store = EntityStore();
      final wrapper = <String, Object?>{
        'data': {'id': 7, 'total': 1},
      };
      wrapper['wrapper'] = wrapper;

      final staged = store.write({'wrapper': wrapper}, schema, 'Envelope');

      final a = readMap(store, staged.skeleton);
      expect(field(a, 'wrapper')['wrapper'], same(a['wrapper']));
      expect(field(a, 'wrapper')['data'], {'id': 7, 'total': 1});
      expect(denormalize(staged.skeleton, store), same(a));

      store.put('Order:7', {'total': 2});

      final b = readMap(store, staged.skeleton);

      expect(b['wrapper'], isNot(same(a['wrapper'])));
      expect(field(field(b, 'wrapper'), 'data')['total'], 2);
      expect(field(b, 'wrapper')['wrapper'], same(b['wrapper']));
      expect(denormalize(staged.skeleton, store), same(b));
    });

    test('keeps sharing when the cycle is the root of the skeleton', () {
      final store = EntityStore();
      final root = <String, Object?>{
        'data': {'id': 7, 'total': 1},
      };
      root['self'] = root;

      final staged = store.write(root, schema, 'Envelope');

      final a = readMap(store, staged.skeleton);
      expect(a['self'], same(a));
      expect(a['data'], {'id': 7, 'total': 1});
      expect(denormalize(staged.skeleton, store), same(a));

      store.put('Order:7', {'total': 2});

      final b = readMap(store, staged.skeleton);
      expect(b, isNot(same(a)));
      expect(field(b, 'data')['total'], 2);
      expect(b['self'], same(b));
      expect(denormalize(staged.skeleton, store), same(b));
    });
  });

  group('dependencies', () {
    test('reports every key a skeleton reaches, transitively', () {
      final (:store, :skeleton) = seeded();

      expect(store.dependencies(skeleton).toList()..sort(), [
        'Customer:c-3',
        'Customer:c-4',
        'Order:7',
        'Order:8',
      ]);
    });

    test('reports a key the store does not hold yet', () {
      final store = EntityStore();
      final staged = store.write({'id': 7}, schema, 'Order');

      store.evict('Order:7');

      expect(store.dependencies(staged.skeleton).toList(), ['Order:7']);
    });
  });

  group('the overlay seam', () {
    test('reads a record through the layer rather than from base', () {
      final store = EntityStore();
      store.put('Order:7', {'id': 7, 'status': 'open'});

      store.overlays = StubLayer({
        'Order:7': {'id': 7, 'status': 'shipped'},
      });

      expect(store.read(makeRef('Order:7')), {'id': 7, 'status': 'shipped'});
    });

    test('rehydrates a layer-deleted record as a hole, exactly as an eviction does', () {
      final store = EntityStore();
      final staged = store.write(
        [
          {'id': 7},
          {'id': 8},
        ],
        const {'Order': EntityMeta(idField: 'id')},
        'Order',
      );

      store.overlays = StubLayer({'Order:7': null});

      expect(store.read(staged.skeleton), [
        {'id': 8},
      ]);
    });

    test('stamps OPTIMISTIC on a held record and on nothing else', () {
      final store = EntityStore();
      store.put('Order:7', {'id': 7});
      store.put('Order:8', {'id': 8});

      store.overlays = StubLayer({
        'Order:7': {'id': 7, 'status': 'shipped'},
      });

      final seven = readMap(store, makeRef('Order:7'));
      final eight = readMap(store, makeRef('Order:8'));

      expect(isOptimistic(seven), isTrue);
      expect(isOptimistic(eight), isFalse);
      // Invisible to everything that walks a map by its keys.
      expect(seven.keys.toList(), ['id', 'status']);
      expect(jsonEncode(seven), '{"id":7,"status":"shipped"}');
    });

    test(
      'touch drops memos for the named keys and leaves the rest sharing',
      () {
        final store = EntityStore();
        final staged = store.write(
          [
            {'id': 7},
            {'id': 8},
          ],
          const {'Order': EntityMeta(idField: 'id')},
          'Order',
        );
        final before = readList(store, staged.skeleton);

        store.touch(['Order:7']);
        final after = readList(store, staged.skeleton);

        expect(after[1], same(before[1]));
        expect(after, isNot(same(before)));
      },
    );

    test('touch on a key nothing ever read bumps no version', () {
      final store = EntityStore();

      expect(store.version, 0);

      store.touch(['Never:Seen']);

      expect(store.version, 0);
    });
  });
}
