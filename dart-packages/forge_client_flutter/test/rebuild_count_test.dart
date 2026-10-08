import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'support/harness.dart';

/// Counts builder calls and records every state a builder was handed.
final class Recorder<T> {
  final List<QueryState<T>> states = [];

  int get builds => states.length;

  QueryState<T> get last => states.last;

  /// Whether any build after the first [builds] rendered a fetching state.
  /// The first load is fetching too, so a refetch is measured from the
  /// builds already seen when it starts.
  bool sawFetchingAfter(int builds) =>
      states.skip(builds).any((state) => state.isFetching);

  Widget Function(BuildContext, QueryState<T>) builder(
    String Function(QueryState<T>) text,
  ) => (context, state) {
    states.add(state);
    return Text(text(state));
  };
}

Future<void> invalidateAndSettle(
  WidgetTester tester,
  Harness h,
  List<String> tags,
) async {
  h.cache.invalidate(tags);
  h.scheduler.flush();
  await settle(tester);
}

void main() {
  group('identity', () {
    testWidgets(
      'does not re-render a component reading an unchanged sibling entity',
      (tester) async {
        // `late` because the handler reads the transport it belongs to.
        late final Harness h;
        h = harness(
          (request, _) => order(idOf(request), h.transport.calls.length),
        );
        final one = Recorder<Order>();
        final two = Recorder<Order>();

        await tester.pumpWidget(
          scope(
            h,
            Column(
              children: [
                ForgeQueryBuilder(
                  query: getOrder(const OrderArgs(1)),
                  builder: one.builder(orderText),
                ),
                ForgeQueryBuilder(
                  query: getOrder(const OrderArgs(2)),
                  builder: two.builder(orderText),
                ),
              ],
            ),
          ),
        );
        await settle(tester);

        final settled = two.builds;

        await patchOrder(h.cache, const PatchOrderArgs(1));
        h.scheduler.flush();
        await settle(tester);

        // Order 1 moved, so its builder did.
        expect(one.builds, greaterThan(settled));
        // Order 2 did not, and neither did its builder. A write to `Order:1` is
        // invisible to everything that does not reference it.
        expect(two.builds, settled);
      },
    );

    testWidgets('keeps the identity of an entity a refetch did not change', (
      tester,
    ) async {
      // A fresh map on every call: structurally equal, referentially new.
      final h = harness((_, _) => [order(1, 99)]);
      final seen = Recorder<List<Order>>();

      await tester.pumpWidget(
        scope(
          h,
          ForgeQueryBuilder(
            query: listOrders(const ListOrdersArgs()),
            builder: seen.builder(listText),
          ),
        ),
      );
      await settle(tester);

      final first = seen.last.dataOrNull;
      expect(first, [const Order(id: 1, total: 99)]);

      await listOrders(const ListOrdersArgs()).refetch(h.cache);
      await settle(tester);

      expect(h.transport.countOf(opListOrders), 2);
      // `Order:1` is provably unchanged, so it is the same object.
      expect(seen.last.dataOrNull?.first, same(first?.first));
      // So is the list around it.
      expect(seen.last.dataOrNull, same(first));
    });

    testWidgets(
      'gives the list a new identity when the refetch really did change it',
      (tester) async {
        var total = 99;
        final h = harness((_, _) => [order(1, total)]);
        final seen = Recorder<List<Order>>();

        await tester.pumpWidget(
          scope(
            h,
            ForgeQueryBuilder(
              query: listOrders(const ListOrdersArgs()),
              builder: seen.builder(listText),
            ),
          ),
        );
        await settle(tester);

        final first = seen.last.dataOrNull;
        total = 120;

        await listOrders(const ListOrdersArgs()).refetch(h.cache);
        await settle(tester);

        expect(seen.last.dataOrNull, isNot(same(first)));
        expect(seen.last.dataOrNull?.first, isNot(same(first?.first)));
        expect(seen.last.dataOrNull?.first.total, 120);
      },
    );

    // In Flutter terms: a parent that rebuilds hands the builder the same
    // QueryState object every time, and never resubscribes. React's mount
    // count is not observable from outside the cache, so "never resubscribes"
    // is pinned by the request count.
    testWidgets(
      'hands React a getSnapshot whose result survives unrelated re-renders',
      (tester) async {
        final h = harness((_, _) => [order(1, 99)]);
        final seen = Recorder<List<Order>>();
        late StateSetter bump;

        await tester.pumpWidget(
          scope(
            h,
            StatefulBuilder(
              builder: (context, setState) {
                bump = setState;
                // A new args object on every build, which is how a caller writes it.
                return ForgeQueryBuilder(
                  query: listOrders(
                    ListOrdersArgs(status: ['op', 'en'].join()),
                  ),
                  builder: seen.builder(listText),
                );
              },
            ),
          ),
        );
        await settle(tester);

        final settled = seen.last;
        final builds = seen.builds;

        for (var i = 0; i < 10; i++) {
          bump(() {});
          await tester.pump();
        }

        // The parent really did rebuild the builder, ten times.
        expect(seen.builds, builds + 10);
        expect(seen.last, same(settled));
        expect(h.transport.countOf(opListOrders), 1);
      },
    );

    // Not a React case. Nothing flips but the data, so only the data
    // comparison can tell the new state from the old one.
    testWidgets(
      'rebuilds when a write to a referenced entity changes what the list reads',
      (tester) async {
        final h = harness((request, _) {
          if (request.meta.id == opPatchOrder.id) return order(1, 55);
          return [order(1, 10), order(2, 20)];
        });
        final seen = Recorder<List<Order>>();

        await tester.pumpWidget(
          scope(
            h,
            ForgeQueryBuilder(
              query: listOrders(const ListOrdersArgs()),
              builder: seen.builder(listText),
            ),
          ),
        );
        await settle(tester);

        final before = seen.builds;
        final data = seen.last.dataOrNull;

        await patchOrder(h.cache, const PatchOrderArgs(1, total: 55));
        await settle(tester);

        expect(seen.builds, greaterThan(before));
        expect(seen.last.dataOrNull, isNot(same(data)));
        expect(seen.last.dataOrNull?.map((o) => o.total), [55, 20]);
      },
    );

    // Not a React case. The write reaches the list through `Order:1`, but it
    // stores what is already there, so the list's state is the state it had.
    // This guards the core store's equal-write path (store.dart `_equal`):
    // the store keeps the same record object, and that identity is what lets
    // sameQueryState see no change.
    testWidgets(
      'does not rebuild when a write to a referenced entity changes nothing',
      (tester) async {
        final h = harness((request, _) {
          if (request.meta.id == opPatchOrder.id) return order(1, 10);
          return [order(1, 10), order(2, 20)];
        });
        final seen = Recorder<List<Order>>();

        await tester.pumpWidget(
          scope(
            h,
            ForgeQueryBuilder(
              query: listOrders(const ListOrdersArgs()),
              builder: seen.builder(listText),
            ),
          ),
        );
        await settle(tester);

        final before = seen.builds;
        final data = seen.last.dataOrNull;

        await patchOrder(h.cache, const PatchOrderArgs(1));
        await settle(tester);

        expect(h.transport.countOf(opPatchOrder), 1);
        expect(seen.builds, before);
        expect(seen.last.dataOrNull, same(data));
      },
    );

    // Not a React case: the seed read at bind time and the stream's first
    // event are the same state. The fetch is held so that event lands in a
    // frame of its own, where a repeat would show as a second build.
    testWidgets('renders each distinct state exactly once', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      final seen = Recorder<Order>();

      await tester.pumpWidget(
        scope(
          h,
          ForgeQueryBuilder(
            query: getOrder(const OrderArgs(1)),
            builder: seen.builder(orderText),
          ),
        ),
      );
      await settle(tester);

      expect(seen.states.map(stateStatusOf), ['loading']);

      gate.complete(order(1, 10));
      await settle(tester);

      expect(seen.states.map(stateStatusOf), ['loading', 'success']);
    });
  });

  group('select', () {
    testWidgets(
      'rebuilds for fetching flips without select, and keeps the data identical',
      (tester) async {
        Completer<Object?>? gate;
        final h = harness((_, _) => gate?.future ?? order(1, 10));
        final seen = Recorder<Order>();

        await tester.pumpWidget(
          scope(
            h,
            ForgeQueryBuilder(
              query: getOrder(const OrderArgs(1)),
              builder: seen.builder(orderText),
            ),
          ),
        );
        await settle(tester);

        final before = seen.builds;
        final data = seen.last.dataOrNull;
        expect(seen.sawFetchingAfter(before), isFalse);

        // Hold the refetch so a frame actually renders the fetching state.
        gate = Completer<Object?>();
        await invalidateAndSettle(tester, h, ['Order:1']);

        expect(h.transport.countOf(opGetOrder), 2);
        // isFetching went true, which a spinner needs to see.
        expect(seen.builds, before + 1);
        expect(seen.last.isFetching, isTrue);
        expect(seen.last.dataOrNull, same(data));

        gate.complete(order(1, 10));
        await settle(tester);

        // And back to false.
        expect(seen.builds, before + 2);
        expect(seen.last.isFetching, isFalse);
        expect(seen.last.dataOrNull, same(data));
      },
    );

    testWidgets(
      'select suppresses rebuilds while the selected slice is unchanged',
      (tester) async {
        var total = 10;
        Completer<Object?>? gate;
        final h = harness((_, _) => gate?.future ?? order(1, total));
        final seen = Recorder<Order>();

        await tester.pumpWidget(
          scope(
            h,
            ForgeQueryBuilder(
              query: getOrder(const OrderArgs(1)),
              select: (state) => state.dataOrNull?.total,
              builder: seen.builder(orderText),
            ),
          ),
        );
        await settle(tester);

        final before = seen.builds;

        // Same total: the refetch runs and the builder does not. Hold it so the
        // cache is provably mid-fetch while the builder stays put.
        gate = Completer<Object?>();
        await invalidateAndSettle(tester, h, ['Order:1']);
        expect(h.transport.countOf(opGetOrder), 2);
        expect(
          getOrder(const OrderArgs(1)).getState(h.cache).isFetching,
          isTrue,
        );
        expect(seen.builds, before);

        gate.complete(order(1, total));
        await settle(tester);
        expect(
          getOrder(const OrderArgs(1)).getState(h.cache).isFetching,
          isFalse,
        );
        expect(seen.builds, before);
        expect(seen.sawFetchingAfter(before), isFalse);

        // A new total: exactly one rebuild.
        gate = null;
        total = 11;
        await invalidateAndSettle(tester, h, ['Order:1']);
        expect(seen.builds, before + 1);
        expect(find.text('success:11'), findsOneWidget);
      },
    );

    // Review Focus 5.
    testWidgets(
      'select returning a new list every time does not rebuild while the list is unchanged',
      (tester) async {
        var rows = [order(1, 10), order(2, 20)];
        Completer<Object?>? gate;
        final h = harness((_, _) => gate?.future ?? rows);
        final seen = Recorder<List<Order>>();
        final selected = <Object?>[];

        await tester.pumpWidget(
          scope(
            h,
            ForgeQueryBuilder(
              query: listOrders(const ListOrdersArgs()),
              select: (state) {
                final ids = state.dataOrNull?.map((o) => o.id).toList();
                selected.add(ids);
                return ids;
              },
              builder: seen.builder(listText),
            ),
          ),
        );
        await settle(tester);

        final before = seen.builds;
        // The selection the builder holds: the last one that was stored.
        final held = selected.last;

        // Totals change, ids do not: a new list of the same ids every time.
        rows = [order(1, 11), order(2, 21)];
        gate = Completer<Object?>();
        final selections = selected.length;
        await invalidateAndSettle(tester, h, ['Order[]']);
        expect(seen.builds, before);

        gate.complete(rows);
        await settle(tester);
        expect(seen.builds, before);

        // The selector ran again and produced a fresh list each time, none of
        // them identical to the one the builder holds, so only deep equality
        // suppressed the rebuild.
        final reselected = selected.skip(selections).toList();
        expect(reselected, isNotEmpty);
        for (final ids in reselected) {
          expect(ids, [1, 2]);
          expect(ids, isNot(same(held)));
        }

        // A new id: one rebuild.
        gate = null;
        rows = [order(1, 11), order(2, 21), order(3, 30)];
        await invalidateAndSettle(tester, h, ['Order[]']);
        expect(seen.builds, before + 1);
        expect(find.text('success:11,21,30'), findsOneWidget);
      },
    );

    testWidgets(
      'select over a record rebuilds when any field of the record changes',
      (tester) async {
        Completer<Object?>? gate;
        final h = harness((_, _) => gate?.future ?? order(1, 10));
        final seen = Recorder<Order>();

        await tester.pumpWidget(
          scope(
            h,
            ForgeQueryBuilder(
              query: getOrder(const OrderArgs(1)),
              select: (state) => (state.dataOrNull?.total, state.isFetching),
              builder: seen.builder(orderText),
            ),
          ),
        );
        await settle(tester);

        final before = seen.builds;
        expect(seen.sawFetchingAfter(before), isFalse);

        // The total stays the same, but the record names isFetching, which
        // flips during the refetch, so the builder sees it.
        gate = Completer<Object?>();
        await invalidateAndSettle(tester, h, ['Order:1']);
        expect(seen.builds, before + 1);
        expect(seen.sawFetchingAfter(before), isTrue);
        expect(seen.last.dataOrNull?.total, 10);

        gate.complete(order(1, 10));
        await settle(tester);
        expect(seen.builds, before + 2);
        expect(seen.last.isFetching, isFalse);
      },
    );
  });
}
