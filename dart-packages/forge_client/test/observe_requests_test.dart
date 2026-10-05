// Ported from packages/client-core/__tests__/observe-requests.test.ts.
//
// TS hands the observer (report, event); the Dart observer takes one sealed
// RequestEvent that carries the report's id, meta and args itself.
import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';

const list = OperationMeta(
  id: 'orderList',
  method: 'GET',
  path: '/orders',
  provides: ['Order[]'],
  security: ['bearer'],
);
const create = OperationMeta(
  id: 'orderCreate',
  method: 'POST',
  path: '/orders',
  invalidates: ['Order[]'],
);

String kind(RequestEvent event) => switch (event) {
  RequestStarted() => 'start',
  RequestAttempt() => 'attempt',
  RequestRefresh() => 'refresh',
  RequestRefreshed() => 'refreshed',
  RequestRetried() => 'retry',
  RequestSettled() => 'settled',
};

Future<void> noSleep(Duration _) => Future<void>.value();

void main() {
  group('watching what the transport did', () {
    test('reports each attempt and the backoff between them', () async {
      final events = <RequestEvent>[];
      final fake = FakeHttp((_, attempt) {
        if (attempt == 0) throw HttpFailure(503);

        return {'ok': true};
      });
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        sleep: noSleep,
        random: () => 0,
        observer: events.add,
      );

      await rest.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );

      expect(events.map(kind), [
        'start',
        'attempt',
        'retry',
        'attempt',
        'settled',
      ]);

      final retry = events.whereType<RequestRetried>().single;

      expect(retry.status, 503);
      expect(retry.delay, greaterThan(Duration.zero));
      expect(events.map((event) => event.id).toSet(), hasLength(1));
    });

    test(
      'says a retry was never available for a method that is not idempotent',
      () async {
        final events = <RequestEvent>[];
        final fake = FakeHttp((_, _) => throw HttpFailure(500));
        final rest = RestTransport(
          baseUrl: base,
          client: fake.client,
          sleep: noSleep,
          observer: events.add,
        );

        await expectLater(
          rest.execute(
            const TransportRequest(
              meta: create,
              args: TagContext(body: {}),
            ),
          ),
          throwsA(anything),
        );

        expect(
          events.first,
          isA<RequestStarted>().having((event) => event.limit, 'limit', 1),
        );
        expect(events.whereType<RequestAttempt>(), hasLength(1));
        expect(
          events.last,
          isA<RequestSettled>()
              .having((event) => event.ok, 'ok', false)
              .having((event) => event.status, 'status', 500),
        );
      },
    );

    test('reports a retryable status that still ran out of attempts', () async {
      final events = <RequestEvent>[];
      final fake = FakeHttp((_, _) => throw HttpFailure(503));
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        retry: const RetryPolicy(attempts: 2),
        sleep: noSleep,
        observer: events.add,
      );

      await expectLater(
        rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        ),
        throwsA(anything),
      );

      expect(
        events.first,
        isA<RequestStarted>().having((event) => event.limit, 'limit', 2),
      );
      expect(events.whereType<RequestAttempt>(), hasLength(2));
    });

    test(
      'marks the credential refresh, and which requests only waited on it',
      () async {
        final events = <RequestEvent>[];
        var refreshes = 0;
        var token = 't0';
        final fake = FakeHttp((request, _) {
          if (request.headers['Authorization'] == 'Bearer t0') {
            throw HttpFailure(401);
          }

          return {'ok': true};
        });
        final auth = AuthProvider.callbacks(
          credentials: (_) => {'Authorization': 'Bearer $token'},
          refresh: () async {
            refreshes += 1;
            token = 't1';
          },
        );
        final rest = RestTransport(
          baseUrl: base,
          client: fake.client,
          auth: auth,
          sleep: noSleep,
          observer: events.add,
        );

        await Future.wait([
          rest.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
          rest.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
          rest.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
        ]);

        expect(refreshes, 1);

        final refreshed = events.whereType<RequestRefresh>().toList();

        expect(refreshed, hasLength(3));
        expect(refreshed.where((event) => !event.joined), hasLength(1));
      },
    );

    // Dart-only: the contracts' request-events amendment. Every request in a
    // stampede reports its own refresh/refreshed pair; only the first one
    // started the refresh.
    test(
      'pairs refresh with refreshed per request, and marks followers as joined',
      () async {
        final events = <RequestEvent>[];
        var token = 't0';
        final gate = Completer<void>();
        final fake = FakeHttp((request, _) {
          if (request.headers['Authorization'] == 'Bearer t0') {
            throw HttpFailure(401);
          }

          return {'ok': true};
        });
        final auth = AuthProvider.callbacks(
          credentials: (_) => {'Authorization': 'Bearer $token'},
          refresh: () async {
            await gate.future;
            token = 't1';
          },
        );
        final rest = RestTransport(
          baseUrl: base,
          client: fake.client,
          auth: auth,
          sleep: noSleep,
          observer: events.add,
        );

        final running = Future.wait([
          for (var i = 0; i < 3; i++)
            rest.execute(
              const TransportRequest(meta: list, args: TagContext.empty),
            ),
        ]);

        await settle();
        gate.complete();
        await running;

        final ids = events.map((event) => event.id).toSet();
        expect(ids, hasLength(3));

        final joined = <bool>[];

        for (final id in ids) {
          final mine = events.where((event) => event.id == id).toList();
          final refreshing = mine.whereType<RequestRefresh>().single;
          final refreshed = mine.whereType<RequestRefreshed>().single;

          expect(
            mine.indexOf(refreshed),
            greaterThan(mine.indexOf(refreshing)),
          );
          expect(refreshed.ok, isTrue);
          expect(refreshing.meta, same(list));
          expect(refreshing.args, same(TagContext.empty));
          joined.add(refreshing.joined);
        }

        expect(joined.where((value) => !value), hasLength(1));
        expect(joined.where((value) => value), hasLength(2));
      },
    );

    test('closes the refresh so its duration is measurable', () async {
      final events = <RequestEvent>[];
      var token = 't0';
      final fake = FakeHttp((request, _) {
        if (request.headers['Authorization'] == 'Bearer t0') {
          throw HttpFailure(401);
        }

        return {'ok': true};
      });
      final auth = AuthProvider.callbacks(
        credentials: (_) => {'Authorization': 'Bearer $token'},
        refresh: () {
          token = 't1';
        },
      );
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        auth: auth,
        sleep: noSleep,
        observer: events.add,
      );

      await rest.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );

      final kinds = events.map(kind).toList();

      expect(kinds, contains('refresh'));
      expect(kinds, contains('refreshed'));
      expect(kinds.indexOf('refreshed'), greaterThan(kinds.indexOf('refresh')));
    });

    test('closes the refresh even when it fails', () async {
      final events = <RequestEvent>[];
      final fake = FakeHttp((_, _) => throw HttpFailure(401));
      final auth = AuthProvider.callbacks(
        credentials: (_) => {'Authorization': 'Bearer t0'},
        refresh: () => Future<void>.error(StateError('refresh is down')),
      );
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        auth: auth,
        sleep: noSleep,
        observer: events.add,
      );

      await expectLater(
        rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        ),
        throwsA(anything),
      );

      expect(events.map(kind), contains('refreshed'));
    });

    test(
      'costs an unwatched transport nothing but an undefined field',
      () async {
        final fake = FakeHttp((_, _) => {'ok': true});
        final rest = RestTransport(baseUrl: base, client: fake.client);

        expect(
          await rest.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
          {'ok': true},
        );
      },
    );

    // Dart-only: events carry the injected clock reading.
    test('stamps every event with the transport clock', () async {
      final events = <RequestEvent>[];
      final clock = ManualClock(start: 5000);
      final fake = FakeHttp((_, _) => {'ok': true});
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        clock: clock,
        observer: events.add,
      );

      await rest.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );

      expect(events.map((event) => event.at).toSet(), {5000});
    });
  });
}
