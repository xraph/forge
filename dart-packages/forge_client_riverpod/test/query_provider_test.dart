import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/testing.dart';
import 'package:forge_client_riverpod/forge_client_riverpod.dart';

import 'support/harness.dart';

final getOrderProvider = queryProvider(getOrder, name: 'getOrderProvider');
final listOrdersProvider = queryProvider(listOrders, name: 'listOrdersProvider');

/// A new, non-constant args object with the given status on every call.
ListOrdersArgs argsWithStatus(String status) => ListOrdersArgs(status: status);

/// A finite staleTime, so advancing the clock past it makes the next mount
/// refetch synchronously inside that mount.
const _stale = Duration(milliseconds: 50);

/// The list screen: the full state of the order list, so it re-renders on an
/// isFetching flip as well as on new data.
final class _ListScreen extends ConsumerWidget {
  const _ListScreen();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(listOrdersProvider.state(const ListOrdersArgs(), staleTime: _stale));
    return Text('list ${state.dataOrNull?.single.total} ${state.isFetching}');
  }
}

/// The detail screen: the same query through the other provider, so mounting
/// it initializes a provider and mounts the query again.
final class _DetailScreen extends ConsumerWidget {
  const _DetailScreen();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final value = ref.watch(listOrdersProvider(const ListOrdersArgs(), staleTime: _stale));
    return Text('detail ${value.value?.single.total}');
  }
}

Future<void> pumpAll(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump();
  }
}

