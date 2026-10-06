import 'dart:async';
import 'dart:convert';
import 'dart:developer' as developer;

import 'package:forge_client/forge_client.dart';
import 'package:forge_client/src/devtools/control.dart';
import 'package:forge_client/src/devtools/protocol.dart';
import 'package:forge_client/src/devtools/seams.dart';
import 'package:forge_client/src/devtools/service_extensions.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:test/test.dart';

import 'harness.dart';

typedef _Posted = ({String kind, Map<String, Object?> data});

final class _Inner implements Transport {
  @override
  Future<Object?> execute(TransportRequest request) async => {'ok': true};
}

void main() {
  late Map<String, developer.ServiceExtensionHandler> handlers;
  late List<_Posted> posted;
  late int registrations;

  setUp(() {
    handlers = {};
    posted = [];
    registrations = 0;
    ForgeDevtoolsHost.debugOverride(
      registrar: (method, handler) {
        registrations++;
        handlers[method] = handler;
      },
      poster: (kind, data) => posted.add((kind: kind, data: data)),
    );
  });

  tearDown(ForgeDevtoolsHost.debugReset);

  Future<developer.ServiceExtensionResponse> raw(
    String method, [
    Map<String, String> params = const {},
  ]) => handlers[method]!(method, params);

  Future<Map<String, Object?>> call(
    String method, [
    Map<String, String> params = const {},
  ]) async {
    final response = await raw(method, params);
    expect(response.errorCode, isNull, reason: response.errorDetail);
    final decoded = jsonDecode(response.result!) as Map<String, Object?>;
    expect(decoded['type'], '_extensionType');
    expect(decoded['method'], method);
    return decoded;
  }

  List<Map<String, Object?>> items(
    Map<String, Object?> page, [
    String key = 'items',
  ]) => (page[key]! as List<Object?>).cast<Map<String, Object?>>();

  group('registration', () {
    test(
      'registers every ext.forge method once, however many caches attach',
      () {
        registerForgeServiceExtensions(Harness().cache);
        registerForgeServiceExtensions(Harness().cache);

        expect(handlers.keys.toSet(), ForgeDevtoolsProtocol.methods.toSet());
        expect(registrations, ForgeDevtoolsProtocol.methods.length);
        expect(
          ForgeDevtoolsProtocol.methods.every(
            (m) => m.startsWith('ext.forge.'),
          ),
          isTrue,
        );
      },
    );

    test('returns the same devtools for a cache registered twice', () {
      final h = Harness();

      expect(
        identical(
          registerForgeServiceExtensions(h.cache),
          registerForgeServiceExtensions(h.cache),
        ),
        isTrue,
      );
    });

    test('keeps at most eight caches attached, detaching the oldest', () {
      for (var i = 0; i < 9; i++) {
        registerForgeServiceExtensions(Harness().cache);
      }

      expect(ForgeDevtoolsHost.instance.debugCacheIds, [
        '2',
        '3',
        '4',
        '5',
        '6',
        '7',
        '8',
        '9',
      ]);
    });

    test(
      'answers hello with the protocol version and the attached caches',
      () async {
        registerForgeServiceExtensions(Harness().cache);
        registerForgeServiceExtensions(Harness().cache);

        final hello = await call(ForgeDevtoolsProtocol.hello);

        expect(hello['protocol'], ForgeDevtoolsProtocol.version);
        expect([for (final c in items(hello, 'caches')) c['id']], ['1', '2']);
      },
    );

    test('detaching gives the observer and the request slot back', () async {
      final h = Harness();
      final rest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient((_) async => http.Response('{}', 200)),
      );

      registerForgeServiceExtensions(h.cache, transport: rest);

      expect(h.cache.observer, isNotNull);
      expect(rest.debugObserver, isNotNull);

      unregisterForgeDevtools(h.cache);

      expect(h.cache.observer, isNull);
      expect(rest.debugObserver, isNull);
      expect(items(await call(ForgeDevtoolsProtocol.hello), 'caches'), isEmpty);
    });
  });

  group('reading', () {
    test('reports a snapshot of counters and status buckets without listing anything', () async {
      final h = Harness();
      registerForgeServiceExtensions(h.cache);
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final snapshot = await call(ForgeDevtoolsProtocol.snapshot);

      expect((snapshot['store']! as Map<String, Object?>)['records'], 3);
      expect((snapshot['statuses']! as Map<String, Object?>)['success'], 1);
      expect(snapshot.containsKey('queries'), isFalse);
      expect(snapshot['cache'], '1');
      expect(snapshot['watchingRequests'], isFalse);

      await sub.cancel();
    });

    // Review Focus 1: a 10,000-entity cache, read page by page, with every
    // response sized by its page and never by the cache.
    test('pages queries, entities and tags from a 10,000 entity cache without dumping it', () async {
      final h = Harness()
        ..reply('GET /orders', [
          for (var i = 0; i < 10000; i++) {'id': i, 'total': i},
        ]);
      registerForgeServiceExtensions(h.cache);
      final sub = h.mount(Ops.orderList);
      await h.settle();

      Future<(Map<String, Object?>, int, Duration)> timed(
        String method,
        Map<String, String> params,
      ) async {
        final watch = Stopwatch()..start();
        final response = await raw(method, params);
        watch.stop();
        expect(response.errorCode, isNull, reason: response.errorDetail);
        return (
          jsonDecode(response.result!) as Map<String, Object?>,
          response.result!.length,
          watch.elapsed,
        );
      }

      final (page, pageBytes, pageTime) = await timed(
        ForgeDevtoolsProtocol.entities,
        {'limit': '100'},
      );
      expect(page['total'], 10000);
      expect(items(page), hasLength(100));
      expect(pageBytes, lessThan(30 * 1024));
      expect(pageTime, lessThan(const Duration(milliseconds: 500)));

      final (clamped, _, _) = await timed(ForgeDevtoolsProtocol.entities, {
        'limit': '100000',
      });
      expect(items(clamped), hasLength(ForgeDevtoolsProtocol.maxPage));

      final (tail, _, _) = await timed(ForgeDevtoolsProtocol.entities, {
        'offset': '9990',
        'limit': '100',
      });
      expect(items(tail), hasLength(10));

      final (queries, queryBytes, _) = await timed(
        ForgeDevtoolsProtocol.queries,
        {},
      );
      final list = items(queries).single;
      expect(list['depCount'], 10000);
      expect(list.containsKey('deps'), isFalse);
      expect(queryBytes, lessThan(5 * 1024));

      final (detail, detailBytes, detailTime) = await timed(
        ForgeDevtoolsProtocol.query,
        {'key': h.key(Ops.orderList)},
      );
      final body = detail['detail']! as Map<String, Object?>;
      expect(body['deps'], hasLength(ForgeDevtoolsProtocol.maxListInDetail));
      expect(body['depsTotal'], 10000);
      expect(body['value'], hasLength(101));
      expect(detailBytes, lessThan(200 * 1024));
      expect(detailTime, lessThan(const Duration(milliseconds: 500)));

      final (snapshot, snapshotBytes, _) = await timed(
        ForgeDevtoolsProtocol.snapshot,
        {},
      );
      expect(snapshotBytes, lessThan(5 * 1024));
      expect(jsonEncode(snapshot), isNot(contains('Order:5000')));

      final (tags, tagBytes, tagTime) = await timed(
        ForgeDevtoolsProtocol.tags,
        {'limit': '100'},
      );
      expect(items(tags), hasLength(100));
      expect(tags['total'], greaterThanOrEqualTo(10001));
      expect(tagBytes, lessThan(50 * 1024));
      expect(tagTime, lessThan(const Duration(milliseconds: 500)));

      await sub.cancel();
    });

    test('explains a near miss over the wire', () async {
      final h = Harness();
      registerForgeServiceExtensions(h.cache);
      final sub = h.mount(Ops.orderList);
      await h.settle();
      await h.cache.mutate(
        Ops.orderCreate,
        const TagContext(body: {'total': 30}),
      );
      h.flush();
      await h.settle();

      final explained = await call(ForgeDevtoolsProtocol.explain, {
        'key': h.key(Ops.orderList),
        'question': 'whyNotRefetched',
      });
      final report = explained['report']! as Map<String, Object?>;

      expect(report['outcome'], 'missed');
      expect(
        items(report, 'nearest').first['relation'],
        'instance-vs-collection',
      );

      final hypothesis = await call(ForgeDevtoolsProtocol.explain, {
        'key': h.key(Ops.orderList),
        'question': 'whyNotRefetched',
        'cause': jsonEncode({
          'tags': ['Order[]'],
          'label': 'what if',
        }),
      });

      expect(
        (hypothesis['report']! as Map<String, Object?>)['outcome'],
        'refetched',
      );

      await sub.cancel();
    });

    test('previews an operation by id from the generated table', () async {
      final h = Harness();
      registerForgeServiceExtensions(
        h.cache,
        operations: {for (final op in Ops.all) op.id: op},
      );

      final operations = await call(ForgeDevtoolsProtocol.operations);
      expect([
        for (final op in items(operations, 'operations')) op['id'],
      ], contains('op_order_create'));

      final preview = await call(ForgeDevtoolsProtocol.wouldInvalidate, {
        'operation': 'op_order_create',
        'args': jsonEncode({'body': <String, Object?>{}}),
        'response': jsonEncode({'id': 9}),
      });
      final body = preview['preview']! as Map<String, Object?>;

      expect(body['tags'], ['Order:9']);
      expect(body['missed'], ['Order:9']);

      final unknown = await raw(ForgeDevtoolsProtocol.wouldInvalidate, {
        'operation': 'op_nope',
      });
      expect(
        unknown.errorCode,
        developer.ServiceExtensionResponse.invalidParams,
      );
    });

    test('pages the log after a sequence number', () async {
      final h = Harness();
      registerForgeServiceExtensions(h.cache);
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final first = await call(ForgeDevtoolsProtocol.log);
      final entries = items(first, 'entries');

      expect(entries, isNotEmpty);
      expect(entries.first['kind'], 'fetch');

      final next = await call(ForgeDevtoolsProtocol.log, {
        'after': '${entries.last['seq']}',
      });
      expect(items(next, 'entries'), isEmpty);
      expect(next['truncated'], isFalse);

      final short = await call(ForgeDevtoolsProtocol.log, {'limit': '1'});
      expect(items(short, 'entries').single['seq'], entries.last['seq']);
      expect(short['truncated'], isTrue);

      await sub.cancel();
    });

    test('turns frame capture on over the wire and reports overflow', () async {
      final h = Harness();
      registerForgeServiceExtensions(h.cache);

      await call(ForgeDevtoolsProtocol.capture, {
        'enabled': 'true',
        'limit': '2',
      });
      for (final id in [1, 2, 3]) {
        debugApplyFrames(h.cache, orderBinding, {'id': id});
      }

      final frames = await call(ForgeDevtoolsProtocol.frames);

      expect(frames['capturing'], isTrue);
      expect(frames['capacity'], 2);
      expect(frames['dropped'], 1);
      expect(
        [
          for (final f in items(frames, 'entries'))
            (f['payload']! as Map<String, Object?>)['id'],
        ],
        [2, 3],
      );

      final off = await call(ForgeDevtoolsProtocol.capture, {
        'enabled': 'false',
      });
      expect(off['capturing'], isFalse);
    });

    test('never sends an Authorization header or credential through ext.forge.requests', () async {
      final h = Harness();
      final rest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient(
          (_) async => http.Response(
            jsonEncode({'ok': true}),
            200,
            headers: {'content-type': 'application/json'},
          ),
        ),
        auth: _SecretAuth(),
      );
      registerForgeServiceExtensions(h.cache, transport: rest);

      await rest.execute(
        const TransportRequest(
          meta: Ops.orderCreate,
          args: TagContext(
            headers: {'Authorization': 'Bearer header-secret-456'},
            body: {'password': 'hunter2'},
          ),
        ),
      );

      final response = await raw(ForgeDevtoolsProtocol.requests);
      final decoded = jsonDecode(response.result!) as Map<String, Object?>;

      expect(decoded['watching'], isTrue);
      expect(items(decoded, 'entries'), hasLength(1));
      for (final secret in [
        'secret-token-123',
        'header-secret-456',
        'Authorization',
        'Bearer',
        'hunter2',
      ]) {
        expect(response.result, isNot(contains(secret)));
      }
    });
  });

  group('writing', () {
    test('runs actions and refuses unknown ones', () async {
      final h = Harness();
      registerForgeServiceExtensions(h.cache);
      final sub = h.mount(Ops.orderList);
      await h.settle();
      final before = h.calls.length;

      expect(
        (await call(ForgeDevtoolsProtocol.action, {
          'action': 'refetch',
          'target': h.key(Ops.orderList),
        }))['ok'],
        isTrue,
      );
      await h.settle();
      expect(h.calls.length, before + 1);

      expect(
        (await call(ForgeDevtoolsProtocol.action, {
          'action': 'evict',
          'target': 'Order:1',
        }))['ok'],
        isTrue,
      );
      expect(h.dev.hasEntity('Order:1'), isFalse);

      final patched = await call(ForgeDevtoolsProtocol.action, {
        'action': 'patch',
        'target': 'Order:2',
        'fields': jsonEncode({'total': 7}),
      });
      expect(patched['id'], isA<int>());
      expect(
        items(await call(ForgeDevtoolsProtocol.overlays), 'overlays'),
        hasLength(1),
      );

      expect(
        (await call(ForgeDevtoolsProtocol.action, {
          'action': 'refetch',
          'target': 'GET /nothing',
        }))['ok'],
        isFalse,
      );

      final unknown = await raw(ForgeDevtoolsProtocol.action, {
        'action': 'explode',
        'target': 'x',
      });
      expect(
        unknown.errorCode,
        developer.ServiceExtensionResponse.invalidParams,
      );

      await sub.cancel();
    });

    test('drives the network controls and says when none are wired', () async {
      final bare = Harness();
      registerForgeServiceExtensions(bare.cache);
      expect(
        (await call(ForgeDevtoolsProtocol.control, {'cache': '1'}))['wired'],
        isFalse,
      );

      final h = Harness();
      final controls = ControlledTransport(_Inner());
      registerForgeServiceExtensions(h.cache, controls: controls);

      final offline = await call(ForgeDevtoolsProtocol.control, {
        'cache': '2',
        'mode': 'offline',
        'latencyMs': '250',
      });
      expect(offline['mode'], 'offline');
      expect(offline['latencyMs'], 250);
      expect(controls.mode, NetworkMode.offline);

      final armed = await call(ForgeDevtoolsProtocol.control, {
        'cache': '2',
        'failNext': '503',
      });
      expect(armed['armed'], isTrue);
      expect(armed['armedStatus'], 503);

      final bogus = await raw(ForgeDevtoolsProtocol.control, {
        'cache': '2',
        'mode': 'underwater',
      });
      expect(bogus.errorCode, developer.ServiceExtensionResponse.invalidParams);
    });

    test('returns the outbox and the sync state, and refuses a replay with no inspector', () async {
      registerForgeServiceExtensions(Harness().cache);

      expect(
        await call(ForgeDevtoolsProtocol.outbox),
        containsPair('wired', false),
      );
      expect(items(await call(ForgeDevtoolsProtocol.sync), 'sources'), isEmpty);

      final refused = await raw(ForgeDevtoolsProtocol.outboxAction, {
        'action': 'replay',
        'id': 'm1',
      });
      expect(
        refused.errorCode,
        developer.ServiceExtensionResponse.extensionError,
      );
      expect(refused.errorDetail, contains('OutboxInspector'));
    });
  });

  group('events and errors', () {
    test(
      'batches log entries into one forge:event post per turn, bounded',
      () async {
        final h = Harness();
        final devtools = registerForgeServiceExtensions(h.cache)!;

        for (var i = 0; i < 10; i++) {
          devtools.actions.hold('q$i', 'error');
        }
        await pumpEventQueue();

        expect(posted, hasLength(1));
        expect(posted.single.kind, ForgeDevtoolsProtocol.eventKind);
        expect(posted.single.data['cache'], '1');
        expect(posted.single.data['entries'], hasLength(10));
        expect(posted.single.data['skipped'], 0);

        for (var i = 0; i < 300; i++) {
          devtools.actions.hold('q$i', 'error');
        }
        await pumpEventQueue();

        expect(posted, hasLength(2));
        expect(
          posted.last.data['entries'],
          hasLength(ForgeDevtoolsProtocol.maxEventsPerPost),
        );
        expect(posted.last.data['skipped'], 100);
      },
    );

    test('rejects an unknown cache id and a malformed parameter with the right error codes', () async {
      registerForgeServiceExtensions(Harness().cache);

      final unknown = await raw(ForgeDevtoolsProtocol.snapshot, {
        'cache': '99',
      });
      expect(
        unknown.errorCode,
        developer.ServiceExtensionResponse.extensionError,
      );

      final malformed = await raw(ForgeDevtoolsProtocol.entities, {
        'limit': 'abc',
      });
      expect(
        malformed.errorCode,
        developer.ServiceExtensionResponse.invalidParams,
      );

      final missing = await raw(ForgeDevtoolsProtocol.query);
      expect(
        missing.errorCode,
        developer.ServiceExtensionResponse.invalidParams,
      );
    });

    test(
      'says no cache is attached rather than throwing an unhandled error',
      () async {
        registerForgeServiceExtensions(Harness().cache);
        unregisterForgeDevtools(ForgeDevtoolsHost.instance.debugCaches.single);

        final response = await raw(ForgeDevtoolsProtocol.snapshot);

        expect(
          response.errorCode,
          developer.ServiceExtensionResponse.extensionError,
        );
        expect(response.errorDetail, contains('no cache is attached'));
      },
    );
  });

  group('a cache that is disposed or detached', () {
    // Pre-flight probe: `ext.forge.entity?cache=1` returned alice's record
    // after `dispose()`. A disposed cache is refused by every method that
    // takes a cache id, and nothing it held is served.
    final cacheMethods = ForgeDevtoolsProtocol.methods
        .where((m) => m != ForgeDevtoolsProtocol.hello)
        .toList();

    test('refuses every extension that takes a cache id once the cache is disposed', () async {
      final h = Harness();
      registerForgeServiceExtensions(h.cache);
      final sub = h.mount(Ops.orderList);
      await h.settle();

      // The premise: the record is served while the cache is alive.
      final before = await raw(ForgeDevtoolsProtocol.entity, {
        'cache': '1',
        'key': 'Order:1',
      });
      expect(before.errorCode, isNull);
      expect(before.result, contains('Order:1'));

      await sub.cancel();
      await h.cache.dispose();

      expect(cacheMethods, hasLength(ForgeDevtoolsProtocol.methods.length - 1));

      for (final method in cacheMethods) {
        final response = await raw(method, {
          'cache': '1',
          'key': 'Order:1',
          'action': 'clear',
          'id': 'm1',
          'enabled': 'true',
        });

        expect(
          response.errorCode,
          developer.ServiceExtensionResponse.extensionError,
          reason: method,
        );
        expect(response.errorDetail, contains('disposed'), reason: method);
        expect(response.result, isNull, reason: method);
        expect(response.errorDetail, isNot(contains('Order')), reason: method);
      }

      // And with no cache id, which would have meant this cache.
      final bare = await raw(ForgeDevtoolsProtocol.entity, {'key': 'Order:1'});
      expect(bare.errorCode, developer.ServiceExtensionResponse.extensionError);
      expect(bare.errorDetail, contains('no cache is attached'));
      expect(items(await call(ForgeDevtoolsProtocol.hello), 'caches'), isEmpty);
    });

    test(
      'refuses in the same turn dispose() is called, before it has finished',
      () async {
        // A sync source makes dispose() wait for it to stop, so the cache is
        // disposed for a while before its streams close and the host hears.
        final h = Harness(
          syncSources: [_SlowSource(Future<void>.value())],
          entities: {'Doc': const EntityMeta(idField: 'id')},
        );
        h.cache.setPrincipal('alice');
        await h.cache.idle;
        registerForgeServiceExtensions(h.cache);
        final sub = h.mount(Ops.orderList);
        await h.settle();

        final disposing = h.cache.dispose();

        expect(h.cache.isDisposed, isTrue);
        expect(ForgeDevtoolsHost.instance.debugCacheIds, [
          '1',
        ], reason: 'the host has not heard yet');

        for (final method in cacheMethods) {
          final response = await raw(method, {'cache': '1'});

          expect(
            response.errorCode,
            developer.ServiceExtensionResponse.extensionError,
            reason: method,
          );
          expect(response.errorDetail, contains('disposed'), reason: method);
        }

        await disposing;
        await sub.cancel();
      },
    );

    test(
      'gives the observer and the request slot back when the cache is disposed',
      () async {
        final h = Harness();
        final rest = RestTransport(
          baseUrl: Uri.parse('http://forge.test'),
          client: MockClient((_) async => http.Response('{}', 200)),
        );
        registerForgeServiceExtensions(h.cache, transport: rest);

        expect(h.cache.observer, isNotNull);
        expect(rest.debugObserver, isNotNull);

        await h.cache.dispose();
        await pumpEventQueue();

        expect(h.cache.observer, isNull);
        expect(rest.debugObserver, isNull);
        expect(ForgeDevtoolsHost.instance.debugCacheIds, isEmpty);
      },
    );

    test(
      'refuses a detached cache by name, and refuses to attach a disposed one',
      () async {
        final h = Harness();
        registerForgeServiceExtensions(h.cache);
        unregisterForgeDevtools(h.cache);

        for (final method in cacheMethods) {
          final response = await raw(method, {'cache': '1'});

          expect(
            response.errorCode,
            developer.ServiceExtensionResponse.extensionError,
            reason: method,
          );
          expect(response.errorDetail, contains('detached'), reason: method);
        }

        final gone = Harness();
        await gone.cache.dispose();

        expect(
          () => registerForgeServiceExtensions(gone.cache),
          throwsA(isA<StateError>()),
        );
      },
    );

    test(
      'returns nothing from a call that was waiting when its cache went away',
      () async {
        final gate = Completer<void>();
        final source = _SlowSource(gate.future);
        final h = Harness(
          syncSources: [source],
          entities: {'Doc': const EntityMeta(idField: 'id')},
        );
        h.cache.setPrincipal('alice');
        await h.cache.idle;
        registerForgeServiceExtensions(h.cache);

        final pending = raw(ForgeDevtoolsProtocol.sync);
        await pumpEventQueue();
        final disposing = h.cache.dispose();
        gate.complete();
        final response = await pending;
        await disposing;

        expect(
          response.errorCode,
          developer.ServiceExtensionResponse.extensionError,
        );
        expect(response.errorDetail, contains('disposed'));
        expect(response.result, isNull);
      },
    );
  });

  group('nothing crosses principals, end to end through the extensions', () {
    // Alice plants a secret in every place the devtools can read: a record, a
    // queued write, a captured frame, a request path and query, a logged
    // mutation and a sync status. After the switch, no read extension answers
    // with it.
    const secret = 'alice-ssn';

    test('serves none of alice to bob from any read extension', () async {
      final replies = _Replies();
      final rest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient(
          (_) async => http.Response(
            '{}',
            200,
            headers: {'content-type': 'application/json'},
          ),
        ),
      );
      final controls = ControlledTransport(_Inner());
      final cache = QueryCache(
        transport: replies,
        entities: {
          ...schema,
          'Doc': const EntityMeta(idField: 'id'),
        },
        storage: memoryStorage(),
        syncSources: [_AliceSource(() => replies.principal)],
      );
      replies.cache = cache;
      cache.setPrincipal('alice');
      await cache.idle;

      registerForgeServiceExtensions(
        cache,
        transport: rest,
        controls: controls,
        operations: {for (final op in Ops.all) op.id: op},
      );

      // A record, through a query that is no longer mounted when the user leaves.
      final sub = cache.watch(Ops.orderList, TagContext.empty).listen((_) {});
      await pumpEventQueue();
      final orderList = queryKey(Ops.orderList, TagContext.empty);
      await sub.cancel();

      // A queued write, and a failed one with the server's body.
      await cache.session!.enqueue(
        PendingMutationRecord(
          id: 'm-alice',
          operationId: 'op_order_create',
          argsJson: '{"body":{"ssn":"$secret"}}',
          idempotencyKey: 'key-m-alice',
          createdAt: DateTime.utc(2026, 10, 4),
          stateJson: '{"kind":"queued"}',
        ),
      );
      await cache.session!.enqueue(
        PendingMutationRecord(
          id: 'm-failed',
          operationId: 'op_order_create',
          argsJson: '{}',
          idempotencyKey: 'key-m-failed',
          createdAt: DateTime.utc(2026, 10, 4),
          stateJson:
              '{"kind":"failed","failure":{"kind":"conflict","status":409,"body":"$secret"}}',
        ),
      );
      cache.observer!(
        debugOutboxFailed(
          'm-events',
          'op_order_create',
          StateError('rejected $secret'),
        ),
      );
      cache.observer!(
        debugSyncStatusChanged('Doc', const SyncFailed('failed for $secret')),
      );

      // A frame, a mutation, a request and an overlay.
      await call(ForgeDevtoolsProtocol.capture, {'enabled': 'true'});
      debugApplyFrames(cache, orderBinding, {'id': 5, 'ssn': secret});
      await cache.mutate(
        Ops.orderCreate,
        const TagContext(body: {'ssn': secret}),
      );
      await rest.execute(
        const TransportRequest(
          meta: Ops.orderGet,
          args: TagContext(path: {'id': secret}, query: {'ssn': secret}),
        ),
      );
      await call(ForgeDevtoolsProtocol.action, {
        'action': 'patch',
        'target': 'Order:1',
        'fields': jsonEncode({'ssn': secret}),
      });
      await call(ForgeDevtoolsProtocol.control, {'failNext': '503'});
      await pumpEventQueue();

      Future<String> read(
        String method, [
        Map<String, String> params = const {},
      ]) async {
        final response = await raw(method, params);
        expect(
          response.errorCode,
          isNull,
          reason: '$method: ${response.errorDetail}',
        );
        return response.result!;
      }

      final reads = <String, Map<String, String>>{
        ForgeDevtoolsProtocol.hello: {},
        ForgeDevtoolsProtocol.snapshot: {},
        ForgeDevtoolsProtocol.queries: {},
        ForgeDevtoolsProtocol.query: {'key': orderList},
        ForgeDevtoolsProtocol.entities: {},
        ForgeDevtoolsProtocol.entity: {'key': 'Order:1'},
        ForgeDevtoolsProtocol.tags: {},
        ForgeDevtoolsProtocol.explain: {
          'key': orderList,
          'question': 'whyNotRefetched',
        },
        ForgeDevtoolsProtocol.operations: {},
        ForgeDevtoolsProtocol.wouldInvalidate: {
          'operation': 'op_order_create',
          'args': '{}',
          'response': '{"id": 9}',
        },
        ForgeDevtoolsProtocol.log: {},
        ForgeDevtoolsProtocol.frames: {},
        ForgeDevtoolsProtocol.requests: {},
        ForgeDevtoolsProtocol.overlays: {},
        ForgeDevtoolsProtocol.control: {},
        ForgeDevtoolsProtocol.outbox: {},
        ForgeDevtoolsProtocol.sync: {},
      };

      // Every method that only reads is in the list: the writers are the other three.
      expect({
        ...reads.keys,
        ForgeDevtoolsProtocol.capture,
        ForgeDevtoolsProtocol.action,
        ForgeDevtoolsProtocol.outboxAction,
      }, ForgeDevtoolsProtocol.methods.toSet());

      // The premise: each surface really served it for alice.
      expect(
        await read(
          ForgeDevtoolsProtocol.entity,
          reads[ForgeDevtoolsProtocol.entity]!,
        ),
        contains(secret),
      );
      expect(await read(ForgeDevtoolsProtocol.frames), contains(secret));
      expect(await read(ForgeDevtoolsProtocol.requests), contains(secret));
      expect(await read(ForgeDevtoolsProtocol.log), contains(secret));
      expect(await read(ForgeDevtoolsProtocol.sync), contains(secret));
      expect(
        await read(ForgeDevtoolsProtocol.outbox),
        allOf(contains('m-alice'), contains('m-events'), contains('m-failed')),
      );
      expect(
        await read(ForgeDevtoolsProtocol.overlays),
        contains('"kind":"merge"'),
      );
      expect(
        await read(ForgeDevtoolsProtocol.control),
        contains('"armed":true'),
      );

      cache.setPrincipal('bob');
      await cache.idle;
      await pumpEventQueue();

      final after = StringBuffer();
      for (final MapEntry(:key, :value) in reads.entries) {
        final text = await read(key, value);

        expect(text, isNot(contains(secret)), reason: key);
        expect(text, isNot(contains('alice')), reason: key);
        after.write(text);
      }

      expect(after.toString(), isNot(contains('m-alice')));
      expect(after.toString(), isNot(contains('m-events')));
      expect(after.toString(), isNot(contains('m-failed')));

      // What bob sees is bob's: an empty outbox, no frames, the marker only,
      // and no armed failure.
      expect(
        await call(ForgeDevtoolsProtocol.outbox),
        containsPair('entries', isEmpty),
      );
      expect(
        items(
          await call(ForgeDevtoolsProtocol.frames),
          'entries',
        ).map((f) => f['payload']),
        everyElement(isNull),
      );
      expect(
        items(
          await call(ForgeDevtoolsProtocol.requests),
          'entries',
        ).every((r) => r['marker'] == true),
        isTrue,
      );
      expect(
        await call(ForgeDevtoolsProtocol.control),
        containsPair('armed', false),
      );
      expect(
        await call(ForgeDevtoolsProtocol.query, {'key': orderList}),
        containsPair('detail', isNull),
      );
      expect(
        items(await call(ForgeDevtoolsProtocol.overlays), 'overlays'),
        isEmpty,
      );
      expect((await call(ForgeDevtoolsProtocol.hello))['caches'], [
        {'id': '1', 'principal': 'bob', 'label': 'cache 1'},
      ]);

      await cache.dispose();
    });

    test(
      'posts none of what was queued for alice in the turn she leaves in',
      () async {
        final h = Harness();
        final devtools = registerForgeServiceExtensions(h.cache)!;

        h.cache.setPrincipal('alice');
        await h.cache.idle;
        await pumpEventQueue();
        posted.clear();

        devtools.actions.hold('$secret-query', 'error');
        devtools.actions.hold('$secret-other', 'error');
        h.cache.setPrincipal('bob');
        await pumpEventQueue();

        expect(
          jsonEncode([for (final p in posted) p.data]),
          isNot(contains(secret)),
        );
        expect(posted, hasLength(1));
        expect(
          [
            for (final e in posted.single.data['entries']! as List<Object?>)
              (e! as Map<String, Object?>)['kind'],
          ],
          ['principal'],
        );
      },
    );
  });

  group('stale answers', () {
    test('passes the stale flag from outbox() and sync() through unchanged, and leaves it off otherwise', () async {
      final h = Harness(
        syncSources: [_AliceSource(() => 'alice')],
        entities: {'Doc': const EntityMeta(idField: 'id')},
      );
      h.cache.setPrincipal('alice');
      await h.cache.idle;
      registerForgeServiceExtensions(h.cache);

      expect(
        await call(ForgeDevtoolsProtocol.outbox),
        isNot(contains('stale')),
      );
      expect(await call(ForgeDevtoolsProtocol.sync), isNot(contains('stale')));

      // Asked while the principal is changing: after the devtools' own
      // listener, before the cache has emptied.
      final answers = <Future<developer.ServiceExtensionResponse>>[];
      h.cache.watchPrincipalChanging((_) {
        answers
          ..add(raw(ForgeDevtoolsProtocol.outbox))
          ..add(raw(ForgeDevtoolsProtocol.sync));
      });
      h.cache.setPrincipal('bob');
      await h.cache.idle;

      for (final answer in answers) {
        final decoded =
            jsonDecode((await answer).result!) as Map<String, Object?>;

        expect(decoded['stale'], isTrue);
        expect(
          decoded.containsKey('entries')
              ? decoded['entries']
              : decoded['sources'],
          isEmpty,
        );
      }

      // And once bob is in, a plain answer again.
      expect(
        await call(ForgeDevtoolsProtocol.outbox),
        isNot(contains('stale')),
      );
    });

    test('marks a sync answer stale when the principal changes while a source is being asked', () async {
      final gate = Completer<void>();
      final h = Harness(
        syncSources: [_SlowSource(gate.future)],
        entities: {'Doc': const EntityMeta(idField: 'id')},
      );
      h.cache.setPrincipal('alice');
      await h.cache.idle;
      registerForgeServiceExtensions(h.cache);

      final pending = call(ForgeDevtoolsProtocol.sync);
      await pumpEventQueue();
      h.cache.setPrincipal('bob');
      gate.complete();

      final result = await pending;

      expect(result['stale'], isTrue);
      expect(result['sources'], isEmpty);
      expect(jsonEncode(result), isNot(contains('alice')));
    });
  });

  group('the session on an action', () {
    test('refuses an action aimed at a session the cache has left, and accepts the current one', () async {
      final h = Harness();
      registerForgeServiceExtensions(h.cache);
      h.cache.setPrincipal('alice');
      await h.cache.idle;
      final sub = h.mount(Ops.orderList);
      await h.settle();

      final aimed =
          (await call(ForgeDevtoolsProtocol.snapshot))['session']! as int;

      // The current session, no session at all, and an action that reads.
      expect(
        (await call(ForgeDevtoolsProtocol.action, {
          'action': 'stale',
          'target': h.key(Ops.orderList),
          'session': '$aimed',
        }))['ok'],
        isTrue,
      );
      expect(
        (await call(ForgeDevtoolsProtocol.action, {
          'action': 'stale',
          'target': h.key(Ops.orderList),
        }))['ok'],
        isTrue,
      );

      h.cache.setPrincipal('bob');
      await h.cache.idle;
      await h.settle();
      expect(
        h.dev.hasEntity('Order:1'),
        isTrue,
        reason: 'bob has his own Order:1 by now',
      );

      final log =
          (await call(ForgeDevtoolsProtocol.log))['entries']! as List<Object?>;
      final overlays = items(
        await call(ForgeDevtoolsProtocol.overlays),
        'overlays',
      );

      for (final action in [
        {'action': 'evict', 'target': 'Order:1'},
        {'action': 'patch', 'target': 'Order:1', 'fields': '{"total": 1}'},
        {'action': 'invalidateTag', 'target': 'Order[]'},
        {'action': 'refetch', 'target': h.key(Ops.orderList)},
        {'action': 'clear'},
      ]) {
        final refused = await raw(ForgeDevtoolsProtocol.action, {
          ...action,
          'session': '$aimed',
        });

        expect(
          refused.errorCode,
          developer.ServiceExtensionResponse.extensionError,
          reason: '$action',
        );
        expect(
          refused.errorDetail,
          contains('session $aimed'),
          reason: '$action',
        );
      }

      // Nothing happened to bob's cache, and nothing was logged.
      expect(h.dev.hasEntity('Order:1'), isTrue);
      expect(
        items(await call(ForgeDevtoolsProtocol.overlays), 'overlays'),
        hasLength(overlays.length),
      );
      expect(
        (await call(ForgeDevtoolsProtocol.log))['entries'],
        hasLength(log.length),
      );

      // The session the panel reads next is accepted.
      final current =
          (await call(ForgeDevtoolsProtocol.snapshot))['session']! as int;

      expect(current, greaterThan(aimed));
      expect(
        (await call(ForgeDevtoolsProtocol.action, {
          'action': 'evict',
          'target': 'Order:1',
          'session': '$current',
        }))['ok'],
        isTrue,
      );
      expect(h.dev.hasEntity('Order:1'), isFalse);

      await sub.cancel();
    });

    test('says a malformed session is a bad parameter', () async {
      registerForgeServiceExtensions(Harness().cache);

      for (final session in ['abc', '-1', '1.5']) {
        final response = await raw(ForgeDevtoolsProtocol.action, {
          'action': 'clear',
          'session': session,
        });

        expect(
          response.errorCode,
          developer.ServiceExtensionResponse.invalidParams,
          reason: session,
        );
      }
    });
  });

  group('a later registration on an attached cache', () {
    test('fills the empty slots without registering anything again', () async {
      final h = Harness();
      final first = registerForgeServiceExtensions(h.cache)!;
      final registered = registrations;

      expect(
        (await call(ForgeDevtoolsProtocol.snapshot))['watchingRequests'],
        isFalse,
      );
      expect(
        await call(ForgeDevtoolsProtocol.control),
        containsPair('wired', false),
      );
      expect(
        items(await call(ForgeDevtoolsProtocol.operations), 'operations'),
        isEmpty,
      );

      final rest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient((_) async => http.Response('{}', 200)),
      );
      final controls = ControlledTransport(_Inner());
      final revalidation = Revalidation({
        RevalidationSource.focus: () => () {},
      });
      final inspector = _Inspector();

      final second = registerForgeServiceExtensions(
        h.cache,
        transport: rest,
        controls: controls,
        operations: {for (final op in Ops.all) op.id: op},
        revalidation: revalidation,
        outbox: inspector,
      );

      expect(identical(second, first), isTrue);
      expect(registrations, registered);
      expect(ForgeDevtoolsHost.instance.debugCacheIds, ['1']);

      // Every slot answers.
      await rest.execute(
        const TransportRequest(meta: Ops.orderList, args: TagContext.empty),
      );
      final requests = await call(ForgeDevtoolsProtocol.requests);
      expect(requests['watching'], isTrue);
      expect(items(requests, 'entries'), hasLength(1));
      expect(
        await call(ForgeDevtoolsProtocol.control, {'mode': 'slow'}),
        containsPair('wired', true),
      );
      expect(controls.mode, NetworkMode.slow);
      final control = await call(ForgeDevtoolsProtocol.control, {
        'toggle': 'focus',
      });
      expect(
        ((control['revalidation']! as Map<String, Object?>)['focus']!
            as Map<String, Object?>)['enabled'],
        isTrue,
      );
      expect([
        for (final op in items(
          await call(ForgeDevtoolsProtocol.operations),
          'operations',
        ))
          op['id'],
      ], contains('op_order_create'));
      final preview = await call(ForgeDevtoolsProtocol.wouldInvalidate, {
        'operation': 'op_order_create',
        'response': '{"id": 9}',
      });
      expect((preview['preview']! as Map<String, Object?>)['tags'], [
        'Order:9',
      ]);
      expect(
        (await call(ForgeDevtoolsProtocol.snapshot))['outboxWired'],
        isTrue,
      );

      // The late request log is the cache's own: a principal change purges it
      // and detaching hands the slot back.
      h.cache.setPrincipal('bob');
      await h.cache.idle;
      expect(
        items(
          await call(ForgeDevtoolsProtocol.requests),
          'entries',
        ).every((r) => r['marker'] == true),
        isTrue,
      );

      unregisterForgeDevtools(h.cache);

      expect(rest.debugObserver, isNull);
    });

    test('never overwrites a slot that is filled', () async {
      final h = Harness();
      final firstRest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient((_) async => http.Response('{}', 200)),
      );
      final firstControls = ControlledTransport(_Inner());
      final firstRevalidation = Revalidation({
        RevalidationSource.poll: () => () {},
      });
      const firstOp = OperationMeta(
        id: 'op_same',
        method: 'GET',
        path: '/first',
      );

      registerForgeServiceExtensions(
        h.cache,
        transport: firstRest,
        controls: firstControls,
        operations: {'op_same': firstOp},
        revalidation: firstRevalidation,
      );
      final observer = firstRest.debugObserver;

      final secondRest = RestTransport(
        baseUrl: Uri.parse('http://forge.test'),
        client: MockClient((_) async => http.Response('{}', 200)),
      );
      final secondControls = ControlledTransport(_Inner());
      final secondRevalidation = Revalidation({
        RevalidationSource.focus: () => () {},
      });

      registerForgeServiceExtensions(
        h.cache,
        transport: secondRest,
        controls: secondControls,
        operations: {
          'op_same': const OperationMeta(
            id: 'op_same',
            method: 'GET',
            path: '/second',
          ),
          'op_new': const OperationMeta(
            id: 'op_new',
            method: 'GET',
            path: '/new',
          ),
        },
        revalidation: secondRevalidation,
      );

      expect(firstRest.debugObserver, same(observer));
      expect(secondRest.debugObserver, isNull);

      await call(ForgeDevtoolsProtocol.control, {'mode': 'offline'});
      expect(firstControls.mode, NetworkMode.offline);
      expect(secondControls.mode, NetworkMode.online);

      final control = await call(ForgeDevtoolsProtocol.control, {
        'toggle': 'poll',
      });
      expect(
        ((control['revalidation']! as Map<String, Object?>)['poll']!
            as Map<String, Object?>)['registered'],
        isTrue,
      );
      expect(
        ((control['revalidation']! as Map<String, Object?>)['focus']!
            as Map<String, Object?>)['registered'],
        isFalse,
      );

      final ops = {
        for (final op in items(
          await call(ForgeDevtoolsProtocol.operations),
          'operations',
        ))
          op['id']: op['path'],
      };
      expect(ops, {'op_same': '/first', 'op_new': '/new'});

      // The first transport is still the one logged.
      await firstRest.execute(
        const TransportRequest(meta: Ops.orderList, args: TagContext.empty),
      );
      expect(
        items(await call(ForgeDevtoolsProtocol.requests), 'entries'),
        hasLength(1),
      );
    });
  });

  group('responses', () {
    test(
      'pass through bounded(): an enormous value is cut, never sent whole',
      () async {
        final long = List.filled(5000, 'x').join();
        final h = Harness();
        registerForgeServiceExtensions(
          h.cache,
          operations: {
            'op_long': OperationMeta(
              id: 'op_long',
              method: 'GET',
              path: '/$long',
            ),
          },
        );

        final response = await raw(ForgeDevtoolsProtocol.operations);
        final path =
            items(
                  jsonDecode(response.result!) as Map<String, Object?>,
                  'operations',
                ).single['path']!
                as String;

        expect(path.length, lessThan(1100));
        expect(path, endsWith('...'));
        expect(response.result!.length, lessThan(2000));
      },
    );

    test('refuses a request that arrives with no parameters it needs, in every method that needs one', () async {
      registerForgeServiceExtensions(Harness().cache);

      for (final method in [
        ForgeDevtoolsProtocol.query,
        ForgeDevtoolsProtocol.entity,
        ForgeDevtoolsProtocol.explain,
        ForgeDevtoolsProtocol.wouldInvalidate,
        ForgeDevtoolsProtocol.capture,
        ForgeDevtoolsProtocol.action,
        ForgeDevtoolsProtocol.outboxAction,
      ]) {
        final response = await raw(method);

        expect(
          response.errorCode,
          developer.ServiceExtensionResponse.invalidParams,
          reason: method,
        );
      }
    });
  });
}

