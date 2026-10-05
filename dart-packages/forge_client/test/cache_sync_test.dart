import 'dart:async';

import 'package:fake_async/fake_async.dart';
import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/harness.dart';

const _schema = <String, EntityMeta>{
  'Document': EntityMeta(idField: 'id'),
  'Folder': EntityMeta(idField: 'id', fields: {'documents': 'Document'}),
};

const _documentList = OperationMeta(
  id: 'op_document_list',
  method: 'GET',
  path: '/documents',
  entity: 'Document',
  provides: ['Document[]'],
);

const _folderGet = OperationMeta(
  id: 'op_folder_get',
  method: 'GET',
  path: '/folders/{id}',
  entity: 'Folder',
  provides: ['Folder:{id}'],
);

const _documentUpdate = OperationMeta(
  id: 'op_document_update',
  method: 'PATCH',
  path: '/documents/{id}',
  entity: 'Document',
  invalidates: ['Document[]'],
);

const _folderUpdate = OperationMeta(
  id: 'op_folder_update',
  method: 'PATCH',
  path: '/folders/{id}',
  entity: 'Folder',
);

/// A REST mutation on a plain entity that declares an owned entity's list, so
/// a placement callback can hand the cache owned records.
const _folderFile = OperationMeta(
  id: 'op_folder_file',
  method: 'POST',
  path: '/folders/{id}/file',
  entity: 'Folder',
  invalidates: ['Document[]'],
);

const _d1 = TagContext(path: {'id': 'd1'});
const _f1 = TagContext(path: {'id': 'f1'});

const _folderFrame = EntityStreamBinding(
  channel: '/ws/folders',
  message: 'folder.updated',
  entity: 'Folder',
  intent: StreamIntent.upsert,
);

/// A sync source the test drives: it logs its lifecycle into a shared log,
/// records what it is asked to apply, and emits whatever status it is told to.
final class _FakeSource implements SyncSource {
  _FakeSource(this.entities, {List<String>? log, this.onStart, this.onStop})
    : log = log ?? [];

  @override
  final Set<String> entities;

  final List<String> log;
  final void Function(SyncContext context)? onStart;
  final void Function()? onStop;

  /// While set, [start] waits for it before it returns.
  Future<void>? startGate;
  final List<PendingMutation> applied = [];
  FutureOr<MutationOutcome> Function(PendingMutation mutation) outcome = (
    mutation,
  ) => Queued(mutation.id);
  SyncContext? context;
  final Map<String, StreamController<SyncStatus>> _status = {};

  @override
  Future<void> start(SyncContext context) async {
    this.context = context;
    log.add('start ${context.principal}');
    onStart?.call(context);

    final gate = startGate;

    if (gate != null) await gate;
  }

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async {
    applied.add(mutation);

    return outcome(mutation);
  }

  @override
  Stream<SyncStatus> status(String entity) =>
      (_status[entity] ??= StreamController<SyncStatus>.broadcast()).stream;

  void emit(String entity, SyncStatus status) => _status[entity]?.add(status);

  @override
  Future<void> stop() async {
    log.add('stop ${context?.principal}');
    onStop?.call();
  }
}

/// memoryStorage, logging every open and close into the shared log.
final class _LoggingStorage implements StorageAdapter {
  _LoggingStorage(this.log, {this.refuse = const {}});

  final List<String> log;

  /// Principals whose storage fails to open.
  final Set<String> refuse;
  final StorageAdapter _inner = memoryStorage();

  @override
  Future<StorageSession> open(String principal) async {
    log.add('open $principal');

    if (refuse.contains(principal)) {
      throw StateError('no storage for $principal');
    }

    return _LoggingSession(await _inner.open(principal), log);
  }

  @override
  Future<void> destroy(String principal) => _inner.destroy(principal);
}

final class _LoggingSession implements StorageSession {
  _LoggingSession(this._inner, this._log);

  final StorageSession _inner;
  final List<String> _log;

  @override
  String get principal => _inner.principal;

  @override
  Future<Snapshot?> readSnapshot() => _inner.readSnapshot();

  @override
  Future<void> writeSnapshot(Snapshot snapshot) =>
      _inner.writeSnapshot(snapshot);

  @override
  Future<List<PendingMutationRecord>> readOutbox() => _inner.readOutbox();

