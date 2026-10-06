/// Nothing crosses principals: after `setPrincipal(B)` none of A's replica
/// rows, pending changes, dataset membership or status reaches B's store,
/// watchers, credentials or server account, and nothing of B's reaches A's.
///
/// Every test holds the switch window open with [SlowStop], so the previous
/// principal's run is still alive (not stopped) while its context is fenced.
/// Leaks are checked synchronously after the switch and after every step,
/// never only after the cache settled: the cache sweeps owned rows once the
/// old sources stopped, which would hide a transient leak. The store's write
/// counter catches a row that was written and swept between two checks.
library;

import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:http/http.dart' as http;
import 'package:test/test.dart';

import 'support/fake_grove_http.dart';
import 'support/harness.dart';
import 'support/kit.dart';

const _alice = 'Bearer alice-token';
const _bob = 'Bearer bob-token';

/// Credentials that follow whoever the app signed in last, as a real app's
/// provider does.
final class _Tokens {
  var token = 'alice-token';
  var refreshes = 0;

  late final AuthProvider auth = AuthProvider.callbacks(
    credentials: (_) => {'authorization': 'Bearer $token'},
    refresh: () => refreshes++,
  );
}

/// One principal switch under test: the harness, the tokens, and what A's
/// run looked like before the switch.
final class _Switch {
  _Switch(this.h, this.tokens, this.slow);

  final Harness h;
  final _Tokens tokens;
  final SlowStop slow;
  late final String aliceNode;
  late final int version;
  late final int sentBefore;

  /// Moves to bob with the window held open.
  void toBob() {
    slow.hold = Completer<void>();
    tokens.token = 'bob-token';
    h.cache.setPrincipal('bob');
    version = h.cache.store.version;
    sentBefore = h.server.requests.length;
  }

  /// The privacy invariants, checked as often as a test likes.
  void check() {
    expect(h.noteKeys, isEmpty, reason: "no row of alice's in bob's store");
    expect(
      h.cache.store.version,
      version,
      reason: 'nothing written after the switch, not even for a moment',
    );

    for (final r in h.server.requests) {
      if (r.authorization == _bob) {
        expect(
          r.nodeId,
          isNot(aliceNode),
          reason: "bob's credentials, A's run",
        );
        expect(r.changes.where((c) => c.nodeId == aliceNode), isEmpty);
      }

      if (r.nodeId == aliceNode) expect(r.authorization, _alice);
    }

    for (final e in h.server.logs.entries) {
      if (e.key.startsWith('$_bob|')) {
        expect(
          e.value.where((c) => c.nodeId == aliceNode),
          isEmpty,
          reason: "alice's changes never reach bob's server account",
        );
      }
    }

    expect(tokens.refreshes, 0, reason: 'no refresh for a fenced context');
  }

  /// Steps time in small slices for [total], checking after each.
  void run(FakeAsync async, Duration total) {
    const slice = Duration(milliseconds: 50);

    for (var t = Duration.zero; t < total; t += slice) {
      async.flushMicrotasks();
      check();
      async.elapse(slice);
      check();
    }
  }
}

_Switch _signedIn(
  FakeAsync async, {
  List<SyncDeclaration> declarations = const [noteSync],
  StorageAdapter? storage,
}) {
  final tokens = _Tokens();
  final slow = SlowStop();
  final h = Harness(
    declarations: declarations,
    auth: tokens.auth,
    before: [slow],
    storage: storage,
  );
  final s = _Switch(h, tokens, slow);

  h.cache.setPrincipal('alice');
  async.flushMicrotasks();

  return s;
}

/// Holds the next request [matches] accepts; returns a function that reports
/// the held request (or null) and the completer that answers it.
(FakeRequest? Function(), Completer<http.Response?>) _hold(
  FakeGroveHttp server,
  bool Function(FakeRequest r) matches,
) {
  final held = Completer<http.Response?>();
  FakeRequest? seen;

  server.intercept = (r) {
    if (!matches(r)) return null;

    server.intercept = null;
    seen = r;

    return held.future;
  };

  return (() => seen, held);
}

