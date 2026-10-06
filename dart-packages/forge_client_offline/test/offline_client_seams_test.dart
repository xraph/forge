@TestOn('vm')
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/harness.dart';

// The client/server idempotency seams: when a key rotates, how long a replay
// may trust the server's memory, what the client does with the middleware's
// Idempotency-Skipped and Idempotent-Truncated headers, and the guarantees
// around persisting and cancelling an attempt.

/// The key the stored record [r] will be sent with.
String storedKey(PendingMutationRecord r) =>
    OutboxEntry.fromRecord(r).idempotencyKey;

/// A reset in the middle of the reply: the request left, its outcome is
/// unknown.
const SocketException lostReply = SocketException('Connection reset by peer');

const Map<String, String> skippedHeaders = {'idempotency-skipped': 'anonymous'};

void main() {
  group('I1: a retry after a final status rotates the key', () {
    test(
      'retry after a 409 sends a new key, persisted before the send',
      () async {
        final h = await Harness.create();

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
        );
        await settle();
        h.network.respond = (r) => r.meta.method == 'GET'
            ? echo(r)
            : throw const HttpStatusError(409, {'error': 'stale'});
        h.goOnline();
        await settle();
        expect(write.error, isA<OutboxConflict>());
        final firstKey = h.writes.single.headers['Idempotency-Key'];

        String? keyInStorageAtSend;
        h.network.respond = (r) async {
          keyInStorageAtSend = storedKey((await h.stored()).single);
          return echo(r);
        };
        await h.offline.currentFailures.single.retry();
        await settle();

        expect(h.writes, hasLength(2));
        final secondKey = h.writes[1].headers['Idempotency-Key'];
        expect(secondKey, isNot(firstKey));
        expect(keyInStorageAtSend, secondKey);
        expect(await h.stored(), isEmpty);
      },
    );

    test('retry after an uncertain failure keeps the key', () async {
      final h = await Harness.create(online: true);
      h.network.respond = (r) =>
          r.meta.method == 'GET' ? echo(r) : throw lostReply;

      // A plain POST: unsafe to repeat, so the lost reply surfaces at once.
      final write = Watched(h.write(opCreateOrder, const TagContext(body: {})));
      await settle();
      expect(write.error, isA<OutboxUncertain>());
      final firstKey = h.writes.single.headers['Idempotency-Key'];

      h.network.respond = echo;
      await h.offline.currentFailures.single.retry();
      await settle();

      expect(h.writes, hasLength(2));
      expect(h.writes[1].headers['Idempotency-Key'], firstKey);
    });

    test('a forced replay after a 403 rotates the key', () async {
      final h = await Harness.create();

      final write = Watched(
        h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
      );
      await settle();
      h.network.respond = (r) => r.meta.method == 'GET'
          ? echo(r)
          : throw const HttpStatusError(403, null);
      h.goOnline();
      await settle();
      expect(write.error, isA<OutboxUnauthorized>());
      final firstKey = h.writes.single.headers['Idempotency-Key'];

      h.network.respond = echo;
      await h.offline.replay(h.offline.pending.single.id);

      expect(h.writes, hasLength(2));
      expect(h.writes[1].headers['Idempotency-Key'], isNot(firstKey));
    });

    test('a rotated key survives a restart', () async {
      final storage = memoryStorage();
      final h = await Harness.create(storage: storage);
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      h.network.respond = (r) => r.meta.method == 'GET'
          ? echo(r)
          : throw const HttpStatusError(422, null);
      h.goOnline();
      await settle();

      // The retry finds the network gone: the write stays queued.
      h.network.respond = (r) => r.meta.method == 'GET'
          ? echo(r)
          : throw const SocketException('Connection refused');
      await h.offline.currentFailures.single.retry();
      await settle();
      final rotated = h.writes.last.headers['Idempotency-Key'];
      expect(rotated, isNot(h.writes.first.headers['Idempotency-Key']));
      expect(storedKey((await h.stored()).single), rotated);

      final again = await Harness.create(storage: storage, online: true);
      await settle();
      expect(again.writes.single.headers['Idempotency-Key'], rotated);
    });
  });

  group('I2: a replay trusts the server only within the idempotency window', () {
    test('an idempotent POST sent before the window surfaces as uncertain after a restart', () async {
      final storage = memoryStorage();
      await seedOutbox(storage, 'alice', [
        seededEntry(
          id: 'm1',
          meta: opCreateOrderIdempotent,
          seq: 1,
          sentAt: DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
        ),
      ]);

      final h = await Harness.create(
        storage: storage,
        online: true,
        clock: ManualClock(start: const Duration(hours: 25).inMilliseconds),
      );
      await settle();

      expect(h.writes, isEmpty);
      expect(h.offline.currentFailures.single, isA<OutboxUncertain>());
    });

    test('inside the window it replays with its key', () async {
      final storage = memoryStorage();
      await seedOutbox(storage, 'alice', [
        seededEntry(
          id: 'm1',
          meta: opCreateOrderIdempotent,
          seq: 1,
          sentAt: DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
        ),
      ]);

      final h = await Harness.create(
        storage: storage,
        online: true,
        clock: ManualClock(start: const Duration(hours: 23).inMilliseconds),
      );
      await settle();

      expect(h.writes.single.headers['Idempotency-Key'], 'key-m1');
      expect(h.offline.currentFailures, isEmpty);
    });

    test(
      'PUT and DELETE are safe by method and replay whatever their age',
      () async {
        final storage = memoryStorage();
        await seedOutbox(storage, 'alice', [
          seededEntry(
            id: 'm1',
            meta: opReplaceOrder,
            seq: 1,
            sentAt: DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
          ),
        ]);

        final h = await Harness.create(
          storage: storage,
          online: true,
          clock: ManualClock(start: const Duration(days: 30).inMilliseconds),
        );
        await settle();

        expect(h.writes.single.headers['Idempotency-Key'], 'key-m1');
      },
    );

    test('the window is a client option', () async {
      final storage = memoryStorage();
      await seedOutbox(storage, 'alice', [
        seededEntry(
          id: 'm1',
          meta: opCreateOrderIdempotent,
          seq: 1,
          sentAt: DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
        ),
      ]);

      final h = await Harness.create(
        storage: storage,
        online: true,
        clock: ManualClock(start: const Duration(hours: 2).inMilliseconds),
        idempotencyWindow: const Duration(hours: 1),
      );
      await settle();

      expect(h.writes, isEmpty);
      expect(h.offline.currentFailures.single, isA<OutboxUncertain>());
    });

    test(
      'in session, the window runs from the first lost reply, not the latest',
      () async {
        final clock = ManualClock(start: 1000);
        final h = await Harness.create(online: true, clock: clock);
        h.network.respond = (r) =>
            r.meta.method == 'GET' ? echo(r) : throw lostReply;

        unawaited(
          h
              .write(opCreateOrderIdempotent, const TagContext(body: {}))
              .catchError((Object _) => null),
        );
        await settle();
        expect(h.writes, hasLength(1));

        // A second attempt, 23 hours on, loses its reply too.
        clock.advance(const Duration(hours: 23));
        h.connectivity.set(false);
        h.goOnline();
        await settle();
        expect(h.writes, hasLength(2));
        expect(h.offline.currentFailures, isEmpty);

        // 25 hours after the first send the server may have forgotten the key.
        clock.advance(const Duration(hours: 2));
        h.network.respond = echo;
        h.connectivity.set(false);
        h.goOnline();
        await settle();

        expect(h.writes, hasLength(2), reason: 'it must not be sent again');
        expect(h.offline.currentFailures.single, isA<OutboxUncertain>());
      },
    );
  });

  group('I3: Idempotency-Skipped is reported', () {
    test('on a success', () async {
      final h = await Harness.create(online: true);
      h.network.respond = (r) async {
        r.onResponse?.call(200, skippedHeaders);
        return echo(r);
      };

      await h.write(opCreateOrderIdempotent, const TagContext(body: {}));

      final reported = h.errors.where(
        (e) => e.$1 == 'forge_client_offline: outbox.idempotency-skipped',
      );
      expect(reported, hasLength(1));
      expect('${reported.single.$2}', contains('op_create_order_idempotent'));
    });

    test('on an error response and on a replay', () async {
      final h = await Harness.create(online: true);
      h.network.respond = (r) async {
        r.onResponse?.call(503, skippedHeaders);
        throw const HttpStatusError(503, null, headers: skippedHeaders);
      };
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})));
      await settle();

      h.network.respond = (r) async {
        r.onResponse?.call(200, skippedHeaders);
        return echo(r);
      };
      h.connectivity.set(false);
      h.goOnline();
      await settle();

      expect(h.writes, hasLength(2));
      expect(
        h.errors.where(
          (e) => e.$1 == 'forge_client_offline: outbox.idempotency-skipped',
        ),
        hasLength(2),
      );
    });

    test('nothing is reported without the header', () async {
      final h = await Harness.create(online: true);
      h.network.respond = (r) async {
        r.onResponse?.call(200, const {'content-type': 'application/json'});
        return echo(r);
      };

      await h.write(opCreateOrderIdempotent, const TagContext(body: {}));

      expect(h.errors, isEmpty);
    });
  });

  group('I4: a write is stored before its first send', () {
    test(
      'it is in storage, marked sent, while the request is out, and gone after',
      () async {
        final h = await Harness.create(online: true);
        final reply = Completer<Object?>();
        h.network.respond = (r) =>
            r.meta.method == 'GET' ? echo(r) : reply.future;

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
        );
        await settle();

        final stored = await h.stored();
        expect(stored, hasLength(1));
        expect(stored.single.stateJson, contains('"kind":"sending"'));
        expect(
          storedKey(stored.single),
          h.writes.single.headers['Idempotency-Key'],
        );
        expect(h.offline.pending, isEmpty, reason: 'in flight, not queued');

        reply.complete(<String, Object?>{'id': '7', 'note': 'x'});
        await settle();
        expect(write.done, isTrue);
        expect(await h.stored(), isEmpty);
      },
    );

    test(
      'killed mid-request, an unsafe write comes back as uncertain',
      () async {
        final storage = memoryStorage();
        final h = await Harness.create(storage: storage, online: true);
        h.network.respond = (r) =>
            r.meta.method == 'GET' ? echo(r) : Completer<Object?>().future;
        unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})));
        await settle();
        expect(h.writes, hasLength(1));

        // The process dies with the request out; the next launch opens the
        // same storage.
        final next = await Harness.create(storage: storage, online: true);
        await settle();

        expect(next.writes, isEmpty);
        expect(next.offline.currentFailures.single, isA<OutboxUncertain>());
      },
    );

    test(
      'killed mid-request, a safe write is replayed with the same key',
      () async {
        final storage = memoryStorage();
        final h = await Harness.create(storage: storage, online: true);
        h.network.respond = (r) =>
            r.meta.method == 'GET' ? echo(r) : Completer<Object?>().future;
        unawaited(h.write(opReplaceOrder, orderArgs('7', {'note': 'x'})));
        await settle();
        final key = h.writes.single.headers['Idempotency-Key'];

        final next = await Harness.create(storage: storage, online: true);
        await settle();

        expect(next.writes.single.headers['Idempotency-Key'], key);
        expect(await next.stored(), isEmpty);
      },
    );

    test(
      'a write the server refuses for good is removed from storage',
      () async {
        final h = await Harness.create(online: true);
        h.network.respond = (r) => r.meta.method == 'GET'
            ? echo(r)
            : throw const HttpStatusError(400, null);

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
        );
        await settle();

        expect(write.error, isA<HttpStatusError>());
        expect(await h.stored(), isEmpty);
      },
    );

    test('a write that never left is stored as queued, not sent', () async {
      final h = await Harness.create(online: true);
      h.network.respond = (r) => r.meta.method == 'GET'
          ? echo(r)
          : throw const SocketException('Connection refused');

      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})));
      await settle();

      expect((await h.stored()).single.stateJson, '{"kind":"queued"}');
      expect(h.offline.pending, hasLength(1));
    });
  });

  group('I5: a principal change cancels the attempt', () {
    test('during an async credentials read, nothing goes out under the next credentials', () async {
      final setup = await _AuthSetup.create();
      final gate = Completer<void>();
      setup.gate = gate;

      final write = Watched(
        setup.cache.mutate(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
      );
      await settle();
      expect(setup.reads, 1, reason: 'the attempt is waiting on credentials');

      setup.cache.setPrincipal('bob');
      setup.token = 'bob';
      gate.complete();
      await settle();

      expect(setup.sent, isEmpty);
      expect(write.error, isA<OutboxSuspended>());
      expect(await storedOutbox(setup.storage, 'alice'), hasLength(1));
      await setup.dispose();
    });

    test('during a 401 refresh, the retry is never sent', () async {
      final setup = await _AuthSetup.create(token: 'expired');
      final refresh = Completer<void>();
      setup.refreshGate = refresh;

      final write = Watched(
        setup.cache.mutate(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
      );
      await settle();
      expect(setup.sent, ['Bearer expired']);
      expect(setup.refreshes, 1);

      setup.cache.setPrincipal('bob');
      setup.token = 'bob';
      refresh.complete();
      await settle();

      expect(setup.sent, ['Bearer expired']);
      expect(write.error, isA<OutboxSuspended>());
      await setup.dispose();
    });

    test('a replay waiting on credentials is cancelled too', () async {
      final setup = await _AuthSetup.create(online: false);
      unawaited(
        setup.cache
            .mutate(opUpdateOrder, orderArgs('7', {'note': 'alice'}))
            .catchError((Object _) => null),
      );
      await settle();
      expect(setup.sent, isEmpty);

      final gate = Completer<void>();
      setup.gate = gate;
      setup.connectivity.set(true);
      await settle();
      expect(setup.reads, 1);

      setup.cache.setPrincipal('bob');
      setup.token = 'bob';
      gate.complete();
      await settle();

      expect(setup.sent, isEmpty);
      final alice = await storedOutbox(setup.storage, 'alice');
      expect(alice, hasLength(1));
      await setup.dispose();
    });
  });

  group('I6: a truncated replay', () {
    test('succeeds with no body and invalidates the entity', () async {
      final h = await Harness.create(online: true);
      await h.seed('7');
      // Only a watched query is refetched, so only a watched one is told.
      final unwatch = h.cache.subscribe(opGetOrder, orderArgs('7'), () {});
      addTearDown(unwatch);
      h.network.respond = (r) async {
        if (r.meta.method == 'GET') return echo(r);
        r.onResponse?.call(201, const {
          'idempotent-replayed': 'true',
          'idempotent-truncated': 'true',
        });
        return null;
      };
      h.events.clear();

      final result = await h.write(opTouchOrder, orderArgs('7', {}));
      await settle();

      expect(result, isNull);
      expect(h.errors, isEmpty);
      expect(
        h.events.whereType<QueryInvalidated>().expand((e) => e.matched),
        contains('Order:7'),
      );
    });

    test(
      'through RestTransport, an empty truncated body is never decoded',
      () async {
        final client = MockClient((request) async {
          if (request.method == 'GET') {
            return http.Response(
              '{"id":"7","total":10}',
              200,
              headers: {'content-type': 'application/json'},
            );
          }
          return http.Response(
            '',
            201,
            headers: {
              'content-type': 'application/json',
              'idempotent-replayed': 'true',
              'idempotent-truncated': 'true',
            },
          );
        });
        final rest = RestTransport(
          baseUrl: Uri.parse('http://x'),
          client: client,
        );
        final errors = <(String, Object)>[];
        final offline = await OfflineClient.open(
          transport: rest,
          entities: entities,
          operations: operations,
          storage: memoryStorage(),
          principal: 'alice',
          onError: (e, c) => errors.add((c, e)),
        );
        final events = <CacheEvent>[];
        await offline.cache.fetch(opGetOrder, orderArgs('7'));
        final unwatch = offline.cache.subscribe(
          opGetOrder,
          orderArgs('7'),
          () {},
        );
        await settle();
        offline.cache.observer = events.add;

        final result = await offline.cache.mutate(
          opTouchOrder,
          orderArgs('7', {}),
        );
        await settle();

        expect(result, isNull);
        expect(errors, isEmpty);
        expect(
          events.whereType<QueryInvalidated>().expand((e) => e.matched),
          contains('Order:7'),
        );
        unwatch();
        await offline.dispose();
      },
    );
  });

  group('minor seams', () {
    test(
      'M1: a replayed 409 that carries Retry-After is a conflict, not busy',
      () async {
        final h = await Harness.create();

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
        );
        await settle();
        h.network.respond = (r) => r.meta.method == 'GET'
            ? echo(r)
            : throw const HttpStatusError(
                409,
                {'error': 'taken'},
                headers: {'retry-after': '5', 'idempotent-replayed': 'true'},
              );
        h.goOnline();
        await settle();

        expect(write.error, isA<OutboxConflict>());
        expect(h.writes, hasLength(1));
      },
    );

    test(
      'M1: a replayed 409 sent directly reaches the caller, not the queue',
      () async {
        final h = await Harness.create(online: true);
        h.network.respond = (r) => r.meta.method == 'GET'
            ? echo(r)
            : throw const HttpStatusError(
                409,
                null,
                headers: {'retry-after': '5', 'idempotent-replayed': 'true'},
              );

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
        );
        await settle();

        expect(write.error, isA<HttpStatusError>());
        expect(h.offline.pending, isEmpty);
        expect(await h.stored(), isEmpty);
      },
    );

    test("M1: the middleware's own busy 409 is still retried", () async {
      final h = await Harness.create(online: true);
      h.network.respond = (r) => r.meta.method == 'GET'
          ? echo(r)
          : throw const HttpStatusError(
              409,
              null,
              headers: {'retry-after': '1'},
            );

      final write = Watched(
        h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
      );
      await settle();

      expect(write.done, isFalse);
      expect(h.offline.pending, hasLength(1));
      expect(h.offline.currentFailures, isEmpty);
    });

    test(
      'M2: the request is built from the stored arguments, not the live ones',
      () async {
        final h = await Harness.create(online: true);
        final tags = <Object?>['a'];

        final write = h.write(
          opUpdateOrder,
          orderArgs('7', {'note': 'x', 'tags': tags}),
        );
        tags.add('mutated after the call');
        await write;

        final body = h.writes.single.args.body! as Map<String, Object?>;
        expect(body['tags'], ['a']);
        expect(identical(body['tags'], tags), isFalse);
      },
    );

    test('M3: a form Set value is stored and sent as a list', () async {
      final h = await Harness.create();

      final write = Watched(
        h.write(
          opTagOrderForm,
          const TagContext(
            path: {'id': '7'},
            body: {
              'tag': {'a', 'b'},
            },
          ),
        ),
      );
      await settle();
      expect(write.error, isNull);
      expect(h.offline.pending.single.args.body, {
        'tag': ['a', 'b'],
      });

      h.goOnline();
      await settle();
      expect((h.writes.single.args.body! as Map<String, Object?>)['tag'], [
        'a',
        'b',
      ]);
      expect(write.done, isTrue);
    });
  });
}

