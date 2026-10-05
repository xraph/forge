// Ported from packages/client-core/__tests__/registry.test.ts.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/schema.dart';

const listSpec = QuerySpec(
  operation: 'orderList',
  args: TagContext(query: {'status': 'open'}),
  provides: ['Order[]'],
);

final String key = operationQueryKey(listSpec.operation, listSpec.args);

QuerySpec withArgs(QuerySpec spec, TagContext args) => QuerySpec(
  operation: spec.operation,
  args: args,
  provides: spec.provides,
  key: spec.key,
);

void main() {
  group('QueryRegistry mounting', () {
    test('ref-counts one entry for a query mounted from several places', () {
      final registry = QueryRegistry();

      final first = registry.mount(listSpec);
      final second = registry.mount(listSpec);

      expect(registry.size, 1);
      expect(registry.get(key)?.mounts, 2);
      expect(registry.queriesFor('Order[]'), hasLength(1));

      first();

      expect(registry.get(key)?.mounts, 1);
      expect(registry.queriesFor('Order[]'), hasLength(1));

      second();

      expect(registry.get(key)?.mounts, 0);
      expect(registry.queriesFor('Order[]'), isEmpty);
      expect(registry.indexedTags, 0);
    });

    test('does not double-decrement when an unmount is called twice', () {
      final registry = QueryRegistry();

      registry.mount(listSpec);
      final unmount = registry.mount(listSpec);

      unmount();
      unmount();

      expect(registry.get(key)?.mounts, 1);
      expect(registry.queriesFor('Order[]'), hasLength(1));
    });

    test('keys queries by operation and arguments, not by identity', () {
      final registry = QueryRegistry();

      registry.mount(
        withArgs(listSpec, const TagContext(query: {'status': 'open'})),
      );
      registry.mount(
        withArgs(listSpec, const TagContext(query: {'status': 'closed'})),
      );

      expect(registry.size, 2);
      expect(registry.queriesFor('Order[]'), hasLength(2));
    });

    test(
      'resolves provides templates against the query arguments at mount',
      () {
        final registry = QueryRegistry();

        registry.mount(
          const QuerySpec(
            operation: 'orderGet',
            args: TagContext(path: {'id': 7}),
            provides: ['Order:{id}'],
          ),
        );

        expect(registry.queriesFor('Order:7'), hasLength(1));
      },
    );

    test('remembers an unmounted query without indexing it', () {
      final registry = QueryRegistry();

      registry.mount(listSpec)();

      expect(registry.size, 1);
      expect(registry.mounted, 0);
      expect(registry.indexedTags, 0);
    });

    test('drops a query from the registry and from every tag', () {
      final registry = QueryRegistry();

      registry.mount(listSpec);

      expect(registry.drop(key), isTrue);
      expect(registry.size, 0);
      expect(registry.indexedTags, 0);
      expect(registry.drop(key), isFalse);
    });
  });

  group('QueryRegistry settle', () {
    test('adopts the entity keys the response normalized to as tags', () {
      final registry = QueryRegistry();
      final store = EntityStore();

      registry.mount(listSpec);

      final staged = store.write(
        [
          {
            'id': 7,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
          {'id': 8},
        ],
        schema,
        'Order',
      );

      registry.settle(key, SettleResult(deps: staged.deps));

      expect(registry.queriesFor('Order:7'), hasLength(1));
      expect(registry.queriesFor('Customer:c-3'), hasLength(1));
      expect(registry.queriesFor('Order[]'), hasLength(1));
      expect(registry.get(key)?.tags, {
        'Order[]',
        'Order:7',
        'Order:8',
        'Customer:c-3',
      });
    });

    test('unindexes a tag a later response no longer provides', () {
      final registry = QueryRegistry();

      registry.mount(listSpec);
      registry.settle(key, const SettleResult(deps: ['Order:7', 'Order:8']));
      registry.settle(key, const SettleResult(deps: ['Order:8']));

      expect(registry.queriesFor('Order:7'), isEmpty);
      expect(registry.queriesFor('Order:8'), hasLength(1));
      expect(registry.queriesFor('Order[]'), hasLength(1));
    });

    test('resolves provides templates naming the response, and reports what cannot resolve', () {
      final unresolved = <String>[];
      final registry = QueryRegistry(
        onUnresolved: (template, _) => unresolved.add(template),
      );

      registry.mount(
        const QuerySpec(
          operation: 'orderCreate',
          provides: ['Order:{res.id}', 'Customer:{res.customerId}'],
          key: 'k',
        ),
      );

      expect(registry.get('k')?.tags, isEmpty);
      expect(unresolved, isEmpty);

      registry.settle('k', const SettleResult(response: {'id': 9}));

      expect(registry.queriesFor('Order:9'), hasLength(1));
      expect(unresolved, ['Customer:{res.customerId}']);
    });

    test(
      'does not report an item template the settled deps already satisfy',
      () {
        final unresolved = <String>[];
        final registry = QueryRegistry(
          onUnresolved: (template, _) => unresolved.add(template),
        );

        registry.mount(
          const QuerySpec(
            operation: 'orderPage',
            provides: ['Order:{id}', 'Order[]'],
            key: 'p',
          ),
        );
        registry.settle(
          'p',
          const SettleResult(
            response: {
              'items': [
                {'id': 7},
              ],
              'total': 1,
            },
            deps: ['Order:7'],
          ),
        );

        expect(unresolved, isEmpty);
        expect(registry.queriesFor('Order:7'), hasLength(1));
        expect(registry.queriesFor('Order[]'), hasLength(1));
      },
    );

    test(
      'still reports a template naming an entity the response never normalized',
      () {
        final unresolved = <String>[];
        final registry = QueryRegistry(
          onUnresolved: (template, _) => unresolved.add(template),
        );

        registry.mount(
          const QuerySpec(
            operation: 'orderPage',
            provides: ['Customer:{customerId}'],
            key: 'p',
          ),
        );
        registry.settle(
          'p',
          const SettleResult(
            response: {
              'items': [
                {'id': 7},
              ],
              'total': 1,
            },
            deps: ['Order:7'],
          ),
        );

        expect(unresolved, ['Customer:{customerId}']);
      },
    );

    test(
      're-indexes a query that settles while unmounted, without indexing it',
      () {
        final registry = QueryRegistry();

        final unmount = registry.mount(listSpec);
        unmount();
        registry.settle(key, const SettleResult(deps: ['Order:7']));

        expect(registry.indexedTags, 0);
        expect(registry.get(key)?.tags.contains('Order:7'), isTrue);

        registry.mount(listSpec);

        expect(registry.queriesFor('Order:7'), hasLength(1));
      },
    );

    test('ignores a settle for a query it does not know', () {
      final registry = QueryRegistry();

      expect(
        () => registry.settle('nope', const SettleResult(deps: ['Order:7'])),
        returnsNormally,
      );
    });

    test('keeps the previous value when settle omits one', () {
      final registry = QueryRegistry();

      registry.mount(listSpec);
      registry.settle(key, const SettleResult.withValue([1, 2]));
      registry.settle(key, const SettleResult(deps: ['Order:7']));

      expect(registry.get(key)?.value, [1, 2]);
    });
  });

  group('QueryRegistry invalidation stamps', () {
    test(
      'remembers an invalidation that arrived while the query was unmounted',
      () {
        final registry = QueryRegistry();
        var staleCalls = 0;

        registry.onStale = (_) => staleCalls++;

        final unmount = registry.mount(listSpec);
        registry.settle(key, const SettleResult(deps: ['Order:7']));
        unmount();

        registry.invalidated(['Order:7']);
        expect(staleCalls, 0);

        registry.mount(listSpec);

        expect(registry.get(key)?.stale, isTrue);
        expect(staleCalls, 1);
      },
    );

    test('does not stamp a tag no remembered query carries', () {
      final registry = QueryRegistry();

      registry.mount(listSpec)();
      registry.settle(key, const SettleResult(deps: ['Order:7']));

      registry.invalidated(['Order:7', 'Order:8', 'Order:9', 'Customer:c-3']);

      expect(registry.stampedTags, 1);

      for (var id = 100; id < 200; id++) {
        registry.invalidated(['Order:$id']);
      }

      expect(registry.stampedTags, 1);
    });

    test('forgets a stamp when the last query carrying its tag is dropped', () {
      final registry = QueryRegistry();

      registry.mount(listSpec)();
      registry.mount(
        const QuerySpec(
          operation: 'orderDetail',
          args: TagContext(path: {'id': 7}),
          key: 'detail',
        ),
      )();
      registry.settle(key, const SettleResult(deps: ['Order:7']));
      registry.settle('detail', const SettleResult(deps: ['Order:7']));

      registry.invalidated(['Order:7']);
      expect(registry.stampedTags, 1);

      registry.drop('detail');
      expect(registry.stampedTags, 1);

      registry.drop(key);
      expect(registry.stampedTags, 0);

      registry.mount(listSpec);
      expect(registry.get(key)?.stale, isFalse);
    });

    test('forgets a stamp for a tag a later response stopped providing', () {
      final registry = QueryRegistry();

      registry.mount(listSpec);
      registry.settle(key, const SettleResult(deps: ['Order:7']));
      registry.invalidated(['Order:7']);

      expect(registry.stampedTags, 1);

      registry.settle(key, const SettleResult(deps: ['Order:8']));

      expect(registry.stampedTags, 0);
    });

    test('clears the stamps and the carrier counts together', () {
      final registry = QueryRegistry();

      registry.mount(listSpec);
      registry.settle(key, const SettleResult(deps: ['Order:7']));
      registry.invalidated(['Order:7']);
      registry.clear();

      expect(registry.stampedTags, 0);

      registry.invalidated(['Order:7']);
      expect(registry.stampedTags, 0);
    });
  });

  group('QueryRegistry dispatch stamps', () {
    test('marks a response stale when the invalidation landed after it was dispatched', () {
      final registry = QueryRegistry();
      final unmount = registry.mount(listSpec);

      unmount();

      final startedAt = registry.stamp;

      registry.invalidated(['Order[]']);
      registry.settle(
        key,
        SettleResult.withValue(const ['pre-write'], startedAt: startedAt),
      );

      expect(registry.get(key)?.stale, isTrue);

      var staleCalls = 0;
      registry.onStale = (_) => staleCalls++;
      registry.mount(listSpec);

      expect(staleCalls, 1);
    });

    test('leaves a response dispatched after the invalidation current', () {
      final registry = QueryRegistry();

      registry.mount(listSpec)();
      registry.invalidated(['Order[]']);

      registry.settle(
        key,
        SettleResult.withValue(const ['post-write'], startedAt: registry.stamp),
      );

      expect(registry.get(key)?.stale, isFalse);
    });

    test('stamps the reading now when the caller has no request behind it', () {
      final registry = QueryRegistry();

      registry.mount(listSpec)();
      registry.invalidated(['Order[]']);
      registry.settle(key, const SettleResult.withValue(['placed']));

      expect(registry.get(key)?.stale, isFalse);
    });
  });

  group('settling with tags supplied', () {
    test(
      'uses the supplied tags instead of resolving provides against a response',
      () {
        final registry = QueryRegistry();

        registry.mount(
          const QuerySpec(
            operation: 'orderList',
            args: TagContext.empty,
            provides: ['Order:{res.id}'],
          ),
        );
        registry.settle(
          operationQueryKey('orderList', TagContext.empty),
          const SettleResult(tags: ['Order:7'], deps: ['Order:7']),
        );

        expect(registry.queriesFor('Order:7').map((entry) => entry.operation), [
          'orderList',
        ]);
      },
    );

    test('still unions the supplied tags with the entity dependencies', () {
      final registry = QueryRegistry();

      registry.mount(
        const QuerySpec(operation: 'orderList', args: TagContext.empty),
      )();
      registry.settle(
        operationQueryKey('orderList', TagContext.empty),
        const SettleResult(tags: ['Order[]'], deps: ['Order:1']),
      );

      expect(
        registry
            .get(operationQueryKey('orderList', TagContext.empty))
            ?.tags
            .toList()
          ?..sort(),
        ['Order:1', 'Order[]'],
      );
    });

    test('reports no unresolved template when tags are supplied', () {
      final unresolved = <String>[];
      final registry = QueryRegistry(
        onUnresolved: (template, _) => unresolved.add(template),
      );

      registry.mount(
        const QuerySpec(
          operation: 'orderList',
          args: TagContext.empty,
          provides: ['Order:{res.id}'],
        ),
      )();
      registry.settle(
        operationQueryKey('orderList', TagContext.empty),
        const SettleResult(tags: ['Order:7']),
      );

      expect(unresolved, isEmpty);
    });
  });
}
