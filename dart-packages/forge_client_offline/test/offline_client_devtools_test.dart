@TestOn('vm')
library;

import 'dart:async';
import 'dart:convert';
import 'dart:developer' as developer;

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/service_extensions.dart'
    show ForgeDevtoolsHost;
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/harness.dart';

/// A RestTransport over a recording fake server, so a request that reaches
/// the wire shows up in [wire].
({RestTransport rest, List<http.Request> wire}) restOverFakeServer() {
  final wire = <http.Request>[];
  final rest = RestTransport(
    baseUrl: Uri.parse('http://forge.test'),
    client: MockClient((request) async {
      wire.add(request);
      final body = request.method == 'GET'
          ? {'id': '7', 'total': 10, 'note': null}
          : {'id': '7', 'total': 10, ...?_noteOf(request)};
      return http.Response(
        jsonEncode(body),
        200,
        headers: {'content-type': 'application/json'},
      );
    }),
  );
  return (rest: rest, wire: wire);
}

Map<String, Object?>? _noteOf(http.Request request) => request.body.isEmpty
    ? null
    : (jsonDecode(request.body) as Map).cast<String, Object?>();

Iterable<http.Request> patches(List<http.Request> wire) =>
    wire.where((r) => r.method == 'PATCH');

const _secret = 'alice-ssn';

/// A sync source owning `Doc`. While alice is current it reports a failure
/// that names the secret through the cache's observer and describes itself
/// with it; for anyone else it reports nothing at all, so only a purge keeps
/// alice's status out of the devtools' sync mirror.
final class _AliceSource implements SyncSource, DevtoolsInspectable {
  _AliceSource(this.principal);

  final String? Function() principal;

  @override
  Set<String> get entities => {'Doc'};

  @override
  Future<void> start(SyncContext context) async {
    // A source reports through the cache's observer, as the grove one does.
    if (context.principal == 'alice') {
      context.cache.observer?.call(
        const SyncStatusChanged(
          entity: 'Doc',
          status: SyncFailed('failed for $_secret'),
        ),
      );
    }
  }

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async =>
      const Applied(null);

  @override
  Stream<SyncStatus> status(String entity) => const Stream.empty();

  @override
  Future<void> stop() async {}

  @override
  Future<Map<String, Object?>> describeForDevtools() async =>
      principal() == 'alice' ? {'owner': _secret} : <String, Object?>{};
}

