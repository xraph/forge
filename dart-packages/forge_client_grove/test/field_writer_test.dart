import 'package:forge_client/forge_client.dart';
import 'package:forge_client_grove/forge_client_grove.dart';
import 'package:test/test.dart';

/// Wire keys are snake_case, client keys camelCase.
final class NoteCodec implements WireCodec {
  const NoteCodec();

  static const _toClient = {
    'note_id': 'noteId',
    'title': 'title',
    'view_count': 'viewCount',
  };

  @override
  Object? decode(Object? wire) => {
    for (final e in (wire! as Map<String, Object?>).entries)
      (_toClient[e.key] ?? e.key): e.value,
  };

  @override
  Object? encode(Object? client) => {
    for (final e in (client! as Map<String, Object?>).entries)
      (_toClient.entries
              .firstWhere(
                (t) => t.value == e.key,
                orElse: () => MapEntry(e.key, e.key),
              )
              .key):
          e.value,
  };
}

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
}
