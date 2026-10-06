@TestOn('vm')
library;

import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:fake_async/fake_async.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:forge_client_offline/src/storage/database_files_native.dart'
    show NativeDatabaseFiles, nativeDatabasePath, nativeStoreDirectory;

import 'support/counting_storage.dart';
import 'support/harness.dart';
import 'support/memory_secret_store.dart';

Future<Object?> numbered(TransportRequest request, int total) async =>
    <String, Object?>{
      'id': request.args.path['id'],
      'total': total,
      'note': null,
    };

/// Storage whose every open fails with [error], as a real adapter does when
/// the keystore is locked or the file is not ours.
final class _FailingStorage implements StorageAdapter {
  _FailingStorage(this.error);

  final Object error;

  @override
  Future<StorageSession> open(String principal) async => throw error;

  @override
  Future<void> destroy(String principal) async {}
}

/// A sync source that takes a while to stop, and says when it has.
final class _SlowSource implements SyncSource {
  _SlowSource(this.log);

  final List<String> log;

  @override
  Set<String> get entities => const {'Customer'};

  @override
  Future<void> start(SyncContext context) async {}

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async =>
      const Applied(null);

  @override
  Stream<SyncStatus> status(String entity) =>
      Stream<SyncStatus>.value(const Synced());

  @override
  Future<void> stop() async {
    await Future<void>.delayed(const Duration(milliseconds: 50));
    log.add('source stopped');
  }
}

/// A sync source that projects one owned row as it starts, the way a replica
/// source does on every launch, and says when it has.
final class _ProjectingSource implements SyncSource {
  _ProjectingSource(this.started);

  final Completer<void> started;

  @override
  Set<String> get entities => const {'Customer'};

  @override
  Future<void> start(SyncContext context) async {
    context.write(
      (store) => store.put('Customer:c1', <String, Object?>{'id': 'c1'}),
    );
    if (!started.isCompleted) started.complete();
  }

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async =>
      const Applied(null);

  @override
  Stream<SyncStatus> status(String entity) =>
      Stream<SyncStatus>.value(const Synced());

  @override
  Future<void> stop() async {}
}

/// A query whose response holds no entity, so it adds nothing to the store.
const opGetStats = OperationMeta(
  id: 'op_get_stats',
  method: 'GET',
  path: '/stats',
);

/// A client over [storage] built with the constructor, plus what it reported.
({QueryCache cache, OfflineClient offline, List<String> errors}) _built(
  StorageAdapter storage, {
  FakeTransport? network,
}) {
  final outbox = OutboxTransport(network ?? FakeTransport());
  final cache = QueryCache(
    transport: outbox,
    entities: entities,
    storage: storage,
  );
  final errors = <String>[];
  final offline = OfflineClient(
    cache: cache,
    operations: operations,
    connectivity: FakeConnectivity(),
    transport: outbox,
    onError: (_, context) => errors.add(context),
  );
  return (cache: cache, offline: offline, errors: errors);
}

Map<String, Object?>? _order(QueryCache cache, String id) =>
    cache.getState(opGetOrder, orderArgs(id)).dataOrNull
        as Map<String, Object?>?;

/// Seeds [principal]'s stored snapshot with order 7 at total 10.
Future<void> seedSnapshot(StorageAdapter storage, String principal) async {
  final source = QueryCache(transport: FakeTransport(), entities: entities)
    ..setPrincipal(principal);
  await source.fetch(opGetOrder, orderArgs('7'));
  final seeded = await storage.open(principal);
  await seeded.writeSnapshot(dehydrate(source, principal: principal));
  await seeded.close();
}

