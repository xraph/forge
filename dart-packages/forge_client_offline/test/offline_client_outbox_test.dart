@TestOn('vm')
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/harness.dart';

/// Answers every write with [errors] in turn, then echoes.
Future<Object?> Function(TransportRequest) failingWith(List<Object> errors) {
  var calls = 0;
  return (r) {
    if (r.meta.method == 'GET') return echo(r);
    final i = calls++;
    if (i < errors.length) throw errors[i];
    return echo(r);
  };
}

/// Reads [future] inside fakeAsync.
T readIn<T>(FakeAsync async, Future<T> future) {
  late T value;
  var done = false;
  unawaited(
    future.then((v) {
      value = v;
      done = true;
    }),
  );
  async.flushMicrotasks();
  expect(done, isTrue, reason: 'the future did not complete');
  return value;
}

void main() {
  group('queue and replay', () {
    test('a write made offline is queued, persisted and not sent', () async {
      final h = await Harness.create();
      await h.seed('7');

      final write = Watched(h.update('7', {'note': 'queued'}));
      await settle();

      expect(h.writes, isEmpty);
      expect(write.done, isFalse);
      expect(h.order('7')!['note'], 'queued');

      final stored = await h.stored();
      expect(stored, hasLength(1));
      expect(stored.single.operationId, 'op_update_order');
      expect(stored.single.idempotencyKey, isNotEmpty);
      expect(stored.single.stateJson, '{"kind":"queued"}');
      expect(h.offline.pending, hasLength(1));
    });

    test('reconnecting replays with the Idempotency-Key and settles the original call', () async {
      final h = await Harness.create();
      await h.seed('7');
      final write = Watched(h.update('7', {'note': 'queued'}));
      await settle();
      final key = (await h.stored()).single.idempotencyKey;

      h.goOnline();
      await settle();

      expect(h.writes, hasLength(1));
      expect(h.writes.single.headers['Idempotency-Key'], key);
      expect(h.writes.single.headers.containsKey(outboxReplayHeader), isFalse);
      expect(write.done, isTrue);
      expect((write.value! as Map<String, Object?>)['note'], 'queued');
      expect(await h.stored(), isEmpty);
      expect(h.offline.pending, isEmpty);
    });

    test('an online write with an idle lane is sent at once with a key and not stored', () async {
      final h = await Harness.create(online: true);
      await h.seed('7');

      await h.update('7', {'note': 'now'});

      expect(h.writes.single.headers['Idempotency-Key'], isNotEmpty);
      expect(await h.stored(), isEmpty);
    });

    test('a network error before any response queues the write with the key it was sent with', () async {
      final h = await Harness.create(online: true);
      await h.seed('7');
      h.network.respond = (r) => r.meta.method == 'GET'
          ? echo(r)
          : throw const SocketException('Connection refused');

      final write = Watched(h.update('7', {'note': 'queued'}));
      await settle();

      expect(write.done, isFalse);
      expect(h.order('7')!['note'], 'queued');
      expect(await h.stored(), hasLength(1));

      h.network.respond = echo;
      h.goOnline();
      await settle();

      // The failed direct attempt and the replay on reconnect: no replay
      // in between while the client still looked online.
      expect(h.writes, hasLength(2));
      expect(
        h.writes[1].headers['Idempotency-Key'],
        h.writes[0].headers['Idempotency-Key'],
      );
      expect(write.done, isTrue);
    });

    test('a write the network refused while online waits for the backoff', () {
      fakeAsync((async) {
        final h = Harness.createIn(async, online: true);
        h.network.respond = failingWith([
          const SocketException('Connection refused'),
        ]);

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
        );
        async.flushMicrotasks();
        expect(h.writes, hasLength(1));
        expect(h.offline.pending, hasLength(1));

        async.elapse(const Duration(milliseconds: 999));
        expect(h.writes, hasLength(1));

        async.elapse(const Duration(milliseconds: 1));
        expect(h.writes, hasLength(2));
        expect(write.done, isTrue);
        expect(write.error, isNull);
      });
    });

    test('a write queued during the backoff does not trigger a replay', () {
      fakeAsync((async) {
        final h = Harness.createIn(async, online: true);
        h.network.respond = failingWith([
          const SocketException('Connection refused'),
        ]);

        unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'a'})));
        async.flushMicrotasks();
        expect(h.writes, hasLength(1));

        // Parked behind the first: storing it must not replay the first
        // while the backoff runs.
        unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'b'})));
        async.flushMicrotasks();
        expect(h.writes, hasLength(1));
        expect(h.offline.pending, hasLength(2));

        async.elapse(const Duration(seconds: 1));
        expect(h.writes.map(noteOf), ['a', 'a', 'b']);
      });
    });

    test('replays in the order the writes were made', () async {
      final h = await Harness.create();
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'a'})));
      unawaited(
        h.write(
          opUpdateCustomer,
          const TagContext(path: {'id': '1'}, body: {'name': 'b'}),
        ),
      );
      unawaited(h.write(opUpdateOrder, orderArgs('8', {'note': 'c'})));
      await settle();

      h.goOnline();
      await settle();

      expect(h.writes.map((r) => r.meta.id), [
        'op_update_order',
        'op_update_customer',
        'op_update_order',
      ]);
      expect(h.writes.map((r) => r.args.path['id']), ['7', '1', '8']);
    });

    test('one write at a time per entity', () async {
      final h = await Harness.create();
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'first'})));
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'second'})));
      await settle();

      final held = Completer<Object?>();
      h.network.respond = (r) => h.writes.length == 1 ? held.future : echo(r);
      h.goOnline();
      await settle();

      expect(h.writes, hasLength(1));

      held.complete(<String, Object?>{'id': '7', 'total': 10, 'note': 'first'});
      await settle();

      expect(h.writes, hasLength(2));
      expect(
        (h.writes[1].args.body! as Map<String, Object?>)['note'],
        'second',
      );
    });

    test('a blocked lane does not hold up another entity', () async {
      final h = await Harness.create();
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'y'})));
      unawaited(
        h.write(
          opUpdateCustomer,
          const TagContext(path: {'id': '1'}, body: {'name': 'z'}),
        ),
      );
      await settle();

      h.network.respond = (r) => noteOf(r) == 'x'
          ? throw const HttpStatusError(409, {'error': 'stale'})
          : echo(r);
      h.goOnline();
      await settle();

      expect(h.writes.map((r) => r.meta.id), [
        'op_update_order',
        'op_update_customer',
      ]);
      expect(h.offline.currentFailures.single, isA<OutboxConflict>());
    });

    test('two identical writes send two requests by default', () async {
      final h = await Harness.create();
      final first = Watched(
        h.write(opCreateOrder, const TagContext(body: {'total': 5})),
      );
      final second = Watched(
        h.write(opCreateOrder, const TagContext(body: {'total': 5})),
      );
      await settle();

      final stored = await h.stored();
      expect(stored, hasLength(2));
      expect(stored[0].idempotencyKey, isNot(stored[1].idempotencyKey));

      h.goOnline();
      await settle();

      expect(h.writes, hasLength(2));
      expect(first.done && second.done, isTrue);
    });

    test(
      'a double tap within an opted-in duplicate window is coalesced',
      () async {
        final h = await Harness.create(
          duplicateWindow: const Duration(milliseconds: 300),
        );
        final first = Watched(
          h.write(opCreateOrder, const TagContext(body: {'total': 5})),
        );
        final second = Watched(
          h.write(opCreateOrder, const TagContext(body: {'total': 5})),
        );
        await settle();

        expect(await h.stored(), hasLength(1));

        h.goOnline();
        await settle();

        expect(h.writes, hasLength(1));
        expect(first.done && second.done, isTrue);
        expect(second.value, first.value);
      },
    );

    test('identical writes outside an opted-in window stay separate', () async {
      final h = await Harness.create(
        duplicateWindow: const Duration(milliseconds: 300),
      );
      unawaited(h.write(opCreateOrder, const TagContext(body: {'total': 5})));
      await settle();
      h.clock.advance(const Duration(seconds: 1));
      unawaited(h.write(opCreateOrder, const TagContext(body: {'total': 5})));
      await settle();

      final stored = await h.stored();
      expect(stored, hasLength(2));
      expect(stored[0].idempotencyKey, isNot(stored[1].idempotencyKey));
    });

    test(
      'emits OutboxEnqueued and OutboxReplayed and counts pending writes',
      () async {
        final h = await Harness.create();
        final counts = <int>[];
        h.offline.pendingCount.listen(counts.add);
        expect(h.offline.pendingCountNow, 0);

        unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})));
        await settle();
        expect(h.offline.pendingCountNow, 1);

        h.goOnline();
        await settle();

        expect(h.events.whereType<OutboxEnqueued>(), hasLength(1));
        expect(h.events.whereType<OutboxReplayed>(), hasLength(1));
        expect(counts, containsAllInOrder(<int>[1, 0]));
        expect(h.offline.pendingCountNow, 0);
      },
    );

    test('an excluded entity goes straight to the network', () async {
      final h = await Harness.create(excludedEntities: {'Order'});

      await h.write(opUpdateOrder, orderArgs('7', {'note': 'x'}));

      expect(h.writes, hasLength(1));
      expect(await h.stored(), isEmpty);
    });

    test('every attempt for one write carries the same Idempotency-Key, and none is stored', () {
      fakeAsync((async) {
        final h = Harness.createIn(async, online: true);
        h.network.respond = failingWith([
          const HttpStatusError(503, null),
          const SocketException('Connection refused'),
          const HttpStatusError(409, null, headers: {'retry-after': '1'}),
          const HttpStatusError(429, null),
        ]);

        final write = Watched(
          h.write(
            opCreateOrder,
            const TagContext(body: {'total': 5}),
            headers: {
              'Authorization': 'Bearer alice',
              'Idempotency-Key': 'caller-key',
              'X-Trace': 't',
            },
          ),
        );
        async.flushMicrotasks();

        final record = readIn(async, h.stored()).single;
        expect(record.idempotencyKey, 'caller-key');
        final envelope = jsonDecode(record.argsJson) as Map<String, Object?>;
        expect(envelope['requestHeaders'], {'X-Trace': 't'});

        async.elapse(const Duration(minutes: 1));

        expect(h.writes, hasLength(5));
        for (final request in h.writes) {
          expect(request.headers['Idempotency-Key'], 'caller-key');
          expect(
            request.headers.keys.where(
              (k) => k.toLowerCase() == 'idempotency-key',
            ),
            hasLength(1),
          );
          expect(request.headers['X-Trace'], 't');
        }
        // Credentials are never stored: only the first, live attempt had
        // the caller's header. The transport supplies them fresh.
        expect(h.writes.first.headers['Authorization'], 'Bearer alice');
        for (final replay in h.writes.skip(1)) {
          expect(replay.headers.containsKey('Authorization'), isFalse);
        }
        expect(write.done, isTrue);
        expect(write.error, isNull);
        expect(readIn(async, h.stored()), isEmpty);
      });
    });
  });

  group('failures', () {
    for (final (status, matcher) in [
      (409, isA<OutboxConflict>()),
      (412, isA<OutboxConflict>()),
      (400, isA<OutboxValidation>()),
      (422, isA<OutboxValidation>()),
      (418, isA<OutboxValidation>()),
      (401, isA<OutboxUnauthorized>()),
      (403, isA<OutboxUnauthorized>()),
      (404, isA<OutboxGone>()),
      (410, isA<OutboxGone>()),
    ]) {
      test(
        'a $status on replay fails the write, rolls back the overlay and keeps the record',
        () async {
          final h = await Harness.create();
          await h.seed('7');
          final failures = <OutboxFailure>[];
          h.offline.failures.listen(failures.add);

          final write = Watched(h.update('7', {'note': 'queued'}));
          await settle();
          h.network.respond = (r) => r.meta.method == 'GET'
              ? echo(r)
              : throw HttpStatusError(status, {'error': 'no'});
          h.goOnline();
          await settle();

          expect(write.error, matcher);
          expect(h.order('7')!['note'], isNull);
          expect(failures.single, matcher);
          expect(h.offline.currentFailures.single, matcher);
          expect(h.events.whereType<OutboxFailed>(), hasLength(1));
          final state = (await h.stored()).single.stateJson.toString();
          expect(state, contains('"kind":"failed"'));
          expect(state, contains('"status":$status'));
        },
      );
    }

    test('a 503 keeps the write and retries after backoff', () {
      fakeAsync((async) {
        final h = Harness.createIn(async);
        unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})));
        async.flushMicrotasks();

        var calls = 0;
        h.network.respond = (r) =>
            ++calls == 1 ? throw const HttpStatusError(503, null) : echo(r);
        h.goOnline();
        async.flushMicrotasks();
        expect(h.writes, hasLength(1));
        expect(h.offline.currentFailures, isEmpty);

        async.elapse(const Duration(seconds: 1));
        expect(h.writes, hasLength(2));
        expect(h.offline.pending, isEmpty);
      });
    });

    for (final status in [408, 429, 500, 502, 504]) {
      test('a $status on replay retries with the same key', () {
        fakeAsync((async) {
          final h = Harness.createIn(async);
          final write = Watched(
            h.write(opCreateOrder, const TagContext(body: {'total': 5})),
          );
          async.flushMicrotasks();

          h.network.respond = failingWith([HttpStatusError(status, null)]);
          h.goOnline();
          async.flushMicrotasks();
          expect(h.writes, hasLength(1));
          expect(h.offline.currentFailures, isEmpty);
          expect(
            readIn(async, h.stored()).single.stateJson,
            '{"kind":"queued"}',
          );

          async.elapse(const Duration(seconds: 1));
          expect(h.writes, hasLength(2));
          expect(
            h.writes[1].headers['Idempotency-Key'],
            h.writes[0].headers['Idempotency-Key'],
          );
          expect(write.error, isNull);
          expect(write.done, isTrue);
        });
      });
    }

    test('a 409 with Retry-After is the key still in flight: it waits that long and retries', () {
      fakeAsync((async) {
        final h = Harness.createIn(async);
        final failures = <OutboxFailure>[];
        h.offline.failures.listen(failures.add);
        final write = Watched(
          h.write(opCreateOrder, const TagContext(body: {'total': 5})),
        );
        async.flushMicrotasks();

        h.network.respond = failingWith([
          const HttpStatusError(
            409,
            {'error': 'in flight'},
            headers: {'Retry-After': '3'},
          ),
        ]);
        h.goOnline();
        async.flushMicrotasks();
        expect(h.writes, hasLength(1));
        expect(h.offline.currentFailures, isEmpty);

        // Not at the 1 s backoff: Retry-After says 3 s.
        async.elapse(const Duration(milliseconds: 2999));
        expect(h.writes, hasLength(1));

        async.elapse(const Duration(milliseconds: 1));
        expect(h.writes, hasLength(2));
        expect(
          h.writes[1].headers['Idempotency-Key'],
          h.writes[0].headers['Idempotency-Key'],
        );
        expect(failures, isEmpty);
        expect(write.error, isNull);
        expect(write.done, isTrue);
      });
    });

    test('a huge Retry-After is capped at the maximum backoff', () {
      fakeAsync((async) {
        final h = Harness.createIn(async);
        unawaited(h.write(opCreateOrder, const TagContext(body: {'total': 5})));
        async.flushMicrotasks();

        h.network.respond = failingWith([
          const HttpStatusError(503, null, headers: {'retry-after': '86400'}),
        ]);
        h.goOnline();
        async.flushMicrotasks();

        async.elapse(const Duration(minutes: 5));
        expect(h.writes, hasLength(2));
      });
    });

    test('an online write answered 503 is stored and retried, not failed', () {
      fakeAsync((async) {
        final h = Harness.createIn(async, online: true);
        h.network.respond = failingWith([const HttpStatusError(503, null)]);

        final write = Watched(
          h.write(opCreateOrder, const TagContext(body: {'total': 5})),
        );
        async.flushMicrotasks();
        expect(write.done, isFalse);
        expect(h.offline.pending, hasLength(1));

        async.elapse(const Duration(seconds: 1));
        expect(h.writes, hasLength(2));
        expect(
          h.writes[1].headers['Idempotency-Key'],
          h.writes[0].headers['Idempotency-Key'],
        );
        expect(write.done, isTrue);
        expect(write.error, isNull);
      });
    });

    test(
      'an online write answered 4xx goes to the caller and is not stored',
      () async {
        final h = await Harness.create(online: true);
        h.network.respond = failingWith([
          const HttpStatusError(422, {'error': 'bad'}),
        ]);

        final write = Watched(
          h.write(opCreateOrder, const TagContext(body: {'total': 5})),
        );
        await settle();

        expect(write.error, isA<HttpStatusError>());
        expect(await h.stored(), isEmpty);
        expect(h.offline.currentFailures, isEmpty);
      },
    );

    test(
      'a cancelled write is rethrown to its caller and nothing is stored',
      () {
        fakeAsync((async) {
          final h = Harness.createIn(async, online: true);
          final cancel = Completer<void>();
          h.network.respond = (r) async {
            await cancel.future;
            throw http.RequestAbortedException(
              Uri.parse('https://api.test/orders'),
            );
          };

          final write = Watched(
            h.cache.mutate(
              opCreateOrder,
              const TagContext(body: {'total': 5}),
              options: MutateOptions(cancel: cancel.future),
            ),
          );
          async.flushMicrotasks();
          cancel.complete();
          async.flushMicrotasks();

          expect(write.error, isA<http.RequestAbortedException>());
          expect(readIn(async, h.stored()), isEmpty);
          expect(h.offline.pending, isEmpty);
          expect(h.offline.currentFailures, isEmpty);

          async.elapse(const Duration(minutes: 10));
          expect(h.writes, hasLength(1));
        });
      },
    );

    // The server stored the 409 under the first key and would replay it, so
    // the retry is a new operation (see offline_client_seams_test I1).
    test('retry after a 409 resends under a new key', () async {
      final h = await Harness.create();
      await h.seed('7');
      unawaited(
        h.update('7', {'note': 'queued'}).catchError((Object _) => null),
      );
      await settle();
      h.network.respond = (r) => r.meta.method == 'GET'
          ? echo(r)
          : throw const HttpStatusError(409, null);
      h.goOnline();
      await settle();

      h.network.respond = echo;
      await h.offline.currentFailures.single.retry();
      await settle();

      expect(h.writes, hasLength(2));
      expect(
        h.writes[1].headers['Idempotency-Key'],
        isNot(h.writes[0].headers['Idempotency-Key']),
      );
      expect(h.offline.currentFailures, isEmpty);
      expect(await h.stored(), isEmpty);
      expect(h.order('7')!['note'], 'queued');
    });

    test('discard drops the write and unblocks the lane', () async {
      final h = await Harness.create();
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      final behind = Watched(
        h.write(opUpdateOrder, orderArgs('7', {'note': 'y'})),
      );
      await settle();
      h.network.respond = (r) =>
          noteOf(r) == 'x' ? throw const HttpStatusError(409, null) : echo(r);
      h.goOnline();
      await settle();
      expect(h.writes, hasLength(1));

      await h.offline.currentFailures.single.discard();
      await settle();

      expect(h.writes, hasLength(2));
      expect(behind.done, isTrue);
      expect(await h.stored(), isEmpty);
    });

    test('edit resends new arguments under a new id and a new key', () async {
      final h = await Harness.create();
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      final originalId = (await h.stored()).single.id;
      h.network.respond = (r) =>
          noteOf(r) == 'x' ? throw const HttpStatusError(422, null) : echo(r);
      h.goOnline();
      await settle();

      await h.offline.currentFailures.single.edit(
        orderArgs('7', {'note': 'fixed'}),
      );
      await settle();

      expect(h.writes, hasLength(2));
      expect(h.writes[1].args.body, {'note': 'fixed'});
      expect(
        h.writes[1].headers['Idempotency-Key'],
        isNot(h.writes[0].headers['Idempotency-Key']),
      );
      expect(await h.stored(), isEmpty);
      expect(h.offline.currentFailures, isEmpty);
      expect(originalId, isNotEmpty);
    });

    test('an uncertain non-idempotent write is reported and never retried', () {
      fakeAsync((async) {
        final h = Harness.createIn(async);
        final write = Watched(
          h.write(opCreateOrder, const TagContext(body: {'total': 5})),
        );
        async.flushMicrotasks();

        h.network.respond = (_) => throw TimeoutException('no response');
        h.goOnline();
        async.flushMicrotasks();

        expect(write.error, isA<OutboxUncertain>());

        async.elapse(const Duration(minutes: 10));
        expect(h.writes, hasLength(1));
      });
    });

    test('an uncertain idempotent write is retried with the same key', () {
      fakeAsync((async) {
        final h = Harness.createIn(async);
        final write = Watched(
          h.write(
            opCreateOrderIdempotent,
            const TagContext(body: {'total': 5}),
          ),
        );
        async.flushMicrotasks();

        var calls = 0;
        h.network.respond = (r) =>
            ++calls == 1 ? throw TimeoutException('no response') : echo(r);
        h.goOnline();
        async.flushMicrotasks();
        async.elapse(const Duration(seconds: 1));

        expect(h.writes, hasLength(2));
        expect(
          h.writes[1].headers['Idempotency-Key'],
          h.writes[0].headers['Idempotency-Key'],
        );
        expect(write.done, isTrue);
        expect(write.error, isNull);
      });
    });

    test('an uncertain PUT or DELETE is retried', () {
      for (final meta in [opReplaceOrder, opDeleteOrder]) {
        fakeAsync((async) {
          final h = Harness.createIn(async);
          unawaited(h.write(meta, orderArgs('7', {'note': 'x'})));
          async.flushMicrotasks();

          var calls = 0;
          h.network.respond = (r) => ++calls == 1
              ? throw const SocketException('Connection reset by peer')
              : echo(r);
          h.goOnline();
          async.flushMicrotasks();
          async.elapse(const Duration(seconds: 1));

          expect(h.writes, hasLength(2), reason: meta.method);
          expect(h.offline.currentFailures, isEmpty, reason: meta.method);
        });
      }
    });

    test('a first attempt that times out on a non-idempotent write fails as uncertain at once', () async {
      final h = await Harness.create(online: true);
      h.network.respond = (_) => throw TimeoutException('no response');

      final write = Watched(
        h.write(opCreateOrder, const TagContext(body: {'total': 5})),
      );
      await settle();

      expect(write.error, isA<OutboxUncertain>());
      final stored = await h.stored();
      expect(
        stored.single.stateJson,
        contains('"kind":"failed","failure":{"kind":"uncertain"'),
      );
    });

    test('failures stored by an earlier run come back on restore', () async {
      final storage = memoryStorage();
      final first = await Harness.create(storage: storage);
      unawaited(
        first
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      first.network.respond = (_) => throw const HttpStatusError(409, null);
      first.goOnline();
      await settle();
      await first.offline.dispose();
      await first.cache.dispose();

      final second = await Harness.create(storage: storage);

      expect(second.offline.currentFailures.single, isA<OutboxConflict>());
      await second.offline.currentFailures.single.discard();
      expect(await second.stored(), isEmpty);
    });
  });

  group('writes that cannot be stored', () {
    test(
      'arguments that do not encode are refused with OutboxUnavailable',
      () async {
        for (final online in [false, true]) {
          final h = await Harness.create(online: online);

          final write = Watched(
            h.write(
              opCreateOrder,
              TagContext(body: {'when': DateTime.utc(2026)}),
            ),
          );
          await settle();

          expect(write.error, isA<OutboxUnavailable>(), reason: '$online');
          expect(
            (write.error! as OutboxUnavailable).cause,
            isA<JsonUnsupportedObjectError>(),
          );
          expect(h.writes, isEmpty, reason: '$online');
          expect(await h.stored(), isEmpty, reason: '$online');
          expect(h.offline.pending, isEmpty, reason: '$online');

          // The queue carries on.
          final next = Watched(
            h.write(opCreateOrder, const TagContext(body: {'total': 5})),
          );
          await settle();
          if (online) {
            expect(next.done && next.error == null, isTrue);
          } else {
            expect(await h.stored(), hasLength(1));
          }
        }
      },
    );

    test('an overlay patch that does not encode is refused', () async {
      final h = await Harness.create(
        overlayIntent: (meta, args) =>
            const MergeOverlay('Order:7', {'at': Object()}),
      );

      final write = Watched(h.write(opUpdateOrder, orderArgs('7', {'n': 1})));
      await settle();

      expect(write.error, isA<OutboxUnavailable>());
      expect(await h.stored(), isEmpty);
    });

    test(
      'an edit whose arguments do not encode throws and keeps the failure',
      () async {
        final h = await Harness.create();
        unawaited(
          h
              .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
              .catchError((Object _) => null),
        );
        await settle();
        h.network.respond = failingWith([const HttpStatusError(422, null)]);
        h.goOnline();
        await settle();
        final before = await h.stored();

        await expectLater(
          h.offline.currentFailures.single.edit(
            const TagContext(path: {'id': '7'}, body: {'at': Object()}),
          ),
          throwsA(isA<OutboxUnavailable>()),
        );

        expect(h.offline.currentFailures, hasLength(1));
        expect(await h.stored(), before);
      },
    );
  });

  group('binary bodies', () {
    test(
      'a binary write queued offline replays its bytes through RestTransport',
      () async {
        final storage = memoryStorage();
        final first = await Harness.create(storage: storage);
        final bytes = Uint8List.fromList([0x00, 0xff, 0x80, 0x0a, 0x0d, 0x22]);
        unawaited(
          first
              .write(
                opUploadScan,
                TagContext(path: const {'id': '7'}, body: bytes),
              )
              .catchError((Object _) => null),
        );
        await settle();
        final record = (await first.stored()).single;
        expect(record.argsJson, contains('bodyBase64'));
        await first.offline.dispose();
        await first.cache.dispose();

        final sent = <http.Request>[];
        final rest = RestTransport(
          baseUrl: Uri.parse('https://api.test'),
          client: MockClient((request) async {
            sent.add(request);
            return http.Response('', 204);
          }),
        );
        final outbox = OutboxTransport(rest);
        final cache = QueryCache(
          transport: outbox,
          entities: entities,
          storage: storage,
        );
        final offline = OfflineClient(
          cache: cache,
          operations: operations,
          connectivity: FakeConnectivity(),
          transport: outbox,
        );
        await switchTo(cache, 'alice');
        await offline.restore();
        await settle();

        final request = sent.single;
        expect(request.method, 'PUT');
        expect(request.url, Uri.parse('https://api.test/orders/7/scan'));
        expect(request.bodyBytes, bytes);
        expect(request.headers['content-type'], 'application/octet-stream');
        expect(request.headers['idempotency-key'], record.idempotencyKey);
        expect(request.headers.containsKey(outboxReplayHeader), isFalse);
        expect(await storedOutbox(storage, 'alice'), isEmpty);

        await offline.dispose();
        await cache.dispose();
      },
    );
  });

  group('restarts and principals', () {
    test('the app killed after sending a non-idempotent write reports it as uncertain', () async {
      final storage = memoryStorage();
      await seedOutbox(storage, 'alice', [
        seededEntry(
          id: 'm1',
          meta: opCreateOrder,
          seq: 1,
          sentAt: DateTime.utc(2026, 10, 4, 12, 1),
        ),
      ]);

      final h = await Harness.create(storage: storage, online: true);
      await settle();

      expect(h.writes, isEmpty);
      expect(h.offline.currentFailures.single, isA<OutboxUncertain>());
      expect(
        (await h.stored()).single.stateJson,
        contains('"kind":"failed","failure":{"kind":"uncertain"'),
      );
    });

    test(
      'the app killed after sending a PUT replays it with the same key',
      () async {
        final storage = memoryStorage();
        await seedOutbox(storage, 'alice', [
          seededEntry(
            id: 'm1',
            meta: opReplaceOrder,
            seq: 1,
            sentAt: DateTime.utc(2026, 10, 4, 12, 1),
          ),
        ]);

        final h = await Harness.create(storage: storage, online: true);
        await settle();

        expect(h.writes.single.headers['Idempotency-Key'], 'key-m1');
        expect(await h.stored(), isEmpty);
      },
    );

    test('writes are replayed by sequence, not by createdAt', () async {
      final storage = memoryStorage();
      await seedOutbox(storage, 'alice', [
        seededEntry(
          id: 'later-clock',
          meta: opUpdateOrder,
          seq: 2,
          createdAt: DateTime.utc(2026, 1, 1),
        ),
        seededEntry(
          id: 'earlier-clock',
          meta: opUpdateOrder,
          seq: 1,
          createdAt: DateTime.utc(2026, 12, 31),
          args: const TagContext(path: {'id': '8'}, body: {'note': 'first'}),
        ),
      ]);

      final h = await Harness.create(storage: storage, online: true);
      await settle();

      expect(h.writes.map((r) => r.headers['Idempotency-Key']), [
        'key-earlier-clock',
        'key-later-clock',
      ]);
    });

    test(
      "switching principal never replays the old principal's writes",
      () async {
        final h = await Harness.create();
        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
        );
        await settle();
        final aliceKey = (await h.stored()).single.idempotencyKey;

        await switchTo(h.cache, 'bob');
        await h.offline.restore();
        h.goOnline();
        await settle();

        expect(h.writes, isEmpty);
        expect(write.error, isA<OutboxSuspended>());
        expect(await h.stored('bob'), isEmpty);
        expect(await h.stored('alice'), hasLength(1));
        expect(h.offline.pending, isEmpty);
        expect(h.offline.pendingCountNow, 0);

        await switchTo(h.cache, 'alice');
        await h.offline.restore();
        await settle();

        expect(h.writes.single.headers['Idempotency-Key'], aliceKey);
        expect(await h.stored('alice'), isEmpty);
      },
    );

    test('with no session nothing is tracked', () async {
      final h = await Harness.create(online: true);
      await switchTo(h.cache, null);

      await h.write(opUpdateOrder, orderArgs('7', {'note': 'x'}));

      expect(h.writes, hasLength(1));
      expect(h.offline.pending, isEmpty);
    });

    test("a write made while the next principal's session opens waits for it and is queued there", () async {
      final h = await Harness.create();

      h.cache.setPrincipal('bob');
      expect(h.cache.session, isNull);
      final write = Watched(
        h.write(opUpdateOrder, orderArgs('7', {'note': 'bob'})),
      );
      await h.cache.idle;
      await settle();

      expect(write.error, isNull);
      expect(write.done, isFalse);
      expect(h.writes, isEmpty);
      expect(await h.stored('bob'), hasLength(1));
      expect(await h.stored('alice'), isEmpty);
      expect(h.offline.pending.single.args.body, {'note': 'bob'});

      h.goOnline();
      await settle();

      expect(h.writes, hasLength(1));
      expect(write.done, isTrue);
      expect(write.error, isNull);
    });

    test(
      "nothing told of a principal change sees the previous principal's queue",
      () async {
        final storage = memoryStorage();
        await seedOutbox(storage, 'alice', [
          seededEntry(id: 'm1', meta: opUpdateOrder, seq: 1),
          seededEntry(
            id: 'm2',
            meta: opUpdateCustomer,
            seq: 2,
          ).copyWith(failureJson: '{"kind":"conflict","status":409}'),
        ]);
        final h = await Harness.create(storage: storage);
        await h.seed('7');
        expect(h.offline.pending, hasLength(2));
        expect(h.offline.currentFailures, hasLength(1));

        final seen = <String, (int, int, int)>{};
        void look(String who) => seen[who] = (
          h.offline.pending.length,
          h.offline.currentFailures.length,
          h.offline.pendingCountNow,
        );

        h.cache.watchPrincipalChanging((_) => look('changing'));
        final unsubscribe = h.cache.subscribe(
          opGetOrder,
          orderArgs('7'),
          () => look('watcher'),
        );
        h.cache.watchPrincipal((_) => look('principal'));
        final principals = h.cache.principalChanges.listen(
          (_) => look('principalChanges'),
        );
        final sessions = h.cache.sessionChanges.listen((_) => look('session'));

        await switchTo(h.cache, 'bob');

        expect(
          seen.keys,
          containsAll(<String>['changing', 'watcher', 'principal']),
        );
        for (final MapEntry(:key, :value) in seen.entries) {
          expect(value, (0, 0, 0), reason: key);
        }

        unsubscribe();
        await principals.cancel();
        await sessions.cancel();
      },
    );
  });

  group('in flight across a principal change', () {
    test("a replay that fails after the switch never reaches the next principal's failures", () async {
      // The previous principal's session stays writable, so the failure is
      // recorded there and only the epoch keeps it out of memory.
      final storage = ScriptedStorage(memoryStorage())..keepOpen = true;
      final h = await Harness.create(storage: storage);
      final failures = <OutboxFailure>[];
      h.offline.failures.listen(failures.add);
      final write = Watched(
        h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
      );
      await settle();

      final held = Completer<Object?>();
      h.network.respond = (_) => held.future;
      h.goOnline();
      await settle();
      expect(h.writes, hasLength(1));

      await switchTo(h.cache, 'bob');
      held.completeError(const HttpStatusError(409, {'secret': 'alice'}));
      await settle();

      expect(failures, isEmpty);
      expect(h.offline.currentFailures, isEmpty);
      expect(h.offline.pending, isEmpty);
      expect(h.offline.pendingCountNow, 0);
      expect(h.events.whereType<OutboxFailed>(), isEmpty);
      expect(write.error, isA<OutboxSuspended>());
      expect(await h.stored('bob'), isEmpty);
      final alice = await h.stored('alice');
      expect(alice.single.stateJson, contains('"kind":"conflict"'));
    });

    test('a replay whose session closed under it still stays out of the next principal', () async {
      final h = await Harness.create();
      final failures = <OutboxFailure>[];
      h.offline.failures.listen(failures.add);
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'alice'}))
            .catchError((Object _) => null),
      );
      await settle();

      final held = Completer<Object?>();
      h.network.respond = (_) => held.future;
      h.goOnline();
      await settle();

      await switchTo(h.cache, 'bob');
      held.completeError(const HttpStatusError(409, null));
      await settle();

      expect(failures, isEmpty);
      expect(h.offline.currentFailures, isEmpty);
      expect(await h.stored('alice'), hasLength(1));
    });

    test("a replay that succeeds after the switch leaves the next principal's queue alone", () async {
      final h = await Harness.create();
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})));
      await settle();

      final held = Completer<Object?>();
      h.network.respond = (_) => held.future;
      h.goOnline();
      await settle();

      await switchTo(h.cache, 'bob');
      await h.offline.restore();
      h.network.respond = (_) => Completer<Object?>().future;
      unawaited(h.write(opUpdateOrder, orderArgs('8', {'note': 'bob'})));
      await settle();
      h.connectivity.set(false);
      unawaited(h.write(opUpdateOrder, orderArgs('9', {'note': 'bob'})));
      await settle();
      final before = h.offline.pending.map((e) => e.id).toList();

      held.complete(<String, Object?>{'id': '7', 'note': 'alice'});
      await settle();

      expect(h.offline.pending.map((e) => e.id), before);
      expect(h.events.whereType<OutboxReplayed>(), isEmpty);
    });
  });

  group('fix round 1', () {
    test(
      "retry sends at once during another write's backoff and keeps its level",
      () {
        fakeAsync((async) {
          final h = Harness.createIn(async);
          unawaited(
            h
                .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
                .catchError((Object _) => null),
          );
          unawaited(
            h
                .write(
                  opUpdateCustomer,
                  const TagContext(path: {'id': '1'}, body: {'name': 'y'}),
                )
                .catchError((Object _) => null),
          );
          async.flushMicrotasks();

          h.network.respond = (r) => noteOf(r) == 'x'
              ? throw const HttpStatusError(409, null)
              : throw const HttpStatusError(503, null);
          h.goOnline();
          async.flushMicrotasks();
          // x failed for good; the customer write backs off (1 s, then 2 s).
          expect(h.writes.map((r) => r.meta.id), [
            'op_update_order',
            'op_update_customer',
          ]);

          h.network.respond = (_) => throw const HttpStatusError(503, null);
          unawaited(h.offline.currentFailures.single.retry());
          async.flushMicrotasks();
          expect(h.writes, hasLength(3));
          expect(h.writes[2].meta.id, 'op_update_order');

          // The repeat failure re-arms at the level reached, 2 s, not 1 s.
          async.elapse(const Duration(milliseconds: 1999));
          expect(h.writes, hasLength(3));
          async.elapse(const Duration(milliseconds: 1));
          expect(h.writes, hasLength(4));
        });
      },
    );

    for (final (random, before, at) in [
      (0.0, 799, 800),
      (0.9999, 1199, 1200),
    ]) {
      test('the backoff is jittered by up to 20 percent (random $random)', () {
        fakeAsync((async) {
          final h = Harness.createIn(async, random: () => random);
          unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})));
          async.flushMicrotasks();

          h.network.respond = failingWith([const HttpStatusError(503, null)]);
          h.goOnline();
          async.flushMicrotasks();
          expect(h.writes, hasLength(1));

          async.elapse(Duration(milliseconds: before));
          expect(h.writes, hasLength(1));
          async.elapse(Duration(milliseconds: at - before));
          expect(h.writes, hasLength(2));
        });
      });
    }

    test('a write that keeps failing retryably is surfaced after 8 attempts and keeps its key', () {
      fakeAsync((async) {
        final h = Harness.createIn(async);
        final write = Watched(
          h.write(opCreateOrder, const TagContext(body: {'total': 5})),
        );
        async.flushMicrotasks();

        h.network.respond = (_) => throw const HttpStatusError(503, null);
        h.goOnline();
        async.flushMicrotasks();
        async.elapse(const Duration(hours: 1));

        expect(h.writes, hasLength(8));
        expect(h.offline.currentFailures.single, isA<OutboxUncertain>());
        expect(write.error, isA<OutboxUncertain>());
        final record = readIn(async, h.stored()).single;
        expect(record.stateJson, contains('"kind":"failed"'));
        expect(h.writes.map((r) => r.headers['Idempotency-Key']).toSet(), {
          record.idempotencyKey,
        });

        h.network.respond = echo;
        readIn(async, h.offline.currentFailures.single.retry());
        async.flushMicrotasks();

        expect(h.writes, hasLength(9));
        expect(h.writes.last.headers['Idempotency-Key'], record.idempotencyKey);
        expect(readIn(async, h.stored()), isEmpty);
      });
    });

    for (final action in ['retry', 'edit']) {
      test(
        '$action on a write a sync source owns never hands it to the source',
        () async {
          final storage = memoryStorage();
          await seedOutbox(storage, 'alice', [
            seededEntry(id: 'm1', meta: opUpdateOrder, seq: 1),
          ]);
          final source = FakeSource({'Order'});
          final h = await Harness.create(
            storage: storage,
            online: true,
            syncSources: [source],
          );
          await settle();
          final failure = h.offline.currentFailures.single;

          if (action == 'retry') {
            await failure.retry();
          } else {
            await failure.edit(orderArgs('7', {'note': 'edited'}));
          }
          await settle();

          expect(source.applied, isEmpty);
          expect(h.writes, isEmpty);
          expect(h.offline.currentFailures.single, isA<OutboxGone>());
          expect(h.offline.pending.single.id, 'm1');
          expect(
            (await h.stored()).single.stateJson,
            contains('"kind":"gone"'),
          );

          // The lane is not stuck: the write can still be discarded.
          await h.offline.currentFailures.single.discard();
          expect(h.offline.pending, isEmpty);
          expect(await h.stored(), isEmpty);
        },
      );
    }

    test(
      'a replay marker for an unknown write is refused, never sent',
      () async {
        final h = await Harness.create(online: true);

        final write = Watched(
          h.cache.mutate(
            opUpdateOrder,
            orderArgs('7', {'note': 'x'}),
            options: const MutateOptions(headers: {outboxReplayHeader: 'nope'}),
          ),
        );
        await settle();

        expect(write.error, isA<StateError>());
        expect(h.writes, isEmpty);
      },
    );

    test('a stored write for an entity a sync source now owns is surfaced, not handed to the source', () async {
      final storage = memoryStorage();
      await seedOutbox(storage, 'alice', [
        seededEntry(id: 'm1', meta: opUpdateOrder, seq: 1),
        seededEntry(
          id: 'm2',
          meta: opUpdateCustomer,
          seq: 2,
          args: const TagContext(path: {'id': '1'}, body: {'name': 'z'}),
        ),
      ]);
      final source = FakeSource({'Order'});

      final h = await Harness.create(
        storage: storage,
        online: true,
        syncSources: [source],
      );
      await settle();

      expect(source.applied, isEmpty);
      final failure = h.offline.currentFailures.single;
      expect(failure, isA<OutboxGone>());
      expect((failure as OutboxGone).status, 0);
      expect(h.writes.map((r) => r.meta.id), ['op_update_customer']);
    });
  });

  group('a write made for a principal who left', () {
    test(
      'a plain clear while a write waits for the restore does not refuse it',
      () async {
        final storage = ScriptedStorage(memoryStorage());
        final h = await Harness.create(storage: storage, online: true);
        await switchTo(h.cache, null);

        final reading = Completer<void>();
        final release = Completer<void>();
        storage.beforeReadOutbox = (principal) async {
          if (principal != 'alice' || reading.isCompleted) return;
          reading.complete();
          await release.future;
        };
        await switchTo(h.cache, 'alice');
        await reading.future;

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
        );
        await settle();
        expect(h.writes, isEmpty);

        h.cache.clear();
        release.complete();
        await settle();

        expect(write.error, isNull);
        expect(write.done, isTrue);
        expect(h.writes, hasLength(1));
        expect(h.network.writesSentAs, ['alice']);
      },
    );

    test("a write made while alice's session opens is refused after alice leaves and returns", () async {
      final h = await Harness.create(online: true);
      await switchTo(h.cache, null);

      h.cache.setPrincipal('alice');
      final write = Watched(
        h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
      );
      h.cache.setPrincipal(null);
      h.cache.setPrincipal('alice');
      await h.cache.idle;
      await settle();

      expect(write.error, isA<OutboxStale>());
      expect(h.writes, isEmpty);
      expect(await h.stored('alice'), isEmpty);
    });

    test(
      "a write made while alice's session opens never goes out as bob",
      () async {
        final h = await Harness.create(online: true, wireAuthPrincipal: true);
        await switchTo(h.cache, null);

        h.cache.setPrincipal('alice');
        expect(h.cache.session, isNull);
        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
        );
        // Alice signs out and bob signs in, in the documented order.
        h.cache.setPrincipal(null);
        h.cache.setPrincipal('bob');
        h.network.credentials = 'bob';
        await h.cache.idle;
        await settle();

        expect(h.writes, isEmpty);
        expect(write.error, isA<OutboxStale>());
        expect(await h.stored('bob'), isEmpty);
        expect(await h.stored('alice'), isEmpty);
      },
    );

    test(
      "a write made while alice's outbox is read never goes out as bob",
      () async {
        final storage = ScriptedStorage(memoryStorage());
        final h = await Harness.create(
          storage: storage,
          online: true,
          wireAuthPrincipal: true,
        );
        await switchTo(h.cache, null);

        final reading = Completer<void>();
        final release = Completer<void>();
        storage.beforeReadOutbox = (principal) async {
          if (principal != 'alice' || reading.isCompleted) return;
          reading.complete();
          await release.future;
        };
        await switchTo(h.cache, 'alice');
        await reading.future;

        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
        );
        await settle();
        expect(h.writes, isEmpty);

        await switchTo(h.cache, 'bob');
        h.network.credentials = 'bob';
        release.complete();
        await settle();

        expect(h.writes, isEmpty);
        expect(write.error, isA<OutboxStale>());
        expect(await h.stored('bob'), isEmpty);
      },
    );
  });

  group('credentials', () {
    test(
      'in the documented order no write is replayed under the next principal',
      () async {
        final h = await Harness.create();
        unawaited(
          h
              .write(opUpdateOrder, orderArgs('7', {'note': 'alice'}))
              .catchError((Object _) => null),
        );
        await settle();

        await switchTo(h.cache, 'bob');
        h.network.credentials = 'bob';
        h.goOnline();
        await settle();

        expect(h.writes, isEmpty);
        expect(await h.stored('alice'), hasLength(1));
      },
    );

    test(
      'with authPrincipal wired, credentials swapped first hold the replay',
      () {
        fakeAsync((async) {
          final h = Harness.createIn(async, wireAuthPrincipal: true);
          final write = Watched(
            h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
          );
          async.flushMicrotasks();

          h.network.credentials = 'bob';
          h.goOnline();
          async.flushMicrotasks();
          async.elapse(const Duration(seconds: 30));

          expect(h.writes, isEmpty);
          expect(
            readIn(async, h.stored()).single.stateJson,
            '{"kind":"queued"}',
          );
          expect(
            h.errors.map((e) => e.$1),
            contains(endsWith('outbox.principal')),
          );

          h.network.credentials = 'alice';
          async.elapse(const Duration(minutes: 5));

          expect(h.network.writesSentAs, ['alice']);
          expect(write.done, isTrue);
          expect(write.error, isNull);
        });
      },
    );

    test('with authPrincipal wired, credentials that change while the send is being recorded hold it', () async {
      final storage = ScriptedStorage(memoryStorage());
      final h = await Harness.create(storage: storage, wireAuthPrincipal: true);
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})));
      await settle();

      storage.beforeUpdateState = (_, state) {
        if (state.contains('"sending"')) h.network.credentials = 'bob';
      };
      h.goOnline();
      await settle();

      expect(h.writes, isEmpty);
      expect((await h.stored()).single.stateJson, '{"kind":"queued"}');
    });

    test('with authPrincipal wired, a direct write under the wrong credentials is stored, not sent', () async {
      final h = await Harness.create(online: true, wireAuthPrincipal: true);
      h.network.credentials = 'bob';

      final write = Watched(
        h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})),
      );
      await settle();

      expect(h.writes, isEmpty);
      expect(write.done, isFalse);
      expect(await h.stored(), hasLength(1));
    });

    test('known limitation: without authPrincipal, credentials swapped before setPrincipal can carry a due write', () async {
      final h = await Harness.create();
      unawaited(h.write(opUpdateOrder, orderArgs('7', {'note': 'alice'})));
      await settle();

      h.network.credentials = 'bob';
      h.goOnline();
      await settle();

      // Documented on OfflineClient: call setPrincipal first, or wire
      // authPrincipal. This pins what happens when an app does neither.
      expect(h.network.writesSentAs, ['bob']);
    });
  });
}
