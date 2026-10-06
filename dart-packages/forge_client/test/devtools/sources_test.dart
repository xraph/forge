import 'dart:async';
import 'dart:convert';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/devtools.dart';
import 'package:forge_client/src/devtools/seams.dart';
import 'package:forge_client/src/devtools/sources.dart';
import 'package:forge_client/src/devtools/types.dart';
import 'package:test/test.dart';

import 'harness.dart';

final class _Inspector implements OutboxInspector {
  final calls = <String>[];

  @override
  Future<void> replay(String mutationId) async =>
      calls.add('replay $mutationId');

  @override
  Future<void> discard(String mutationId) async =>
      calls.add('discard $mutationId');
}

/// Stands in for plan 04's `OutboxOffline`, which `forge_client` cannot import:
/// what matters here is that the devtools pass the inspector's error through.
final class OutboxOffline implements Exception {
  const OutboxOffline(this.mutationId);

  final String mutationId;

  @override
  String toString() =>
      'OutboxOffline: $mutationId stays queued until the server is reachable';
}

final class _OfflineInspector implements OutboxInspector {
  @override
  Future<void> replay(String mutationId) =>
      Future.error(OutboxOffline(mutationId));

  @override
  Future<void> discard(String mutationId) async {}
}

/// What `OfflineClient._inspected` does: it finds a write only among the
/// current principal's, and says so in a `StateError` otherwise.
final class _PrincipalInspector implements OutboxInspector {
  _PrincipalInspector(this.cache);

  final QueryCache cache;
  final _owned = <String, Set<String>>{};

  void queue(String id) => (_owned[cache.principal ?? ''] ??= {}).add(id);

  void _inspected(String mutationId) {
    if (!(_owned[cache.principal ?? '']?.contains(mutationId) ?? false)) {
      throw StateError('no stored write $mutationId for the current principal');
    }
  }

  @override
  Future<void> replay(String mutationId) async => _inspected(mutationId);

  @override
  Future<void> discard(String mutationId) async => _inspected(mutationId);
}

final class _NoTransport implements Transport {
  @override
  Future<Object?> execute(TransportRequest request) async => null;
}

final class _DescribedSource implements SyncSource, DevtoolsInspectable {
  @override
  Set<String> get entities => {'Doc', 'Comment'};

  @override
  Future<void> start(SyncContext context) async {}

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async =>
      const Applied(null);

  @override
  Stream<SyncStatus> status(String entity) => const Stream.empty();

  @override
  Future<void> stop() async {}

  @override
  Future<Map<String, Object?>> describeForDevtools() async => {
    'hlc': '0001:42:replica-a',
    'peers': 2,
  };
}

/// A source whose self-description waits on [gate], so a test can change the
/// principal while the devtools are waiting for it.
final class _SlowSource implements SyncSource, DevtoolsInspectable {
  _SlowSource(this.gate, {this.fail = false});

  final Future<void> gate;
  final bool fail;

  @override
  Set<String> get entities => {'Doc'};

  @override
  Future<void> start(SyncContext context) async {}

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async =>
      const Applied(null);

  @override
  Stream<SyncStatus> status(String entity) => const Stream.empty();

  @override
  Future<void> stop() async {}

  @override
  Future<Map<String, Object?>> describeForDevtools() async {
    await gate;
    if (fail) throw StateError('alice-ssn could not be described');
    return {'owner': 'alice-ssn'};
  }
}

/// A storage adapter whose sessions hold `readOutbox` open until [gate]
/// completes. With [replay] the answer is what the session held when the read
/// began, even if the session closed meanwhile; without it the read is done
/// after the wait, against a session that may be closed by then.
final class _GatedStorage implements StorageAdapter {
  _GatedStorage(this.gate, {this.replay = false});

  final Future<void> gate;
  final bool replay;
  final StorageAdapter inner = memoryStorage();

  @override
  Future<StorageSession> open(String principal) async =>
      _GatedSession(await inner.open(principal), gate, replay: replay);

  @override
  Future<void> destroy(String principal) => inner.destroy(principal);
}

final class _GatedSession implements StorageSession {
  _GatedSession(this.inner, this.gate, {required this.replay});

  final StorageSession inner;
  final Future<void> gate;
  final bool replay;

  @override
  String get principal => inner.principal;

  @override
  Future<List<PendingMutationRecord>> readOutbox() async {
    final held = replay ? await inner.readOutbox() : null;
    await gate;
    return held ?? await inner.readOutbox();
  }

