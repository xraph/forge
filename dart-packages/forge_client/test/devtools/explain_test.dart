import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/devtools.dart';
import 'package:forge_client/src/devtools/tag.dart';
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

import 'harness.dart';

const _one = TagContext(path: {'id': 1});

void main() {
  group('why did this query refetch', () {
    test('names the tag, the operation and the query it hit', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      // `orderUpdate` declares `Order[]`, which the list provides.
      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      final report = devtools.whyRefetched(h.key(Ops.orderList));

      expect(report, isNotNull);
      expect(report!.reason, FetchReason.invalidation);
      expect(report.cause?.label, 'mutation PATCH /orders/{id}');
      expect(report.cause?.tags, ['Order:1', 'Order[]']);
      expect(report.matched, contains('Order[]'));
      expect(report.summary, contains('PATCH /orders/{id}'));

      await sub.cancel();
      devtools.dispose();
    });

    test('records the invalidation, its cause, and every query it hit', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final list = h.mount(Ops.orderList);
      final one = h.mount(Ops.orderGet, _one);
      await h.settle();

      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      final log = devtools.log();
      final mutation = log.whereType<MutationLog>().first;

      expect(mutation.operation, 'PATCH /orders/{id}');
      expect(mutation.tags, ['Order:1', 'Order[]']);
      expect(mutation.unresolved, isEmpty);

      final hits = log.whereType<InvalidatedLog>().toList();

      // Both queries: the list through `Order[]`, the detail through `Order:1`.
      expect(hits, hasLength(2));
      for (final hit in hits) {
        expect(hit.cause, mutation.seq);
      }

      final listHit = hits.firstWhere((e) => e.query == h.key(Ops.orderList));
      final detailHit = hits.firstWhere(
        (e) => e.query == h.key(Ops.orderGet, _one),
      );

      // The list is reached twice over: through the `Order[]` it declares, and
      // through the `Order:1` its last response put in its dependency set.
      expect(listHit.matched, ['Order:1', 'Order[]']);
      expect(detailHit.matched, ['Order:1']);

      await list.cancel();
      await one.cancel();
      devtools.dispose();
    });

    test('calls a first fetch a mount rather than an invalidation', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      expect(
        devtools.whyRefetched(h.key(Ops.orderList))?.reason,
        FetchReason.mount,
      );

      await sub.cancel();
      devtools.dispose();
    });
  });

  group('why did this query NOT refetch', () {
    // The one that matters. A list carrying `Order[]`, a create invalidating
    // `Order:9`, and nothing anywhere reporting that they never met.
    test(
      'answers the Order[] / Order:9 near miss with the declaration to change',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        int reads() => h.calls
            .where((c) => c.meta.path == '/orders' && c.meta.method == 'GET')
            .length;
        final before = reads();

        await h.cache.mutate(
          Ops.orderCreate,
          const TagContext(body: {'total': 30}),
        );
        h.flush();
        await h.settle();

        // The premise: it really did not refetch.
        expect(reads(), before);

        final report = devtools.whyNotRefetched(h.key(Ops.orderList));

        expect(report.outcome, MissOutcome.missed);
        expect(report.cause.label, 'mutation POST /orders');
        expect(report.invalidated, ['Order:9']);
        expect(report.carried, contains('Order[]'));
        expect(report.matched, isEmpty);
        expect(report.nearest.first.invalidated, 'Order:9');
        expect(report.nearest.first.carried, 'Order[]');
        expect(
          report.nearest.first.relation,
          NearMissRelation.instanceVsCollection,
        );
        expect(
          report.suggestions.join(' '),
          contains("Add `Order[]` to the operation's Invalidates"),
        );
        expect(report.reason, contains('disjoint'));

        await sub.cancel();
        devtools.dispose();
      },
    );

    test('distinguishes a real miss from a query nobody has mounted', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();
      // Unmount, but the registry still remembers it.
      await sub.cancel();

      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      final report = devtools.whyNotRefetched(h.key(Ops.orderList));

      expect(report.outcome, MissOutcome.staleWhileUnmounted);
      expect(report.matched, contains('Order[]'));
      expect(report.mounts, 0);
      expect(report.reason, contains('refetches the moment it mounts again'));
      // Not a declaration bug, so it offers no declarations to change.
      expect(report.suggestions, isEmpty);

      devtools.dispose();
    });

    test(
      'blames a placement callback rather than the tag graph when one answered',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        await h.cache.mutate(
          Ops.orderUpdate,
          _one,
          options: MutateOptions(
            place: {
              'Order[]': (created, current, args) => [
                created,
                ...?(current as List<Object?>?),
              ],
              'Order:1': (created, current, args) => current as List<Object?>?,
            },
          ),
        );
        h.flush();
        await h.settle();

        final report = devtools.whyNotRefetched(h.key(Ops.orderList));

        expect(report.outcome, MissOutcome.placed);
        expect(report.reason, contains('placement callback answered'));

        await sub.cancel();
        devtools.dispose();
      },
    );

    test('names an unresolved template, which is the silent case', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      // The response carries no `ref`, so `Order[]:{res.ref}` resolves to
      // nothing and is skipped. Nothing is invalidated and nothing says so.
      await h.cache.mutate(Ops.orderArchive, _one);
      h.flush();
      await h.settle();

      final report = devtools.whyNotRefetched(h.key(Ops.orderList));

      expect(report.outcome, MissOutcome.missed);
      expect(report.invalidated, isEmpty);
      expect(report.cause.unresolved, ['Order[]:{res.ref}']);
      expect(
        report.suggestions.first,
        contains('resolved to nothing and were skipped'),
      );

      await sub.cancel();
      devtools.dispose();
    });

    test('reports a key it has never heard of as such', () {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final report = devtools.whyNotRefetched('GET /nope');

      expect(report.outcome, MissOutcome.notTracked);
      expect(report.reason, contains('never heard of'));

      devtools.dispose();
    });

    test('says so when there is no cause in the log at all', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final report = devtools.whyNotRefetched(h.key(Ops.orderList));

      expect(report.outcome, MissOutcome.missed);
      expect(report.cause.label, contains('no mutation or frame batch'));

      await sub.cancel();
      devtools.dispose();
    });
  });

  group('explain picks the question', () {
    test('returns the refetch story when it refetched and the miss story when it did not', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());
      final sub = h.mount(Ops.orderList);
      await h.settle();

      await h.cache.mutate(Ops.orderUpdate, _one);
      h.flush();
      await h.settle();

      final hit = devtools.explain(h.key(Ops.orderList)) as RefetchReport;
      expect(hit.reason, FetchReason.invalidation);

      await h.cache.mutate(Ops.orderCreate, const TagContext(body: {}));
      h.flush();
      await h.settle();

      final miss = devtools.explain(h.key(Ops.orderList)) as MissReport;
      expect(miss.outcome, MissOutcome.missed);

      await sub.cancel();
      devtools.dispose();
    });
  });

  group('asking before running it', () {
    test(
      'reports what an operation would invalidate and who it would reach',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final sub = h.mount(Ops.orderList);
        await h.settle();

        final before = h.calls.length;
        final preview = devtools.wouldInvalidate(
          Ops.orderCreate,
          const TagContext(body: {}),
          {'id': 9},
        );

        // Asking must not be answered by doing.
        expect(h.calls.length, before);
        expect(preview.tags, ['Order:9']);
        expect(preview.missed, ['Order:9']);
        expect(preview.hits.first.queries, isEmpty);

        final covered = devtools.wouldInvalidate(Ops.orderUpdate, _one);

        expect(covered.tags, ['Order:1', 'Order[]']);
        expect(covered.hits.firstWhere((hit) => hit.tag == 'Order[]').queries, [
          h.key(Ops.orderList),
        ]);

        await sub.cancel();
        devtools.dispose();
      },
    );
  });
}
