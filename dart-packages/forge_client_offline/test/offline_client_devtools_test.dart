@TestOn('vm')
library;

import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools.dart';
import 'package:forge_client/forge_client.dart';
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

void main() {
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
  });

  // The same wiring by hand, which is what the README shows for an app that
  // builds the client itself.
  group('a hand-built client with the simulator beneath the outbox', () {
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