  @override
  Future<void> enqueue(PendingMutationRecord record) => _inner.enqueue(record);

  @override
  Future<void> remove(String mutationId) => _inner.remove(mutationId);

  @override
  Future<void> updateState(String mutationId, String stateJson) =>
      _inner.updateState(mutationId, stateJson);

  @override
  KeyValueStore namespace(String name) => _inner.namespace(name);

  @override
  Future<void> close() async {
    _log.add('close $principal');
    await _inner.close();
  }
}

typedef _Kit = ({
  QueryCache cache,
  _FakeSource source,
  FakeTransport transport,
  List<String> log,
  ManualScheduler batches,
  List<(Object, String)> errors,
});

_Kit _build(
  FutureOr<Object?> Function(TransportRequest request, int call) handler, {
  void Function(SyncContext context)? onStart,
  void Function()? onStop,
  bool withStorage = true,
  Set<String> refuseStorage = const {},
  bool throwingErrorHandler = false,
}) {
  final log = <String>[];
  final errors = <(Object, String)>[];
  final source = _FakeSource(
    {'Document'},
    log: log,
    onStart: onStart,
    onStop: onStop,
  );
  final transport = FakeTransport(handler);
  final batches = ManualScheduler();
  final cache = QueryCache(
    transport: transport,
    entities: _schema,
    scheduler: batches,
    syncSources: [source],
    storage: withStorage ? _LoggingStorage(log, refuse: refuseStorage) : null,
    onError: (error, context) {
      errors.add((error, context));

      if (throwingErrorHandler) throw StateError('the handler broke too');
    },
  );

  return (
    cache: cache,
    source: source,
    transport: transport,
    log: log,
    batches: batches,
    errors: errors,
  );
}

PendingMutationRecord _record(String id) => PendingMutationRecord(
  id: id,
  operationId: 'op_document_update',
  argsJson: '{}',
  idempotencyKey: 'k-$id',
  createdAt: DateTime.utc(2026, 10, 4),
);

void _putReplica(SyncContext context) => context.cache.store.put(
  'Document:d1',
  {'id': 'd1', 'title': 'from the replica'},
);

