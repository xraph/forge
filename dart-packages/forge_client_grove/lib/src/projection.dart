import 'package:forge_client/forge_client.dart';
import 'package:grove_crdt/grove_crdt.dart';

import 'config.dart';

/// The server-shaped record of a replica document. Rows of a dataset carry
/// the composite store id (see [compositeId]).
///
/// Every value comes from [resolveFieldValue], which returns a deep copy, so
/// the record is the caller's to change.
Map<String, Object?> wireRecord(
  DocumentState doc,
  GroveEntity binding,
  String wireIdKey, {
  String datasetId = '',
}) => {
  for (final e in doc.fields.entries)
    (binding.columns[e.key] ?? e.key): resolveFieldValue(e.value),
  wireIdKey: datasetId.isEmpty ? doc.pk : compositeId(datasetId, doc.pk),
};

/// Writes replica documents of one entity into a cache's store.
///
/// Every store edit goes through [SyncContext.write], the fence the cache
/// closes synchronously when it moves to another principal. A projection
/// started for one principal therefore never lands in the next principal's
/// store, however late it runs.
final class Projector {
  /// Creates a projector for [entity].
  Projector({
    required this.entity,
    required this.binding,
    required this.wireIdKey,
    this.datasetId = '',
  });

  /// The entity typename.
  final String entity;

  /// Its mapping.
  final GroveEntity binding;

  /// The server JSON key of its id.
  final String wireIdKey;

  /// The dataset whose rows this projector writes; empty for none.
  final String datasetId;

  /// The store id of row [pk].
  String storeId(String pk) =>
      datasetId.isEmpty ? pk : compositeId(datasetId, pk);

  /// The client-shaped record of [doc].
  Object? clientRecord(DocumentState doc) => binding.codec.decode(
    wireRecord(doc, binding, wireIdKey, datasetId: datasetId),
  );

  /// Projects [docs] into [context]'s store: writes live documents and evicts
  /// tombstoned ones, all under one frame stamp.
  ///
  /// Does nothing once [context] is inactive: no write, no invalidation, no
  /// notification. The collection tag `'<entity>[]'` is invalidated only when
  /// membership changed (a record new to the store, or an eviction), so a field
  /// edit re-renders dependents without refetching lists. The watchers are
  /// notified by [SyncContext.write] after the edit.
  ///
  /// Records are decoded before the edit starts, so a document the codec
  /// cannot decode throws without leaving a partial projection behind.
  void project(SyncContext context, Iterable<DocumentState> docs) {
    if (!context.active) return;

    final entries = [
      for (final doc in docs)
        (
          key: '$entity:${storeId(doc.pk)}',
          record: doc.tombstone ? null : clientRecord(doc),
        ),
    ];

    if (entries.isEmpty) return;

    final entities = context.cache.entities;
    var membershipChanged = false;

    context.write((store) {
      final stamp = store.nextFrame();

      for (final (:key, :record) in entries) {
        if (record == null) {
          if (store.getRecord(key) != null) membershipChanged = true;

          store.evict(key, stamp);
        } else {
          if (store.getRecord(key) == null) membershipChanged = true;

          store.write(record, entities, entity, CommitOptions(frameAt: stamp));
        }
      }
    });

    if (membershipChanged && context.active) {
      context.cache.invalidate(['$entity[]']);
    }
  }
}
