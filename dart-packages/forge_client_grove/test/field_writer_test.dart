import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:test/test.dart';

import 'support/kit.dart' show NoteCodec;

const meta = EntityMeta(idField: 'noteId');
const binding = GroveEntity(codec: NoteCodec());

PendingMutation mutation(
  String method,
  String path,
  TagContext args, {
  List<String> invalidates = const [],
}) => PendingMutation(
  id: 'm1',
  meta: OperationMeta(
    id: 'op',
    method: method,
    path: path,
    entity: 'Note',
    invalidates: invalidates,
  ),
  args: args,
  optimistic: null,
  idempotencyKey: 'k1',
  createdAt: DateTime.utc(2026, 10, 4),
);

void main() {
  final wireId = wireIdKeyFor(binding, 'noteId');

  test('wireIdKeyFor maps the client id field to its server key', () {
    expect(wireId, 'note_id');
  });

  test('PATCH with a path id upserts only the provided wire keys', () {
    final w = toGroveWrite(
      mutation(
        'PATCH',
        '/notes/{noteId}',
        const TagContext(path: {'noteId': 'n1'}, body: {'title': 'Hi'}),
      ),
      entity: 'Note',
      meta: meta,
      binding: binding,
      wireIdKey: wireId,
    );
    expect(
      w,
      isA<GroveUpsert>().having((u) => u.id, 'id', 'n1').having(
        (u) => u.wireFields,
        'fields',
        {'title': 'Hi'},
      ),
    );
  });

  test('POST without an id mints one and keeps it out of the fields', () {
    final w = toGroveWrite(
      mutation(
        'POST',
        '/notes',
        const TagContext(body: {'title': 'New', 'viewCount': 0}),
      ),
      entity: 'Note',
      meta: meta,
      binding: binding,
      wireIdKey: wireId,
      newId: () => 'fresh',
    ) as GroveUpsert;
    expect(w.id, 'fresh');
    expect(w.wireFields, {'title': 'New', 'view_count': 0});
  });

  test('POST with an id in the body uses it', () {
    final w = toGroveWrite(
      mutation(
        'POST',
        '/notes',
        const TagContext(body: {'noteId': 'given', 'title': 'x'}),
      ),
      entity: 'Note',
      meta: meta,
      binding: binding,
      wireIdKey: wireId,
    );
    expect(w.id, 'given');
  });

  test('DELETE becomes a GroveDelete', () {
    final w = toGroveWrite(
      mutation(
        'DELETE',
        '/notes/{noteId}',
        const TagContext(path: {'noteId': 'n1'}),
      ),
      entity: 'Note',
      meta: meta,
      binding: binding,
      wireIdKey: wireId,
    );
    expect(w, isA<GroveDelete>().having((d) => d.id, 'id', 'n1'));
  });

  test('PATCH without a resolvable id is refused', () {
    expect(
      () => toGroveWrite(
        mutation('PATCH', '/notes', const TagContext(body: {'title': 'x'})),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      ),
      throwsArgumentError,
    );
  });

  test('GET is refused', () {
    expect(
      () => toGroveWrite(
        mutation(
          'GET',
          '/notes/{noteId}',
          const TagContext(path: {'noteId': 'n1'}),
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      ),
      throwsArgumentError,
    );
  });

  group('id resolution', () {
    test(
      'the invalidates tag is tried first, then the path, then the body',
      () {
        final w = toGroveWrite(
          mutation(
            'PATCH',
            '/notes/{noteId}',
            const TagContext(
              path: {'noteId': 'from-path'},
              body: {'noteId': 'from-body', 'title': 'x'},
            ),
            invalidates: ['Note:{req.slug}'],
          ),
          entity: 'Note',
          meta: meta,
          binding: binding,
          wireIdKey: wireId,
        );
        // `{req.slug}` is absent, so the tag does not resolve and the path is next.
        expect(w.id, 'from-path');

        final tagged = toGroveWrite(
          mutation(
            'PATCH',
            '/notes/{slug}',
            const TagContext(
              path: {'slug': 'from-tag', 'noteId': 'from-path'},
              body: {'title': 'x'},
            ),
            invalidates: ['Note:{slug}'],
          ),
          entity: 'Note',
          meta: meta,
          binding: binding,
          wireIdKey: wireId,
        );
        expect(tagged.id, 'from-tag');
      },
    );

    test('a generated-style invalidates tag resolves the id', () {
      final w = toGroveWrite(
        mutation(
          'DELETE',
          '/notes/{noteId}',
          const TagContext(path: {'noteId': 'n9'}),
          invalidates: ['Note:{noteId}'],
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      );
      expect(w, isA<GroveDelete>().having((d) => d.id, 'id', 'n9'));
    });

    test('an id containing a colon survives intact', () {
      final w = toGroveWrite(
        mutation(
          'DELETE',
          '/notes/{noteId}',
          const TagContext(path: {'noteId': 'a:b'}),
          invalidates: ['Note:{noteId}'],
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      );
      expect(w.id, 'a:b');
    });

    test('a path parameter spelled differently still resolves the id', () {
      final w = toGroveWrite(
        mutation(
          'PATCH',
          '/notes/{note_id}',
          const TagContext(path: {'note_id': 'n2'}, body: {'title': 'x'}),
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      );
      expect(w.id, 'n2');
    });

    test('a tag naming another entity is not the target', () {
      final w = toGroveWrite(
        mutation(
          'PATCH',
          '/notes/{noteId}',
          const TagContext(
            path: {'noteId': 'n3', 'folderId': 'f1'},
            body: {'title': 'x'},
          ),
          invalidates: ['Folder:{folderId}'],
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      );
      expect(w.id, 'n3');
    });

    test('a numeric path id is rendered as a string', () {
      final w = toGroveWrite(
        mutation(
          'DELETE',
          '/notes/{noteId}',
          const TagContext(path: {'noteId': 7}),
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      );
      expect(w.id, '7');
    });

    test('a mutation invalidating two entities falls back to the path', () {
      final w = toGroveWrite(
        mutation(
          'PATCH',
          '/notes/{noteId}',
          const TagContext(
            path: {'noteId': 'n4', 'other': 'o'},
            body: {'title': 'x'},
          ),
          invalidates: ['Note:{noteId}', 'Note:{other}'],
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      );
      expect(w.id, 'n4');
    });
  });

  test(
    'PUT upserts only the keys present and keeps unspecified fields out',
    () {
      final w = toGroveWrite(
        mutation(
          'PUT',
          '/notes/{noteId}',
          const TagContext(path: {'noteId': 'n1'}, body: {'title': 'Whole'}),
        ),
        entity: 'Note',
        meta: meta,
        binding: binding,
        wireIdKey: wireId,
      ) as GroveUpsert;
      expect(w.wireFields, {'title': 'Whole'});
    },
  );

  group('a dataset route', () {
    const rowMeta = EntityMeta(idField: 'id');
    const rowBinding = GroveEntity(codec: _PassCodec());
    final rowWireId = wireIdKeyFor(rowBinding, 'id');

    PendingMutation rowMutation(String method, String path, TagContext args) =>
        PendingMutation(
          id: 'm1',
          meta: OperationMeta(
            id: 'op',
            method: method,
            path: path,
            entity: 'Row',
          ),
          args: args,
          optimistic: null,
          idempotencyKey: 'k1',
          createdAt: DateTime.utc(2026, 10, 6),
        );

    GroveWrite write(PendingMutation m, {String Function()? newId}) =>
        toGroveWrite(
          m,
          entity: 'Row',
          meta: rowMeta,
          binding: rowBinding,
          wireIdKey: rowWireId,
          datasetParam: 'id',
          newId: newId,
        );

    test('two creates make two rows, neither keyed by the dataset id', () {
      const args = TagContext(path: {'id': 'ds1'}, body: {'name': 'x'});
      final ids = ['r1', 'r2'].iterator;
      String next() => (ids..moveNext()).current;

      final first = write(
        rowMutation('POST', '/datasets/{id}/rows', args),
        newId: next,
      );
      final second = write(
        rowMutation('POST', '/datasets/{id}/rows', args),
        newId: next,
      );

      expect([first.id, second.id], ['r1', 'r2']);
      expect((first as GroveUpsert).wireFields, {'name': 'x'});
    });

    test('a create with an id in the body keeps it', () {
      final w = write(
        rowMutation(
          'POST',
          '/datasets/{id}/rows',
          const TagContext(
            path: {'id': 'ds1'},
            body: {'id': 'given', 'name': 'x'},
          ),
        ),
      );

      expect(w.id, 'given');
    });

    test('a PATCH finds the row in the other path parameter', () {
      final w = write(
        rowMutation(
          'PATCH',
          '/datasets/{id}/rows/{rowId}',
          const TagContext(
            path: {'id': 'ds1', 'rowId': 'r7'},
            body: {'name': 'x'},
          ),
        ),
      );

      expect(w.id, 'r7');
    });

    test(
      'a PATCH whose only id-like parameter is the dataset fails loudly',
      () {
        expect(
          () => write(
            rowMutation(
              'PATCH',
              '/datasets/{id}/rows',
              const TagContext(path: {'id': 'ds1'}, body: {'name': 'x'}),
            ),
          ),
          throwsA(
            isA<ArgumentError>().having(
              (e) => '$e',
              'message',
              contains('row id'),
            ),
          ),
        );
        expect(
          () => write(
            rowMutation(
              'DELETE',
              '/datasets/{id}/rows',
              const TagContext(path: {'id': 'ds1'}),
            ),
          ),
          throwsArgumentError,
        );
      },
    );

    test('a parent parameter is never taken for the row', () {
      const parents = TagContext(path: {'id': 'ds1', 'folderId': 'f1'});

      expect(
        () => write(
          rowMutation(
            'DELETE',
            '/datasets/{id}/folders/{folderId}/rows',
            parents,
          ),
        ),
        throwsA(
          isA<ArgumentError>().having(
            (e) => '$e',
            'message',
            contains('row id'),
          ),
        ),
      );
      expect(
        () => write(
          rowMutation(
            'PATCH',
            '/datasets/{id}/folders/{folderId}/rows',
            const TagContext(
              path: {'id': 'ds1', 'folderId': 'f1'},
              body: {'name': 'x'},
            ),
          ),
        ),
        throwsArgumentError,
      );
    });

    test('the body id outranks a parent parameter', () {
      final w = write(
        rowMutation(
          'PUT',
          '/datasets/{id}/folders/{folderId}/rows',
          const TagContext(
            path: {'id': 'ds1', 'folderId': 'f1'},
            body: {'id': 'r9', 'name': 'x'},
          ),
        ),
      );

      expect(w.id, 'r9');
    });

    test('the body id outranks the trailing parameter fallback', () {
      final w = write(
        rowMutation(
          'PUT',
          '/datasets/{id}/rows/{rowId}',
          const TagContext(
            path: {'id': 'ds1', 'rowId': 'r7'},
            body: {'id': 'r9', 'name': 'x'},
          ),
        ),
      );

      expect(w.id, 'r9');
    });

    test('without a dataset param the path id still wins for a PATCH', () {
      final w = toGroveWrite(
        rowMutation(
          'PATCH',
          '/rows/{id}',
          const TagContext(path: {'id': 'r1'}, body: {'name': 'x'}),
        ),
        entity: 'Row',
        meta: rowMeta,
        binding: rowBinding,
        wireIdKey: rowWireId,
      );

      expect(w.id, 'r1');
    });
  });

  group('codec failures are ArgumentErrors', () {
    test('wireIdKeyFor with a codec that encodes to no single key', () {
      expect(
        () => wireIdKeyFor(const GroveEntity(codec: _FlatCodec()), 'noteId'),
        throwsArgumentError,
      );
    });

    test('a body that does not encode to a map', () {
      expect(
        () => toGroveWrite(
          mutation('POST', '/notes', const TagContext(body: {'title': 'x'})),
          entity: 'Note',
          meta: meta,
          binding: const GroveEntity(codec: _ScalarCodec()),
          wireIdKey: wireId,
          newId: () => 'n',
        ),
        throwsArgumentError,
      );
    });
  });
}

/// Wire and client shapes are the same.
final class _PassCodec implements WireCodec {
  const _PassCodec();

  @override
  Object? decode(Object? wire) => wire;

  @override
  Object? encode(Object? client) => client;
}

/// Encodes the id field to two keys.
final class _FlatCodec implements WireCodec {
  const _FlatCodec();

  @override
  Object? decode(Object? wire) => wire;

  @override
  Object? encode(Object? client) => {'a': 1, 'b': 2};
}

/// Encodes to a string.
final class _ScalarCodec implements WireCodec {
  const _ScalarCodec();

  @override
  Object? decode(Object? wire) => wire;

  @override
  Object? encode(Object? client) => 'nope';
}