  @override
  Future<Snapshot?> readSnapshot() => inner.readSnapshot();

  @override
  Future<void> writeSnapshot(Snapshot snapshot) =>
      inner.writeSnapshot(snapshot);

  @override
  Future<void> enqueue(PendingMutationRecord record) => inner.enqueue(record);

  @override
  Future<void> remove(String mutationId) => inner.remove(mutationId);

  @override
  Future<void> updateState(String mutationId, String stateJson) =>
      inner.updateState(mutationId, stateJson);

  @override
  KeyValueStore namespace(String name) => inner.namespace(name);

  @override
  Future<void> close() => inner.close();
}

PendingMutationRecord _record(
  String id, {
  String args = '{}',
  String? state = '{"kind":"queued"}',
}) => PendingMutationRecord(
  id: id,
  operationId: 'op_order_create',
  argsJson: args,
  idempotencyKey: 'key-$id',
  createdAt: DateTime.utc(2026, 10, 4),
  stateJson: state,
);

List<Map<String, Object?>> _rows(Map<String, Object?> result) =>
    (result['entries']! as List<Object?>).cast<Map<String, Object?>>();

/// A cache over [storage], signed in as [principal] with its session open.
Future<QueryCache> _signedIn(
  StorageAdapter storage,
  String principal, {
  List<SyncSource> sources = const [],
}) async {
  final cache = QueryCache(
    transport: _NoTransport(),
    entities: schema,
    storage: storage,
    syncSources: sources,
  )..setPrincipal(principal);
  await cache.idle;
  return cache;
}

