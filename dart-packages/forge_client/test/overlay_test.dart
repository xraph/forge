// Ported from packages/client-core/__tests__/overlay.test.ts, part one: the
// overlay stack on its own. The cases that drive it through the query cache
// are in overlay_optimistic_test.dart, written with the cache task.
//
// The TS OPTIMISTIC symbol is `isOptimistic(record)`; Dart maps carry no
// hidden keys, so plain `equals` checks need no `objectContaining`.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

const none = TagContext.empty;

({EntityStore store, OverlayStack stack}) host() {
  final store = EntityStore();
  final stack = OverlayStack(store);

  store.overlays = stack;

  return (store: store, stack: stack);
}

MergePatch merge(Json fields) => MergePatch(fields);

MergePatch compute(MergeSource fn) => MergePatch.computed(fn);

Json? order(EntityStore store, EntityKey key) =>
    store.read(makeRef(key)) as Json?;

List<Object?> prepend(Object? made, Object? current, TagContext _) => [
  made,
  ...current! as List<Object?>,
];

const patchMeta = OperationMeta(
  id: 'orderPatch',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  invalidates: ['Order:{id}', 'Order[]'],
);

void main() {
  group('the entity plane', () {
    test('shows a merged patch over the base record', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7, 'status': 'open', 'total': 99});

      stack.add({
        'Order:7': merge({'status': 'shipped'}),
      });

      expect(order(store, 'Order:7'), {
        'id': 7,
        'status': 'shipped',
        'total': 99,
      });
      expect(isOptimistic(order(store, 'Order:7')), isTrue);
    });

    test('rebases: dropping the FIRST of two overlays keeps the second', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7, 'status': 'open', 'note': ''});

      final first = stack.add({
        'Order:7': merge({'status': 'shipped'}),
      });
      stack.add({
        'Order:7': merge({'note': 'gift'}),
      });

      stack.take(first);

      expect(order(store, 'Order:7'), {
        'id': 7,
        'status': 'open',
        'note': 'gift',
      });
    });

    test('composes computed patches, and recomputes them on a refold', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7, 'likes': 0});

      final first = stack.add({
        'Order:7': compute((prev) => {'likes': (prev['likes']! as int) + 1}),
      });
      stack.add({
        'Order:7': compute((prev) => {'likes': (prev['likes']! as int) + 1}),
      });

      expect(order(store, 'Order:7'), {'id': 7, 'likes': 2});

      stack.take(first);

      expect(order(store, 'Order:7'), {'id': 7, 'likes': 1});
    });

    test(
      'refolds over a base write, so a stream frame lands underneath the patch',
      () {
        final (:store, :stack) = host();
        store.put('Order:7', {'id': 7, 'status': 'open', 'total': 99});

        stack.add({
          'Order:7': merge({'status': 'shipped'}),
        });
        store.put('Order:7', {'total': 120}, 1);

        expect(order(store, 'Order:7'), {
          'id': 7,
          'status': 'shipped',
          'total': 120,
        });
      },
    );

    test('is a no-op over a record an evicting frame removed', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7, 'status': 'open'});

      stack.add({
        'Order:7': merge({'status': 'shipped'}),
      });
      store.evict('Order:7', 1);

      expect(order(store, 'Order:7'), isNull);
    });

    test('deletes, and restores on drop', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7});

      final id = stack.add({'Order:7': const DeletePatch()});
      expect(order(store, 'Order:7'), isNull);

      stack.take(id);
      expect(order(store, 'Order:7'), {'id': 7});
      expect(isOptimistic(order(store, 'Order:7')), isFalse);
    });

    test('creates a record that base never held', () {
      final (:store, :stack) = host();

      stack.add({
        'Order:~opt1': const CreatePatch({'id': '~opt1', 'total': 99}),
      });

      expect(order(store, 'Order:~opt1'), {'id': '~opt1', 'total': 99});
      expect(store.has('Order:~opt1'), isFalse);
    });

    test('keeps the identity of records no overlay touches', () {
      final (:store, :stack) = host();
      final staged = store.write(
        [
          {'id': 7},
          {'id': 8},
        ],
        const {'Order': EntityMeta(idField: 'id')},
        'Order',
      );
      final before = store.read(staged.skeleton)! as List<Object?>;

      stack.add({
        'Order:7': merge({'status': 'shipped'}),
      });
      final after = store.read(staged.skeleton)! as List<Object?>;

      expect(after[1], same(before[1]));
      expect(after[0], isNot(same(before[0])));
    });

    test('reports a throwing compute patch and treats it as no change', () {
      final reports = <(Object, String)>[];
      final store = EntityStore();
      final stack = OverlayStack(
        store,
        (error, context) => reports.add((error, context)),
      );
      store.overlays = stack;
      store.put('Order:7', {'id': 7, 'status': 'open'});

      stack.add({'Order:7': compute((_) => throw StateError('boom'))});

      expect(order(store, 'Order:7'), {'id': 7, 'status': 'open'});
      expect(reports.single.$1, isA<StateError>());
      expect(reports.single.$2, 'optimistic');
    });

    test('promote writes merges into base and reports delete targets', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7, 'status': 'open'});
      store.put('Order:8', {'id': 8});

      final id = stack.add({
        'Order:7': merge({'status': 'shipped'}),
        'Order:8': const DeletePatch(),
        'Order:~opt1': const CreatePatch({'id': '~opt1'}),
      });

      final buried = stack.promote(stack.take(id)!);

      expect(store.getRecord('Order:7')?.data, {'id': 7, 'status': 'shipped'});
      expect(store.has('Order:8'), isFalse);
      expect(store.has('Order:~opt1'), isFalse);
      expect(buried, ['Order:8']);
    });

    test(
      'promote evaluates a computed source against raw base, never the fold',
      () {
        final (:store, :stack) = host();
        store.put('Order:7', {'id': 7, 'likes': 0});

        final first = stack.add({
          'Order:7': compute((prev) => {'likes': (prev['likes']! as int) + 1}),
        });
        stack.add({
          'Order:7': compute((prev) => {'likes': (prev['likes']! as int) + 1}),
        });

        expect(order(store, 'Order:7'), {'id': 7, 'likes': 2});

        stack.promote(stack.take(first)!);

        expect(store.getRecord('Order:7')?.data, {'id': 7, 'likes': 1});
        expect(order(store, 'Order:7'), {'id': 7, 'likes': 2});
      },
    );

    test('promote never invokes a computed source when base is gone: merge over an absent record is a no-op', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7, 'likes': 0});
      var calls = 0;

      final id = stack.add({
        'Order:7': compute((prev) {
          calls++;

          return {'likes': (prev['likes']! as int) + 1};
        }),
      });
      store.evict('Order:7');

      final entry = stack.take(id)!;
      stack.promote(entry);

      expect(calls, 0);
      expect(store.has('Order:7'), isFalse);
    });

    test('clear drops every overlay', () {
      final (:store, :stack) = host();
      store.put('Order:7', {'id': 7, 'status': 'open'});

      stack.add({
        'Order:7': merge({'status': 'shipped'}),
      });
      stack.clear();

      expect(stack.empty, isTrue);
      expect(order(store, 'Order:7'), {'id': 7, 'status': 'open'});
    });
  });

  group('affects: a tag match only counts when the overlay can place', () {
    test('does not flag a list a plain update overlay does not reach', () {
      final (store: _, :stack) = host();
      final registry = QueryRegistry();
      registry.mount(
        const QuerySpec(
          operation: 'orderList',
          provides: ['Order[]'],
          key: 'orderList',
        ),
      );
      final entry = registry.get('orderList');

      stack.add(
        {
          'Order:7': merge({'status': 'shipped'}),
        },
        null,
        ['Order[]'],
      );

      expect(stack.affects(entry), isFalse);
    });

    test('flags a list a create overlay with a place callback targets', () {
      final (store: _, :stack) = host();
      final registry = QueryRegistry();
      registry.mount(
        const QuerySpec(
          operation: 'orderList',
          provides: ['Order[]'],
          key: 'orderList',
        ),
      );
      final entry = registry.get('orderList');

      stack.add(
        {
          'Order:~opt1': const CreatePatch({'id': '~opt1'}),
        },
        {'Order[]': prepend},
        ['Order[]'],
        'Order:~opt1',
      );

      expect(stack.affects(entry), isTrue);
    });

    test('is false on an empty stack without even looking at the entry', () {
      final (store: _, :stack) = host();

      expect(stack.affects(null), isFalse);
    });
  });

  group('deriving the target from what a mutation invalidates', () {
    test('finds the one entity-key tag', () {
      expect(targetOf(patchMeta, const TagContext(path: {'id': 7})), 'Order:7');
    });

    test('says create when no tag names an entity key', () {
      const create = OperationMeta(
        id: 'c',
        method: 'POST',
        path: '/orders',
        entity: 'Order',
        invalidates: ['Order[]'],
      );

      expect(targetOf(create, none), isNull);
    });

    test('is not fooled by a parameterised COLLECTION tag', () {
      const archive = OperationMeta(
        id: 'a',
        method: 'POST',
        path: '/orders/archive',
        entity: 'Order',
        invalidates: ['Order[]:{req.archived}'],
      );

      expect(
        targetOf(archive, const TagContext(body: {'archived': true})),
        isNull,
      );
    });

    test('reports ambiguity rather than guessing between two entities', () {
      const transfer = OperationMeta(
        id: 't',
        method: 'POST',
        path: '/orders/{id}/transfer',
        entity: 'Order',
        invalidates: ['Order:{id}', 'Customer:{req.customerId}'],
      );

      expect(
        () => targetOf(
          transfer,
          const TagContext(path: {'id': 7}, body: {'customerId': 3}),
        ),
        throwsA(
          isA<AmbiguousTargetError>().having((error) => error.keys, 'keys', [
            'Order:7',
            'Customer:3',
          ]),
        ),
      );
    });

    test('ignores a tag that resolves to nothing', () {
      expect(targetOf(patchMeta, none), isNull);
    });
  });

  group('reading the stack', () {
    test('lists the live overlays in push order', () {
      final (store: _, :stack) = host();

      final first = stack.add(
        {
          'Order:1': merge({'total': 11}),
        },
        null,
        ['Order[]'],
      );
      final second = stack.add(
        {
          'Order:2': merge({'total': 22}),
        },
        null,
        ['Order:2'],
        'Order:~opt1',
      );

      final live = stack.list();

      expect(live.map((entry) => entry.id), [first, second]);
      expect(live[0].patches.keys.toList(), ['Order:1']);
      expect(live[1].tags, ['Order:2']);
      expect(live[1].created, 'Order:~opt1');
    });

    test('drops an overlay from the listing once it is taken', () {
      final (store: _, :stack) = host();

      final first = stack.add({
        'Order:1': merge({'total': 11}),
      });
      final second = stack.add({
        'Order:2': merge({'total': 22}),
      });

      stack.take(first);

      expect(stack.list().map((entry) => entry.id), [second]);
    });

    test('hands out a copy, so a reader cannot reorder the stack', () {
      final (store: _, :stack) = host();

      stack.add({
        'Order:1': merge({'total': 11}),
      });

      final live = stack.list();

      expect(live.removeLast, throwsUnsupportedError);
      expect(stack.list(), hasLength(1));
    });
  });
}
