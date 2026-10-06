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
  /// A record replaces what the store held: owned records are written only
  /// from the replica (REST, frame and snapshot commits skip them), so a field
  /// the replica no longer has, for instance after `dropField`, must go. The
  /// store has no replace, so such a record is evicted and written again in
  /// the same edit and under the same frame stamp. A record that lost no field
  /// is written in place, which keeps its identity. Either way membership is
  /// judged from what the store held before the edit.
  ///
  /// Records are decoded and normalized before the edit starts, so a document
  /// the codec cannot decode throws without leaving a partial projection
  /// behind. A record that does not normalize under its store id (the codec's
  /// id key differs from the entity's id field, or [entity] is not in the
  /// cache's schema) throws a [StateError] rather than being dropped silently.
  void project(SyncContext context, Iterable<DocumentState> docs) {
    if (!context.active) return;

    final entities = context.cache.entities;
    final entries = <({String key, StagedWrite? staged})>[];

    for (final doc in docs) {
      final key = '$entity:${storeId(doc.pk)}';

      if (doc.tombstone) {
        entries.add((key: key, staged: null));

        continue;
      }

      final staged = normalize(clientRecord(doc), entity, entities);

      if (!staged.records.containsKey(key)) {
        throw StateError(
          'grove: the $entity record of ${doc.table}/${doc.pk} did not '
          'normalize to $key; check the codec and the entity id field',
        );
      }

      entries.add((key: key, staged: staged));
    }

    if (entries.isEmpty) return;

    var membershipChanged = false;

    context.write((store) {
      final stamp = store.nextFrame();

      for (final (:key, :staged) in entries) {
        final prev = store.getRecord(key);

        if (staged == null) {
          if (prev != null) membershipChanged = true;

          store.evict(key, stamp);

          continue;
        }

        if (prev == null) membershipChanged = true;

        final next = staged.records[key]!;

        if (prev != null && prev.data.keys.any((k) => !next.containsKey(k))) {
          store.evict(key, stamp);
        }

        store.commit(staged, CommitOptions(frameAt: stamp));
      }
    });

    // Defensive: the edit is synchronous, so this holds whenever the early
    // return above did not fire.
    if (membershipChanged && context.active) {
      context.cache.invalidate(['$entity[]']);
    }
  }
}
