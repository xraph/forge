import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:test/test.dart';

HLC _hlc(int ts, String node) => HLC(BigInt.from(ts), 0, node);

DocumentState _doc(String table, String pk, [String? name]) => DocumentState(
  table: table,
  pk: pk,
  fields: {
    if (name != null)
      'name': FieldState(
        type: CrdtType.lww,
        hlc: _hlc(100, 'n1'),
        nodeId: 'n1',
        value: JsonValue(name),
      ),
  },
);

PendingChange _pending(String table, String pk, String value) => PendingChange(
  ChangeRecord(
    table: table,
    pk: pk,
    field: 'name',
    crdtType: CrdtType.lww,
    hlc: _hlc(200, 'n1'),
    nodeId: 'n1',
    value: JsonValue(value),
  ),
);

var _issued = 0;

/// Distinct node ids without the system random source. `uuid`'s secure
/// generator throws under the Node test runner (dart2js there runs with a
/// module `this`), so only the VM tests use the default generator.
String _nextNodeId() => 'dart-test-${_issued++}';

Future<ReplicaSpace> _open(StorageSession? session) =>
    ReplicaSpace.open(session, newNodeId: _nextNodeId);

/// A namespace that counts how its writes arrive.
final class _CountingStore implements KeyValueStore {
  _CountingStore(this._inner);

  final KeyValueStore _inner;
  var batches = 0;
  var singleWrites = 0;

  @override
  Future<String?> get(String key) => _inner.get(key);

  @override
  Future<void> put(String key, String value) {
    singleWrites++;

    return _inner.put(key, value);
  }

  @override
  Future<void> delete(String key) {
    singleWrites++;

    return _inner.delete(key);
  }

  @override
  Future<Map<String, String>> scan(String prefix) => _inner.scan(prefix);

  @override
  Future<void> batch(void Function(KeyValueBatch batch) build) {
    batches++;

    return _inner.batch(build);
  }
}