/// A cache and an offline client over a real [RestTransport] whose
/// credentials callback the test can hold, and an HTTP client that records
/// the Authorization header of everything that reached it, ignoring aborts
/// (a socket that already wrote the request cannot take it back).
final class _AuthSetup {
  _AuthSetup._(this.storage, this.connectivity);

  final StorageAdapter storage;
  final FakeConnectivity connectivity;
  late final QueryCache cache;
  late final OfflineClient offline;

  /// Every Authorization header that reached the network.
  final List<String?> sent = [];
  String token = 'alice';
  int reads = 0;
  int refreshes = 0;

  /// Held by the next credentials read when set.
  Completer<void>? gate;

  /// Held by the next refresh when set.
  Completer<void>? refreshGate;

  static Future<_AuthSetup> create({
    String token = 'alice',
    bool online = true,
  }) async {
    final setup = _AuthSetup._(memoryStorage(), FakeConnectivity())
      ..token = token;
    final client = _SendingClient((request) {
      final auth = request.headers['Authorization'];
      setup.sent.add(auth);
      return auth == 'Bearer ${setup.token}' && setup.token != 'expired'
          ? 200
          : 401;
    });
    final rest = RestTransport(
      baseUrl: Uri.parse('http://x'),
      client: client,
      auth: AuthProvider.callbacks(
        credentials: (_) async {
          setup.reads++;
          final held = setup.gate;
          setup.gate = null;
          if (held != null) await held.future;
          return {'Authorization': 'Bearer ${setup.token}'};
        },
        refresh: () async {
          setup.refreshes++;
          final held = setup.refreshGate;
          setup.refreshGate = null;
          if (held != null) await held.future;
          if (setup.token == 'expired') setup.token = 'alice';
        },
      ),
    );
    final outbox = OutboxTransport(rest);
    setup.cache = QueryCache(
      transport: outbox,
      entities: entities,
      storage: setup.storage,
    );
    setup.offline = OfflineClient(
      cache: setup.cache,
      operations: operations,
      connectivity: setup.connectivity,
      transport: outbox,
      initiallyOnline: online,
    );
    await switchTo(setup.cache, 'alice');
    await setup.offline.restore();
    return setup;
  }

  Future<void> dispose() async {
    await offline.dispose();
    await cache.dispose();
  }
}

final class _SendingClient extends http.BaseClient {
  _SendingClient(this.answer);

  final int Function(http.BaseRequest request) answer;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final status = answer(request);
    return http.StreamedResponse(
      Stream.value(utf8.encode(status == 200 ? '{"id":"7"}' : '{}')),
      status,
      headers: {'content-type': 'application/json'},
    );
  }
}
