import 'package:forge_client/forge_client.dart';

import '../field_writer_test.dart' show NoteCodec;

export '../field_writer_test.dart' show NoteCodec;

/// No REST in these tests: every request fails loudly.
final class NoRest implements Transport {
  @override
  Future<Object?> execute(TransportRequest request) =>
      throw StateError('unexpected REST ${request.meta.id}');
}

const noteEntities = <String, EntityMeta>{
  'Note': EntityMeta(idField: 'noteId'),
};

const opGetNote = OperationMeta(
  id: 'op_get_note',
  method: 'GET',
  path: '/notes/{noteId}',
  entity: 'Note',
  rootType: 'Note',
  provides: ['Note:{noteId}'],
  responseCodec: NoteCodec(),
);
const opUpdateNote = OperationMeta(
  id: 'op_update_note',
  method: 'PATCH',
  path: '/notes/{noteId}',
  entity: 'Note',
  rootType: 'Note',
  invalidates: ['Note:{noteId}'],
  bodyCodec: NoteCodec(),
  responseCodec: NoteCodec(),
);
const opCreateNote = OperationMeta(
  id: 'op_create_note',
  method: 'POST',
  path: '/notes',
  entity: 'Note',
  rootType: 'Note',
  bodyCodec: NoteCodec(),
  responseCodec: NoteCodec(),
);
const opDeleteNote = OperationMeta(
  id: 'op_delete_note',
  method: 'DELETE',
  path: '/notes/{noteId}',
  entity: 'Note',
  invalidates: ['Note:{noteId}'],
);

/// Answers every request with an empty list, and counts them.
final class ListTransport implements Transport {
  /// Requests received so far.
  var requests = 0;

  @override
  Future<Object?> execute(TransportRequest request) async {
    requests++;

    return <Object?>[];
  }
}

/// A list query that provides the `Note[]` collection tag.
const opListNotes = OperationMeta(
  id: 'op_list_notes',
  method: 'GET',
  path: '/notes',
  provides: ['Note[]'],
);