void main() {
  group('a push held open across a principal switch', () {
    for (final (name, answer) in <(String, http.Response? Function())>[
      (
        'answered 503 (the transport retries)',
        () => http.Response('busy', 503, headers: fakeHeaders),
      ),
      (
        'answered 401 (the transport refreshes)',
        () => http.Response('{"message":"expired"}', 401, headers: fakeHeaders),
      ),
      ('answered 200 (the run carries on)', () => null),
    ]) {
      test(
        'sends nothing with bob\'s credentials and shows bob nothing, $name',
        () {
          fakeAsync((async) {
            final s = _signedIn(async);
            final h = s.h;

            s.aliceNode = h.server.requests.first.nodeId!;

            final (pushed, held) = _hold(h.server, (r) => r.isPush);

            unawaited(
              h.mutate(
                opUpdateNote,
                const TagContext(
                  path: {'noteId': 'n1'},
                  body: {'title': 'alice secret'},
                ),
              ),
            );
            async.elapse(const Duration(milliseconds: 1));
            expect(pushed()!.authorization, _alice);
            expect(h.record('n1')!['title'], 'alice secret');

            s.toBob();
            s.check();

            // Anything that would make A's run talk now is refused.
            unawaited(h.source.syncNow());
            s.check();

            held.complete(answer());
            s.run(async, const Duration(seconds: 5));

            // The window closes: A's run stops, bob's starts.
            s.slow.hold!.complete();
            s.run(async, const Duration(seconds: 2));

            final afterSwitch = h.server.requests.skip(s.sentBefore).toList();

            expect(afterSwitch, isNotEmpty, reason: 'bob syncs');

            for (final r in afterSwitch) {
              expect(r.authorization, _bob);
            }
          });
        },
      );
    }
  });

  test('a pull held open across a switch lands nothing in bob\'s store', () {
    fakeAsync((async) {
      final s = _signedIn(async);
      final h = s.h;

      s.aliceNode = h.server.requests.first.nodeId!;

      final (pulled, held) = _hold(h.server, (r) => r.isPull);

      unawaited(h.source.syncNow());
      async.flushMicrotasks();
      expect(pulled()!.authorization, _alice);

      s.toBob();
      s.check();
      held.complete(
        FakeGroveHttp.pullResponse([
          FakeGroveHttp.change('n7', 'title', "alice's row", 9),
        ]),
      );
      s.run(async, const Duration(seconds: 2));
      expect(h.source.records('Note'), isEmpty);
      s.slow.hold!.complete();
      s.run(async, const Duration(seconds: 1));
    });
  });

  group('a join in the switch window', () {
    test('made after the switch belongs to the next principal', () {
      fakeAsync((async) {
        final s = _signedIn(async, declarations: const [rowsSync]);
        final h = s.h;

        unawaited(h.source.join(const GroveDataset('a', table: 'ds_a')));
        async.flushMicrotasks();
        s.aliceNode = h.server.requests.first.nodeId!;

        s.toBob();
        unawaited(h.source.join(const GroveDataset('x', table: 'ds_x')));
        expect(h.source.joined, {'x'}, reason: 'waiting for bob, not alice');
        s.run(async, const Duration(seconds: 1));
        expect(
          h.server.paths.where((p) => p.startsWith('/d/x/')),
          isEmpty,
          reason: "alice's run never syncs bob's join",
        );

        s.slow.hold!.complete();
        s.run(async, const Duration(seconds: 1));
        expect(h.source.joined, {'x'}, reason: "alice's own join is gone");

        final x = [
          for (final r in h.server.requests)
            if (r.path.startsWith('/d/x/')) r,
        ];

        expect(x, isNotEmpty);

        for (final r in x) {
          expect(r.authorization, _bob);
          expect(r.nodeId, isNot(s.aliceNode));
        }

        // Nothing of x in alice's partition.
        Map<String, String>? aliceX;

        h.storage!
            .open('alice')
            .then((session) => session.namespace('grove/x').scan(''))
            .then((v) => aliceX = v);
        async.flushMicrotasks();
        expect(aliceX, isEmpty);
      });
    });

    test('made before the switch stays with the principal on the way out', () {
      fakeAsync((async) {
        final s = _signedIn(async, declarations: const [rowsSync]);
        final h = s.h;

        s.aliceNode = _nodeIdOf(async, h.source);

        // Starts under alice; its replica is still loading at the switch.
        unawaited(h.source.join(const GroveDataset('y', table: 'ds_y')));
        s.toBob();
        s.check();
        expect(h.source.joined, isEmpty);
        s.run(async, const Duration(seconds: 1));
        s.slow.hold!.complete();
        s.run(async, const Duration(seconds: 1));
        expect(h.source.joined, isEmpty, reason: 'bob never joined y');
        expect(
          h.server.paths.where((p) => p.startsWith('/d/y/')),
          isEmpty,
          reason: 'the join was cut off before it synced, and not carried over',
        );
      });
    });
  });

  test('public reads are empty during the window', () {
    fakeAsync((async) {
      final s = _signedIn(async, declarations: const [rowsSync]);
      final h = s.h;

      unawaited(h.source.join(const GroveDataset('a', table: 'ds_a')));
      async.flushMicrotasks();
      s.aliceNode = _nodeIdOf(async, h.source);
      h.server.rejectField = 'title';
      unawaited(
        h.mutate(
          opUpdateNote,
          TagContext(
            path: {'noteId': compositeId('a', 'r1')},
            body: const {'title': 'alice secret'},
          ),
        ),
      );
      async.elapse(const Duration(milliseconds: 1));

      // What alice sees.
      expect(h.source.records('Note'), hasLength(1));
      expect(h.source.rejected('Note'), hasLength(1));
      expect(h.source.replica('Note'), isNotNull);
      expect(h.source.joined, {'a'});

      final aliceStatus = <SyncStatus>[];

      h.source.statusOf('a').listen(aliceStatus.add);
      async.flushMicrotasks();
      expect(aliceStatus.last, isA<SyncFailed>());

      final eventsBefore = h.events.length;

      s.toBob();

      expect(h.source.records('Note'), isEmpty);
      expect(h.source.records('Note', datasetId: 'a'), isEmpty);
      expect(h.source.rejected('Note'), isEmpty);
      expect(h.source.replica('Note'), isNull);
      expect(h.source.replica('Note', 'a'), isNull);
      expect(h.source.joined, isEmpty);

      final statusA = <SyncStatus>[];
      final statusNote = <SyncStatus>[];

      h.source.statusOf('a').listen(statusA.add);
      h.source.status('Note').listen(statusNote.add);

      Map<String, Object?>? described;

      h.source.describeForDevtools().then((d) => described = d);
      async.flushMicrotasks();
      expect(statusA, [const Synced()]);
      expect(statusNote, [const Synced()]);
      expect(described!['nodeId'], isNull);
      expect(described!['peers'], isEmpty);

      final note =
          (described!['entities']! as Map<String, Object?>)['Note']!
              as Map<String, Object?>;

      expect(note['datasets'], isEmpty);
      expect(note['pending'], 0);

      // A's run keeps going in the window; none of its status is announced.
      unawaited(h.source.retryRejected('anything'));
      s.run(async, const Duration(seconds: 2));
      expect(aliceStatus.last, isA<SyncFailed>(), reason: 'no new A status');
      expect(h.events.length, eventsBefore, reason: 'no observer event');
      s.slow.hold!.complete();
      s.run(async, const Duration(seconds: 1));

      for (final e in h.events.skip(eventsBefore)) {
        expect(e.status, const Synced(), reason: "bob's own, empty status");
      }
    });
  });

  test(
    'a late pull after leave(erase) does not resurrect the namespace',
    () async {
      final h = Harness(declarations: const [rowsSync]);

      await h.signIn();
      await h.source.join(const GroveDataset('a', table: 'ds_a'));
      await h.mutate(
        opUpdateNote,
        TagContext(
          path: {'noteId': compositeId('a', 'r1')},
          body: const {'title': 'x'},
        ),
      );
      await pumpEventQueue(times: 50);

      final (pulled, held) = _hold(h.server, (r) => r.isPull);

      unawaited(h.source.syncNow());
      await pumpEventQueue();
      expect(pulled(), isNotNull);
      await h.source.leave('a', erase: true);
      held.complete(
        FakeGroveHttp.pullResponse([
          FakeGroveHttp.change('r2', 'title', 'late', 9, table: 'ds_a'),
        ]),
      );
      await pumpEventQueue(times: 50);
      // Past the replica's persistence debounce.
      await Future<void>.delayed(const Duration(milliseconds: 120));
      expect(await h.cache.session!.namespace('grove/a').scan(''), isEmpty);
      expect(h.noteKeys, isEmpty);
    },
  );

  test(
    'two principals on one storage never share a node id or a namespace',
    () async {
      final storage = memoryStorage();
      // One account per principal on the fake server, as a real server keeps.
      final tokens = _Tokens();
      final h = Harness(
        storage: storage,
        declarations: const [rowsSync],
        auth: tokens.auth,
      );

      Future<String> writeAs(String principal, String pk) async {
        tokens.token = '$principal-token';
        await h.signIn(principal);
        await h.source.join(const GroveDataset('a', table: 'ds_a'));
        await h.mutate(
          opUpdateNote,
          TagContext(
            path: {'noteId': compositeId('a', pk)},
            body: {'title': '$principal row'},
          ),
        );
        await pumpEventQueue(times: 50);

        return (await h.source.describeForDevtools())['nodeId']! as String;
      }

      final aliceNode = await writeAs('alice', 'r1');
      final bobNode = await writeAs('bob', 'r2');

      expect(bobNode, isNot(aliceNode));
      expect(h.record('a:r1'), isNull, reason: "bob's store holds only bob's");
      expect(h.record('a:r2'), isNotNull);

      // Stopping flushes bob's replica.
      h.cache.setPrincipal(null);
      await h.cache.idle;

      final alice = await storage.open('alice');
      final bob = await storage.open('bob');

      expect(await alice.namespace('grove').get('node'), aliceNode);
      expect(await bob.namespace('grove').get('node'), bobNode);

      final aliceDocs = (await alice.namespace('grove/a').scan('doc/')).keys;
      final bobDocs = (await bob.namespace('grove/a').scan('doc/')).keys;

      expect(aliceDocs.where((k) => k.contains('r1')), isNotEmpty);
      expect(aliceDocs.where((k) => k.contains('r2')), isEmpty);
      expect(bobDocs.where((k) => k.contains('r2')), isNotEmpty);
      expect(bobDocs.where((k) => k.contains('r1')), isEmpty);
    },
  );

  test(
    'a replica that cannot be read fails its dataset and never syncs',
    () async {
      final h = Harness(
        storage: _FailingStorage(memoryStorage(), 'grove/a'),
        declarations: const [rowsSync],
      );

      await h.signIn();
      await h.source.join(const GroveDataset('a', table: 'ds_a'));
      await h.source.join(const GroveDataset('b', table: 'ds_b'));
      await pumpEventQueue(times: 50);
      expect([
        for (final (e, _) in h.errors) e,
      ], contains(isA<ReplicaUnavailable>()));
      expect(
        await h.source.statusOf('a').first,
        isA<SyncFailed>().having(
          (f) => f.error,
          'error',
          isA<ReplicaUnavailable>(),
        ),
      );
      expect(h.server.paths.where((p) => p.startsWith('/d/a/')), isEmpty);
      expect(h.server.paths.where((p) => p.startsWith('/d/b/')), isNotEmpty);
      await expectLater(
        h.mutate(
          opUpdateNote,
          TagContext(
            path: {'noteId': compositeId('a', 'r1')},
            body: const {'title': 'x'},
          ),
        ),
        throwsA(isA<StateError>()),
      );
      expect(await h.source.statusOf('b').first, const Synced());
    },
  );
}

