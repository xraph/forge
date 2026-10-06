import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/devtools.dart';
import 'package:forge_client/src/devtools/requests.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:test/test.dart';

import 'harness.dart';

const _list = OperationMeta(
  id: 'op_list',
  method: 'GET',
  path: '/orders',
  provides: ['Order[]'],
);
const _create = OperationMeta(
  id: 'op_create',
  method: 'POST',
  path: '/orders',
  invalidates: ['Order[]'],
);

http.Response _json(Object? body, [int status = 200]) => http.Response(
  jsonEncode(body),
  status,
  headers: {'content-type': 'application/json'},
);

({RequestLog log, RestTransport rest}) _wired(
  FutureOr<http.Response> Function(http.Request request, int call) handler, {
  int capacity = 50,
  AuthProvider? auth,
}) {
  var calls = 0;
  final log = RequestLog(capacity: capacity, clock: CounterClock());
  final rest = RestTransport(
    baseUrl: Uri.parse('http://forge.test'),
    client: MockClient((request) async => handler(request, calls++)),
    auth: auth,
    sleep: (_) async {},
    observer: log.observer,
  );
  return (log: log, rest: rest);
}

final class _TokenAuth implements AuthProvider {
  _TokenAuth(this.token);

  String token;

  @override
  Map<String, String> credentials(OperationMeta meta) => {
    'Authorization': 'Bearer $token',
  };

  @override
  void refresh() => token = 't1';
}

