import 'package:forge_client/forge_client.dart';
import 'package:uuid/uuid.dart';

import 'config.dart';

/// A REST-shaped write turned into replica terms.
sealed class GroveWrite {
  const GroveWrite(this.id);

  /// The record id.
  final String id;
}

/// Write these server-keyed fields of [id].
final class GroveUpsert extends GroveWrite {
  /// Creates the write.
  const GroveUpsert(super.id, this.wireFields);

  /// Server JSON key to value, id key excluded.
  final Map<String, Object?> wireFields;
}

/// Delete [id].
final class GroveDelete extends GroveWrite {
  /// Creates the delete.
  const GroveDelete(super.id);
}

/// The server JSON key of the client id field.
String wireIdKeyFor(GroveEntity binding, String idField) =>
    (binding.codec.encode({idField: ''})! as Map<String, Object?>).keys.single;

/// Maps a mutation on a Grove-backed entity to a [GroveWrite].
///
/// The target id is, in order: the entity `targetOf` names from the
/// operation's `invalidates` tags, the path parameter that spells the id
/// field, the client-shaped body's id field, and for a `POST` a new uuid v4
/// written into the body before encoding. A `PUT`, `PATCH` or `DELETE` with
/// no resolvable id throws [ArgumentError], as does any other method.
GroveWrite toGroveWrite(
  PendingMutation m, {
  required String entity,
  required EntityMeta meta,
  required GroveEntity binding,
  required String wireIdKey,
  String Function()? newId,
}) {
  final method = m.meta.method.toUpperCase();
  final idField = meta.idField;
  final body = switch (m.args.body) {
    final Map<String, Object?> b => {...b},
    _ => <String, Object?>{},
  };

  var id = _targetId(m, entity) ?? _pathId(m, idField);

  if (id == null && idField != null && isIdentity(body[idField])) {
    id = jsString(body[idField]);
  }

  switch (method) {
    case 'DELETE':
      if (id == null) {
        throw ArgumentError(
          'grove: ${m.meta.id} deletes $entity without an id',
        );
      }

      return GroveDelete(id);
    case 'POST' || 'PUT' || 'PATCH':
      if (id == null) {
        if (method != 'POST') {
          throw ArgumentError(
            'grove: ${m.meta.id} writes $entity without an id',
          );
        }

        id = (newId ?? const Uuid().v4)();

        if (idField != null) body[idField] = id;
      }

      final wire = binding.codec.encode(body)! as Map<String, Object?>;

      return GroveUpsert(id, {
        for (final e in wire.entries)
          if (e.key != wireIdKey) e.key: e.value,
      });
    default:
      throw ArgumentError(
        'grove: ${m.meta.method} ${m.meta.path} is not a write',
      );
  }
}

/// The id of the [entity] the operation's `invalidates` tags name, or null
/// when they name none, name another entity, or name several.
String? _targetId(PendingMutation m, String entity) {
  final EntityKey? key;

  try {
    key = targetOf(m.meta, m.args);
  } on AmbiguousTargetError {
    return null;
  }

  if (key == null) return null;

  final colon = key.indexOf(':');

  return key.substring(0, colon) == entity ? key.substring(colon + 1) : null;
}

/// The path parameter that spells the id field, found the way tag lookups do:
/// the exact key, else the key that differs only in `_`, `-` and case.
String? _pathId(PendingMutation m, String? idField) {
  if (idField == null) return null;

  final path = m.args.path;
  final wanted = _fold(idField);
  final key = path.containsKey(idField)
      ? idField
      : path.keys.where((k) => _fold(k) == wanted).firstOrNull;

  if (key == null) return null;

  final value = path[key];

  return isIdentity(value) ? jsString(value) : null;
}

String _fold(String key) => key.replaceAll(RegExp('[_-]'), '').toLowerCase();
