// Ported from packages/client-core/__tests__/invalidate.test.ts.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

/// The whole chunk under test with no network and no timers.
final class Harness {
  Harness({
    Scheduler? scheduler,
    bool defaultScheduler = false,
    void Function(String template, String context)? onUnresolved,
    void Function(Object error, String context)? onError,
    void Function(QueryEntry entry, List<Object?> value)? onPlace,
    void Function(List<QueryEntry> batch)? execute,
  }) {
    invalidator = Invalidator(
      registry,
      execute:
          execute ??
          (batch) => batches.add([for (final entry in batch) entry.key]),
      scheduler: defaultScheduler ? null : (scheduler ?? this.scheduler),
      onUnresolved: onUnresolved ?? (_, _) {},
      onError: onError,
      onPlace: onPlace,
    );
  }

  final List<List<String>> batches = [];
  final ManualScheduler scheduler = ManualScheduler();
  final QueryRegistry registry = QueryRegistry();
  late final Invalidator invalidator;

  ({String key, Unmount unmount}) mount(
    String operation,
    List<String> provides, [
    TagContext args = TagContext.empty,
  ]) {
    final key = operationQueryKey(operation, args);
    final unmount = registry.mount(
      QuerySpec(operation: operation, args: args, provides: provides),
    );

    return (key: key, unmount: unmount);
  }
}