void main() {
  group('the request log', () {
    test(
      'records one settled request with its operation and its arguments',
      () async {
        final (:log, :rest) = _wired((_, _) => _json({'ok': true}));

        await rest.execute(
          const TransportRequest(
            meta: _list,
            args: TagContext(query: {'page': 1}),
          ),
        );

        final entry = log.entries().single;

        expect(entry.operation, 'GET /orders');
        expect(entry.args, contains('page'));
        expect(entry.outcome, RequestOutcome.ok);
        expect(entry.attempts, 1);
      },
    );

    test('keeps the ledger of attempts and the backoff between them', () async {
      final (:log, :rest) = _wired(
        (_, call) => call == 0 ? _json({}, 503) : _json({'ok': true}),
      );

      await rest.execute(
        const TransportRequest(meta: _list, args: TagContext.empty),
      );

      final entry = log.entries().single;

      expect(entry.attempts, 2);
      expect(entry.retries, hasLength(1));
      expect(entry.retries.first.status, 503);
      expect(entry.retries.first.delayMs, greaterThan(0));
      expect(entry.outcome, RequestOutcome.ok);
    });

    // The row the whole view exists for. A browser network tab shows this and
    // a retried GET as the same thing: one request, one failure.
    test('records that a failed POST was never eligible for a retry', () async {
      final (:log, :rest) = _wired((_, _) => _json({}, 500));

      await expectLater(
        rest.execute(
          const TransportRequest(
            meta: _create,
            args: TagContext(body: {}),
          ),
        ),
        throwsA(isA<HttpStatusError>()),
      );

      final entry = log.entries().single;

      expect(entry.outcome, RequestOutcome.failed);
      expect(entry.status, 500);
      expect(entry.attempts, 1);
      expect(entry.limit, 1);
    });

    test(
      'records a 4xx that the policy declined to retry despite the budget',
      () async {
        final (:log, :rest) = _wired((_, _) => _json({}, 403));

        await expectLater(
          rest.execute(
            const TransportRequest(meta: _list, args: TagContext.empty),
          ),
          throwsA(anything),
        );

        final entry = log.entries().single;

        expect(entry.attempts, 1);
        // The budget was there and went unused: the status was the reason.
        expect(entry.limit, greaterThan(1));
        expect(entry.status, 403);
      },
    );

    test('shows a request while it is still in flight', () async {
      final answer = Completer<http.Response>();
      final (:log, :rest) = _wired((_, _) => answer.future);

      final pending = rest.execute(
        const TransportRequest(meta: _list, args: TagContext.empty),
      );
      await pumpEventQueue();

      expect(log.entries().single.outcome, RequestOutcome.pending);
      expect(log.entries().single.duration, isNull);

      answer.complete(_json({'ok': true}));
      await pending;

      expect(log.entries().single.outcome, RequestOutcome.ok);
      expect(log.entries().single.duration, greaterThanOrEqualTo(0));
    });

    test(
      'overwrites the oldest request once the ring is full, and says how many',
      () async {
        final (:log, :rest) = _wired(
          (_, _) => _json({'ok': true}),
          capacity: 2,
        );

        for (var i = 0; i < 3; i++) {
          await rest.execute(
            const TransportRequest(meta: _list, args: TagContext.empty),
          );
        }

        expect(log.entries(), hasLength(2));
        expect(log.dropped, 1);
      },
    );
  });

  group('the credential refresh, timed', () {
    test('measures how long a request sat waiting on the refresh', () async {
      final (:log, :rest) = _wired(
        (request, _) => request.headers['Authorization'] == 'Bearer t0'
            ? _json({}, 401)
            : _json({'ok': true}),
        auth: _TokenAuth('t0'),
      );

      await rest.execute(
        const TransportRequest(meta: _list, args: TagContext.empty),
      );

      final entry = log.entries().single;

      expect(entry.refreshes, 1);
      // It started the refresh itself, so it did not merely join one.
      expect(entry.joined, isFalse);
      // The clock ticks once per read, so any wait at all is positive.
      expect(entry.authMs, greaterThan(0));
    });

    test('reports no auth time for a request that never met a 401', () async {
      final (:log, :rest) = _wired((_, _) => _json({'ok': true}));

      await rest.execute(
        const TransportRequest(meta: _list, args: TagContext.empty),
      );

      expect(log.entries().single.authMs, 0);
    });
  });

  group('what the log never keeps', () {
    test(
      'never records an Authorization header, a credential or a request body',
      () async {
        final sent = <String, String>{};
        final (:log, :rest) = _wired((request, _) {
          sent.addAll(request.headers);
          return _json({'ok': true, 'echo': 'response-body-111'});
        }, auth: _TokenAuth('secret-token-123'));

        await rest.execute(
          const TransportRequest(
            meta: _create,
            args: TagContext(
              headers: {
                'X-Api-Key': 'key-789',
                'Authorization': 'Bearer header-secret-456',
              },
              body: {'password': 'hunter2'},
            ),
          ),
        );

        // The premise: the credentials really were on the wire.
        expect(
          sent.keys.map((k) => k.toLowerCase()),
          contains('authorization'),
        );

        final serialised = jsonEncode([
          for (final entry in log.entries()) entry.toJson(),
        ]);

        for (final secret in [
          'secret-token-123',
          'header-secret-456',
          'key-789',
          'Bearer',
          'Authorization',
          'hunter2',
          'password',
          'response-body-111',
        ]) {
          expect(
            serialised,
            isNot(contains(secret)),
            reason: 'the request log kept "$secret"',
          );
        }
        expect(
          log.entries().single.toJson().keys,
          isNot(anyOf(contains('headers'), contains('body'))),
        );
      },
    );
  });

  group('wired into the devtools', () {
    test(
      'says whether anything is recording, rather than showing an empty table',
      () async {
        final h = Harness();
        final bare = attach(h.cache, clock: CounterClock());

        expect(bare.watchingRequests, isFalse);
        expect(bare.requests(), isEmpty);
        bare.dispose();

        final (:log, :rest) = _wired((_, _) => _json({'ok': true}));
        final watched = attach(h.cache, clock: CounterClock(), requests: log);

        await rest.execute(
          const TransportRequest(meta: _list, args: TagContext.empty),
        );

        expect(watched.watchingRequests, isTrue);
        expect(watched.requests(), hasLength(1));
        expect(watched.requestsDropped, 0);
        watched.dispose();
      },
    );
  });

  group('nothing crosses principals', () {
    const get = OperationMeta(
      id: 'op_get',
      method: 'GET',
      path: '/orders/{id}',
    );

    TagContext aliceArgs() => const TagContext(
      path: {'id': 'alice-order-77'},
      query: {'q': 'alice-secret-query'},
      headers: {'Authorization': 'Bearer alice-token-999'},
    );

    test(
      'leaves one marker and none of the first principal after a switch',
      () async {
        final h = Harness();
        final (:log, :rest) = _wired((_, _) => _json({'ok': true}));
        final devtools = attach(h.cache, clock: CounterClock(), requests: log);

        await rest.execute(TransportRequest(meta: get, args: aliceArgs()));

        // The premise: alice's path and query really were recorded.
        expect(
          jsonEncode(devtools.requests().map((e) => e.toJson()).toList()),
          contains('alice-order-77'),
        );
        expect(
          jsonEncode(devtools.requests().map((e) => e.toJson()).toList()),
          contains('alice-secret-query'),
        );

        h.cache.setPrincipal('bob');
        await h.settle();

        final after = devtools.requests();

        expect(after, hasLength(1));
        expect(after.single.marker, isTrue);
        expect(after.single.operation, isNot(contains('orders')));
        expect(after.single.args, isEmpty);
        expect(after.single.method, isEmpty);
        expect(after.single.status, isNull);

        final everything = jsonEncode([
          for (final entry in devtools.requests()) entry.toJson(),
          for (final entry in log.entries()) entry.toJson(),
          for (final entry in devtools.requestLog!.entries()) entry.toJson(),
        ]);

        for (final secret in [
          'alice',
          'orders',
          'secret-query',
          'token-999',
          'Bearer',
        ]) {
          expect(
            everything,
            isNot(contains(secret)),
            reason: 'the request log kept "$secret" past the switch',
          );
        }
        expect(devtools.requestsDropped, 0);

        devtools.dispose();
      },
    );

    test('has purged by the time the changing notification reaches a later listener', () async {
      final h = Harness();
      final (:log, :rest) = _wired((_, _) => _json({'ok': true}));
      final devtools = attach(h.cache, clock: CounterClock(), requests: log);

      await rest.execute(TransportRequest(meta: get, args: aliceArgs()));

      var seen = <RequestSnapshot>[];
      final stop = h.cache.watchPrincipalChanging(
        (_) => seen = devtools.requests(),
      );

      h.cache.setPrincipal('bob');
      stop();

      expect(seen, hasLength(1));
      expect(seen.single.marker, isTrue);

      devtools.dispose();
    });

    test(
      'does not record a request that was in flight across the switch',
      () async {
        final h = Harness();
        final answer = Completer<http.Response>();
        var calls = 0;
        final (:log, :rest) = _wired(
          (_, _) => calls++ == 0 ? answer.future : _json({'ok': true}),
        );
        final devtools = attach(h.cache, clock: CounterClock(), requests: log);

        final inFlight = rest.execute(
          TransportRequest(meta: get, args: aliceArgs()),
        );
        await pumpEventQueue();

        expect(devtools.requests().single.outcome, RequestOutcome.pending);

        h.cache.setPrincipal('bob');
        await h.settle();

        answer.complete(_json({'ok': true}));
        await inFlight;
        await pumpEventQueue();

        // Neither its settle nor anything about it reached the log.
        expect(devtools.requests().single.marker, isTrue);
        expect(devtools.requests().single.outcome, RequestOutcome.ok);
        expect(devtools.requests().single.duration, isNull);
        expect(
          jsonEncode(devtools.requests().map((e) => e.toJson()).toList()),
          isNot(contains('alice')),
        );

        // The log still works for the new principal.
        await rest.execute(
          const TransportRequest(
            meta: _list,
            args: TagContext(query: {'who': 'bob-query'}),
          ),
        );

        final entries = devtools.requests();

        expect(entries, hasLength(2));
        expect(entries.last.operation, 'GET /orders');
        expect(entries.last.args, contains('bob-query'));
        expect(entries.last.outcome, RequestOutcome.ok);

        devtools.dispose();
      },
    );

    test('does not record the retries of a request that was in flight across the switch', () async {
      final h = Harness();
      final gate = Completer<void>();
      var calls = 0;
      final (:log, :rest) = _wired((_, _) async {
        if (calls++ == 0) {
          await gate.future;
          return _json({}, 503);
        }
        return _json({'ok': true});
      });
      final devtools = attach(h.cache, clock: CounterClock(), requests: log);

      final inFlight = rest.execute(
        TransportRequest(meta: get, args: aliceArgs()),
      );
      await pumpEventQueue();

      h.cache.setPrincipal('bob');
      await h.settle();

      gate.complete();
      await inFlight;

      final entries = devtools.requests();

      expect(entries, hasLength(1));
      expect(entries.single.marker, isTrue);
      expect(entries.single.retries, isEmpty);

      devtools.dispose();
    });

    test('keeps queries only as bounded values', () async {
      final (:log, :rest) = _wired((_, _) => _json({'ok': true}));
      final long = 'x' * 5000;

      await rest.execute(
        TransportRequest(
          meta: _list,
          args: TagContext(query: {'q': long}),
        ),
      );

      final args = log.entries().single.args;

      expect(args.length, lessThan(300));
      expect(args, isNot(contains(long)));
    });

    test(
      'drops everything and stops answering once the inspector is disposed',
      () async {
        final h = Harness();
        final (:log, :rest) = _wired((_, _) => _json({'ok': true}));
        final devtools = attach(h.cache, clock: CounterClock(), requests: log);

        await rest.execute(TransportRequest(meta: get, args: aliceArgs()));
        devtools.dispose();

        expect(devtools.requests(), isEmpty);
        expect(devtools.watchingRequests, isFalse);
        expect(log.entries(), isEmpty);
      },
    );
  });
}