const _secretOwner = 'alice-ssn';

/// A transport that answers by `METHOD path`, and by who is signed in: alice's
/// order carries her secret, bob's does not.
final class _Replies implements Transport {
  QueryCache? cache;

  String? get principal => cache?.principal;

  @override
  Future<Object?> execute(TransportRequest request) async {
    await Future<void>.value();

    return switch ('${request.meta.method} ${request.meta.path}') {
      'GET /orders' => [
        if (principal == 'alice')
          {'id': 1, 'total': 10, 'ssn': _secretOwner}
        else
          {'id': 1, 'total': 99},
      ],
      'POST /orders' => {'id': 9, 'total': 30},
      _ => null,
    };
  }
}

/// Describes itself with a secret while [principal] is alice, and with nothing
/// for anyone else, as a real source does once it has been stopped.
final class _AliceSource implements SyncSource, DevtoolsInspectable {
  _AliceSource(this.principal);

  final String? Function() principal;

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
  Future<Map<String, Object?>> describeForDevtools() async =>
      principal() == 'alice' ? {'owner': _secretOwner} : <String, Object?>{};
}

/// A source whose self-description waits on [gate].
final class _SlowSource implements SyncSource, DevtoolsInspectable {
  _SlowSource(this.gate);

  final Future<void> gate;

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
    return {'owner': _secretOwner};
  }
}

final class _Inspector implements OutboxInspector {
  @override
  Future<void> replay(String mutationId) async {}

  @override
  Future<void> discard(String mutationId) async {}
}

final class _SecretAuth implements AuthProvider {
  @override
  Map<String, String> credentials(OperationMeta meta) => {
    'Authorization': 'Bearer secret-token-123',
  };

  @override
  void refresh() {}
}
