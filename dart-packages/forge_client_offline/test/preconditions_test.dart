import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';

const _get = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
);
const _patch = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  invalidates: ['Order:{id}'],
);
const EntitySchema _entities = {'Order': EntityMeta(idField: 'id')};
const _seven = TagContext(path: {'id': '7'});

final class _Recording implements Transport {
  final List<TransportRequest> requests = [];
  int total = 10;

  @override
  Future<Object?> execute(TransportRequest request) async {
    requests.add(request);
    return <String, Object?>{'id': request.args.path['id'], 'total': total};
  }
}

PendingMutationRecord _record(String id, String args) => PendingMutationRecord(
  id: id,
  operationId: 'op_update_order',
  argsJson: args,
  idempotencyKey: 'key-$id',
  createdAt: DateTime.utc(2026, 10, 4),
  stateJson: '{"kind":"queued"}',
);

void main() {
  group('forge_client behaviour forge_client_offline relies on', () {
    test('memoryStorage enqueues a record once and updateState overwrites its state', () async {
      final session = await memoryStorage().open('alice');
      await session.enqueue(_record('a', '1'));
      await session.enqueue(_record('b', '2'));
      await expectLater(session.enqueue(_record('a', '3')), throwsStateError);

      await session.updateState('a', '{"kind":"sending","at":1}');
      await session.updateState(
        'a',
        '{"kind":"failed","failure":{"kind":"gone","status":410}}',
      );

      final records = await session.readOutbox();
      expect(records.map((r) => r.id), ['a', 'b']);
      expect(records.first.argsJson, '1');
      expect(
        records.first.stateJson,
        '{"kind":"failed","failure":{"kind":"gone","status":410}}',
      );
      expect(records[1].stateJson, '{"kind":"queued"}');
    });

    test(
      'memoryStorage shares records across sessions of one principal only',
      () async {
        final storage = memoryStorage();
        final alice = await storage.open('alice');
        await alice.enqueue(_record('a', '1'));
        await alice.close();

        expect(await (await storage.open('alice')).readOutbox(), hasLength(1));
        expect(await (await storage.open('bob')).readOutbox(), isEmpty);
      },
    );

    test('MutateOptions.headers reach the transport', () async {
      final transport = _Recording();
      final cache = QueryCache(transport: transport, entities: _entities);

      await cache.mutate(
        _patch,
        const TagContext(path: {'id': '7'}, body: {'total': 1}),
        options: const MutateOptions(headers: {'Idempotency-Key': 'k'}),
      );

      expect(transport.requests.single.headers['Idempotency-Key'], 'k');
    });

    test('targetOf reads the entity a write invalidates', () {
      expect(targetOf(_patch, _seven), 'Order:7');
    });

    test('commits fires after a fetch settles', () async {
      final cache = QueryCache(transport: _Recording(), entities: _entities);
      final commit = cache.commits.first;

      await cache.fetch(_get, _seven);
      await commit.timeout(const Duration(seconds: 1));
    });

    test(
      'hydrate with stale: true serves data and refetches when watched',
      () async {
        final source = QueryCache(transport: _Recording(), entities: _entities)
          ..setPrincipal('alice');
        await source.fetch(_get, _seven);
        final snapshot = dehydrate(source, principal: 'alice');

        final transport = _Recording()..total = 99;
        final target = QueryCache(transport: transport, entities: _entities)
          ..setPrincipal('alice');
        hydrate(
          target,
          snapshot,
          principal: 'alice',
          operations: const {'op_get_order': _get},
          stale: true,
        );

        final data =
            target.getState(_get, _seven).dataOrNull! as Map<String, Object?>;
        expect(data['total'], 10);
        expect(transport.requests, isEmpty);

        final sub = target.watch(_get, _seven).listen((_) {});
        await Future<void>.delayed(const Duration(milliseconds: 50));
        await sub.cancel();

        expect(transport.requests, hasLength(1));
      },
    );

    test('the cache opens a session per principal and announces it', () async {
      final cache = QueryCache(
        transport: _Recording(),
        entities: _entities,
        storage: memoryStorage(),
      );

      // sessionChanges is a synchronous broadcast stream, and closing a session
      // announces null from inside setPrincipal. Subscribe before either call
      // and record, because a listener attached afterwards misses the event.
      final announced = <StorageSession?>[];
      final opened = Completer<StorageSession>();
      final sub = cache.sessionChanges.listen((session) {
        announced.add(session);
        if (session != null && !opened.isCompleted) opened.complete(session);
      });

      cache.setPrincipal('alice');
      final session = await opened.future.timeout(const Duration(seconds: 1));
      expect(session.principal, 'alice');
      expect(identical(cache.session, session), isTrue);
      expect(announced, hasLength(1));

      cache.setPrincipal(null);
      expect(cache.session, isNull);
      expect(announced, hasLength(2));
      expect(announced.last, isNull);

      await Future<void>.delayed(Duration.zero);
      await sub.cancel();
      expect(announced.whereType<StorageSession>().single.principal, 'alice');
    });

    test('ManualClock advances by the duration given', () {
      final clock = ManualClock();
      final start = clock.now();
      clock.advance(const Duration(seconds: 2));

      expect(clock.now() - start, 2000);
    });
  });
}
