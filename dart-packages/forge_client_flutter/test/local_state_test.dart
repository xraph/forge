import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';

import 'support/harness.dart';

final counter = ForgeStateKey<int>(() => 0, debugLabel: 'counter');
final selectedId = ForgeStateKey<int>(() => 1, debugLabel: 'selectedId');

final selectedTotal = ForgeComputedKey<int?>(
  (read) => read.query(getOrder(OrderArgs(read.state(selectedId)))).dataOrNull?.total,
  debugLabel: 'selectedTotal',
);

final doubledTotal = ForgeComputedKey<int?>((read) {
  final total = read.computed(selectedTotal);
  return total == null ? null : total * 2;
}, debugLabel: 'doubledTotal');

final orderIds = ForgeComputedKey<List<int>?>(
  (read) => read.query(listOrders(const ListOrdersArgs())).dataOrNull?.map((o) => o.id).toList(),
  debugLabel: 'orderIds',
);

final ForgeComputedKey<int> cycleA = ForgeComputedKey((read) => read.computed(cycleB) + 1);
final ForgeComputedKey<int> cycleB = ForgeComputedKey((read) => read.computed(cycleA) + 1);

/// Total is ten times the id, so a test can tell which order it is showing.
Harness byId({int factor = 10}) =>
    harness((request, _) => order(idOf(request), (idOf(request)! as int) * factor));

Widget showTotal(void Function(BuildContext context) grab) => Builder(builder: (context) {
  grab(context);
  return ValueListenableBuilder<int?>(
    valueListenable: context.forgeComputed(selectedTotal),
    builder: (context, total, _) => Text('total:${total ?? '-'}'),
  );
});

Future<void> invalidateAndSettle(WidgetTester tester, Harness h, List<String> tags) async {
  h.cache.invalidate(tags);
  h.scheduler.flush();
  await settle(tester);
}

const _stale = Duration(milliseconds: 50);

/// The order list as text, with its isFetching flag, so a refetch that
/// returns the same rows still changes the value and notifies.
String _listWithFetching(ForgeReader read) {
  final state = read.query(listOrders(const ListOrdersArgs()), staleTime: _stale);
  return '${listText(state)}${state.isFetching ? ' fetching' : ''}';
}

/// Two keys over the same query, so two computed values watch one record.
const listView = ForgeComputedKey<String>(_listWithFetching, debugLabel: 'listView');
const detailView = ForgeComputedKey<String>(_listWithFetching, debugLabel: 'detailView');

Widget showComputed(String label, ForgeComputedKey<String> key) => Builder(
  builder: (context) => ValueListenableBuilder<String>(
    valueListenable: context.forgeComputed(key),
    builder: (context, value, _) => Text('$label $value'),
  ),
);

final tick = ForgeStateKey<int>(() => 0, debugLabel: 'tick');

/// Set by the test that needs it: the state [selfWriter] writes.
late ForgeState<int> tickState;

/// A broken compute function: it writes the state it reads.
final selfWriter = ForgeComputedKey<int>((read) {
  final n = read.state(tick);
  tickState.value = n + 1;
  return n;
}, debugLabel: 'selfWriter');

final secondReader = ForgeStateKey<bool>(() => false, debugLabel: 'secondReader');

/// Reads order 1 with a long staleTime, then, once [secondReader] is set,
/// again with a short one, which refetches and so changes the first read's
/// state while this computation is still running.
final heldIsFetching = ForgeComputedKey<bool>((read) {
  final held = read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(minutes: 1));
  if (read.state(secondReader)) {
    read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(milliseconds: 1));
  }
  return held.isFetching;
}, debugLabel: 'heldIsFetching');

