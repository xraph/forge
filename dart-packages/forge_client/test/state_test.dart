// New in Dart: state.dart, sync.dart and observe.dart, which have no
// TypeScript suite of their own.
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

String describe(CacheEvent event) => switch (event) {
  QueryTransition(:final key) => 'query $key',
  MutationCommitted(:final meta) => 'mutation ${meta.id}',
  FramesCommitted(:final count) => 'frames $count',
  QueryInvalidated(:final key) => 'invalidated $key',
  QueryPlaced(:final key) => 'placed $key',
  OutboxEnqueued(:final mutationId) => 'enqueued $mutationId',
  OutboxReplayed(:final mutationId) => 'replayed $mutationId',
  OutboxFailed(:final mutationId) => 'failed $mutationId',
  SyncStatusChanged(:final entity) => 'sync $entity',
};

void main() {
  group('QueryState', () {
    test('carries data only where there is some', () {
      expect(const QueryIdle<int>().dataOrNull, isNull);
      expect(const QueryLoading<int>(isFetching: true).dataOrNull, isNull);
      expect(const QuerySuccess<int>(7).dataOrNull, 7);
      expect(const QueryFailure<int>('boom', previous: 6).dataOrNull, 6);
      expect(const QuerySuccess<int>(7).syncStatus, isA<Synced>());
    });
  });

  group('foldSyncStatus', () {
    test(
      'ranks failure over offline over pending over synced, summing pending',
      () {
        expect(foldSyncStatus(const []), isA<Synced>());
        expect(
          foldSyncStatus(const [Synced(), Pending(2), Pending(3)]),
          isA<Pending>().having((s) => s.count, 'count', 5),
        );
        expect(foldSyncStatus(const [Pending(2), Offline()]), isA<Offline>());
        expect(
          foldSyncStatus(const [Offline(), SyncFailed('a'), SyncFailed('b')]),
          isA<SyncFailed>().having((s) => s.error, 'error', 'a'),
        );
      },
    );
  });

  group('SyncStatus equality', () {
    test('compares every status by value', () {
      expect(Pending(int.parse('1')), const Pending(1));
      expect(Pending(int.parse('1')).hashCode, const Pending(1).hashCode);
      expect(const Pending(1), isNot(const Pending(2)));
      expect(SyncFailed('a' * 2), const SyncFailed('aa'));
      expect(SyncFailed('a' * 2).hashCode, const SyncFailed('aa').hashCode);
      expect(const Synced(), const Synced());
      expect(const Offline(), const Offline());
      expect(const Synced(), isNot(const Offline()));
    });

    test('folds equal inputs to equal statuses', () {
      List<SyncStatus> inputs() => [
        const Synced(),
        Pending(int.parse('2')),
        Pending(int.parse('3')),
      ];

      expect(foldSyncStatus(inputs()), foldSyncStatus(inputs()));
      expect(foldSyncStatus(inputs()), const Pending(5));
      expect(
        foldSyncStatus([SyncFailed('x' * 2)]),
        foldSyncStatus([SyncFailed('x' * 2)]),
      );
      expect(foldSyncStatus([const Offline()]), const Offline());
      expect(foldSyncStatus(const []), const Synced());
    });
  });

  group('CacheEvent', () {
    test('is a closed set an observer can switch over exhaustively', () {
      expect(describe(const QueryPlaced(key: 'k')), 'placed k');
      expect(
        describe(
          const SyncStatusChanged(entity: 'Document', status: Offline()),
        ),
        'sync Document',
      );
      expect(
        describe(
          const OutboxFailed(mutationId: 'm', operationId: 'o', failure: 'x'),
        ),
        'failed m',
      );
    });
  });

  group('devtools hooks', () {
    test('can be implemented by downstream packages', () async {
      final inspector = _Inspector();

      await inspector.replay('m-1');
      await inspector.discard('m-2');

      expect(inspector.calls, ['replay m-1', 'discard m-2']);
      expect(await inspector.describeForDevtools(), {'pending': 0});
    });

    test('accepts a narrower failure stream, by Stream covariance', () {
      expect(_Failures().failures, isA<Stream<Object>>());
    });
  });
}

final class _Inspector implements OutboxInspector, DevtoolsInspectable {
  final List<String> calls = [];

  @override
  Future<void> replay(String mutationId) async =>
      calls.add('replay $mutationId');

  @override
  Future<void> discard(String mutationId) async =>
      calls.add('discard $mutationId');

  @override
  Future<Map<String, Object?>> describeForDevtools() async => {'pending': 0};
}

final class _Failures implements OutboxFailureSource {
  @override
  Stream<String> get failures => const Stream<String>.empty();
}