void main() {
  group('the outbox, from its events', () {
    test(
      'mirrors enqueued, replayed and failed writes when no inspector is wired',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());
        final observe = h.cache.observer!;

        observe(debugOutboxEnqueued('m1', 'op_order_create'));
        observe(debugOutboxEnqueued('m2', 'op_order_create'));
        observe(debugOutboxReplayed('m1', 'op_order_create'));
        observe(
          debugOutboxFailed(
            'm2',
            'op_order_create',
            StateError('version mismatch'),
          ),
        );

        final outbox = await devtools.outbox();
        final entries = _rows(outbox);

        expect(outbox['wired'], isFalse);
        expect(outbox['source'], 'events');
        expect(
          [
            for (final e in entries)
              '${e['id']} ${e['state']} ${e['operation']}',
          ],
          ['m1 replayed op_order_create', 'm2 failed op_order_create'],
        );
        expect(entries[1]['failure'], contains('version mismatch'));
        expect(
          [for (final e in devtools.log().whereType<OutboxLog>()) e.phase],
          [
            OutboxPhase.enqueued,
            OutboxPhase.enqueued,
            OutboxPhase.replayed,
            OutboxPhase.failed,
          ],
        );

        devtools.dispose();
      },
    );

    test(
      'keeps a write that failed before the devtools saw it queued',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());

        h.cache.observer!(
          debugOutboxFailed('m9', 'op_order_archive', StateError('gone')),
        );

        final entries = _rows(await devtools.outbox());

        expect(entries.single['id'], 'm9');
        expect(entries.single['state'], 'failed');
        // The failure event names its operation, so even an unseen enqueue has one.
        expect(entries.single['operation'], 'op_order_archive');
        expect(entries.single['createdAt'], isNull);

        devtools.dispose();
      },
    );

    test('is bounded, dropping the oldest write first', () {
      final mirror = OutboxMirror(capacity: 2)
        ..enqueued('m1', 'op_order_create', 1)
        ..enqueued('m2', 'op_order_create', 2)
        ..enqueued('m3', 'op_order_create', 3);

      expect([for (final e in mirror.entries()) e.id], ['m2', 'm3']);
    });
  });

  group('the outbox, from the cache storage session', () {
    // The cache owns its per-principal StorageSession (contracts amendment),
    // so the panel lists queued writes without importing forge_client_offline.
    test('lists queued, sending and failed writes from the session state, never their arguments', () async {
      final cache = await _signedIn(memoryStorage(), 'user-1');
      final session = cache.session!;

      await session.enqueue(
        _record('m1', args: '{"body":{"password":"hunter2"}}'),
      );
      await session.enqueue(_record('m2'));
      await session.enqueue(_record('m3'));
      // The stored shape: kind and status, and the server's body, which the
      // panel must not show.
      await session.updateState(
        'm2',
        '{"kind":"failed","failure":{"kind":"conflict","status":409,"body":{"detail":"hunter3"}}}',
      );
      await session.updateState('m3', '{"kind":"sending","at":1759572000000}');

      final devtools = attach(cache, clock: CounterClock());
      final outbox = await devtools.outbox();
      final entries = _rows(outbox);

      expect(outbox['source'], 'session');
      // Listing needs no inspector; replaying and discarding do.
      expect(outbox['wired'], isFalse);
      expect(
        [for (final e in entries) '${e['id']} ${e['state']}'],
        ['m1 queued', 'm2 failed', 'm3 sending'],
      );
      expect(entries[1]['failure'], 'conflict 409');
      expect(entries[2]['since'], 1759572000000);
      expect(jsonEncode(outbox), isNot(contains('hunter2')));
      expect(jsonEncode(outbox), isNot(contains('hunter3')));
      expect(jsonEncode(outbox), isNot(contains('key-m1')));

      devtools.dispose();
      await cache.dispose();
    });

    test('reads a record with no stored state as queued', () async {
      final cache = await _signedIn(memoryStorage(), 'user-1');

      await cache.session!.enqueue(_record('m1', state: null));

      final devtools = attach(cache, clock: CounterClock());
      final entries = _rows(await devtools.outbox());

      expect(entries.single['state'], 'queued');
      expect(entries.single['failure'], isNull);

      devtools.dispose();
      await cache.dispose();
    });
  });

  group('reading a record state', () {
    test('derives queued, sending and failed, and names the failure kind', () {
      expect(outboxStateOf('{"kind":"queued"}'), (
        state: 'queued',
        failure: null,
        since: null,
      ));
      expect(outboxStateOf('{"kind":"sending","at":5}'), (
        state: 'sending',
        failure: null,
        since: 5,
      ));
      expect(
        outboxStateOf(
          '{"kind":"failed","failure":{"kind":"unauthorized","status":401}}',
        ),
        (state: 'failed', failure: 'unauthorized 401', since: null),
      );
      expect(
        outboxStateOf(
          '{"kind":"failed","failure":{"kind":"uncertain","reason":"the connection dropped"}}',
        ),
        (
          state: 'failed',
          failure: 'uncertain: the connection dropped',
          since: null,
        ),
      );
      expect(
        outboxStateOf(
          '{"kind":"failed","failure":{"kind":"gone","reason":"${'x' * 500}"}}',
        ).failure,
        'gone',
      );
      expect(
        outboxStateOf(
          '{"kind":"failed","failure":{"kind":"uncertain","reason":"${'x' * 500}"}}',
        ).failure!.length,
        lessThan(220),
      );
    });

    test('never shows a response body or a raw stored failure', () {
      expect(
        outboxStateOf(
          '{"kind":"failed","failure":{"kind":"validation","status":422,"body":{"email":"alice-ssn"}}}',
        ).failure,
        'validation 422',
      );
      // `OutboxEntry.stateJson` keeps an unreadable failure's raw text.
      expect(
        outboxStateOf(
          '{"kind":"failed","failure":{"kind":"unreadable","raw":"alice-ssn"}}',
        ).failure,
        'unreadable',
      );
      expect(
        outboxStateOf('{"kind":"failed","failure":"alice-ssn"}').failure,
        'unreadable',
      );
    });

    test(
      'reports a state it cannot read rather than throwing inside a panel call',
      () {
        expect(outboxStateOf('not json').state, 'unknown');
        expect(outboxStateOf('[1]').state, 'unknown');
        expect(outboxStateOf('{"kind":"teleporting"}').state, 'teleporting');
      },
    );
  });

  group('the outbox actions, through an inspector', () {
    test('passes on the offline failure of a replay, so the panel can say the write stays queued', () async {
      final h = Harness();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        outbox: _OfflineInspector(),
      );

      await expectLater(
        devtools.replayOutbox('m1'),
        throwsA(isA<OutboxOffline>()),
      );
      // The attempt is still on the record.
      expect(
        [for (final a in devtools.log().whereType<ActionLog>()) a.target],
        ['m1'],
      );

      devtools.dispose();
    });

    test('replays and discards through the inspector, logging each, and refuses without one', () async {
      final h = Harness();
      final inspector = _Inspector();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        outbox: inspector,
      );

      await devtools.replayOutbox('m1');
      await devtools.discardOutbox('m2');

      expect(inspector.calls, ['replay m1', 'discard m2']);
      expect(
        [
          for (final a in devtools.log().whereType<ActionLog>())
            '${a.action.name} ${a.target}',
        ],
        ['replay m1', 'discard m2'],
      );
      devtools.dispose();

      final bare = attach(h.cache, clock: CounterClock());
      await expectLater(bare.replayOutbox('m1'), throwsA(isA<StateError>()));
      await expectLater(bare.discardOutbox('m1'), throwsA(isA<StateError>()));
      bare.dispose();
    });

    test('a late registration can supply the inspector', () async {
      final h = Harness();
      final inspector = _Inspector();
      final devtools = attach(h.cache, clock: CounterClock());

      expect((await devtools.outbox())['wired'], isFalse);

      devtools.outboxInspector = inspector;

      expect((await devtools.outbox())['wired'], isTrue);
      await devtools.replayOutbox('m1');
      expect(inspector.calls, ['replay m1']);

      devtools.dispose();
    });

    test('refuses to replay or discard a write another principal queued, with the inspector error', () async {
      final cache = await _signedIn(memoryStorage(), 'alice');
      final inspector = _PrincipalInspector(cache)..queue('m-alice');
      final devtools = attach(cache, clock: CounterClock(), outbox: inspector);

      // Alice's own write is reachable.
      await devtools.replayOutbox('m-alice');

      cache.setPrincipal('bob');
      await cache.idle;

      await expectLater(
        devtools.replayOutbox('m-alice'),
        throwsA(
          isA<StateError>().having(
            (e) => e.message,
            'message',
            'no stored write m-alice for the current principal',
          ),
        ),
      );
      await expectLater(
        devtools.discardOutbox('m-alice'),
        throwsA(
          isA<StateError>().having(
            (e) => e.message,
            'message',
            contains('no stored write m-alice'),
          ),
        ),
      );

      // Only the attempts made in bob's session are on the record: alice's
      // was purged with her session.
      expect(
        [
          for (final a in devtools.log().whereType<ActionLog>())
            '${a.action.name} ${a.target}',
        ],
        ['replay m-alice', 'discard m-alice'],
      );
      expect(
        devtools.log().whereType<ActionLog>().every(
          (a) => a.session == devtools.session,
        ),
        isTrue,
      );

      devtools.dispose();
      await cache.dispose();
    });

    test('refuses while the cache is changing principal, before the inspector is asked', () async {
      final h = Harness();
      final inspector = _Inspector();
      final devtools = attach(
        h.cache,
        clock: CounterClock(),
        outbox: inspector,
      );
      Object? replayError;
      Object? discardError;

      // Runs after the devtools' own listener: inside the change window.
      h.cache.watchPrincipalChanging((_) {
        devtools
            .replayOutbox('m1')
            .then<void>(
              (_) {},
              onError: (Object e) {
                replayError = e;
              },
            );
        devtools
            .discardOutbox('m1')
            .then<void>(
              (_) {},
              onError: (Object e) {
                discardError = e;
              },
            );
      });
      h.cache.setPrincipal('bob');
      await h.settle();

      expect(replayError, isA<StateError>());
      expect(discardError, isA<StateError>());
      expect(inspector.calls, isEmpty);

      devtools.dispose();
    });
  });

  group('sync', () {
    test('reports status per entity from the events, and a source that describes itself', () async {
      final cache = QueryCache(
        transport: _NoTransport(),
        entities: schema,
        syncSources: [_DescribedSource()],
      );
      final devtools = attach(cache, clock: CounterClock());

      cache.observer!(debugSyncStatusChanged('Doc', const Pending(3)));
      cache.observer!(debugSyncStatusChanged('Comment', const Offline()));

      final sync = await devtools.sync();
      final source =
          ((sync['sources']! as List<Object?>).single! as Map<String, Object?>);
      final entities = (sync['entities']! as List<Object?>)
          .cast<Map<String, Object?>>();

      expect(source['entities'], ['Comment', 'Doc']);
      expect(source['detail'], {'hlc': '0001:42:replica-a', 'peers': 2});
      expect(
        [
          for (final e in entities)
            '${e['entity']} ${e['status']} ${e['pending']}',
        ],
        ['Comment offline 0', 'Doc pending 3'],
      );
      expect(devtools.log().whereType<SyncLog>(), hasLength(2));

      devtools.dispose();
      await cache.dispose();
    });

    test(
      'lists a source that does not describe itself with no detail',
      () async {
        final cache = QueryCache(
          transport: _NoTransport(),
          entities: schema,
          syncSources: [
            IdleSource({'Doc'}),
          ],
        );
        final devtools = attach(cache, clock: CounterClock());

        final sources = ((await devtools.sync())['sources']! as List<Object?>)
            .cast<Map<String, Object?>>();

        expect(sources.single['type'], 'IdleSource');
        expect(sources.single['detail'], isNull);

        devtools.dispose();
        await cache.dispose();
      },
    );

    test(
      'says there is nothing to show for a cache with no sync sources',
      () async {
        final h = Harness();
        final devtools = attach(h.cache, clock: CounterClock());

        expect(await devtools.sync(), {
          'sources': <Object?>[],
          'entities': <Object?>[],
        });

        devtools.dispose();
      },
    );
  });

  group('nothing crosses principals', () {
    test('a secret in a queued write and in a sync status is reachable nowhere once the next principal is in', () async {
      const secret = 'alice-ssn';
      final cache = await _signedIn(
        memoryStorage(),
        'alice',
        sources: [_DescribedSource()],
      );
      final inspector = _PrincipalInspector(cache)..queue('m-alice');
      final devtools = attach(cache, clock: CounterClock(), outbox: inspector);
      final observe = cache.observer!;

      await cache.session!.enqueue(
        _record('m-alice', args: '{"body":{"ssn":"$secret"}}'),
      );
      await cache.session!.enqueue(_record('m-failed'));
      await cache.session!.updateState(
        'm-failed',
        '{"kind":"failed","failure":{"kind":"conflict","status":409,"body":"$secret"}}',
      );
      observe(debugOutboxEnqueued('m-alice', 'op_order_create'));
      observe(
        debugOutboxFailed(
          'm-events',
          'op_order_create',
          StateError('rejected $secret'),
        ),
      );
      observe(debugOutboxReplayed('m-replayed', 'op_order_create'));
      observe(
        debugSyncStatusChanged('Doc', const SyncFailed('failed for $secret')),
      );

      Future<String> everything() async => jsonEncode([
        await devtools.outbox(),
        await devtools.sync(),
        [for (final entry in devtools.log()) entry.toJson()],
        [for (final entry in devtools.eventLog.entries()) entry.toJson()],
      ]);

      // The premise: it is all reachable for alice.
      final before = await everything();

      expect(before, contains(secret));
      expect(before, contains('m-alice'));
      expect(before, contains('m-events'));
      expect(before, contains('m-replayed'));
      expect(before, contains('m-failed'));
      expect(jsonEncode(await devtools.sync()), contains(secret));

      cache.setPrincipal('bob');
      await cache.idle;

      final after = await everything();

      expect(after, isNot(contains(secret)));
      expect(after, isNot(contains('alice')));
      for (final id in ['m-alice', 'm-events', 'm-replayed', 'm-failed']) {
        expect(after, isNot(contains(id)));
      }
      expect(_rows(await devtools.outbox()), isEmpty);
      expect((await devtools.sync())['entities'], isEmpty);

      // Bob's own writes show up, and only his.
      await cache.session!.enqueue(_record('m-bob'));

      expect(
        [for (final r in _rows(await devtools.outbox())) r['id']],
        ['m-bob'],
      );

      devtools.dispose();
      await cache.dispose();
    });

    test('answers empty, marked stale, while a change is pending', () async {
      final cache = await _signedIn(
        memoryStorage(),
        'alice',
        sources: [_DescribedSource()],
      );
      final devtools = attach(cache, clock: CounterClock());
      final answers = <Future<Map<String, Object?>>>[];

      await cache.session!.enqueue(_record('m-alice'));
      cache.observer!(debugOutboxEnqueued('m-alice', 'op_order_create'));
      cache.observer!(debugSyncStatusChanged('Doc', const Offline()));

      // Runs after the devtools' own listener: the cache still holds alice's
      // session and records, and the devtools must not read them.
      cache.watchPrincipalChanging((_) {
        expect(cache.session, isNotNull);
        answers
          ..add(devtools.outbox())
          ..add(devtools.sync());
      });
      cache.setPrincipal('bob');

      final results = await Future.wait(answers);

      expect(results[0], containsPair('stale', true));
      expect(results[0]['entries'], isEmpty);
      expect(results[1], containsPair('stale', true));
      expect(results[1]['sources'], isEmpty);
      expect(results[1]['entities'], isEmpty);
      expect(jsonEncode(results), isNot(contains('m-alice')));

      devtools.dispose();
      await cache.dispose();
    });

    for (final replay in [false, true]) {
      test(
        'drops a session read that finishes after the principal changed '
        '(${replay ? "the session answers from what it held" : "the session has closed"})',
        () async {
          final gate = Completer<void>();
          final cache = await _signedIn(
            _GatedStorage(gate.future, replay: replay),
            'alice',
          );
          final devtools = attach(cache, clock: CounterClock());

          await cache.session!.enqueue(_record('m-alice'));
          cache.observer!(debugOutboxReplayed('m-events', 'op_order_create'));

          final slow = devtools.outbox();

          cache.setPrincipal('bob');
          await cache.idle;
          gate.complete();

          final result = await slow;

          expect(result, containsPair('stale', true));
          expect(result['entries'], isEmpty);
          expect(jsonEncode(result), isNot(contains('m-alice')));
          expect(jsonEncode(result), isNot(contains('m-events')));

          // Nothing was cached around the read: bob sees his own and nothing
          // of alice's.
          final bob = await devtools.outbox();

          expect(bob, isNot(contains('stale')));
          expect(_rows(bob), isEmpty);

          devtools.dispose();
          await cache.dispose();
        },
      );
    }

    test(
      'drops a session read that finishes after the inspector was disposed',
      () async {
        final gate = Completer<void>();
        final cache = await _signedIn(_GatedStorage(gate.future), 'alice');
        final devtools = attach(cache, clock: CounterClock());

        await cache.session!.enqueue(_record('m-alice'));

        final slow = devtools.outbox();

        devtools.dispose();
        gate.complete();

        final result = await slow;

        expect(result['entries'], isEmpty);
        expect(jsonEncode(result), isNot(contains('m-alice')));

        await cache.dispose();
      },
    );

    test(
      'keeps an unmoved read, so the fence is not simply always on',
      () async {
        final gate = Completer<void>();
        final cache = await _signedIn(_GatedStorage(gate.future), 'alice');
        final devtools = attach(cache, clock: CounterClock());

        await cache.session!.enqueue(_record('m-alice'));

        final slow = devtools.outbox();

        gate.complete();

        final result = await slow;

        expect(result, isNot(contains('stale')));
        expect([for (final r in _rows(result)) r['id']], ['m-alice']);

        devtools.dispose();
        await cache.dispose();
      },
    );

    for (final fail in [false, true]) {
      test(
        'drops a source description that ${fail ? "fails" : "arrives"} after the principal changed',
        () async {
          final gate = Completer<void>();
          final cache = QueryCache(
            transport: _NoTransport(),
            entities: schema,
            syncSources: [_SlowSource(gate.future, fail: fail)],
          )..setPrincipal('alice');
          final devtools = attach(cache, clock: CounterClock());

          cache.observer!(debugSyncStatusChanged('Doc', const Pending(1)));

          final slow = devtools.sync();

          cache.setPrincipal('bob');
          gate.complete();

          final result = await slow;

          expect(result, containsPair('stale', true));
          expect(result['sources'], isEmpty);
          expect(result['entities'], isEmpty);
          expect(jsonEncode(result), isNot(contains('alice-ssn')));

          // Nothing was cached around it.
          final bob = await devtools.sync();

          expect(bob, isNot(contains('stale')));
          expect(bob['entities'], isEmpty);

          devtools.dispose();
          await cache.dispose();
        },
      );
    }

    test('keeps an unmoved source description', () async {
      final cache = QueryCache(
        transport: _NoTransport(),
        entities: schema,
        syncSources: [_SlowSource(Future.value())],
      )..setPrincipal('alice');
      final devtools = attach(cache, clock: CounterClock());

      final result = await devtools.sync();

      expect(result, isNot(contains('stale')));
      expect(
        ((result['sources']! as List<Object?>).single!
            as Map<String, Object?>)['detail'],
        {'owner': 'alice-ssn'},
      );

      devtools.dispose();
      await cache.dispose();
    });

    test('a disposed inspector answers empty and holds nothing', () async {
      final h = Harness();
      final devtools = attach(h.cache, clock: CounterClock());

      h.cache.observer!(debugOutboxEnqueued('m1', 'op_order_create'));
      h.cache.observer!(debugSyncStatusChanged('Doc', const Offline()));
      devtools.dispose();

      expect((await devtools.outbox())['entries'], isEmpty);
      expect((await devtools.sync())['entities'], isEmpty);
    });
  });
}
