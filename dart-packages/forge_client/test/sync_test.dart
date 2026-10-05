import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/schema.dart';

const _update = OperationMeta(
  id: 'op_update_document',
  method: 'PATCH',
  path: '/documents/{id}',
  entity: 'Document',
);

String _describe(MutationOutcome outcome) => switch (outcome) {
  Applied(:final response) => 'applied $response',
  Queued(:final mutationId) => 'queued $mutationId',
  Rejected(:final error) => 'rejected $error',
};

/// A source that records what it was started with and answers from a script.
final class _RecordingSource implements SyncSource {
  _RecordingSource(this._outcome);

  final MutationOutcome _outcome;

  SyncContext? started;
  PendingMutation? applied;
  var stopped = false;

  @override
  Set<String> get entities => const {'Document'};

  @override
  Future<void> start(SyncContext context) async => started = context;

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async {
    applied = mutation;

    return _outcome;
  }

  @override
  Stream<SyncStatus> status(String entity) =>
      Stream.value(const Pending(1)).asBroadcastStream();

  @override
  Future<void> stop() async => stopped = true;
}

void main() {
  group('foldSyncStatus', () {
    test(
      'folds to Synced when every entity is synced, and for no entities',
      () {
        expect(foldSyncStatus(const [Synced(), Synced()]), const Synced());
        expect(foldSyncStatus(const []), const Synced());
      },
    );

    test('sums the pending counts', () {
      expect(
        foldSyncStatus(const [Pending(2), Synced(), Pending(3)]),
        const Pending(5),
      );
    });

    test('puts Offline above Pending', () {
      expect(foldSyncStatus(const [Pending(2), Offline()]), const Offline());
    });

    test('puts SyncFailed above everything, keeping the first failure', () {
      final first = SyncFailed(StateError('first'));
      final second = SyncFailed(StateError('second'));

      final folded = foldSyncStatus([
        const Offline(),
        first,
        const Pending(1),
        second,
      ]);

      expect(folded, first);
      expect(folded, isNot(second));
    });

    test('compares statuses by value', () {
      final error = StateError('x');

      expect(const Pending(2), const Pending(2));
      expect(const Pending(2).hashCode, const Pending(2).hashCode);
      expect(const Pending(2), isNot(const Pending(3)));
      expect(SyncFailed(error), SyncFailed(error));
      expect(SyncFailed(error), isNot(SyncFailed(StateError('x'))));
    });

    test('prints each status', () {
      expect(const Synced().toString(), 'Synced()');
      expect(const Pending(2).toString(), 'Pending(2)');
      expect(const Offline().toString(), 'Offline()');
      expect(const SyncFailed('boom').toString(), 'SyncFailed(boom)');
    });
  });

  group('mutations', () {
    test(
      'carries an operation, its arguments and a stable idempotency key',
      () {
        final mutation = PendingMutation(
          id: 'm-1',
          meta: _update,
          args: const TagContext(path: {'id': 'd1'}),
          optimistic: null,
          idempotencyKey: 'k-1',
          createdAt: DateTime.utc(2026, 10, 4),
        );

        expect(mutation.id, 'm-1');
        expect(mutation.meta.id, 'op_update_document');
        expect(mutation.args.path, {'id': 'd1'});
        expect(mutation.optimistic, isNull);
        expect(mutation.idempotencyKey, 'k-1');
        expect(mutation.createdAt, DateTime.utc(2026, 10, 4));
      },
    );

    test('names every outcome exhaustively', () {
      expect(_describe(const Applied({'id': 1})), 'applied {id: 1}');
      expect(_describe(const Queued('m-1')), 'queued m-1');
      expect(_describe(Rejected(StateError('no'))), 'rejected Bad state: no');
    });

    test('compares outcomes by value', () {
      final error = StateError('no');

      expect(const Applied('a'), const Applied('a'));
      expect(const Applied('a').hashCode, const Applied('a').hashCode);
      expect(const Applied('a'), isNot(const Applied('b')));
      expect(const Applied(null), const Applied(null));
      expect(const Queued('m-1'), const Queued('m-1'));
      expect(const Queued('m-1').hashCode, const Queued('m-1').hashCode);
      expect(const Queued('m-1'), isNot(const Queued('m-2')));
      expect(Rejected(error), Rejected(error));
      expect(Rejected(error).hashCode, Rejected(error).hashCode);
      expect(Rejected(error), isNot(Rejected(StateError('no'))));

      // The same payload under a different outcome is a different outcome.
      expect(const Applied('m-1'), isNot(const Queued('m-1')));
      expect(const Queued('m-1'), isNot(const Rejected('m-1')));
    });

    test('prints each outcome', () {
      expect(const Applied(1).toString(), 'Applied(1)');
      expect(const Queued('m-1').toString(), 'Queued(m-1)');
      expect(const Rejected('no').toString(), 'Rejected(no)');
    });
  });

  group('SyncSource', () {
    final mutation = PendingMutation(
      id: 'm-1',
      meta: _update,
      args: TagContext.empty,
      optimistic: null,
      idempotencyKey: 'k-1',
      createdAt: DateTime.utc(2026, 10, 4),
    );

    test(
      'is started with the cache, principal, transport and session',
      () async {
        final cache = QueryCache(
          transport: ScriptedTransport(const []),
          entities: schema,
        );
        final transport = ScriptedTransport(const []);
        final session = await memoryStorage().open('alice');
        addTearDown(session.close);

        final source = _RecordingSource(const Queued('m-1'));
        final context = SyncContext(
          cache: cache,
          principal: 'alice',
          transport: transport,
          storage: session,
        );

        await source.start(context);

        final started = source.started!;

        expect(started.cache, same(cache));
        expect(started.principal, 'alice');
        expect(started.transport, same(transport));
        expect(started.storage, same(session));
      },
    );

    test(
      'is started without a session when the cache has no storage',
      () async {
        final source = _RecordingSource(const Queued('m-1'));

        await source.start(
          SyncContext(
            cache: QueryCache(
              transport: ScriptedTransport(const []),
              entities: schema,
            ),
            principal: 'alice',
            transport: ScriptedTransport(const []),
            storage: null,
          ),
        );

        expect(source.started!.storage, isNull);
      },
    );

    test('takes a mutation and answers with one of the outcomes', () async {
      final source = _RecordingSource(const Queued('m-1'));

      final outcome = await source.apply(mutation);

      expect(outcome, const Queued('m-1'));
      expect(source.applied!.idempotencyKey, 'k-1');
      expect(source.entities, {'Document'});
      expect(await source.status('Document').first, const Pending(1));

      await source.stop();

      expect(source.stopped, isTrue);
    });
  });

  test('declares where an entity syncs', () {
    const declaration = SyncDeclaration(
      protocol: 'grove-crdt',
      entity: 'Document',
      table: 'documents',
      pull: '/datasets/{id}/sync/pull',
      push: '/datasets/{id}/sync/push',
      stream: '/datasets/{id}/sync/stream',
      dataset: '{id}',
    );

    expect(declaration.socket, isNull);
    expect(declaration.protocol, 'grove-crdt');
    expect(declaration.entity, 'Document');
    expect(declaration.table, 'documents');
    expect(declaration.pull, '/datasets/{id}/sync/pull');
    expect(declaration.push, '/datasets/{id}/sync/push');
    expect(declaration.stream, '/datasets/{id}/sync/stream');
    expect(declaration.dataset, '{id}');

    // One table per dataset, named at runtime.
    const perDataset = SyncDeclaration(
      protocol: 'grove-crdt',
      entity: 'Document',
      pull: '/datasets/{id}/sync/pull',
      push: '/datasets/{id}/sync/push',
    );

    expect(perDataset.table, isNull);
    expect(perDataset.stream, isNull);
    expect(perDataset.dataset, isNull);
  });
}
