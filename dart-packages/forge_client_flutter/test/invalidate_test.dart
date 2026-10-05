import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'support/harness.dart';

/// Captures a [BuildContext] for a widget that holds no query of its own,
/// like a dialog that has to refresh what its write changed.
final class _Grab extends StatelessWidget {
  const _Grab(this.onContext);

  final void Function(BuildContext context) onContext;

  @override
  Widget build(BuildContext context) {
    onContext(context);
    return const SizedBox();
  }
}

Widget totalOf(QueryRef<List<Order>, ListOrdersArgs> query, {QueryCache? client}) =>
    ForgeQueryBuilder(
      query: query,
      client: client,
      builder: (context, state) => Text('total:${state.dataOrNull?.first.total ?? '-'}'),
    );

Widget listOf(QueryRef<List<Order>, ListOrdersArgs> query) => ForgeQueryBuilder(
  query: query,
  builder: (context, state) => Text(listText(state)),
);

/// Runs the invalidator's pending batch and lets the refetches it started
/// settle. `forgeInvalidate` marks queries stale and returns, so a test
/// asserting on the requests it caused drives the same scheduler the
/// application's microtask would have driven.
Future<void> flushAndSettle(WidgetTester tester, Harness h) async {
  h.scheduler.flush();
  await settle(tester);
}