void main() {
  group('invalidation', () {
    test('hits exactly the queries carrying an invalidated tag', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);
      final detail = h.mount('orderGet', [
        'Order:7',
      ], const TagContext(path: {'id': 7}));
      final unrelated = h.mount('customerList', ['Customer[]']);

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
      ]);
      expect(h.invalidator.registry.get(detail.key)?.stale, isFalse);
      expect(h.invalidator.registry.get(unrelated.key)?.stale, isFalse);
    });

    test('resolves the mutation template before matching', () {
      final h = Harness();

      final customer = h.mount('customerGet', [
        'Customer:{id}',
      ], const TagContext(path: {'id': 'c-3'}));
      h.mount('customerGet', [
        'Customer:{id}',
      ], const TagContext(path: {'id': 'c-9'}));

      h.invalidator.settled(
        const MutationSettled(
          invalidates: ['Customer:{req.customerId}'],
          args: TagContext(body: {'customerId': 'c-3'}),
        ),
      );
      h.scheduler.flush();

      expect(h.batches, [
        [customer.key],
      ]);
    });

    test(
      'skips a tag that resolves to nothing and reports it, keeping the rest',
      () {
        final reported = <(String, String)>[];
        final h = Harness(
          onUnresolved: (template, context) =>
              reported.add((template, context)),
        );

        final list = h.mount('orderList', ['Order[]']);

        h.invalidator.settled(
          const MutationSettled(
            invalidates: ['Order[]', 'Customer:{customerId}'],
          ),
        );
        h.scheduler.flush();

        expect(reported, contains(('Customer:{customerId}', 'invalidates')));
        expect(h.invalidator.registry.queriesFor('Customer:'), isEmpty);
        expect(h.batches, [
          [list.key],
        ]);
      },
    );
  });

  group('coalescing', () {
    test('turns N invalidated queries in one tick into one batch', () {
      final h = Harness();

      final a = h.mount('orderList', ['Order[]']);
      final b = h.mount('orderCount', ['Order[]']);
      final c = h.mount('orderStats', ['Order[]']);

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));

      expect(h.batches, isEmpty);
      expect(h.scheduler.pending, isTrue);

      h.scheduler.flush();

      expect(h.batches, hasLength(1));
      expect(h.batches[0], [a.key, b.key, c.key]);
    });

    test('refetches a query hit by two tags once', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);

      h.registry.settle(list.key, const SettleResult(deps: ['Order:7']));
      h.invalidator.settled(
        const MutationSettled(invalidates: ['Order[]', 'Order:7']),
      );
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
      ]);
    });

    test('coalesces several mutations settling in the same tick', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
      ]);
    });

    test('schedules a fresh batch for the next tick', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.scheduler.flush();
      h.registry.settle(list.key);

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
        [list.key],
      ]);
    });

    test('runs one batch per microtask under the default scheduler', () async {
      final h = Harness(defaultScheduler: true);

      final a = h.mount('orderList', ['Order[]']);
      final b = h.mount('orderCount', ['Order[]']);

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));

      expect(h.batches, isEmpty);

      await Future<void>.value();

      expect(h.batches, [
        [a.key, b.key],
      ]);
    });

    test(
      'drops a query that unmounted between the invalidation and the flush',
      () {
        final h = Harness();

        final staying = h.mount('orderList', ['Order[]']);
        final leaving = h.mount('orderCount', ['Order[]']);

        h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
        leaving.unmount();
        h.scheduler.flush();

        expect(h.batches, [
          [staying.key],
        ]);
      },
    );
  });

  group('unmounted queries', () {
    test('refetches on the next mount, and not before', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);

      h.registry.settle(list.key, const SettleResult.withValue(['a']));
      list.unmount();

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.scheduler.flush();

      expect(h.batches, isEmpty);
      expect(h.scheduler.pending, isFalse);

      h.mount('orderList', ['Order[]']);
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
      ]);
      expect(h.registry.get(list.key)?.stale, isTrue);
    });

    test('does not refetch a query that mounts having missed nothing', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);

      h.registry.settle(list.key);
      list.unmount();

      h.invalidator.settled(const MutationSettled(invalidates: ['Customer[]']));
      h.mount('orderList', ['Order[]']);
      h.scheduler.flush();

      expect(h.batches, isEmpty);
    });

    test('refetches once no matter how many invalidations it missed', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);

      h.registry.settle(list.key, const SettleResult(deps: ['Order:7']));
      list.unmount();

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.invalidator.settled(const MutationSettled(invalidates: ['Order:7']));
      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));

      h.mount('orderList', ['Order[]']);
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
      ]);
    });
  });

  group('placement', () {
    const created = {'id': 9, 'status': 'open'};

    ({Harness h, ({String key, Unmount unmount}) list}) placed() {
      final h = Harness();
      final list = h.mount('orderList', [
        'Order[]',
      ], const TagContext(query: {'status': 'open'}));

      h.registry.settle(
        list.key,
        const SettleResult.withValue([
          {'id': 7},
        ]),
      );

      return (h: h, list: list);
    }

    List<Object?> prepend(Object? entity, Object? current, TagContext _) => [
      entity,
      ...current! as List<Object?>,
    ];

    test('skips the refetch when a callback returns a list', () {
      final (:h, :list) = placed();

      h.invalidator.settled(
        MutationSettled(
          invalidates: const ['Order[]'],
          response: created,
          place: {'Order[]': prepend},
        ),
      );
      h.scheduler.flush();

      expect(h.batches, isEmpty);
      expect(h.registry.get(list.key)?.value, [
        created,
        {'id': 7},
      ]);
      expect(h.registry.get(list.key)?.stale, isFalse);
    });

    test(
      'hands the callback the query arguments, so it can decline per query',
      () {
        final h = Harness();

        final open = h.mount('orderList', [
          'Order[]',
        ], const TagContext(query: {'status': 'open'}));
        final closed = h.mount('orderList', [
          'Order[]',
        ], const TagContext(query: {'status': 'closed'}));

        h.registry.settle(open.key, const SettleResult.withValue(<Object?>[]));
        h.registry.settle(
          closed.key,
          const SettleResult.withValue(<Object?>[]),
        );

        h.invalidator.settled(
          MutationSettled(
            invalidates: const ['Order[]'],
            response: created,
            place: {
              'Order[]': (entity, current, args) =>
                  args.query['status'] ==
                      (entity! as Map<String, Object?>)['status']
                  ? [...current! as List<Object?>, entity]
                  : null,
            },
          ),
        );
        h.scheduler.flush();

        expect(h.batches, [
          [closed.key],
        ]);
        expect(h.registry.get(open.key)?.value, [created]);
      },
    );

    test('falls back to a refetch when the callback returns undefined', () {
      final (:h, :list) = placed();

      h.invalidator.settled(
        MutationSettled(
          invalidates: const ['Order[]'],
          response: created,
          place: {'Order[]': (_, _, _) => null},
        ),
      );
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
      ]);
    });

    test(
      'refetches when only some of the tags that matched have a callback',
      () {
        final (:h, :list) = placed();

        h.registry.settle(
          list.key,
          const SettleResult.withValue(
            [
              {'id': 7},
            ],
            deps: ['Order:7'],
          ),
        );

        h.invalidator.settled(
          MutationSettled(
            invalidates: const ['Order[]', 'Order:7'],
            response: created,
            place: {'Order[]': prepend},
          ),
        );
        h.scheduler.flush();

        expect(h.batches, [
          [list.key],
        ]);
      },
    );

    test('does not take the batch down when a callback throws', () {
      final errors = <(Object, String)>[];
      final h = Harness(
        onError: (error, context) => errors.add((error, context)),
      );

      final thrower = h.mount('orderList', ['Order[]']);
      final other = h.mount('orderCount', ['Order[]']);

      h.registry.settle(thrower.key, const SettleResult.withValue(<Object?>[]));
      h.registry.settle(other.key, const SettleResult.withValue(<Object?>[]));

      h.invalidator.settled(
        MutationSettled(
          invalidates: const ['Order[]'],
          response: created,
          place: {
            'Order[]': (_, current, _) {
              if ((current! as List<Object?>).isEmpty) throw StateError('boom');

              return [];
            },
          },
        ),
      );
      h.scheduler.flush();

      expect(errors, hasLength(2));
      expect(errors[0].$2, 'place Order[]');
      expect(h.batches, [
        [thrower.key, other.key],
      ]);
    });

    test('reports what it placed', () {
      final placedCalls = <(QueryEntry, List<Object?>)>[];
      final h = Harness(
        onPlace: (entry, value) => placedCalls.add((entry, value)),
      );
      final list = h.mount('orderList', ['Order[]']);

      h.registry.settle(list.key, const SettleResult.withValue(<Object?>[]));
      h.invalidator.settled(
        MutationSettled(
          invalidates: const ['Order[]'],
          created: created,
          place: {
            'Order[]': (entity, _, _) => [entity],
          },
        ),
      );

      expect(placedCalls, hasLength(1));
      expect(placedCalls[0].$1.key, list.key);
      expect(placedCalls[0].$2, [created]);
    });

    test('does not refetch a placed query when it mounts again', () {
      final (:h, :list) = placed();

      h.invalidator.settled(
        MutationSettled(
          invalidates: const ['Order[]'],
          response: created,
          place: {
            'Order[]': (entity, _, _) => [entity],
          },
        ),
      );
      h.scheduler.flush();

      list.unmount();
      h.mount('orderList', [
        'Order[]',
      ], const TagContext(query: {'status': 'open'}));
      h.scheduler.flush();

      expect(h.batches, isEmpty);
    });
  });

  group('flush', () {
    test(
      'runs the pending batch on demand and leaves the scheduled one empty',
      () {
        final h = Harness();

        final list = h.mount('orderList', ['Order[]']);

        h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
        h.invalidator.flush();

        expect(h.batches, [
          [list.key],
        ]);

        h.scheduler.flush();

        expect(h.batches, [
          [list.key],
        ]);
      },
    );

    test('does not call the executor with an empty batch', () {
      final h = Harness();

      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));
      h.scheduler.flush();

      expect(h.batches, isEmpty);
    });

    test('reports an executor that throws rather than losing it to the microtask queue', () {
      final errors = <(Object, String)>[];
      final h = Harness(
        onError: (error, context) => errors.add((error, context)),
        execute: (_) => throw StateError('transport is down'),
      );

      h.mount('orderList', ['Order[]']);
      h.invalidator.settled(const MutationSettled(invalidates: ['Order[]']));

      expect(h.scheduler.flush, returnsNormally);
      expect(errors.single.$1, isA<StateError>());
      expect(errors.single.$2, 'execute');
    });

    test('invalidates already-resolved tags directly', () {
      final h = Harness();

      final list = h.mount('orderList', ['Order[]']);

      h.invalidator.invalidate(['Order[]']);
      h.scheduler.flush();

      expect(h.batches, [
        [list.key],
      ]);
    });
  });
}
