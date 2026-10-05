import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';

import 'support/harness.dart';

const _stale = Duration(milliseconds: 50);

/// A read of an entity no sync source owns, so its sync status stays Synced.
const _opGetCustomer = OperationMeta(
  id: 'op_get_customer',
  method: 'GET',
  path: '/customers/{id}',
  entity: 'Customer',
  rootType: 'Customer',
  provides: ['Customer:{id}'],
);

final _getCustomer = query<Object?, OrderArgs>(_opGetCustomer, (client) => client);

/// A sync source the test drives: it owns Order and says whatever status it
/// is told to. Modelled on forge_client's own test source.
final class _FakeSource implements SyncSource {
  final _status = StreamController<SyncStatus>.broadcast();

  @override
  Set<String> get entities => const {'Order'};

  @override
  Future<void> start(SyncContext context) async {}

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async => Queued(mutation.id);

  @override
  Stream<SyncStatus> status(String entity) => _status.stream;

  void emit(SyncStatus status) => _status.add(status);

  @override
  Future<void> stop() async {}
}

Widget _staleList(String label) => ForgeQueryBuilder(
  query: listOrders(const ListOrdersArgs()),
  staleTime: _stale,
  builder: (context, state) => Text('$label ${listText(state)}'),
);

void main() {
  tearDown(() => setClient(null));

  group('ForgeQueriesState', () {
    // Pure value tests, so the precedence is pinned without a widget tree.
    final h = harness((request, _) => order(idOf(request), 5));
    QueryState<Object?> idle() => getOrder(const OrderArgs(1)).getState(h.cache, enabled: false);

    test('is success for an empty list', () {
      final state = ForgeQueriesState(const []);
      expect(state.status, ForgeCombinedStatus.success);
      expect(state.data, isEmpty);
      expect(state.error, isNull);
      expect(state.isFetching, isFalse);
    });

    test('reports the first failure, not the last', () {
      final state = ForgeQueriesState([
        const QuerySuccess<Object?>(1),
        const QueryFailure<Object?>(Boom('first')),
        const QueryFailure<Object?>(Boom('second')),
      ]);
      expect(state.status, ForgeCombinedStatus.failure);
      expect(state.error.toString(), 'first');
    });

    test('is optimistic when any query is, and only then', () {
      expect(
        ForgeQueriesState([
          const QuerySuccess<Object?>(1),
          const QuerySuccess<Object?>(2, isOptimistic: true),
        ]).isOptimistic,
        isTrue,
      );
      expect(
        ForgeQueriesState([
          const QuerySuccess<Object?>(1),
          const QuerySuccess<Object?>(2),
        ]).isOptimistic,
        isFalse,
      );
    });

    test('puts idle ahead of success, with no data', () {
      final state = ForgeQueriesState([idle()]);
      expect(state.status, ForgeCombinedStatus.idle);
      expect(state.data, isNull);
    });
  });

  group('ForgeQueriesBuilder', () {
    testWidgets('reports loading until every query has data, then success with all of them', (tester) async {
      final gate = Completer<Object?>();
      final h = harness(
        (request, _) => request.meta.id == opListOrders.id ? gate.future : order(idOf(request), 5),
      );
      ForgeQueriesState? last;

      await tester.pumpWidget(scope(
        h,
        ForgeQueriesBuilder(
          queries: [getOrder(const OrderArgs(1)), listOrders(const ListOrdersArgs())],
          builder: (context, state) {
            last = state;
            return Text('${state.status.name}:${state.data?.length ?? '-'}');
          },
        ),
      ));
      await settle(tester);

      // The order is in, the list is not.
      expect(find.text('loading:-'), findsOneWidget);

      gate.complete([order(2, 6)]);
      await settle(tester);

      expect(find.text('success:2'), findsOneWidget);
      expect(last!.dataAt<Order>(0).total, 5);
      expect(last!.dataAt<List<Order>>(1).first.total, 6);
    });

    testWidgets('reports failure when any query fails, with the first error', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((request, _) {
        // The order is held loading, so failure must beat loading.
        if (request.meta.id == opGetOrder.id) return gate.future;
        throw const Boom('list down');
      });

      await tester.pumpWidget(scope(
        h,
        ForgeQueriesBuilder(
          queries: [getOrder(const OrderArgs(1)), listOrders(const ListOrdersArgs())],
          builder: (context, state) => Text('${state.status.name}:${state.error ?? '-'}'),
        ),
      ));
      await settle(tester);

      expect(find.text('failure:list down'), findsOneWidget);

      gate.complete(order(1, 5));
      await settle(tester);
    });

    testWidgets('reports idle and fetches nothing while disabled', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));

      Widget both({required bool enabled}) => scope(
        h,
        ForgeQueriesBuilder(
          enabled: enabled,
          queries: [getOrder(const OrderArgs(1)), getOrder(const OrderArgs(2))],
          builder: (context, state) => Text(state.status.name),
        ),
      );

      await tester.pumpWidget(both(enabled: false));
      await settle(tester);
      expect(find.text('idle'), findsOneWidget);
      expect(h.transport.calls, isEmpty);

      await tester.pumpWidget(both(enabled: true));
      await settle(tester);
      expect(find.text('success'), findsOneWidget);
      expect(h.transport.countOf(opGetOrder), 2);
    });

    testWidgets('folds isFetching across its queries', (tester) async {
      // The fake transport answers before the next frame, so the refetch of
      // order 2 is held on a gate to let a build see it in flight.
      final gate = Completer<Object?>();
      final h = harness((request, call) => call < 2 ? order(idOf(request), 5) : gate.future);
      final seen = <ForgeQueriesState>[];

      await tester.pumpWidget(scope(
        h,
        ForgeQueriesBuilder(
          queries: [getOrder(const OrderArgs(1)), getOrder(const OrderArgs(2))],
          builder: (context, state) {
            seen.add(state);
            return const SizedBox();
          },
        ),
      ));
      await settle(tester);
      expect(seen.last.isFetching, isFalse);

      h.cache.invalidate(['Order:2']);
      h.scheduler.flush();
      await settle(tester);

      // One query refetching makes the combination fetching, while it stays a
      // success because both still have data.
      expect(seen.last.isFetching, isTrue);
      expect(seen.last.status, ForgeCombinedStatus.success);
      expect(seen.last.states[0].isFetching, isFalse);
      expect(seen.last.states[1].isFetching, isTrue);

      gate.complete(order(2, 9));
      await settle(tester);

      expect(seen.last.isFetching, isFalse);
      expect(seen.last.dataAt<Order>(1).total, 9);
    });

    testWidgets('resubscribes only the query whose arguments changed', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));

      Widget pair(int second) => scope(
        h,
        ForgeQueriesBuilder(
          queries: [getOrder(const OrderArgs(1)), getOrder(OrderArgs(second))],
          builder: (context, state) => Text(state.status.name),
        ),
      );

      await tester.pumpWidget(pair(2));
      await settle(tester);
      await tester.pumpWidget(pair(3));
      await settle(tester);

      expect(h.transport.calls.map(idOf), [1, 2, 3]);
    });

    testWidgets('keeps the subscription of a query whose arguments did not change', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));

      // staleTime zero makes every new mount refetch, so a subscription that
      // was torn down and rebuilt would show up as a second request for 1.
      Widget pair(int second) => scope(
        h,
        ForgeQueriesBuilder(
          staleTime: Duration.zero,
          queries: [getOrder(const OrderArgs(1)), getOrder(OrderArgs(second))],
          builder: (context, state) => Text(state.status.name),
        ),
      );

      await tester.pumpWidget(pair(2));
      await settle(tester);
      await tester.pumpWidget(pair(3));
      await settle(tester);

      expect(h.transport.calls.map(idOf), [1, 2, 3]);
    });

    testWidgets('keeps each subscription when the queries are reordered', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));

      // Matching is by position, so [1, 2] -> [2, 1] rebinds both slots to
      // each other's key. The cache already holds both fresh results, so at
      // the default staleTime that is two resubscribes and no request.
      Widget pair(List<int> ids) => scope(
        h,
        ForgeQueriesBuilder(
          queries: [for (final id in ids) getOrder(OrderArgs(id))],
          builder: (context, state) => Text(state.status.name),
        ),
      );

      await tester.pumpWidget(pair([1, 2]));
      await settle(tester);
      await tester.pumpWidget(pair([2, 1]));
      await settle(tester);

      expect(find.text('success'), findsOneWidget);
      expect(h.transport.calls.map(idOf), [1, 2]);
    });

    testWidgets('folds syncStatus across its queries, summing pending counts', (tester) async {
      final source = _FakeSource();
      final transport = FakeTransport((request, _) => order(idOf(request), 5));
      final scheduler = ManualScheduler();
      final cache = QueryCache(
        transport: transport,
        entities: {...schema, 'Customer': const EntityMeta(idField: 'id')},
        scheduler: scheduler,
        syncSources: [source],
      );
      final seen = <ForgeQueriesState>[];

      cache.setPrincipal('alice');
      await tester.pump();
      await tester.pumpWidget(ForgeScope(
        client: cache,
        focus: FakeFocusSignal(),
        connectivity: FakeConnectivitySignal(),
        child: ltr(ForgeQueriesBuilder(
          queries: [
            getOrder(const OrderArgs(1)),
            getOrder(const OrderArgs(2)),
            _getCustomer(const OrderArgs(3)),
          ],
          builder: (context, state) {
            seen.add(state);
            return const SizedBox();
          },
        )),
      ));
      await settle(tester);
      expect(seen.last.syncStatus, const Synced());

      // The status is per entity, so each Order read carries Pending(2) and
      // the Customer read stays Synced. Only a sum of both Order reads is 4.
      source.emit(const Pending(2));
      await settle(tester);
      expect(seen.last.states[0].syncStatus, const Pending(2));
      expect(seen.last.states[2].syncStatus, const Synced());
      expect(seen.last.syncStatus, const Pending(4));

      source.emit(const Offline());
      await settle(tester);
      expect(seen.last.syncStatus, const Offline());

      source.emit(const SyncFailed('rejected'));
      await settle(tester);
      expect(seen.last.syncStatus, const SyncFailed('rejected'));

      unawaited(cache.dispose());
    });

    testWidgets('follows the list as queries are added and removed', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));

      Widget some(List<int> ids) => scope(
        h,
        ForgeQueriesBuilder(
          queries: [for (final id in ids) getOrder(OrderArgs(id))],
          builder: (context, state) => Text('${state.states.length}:${state.status.name}'),
        ),
      );

      await tester.pumpWidget(some([1]));
      await settle(tester);
      expect(find.text('1:success'), findsOneWidget);

      await tester.pumpWidget(some([1, 2]));
      await settle(tester);
      expect(find.text('2:success'), findsOneWidget);

      await tester.pumpWidget(some([1]));
      await settle(tester);
      expect(find.text('1:success'), findsOneWidget);

      // Order 2 was released with the entry that watched it.
      h.cache.invalidate(['Order:1', 'Order:2']);
      h.scheduler.flush();
      await settle(tester);
      expect(h.transport.calls.map(idOf), [1, 2, 1]);
    });

    testWidgets('releases every query when it goes away', (tester) async {
      final h = harness((request, _) => order(idOf(request), 5));

      await tester.pumpWidget(scope(
        h,
        ForgeQueriesBuilder(
          queries: [getOrder(const OrderArgs(1)), getOrder(const OrderArgs(2))],
          builder: (context, state) => Text(state.status.name),
        ),
      ));
      await settle(tester);

      await tester.pumpWidget(scope(h, const SizedBox()));
      h.cache.invalidate(['Order:1', 'Order:2']);
      h.scheduler.flush();
      await settle(tester);

      expect(h.transport.countOf(opGetOrder), 2);
    });

    // Ruling R7. A builder mounted during a build onto a stale query that this
    // builder already watches starts a fetch whose isFetching event reaches
    // this builder in the middle of that build.
    testWidgets('mounts mid-build onto a stale query another builder watches', (tester) async {
      final clock = ManualClock();
      final gate = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? [order(1, 10)] : gate.future, clock: clock);

      Widget screen({required bool detail}) => scope(
        h,
        Column(children: [
          ForgeQueriesBuilder(
            queries: [listOrders(const ListOrdersArgs())],
            staleTime: _stale,
            builder: (context, state) => Text('both ${state.status.name}:${state.isFetching}'),
          ),
          if (detail) Builder(builder: (context) => _staleList('detail')),
        ]),
      );

      await tester.pumpWidget(screen(detail: false));
      await settle(tester);
      expect(find.text('both success:false'), findsOneWidget);

      clock.advance(const Duration(milliseconds: 100));
      await tester.pumpWidget(screen(detail: true));

      // No "setState() or markNeedsBuild() called during build".
      expect(tester.takeException(), isNull);
      expect(h.transport.countOf(opListOrders), 2);
      expect(tester.binding.hasScheduledFrame, isTrue);

      await settle(tester);
      expect(find.text('both success:true'), findsOneWidget);

      gate.complete([order(1, 20)]);
      await settle(tester);
      expect(find.text('both success:false'), findsOneWidget);
    });
  });
}