void main() {
  tearDown(() => setClient(null));

  group('forgeInvalidate', () {
    testWidgets('refreshes a list held by a sibling that this component cannot see', (tester) async {
      var served = 0;
      final h = harness((_, _) => [order(1, ++served)]);
      late BuildContext dialog;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          totalOf(listOrders(const ListOrdersArgs())),
          _Grab((context) => dialog = context),
        ]),
      ));
      await settle(tester);
      expect(find.text('total:1'), findsOneWidget);

      dialog.forgeInvalidate(listOrders);
      await flushAndSettle(tester, h);

      expect(find.text('total:2'), findsOneWidget);
    });

    testWidgets('reaches a query the tag graph cannot, because it declares no tags', (tester) async {
      late final Harness h;
      h = harness((request, _) {
        if (request.meta.id == opSearchOrders.id) {
          return [order(1, h.transport.countOf(opSearchOrders))];
        }
        return order(2, 0);
      });
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          totalOf(searchOrders(const ListOrdersArgs())),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);
      expect(find.text('total:1'), findsOneWidget);

      // A create declaring `Order[]`. The search carries only `Order:1`, so
      // the tag graph has no edge to it and the write is invisible to it.
      await createOrder(h.cache, const CreateOrderArgs(7));
      await flushAndSettle(tester, h);
      expect(h.transport.countOf(opSearchOrders), 1);

      // Addressed by operation, it refetches regardless of what it declares.
      context.forgeInvalidate(searchOrders);
      await flushAndSettle(tester, h);

      expect(h.transport.countOf(opSearchOrders), 2);
      expect(find.text('total:2'), findsOneWidget);
    });

    testWidgets('hits every argument variant when no arguments are given', (tester) async {
      late final Harness h;
      h = harness((request, _) => order(idOf(request), h.transport.calls.length));
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          ForgeQueryBuilder(query: getOrder(const OrderArgs(1)), builder: (c, s) => Text(orderText(s))),
          ForgeQueryBuilder(query: getOrder(const OrderArgs(2)), builder: (c, s) => Text(orderText(s))),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);
      expect(h.transport.countOf(opGetOrder), 2);

      context.forgeInvalidate(getOrder);
      await flushAndSettle(tester, h);

      // Both variants, and not the whole cache either.
      expect(h.transport.countOf(opGetOrder), 4);
    });

    testWidgets('targets exactly one variant when arguments are given', (tester) async {
      late final Harness h;
      h = harness((request, _) => order(idOf(request), h.transport.calls.length));
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          ForgeQueryBuilder(query: getOrder(const OrderArgs(1)), builder: (c, s) => Text(orderText(s))),
          ForgeQueryBuilder(query: getOrder(const OrderArgs(2)), builder: (c, s) => Text(orderText(s))),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);
      expect(h.transport.countOf(opGetOrder), 2);

      context.forgeInvalidate(getOrder, const OrderArgs(1));
      await flushAndSettle(tester, h);

      expect(h.transport.countOf(opGetOrder), 3);
      expect(idOf(h.transport.calls.last), 1);
    });

    testWidgets('refetches a query fetched with no arguments at all', (tester) async {
      // The TS `open` args trap: a query keyed with no args must be refetched
      // under that same key, not under a second, empty one.
      var served = 0;
      final h = harness((_, _) => [order(1, ++served)]);
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          totalOf(listOrders(const ListOrdersArgs())),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);

      final size = h.cache.size;

      context.forgeInvalidate(listOrders);
      await flushAndSettle(tester, h);

      expect(find.text('total:2'), findsOneWidget);
      // No second record was opened behind the widget's back.
      expect(h.cache.size, size);
    });

    testWidgets('does not fetch an unmounted query now, and refetches it on its next mount', (tester) async {
      var served = 0;
      final h = harness((_, _) => order(1, ++served));
      late BuildContext context;

      Widget screen({required bool visible}) => scope(
        h,
        Column(children: [
          _Grab((c) => context = c),
          if (visible)
            ForgeQueryBuilder(query: getOrder(const OrderArgs(1)), builder: (c, s) => Text(orderText(s))),
        ]),
      );

      await tester.pumpWidget(screen(visible: true));
      await settle(tester);
      expect(h.transport.countOf(opGetOrder), 1);

      await tester.pumpWidget(screen(visible: false));
      await settle(tester);

      context.forgeInvalidate(getOrder);
      await flushAndSettle(tester, h);

      // Nobody is watching it, so nothing is fetched.
      expect(h.transport.countOf(opGetOrder), 1);

      await tester.pumpWidget(screen(visible: true));
      await flushAndSettle(tester, h);

      // The staleness was remembered and paid for when it matters.
      expect(h.transport.countOf(opGetOrder), 2);
      expect(find.text('success:2'), findsOneWidget);
    });

    testWidgets('resolves refetch only once the mounted query has settled', (tester) async {
      var served = 0;
      final h = harness((_, _) => [order(1, ++served)]);
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          totalOf(listOrders(const ListOrdersArgs())),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);
      expect(find.text('total:1'), findsOneWidget);

      await context.forgeRefetch(listOrders);
      await tester.pump();

      // Awaited, so the new value is in the cache with no scheduler flush.
      expect(find.text('total:2'), findsOneWidget);
      expect(h.transport.countOf(opListOrders), 2);

      // And no batch was queued to spend a second request on it.
      await flushAndSettle(tester, h);
      expect(h.transport.countOf(opListOrders), 2);
    });

    testWidgets('forwards tags to the tag graph', (tester) async {
      var served = 0;
      final h = harness((_, _) => [order(1, ++served)]);
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          totalOf(listOrders(const ListOrdersArgs())),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);

      context.forgeInvalidateTags(['Order[]']);
      await flushAndSettle(tester, h);

      expect(find.text('total:2'), findsOneWidget);
    });

    test(
      'keeps its identity across re-renders, so it is safe in a dependency array',
      () {},
      skip: 'forgeInvalidate is an extension method on BuildContext; there is no returned function whose identity could change.',
    );

    testWidgets('resolves its cache explicitly, then provided, then global', (tester) async {
      final global = harness((_, _) => [order(1, 1)]);
      final provided = harness((_, _) => [order(1, 2)]);
      final explicit = harness((_, _) => [order(1, 3)]);
      setClient(global.cache);
      late BuildContext context;

      // One mounted list in each cache, so each has something to refresh.
      await tester.pumpWidget(scope(
        provided,
        Column(children: [
          totalOf(listOrders(const ListOrdersArgs()), client: global.cache),
          totalOf(listOrders(const ListOrdersArgs())),
          totalOf(listOrders(const ListOrdersArgs()), client: explicit.cache),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);
      expect(global.transport.countOf(opListOrders), 1);

      await context.forgeRefetch(listOrders);
      await refetchBinding(explicit.cache, listOrders);

      expect(provided.transport.countOf(opListOrders), 2);
      expect(explicit.transport.countOf(opListOrders), 2);
      // The scope beat the global, and the explicit cache beat the scope.
      expect(global.transport.countOf(opListOrders), 1);
    });

    // The cases below have no counterpart in the React suite. The Dart port
    // matches over the registry, as TS does, so it also reaches queries that
    // have not settled successfully.

    testWidgets('marks every variant stale through the plain function, for code with no context', (tester) async {
      var served = 0;
      final h = harness((_, _) => [order(1, ++served)]);

      await tester.pumpWidget(scope(h, totalOf(listOrders(const ListOrdersArgs()))));
      await settle(tester);

      invalidateBinding(h.cache, listOrders);
      await flushAndSettle(tester, h);

      expect(find.text('total:2'), findsOneWidget);
    });

    testWidgets('rejects the forgeRefetch future, rather than throwing, when no client is configured', (tester) async {
      late BuildContext context;
      await tester.pumpWidget(ltr(_Grab((c) => context = c)));

      late Future<void> refetch;
      expect(() => refetch = context.forgeRefetch(listOrders), returnsNormally);
      await expectLater(refetch, throwsA(isA<StateError>()));
    });

    testWidgets('retries a query whose last fetch failed', (tester) async {
      var served = 0;
      final h = harness((_, _) {
        if (++served == 1) throw const Boom('down');
        return [order(1, served)];
      });
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          listOf(listOrders(const ListOrdersArgs())),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);

      // Not in `QueryCache.queries`, which lists settled successes only.
      expect(find.text('error:-'), findsOneWidget);
      expect(h.cache.queries, isEmpty);

      context.forgeInvalidate(listOrders);
      await flushAndSettle(tester, h);

      expect(h.transport.countOf(opListOrders), 2);
      expect(find.text('success:2'), findsOneWidget);
    });

    testWidgets('retries a failed query through forgeRefetch, and a second failure rejects', (tester) async {
      var served = 0;
      final h = harness((_, _) {
        if (served++ < 2) throw const Boom('down');
        return [order(1, served)];
      });
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          listOf(listOrders(const ListOrdersArgs())),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);
      expect(find.text('error:-'), findsOneWidget);

      // The second attempt fails too, so the awaited form hands it back.
      await expectLater(context.forgeRefetch(listOrders), throwsA(isA<Boom>()));
      await tester.pump();
      expect(find.text('error:-'), findsOneWidget);

      await context.forgeRefetch(listOrders);
      await tester.pump();

      expect(find.text('success:3'), findsOneWidget);
    });

    testWidgets('restarts a query whose first fetch is still in flight', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? gate.future : [order(1, 2)]);
      late BuildContext context;

      await tester.pumpWidget(scope(
        h,
        Column(children: [
          listOf(listOrders(const ListOrdersArgs())),
          _Grab((c) => context = c),
        ]),
      ));
      await settle(tester);
      expect(find.text('loading:-'), findsOneWidget);
      expect(h.cache.queries, isEmpty);

      // The answer on its way predates the write, so it must not be trusted.
      context.forgeInvalidate(listOrders);
      gate.complete([order(1, 1)]);
      await flushAndSettle(tester, h);

      expect(h.transport.countOf(opListOrders), 2);
      expect(find.text('success:2'), findsOneWidget);
    });

    testWidgets('refetch touches only mounted entries and marks unmounted ones stale', (tester) async {
      late final Harness h;
      h = harness((request, _) => order(idOf(request), h.transport.calls.length));
      late BuildContext context;

      Widget screen({required bool second}) => scope(
        h,
        Column(children: [
          _Grab((c) => context = c),
          ForgeQueryBuilder(query: getOrder(const OrderArgs(1)), builder: (c, s) => Text('one:${orderText(s)}')),
          if (second)
            ForgeQueryBuilder(query: getOrder(const OrderArgs(2)), builder: (c, s) => Text('two:${orderText(s)}')),
        ]),
      );

      await tester.pumpWidget(screen(second: true));
      await settle(tester);
      expect(h.transport.countOf(opGetOrder), 2);

      // Variant 2 stays in the cache, with nobody watching it.
      await tester.pumpWidget(screen(second: false));
      await settle(tester);

      await context.forgeRefetch(getOrder);
      await tester.pump();

      // Only the mounted variant was fetched.
      expect(h.transport.countOf(opGetOrder), 3);
      expect(idOf(h.transport.calls.last), 1);
      expect(h.cache.registry.get(h.cache.key(opGetOrder, const OrderArgs(2).toTagContext()))?.stale, isTrue);

      // And the unmounted one pays when it is next shown.
      await tester.pumpWidget(screen(second: true));
      await flushAndSettle(tester, h);

      expect(h.transport.countOf(opGetOrder), 4);
      expect(idOf(h.transport.calls.last), 2);
    });
  });
}
