import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/harness.dart';
import 'support/schema.dart';

/// Every 01a member plan 01b calls, exercised once, so a later 01b task never
/// fails for a reason outside its own code.
void main() {
  test(
    'the 01a surface the stream, snapshot and sync layers consume',
    () async {
      final transport = FakeTransport(
        (request, call) => [
          {
            'id': 7,
            'total': 99,
            'customer': {'id': 'c-3', 'name': 'Ada'},
          },
        ],
      );
      final errors = <String>[];
      final cache = QueryCache(
        transport: transport,
        entities: schema,
        scheduler: ManualScheduler(),
        commitScheduler: microtaskCommitScheduler(),
        clock: const FixedClock(1000),
        onError: (error, context) => errors.add(context),
      );

      final principals = <String?>[];
      cache.principalChanges.listen(principals.add);
      cache.setPrincipal('u-1');
      expect(cache.principal, 'u-1');
      expect(principals, ['u-1']);

      // The first watch event arrives on a microtask, never inside listen.
      final states = <QueryState<Object?>>[];
      final watching = cache
          .watch(orderList, TagContext.empty)
          .listen(states.add);
      expect(states, isEmpty);
      await settle();
      expect(states, isNotEmpty);

      expect(
        cache.getState(orderList, TagContext.empty),
        isA<QuerySuccess<Object?>>(),
      );
      expect(
        cache.getState(orderList, TagContext.empty).syncStatus,
        const Synced(),
      );
      expect(cache.size, 1);
      expect(cache.settledTimeOf(orderList, TagContext.empty), 1000);
      expect(cache.key(orderList, TagContext.empty), 'GET /orders');
      expect(queryKey(orderList, TagContext.empty), 'GET /orders');
      expect(operationName(orderList), 'GET /orders');

      final settled = cache.queries.single;
      expect(settled.args, TagContext.empty);
      expect(settled.meta, same(orderList));
      expect(settled.settledTime, 1000);

      final entry = cache.registry.get(settled.key);
      expect(entry?.tags, contains('Order[]'));
      expect(entry?.stale, isFalse);
      expect(entry?.deps, contains('Order:7'));
      expect(cache.registry.queriesFor('Order[]'), isNotEmpty);
      expect(cache.registry.mounted, 1);

      final store = cache.store;
      final stamp = store.nextFrame();
      expect(store.frameVersion, stamp);
      final written = store.write(
        {'id': 9, 'total': 1},
        cache.entities,
        'Order',
        CommitOptions(frameAt: stamp),
      );
      expect(written.skeleton, isA<EntityRef>());
      expect(store.getRecord('Order:9')?.frameAt, stamp);
      expect(store.put('Order:10', {'id': 10}), isTrue);
      expect(store.evict('Order:10', stamp), isTrue);
      expect(store.has('Order:10'), isFalse);
      expect(store.keys, contains('Order:7'));
      expect(store.read(settled.skeleton), isA<List<Object?>>());
      expect(store.frameStamp('Order:9'), stamp);
      expect(store.racedSince(['Order:9'], stamp - 1), ['Order:9']);
      store.commit(
        store.stage({'id': 11}, cache.entities, 'Order'),
        const CommitOptions(skip: {'Order:11'}),
      );
      expect(store.has('Order:11'), isFalse);

      cache.restore(
        const RestoreInput(
          meta: orderGet,
          args: TagContext(path: {'id': 7}),
          skeleton: EntityRef('Order:7'),
          tags: ['Order:7'],
          stale: true,
          settledTime: 500,
        ),
      );
      expect(
        cache.settledTimeOf(orderGet, const TagContext(path: {'id': 7})),
        500,
      );

      cache.invalidate(['Order[]']);
      cache.notifyChanged();
      cache.report(StateError('probe'), 'probe');
      expect(errors, contains('probe'));

      expect(cache.live, isNull);
      final release = cache.watchLive(orderList, TagContext.empty);
      release();
      expect(errors, contains('live'));
      expect(cache.commitScheduler, isA<CommitScheduler>());

      final frames = <FramesCommitted>[];
      cache.observer = (event) {
        if (event is FramesCommitted) frames.add(event);
      };
      cache.observer?.call(
        const FramesCommitted(
          count: 1,
          tags: {'Order[]'},
          frames: [
            StreamFrame(
              binding: EntityStreamBinding(
                channel: '/ws/orders',
                message: 'order.updated',
                entity: 'Order',
                intent: StreamIntent.patch,
              ),
              payload: {'id': 7},
            ),
          ],
        ),
      );
      expect(frames.single.frames.single.binding.message, 'order.updated');
      cache.observer = null;

      const options = SubscribeOptions(hello: {'a': 1});
      expect(options.goodbye, isNull);
      void handle(Object? message, String channel) {}
      final FrameHandler handler = handle;
      handler(null, '/ws');
      const duplex = DuplexStreamBinding(
        channel: '/ws',
        send: 'a',
        receive: 'b',
      );
      expect(duplex.receive, 'b');

      final marked = markRewritten(<String, Object?>{
        'a': const EntityRef('Order:7'),
      });
      expect(isRewritten(marked), isTrue);
      expect(isRef(const EntityRef('Order:7')), isTrue);
      expect(isIdentity(7), isTrue);
      expect(entityKey('Order', 7), 'Order:7');

      final normalized = normalize(
        {
          'id': 1,
          'customer': {'id': 'c', 'name': 'x'},
        },
        'Order',
        schema,
      );
      expect(normalized.records.keys, containsAll(['Order:1', 'Customer:c']));

      expect(
        resolveTags(['Order:{id}'], const TagContext(path: {'id': 7})).tags,
        ['Order:7'],
      );
      expect(foldSyncStatus(const [Pending(1), Pending(2)]), isA<Pending>());

      final Sleep sleep = realSleep;
      await sleep(Duration.zero);
      expect(microtaskScheduler(), isA<Scheduler>());
      expect(const HttpStatusError(500, null).status, 500);

      await watching.cancel();
      await cache.dispose();
    },
  );
}
