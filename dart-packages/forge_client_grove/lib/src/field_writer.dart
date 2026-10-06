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
///
/// Throws [ArgumentError] when the codec does not encode the id field to
/// exactly one key.
String wireIdKeyFor(GroveEntity binding, String idField) {
  final encoded = binding.codec.encode({idField: ''});

  if (encoded is! Map<String, Object?> || encoded.length != 1) {
    throw ArgumentError.value(
      encoded,
      'binding',
      'the codec must encode {$idField: ""} to a map with one key, the '
          'server id key',
    );
  }

  return encoded.keys.single;
}

/// Maps a mutation on a Grove-backed entity to a [GroveWrite].
///
/// The target id is, in order: the entity `targetOf` names from the
/// operation's `invalidates` tags, the path parameter that spells the id
/// field, the client-shaped body's id field, on a dataset route the lone
/// remaining path parameter when it ends the path template, and for a `POST` a
/// new uuid v4 written into the body before encoding. A `PUT`, `PATCH` or
/// `DELETE` with
/// no resolvable id throws [ArgumentError], as does any other method, and a
/// codec that does not encode the body to a map.
///
/// [datasetParam] names the path parameter that selects the dataset (see
/// `datasetParam`). It is never a row id, so the path lookup skips it, and a
/// `POST` skips the path lookup altogether: the path parameters of a create
/// name its parents, not the new row.
GroveWrite toGroveWrite(
  PendingMutation m, {
  required String entity,
  required EntityMeta meta,
  required GroveEntity binding,
  required String wireIdKey,
  String? datasetParam,
  String Function()? newId,
}) {
  final method = m.meta.method.toUpperCase();
  final idField = meta.idField;
  final body = switch (m.args.body) {
    final Map<String, Object?> b => {...b},
    _ => <String, Object?>{},
  };

  final isCreate = method == 'POST';
  var id =
      _targetId(m, entity) ??
      (isCreate ? null : _pathId(m, idField, datasetParam));

  if (id == null && idField != null && isIdentity(body[idField])) {
    id = jsString(body[idField]);
  }

  // Last, and only for a route that ends in the row: see [_trailingRowParam].
  if (id == null && !isCreate) id = _trailingRowParam(m, datasetParam);

  switch (method) {
    case 'DELETE':
      if (id == null) {
        throw ArgumentError(
          'grove: ${m.meta.id} deletes $entity without a row id '
          '(${idField ?? 'no id field'})',
        );
      }

      return GroveDelete(id);
    case 'POST' || 'PUT' || 'PATCH':
      if (id == null) {
        if (method != 'POST') {
          throw ArgumentError(
            'grove: ${m.meta.id} writes $entity without a row id '
            '(${idField ?? 'no id field'})',
          );
        }

        id = (newId ?? const Uuid().v4)();

        if (idField != null) body[idField] = id;
      }

      final wire = binding.codec.encode(body);

      if (wire is! Map<String, Object?>) {
        throw ArgumentError.value(
          wire,
          'binding',
          'the codec must encode a $entity body to a map',
        );
      }

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

/// The path parameter that spells the id field, found the way tag lookups do
/// (the exact key, else the key that differs only in `_`, `-` and case), and
/// never the dataset parameter.
String? _pathId(PendingMutation m, String? idField, String? datasetParam) {
  if (idField == null) return null;

  final skip = datasetParam == null ? null : _fold(datasetParam);
  final wanted = _fold(idField);
  final path = m.args.path;
  final candidates = [
    for (final k in path.keys)
      if (skip == null || _fold(k) != skip) k,
  ];
  final key = candidates.contains(idField)
      ? idField
      : candidates.where((k) => _fold(k) == wanted).firstOrNull;

  if (key == null) return null;

  final value = path[key];

  return isIdentity(value) ? jsString(value) : null;
}

/// On a dataset route, the one parameter besides the dataset parameter, when
/// it is the final segment of the path template (`/datasets/{id}/rows/{rowId}`
/// names its row `rowId`, whatever the id field is called).
///
/// A parameter anywhere else names a parent (`/datasets/{id}/folders/
/// {folderId}/rows`), and guessing it would write or delete the wrong row, so
/// the route yields null and the caller fails loudly.
String? _trailingRowParam(PendingMutation m, String? datasetParam) {
  if (datasetParam == null) return null;

  final skip = _fold(datasetParam);
  final path = m.args.path;
  final candidates = [
    for (final k in path.keys)
      if (_fold(k) != skip) k,
  ];

  if (candidates.length != 1) return null;

  final key = candidates.single;

  if (!m.meta.path.endsWith('/{$key}')) return null;

  final value = path[key];

  return isIdentity(value) ? jsString(value) : null;
}

String _fold(String key) => key.replaceAll(RegExp('[_-]'), '').toLowerCase();
