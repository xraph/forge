// Ported from packages/client-core/__tests__/freshness.test.ts.
//
// TS listens on DOM event targets; the Dart core takes FocusSignal and
// ConnectivitySignal, which the Flutter adapter implements. `visibilitychange`
// to visible is `focused` emitting true, to hidden is false; `online` is
// `online` emitting true.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';
import 'support/schema.dart';

const orderList = OperationMeta(
  id: 'orderList',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);
const none = TagContext.empty;

/// A clock that counts how often it is read.
final class CountingClock implements Clock {
  CountingClock(this.value);

  int value;
  int reads = 0;

  @override
  int now() {
    reads++;

    return value;
  }
}

({FakeTransport transport, ManualScheduler scheduler, QueryCache queries}) rig({
  Duration? staleTime,
  Clock? clock,
}) {
  final transport = FakeTransport(
    (_, _) => [
      {'id': 7, 'total': 99},
    ],
  );
  final scheduler = ManualScheduler();

  return (
    transport: transport,
    scheduler: scheduler,
    queries: QueryCache(
      transport: transport,
      entities: schema,
      scheduler: scheduler,
      staleTime: staleTime,
      clock: clock ?? realClock,
    ),
  );
}

final class FakeFocus implements FocusSignal {
  final StreamController<bool> controller = StreamController<bool>.broadcast(
    sync: true,
  );

  @override
  Stream<bool> get focused => controller.stream;
}

final class FakeConnectivity implements ConnectivitySignal {
  final StreamController<bool> controller = StreamController<bool>.broadcast(
    sync: true,
  );

  @override
  Stream<bool> get online => controller.stream;
}

Future<void> advance(ManualClock clock, int ms) async {
  await settle();
  clock.advance(Duration(milliseconds: ms));
  await settle();
}

