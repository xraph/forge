import 'dart:async';

import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'support/harness.dart';

/// Counts how many times it was built.
final class _Counted extends StatelessWidget {
  const _Counted(this.onBuild);

  final void Function() onBuild;

  @override
  Widget build(BuildContext context) {
    onBuild();
    return const SizedBox();
  }
}

void main() {
  tearDown(() => setClient(null));

  group('ForgeListener', () {
    testWidgets('calls the listener on each transition and never rebuilds its child (a contract pin: the child widget is identical, so Flutter skips it either way)', (tester) async {
      var served = 0;
      final h = harness((request, _) => order(idOf(request), ++served));
      final transitions = <String>[];
      var childBuilds = 0;

      await tester.pumpWidget(scope(
        h,
        ForgeListener(
          query: getOrder(const OrderArgs(1)),
          listener: (context, previous, next) =>
              transitions.add('${stateStatusOf(previous)}>${stateStatusOf(next)}'),
          child: _Counted(() => childBuilds++),
        ),
      ));
      await settle(tester);

      expect(transitions.first, 'loading>success');
      final builds = childBuilds;

      h.cache.invalidate(['Order:1']);
      h.scheduler.flush();
      await settle(tester);

      expect(transitions.length, greaterThan(1));
      expect(childBuilds, builds);
    });

    testWidgets('skips transitions listenWhen rejects', (tester) async {
      var served = 0;
      final h = harness((request, _) => order(idOf(request), ++served));
      final totals = <int>[];

      await tester.pumpWidget(scope(
        h,
        ForgeListener(
          query: getOrder(const OrderArgs(1)),
          // Only settled values whose data actually moved.
          listenWhen: (previous, next) =>
              next is QuerySuccess<Order> &&
              !next.isFetching &&
              !identical(previous.dataOrNull, next.dataOrNull),
          listener: (context, previous, next) => totals.add(next.dataOrNull!.total),
          child: const SizedBox(),
        ),
      ));
      await settle(tester);

      h.cache.invalidate(['Order:1']);
      h.scheduler.flush();
      await settle(tester);

      expect(totals, [1, 2]);
    });

    testWidgets('calls the latest listener, not the first', (tester) async {
      var served = 0;
      final h = harness((request, _) => order(idOf(request), ++served));
      final first = <String>[];
      final second = <String>[];

      Widget listen(List<String> into) => scope(
        h,
        ForgeListener(
          query: getOrder(const OrderArgs(1)),
          listener: (context, previous, next) => into.add(stateStatusOf(next)),
          child: const SizedBox(),
        ),
      );

      await tester.pumpWidget(listen(first));
      await settle(tester);
      await tester.pumpWidget(listen(second));

      h.cache.invalidate(['Order:1']);
      h.scheduler.flush();
      await settle(tester);

      // The first listener saw only the first load; every later transition
      // went to the listener the latest widget carried.
      expect(first, ['success']);
      expect(second, isNotEmpty);
    });

    testWidgets('does not report the starting state of a new query as a transition', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));
      final transitions = <String>[];

      Widget watch(int id) => scope(
        h,
        ForgeListener(
          query: getOrder(OrderArgs(id)),
          listener: (context, previous, next) =>
              transitions.add('${stateStatusOf(previous)}>${stateStatusOf(next)}'),
          child: const SizedBox(),
        ),
      );

      await tester.pumpWidget(watch(1));
      await settle(tester);
      expect(transitions, ['loading>success']);

      // Order 1 succeeded, order 2 starts loading: that is not a transition
      // of one query into the other, only order 2's own load is reported.
      await tester.pumpWidget(watch(2));
      await settle(tester);
      expect(transitions, ['loading>success', 'loading>success']);
    });

    testWidgets('stops listening, and releases the query, when removed', (tester) async {
      var served = 0;
      final h = harness((request, _) => order(idOf(request), ++served));
      var calls = 0;

      await tester.pumpWidget(scope(
        h,
        ForgeListener(
          query: getOrder(const OrderArgs(1)),
          listener: (context, previous, next) => calls++,
          child: const SizedBox(),
        ),
      ));
      await settle(tester);
      final before = calls;

      await tester.pumpWidget(scope(h, const SizedBox()));
      h.cache.invalidate(['Order:1']);
      h.scheduler.flush();
      await settle(tester);

      expect(calls, before);
      expect(h.transport.countOf(opGetOrder), 1);
    });

    // Ruling R7. A builder mounted during a build onto a stale query this
    // listener already watches starts a fetch whose isFetching event reaches
    // the listener in the middle of that build. The listener must wait.
    testWidgets('never runs its listener during a build', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);
      final phases = <SchedulerPhase>[];

      Widget screen({required bool detail}) => scope(
        h,
        Column(children: [
          ForgeListener(
            query: listOrders(const ListOrdersArgs()),
            staleTime: const Duration(milliseconds: 50),
            listener: (context, previous, next) =>
                phases.add(SchedulerBinding.instance.schedulerPhase),
            child: const SizedBox(),
          ),
          if (detail)
            Builder(
              builder: (context) => ForgeQueryBuilder(
                query: listOrders(const ListOrdersArgs()),
                staleTime: const Duration(milliseconds: 50),
                builder: (context, state) => Text('detail ${listText(state)}'),
              ),
            ),
        ]),
      );

      await tester.pumpWidget(screen(detail: false));
      await settle(tester);
      final before = phases.length;

      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(detail: true));

      // No "setState() or markNeedsBuild() called during build". The
      // listener's isFetching transition arrived mid-build, was held, and ran
      // in the post-frame callbacks of that same frame.
      expect(tester.takeException(), isNull);
      expect(h.transport.countOf(opListOrders), 2);
      expect(phases.skip(before), [SchedulerPhase.postFrameCallbacks]);

      gate.complete([order(1, 20)]);
      await settle(tester);

      expect(phases.length, greaterThan(before));
      expect(phases, isNot(contains(SchedulerPhase.persistentCallbacks)));
      expect(find.text('detail success:20'), findsOneWidget);
    });

    testWidgets('follows setPrincipal, and its previous state never carries the previous principal\'s data', (tester) async {
      final h = principalHarness();
      final seen = <String>[];

      await tester.pumpWidget(scope(
        h,
        ForgeListener<Order>(
          query: getOrder(const OrderArgs(1)),
          listener: (context, previous, next) =>
              seen.add('${h.cache.principal}: ${orderText(previous)} > ${orderText(next)}'),
          child: const SizedBox(),
        ),
      ));
      await settle(tester);
      expect(seen.last, 'alice: loading:- > success:101');
      seen.clear();

      h.cache.setPrincipal('bob');
      await settle(tester);

      expect(seen, isNotEmpty);
      expect(seen.where(showsAlice), isEmpty, reason: '$seen');
      expect(seen.last, 'bob: loading:- > success:201');
    });
  });
}
