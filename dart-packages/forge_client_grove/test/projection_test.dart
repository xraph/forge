import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:test/test.dart';

import 'support/kit.dart';

DocumentState doc(
  String pk,
  Map<String, Object?> fields, {
  bool tombstone = false,
}) => DocumentState(
  table: 'notes',
  pk: pk,
  tombstone: tombstone,
  fields: {
    for (final e in fields.entries)
      e.key: FieldState(
        type: CrdtType.lww,
        hlc: HLC(BigInt.one, 0, 'n'),
        nodeId: 'n',
        value: JsonValue(e.value),
      ),
  },
);

/// Owns `Note` and keeps the context it was started with.
final class _CapturingSource implements SyncSource {
  final contexts = <SyncContext>[];

  @override
  Set<String> get entities => const {'Note'};

  @override
  Future<void> start(SyncContext context) async => contexts.add(context);

  @override
  Future<MutationOutcome> apply(PendingMutation mutation) async =>
      Rejected(StateError('not used'));

  @override
  Stream<SyncStatus> status(String entity) => Stream.value(const Synced());

  @override
  Future<void> stop() async {}
}

void main() {
  late QueryCache cache;
  late SyncContext context;
  late Projector projector;

  setUp(() {
    cache = QueryCache(transport: NoRest(), entities: noteEntities);
    context = SyncContext(
      cache: cache,
      principal: 'alice',
      transport: NoRest(),
      storage: null,
    );
    projector = Projector(
      entity: 'Note',
      binding: const GroveEntity(codec: NoteCodec()),
      wireIdKey: 'note_id',
    );
  });

  tearDown(() => cache.dispose());

  test('writes a client-shaped record keyed by entity and pk', () {
    projector.project(context, [
      doc('n1', {'title': 'Hello', 'view_count': 2}),
    ]);
    expect(cache.store.getRecord('Note:n1')!.data, {
      'noteId': 'n1',
      'title': 'Hello',
      'viewCount': 2,
    });
  });

  test('renames replica columns through the binding', () {
    final p = Projector(
      entity: 'Note',
      binding: const GroveEntity(
        codec: NoteCodec(),
        columns: {'views': 'view_count'},
      ),
      wireIdKey: 'note_id',
    );
    p.project(context, [
      doc('n1', {'views': 5}),
    ]);
    expect(cache.store.getRecord('Note:n1')!.data['viewCount'], 5);
  });

  test('evicts a tombstoned document', () {
    projector.project(context, [
      doc('n1', {'title': 'x'}),
    ]);
    projector.project(context, [
      doc('n1', {'title': 'x'}, tombstone: true),
    ]);
    expect(cache.store.getRecord('Note:n1'), isNull);
  });

  test('an unchanged projection keeps record identity', () {
    projector.project(context, [
      doc('n1', {'title': 'same'}),
    ]);
    final first = cache.store.getRecord('Note:n1')!.data;
    projector.project(context, [
      doc('n1', {'title': 'same'}),
    ]);
    expect(identical(cache.store.getRecord('Note:n1')!.data, first), isTrue);
  });

  test('rows of two datasets with the same pk get distinct keys', () {
    final a = Projector(
      entity: 'Note',
      binding: const GroveEntity(codec: NoteCodec()),
      wireIdKey: 'note_id',
      datasetId: 'a',
    );
    final b = Projector(
      entity: 'Note',
      binding: const GroveEntity(codec: NoteCodec()),
      wireIdKey: 'note_id',
      datasetId: 'b',
    );
    a.project(context, [
      doc('r1', {'title': 'from a'}),
    ]);
    b.project(context, [
      doc('r1', {'title': 'from b'}),
    ]);
    expect(cache.store.getRecord('Note:a:r1')!.data, {
      'noteId': 'a:r1',
      'title': 'from a',
    });
    expect(cache.store.getRecord('Note:b:r1')!.data, {
      'noteId': 'b:r1',
      'title': 'from b',
    });
  });

  test('a tombstone evicts only its own dataset row', () {
    final a = Projector(
      entity: 'Note',
      binding: const GroveEntity(codec: NoteCodec()),
      wireIdKey: 'note_id',
      datasetId: 'a',
    );
    final b = Projector(
      entity: 'Note',
      binding: const GroveEntity(codec: NoteCodec()),
      wireIdKey: 'note_id',
      datasetId: 'b',
    );
    a.project(context, [
      doc('r1', {'title': 'from a'}),
    ]);
    b.project(context, [
      doc('r1', {'title': 'from b'}),
    ]);
    a.project(context, [doc('r1', {}, tombstone: true)]);
    expect(cache.store.getRecord('Note:a:r1'), isNull);
    expect(cache.store.getRecord('Note:b:r1'), isNotNull);
  });

  test('clientRecord is the Applied response shape', () {
    expect(projector.clientRecord(doc('n1', {'title': 't'})), {
      'noteId': 'n1',
      'title': 't',
    });
  });

  test('wireRecord resolves every field type', () {
    final d = DocumentState(
      table: 'notes',
      pk: 'n1',
      fields: {
        'view_count': FieldState(
          type: CrdtType.counter,
          hlc: HLC.zero,
          nodeId: 'n',
          counterState: const PnCounterState(inc: {'a': 3}, dec: {'b': 1}),
        ),
      },
    );
    expect(wireRecord(d, const GroveEntity(codec: NoteCodec()), 'note_id'), {
      'view_count': 2,
      'note_id': 'n1',
    });
  });

  test('a record the store never held is stamped with the batch frame', () {
    projector.project(context, [
      doc('n1', {'title': 'a'}),
      doc('n2', {'title': 'b'}),
    ]);
    final first = cache.store.getRecord('Note:n1')!.frameAt;
    expect(first, isNotNull);
    expect(first, greaterThan(0));
    expect(cache.store.getRecord('Note:n2')!.frameAt, first);
  });

  test('a batch notifies the cache once', () async {
    var commits = 0;
    final sub = cache.commits.listen((_) => commits++);
    addTearDown(sub.cancel);

    projector.project(context, [
      doc('n1', {'title': 'a'}),
      doc('n2', {'title': 'b'}),
    ]);
    await pumpEventQueue();

    expect(commits, 1);
  });

  test('projecting nothing does not notify', () async {
    var commits = 0;
    final sub = cache.commits.listen((_) => commits++);
    addTearDown(sub.cancel);

    projector.project(context, const []);
    await pumpEventQueue();

    expect(commits, 0);
  });

  test('a document the codec cannot decode leaves no partial projection', () {
    final bad = Projector(
      entity: 'Note',
      binding: const GroveEntity(codec: _ThrowingCodec()),
      wireIdKey: 'note_id',
    );

    expect(
      () => bad.project(context, [
        doc('n1', {'title': 'a'}),
      ]),
      throwsStateError,
    );
    expect(cache.store.getRecord('Note:n1'), isNull);
  });

  group('a field the replica no longer has', () {
    test('disappears from the store, keeping the record and one frame', () {
      projector.project(context, [
        doc('n1', {'title': 'x', 'view_count': 3}),
      ]);

      final before = cache.store.getRecord('Note:n1')!;

      projector.project(context, [
        doc('n1', {'title': 'x'}),
      ]);

      final after = cache.store.getRecord('Note:n1')!;

      expect(after.data, {'noteId': 'n1', 'title': 'x'});
      expect(after.frameAt, greaterThan(before.frameAt!));
    });

    test('also goes when the replica dropped it with dropField', () {
      final replica = CrdtStore('n', HybridClock('n'));

      replica.setField('notes', 'n1', 'title', 'x');
      replica.setField('notes', 'n1', 'view_count', 3);
      projector.project(context, replica.exportTable('notes').values);

      expect(cache.store.getRecord('Note:n1')!.data['viewCount'], 3);

      replica.dropField('notes', 'n1', 'view_count');
      projector.project(context, replica.exportTable('notes').values);

      expect(cache.store.getRecord('Note:n1')!.data, {
        'noteId': 'n1',
        'title': 'x',
      });
    });

    test('is one notification, and not a tombstone', () async {
      projector.project(context, [
        doc('n1', {'title': 'x', 'view_count': 3}),
      ]);
      await pumpEventQueue();

      var commits = 0;
      final sub = cache.commits.listen((_) => commits++);
      addTearDown(sub.cancel);

      projector.project(context, [
        doc('n1', {'title': 'x'}),
      ]);
      await pumpEventQueue();

      expect(commits, 1);
      expect(cache.store.has('Note:n1'), isTrue);
    });

    test('a record that lost nothing keeps its identity', () {
      projector.project(context, [
        doc('n1', {'title': 'x', 'view_count': 3}),
      ]);

      final first = cache.store.getRecord('Note:n1')!.data;

      projector.project(context, [
        doc('n1', {'title': 'x', 'view_count': 3}),
      ]);

      expect(identical(cache.store.getRecord('Note:n1')!.data, first), isTrue);

      // A changed value is not a dropped field: it is merged in place.
      projector.project(context, [
        doc('n1', {'title': 'y', 'view_count': 3}),
      ]);

      expect(cache.store.getRecord('Note:n1')!.version, 2);
    });
  });

  group('a record that cannot be keyed', () {
    test('throws instead of being dropped, and writes nothing', () {
      final other = Projector(
        entity: 'Note',
        binding: const GroveEntity(codec: _RenamingCodec()),
        wireIdKey: 'note_id',
      );

      expect(
        () => other.project(context, [
          doc('a', {'title': 'x'}),
          doc('b', {'title': 'y'}),
        ]),
        throwsStateError,
      );
      expect(cache.store.size, 0);
    });

    test('an entity missing from the schema throws too', () {
      final stranger = Projector(
        entity: 'Folder',
        binding: const GroveEntity(codec: NoteCodec()),
        wireIdKey: 'note_id',
      );

      expect(
        () => stranger.project(context, [
          doc('n1', {'title': 'x'}),
        ]),
        throwsStateError,
      );
    });
  });

  group('collection invalidation', () {
    late ListTransport transport;
    late QueryCache listing;
    late SyncContext listingContext;
    late StreamSubscription<QueryState<Object?>> watching;

    setUp(() async {
      transport = ListTransport();
      listing = QueryCache(transport: transport, entities: noteEntities);
      listingContext = SyncContext(
        cache: listing,
        principal: 'alice',
        transport: transport,
        storage: null,
      );
      watching = listing.watch(opListNotes, TagContext.empty).listen((_) {});
      await pumpEventQueue();
    });

    tearDown(() async {
      await watching.cancel();
      await listing.dispose();
    });

    test('dropping a field does not refetch the lists', () async {
      projector.project(listingContext, [
        doc('n1', {'title': 'a', 'view_count': 1}),
      ]);
      await pumpEventQueue();

      final before = transport.requests;

      projector.project(listingContext, [
        doc('n1', {'title': 'a'}),
      ]);
      await pumpEventQueue();

      expect(transport.requests, before);
      expect(
        listing.store.getRecord('Note:n1')!.data.containsKey('viewCount'),
        isFalse,
      );
    });

    test('a new record refetches the lists, an edit does not', () async {
      expect(transport.requests, 1);

      projector.project(listingContext, [
        doc('n1', {'title': 'a'}),
      ]);
      await pumpEventQueue();

      expect(transport.requests, 2, reason: 'a record appeared');

      projector.project(listingContext, [
        doc('n1', {'title': 'edited'}),
      ]);
      await pumpEventQueue();

      expect(transport.requests, 2, reason: 'a field edit keeps membership');

      projector.project(listingContext, [doc('n1', {}, tombstone: true)]);
      await pumpEventQueue();

      expect(transport.requests, 3, reason: 'a record disappeared');

      projector.project(listingContext, [doc('n1', {}, tombstone: true)]);
      await pumpEventQueue();

      expect(
        transport.requests,
        3,
        reason: 'evicting what is already gone changes nothing',
      );
    });
  });

  group('after the principal moved on', () {
    test('the cache fences a source context on a principal switch (writes nothing, notifies nothing)', () async {
      final source = _CapturingSource();
      final owned = QueryCache(
        transport: NoRest(),
        entities: noteEntities,
        syncSources: [source],
      );
      addTearDown(owned.dispose);

      owned.setPrincipal('alice');
      await owned.idle;

      final alice = source.contexts.single;

      Projector(
        entity: 'Note',
        binding: const GroveEntity(codec: NoteCodec()),
        wireIdKey: 'note_id',
      ).project(alice, [
        doc('n1', {'title': 'alice row'}),
      ]);
      expect(owned.store.getRecord('Note:n1'), isNotNull);

      owned.setPrincipal('bob');

      // The switch is synchronous: alice's context is fenced before the store
      // empties, and a projection that arrives afterwards lands nowhere.
      expect(alice.active, isFalse);

      var commits = 0;
      final sub = owned.commits.listen((_) => commits++);
      addTearDown(sub.cancel);

      Projector(
        entity: 'Note',
        binding: const GroveEntity(codec: NoteCodec()),
        wireIdKey: 'note_id',
      ).project(alice, [
        doc('n2', {'title': 'late alice row'}),
        doc('n1', {}, tombstone: true),
      ]);

      // Before the cache's own sweep of owned records can hide a leak.
      expect(owned.store.getRecord('Note:n2'), isNull);

      await owned.idle;
      await pumpEventQueue();

      expect(owned.store.getRecord('Note:n1'), isNull);
      expect(
        owned.store.getRecord('Note:n2'),
        isNull,
        reason: 'alice rows must not reach bob\'s store',
      );
      expect(source.contexts, hasLength(2));
      expect(source.contexts.last.principal, 'bob');
      expect(owned.store.keys.where((k) => k.startsWith('Note:')), isEmpty);
      expect(
        commits,
        lessThanOrEqualTo(1),
        reason: 'only the switch itself may notify',
      );
    });

    test(
      'an inactive context does not invalidate collections either',
      () async {
        final source = _CapturingSource();
        final transport = ListTransport();
        final owned = QueryCache(
          transport: transport,
          entities: noteEntities,
          syncSources: [source],
        );
        addTearDown(owned.dispose);

        owned.setPrincipal('alice');
        await owned.idle;

        final sub = owned.watch(opListNotes, TagContext.empty).listen((_) {});
        addTearDown(sub.cancel);
        await pumpEventQueue();

        final alice = source.contexts.single;

        owned.setPrincipal('bob');
        await owned.idle;
        await pumpEventQueue();

        final before = transport.requests;

        Projector(
          entity: 'Note',
          binding: const GroveEntity(codec: NoteCodec()),
          wireIdKey: 'note_id',
        ).project(alice, [
          doc('n1', {'title': 'late'}),
        ]);
        await pumpEventQueue();

        expect(transport.requests, before);
      },
    );
  });
}

final class _ThrowingCodec implements WireCodec {
  const _ThrowingCodec();

  @override
  Object? decode(Object? wire) => throw StateError('cannot decode');

  @override
  Object? encode(Object? client) => client;
}

/// Decodes the id to a key the entity does not use.
final class _RenamingCodec implements WireCodec {
  const _RenamingCodec();

  @override
  Object? decode(Object? wire) => {
    for (final e in (wire! as Map<String, Object?>).entries)
      (e.key == 'note_id' ? 'identifier' : e.key): e.value,
  };

  @override
  Object? encode(Object? client) => client;
}
