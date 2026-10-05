import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'support/harness.dart';
import 'support/live_harness.dart';

Widget list({bool live = false}) => ForgeQueryBuilder(
  query: listOrders(const ListOrdersArgs()),
  live: live,
  builder: (context, state) => Text(listText(state)),
);

const updated100 = {
  'type': 'order.updated',
  'payload': {'id': 7, 'total': 100},
};

Future<void> emit(WidgetTester tester, LiveHarness h, Object? message) async {
  h.emit(message);
  await settle(tester);
}

void main() {
  group('ForgeQueryBuilder(live:)', () {
    testWidgets('updates from a frame, with no request behind it', (tester) async {
      final h = liveHarness((_, _) => [order(7, 99)]);

      await tester.pumpWidget(scope(h.harness, list(live: true)));
      await settle(tester);

      expect(find.text('success:99'), findsOneWidget);
      expect(h.transport.calls, hasLength(1));

      await emit(tester, h, updated100);

      // The value moved and not one request was spent: `order.updated` is a
      // patch, so it invalidates nothing.
      expect(find.text('success:100'), findsOneWidget);
      expect(h.transport.calls, hasLength(1));
    });

    testWidgets('commits a frame on the next Flutter frame, not before', (tester) async {
      final h = liveHarness((_, _) => [order(7, 99)]);

      await tester.pumpWidget(scope(h.harness, list(live: true)));
      await settle(tester);

      h.emit(updated100);

      // Delivered, not yet committed: the commit waits for the frame. Draining
      // the microtask queue must not commit it either, which is what tells
      // the frame scheduler from the cache's default microtask one.
      expect(listOrders(const ListOrdersArgs()).getState(h.cache).dataOrNull?.first.total, 99);
      await tester.idle();
      expect(listOrders(const ListOrdersArgs()).getState(h.cache).dataOrNull?.first.total, 99);

      await tester.pump();

      expect(listOrders(const ListOrdersArgs()).getState(h.cache).dataOrNull?.first.total, 100);
    });

    testWidgets('is one subscription for two components on the same live query', (tester) async {
      final h = liveHarness((_, _) => [order(7, 99)]);

      await tester.pumpWidget(scope(h.harness, Column(children: [list(live: true), list(live: true)])));
      await settle(tester);

      // One socket, one subscription on it. Two widgets must not mean two
      // connections.
      expect(h.opened, hasLength(1));
      expect(h.manager.size, 1);
      expect(h.manager.connected('/ws/orders'), isTrue);
    });

    testWidgets('is one channel for two different live queries on the same entity', (tester) async {
      final h = liveHarness(
        (request, _) => request.meta.id == opListOrders.id ? [order(7, 99)] : order(7, 99),
      );

      await tester.pumpWidget(scope(
        h.harness,
        Column(children: [
          list(live: true),
          ForgeQueryBuilder(
            query: getOrder(const OrderArgs(7)),
            live: true,
            builder: (context, state) => Text('detail:${state.dataOrNull?.total ?? '-'}'),
          ),
        ]),
      ));
      await settle(tester);

      // Two queries, two requests, and one socket.
      expect(h.transport.calls, hasLength(2));
      expect(h.opened, hasLength(1));
      expect(h.manager.size, 1);

      // And one frame updates both.
      await emit(tester, h, updated100);

      expect(find.text('success:100'), findsOneWidget);
      expect(find.text('detail:100'), findsOneWidget);
    });

    testWidgets('releases the socket when the last consumer unmounts, and not before', (tester) async {
      final h = liveHarness((_, _) => [order(7, 99)]);

      Widget pair({required bool both}) =>
          scope(h.harness, Column(children: [list(live: true), if (both) list(live: true)]));

      await tester.pumpWidget(pair(both: true));
      await settle(tester);
      expect(h.live(), 1);

      // One of two goes away. The other still wants the channel.
      await tester.pumpWidget(pair(both: false));
      h.closes.flush();
      await settle(tester);

      expect(h.live(), 1);
    });

    testWidgets('releases the socket on the framework teardown path', (tester) async {
      final h = liveHarness((_, _) => [order(7, 99)]);

      await tester.pumpWidget(scope(h.harness, list(live: true)));
      await settle(tester);
      expect(h.live(), 1);

      // Flutter's own teardown: the element is unmounted and the State
      // disposed, which is the only place the adapter holds the release.
      await tester.pumpWidget(const SizedBox());
      h.closes.flush();
      await settle(tester);

      expect(h.live(), 0);
      expect(h.manager.size, 0);
    });

    test(
      'survives a StrictMode double-invoke without tearing the subscription down',
      () {},
      skip: 'React StrictMode double-invokes effects; Flutter mounts a State exactly once.',
    );

    testWidgets('subscribes and unsubscribes as `live` toggles, without refetching', (tester) async {
      final h = liveHarness((_, _) => [order(7, 99)]);
      late StateSetter setOuter;
      var live = false;

      await tester.pumpWidget(scope(
        h.harness,
        StatefulBuilder(builder: (context, setState) {
          setOuter = setState;
          return list(live: live);
        }),
      ));
      await settle(tester);

      expect(h.opened, isEmpty);
      expect(h.transport.calls, hasLength(1));

      // Off to on. A socket appears; the query is untouched.
      setOuter(() => live = true);
      await settle(tester);

      expect(h.live(), 1);
      expect(h.transport.calls, hasLength(1));
      expect(find.text('success:99'), findsOneWidget);

      await emit(tester, h, updated100);
      expect(find.text('success:100'), findsOneWidget);

      // On to off. The socket goes; the value stays; still one request.
      setOuter(() => live = false);
      await settle(tester);
      h.closes.flush();
      await settle(tester);

      expect(h.live(), 0);
      expect(h.transport.calls, hasLength(1));
      expect(find.text('success:100'), findsOneWidget);

      // Deaf because nothing is subscribed, not merely because the socket
      // ignores a push: the manager holds no subscription and no connection,
      // and the one socket it opened was closed.
      expect(h.manager.size, 0);
      expect(h.manager.connected('/ws/orders'), isFalse);
      expect(h.opened, hasLength(1));
      expect(h.opened.single.isClosed, isTrue);

      // And it really is deaf now.
      await emit(tester, h, const {
        'type': 'order.updated',
        'payload': {'id': 7, 'total': 250},
      });
      expect(find.text('success:100'), findsOneWidget);
    });

    testWidgets('opts a query out entirely when `live` is not asked for', (tester) async {
      final h = liveHarness((_, _) => [order(7, 99)]);

      await tester.pumpWidget(scope(h.harness, list()));
      await settle(tester);

      // Nothing subscribed, so no socket and no frame to arrive.
      expect(h.opened, isEmpty);
      expect(h.manager.size, 0);

      await emit(tester, h, updated100);

      expect(find.text('success:99'), findsOneWidget);
      expect(h.transport.calls, hasLength(1));
    });

    testWidgets('re-subscribes for the new principal rather than going deaf', (tester) async {
      var total = 99;
      final h = liveHarness((_, _) => [order(7, total)]);

      await tester.pumpWidget(scope(h.harness, list(live: true)));
      await settle(tester);

      final first = h.opened.first;
      total = 5;

      h.cache.setPrincipal('user-b');
      await settle(tester);

      // The previous principal's socket is gone and a replacement is open,
      // with the widget doing nothing at all.
      expect(first.isClosed, isTrue);
      expect(h.opened, hasLength(2));
      expect(h.live(), 1);

      // The new principal's store was refetched, not the old one's 99 kept.
      expect(find.text('success:5'), findsOneWidget);
      expect(h.transport.calls, hasLength(2));

      await emit(tester, h, const {
        'type': 'order.updated',
        'payload': {'id': 7, 'total': 6},
      });

      expect(find.text('success:6'), findsOneWidget);
    });

    testWidgets('drops frames off a manager that disagrees with the cache about the principal', (
      tester,
    ) async {
      // Miswired on purpose: the manager opens sockets for 'other' while the
      // cache belongs to nobody. The binder reports it once, when it is built,
      // and then fails closed.
      final h = liveHarness((_, _) => [order(7, 99)], managerPrincipal: () => 'other');

      await tester.pumpWidget(scope(h.harness, list(live: true)));
      await settle(tester);

      expect(find.text('success:99'), findsOneWidget);
      expect(h.live(), 1);

      await emit(tester, h, updated100);

      // The frame reached an open socket and was dropped on the floor.
      expect(h.live(), 1);
      expect(find.text('success:99'), findsOneWidget);
      expect(listOrders(const ListOrdersArgs()).getState(h.cache).dataOrNull?.first.total, 99);
      expect(h.transport.calls, hasLength(1));

      h.expectErrors(
        equals([
          allOf(startsWith('principal: '), contains('opens sockets for other but the cache belongs to null')),
        ]),
      );
    });

    testWidgets('reconnects after the server drops the socket, and recovers the value', (
      tester,
    ) async {
      var total = 99;
      final h = liveHarness((_, _) => [order(7, total)]);

      await tester.pumpWidget(scope(h.harness, list(live: true)));
      await settle(tester);
      expect(find.text('success:99'), findsOneWidget);

      // A change the client never heard about, because the socket was down.
      total = 5;
      h.opened.first.dropFromServer();
      await settle(tester);

      // Backoff has not elapsed: the first rung is 500ms less all of its 20%
      // jitter, which the harness pins at its floor.
      await tester.pump(const Duration(milliseconds: 399));
      expect(h.opened, hasLength(1));
      expect(h.live(), 0);

      await tester.pump(const Duration(milliseconds: 1));
      await settle(tester);

      expect(h.opened, hasLength(2));
      expect(h.live(), 1);
      expect(h.manager.connected('/ws/orders'), isTrue);

      // The reconnect waits one second for a `forge.resumed` before it
      // refetches what the gap may have hidden. Nothing says it was filled.
      expect(find.text('success:99'), findsOneWidget);
      await tester.pump(const Duration(seconds: 1));
      await settle(tester);

      expect(find.text('success:5'), findsOneWidget);
      expect(h.transport.calls, hasLength(2));
    });
  });
}
