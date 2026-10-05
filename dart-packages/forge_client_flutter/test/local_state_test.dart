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

/// Set by the client-swap test: the next computation of [swapFirst] throws
/// before reading anything.
var failNextSwapCompute = false;

final swapFirst = ForgeComputedKey<int?>((read) {
  if (failNextSwapCompute) {
    failNextSwapCompute = false;
    throw StateError('swap failure');
  }
  return read.query(getOrder(const OrderArgs(1))).dataOrNull?.total;
}, debugLabel: 'swapFirst');

final swapSecond = ForgeComputedKey<int?>(
  (read) => read.query(getOrder(const OrderArgs(2))).dataOrNull?.total,
  debugLabel: 'swapSecond',
);

final staleFlag = ForgeStateKey<bool>(() => false, debugLabel: 'staleFlag');

/// The reviewer's shape: Y reads order 1 with a short staleTime once the flag
/// is set, and X reads order 1 with a long one and then Y.
final cycleFreeY = ForgeComputedKey<bool>((read) {
  final flag = read.state(staleFlag);
  if (flag) read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(milliseconds: 1));
  return flag;
}, debugLabel: 'cycleFreeY');

final cycleFreeX = ForgeComputedKey<String>((read) {
  final state = read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(minutes: 1));
  return '${state.isFetching} ${read.computed(cycleFreeY)}';
}, debugLabel: 'cycleFreeX');

/// Whether order 1 is fetching, read with a long staleTime.
final order1Fetching = ForgeComputedKey<bool>(
  (read) => read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(minutes: 1)).isFetching,
  debugLabel: 'order1Fetching',
);

/// Reads [order1Fetching] first, then order 1 with a short staleTime, which
/// refetches and flips the value it read first.
final readsComputedThenStale = ForgeComputedKey<bool>((read) {
  final fetching = read.computed(order1Fetching);
  read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(milliseconds: 1));
  return fetching;
}, debugLabel: 'readsComputedThenStale');

final mirrorOn = ForgeStateKey<bool>(() => false, debugLabel: 'mirrorOn');

/// Set by the throw-before-reading test.
var throwBeforeReading = false;

final input = ForgeStateKey<int>(() => 1, debugLabel: 'input');

final guardedDouble = ForgeComputedKey<int>((read) {
  if (throwBeforeReading) throw StateError('threw before reading');
  return read.state(input) * 2;
}, debugLabel: 'guardedDouble');

final boundedTotal = ForgeComputedKey<int?>((read) {
  final total = read.query(getOrder(const OrderArgs(1))).dataOrNull?.total;
  if (total != null && total > 100) throw StateError('total $total is out of range');
  return total;
}, debugLabel: 'boundedTotal');

/// Order 1's isFetching, for the flush-order test.
final orderedY = ForgeComputedKey<bool>(
  (read) => read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(minutes: 1)).isFetching,
  debugLabel: 'orderedY',
);

/// Reads order 1 before [orderedY], so its subscription is notified first.
final orderedX = ForgeComputedKey<String>((read) {
  final state = read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(minutes: 1));
  return '${state.isFetching} ${read.computed(orderedY)}';
}, debugLabel: 'orderedX');

final refetchNow = ForgeStateKey<bool>(() => false, debugLabel: 'refetchNow');

/// Refetches order 1 from inside a computation once [refetchNow] is set.
final refetcher = ForgeComputedKey<int>((read) {
  if (read.state(refetchNow)) {
    read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(milliseconds: 1));
  }
  return 0;
}, debugLabel: 'refetcher');

/// Set by the fix round 2 tests: the next computation of [movedTotal] throws.
var failNextMove = false;

final movedTotal = ForgeComputedKey<int?>((read) {
  if (failNextMove) {
    failNextMove = false;
    throw StateError('failed on the new client');
  }
  return read.query(getOrder(const OrderArgs(1))).dataOrNull?.total;
}, debugLabel: 'movedTotal');

/// How many times [movedDoubled] has computed.
var movedDoubledComputes = 0;

final movedDoubled = ForgeComputedKey<int?>((read) {
  movedDoubledComputes++;
  final total = read.computed(movedTotal);
  return total == null ? null : total * 2;
}, debugLabel: 'movedDoubled');

/// Order 2's total: the fallback [fallbackX] reads.
final fallbackW = ForgeComputedKey<int?>(
  (read) => read.query(getOrder(const OrderArgs(2))).dataOrNull?.total,
  debugLabel: 'fallbackW',
);

