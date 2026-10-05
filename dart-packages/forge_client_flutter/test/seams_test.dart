import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/scheduler.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';

import 'support/harness.dart';

void main() {
  group('frameCommitScheduler', () {
    testWidgets('runs every commit of a frame together, in order, when the frame begins', (tester) async {
      final scheduler = frameCommitScheduler();
      final log = <int>[];
      final before = SchedulerBinding.instance.transientCallbackCount;

      scheduler.schedule(() => log.add(1));
      scheduler.schedule(() => log.add(2));

      // One frame callback for the whole burst, and nothing has run yet.
      expect(SchedulerBinding.instance.transientCallbackCount, before + 1);
      expect(log, isEmpty);

      await tester.pump();

      expect(log, [1, 2]);
      expect(SchedulerBinding.instance.transientCallbackCount, before);
    });

    testWidgets('falls back to a microtask while frames are disabled', (tester) async {
      await setLifecycle(tester, AppLifecycleState.resumed);
      await setLifecycle(tester, AppLifecycleState.paused);

      final scheduler = frameCommitScheduler();
      final log = <int>[];
      final before = SchedulerBinding.instance.transientCallbackCount;

      scheduler.schedule(() => log.add(1));

      // A paused app produces no frames, so waiting for one would hold the
      // commit until the user came back.
      expect(SchedulerBinding.instance.transientCallbackCount, before);

      await tester.pump();
      expect(log, [1]);

      await setLifecycle(tester, AppLifecycleState.resumed);
    });
  });

  group('AppLifecycleFocusSignal', () {
    testWidgets('reports focus lost on hide and regained on resume', (tester) async {
      await setLifecycle(tester, AppLifecycleState.resumed);

      final seen = <bool>[];
      final subscription = AppLifecycleFocusSignal().focused.listen(seen.add);

      await setLifecycle(tester, AppLifecycleState.paused);
      await setLifecycle(tester, AppLifecycleState.resumed);

      expect(seen, [false, true]);
      unawaited(subscription.cancel());
    });
  });

  group('ConnectivityPlusSignal', () {
    test('reports only going offline and coming back online', () async {
      final changes = StreamController<List<ConnectivityResult>>();
      final seen = <bool>[];
      final subscription =
          ConnectivityPlusSignal(changes: changes.stream).online.listen(seen.add);

      changes
        ..add([.wifi]) // the starting state: online, nothing to report
        ..add([.none]) // offline
        ..add([.none]) // still offline
        ..add([.mobile]) // back online
        ..add([.wifi, .mobile]); // still online, a different radio

      await Future<void>.delayed(Duration.zero);

      expect(seen, [false, true]);
      await subscription.cancel();
      await changes.close();
    });
  });

  group('installFlutterSeams', () {
    testWidgets('refetches stale mounted queries once when the app comes back after hours', (tester) async {
      final clock = ManualClock();
      final h = harness((request, call) => order(idOf(request), 10 + call), clock: clock);

      await setLifecycle(tester, AppLifecycleState.resumed);
      final uninstall = installFlutterSeams(h.cache, connectivity: FakeConnectivitySignal());

      // Stale after a minute, fresh for a day, and one nobody watches any more.
      final stale = getOrder(const OrderArgs(1))
          .watch(h.cache, staleTime: const Duration(minutes: 1))
          .listen((_) {});
      final fresh = getOrder(const OrderArgs(2))
          .watch(h.cache, staleTime: const Duration(days: 1))
          .listen((_) {});
      final gone = getOrder(const OrderArgs(3))
          .watch(h.cache, staleTime: const Duration(minutes: 1))
          .listen((_) {});
      await settle(tester);
      unawaited(gone.cancel());
      expect(h.transport.countOf(opGetOrder), 3);

      await setLifecycle(tester, AppLifecycleState.paused);
      clock.advance(const Duration(hours: 3));
      await setLifecycle(tester, AppLifecycleState.resumed);
      await settle(tester);

      // One request, for the one query that is both mounted and stale.
      expect(h.transport.countOf(opGetOrder), 4);
      expect(idOf(h.transport.calls.last), 1);
      expect(getOrder(const OrderArgs(1)).getState(h.cache).dataOrNull?.total, 13);

      unawaited(stale.cancel());
      unawaited(fresh.cancel());
      uninstall();
    });

    testWidgets('refetches stale mounted queries when the device comes back online', (tester) async {
      final clock = ManualClock();
      final h = harness((request, call) => order(idOf(request), 10 + call), clock: clock);
      final connectivity = FakeConnectivitySignal();
      final uninstall = installFlutterSeams(
        h.cache,
        focus: FakeFocusSignal(),
        connectivity: connectivity,
      );

      final subscription = getOrder(const OrderArgs(1))
          .watch(h.cache, staleTime: const Duration(minutes: 1))
          .listen((_) {});
      await settle(tester);

      connectivity.goOffline();
      clock.advance(const Duration(minutes: 5));
      connectivity.goOnline();
      await settle(tester);

      expect(h.transport.countOf(opGetOrder), 2);

      unawaited(subscription.cancel());
      uninstall();
    });

    test('installs once per cache however many callers ask, and removes on the last release', () {
      final h = harness((_, _) => null);
      final focus = FakeFocusSignal();
      final connectivity = FakeConnectivitySignal();

      final first = installFlutterSeams(h.cache, focus: focus, connectivity: connectivity);
      // The first installation's signals win while it is installed.
      final second = installFlutterSeams(
        h.cache,
        focus: FakeFocusSignal(),
        connectivity: FakeConnectivitySignal(),
      );

      expect(flutterSeamsInstalled(h.cache), isTrue);
      expect(focus.hasListener, isTrue);
      expect(connectivity.hasListener, isTrue);

      first();
      first(); // releasing twice is a no-op, not a second decrement
      expect(flutterSeamsInstalled(h.cache), isTrue);

      second();
      expect(flutterSeamsInstalled(h.cache), isFalse);
      expect(focus.hasListener, isFalse);
      expect(connectivity.hasListener, isFalse);
    });
  });
}
