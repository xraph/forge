// Ported from packages/client-core/__tests__/transport.test.ts.
//
// The TS transport drives the generated client's `request`; the Dart transport
// sends through package:http itself, so these assertions read the recorded
// http.Request instead of a request config.
import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:http/http.dart' as http;
import 'package:test/test.dart';

import 'support/harness.dart';

const list = OperationMeta(
  id: 'orderList',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);

const create = OperationMeta(
  id: 'orderCreate',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  invalidates: ['Order[]'],
);

const detail = OperationMeta(
  id: 'orderGet',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
);

/// Upper-cases every key, standing in for a generated wire codec.
final class ShoutingCodec implements WireCodec {
  const ShoutingCodec();

  @override
  Object? decode(Object? wire) => {
    for (final MapEntry(:key, :value)
        in (wire! as Map<String, Object?>).entries)
      key.toLowerCase(): value,
  };

  @override
  Object? encode(Object? client) => {
    for (final MapEntry(:key, :value)
        in (client! as Map<String, Object?>).entries)
      key.toUpperCase(): value,
  };
}

/// A transport whose backoff is a fixed multiple, so a test can name the delay.
({ManualClock clock, RestTransport transport}) rig(FakeHttp fake) {
  final manual = ManualClock();

  return (
    clock: manual,
    transport: RestTransport(
      baseUrl: base,
      client: fake.client,
      sleep: manual.sleep,
      random: () => 0,
      retry: const RetryPolicy(baseDelay: Duration(milliseconds: 100)),
    ),
  );
}

/// Drains, moves the clock, drains again: the TS `clock.advance`.
Future<void> advance(ManualClock clock, int ms) async {
  await settle();
  clock.advance(Duration(milliseconds: ms));
  await settle();
}

