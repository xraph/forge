import 'dart:async';

import 'package:flutter/scheduler.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';

import 'package:forge_client_flutter/testing.dart';

import 'support/harness.dart';

/// The generated operations table, reduced to the one read these tests
/// restore. `hydrate` matches a payload's queries to it by `METHOD path`.
const Map<String, OperationMeta> operations = {'getOrder': opGetOrder};

Widget detail() => ForgeQueryBuilder(
  query: getOrder(const OrderArgs(1)),
  builder: (context, state) => Text(orderText(state)),
);

/// A snapshot of a cache that fetched order 1 with [total], dehydrated for
/// the anonymous principal.
Future<Snapshot> snapshotOf(int total) async {
  final source = harness((_, _) => order(1, total));
  await getOrder(const OrderArgs(1)).fetch(source.cache);
  return dehydrate(source.cache);
}

void main() {
  group('restoring', () {
    testWidgets('shows the placeholder until restore completes, then the child', (tester) async {
      final h = harness((_, _) => order(1, 5));
      final gate = Completer<void>();

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(
          restore: () => gate.future,
          placeholder: const Text('restoring'),
          child: const Text('ready'),
        ),
      ));
      await settle(tester);
      expect(find.text('restoring'), findsOneWidget);
      expect(find.text('ready'), findsNothing);

      gate.complete();
      await settle(tester);
      expect(find.text('ready'), findsOneWidget);
    });

    testWidgets('renders the server data on the first pass and issues no request', (tester) async {
      // In Dart the payload comes from the device, not a server: a snapshot
      // dehydrated from one cache and hydrated into another by the callback.
      final snapshot = await snapshotOf(99);
      final target = harness((_, _) => order(1, 0));

      await tester.pumpWidget(scope(
        target,
        ForgeRestoreBoundary(
          restore: () async {
            hydrate(target.cache, snapshot, operations: operations);
          },
          placeholder: const Text('restoring'),
          child: detail(),
        ),
      ));
      await settle(tester);

      expect(find.text('success:99'), findsOneWidget);
      expect(target.transport.calls, isEmpty);
    });

    testWidgets('hydrates once under StrictMode, whose renders are double-invoked', (tester) async {
      // There is no StrictMode in Flutter. The hazard it probes is the same
      // one a rebuilding parent poses: a new restore closure on every build
      // must not restore again.
      final h = harness((_, _) => order(1, 5));
      var runs = 0;
      late StateSetter bump;

      await tester.pumpWidget(scope(
        h,
        StatefulBuilder(builder: (context, setState) {
          bump = setState;
          return ForgeRestoreBoundary(
            restore: () async {
              runs++;
            },
            child: const Text('ready'),
          );
        }),
      ));
      await settle(tester);

      for (var i = 0; i < 3; i++) {
        bump(() {});
        await tester.pump();
      }

      expect(runs, 1);
    });

    testWidgets('refetches on mount when hydrated stale', (tester) async {
      // Ported rather than skipped: whether a payload is stale is the
      // restore callback's decision (`hydrate(..., stale: true)`), but what
      // the boundary guarantees is that the child mounts after the restore,
      // so it sees the stale entry and refetches.
      final snapshot = await snapshotOf(99);
      final target = harness((_, _) => order(1, 7));

      await tester.pumpWidget(scope(
        target,
        ForgeRestoreBoundary(
          restore: () async {
            hydrate(target.cache, snapshot, operations: operations, stale: true);
          },
          child: detail(),
        ),
      ));
      await settle(tester);

      // The hydrated value is on screen first, with the refetch queued on the
      // cache's invalidation scheduler, which the harness runs by hand.
      expect(find.text('success:99'), findsOneWidget);
      expect(target.transport.calls, isEmpty);

      target.scheduler.flush();
      await settle(tester);

      expect(find.text('success:7'), findsOneWidget);
      expect(target.transport.countOf(opGetOrder), 1);
    });

    testWidgets('renders children unchanged when there is nothing to hydrate', (tester) async {
      final h = harness((_, _) => order(1, 5));

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(restore: () async {}, child: detail()),
      ));
      await settle(tester);

      // Nothing restored, so the child fetches as it would without a boundary.
      expect(find.text('success:5'), findsOneWidget);
      expect(h.transport.countOf(opGetOrder), 1);
    });

    test(
      'adds no element of its own, so the server and client DOM agree',
      () {},
      skip: 'There is no server DOM to agree with in Flutter.',
    );
  });

  group('server rendering', () {
    test(
      'emits the data rather than the loading branch',
      () {},
      skip: 'Flutter has no server rendering.',
    );
  });

  group('when hydrate refuses the payload', () {
    testWidgets('rethrows a principal mismatch, so an error boundary catches it', (tester) async {
      // Flutter has no error boundaries. Without an onError the failure is
      // reported through FlutterError, where the app's handler sees it, and
      // the child renders on a cold cache.
      final foreign = harness((_, _) => order(1, 99));
      foreign.cache.setPrincipal('alice');
      await getOrder(const OrderArgs(1)).fetch(foreign.cache);
      final snapshot = dehydrate(foreign.cache, principal: 'alice');
      final h = harness((_, _) => order(1, 5));

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(
          restore: () async {
            hydrate(h.cache, snapshot, operations: operations);
          },
          child: const Text('ready'),
        ),
      ));
      await settle(tester);

      final error = tester.takeException();
      expect(error, isA<HydrationFailure>());
      expect((error! as HydrationFailure).reason, 'principal');
      expect(find.text('ready'), findsOneWidget);
    });

    testWidgets('reports and renders on when the payload is from a newer client', (tester) async {
      final h = harness((_, _) => order(1, 5));
      final errors = <Object>[];

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(
          restore: () async {
            hydrate(h.cache, const Snapshot({'v': 2, 'queries': []}), operations: operations);
          },
          onError: (error, _) => errors.add(error),
          child: const Text('ready'),
        ),
      ));
      await settle(tester);

      expect(errors, hasLength(1));
      expect(errors.single, isA<HydrationFailure>());
      expect((errors.single as HydrationFailure).reason, 'version');
      expect(tester.takeException(), isNull);
      expect(find.text('ready'), findsOneWidget);
    });
  });

  group('while the tree is being built', () {
    testWidgets('never runs restore during a build', (tester) async {
      // A restore hydrates the cache, and the cache notifies its watchers
      // synchronously. Run inside initState, that would reach them in the
      // middle of the frame that is mounting the boundary.
      final h = harness((_, _) => order(1, 5));
      SchedulerPhase? phase;

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(
          restore: () async {
            phase = SchedulerBinding.instance.schedulerPhase;
          },
          child: const Text('ready'),
        ),
      ));
      await settle(tester);

      expect(phase, isNotNull);
      expect(phase, isNot(SchedulerPhase.persistentCallbacks));
      expect(find.text('ready'), findsOneWidget);
    });

    testWidgets('reports a restore that throws before it returns a future, outside the build', (tester) async {
      // `() => throw ...` has no async body, so the error is thrown by the
      // call itself. It must be treated like any other failed restore.
      final h = harness((_, _) => order(1, 5));
      final reports = <SchedulerPhase>[];
      final errors = <Object>[];

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(
          restore: () => throw const Boom('sync'),
          onError: (error, _) {
            reports.add(SchedulerBinding.instance.schedulerPhase);
            errors.add(error);
          },
          child: const Text('ready'),
        ),
      ));
      await settle(tester);

      expect(errors.map((e) => '$e'), ['sync']);
      expect(reports, isNot(contains(SchedulerPhase.persistentCallbacks)));
      expect(tester.takeException(), isNull);
      expect(find.text('ready'), findsOneWidget);
    });

    testWidgets('mounts the child and reports through FlutterError when onError itself throws', (tester) async {
      final h = harness((_, _) => order(1, 5));

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(
          restore: () async => throw const Boom('restore failed'),
          onError: (error, _) => throw const Boom('handler failed'),
          child: const Text('ready'),
        ),
      ));
      await settle(tester);

      final reported = tester.takeException();
      expect(reported, isA<Boom>());
      expect('$reported', 'handler failed');
      expect(find.text('ready'), findsOneWidget);
    });

    testWidgets('does nothing when it was removed before restore completed', (tester) async {
      final h = harness((_, _) => order(1, 5));
      final gate = Completer<void>();

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(restore: () => gate.future, child: const Text('ready')),
      ));
      await settle(tester);
      await tester.pumpWidget(scope(h, const Text('gone')));

      gate.complete();
      await settle(tester);

      expect(tester.takeException(), isNull);
      expect(find.text('gone'), findsOneWidget);
    });
  });

  group('restoring from the cache\'s own session', () {
    testWidgets('a restore reads the session after cache.idle, since it is null during a principal switch', (tester) async {
      final storage = memoryStorage();
      final foreign = harness((_, _) => order(1, 99));
      foreign.cache.setPrincipal('alice');
      await getOrder(const OrderArgs(1)).fetch(foreign.cache);
      await (await storage.open('alice')).writeSnapshot(dehydrate(foreign.cache, principal: 'alice'));

      final transport = FakeTransport((_, _) => order(1, 0));
      final cache = QueryCache(
        transport: transport,
        entities: schema,
        scheduler: ManualScheduler(),
        storage: storage,
      );
      final h = Harness(cache, transport, ManualScheduler());
      cache.setPrincipal('alice');
      // The switch has begun, and the session opens when it finishes.
      expect(cache.session, isNull);

      await tester.pumpWidget(scope(
        h,
        ForgeRestoreBoundary(
          restore: () async {
            await cache.idle;
            final stored = await cache.session!.readSnapshot();
            hydrate(cache, stored!, principal: 'alice', operations: operations);
          },
          child: detail(),
        ),
      ));
      await settle(tester);

      expect(find.text('success:99'), findsOneWidget);
      expect(transport.calls, isEmpty);
    });
  });

  group('restoring again', () {
    testWidgets('a new key shows the placeholder and runs restore again', (tester) async {
      // The way to restore for another principal: re-key the boundary, for
      // example with `ValueKey(principal)`.
      final h = harness((_, _) => order(1, 5));
      final runs = <String>[];

      Widget boundary(String principal) => scope(
        h,
        ForgeRestoreBoundary(
          key: ValueKey(principal),
          restore: () async {
            runs.add(principal);
          },
          placeholder: Text('restoring $principal'),
          child: Text('ready $principal'),
        ),
      );

      await tester.pumpWidget(boundary('alice'));
      await settle(tester);
      expect(find.text('ready alice'), findsOneWidget);

      await tester.pumpWidget(boundary('bob'));
      expect(find.text('restoring bob'), findsOneWidget);
      expect(find.text('ready alice'), findsNothing);

      await settle(tester);
      expect(find.text('ready bob'), findsOneWidget);
      expect(runs, ['alice', 'bob']);
    });
  });
}