void main() {
  group('ForgeState', () {
    testWidgets('creates a state once per scope and keeps it across rebuilds', (tester) async {
      final h = harness((_, _) => null);
      final seen = <ForgeState<int>>[];

      Widget tree() => scope(h, Builder(builder: (context) {
        seen.add(context.forgeState(counter));
        return const SizedBox();
      }));

      await tester.pumpWidget(tree());
      await tester.pumpWidget(tree());

      expect(seen, hasLength(2));
      expect(seen.first, same(seen.last));
      expect(seen.first.value, 0);
    });

    testWidgets('gives two scopes two independent states', (tester) async {
      final outer = harness((_, _) => null);
      final inner = harness((_, _) => null);
      late ForgeState<int> outerState;
      late ForgeState<int> innerState;

      await tester.pumpWidget(scope(
        outer,
        Builder(builder: (context) {
          outerState = context.forgeState(counter);
          return scope(inner, Builder(builder: (context) {
            innerState = context.forgeState(counter);
            return const SizedBox();
          }));
        }),
      ));

      expect(outerState, isNot(same(innerState)));
    });

    testWidgets('notifies listeners on change and not on an equal value', (tester) async {
      final h = harness((_, _) => null);
      late ForgeState<int> state;

      await tester.pumpWidget(scope(h, Builder(builder: (context) {
        state = context.forgeState(counter);
        return const SizedBox();
      })));

      var notified = 0;
      state.addListener(() => notified++);

      state.value = 0;
      expect(notified, 0);

      state.update((n) => n + 1);
      expect(notified, 1);
      expect(state.value, 1);
    });

    testWidgets('throws a clear error without a scope', (tester) async {
      await tester.pumpWidget(Builder(builder: (context) {
        context.forgeState(counter);
        return const SizedBox();
      }));

      expect(tester.takeException(), isA<StateError>());
    });
  });

  group('ForgeComputed', () {
    testWidgets('derives a value from state and a query together', (tester) async {
      final h = byId();
      late BuildContext context;

      await tester.pumpWidget(scope(h, showTotal((c) => context = c)));
      // Seeded from getState on the first frame: loading, so no total yet.
      expect(find.text('total:-'), findsOneWidget);

      await settle(tester);
      expect(find.text('total:10'), findsOneWidget);

      context.forgeState(selectedId).value = 2;
      await settle(tester);
      expect(find.text('total:20'), findsOneWidget);
    });

    testWidgets('releases the queries a computed stopped reading', (tester) async {
      final h = byId();
      late BuildContext context;

      await tester.pumpWidget(scope(h, showTotal((c) => context = c)));
      await settle(tester);
      context.forgeState(selectedId).value = 2;
      await settle(tester);

      await invalidateAndSettle(tester, h, ['Order:1', 'Order:2']);

      // Order 1 is no longer read, so only order 2 refetched.
      expect(h.transport.calls.map(idOf), [1, 2, 2]);
    });

    testWidgets('does not notify when the computed value is unchanged', (tester) async {
      final h = byId();
      late BuildContext context;

      await tester.pumpWidget(scope(h, showTotal((c) => context = c)));
      await settle(tester);

      var notified = 0;
      context.forgeComputed(selectedTotal).addListener(() => notified++);

      // The refetch flips isFetching and returns the same total.
      await invalidateAndSettle(tester, h, ['Order:1']);

      expect(h.transport.countOf(opGetOrder), 2);
      expect(notified, 0);
    });

    testWidgets('deep-compares computed collections', (tester) async {
      var rows = [order(1, 10), order(2, 20)];
      final h = harness((_, _) => rows);
      late ForgeComputed<List<int>?> ids;

      await tester.pumpWidget(scope(h, Builder(builder: (context) {
        ids = context.forgeComputed(orderIds);
        return const SizedBox();
      })));
      await settle(tester);
      expect(ids.value, [1, 2]);

      var notified = 0;
      ids.addListener(() => notified++);

      // A new list of the same ids.
      rows = [order(1, 11), order(2, 21)];
      await invalidateAndSettle(tester, h, ['Order[]']);
      expect(notified, 0);

      rows = [order(1, 11), order(2, 21), order(3, 30)];
      await invalidateAndSettle(tester, h, ['Order[]']);
      expect(notified, 1);
      expect(ids.value, [1, 2, 3]);
    });

    testWidgets('depends on another computed', (tester) async {
      final h = byId();
      late BuildContext context;
      late ForgeComputed<int?> doubled;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        doubled = c.forgeComputed(doubledTotal);
        return const SizedBox();
      })));
      await settle(tester);
      expect(doubled.value, 20);

      context.forgeState(selectedId).value = 2;
      await settle(tester);
      expect(doubled.value, 40);
    });

    testWidgets('reads any ValueListenable as UI state', (tester) async {
      final h = harness((_, _) => null);
      final filter = ValueNotifier<int>(1);
      final tripled = ForgeComputedKey<int>((read) => read.listen(filter) * 3);
      late ForgeComputed<int> computed;

      await tester.pumpWidget(scope(h, Builder(builder: (context) {
        computed = context.forgeComputed(tripled);
        return const SizedBox();
      })));
      expect(computed.value, 3);

      filter.value = 2;
      expect(computed.value, 6);
      filter.dispose();
    });

    testWidgets('reports a cycle instead of overflowing the stack', (tester) async {
      final h = harness((_, _) => null);
      late BuildContext context;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        return const SizedBox();
      })));

      expect(() => context.forgeComputed(cycleA), throwsStateError);
    });

    testWidgets('disposes its states and computeds, and releases their queries, with the scope', (tester) async {
      final h = byId();
      late ForgeState<int> state;
      late ForgeComputed<int?> computed;

      await tester.pumpWidget(scope(h, Builder(builder: (context) {
        state = context.forgeState(selectedId);
        computed = context.forgeComputed(selectedTotal);
        return const SizedBox();
      })));
      await settle(tester);

      await tester.pumpWidget(const SizedBox());

      expect(() => ChangeNotifier.debugAssertNotDisposed(state), throwsFlutterError);
      expect(() => ChangeNotifier.debugAssertNotDisposed(computed), throwsFlutterError);

      await invalidateAndSettle(tester, h, ['Order:1']);
      expect(h.transport.countOf(opGetOrder), 1);
    });

    // Review Focus 3, for derived state.
    testWidgets('follows the scope to a new client', (tester) async {
      final a = byId();
      final b = byId(factor: 70);

      Widget tree(Harness h) => ForgeScope(
        client: h.cache,
        focus: FakeFocusSignal(),
        connectivity: FakeConnectivitySignal(),
        child: ltr(showTotal((_) {})),
      );

      await tester.pumpWidget(tree(a));
      await settle(tester);
      expect(find.text('total:10'), findsOneWidget);

      await tester.pumpWidget(tree(b));
      await settle(tester);
      expect(find.text('total:70'), findsOneWidget);

      // The old client was released.
      await invalidateAndSettle(tester, a, ['Order:1']);
      expect(a.transport.countOf(opGetOrder), 1);
    });
  });

  // Ruling R7: the cache notifies synchronously, so first reading a computed
  // value during a build, onto a stale query something else watches, starts
  // a fetch whose isFetching event reaches the other watcher mid-build.
  group('ForgeComputed during a build', () {
    testWidgets('first-reads a computed mid-build next to another computed on the same stale query', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);

      Widget screen({required bool detail}) => scope(
        h,
        Column(children: [
          showComputed('list', listView),
          if (detail) showComputed('detail', detailView),
        ]),
      );

      await tester.pumpWidget(screen(detail: false));
      await settle(tester);
      expect(find.text('list success:10'), findsOneWidget);

      // Past the staleTime, so the detail's first read refetches.
      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(detail: true));

      // No "setState() or markNeedsBuild() called during build".
      expect(tester.takeException(), isNull);
      expect(h.transport.countOf(opListOrders), 2);

      await tester.pump();
      expect(find.text('list success:10 fetching'), findsOneWidget);
      expect(find.text('detail success:10 fetching'), findsOneWidget);

      gate.complete([order(1, 20)]);
      await settle(tester);

      expect(find.text('list success:20'), findsOneWidget);
      expect(find.text('detail success:20'), findsOneWidget);
    });

    testWidgets('first-reads a computed mid-build next to a builder on the same stale query', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);

      Widget screen({required bool detail}) => scope(
        h,
        Column(children: [
          ForgeQueryBuilder(
            query: listOrders(const ListOrdersArgs()),
            staleTime: _stale,
            builder: (context, state) => Text('list ${listText(state)}'),
          ),
          if (detail) showComputed('detail', detailView),
        ]),
      );

      await tester.pumpWidget(screen(detail: false));
      await settle(tester);
      expect(find.text('list success:10'), findsOneWidget);

      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(detail: true));

      expect(tester.takeException(), isNull);
      expect(h.transport.countOf(opListOrders), 2);

      gate.complete([order(1, 20)]);
      await settle(tester);

      expect(find.text('list success:20'), findsOneWidget);
      expect(find.text('detail success:20'), findsOneWidget);
    });
  });

  group('ForgeComputed reads', () {
    testWidgets('sees a change that a later read in the same computation caused', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness(
        (request, call) => call == 0 ? order(idOf(request), 10) : gate.future,
        clock: clock,
      );
      late BuildContext context;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        return const SizedBox();
      })));
      final computed = context.forgeComputed(heldIsFetching);
      await settle(tester);
      expect(computed.value, isFalse);

      // Stale for the second read only. Its listen starts a refetch while the
      // computation runs, after the first read's state was taken.
      clock.advance(const Duration(seconds: 1));
      context.forgeState(secondReader).value = true;

      // The refetch is already in flight, synchronously, and the value says so.
      expect(h.cache.peek(opGetOrder, const OrderArgs(1).toTagContext())!.isFetching, isTrue);
      expect(computed.value, isTrue);
      await tester.pump();
      expect(h.transport.countOf(opGetOrder), 2);

      gate.complete(order(1, 10));
      await settle(tester);
      expect(computed.value, isFalse);
    });

    testWidgets('reports a compute function that writes what it reads instead of looping', (tester) async {
      final h = harness((_, _) => null);
      late BuildContext context;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        return const SizedBox();
      })));
      tickState = context.forgeState(tick);
      final computed = context.forgeComputed(selfWriter);
      expect(computed.value, 0);

      // Now listened to, so each write restarts the computation.
      tickState.value = 5;

      expect(tester.takeException(), isA<StateError>());
    });
  });
}