void main() {
  group('restore', () {
    test('serves the stored snapshot at once and marks it stale', () async {
      final storage = memoryStorage();
      await seedSnapshot(storage, 'alice');

      final h = await Harness.create(storage: storage);

      expect(h.order('7')!['total'], 10);
      expect(h.network.requests, isEmpty);

      h.network.respond = (r) => numbered(r, 99);
      final sub = h.cache.watch(opGetOrder, orderArgs('7')).listen((_) {});
      await settle();
      await sub.cancel();

      expect(
        h.network.requests.where((r) => r.meta.id == 'op_get_order'),
        hasLength(1),
      );
      expect(h.order('7')!['total'], 99);
    });

    test('runs once per session', () async {
      final storage = CountingStorage(memoryStorage());
      final h = await Harness.create(storage: storage);

      await h.offline.restore();
      await h.offline.restore();

      expect(storage.snapshotReads, 1);
    });

    test('completes at once with no session', () async {
      final h = await Harness.create();
      await switchTo(h.cache, null);

      await h.offline.restore().timeout(const Duration(milliseconds: 100));
    });

    test('called mid-switch, waits for the session and its outbox', () async {
      final storage = memoryStorage();
      await seedOutbox(storage, 'alice', [
        seededEntry(id: 'm1', meta: opUpdateOrder, seq: 1),
      ]);
      final outbox = OutboxTransport(FakeTransport());
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
        initiallyOnline: false,
      );

      cache.setPrincipal('alice');
      expect(cache.session, isNull, reason: 'a switch is in progress');
      await offline.restore();

      expect(offline.pending.map((e) => e.id), ['m1']);
      await offline.dispose();
      await cache.dispose();
    });

    test(
      'skips the snapshot when the cache already holds data, and says so',
      () async {
        final storage = CountingStorage(memoryStorage());
        await seedSnapshot(storage.inner, 'alice');
        final network = FakeTransport()..respond = (r) => numbered(r, 55);
        final outbox = OutboxTransport(network);
        final cache = QueryCache(
          transport: outbox,
          entities: entities,
          storage: storage,
        );
        final errors = <String>[];
        final offline = OfflineClient(
          cache: cache,
          operations: operations,
          connectivity: FakeConnectivity(),
          transport: outbox,
          onError: (_, context) => errors.add(context),
        );
        // Fresher data lands while the stored snapshot is being read.
        storage.beforeReadSnapshot = () async {
          await cache.fetch(opGetOrder, orderArgs('7'));
        };

        await switchTo(cache, 'alice');
        await offline.restore();

        final state = cache.getState(opGetOrder, orderArgs('7'));
        expect((state.dataOrNull! as Map<String, Object?>)['total'], 55);
        expect(errors, contains('forge_client_offline: snapshot.skipped'));
        await offline.dispose();
        await cache.dispose();
      },
    );

    test(
      'restores the snapshot when only a sync source wrote during the read',
      () async {
        final storage = CountingStorage(memoryStorage());
        await seedSnapshot(storage.inner, 'alice');
        final started = Completer<void>();
        // The read returns only after the source projected its row.
        storage.beforeReadSnapshot = () => started.future;
        final network = FakeTransport();

        final offline = await OfflineClient.open(
          transport: network,
          entities: entities,
          operations: operations,
          storage: storage,
          principal: 'alice',
          syncSources: [_ProjectingSource(started)],
          onError: (_, _) {},
        );

        expect(offline.cache.store.has('Customer:c1'), isTrue);
        expect(_order(offline.cache, '7')!['total'], 10);
        expect(network.requests, isEmpty);
        await offline.close();
      },
    );

    test(
      'skips the snapshot when an entity landed during the read with no query',
      () async {
        final storage = CountingStorage(memoryStorage());
        await seedSnapshot(storage.inner, 'alice');
        final c = _built(storage);
        var queriesThen = -1;
        storage.beforeReadSnapshot = () {
          // A frame or a mutation response: a record, and no query.
          c.cache.store.put('Order:9', <String, Object?>{
            'id': '9',
            'total': 1,
          });
          queriesThen = c.cache.queries.length;
        };

        await switchTo(c.cache, 'alice');
        await c.offline.restore();

        expect(queriesThen, 0, reason: 'only the store holds data');
        expect(_order(c.cache, '7'), isNull);
        expect(c.errors, contains('forge_client_offline: snapshot.skipped'));
        await c.offline.dispose();
        await c.cache.dispose();
      },
    );

    test(
      'skips the snapshot when a query settled during the read with no entity',
      () async {
        final storage = CountingStorage(memoryStorage());
        await seedSnapshot(storage.inner, 'alice');
        final network = FakeTransport()
          ..respond = (r) async => r.meta.id == opGetStats.id
              ? <String, Object?>{'count': 3}
              : echo(r);
        final c = _built(storage, network: network);
        var storeThen = -1;
        storage.beforeReadSnapshot = () async {
          await c.cache.fetch(opGetStats, TagContext.empty);
          storeThen = c.cache.store.size;
        };

        await switchTo(c.cache, 'alice');
        await c.offline.restore();

        expect(storeThen, 0, reason: 'only a query holds data');
        expect(_order(c.cache, '7'), isNull);
        expect(c.errors, contains('forge_client_offline: snapshot.skipped'));
        await c.offline.dispose();
        await c.cache.dispose();
      },
    );

    test(
      'a debounced write waits for the stored snapshot to be read',
      () async {
        final storage = CountingStorage(memoryStorage());
        await seedSnapshot(storage.inner, 'alice');
        fakeAsync((async) {
          storage.beforeReadSnapshot = () =>
              Future<void>.delayed(const Duration(seconds: 3));
          final c = _built(storage);
          c.cache.setPrincipal('alice');
          async.flushMicrotasks();

          // A commit one debounce period before the read returns.
          unawaited(c.cache.fetch(opGetOrder, orderArgs('8')));
          async.elapse(const Duration(milliseconds: 2500));
          expect(
            storage.snapshotWrites,
            0,
            reason: 'the read is still running',
          );

          async.elapse(const Duration(seconds: 2));
          expect(storage.snapshotWrites, 1, reason: 'owed, then written');
        });
      },
    );

    test(
      'a snapshot is never written before the stored one was read',
      () async {
        final storage = CountingStorage(memoryStorage());
        await seedSnapshot(storage.inner, 'alice');
        final outbox = OutboxTransport(FakeTransport());
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
        final reading = Completer<void>();
        final release = Completer<void>();
        storage.beforeReadSnapshot = () {
          reading.complete();
          return release.future;
        };

        cache.setPrincipal('alice');
        await reading.future;
        final flushed = offline.flush();
        await settle();
        release.complete();
        await flushed;

        final session = await storage.inner.open('alice');
        final stored = await session.readSnapshot();
        await session.close();
        expect(stored!.json['queries'], hasLength(1));
        await offline.dispose();
        await cache.dispose();
      },
    );
  });

  group('snapshots', () {
    test('writes are debounced', () {
      fakeAsync((async) {
        final storage = CountingStorage(memoryStorage());
        final h = Harness.createIn(async, storage: storage);
        var total = 0;
        h.network.respond = (r) => numbered(r, ++total);

        for (var i = 0; i < 5; i++) {
          unawaited(h.cache.refetch(opGetOrder, orderArgs('7')));
          async.elapse(const Duration(milliseconds: 100));
        }
        expect(storage.snapshotWrites, 0);

        async.elapse(const Duration(seconds: 1));
        expect(storage.snapshotWrites, 1);
      });
    });

    test(
      'a steady stream of commits still writes within five debounce periods',
      () {
        fakeAsync((async) {
          final storage = CountingStorage(memoryStorage());
          final h = Harness.createIn(async, storage: storage);
          var total = 0;
          h.network.respond = (r) => numbered(r, ++total);

          for (var i = 0; i < 9; i++) {
            unawaited(h.cache.refetch(opGetOrder, orderArgs('7')));
            async.elapse(const Duration(milliseconds: 600));
          }

          expect(storage.snapshotWrites, greaterThanOrEqualTo(1));
        });
      },
    );

    test('flush writes at once', () async {
      final storage = CountingStorage(memoryStorage());
      final h = await Harness.create(storage: storage);
      await h.seed('7');

      await h.offline.flush();

      expect(storage.snapshotWrites, greaterThanOrEqualTo(1));
      final session = await storage.inner.open('alice');
      expect(await session.readSnapshot(), isNotNull);
      await session.close();
    });

    test("a switch never writes one principal's cache into another's", () {
      fakeAsync((async) {
        final storage = CountingStorage(memoryStorage());
        final h = Harness.createIn(async, storage: storage);
        unawaited(h.seed('7'));
        async.flushMicrotasks();

        unawaited(switchTo(h.cache, 'bob'));
        async.flushMicrotasks();
        unawaited(h.seed('8'));
        async.elapse(const Duration(seconds: 10));

        Snapshot? stored(String principal) {
          Snapshot? read;
          unawaited(
            storage.inner.open(principal).then((s) async {
              read = await s.readSnapshot();
              await s.close();
            }),
          );
          async.flushMicrotasks();
          return read;
        }

        expect(stored('alice'), isNull, reason: 'nothing written for alice');
        final bob = stored('bob')!;
        expect((bob.json['records']! as Map<String, Object?>).keys, [
          'Order:8',
        ]);
        expect(bob.json['queries'], hasLength(1));
      });
    });
  });

  group('open, close and sign-out', () {
    test('open builds the cache, opens the principal and queues writes made offline', () async {
      final network = FakeTransport();
      final connectivity = FakeConnectivity();
      final storage = memoryStorage();

      final offline = await OfflineClient.open(
        transport: network,
        entities: entities,
        operations: operations,
        storage: storage,
        principal: 'alice',
        connectivity: connectivity,
      );
      expect(offline.cache.session!.principal, 'alice');

      connectivity.set(false);
      final write = Watched(
        offline.cache.mutate(opUpdateOrder, orderArgs('7', {'note': 'x'})),
      );
      await settle();
      expect(network.writes, isEmpty);
      expect(await storedOutbox(storage, 'alice'), hasLength(1));

      connectivity.set(true);
      await settle();
      expect(write.done, isTrue);
      expect(network.writes.single.headers['Idempotency-Key'], isNotEmpty);

      await offline.close();
    });

    test(
      'open without connectivity assumes online and queues on a network error',
      () async {
        final network = FakeTransport()
          ..respond = (_) => throw const SocketException('Connection refused');
        final storage = memoryStorage();
        final offline = await OfflineClient.open(
          transport: network,
          entities: entities,
          operations: operations,
          storage: storage,
          principal: 'alice',
        );

        unawaited(
          offline.cache
              .mutate(opUpdateOrder, orderArgs('7', {'note': 'x'}))
              .catchError((Object _) => null),
        );
        await settle();

        expect(network.writes, hasLength(1));
        expect(await storedOutbox(storage, 'alice'), hasLength(1));
        await offline.close();
      },
    );

    test(
      'close flushes the snapshot and closes the session, keeping the data',
      () async {
        final storage = CountingStorage(memoryStorage());
        final offline = await OfflineClient.open(
          transport: FakeTransport(),
          entities: entities,
          operations: operations,
          storage: storage,
          principal: 'alice',
        );
        await offline.cache.fetch(opGetOrder, orderArgs('7'));

        await offline.close();

        expect(storage.snapshotWrites, greaterThanOrEqualTo(1));
        expect(storage.closes, greaterThanOrEqualTo(1));
        final session = await storage.inner.open('alice');
        expect(await session.readSnapshot(), isNotNull);
        await session.close();
      },
    );

    test('signOut drops the principal and erases its storage', () async {
      final connectivity = FakeConnectivity();
      final storage = memoryStorage();
      final offline = await OfflineClient.open(
        transport: FakeTransport(),
        entities: entities,
        operations: operations,
        storage: storage,
        principal: 'alice',
        connectivity: connectivity,
      );
      connectivity.set(false);
      unawaited(
        offline.cache
            .mutate(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();

      await offline.signOut();

      expect(offline.cache.principal, isNull);
      expect(offline.cache.session, isNull);
      expect(await storedOutbox(storage, 'alice'), isEmpty);
      await offline.close();
    });

    test(
      'signOut without erase drops the principal and keeps its data',
      () async {
        final connectivity = FakeConnectivity();
        final storage = memoryStorage();
        final offline = await OfflineClient.open(
          transport: FakeTransport(),
          entities: entities,
          operations: operations,
          storage: storage,
          principal: 'alice',
          connectivity: connectivity,
        );
        connectivity.set(false);
        unawaited(
          offline.cache
              .mutate(opUpdateOrder, orderArgs('7', {'note': 'x'}))
              .catchError((Object _) => null),
        );
        await settle();

        await offline.signOut(erase: false);

        expect(offline.cache.principal, isNull);
        expect(offline.cache.session, isNull);
        expect(await storedOutbox(storage, 'alice'), hasLength(1));
        await offline.close();
      },
    );

    test(
      'signOut erases only after the sources stopped and the session closed',
      () async {
        final log = <String>[];
        final storage = CountingStorage(memoryStorage(), log: log);
        final offline = await OfflineClient.open(
          transport: FakeTransport(),
          entities: entities,
          operations: operations,
          storage: storage,
          principal: 'alice',
          syncSources: [_SlowSource(log)],
        );

        await offline.signOut();

        expect(log, ['source stopped', 'close alice', 'destroy alice']);
        await offline.close();
      },
    );

    test('signOut with erase on a client built with the constructor says what to do instead', () async {
      final h = await Harness.create();

      await expectLater(h.offline.signOut(), throwsStateError);
      expect(h.cache.principal, 'alice', reason: 'nothing was changed');
    });

    test(
      'signOut without erase works on a client built with the constructor',
      () async {
        final h = await Harness.create();

        await h.offline.signOut(erase: false);

        expect(h.cache.principal, isNull);
        expect(h.cache.session, isNull);
      },
    );

    test('open passes authPrincipal through to the client', () async {
      final network = FakeTransport();
      final storage = memoryStorage();
      final offline = await OfflineClient.open(
        transport: network,
        entities: entities,
        operations: operations,
        storage: storage,
        principal: 'alice',
        authPrincipal: () => 'mallory',
        onError: (_, _) {},
      );

      unawaited(
        offline.cache
            .mutate(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();

      expect(network.writes, isEmpty, reason: 'the credentials are not hers');
      expect(await storedOutbox(storage, 'alice'), hasLength(1));
      await offline.close();
    });

    test('open registers the client first, so an app changing-listener never sees the old queue', () async {
      final connectivity = FakeConnectivity();
      final offline = await OfflineClient.open(
        transport: FakeTransport(),
        entities: entities,
        operations: operations,
        storage: memoryStorage(),
        principal: 'alice',
        connectivity: connectivity,
      );
      connectivity.set(false);
      unawaited(
        offline.cache
            .mutate(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      expect(offline.pending, hasLength(1));

      final seen = <int>[];
      offline.cache.watchPrincipalChanging(
        (_) => seen.add(offline.pending.length),
      );
      offline.cache.setPrincipal('bob');

      expect(seen, [0]);
      await offline.cache.idle;
      await offline.close();
    });
  });

  group('open surfaces storage errors', () {
    final cases = <(String, Object)>[
      ('KeyUnavailable', const KeyUnavailable('alice', 'keystore locked')),
      ('WrongKey', const WrongKey()),
      (
        'UnsupportedSchemaVersion',
        const UnsupportedSchemaVersion(found: 9, supported: 1),
      ),
      ('EncryptionUnavailable', const EncryptionUnavailable()),
    ];

    for (final (name, error) in cases) {
      test('$name is rethrown at once, never as a timeout', () async {
        final reported = <(String, Object)>[];

        await expectLater(
          OfflineClient.open(
            transport: FakeTransport(),
            entities: entities,
            operations: operations,
            storage: _FailingStorage(error),
            principal: 'alice',
            onError: (e, context) => reported.add((context, e)),
          ).timeout(const Duration(seconds: 2)),
          throwsA(same(error)),
        );

        expect(reported, contains(('storage', error)));
      });
    }
  });

  group('storage resets and recovery', () {
    late Directory dir;
    late MemorySecretStore secrets;
    late KeystoreKeys keystore;

    setUp(() async {
      dir = await Directory.systemTemp.createTemp('forge_offline_client_');
      secrets = MemorySecretStore();
      keystore = keystoreKeys(store: secrets);
    });

    tearDown(() async {
      if (await dir.exists()) await dir.delete(recursive: true);
    });

    EncryptedSqliteStorage storageWith([KeyProvider? keys]) {
      final storage = EncryptedSqliteStorage(
        keys: keys ?? keystore,
        files: NativeDatabaseFiles(dir.path, labels: keystore),
        secrets: [keystore],
      );
      addTearDown(storage.resetOfflineData);
      return storage;
    }

    Future<void> queueFor(StorageAdapter storage, String principal) =>
        seedOutbox(storage, principal, [
          seededEntry(id: 'm1', meta: opUpdateOrder, seq: 1),
        ]);

    /// Loses alice's key from the keystore, as an OS restore can.
    Future<void> forgetKeys() async {
      final label = await keystore.principalLabel('alice');
      secrets.values.remove('forge_client_offline.key.$label');
    }

    test('a reset during open is reported and kept for the app', () async {
      final storage = storageWith();
      await queueFor(storage, 'alice');
      await forgetKeys();
      final reported = <(String, Object)>[];

      final offline = await OfflineClient.open(
        transport: FakeTransport(),
        entities: entities,
        operations: operations,
        storage: storage,
        principal: 'alice',
        connectivity: FakeConnectivity(),
        onError: (e, context) => reported.add((context, e)),
      );

      expect(offline.currentResets.single.principal, 'alice');
      expect(
        reported.where(
          (r) =>
              r.$1 == 'forge_client_offline: storage.reset' &&
              r.$2 is StorageReset,
        ),
        hasLength(1),
      );
      await offline.close();
    });

    test(
      'a reset after open reaches resets, and leaves with its principal',
      () async {
        final storage = storageWith();
        await queueFor(storage, 'alice');
        final offline = await OfflineClient.open(
          transport: FakeTransport(),
          entities: entities,
          operations: operations,
          storage: storage,
          principal: 'bob',
          connectivity: FakeConnectivity(),
          onError: (_, _) {},
        );
        final heard = <StorageReset>[];
        offline.resets.listen(heard.add);
        await forgetKeys();

        await switchTo(offline.cache, 'alice');
        await settle();

        expect(heard.single.principal, 'alice');
        expect(offline.currentResets.single.principal, 'alice');

        await switchTo(offline.cache, 'bob');
        expect(offline.currentResets, isEmpty);
        await offline.close();
      },
    );

    test(
      'a client built with the constructor surfaces storageResets',
      () async {
        final storage = storageWith();
        await queueFor(storage, 'alice');
        await forgetKeys();
        final outbox = OutboxTransport(FakeTransport());
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
          storageResets: storage.resets,
        );

        await switchTo(cache, 'alice');
        await offline.restore();

        expect(offline.currentResets.single.principal, 'alice');
        await offline.dispose();
        await cache.dispose();
      },
    );

    test(
      'resetOfflineData signs out and erases everything the package keeps',
      () async {
        final storage = storageWith();
        final connectivity = FakeConnectivity();
        final offline = await OfflineClient.open(
          transport: FakeTransport(),
          entities: entities,
          operations: operations,
          storage: storage,
          principal: 'alice',
          connectivity: connectivity,
        );
        connectivity.set(false);
        unawaited(
          offline.cache
              .mutate(opUpdateOrder, orderArgs('7', {'note': 'x'}))
              .catchError((Object _) => null),
        );
        await settle();

        await offline.resetOfflineData();

        expect(offline.cache.principal, isNull);
        expect(offline.cache.session, isNull);
        expect(secrets.values, isEmpty);
        final store = Directory(nativeStoreDirectory(dir.path));
        expect(
          store.existsSync() ? store.listSync() : const <FileSystemEntity>[],
          isEmpty,
        );
        await offline.close();
      },
    );

    test(
      'resetOfflineData needs the storage OfflineClient.open built',
      () async {
        final h = await Harness.create();

        await expectLater(h.offline.resetOfflineData(), throwsStateError);
        expect(h.cache.principal, 'alice');
      },
    );

    test(
      'an unreadable key fails open and leaves the database alone',
      () async {
        final storage = storageWith();
        await queueFor(storage, 'alice');
        final path = await nativeDatabasePath(dir.path, keystore, 'alice');
        secrets.failReadsWith = StateError('keystore locked');
        secrets.failReadsFor = (name) => name.contains('.key.');

        await expectLater(
          OfflineClient.open(
            transport: FakeTransport(),
            entities: entities,
            operations: operations,
            storage: storageWith(),
            principal: 'alice',
            onError: (_, _) {},
          ).timeout(const Duration(seconds: 2)),
          throwsA(isA<KeyUnavailable>()),
        );

        expect(File(path).existsSync(), isTrue, reason: 'never reset');
        secrets.failReadsWith = null;
        expect(await storedOutbox(storage, 'alice'), hasLength(1));
      },
    );
  });

  group('devtools inspector', () {
    test('the client is an OutboxInspector', () async {
      final h = await Harness.create();

      expect(h.offline, isA<OutboxInspector>());
    });

    test(
      'the client is an OutboxFailureSource whose failures are typed',
      () async {
        final h = await Harness.create();
        final OutboxFailureSource source = h.offline;
        final seen = <Object>[];
        source.failures.listen(seen.add);

        unawaited(
          h
              .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
              .catchError((Object _) => null),
        );
        await settle();
        h.network.respond = (_) => throw const HttpStatusError(409, null);
        h.goOnline();
        await settle();

        expect(seen.single, isA<OutboxConflict>());
      },
    );

    test(
      'replay sends a queued write now, ignoring the offline flag and backoff',
      () async {
        final h = await Harness.create();
        final write = Watched(
          h.write(opUpdateOrder, orderArgs('7', {'note': 'x'})),
        );
        await settle();
        final id = h.offline.pending.single.id;

        await h.offline.replay(id);
        await settle();

        expect(h.writes, hasLength(1));
        expect(write.done, isTrue);
        expect(await h.stored(), isEmpty);
      },
    );

    test('replay of a write that cannot reach the server fails typed and keeps it queued', () async {
      final h = await Harness.create();
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      final id = h.offline.pending.single.id;
      h.network.respond = (_) =>
          throw const SocketException('Connection refused');

      await expectLater(
        h.offline.replay(id),
        throwsA(
          isA<OutboxOffline>()
              .having((e) => e.cause, 'cause', OutboxOfflineCause.offline)
              .having((e) => e.status, 'status', isNull),
        ),
      );

      final stored = await h.stored();
      expect(stored.single.id, id);
      expect(stored.single.stateJson, '{"kind":"queued"}');
    });

    test(
      'replay resends a failed write and reports a new failure typed',
      () async {
        final h = await Harness.create();
        unawaited(
          h
              .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
              .catchError((Object _) => null),
        );
        await settle();
        h.network.respond = (_) => throw const HttpStatusError(409, null);
        h.goOnline();
        await settle();
        final id = h.offline.currentFailures.single.mutationId;

        await expectLater(h.offline.replay(id), throwsA(isA<OutboxConflict>()));

        h.network.respond = echo;
        await h.offline.replay(id);
        await settle();

        expect(h.offline.currentFailures, isEmpty);
        expect(await h.stored(), isEmpty);
      },
    );

    test('replay after a retryable status says so, with the status', () async {
      final h = await Harness.create();
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      final id = h.offline.pending.single.id;
      h.network.respond = (_) => throw const HttpStatusError(503, null);

      await expectLater(
        h.offline.replay(id),
        throwsA(
          isA<OutboxOffline>()
              .having(
                (e) => e.cause,
                'cause',
                OutboxOfflineCause.retryableStatus,
              )
              .having((e) => e.status, 'status', 503),
        ),
      );
      expect(await h.stored(), hasLength(1));
    });

    test('replay held for credentials of another principal says so', () async {
      final h = await Harness.create(wireAuthPrincipal: true);
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      final id = h.offline.pending.single.id;
      h.network.credentials = 'bob';

      await expectLater(
        h.offline.replay(id),
        throwsA(
          isA<OutboxOffline>().having(
            (e) => e.cause,
            'cause',
            OutboxOfflineCause.credentialsHeld,
          ),
        ),
      );
      expect(h.writes, isEmpty);
    });

    test('replay of an unknown write is a StateError', () async {
      final h = await Harness.create();

      await expectLater(h.offline.replay('nope'), throwsStateError);
    });

    test('replay waits for the attempt already on the wire instead of sending beside it', () async {
      final h = await Harness.create();
      for (final note in ['a', 'b']) {
        unawaited(
          h
              .write(opUpdateOrder, orderArgs('7', {'note': note}))
              .catchError((Object _) => null),
        );
      }
      await settle();
      final second = h.offline.pending[1].id;

      final gate = Completer<void>();
      var onTheWire = 0;
      var most = 0;
      h.network.respond = (r) async {
        onTheWire++;
        most = math.max(most, onTheWire);
        if (noteOf(r) == 'a') await gate.future;
        onTheWire--;
        return echo(r);
      };
      h.goOnline();
      await settle();
      expect(h.writes.map(noteOf), ['a'], reason: 'a is on the wire');

      final replayed = Watched(h.offline.replay(second));
      // Well past any fixed number of event-loop turns.
      for (var i = 0; i < 8; i++) {
        await settle();
      }
      expect(replayed.done, isFalse);
      expect(most, 1);

      gate.complete();
      await settle();

      expect(replayed.done, isTrue);
      expect(replayed.error, isNull);
      expect(most, 1);
      expect(h.writes.map(noteOf), ['a', 'b']);
    });

    test('discard removes a queued write and rolls back its overlay', () async {
      final h = await Harness.create();
      await h.seed('7');
      final write = Watched(h.update('7', {'note': 'queued'}));
      await settle();
      expect(h.order('7')!['note'], 'queued');

      await h.offline.discard(h.offline.pending.single.id);
      await settle();

      expect(write.error, isA<OutboxDiscarded>());
      expect(h.order('7')!['note'], isNull);
      expect(await h.stored(), isEmpty);
    });

    test('discard of an unknown write is a StateError', () async {
      final h = await Harness.create();

      await expectLater(h.offline.discard('nope'), throwsStateError);
    });

    test('after a switch, the inspector cannot see or touch the previous '
        "principal's writes", () async {
      final h = await Harness.create();
      unawaited(
        h
            .write(opUpdateOrder, orderArgs('7', {'note': 'x'}))
            .catchError((Object _) => null),
      );
      await settle();
      final id = h.offline.pending.single.id;

      await switchTo(h.cache, 'bob');
      await h.offline.restore();

      expect(h.offline.pending, isEmpty);
      await expectLater(h.offline.replay(id), throwsStateError);
      await expectLater(h.offline.discard(id), throwsStateError);
      expect(h.writes, isEmpty);
      expect(await h.stored('alice'), hasLength(1));
    });
  });
}
