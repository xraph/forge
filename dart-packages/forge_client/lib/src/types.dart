/// The vocabulary the entity store is written against.
///
/// Every name here is resolved in Go at generation time and shipped in the
/// generated `ops.dart`. Nothing in this file is inferred from the shape of a
/// response: identity was decided against real Go types, and re-deriving it
/// from JSON is the guess that keys two tenants' records to one entry.
library;

/// `Type:id`, e.g. `Order:7`. The store's primary key.
typedef EntityKey = String;

/// A client-shaped JSON object: after the wire codec, before `fromClient`.
typedef Json = Map<String, Object?>;

/// What the runtime knows about one named type.
///
/// [idField] is the JSON property that identifies a record of this type. A
/// null [idField] is a positive statement rather than missing data: the type
/// is a signpost (an envelope, or an intermediate hop on the way to an
/// entity), so `normalize` walks it for its [fields] and never stores it.
final class EntityMeta {
  /// Creates the metadata row for one typename.
  const EntityMeta({this.idField, this.fields = const {}});

  /// The property that identifies a record, or null for a signpost type.
  final String? idField;

  /// Field name to the typename of what that field holds (the element
  /// typename for a list).
  final Map<String, String> fields;
}

/// The typename-to-metadata table, the shape of the generated `entities`.
typedef EntitySchema = Map<String, EntityMeta>;

/// One row of the store: the entity's own fields, plus a write counter.
final class EntityRecord {
  /// Creates a record. [frameAt] is the frame-clock reading of the last stream
  /// frame that wrote it, or null when no frame ever has.
  const EntityRecord({required this.data, required this.version, this.frameAt});

  /// The record's fields. Never copied on read: treat it as frozen.
  final Json data;

  /// Bumps on every write that actually changed [data].
  final int version;

  /// When a stream frame last wrote this record. See `EntityStore.racedSince`.
  final int? frameAt;
}

/// What `normalize` produces. Pure: the input is not touched.
final class NormalizeResult {
  /// Creates a result.
  const NormalizeResult({
    required this.skeleton,
    required this.records,
    required this.deps,
  });

  /// The input tree with every recognised entity replaced by an `EntityRef`.
  final Object? skeleton;

  /// Every entity lifted out, keyed by [EntityKey].
  final Map<EntityKey, Json> records;

  /// Every entity key this pass touched, transitively.
  final Set<EntityKey> deps;
}