void main() {
  group('queryProvider', () {
    test('yields AsyncLoading, then AsyncData', () async {
      final h = harness((request, _) => order(idOf(request), 5));
      final container = containerFor(h);
      final seen = <AsyncValue<Order>>[];

      container.listen(
        getOrderProvider(const OrderArgs(1)),
        (_, next) => seen.add(next),
        fireImmediately: true,
      );
      await settle();

      expect(seen.first, isA<AsyncLoading<Order>>());
      expect(seen.last, isA<AsyncData<Order>>());
      expect(seen.last.value, const Order(id: 1, total: 5));
    });

    test('serves a cached value as AsyncData on the very first read', () async {
      final h = harness((request, _) => order(idOf(request), 5));
      await getOrder(const OrderArgs(1)).fetch(h.cache);
      final container = containerFor(h);

      // No settle: the first value is seeded from getState, not from the
      // stream, whose first event arrives on a microtask.
      final first = container.read(getOrderProvider(const OrderArgs(1)));
      expect(first, isA<AsyncData<Order>>());
      expect(first.value, const Order(id: 1, total: 5));
      expect(container.read(getOrderProvider.state(const OrderArgs(1))), isA<QuerySuccess<Order>>());
      expect(h.transport.countOf(opGetOrder), 1);
    });

    test('serves two listeners on one query from a single request', () async {
      final h = harness((_, _) => [order(1, 99)]);
      final container = containerFor(h);

      container.listen(listOrdersProvider(const ListOrdersArgs()), (_, _) {});
      container.listen(listOrdersProvider(const ListOrdersArgs()), (_, _) {});
      await settle();
      expect(h.transport.countOf(opListOrders), 1);

      // One mount, so one invalidation is one refetch.
      await invalidate(h, ['Order[]']);
      expect(h.transport.countOf(opListOrders), 2);
    });

    test('holds the registry ref-count while watched and releases it on dispose', () async {
      final h = harness((_, _) => [order(1, 99)]);
      final container = containerFor(h);

      final subscription = container.listen(listOrdersProvider(const ListOrdersArgs()), (_, _) {});
      await settle();
      expect(h.transport.countOf(opListOrders), 1);

      subscription.close();
      await settle();

      // Disposed, so released: an invalidation only marks it stale.
      await invalidate(h, ['Order[]']);
      expect(h.transport.countOf(opListOrders), 1);

      // Watched again, it pays for the staleness it remembered. The re-listen
      // reports that staleness through the invalidator, so flush it (R6).
      container.listen(listOrdersProvider(const ListOrdersArgs()), (_, _) {});
      await invalidate(h, const []);
      expect(h.transport.countOf(opListOrders), 2);
    });

    // Review Focus 2.
    test('keys the family on the query key, so a new args object is the same provider', () async {
      final h = harness((_, _) => [order(1, 99)]);
      final container = containerFor(h);

      final a = argsWithStatus('open');
      final b = argsWithStatus('open');
      expect(identical(a, b), isFalse);
      expect(listOrdersProvider(a), listOrdersProvider(b));

      container.listen(listOrdersProvider(a), (_, _) {});
      container.listen(listOrdersProvider(b), (_, _) {});
      await settle();

      expect(h.transport.countOf(opListOrders), 1);
    });

    test('keys different query keys and options to different providers', () {
      // ListOrdersArgs has no `==`, so equal providers above can only come
      // from the query key; these show the key and the options still count.
      expect(listOrdersProvider(argsWithStatus('open')), isNot(listOrdersProvider(argsWithStatus('closed'))));
      expect(
        listOrdersProvider(argsWithStatus('open')),
        isNot(listOrdersProvider(argsWithStatus('open'), staleTime: _stale)),
      );
      expect(
        listOrdersProvider(argsWithStatus('open')),
        isNot(listOrdersProvider(argsWithStatus('open'), enabled: false)),
      );
      expect(listOrdersProvider.state(argsWithStatus('open')), listOrdersProvider.state(argsWithStatus('open')));
    });

    test('shares one notifier between args objects with the same query key', () async {
      final h = harness((_, _) => [order(1, 99)]);
      final container = containerFor(h);

      container.listen(listOrdersProvider(argsWithStatus('open')), (_, _) {});
      await settle();

      // One provider element, so one notifier, found through a second object.
      expect(
        container.read(listOrdersProvider(argsWithStatus('open')).notifier),
        same(container.read(listOrdersProvider(argsWithStatus('open')).notifier)),
      );
      expect(
        listOrdersProvider(argsWithStatus('open')).hashCode,
        listOrdersProvider(argsWithStatus('open')).hashCode,
      );
    });

    test('keeps the last good value beside an error from a failed refetch', () async {
      final h = harness((_, call) {
        if (call > 0) throw const Boom('boom');
        return [order(1, 99)];
      });
      final container = containerFor(h);
      final provider = listOrdersProvider(const ListOrdersArgs());

      container.listen(provider, (_, _) {});
      await settle();
      final good = container.read(provider).value;

      await expectLater(listOrders(const ListOrdersArgs()).refetch(h.cache), throwsA(isA<Boom>()));
      await settle();

      final value = container.read(provider);
      expect(value.hasError, isTrue);
      expect('${value.error}', 'boom');
      expect(value.value, same(good));
    });

    test('stays loading and fetches nothing while disabled', () async {
      final h = harness((request, _) => order(idOf(request), 5));
      final container = containerFor(h);

      container.listen(getOrderProvider(const OrderArgs(1), enabled: false), (_, _) {});
      await settle();

      expect(container.read(getOrderProvider(const OrderArgs(1), enabled: false)), isA<AsyncLoading<Order>>());
      expect(
        container.read(getOrderProvider.state(const OrderArgs(1), enabled: false)),
        isA<QueryIdle<Order>>(),
      );
      expect(h.transport.calls, isEmpty);
    });

    test('select suppresses notifications while the slice is unchanged', () async {
      var total = 10;
      final h = harness((request, _) => order(idOf(request), total));
      final container = containerFor(h);
      var notified = 0;

      container.listen(
        getOrderProvider(const OrderArgs(1)).select((value) => value.value?.total),
        (_, _) => notified++,
      );
      await settle();
      final before = notified;

      // Same total: the refetch runs and nothing is notified.
      await invalidate(h, ['Order:1']);
      expect(h.transport.countOf(opGetOrder), 2);
      expect(notified, before);

      total = 11;
      await invalidate(h, ['Order:1']);
      expect(notified, before + 1);
    });

    test('exposes the full QueryState, isFetching and syncStatus included, through .state', () async {
      final h = harness((request, _) => order(idOf(request), 5));
      final container = containerFor(h);
      final states = <QueryState<Order>>[];

      container.listen(
        getOrderProvider.state(const OrderArgs(1)),
        (_, next) => states.add(next),
        fireImmediately: true,
      );
      await settle();

      expect(states.first, isA<QueryLoading<Order>>());
      final current = getOrder(const OrderArgs(1)).getState(h.cache);
      // Passed through untouched: the same data object, the same flags.
      expect(states.last.dataOrNull, same(current.dataOrNull));
      expect(states.last.syncStatus, current.syncStatus);
      expect(states.last.isFetching, current.isFetching);
      expect(states.last.isOptimistic, current.isOptimistic);

      await invalidate(h, ['Order:1']);
      expect(states.any((s) => s.isFetching), isTrue);
    });

    // Review Focus 4, for the Riverpod adapter.
    test('revalidates stale queries on focus through the installed seams', () async {
      final clock = ManualClock();
      final h = harness((request, call) => order(idOf(request), 10 + call), clock: clock);
      final focus = FakeFocusSignal();
      final container = containerFor(h, focus: focus);

      container.listen(
        getOrderProvider(const OrderArgs(1), staleTime: const Duration(minutes: 1)),
        (_, _) {},
      );
      await settle();
      expect(h.transport.countOf(opGetOrder), 1);

      focus.blur();
      clock.advance(const Duration(hours: 3));
      focus.focus();
      await settle();

      expect(h.transport.countOf(opGetOrder), 2);
      expect(
        container.read(getOrderProvider(const OrderArgs(1), staleTime: const Duration(minutes: 1))).value?.total,
        11,
      );
    });

    test('treats live and not live as two providers over the same query', () async {
      final h = harness((request, _) => order(idOf(request), 5));
      final container = containerFor(h);

      expect(getOrderProvider(const OrderArgs(1), live: true), isNot(getOrderProvider(const OrderArgs(1))));

      container.listen(getOrderProvider(const OrderArgs(1), live: true), (_, _) {});
      container.listen(getOrderProvider(const OrderArgs(1)), (_, _) {});
      await settle();

      // Two providers, one cache query, one request.
      expect(h.transport.countOf(opGetOrder), 1);
    });
  });

  // Ruling R7. The cache notifies its listeners synchronously, so a provider
  // initialized during another provider's build, or during a widget build,
  // onto a stale query already watched elsewhere starts a fetch whose
  // isFetching transition reaches the other watcher in the middle of that
  // build. Riverpod asserts "Providers are not allowed to modify other
  // providers during their initialization".
  group('queryProvider mid-build updates', () {
    test('applies an update that arrives outside any build at once', () async {
      final h = harness((_, call) => [order(1, 10 + call)]);
      final container = containerFor(h);
      final list = listOrdersProvider.state(const ListOrdersArgs());

      container.listen(list, (_, _) {});
      await settle();

      h.cache.invalidate(['Order[]']);
      h.scheduler.flush();
      // No await: the refetch's isFetching flip is exposed already.
      expect(container.read(list).isFetching, isTrue);

      await settle();
      expect(container.read(list).dataOrNull?.single.total, 11);
    });

    test('initializes a provider inside another provider\'s build onto a stale, watched query', () async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);
      final container = containerFor(h);
      final list = listOrdersProvider.state(const ListOrdersArgs(), staleTime: _stale);
      final listValue = listOrdersProvider(const ListOrdersArgs(), staleTime: _stale);

      container.listen(list, (_, _) {});
      await settle();
      expect(container.read(list).dataOrNull?.single.total, 10);

      // Past the staleTime, so initializing the value provider refetches.
      clock.advance(const Duration(milliseconds: 100));
      final detail = Provider<AsyncValue<List<Order>>>((ref) => ref.watch(listValue));
      container.listen(detail, (_, _) {});

      // The isFetching transition reached `list` during `detail`'s build, so
      // it waits for a microtask.
      expect(container.read(list).isFetching, isFalse);
      await settle();
      expect(h.transport.countOf(opListOrders), 2);
      expect(container.read(list).isFetching, isTrue);

      gate.complete([order(1, 20)]);
      await settle();

      expect(container.read(list).isFetching, isFalse);
      expect(container.read(list).dataOrNull?.single.total, 20);
      expect(container.read(detail).value?.single.total, 20);
    });

    test('drops an update deferred past the disposal of its provider', () async {
      final clock = ManualClock();
      final h = harness((_, call) => [order(1, 10 + call)], clock: clock);
      final container = containerFor(h);
      final list = listOrdersProvider.state(const ListOrdersArgs(), staleTime: _stale);

      container.listen(list, (_, _) {});
      await settle();

      clock.advance(const Duration(milliseconds: 100));
      container.listen(
        Provider<AsyncValue<List<Order>>>(
          (ref) => ref.watch(listOrdersProvider(const ListOrdersArgs(), staleTime: _stale)),
        ),
        (_, _) {},
      );
      // The update to `list` is held for a microtask; dispose first.
      container.dispose();

      // No UnmountedRefException from the held write.
      await settle();
      expect(h.transport.countOf(opListOrders), 2);
    });

    testWidgets('mounts a ConsumerWidget mid-build next to a watcher of the same stale query', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);
      final container = containerFor(h);

      Widget screen({required bool detail}) => UncontrolledProviderScope(
        container: container,
        child: Directionality(
          textDirection: TextDirection.ltr,
          child: Column(children: [
            const _ListScreen(),
            if (detail) Builder(builder: (context) => const _DetailScreen()),
          ]),
        ),
      );

      await tester.pumpWidget(screen(detail: false));
      await pumpAll(tester);
      expect(find.text('list 10 false'), findsOneWidget);

      // Past the staleTime, so mounting the detail refetches.
      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(detail: true));

      // No "Providers are not allowed to modify other providers during their
      // initialization", no "setState() or markNeedsBuild() called during
      // build".
      expect(tester.takeException(), isNull);
      expect(h.transport.countOf(opListOrders), 2);

      await tester.pump();
      expect(find.text('list 10 true'), findsOneWidget);

      gate.complete([order(1, 20)]);
      await pumpAll(tester);

      expect(find.text('list 20 false'), findsOneWidget);
      expect(find.text('detail 20'), findsOneWidget);
    });

    testWidgets('applies only the latest of several updates held during one build', (tester) async {
      final clock = ManualClock();
      final h = harness((request, call) {
        // Neither the refetch nor the write lands during this test.
        if (call > 0) return Completer<Object?>().future;
        return [order(1, 10)];
      }, clock: clock);
      final container = containerFor(h);
      final seen = <QueryState<List<Order>>>[];
      container.listen(
        listOrdersProvider.state(const ListOrdersArgs(), staleTime: _stale),
        (_, next) => seen.add(next),
      );
      var mounted = false;

      Widget screen({required bool mount}) => UncontrolledProviderScope(
        container: container,
        child: Directionality(
          textDirection: TextDirection.ltr,
          child: Column(children: [
            const _ListScreen(),
            if (mount) const _DetailScreen(),
            // Built after the detail in the same frame: a second synchronous
            // transition, the optimistic total, while the first is held.
            if (mount)
              Builder(builder: (context) {
                if (!mounted) {
                  mounted = true;
                  unawaited(patchOrder(
                    h.cache,
                    const PatchOrderArgs(1, total: 77),
                    optimistic: OptimisticUpdate<Order>((o) => o.copyWith(total: 77)),
                  ));
                }
                return const SizedBox();
              }),
          ]),
        ),
      );

      await tester.pumpWidget(screen(mount: false));
      await pumpAll(tester);
      seen.clear();

      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(mount: true));
      expect(tester.takeException(), isNull);

      // The isFetching flip and the optimistic total were both held, and
      // one write carried the latest of them.
      expect(seen, hasLength(1));
      expect(seen.single.dataOrNull?.single.total, 77);
      expect(seen.single.isFetching, isTrue);
      expect(seen.single.isOptimistic, isTrue);

      await tester.pump();
      expect(find.text('list 77 true'), findsOneWidget);
    });

    testWidgets('defers an update that a mount outside Riverpod delivers during a widget build', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);
      final container = containerFor(h);
      StreamSubscription<Object?>? raw;
      // R3: never await a cancel inside testWidgets.
      addTearDown(() => unawaited(raw?.cancel()));

      Widget screen({required bool mount}) => UncontrolledProviderScope(
        container: container,
        child: Directionality(
          textDirection: TextDirection.ltr,
          child: Column(children: [
            const _ListScreen(),
            // No provider is initialized here, so only the widget build phase
            // tells the notifier that it is mid-build.
            if (mount)
              Builder(builder: (context) {
                raw ??= listOrders(const ListOrdersArgs())
                    .watch(h.cache, staleTime: _stale)
                    .listen((_) {});
                return const SizedBox();
              }),
          ]),
        ),
      );

      await tester.pumpWidget(screen(mount: false));
      await pumpAll(tester);
      expect(find.text('list 10 false'), findsOneWidget);

      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(mount: true));

      // No "Tried to modify a provider while the widget tree was building".
      expect(tester.takeException(), isNull);
      expect(h.transport.countOf(opListOrders), 2);

      await tester.pump();
      expect(find.text('list 10 true'), findsOneWidget);

      gate.complete([order(1, 20)]);
      await pumpAll(tester);
      expect(find.text('list 20 false'), findsOneWidget);
    });
  });
}
