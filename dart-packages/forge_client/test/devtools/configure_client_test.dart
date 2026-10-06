import 'dart:convert';
import 'dart:developer' as developer;

import 'package:forge_client/devtools.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/service_extensions.dart'
    show ForgeDevtoolsHost;
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
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

/// Stands in for an `OutboxTransport`: forwards to [inner] and is not a
/// `RestTransport`, so `configureClient` must leave it alone.
final class _Forwarding implements Transport {
  _Forwarding(this.inner);

  final Transport inner;
  final List<TransportRequest> seen = [];

  @override
  Future<Object?> execute(TransportRequest request) {
    seen.add(request);
    return inner.execute(request);
  }
}

const _secret = 'alice-ssn';

void main() {
  late Map<String, developer.ServiceExtensionHandler> handlers;
  late int registrations;

  setUp(() {
    handlers = {};
    registrations = 0;
    ForgeDevtoolsHost.debugOverride(
      registrar: (method, handler) {
        registrations++;
        handlers[method] = handler;
      },
      poster: (_, _) {},
    );
  });

  tearDown(() {
    ForgeDevtoolsHost.debugReset();
    setClient(null);
  });

  Future<Map<String, Object?>> call(
    String method, [
    Map<String, String> params = const {},
  ]) async {
    final response = await handlers[method]!(method, params);
    expect(response.errorCode, isNull, reason: response.errorDetail);
    return jsonDecode(response.result!) as Map<String, Object?>;
  }

  ({RestTransport rest, List<http.Request> sent}) wiredRest({
    http.Response Function(http.Request request)? reply,
  }) {
    final sent = <http.Request>[];
    final transport = RestTransport(
      baseUrl: Uri.parse('http://forge.test'),
      client: MockClient((request) async {
        sent.add(request);
        return reply?.call(request) ??
            http.Response(
              jsonEncode([
                {'id': 1, 'total': 10},
              ]),
              200,
              headers: {'content-type': 'application/json'},
            );
      }),
    );
    return (rest: transport, sent: sent);
  }

  test('configureClient attaches the devtools and the simulator in debug, with no app code', () async {
    final (:rest, sent: _) = wiredRest();
    final cache = configureClient(transport: rest, entities: schema);

    expect(forgeDevtoolsFor(cache), isNotNull);
    expect(forgeDevtoolsFor(cache)!.controls, isNotNull);
    expect(rest.debugObserver, isNotNull);
    expect(
      [
        for (final c
            in (await call(ForgeDevtoolsProtocol.hello))['caches']!
                as List<Object?>)
          (c! as Map<String, Object?>)['id'],
      ],
      ['1'],
    );
    expect((await call(ForgeDevtoolsProtocol.control))['wired'], isTrue);
  });

  test(
    'a RestTransport given to configureClient feeds the request log',
    () async {
      final (:rest, :sent) = wiredRest();
      final cache = configureClient(transport: rest, entities: schema);

      await cache.fetch(Ops.orderList, TagContext.empty);

      expect(sent, hasLength(1));
      expect(
        (await call(ForgeDevtoolsProtocol.requests))['entries'],
        hasLength(1),
      );
    },
  );

  test('offline from the panel fails the cache requests before they reach the wire', () async {
    final (:rest, :sent) = wiredRest();
    final cache = configureClient(transport: rest, entities: schema);

    await call(ForgeDevtoolsProtocol.control, {'mode': 'offline'});

    await expectLater(
      cache.fetch(Ops.orderList, TagContext.empty),
      throwsA(isA<SimulatedOffline>()),
    );
    expect(sent, isEmpty);
  });

  test(
    'a cache built with the constructor is not attached until registered',
    () {
      final cache = Harness().cache;

      expect(forgeDevtoolsFor(cache), isNull);
      expect(
        registerForgeServiceExtensions(cache),
        same(forgeDevtoolsFor(cache)),
      );
    },
  );

  // Contracts "Devtools registration": configureClient attaches first, the
  // app hands over its OfflineClient afterwards.
  test('a second register with an outbox wires replay and discard into the attached devtools, registering nothing again', () async {
    final (:rest, sent: _) = wiredRest();
    final cache = configureClient(transport: rest, entities: schema);
    final attached = forgeDevtoolsFor(cache);
    final inspector = _Inspector();

    expect(registrations, ForgeDevtoolsProtocol.methods.length);
    expect((await call(ForgeDevtoolsProtocol.outbox))['wired'], isFalse);

    final returned = registerForgeServiceExtensions(cache, outbox: inspector);

    expect(returned, same(attached));
    expect(returned!.outboxInspector, same(inspector));
    expect(registrations, ForgeDevtoolsProtocol.methods.length);
    expect(ForgeDevtoolsHost.instance.debugCacheIds, ['1']);
    expect((await call(ForgeDevtoolsProtocol.outbox))['wired'], isTrue);

    await call(ForgeDevtoolsProtocol.outboxAction, {
      'action': 'replay',
      'id': 'm1',
    });
    await call(ForgeDevtoolsProtocol.outboxAction, {
      'action': 'discard',
      'id': 'm2',
    });

    expect(inspector.calls, ['replay m1', 'discard m2']);

    // A later call without an outbox leaves the inspector in place.
    registerForgeServiceExtensions(cache);
    expect(forgeDevtoolsFor(cache)!.outboxInspector, same(inspector));
    expect(registrations, ForgeDevtoolsProtocol.methods.length);
  });

  // The brief for this task asserted that a second register refused `transport`,
  // `operations` and `revalidation`. Task 9 (B5/B6) replaced the assert with
  // filling: a slot that is empty is filled, one that is filled is never
  // overwritten, and nothing registers again.
  test('a second register fills the slots configureClient left empty and never overwrites a filled one', () async {
    final inner = wiredRest();
    final forwarding = _Forwarding(inner.rest);
    final cache = configureClient(transport: forwarding, entities: schema);
    final attached = forgeDevtoolsFor(cache)!;

    // A transport that is not a RestTransport: attached, but with no request
    // log and no simulator.
    expect(attached.controls, isNull);
    expect(attached.requestLog, isNull);
    expect(inner.rest.debugObserver, isNull);

    final controls = ControlledTransport(inner.rest);
    final revalidation = Revalidation({});
    final returned = registerForgeServiceExtensions(
      cache,
      transport: inner.rest,
      controls: controls,
      revalidation: revalidation,
      operations: {Ops.orderList.id: Ops.orderList},
    );

    expect(returned, same(attached));
    expect(attached.controls, same(controls));
    expect(attached.requestLog, isNotNull);
    expect(attached.revalidation, same(revalidation));
    expect(inner.rest.debugObserver, isNotNull);
    expect(registrations, ForgeDevtoolsProtocol.methods.length);

    // The request log now records what goes through the REST transport.
    await cache.fetch(Ops.orderList, TagContext.empty);
    expect(
      (await call(ForgeDevtoolsProtocol.requests))['entries'],
      hasLength(1),
    );

    // The first registration wins.
    final log = attached.requestLog;
    registerForgeServiceExtensions(
      cache,
      controls: ControlledTransport(inner.rest),
      revalidation: Revalidation({}),
    );
    expect(attached.controls, same(controls));
    expect(attached.revalidation, same(revalidation));
    expect(attached.requestLog, same(log));
  });

  test('an outbox-like wrapper given to configureClient is never wrapped, so offline writes still reach it', () async {
    final inner = wiredRest();
    final forwarding = _Forwarding(inner.rest);
    final cache = configureClient(transport: forwarding, entities: schema);

    await cache.fetch(Ops.orderList, TagContext.empty);

    // The cache's transport is the app's own: its wrapper saw the request
    // first, with no simulator in front of it.
    expect(forwarding.seen, hasLength(1));
    expect(forgeDevtoolsFor(cache)!.controls, isNull);
    expect((await call(ForgeDevtoolsProtocol.control))['wired'], isFalse);
  });

  test('forgeDevtoolsFor is null once the cache is detached', () {
    final (:rest, sent: _) = wiredRest();
    final cache = configureClient(transport: rest, entities: schema);

    unregisterForgeDevtools(cache);

    expect(forgeDevtoolsFor(cache), isNull);
  });

  // Privacy (preflight P1/P3), through the production path: nothing a
  // configureClient cache recorded for alice is readable once bob is current.
  test(
    'after setPrincipal nothing alice did is readable from any ext.forge read',
    () async {
      final (:rest, :sent) = wiredRest(
        reply: (request) => http.Response(
          jsonEncode(
            request.method == 'GET'
                ? [
                    {'id': 1, 'total': 10, 'owner': _secret},
                  ]
                : {'id': 1, 'total': 10, 'owner': _secret},
          ),
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      final cache = configureClient(transport: rest, entities: schema);
      final reads = <String, Map<String, String>>{
        ForgeDevtoolsProtocol.hello: {},
        ForgeDevtoolsProtocol.snapshot: {},
        ForgeDevtoolsProtocol.queries: {},
        ForgeDevtoolsProtocol.entities: {},
        ForgeDevtoolsProtocol.entity: {'key': 'Order:1'},
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

      Future<String> read(String method, Map<String, String> params) async {
        final response = await handlers[method]!(method, params);
        expect(
          response.errorCode,
          isNull,
          reason: '$method: ${response.errorDetail}',
        );
        return response.result!;
      }

      cache.setPrincipal('alice');
      await cache.idle;

      const args = TagContext(query: {'owner': _secret});
      await cache.fetch(Ops.orderList, args);
      await cache.mutate(
        Ops.orderUpdate,
        const TagContext(path: {'id': '1'}, body: {'owner': _secret}),
      );

      // The premise: the surfaces really did hold the secret for alice.
      expect(
        await read(
          ForgeDevtoolsProtocol.entity,
          reads[ForgeDevtoolsProtocol.entity]!,
        ),
        contains(_secret),
      );
      expect(await read(ForgeDevtoolsProtocol.requests, {}), contains(_secret));
      expect(await read(ForgeDevtoolsProtocol.log, {}), contains(_secret));
      expect(await read(ForgeDevtoolsProtocol.queries, {}), contains(_secret));
      expect(sent, isNotEmpty);

      cache.setPrincipal('bob');
      await cache.idle;
      await pumpEventQueue();

      for (final MapEntry(:key, :value) in reads.entries) {
        final text = await read(key, value);

        expect(text, isNot(contains(_secret)), reason: key);
        expect(text, isNot(contains('alice')), reason: key);
      }
    },
  );
}