void main() {
  group('operationUrl', () {
    test('substitutes path parameters and percent-encodes them', () {
      expect(
        operationUrl(
          '/orders/{id}/items/{sku}',
          const TagContext(path: {'id': 7, 'sku': 'a/b'}),
        ),
        '/orders/7/items/a%2Fb',
      );
    });

    test(
      'keeps an empty-string query parameter, which addresses no resource',
      () {
        expect(
          operationUrl('/orders', const TagContext(query: {'q': ''})),
          '/orders?q=',
        );
      },
    );

    group('a path parameter that renders to nothing', () {
      test(
        'throws rather than requesting a URL with the placeholder still in it',
        () {
          expect(
            () => operationUrl('/orders/{id}', TagContext.empty),
            throwsA(isA<MissingPathParamsError>()),
          );
          expect(
            () => operationUrl('/orders/{id}', TagContext.empty),
            throwsA(
              predicate((Object error) => !error.toString().contains('%7B')),
            ),
          );
        },
      );

      test(
        'throws for null, which the query loop skips and this one cannot',
        () {
          expect(
            () => operationUrl(
              '/orders/{id}',
              const TagContext(path: {'id': null}),
            ),
            throwsA(isA<MissingPathParamsError>()),
          );
        },
      );

      test('throws for an empty string, which would silently address the collection', () {
        expect(
          () =>
              operationUrl('/orders/{id}', const TagContext(path: {'id': ''})),
          throwsA(isA<MissingPathParamsError>()),
        );
      });

      test('accepts values that are falsy but render to a segment', () {
        expect(
          operationUrl('/orders/{id}', const TagContext(path: {'id': 0})),
          '/orders/0',
        );
        expect(
          operationUrl('/flags/{on}', const TagContext(path: {'on': false})),
          '/flags/false',
        );
      });

      test('names the operation and every missing parameter, once', () {
        MissingPathParamsError? caught;

        try {
          operationUrl(
            '/orgs/{orgId}/repos/{repo}',
            const TagContext(path: {'orgId': null}),
            'GET /orgs/{orgId}/repos/{repo}',
          );
        } on MissingPathParamsError catch (error) {
          caught = error;
        }

        expect(caught, isNotNull);
        expect(caught?.missing, ['orgId', 'repo']);
        expect(caught?.operation, 'GET /orgs/{orgId}/repos/{repo}');
        expect(caught.toString(), contains('{orgId}, {repo}'));
        // TS also asserts `name === 'MissingPathParamsError'`; Dart's type is
        // the name.
        expect(caught.runtimeType.toString(), 'MissingPathParamsError');
      });

      test(
        'falls back to the path template when no operation name is given',
        () {
          expect(
            () => operationUrl('/orders/{id}', TagContext.empty),
            throwsA(
              predicate(
                (Object error) =>
                    error.toString().startsWith('/orders/{id}: no value for'),
              ),
            ),
          );
        },
      );

      test(
        'carries no status, so it is never mistaken for a server answer',
        () {
          const error = MissingPathParamsError('GET /orders/{id}', ['id']);

          expect(statusOf(error), isNull);
        },
      );

      test(
        'does not build the query string of a request it refuses to make',
        () {
          expect(
            () => operationUrl(
              '/orders/{id}',
              const TagContext(query: {'verbose': true}),
            ),
            throwsA(
              predicate(
                (Object error) =>
                    error.toString().endsWith('No request was sent.'),
              ),
            ),
          );
        },
      );
    });

    test('renders query parameters in sorted key order, skipping nullish', () {
      final url = operationUrl(
        '/orders',
        const TagContext(
          query: {
            'status': 'open',
            'after': '2026-01-01',
            'cursor': null,
            'owner': null,
          },
        ),
      );

      expect(url, '/orders?after=2026-01-01&status=open');
    });

    test(
      'repeats an array parameter and appends to an existing query string',
      () {
        expect(
          operationUrl(
            '/orders?fixed=1',
            const TagContext(
              query: {
                'tag': ['a', 'b'],
              },
            ),
          ),
          '/orders?fixed=1&tag=a&tag=b',
        );
      },
    );

    // Dart-only: the URLSearchParams encoding, byte for byte.
    test('encodes the query string exactly as URLSearchParams does', () {
      expect(
        operationUrl(
          '/search',
          const TagContext(query: {'q': 'a b~*é', 'n': 1.0}),
        ),
        '/search?n=1&q=a+b%7E*%C3%A9',
      );
    });
  });

  group('retry classification', () {
    test('retries a failure with no status, and never an abort', () {
      expect(retryable(list, http.ClientException('network down')), isTrue);
      expect(retryable(list, http.RequestAbortedException()), isFalse);
      // Dart-only: classification includes the method, so a POST never retries.
      expect(retryable(create, http.ClientException('network down')), isFalse);
    });

    test('retries 5xx, 408 and 429 but no other 4xx', () {
      for (final status in [500, 503, 408, 429]) {
        expect(
          retryable(list, HttpStatusError(status, null)),
          isTrue,
          reason: '$status',
        );
      }

      for (final status in [400, 401, 404, 422]) {
        expect(
          retryable(list, HttpStatusError(status, null)),
          isFalse,
          reason: '$status',
        );
      }
    });

    // TS reads `statusCode` or `status` off any object. Dart has no structural
    // reads, so only HttpStatusError carries a status.
    test('reads either statusCode or status', () {
      expect(statusOf(const HttpStatusError(429, null)), 429);
      expect(statusOf(Exception('not an object')), isNull);
    });
  });

  group('RestTransport with an unsubstituted path parameter', () {
    test('rejects, sends nothing, and does not spend a retry', () async {
      final fake = FakeHttp(
        (_, _) => [
          {'id': 7},
        ],
      );
      final (:clock, :transport) = rig(fake);

      await expectLater(
        transport.execute(
          const TransportRequest(
            meta: detail,
            args: TagContext(path: {'id': null}),
          ),
        ),
        throwsA(isA<MissingPathParamsError>()),
      );

      expect(fake.calls, isEmpty);
      expect(clock.pending, 0);
    });

    test('names the operation with its method, not just the path', () async {
      final fake = FakeHttp(
        (_, _) => [
          {'id': 7},
        ],
      );
      final (clock: _, :transport) = rig(fake);

      await expectLater(
        transport.execute(
          const TransportRequest(meta: detail, args: TagContext.empty),
        ),
        throwsA(
          predicate(
            (Object error) => error.toString() == 'GET /orders/{id}: no value for path parameter {id}. No request was sent.',
          ),
        ),
      );
    });

    test('still sends the request when the parameter is present', () async {
      final fake = FakeHttp((_, _) => {'id': 7});
      final (clock: _, :transport) = rig(fake);

      expect(
        await transport.execute(
          const TransportRequest(
            meta: detail,
            args: TagContext(path: {'id': 7}),
          ),
        ),
        {'id': 7},
      );
      expect(fake.calls[0].url.toString(), 'https://api.test/orders/7');
    });
  });

  group('RestTransport retries', () {
    test(
      'retries a GET that failed with a 500 and resolves from the retry',
      () async {
        final fake = FakeHttp((_, attempt) {
          if (attempt == 0) throw HttpFailure(500);

          return [
            {'id': 7},
          ];
        });
        final (:clock, :transport) = rig(fake);

        final running = transport.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        );

        await advance(clock, 0);
        expect(fake.calls, hasLength(1));
        expect(clock.pending, 1);

        await advance(clock, 100);

        expect(await running, [
          {'id': 7},
        ]);
        expect(fake.calls, hasLength(2));
      },
    );

    test('never retries a POST, however transient the failure looks', () async {
      final fake = FakeHttp((_, _) => throw HttpFailure(500));
      final (:clock, :transport) = rig(fake);

      await expectLater(
        transport.execute(
          const TransportRequest(
            meta: create,
            args: TagContext(body: {'total': 99}),
          ),
        ),
        throwsA(httpError(500)),
      );

      expect(fake.calls, hasLength(1));
      expect(clock.pending, 0);
    });

    test(
      'does not retry a GET that got a 400, but does retry one that got a 429',
      () async {
        final rejecting = FakeHttp((_, _) => throw HttpFailure(400));
        final first = rig(rejecting);

        await expectLater(
          first.transport.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
          throwsA(httpError(400)),
        );
        expect(rejecting.calls, hasLength(1));

        final throttled = FakeHttp((_, attempt) {
          if (attempt == 0) throw HttpFailure(429);

          return <Object?>[];
        });
        final second = rig(throttled);
        final running = second.transport.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        );

        await advance(second.clock, 100);

        expect(await running, isEmpty);
        expect(throttled.calls, hasLength(2));
      },
    );

    test('gives up after the configured number of attempts', () async {
      final fake = FakeHttp((_, _) => throw HttpFailure(503));
      final (:clock, :transport) = rig(fake);
      final running = transport.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );
      final settled = running.then<Object?>(
        (value) => value,
        onError: (Object error) => error,
      );

      await advance(clock, 100);
      await advance(clock, 200);

      expect(await settled, httpError(503));
      expect(fake.calls, hasLength(3));
    });

    test('backs off exponentially, and jitters within the window', () async {
      final delays = <int>[];
      final fake = FakeHttp((_, _) => throw HttpFailure(500));
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        // Records rather than waits: the delay is the observable.
        sleep: (duration) {
          delays.add(duration.inMilliseconds);

          return Future<void>.value();
        },
        random: () => 1,
        retry: const RetryPolicy(
          attempts: 4,
          baseDelay: Duration(milliseconds: 100),
          maxDelay: Duration(milliseconds: 250),
        ),
      );

      await expectLater(
        rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        ),
        throwsA(anything),
      );

      // 100, 200, then capped at 250; random() == 1 puts each at the top of its
      // window, and random() == 0 would put it at the bottom.
      expect(delays, [100, 200, 250]);

      final floor = <int>[];
      final low = RestTransport(
        baseUrl: base,
        client: FakeHttp((_, _) => throw HttpFailure(500)).client,
        sleep: (duration) {
          floor.add(duration.inMilliseconds);

          return Future<void>.value();
        },
        random: () => 0,
        retry: const RetryPolicy(baseDelay: Duration(milliseconds: 100)),
      );

      await expectLater(
        low.execute(const TransportRequest(meta: list, args: TagContext.empty)),
        throwsA(anything),
      );
      expect(floor, [50, 100]);
    });

    // TS also asserts the generated client's own retry loop is switched off.
    // The Dart transport sends through package:http directly, so there is no
    // second retry loop; the method and URL assertions still apply.
    test('switches the generated client\'s own retry loop off', () async {
      final fake = FakeHttp((_, _) => {'id': 7});
      final (clock: _, :transport) = rig(fake);

      await transport.execute(
        const TransportRequest(
          meta: detail,
          args: TagContext(path: {'id': 7}),
        ),
      );

      expect(fake.calls, hasLength(1));
      expect(fake.calls[0].url.path, '/orders/7');
      expect(fake.calls[0].method, 'GET');
    });

    test('sends the body and the base URL it was configured with', () async {
      final fake = FakeHttp((_, _) => {'id': 8});
      final rest = RestTransport(
        baseUrl: Uri.parse('https://api.example.com'),
        client: fake.client,
      );

      await rest.execute(
        const TransportRequest(
          meta: create,
          args: TagContext(body: {'total': 99}),
          headers: {'X-Trace': 'abc'},
        ),
      );

      expect(fake.calls[0].url.toString(), 'https://api.example.com/orders');
      expect(fake.bodyOf(0), {'total': 99});
      expect(fake.calls[0].headers, containsPair('X-Trace', 'abc'));
    });
  });

  // TS: `RestClientLike` is a compile-time fit for the generated client. The
  // Dart transport takes an http.Client, so there is no adapter to check.
  group('the generated client fits the transport', () {
    test(
      'accepts a RESTClient with no adapter in between',
      () {},
      skip: 'Dart sends through package:http; there is no generated RESTClient seam',
    );
  });

  // TS forwards codec ids for the generated client to apply. The Dart
  // transport applies the WireCodec itself, which is the same parity property
  // from the other side: what the cache sees is always client-shaped.
  group('codec ids reach the generated client', () {
    const coded = OperationMeta(
      id: 'orderCreate',
      method: 'POST',
      path: '/orders',
      entity: 'Order',
      invalidates: ['Order[]'],
      bodyCodec: ShoutingCodec(),
      responseCodec: ShoutingCodec(),
    );

    test(
      'forwards both codec ids from the operation onto the request config',
      () async {
        final fake = FakeHttp((_, _) => {'OK': true});

        final value = await RestTransport(baseUrl: base, client: fake.client)
            .execute(
              const TransportRequest(
                meta: coded,
                args: TagContext(body: {'id': 1}),
              ),
            );

        expect(fake.bodyOf(0), {'ID': 1});
        expect(value, {'ok': true});
      },
    );

    test('omits them entirely for an operation that declares none', () async {
      final fake = FakeHttp((_, _) => {'OK': true});

      final value = await RestTransport(baseUrl: base, client: fake.client)
          .execute(
            const TransportRequest(
              meta: create,
              args: TagContext(body: {'id': 1}),
            ),
          );

      expect(fake.bodyOf(0), {'id': 1});
      expect(value, {'OK': true});
    });

    test('keeps them across the credential refresh retry', () async {
      var attempts = 0;
      final fake = FakeHttp((_, _) {
        attempts++;

        if (attempts == 1) throw HttpFailure(401);

        return {'OK': true};
      });

      final value =
          await RestTransport(
            baseUrl: base,
            client: fake.client,
            auth: AuthProvider.callbacks(
              credentials: (_) => {'authorization': 'Bearer t'},
              refresh: () {},
            ),
          ).execute(
            const TransportRequest(
              meta: coded,
              args: TagContext(body: {'id': 1}),
            ),
          );

      expect(fake.calls, hasLength(2));
      expect(fake.bodyOf(1), {'ID': 1});
      expect(value, {'ok': true});
    });
  });

  // TS forwards a fetch `credentials` mode. In Dart that is a property of the
  // client: pass `BrowserClient()..withCredentials = true` as `client`.
  group('cross-origin credentials', () {
    test(
      'forwards the configured credentials mode to the client',
      () {},
      skip: 'a property of BrowserClient in Dart, not of the transport',
    );
    test(
      "sends nothing when unset, so today's behaviour is unchanged",
      () {},
      skip: 'a property of BrowserClient in Dart, not of the transport',
    );
  });

  // Review focus: a path parameter carrying characters that need encoding
  // must address exactly one segment, byte-identical to what TypeScript's
  // encodeURIComponent produces, and survive Uri.parse unchanged.
  group('path parameters that need encoding', () {
    test(
      'encodes reserved, percent and non-ASCII characters into one segment',
      () {
        expect(
          operationUrl(
            '/files/{name}',
            const TagContext(path: {'name': 'a b?c#d%e/é&f'}),
          ),
          '/files/a%20b%3Fc%23d%25e%2F%C3%A9%26f',
        );
        // encodeURIComponent leaves these alone, and so must we.
        expect(
          operationUrl(
            '/files/{name}',
            const TagContext(path: {'name': "-_.!~*'()"}),
          ),
          "/files/-_.!~*'()",
        );
      },
    );

    test(
      'sends the encoded segment on the wire without double-encoding it',
      () async {
        final fake = FakeHttp((_, _) => {'ok': true});
        final rest = RestTransport(baseUrl: base, client: fake.client);
        const meta = OperationMeta(
          id: 'fileGet',
          method: 'GET',
          path: '/files/{name}',
        );

        await rest.execute(
          const TransportRequest(
            meta: meta,
            args: TagContext(path: {'name': 'a b/é?'}),
          ),
        );

        expect(
          fake.calls[0].url.toString(),
          'https://api.test/files/a%20b%2F%C3%A9%3F',
        );
        expect(fake.calls[0].url.pathSegments, ['files', 'a b/é?']);
      },
    );
  });

  // Dart-only: decoding.
  group('response bodies', () {
    test(
      'returns null for an empty 204 and the text of a non-JSON body',
      () async {
        final fake = FakeHttp(
          (request, _) => request.url.path == '/orders'
              ? http.Response('', 204)
              : http.Response(
                  'plain',
                  200,
                  headers: {'content-type': 'text/plain'},
                ),
        );
        final rest = RestTransport(baseUrl: base, client: fake.client);

        expect(
          await rest.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
          isNull,
        );
        expect(
          await rest.execute(
            const TransportRequest(
              meta: detail,
              args: TagContext(path: {'id': 1}),
            ),
          ),
          'plain',
        );
      },
    );

    test('carries the decoded body on a status error', () async {
      final fake = FakeHttp(
        (_, _) => http.Response(
          '{"message":"nope"}',
          422,
          headers: {'content-type': 'application/json'},
        ),
      );
      final rest = RestTransport(baseUrl: base, client: fake.client);

      await expectLater(
        rest.execute(
          const TransportRequest(
            meta: create,
            args: TagContext(body: {}),
          ),
        ),
        throwsA(
          isA<HttpStatusError>().having((error) => error.body, 'body', {
            'message': 'nope',
          }),
        ),
      );
    });

    test(
      'throws FormatException for malformed JSON on success response',
      () async {
        final fake = FakeHttp(
          (_, _) => http.Response(
            '{bad',
            200,
            headers: {'content-type': 'application/json'},
          ),
        );
        final rest = RestTransport(baseUrl: base, client: fake.client);

        await expectLater(
          rest.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
          throwsA(isA<FormatException>()),
        );
      },
    );

    test('preserves status when JSON body is malformed and triggers refresh on 401', () async {
      var attempts = 0;
      final fake = FakeHttp((_, _) {
        attempts++;
        if (attempts == 1) {
          return http.Response(
            '<html>not json</html>',
            401,
            headers: {'content-type': 'application/json'},
          );
        }
        return {'ok': true};
      });

      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        auth: AuthProvider.callbacks(
          credentials: (_) => {'authorization': 'Bearer t'},
          refresh: () {},
        ),
      );

      final result = await rest.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );

      expect(attempts, 2);
      expect(result, {'ok': true});
    });

    test(
      'retries a GET with malformed JSON 502 with correct status in event',
      () async {
        final events = <RequestEvent>[];
        var attempts = 0;
        final fake = FakeHttp((_, _) {
          attempts++;
          if (attempts == 1) {
            return http.Response(
              'not json at all',
              502,
              headers: {'content-type': 'application/json'},
            );
          }
          return {'ok': true};
        });

        final manual = ManualClock();
        final rest = RestTransport(
          baseUrl: base,
          client: fake.client,
          sleep: manual.sleep,
          random: () => 0,
          retry: const RetryPolicy(baseDelay: Duration(milliseconds: 100)),
          observer: events.add,
        );

        final running = rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        );

        await settle();
        manual.advance(const Duration(milliseconds: 100));
        await settle();

        expect(await running, {'ok': true});
        expect(fake.calls, hasLength(2));

        final retried = events.whereType<RequestRetried>().single;
        expect(retried.status, 502);
      },
    );

    test('does not retry a GET with malformed JSON 400', () async {
      final fake = FakeHttp(
        (_, _) => http.Response(
          'not json at all',
          400,
          headers: {'content-type': 'application/json'},
        ),
      );
      final rest = RestTransport(baseUrl: base, client: fake.client);

      await expectLater(
        rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        ),
        throwsA(
          isA<HttpStatusError>().having((error) => error.status, 'status', 400),
        ),
      );

      expect(fake.calls, hasLength(1));
    });
  });

  group('timeout handling', () {
    test('times out when headers arrive but the body stalls', () async {
      final bodyStream = StreamController<List<int>>();
      final client = TimeoutTestClient(
        handleRequest: (_) => http.StreamedResponse(
          bodyStream.stream,
          200,
          headers: {'content-type': 'application/json'},
        ),
      );

      addTearDown(bodyStream.close);

      final rest = RestTransport(
        baseUrl: base,
        client: client,
        timeout: const Duration(milliseconds: 50),
        retry: const RetryPolicy(attempts: 1),
      );

      await expectLater(
        rest.execute(
          const TransportRequest(
            meta: create,
            args: TagContext(body: {}),
          ),
        ),
        throwsA(isA<TimeoutException>()),
      );
    });

    test('does not retry a timed-out POST', () async {
      final bodyStream = StreamController<List<int>>();
      var requestCount = 0;
      final client = TimeoutTestClient(
        handleRequest: (_) {
          requestCount++;
          return http.StreamedResponse(
            bodyStream.stream,
            200,
            headers: {'content-type': 'application/json'},
          );
        },
      );

      addTearDown(bodyStream.close);

      final rest = RestTransport(
        baseUrl: base,
        client: client,
        timeout: const Duration(milliseconds: 50),
      );

      await expectLater(
        rest.execute(
          const TransportRequest(
            meta: create,
            args: TagContext(body: {}),
          ),
        ),
        throwsA(isA<TimeoutException>()),
      );

      expect(requestCount, 1);
    });

    test('retries a GET whose first attempt times out on the body', () async {
      var attempt = 0;
      final bodyStream = StreamController<List<int>>();

      addTearDown(bodyStream.close);

      final client = TimeoutTestClient(
        handleRequest: (request) {
          if (attempt == 0) {
            attempt++;
            return http.StreamedResponse(
              bodyStream.stream,
              200,
              headers: {'content-type': 'application/json'},
            );
          }
          return http.StreamedResponse(
            Stream.value(utf8.encode('{"ok":true}')),
            200,
            headers: {'content-type': 'application/json'},
          );
        },
      );

      final rest = RestTransport(
        baseUrl: base,
        client: client,
        timeout: const Duration(milliseconds: 50),
        sleep: (_) => Future<void>.value(),
      );

      final result = await rest.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );

      expect(result, {'ok': true});
      expect(client.recordedRequests, hasLength(2));
      expect(client.recordedAborts[0], isTrue);
    });

    group('caller cancel aborts the in-flight request', () {
      for (final timeout in <Duration?>[const Duration(seconds: 5), null]) {
        test('with timeout: $timeout', () async {
          final body = StreamController<List<int>>();
          addTearDown(body.close);
          final client = TimeoutTestClient(
            handleRequest: (_) => http.StreamedResponse(
              body.stream,
              200,
              headers: {'content-type': 'application/json'},
            ),
          );
          final transport = RestTransport(
            baseUrl: Uri.parse('https://api.test'),
            client: client,
            timeout: timeout,
          );
          final cancel = Completer<void>();
          final pending = transport.execute(
            TransportRequest(
              meta: list, // GET: retryable, so "not retried" is the cancel's doing
              args: TagContext.empty,
              cancel: cancel.future,
            ),
          );
          await pumpEventQueue(); // request sent, headers delivered, body stalled
          expect(client.recordedRequests, hasLength(1));
          final clock = Stopwatch()..start();
          cancel.complete();
          // The lib's own abort, not the timeout's TimeoutException.
          await expectLater(
            pending,
            throwsA(isA<http.RequestAbortedException>()),
          );
          clock.stop();
          expect(clock.elapsed, lessThan(const Duration(seconds: 1)));
          await pumpEventQueue(); // let the abortTrigger listener run
          expect(client.recordedAborts.single, isTrue);
          expect(
            client.recordedRequests,
            hasLength(1),
          ); // GET, still not retried
        });
      }
    });

    test('aborts the timed-out attempt', () async {
      final body = StreamController<List<int>>();
      addTearDown(body.close);
      final client = TimeoutTestClient(
        handleRequest: (_) => http.StreamedResponse(
          body.stream,
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      final transport = RestTransport(
        baseUrl: Uri.parse('https://api.test'),
        client: client,
        timeout: const Duration(milliseconds: 50),
      );
      await expectLater(
        transport.execute(
          const TransportRequest(meta: create, args: TagContext.empty),
        ),
        throwsA(isA<TimeoutException>()),
      );
      await pumpEventQueue();
      expect(client.recordedAborts.single, isTrue);
    });
  });

  // Dart-only: TypeScript's fetch has no client to release.
  group('close', () {
    test('closes the http client the transport created', () {
      final made = ClosingClient();
      final rest = http.runWithClient(
        () => RestTransport(baseUrl: base),
        () => made,
      );

      rest.close();

      expect(made.closed, 1);
    });

    test('leaves a client that was passed in open', () {
      final passed = ClosingClient();
      final rest = RestTransport(baseUrl: base, client: passed);

      rest.close();

      expect(passed.closed, 0);
    });
  });
}

/// An http client that counts how often it was closed.
final class ClosingClient extends http.BaseClient {
  int closed = 0;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async =>
      http.StreamedResponse(const Stream.empty(), 200);

  @override
  void close() => closed++;
}