void main() {
  group('ReplicaSpace', () {
    test('without a session the replica is memory-only', () async {
      final space = await _open(null);

      expect(space.persistent, isFalse);
      expect(space.nodeId, startsWith('dart-'));
    });

    test(
      'generates a distinct dart-<uuid v4> node id per principal by default',
      () async {
        final storage = memoryStorage();
        final alice = await storage.open('alice');
        final bob = await storage.open('bob');
        addTearDown(alice.close);
        addTearDown(bob.close);

        final aliceId = (await ReplicaSpace.open(alice)).nodeId;
        final bobId = (await ReplicaSpace.open(bob)).nodeId;

        final uuidV4 = RegExp(
          r'^dart-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-'
          r'[0-9a-f]{12}$',
        );

        expect(aliceId, matches(uuidV4));
        expect(bobId, matches(uuidV4));
        expect(aliceId, isNot(bobId));
      },
      testOn: 'vm',
    );

    test('with a session the replica is persistent', () async {
      final session = await memoryStorage().open('alice');
      addTearDown(session.close);

      expect((await _open(session)).persistent, isTrue);
    });

    test('the node id persists per principal in the grove namespace', () async {
      final storage = memoryStorage();
      final alice = await storage.open('alice');
      final first = await _open(alice);
      final again = await _open(alice);

      expect(again.nodeId, first.nodeId);
      expect(await alice.namespace('grove').get('node'), first.nodeId);

      final bob = await storage.open('bob');

      expect((await _open(bob)).nodeId, isNot(first.nodeId));
    });

    test('the node id survives the session being reopened', () async {
      final storage = memoryStorage();
      final first = await storage.open('alice');
      final id = (await _open(first)).nodeId;

      await first.close();

      final second = await storage.open('alice');
      addTearDown(second.close);

      expect((await _open(second)).nodeId, id);
    });

    test('a stored node id wins over the generator', () async {
      final session = await memoryStorage().open('alice');
      addTearDown(session.close);

      final first = await ReplicaSpace.open(session, newNodeId: () => 'dart-1');
      final again = await ReplicaSpace.open(
        session,
        newNodeId: () => fail('a node id was already stored'),
      );

      expect(first.nodeId, 'dart-1');
      expect(again.nodeId, 'dart-1');
    });

    test(
      'two principals on one device share no node id and no replica',
      () async {
        final storage = memoryStorage();
        final aliceSession = await storage.open('alice');
        final bobSession = await storage.open('bob');
        addTearDown(aliceSession.close);
        addTearDown(bobSession.close);

        final alice = await _open(aliceSession);
        final bob = await _open(bobSession);

        expect(alice.nodeId, isNot(bob.nodeId));

        // The same dataset id for both principals: their replicas stay apart.
        await alice
            .dataset('ds1')
            .saveDocument('t', '1', _doc('t', '1', 'alice only'));
        await alice.dataset('ds1').savePendingChanges([
          _pending('t', '1', 'alice only'),
        ]);

        expect(await bob.dataset('ds1').loadState(), isEmpty);
        expect(await bob.dataset('ds1').loadPendingChanges(), isEmpty);
        expect(await bobSession.namespace('grove/ds1').scan(''), isEmpty);

        // Nothing of alice's is anywhere in bob's session.
        for (final name in ['grove', 'grove/ds1']) {
          final entries = await bobSession.namespace(name).scan('');

          expect('$entries', isNot(contains(alice.nodeId)));
          expect('$entries', isNot(contains('alice only')));
        }

        // Erasing bob's copy of the dataset leaves alice's alone.
        await bob.dataset('ds1').saveDocument('t', '1', _doc('t', '1', 'bob'));
        await bob.erase('ds1');

        expect((await alice.dataset('ds1').loadState())['t']!.keys, ['1']);
        expect(await aliceSession.namespace('grove').get('node'), alice.nodeId);
        expect(await bobSession.namespace('grove').get('node'), bob.nodeId);
      },
    );

    test(
      'each dataset has its own namespace and erase empties only that one',
      () async {
        final session = await memoryStorage().open('alice');
        addTearDown(session.close);

        final space = await _open(session);

        await space.dataset('a').saveDocument('ds_a', '1', _doc('ds_a', '1'));
        await space.dataset('b').saveDocument('ds_b', '2', _doc('ds_b', '2'));

        expect(await session.namespace('grove/a').scan(''), isNotEmpty);

        await space.erase('a');

        expect(await session.namespace('grove/a').scan(''), isEmpty);
        expect((await space.dataset('b').loadState())['ds_b']!.keys, ['2']);
        expect(await session.namespace('grove').get('node'), space.nodeId);
      },
    );

    test(
      'a replica key naming a pull path does not collide with a dataset id',
      () async {
        final session = await memoryStorage().open('alice');
        addTearDown(session.close);

        final space = await _open(session);

        await space.dataset('a').saveDocument('t', '1', _doc('t', '1'));
        await space.dataset('~a').saveDocument('t', '2', _doc('t', '2'));
        await space.erase('~a');

        expect((await space.dataset('a').loadState())['t']!.keys, ['1']);
        expect(await space.dataset('~a').loadState(), isEmpty);
      },
    );

    test(
      'without a session each dataset still has a separate replica',
      () async {
        final space = await _open(null);

        await space.dataset('a').saveDocument('t', '1', _doc('t', '1'));

        expect(await space.dataset('b').loadState(), isEmpty);
        expect((await space.dataset('a').loadState())['t']!.keys, ['1']);

        await space.erase('a');

        expect(await space.dataset('a').loadState(), isEmpty);
      },
    );
  });

  group('ForgeKeyValueAdapter', () {
    late KeyValueStore store;
    late ForgeKeyValueAdapter adapter;

    setUp(() async {
      final session = await memoryStorage().open('alice');
      addTearDown(session.close);

      store = session.namespace('grove');
      adapter = ForgeKeyValueAdapter(store);
    });

    test('delegates reads and writes', () async {
      await adapter.put('a', '1');
      await adapter.put('ab', '2');
      await adapter.put('b', '3');

      expect(await adapter.get('a'), '1');
      expect(await store.get('ab'), '2');
      expect(await adapter.scan('a'), {'a': '1', 'ab': '2'});

      await adapter.delete('a');

      expect(await adapter.get('a'), isNull);
    });

    test('delegates batch writes', () async {
      await adapter.batch(
        (b) => b
          ..put('x', '1')
          ..put('y', '2')
          ..delete('x'),
      );

      expect(await adapter.scan(''), {'y': '2'});
    });

    test('a batch that throws applies nothing', () async {
      await adapter.put('keep', '1');

      await expectLater(
        adapter.batch((b) {
          b.put('x', '1');
          b.delete('keep');

          throw StateError('abort');
        }),
        throwsStateError,
      );

      expect(await adapter.scan(''), {'keep': '1'});
    });

    test('a write recorded after the builder returned throws', () async {
      late ReplicaKeyValueBatch escaped;

      await adapter.batch((b) {
        escaped = b;
        b.put('x', '1');
      });

      expect(() => escaped.put('y', '2'), throwsStateError);
      expect(() => escaped.delete('x'), throwsStateError);
      expect(await adapter.scan(''), {'x': '1'});
    });
  });

  // E4: the replica storage grove_crdt ships, over a real forge_client session.
  // A round trip over the plan 04 SQLite session belongs to plan 07's harness.
  group('KeyValueReplicaStorage over a forge_client session', () {
    test('round-trips documents, the pending queue, cursors and meta across a reopen', () async {
      final storage = memoryStorage();
      final first = await storage.open('alice');
      final space = await _open(first);
      final replica = space.dataset('ds1');

      await replica.saveDocument('notes', 'a/b', _doc('notes', 'a/b', 'slash'));
      await replica.saveDocument(
        'notes',
        'café \u{1F600}',
        _doc('notes', 'café \u{1F600}', 'unicode'),
      );
      await replica.saveDocument('tags', '1', _doc('tags', '1'));
      await replica.deleteDocument('tags', '1');
      await replica.savePendingChanges([_pending('notes', 'a/b', 'slash')]);
      await replica.writeCursor('notes', _hlc(500, 'srv'));
      await replica.writeMeta('epoch', '3');

      await first.close();

      final second = await storage.open('alice');
      addTearDown(second.close);

      final reopened = (await _open(second)).dataset('ds1');
      final state = await reopened.loadState();

      expect(state.keys, ['notes']);
      expect(state['notes']!.keys, unorderedEquals(['a/b', 'café \u{1F600}']));
      expect(
        state['notes']!['a/b']!.fields['name']!.value,
        const JsonValue('slash'),
      );
      expect(
        state['notes']!['café \u{1F600}']!.fields['name']!.value,
        const JsonValue('unicode'),
      );
      expect(
        (await reopened.loadPendingChanges()).single.change.value,
        const JsonValue('slash'),
      );
      expect((await reopened.readCursor('notes'))!.ts, BigInt.from(500));
      expect(await reopened.readCursor('missing'), isNull);
      expect(await reopened.readMeta('epoch'), '3');
    });

    test('commit is one batch and no single write', () async {
      final session = await memoryStorage().open('alice');
      addTearDown(session.close);

      final counting = _CountingStore(session.namespace('grove/ds1'));
      final replica = KeyValueReplicaStorage(ForgeKeyValueAdapter(counting));

      await replica.saveDocument('notes', '2', _doc('notes', '2', 'old'));
      counting
        ..batches = 0
        ..singleWrites = 0;

      await replica.commit(
        documents: {
          ('notes', '1'): _doc('notes', '1', 'new'),
          ('notes', '2'): null,
        },
        pending: [_pending('notes', '1', 'new')],
      );

      expect(counting.batches, 1);
      expect(counting.singleWrites, 0);

      final state = await replica.loadState();

      expect(state['notes']!.keys, ['1']);
      expect((await replica.loadPendingChanges()).single.change.pk, '1');
    });

    test('clearAll empties the namespace through the adapter', () async {
      final session = await memoryStorage().open('alice');
      addTearDown(session.close);

      final namespace = session.namespace('grove/ds1');
      final replica = KeyValueReplicaStorage(ForgeKeyValueAdapter(namespace));

      await replica.saveDocument('notes', '1', _doc('notes', '1'));
      await replica.writeCursor('notes', _hlc(1, 'srv'));
      await replica.writeMeta('epoch', '1');
      await replica.savePendingChanges([_pending('notes', '1', 'x')]);

      expect(await namespace.scan(''), isNotEmpty);

      await replica.clearAll();

      expect(await namespace.scan(''), isEmpty);
    });

    test(
      'an unreadable stored document is reported, not thrown, when asked',
      () async {
        final session = await memoryStorage().open('alice');
        addTearDown(session.close);

        final namespace = session.namespace('grove/ds1');
        final replica = KeyValueReplicaStorage(ForgeKeyValueAdapter(namespace));

        await replica.saveDocument('notes', '1', _doc('notes', '1'));
        await namespace.put('doc/notes/2', 'not json');

        final problems = <FormatException>[];
        final state = await replica.loadState(onUnreadable: problems.add);

        expect(state['notes']!.keys, ['1']);
        expect(problems, hasLength(1));
        await expectLater(replica.loadState(), throwsFormatException);
      },
    );
  });
}