void main() {
  group('principal scoping', () {
    test('opens the principal’s session, then starts sources with it', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        expect(kit.log, ['open alice', 'start alice']);
        expect(kit.cache.session?.principal, 'alice');
        expect(kit.source.context?.cache, same(kit.cache));
        expect(kit.source.context?.transport, same(kit.transport));
        expect(kit.source.context?.storage, same(kit.cache.session));
      });
    });

    test('opens nothing and starts nothing while the principal is null', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);

        async.flushMicrotasks();

        expect(kit.log, isEmpty);
        expect(kit.cache.session, isNull);
      });
    });

    test('stops the old sources and closes the old session before opening the next', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();
        kit.cache.setPrincipal('bob');
        async.flushMicrotasks();

        expect(kit.log, [
          'open alice',
          'start alice',
          'stop alice',
          'close alice',
          'open bob',
          'start bob',
        ]);
        expect(kit.cache.session?.principal, 'bob');
      });
    });

    test('gives each principal a fresh session and never reopens the old one for the next', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        final alice = kit.cache.session!;
        unawaited(alice.enqueue(_record('m-1')));
        async.flushMicrotasks();

        kit.cache.setPrincipal('bob');
        async.flushMicrotasks();

        final bob = kit.cache.session!;
        List<PendingMutationRecord>? bobsOutbox;
        unawaited(bob.readOutbox().then((records) => bobsOutbox = records));
        Object? aliceAfter;
        unawaited(
          alice.readOutbox().catchError((Object error) {
            aliceAfter = error;

            return <PendingMutationRecord>[];
          }),
        );
        async.flushMicrotasks();

        expect(identical(bob, alice), isFalse);
        expect(bobsOutbox, isEmpty);
        expect(aliceAfter, isA<StateError>());
      });
    });

    test('stops every source and closes the session on sign-out', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();
        kit.cache.setPrincipal(null);
        async.flushMicrotasks();

        expect(kit.log, [
          'open alice',
          'start alice',
          'stop alice',
          'close alice',
        ]);
        expect(kit.cache.session, isNull);
      });
    });

    test(
      'never opens or starts anything for a principal replaced before it began',
      () {
        fakeAsync((async) {
          final kit = _build((_, _) => null);

          kit.cache.setPrincipal('alice');
          kit.cache.setPrincipal('bob');
          async.flushMicrotasks();

          expect(kit.log, ['open bob', 'start bob']);
        });
      },
    );

    test('reports each session as it opens and closes', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);
        final seen = <String?>[];

        kit.cache.sessionChanges.listen(
          (session) => seen.add(session?.principal),
        );

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();
        kit.cache.setPrincipal('bob');
        async.flushMicrotasks();

        expect(seen, ['alice', null, 'bob']);
      });
    });

    test('starts sources with no session when the cache has no storage', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null, withStorage: false);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        expect(kit.log, ['start alice']);
        expect(kit.cache.session, isNull);
        expect(kit.source.context?.storage, isNull);
      });
    });

    test('stops sources and closes the session on dispose', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();
        unawaited(kit.cache.dispose());
        async.flushMicrotasks();

        expect(kit.log, [
          'open alice',
          'start alice',
          'stop alice',
          'close alice',
        ]);
        expect(kit.cache.session, isNull);
      });
    });

    test('refuses two sources that own the same entity', () {
      expect(
        () => QueryCache(
          transport: FakeTransport((_, _) => null),
          entities: _schema,
          syncSources: [
            _FakeSource({'Document'}),
            _FakeSource({'Document', 'Folder'}),
          ],
        ),
        throwsArgumentError,
      );
    });

    test('keeps the session and the sources across a clear', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        final session = kit.cache.session;

        kit.cache.clear();
        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        expect(kit.log, ['open alice', 'start alice']);
        expect(kit.cache.session, same(session));
      });
    });

    test('runs one transition at a time, even when a source starts slowly', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);
        final gate = Completer<void>();
        kit.source.startGate = gate.future;

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();
        expect(kit.log, ['open alice', 'start alice']);

        // Bob arrives while alice's source is still starting: nothing of his
        // may begin until alice's start has finished, been stopped and her
        // session closed.
        kit.cache.setPrincipal('bob');
        async.flushMicrotasks();
        expect(kit.log, ['open alice', 'start alice']);
        expect(kit.cache.session, isNull);

        kit.source.startGate = null;
        gate.complete();
        async.flushMicrotasks();

        expect(kit.log, [
          'open alice',
          'start alice',
          'stop alice',
          'close alice',
          'open bob',
          'start bob',
        ]);
        expect(kit.cache.session?.principal, 'bob');
      });
    });

    test('drops a pre-clear invalidation instead of refetching the next principal’s query', () {
      fakeAsync((async) {
        final kit = _build((_, _) => {'id': 'f1', 'name': 'Inbox'});

        kit.cache.setPrincipal('alice');
        kit.cache.watch(_folderGet, _f1).listen((_) {});
        async.flushMicrotasks();
        expect(kit.transport.calls, hasLength(1));

        // Queued in the invalidator, whose batch has not run yet.
        kit.cache.invalidate(['Folder:f1']);
        expect(kit.batches.pending, isTrue);

        kit.cache.setPrincipal('bob');
        async.flushMicrotasks();
        expect(kit.transport.calls, hasLength(2));

        // The batch runs under bob. The entry it holds was alice's.
        kit.batches.flush();
        async.flushMicrotasks();

        expect(kit.transport.calls, hasLength(2));
      });
    });
  });

  group('lifecycle failures', () {
    test('reports a source that fails to start and keeps the cache usable', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => {'id': 'f1', 'name': 'Inbox'},
          onStart: (_) => throw StateError('cannot start'),
        );

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        expect(kit.errors.single.$2, 'sync');
        expect(kit.cache.session?.principal, 'alice');

        kit.cache.watch(_folderGet, _f1).listen((_) {});
        async.flushMicrotasks();
        expect(kit.cache.getState(_folderGet, _f1).dataOrNull, {
          'id': 'f1',
          'name': 'Inbox',
        });

        Object? caught;
        unawaited(
          kit.cache
              .mutate(_documentUpdate, _d1)
              .catchError((Object error) => caught = error),
        );
        async.flushMicrotasks();
        expect(caught, isA<StateError>());
        expect(kit.source.applied, isEmpty);

        // The session is still the cache's, so it is closed, not leaked.
        kit.cache.setPrincipal(null);
        async.flushMicrotasks();

        expect(kit.log, ['open alice', 'start alice', 'close alice']);
        expect(kit.cache.session, isNull);
      });
    });

    test('reports a source that fails to stop and still moves on', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => null,
          onStop: () => throw StateError('cannot stop'),
        );

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();
        kit.cache.setPrincipal('bob');
        async.flushMicrotasks();

        expect(kit.errors.single.$2, 'sync');
        expect(kit.log, [
          'open alice',
          'start alice',
          'stop alice',
          'close alice',
          'open bob',
          'start bob',
        ]);
      });
    });

    test('starts nothing for a principal whose storage fails to open', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null, refuseStorage: {'alice'});

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        expect(kit.errors.single.$2, 'storage');
        expect(kit.log, ['open alice']);
        expect(kit.cache.session, isNull);

        kit.cache.setPrincipal('bob');
        async.flushMicrotasks();

        expect(kit.log, ['open alice', 'open bob', 'start bob']);
      });
    });

    test('closes the session even when the error handler throws', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => null,
          onStart: (_) => throw StateError('cannot start'),
          throwingErrorHandler: true,
        );

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();
        kit.cache.setPrincipal(null);
        async.flushMicrotasks();

        expect(kit.log, ['open alice', 'start alice', 'close alice']);
      });
    });
  });

  group('mutations', () {
    test('routes a mutation on an owned entity to the sync source, not the transport', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);
        const optimistic = OptimisticDelete<Object?>();

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        Object? result = 'unset';
        unawaited(
          kit.cache
              .mutate(
                _documentUpdate,
                _d1,
                options: const MutateOptions(optimistic: optimistic),
              )
              .then((value) => result = value),
        );
        async.flushMicrotasks();

        expect(kit.transport.calls, isEmpty);
        expect(result, isNull);

        final mutation = kit.source.applied.single;
        expect(mutation.meta, same(_documentUpdate));
        expect(mutation.args.path, {'id': 'd1'});
        expect(mutation.optimistic, same(optimistic));
        expect(mutation.id, isNotEmpty);
        expect(mutation.idempotencyKey, isNotEmpty);
        expect(mutation.idempotencyKey, isNot(mutation.id));
      });
    });

    test(
      'returns an applied response and invalidates what the operation declares',
      () {
        fakeAsync((async) {
          final kit = _build(
            (_, _) => [
              {'id': 'd1', 'title': 'x'},
            ],
          );
          kit.source.outcome = (_) => const Applied({'id': 'd1', 'title': 'y'});

          kit.cache.setPrincipal('alice');
          kit.cache.watch(_documentList, TagContext.empty).listen((_) {});
          async.flushMicrotasks();
          expect(kit.transport.calls, hasLength(1));

          Object? result;
          unawaited(
            kit.cache
                .mutate(_documentUpdate, _d1)
                .then((value) => result = value),
          );
          async.flushMicrotasks();
          kit.batches.flush();
          async.flushMicrotasks();

          expect(result, {'id': 'd1', 'title': 'y'});
          expect(kit.transport.calls, hasLength(2));
        });
      },
    );

    test('throws the error of a rejected mutation', () {
      fakeAsync((async) {
        final kit = _build((_, _) => null);
        final refusal = StateError('nope');
        kit.source.outcome = (_) => Rejected(refusal);

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        Object? caught;
        unawaited(
          kit.cache
              .mutate(_documentUpdate, _d1)
              .catchError((Object error) => caught = error),
        );
        async.flushMicrotasks();

        expect(caught, same(refusal));
      });
    });

    test(
      'refuses a mutation on an owned entity while no source is running',
      () {
        fakeAsync((async) {
          final kit = _build((_, _) => null);

          Object? caught;
          unawaited(
            kit.cache
                .mutate(_documentUpdate, _d1)
                .catchError((Object error) => caught = error),
          );
          async.flushMicrotasks();

          expect(
            caught,
            isA<StateError>().having(
              (e) => e.message,
              'message',
              contains('not running'),
            ),
          );
          expect(kit.source.applied, isEmpty);
          expect(kit.transport.calls, isEmpty);
        });
      },
    );

    test(
      'sends a mutation on an entity no source owns through the transport',
      () {
        fakeAsync((async) {
          final kit = _build((_, _) => {'id': 'f1', 'name': 'Inbox'});

          kit.cache.setPrincipal('alice');
          async.flushMicrotasks();

          unawaited(kit.cache.mutate(_folderUpdate, _f1));
          async.flushMicrotasks();

          expect(kit.transport.calls, hasLength(1));
          expect(kit.source.applied, isEmpty);
        });
      },
    );

    test(
      'does not write an applied response into the next principal’s cache',
      () {
        fakeAsync((async) {
          final kit = _build(
            (_, _) => [
              {'id': 'd1', 'title': 'x'},
            ],
          );
          final applied = Completer<MutationOutcome>();
          kit.source.outcome = (_) => applied.future;

          kit.cache.setPrincipal('alice');
          async.flushMicrotasks();

          Object? result;
          unawaited(
            kit.cache
                .mutate(_documentUpdate, _d1)
                .then((value) => result = value),
          );
          async.flushMicrotasks();
          expect(kit.source.applied, hasLength(1));

          kit.cache.setPrincipal('bob');
          kit.cache.watch(_documentList, TagContext.empty).listen((_) {});
          async.flushMicrotasks();
          expect(kit.transport.calls, hasLength(1));

          // Alice's write lands under bob. It invalidates nothing of his.
          applied.complete(const Applied({'id': 'd1', 'title': 'y'}));
          async.flushMicrotasks();
          kit.batches.flush();
          async.flushMicrotasks();

          expect(result, {'id': 'd1', 'title': 'y'});
          expect(kit.transport.calls, hasLength(1));
        });
      },
    );

    test(
      'refuses a mutation whose principal changed before it reached the source',
      () {
        fakeAsync((async) {
          final kit = _build((_, _) => null);
          final gate = Completer<void>();
          kit.source.startGate = gate.future;

          // Alice's source is still starting, so the mutation waits for it.
          kit.cache.setPrincipal('alice');
          async.flushMicrotasks();

          Object? caught;
          unawaited(
            kit.cache
                .mutate(_documentUpdate, _d1)
                .catchError((Object error) => caught = error),
          );
          async.flushMicrotasks();

          // Bob signs in before alice's source finishes starting. When it
          // does, it is running, but it is alice's, and the mutation was too.
          kit.cache.setPrincipal('bob');
          kit.source.startGate = null;
          gate.complete();
          async.flushMicrotasks();

          expect(
            caught,
            isA<StateError>().having(
              (e) => e.message,
              'message',
              contains('principal changed'),
            ),
          );
          expect(kit.source.applied, isEmpty);
        });
      },
    );
  });

  group('owned records', () {
    test('never lets a REST response overwrite an owned record', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => {
            'id': 'f1',
            'name': 'Inbox',
            'documents': [
              {'id': 'd1', 'title': 'from REST'},
            ],
          },
          onStart: _putReplica,
        );

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        kit.cache.watch(_folderGet, _f1).listen((_) {});
        async.flushMicrotasks();

        expect(
          kit.cache.store.getRecord('Document:d1')?.data['title'],
          'from the replica',
        );
        expect(kit.cache.store.getRecord('Folder:f1')?.data['name'], 'Inbox');
        expect(kit.cache.getState(_folderGet, _f1).dataOrNull, {
          'id': 'f1',
          'name': 'Inbox',
          'documents': [
            {'id': 'd1', 'title': 'from the replica'},
          ],
        });
      });
    });

    test(
      'never lets a response a frame overtook overwrite an owned record',
      () {
        fakeAsync((async) {
          final first = Completer<Object?>();
          final kit = _build(
            (_, call) =>
                call == 0 ? first.future : {'id': 'f1', 'name': 'Inbox'},
            onStart: _putReplica,
          );

          kit.cache.setPrincipal('alice');
          async.flushMicrotasks();

          kit.cache.watch(_folderGet, _f1).listen((_) {});
          async.flushMicrotasks();

          // A frame overtakes the folder while its request is out, so the
          // answer commits around it and the query asks again.
          applyFrames(kit.cache, [
            const StreamFrame(
              binding: _folderFrame,
              payload: {'id': 'f1', 'name': 'From a frame'},
            ),
          ]);
          first.complete({
            'id': 'f1',
            'name': 'Inbox',
            'documents': [
              {'id': 'd1', 'title': 'from REST'},
            ],
          });
          async.flushMicrotasks();

          expect(kit.transport.calls, hasLength(2));
          expect(
            kit.cache.store.getRecord('Document:d1')?.data['title'],
            'from the replica',
          );
        });
      },
    );

    test('never lets a mutation response overwrite an owned record', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => {
            'id': 'f1',
            'name': 'Renamed',
            'documents': [
              {'id': 'd1', 'title': 'from REST'},
            ],
          },
          onStart: _putReplica,
        );

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        unawaited(kit.cache.mutate(_folderUpdate, _f1));
        async.flushMicrotasks();

        expect(kit.cache.store.getRecord('Folder:f1')?.data['name'], 'Renamed');
        expect(
          kit.cache.store.getRecord('Document:d1')?.data['title'],
          'from the replica',
        );
      });
    });

    test('never promotes an optimistic patch onto an owned record', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => {'id': 'f1', 'name': 'Renamed'},
          onStart: _putReplica,
        );

        kit.cache.setPrincipal('alice');
        async.flushMicrotasks();

        unawaited(
          kit.cache.mutate(
            _folderUpdate,
            _f1,
            options: MutateOptions(
              optimistic: OptimisticUpdate<Object?>(
                (previous) => {
                  ...previous! as Map<String, Object?>,
                  'title': 'optimistic',
                },
                key: 'Document:d1',
              ),
            ),
          ),
        );
        async.flushMicrotasks();

        expect(
          kit.cache.store.getRecord('Document:d1')?.data['title'],
          'from the replica',
        );
      });
    });

    test('never lets a placement callback overwrite an owned record', () {
      fakeAsync((async) {
        final kit = _build(
          (request, _) => request.meta == _documentList
              ? [
                  {'id': 'd1', 'title': 'from REST'},
                ]
              : {'id': 'f1', 'name': 'Inbox'},
          onStart: _putReplica,
        );

        kit.cache.setPrincipal('alice');
        kit.cache.watch(_documentList, TagContext.empty).listen((_) {});
        async.flushMicrotasks();

        unawaited(
          kit.cache.mutate(
            _folderFile,
            _f1,
            options: MutateOptions(
              place: {
                'Document[]': (created, current, args) => [
                  {'id': 'd1', 'title': 'placed'},
                ],
              },
            ),
          ),
        );
        async.flushMicrotasks();

        expect(
          kit.cache.store.getRecord('Document:d1')?.data['title'],
          'from the replica',
        );
      });
    });

    test('skips stream frames for an owned entity', () {
      final kit = _build((_, _) => null);

      applyFrames(kit.cache, [
        const StreamFrame(
          binding: EntityStreamBinding(
            channel: '/ws/documents',
            message: 'document.updated',
            entity: 'Document',
            intent: StreamIntent.upsert,
          ),
          payload: {'id': 'd1', 'title': 'from a frame'},
        ),
      ]);

      expect(kit.cache.store.has('Document:d1'), isFalse);
      expect(kit.cache.owns('Document'), isTrue);
      expect(kit.cache.owns('Folder'), isFalse);

      // Plan 05 projects a source's rows with store.write and store.evict; a
      // frame evicting an owned row must not delete it either.
      kit.cache.store.put('Document:d2', {
        'id': 'd2',
        'title': 'from the replica',
      });
      applyFrames(kit.cache, [
        const StreamFrame(
          binding: EntityStreamBinding(
            channel: '/ws/documents',
            message: 'document.deleted',
            entity: 'Document',
            intent: StreamIntent.evict,
          ),
          payload: 'd2',
        ),
      ]);

      expect(
        kit.cache.store.getRecord('Document:d2')?.data['title'],
        'from the replica',
      );
    });

    test('skips owned records nested in a frame for a plain entity', () {
      final kit = _build((_, _) => null);

      kit.cache.store.put('Document:d1', {
        'id': 'd1',
        'title': 'from the replica',
      });
      applyFrames(kit.cache, [
        const StreamFrame(
          binding: _folderFrame,
          payload: {
            'id': 'f1',
            'name': 'Inbox',
            'documents': [
              {'id': 'd1', 'title': 'from a frame'},
            ],
          },
        ),
      ]);

      expect(kit.cache.store.getRecord('Folder:f1')?.data['name'], 'Inbox');
      expect(
        kit.cache.store.getRecord('Document:d1')?.data['title'],
        'from the replica',
      );
    });
  });

  group('sync status', () {
    test('folds sync status across the entities a query touches', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => {
            'id': 'f1',
            'name': 'Inbox',
            'documents': [
              {'id': 'd1', 'title': 'a'},
            ],
          },
        );

        kit.cache.setPrincipal('alice');
        kit.cache.watch(_folderGet, _f1).listen((_) {});
        async.flushMicrotasks();

        expect(kit.cache.getState(_folderGet, _f1).syncStatus, const Synced());

        kit.source.emit('Document', const Pending(2));
        async.flushMicrotasks();
        expect(
          kit.cache.getState(_folderGet, _f1).syncStatus,
          const Pending(2),
        );

        final failure = SyncFailed(StateError('hook rejected'));
        kit.source.emit('Document', failure);
        async.flushMicrotasks();
        expect(kit.cache.getState(_folderGet, _f1).syncStatus, failure);
      });
    });

    test('notifies watchers when an owned entity’s status changes', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => [
            {'id': 'd1', 'title': 'a'},
          ],
        );
        final seen = <SyncStatus>[];

        kit.cache.setPrincipal('alice');
        kit.cache
            .watch(_documentList, TagContext.empty)
            .listen((state) => seen.add(state.syncStatus));
        async.flushMicrotasks();

        kit.source.emit('Document', const Offline());
        async.flushMicrotasks();

        expect(seen.last, const Offline());
      });
    });

    test('reports Synced for a query that touches no owned entity', () {
      fakeAsync((async) {
        final kit = _build((_, _) => {'id': 'f1', 'name': 'Inbox'});

        kit.cache.setPrincipal('alice');
        kit.cache.watch(_folderGet, _f1).listen((_) {});
        async.flushMicrotasks();

        kit.source.emit('Document', const Offline());
        async.flushMicrotasks();

        expect(kit.cache.getState(_folderGet, _f1).syncStatus, const Synced());
      });
    });

    test(
      'keeps the same state object while the folded status is unchanged',
      () {
        fakeAsync((async) {
          final kit = _build(
            (_, _) => [
              {'id': 'd1', 'title': 'a'},
            ],
          );
          final seen = <QueryState<Object?>>[];

          kit.cache.setPrincipal('alice');
          kit.cache.watch(_documentList, TagContext.empty).listen(seen.add);
          async.flushMicrotasks();

          kit.source.emit('Document', const Pending(2));
          async.flushMicrotasks();

          final before = kit.cache.getState(_documentList, TagContext.empty);
          final emitted = seen.length;

          // The fold builds a fresh Pending(2) on every read. Equal is enough.
          kit.cache.notifyChanged();
          kit.source.emit('Document', const Pending(2));
          async.flushMicrotasks();

          expect(
            kit.cache.getState(_documentList, TagContext.empty),
            same(before),
          );
          expect(seen, hasLength(emitted));
        });
      },
    );

    test('drops the previous principal’s status the moment it changes', () {
      fakeAsync((async) {
        final kit = _build(
          (_, _) => [
            {'id': 'd1', 'title': 'a'},
          ],
        );
        final seen = <SyncStatus>[];

        kit.cache.setPrincipal('alice');
        kit.cache
            .watch(_documentList, TagContext.empty)
            .listen((state) => seen.add(state.syncStatus));
        async.flushMicrotasks();

        kit.source.emit('Document', const Pending(2));
        async.flushMicrotasks();
        expect(seen.last, const Pending(2));

        final mark = seen.length;

        kit.cache.setPrincipal('bob');

        // Alice's source is still running until the transition stops it; what
        // it says now is about alice.
        kit.source.emit('Document', const Pending(5));
        async.flushMicrotasks();

        expect(seen.length, greaterThan(mark));
        expect(seen.skip(mark), everyElement(const Synced()));
      });
    });
  });

  group('configureClient', () {
    tearDown(() => setClient(null));

    test('forwards the sync sources and the storage', () {
      fakeAsync((async) {
        final log = <String>[];
        final cache = configureClient(
          transport: FakeTransport((_, _) => null),
          entities: _schema,
          syncSources: [
            _FakeSource({'Document'}, log: log),
          ],
          storage: _LoggingStorage(log),
        );

        cache.setPrincipal('alice');
        async.flushMicrotasks();

        expect(cache.owns('Document'), isTrue);
        expect(cache.session?.principal, 'alice');
        expect(log, ['open alice', 'start alice']);
      });
    });
  });
}