/// Order 1's total, falling back to [fallbackW] while order 1 has no data.
/// On the old client it never reads W, so last time's reads cannot order it.
final fallbackX = ForgeComputedKey<int?>((read) {
  final total = read.query(getOrder(const OrderArgs(1))).dataOrNull?.total;
  return total ?? read.computed(fallbackW);
}, debugLabel: 'fallbackX');

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

      // Listened to from the moment it is read, so the first pass's own write
      // already restarts it. The loop is reported and stopped, not run forever.
      expect(tester.takeException(), isA<StateError>());
      expect(computed.value, lessThan(200));

      tickState.value = 1000;
      expect(tester.takeException(), isA<StateError>());
    });
  });

  group('ForgeComputed fix round 1', () {
    testWidgets('moves every computed to a new client even when one throws there', (tester) async {
      final a = byId();
      final b = byId(factor: 70);
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();

      Widget tree(Harness h) => ForgeScope(
        client: h.cache,
        focus: focus,
        connectivity: connectivity,
        child: ltr(Builder(builder: (context) {
          final first = context.forgeComputed(swapFirst);
          final second = context.forgeComputed(swapSecond);
          return ListenableBuilder(
            listenable: Listenable.merge([first, second]),
            builder: (context, _) => Text('${first.value ?? '-'}/${second.value ?? '-'}'),
          );
        })),
      );

      await tester.pumpWidget(tree(a));
      await settle(tester);
      expect(find.text('10/20'), findsOneWidget);
      expect(a.cache.registry.mounted, 2);

      final errors = <FlutterErrorDetails>[];
      final onError = FlutterError.onError;
      FlutterError.onError = errors.add;
      try {
        failNextSwapCompute = true;
        await tester.pumpWidget(tree(b));
        await settle(tester);
      } finally {
        FlutterError.onError = onError;
      }

      // Reported, not thrown out of didUpdateWidget. The build that read the
      // failed value raised it again before the new client's data arrived.
      expect(failNextSwapCompute, isFalse);
      expect(errors, isNotEmpty);
      expect(errors.map((details) => details.exception), everyElement(isA<StateError>()));
      expect(errors.first.context.toString(), contains('moving'));
      expect(find.text('70/140'), findsOneWidget);
      expect(b.transport.calls.map(idOf), unorderedEquals([1, 2]));

      // Nothing is left mounted on the old client, and invalidating there
      // refetches nothing.
      expect(a.cache.registry.mounted, 0);
      await invalidateAndSettle(tester, a, ['Order:1', 'Order:2']);
      expect(a.transport.countOf(opGetOrder), 2);
    });

    testWidgets('reports no cycle when a dependency changes while another computed is computing', (tester) async {
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
      final x = context.forgeComputed(cycleFreeX);
      await settle(tester);
      expect(x.value, 'false false');

      // Y's new stale read refetches, which reaches X's subscription while Y
      // is still computing. X must wait for Y rather than read it mid-compute.
      clock.advance(const Duration(seconds: 1));
      context.forgeState(staleFlag).value = true;

      expect(tester.takeException(), isNull);
      expect(x.value, 'true true');

      gate.complete(order(1, 10));
      await settle(tester);
      expect(x.value, 'false true');
    });

    testWidgets('sees a computed it first read flip while the same computation runs', (tester) async {
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
      // B is already live, so the refetch reaches it synchronously.
      final b = context.forgeComputed(order1Fetching);
      await settle(tester);
      expect(b.value, isFalse);
      clock.advance(const Duration(seconds: 1));

      // A reads B first, then refetches through its own stale read, which
      // flips B before A's first computation returns.
      final a = context.forgeComputed(readsComputedThenStale);

      expect(b.value, isTrue);
      expect(a.value, isTrue);

      gate.complete(order(1, 10));
      await settle(tester);
      expect(a.value, isFalse);
    });

    testWidgets('sees a listenable it first read change while the same computation runs', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness(
        (request, call) => call == 0 ? order(idOf(request), 10) : gate.future,
        clock: clock,
      );
      // App code mirroring the query's isFetching into a plain notifier.
      final mirror = ValueNotifier<bool>(false);
      final subscription = getOrder(const OrderArgs(1))
          .watch(h.cache, staleTime: const Duration(minutes: 1))
          .listen((state) => mirror.value = state.isFetching);
      final mirrored = ForgeComputedKey<bool>((read) {
        if (!read.state(mirrorOn)) return false;
        final fetching = read.listen(mirror);
        read.query(getOrder(const OrderArgs(1)), staleTime: const Duration(milliseconds: 1));
        return fetching;
      });
      late BuildContext context;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        return const SizedBox();
      })));
      final computed = context.forgeComputed(mirrored);
      await settle(tester);
      expect(computed.value, isFalse);

      // The pass reads the mirror for the first time, then refetches, which
      // flips the mirror before the pass returns.
      clock.advance(const Duration(seconds: 1));
      context.forgeState(mirrorOn).value = true;

      expect(mirror.value, isTrue);
      expect(computed.value, isTrue);

      gate.complete(order(1, 10));
      await settle(tester);
      expect(computed.value, isFalse);

      unawaited(subscription.cancel());
      mirror.dispose();
    });

    testWidgets('keeps its dependencies through a pass that threw before reading them', (tester) async {
      final h = harness((_, _) => null);
      late BuildContext context;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        return const SizedBox();
      })));
      final computed = context.forgeComputed(guardedDouble);
      expect(computed.value, 2);

      throwBeforeReading = true;
      context.forgeState(input).value = 2;
      expect(tester.takeException(), isA<StateError>());
      expect(computed.value, 2);

      // Still listening to the input the throwing pass never reached.
      throwBeforeReading = false;
      context.forgeState(input).value = 3;
      expect(computed.value, 6);
    });

    testWidgets('keeps the old value, reports a recompute error from a query, and recovers', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((request, call) => switch (call) {
        0 => order(idOf(request), 10),
        1 => order(idOf(request), 1000),
        _ => gate.future,
      });
      late BuildContext context;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        return const SizedBox();
      })));
      final computed = context.forgeComputed(boundedTotal);
      await settle(tester);
      expect(computed.value, 10);

      // Delivered by the query's subscription, reported rather than escaping
      // to the zone, and the old value stays.
      await invalidateAndSettle(tester, h, ['Order:1']);
      expect(tester.takeException(), isA<StateError>());
      expect(computed.value, 10);

      // The refetch's in-flight state still carries 1000, so that pass throws
      // too; the answer clears it.
      await invalidateAndSettle(tester, h, ['Order:1']);
      expect(tester.takeException(), isA<StateError>());
      expect(computed.value, 10);

      gate.complete(order(1, 20));
      await settle(tester);
      expect(tester.takeException(), isNull);
      expect(computed.value, 20);
    });

    testWidgets('recomputes dirty values after the dirty values they read', (tester) async {
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
      final x = context.forgeComputed(orderedX);
      context.forgeComputed(refetcher);
      await settle(tester);
      expect(x.value, 'false false');

      final seen = <String>[];
      x.addListener(() => seen.add(x.value));

      // The refetch starts inside the refetcher's computation, so X and then
      // Y are marked dirty in one batch. Y goes first, so X never computes
      // against a stale Y.
      clock.advance(const Duration(seconds: 1));
      context.forgeState(refetchNow).value = true;

      expect(seen, ['true true']);

      gate.complete(order(1, 10));
      await settle(tester);
      expect(x.value, 'false false');
    });

    testWidgets('rethrows an error from the first read', (tester) async {
      throwBeforeReading = true;
      addTearDown(() => throwBeforeReading = false);
      final h = harness((_, _) => null);

      await tester.pumpWidget(scope(h, Builder(builder: (context) {
        context.forgeComputed(guardedDouble);
        return const SizedBox();
      })));

      expect(tester.takeException(), isA<StateError>());
    });
  });

  group('ForgeComputed fix round 2', () {
    testWidgets('exposes no old-client value after failing on a new client, then recovers', (tester) async {
      final a = byId();
      final gate = Completer<Object?>();
      final b = harness((_, _) => gate.future);
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();
      late ForgeComputed<int?> total;
      late ForgeComputed<int?> doubled;

      Widget tree(Harness h) => ForgeScope(
        client: h.cache,
        focus: focus,
        connectivity: connectivity,
        child: ltr(Builder(builder: (context) {
          // The dependent is created first, so a move that went in creation
          // order would recompute it against the old total.
          doubled = context.forgeComputed(movedDoubled);
          total = context.forgeComputed(movedTotal);
          return ListenableBuilder(
            listenable: total,
            builder: (context, _) => Text('total:${total.value}'),
          );
        })),
      );

      await tester.pumpWidget(tree(a));
      await settle(tester);
      expect(find.text('total:10'), findsOneWidget);
      expect(doubled.value, 20);

      var totalNotified = 0;
      var doubledNotified = 0;
      total.addListener(() => totalNotified++);
      doubled.addListener(() => doubledNotified++);

      final errors = <FlutterErrorDetails>[];
      final onError = FlutterError.onError;
      FlutterError.onError = errors.add;
      try {
        failNextMove = true;
        movedDoubledComputes = 0;
        await tester.pumpWidget(tree(b));

        // The move recomputed the total before the value that reads it, so
        // the dependent ran once, against the failure, never against the old
        // client's total.
        expect(movedDoubledComputes, 1);

        // (a) Nothing derived from the old client is readable: not the value,
        // not a value that reads it, not the widget.
        expect(() => total.value, throwsStateError);
        expect(() => doubled.value, throwsStateError);
        expect(totalNotified, 1);
        expect(doubledNotified, 1);
        expect(find.text('total:10'), findsNothing);
        expect(find.byType(ErrorWidget), findsOneWidget);
        expect(errors, isNotEmpty);
        expect(errors.map((details) => details.exception), everyElement(isA<StateError>()));
        expect(errors.first.context.toString(), contains('moving'));
        expect(a.cache.registry.mounted, 0);

        // (b) The new client's answer recovers it and wakes its listeners,
        // even though the total happens to equal the old one.
        errors.clear();
        gate.complete(order(1, 10));
        await settle(tester);
      } finally {
        FlutterError.onError = onError;
      }

      expect(errors, isEmpty);
      expect(total.value, 10);
      expect(doubled.value, 20);
      expect(totalNotified, 2);
      expect(doubledNotified, 2);
      expect(find.text('total:10'), findsOneWidget);
      expect(b.transport.countOf(opGetOrder), 1);
    });

    testWidgets('keeps the old value on an error within the same client', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((request, call) => call == 0 ? order(idOf(request), 10) : gate.future);
      late BuildContext context;

      await tester.pumpWidget(scope(h, Builder(builder: (c) {
        context = c;
        return const SizedBox();
      })));
      final total = context.forgeComputed(movedTotal);
      await settle(tester);
      expect(total.value, 10);

      // No client change, so round 1's rule: the refetch's isFetching pass
      // throws, which is reported, and the value stays readable while the
      // refetch is held.
      failNextMove = true;
      await invalidateAndSettle(tester, h, ['Order:1']);
      expect(tester.takeException(), isA<StateError>());
      expect(total.value, 10);

      gate.complete(order(1, 30));
      await settle(tester);
      expect(total.value, 30);
    });
  });

  group('ForgeComputed fix round 3', () {
    testWidgets('never shows an old-client value through a computed it first reads during a move', (tester) async {
      final a = byId();
      final gate = Completer<void>();
      final b = harness((request, _) => gate.future.then((_) => order(idOf(request), (idOf(request)! as int) * 70)));
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();
      late ForgeComputed<int?> x;
      late ForgeComputed<int?> w;

      Widget tree(Harness h) => ForgeScope(
        client: h.cache,
        focus: focus,
        connectivity: connectivity,
        child: ltr(Builder(builder: (context) {
          // X first, so it is ahead of W in the move's batch.
          x = context.forgeComputed(fallbackX);
          w = context.forgeComputed(fallbackW);
          return const SizedBox();
        })),
      );

      await tester.pumpWidget(tree(a));
      await settle(tester);
      expect(x.value, 10);
      expect(w.value, 20);

      final seen = <int?>[];
      x.addListener(() => seen.add(x.value));

      // On the gated client order 1 has no data, so X falls back to W, which
      // still holds the old client's 20 until it recomputes.
      await tester.pumpWidget(tree(b));
      expect(seen, [null]);
      expect(w.value, isNull);

      gate.complete();
      await settle(tester);
      expect(seen, [null, 70]);
      expect(seen, isNot(contains(20)));
      expect(w.value, 140);
    });
  });

  group('ForgeComputed across setPrincipal', () {
    testWidgets('never hands a synchronous listener a value mixing in the previous principal\'s data', (tester) async {
      final h = principalHarness();
      final key = ForgeComputedKey<String>((read) {
        final a = read.query(getOrder(const OrderArgs(1))).dataOrNull?.total;
        final b = read.query(getOrder(const OrderArgs(2))).dataOrNull?.total;
        return 'a=$a b=$b';
      }, debugLabel: 'two orders');
      late ForgeComputed<String> computed;

      await tester.pumpWidget(scope(h, Builder(builder: (context) {
        computed = context.forgeComputed(key);
        return const SizedBox();
      })));
      await settle(tester);
      expect(computed.value, 'a=101 b=102');

      final seen = <String>[];
      computed.addListener(() => seen.add('${h.cache.principal}: ${computed.value}'));

      // Order 1's clear notification recomputes the value while order 2's
      // has not arrived yet.
      h.cache.setPrincipal('bob');
      expect(seen, isNotEmpty);
      expect(seen.where(showsAlice), isEmpty, reason: '$seen');

      await settle(tester);
      expect(seen.where(showsAlice), isEmpty, reason: '$seen');
      expect(seen.last, 'bob: a=201 b=202');
    });
  });
}
