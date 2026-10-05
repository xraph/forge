import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/testing.dart';
import 'package:forge_client_riverpod/forge_client_riverpod.dart';

import 'support/harness.dart';

final createOrderProvider = mutationProvider(createOrder, name: 'createOrderProvider');
final patchOrderProvider = mutationProvider(patchOrder, name: 'patchOrderProvider');
final listOrdersProvider = queryProvider(listOrders);
final getOrderProvider = queryProvider(getOrder);

String statusOf(MutationState<Order> state) => switch (state) {
  MutationIdle() => 'idle',
  MutationPending() => 'pending',
  MutationSuccess() => 'success',
  MutationFailure() => 'error',
};

/// Shows the create mutation's status.
final class _Status extends ConsumerWidget {
  const _Status();

  @override
  Widget build(BuildContext context, WidgetRef ref) => Text(statusOf(ref.watch(createOrderProvider)));
}

/// The overrides of [containerFor], over [cache], with signals that outlive
/// a swap.
List<Override> overridesFor(
  QueryCache cache,
  FakeFocusSignal focus,
  FakeConnectivitySignal connectivity,
) => [
  forgeClientProvider.overrideWithValue(cache),
  forgeFocusSignalProvider.overrideWithValue(focus),
  forgeConnectivitySignalProvider.overrideWithValue(connectivity),
];

Future<void> pumpAll(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump();
  }
}