void main() {
  // Every test goes through a recording host, so the real `ext.forge.*`
  // handlers can be called and the process-wide `dart:developer` registration
  // (which can happen once per isolate) is never touched.
  late Map<String, developer.ServiceExtensionHandler> handlers;

  setUp(() {
    handlers = {};
    ForgeDevtoolsHost.debugOverride(
      registrar: (method, handler) => handlers[method] = handler,
      poster: (_, _) {},
    );
  });

  tearDown(ForgeDevtoolsHost.debugReset);

  Future<String> read(
    String method, [
    Map<String, String> params = const {},
  ]) async {
    final response = await handlers[method]!(method, params);
    expect(
      response.errorCode,
      isNull,
      reason: '$method: ${response.errorDetail}',
    );
    return response.result!;
  }

  group('OfflineClient.open(devtools: true)', () {
    test(
      'attaches the devtools and makes the client the outbox inspector',
      () async {
        final (:rest, wire: _) = restOverFakeServer();
        final offline = await OfflineClient.open(
          transport: rest,
          entities: entities,
          operations: operations,
          storage: memoryStorage(),
          principal: 'alice',
          devtools: true,
        );
        addTearDown(offline.dispose);

        final attached = forgeDevtoolsFor(offline.cache);

        expect(attached, isNotNull);
        expect(attached!.outboxInspector, same(offline));
        expect(attached.controls, isNotNull);
        expect(attached.requestLog, isNotNull);
      },
    );

    test('attaches nothing unless asked, because open never goes through configureClient', () async {
      final (:rest, wire: _) = restOverFakeServer();
      final offline = await OfflineClient.open(
        transport: rest,
        entities: entities,
        operations: operations,
        storage: memoryStorage(),
        principal: 'alice',
      );
      addTearDown(offline.dispose);

      expect(forgeDevtoolsFor(offline.cache), isNull);
    });

    test(
      'a transport that is not a RestTransport is attached without a simulator',
      () async {
        final offline = await OfflineClient.open(
          transport: FakeTransport(),
          entities: entities,
          operations: operations,
          storage: memoryStorage(),
          principal: 'alice',
          devtools: true,
        );
        addTearDown(offline.dispose);

        final attached = forgeDevtoolsFor(offline.cache)!;

        expect(attached.controls, isNull);
        expect(attached.requestLog, isNull);
        expect(attached.outboxInspector, same(offline));
      },
    );

    // Plan 06 Task 7 ruling I1: a simulated offline must queue a PATCH, not
    // fail it as uncertain, and the replay must feel the simulated network.
    test('a PATCH made while the panel says offline is queued, and drains through the simulator when the network is back', () async {
      final (:rest, :wire) = restOverFakeServer();
      final offline = await OfflineClient.open(
        transport: rest,
        entities: entities,
        operations: operations,
        storage: memoryStorage(),
        principal: 'alice',
        devtools: true,
      );
      addTearDown(offline.dispose);
      final controls = forgeDevtoolsFor(offline.cache)!.controls!;

      await offline.cache.fetch(opGetOrder, orderArgs('7'));
      controls.mode = NetworkMode.offline;
      await settle();

      expect(offline.isOnline, isFalse);

      final future = offline.cache.mutate(
        opUpdateOrder,
        orderArgs('7', {'note': 'queued'}),
      );
      final write = Watched(future);
      await settle();

      // Queued, not failed: no uncertain failure, nothing on the wire.
      expect(write.done, isFalse);
      expect(offline.pending, hasLength(1));
      expect(offline.currentFailures, isEmpty);
      expect(patches(wire), isEmpty);

      // Back online, but slow: the replay is held in the simulator, so it
      // went through it rather than around it.
      controls.latency = const Duration(milliseconds: 200);
      controls.mode = NetworkMode.online;
      await settle();

      expect(offline.isOnline, isTrue);
      expect(patches(wire), isEmpty);
      expect(offline.pending, hasLength(1));

      final value = await future.timeout(const Duration(seconds: 5));

      expect((value! as Map<String, Object?>)['note'], 'queued');
      expect(patches(wire), hasLength(1));
      expect(offline.pending, isEmpty);
      expect(offline.currentFailures, isEmpty);
    });

    // Privacy through the production wiring (preflight P1, P2). A configureClient
    // cache has no OutboxInspector, so only this path puts the outbox and sync
    // mirrors behind the extensions.
    test('after setPrincipal nothing alice did, queued, replayed or synced, is readable from any ext.forge read', () async {
      final (:rest, wire: _) = restOverFakeServer();
      QueryCache? cacheRef;
      final offline = await OfflineClient.open(
        transport: rest,
        entities: {
          ...entities,
          'Doc': const EntityMeta(idField: 'id'),
        },
        operations: operations,
        storage: memoryStorage(),
        principal: 'alice',
        syncSources: [_AliceSource(() => cacheRef?.principal)],
        devtools: true,
      );
      addTearDown(offline.dispose);
      final cache = cacheRef = offline.cache;
      final controls = forgeDevtoolsFor(cache)!.controls!;

      // A read whose query value carries the secret, which the log and the
      // query list show. The PATCH body is shown by no read.
      const secretArgs = TagContext(
        path: {'id': '7'},
        query: {'owner': _secret},
      );
      await cache.fetch(opGetOrder, secretArgs);

      // One write replays (the event mirror remembers it after the session no
      // longer holds it), one stays queued in the session.
      controls.mode = NetworkMode.offline;
      await settle();
      final replayed = cache.mutate(
        opUpdateOrder,
        const TagContext(
          path: {'id': '7'},
          query: {'owner': _secret},
          body: {'note': 'replayed'},
        ),
      );
      await settle();
      controls.mode = NetworkMode.online;
      await replayed.timeout(const Duration(seconds: 5));
      await settle();
      controls.mode = NetworkMode.offline;
      await settle();
      unawaited(
        cache
            .mutate(
              opUpdateOrder,
              const TagContext(
                path: {'id': '7'},
                query: {'owner': _secret},
                body: {'note': 'queued'},
              ),
            )
            .then<void>((_) {}, onError: (Object _) {}),
      );
      await settle();
      await pumpEventQueue();

      final reads = <String, Map<String, String>>{
        ForgeDevtoolsProtocol.hello: {},
        ForgeDevtoolsProtocol.snapshot: {},
        ForgeDevtoolsProtocol.queries: {},
        ForgeDevtoolsProtocol.entities: {},
        ForgeDevtoolsProtocol.entity: {'key': 'Order:7'},
        ForgeDevtoolsProtocol.tags: {},
        ForgeDevtoolsProtocol.operations: {},
        ForgeDevtoolsProtocol.log: {},
        ForgeDevtoolsProtocol.frames: {},
        ForgeDevtoolsProtocol.requests: {},
        ForgeDevtoolsProtocol.overlays: {},
        ForgeDevtoolsProtocol.control: {},
        ForgeDevtoolsProtocol.outbox: {},
        ForgeDevtoolsProtocol.sync: {},
      };

      // The premise: each surface really did hold something of alice's.
      expect(await read(ForgeDevtoolsProtocol.log), contains(_secret));
      expect(await read(ForgeDevtoolsProtocol.queries), contains(_secret));
      expect(await read(ForgeDevtoolsProtocol.requests), contains(_secret));
      final syncRead = await read(ForgeDevtoolsProtocol.sync);
      expect(syncRead, contains('"entity":"Doc"'));
      expect(syncRead, contains('failed for $_secret'));
      final outbox = await read(ForgeDevtoolsProtocol.outbox);
      final rows =
          (jsonDecode(outbox) as Map<String, Object?>)['entries']!
              as List<Object?>;
      final aliceIds = [
        for (final row in rows) (row! as Map<String, Object?>)['id']! as String,
      ];
      // The queued write from the session and the replayed one from the mirror.
      expect(aliceIds, hasLength(2));
      expect(outbox, contains('"state":"replayed"'));
      expect(outbox, contains('"state":"queued"'));

      cache.setPrincipal('bob');
      await cache.idle;
      await pumpEventQueue();

      for (final MapEntry(:key, :value) in reads.entries) {
        final text = await read(key, value);

        expect(text, isNot(contains(_secret)), reason: key);
        expect(text, isNot(contains('alice')), reason: key);
        for (final id in aliceIds) {
          expect(text, isNot(contains(id)), reason: key);
        }
      }

      // What bob sees is bob's: nothing queued, nothing remembered.
      expect(
        jsonDecode(await read(ForgeDevtoolsProtocol.outbox)),
        containsPair('entries', isEmpty),
      );
    });
  });

  // The same wiring by hand, which is what the README shows for an app that
  // builds the client itself.
  group('a hand-built client with the simulator beneath the outbox', () {
    // The README's configureClient route: the app builds the simulator and the
    // outbox, configureClient attaches without either, and a register call
    // fills the request log and the simulator in.
    test('configureClient over an outbox is filled by a later register, and offline still queues', () async {
      final (:rest, :wire) = restOverFakeServer();
      final controls = ControlledTransport(rest);
      final outbox = OutboxTransport(controls);
      final cache = configureClient(
        transport: outbox,
        entities: entities,
        storage: memoryStorage(),
      );
      addTearDown(() => setClient(null));
      addTearDown(cache.dispose);
      final offline = OfflineClient(
        cache: cache,
        operations: operations,
        connectivity: withSimulatedConnectivity(FakeConnectivity(), controls),
        transport: outbox,
      );
      addTearDown(offline.dispose);

      final attached = forgeDevtoolsFor(cache)!;

      expect(attached.controls, isNull);
      expect(attached.requestLog, isNull);

      registerForgeServiceExtensions(
        cache,
        transport: rest,
        controls: controls,
        operations: operations,
        outbox: offline,
      );

      expect(attached.controls, same(controls));
      expect(attached.requestLog, isNotNull);
      expect(attached.outboxInspector, same(offline));

      await switchTo(cache, 'alice');
      await offline.restore();
      await cache.fetch(opGetOrder, orderArgs('7'));
      expect(attached.requests().where((r) => !r.marker), hasLength(1));

      controls.mode = NetworkMode.offline;
      await settle();
      final write = Watched(
        cache.mutate(opUpdateOrder, orderArgs('7', {'note': 'queued'})),
      );
      await settle();

      expect(write.done, isFalse);
      expect(offline.pending, hasLength(1));
      expect(offline.currentFailures, isEmpty);
      expect(patches(wire), isEmpty);
    });

    test('disposing the devtools while a write sleeps in simulated latency forwards it once and it clears normally', () async {
      final network = FakeTransport();
      final gate = Completer<void>();
      final controls = ControlledTransport(network, sleep: (_) => gate.future);
      final outbox = OutboxTransport(controls);
      final storage = memoryStorage();
      final cache = QueryCache(
        transport: outbox,
        entities: entities,
        storage: storage,
      );
      final connectivity = FakeConnectivity();
      final offline = OfflineClient(
        cache: cache,
        operations: operations,
        connectivity: withSimulatedConnectivity(connectivity, controls),
        transport: outbox,
      );
      addTearDown(offline.dispose);
      addTearDown(cache.dispose);
      registerForgeServiceExtensions(
        cache,
        controls: controls,
        outbox: offline,
        operations: operations,
      );
      await switchTo(cache, 'alice');
      await offline.restore();

      controls.latency = const Duration(seconds: 30);
      final future = cache.mutate(
        opUpdateOrder,
        orderArgs('7', {'note': 'sleeping'}),
      );
      final write = Watched(future);
      await settle();

      // Asleep in the simulator: not on the wire, and stored while it is out.
      expect(network.writes, isEmpty);
      expect(write.done, isFalse);

      unregisterForgeDevtools(cache);
      await settle();

      // Released, not aborted: it went out once, and the outbox treated the
      // answer as the success it was.
      expect(network.writes, hasLength(1));
      expect(noteOf(network.writes.single), 'sleeping');
      expect(write.done, isTrue);
      expect(write.error, isNull);
      expect((write.value! as Map<String, Object?>)['note'], 'sleeping');
      expect(offline.pending, isEmpty);
      expect(offline.currentFailures, isEmpty);
      expect(await storedOutbox(storage, 'alice'), isEmpty);
      expect(controls.isOnline, isTrue);
    });
  });
}
