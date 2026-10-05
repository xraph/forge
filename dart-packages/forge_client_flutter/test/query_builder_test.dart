import 'dart:async';

import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/src/subscription.dart';

import 'support/harness.dart';

Widget orderBuilder(int id) => ForgeQueryBuilder(
  query: getOrder(OrderArgs(id)),
  builder: (context, state) => Text(orderText(state)),
);

Widget listBuilder() => ForgeQueryBuilder(
  query: listOrders(const ListOrdersArgs()),
  builder: (context, state) => Text(listText(state)),
);

Future<void> invalidateAndSettle(WidgetTester tester, Harness h, List<String> tags) async {
  h.cache.invalidate(tags);
  h.scheduler.flush();
  await settle(tester);
}

/// A list builder with a finite staleTime, so advancing the clock past it
/// makes the next mount refetch synchronously inside that mount.
const _stale = Duration(milliseconds: 50);

Widget staleList(String label) => ForgeQueryBuilder(
  query: listOrders(const ListOrdersArgs()),
  staleTime: _stale,
  builder: (context, state) => Text('$label ${listText(state)}'),
);

void main() {
  tearDown(() => setClient(null));

  group('ForgeQueryBuilder', () {
    testWidgets('renders its value, and re-renders when an entity it depends on changes', (tester) async {
      var total = 0;
      final h = harness((request, _) {
        total += 100;
        return order(idOf(request), total);
      });

      await tester.pumpWidget(scope(h, orderBuilder(1)));

      // The subscription started a request while mounting, so the first frame
      // is the loading branch.
      expect(find.text('loading:-'), findsOneWidget);

      await settle(tester);
      expect(find.text('success:100'), findsOneWidget);

      // Patching order 1 invalidates `Order:1`, this query's own tag.
      await patchOrder(h.cache, const PatchOrderArgs(1));
      h.scheduler.flush();
      await settle(tester);

      expect(find.text('success:300'), findsOneWidget);
    });

    testWidgets('serves two components on one query from a single request', (tester) async {
      final h = harness((_, _) => [order(1, 99)]);

      await tester.pumpWidget(scope(h, Column(children: [listBuilder(), listBuilder()])));
      await settle(tester);

      expect(h.transport.countOf(opListOrders), 1);
      expect(find.text('success:99'), findsNWidgets(2));

      // One registry entry with two listeners, not two entries: one
      // invalidation is one refetch, not two of identical data.
      await invalidateAndSettle(tester, h, ['Order[]']);
      expect(h.transport.countOf(opListOrders), 2);
    });

    testWidgets('releases the subscription when the last consumer unmounts', (tester) async {
      final h = harness((_, _) => [order(1, 99)]);

      Widget shell(int show) => scope(
        h,
        Column(children: [
          if (show > 0) listBuilder(),
          if (show > 1) listBuilder(),
        ]),
      );

      await tester.pumpWidget(shell(2));
      await settle(tester);
      expect(h.transport.countOf(opListOrders), 1);

      // One of two consumers goes away: still mounted, so an invalidation
      // refetches.
      await tester.pumpWidget(shell(1));
      await invalidateAndSettle(tester, h, ['Order[]']);
      expect(h.transport.countOf(opListOrders), 2);

      // The last one: released, so an invalidation only marks it stale.
      await tester.pumpWidget(shell(0));
      await invalidateAndSettle(tester, h, ['Order[]']);
      expect(h.transport.countOf(opListOrders), 2);
    });

    test(
      'survives a StrictMode mount / unmount / mount with a live subscription',
      () {},
      skip: 'React StrictMode double-invokes effects; Flutter mounts a State exactly once.',
    );

    testWidgets('re-subscribes to the new query when its arguments change', (tester) async {
      final h = harness((request, _) => order(idOf(request), 7));

      await tester.pumpWidget(scope(h, orderBuilder(1)));
      await settle(tester);
      expect(find.text('success:7'), findsOneWidget);

      await tester.pumpWidget(scope(h, orderBuilder(2)));
      await settle(tester);
      expect(idOf(h.transport.calls.last), 2);

      // Order 1 was released and order 2 is mounted.
      await invalidateAndSettle(tester, h, ['Order:1', 'Order:2']);
      final ids = h.transport.calls.map(idOf).toList();
      expect(ids, [1, 2, 2]);
    });

    testWidgets('keeps the last good value beside an error from a failed refetch', (tester) async {
      final h = harness((_, call) {
        if (call > 0) throw const Boom('boom');
        return [order(1, 99)];
      });
      final seen = <QueryState<List<Order>>>[];

      await tester.pumpWidget(scope(
        h,
        ForgeQueryBuilder(
          query: listOrders(const ListOrdersArgs()),
          builder: (context, state) {
            seen.add(state);
            return Text(stateStatusOf(state));
          },
        ),
      ));
      await settle(tester);

      final good = seen.last.dataOrNull;

      await expectLater(listOrders(const ListOrdersArgs()).refetch(h.cache), throwsA(isA<Boom>()));
      await settle(tester);

      final last = seen.last;
      expect(last, isA<QueryFailure<List<Order>>>());
      final failure = last as QueryFailure<List<Order>>;
      expect('${failure.error}', 'boom');
      // Stale data plus an error beats an empty screen, and a failure does
      // not change the identity of the data it kept.
      expect(failure.previous, same(good));
    });

    testWidgets('reports a failed first fetch as an error state rather than throwing in render', (tester) async {
      final h = harness((_, _) => throw const Boom('nope'));

      // An explicit client and no scope at all.
      await tester.pumpWidget(ltr(ForgeQueryBuilder(
        client: h.cache,
        query: listOrders(const ListOrdersArgs()),
        builder: (context, state) => Text(switch (state) {
          QueryFailure(:final error) => 'error:$error',
          _ => stateStatusOf(state),
        }),
      )));
      await settle(tester);

      expect(find.text('error:nope'), findsOneWidget);
    });

    testWidgets('passes a per-call staleTime through to the cache', (tester) async {
      // client-core exposes effectiveStaleTime; the Dart contract does not,
      // so this asserts the behaviour the staleTime causes instead.
      final clock = ManualClock();
      final h = harness((_, _) => [order(1, 10)], clock: clock);

      await tester.pumpWidget(scope(
        h,
        ForgeQueryBuilder(
          query: listOrders(const ListOrdersArgs()),
          staleTime: const Duration(milliseconds: 250),
          builder: (context, state) => Text(listText(state)),
        ),
      ));
      await settle(tester);

      clock.advance(const Duration(milliseconds: 300));
      h.cache.revalidate();
      await settle(tester);

      expect(h.transport.countOf(opListOrders), 2);
    });

    testWidgets('rebuilds the handle when staleTime changes', (tester) async {
      final clock = ManualClock();
      final h = harness((_, _) => [order(1, 10)], clock: clock);

      Widget list(Duration staleTime) => scope(
        h,
        ForgeQueryBuilder(
          query: listOrders(const ListOrdersArgs()),
          staleTime: staleTime,
          builder: (context, state) => Text(listText(state)),
        ),
      );

      await tester.pumpWidget(list(const Duration(hours: 1)));
      await settle(tester);

      clock.advance(const Duration(milliseconds: 100));
      h.cache.revalidate();
      await settle(tester);
      expect(h.transport.countOf(opListOrders), 1);

      // The memo-dependency trap: if staleTime were not part of the
      // subscription's identity the change would be silently lost.
      await tester.pumpWidget(list(const Duration(milliseconds: 50)));
      h.cache.revalidate();
      await settle(tester);
      expect(h.transport.countOf(opListOrders), 2);
    });

    testWidgets('stays idle and fetches nothing while disabled, then fetches when enabled', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));

      Widget detail({required bool enabled}) => scope(
        h,
        ForgeQueryBuilder(
          query: getOrder(const OrderArgs(1)),
          enabled: enabled,
          builder: (context, state) => Text(orderText(state)),
        ),
      );

      await tester.pumpWidget(detail(enabled: false));
      await settle(tester);
      expect(find.text('idle:-'), findsOneWidget);
      expect(h.transport.calls, isEmpty);

      await tester.pumpWidget(detail(enabled: true));
      await settle(tester);
      expect(find.text('success:5'), findsOneWidget);
      expect(h.transport.countOf(opGetOrder), 1);
    });

    testWidgets('waits for the query it depends on before fetching', (tester) async {
      final h = harness((request, _) => switch (request.meta.id) {
        'op_list_orders' => [order(4, 1)],
        _ => order(idOf(request), 40),
      });

      // The dependent-query gate TwinOS wrote by hand around the TS useQuery.
      await tester.pumpWidget(scope(
        h,
        ForgeQueryBuilder(
          query: listOrders(const ListOrdersArgs()),
          builder: (context, list) {
            final first = list.dataOrNull?.first;
            return ForgeQueryBuilder(
              query: getOrder(OrderArgs(first?.id ?? 0)),
              enabled: first != null,
              builder: (context, detail) => Text(orderText(detail)),
            );
          },
        ),
      ));

      expect(find.text('idle:-'), findsOneWidget);
      await settle(tester);

      expect(find.text('success:40'), findsOneWidget);
      expect(h.transport.calls.map((r) => r.meta.id), ['op_list_orders', 'op_get_order']);
    });

    testWidgets('renders a cached value on its first frame and does not rebuild for the first stream event', (tester) async {
      final h = harness((request, _) => order(idOf(request), 8));
      await getOrder(const OrderArgs(1)).fetch(h.cache);
      var builds = 0;

      await tester.pumpWidget(scope(
        h,
        ForgeQueryBuilder(
          query: getOrder(const OrderArgs(1)),
          builder: (context, state) {
            builds++;
            return Text(orderText(state));
          },
        ),
      ));

      // The first frame is seeded from getState: `watch` delivers its first
      // event on a microtask, after this frame was built.
      expect(find.text('success:8'), findsOneWidget);
      expect(builds, 1);

      await settle(tester);

      // That first event repeated the seed, so nothing rebuilt and nothing
      // refetched.
      expect(builds, 1);
      expect(h.transport.countOf(opGetOrder), 1);
    });

    // Review Focus 1.
    testWidgets('drops a response that lands after the widget is gone, and keeps it in the cache', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);

      await tester.pumpWidget(scope(h, orderBuilder(1)));
      await tester.pump();
      expect(h.transport.countOf(opGetOrder), 1);

      await tester.pumpWidget(scope(h, const SizedBox()));
      gate.complete(order(1, 42));
      await settle(tester);

      // No setState after dispose, nothing reported.
      expect(tester.takeException(), isNull);
      // The answer still landed, so the next mount shows it with no request.
      expect(getOrder(const OrderArgs(1)).getState(h.cache).dataOrNull?.total, 42);

      await tester.pumpWidget(scope(h, orderBuilder(1)));
      expect(find.text('success:42'), findsOneWidget);
      await settle(tester);
      expect(h.transport.countOf(opGetOrder), 1);
    });

    // Review Focus 2.
    testWidgets('does not refetch or resubscribe when the parent rebuilds with a new args object', (tester) async {
      final h = harness((_, _) => [order(1, 99)]);
      late StateSetter rebuild;
      var ticks = 0;

      await tester.pumpWidget(scope(
        h,
        StatefulBuilder(builder: (context, setState) {
          rebuild = setState;
          ticks++;
          // A new args object on every build, written the way a caller writes it.
          return ForgeQueryBuilder(
            query: listOrders(ListOrdersArgs(status: 'open-$ticks'.substring(0, 4))),
            builder: (context, state) => Text(listText(state)),
          );
        }),
      ));
      await settle(tester);

      for (var i = 0; i < 10; i++) {
        rebuild(() {});
        await tester.pump();
      }

      expect(ticks, 11);
      expect(h.transport.countOf(opListOrders), 1);

      // Still mounted exactly once: an invalidation is one refetch.
      await invalidateAndSettle(tester, h, ['Order[]']);
      expect(h.transport.countOf(opListOrders), 2);
    });

    // Review Focus 3.
    testWidgets('moves to the new client when the scope swaps it', (tester) async {
      final a = harness((_, _) => [order(1, 1)]);
      final b = harness((_, _) => [order(1, 2)]);

      await tester.pumpWidget(scope(a, listBuilder()));
      await settle(tester);
      expect(find.text('success:1'), findsOneWidget);

      await tester.pumpWidget(scope(b, listBuilder()));
      await settle(tester);
      expect(find.text('success:2'), findsOneWidget);

      // The old cache was released: invalidating it fetches nothing.
      await invalidateAndSettle(tester, a, ['Order[]']);
      expect(a.transport.countOf(opListOrders), 1);
      expect(flutterSeamsInstalled(a.cache), isFalse);
      expect(flutterSeamsInstalled(b.cache), isTrue);
    });

    // Ruling R7. The cache notifies its listeners synchronously, so a second
    // builder mounted during a build onto a stale query that another builder
    // already watches starts a fetch whose isFetching event reaches the first
    // builder in the middle of that build.
    testWidgets('mounts a second builder mid-build onto a stale query another builder watches', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);

      Widget screen({required bool detail}) => scope(
        h,
        Column(children: [
          staleList('list'),
          if (detail) Builder(builder: (context) => staleList('detail')),
        ]),
      );

      await tester.pumpWidget(screen(detail: false));
      await settle(tester);
      expect(find.text('list success:10'), findsOneWidget);

      // Past the staleTime, so mounting the detail refetches.
      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(detail: true));

      // No "setState() or markNeedsBuild() called during build".
      expect(tester.takeException(), isNull);
      expect(h.transport.countOf(opListOrders), 2);
      // The list's update waited for the end of the frame, and the setState
      // it made there scheduled the next one.
      expect(tester.binding.hasScheduledFrame, isTrue);

      gate.complete([order(1, 20)]);
      await settle(tester);

      expect(find.text('list success:20'), findsOneWidget);
      expect(find.text('detail success:20'), findsOneWidget);
    });

    testWidgets('drops an update deferred past a frame in which its widget was disposed', (tester) async {
      final clock = ManualClock();
      final h = harness((_, call) => [order(1, 10 + call)], clock: clock);

      await tester.pumpWidget(scope(
        h,
        Column(children: [KeyedSubtree(key: const ValueKey('list'), child: staleList('list'))]),
      ));
      await settle(tester);
      clock.advance(const Duration(milliseconds: 100));

      // One frame removes the list and mounts the detail. Keyed children are
      // deactivated after the new ones mount, so the detail's refetch reaches
      // the list mid-build and is deferred; the list is then disposed at the
      // end of the same frame, before the deferred update runs.
      await tester.pumpWidget(scope(
        h,
        Column(children: [
          KeyedSubtree(
            key: const ValueKey('detail'),
            child: Builder(builder: (context) => staleList('detail')),
          ),
        ]),
      ));

      // No "setState() called after dispose()" from the post-frame callback.
      expect(tester.takeException(), isNull);

      await settle(tester);
      expect(find.text('detail success:11'), findsOneWidget);
    });
  });

  group('firstState', () {
    testWidgets('seeds a disabled query with the binding\'s shared idle and opens no record', (tester) async {
      final h = harness((request, _) => order(idOf(request), 1));

      final seed = firstState(h.cache, getOrder(const OrderArgs(1)), enabled: false);

      // The same idle the disabled watch delivers, so its first event is not
      // a change, and no record was opened for a query that must hold none.
      expect(seed, same(getOrder(const OrderArgs(1)).getState(h.cache, enabled: false)));
      expect(h.cache.size, 0);
      expect(h.transport.calls, isEmpty);
    });
  });

  group('QuerySubscription', () {
    /// A fetched order 1 with total 8, and a snapshot of the same query on
    /// another cache with total 50, for a synchronous second update.
    Future<(Harness, Snapshot)> fixture(Completer<Object?> gate) async {
      final h = harness((request, call) => call == 0 ? order(idOf(request), 8) : gate.future);
      await getOrder(const OrderArgs(1)).fetch(h.cache);
      final other = harness((request, _) => order(idOf(request), 50));
      await getOrder(const OrderArgs(1)).fetch(other.cache);
      return (h, dehydrate(other.cache));
    }

    void restore(Harness h, Snapshot snapshot) =>
        hydrate(h.cache, snapshot, operations: const {'op_get_order': opGetOrder});

    /// Runs [during] once, inside the build of the next frame.
    Future<void> inBuild(WidgetTester tester, void Function() during) async {
      var ran = false;
      await tester.pumpWidget(Builder(builder: (context) {
        if (!ran) {
          ran = true;
          during();
        }
        return const SizedBox();
      }));
    }

    testWidgets('is keyed on the query key and the watch options, not the args object', (tester) async {
      final a = harness((_, _) => [order(1, 1)]);
      final b = harness((_, _) => [order(1, 2)]);
      final subscription = QuerySubscription<List<Order>>((_, _) {});
      addTearDown(subscription.dispose);
      // Not const, so every call builds a new args object with the same key.
      QueryRef<List<Order>, ListOrdersArgs> open() =>
          listOrders(ListOrdersArgs(status: 'open-'.substring(0, 4)));

      expect(subscription.bind(a.cache, open(), live: false, enabled: true), isTrue);
      expect(subscription.bind(a.cache, open(), live: false, enabled: true), isFalse);
      expect(
        subscription.bind(a.cache, open(), live: false, staleTime: _stale, enabled: true),
        isTrue,
      );
      expect(
        subscription.bind(a.cache, open(), live: false, staleTime: _stale, enabled: false),
        isTrue,
      );
      expect(
        subscription.bind(b.cache, open(), live: false, staleTime: _stale, enabled: false),
        isTrue,
      );
      expect(
        subscription.bind(b.cache, listOrders(const ListOrdersArgs()), live: false, staleTime: _stale, enabled: false),
        isTrue,
      );
      await tester.pump();
    });

    testWidgets('coalesces the updates of one build into one change, and the latest wins', (tester) async {
      final gate = Completer<Object?>();
      final (h, snapshot) = await fixture(gate);
      final changes = <(QueryState<Order>, QueryState<Order>)>[];
      final subscription = QuerySubscription<Order>((previous, next) => changes.add((previous, next)));
      addTearDown(subscription.dispose);

      subscription.bind(h.cache, getOrder(const OrderArgs(1)), live: false, enabled: true);
      await tester.pump();
      expect(changes, isEmpty);

      var duringBuild = -1;
      await inBuild(tester, () {
        // Two synchronous transitions: the refetch starts, then a restore
        // settles the query on new data.
        unawaited(getOrder(const OrderArgs(1)).refetch(h.cache));
        restore(h, snapshot);
        duringBuild = changes.length;
      });

      expect(duringBuild, 0);
      expect(changes, hasLength(1));
      final (previous, next) = changes.single;
      expect(previous.dataOrNull?.total, 8);
      expect(previous.isFetching, isFalse);
      expect(next.dataOrNull?.total, 50);
      expect(subscription.state, same(next));
    });

    testWidgets('drops a deferred update when it resubscribes before the frame ends', (tester) async {
      final gate = Completer<Object?>();
      final (h, _) = await fixture(gate);
      final changes = <(QueryState<Order>, QueryState<Order>)>[];
      final subscription = QuerySubscription<Order>((previous, next) => changes.add((previous, next)));
      addTearDown(subscription.dispose);

      subscription.bind(h.cache, getOrder(const OrderArgs(1)), live: false, enabled: true);
      await tester.pump();

      await inBuild(tester, () {
        // Order 1's refetch is deferred, then the subscription moves to order 2.
        unawaited(getOrder(const OrderArgs(1)).refetch(h.cache));
        expect(
          subscription.bind(h.cache, getOrder(const OrderArgs(2)), live: false, enabled: true),
          isTrue,
        );
      });

      // Order 1's update belonged to the old query and never arrived.
      expect(changes, isEmpty);
      expect(subscription.state, isA<QueryLoading<Order>>());
    });

    testWidgets('never lets a deferred update overwrite a newer one delivered after the build', (tester) async {
      final gate = Completer<Object?>();
      final (h, snapshot) = await fixture(gate);
      final changes = <(QueryState<Order>, QueryState<Order>)>[];
      final subscription = QuerySubscription<Order>((previous, next) => changes.add((previous, next)));
      addTearDown(subscription.dispose);

      subscription.bind(h.cache, getOrder(const OrderArgs(1)), live: false, enabled: true);
      await tester.pump();

      await inBuild(tester, () {
        // Registered first, so it runs before the subscription's own
        // post-frame callback, outside the build, where updates are
        // delivered at once.
        SchedulerBinding.instance.addPostFrameCallback((_) => restore(h, snapshot));
        unawaited(getOrder(const OrderArgs(1)).refetch(h.cache));
      });

      expect(changes, hasLength(1));
      expect(changes.single.$2.dataOrNull?.total, 50);
      expect(subscription.state.dataOrNull?.total, 50);
      expect(subscription.state.isFetching, isFalse);
    });
  });
}
