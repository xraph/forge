import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/control.dart';
import 'package:forge_client/src/devtools/devtools.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:test/test.dart';

import 'harness.dart';

/// The inner transport, counting what actually reached it.
final class _Inner implements Transport {
  final calls = <TransportRequest>[];

  @override
  Future<Object?> execute(TransportRequest request) async {
    calls.add(request);
    return {'ok': true};
  }
}

final class _Real implements ConnectivitySignal {
  _Real(this.online);

  @override
  final Stream<bool> online;
}

const _request = TransportRequest(meta: Ops.orderList, args: TagContext.empty);

void main() {
  group('the network conditions', () {
    test('passes everything through when nothing is set', () async {
      final base = _Inner();
      final controls = ControlledTransport(base);

      expect(await controls.execute(_request), {'ok': true});
      expect(base.calls, hasLength(1));
    });

    // Offline has to fail before the inner transport, not after: a control
    // that let the request out and threw the answer away would still hit the
    // server, which is the one thing offline is meant to prove it does not.
    test('fails offline without letting the request reach the wire', () async {
      final base = _Inner();
      final controls = ControlledTransport(base)..mode = NetworkMode.offline;

      await expectLater(
        controls.execute(_request),
        throwsA(
          isA<SimulatedOffline>().having(
            (e) => e.message,
            'message',
            contains('offline'),
          ),
        ),
      );
      expect(base.calls, isEmpty);
    });

    test('waits the injected latency before the request goes out', () async {
      final slept = <Duration>[];
      final base = _Inner();
      final controls = ControlledTransport(
        base,
        sleep: (d) async => slept.add(d),
      )..latency = const Duration(milliseconds: 750);

      await controls.execute(_request);

      expect(slept, [const Duration(milliseconds: 750)]);
      expect(base.calls, hasLength(1));
    });

    test(
      'adds a delay of its own in slow mode, on top of the injected one',
      () async {
        final slept = <Duration>[];
        final controls =
            ControlledTransport(_Inner(), sleep: (d) async => slept.add(d))
              ..mode = NetworkMode.slow
              ..latency = const Duration(milliseconds: 100);

        await controls.execute(_request);

        expect(slept.first, greaterThan(const Duration(milliseconds: 100)));
      },
    );

    // Armed once and disarmed on use. A toggle that stayed on would make every
    // later request fail, which looks exactly like the bug being reproduced.
    test('fails exactly one request when fail next is armed', () async {
      final base = _Inner();
      final controls = ControlledTransport(base)..failNext();

      await expectLater(controls.execute(_request), throwsA(anything));
      expect(base.calls, isEmpty);

      expect(await controls.execute(_request), {'ok': true});
      expect(base.calls, hasLength(1));
      expect(controls.armed, isFalse);
    });

    test('fails with a status the retry policy can read', () async {
      final controls = ControlledTransport(_Inner())..failNext(503);

      Object? caught;
      try {
        await controls.execute(_request);
      } on Object catch (error) {
        caught = error;
      }

      expect(caught, isA<HttpStatusError>());
      expect(statusOf(caught!), 503);
    });
  });

  group('the revalidation toggles', () {
    test('starts switched off until something is registered', () {
      final revalidation = Revalidation({});

      expect(revalidation.enabled(RevalidationSource.focus), isFalse);
      expect(revalidation.registered(RevalidationSource.focus), isFalse);
    });

    test('starts a source on and stops it when toggled off', () {
      var started = 0;
      var stopped = 0;
      final revalidation = Revalidation({
        RevalidationSource.focus: () {
          started++;
          return () => stopped++;
        },
      });

      // Registering does not start it; the application decides the initial state.
      expect(started, 0);

      revalidation.toggle(RevalidationSource.focus);
      expect(started, 1);
      expect(revalidation.enabled(RevalidationSource.focus), isTrue);

      revalidation.toggle(RevalidationSource.focus);
      expect(stopped, 1);
      expect(revalidation.enabled(RevalidationSource.focus), isFalse);
    });

    test('stops everything it started when disposed', () {
      var stoppedFocus = 0;
      var stoppedPoll = 0;
      final revalidation = Revalidation({
        RevalidationSource.focus: () =>
            () => stoppedFocus++,
        RevalidationSource.poll: () =>
            () => stoppedPoll++,
      });

      revalidation
        ..toggle(RevalidationSource.focus)
        ..toggle(RevalidationSource.poll)
        ..dispose();

      expect(stoppedFocus, 1);
      expect(stoppedPoll, 1);
      expect(revalidation.enabled(RevalidationSource.focus), isFalse);
    });

    test('ignores a toggle for a source the application never registered', () {
      final revalidation = Revalidation({});

      expect(
        () => revalidation.toggle(RevalidationSource.reconnect),
        returnsNormally,
      );
      expect(revalidation.enabled(RevalidationSource.reconnect), isFalse);
    });
  });

  group('simulated connectivity', () {
    test(
      'reports going offline and back online on its connectivity signal',
      () async {
        final controls = ControlledTransport(_Inner());
        final heard = <bool>[];
        final sub = controls.online.listen(heard.add);

        controls
          ..mode = NetworkMode.slow
          ..mode = NetworkMode.offline
          ..mode = NetworkMode.offline
          ..mode = NetworkMode.online;
        await pumpEventQueue();

        // Slow is still online, and repeating a mode is not a transition.
        expect(heard, [false, true]);
        expect(controls.isOnline, isTrue);

        await sub.cancel();
        await controls.dispose();
      },
    );

    test(
      'merges simulated and real connectivity, offline when either is',
      () async {
        final real = StreamController<bool>();
        final controls = ControlledTransport(_Inner());
        final heard = <bool>[];
        final sub = withSimulatedConnectivity(
          _Real(real.stream),
          controls,
        ).online.listen(heard.add);

        real.add(true);
        await pumpEventQueue();
        controls.mode = NetworkMode.offline;
        await pumpEventQueue();
        real.add(false);
        await pumpEventQueue();
        controls.mode = NetworkMode.online;
        await pumpEventQueue();
        real.add(true);
        await pumpEventQueue();

        expect(heard, [true, false, true]);

        await sub.cancel();
        await real.close();
        await controls.dispose();
      },
    );

    test(
      'is a ClientException, so code that handles network errors handles it',
      () {
        expect(SimulatedOffline(null), isA<http.ClientException>());
      },
    );
  });

  group('nothing crosses principals', () {
    test(
      'disarms a failure armed by the first principal before the second sends',
      () async {
        final h = Harness();
        final base = _Inner();
        final controls = ControlledTransport(base)..failNext(503);
        final devtools = attach(
          h.cache,
          clock: CounterClock(),
          controls: controls,
        );

        h.cache.setPrincipal('alice');
        controls.failNext(502);

        expect(controls.armed, isTrue);

        h.cache.setPrincipal('bob');

        // Synchronously, before the cache has emptied or anyone has settled.
        expect(controls.armed, isFalse);
        expect(controls.armedStatus, isNull);
        expect(controls.toJson()['armed'], isFalse);
        expect(controls.toJson()['armedStatus'], isNull);

        await h.settle();

        // Bob's first request is not alice's failure.
        expect(await controls.execute(_request), {'ok': true});
        expect(base.calls, hasLength(1));

        devtools.dispose();
      },
    );

    test('has disarmed by the time the changing notification reaches a later listener', () {
      final h = Harness();
      final controls = ControlledTransport(_Inner());
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        controls: controls,
      );
      h.cache.setPrincipal('alice');
      controls.failNext();

      bool? armedThen;
      final stop = h.cache.watchPrincipalChanging(
        (_) => armedThen = controls.armed,
      );

      h.cache.setPrincipal('bob');
      stop();

      expect(armedThen, isFalse);

      devtools.dispose();
    });

    test(
      'keeps the mode and the latency, which are the developer\'s network',
      () async {
        final h = Harness();
        final slept = <Duration>[];
        final controls =
            ControlledTransport(_Inner(), sleep: (d) async => slept.add(d))
              ..latency = const Duration(milliseconds: 250)
              ..mode = NetworkMode.slow;
        final devtools = attach(
          h.cache,
          clock: CounterClock(),
          controls: controls,
        );

        h.cache.setPrincipal('alice');
        h.cache.setPrincipal('bob');
        await h.settle();

        expect(controls.mode, NetworkMode.slow);
        expect(controls.latency, const Duration(milliseconds: 250));

        await controls.execute(_request);

        expect(slept.single, greaterThan(const Duration(milliseconds: 250)));

        controls.mode = NetworkMode.offline;
        h.cache.setPrincipal('carol');
        await h.settle();

        expect(controls.mode, NetworkMode.offline);
        await expectLater(
          controls.execute(_request),
          throwsA(isA<SimulatedOffline>()),
        );

        devtools.dispose();
      },
    );

    test('keeps the revalidation toggles across a switch', () async {
      final h = Harness();
      var stopped = 0;
      final revalidation = Revalidation({
        RevalidationSource.poll: () =>
            () => stopped++,
      })..toggle(RevalidationSource.poll);
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        revalidation: revalidation,
      );

      h.cache.setPrincipal('alice');
      h.cache.setPrincipal('bob');
      await h.settle();

      expect(revalidation.enabled(RevalidationSource.poll), isTrue);
      expect(stopped, 0);
      expect(devtools.revalidation, same(revalidation));

      revalidation.dispose();
      devtools.dispose();
    });

    test('disarms when the inspector is disposed, since it is no longer told of a switch', () {
      final h = Harness();
      final controls = ControlledTransport(_Inner())..failNext();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        controls: controls,
      );

      expect(devtools.controls, same(controls));

      devtools.dispose();

      expect(controls.armed, isFalse);
    });
  });

  group('placement', () {
    // The decorator goes around the RestTransport and nothing goes around it
    // but an outbox, so it is a plain Transport over a plain Transport.
    test('wraps a RestTransport, and offline never reaches the wire', () async {
      var wire = 0;
      final rest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient((_) async {
          wire++;
          return http.Response(
            '{"ok":true}',
            200,
            headers: {'content-type': 'application/json'},
          );
        }),
        sleep: (_) async {},
      );
      final controls = ControlledTransport(rest);
      final Transport outer = _Outer(controls);

      expect(await outer.execute(_request), {'ok': true});
      expect(wire, 1);

      controls.mode = NetworkMode.offline;

      await expectLater(
        outer.execute(_request),
        throwsA(isA<SimulatedOffline>()),
      );
      expect(wire, 1);
    });

    test('fails the operation once rather than once per retry, because it sits outside the retry loop', () async {
      var wire = 0;
      final rest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient((_) async {
          wire++;
          return http.Response(
            '{"ok":true}',
            200,
            headers: {'content-type': 'application/json'},
          );
        }),
        sleep: (_) async {},
      );
      final controls = ControlledTransport(rest)..failNext(503);

      await expectLater(
        controls.execute(_request),
        throwsA(isA<HttpStatusError>()),
      );
      expect(wire, 0);
      expect(await controls.execute(_request), {'ok': true});
      expect(wire, 1);
    });
  });
}

/// Stands in for an outbox: a transport that holds another transport.
final class _Outer implements Transport {
  _Outer(this.inner);

  final Transport inner;

  @override
  Future<Object?> execute(TransportRequest request) => inner.execute(request);
}
