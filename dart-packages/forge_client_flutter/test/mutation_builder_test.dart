import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'support/harness.dart';

String mutationStatus(MutationState<Object?> state) => switch (state) {
  MutationIdle() => 'idle',
  MutationPending() => 'pending',
  MutationSuccess() => 'success',
  MutationFailure() => 'error',
};

typedef CreateHandle = ForgeMutation<Order, CreateOrderArgs, Order>;

void main() {
  group('ForgeMutationBuilder', () {
    testWidgets('reports idle, pending and success around one call', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          h,
          ForgeMutationBuilder(
            mutation: createOrder,
            builder: (context, m) {
              handle = m;
              return Text(mutationStatus(m.state));
            },
          ),
        ),
      );
      expect(find.text('idle'), findsOneWidget);

      final settled = handle.mutate(const CreateOrderArgs(5));
      await tester.pump();

      expect(find.text('pending'), findsOneWidget);
      expect(handle.isPending, isTrue);

      gate.complete(order(9, 5));
      await settled;
      await tester.pump();

      expect(find.text('success'), findsOneWidget);
      expect(handle.dataOrNull, const Order(id: 9, total: 5));
      expect(handle.isPending, isFalse);
    });

    testWidgets('runs the write against a client supplied per call', (
      tester,
    ) async {
      final hook = harness((_, _) => order(1, 1));
      final perCall = harness((_, _) => order(2, 2));
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          hook,
          ForgeMutationBuilder(
            mutation: createOrder,
            builder: (context, m) {
              handle = m;
              return const SizedBox();
            },
          ),
        ),
      );

      await handle.mutateAsync(const CreateOrderArgs(2), client: perCall.cache);
      await tester.pump();

      expect(perCall.transport.calls, hasLength(1));
      expect(hook.transport.calls, isEmpty);
      expect(handle.dataOrNull, const Order(id: 2, total: 2));
    });

    testWidgets('records an error, and reset returns it to idle', (
      tester,
    ) async {
      final h = harness((_, _) => throw const Boom('conflict'));
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          h,
          ForgeMutationBuilder(
            mutation: createOrder,
            builder: (context, m) {
              handle = m;
              return Text(mutationStatus(m.state));
            },
          ),
        ),
      );

      Object? caught;
      try {
        await handle.mutateAsync(const CreateOrderArgs(0));
      } on Boom catch (error) {
        caught = error;
      }
      await tester.pump();

      expect('$caught', 'conflict');
      expect(find.text('error'), findsOneWidget);
      expect('${handle.errorOrNull}', 'conflict');

      handle.reset();
      await tester.pump();

      expect(find.text('idle'), findsOneWidget);
    });

    testWidgets('resolves rather than rejecting when the mutation fails', (
      tester,
    ) async {
      final h = harness((_, _) => throw const Boom('conflict'));
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          h,
          ForgeMutationBuilder(
            mutation: createOrder,
            builder: (context, m) {
              handle = m;
              return Text(mutationStatus(m.state));
            },
          ),
        ),
      );

      // No try, exactly as the README's onTap writes it.
      final resolved = await handle.mutate(const CreateOrderArgs(0));
      await tester.pump();

      expect(resolved, isNull);
      expect(find.text('error'), findsOneWidget);
      expect('${handle.errorOrNull}', 'conflict');
    });

    testWidgets(
      'raises no unhandled rejection from the documented click handler',
      (tester) async {
        final h = harness((_, _) => throw const Boom('conflict'));

        await tester.pumpWidget(
          scope(
            h,
            ForgeMutationBuilder(
              mutation: createOrder,
              builder: (context, m) => GestureDetector(
                onTap: () => m.mutate(const CreateOrderArgs(0)),
                child: const Text('create'),
              ),
            ),
          ),
        );

        await tester.tap(find.text('create'));
        await settle(tester);

        // An uncaught async error would fail this test on its own; this makes
        // the expectation explicit.
        expect(tester.takeException(), isNull);
      },
    );

    testWidgets(
      'rejects from mutateAsync, for a caller that sequences on the write',
      (tester) async {
        final h = harness((_, _) => throw const Boom('conflict'));
        late CreateHandle handle;

        await tester.pumpWidget(
          scope(
            h,
            ForgeMutationBuilder(
              mutation: createOrder,
              builder: (context, m) {
                handle = m;
                return Text(mutationStatus(m.state));
              },
            ),
          ),
        );

        await expectLater(
          handle.mutateAsync(const CreateOrderArgs(0)),
          throwsA(isA<Boom>()),
        );
        await tester.pump();

        // Same state either way. The only difference is who owns the failure.
        expect(find.text('error'), findsOneWidget);
      },
    );

    testWidgets('updates the queries the mutation invalidated', (tester) async {
      var next = 1;
      final h = harness((request, _) {
        if (request.meta.id == opCreateOrder.id) return order(9, 5);
        return [order(next++, 99)];
      });
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          h,
          Column(
            children: [
              ForgeQueryBuilder(
                query: listOrders(const ListOrdersArgs()),
                builder: (context, state) =>
                    Text('first:${state.dataOrNull?.first.id ?? '-'}'),
              ),
              ForgeMutationBuilder(
                mutation: createOrder,
                builder: (context, m) {
                  handle = m;
                  return const SizedBox();
                },
              ),
            ],
          ),
        ),
      );
      await settle(tester);
      expect(find.text('first:1'), findsOneWidget);

      await handle.mutate(const CreateOrderArgs(5));
      h.scheduler.flush();
      await settle(tester);

      // POST /orders declares `Order[]`, the list provides it, so the list
      // refetched with no invalidation written in this widget.
      expect(find.text('first:2'), findsOneWidget);
      expect(h.transport.countOf(opListOrders), 2);
    });

    testWidgets('applies a placement callback instead of refetching', (
      tester,
    ) async {
      final h = harness(
        (request, _) =>
            request.meta.id == opCreateOrder.id ? order(9, 5) : [order(1, 99)],
      );
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          h,
          Column(
            children: [
              ForgeQueryBuilder(
                query: listOrders(const ListOrdersArgs()),
                builder: (context, state) => Text(
                  'ids:${state.dataOrNull?.map((o) => o.id).join(',') ?? '-'}',
                ),
              ),
              ForgeMutationBuilder(
                mutation: createOrder,
                place: {
                  'Order[]': (created, current, args) => [
                    created,
                    ...?(current as List<Object?>?),
                  ],
                },
                builder: (context, m) {
                  handle = m;
                  return const SizedBox();
                },
              ),
            ],
          ),
        ),
      );
      await settle(tester);
      expect(find.text('ids:1'), findsOneWidget);

      await handle.mutate(const CreateOrderArgs(5));
      h.scheduler.flush();
      await settle(tester);

      expect(find.text('ids:9,1'), findsOneWidget);
      // Placed, so the list was never refetched.
      expect(h.transport.countOf(opListOrders), 1);
    });

    testWidgets(
      'reads the placement callbacks handed to the latest render, not the first',
      (tester) async {
        final h = harness(
          (request, _) => request.meta.id == opCreateOrder.id
              ? order(9, 5)
              : [order(1, 99)],
        );
        late CreateHandle handle;
        late StateSetter setOuter;
        var reversed = false;

        await tester.pumpWidget(
          scope(
            h,
            StatefulBuilder(
              builder: (context, setState) {
                setOuter = setState;
                // Read here, so each build's closure carries its own value.
                final built = reversed;
                return Column(
                  children: [
                    ForgeQueryBuilder(
                      query: listOrders(const ListOrdersArgs()),
                      builder: (context, state) => Text(
                        'ids:${state.dataOrNull?.map((o) => o.id).join(',') ?? '-'}',
                      ),
                    ),
                    ForgeMutationBuilder(
                      mutation: createOrder,
                      // A fresh map and closure every build, as a caller writes it.
                      place: {
                        'Order[]': (created, current, args) {
                          final rows =
                              (current as List<Object?>?) ?? const <Object?>[];
                          return built
                              ? [...rows, created]
                              : [created, ...rows];
                        },
                      },
                      builder: (context, m) {
                        handle = m;
                        return const SizedBox();
                      },
                    ),
                  ],
                );
              },
            ),
          ),
        );
        await settle(tester);

        setOuter(() => reversed = true);
        await tester.pump();

        await handle.mutate(const CreateOrderArgs(5));
        h.scheduler.flush();
        await settle(tester);

        expect(find.text('ids:1,9'), findsOneWidget);
      },
    );

    testWidgets('lets the last of two overlapping calls win', (tester) async {
      final first = Completer<Object?>();
      final second = Completer<Object?>();
      final h = harness((_, call) => call == 0 ? first.future : second.future);
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          h,
          ForgeMutationBuilder(
            mutation: createOrder,
            builder: (context, m) {
              handle = m;
              return Text('total:${m.dataOrNull?.total ?? '-'}');
            },
          ),
        ),
      );

      final a = handle.mutate(const CreateOrderArgs(1));
      final b = handle.mutate(const CreateOrderArgs(2));
      await tester.pump();

      // The second settles first, then the first: the stale answer must not
      // overwrite the fresh one.
      second.complete(order(2, 2));
      await b;
      first.complete(order(1, 1));
      await a;
      await settle(tester);

      expect(find.text('total:2'), findsOneWidget);
    });

    test(
      'still reports status after a StrictMode mount / unmount / mount',
      () {},
      skip: 'React StrictMode double-invokes effects; Flutter mounts a State exactly once.',
    );

    testWidgets(
      'shows an optimistic update at once and rolls it back when the write fails',
      (tester) async {
        final gate = Completer<Object?>();
        final h = harness(
          (request, _) =>
              request.meta.id == opPatchOrder.id ? gate.future : order(1, 10),
        );
        late ForgeMutation<Order, PatchOrderArgs, Order> handle;
        final optimistic = <bool>[];

        await tester.pumpWidget(
          scope(
            h,
            Column(
              children: [
                ForgeQueryBuilder(
                  query: getOrder(const OrderArgs(1)),
                  builder: (context, state) {
                    optimistic.add(state.isOptimistic);
                    return Text(orderText(state));
                  },
                ),
                ForgeMutationBuilder(
                  mutation: patchOrder,
                  optimistic: (args) => OptimisticUpdate<Order>(
                    (o) => o.copyWith(total: args.total),
                  ),
                  builder: (context, m) {
                    handle = m;
                    return Text('mutation:${mutationStatus(m.state)}');
                  },
                ),
              ],
            ),
          ),
        );
        await settle(tester);
        expect(find.text('success:10'), findsOneWidget);

        final settled = handle.mutate(const PatchOrderArgs(1, total: 500));
        await tester.pump();

        expect(find.text('success:500'), findsOneWidget);
        expect(optimistic.last, isTrue);

        gate.completeError(const Boom('conflict'));
        await settled;
        await settle(tester);

        expect(find.text('success:10'), findsOneWidget);
        expect(find.text('mutation:error'), findsOneWidget);
      },
    );

    // Review Focus 1, for writes.
    testWidgets('never runs a mutation result into a widget that is gone', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      late CreateHandle handle;

      await tester.pumpWidget(
        scope(
          h,
          ForgeMutationBuilder(
            mutation: createOrder,
            builder: (context, m) {
              handle = m;
              return const SizedBox();
            },
          ),
        ),
      );

      final settled = handle.mutate(const CreateOrderArgs(5));
      await tester.pumpWidget(scope(h, const SizedBox()));

      gate.complete(order(9, 5));
      // The write still happened and still resolves for its caller.
      expect(await settled, const Order(id: 9, total: 5));
      await settle(tester);
      expect(h.transport.calls, hasLength(1));
      expect(tester.takeException(), isNull);
    });

    // The two builders below share one binding, as two buttons for one write
    // do. React's useMutation is local to its component the same way.
    testWidgets('keeps the status of two builders on one binding independent', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      late CreateHandle left;

      await tester.pumpWidget(
        scope(
          h,
          Column(
            children: [
              ForgeMutationBuilder(
                mutation: createOrder,
                builder: (context, m) {
                  left = m;
                  return Text('left:${mutationStatus(m.state)}');
                },
              ),
              ForgeMutationBuilder(
                mutation: createOrder,
                builder: (context, m) =>
                    Text('right:${mutationStatus(m.state)}'),
              ),
            ],
          ),
        ),
      );

      final settled = left.mutate(const CreateOrderArgs(5));
      await tester.pump();

      expect(find.text('left:pending'), findsOneWidget);
      expect(find.text('right:idle'), findsOneWidget);

      gate.complete(order(9, 5));
      await settled;
      await tester.pump();

      expect(find.text('left:success'), findsOneWidget);
      expect(find.text('right:idle'), findsOneWidget);
    });

    testWidgets(
      'reads the optimistic callback handed to the latest render, not the first',
      (tester) async {
        final gate = Completer<Object?>();
        final h = harness(
          (request, _) =>
              request.meta.id == opPatchOrder.id ? gate.future : order(1, 10),
        );
        late ForgeMutation<Order, PatchOrderArgs, Order> handle;
        late StateSetter setOuter;
        var total = 100;

        await tester.pumpWidget(
          scope(
            h,
            StatefulBuilder(
              builder: (context, setState) {
                setOuter = setState;
                // Read here, so each build's closure carries its own value.
                final built = total;
                return Column(
                  children: [
                    ForgeQueryBuilder(
                      query: getOrder(const OrderArgs(1)),
                      builder: (context, state) => Text(orderText(state)),
                    ),
                    ForgeMutationBuilder(
                      mutation: patchOrder,
                      // A fresh closure every build, as a caller writes it.
                      optimistic: (args) => OptimisticUpdate<Order>(
                        (o) => o.copyWith(total: built),
                      ),
                      builder: (context, m) {
                        handle = m;
                        return const SizedBox();
                      },
                    ),
                  ],
                );
              },
            ),
          ),
        );
        await settle(tester);

        setOuter(() => total = 777);
        await tester.pump();

        final settled = handle.mutate(const PatchOrderArgs(1, total: 1));
        await tester.pump();

        expect(find.text('success:777'), findsOneWidget);

        gate.complete(order(1, 777));
        await settled;
        await settle(tester);
      },
    );

    testWidgets('lets a per-call optimistic patch win over the widget one', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final h = harness(
        (request, _) =>
            request.meta.id == opPatchOrder.id ? gate.future : order(1, 10),
      );
      late ForgeMutation<Order, PatchOrderArgs, Order> handle;

      await tester.pumpWidget(
        scope(
          h,
          Column(
            children: [
              ForgeQueryBuilder(
                query: getOrder(const OrderArgs(1)),
                builder: (context, state) => Text(orderText(state)),
              ),
              ForgeMutationBuilder(
                mutation: patchOrder,
                optimistic: (args) =>
                    OptimisticUpdate<Order>((o) => o.copyWith(total: 1)),
                builder: (context, m) {
                  handle = m;
                  return const SizedBox();
                },
              ),
            ],
          ),
        ),
      );
      await settle(tester);

      final settled = handle.mutate(
        const PatchOrderArgs(1, total: 1),
        optimistic: OptimisticUpdate<Order>((o) => o.copyWith(total: 42)),
      );
      await tester.pump();

      expect(find.text('success:42'), findsOneWidget);

      gate.complete(order(1, 42));
      await settled;
      await settle(tester);
    });

    // R10: parity with MutationBinding.call, which takes headers and a cancel
    // future.
    testWidgets('hands per-call headers and a cancel future to the transport', (
      tester,
    ) async {
      final h = harness((_, _) => order(9, 5));
      late CreateHandle handle;
      final cancel = Completer<void>();

      await tester.pumpWidget(
        scope(
          h,
          ForgeMutationBuilder(
            mutation: createOrder,
            builder: (context, m) {
              handle = m;
              return const SizedBox();
            },
          ),
        ),
      );

      await handle.mutate(
        const CreateOrderArgs(5),
        options: RequestOptions(
          headers: const {'x-trace': 'one'},
          cancel: cancel.future,
        ),
      );
      await handle.mutateAsync(
        const CreateOrderArgs(6),
        options: const RequestOptions(headers: {'x-trace': 'two'}),
      );

      expect(h.transport.calls, hasLength(2));
      expect(h.transport.calls[0].headers, {'x-trace': 'one'});
      expect(h.transport.calls[0].cancel, same(cancel.future));
      expect(h.transport.calls[1].headers, {'x-trace': 'two'});
      expect(h.transport.calls[1].cancel, isNull);
    });

    testWidgets(
      'records a missing client as the failure instead of throwing from mutate',
      (tester) async {
        late CreateHandle handle;

        // No scope and no global client: getClient throws a StateError.
        await tester.pumpWidget(
          ltr(
            ForgeMutationBuilder(
              mutation: createOrder,
              builder: (context, m) {
                handle = m;
                return Text(mutationStatus(m.state));
              },
            ),
          ),
        );

        final resolved = await handle.mutate(const CreateOrderArgs(5));
        await tester.pump();

        expect(resolved, isNull);
        expect(find.text('error'), findsOneWidget);
        expect(handle.errorOrNull, isA<StateError>());
      },
    );

    // A mutate called while the tree is being built, from another widget's
    // build method, moves this widget to pending in the middle of the frame.
    testWidgets(
      'applies a state change that happens during a build after the frame',
      (tester) async {
        final gate = Completer<Object?>();
        final h = harness((_, _) => gate.future);
        late CreateHandle handle;
        late StateSetter setOuter;
        var fire = false;
        var fired = false;

        await tester.pumpWidget(
          scope(
            h,
            StatefulBuilder(
              builder: (context, setState) {
                setOuter = setState;
                return Column(
                  children: [
                    Builder(
                      builder: (context) {
                        if (fire && !fired) {
                          fired = true;
                          handle.mutate(const CreateOrderArgs(5));
                        }
                        return const SizedBox();
                      },
                    ),
                    ForgeMutationBuilder(
                      mutation: createOrder,
                      builder: (context, m) {
                        handle = m;
                        return Text(mutationStatus(m.state));
                      },
                    ),
                  ],
                );
              },
            ),
          ),
        );
        expect(find.text('idle'), findsOneWidget);

        setOuter(() => fire = true);
        await tester.pump();
        expect(tester.takeException(), isNull);

        // The state change was held for the end of that frame and its setState
        // asks for one more.
        await tester.pump();
        expect(find.text('pending'), findsOneWidget);

        gate.complete(order(9, 5));
        await settle(tester);

        expect(find.text('success'), findsOneWidget);
        expect(tester.takeException(), isNull);
      },
    );

    // Two changes in one build, with the rebuild held to the end of the frame.
    // Setting state twice before a frame is one rebuild either way, so this
    // pins the count of rebuilds, not the number of post-frame callbacks.
    testWidgets('rebuilds once for two state changes made during one build', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      late CreateHandle handle;
      late StateSetter setOuter;
      var fire = false;
      var fired = false;
      var builds = 0;

      await tester.pumpWidget(
        scope(
          h,
          StatefulBuilder(
            builder: (context, setState) {
              setOuter = setState;
              return Column(
                children: [
                  Builder(
                    builder: (context) {
                      if (fire && !fired) {
                        fired = true;
                        unawaited(handle.mutate(const CreateOrderArgs(1)));
                        unawaited(handle.mutate(const CreateOrderArgs(2)));
                      }
                      return const SizedBox();
                    },
                  ),
                  ForgeMutationBuilder(
                    mutation: createOrder,
                    builder: (context, m) {
                      builds++;
                      handle = m;
                      return Text(mutationStatus(m.state));
                    },
                  ),
                ],
              );
            },
          ),
        ),
      );
      expect(builds, 1);

      setOuter(() => fire = true);
      await tester.pump();
      // The outer rebuild reaches this builder once.
      expect(builds, 2);
      expect(tester.takeException(), isNull);

      await tester.pump();
      // Held to the end of that frame, then applied once for both changes.
      expect(builds, 3);
      expect(find.text('pending'), findsOneWidget);

      await tester.pump();
      expect(builds, 3);

      gate.complete(order(9, 5));
      await settle(tester);
    });

    // The rebuild held for the end of a frame is dropped when the widget is
    // gone by then: unmounting happens at the end of the same frame, before
    // its post-frame callbacks run.
    testWidgets(
      'drops a held rebuild when the widget leaves the tree in the same frame',
      (tester) async {
        final gate = Completer<Object?>();
        final h = harness((_, _) => gate.future);
        late CreateHandle handle;
        late StateSetter setOuter;
        var show = true;
        var fired = false;

        await tester.pumpWidget(
          scope(
            h,
            StatefulBuilder(
              builder: (context, setState) {
                setOuter = setState;
                return Column(
                  children: [
                    Builder(
                      builder: (context) {
                        if (!show && !fired) {
                          fired = true;
                          // Two changes while the mutation builder is still mounted,
                          // in the build that removes it.
                          unawaited(handle.mutate(const CreateOrderArgs(1)));
                          unawaited(handle.mutate(const CreateOrderArgs(2)));
                        }
                        return const SizedBox();
                      },
                    ),
                    if (show)
                      ForgeMutationBuilder(
                        mutation: createOrder,
                        builder: (context, m) {
                          handle = m;
                          return Text(mutationStatus(m.state));
                        },
                      ),
                  ],
                );
              },
            ),
          ),
        );

        setOuter(() => show = false);
        await tester.pump();
        await tester.pump();

        expect(fired, isTrue);
        expect(find.text('idle'), findsNothing);
        expect(find.text('pending'), findsNothing);
        expect(tester.takeException(), isNull);

        gate.complete(order(9, 5));
        await settle(tester);
        expect(tester.takeException(), isNull);
      },
    );

    testWidgets(
      'records nothing for a call that is reset away, and builds nothing after it',
      (tester) async {
        final gate = Completer<Object?>();
        final h = harness((_, _) => gate.future);
        late CreateHandle handle;

        await tester.pumpWidget(
          scope(
            h,
            ForgeMutationBuilder(
              mutation: createOrder,
              builder: (context, m) {
                handle = m;
                return Text(mutationStatus(m.state));
              },
            ),
          ),
        );

        final settled = handle.mutate(const CreateOrderArgs(5));
        await tester.pump();
        expect(find.text('pending'), findsOneWidget);

        handle.reset();
        await tester.pump();
        expect(find.text('idle'), findsOneWidget);

        gate.complete(order(9, 5));
        expect(await settled, const Order(id: 9, total: 5));
        await settle(tester);

        // Reset superseded the call, so its result is not recorded.
        expect(find.text('idle'), findsOneWidget);
      },
    );
  });

  // Ported from forge_client_riverpod's "mutationProvider identity changes".
  // A mutation's status belongs to the client and principal it ran for.
  // After setPrincipal or a scope client swap, nothing of the previous one's
  // result, error or data can be read.
  group('ForgeMutationBuilder identity changes', () {
    late CreateHandle handle;
    late List<String> built;

    Widget tree(Harness h) => scope(
      h,
      ForgeMutationBuilder(
        mutation: createOrder,
        builder: (context, m) {
          handle = m;
          built.add(mutationStatus(m.state));
          return Text(mutationStatus(m.state));
        },
      ),
    );

    setUp(() => built = []);

    /// What the builder built since the last [built] clear, other than idle.
    Iterable<String> notIdle() => built.where((status) => status != 'idle');

    testWidgets('resets a completed result on setPrincipal', (tester) async {
      final h = harness((_, _) => order(9, 5));
      h.cache.setPrincipal('alice');
      await tester.pumpWidget(tree(h));

      await handle.mutate(const CreateOrderArgs(5));
      await settle(tester);
      expect(handle.dataOrNull, const Order(id: 9, total: 5));
      built.clear();

      h.cache.setPrincipal('bob');
      await settle(tester);

      expect(handle.state, isA<MutationIdle<Order>>());
      expect(handle.dataOrNull, isNull);
      expect(find.text('idle'), findsOneWidget);
      expect(notIdle(), isEmpty);
    });

    testWidgets(
      'resets a completed result on a scope client swap, and writes to the new client',
      (tester) async {
        final a = harness((_, _) => order(9, 5));
        final b = harness((_, _) => order(9, 6));
        await tester.pumpWidget(tree(a));

        await handle.mutate(const CreateOrderArgs(5));
        await settle(tester);
        expect(handle.dataOrNull, const Order(id: 9, total: 5));
        built.clear();

        await tester.pumpWidget(tree(b));
        await settle(tester);
        expect(handle.state, isA<MutationIdle<Order>>());
        expect(notIdle(), isEmpty);

        expect(
          await handle.mutate(const CreateOrderArgs(6)),
          const Order(id: 9, total: 6),
        );
        await settle(tester);
        expect(handle.dataOrNull, const Order(id: 9, total: 6));
        expect(a.transport.calls, hasLength(1));
        expect(b.transport.calls, hasLength(1));
      },
    );

    testWidgets('resets a recorded failure on a scope client swap', (
      tester,
    ) async {
      final a = harness((_, _) => throw const Boom('a failed'));
      final b = harness((_, _) => order(9, 5));
      await tester.pumpWidget(tree(a));

      await handle.mutate(const CreateOrderArgs(5));
      await settle(tester);
      expect(handle.errorOrNull, isA<Boom>());
      built.clear();

      await tester.pumpWidget(tree(b));
      await settle(tester);
      expect(handle.state, isA<MutationIdle<Order>>());
      expect(handle.errorOrNull, isNull);
      expect(notIdle(), isEmpty);
    });

    testWidgets('drops a call in flight across setPrincipal', (tester) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      h.cache.setPrincipal('alice');
      await tester.pumpWidget(tree(h));

      final settled = handle.mutate(const CreateOrderArgs(5));
      await tester.pump();
      expect(handle.isPending, isTrue);
      built.clear();

      h.cache.setPrincipal('bob');
      gate.complete(order(9, 5));
      // The caller still gets its own result.
      expect(await settled, const Order(id: 9, total: 5));
      await settle(tester);

      expect(handle.state, isA<MutationIdle<Order>>());
      expect(notIdle(), isEmpty);
    });

    testWidgets('drops a failure in flight across setPrincipal', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final h = harness((_, _) => gate.future);
      h.cache.setPrincipal('alice');
      await tester.pumpWidget(tree(h));

      final settled = handle.mutateAsync(const CreateOrderArgs(5));
      await tester.pump();
      expect(handle.isPending, isTrue);
      built.clear();

      h.cache.setPrincipal('bob');
      gate.completeError(const Boom('alice conflict'));
      // The caller still hears of its own failure.
      await expectLater(settled, throwsA(isA<Boom>()));
      await settle(tester);

      expect(handle.state, isA<MutationIdle<Order>>());
      expect(notIdle(), isEmpty);
    });

    testWidgets(
      'records a call made straight after setPrincipal for the new principal',
      (tester) async {
        final h = harness((_, _) => order(9, 5));
        h.cache.setPrincipal('alice');
        await tester.pumpWidget(tree(h));
        // Held from before the change, as a callback captures it.
        final held = handle;

        // Before any frame for the new principal.
        h.cache.setPrincipal('bob');
        expect(
          await held.mutate(const CreateOrderArgs(5)),
          const Order(id: 9, total: 5),
        );
        await settle(tester);

        expect(handle.dataOrNull, const Order(id: 9, total: 5));
        expect(find.text('success'), findsOneWidget);
      },
    );

    testWidgets('drops a call in flight across a scope client swap', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final a = harness((_, _) => gate.future);
      final b = harness((_, _) => order(1, 1));
      await tester.pumpWidget(tree(a));

      final settled = handle.mutate(const CreateOrderArgs(5));
      await tester.pump();
      expect(handle.isPending, isTrue);
      built.clear();

      await tester.pumpWidget(tree(b));
      gate.complete(order(9, 5));
      expect(await settled, const Order(id: 9, total: 5));
      await settle(tester);

      expect(handle.state, isA<MutationIdle<Order>>());
      expect(notIdle(), isEmpty);
    });

    testWidgets('drops a failure in flight across a scope client swap', (
      tester,
    ) async {
      final gate = Completer<Object?>();
      final a = harness((_, _) => gate.future);
      final b = harness((_, _) => order(1, 1));
      await tester.pumpWidget(tree(a));

      final settled = handle.mutate(const CreateOrderArgs(5));
      await tester.pump();
      expect(handle.isPending, isTrue);
      built.clear();

      await tester.pumpWidget(tree(b));
      gate.completeError(const Boom('a conflict'));
      expect(await settled, isNull);
      await settle(tester);

      expect(handle.state, isA<MutationIdle<Order>>());
      expect(notIdle(), isEmpty);
    });

    testWidgets(
      'drops a result whose per-call client changed principal while it was in flight',
      (tester) async {
        final gate = Completer<Object?>();
        final h = harness((_, _) => order(1, 1));
        final perCall = harness((_, _) => gate.future);
        perCall.cache.setPrincipal('alice');
        await tester.pumpWidget(tree(h));

        final settled = handle.mutate(
          const CreateOrderArgs(5),
          client: perCall.cache,
        );
        await tester.pump();
        expect(handle.isPending, isTrue);

        perCall.cache.setPrincipal('bob');
        gate.complete(order(9, 5));
        expect(await settled, const Order(id: 9, total: 5));
        await settle(tester);

        // Neither bob's nor still pending: the call no longer belongs here.
        expect(handle.state, isA<MutationIdle<Order>>());
      },
    );
  });
}