void main() {
  // Ported case for case from packages/client-react/__tests__/useMutation.test.tsx.
  group('mutationProvider', () {
    test('reports idle, pending and success around one call', () async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      final container = containerFor(h);
      final states = <MutationState<Order>>[];

      container.listen(createOrderProvider, (_, next) => states.add(next), fireImmediately: true);
      expect(states.single, isA<MutationIdle<Order>>());

      final settled = container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));
      await settle();
      expect(states.last, isA<MutationPending<Order>>());

      gate.complete(order(9, 5));
      expect(await settled, const Order(id: 9, total: 5));
      await settle();

      final last = states.last;
      expect(last, isA<MutationSuccess<Order>>());
      expect((last as MutationSuccess<Order>).data, const Order(id: 9, total: 5));
    });

    test(
      'runs the write against a client supplied per call',
      () {},
      skip: 'A mutation provider writes through forgeClientProvider and takes no per-call '
          'client; override forgeClientProvider, or use ForgeMutationBuilder, for another cache.',
    );

    test('records an error, and reset returns it to idle', () async {
      final h = harness((_, _) => throw const Boom('conflict'));
      final container = containerFor(h);
      container.listen(createOrderProvider, (_, _) {});
      final notifier = container.read(createOrderProvider.notifier);

      await notifier.mutate(const CreateOrderArgs(0));
      await settle();
      expect(container.read(createOrderProvider), isA<MutationFailure<Order>>());

      notifier.reset();
      await settle();
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());
    });

    test('resolves rather than rejecting when the mutation fails', () async {
      final h = harness((_, _) => throw const Boom('conflict'));
      final container = containerFor(h);
      container.listen(createOrderProvider, (_, _) {});

      final resolved = await container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(0));
      await settle();

      expect(resolved, isNull);
      final state = container.read(createOrderProvider);
      expect(state, isA<MutationFailure<Order>>());
      expect('${(state as MutationFailure<Order>).error}', 'conflict');
    });

    test('raises no unhandled rejection from the documented click handler', () async {
      final h = harness((_, _) => throw const Boom('conflict'));
      final container = containerFor(h);
      container.listen(createOrderProvider, (_, _) {});
      final uncaught = <Object>[];

      await runZonedGuarded(() async {
        // The README's onPressed: the future is dropped.
        void onPressed() => container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(0));
        onPressed();
        await settle();
      }, (error, _) => uncaught.add(error));

      expect(uncaught, isEmpty);
      expect(container.read(createOrderProvider), isA<MutationFailure<Order>>());
    });

    test('rejects from mutateAsync, for a caller that sequences on the write', () async {
      final h = harness((_, _) => throw const Boom('conflict'));
      final container = containerFor(h);
      container.listen(createOrderProvider, (_, _) {});

      await expectLater(
        container.read(createOrderProvider.notifier).mutateAsync(const CreateOrderArgs(0)),
        throwsA(isA<Boom>()),
      );
      await settle();

      // Same state either way.
      expect(container.read(createOrderProvider), isA<MutationFailure<Order>>());
    });

    test('updates the queries the mutation invalidated', () async {
      var next = 1;
      final h = harness((request, _) {
        if (request.meta.id == opCreateOrder.id) return order(9, 5);
        return [order(next++, 99)];
      });
      final container = containerFor(h);
      container.listen(listOrdersProvider(const ListOrdersArgs()), (_, _) {});
      container.listen(createOrderProvider, (_, _) {});
      await settle();

      await container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));
      await invalidate(h, const []);

      expect(container.read(listOrdersProvider(const ListOrdersArgs())).value?.first.id, 2);
      expect(h.transport.countOf(opListOrders), 2);
    });

    test('applies a placement callback instead of refetching', () async {
      final h = harness(
        (request, _) => request.meta.id == opCreateOrder.id ? order(9, 5) : [order(1, 99)],
      );
      final container = containerFor(h);
      final list = listOrdersProvider(const ListOrdersArgs());
      container.listen(list, (_, _) {});
      container.listen(createOrderProvider, (_, _) {});
      await settle();
      expect(container.read(list).value?.map((o) => o.id), [1]);

      await container.read(createOrderProvider.notifier).mutate(
        const CreateOrderArgs(5),
        place: {
          'Order[]': (created, current, args) => [created, ...?(current as List<Object?>?)],
        },
      );
      await invalidate(h, const []);

      expect(container.read(list).value?.map((o) => o.id), [9, 1]);
      // Placed, so the list was never refetched.
      expect(h.transport.countOf(opListOrders), 1);
    });

    test(
      'reads the placement callbacks handed to the latest render, not the first',
      () {},
      skip: 'A notifier has no render to read options from: place is passed on each call.',
    );

    test('lets the last of two overlapping calls win', () async {
      final first = Completer<Object?>();
      final second = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? first.future : second.future);
      final container = containerFor(h);
      container.listen(createOrderProvider, (_, _) {});
      final notifier = container.read(createOrderProvider.notifier);

      final a = notifier.mutate(const CreateOrderArgs(1));
      final b = notifier.mutate(const CreateOrderArgs(2));

      second.complete(order(2, 2));
      await b;
      first.complete(order(1, 1));
      await a;
      await settle();

      final state = container.read(createOrderProvider);
      expect((state as MutationSuccess<Order>).data.total, 2);
    });

    test(
      'still reports status after a StrictMode mount / unmount / mount',
      () {},
      skip: 'React StrictMode double-invokes effects; Riverpod builds a provider once per listen.',
    );
  });

  group('mutationProvider beyond the React suite', () {
    test('applies an optimistic update at once and rolls it back when the write fails', () async {
      final gate = Completer<Object?>();
      final h = harness(
        (request, _) => request.meta.id == opPatchOrder.id ? gate.future : order(1, 10),
      );
      final container = containerFor(h);
      final detail = getOrderProvider.state(const OrderArgs(1));
      container.listen(detail, (_, _) {});
      container.listen(patchOrderProvider, (_, _) {});
      await settle();

      final settled = container.read(patchOrderProvider.notifier).mutate(
        const PatchOrderArgs(1, total: 500),
        optimistic: OptimisticUpdate<Order>((o) => o.copyWith(total: 500)),
      );
      await settle();

      expect(container.read(detail).dataOrNull?.total, 500);
      expect(container.read(detail).isOptimistic, isTrue);

      gate.completeError(const Boom('conflict'));
      await settled;
      await settle();

      expect(container.read(detail).dataOrNull?.total, 10);
      expect(container.read(patchOrderProvider), isA<MutationFailure<Order>>());
    });

    test('drops a result that lands after the provider is disposed', () async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      final container = containerFor(h);
      final subscription = container.listen(createOrderProvider, (_, _) {});

      final settled = container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));
      await settle();

      subscription.close();
      await settle();

      gate.complete(order(9, 5));
      // The write still happened and still resolves for its caller, and
      // nothing tried to set state on a disposed notifier.
      expect(await settled, const Order(id: 9, total: 5));
    });

    // Ruling 3: reset supersedes a call in flight, as in useMutation and
    // ForgeMutationBuilder.
    test('records nothing for a call that is reset away', () async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      final container = containerFor(h);
      final states = <MutationState<Order>>[];
      container.listen(createOrderProvider, (_, next) => states.add(next));
      final notifier = container.read(createOrderProvider.notifier);

      final settled = notifier.mutate(const CreateOrderArgs(5));
      await settle();
      notifier.reset();
      states.clear();

      gate.complete(order(9, 5));
      expect(await settled, const Order(id: 9, total: 5));
      await settle();

      expect(states, isEmpty);
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());
    });

    // Ruling R10: parity with MutationBinding.call and ForgeMutation.
    test('hands per-call headers and a cancel future to the transport', () async {
      final h = harness((_, _) => order(9, 5));
      final container = containerFor(h);
      container.listen(createOrderProvider, (_, _) {});
      final notifier = container.read(createOrderProvider.notifier);
      final cancel = Completer<void>();

      await notifier.mutate(
        const CreateOrderArgs(5),
        options: RequestOptions(headers: const {'x-trace': 'one'}, cancel: cancel.future),
      );
      await notifier.mutateAsync(
        const CreateOrderArgs(6),
        options: const RequestOptions(headers: {'x-trace': 'two'}),
      );

      expect(h.transport.calls, hasLength(2));
      expect(h.transport.calls[0].headers, {'x-trace': 'one'});
      expect(h.transport.calls[0].cancel, same(cancel.future));
      expect(h.transport.calls[1].headers, {'x-trace': 'two'});
      expect(h.transport.calls[1].cancel, isNull);
    });

    test('records a missing client as the failure instead of throwing from mutate', () async {
      // No client configured and none overridden: getClient throws.
      setClient(null);
      final container = ProviderContainer(
        overrides: [
          forgeFocusSignalProvider.overrideWithValue(FakeFocusSignal()),
          forgeConnectivitySignalProvider.overrideWithValue(FakeConnectivitySignal()),
        ],
        retry: (_, _) => null,
      );
      addTearDown(container.dispose);
      container.listen(createOrderProvider, (_, _) {}, onError: (_, _) {});

      final resolved = await container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));

      expect(resolved, isNull);
      final state = container.read(createOrderProvider);
      expect((state as MutationFailure<Order>).error, isA<StateError>());
    });

    // Ruling R7. A mutate called from a widget's build method would otherwise
    // modify a provider while the tree is building, which flutter_riverpod
    // asserts against.
    testWidgets('applies a state change that happens during a build after that build', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      final container = containerFor(h);
      late StateSetter setOuter;
      var fire = false;
      var fired = false;

      await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: Directionality(
          textDirection: TextDirection.ltr,
          child: StatefulBuilder(builder: (context, setState) {
            setOuter = setState;
            return Column(children: [
              Builder(builder: (context) {
                if (fire && !fired) {
                  fired = true;
                  unawaited(container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5)));
                }
                return const SizedBox();
              }),
              const _Status(),
            ]);
          }),
        ),
      ));
      expect(find.text('idle'), findsOneWidget);

      setOuter(() => fire = true);
      await tester.pump();
      expect(tester.takeException(), isNull);

      await tester.pump();
      expect(find.text('pending'), findsOneWidget);

      gate.complete(order(9, 5));
      await pumpAll(tester);

      expect(find.text('success'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  });

  // Ruling 2 (privacy). A mutation's status belongs to the client and
  // principal it ran for. After setPrincipal or a new forgeClientProvider,
  // nothing of the previous one's result, error or data can be read.
  group('mutationProvider identity changes', () {
    test('resets a completed result on setPrincipal', () async {
      final h = harness((_, _) => order(9, 5));
      h.cache.setPrincipal('alice');
      final container = containerFor(h);
      final states = <MutationState<Order>>[];
      container.listen(createOrderProvider, (_, next) => states.add(next));

      await container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));
      await settle();
      expect(container.read(createOrderProvider), isA<MutationSuccess<Order>>());
      states.clear();

      h.cache.setPrincipal('bob');
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());

      await settle();
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());
      expect(states.where((state) => state is! MutationIdle<Order>), isEmpty);
    });

    test('resets a recorded failure on a client swap, and writes to the new client', () async {
      final a = harness((_, _) => throw const Boom('a failed'));
      final b = harness((_, _) => order(9, 5));
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();
      final container = ProviderContainer(
        overrides: overridesFor(a.cache, focus, connectivity),
        retry: (_, _) => null,
      );
      addTearDown(container.dispose);
      final states = <MutationState<Order>>[];
      container.listen(createOrderProvider, (_, next) => states.add(next));

      await container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));
      await settle();
      expect(container.read(createOrderProvider), isA<MutationFailure<Order>>());
      states.clear();

      container.updateOverrides(overridesFor(b.cache, focus, connectivity));
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());
      await settle();
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());
      expect(states.where((state) => state is! MutationIdle<Order>), isEmpty);

      expect(await container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(6)), isNotNull);
      expect(a.transport.calls, hasLength(1));
      expect(b.transport.calls, hasLength(1));
    });

    test('drops a call in flight across setPrincipal', () async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      h.cache.setPrincipal('alice');
      final container = containerFor(h);
      final states = <MutationState<Order>>[];
      container.listen(createOrderProvider, (_, next) => states.add(next));

      final settled = container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));
      await settle();
      expect(container.read(createOrderProvider), isA<MutationPending<Order>>());
      states.clear();

      h.cache.setPrincipal('bob');
      gate.complete(order(9, 5));
      // Only microtasks have run, so Riverpod's scheduled rebuild has not:
      // no listener saw alice's result in that gap either.
      await settled;
      expect(states.where((state) => state is! MutationIdle<Order>), isEmpty);
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());

      await settle();
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());
      expect(states.where((state) => state is! MutationIdle<Order>), isEmpty);
    });

    test('records a call made straight after setPrincipal for the new principal', () async {
      final h = harness((_, _) => order(9, 5));
      h.cache.setPrincipal('alice');
      final container = containerFor(h);
      container.listen(createOrderProvider, (_, _) {});
      // Held from before the change, as a callback captures it; reading
      // .notifier again would bring the provider up to date by itself.
      final notifier = container.read(createOrderProvider.notifier);
      await settle();

      // Before Riverpod's scheduled rebuild for the new principal.
      h.cache.setPrincipal('bob');
      final resolved = await notifier.mutate(const CreateOrderArgs(5));
      await settle();

      expect(resolved, const Order(id: 9, total: 5));
      final state = container.read(createOrderProvider);
      expect((state as MutationSuccess<Order>).data, const Order(id: 9, total: 5));
    });

    test('drops a call in flight across a client swap', () async {
      final gate = Completer<Object?>();
      final a = harness((_, _) => gate.future);
      final b = harness((_, _) => order(1, 1));
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();
      final container = ProviderContainer(
        overrides: overridesFor(a.cache, focus, connectivity),
        retry: (_, _) => null,
      );
      addTearDown(container.dispose);
      final states = <MutationState<Order>>[];
      container.listen(createOrderProvider, (_, next) => states.add(next));

      final settled = container.read(createOrderProvider.notifier).mutate(const CreateOrderArgs(5));
      await settle();
      expect(container.read(createOrderProvider), isA<MutationPending<Order>>());
      states.clear();

      container.updateOverrides(overridesFor(b.cache, focus, connectivity));
      gate.complete(order(9, 5));
      expect(await settled, const Order(id: 9, total: 5));
      expect(states.where((state) => state is! MutationIdle<Order>), isEmpty);
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());

      await settle();
      expect(container.read(createOrderProvider), isA<MutationIdle<Order>>());
      expect(states.where((state) => state is! MutationIdle<Order>), isEmpty);
    });
  });
}