/// The running principal's node id, read the way devtools reads it.
String _nodeIdOf(FakeAsync async, GroveSyncSource source) {
  String? node;

  source.describeForDevtools().then((d) => node = d['nodeId'] as String?);
  async.flushMicrotasks();

  return node!;
}

/// A storage whose [failing] namespace cannot be read.
final class _FailingStorage implements StorageAdapter {
  _FailingStorage(this._inner, this.failing);

  final StorageAdapter _inner;
  final String failing;

  @override
  Future<StorageSession> open(String principal) async =>
      _FailingSession(await _inner.open(principal), failing);

  @override
  Future<void> destroy(String principal) => _inner.destroy(principal);
}

final class _FailingSession implements StorageSession {
  _FailingSession(this._inner, this._failing);

  final StorageSession _inner;
  final String _failing;

  @override
  String get principal => _inner.principal;

  @override
  KeyValueStore namespace(String name) =>
      name == _failing ? _Unreadable() : _inner.namespace(name);

  @override
  Future<void> close() => _inner.close();

  @override
  Future<void> enqueue(PendingMutationRecord record) => _inner.enqueue(record);

  @override
  Future<List<PendingMutationRecord>> readOutbox() => _inner.readOutbox();

  @override
  Future<Snapshot?> readSnapshot() => _inner.readSnapshot();

  @override
  Future<void> remove(String mutationId) => _inner.remove(mutationId);

  @override
  Future<void> updateState(String mutationId, String stateJson) =>
      _inner.updateState(mutationId, stateJson);

  @override
  Future<void> writeSnapshot(Snapshot snapshot) =>
      _inner.writeSnapshot(snapshot);
}

final class _Unreadable implements KeyValueStore {
  static Never _fail() => throw StateError('disk read failed');

  @override
  Future<String?> get(String key) async => _fail();

  @override
  Future<void> put(String key, String value) async => _fail();

  @override
  Future<void> delete(String key) async => _fail();

  @override
  Future<Map<String, String>> scan(String prefix) async => _fail();

  @override
  Future<void> batch(void Function(KeyValueBatch batch) build) async => _fail();
}