void main() {
  group('the settle timestamp', () {
    test('stamps a record with the injected clock when it settles', () async {
      final time = ManualClock(start: 1000);
      final (:queries, transport: _, scheduler: _) = rig(clock: time);

      time.advance(const Duration(milliseconds: 500));
      await queries.fetch(orderList, none);
      await settle();

      expect(queries.settledTimeOf(orderList, none), 1500);
    });

    test('reads the clock once per settle and no more', () async {
      final clock = CountingClock(1000);
      final (:queries, transport: _, scheduler: _) = rig(clock: clock);

      await queries.fetch(orderList, none);
      await settle();

      expect(clock.reads, 1);
    });
  });

  group('resolving staleTime', () {
    test(
      'takes the call value over the manifest value over the cache default',
      () async {
        const declared = OperationMeta(
          id: 'orderList',
          method: 'GET',
          path: '/orders',
          entity: 'Order',
          provides: ['Order[]'],
          staleTime: Duration(seconds: 5),
        );
        final (:queries, transport: _, scheduler: _) = rig(
          staleTime: const Duration(minutes: 1),
          clock: ManualClock(start: 1000),
        );

        final a = queries.subscribe(orderList, none, () {});
        expect(
          queries.effectiveStaleTime(orderList, none),
          const Duration(minutes: 1),
        );
        a();

        final b = queries.subscribe(declared, none, () {});
        expect(
          queries.effectiveStaleTime(declared, none),
          const Duration(seconds: 5),
        );
        b();

        final c = queries.subscribe(
          declared,
          none,
          () {},
          staleTime: const Duration(milliseconds: 100),
        );
        expect(
          queries.effectiveStaleTime(declared, none),
          const Duration(milliseconds: 100),
        );
        c();
      },
    );

    test('uses the strictest live subscriber, and relaxes when it leaves', () {
      final (:queries, transport: _, scheduler: _) = rig(
        staleTime: const Duration(minutes: 1),
        clock: ManualClock(start: 1000),
      );

      final loose = queries.subscribe(
        orderList,
        none,
        () {},
        staleTime: const Duration(seconds: 30),
      );
      final strict = queries.subscribe(
        orderList,
        none,
        () {},
        staleTime: const Duration(seconds: 1),
      );

      expect(
        queries.effectiveStaleTime(orderList, none),
        const Duration(seconds: 1),
      );

      strict();
      expect(
        queries.effectiveStaleTime(orderList, none),
        const Duration(seconds: 30),
      );

      loose();
      expect(
        queries.effectiveStaleTime(orderList, none),
        const Duration(minutes: 1),
      );
    });
  });

  group('refetch on mount', () {
    test('does not refetch a settled query at the default staleTime', () async {
      final (:queries, :transport, scheduler: _) = rig();

      await queries.fetch(orderList, none);
      await settle();
      expect(transport.calls, hasLength(1));

      queries.subscribe(orderList, none, () {});
      await settle();

      expect(transport.calls, hasLength(1));
    });

    test(
      'fetch runs the request again once the result has aged past staleTime',
      () async {
        final time = ManualClock(start: 1000);
        final (:queries, :transport, scheduler: _) = rig(
          staleTime: const Duration(seconds: 1),
          clock: time,
        );

        await queries.fetch(orderList, none);
        await queries.fetch(orderList, none);
        expect(transport.calls, hasLength(1));

        time.advance(const Duration(milliseconds: 1001));

        await queries.fetch(orderList, none);
        expect(transport.calls, hasLength(2));
      },
    );

    test(
      'refetches on mount once the result has aged past staleTime',
      () async {
        final time = ManualClock(start: 1000);
        final (:queries, :transport, scheduler: _) = rig(
          staleTime: const Duration(seconds: 1),
          clock: time,
        );

        await queries.fetch(orderList, none);
        await settle();
        expect(transport.calls, hasLength(1));

        time.advance(const Duration(milliseconds: 999));
        queries.subscribe(orderList, none, () {})();
        await settle();
        expect(transport.calls, hasLength(1));

        time.advance(const Duration(milliseconds: 2));
        queries.subscribe(orderList, none, () {});
        await settle();
        expect(transport.calls, hasLength(2));
      },
    );

    test(
      'adds no clock read on mount while every layer resolves to Infinity',
      () async {
        final clock = CountingClock(1000);
        final (:queries, :transport, scheduler: _) = rig(clock: clock);

        await queries.fetch(orderList, none);
        await settle();

        expect(clock.reads, 1);

        queries.subscribe(orderList, none, () {});
        await settle();

        expect(clock.reads, 1);
        expect(transport.calls, hasLength(1));
      },
    );
  });

  group('revalidate', () {
    test(
      'refetches mounted expired queries and reports how many it started',
      () async {
        final time = ManualClock(start: 1000);
        final (:queries, :transport, scheduler: _) = rig(
          staleTime: const Duration(seconds: 1),
          clock: time,
        );

        queries.subscribe(orderList, none, () {});
        await settle();
        expect(transport.calls, hasLength(1));

        expect(queries.revalidate(), 0);

        time.advance(const Duration(milliseconds: 1001));
        expect(queries.revalidate(), 1);

        await settle();
        expect(transport.calls, hasLength(2));
      },
    );

    test('skips a settled, expired record nobody is watching', () async {
      final time = ManualClock(start: 1000);
      final (:queries, :transport, scheduler: _) = rig(
        staleTime: const Duration(seconds: 1),
        clock: time,
      );

      await queries.fetch(orderList, none);
      await settle();
      expect(transport.calls, hasLength(1));

      time.advance(const Duration(milliseconds: 1001));

      expect(queries.revalidate(), 0);
      expect(transport.calls, hasLength(1));
    });

    test('skips a subscribed record that has not settled, even once it reads as expired', () async {
      final time = ManualClock(start: 1000);
      final transport = FakeTransport((_, _) => throw StateError('boom'));
      final queries = QueryCache(
        transport: transport,
        entities: schema,
        staleTime: const Duration(seconds: 1),
        clock: time,
      );

      queries.subscribe(orderList, none, () {});
      await settle();
      expect(transport.calls, hasLength(1));

      time.advance(const Duration(milliseconds: 5000));

      expect(queries.revalidate(), 0);
      expect(transport.calls, hasLength(1));
    });

    test(
      'skips a settled, expired record with a second request already running',
      () async {
        final time = ManualClock(start: 1000);
        final held = Completer<Object?>();
        final transport = FakeTransport(
          (_, call) => call == 0
              ? [
                  {'id': 7, 'total': 99},
                ]
              : held.future,
        );
        final queries = QueryCache(
          transport: transport,
          entities: schema,
          staleTime: const Duration(seconds: 1),
          clock: time,
        );

        queries.subscribe(orderList, none, () {});
        await settle();
        expect(transport.calls, hasLength(1));

        time.advance(const Duration(milliseconds: 1001));

        unawaited(queries.refetch(orderList, none));
        await settle();
        expect(transport.calls, hasLength(2));

        expect(queries.revalidate(), 0);
        expect(transport.calls, hasLength(2));
      },
    );

    // Dart-only: the contract's onlyStale switch.
    test(
      'refetches every watched settled query when onlyStale is false',
      () async {
        final (:queries, :transport, scheduler: _) = rig();

        queries.subscribe(orderList, none, () {});
        await settle();

        expect(queries.revalidate(onlyStale: false), 1);
        await settle();
        expect(transport.calls, hasLength(2));
      },
    );
  });

  group('revalidateOnFocus', () {
    test(
      'revalidates when the document becomes visible, and not while hidden',
      () async {
        final time = ManualClock(start: 1000);
        final (:queries, :transport, scheduler: _) = rig(
          staleTime: const Duration(seconds: 1),
          clock: time,
        );
        final signal = FakeFocus();

        queries.subscribe(orderList, none, () {});
        await settle();
        expect(transport.calls, hasLength(1));

        final stop = revalidateOnFocus(queries, signal);
        expect(signal.controller.hasListener, isTrue);

        time.advance(const Duration(milliseconds: 1001));
        signal.controller.add(true);
        await settle();
        expect(transport.calls, hasLength(2));

        stop();
        await settle();
        expect(signal.controller.hasListener, isFalse);

        stop();
        expect(signal.controller.hasListener, isFalse);
      },
    );

    test('does nothing when the document is hidden', () async {
      final time = ManualClock(start: 1000);
      final (:queries, :transport, scheduler: _) = rig(
        staleTime: const Duration(seconds: 1),
        clock: time,
      );
      final signal = FakeFocus();

      queries.subscribe(orderList, none, () {});
      await settle();

      revalidateOnFocus(queries, signal);
      time.advance(const Duration(milliseconds: 5000));
      signal.controller.add(false);
      await settle();

      expect(transport.calls, hasLength(1));
    });

    // TS: `target: false` installs nothing. A Dart signal is always explicit;
    // the property kept is that the uninstaller is safe to call when nothing
    // ever arrived.
    test('returns a working no-op when there is no target to listen on', () {
      final (:queries, transport: _, scheduler: _) = rig();

      final stop = revalidateOnFocus(queries, FakeFocus());

      expect(stop, returnsNormally);
    });

    // Dart-only: the contract's throttle.
    test('revalidates at most once per throttle window', () async {
      final time = ManualClock(start: 1000);
      final (:queries, :transport, scheduler: _) = rig(
        staleTime: const Duration(milliseconds: 1),
        clock: time,
      );
      final signal = FakeFocus();

      queries.subscribe(orderList, none, () {});
      await settle();

      revalidateOnFocus(queries, signal, throttle: const Duration(seconds: 5));

      time.advance(const Duration(milliseconds: 10));
      signal.controller.add(true);
      await settle();

      time.advance(const Duration(milliseconds: 10));
      signal.controller.add(true);
      await settle();

      expect(transport.calls, hasLength(2));

      time.advance(const Duration(seconds: 5));
      signal.controller.add(true);
      await settle();

      expect(transport.calls, hasLength(3));
    });
  });

  group('revalidateOnReconnect', () {
    test('revalidates when the network comes back', () async {
      final time = ManualClock(start: 1000);
      final (:queries, :transport, scheduler: _) = rig(
        staleTime: const Duration(seconds: 1),
        clock: time,
      );
      final signal = FakeConnectivity();

      queries.subscribe(orderList, none, () {});
      await settle();

      final stop = revalidateOnReconnect(queries, signal);
      expect(signal.controller.hasListener, isTrue);

      time.advance(const Duration(milliseconds: 1001));
      signal.controller.add(true);
      await settle();
      expect(transport.calls, hasLength(2));

      stop();
      await settle();
      expect(signal.controller.hasListener, isFalse);
    });
  });

  group('poll', () {
    test('refetches on the interval and stops when disposed', () async {
      final (:queries, :transport, scheduler: _) = rig();
      final timers = ManualClock();

      await queries.fetch(orderList, none);
      await settle();
      expect(transport.calls, hasLength(1));

      final stop = poll(
        queries,
        orderList,
        none,
        const Duration(seconds: 1),
        sleep: timers.sleep,
      );

      await advance(timers, 1000);
      expect(transport.calls, hasLength(2));

      await advance(timers, 1000);
      expect(transport.calls, hasLength(3));

      stop();

      await advance(timers, 1000);
      expect(transport.calls, hasLength(3));
    });

    test('keeps polling after a request fails', () async {
      var call = 0;
      final transport = FakeTransport((_, _) {
        call++;

        if (call == 1) throw StateError('network down');

        return [
          {'id': 7, 'total': 99},
        ];
      });
      final queries = QueryCache(
        transport: transport,
        entities: schema,
        onError: (_, _) {},
      );
      final timers = ManualClock();

      final stop = poll(
        queries,
        orderList,
        none,
        const Duration(seconds: 1),
        sleep: timers.sleep,
      );

      await advance(timers, 1000);
      await advance(timers, 1000);

      expect(transport.calls.length, greaterThanOrEqualTo(2));

      stop();
    });

    test(
      'pauses while the document is hidden, and resumes once visible',
      () {},
      skip: 'no document in the Dart core; a host pauses by calling stop and polls again on focus',
    );

    test(
      'does not throw when there is no document at all',
      () {},
      skip: 'no document in the Dart core, so there is nothing to be absent',
    );
  });
}
