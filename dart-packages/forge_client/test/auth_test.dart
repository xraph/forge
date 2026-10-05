// Ported from packages/client-core/__tests__/auth.test.ts.
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
const metrics = OperationMeta(
  id: 'metrics',
  method: 'GET',
  path: '/metrics',
  security: ['apiKey'],
);
const health = OperationMeta(id: 'health', method: 'GET', path: '/health');

final class GatedAuth implements AuthProvider {
  final Completer<void> gate = Completer<void>();
  String token = 't0';
  int refreshes = 0;

  @override
  Map<String, String> credentials(OperationMeta meta) => {
    'Authorization': 'Bearer $token',
  };

  @override
  Future<void> refresh() async {
    refreshes++;
    await gate.future;
    token = 't1';
  }
}

/// Rejects anything not bearing the current token.
FakeHttp guarded(String Function() valid) => FakeHttp((request, _) {
  if (request.headers['Authorization'] != 'Bearer ${valid()}') {
    throw HttpFailure(401);
  }

  return {'ok': true};
});

void main() {
  group('credential attach', () {
    test('attaches per the endpoint\'s declared scheme, and nothing to a public one', () async {
      final fake = FakeHttp((_, _) => <String, Object?>{});
      final auth = AuthProvider.callbacks(
        credentials: (meta) {
          if (meta.security.contains('bearer')) {
            return {'Authorization': 'Bearer t0'};
          }
          if (meta.security.contains('apiKey')) return {'X-Api-Key': 'k0'};

          return null;
        },
      );
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        auth: auth,
      );

      await rest.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );
      await rest.execute(
        const TransportRequest(meta: metrics, args: TagContext.empty),
      );
      await rest.execute(
        const TransportRequest(meta: health, args: TagContext.empty),
      );

      expect(fake.calls[0].headers, containsPair('Authorization', 'Bearer t0'));
      expect(fake.calls[0].headers.containsKey('X-Api-Key'), isFalse);
      expect(fake.calls[1].headers, containsPair('X-Api-Key', 'k0'));
      expect(fake.calls[1].headers.containsKey('Authorization'), isFalse);
      expect(fake.calls[2].headers.containsKey('Authorization'), isFalse);
      expect(fake.calls[2].headers.containsKey('X-Api-Key'), isFalse);
    });

    test('merges credentials over the caller\'s own headers', () async {
      final fake = FakeHttp((_, _) => <String, Object?>{});
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        auth: AuthProvider.callbacks(
          credentials: (_) => {'Authorization': 'Bearer t0'},
        ),
      );

      await rest.execute(
        const TransportRequest(
          meta: list,
          args: TagContext.empty,
          headers: {'X-Trace': 'abc', 'Authorization': 'caller'},
        ),
      );

      expect(fake.calls[0].headers, containsPair('X-Trace', 'abc'));
      expect(fake.calls[0].headers, containsPair('Authorization', 'Bearer t0'));
    });
  });

  group('single-flight refresh', () {
    test(
      'turns two concurrent 401s into one refresh, and retries both',
      () async {
        final auth = GatedAuth();
        final fake = guarded(() => 't1');
        final rest = RestTransport(
          baseUrl: base,
          client: fake.client,
          auth: auth,
          sleep: (_) => Future<void>.value(),
        );

        final first = rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        );
        final second = rest.execute(
          const TransportRequest(
            meta: list,
            args: TagContext(query: {'status': 'open'}),
          ),
        );

        await settle();
        expect(auth.refreshes, 1);
        expect(fake.calls, hasLength(2));

        auth.gate.complete();

        expect(await first, {'ok': true});
        expect(await second, {'ok': true});

        expect(auth.refreshes, 1);
        expect(fake.calls, hasLength(4));
        expect(
          fake.calls[2].headers,
          containsPair('Authorization', 'Bearer t1'),
        );
        expect(
          fake.calls[3].headers,
          containsPair('Authorization', 'Bearer t1'),
        );
      },
    );

    test(
      'retries exactly once: a 401 against a fresh credential is an answer',
      () async {
        final auth = GatedAuth();
        final fake = guarded(() => 'never');
        final rest = RestTransport(
          baseUrl: base,
          client: fake.client,
          auth: auth,
        );

        final running = rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        );

        await settle();
        auth.gate.complete();

        await expectLater(running, throwsA(httpError(401)));
        expect(auth.refreshes, 1);
        expect(fake.calls, hasLength(2));
      },
    );

    test(
      'surfaces the 401, not the refresh failure, when the refresh fails',
      () async {
        final fake = guarded(() => 'never');
        final rest = RestTransport(
          baseUrl: base,
          client: fake.client,
          auth: AuthProvider.callbacks(
            credentials: (_) => {'Authorization': 'Bearer t0'},
            refresh: () =>
                Future<void>.error(StateError('refresh token expired')),
          ),
        );

        await expectLater(
          rest.execute(
            const TransportRequest(meta: list, args: TagContext.empty),
          ),
          throwsA(httpError(401)),
        );
        expect(fake.calls, hasLength(1));
      },
    );

    test('starts a new refresh for a 401 that arrives after the previous one landed', () async {
      var token = 't0';
      var refreshes = 0;
      final auth = AuthProvider.callbacks(
        credentials: (_) => {'Authorization': 'Bearer $token'},
        refresh: () {
          refreshes++;
          token = 't$refreshes';
        },
      );
      // The first attempt of each operation 401s; the retry succeeds.
      final fake = FakeHttp((_, attempt) {
        if (attempt.isEven) throw HttpFailure(401);

        return {'ok': true};
      });
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        auth: auth,
      );

      await rest.execute(
        const TransportRequest(meta: list, args: TagContext.empty),
      );
      expect(refreshes, 1);
      expect(fake.calls[1].headers, containsPair('Authorization', 'Bearer t1'));

      await rest.execute(
        const TransportRequest(meta: metrics, args: TagContext.empty),
      );

      expect(refreshes, 2);
    });

    test('leaves a 401 alone when no refresh is configured', () async {
      final fake = guarded(() => 'never');
      final rest = RestTransport(
        baseUrl: base,
        client: fake.client,
        auth: AuthProvider.callbacks(
          credentials: (_) => {'Authorization': 'Bearer t0'},
        ),
      );

      await expectLater(
        rest.execute(
          const TransportRequest(meta: list, args: TagContext.empty),
        ),
        throwsA(httpError(401)),
      );
      expect(fake.calls, hasLength(1));
    });
  });
}
